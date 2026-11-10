package auth_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
)

type memberList struct {
	Members []struct {
		UserID string `json:"user_id"`
		Email  string
		Role   string
	}
	Invites []struct{ ID, Role string }
}

func TestMembersListAndRoles(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	owner := seedUser(t, e.d, "Olga", "olga@example.com", hid, "owner")
	admin := seedUser(t, e.d, "Adam", "adam@example.com", hid, "admin")
	mia := seedUser(t, e.d, "Mia", "mia@example.com", hid, "member")
	seedUser(t, e.d, "Vic", "vic@example.com", hid, "viewer")
	o, a, m, v := e.client(), e.client(), e.client(), e.client()
	o.login("olga@example.com")
	a.login("adam@example.com")
	m.login("mia@example.com")
	v.login("vic@example.com")

	var inv struct{ ID, URL string }
	a.json("POST", "/api/v1/invites", `{"role":"member"}`, 201, &inv)
	if !strings.HasPrefix(inv.URL, "/invite#") || len(inv.URL) < 40 {
		t.Fatalf("invite url %q", inv.URL)
	}
	var l memberList
	v.json("GET", "/api/v1/members", "", 200, &l)
	if len(l.Members) != 4 || l.Invites == nil || len(l.Invites) != 0 {
		t.Fatalf("viewer sees %+v (members for all, no invites)", l)
	}
	a.json("GET", "/api/v1/members", "", 200, &l)
	if len(l.Invites) != 1 || l.Invites[0].Role != "member" {
		t.Fatalf("admin sees %+v", l)
	}
	if s := m.status("POST", "/api/v1/invites", `{"role":"viewer"}`); s != 403 {
		t.Fatalf("member inviting: %d", s)
	}
	if s := a.status("POST", "/api/v1/invites", `{"role":"admin"}`); s != 403 {
		t.Fatalf("admin inviting an admin: %d", s)
	}

	// Roles.
	if s := a.status("PATCH", "/api/v1/members/"+mia, `{"role":"viewer"}`); s != 200 {
		t.Fatalf("admin demotes member: %d", s)
	}
	if s := a.status("PATCH", "/api/v1/members/"+mia, `{"role":"owner"}`); s != 403 {
		t.Fatalf("admin promotes to owner: %d", s)
	}
	if s := a.status("PATCH", "/api/v1/members/"+owner, `{"role":"member"}`); s != 403 {
		t.Fatalf("admin demotes owner: %d", s)
	}
	if s := o.status("PATCH", "/api/v1/members/"+owner, `{"role":"admin"}`); s != 409 {
		t.Fatalf("last owner demoted: %d", s)
	}
	if s := o.status("DELETE", "/api/v1/members/"+owner, ""); s != 409 {
		t.Fatalf("last owner removed: %d", s)
	}
	if s := o.status("PATCH", "/api/v1/members/"+admin, `{"role":"owner"}`); s != 200 {
		t.Fatalf("promote second owner: %d", s)
	}
	if s := o.status("PATCH", "/api/v1/members/"+owner, `{"role":"admin"}`); s != 200 {
		t.Fatalf("demote when another owner exists: %d", s)
	}
	if s := o.status("PATCH", "/api/v1/members/"+mia, `{"role":"boss"}`); s != 400 {
		t.Fatalf("bad role: %d", s)
	}

	// Remove: sessions and tokens of that member go.
	m.json("POST", "/api/v1/tokens", `{"name":"x","scope":"read"}`, 403, nil) // mia is a viewer now
	if s := a.status("DELETE", "/api/v1/members/"+mia, ""); s != 204 {
		t.Fatalf("remove: %d", s)
	}
	if s := m.status("GET", "/api/v1/me", ""); s != 401 {
		t.Fatalf("removed member keeps a session: %d", s)
	}
	if s := a.status("DELETE", "/api/v1/members/"+mia, ""); s != 404 {
		t.Fatalf("remove twice: %d", s)
	}
	if s := a.status("DELETE", "/api/v1/invites/"+inv.ID, ""); s != 204 {
		t.Fatalf("revoke invite: %d", s)
	}
	if s := a.status("DELETE", "/api/v1/invites/"+inv.ID, ""); s != 404 {
		t.Fatalf("revoke twice: %d", s)
	}
}

func TestInviteAccept(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	e := newEnv(t, config.Config{})
	h1, h2 := seedHousehold(t, e.d, "One"), seedHousehold(t, e.d, "Two")
	seedUser(t, e.d, "Olga", "olga@example.com", h1, "owner")
	seedUser(t, e.d, "Sam", "sam@example.com", h2, "owner")
	o := e.client()
	o.login("olga@example.com")
	mk := func(role string) string {
		var inv struct{ URL string }
		o.json("POST", "/api/v1/invites", `{"role":"`+role+`","expires_in_days":2}`, 201, &inv)
		return strings.TrimPrefix(inv.URL, "/invite#")
	}
	tok := mk("member")

	anon := e.client()
	var info struct {
		Valid         bool
		HouseholdName string `json:"household_name"`
		Role          string
	}
	anon.json("GET", "/api/v1/invites/"+tok, "", 200, &info)
	if !info.Valid || info.HouseholdName != "One" || info.Role != "member" {
		t.Fatalf("%+v", info)
	}
	anon.json("GET", "/api/v1/invites/nope", "", 200, &info)
	if info.Valid {
		t.Fatal("bogus token valid")
	}
	if s := anon.status("POST", "/api/v1/invites/"+tok+"/accept", `{"name":"N","email":"new@example.com","password":"short"}`); s != 400 {
		t.Fatalf("weak password: %d", s)
	}
	var me struct {
		User   struct{ Email string }
		Role   string
		Active struct{ Name string } `json:"active_household"`
	}
	anon.json("POST", "/api/v1/invites/"+tok+"/accept", `{"name":"Nia","email":"New@Example.com","password":"`+pw+`"}`, 200, &me)
	if me.User.Email != "new@example.com" || me.Role != "member" || me.Active.Name != "One" {
		t.Fatalf("%+v", me)
	}
	if s := anon.status("GET", "/api/v1/me", ""); s != 200 {
		t.Fatalf("not signed in after accepting: %d", s)
	}
	if s := e.client().status("POST", "/api/v1/invites/"+tok+"/accept", `{"name":"X","email":"x@example.com","password":"`+pw+`"}`); s != 400 {
		t.Fatalf("used link: %d", s)
	}
	anon.json("GET", "/api/v1/invites/"+tok, "", 200, &info)
	if info.Valid {
		t.Fatal("used invite still valid")
	}
	var users int
	e.d.Read.QueryRow(`SELECT COUNT(*) FROM users WHERE email='x@example.com'`).Scan(&users)
	if users != 0 {
		t.Fatal("a rejected accept created a user")
	}

	// Signed-in user of another household just joins.
	sam := e.client()
	sam.login("sam@example.com")
	tok2 := mk("viewer")
	sam.json("POST", "/api/v1/invites/"+tok2+"/accept", `{}`, 200, &me)
	if me.Role != "viewer" || me.Active.Name != "One" {
		t.Fatalf("%+v", me)
	}
	tok3 := mk("viewer")
	if s := sam.status("POST", "/api/v1/invites/"+tok3+"/accept", `{}`); s != 409 {
		t.Fatalf("already a member: %d", s)
	}
	// Existing email, not signed in.
	if s := e.client().status("POST", "/api/v1/invites/"+tok3+"/accept", `{"name":"X","email":"sam@example.com","password":"`+pw+`"}`); s != 400 {
		t.Fatalf("existing email: %d", s)
	}
	// Expired.
	e.d.Write.Exec(`UPDATE invites SET expires_at='2020-01-01T00:00:00.000Z'`)
	anon.json("GET", "/api/v1/invites/"+tok3, "", 200, &info)
	if info.Valid {
		t.Fatal("expired invite valid")
	}
	if strings.Contains(buf.String(), tok) || strings.Contains(buf.String(), tok2) {
		t.Fatalf("invite token in logs: %s", buf.String())
	}
}

func TestDeleteHousehold(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Olga", "olga@example.com", hid, "owner")
	seedUser(t, e.d, "Adam", "adam@example.com", hid, "admin")
	a := e.client()
	a.login("adam@example.com")
	if s := a.status("DELETE", "/api/v1/household", ""); s != 403 {
		t.Fatalf("admin deleting: %d", s)
	}
	o := e.client()
	o.login("olga@example.com")
	if s := o.status("DELETE", "/api/v1/household", ""); s != 204 {
		t.Fatalf("owner deleting: %d", s)
	}
	var n int
	e.d.Read.QueryRow(`SELECT (SELECT COUNT(*) FROM households)+(SELECT COUNT(*) FROM memberships)+(SELECT COUNT(*) FROM sessions)`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d rows left", n)
	}
}

func TestAccessTokens(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Olga", "olga@example.com", hid, "owner")
	mia := seedUser(t, e.d, "Mia", "mia@example.com", hid, "member")
	o, m := e.client(), e.client()
	o.login("olga@example.com")
	m.login("mia@example.com")

	var rd, wr struct {
		ID, Token, Hint string
	}
	m.json("POST", "/api/v1/tokens", `{"name":"laptop","scope":"read"}`, 201, &rd)
	m.json("POST", "/api/v1/tokens", `{"name":"agent","scope":"write"}`, 201, &wr)
	if !strings.HasPrefix(rd.Token, "stash_pat_") || len(rd.Token) != 42 || rd.Hint != rd.Token[38:] {
		t.Fatalf("token shape: %+v", rd)
	}
	var stored string
	e.d.Read.QueryRow(`SELECT token_hash FROM access_tokens WHERE id=?`, rd.ID).Scan(&stored)
	if stored == "" || strings.Contains(stored, rd.Token) {
		t.Fatal("hash not stored")
	}
	for _, bad := range []string{`{"name":"","scope":"read"}`, `{"name":"x","scope":"admin"}`, `{"name":"x","scope":"read","expires_at":"2001-01-01T00:00:00Z"}`} {
		if s := m.status("POST", "/api/v1/tokens", bad); s != 400 {
			t.Errorf("%s: %d", bad, s)
		}
	}
	var list struct {
		Tokens []map[string]any
	}
	m.json("GET", "/api/v1/tokens", "", 200, &list)
	if len(list.Tokens) != 2 {
		t.Fatalf("%+v", list)
	}
	if b, _ := m.do2("GET", "/api/v1/tokens"); strings.Contains(b, rd.Token) {
		t.Fatal("token value in list")
	}
	var ol struct{ Tokens []any }
	o.json("GET", "/api/v1/tokens", "", 200, &ol)
	if len(ol.Tokens) != 0 {
		t.Fatal("tokens of another member listed")
	}

	// A stable route as a token sees it; sessions-only routes are closed.
	write := []string{"Authorization", "Bearer " + wr.Token}
	c := e.client()
	for _, p := range []string{"/api/v1/me", "/api/v1/members", "/api/v1/tokens", "/api/v1/household", "/api/v1/settings/ai"} {
		if s, _, _ := c.do("GET", p, "", write...); s != 403 {
			t.Errorf("token on %s: %d", p, s)
		}
	}
	if s, _, _ := c.do("POST", "/api/v1/tokens", `{"name":"x","scope":"read"}`, write...); s != 403 {
		t.Errorf("token minting tokens: %d", s)
	}
	// Revocation and membership.
	if s := m.status("DELETE", "/api/v1/tokens/"+rd.ID, ""); s != 204 {
		t.Fatalf("revoke: %d", s)
	}
	if s := o.status("DELETE", "/api/v1/tokens/"+wr.ID, ""); s != 404 {
		t.Fatalf("revoking someone else's token: %d", s)
	}
	if strings.Contains(buf.String(), rd.Token) || strings.Contains(buf.String(), wr.Token) {
		t.Fatal("token in logs")
	}
	if s := o.status("DELETE", "/api/v1/members/"+mia, ""); s != 204 {
		t.Fatalf("remove member: %d", s)
	}
	var n int
	e.d.Read.QueryRow(`SELECT COUNT(*) FROM access_tokens`).Scan(&n)
	if n != 0 {
		t.Fatalf("tokens survive membership: %d", n)
	}
}

func TestTokenActsOnStableRoutes(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Olga", "olga@example.com", hid, "owner")
	mia := seedUser(t, e.d, "Mia", "mia@example.com", hid, "member")
	e.srv = httpapitest.Server(t, e.svc.Routes, func(mux *httpapi.Mux) {
		mux.HandleFunc("GET /api/v1/stable", auth.Require(auth.ActionInventoryRead, func(w http.ResponseWriter, r *http.Request) {
			id := auth.MustIdentity(r.Context())
			httpapi.WriteJSON(w, 200, map[string]string{"household": id.HouseholdID, "token_name": id.TokenName, "role": id.Role.String()})
		}))
		mux.HandleFunc("POST /api/v1/stable", auth.Require(auth.ActionInventoryWrite, func(w http.ResponseWriter, r *http.Request) {
			httpapi.WriteJSON(w, 200, map[string]string{})
		}))
	})
	m := e.client()
	m.login("mia@example.com")
	var rd, wr struct{ Token string }
	m.json("POST", "/api/v1/tokens", `{"name":"Claude on the laptop","scope":"read"}`, 201, &rd)
	m.json("POST", "/api/v1/tokens", `{"name":"agent","scope":"write"}`, 201, &wr)
	read, write := []string{"Authorization", "Bearer " + rd.Token}, []string{"Authorization", "Bearer " + wr.Token}
	c := e.client() // no cookie

	if s, b, _ := c.do("GET", "/api/v1/stable", "", read...); s != 200 || !strings.Contains(b, "Claude on the laptop") || !strings.Contains(b, hid) {
		t.Fatalf("read token reading: %d %s", s, b)
	}
	if s, _, _ := c.do("POST", "/api/v1/stable", `{}`, read...); s != 403 {
		t.Fatalf("read token writing: %d", s)
	}
	// Cross-site headers do not matter for a token (no ambient credential).
	if s, _, _ := c.do("POST", "/api/v1/stable", `{}`, append(write, "Sec-Fetch-Site", "cross-site")...); s != 200 {
		t.Fatalf("write token: %d", s)
	}
	// Never more than the member has now.
	if _, err := e.d.Write.Exec(`UPDATE memberships SET role='viewer' WHERE user_id=?`, mia); err != nil {
		t.Fatal(err)
	}
	if s, _, _ := c.do("POST", "/api/v1/stable", `{}`, write...); s != 403 {
		t.Fatalf("write token of a demoted member: %d", s)
	}
	if s, _, _ := c.do("GET", "/api/v1/stable", "", write...); s != 200 {
		t.Fatalf("viewer token reading: %d", s)
	}
	// Expired.
	e.d.Write.Exec(`UPDATE access_tokens SET expires_at='2020-01-01T00:00:00.000Z'`)
	if s, _, _ := c.do("GET", "/api/v1/stable", "", write...); s != 401 {
		t.Fatalf("expired: %d", s)
	}
	e.d.Write.Exec(`UPDATE access_tokens SET expires_at=NULL`)
	// Membership gone: dead at once, without deleting the row.
	e.d.Write.Exec(`DELETE FROM memberships WHERE user_id=?`, mia)
	if s, _, _ := c.do("GET", "/api/v1/stable", "", write...); s != 401 {
		t.Fatalf("after removal: %d", s)
	}
}

func TestTokenRateLimit(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Mia", "mia@example.com", hid, "member")
	e.srv = httpapitest.Server(t, e.svc.Routes, func(mux *httpapi.Mux) {
		mux.HandleFunc("GET /api/v1/stable", auth.Require(auth.ActionInventoryRead, func(w http.ResponseWriter, r *http.Request) {
			httpapi.WriteJSON(w, 200, map[string]string{})
		}))
	})
	m := e.client()
	m.login("mia@example.com")
	var tk struct{ Token string }
	m.json("POST", "/api/v1/tokens", `{"name":"x","scope":"read"}`, 201, &tk)
	c := e.client()
	got429 := false
	for range 620 {
		if s, _, _ := c.do("GET", "/api/v1/stable", "", "Authorization", "Bearer "+tk.Token); s == 429 {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("no rate limit after 620 requests in a minute")
	}
}
