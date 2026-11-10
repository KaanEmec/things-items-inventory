package ai_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
)

const (
	actToken  = "tok-activity-secret-7788"
	actPrompt = "PROMPT-SECRET-sony-headphones"
)

// chatEndpoint answers chat completions with usage, or with 401 when failing.
type chatEndpoint struct {
	*httptest.Server
	hits atomic.Int32
	mu   sync.Mutex
	fail bool
}

func newChatEndpoint(t *testing.T) *chatEndpoint {
	c := &chatEndpoint{}
	c.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		c.hits.Add(1)
		c.mu.Lock()
		fail := c.fail
		c.mu.Unlock()
		if fail {
			w.WriteHeader(401)
			fmt.Fprintf(w, `{"error":"bad key %s"}`, actToken)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"served-model","choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":100,"completion_tokens":20,"cost":0.0025}}`))
	}))
	t.Cleanup(c.Close)
	return c
}

func (c *chatEndpoint) setFail(b bool) { c.mu.Lock(); c.fail = b; c.mu.Unlock() }

func actEnv(t *testing.T) (*env, *chatEndpoint, string) {
	e := newEnv(t, config.Config{})
	ep := newChatEndpoint(t)
	admin := e.session(e.hid, "admin")
	if c, b := put(e, admin, "vision", `{"base_url":"`+ep.URL+`","model":"vm","token":"`+actToken+`"}`); c != 200 {
		t.Fatal(c, b)
	}
	return e, ep, admin
}

var okSchema = json.RawMessage(`{"type":"object"}`)

func (e *env) run(feature ai.Feature, ref ai.Ref) (ai.Result, error) {
	return e.svc.Run(context.Background(), e.hid, ai.RoleVision, feature,
		ai.Request{User: actPrompt, Images: [][]byte{[]byte("IMAGEBYTES-xyz")}, Schema: okSchema},
		func(json.RawMessage) error { return nil }, ref)
}

type actPage struct {
	Data []struct {
		ID         string   `json:"id"`
		Feature    string   `json:"feature"`
		Role       string   `json:"role"`
		Model      string   `json:"model"`
		TokensIn   *int64   `json:"tokens_in"`
		TokensOut  *int64   `json:"tokens_out"`
		Cost       *float64 `json:"cost"`
		OK         bool     `json:"ok"`
		ErrorCode  *string  `json:"error_code"`
		Error      *string  `json:"error"`
		JobID      *string  `json:"job_id"`
		ItemID     *string  `json:"item_id"`
		DurationMs int64    `json:"duration_ms"`
	} `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

func (e *env) activityPage(cookie, query string) (actPage, string) {
	e.t.Helper()
	c, body := e.do(cookie, "GET", "/api/v1/settings/ai/activity"+query, "")
	if c != 200 {
		e.t.Fatal(c, body)
	}
	var p actPage
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		e.t.Fatal(err)
	}
	return p, body
}

func TestRunRecordsSuccessAndFailureWithoutSecrets(t *testing.T) {
	e, ep, admin := actEnv(t)
	if _, err := e.run(ai.FeatureLabel, ai.Ref{ItemID: "ITEM1", JobID: "JOB1"}); err != nil {
		t.Fatal(err)
	}
	ep.setFail(true)
	if _, err := e.run(ai.FeatureReceipt, ai.Ref{}); err == nil {
		t.Fatal("want an error")
	}
	p, body := e.activityPage(admin, "")
	if len(p.Data) != 2 {
		t.Fatalf("rows %d: %s", len(p.Data), body)
	}
	fail, ok := p.Data[0], p.Data[1] // newest first
	if !ok.OK || ok.Feature != "label" || ok.Role != "vision" || ok.Model != "served-model" ||
		ok.TokensIn == nil || *ok.TokensIn != 100 || *ok.TokensOut != 20 || ok.Cost == nil || *ok.Cost != 0.0025 ||
		ok.ItemID == nil || *ok.ItemID != "ITEM1" || ok.JobID == nil || *ok.JobID != "JOB1" || ok.ErrorCode != nil {
		t.Fatalf("ok row %+v", ok)
	}
	if fail.OK || fail.Feature != "receipt" || fail.Model != "vm" || fail.ErrorCode == nil || *fail.ErrorCode != "auth" || fail.Error == nil || fail.Cost != nil {
		t.Fatalf("failed row %+v", fail)
	}
	// Nothing sensitive anywhere in the API output or the raw table.
	rows, err := e.d.Read.Query(`SELECT * FROM ai_activity`)
	if err != nil {
		t.Fatal(err)
	}
	cols, _ := rows.Columns()
	var dump strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rows.Scan(ptrs...)
		fmt.Fprint(&dump, vals...)
	}
	rows.Close()
	all := body + dump.String()
	for _, secret := range []string{actToken, actPrompt, "IMAGEBYTES", base64.StdEncoding.EncodeToString([]byte("IMAGEBYTES-xyz"))} {
		if strings.Contains(all, secret) {
			t.Fatalf("activity contains %q", secret)
		}
	}
}

func TestRunStoresDiscoveredOutputMode(t *testing.T) {
	e, _, _ := actEnv(t)
	if _, err := e.run(ai.FeatureItemDraft, ai.Ref{}); err != nil {
		t.Fatal(err)
	}
	row, err := sqlc.New(e.d.Read).GetAIProfile(context.Background(), sqlc.GetAIProfileParams{HouseholdID: e.hid, Role: "vision"})
	if err != nil || row.OutputMode.String != "json_schema" {
		t.Fatalf("output mode %+v %v", row.OutputMode, err)
	}
}

func TestRunWhenPausedSendsNothing(t *testing.T) {
	e, ep, admin := actEnv(t)
	if _, err := e.d.Write.Exec(`UPDATE households SET ai_paused = 1 WHERE id = ?`, e.hid); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run(ai.FeatureLabel, ai.Ref{}); !errors.Is(err, ai.ErrPaused) {
		t.Fatalf("err %v", err)
	}
	if p, err := e.svc.IsPaused(context.Background(), e.hid); err != nil || !p {
		t.Fatalf("IsPaused %v %v", p, err)
	}
	if ep.hits.Load() != 0 {
		t.Fatal("a request was sent while paused")
	}
	if p, _ := e.activityPage(admin, ""); len(p.Data) != 0 {
		t.Fatal("a paused refusal must not be logged as a request")
	}
	_, body := e.do(admin, "GET", "/api/v1/settings/ai", "")
	var got map[string]struct {
		Paused *bool `json:"paused"`
	}
	json.Unmarshal([]byte(body), &got)
	if got["vision"].Paused == nil || !*got["vision"].Paused {
		t.Fatalf("GET /settings/ai: %s", body)
	}
	e.d.Write.Exec(`UPDATE households SET ai_paused = 0 WHERE id = ?`, e.hid)
	if _, err := e.run(ai.FeatureLabel, ai.Ref{}); err != nil {
		t.Fatal(err)
	}
}

func TestRunNotSetUp(t *testing.T) {
	e := newEnv(t, config.Config{})
	_, err := e.run(ai.FeatureLabel, ai.Ref{})
	if !errors.Is(err, ai.ErrNotSetUp) {
		t.Fatalf("err %v", err)
	}
	if n := countRows(t, e, e.hid); n != 0 {
		t.Fatalf("%d rows", n)
	}
}

func countRows(t *testing.T, e *env, hid string) int {
	var n int
	if err := e.d.Read.QueryRow(`SELECT COUNT(*) FROM ai_activity WHERE household_id = ?`, hid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMonthlyUsageEqualsSumOfRows(t *testing.T) {
	e, ep, admin := actEnv(t)
	for i := 0; i < 3; i++ {
		if _, err := e.run(ai.FeatureLabel, ai.Ref{}); err != nil {
			t.Fatal(err)
		}
	}
	ep.setFail(true)
	e.run(ai.FeatureLabel, ai.Ref{})
	ep.setFail(false)
	// A text request, and a row from an earlier month that must not count.
	if _, err := e.svc.Run(context.Background(), e.hid, ai.RoleText, ai.FeatureTextToItems, ai.Request{User: "x", Schema: okSchema},
		func(json.RawMessage) error { return nil }, ai.Ref{}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02T15:04:05.000Z")
	if _, err := e.d.Write.Exec(`INSERT INTO ai_activity (id, household_id, at, feature, role, model, tokens_in, tokens_out, cost, duration_ms, ok)
		VALUES ('OLD', ?, ?, 'label', 'vision', 'm', 999, 999, 9.0, 1, 1)`, e.hid, old); err != nil {
		t.Fatal(err)
	}

	c, body := e.do(admin, "GET", "/api/v1/settings/ai/usage", "")
	if c != 200 {
		t.Fatal(c, body)
	}
	var raw struct {
		Month string                    `json:"month"`
		Roles map[string]map[string]any `json:"roles"`
	}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Month != time.Now().UTC().Format("2006-01") {
		t.Fatalf("month %q", raw.Month)
	}
	for _, role := range []string{"vision", "text"} {
		var req, fails, in, out int64
		var cost float64
		err := e.d.Read.QueryRow(`SELECT COUNT(*), COALESCE(SUM(1-ok),0), COALESCE(SUM(tokens_in),0), COALESCE(SUM(tokens_out),0), COALESCE(SUM(cost),0)
			FROM ai_activity WHERE household_id = ? AND role = ? AND id <> 'OLD'`, e.hid, role).Scan(&req, &fails, &in, &out, &cost)
		if err != nil {
			t.Fatal(err)
		}
		g := raw.Roles[role]
		if g["requests"] != float64(req) || g["failures"] != float64(fails) || g["tokens_in"] != float64(in) ||
			g["tokens_out"] != float64(out) || g["cost"] != cost {
			t.Fatalf("%s: got %v, rows say %d %d %d %d %v", role, g, req, fails, in, out, cost)
		}
	}
	if v := raw.Roles["vision"]; v["requests"] != float64(4) || v["failures"] != float64(1) || v["tokens_in"] != float64(300) {
		t.Fatalf("vision %v", v)
	}
}

func TestActivityPagingAndAccess(t *testing.T) {
	e, _, admin := actEnv(t)
	for i := 0; i < 5; i++ {
		if _, err := e.run(ai.FeatureLabel, ai.Ref{}); err != nil {
			t.Fatal(err)
		}
	}
	p1, _ := e.activityPage(admin, "?limit=2")
	if len(p1.Data) != 2 || p1.NextCursor == nil {
		t.Fatalf("page 1: %+v", p1)
	}
	p2, _ := e.activityPage(admin, "?limit=2&cursor="+*p1.NextCursor)
	p3, _ := e.activityPage(admin, "?limit=2&cursor="+*p2.NextCursor)
	if len(p2.Data) != 2 || len(p3.Data) != 1 || p3.NextCursor != nil {
		t.Fatalf("pages %d %d %d", len(p1.Data), len(p2.Data), len(p3.Data))
	}
	seen := map[string]bool{}
	for _, p := range []actPage{p1, p2, p3} {
		for _, r := range p.Data {
			if seen[r.ID] {
				t.Fatal("duplicate row across pages")
			}
			seen[r.ID] = true
		}
	}
	if c, _ := e.do(admin, "GET", "/api/v1/settings/ai/activity?cursor=%25%25", ""); c != 400 {
		t.Fatalf("bad cursor %d", c)
	}
	for _, role := range []string{"viewer", "member", "admin"} {
		ck := e.session(e.hid, role)
		for _, path := range []string{"/api/v1/settings/ai/activity", "/api/v1/settings/ai/usage"} {
			if c, b := e.do(ck, "GET", path, ""); c != 200 {
				t.Errorf("%s %s: %d %s", role, path, c, b)
			}
		}
	}
	for _, path := range []string{"/api/v1/settings/ai/activity", "/api/v1/settings/ai/usage"} {
		if c, _ := e.do("", "GET", path, ""); c != 401 {
			t.Errorf("anonymous %s: %d", path, c)
		}
	}
}

func TestActivityIsPerHousehold(t *testing.T) {
	e, _, admin := actEnv(t)
	if _, err := e.run(ai.FeatureLabel, ai.Ref{}); err != nil {
		t.Fatal(err)
	}
	other := e.household("Other")
	markers, err := e.svc.Seed(context.Background(), e.d, other)
	if err != nil {
		t.Fatal(err)
	}
	p, body := e.activityPage(admin, "")
	if len(p.Data) != 1 {
		t.Fatalf("rows %d", len(p.Data))
	}
	_, usage := e.do(admin, "GET", "/api/v1/settings/ai/usage", "")
	for _, m := range markers {
		if strings.Contains(body+usage, m) {
			t.Fatalf("household B marker %q leaked", m)
		}
	}
	if countRows(t, e, other) != 1 {
		t.Fatal("seed should leave one activity row")
	}
}

func TestConnectionTestIsLogged(t *testing.T) {
	e, _, admin := actEnv(t)
	if c, b := e.do(admin, "POST", "/api/v1/settings/ai/vision/test", `{}`); c != 200 {
		t.Fatal(c, b)
	}
	p, _ := e.activityPage(admin, "")
	if len(p.Data) == 0 || p.Data[0].Feature != "test" {
		t.Fatalf("rows %+v", p.Data)
	}
}
