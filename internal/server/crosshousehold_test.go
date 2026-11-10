package server

// The cross-household test walks every route registered through
// httpapi (via Features in server.go) as a signed-in owner of household A
// and tries to reach or change household B's data. It needs no edit when a
// feature adds routes.
//
// How a feature plugs in:
//  1. Register its Routes in Features (server.go). Its patterns are then
//     tried automatically. Every {param} receives IDs that belong to
//     household B and must answer 404 (for routes that take a body, 400/422
//     only count if some other probe of the same route and ID reaches the
//     lookup and answers 404: a route that validates the body before it
//     looks the row up proves nothing). Every route also gets B's IDs in
//     request bodies and query strings under the ID-shaped keys in idKeys
//     (add a key there when a handler introduces a new one). No response may
//     contain B's marker strings.
//  2. Give the Feature a Seed that creates a couple of rows in the household
//     it is given and returns their IDs (SeedResult.PathValues) and unique
//     text from their JSON (SeedResult.Markers). Without a Seed the test
//     only proves that unknown IDs give 404.
//  3. Nothing else: after every route the test compares a dump of every row
//     of B in every table that has a household_id column (found through
//     the schema, so new tables are covered) and checks that no row of
//     another household references B's IDs. A route that changes B's data
//     or links A's rows to B's fails even if it answers 404.
//  4. A route that is public on purpose (no session needed) must be added to
//     publicRoutes below with a reason; a route that answers an anonymous
//     request with anything but 401 fails the test until it is listed.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/auth"
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

// idKeys are the ID-shaped keys the app's request bodies and query strings
// use. Household B's IDs are sent under each of them to every route.
var idKeys = []string{"id", "household_id", "place_id", "parent_id", "home_id", "item_id", "box_id",
	"goes_to_id", "photo_id", "target_id", "user_id", "move_to"}

// sampleBodies are valid request bodies for routes that check the body
// before they look the row up (so `{}` never reaches the lookup). The test
// fails a body route whose every probe is rejected as malformed, naming it;
// add its pattern here with a body that passes validation. Only needed for
// such routes.
var sampleBodies = map[string]string{
	"PATCH /api/v1/members/{user_id}": `{"role":"viewer"}`,
}

func hasBody(method string) bool {
	return method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
}

func TestCrossHousehold(t *testing.T) {
	d := dbtest.New(t)
	features := Features(d, config.Config{})
	if violations := checkCrossHousehold(t, d, features); len(violations) > 0 {
		t.Fatalf("%d violation(s):\n%s", len(violations), strings.Join(violations, "\n"))
	}
}

// leakyAuthed wraps a deliberately broken handler the way real routes are
// wrapped, so the self-tests below fail only on the household check, never
// on the anonymous-401 check.
func leakyAuthed(h http.HandlerFunc) http.HandlerFunc {
	return auth.RequireSession(auth.ActionSession, h)
}

// TestCrossHouseholdCatchesALeak proves the checker fails on an
// authenticated route that forgets the household filter, and says so about
// household B's data (not about missing auth).
func TestCrossHouseholdCatchesALeak(t *testing.T) {
	d := dbtest.New(t)
	features := append(Features(d, config.Config{}), Feature{
		Name: "leaky",
		Routes: func(mux *httpapi.Mux) {
			// Signed in as a member of household A, but no household filter:
			// returns any household's name.
			mux.HandleFunc("GET /api/v1/leaky/{id}", leakyAuthed(func(w http.ResponseWriter, r *http.Request) {
				var name string
				d.Read.QueryRow(`SELECT name FROM households WHERE id = ?`, r.PathValue("id")).Scan(&name)
				httpapi.WriteJSON(w, 200, map[string]string{"name": name})
			}))
		},
	})
	violations := checkCrossHousehold(t, d, features)
	if !hasViolation(violations, "/api/v1/leaky/{id}", "of household B") {
		t.Fatalf("the leaky route's leak of household B's marker was not reported:\n%s", strings.Join(violations, "\n"))
	}
	if hasViolation(violations, "anonymous") {
		t.Fatalf("the leaky route must be authenticated, but an anonymous violation was reported:\n%s", strings.Join(violations, "\n"))
	}
	if len(violations) != countViolations(violations, "/api/v1/leaky/{id}") {
		t.Fatalf("a real route was reported; the app is leaking:\n%s", strings.Join(violations, "\n"))
	}
}

// TestCrossHouseholdCatchesASilentWrite proves a route that changes household
// B's row but answers 404 (and returns nothing) is caught by the row
// snapshot.
func TestCrossHouseholdCatchesASilentWrite(t *testing.T) {
	d := dbtest.New(t)
	features := append(Features(d, config.Config{}), Feature{
		Name: "silent-writer",
		Routes: func(mux *httpapi.Mux) {
			mux.HandleFunc("PATCH /api/v1/leaky/{id}", leakyAuthed(func(w http.ResponseWriter, r *http.Request) {
				d.Write.Exec(`UPDATE households SET name = 'pwned' WHERE id = ?`, r.PathValue("id"))
				httpapi.WriteError(w, httpapi.NotFound("No such thing."))
			}))
		},
	})
	violations := checkCrossHousehold(t, d, features)
	if !hasViolation(violations, "/api/v1/leaky/{id}", "households", "changed") {
		t.Fatalf("the silent write to household B was not reported:\n%s", strings.Join(violations, "\n"))
	}
	if hasViolation(violations, "anonymous") {
		t.Fatalf("unexpected anonymous violation:\n%s", strings.Join(violations, "\n"))
	}
}

// TestCrossHouseholdCatchesAForeignLink proves that a route that stores
// household B's ID in one of household A's rows is caught even though it
// answers 404 and leaks nothing.
func TestCrossHouseholdCatchesAForeignLink(t *testing.T) {
	d := dbtest.New(t)
	features := append(Features(d, config.Config{}), Feature{
		Name: "linker",
		Routes: func(mux *httpapi.Mux) {
			mux.HandleFunc("PATCH /api/v1/leaky/{id}", leakyAuthed(func(w http.ResponseWriter, r *http.Request) {
				hid := auth.MustIdentity(r.Context()).HouseholdID
				d.Write.Exec(`INSERT OR REPLACE INTO ai_profiles (household_id, role, model, updated_at) VALUES (?, 'vision', ?, ?)`, hid, r.PathValue("id"), stamp)
				httpapi.WriteError(w, httpapi.NotFound("No such thing."))
			}))
		},
	})
	violations := checkCrossHousehold(t, d, features)
	if !hasViolation(violations, "/api/v1/leaky/{id}", "references") {
		t.Fatalf("the link from household A's row to household B's ID was not reported:\n%s", strings.Join(violations, "\n"))
	}
}

func hasViolation(violations []string, parts ...string) bool {
	return countViolations(violations, parts...) > 0
}

func countViolations(violations []string, parts ...string) (n int) {
next:
	for _, v := range violations {
		for _, p := range parts {
			if !strings.Contains(v, p) {
				continue next
			}
		}
		n++
	}
	return n
}

func checkCrossHousehold(t *testing.T, d *db.DB, features []Feature) (violations []string) {
	t.Helper()
	ctx := context.Background()
	hA, hB := seedHousehold(t, d, "Household A"), seedHousehold(t, d, "B-Secret-Household")
	userA := seedUser(t, d, "Alice", "alice@example.com", hA, "owner")
	userB := seedUser(t, d, "B-Secret-User", "b-secret@example.com", hB, "owner")
	values := []string{hB, userB}
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
	// Probe values: B's real IDs, deduplicated, plus one ID that exists
	// nowhere (path-only: it must give 404 like a foreign row).
	var real []string
	seen := map[string]bool{}
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			real = append(real, v)
		}
	}
	candidatesWithParams := append(append([]string{}, real...), id.New())

	srv := httpapitest.Server(t, Routes(features)...)
	patterns := httpapi.Patterns(Routes(features)...)
	if len(patterns) < 5 {
		t.Fatalf("suspiciously few routes: %v", patterns)
	}
	reported := map[string]bool{}
	violate := func(format string, a ...any) {
		if msg := fmt.Sprintf(format, a...); !reported[msg] {
			reported[msg] = true
			violations = append(violations, msg)
		}
	}

	tables := householdOwnedTables(t, d)
	if len(tables) == 0 {
		t.Fatal("found no household-owned tables; the snapshot is broken")
	}
	baseline := snapshotB(t, d, tables, hB, userB)
	linked := map[string]bool{}
	// afterRoute reports any change to B's rows and any row of another
	// household that holds one of B's IDs, then resets the baseline so one
	// bad route does not blame the next.
	afterRoute := func(pattern string) {
		now := snapshotB(t, d, tables, hB, userB)
		for _, name := range sortedKeys(now) {
			if now[name] != baseline[name] {
				violate("%s: changed household B's data in table %s", pattern, name)
			}
		}
		baseline = now
		for _, l := range foreignLinks(t, d, tables, hB, append([]string{hB, userB}, real...)) {
			if !linked[l] {
				linked[l] = true
				violate("%s: %s", pattern, l)
			}
		}
	}

	for _, pattern := range patterns {
		method, path, _ := strings.Cut(pattern, " ")
		hasParams := paramRe.MatchString(path)
		_, public := publicRoutes[pattern]

		// Anonymous: 401 unless the route is on the public list.
		status, _ := send(t, srv, method, paramRe.ReplaceAllString(path, "x"), "", "")
		if public {
			if status >= 500 {
				violate("%s anonymous: %d", pattern, status)
			}
		} else if status != 401 {
			violate("%s answers an anonymous request with %d, want 401 (list it in publicRoutes if it is meant to be public)", pattern, status)
		}

		// As A's owner, with B's IDs in the path, in bodies and in query
		// strings.
		candidates := []string{""}
		if hasParams {
			candidates = candidatesWithParams
		}
		// try sends one request and applies the checks every request shares.
		// label describes it. It returns the status.
		try := func(v, label, p, body string) int {
			ensureHousehold(t, d, hA, userA) // an earlier route (DELETE /household) may have removed it
			status, resp := send(t, srv, method, p, sessionFor(t, d, userA, hA), body)
			if status >= 500 {
				violate("%s with %q%s: server error %d", pattern, v, label, status)
			}
			for _, m := range markers {
				if strings.Contains(resp, m) {
					violate("%s with %q%s: response contains %q of household B", pattern, v, label, m)
				}
			}
			if hasParams && !public && status != 404 && status != 400 && status != 422 && status < 500 {
				violate("%s with %q: status %d, want 404 for a row of another household (first seen%s)", pattern, v, status, label)
			}
			return status
		}
		for _, v := range candidates {
			p := paramRe.ReplaceAllString(path, v)
			reached := false // some request got as far as the row lookup (404)
			body := ""
			if hasBody(method) {
				body = "{}"
			}
			status := try(v, "", p, body)
			reached = reached || status == 404
			if sample, ok := sampleBodies[pattern]; ok {
				status = try(v, " sample body", p, sample)
				reached = reached || status == 404
			}
			if hasParams && !public && !hasBody(method) && status == 400 {
				violate("%s with %q: status 400, want 404 for a row of another household", pattern, v)
			}

			// ID-shaped keys, one at a time, in the query string and in a
			// body, holding B's ID (the path holds the same ID).
			if v != "" && !isNew(v, real) {
				continue
			}
			for _, key := range idKeys {
				q := "?" + key + "=" + url.QueryEscape(v)
				status = try(v, " ?"+key, p+q, body)
				reached = reached || status == 404
				if hasBody(method) {
					status = try(v, " body "+key, p, `{"`+key+`":"`+v+`"}`)
					reached = reached || status == 404
				}
			}
			if hasParams && !public && hasBody(method) && !reached {
				violate("%s with %q: every request answered 400/422, so no probe reached the row lookup; it cannot be shown to be household-scoped (add a valid body to sampleBodies)", pattern, v)
			}
		}
		// ids: lists of B's IDs, repeated and comma-separated, in a body and
		// in a query string.
		pv := real[0]
		p := paramRe.ReplaceAllString(path, pv)
		try(pv, " ?ids", p+"?ids="+url.QueryEscape(strings.Join(real, ",")), "")
		try(pv, " ?ids repeated", p+"?ids="+strings.Join(mapStrings(real, url.QueryEscape), "&ids="), "")
		if hasBody(method) {
			list, _ := json.Marshal(real)
			try(pv, " body ids", p, `{"ids":`+string(list)+`}`)
		}
		afterRoute(pattern)
	}
	return violations
}

func isNew(v string, real []string) bool { return slices.Contains(real, v) }

func mapStrings(in []string, f func(string) string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = f(s)
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// householdOwnedTables lists every table with a household_id column, found
// through the schema (so a feature's new table, FTS tables included, is
// covered without a test edit).
func householdOwnedTables(t *testing.T, d *db.DB) []string {
	t.Helper()
	rows, err := d.Write.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	rows.Close()
	var out []string
	for _, n := range names {
		cols, err := d.Write.Query(`SELECT name FROM pragma_table_info(?)`, n)
		if err != nil {
			t.Fatal(err)
		}
		for cols.Next() {
			var c string
			cols.Scan(&c)
			if c == "household_id" {
				out = append(out, n)
			}
		}
		cols.Close()
	}
	return out
}

// dump returns every row of query as a sorted list of strings.
func dump(t *testing.T, d *db.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := d.Write.Query(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%q", vals))
	}
	sort.Strings(out)
	return out
}

// snapshotB hashes, per table, every row that belongs to household B: rows
// with household_id = B in every household-owned table, B's households row
// and B's owner's users row.
func snapshotB(t *testing.T, d *db.DB, tables []string, hB, userB string) map[string]string {
	t.Helper()
	hash := func(rows []string) string {
		sum := sha256.Sum256([]byte(strings.Join(rows, "\n")))
		return hex.EncodeToString(sum[:])
	}
	out := map[string]string{
		"households": hash(dump(t, d, `SELECT * FROM households WHERE id = ?`, hB)),
		"users":      hash(dump(t, d, `SELECT * FROM users WHERE id = ?`, userB)),
	}
	for _, name := range tables {
		out[name] = hash(dump(t, d, `SELECT * FROM "`+name+`" WHERE household_id = ?`, hB))
	}
	return out
}

// foreignLinks describes rows of other households that contain one of B's
// IDs in any column.
func foreignLinks(t *testing.T, d *db.DB, tables []string, hB string, bIDs []string) (out []string) {
	t.Helper()
	for _, name := range tables {
		for _, row := range dump(t, d, `SELECT * FROM "`+name+`" WHERE household_id <> ?`, hB) {
			for _, v := range bIDs {
				if strings.Contains(row, v) {
					out = append(out, fmt.Sprintf("a row of another household in table %s references household B's ID %s: %s", name, v, row))
					break
				}
			}
		}
	}
	return out
}

// send makes one request with a throwaway client. token is the session
// cookie, or "" for an anonymous request. path may carry a query string.
// Methods that take a body get body, or an empty JSON object when it is "".
func send(t *testing.T, srv *httptest.Server, method, path, token, body string) (int, string) {
	t.Helper()
	var rd io.Reader
	if hasBody(method) {
		if body == "" {
			body = "{}"
		}
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, rd)
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
