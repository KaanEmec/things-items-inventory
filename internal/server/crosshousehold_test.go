package server

// The cross-household test walks every route registered through
// httpapi (via Features in server.go) as a signed-in owner of household A
// and tries to reach household B's data. It needs no edit when a feature adds
// routes.
//
// How a feature package plugs in:
//  1. Register its Routes in Features (server.go). Its patterns are then
//     tried automatically: every {param} receives IDs that belong to
//     household B and must answer 404 (400/422 for a malformed request is
//     also accepted); other routes must never return B's marker strings.
//  2. Give the Feature a Seed that creates a couple of rows in the household
//     it is given and returns their IDs (SeedResult.PathValues) and unique
//     text from their JSON (SeedResult.Markers). Without a Seed the test
//     only proves that unknown IDs give 404.
//  3. A route that is public on purpose (no session needed) must be added to
//     publicRoutes below with a reason; a route that answers an anonymous
//     request with anything but 401 fails the test until it is listed.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

// publicRoutes are the only routes that may answer without a session.
var publicRoutes = map[string]string{
	"GET /api/v1/setup":       "first-run status",
	"POST /api/v1/setup":      "first-run setup, only while no user exists",
	"POST /api/v1/auth/login": "login",
	"POST /api/v1/auth/reset": "one-time reset link",

	"GET /api/v1/invites/{token}":         "invite link preview; the secret token is the credential",
	"POST /api/v1/invites/{token}/accept": "invite link accept; the secret token is the credential",
}

var paramRe = regexp.MustCompile(`\{[^}]+\}`)

const stamp = "2026-01-01T00:00:00.000Z"

func TestCrossHousehold(t *testing.T) {
	d := dbtest.New(t)
	features := Features(d, config.Config{})
	if violations := checkCrossHousehold(t, d, features); len(violations) > 0 {
		t.Fatalf("%d violation(s):\n%s", len(violations), strings.Join(violations, "\n"))
	}
}

// TestCrossHouseholdCatchesALeak proves the checker fails on a route that
// forgets the household filter.
func TestCrossHouseholdCatchesALeak(t *testing.T) {
	d := dbtest.New(t)
	features := append(Features(d, config.Config{}), Feature{
		Name: "leaky",
		Routes: func(mux *httpapi.Mux) {
			// No auth, no household filter: returns every household's name.
			mux.HandleFunc("GET /api/v1/leaky/{id}", func(w http.ResponseWriter, r *http.Request) {
				var name string
				d.Read.QueryRow(`SELECT name FROM households WHERE id = ?`, r.PathValue("id")).Scan(&name)
				httpapi.WriteJSON(w, 200, map[string]string{"name": name})
			})
		},
	})
	if violations := checkCrossHousehold(t, d, features); len(violations) == 0 {
		t.Fatal("the leaky route was not caught")
	}
}

func checkCrossHousehold(t *testing.T, d *db.DB, features []Feature) (violations []string) {
	t.Helper()
	ctx := context.Background()
	hA, hB := seedHousehold(t, d, "Household A"), seedHousehold(t, d, "B-Secret-Household")
	userA := seedUser(t, d, "Alice", "alice@example.com", hA, "owner")
	userB := seedUser(t, d, "B-Secret-User", "b-secret@example.com", hB, "owner")
	values := []string{hB, userB, id.New()}
	markers := []string{"B-Secret-Household", "B-Secret-User", "b-secret@example.com", hB, userB}
	for _, f := range features {
		if f.Seed == nil {
			continue
		}
		res, err := f.Seed(ctx, d, hB)
		if err != nil {
			t.Fatalf("seed %s: %v", f.Name, err)
		}
		values = append(values, res.PathValues...)
		markers = append(markers, res.Markers...)
	}

	srv := httpapitest.Server(t, Routes(features)...)
	patterns := httpapi.Patterns(Routes(features)...)
	if len(patterns) < 5 {
		t.Fatalf("suspiciously few routes: %v", patterns)
	}
	violate := func(format string, a ...any) { violations = append(violations, fmt.Sprintf(format, a...)) }

	for _, pattern := range patterns {
		method, path, _ := strings.Cut(pattern, " ")
		hasParams := paramRe.MatchString(path)

		// Anonymous: 401 unless the route is on the public list.
		status, _ := send(t, srv, method, paramRe.ReplaceAllString(path, "x"), "")
		if _, public := publicRoutes[pattern]; public {
			if status >= 500 {
				violate("%s anonymous: %d", pattern, status)
			}
		} else if status != 401 {
			violate("%s answers an anonymous request with %d, want 401 (list it in publicRoutes if it is meant to be public)", pattern, status)
		}

		// As A's owner, with B's IDs in every parameter position.
		candidates := []string{""}
		if hasParams {
			candidates = values
		}
		for _, v := range candidates {
			p := paramRe.ReplaceAllString(path, v)
			ensureHousehold(t, d, hA, userA) // an earlier route (DELETE /household) may have removed it
			status, body := send(t, srv, method, p, sessionFor(t, d, userA, hA))
			switch {
			case status >= 500:
				violate("%s with %q: server error %d", pattern, v, status)
			case hasParams && publicRoutes[pattern] == "" && status != 404 && status != 400 && status != 422:
				violate("%s with %q: status %d, want 404 for a row of another household", pattern, v, status)
			}
			for _, m := range markers {
				if strings.Contains(body, m) {
					violate("%s with %q: response contains %q of household B", pattern, v, m)
				}
			}
		}
	}
	return violations
}

// send makes one request with a throwaway client. cookie is the session
// token, or "" for an anonymous request. Writes get an empty JSON object.
func send(t *testing.T, srv *httptest.Server, method, path, token string) (int, string) {
	t.Helper()
	var body io.Reader
	if method == "POST" || method == "PUT" || method == "PATCH" {
		body = strings.NewReader("{}")
	}
	req, err := http.NewRequest(method, srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "session", Value: token})
	}
	jar, _ := cookiejar.New(nil)
	hc := *srv.Client()
	hc.Jar = jar
	res, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// sessionFor stores a fresh session (so routes that end sessions cannot
// affect the next request) and returns its cookie value.
func sessionFor(t *testing.T, d *db.DB, userID, householdID string) string {
	t.Helper()
	token := id.New() + id.New()
	sum := sha256.Sum256([]byte(token))
	err := sqlc.New(d.Write).CreateSession(context.Background(), sqlc.CreateSessionParams{
		ID: id.New(), TokenHash: hex.EncodeToString(sum[:]), UserID: userID, HouseholdID: householdID,
		CreatedAt: stamp, LastSeenAt: "2099-01-01T00:00:00.000Z", ExpiresAt: "2099-01-01T00:00:00.000Z"})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func seedHousehold(t *testing.T, d *db.DB, name string) string {
	t.Helper()
	hid := id.New()
	if _, err := sqlc.New(d.Write).CreateHousehold(context.Background(), sqlc.CreateHouseholdParams{
		ID: hid, Name: name, Language: "en", Currency: "EUR", CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
		t.Fatal(err)
	}
	return hid
}

func seedUser(t *testing.T, d *db.DB, name, email, householdID, role string) string {
	t.Helper()
	ctx := context.Background()
	uid := id.New()
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		if _, err := q.CreateUser(ctx, sqlc.CreateUserParams{ID: uid, Name: name, Email: email,
			PasswordHash: "x", CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
			return err
		}
		return q.CreateMembership(ctx, sqlc.CreateMembershipParams{HouseholdID: householdID, UserID: uid, Role: role, CreatedAt: stamp})
	})
	if err != nil {
		t.Fatal(err)
	}
	return uid
}

// ensureHousehold recreates household A and its owner membership when a
// route deleted them.
func ensureHousehold(t *testing.T, d *db.DB, householdID, ownerID string) {
	t.Helper()
	if _, err := d.Write.Exec(`INSERT OR IGNORE INTO households (id, name, created_at, updated_at) VALUES (?, 'Household A', ?, ?)`,
		householdID, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Write.Exec(`INSERT OR IGNORE INTO memberships (household_id, user_id, role, created_at) VALUES (?, ?, 'owner', ?)`,
		householdID, ownerID, stamp); err != nil {
		t.Fatal(err)
	}
}
