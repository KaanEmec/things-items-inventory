package export

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
)

const sha = "ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12"

func TestExportZipHasDataAndNoSecrets(t *testing.T) {
	d := dbtest.New(t)
	dir := t.TempDir()
	svc := New(d, config.Config{DataDir: dir})
	ctx := context.Background()
	const hid, other = "h1", "h2"
	for _, h := range []string{hid, other} {
		if _, err := sqlc.New(d.Write).CreateHousehold(ctx, sqlc.CreateHouseholdParams{ID: h, Name: "H", Language: "en", Currency: "EUR", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Seed(ctx, d, hid); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Seed(ctx, d, other); err != nil {
		t.Fatal(err)
	}
	secrets := []string{"PWHASH-SECRET", "SESSION-SECRET", "TOKENHASH-SECRET", "INVITE-SECRET", "AITOKEN-SECRET"}
	for _, q := range []string{
		`INSERT INTO users (id, name, email, password_hash, created_at, updated_at) VALUES ('u1', 'Ann', 'a@x.test', 'PWHASH-SECRET', 't', 't')`,
		`INSERT INTO memberships (household_id, user_id, role, created_at) VALUES ('h1', 'u1', 'owner', 't')`,
		`INSERT INTO sessions (id, token_hash, user_id, household_id, created_at, last_seen_at, expires_at) VALUES ('s1', 'SESSION-SECRET', 'u1', 'h1', 't', 't', 't')`,
		`INSERT INTO access_tokens (id, household_id, created_at, updated_at, user_id, name, scope, token_hash, hint) VALUES ('a1', 'h1', 't', 't', 'u1', 'n', 'read', 'TOKENHASH-SECRET', 'hint')`,
		`INSERT INTO invites (id, household_id, created_at, updated_at, kind, token_hash, expires_at, created_by) VALUES ('i1', 'h1', 't', 't', 'invite', 'INVITE-SECRET', 't', 'u1')`,
		`INSERT INTO ai_profiles (household_id, role, token_enc, token_hint, updated_at) VALUES ('h1', 'vision', 'AITOKEN-SECRET', 'AITOKEN-SECRET', 't')`,
		`INSERT INTO photos (id, household_id, created_at, updated_at, sha256, mime, ext, bytes) VALUES ('p1', 'h1', 't', 't', '` + sha + `', 'image/jpeg', 'jpg', 5)`,
	} {
		if _, err := d.Write.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "photos", sha[:2]), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "photos", sha[:2], sha+".jpg"), []byte("JPEG!"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := svc.Write(ctx, &buf, hid); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		names[f.Name] = string(b)
		for _, s := range secrets {
			if strings.Contains(string(b), s) || strings.Contains(f.Name, s) {
				t.Errorf("%s contains secret %s", f.Name, s)
			}
		}
	}
	for _, n := range []string{"items.csv", "places.csv", "boxes.csv", "contents.csv", "history.csv", "inventory.json", "README.txt", "photos/p1.jpg"} {
		if _, ok := names[n]; !ok {
			t.Errorf("zip lacks %s", n)
		}
	}
	if !strings.Contains(names["items.csv"], "Secret-ExportItem-7Q") || names["photos/p1.jpg"] != "JPEG!" {
		t.Errorf("items.csv or photo wrong: %q", names["items.csv"])
	}
	if strings.Count(names["items.csv"], "Secret-ExportItem-7Q") != 1 {
		t.Errorf("other household's rows leaked into items.csv")
	}
	if !strings.Contains(names["inventory.json"], "Ann") {
		t.Errorf("members missing")
	}
}

func TestExportRoles(t *testing.T) {
	d := dbtest.New(t)
	svc := New(d, config.Config{DataDir: t.TempDir()})
	if _, err := sqlc.New(d.Write).CreateHousehold(context.Background(), sqlc.CreateHouseholdParams{ID: "h1", Name: "H", Language: "en", Currency: "EUR", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatal(err)
	}
	srv := httpapitest.Server(t, func(mux *httpapi.Mux) {
		mux.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				role, _ := auth.ParseRole(r.Header.Get("X-Role"))
				r = r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{UserID: "u1", HouseholdID: "h1", Role: role, Source: auth.SourceSession}))
				next.ServeHTTP(w, r)
			})
		})
	}, svc.Routes)
	for role, want := range map[string]int{"viewer": 403, "member": 403, "admin": 200, "owner": 200} {
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/export", nil)
		req.Header.Set("X-Role", role)
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("%s: status %d, want %d", role, res.StatusCode, want)
		}
	}
}

func TestSafeCell(t *testing.T) {
	for in, want := range map[string]string{"=1+1": "'=1+1", "+a": "'+a", "-a": "'-a", "@a": "'@a", "\tx": "'\tx", "\rx": "'\rx", "plain": "plain", "": ""} {
		if got := cell(in); got != want {
			t.Errorf("cell(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExportOneAtATime(t *testing.T) {
	d := dbtest.New(t)
	svc := New(d, config.Config{DataDir: t.TempDir()})
	svc.busy.Store("h1", true)
	srv := httpapitest.Server(t, func(mux *httpapi.Mux) {
		mux.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r = r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{UserID: "u1", HouseholdID: "h1", Role: auth.RoleOwner, Source: auth.SourceSession}))
				next.ServeHTTP(w, r)
			})
		})
	}, svc.Routes)
	if st, _ := httpapitest.Do(t, srv, "GET", "/api/v1/export", ""); st != 409 {
		t.Errorf("status %d, want 409", st)
	}
}
