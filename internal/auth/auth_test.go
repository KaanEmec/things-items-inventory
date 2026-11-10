package auth_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
)

func TestPasswordHash(t *testing.T) {
	h, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Fatalf("not PHC argon2id with parameters: %s", h)
	}
	h2, _ := auth.HashPassword(pw)
	if h == h2 {
		t.Fatal("salt is not random")
	}
	if ok, err := auth.VerifyPassword(pw, h); !ok || err != nil {
		t.Fatalf("verify right: %v %v", ok, err)
	}
	if ok, _ := auth.VerifyPassword(pw+"x", h); ok {
		t.Fatal("verify wrong passed")
	}
	for _, bad := range []string{"", "plain", "$argon2id$v=19$m=1,t=1,p=1$AAAA$AAAA", "$argon2id$v=19$m=99999999,t=3,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"} {
		if ok, err := auth.VerifyPassword(pw, bad); ok || err == nil {
			t.Fatalf("malformed %q accepted: %v %v", bad, ok, err)
		}
	}
	if auth.CheckPasswordRules("123456789") == "" || auth.CheckPasswordRules("1234567890") != "" {
		t.Fatal("minimum length is 10")
	}
}

func TestSetupFlow(t *testing.T) {
	e := newEnv(t, config.Config{})
	c := e.client()
	var st struct{ Needed bool }
	c.json("GET", "/api/v1/setup", "", 200, &st)
	if !st.Needed {
		t.Fatal("fresh database should need setup")
	}
	if s := c.status("GET", "/api/v1/me", ""); s != 401 {
		t.Fatalf("me before setup: %d", s)
	}
	if s, b, _ := c.do("POST", "/api/v1/setup", `{"name":"","email":"x","password":"short","household_name":""}`); s != 400 || !strings.Contains(b, `"fields"`) {
		t.Fatalf("validation: %d %s", s, b)
	}

	s, body, res := c.do("POST", "/api/v1/setup", setupBody)
	if s != 201 {
		t.Fatalf("setup: %d %s", s, body)
	}
	var ck *http.Cookie
	for _, x := range res.Cookies() {
		if x.Name == "session" {
			ck = x
		}
	}
	if ck == nil || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/" || ck.Secure || len(ck.Value) < 40 {
		t.Fatalf("cookie: %+v", ck)
	}
	if strings.Contains(body, "password") || strings.Contains(body, ck.Value) {
		t.Fatalf("response leaks secrets: %s", body)
	}
	var me struct {
		User   struct{ Email string }
		Role   string
		Active struct{ Name string } `json:"active_household"`
	}
	c.json("GET", "/api/v1/me", "", 200, &me)
	if me.User.Email != "ada@example.com" || me.Role != "owner" || me.Active.Name != "Home" {
		t.Fatalf("me: %+v", me)
	}
	c.json("GET", "/api/v1/setup", "", 200, &st)
	if st.Needed {
		t.Fatal("setup should be done")
	}
	if s := e.client().status("POST", "/api/v1/setup", setupBody); s != 409 {
		t.Fatalf("second setup: %d", s)
	}
	// Only the hash of the token is stored.
	var stored string
	if err := e.d.Read.QueryRow(`SELECT token_hash FROM sessions`).Scan(&stored); err != nil || stored == ck.Value || len(stored) != 64 {
		t.Fatalf("stored token %q (%v)", stored, err)
	}
}

func TestSetupRace(t *testing.T) {
	e := newEnv(t, config.Config{})
	const n = 8
	var created, conflicts atomic.Int32
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := e.client()
			body := strings.Replace(setupBody, "Ada@Example.com", "user"+string(rune('a'+i))+"@example.com", 1)
			switch c.status("POST", "/api/v1/setup", body) {
			case 201:
				created.Add(1)
			case 409:
				conflicts.Add(1)
			}
		}()
	}
	wg.Wait()
	var users, households int
	e.d.Read.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users)
	e.d.Read.QueryRow(`SELECT COUNT(*) FROM households`).Scan(&households)
	if created.Load() != 1 || conflicts.Load() != n-1 || users != 1 || households != 1 {
		t.Fatalf("created=%d conflicts=%d users=%d households=%d", created.Load(), conflicts.Load(), users, households)
	}
}

func TestLoginLogoutAndUniformError(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Bob", "bob@example.com", hid, "member")
	c := e.client()

	s1, b1, _ := c.do("POST", "/api/v1/auth/login", `{"email":"bob@example.com","password":"wrong wrong wrong"}`)
	s2, b2, _ := c.do("POST", "/api/v1/auth/login", `{"email":"nobody@example.com","password":"wrong wrong wrong"}`)
	if s1 != 401 || s2 != 401 || b1 != b2 {
		t.Fatalf("not uniform: %d %q / %d %q", s1, b1, s2, b2)
	}
	c.login("BOB@example.com")
	if s := c.status("GET", "/api/v1/me", ""); s != 200 {
		t.Fatalf("me: %d", s)
	}
	other := e.client()
	other.login("bob@example.com")

	if s := c.status("POST", "/api/v1/auth/logout", ""); s != 204 {
		t.Fatalf("logout: %d", s)
	}
	if s := c.status("GET", "/api/v1/me", ""); s != 401 {
		t.Fatalf("after logout: %d", s)
	}
	if s := other.status("GET", "/api/v1/me", ""); s != 200 {
		t.Fatalf("other session should survive logout: %d", s)
	}
	third := e.client()
	third.login("bob@example.com")
	if s := third.status("POST", "/api/v1/auth/logout-all", ""); s != 204 {
		t.Fatalf("logout-all: %d", s)
	}
	if other.status("GET", "/api/v1/me", "") != 401 || third.status("GET", "/api/v1/me", "") != 401 {
		t.Fatal("logout-all left a session alive")
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Bob", "bob@example.com", hid, "member")
	c := e.client()
	for i := range 10 {
		if s := c.status("POST", "/api/v1/auth/login", `{"email":"bob@example.com","password":"wrong wrong wrong"}`); s != 401 {
			t.Fatalf("attempt %d: %d", i, s)
		}
	}
	s, _, res := c.do("POST", "/api/v1/auth/login", `{"email":"bob@example.com","password":"`+pw+`"}`)
	if s != 429 || res.Header.Get("Retry-After") == "" {
		t.Fatalf("want 429 with Retry-After, got %d", s)
	}
}

func TestSessionExpiryAndSliding(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Bob", "bob@example.com", hid, "member")
	c := e.client()
	c.login("bob@example.com")

	// Seen two hours ago, 10 days left: a request refreshes both and the cookie.
	old := time.Now().Add(-2 * time.Hour)
	set := func(seen, exp time.Time) {
		if _, err := e.d.Write.Exec(`UPDATE sessions SET last_seen_at=?, expires_at=?`, ts(seen), ts(exp)); err != nil {
			t.Fatal(err)
		}
	}
	set(old, time.Now().Add(10*24*time.Hour))
	_, _, res := c.do("GET", "/api/v1/me", "")
	var seen, exp string
	e.d.Read.QueryRow(`SELECT last_seen_at, expires_at FROM sessions`).Scan(&seen, &exp)
	if seen <= ts(old) || exp < ts(time.Now().Add(29*24*time.Hour)) || len(res.Cookies()) == 0 {
		t.Fatalf("not refreshed: %s %s %v", seen, exp, res.Cookies())
	}
	// Seen a minute ago: no write.
	set(time.Now().Add(-time.Minute), time.Now().Add(10*24*time.Hour))
	_, _, res = c.do("GET", "/api/v1/me", "")
	var seen2 string
	e.d.Read.QueryRow(`SELECT last_seen_at FROM sessions`).Scan(&seen2)
	if seen2 == seen || len(res.Cookies()) != 0 {
		t.Fatalf("refreshed too often: %s %v", seen2, res.Cookies())
	}
	// Expired.
	set(time.Now().Add(-40*24*time.Hour), time.Now().Add(-time.Hour))
	if s := c.status("GET", "/api/v1/me", ""); s != 401 {
		t.Fatalf("expired session: %d", s)
	}
}

func TestCSRF(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Bob", "bob@example.com", hid, "member")
	c := e.client()
	c.login("bob@example.com")
	host := strings.TrimPrefix(e.srv.URL, "http://")
	cases := []struct {
		name string
		hdr  []string
		want int
	}{
		{"no headers", nil, 204},
		{"same origin", []string{"Origin", "http://" + host, "Sec-Fetch-Site", "same-origin"}, 204},
		{"cross-site fetch", []string{"Sec-Fetch-Site", "cross-site"}, 403},
		{"foreign origin", []string{"Origin", "https://evil.example"}, 403},
		{"null origin", []string{"Origin", "null"}, 403},
		{"same-site other subdomain", []string{"Origin", "http://other." + host, "Sec-Fetch-Site", "same-site"}, 403},
	}
	for _, tc := range cases {
		c2 := e.client()
		c2.login("bob@example.com")
		if s, _, _ := c2.do("POST", "/api/v1/auth/logout", "", tc.hdr...); s != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, s, tc.want)
		}
	}
	// Reads are never blocked by the check.
	if s, _, _ := c.do("GET", "/api/v1/me", "", "Sec-Fetch-Site", "cross-site"); s != 200 {
		t.Errorf("GET cross-site: %d", s)
	}
	// A blocked login makes no session.
	var n int
	e.d.Read.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n)
	before := n
	e.client().do("POST", "/api/v1/auth/login", `{"email":"bob@example.com","password":"`+pw+`"}`, "Origin", "https://evil.example")
	e.d.Read.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n)
	if n != before {
		t.Error("cross-site login created a session")
	}
}

func TestSecureCookie(t *testing.T) {
	for _, tc := range []struct {
		publicURL string
		want      bool
	}{{"", false}, {"http://x.example", false}, {"https://x.example", true}} {
		e := newEnv(t, config.Config{PublicURL: tc.publicURL})
		_, _, res := e.client().do("POST", "/api/v1/setup", setupBody)
		var secure bool
		for _, c := range res.Cookies() {
			if c.Name == "session" {
				secure = c.Secure
			}
		}
		if secure != tc.want {
			t.Errorf("public URL %q: Secure=%v", tc.publicURL, secure)
		}
	}
}

func TestForwardedProtoOnlyFromTrustedProxy(t *testing.T) {
	loop := netip.MustParsePrefix("127.0.0.0/8") // the test server's peer
	other := netip.MustParsePrefix("192.0.2.0/24")
	for _, tc := range []struct {
		proxies []netip.Prefix
		want    bool
	}{{nil, false}, {[]netip.Prefix{other}, false}, {[]netip.Prefix{loop}, true}} {
		e := newEnv(t, config.Config{TrustedProxies: tc.proxies})
		_, _, res := e.client().do("POST", "/api/v1/setup", setupBody, "X-Forwarded-Proto", "https")
		var secure bool
		for _, c := range res.Cookies() {
			if c.Name == "session" {
				secure = c.Secure
			}
		}
		if secure != tc.want {
			t.Errorf("proxies %v: Secure=%v, want %v", tc.proxies, secure, tc.want)
		}
	}
}

func TestForwardedForIgnoredFromUntrustedPeer(t *testing.T) {
	e := newEnv(t, config.Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Bob", "bob@example.com", hid, "member")
	for i := range 12 { // each guess forges a new address; the limiter must still see one peer
		e.client().do("POST", "/api/v1/auth/login", `{"email":"bob@example.com","password":"wrong wrong wrong"}`, "X-Forwarded-For", "203.0.113."+string(rune('1'+i%9)))
	}
	if s, _, _ := e.client().do("POST", "/api/v1/auth/login", `{"email":"bob@example.com","password":"`+pw+`"}`, "X-Forwarded-For", "198.51.100.77"); s != 429 {
		t.Fatalf("forged X-Forwarded-For dodged the limiter: %d", s)
	}
}

func TestPasswordReset(t *testing.T) {
	e := newEnv(t, config.Config{PublicURL: "https://x.example"})
	hid := seedHousehold(t, e.d, "H")
	otherHid := seedHousehold(t, e.d, "Other")
	seedUser(t, e.d, "Olga", "olga@example.com", hid, "owner")
	adminID := seedUser(t, e.d, "Adam", "adam@example.com", hid, "admin")
	memberID := seedUser(t, e.d, "Mia", "mia@example.com", hid, "member")
	viewerID := seedUser(t, e.d, "Vic", "vic@example.com", hid, "viewer")
	strangerID := seedUser(t, e.d, "Sam", "sam@example.com", otherHid, "member")
	_ = adminID

	owner, admin, mia, viewer := e.client(), e.client(), e.client(), e.client()
	owner.login("olga@example.com")
	admin.login("adam@example.com")
	mia.login("mia@example.com")
	viewer.login("vic@example.com")

	if s := mia.status("POST", "/api/v1/members/"+viewerID+"/reset-link", ""); s != 403 {
		t.Fatalf("member creating link: %d", s)
	}
	if s := admin.status("POST", "/api/v1/members/"+strangerID+"/reset-link", ""); s != 404 {
		t.Fatalf("other household's user: %d", s)
	}
	var olga struct{ User struct{ ID string } }
	owner.json("GET", "/api/v1/me", "", 200, &olga)
	if s := admin.status("POST", "/api/v1/members/"+olga.User.ID+"/reset-link", ""); s != 403 {
		t.Fatalf("admin resetting owner: %d", s)
	}
	if s := admin.status("POST", "/api/v1/members/"+adminID+"/reset-link", ""); s != 403 {
		t.Fatalf("admin resetting admin: %d", s)
	}

	var link struct {
		URL       string
		ExpiresAt string `json:"expires_at"`
	}
	admin.json("POST", "/api/v1/members/"+memberID+"/reset-link", "", 201, &link)
	const prefix = "https://x.example/reset#"
	if !strings.HasPrefix(link.URL, prefix) {
		t.Fatalf("link: %q", link.URL)
	}
	token := strings.TrimPrefix(link.URL, prefix)
	var stored string
	e.d.Read.QueryRow(`SELECT token_hash FROM invites WHERE kind='password_reset'`).Scan(&stored)
	if stored == "" || stored == token {
		t.Fatalf("token must be stored hashed: %q", stored)
	}

	const newPw = "a brand new password"
	if s := e.client().status("POST", "/api/v1/auth/reset", `{"token":"`+token+`","password":"short"}`); s != 400 {
		t.Fatalf("short password: %d", s)
	}
	if s := e.client().status("POST", "/api/v1/auth/reset", `{"token":"nope","password":"`+newPw+`"}`); s != 400 {
		t.Fatalf("bad token: %d", s)
	}
	if s := e.client().status("POST", "/api/v1/auth/reset", `{"token":"`+token+`","password":"`+newPw+`"}`); s != 204 {
		t.Fatalf("reset: %d", s)
	}
	if s := e.client().status("POST", "/api/v1/auth/reset", `{"token":"`+token+`","password":"`+newPw+`2"}`); s != 400 {
		t.Fatalf("token reuse: %d", s)
	}
	if s := mia.status("GET", "/api/v1/me", ""); s != 401 {
		t.Fatalf("reset should end sessions: %d", s)
	}
	fresh := e.client()
	if s := fresh.status("POST", "/api/v1/auth/login", `{"email":"mia@example.com","password":"`+pw+`"}`); s != 401 {
		t.Fatalf("old password still works: %d", s)
	}
	if s := fresh.status("POST", "/api/v1/auth/login", `{"email":"mia@example.com","password":"`+newPw+`"}`); s != 200 {
		t.Fatalf("new password: %d", s)
	}

	// Expired and revoked links do not work.
	admin.json("POST", "/api/v1/members/"+memberID+"/reset-link", "", 201, &link)
	tok2 := strings.TrimPrefix(link.URL, prefix)
	admin.json("POST", "/api/v1/members/"+memberID+"/reset-link", "", 201, &link) // revokes tok2
	if s := e.client().status("POST", "/api/v1/auth/reset", `{"token":"`+tok2+`","password":"`+newPw+`"}`); s != 400 {
		t.Fatalf("revoked link: %d", s)
	}
	e.d.Write.Exec(`UPDATE invites SET expires_at='2020-01-01T00:00:00.000Z'`)
	if s := e.client().status("POST", "/api/v1/auth/reset", `{"token":"`+strings.TrimPrefix(link.URL, prefix)+`","password":"`+newPw+`"}`); s != 400 {
		t.Fatalf("expired link: %d", s)
	}
}

func TestResetLinkForEmailCLI(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Olga", "olga@example.com", hid, "owner")
	link, _, err := auth.ResetLinkForEmail(context.Background(), e.d, config.Config{}, "OLGA@example.com")
	if err != nil || !strings.HasPrefix(link, "/reset#") {
		t.Fatalf("%q %v", link, err)
	}
	if s := e.client().status("POST", "/api/v1/auth/reset", `{"token":"`+strings.TrimPrefix(link, "/reset#")+`","password":"a brand new password"}`); s != 204 {
		t.Fatalf("reset via CLI link: %d", s)
	}
	if _, _, err := auth.ResetLinkForEmail(context.Background(), e.d, config.Config{}, "nobody@example.com"); err == nil {
		t.Fatal("unknown email should fail")
	}
}

func TestRolesOnHouseholdSettings(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	for role, email := range map[string]string{"viewer": "v@example.com", "member": "m@example.com", "admin": "a@example.com", "owner": "o@example.com"} {
		seedUser(t, e.d, role, email, hid, role)
	}
	want := map[string][2]int{"viewer": {200, 403}, "member": {200, 403}, "admin": {200, 200}, "owner": {200, 200}}
	for role, email := range map[string]string{"viewer": "v@example.com", "member": "m@example.com", "admin": "a@example.com", "owner": "o@example.com"} {
		c := e.client()
		c.login(email)
		if s := c.status("GET", "/api/v1/household", ""); s != want[role][0] {
			t.Errorf("%s GET: %d", role, s)
		}
		if s := c.status("PATCH", "/api/v1/household", `{"default_warranty_months":36}`); s != want[role][1] {
			t.Errorf("%s PATCH: %d", role, s)
		}
	}
	c := e.client()
	c.login("a@example.com")
	var h struct {
		Name, Language, Currency string
		Months                   int  `json:"default_warranty_months"`
		Paused                   bool `json:"ai_paused"`
	}
	c.json("PATCH", "/api/v1/household", `{"name":" New ","language":"tr","currency":"TRY","ai_paused":true}`, 200, &h)
	if h.Name != "New" || h.Language != "tr" || h.Currency != "TRY" || h.Months != 36 || !h.Paused {
		t.Fatalf("%+v", h)
	}
	for _, bad := range []string{`{"currency":"euro"}`, `{"name":""}`, `{"default_warranty_months":-1}`, `{"language":"x"}`} {
		if s := c.status("PATCH", "/api/v1/household", bad); s != 400 {
			t.Errorf("%s: %d", bad, s)
		}
	}
}

func TestSwitchHousehold(t *testing.T) {
	e := newEnv(t, config.Config{})
	a, b, c := seedHousehold(t, e.d, "Alpha"), seedHousehold(t, e.d, "Beta"), seedHousehold(t, e.d, "Gamma")
	uid := seedUser(t, e.d, "Bob", "bob@example.com", a, "owner")
	addMember(t, e.d, uid, b, "viewer")
	cl := e.client()
	cl.login("bob@example.com")
	var me struct {
		Households []struct{ ID, Role string }
		Role       string
	}
	cl.json("GET", "/api/v1/me", "", 200, &me)
	if len(me.Households) != 2 {
		t.Fatalf("%+v", me)
	}
	cl.json("PUT", "/api/v1/me/household", `{"household_id":"`+b+`"}`, 200, &me)
	if me.Role != "viewer" {
		t.Fatalf("role after switch: %+v", me)
	}
	if s := cl.status("PATCH", "/api/v1/household", `{"name":"x"}`); s != 403 {
		t.Fatalf("viewer in Beta: %d", s)
	}
	if s := cl.status("PUT", "/api/v1/me/household", `{"household_id":"`+c+`"}`); s != 404 {
		t.Fatalf("household not joined: %d", s)
	}
	var hh struct{ ID string }
	cl.json("GET", "/api/v1/household", "", 200, &hh)
	if hh.ID != b {
		t.Fatalf("active household should still be Beta: %s", hh.ID)
	}
}

func TestChangePasswordAndProfile(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Bob", "bob@example.com", hid, "member")
	seedUser(t, e.d, "Other", "other@example.com", hid, "member")
	c, c2 := e.client(), e.client()
	c.login("bob@example.com")
	c2.login("bob@example.com")
	if s := c.status("POST", "/api/v1/me/password", `{"current":"wrong wrong wrong","new":"another long one"}`); s != 400 {
		t.Fatalf("wrong current: %d", s)
	}
	if s := c.status("POST", "/api/v1/me/password", `{"current":"`+pw+`","new":"another long one"}`); s != 204 {
		t.Fatalf("change: %d", s)
	}
	if c.status("GET", "/api/v1/me", "") != 200 || c2.status("GET", "/api/v1/me", "") != 401 {
		t.Fatal("password change should keep this session and end the others")
	}
	var me struct{ User struct{ Name, Email string } }
	c.json("PATCH", "/api/v1/me", `{"name":"Robert","email":"Rob@Example.com"}`, 200, &me)
	if me.User.Name != "Robert" || me.User.Email != "rob@example.com" {
		t.Fatalf("%+v", me)
	}
	if s := c.status("PATCH", "/api/v1/me", `{"email":"other@example.com"}`); s != 400 {
		t.Fatalf("duplicate email: %d", s)
	}
}

func TestAuthenticatorSeam(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	uid := seedUser(t, e.d, "Bob", "bob@example.com", hid, "member")
	// What J9.1 will do: turn a bearer token into an Identity.
	e.svc.AddAuthenticator(func(r *http.Request) (auth.Identity, bool, error) {
		if r.Header.Get("Authorization") != "Bearer good-token" {
			return auth.Identity{}, false, nil
		}
		return auth.Identity{UserID: uid, HouseholdID: hid, Role: auth.RoleMember, Source: auth.SourceToken, ReadOnly: true}, true, nil
	})
	// A stable (S) route, as a feature package would register it.
	e.srv = httpapitest.Server(t, e.svc.Routes, func(mux *httpapi.Mux) {
		h := auth.Require(auth.ActionInventoryWrite, func(w http.ResponseWriter, r *http.Request) {
			httpapi.WriteJSON(w, 200, map[string]string{"household": auth.HouseholdID(r.Context())})
		})
		mux.HandleFunc("GET /api/v1/stable", h)
		mux.HandleFunc("POST /api/v1/stable", h)
	})
	c := e.client()
	bearer := []string{"Authorization", "Bearer good-token"}
	if s, b, _ := c.do("GET", "/api/v1/stable", "", bearer...); s != 200 || !strings.Contains(b, hid) {
		t.Fatalf("token on a stable route: %d", s)
	}
	if s, _, _ := c.do("GET", "/api/v1/me", "", bearer...); s != 403 {
		t.Fatalf("token on an internal route: %d", s)
	}
	if s, _, _ := c.do("POST", "/api/v1/stable", `{}`, bearer...); s != 403 {
		t.Fatalf("read-only token writing: %d", s)
	}
	if s, _, _ := c.do("GET", "/api/v1/stable", "", "Authorization", "Bearer bad"); s != 401 {
		t.Fatalf("bad token: %d", s)
	}
}

func TestRequireUnknownActionPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic for an action missing from the table")
		}
	}()
	auth.Require("no.such.action", func(http.ResponseWriter, *http.Request) {})
}

func TestSecretsStayOutOfLogs(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)

	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Olga", "olga@example.com", hid, "owner")
	seedUser(t, e.d, "Mia", "mia@example.com", hid, "member")
	c := e.client()
	_, _, res := c.do("POST", "/api/v1/auth/login", `{"email":"olga@example.com","password":"`+pw+`"}`)
	c.do("POST", "/api/v1/auth/login", `{"email":"olga@example.com","password":"wrong wrong wrong"}`)
	var cookie string
	for _, ck := range res.Cookies() {
		cookie = ck.Value
	}
	var memberID string
	e.d.Read.QueryRow(`SELECT id FROM users WHERE email='mia@example.com'`).Scan(&memberID)
	var link struct{ URL string }
	c.json("POST", "/api/v1/members/"+memberID+"/reset-link", "", 201, &link)
	token := strings.TrimPrefix(link.URL, "/reset#")
	e.client().do("POST", "/api/v1/auth/reset", `{"token":"`+token+`","password":"a brand new password"}`)

	logs := buf.String()
	if !strings.Contains(logs, "path=/api/v1/auth/login") {
		t.Fatalf("expected request logs, got %q", logs)
	}
	for name, secret := range map[string]string{"password": pw, "session token": cookie, "reset token": token, "new password": "a brand new password"} {
		if secret == "" || strings.Contains(logs, secret) {
			t.Errorf("logs contain the %s (or it was empty): %s", name, logs)
		}
	}
}

func TestResetLinkRefusedForPeopleInOtherHouseholds(t *testing.T) {
	e := newEnv(t, config.Config{})
	h1, h2 := seedHousehold(t, e.d, "One"), seedHousehold(t, e.d, "Two")
	seedUser(t, e.d, "Adam", "adam@example.com", h1, "admin")
	solo := seedUser(t, e.d, "Mia", "mia@example.com", h1, "member")
	both := seedUser(t, e.d, "Sam", "sam@example.com", h1, "viewer")
	addMember(t, e.d, both, h2, "owner")
	a := e.client()
	a.login("adam@example.com")
	if s, b, _ := a.do("POST", "/api/v1/members/"+both+"/reset-link", ""); s != 403 || !strings.Contains(b, "reset-password") {
		t.Fatalf("person in two households: %d %s", s, b)
	}
	if s := a.status("POST", "/api/v1/members/"+solo+"/reset-link", ""); s != 201 {
		t.Fatalf("person in one household: %d", s)
	}
	var n int
	e.d.Read.QueryRow(`SELECT COUNT(*) FROM invites WHERE user_id=?`, both).Scan(&n)
	if n != 0 {
		t.Fatal("a link was stored for the refused request")
	}
}

func TestParallelLoginsCannotAllPass(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Bob", "bob@example.com", hid, "member")
	var wg sync.WaitGroup
	var wrong, limited atomic.Int32
	for range 25 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch e.client().status("POST", "/api/v1/auth/login", `{"email":"bob@example.com","password":"wrong wrong wrong"}`) {
			case 401:
				wrong.Add(1)
			case 429:
				limited.Add(1)
			}
		}()
	}
	wg.Wait()
	if wrong.Load() > 10 || limited.Load() < 15 {
		t.Fatalf("401=%d 429=%d: more than 10 guesses got through", wrong.Load(), limited.Load())
	}
}

func TestEmailLockoutIsPerClientAddress(t *testing.T) {
	e := newEnv(t, config.Config{TrustProxy: true})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Bob", "bob@example.com", hid, "member")
	attacker := []string{"X-Forwarded-For", "203.0.113.9"}
	for range 10 {
		e.client().do("POST", "/api/v1/auth/login", `{"email":"bob@example.com","password":"wrong wrong wrong"}`, attacker...)
	}
	if s, _, _ := e.client().do("POST", "/api/v1/auth/login", `{"email":"bob@example.com","password":"`+pw+`"}`, attacker...); s != 429 {
		t.Fatalf("attacker address should be limited: %d", s)
	}
	if s, _, _ := e.client().do("POST", "/api/v1/auth/login", `{"email":"bob@example.com","password":"`+pw+`"}`, "X-Forwarded-For", "198.51.100.7"); s != 200 {
		t.Fatalf("the real owner from another address is locked out: %d", s)
	}
}

func TestChangePasswordNotBlockedByLoginFailures(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Bob", "bob@example.com", hid, "member")
	c := e.client()
	c.login("bob@example.com")
	for range 12 { // an outsider (same address here) exhausts the login limiter
		e.client().status("POST", "/api/v1/auth/login", `{"email":"bob@example.com","password":"wrong wrong wrong"}`)
	}
	if s := c.status("POST", "/api/v1/me/password", `{"current":"`+pw+`","new":"another long one"}`); s != 204 {
		t.Fatalf("password change blocked by login failures: %d", s)
	}
}

func TestResetEndpointIsRateLimited(t *testing.T) {
	e := newEnv(t, config.Config{})
	c := e.client()
	var got429 bool
	for range 35 {
		if c.status("POST", "/api/v1/auth/reset", `{"token":"guess","password":"a brand new password"}`) == 429 {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("no limit on /auth/reset")
	}
}

func TestSetupAfterDoneDoesNotHash(t *testing.T) {
	e := newEnv(t, config.Config{})
	e.client().status("POST", "/api/v1/setup", setupBody)
	// Hold every hashing slot: a request that tried to hash would block, but
	// a finished setup answers 409 without touching argon2.
	release := auth.HoldHashSlotsForTest(t)
	defer release()
	if s := e.client().status("POST", "/api/v1/setup", setupBody); s != 409 {
		t.Fatalf("second setup: %d", s)
	}
	// Same for a bad reset token and a bad invite.
	if s := e.client().status("POST", "/api/v1/auth/reset", `{"token":"nope","password":"a brand new password"}`); s != 400 {
		t.Fatalf("bad reset token: %d", s)
	}
	if s := e.client().status("POST", "/api/v1/invites/nope/accept", `{"name":"N","email":"n@example.com","password":"`+pw+`"}`); s != 400 {
		t.Fatalf("bad invite: %d", s)
	}
}

func TestHashSlotsRespectContext(t *testing.T) {
	release := auth.HoldHashSlotsForTest(t)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := auth.HashPasswordCtx(ctx, pw); err == nil {
		t.Fatal("hash ran although every slot was taken")
	}
	if _, err := auth.VerifyPasswordCtx(ctx, pw, "$argon2id$v=19$m=65536,t=3,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"); err == nil {
		t.Fatal("verify ran although every slot was taken")
	}
}

// A request with an Authorization header is token-or-nothing: a bad bearer
// neither skips the CSRF check nor falls back to the session cookie.
func TestBadBearerDoesNotFallBackToCookie(t *testing.T) {
	e := newEnv(t, config.Config{})
	hid := seedHousehold(t, e.d, "H")
	seedUser(t, e.d, "Bob", "bob@example.com", hid, "member")
	c := e.client()
	c.login("bob@example.com")
	if s, _, _ := c.do("GET", "/api/v1/me", "", "Authorization", "Bearer stash_pat_nonsense"); s != 401 {
		t.Errorf("bad token with a good cookie: %d, want 401", s)
	}
	if s, _, _ := c.do("GET", "/api/v1/me", "", "Authorization", "Basic abc"); s != 401 {
		t.Errorf("other Authorization scheme with a good cookie: %d, want 401", s)
	}
	if s, _, _ := c.do("POST", "/api/v1/auth/logout", "", "Authorization", "Bearer stash_pat_nonsense", "Sec-Fetch-Site", "cross-site"); s != 403 {
		t.Errorf("cross-site write with a bad bearer: %d, want 403", s)
	}
}
