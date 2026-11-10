package quickadd_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
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
	"github.com/kaanemec/things-items-inventory/internal/quickadd"
)

const (
	stamp = "2026-01-01T00:00:00.000Z"
	token = "tok-quickadd-secret-4411"
)

// endpoint is an OpenAI-compatible fake that completes the rows it is sent.
type endpoint struct {
	*httptest.Server
	calls atomic.Int32
	mu    sync.Mutex
	seen  []string // user messages
	fail  bool
	delay time.Duration
}

var pickedRe = regexp.MustCompile(`\(category picked: (\w+)\)`)
var rowLine = regexp.MustCompile(`(?m)^(\d+)\t(.*)$`)

func newEndpoint(t *testing.T) *endpoint {
	ep := &endpoint{}
	ep.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ep.calls.Add(1)
		var body struct {
			Messages []struct{ Role, Content string }
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		user := body.Messages[len(body.Messages)-1].Content
		ep.mu.Lock()
		ep.seen = append(ep.seen, user)
		fail, delay := ep.fail, ep.delay
		ep.mu.Unlock()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		if fail {
			w.WriteHeader(401)
			fmt.Fprintf(w, `{"error":"bad key %s"}`, token)
			return
		}
		var rows []string
		for _, m := range rowLine.FindAllStringSubmatch(user, -1) {
			cat := "gadget"
			if p := pickedRe.FindStringSubmatch(m[2]); p != nil {
				cat = p[1]
			}
			attrs := `[]`
			if cat == "gadget" {
				attrs = `[{"key":"type","value":"headphones","confidence":"sure"}]`
			}
			if strings.Contains(m[2], "wh1000") {
				rows = append(rows, fmt.Sprintf(`{"id":%q,"category":{"value":%q,"confidence":"sure"},"brand":{"value":"Sony","confidence":"likely"},`+
					`"model":{"value":"WH-1000XM4","confidence":"likely"},"name":{"value":"Sony WH-1000XM4 headphones","confidence":"sure"},`+
					`"color":{"value":"Black","confidence":"sure"},"quantity":null,"attributes":%s}`, m[1], cat, attrs))
			} else {
				rows = append(rows, fmt.Sprintf(`{"id":%q,"category":null,"brand":null,"model":null,"name":null,"color":null,"quantity":null,"attributes":[]}`, m[1]))
			}
		}
		content, _ := json.Marshal(`{"rows":[` + strings.Join(rows, ",") + `]}`)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"model":"m","choices":[{"message":{"content":%s}}]}`, content)
	}))
	t.Cleanup(ep.Close)
	return ep
}

func (e *endpoint) setFail(b bool) { e.mu.Lock(); e.fail = b; e.mu.Unlock() }

type env struct {
	t   *testing.T
	d   *db.DB
	srv *httptest.Server
	svc *quickadd.Service
	hid string
	ep  *endpoint
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d := dbtest.New(t)
	cfg := config.Config{DataDir: t.TempDir()}
	aiSvc := ai.NewService(d, cfg)
	svc := quickadd.New(d, aiSvc)
	srv := httptest.NewServer(httpapi.New("t", "t", nil, auth.New(d, cfg).Routes, aiSvc.Routes, svc.Routes))
	t.Cleanup(srv.Close)
	e := &env{t: t, d: d, srv: srv, svc: svc, ep: newEndpoint(t)}
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
	tok := id.New() + id.New()
	sum := sha256.Sum256([]byte(tok))
	if err := q.CreateSession(ctx, sqlc.CreateSessionParams{ID: id.New(), TokenHash: hex.EncodeToString(sum[:]),
		UserID: uid, HouseholdID: hid, CreatedAt: stamp, LastSeenAt: "2099-01-01T00:00:00.000Z", ExpiresAt: "2099-01-01T00:00:00.000Z"}); err != nil {
		e.t.Fatal(err)
	}
	return tok
}

func (e *env) do(cookie, method, path, body string) (int, http.Header, string) {
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
	return res.StatusCode, res.Header, b.String()
}

// setUp points the household's text role at the fake endpoint.
func (e *env) setUp(hid string) {
	e.t.Helper()
	admin := e.session(hid, "admin")
	c, _, b := e.do(admin, "PUT", "/api/v1/settings/ai/text", `{"base_url":"`+e.ep.URL+`","model":"tm","token":"`+token+`","same_as_vision":false}`)
	if c != 200 {
		e.t.Fatal(c, b)
	}
}

type result struct {
	Rows []struct {
		ID         string            `json:"id"`
		Cells      map[string]string `json:"cells"`
		Confidence map[string]string `json:"confidence"`
	} `json:"rows"`
}

func (e *env) complete(cookie, body string) (int, http.Header, result, string) {
	e.t.Helper()
	c, h, b := e.do(cookie, "POST", "/api/v1/ai/complete-rows", body)
	var r result
	json.Unmarshal([]byte(b), &r)
	return c, h, r, b
}

func TestBatchesRowsInOneCallAndAnswersWithSheetKeys(t *testing.T) {
	e := newEnv(t)
	e.setUp(e.hid)
	m := e.session(e.hid, "member")
	c, _, r, b := e.complete(m, `{"rows":[{"id":"a","name":"wh1000xm4 black","values":{}},{"id":"b","name":"asdf","values":{}},{"id":"c","name":"  WH1000XM4   Black ","values":{}}]}`)
	if c != 200 {
		t.Fatal(c, b)
	}
	if n := e.ep.calls.Load(); n != 1 {
		t.Fatalf("%d endpoint calls, want 1", n)
	}
	// "a" and "c" are the same input after normalising: one question, two answers; "asdf" gets nothing.
	if len(r.Rows) != 2 || r.Rows[0].ID != "a" || r.Rows[1].ID != "c" {
		t.Fatalf("rows: %s", b)
	}
	cells := r.Rows[0].Cells
	if cells["brand"] != "Sony" || cells["model"] != "WH-1000XM4" || cells["category"] != "gadget" ||
		cells["name"] != "Sony WH-1000XM4 headphones" || cells["attr.type"] != "headphones" {
		t.Fatalf("cells %v", cells)
	}
	if r.Rows[0].Confidence["brand"] != "likely" {
		t.Fatalf("confidence %v", r.Rows[0].Confidence)
	}
	if strings.Count(e.ep.seen[0], "wh1000") != 1 {
		t.Fatalf("duplicate input sent twice: %s", e.ep.seen[0])
	}
}

func TestCacheHitSendsNothingAndIsPerHousehold(t *testing.T) {
	e := newEnv(t)
	e.setUp(e.hid)
	m := e.session(e.hid, "member")
	body := `{"rows":[{"id":"1","name":"wh1000xm4 black","values":{}}]}`
	e.complete(m, body)
	_, _, r, _ := e.complete(m, `{"rows":[{"id":"zz","name":"WH1000XM4  black","values":{}}]}`)
	if e.ep.calls.Load() != 1 || len(r.Rows) != 1 || r.Rows[0].ID != "zz" || r.Rows[0].Cells["brand"] != "Sony" {
		t.Fatalf("calls %d rows %+v", e.ep.calls.Load(), r.Rows)
	}
	// A different picked category is a different question.
	e.complete(m, `{"rows":[{"id":"1","name":"wh1000xm4 black","values":{"category":"other"}}]}`)
	if e.ep.calls.Load() != 2 {
		t.Fatalf("calls %d", e.ep.calls.Load())
	}
	// Another household never sees this household's cache.
	other := e.household("Other")
	e.setUp(other)
	om := e.session(other, "member")
	e.complete(om, body)
	if e.ep.calls.Load() != 3 {
		t.Fatalf("calls %d: the cache leaked across households", e.ep.calls.Load())
	}
}

func TestRateLimitPerHousehold(t *testing.T) {
	e := newEnv(t)
	e.setUp(e.hid)
	m := e.session(e.hid, "member")
	for i := 0; i < 30; i++ {
		if c, _, _, b := e.complete(m, fmt.Sprintf(`{"rows":[{"id":"1","name":"thing %d","values":{}}]}`, i)); c != 200 {
			t.Fatalf("request %d: %d %s", i, c, b)
		}
	}
	c, h, _, b := e.complete(m, `{"rows":[{"id":"1","name":"one more","values":{}}]}`)
	if c != 429 || h.Get("Retry-After") == "" || !strings.Contains(b, `"rate_limited"`) {
		t.Fatalf("%d %v %s", c, h, b)
	}
	// Cached answers still work, and another household is not limited.
	if c, _, _, _ := e.complete(m, `{"rows":[{"id":"1","name":"thing 3","values":{}}]}`); c != 200 {
		t.Fatalf("cache hit under limit: %d", c)
	}
	other := e.household("Other")
	e.setUp(other)
	if c, _, _, b := e.complete(e.session(other, "member"), `{"rows":[{"id":"1","name":"one more","values":{}}]}`); c != 200 {
		t.Fatalf("other household: %d %s", c, b)
	}
}

func TestNotSetUpPausedAndEndpointFailureNeverLeakTheToken(t *testing.T) {
	e := newEnv(t)
	m := e.session(e.hid, "member")
	body := `{"rows":[{"id":"1","name":"wh1000xm4","values":{}}]}`
	if c, _, _, b := e.complete(m, body); c != 409 || !strings.Contains(b, `"ai_not_set_up"`) {
		t.Fatalf("not set up: %d %s", c, b)
	}
	e.setUp(e.hid)
	e.d.Write.Exec(`UPDATE households SET ai_paused = 1 WHERE id = ?`, e.hid)
	if c, _, _, b := e.complete(m, body); c != 409 || !strings.Contains(b, `"ai_paused"`) {
		t.Fatalf("paused: %d %s", c, b)
	}
	e.d.Write.Exec(`UPDATE households SET ai_paused = 0 WHERE id = ?`, e.hid)
	if e.ep.calls.Load() != 0 {
		t.Fatal("a request was sent while not set up or paused")
	}
	e.ep.setFail(true)
	c, _, _, b := e.complete(m, body)
	if c != 502 || !strings.Contains(b, `"ai_failed"`) || strings.Contains(b, token) {
		t.Fatalf("failure: %d %s", c, b)
	}
}

func TestMembersOnlyAndLimits(t *testing.T) {
	e := newEnv(t)
	e.setUp(e.hid)
	body := `{"rows":[{"id":"1","name":"wh1000xm4","values":{}}]}`
	if c, _, _, _ := e.complete(e.session(e.hid, "viewer"), body); c != 403 {
		t.Fatalf("viewer: %d", c)
	}
	if c, _, _, _ := e.complete("nope", body); c != 401 {
		t.Fatalf("anonymous: %d", c)
	}
	m := e.session(e.hid, "member")
	var rows []string
	for i := 0; i < 21; i++ {
		rows = append(rows, fmt.Sprintf(`{"id":"%d","name":"n%d","values":{}}`, i, i))
	}
	if c, _, _, _ := e.complete(m, `{"rows":[`+strings.Join(rows, ",")+`]}`); c != 422 {
		t.Fatalf("21 rows: %d", c)
	}
	if c, _, _, _ := e.complete(m, `{"rows":[{"id":"1","name":"a","values":{}},{"id":"1","name":"b","values":{}}]}`); c != 422 {
		t.Fatalf("duplicate id: %d", c)
	}
	if c, _, r, _ := e.complete(m, `{"rows":[]}`); c != 200 || len(r.Rows) != 0 {
		t.Fatalf("empty: %d", c)
	}
}

func TestSlowEndpointTimesOut(t *testing.T) {
	e := newEnv(t)
	e.setUp(e.hid)
	e.svc.SetTimeout(150 * time.Millisecond)
	e.ep.mu.Lock()
	e.ep.delay = 2 * time.Second
	e.ep.mu.Unlock()
	start := time.Now()
	c, _, _, b := e.complete(e.session(e.hid, "member"), `{"rows":[{"id":"1","name":"wh1000xm4","values":{}}]}`)
	if c != 502 || !strings.Contains(b, "ai_failed") || time.Since(start) > time.Second {
		t.Fatalf("%d %s after %v", c, b, time.Since(start))
	}
}

func TestSeedIsAHouseholdsOwn(t *testing.T) {
	e := newEnv(t)
	e.setUp(e.hid)
	markers, err := e.svc.Seed(context.Background(), e.d, e.hid)
	if err != nil || len(markers) == 0 {
		t.Fatal(err)
	}
	other := e.household("Other")
	e.setUp(other)
	_, _, _, b := e.complete(e.session(other, "member"), `{"rows":[{"id":"1","name":"b-secret-quickadd-name","values":{}}]}`)
	for _, m := range markers[:1] {
		if strings.Contains(b, m) {
			t.Fatalf("household B's cache answered household A: %s", b)
		}
	}
}
