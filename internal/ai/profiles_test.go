package ai_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

const stamp = "2026-01-01T00:00:00.000Z"

type env struct {
	t   *testing.T
	d   *db.DB
	svc *ai.Service
	srv *httptest.Server
	hid string
}

func newEnv(t *testing.T, cfg config.Config) *env {
	t.Helper()
	d := dbtest.New(t)
	cfg.DataDir = t.TempDir()
	svc := ai.NewService(d, cfg)
	authSvc := auth.New(d, cfg)
	srv := httptest.NewServer(httpapi.New("t", "t", nil, authSvc.Routes, svc.Routes))
	t.Cleanup(srv.Close)
	e := &env{t: t, d: d, svc: svc, srv: srv}
	e.hid = e.household("Home")
	return e
}

func (e *env) household(name string) string {
	hid := id.New()
	if _, err := sqlc.New(e.d.Write).CreateHousehold(context.Background(), sqlc.CreateHouseholdParams{
		ID: hid, Name: name, Language: "en", Currency: "EUR", CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
		e.t.Fatal(err)
	}
	return hid
}

// session creates a user with role in household and returns a session cookie value.
func (e *env) session(hid, role string) string {
	e.t.Helper()
	ctx := context.Background()
	q := sqlc.New(e.d.Write)
	uid := id.New()
	if _, err := q.CreateUser(ctx, sqlc.CreateUserParams{ID: uid, Name: role, Email: uid + "@example.com",
		PasswordHash: "x", CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
		e.t.Fatal(err)
	}
	if err := q.CreateMembership(ctx, sqlc.CreateMembershipParams{HouseholdID: hid, UserID: uid, Role: role, CreatedAt: stamp}); err != nil {
		e.t.Fatal(err)
	}
	token := id.New() + id.New()
	sum := sha256.Sum256([]byte(token))
	if err := q.CreateSession(ctx, sqlc.CreateSessionParams{ID: id.New(), TokenHash: hex.EncodeToString(sum[:]),
		UserID: uid, HouseholdID: hid, CreatedAt: stamp, LastSeenAt: "2099-01-01T00:00:00.000Z", ExpiresAt: "2099-01-01T00:00:00.000Z"}); err != nil {
		e.t.Fatal(err)
	}
	return token
}

func (e *env) do(cookie, method, path, body string) (int, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: cookie})
	res, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var b bytes.Buffer
	b.ReadFrom(res.Body)
	return res.StatusCode, b.String()
}

func (e *env) q() *sqlc.Queries { return sqlc.New(e.d.Read) }

// fake is an OpenAI-compatible endpoint that records the bearer tokens it saw.
type fake struct {
	*httptest.Server
	mu     sync.Mutex
	tokens []string
	models []string
}

func newFake(t *testing.T) *fake {
	f := &fake{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Model string }
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.tokens = append(f.tokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		f.models = append(f.models, body.Model)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"x","choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fake) call(t *testing.T, cfg ai.Config) {
	t.Helper()
	_, err := ai.New(nil).Complete(context.Background(), cfg, ai.Request{User: "hi", Schema: json.RawMessage(`{"type":"object"}`)},
		func(json.RawMessage) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func put(e *env, cookie, role, body string) (int, string) {
	return e.do(cookie, "PUT", "/api/v1/settings/ai/"+role, body)
}

func TestTwoProfilesResolveToTheirOwnEndpoints(t *testing.T) {
	logs := captureLogs(t)
	e := newEnv(t, config.Config{})
	vf, tf := newFake(t), newFake(t)
	admin := e.session(e.hid, "admin")
	const vTok, tTok = "tok-vision-AAAA1111", "tok-text-BBBB2222"

	if c, b := put(e, admin, "vision", `{"base_url":"`+vf.URL+`/v1/chat/completions/","model":"vision-model","token":"`+vTok+`"}`); c != 200 {
		t.Fatal(c, b)
	}
	if c, b := put(e, admin, "text", `{"base_url":"`+tf.URL+`","model":"text-model","token":"`+tTok+`","same_as_vision":false}`); c != 200 {
		t.Fatal(c, b)
	}
	ctx := context.Background()
	vc, err := ai.For(ctx, e.q(), e.hid, ai.RoleVision)
	if err != nil {
		t.Fatal(err)
	}
	tc, err := ai.For(ctx, e.q(), e.hid, ai.RoleText)
	if err != nil {
		t.Fatal(err)
	}
	if vc.BaseURL != vf.URL+"/v1" || tc.BaseURL != tf.URL {
		t.Fatalf("base urls: %q %q", vc.BaseURL, tc.BaseURL)
	}
	vf.call(t, vc)
	tf.call(t, tc)
	if len(vf.tokens) != 1 || vf.tokens[0] != vTok || vf.models[0] != "vision-model" {
		t.Fatalf("vision server saw %v %v", vf.tokens, vf.models)
	}
	if len(tf.tokens) != 1 || tf.tokens[0] != tTok || tf.models[0] != "text-model" {
		t.Fatalf("text server saw %v %v", tf.tokens, tf.models)
	}

	// Token in no response and no log line, whatever we ask.
	c, body := e.do(admin, "GET", "/api/v1/settings/ai", "")
	if c != 200 {
		t.Fatal(c, body)
	}
	var got map[string]struct {
		TokenSet     bool   `json:"token_set"`
		TokenHint    string `json:"token_hint"`
		Source       string `json:"source"`
		Model        string `json:"model"`
		SameAsVision *bool  `json:"same_as_vision"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if v := got["vision"]; !v.TokenSet || v.TokenHint != "1111" || v.Source != "household" || v.Model != "vision-model" || v.SameAsVision != nil {
		t.Fatalf("vision view %+v", v)
	}
	if v := got["text"]; !v.TokenSet || v.TokenHint != "2222" || v.SameAsVision == nil || *v.SameAsVision {
		t.Fatalf("text view %+v", v)
	}
	// Bad requests must not echo the token either.
	_, bad := put(e, admin, "vision", `{"token":"`+vTok+` with spaces","base_url":"http://u:`+vTok+`@h/"}`)
	_, bad2 := put(e, admin, "vision", `{"token":"","tokn":"`+vTok+`"}`)
	for name, s := range map[string]string{"GET": body, "bad PUT": bad, "bad PUT 2": bad2, "logs": logs.String()} {
		for _, tok := range []string{vTok, tTok} {
			if strings.Contains(s, tok) {
				t.Errorf("%s contains a token: %s", name, s)
			}
		}
	}
	// Not in the database in clear either.
	var raw []byte
	e.d.Read.QueryRow(`SELECT token_enc FROM ai_profiles WHERE role='vision'`).Scan(&raw)
	if len(raw) == 0 || bytes.Contains(raw, []byte(vTok)) {
		t.Fatal("token stored in clear or missing")
	}
}

func TestSameAsVision(t *testing.T) {
	e := newEnv(t, config.Config{})
	admin := e.session(e.hid, "owner")
	ctx := context.Background()

	if _, err := ai.For(ctx, e.q(), e.hid, ai.RoleVision); !errors.Is(err, ai.ErrNotSetUp) {
		t.Fatalf("empty household: %v", err)
	}
	put(e, admin, "vision", `{"model":"v","token":"tok-vision-9999"}`)
	// No text profile: text uses vision.
	tc, err := ai.For(ctx, e.q(), e.hid, ai.RoleText)
	if err != nil || tc.Model != "v" || tc.Token != "tok-vision-9999" {
		t.Fatalf("text without profile: %+v %v", tc, err)
	}
	// Explicit same_as_vision on.
	put(e, admin, "text", `{"model":"t","same_as_vision":true}`)
	if tc, _ = ai.For(ctx, e.q(), e.hid, ai.RoleText); tc.Model != "v" {
		t.Fatalf("same_as_vision on: %+v", tc)
	}
	// Off, no model: text not set up, vision still works.
	put(e, admin, "text", `{"model":"","same_as_vision":false}`)
	_, err = ai.For(ctx, e.q(), e.hid, ai.RoleText)
	var ns *ai.NotSetUpError
	if !errors.Is(err, ai.ErrNotSetUp) || !errors.As(err, &ns) || ns.Role != ai.RoleText {
		t.Fatalf("same_as_vision off: %v", err)
	}
	if _, err := ai.For(ctx, e.q(), e.hid, ai.RoleVision); err != nil {
		t.Fatal(err)
	}
	// Off with own model.
	put(e, admin, "text", `{"model":"t"}`)
	if tc, err = ai.For(ctx, e.q(), e.hid, ai.RoleText); err != nil || tc.Model != "t" || tc.BaseURL != ai.DefaultBaseURL {
		t.Fatalf("own text: %+v %v", tc, err)
	}
	if c, _ := put(e, admin, "vision", `{"same_as_vision":true}`); c != 400 && c != 422 {
		t.Fatalf("same_as_vision on vision: %d", c)
	}
}

func TestServerDefaultsAndSource(t *testing.T) {
	cfg := config.Config{AI: config.AIDefaults{
		Vision: config.AIEndpoint{BaseURL: "https://vision.example.com/v1/", Token: "srv-vision-token-ZZZZ", Model: "srv-v"},
		Text:   config.AIEndpoint{Token: "srv-text-token-YYYY", Model: "srv-t"},
	}}
	e := newEnv(t, cfg)
	admin := e.session(e.hid, "admin")
	ctx := context.Background()

	vc, err := ai.For(ctx, e.q(), e.hid, ai.RoleVision)
	if err != nil || vc.BaseURL != "https://vision.example.com/v1" || vc.Token != "srv-vision-token-ZZZZ" || vc.Model != "srv-v" {
		t.Fatalf("%+v %v", vc, err)
	}
	if tc, err := ai.For(ctx, e.q(), e.hid, ai.RoleText); err != nil || tc.Model != "srv-t" || tc.BaseURL != ai.DefaultBaseURL {
		t.Fatalf("%+v %v", tc, err)
	}
	_, body := e.do(admin, "GET", "/api/v1/settings/ai", "")
	if !strings.Contains(body, `"source":"server"`) || strings.Contains(body, "srv-") && strings.Contains(body, "token-") {
		t.Fatalf("body: %s", body)
	}
	if strings.Contains(body, "srv-vision-token") || strings.Contains(body, "srv-text-token") || strings.Contains(body, "ZZZZ") {
		t.Fatalf("server token leaked: %s", body)
	}
	if c, _ := e.do(admin, "DELETE", "/api/v1/settings/ai/vision", ""); c != 404 {
		t.Fatalf("delete of a server default: %d", c)
	}
	// The household's own profile takes over, then DELETE gives the server default back.
	if c, b := put(e, admin, "vision", `{"model":"mine","token":"tok-own-token-1234"}`); c != 200 || !strings.Contains(b, `"source":"household"`) {
		t.Fatal(c, b)
	}
	if vc, _ = ai.For(ctx, e.q(), e.hid, ai.RoleVision); vc.Model != "mine" || vc.Token != "tok-own-token-1234" {
		t.Fatalf("%+v", vc)
	}
	if c, b := e.do(admin, "DELETE", "/api/v1/settings/ai/vision", ""); c != 200 || !strings.Contains(b, `"source":"server"`) {
		t.Fatal(c, b)
	}
	if vc, _ = ai.For(ctx, e.q(), e.hid, ai.RoleVision); vc.Model != "srv-v" {
		t.Fatalf("%+v", vc)
	}
}

func TestRoles(t *testing.T) {
	e := newEnv(t, config.Config{})
	owner, admin := e.session(e.hid, "owner"), e.session(e.hid, "admin")
	member, viewer := e.session(e.hid, "member"), e.session(e.hid, "viewer")
	put(e, owner, "vision", `{"model":"v","token":"tok-vision-5555"}`)

	for _, c := range []string{member, viewer} {
		if code, _ := e.do(c, "GET", "/api/v1/settings/ai", ""); code != 200 {
			t.Fatalf("read: %d", code)
		}
		if code, _ := put(e, c, "vision", `{"model":"hacked"}`); code != 403 {
			t.Fatalf("put: %d", code)
		}
		if code, _ := e.do(c, "DELETE", "/api/v1/settings/ai/vision", ""); code != 403 {
			t.Fatalf("delete: %d", code)
		}
	}
	if code, _ := e.do("", "GET", "/api/v1/settings/ai", ""); code != 401 {
		t.Fatalf("anonymous: %d", code)
	}
	cfg, _ := ai.For(context.Background(), e.q(), e.hid, ai.RoleVision)
	if cfg.Model != "v" {
		t.Fatalf("profile changed by a non-admin: %+v", cfg)
	}
	// Omitting the token keeps it; admin may change and clear.
	if c, b := put(e, admin, "vision", `{"model":"v2"}`); c != 200 || !strings.Contains(b, `"token_set":true`) {
		t.Fatal(c, b)
	}
	if cfg, _ = ai.For(context.Background(), e.q(), e.hid, ai.RoleVision); cfg.Model != "v2" || cfg.Token != "tok-vision-5555" {
		t.Fatalf("%+v", cfg)
	}
	if c, _ := put(e, admin, "nope", `{}`); c != 404 {
		t.Fatalf("bad role: %d", c)
	}
	if c, _ := e.do(owner, "DELETE", "/api/v1/settings/ai/vision", ""); c != 200 {
		t.Fatal(c)
	}
	if _, err := ai.For(context.Background(), e.q(), e.hid, ai.RoleVision); !errors.Is(err, ai.ErrNotSetUp) {
		t.Fatal(err)
	}
}

func TestCiphertextBoundToHouseholdAndRole(t *testing.T) {
	e := newEnv(t, config.Config{})
	other := e.household("Other")
	owner := e.session(e.hid, "owner")
	put(e, owner, "vision", `{"model":"v","token":"tok-vision-7777"}`)
	put(e, owner, "text", `{"model":"t","token":"tok-text-8888","same_as_vision":false}`)
	ctx := context.Background()

	var enc []byte
	e.d.Read.QueryRow(`SELECT token_enc FROM ai_profiles WHERE household_id=? AND role='vision'`, e.hid).Scan(&enc)
	// Copy to another household, and onto the text row of the same household.
	if _, err := e.d.Write.Exec(`INSERT INTO ai_profiles (household_id, role, model, token_enc, updated_at) VALUES (?, 'vision', 'v', ?, ?)`, other, enc, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.Write.Exec(`UPDATE ai_profiles SET token_enc=? WHERE household_id=? AND role='text'`, enc, e.hid); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		hid  string
		role ai.Role
	}{{other, ai.RoleVision}, {e.hid, ai.RoleText}} {
		cfg, err := ai.For(ctx, e.q(), c.hid, c.role)
		if err == nil || cfg.Token != "" || strings.Contains(err.Error(), "tok-") {
			t.Fatalf("%v: %+v %v", c, cfg, err)
		}
	}
	if cfg, err := ai.For(ctx, e.q(), e.hid, ai.RoleVision); err != nil || cfg.Token != "tok-vision-7777" {
		t.Fatalf("original broke: %+v %v", cfg, err)
	}
}

func TestEnvSecretKey(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	e := newEnv(t, config.Config{SecretKey: config.Secret(key)})
	put(e, e.session(e.hid, "owner"), "vision", `{"model":"v","token":"tok-vision-4444"}`)
	if cfg, err := ai.For(context.Background(), e.q(), e.hid, ai.RoleVision); err != nil || cfg.Token != "tok-vision-4444" {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestStoredModeAndSetters(t *testing.T) {
	e := newEnv(t, config.Config{})
	put(e, e.session(e.hid, "owner"), "vision", `{"model":"v"}`)
	ctx := context.Background()
	w := sqlc.New(e.d.Write)
	if cfg, _ := ai.For(ctx, e.q(), e.hid, ai.RoleVision); cfg.Mode != ai.ModeAuto {
		t.Fatalf("default mode %q", cfg.Mode)
	}
	err := ai.SaveTest(ctx, w, e.hid, ai.RoleVision, ai.TestOutcome{Mode: ai.ModeJSONObject,
		Capabilities: ai.Capabilities{Images: true}, Result: json.RawMessage(`{"steps":[]}`), At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _ := ai.For(ctx, e.q(), e.hid, ai.RoleVision); cfg.Mode != ai.ModeJSONObject {
		t.Fatalf("stored mode %q", cfg.Mode)
	}
	if err := ai.MarkFailing(ctx, w, e.hid, ai.RoleVision, time.Now()); err != nil {
		t.Fatal(err)
	}
	owner := e.session(e.hid, "owner")
	_, body := e.do(owner, "GET", "/api/v1/settings/ai", "")
	if !strings.Contains(body, `"failing_since":"20`) || !strings.Contains(body, `"images":true`) {
		t.Fatalf("%s", body)
	}
	// Changing the model forgets the test.
	put(e, owner, "vision", `{"model":"other"}`)
	if cfg, _ := ai.For(ctx, e.q(), e.hid, ai.RoleVision); cfg.Mode != ai.ModeAuto {
		t.Fatalf("mode after change %q", cfg.Mode)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	ok := map[string]string{
		"https://openrouter.ai/api/v1":                   "https://openrouter.ai/api/v1",
		"  https://openrouter.ai/api/v1/  ":              "https://openrouter.ai/api/v1",
		"https://openrouter.ai/api/v1/chat/completions":  "https://openrouter.ai/api/v1",
		"https://openrouter.ai/api/v1/chat/completions/": "https://openrouter.ai/api/v1",
		"HTTP://Localhost:11434/v1/":                     "http://localhost:11434/v1",
		"http://192.168.1.5:8080":                        "http://192.168.1.5:8080",
		"http://[::1]:8080/":                             "http://[::1]:8080",
		"https://example.com/chat/completions":           "https://example.com",
		"https://example.com//":                          "https://example.com",
	}
	for in, want := range ok {
		if got, err := ai.NormalizeBaseURL(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "  ", "ftp://x.com", "javascript:alert(1)", "//x.com/v1", "example.com/v1", "https://", "https://user:pw@x.com/v1",
		"https://user@x.com", "https://x.com/v1?key=1", "https://x.com/v1#f", "file:///etc/passwd", "https://x.com/\x7f", "http://%zz"} {
		got, err := ai.NormalizeBaseURL(in)
		if err == nil {
			t.Errorf("%q accepted as %q", in, got)
		} else if strings.Contains(err.Error(), "pw") {
			t.Errorf("error repeats input: %v", err)
		}
	}
}

func TestProfileChangesWriteEventsWithoutToken(t *testing.T) {
	e := newEnv(t, config.Config{})
	owner := e.session(e.hid, "owner")
	if c, b := put(e, owner, "vision", `{"model":"m1","token":"tok-secret-7777"}`); c != 200 {
		t.Fatal(c, b)
	}
	put(e, owner, "vision", `{"model":"m2"}`)
	e.do(owner, "DELETE", "/api/v1/settings/ai/vision", "")
	rows, err := e.d.Read.Query(`SELECT kind, actor_kind, user_id, data FROM events WHERE household_id = ? ORDER BY at, id`, e.hid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var datas []string
	for rows.Next() {
		var kind, actor, uid, data string
		if err := rows.Scan(&kind, &actor, &uid, &data); err != nil {
			t.Fatal(err)
		}
		if kind != "settings_changed" || actor != "user" || uid == "" {
			t.Fatalf("event %s %s %s", kind, actor, uid)
		}
		datas = append(datas, data)
	}
	if len(datas) != 3 {
		t.Fatalf("%d events: %v", len(datas), datas)
	}
	all := strings.Join(datas, "\n")
	for _, bad := range []string{"tok-secret-7777", "7777"} {
		if strings.Contains(all, bad) {
			t.Fatalf("token leaked into events: %s", all)
		}
	}
	if !strings.Contains(datas[0], `"token_changed":true`) || !strings.Contains(datas[1], `"m2"`) || !strings.Contains(datas[2], `"cleared":true`) {
		t.Fatalf("unexpected data: %s", all)
	}
	// A no-op save writes nothing.
	put(e, owner, "vision", `{"model":"m3"}`)
	n := len(datas)
	put(e, owner, "vision", `{"model":"m3"}`)
	var cnt int
	e.d.Read.QueryRow(`SELECT count(*) FROM events WHERE household_id = ?`, e.hid).Scan(&cnt)
	if cnt != n+1 {
		t.Fatalf("events %d, want %d", cnt, n+1)
	}
}
