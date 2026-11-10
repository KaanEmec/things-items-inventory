package auth_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

const pw = "correct horse battery"

type env struct {
	t   *testing.T
	d   *db.DB
	svc *auth.Service
	srv *httptest.Server
}

func newEnv(t *testing.T, cfg config.Config) *env {
	t.Helper()
	d := dbtest.New(t)
	svc := auth.New(d, cfg)
	return &env{t: t, d: d, svc: svc, srv: httpapitest.Server(t, svc.Routes)}
}

// client is one browser: its own cookie jar.
type client struct {
	t   *testing.T
	srv *httptest.Server
	hc  *http.Client
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	hc := *e.srv.Client()
	hc.Jar = jar
	return &client{t: e.t, srv: e.srv, hc: &hc}
}

func (c *client) do(method, path, body string, hdr ...string) (int, string, *http.Response) {
	c.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, c.srv.URL+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b), res
}

func (c *client) status(method, path, body string) int {
	c.t.Helper()
	s, _, _ := c.do(method, path, body)
	return s
}

func (c *client) json(method, path, body string, want int, out any) {
	c.t.Helper()
	s, b, _ := c.do(method, path, body)
	if s != want {
		c.t.Fatalf("%s %s: status %d, want %d: %s", method, path, s, want, b)
	}
	if out != nil {
		if err := json.Unmarshal([]byte(b), out); err != nil {
			c.t.Fatalf("%s %s: bad json %q", method, path, b)
		}
	}
}

func (c *client) login(email string) {
	c.t.Helper()
	c.json("POST", "/api/v1/auth/login", `{"email":"`+email+`","password":"`+pw+`"}`, 200, nil)
}

const setupBody = `{"name":"Ada","email":"Ada@Example.com","password":"` + pw + `","household_name":"Home","language":"en"}`

// seedHousehold creates a household; seedUser creates a user in it.
func seedHousehold(t *testing.T, d *db.DB, name string) string {
	t.Helper()
	hid, now := id.New(), "2026-01-01T00:00:00.000Z"
	if _, err := sqlc.New(d.Write).CreateHousehold(context.Background(), sqlc.CreateHouseholdParams{
		ID: hid, Name: name, Language: "en", Currency: "EUR", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return hid
}

func seedUser(t *testing.T, d *db.DB, name, email, householdID, role string) string {
	t.Helper()
	hash, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	uid, now := id.New(), "2026-01-01T00:00:00.000Z"
	q := sqlc.New(d.Write)
	if _, err := q.CreateUser(context.Background(), sqlc.CreateUserParams{
		ID: uid, Name: name, Email: email, PasswordHash: hash, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	addMember(t, d, uid, householdID, role)
	return uid
}

func addMember(t *testing.T, d *db.DB, userID, householdID, role string) {
	t.Helper()
	if err := sqlc.New(d.Write).CreateMembership(context.Background(), sqlc.CreateMembershipParams{
		HouseholdID: householdID, UserID: userID, Role: role, CreatedAt: "2026-01-01T00:00:00.000Z"}); err != nil {
		t.Fatal(err)
	}
}

func ts(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func (c *client) do2(method, path string) (string, int) {
	c.t.Helper()
	s, b, _ := c.do(method, path, "")
	return b, s
}
