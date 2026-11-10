package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/apidoc"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
)

// Routes outside /api/v1 that are deliberately not in the API description.
var undocumented = []string{"GET /mcp", "POST /mcp", "DELETE /mcp", "GET /api/docs"}

// TestAPIRouteCoverage fails when a registered /api/v1 route is missing from
// the API description (internal/apidoc/routes.go), or the description lists a
// route that is not registered.
func TestAPIRouteCoverage(t *testing.T) {
	d := dbtest.New(t)
	registered := httpapi.Patterns(Routes(Features(d, config.Config{}))...)
	documented := map[string]bool{}
	for _, o := range apidoc.Ops() {
		documented[o.Pattern()] = true
	}
	documented["GET /api/v1/openapi.json"] = true
	for _, p := range registered {
		if documented[p] || slices.Contains(undocumented, p) {
			continue
		}
		t.Errorf("route %q is registered but not described: add a line to internal/apidoc/routes.go", p)
	}
	for p := range documented {
		if !slices.Contains(registered, p) {
			t.Errorf("route %q is described but not registered", p)
		}
	}
}

// TestAPIExamples runs every curl example of the reference page as an access
// token and expects success; the IDs the examples mention are real rows.
func TestAPIExamples(t *testing.T) {
	ctx := context.Background()
	d := dbtest.New(t)
	srv := httptest.NewServer(Handler("test", "test", nil, d, config.Config{}))
	t.Cleanup(srv.Close)

	hid, uid := id.New(), id.New()
	var place, place2, box string
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		if _, err := q.CreateHousehold(ctx, sqlc.CreateHouseholdParams{ID: hid, Name: "Docs", Language: "en", Currency: "EUR", CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
			return err
		}
		if _, err := q.CreateUser(ctx, sqlc.CreateUserParams{ID: uid, Name: "Docs", Email: "docs@example.com", PasswordHash: "x", CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
			return err
		}
		if err := q.CreateMembership(ctx, sqlc.CreateMembershipParams{HouseholdID: hid, UserID: uid, Role: "owner", CreatedAt: stamp}); err != nil {
			return err
		}
		h, err := inventory.CreateHome(ctx, tx, hid, "Home", "")
		if err != nil {
			return err
		}
		p1, err := inventory.CreatePlace(ctx, tx, hid, inventory.PlaceInput{HomeID: h.ID, Kind: "room", Name: "Garage"})
		if err != nil {
			return err
		}
		p2, err := inventory.CreatePlace(ctx, tx, hid, inventory.PlaceInput{HomeID: h.ID, Kind: "room", Name: "Cellar"})
		if err != nil {
			return err
		}
		place, place2 = p1.ID, p2.ID
		bx, err := inventory.CreateBoxes(ctx, tx, hid, 1, 0, 0, inventory.BoxInput{})
		box = bx[0].ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	token := "stash_pat_" + strings.Repeat("D", 32)
	sum := sha256.Sum256([]byte(token))
	if err := sqlc.New(d.Write).CreateAccessToken(ctx, sqlc.CreateAccessTokenParams{ID: id.New(), HouseholdID: hid, CreatedAt: stamp,
		UpdatedAt: stamp, UserID: uid, Name: "docs", Scope: "write", TokenHash: hex.EncodeToString(sum[:]), Hint: "DDDD"}); err != nil {
		t.Fatal(err)
	}
	_ = box

	var item string
	for _, e := range apidoc.Examples {
		sub := func(s string) string {
			return strings.NewReplacer("$PLACE2", place2, "$PLACE", place, "$ITEM", item).Replace(s)
		}
		var body io.Reader
		ctype := ""
		switch {
		case e.Photo != "":
			var buf bytes.Buffer
			mw := multipart.NewWriter(&buf)
			for _, f := range strings.Fields(sub(e.Photo)) {
				k, v, _ := strings.Cut(f, "=")
				mw.WriteField(k, v)
			}
			fw, _ := mw.CreateFormFile("file", "photo.jpg")
			img := image.NewRGBA(image.Rect(0, 0, 40, 30))
			for x := range 40 {
				img.Set(x, 5, color.RGBA{200, 30, 30, 255})
			}
			jpeg.Encode(fw, img, nil)
			mw.Close()
			body, ctype = &buf, mw.FormDataContentType()
		case e.JSON != "":
			body, ctype = strings.NewReader(sub(e.JSON)), "application/json"
		}
		req, _ := http.NewRequest(e.Method, srv.URL+"/api/v1"+sub(e.Path), body)
		req.Header.Set("Authorization", "Bearer "+token)
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode/100 != 2 {
			t.Errorf("example %q: %s %s = %d %s", e.Title, e.Method, e.Path, res.StatusCode, b)
			continue
		}
		if e.Title == "Add an item" {
			var it struct{ ID string }
			json.Unmarshal(b, &it)
			item = it.ID
		}
	}
	if item == "" {
		t.Fatal("the add-item example returned no id")
	}
	// The page and the description are public and valid.
	for _, p := range []string{"/api/docs", "/api/v1/openapi.json"} {
		res, err := http.Get(srv.URL + p)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("GET %s: %v %v", p, err, res)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if p != "/api/docs" {
			var doc struct{ Paths map[string]any }
			if json.Unmarshal(b, &doc) != nil || len(doc.Paths) == 0 {
				t.Fatalf("openapi.json is not a description: %s", b[:min(len(b), 200)])
			}
		}
	}
}
