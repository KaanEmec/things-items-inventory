package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
)

const goodToken = "tok-good-token-0042"

// endpoint is an OpenAI-compatible fake with a few model personalities:
//
//	good        reads images, supports every JSON mode
//	text-only   rejects image input
//	prompt-only rejects response_format, reads images
//	blind       accepts images but answers with the wrong label
type endpoint struct {
	*httptest.Server
	chats atomic.Int32
}

func newEndpoint(t *testing.T) *endpoint {
	ep := &endpoint{}
	ep.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+goodToken {
			// Real endpoints sometimes echo the key back.
			http.Error(w, `{"error":{"message":"Incorrect API key provided: `+strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")+`"}}`, 401)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Write([]byte(`{"data":[
			 {"id":"text-only","name":"Text Only","architecture":{"input_modalities":["text"]}},
			 {"id":"unknown","name":"Unknown"},
			 {"id":"good","name":"Good","architecture":{"input_modalities":["text","image"]}}]}`))
			return
		}
		ep.chats.Add(1)
		raw := new(strings.Builder)
		buf := make([]byte, 1<<16)
		for {
			n, err := r.Body.Read(buf)
			raw.Write(buf[:n])
			if err != nil {
				break
			}
		}
		var body struct {
			Model          string          `json:"model"`
			ResponseFormat json.RawMessage `json:"response_format"`
		}
		json.Unmarshal([]byte(raw.String()), &body)
		hasImage := strings.Contains(raw.String(), "image_url")
		switch {
		case body.Model == "nope":
			http.Error(w, `{"error":{"message":"model not found"}}`, 404)
		case body.Model == "text-only" && hasImage:
			http.Error(w, `{"error":{"message":"this model does not support image input"}}`, 400)
		case body.Model == "prompt-only" && len(body.ResponseFormat) > 0:
			http.Error(w, `{"error":{"message":"response_format is not supported"}}`, 400)
		default:
			answer := `{"brand":"Sony","model":"WH-1000XM4","color":"black","category":"headphones"}`
			if hasImage {
				answer = `{"label_text":"MODEL KX-4821 SERIAL 7Q93","object":"label"}`
				if body.Model == "blind" {
					answer = `{"label_text":"unreadable","object":"blur"}`
				}
			}
			out, _ := json.Marshal(map[string]any{"model": body.Model,
				"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}})
			w.Write(out)
		}
	}))
	t.Cleanup(ep.Close)
	return ep
}

type testResp struct {
	OK    bool    `json:"ok"`
	Mode  *string `json:"mode"`
	Steps []struct {
		ID      string `json:"id"`
		OK      *bool  `json:"ok"`
		Message string `json:"message"`
	} `json:"steps"`
	TestedAt string `json:"tested_at"`
}

func (r testResp) status() string {
	var parts []string
	for _, s := range r.Steps {
		v := "null"
		if s.OK != nil {
			v = map[bool]string{true: "ok", false: "FAIL"}[*s.OK]
		}
		parts = append(parts, s.ID+"="+v)
	}
	return strings.Join(parts, " ")
}

func runTest(t *testing.T, e *env, cookie, role, body string) (testResp, string) {
	t.Helper()
	code, raw := e.do(cookie, "POST", "/api/v1/settings/ai/"+role+"/test", body)
	if code != 200 {
		t.Fatalf("test: %d %s", code, raw)
	}
	var r testResp
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	return r, raw
}

func saveProfile(t *testing.T, e *env, cookie, role, url, model string) {
	t.Helper()
	body := `{"base_url":"` + url + `","model":"` + model + `","token":"` + goodToken + `"`
	if role == "text" {
		body += `,"same_as_vision":false`
	}
	if c, b := put(e, cookie, role, body+"}"); c != 200 {
		t.Fatal(c, b)
	}
}

func TestConnectionTestVisionPassesAndStores(t *testing.T) {
	e, ep := newEnv(t, config.Config{}), newEndpoint(t)
	admin := e.session(e.hid, "admin")
	saveProfile(t, e, admin, "vision", ep.URL, "good")
	r, _ := runTest(t, e, admin, "vision", `{}`)
	if !r.OK || r.status() != "reach=ok token=ok model=ok images=ok json=ok" || r.Mode == nil || *r.Mode != "json_schema" || r.TestedAt == "" {
		t.Fatalf("%+v %s", r, r.status())
	}
	cfg, _ := ai.For(context.Background(), e.q(), e.hid, ai.RoleVision)
	if cfg.Mode != ai.ModeStrict {
		t.Fatalf("stored mode %q", cfg.Mode)
	}
	_, body := e.do(admin, "GET", "/api/v1/settings/ai", "")
	if !strings.Contains(body, `"images":true,"json_schema":true`) || !strings.Contains(body, `"test_result":{"ok":true`) {
		t.Fatal(body)
	}
}

func TestTextOnlyModelFailsAtImagesAndSavesNothingWorking(t *testing.T) {
	e, ep := newEnv(t, config.Config{}), newEndpoint(t)
	admin := e.session(e.hid, "admin")
	saveProfile(t, e, admin, "vision", ep.URL, "text-only")
	r, _ := runTest(t, e, admin, "vision", `{}`)
	if r.OK || r.status() != "reach=ok token=ok model=ok images=FAIL json=null" || r.Mode != nil {
		t.Fatal(r.status())
	}
	if !strings.Contains(r.Steps[3].Message, "This model can't read images. Pick a model that accepts image input for photo understanding.") {
		t.Fatal(r.Steps[3].Message)
	}
	cfg, _ := ai.For(context.Background(), e.q(), e.hid, ai.RoleVision)
	_, body := e.do(admin, "GET", "/api/v1/settings/ai", "")
	if cfg.Mode != ai.ModeAuto || !strings.Contains(body, `"capabilities":{"images":false,"json_schema":false}`) || !strings.Contains(body, `"test_result":{"ok":false`) {
		t.Fatal(cfg.Mode, body)
	}
}

func TestBlindModelFailsAtImages(t *testing.T) {
	e, ep := newEnv(t, config.Config{}), newEndpoint(t)
	admin := e.session(e.hid, "admin")
	saveProfile(t, e, admin, "vision", ep.URL, "blind")
	if r, _ := runTest(t, e, admin, "vision", `{}`); r.OK || r.status() != "reach=ok token=ok model=ok images=FAIL json=null" {
		t.Fatal(r.status())
	}
}

func TestPromptOnlyModelPasses(t *testing.T) {
	e, ep := newEnv(t, config.Config{}), newEndpoint(t)
	admin := e.session(e.hid, "admin")
	saveProfile(t, e, admin, "vision", ep.URL, "prompt-only")
	r, _ := runTest(t, e, admin, "vision", `{}`)
	if !r.OK || r.Mode == nil || *r.Mode != "prompt" || !strings.Contains(r.Steps[4].Message, "checks every answer itself") {
		t.Fatalf("%s %+v", r.status(), r)
	}
	if cfg, _ := ai.For(context.Background(), e.q(), e.hid, ai.RoleVision); cfg.Mode != ai.ModePrompt {
		t.Fatalf("stored mode %q", cfg.Mode)
	}
}

func TestTextRole(t *testing.T) {
	e, ep := newEnv(t, config.Config{}), newEndpoint(t)
	admin := e.session(e.hid, "admin")
	saveProfile(t, e, admin, "text", ep.URL, "text-only")
	r, _ := runTest(t, e, admin, "text", `{}`)
	if !r.OK || r.status() != "reach=ok token=ok model=ok json=ok" {
		t.Fatal(r.status())
	}
}

func TestWrongTokenFailsAtTokenAndUnsavedValuesAreNotStored(t *testing.T) {
	logs := captureLogs(t)
	e, ep := newEnv(t, config.Config{}), newEndpoint(t)
	admin := e.session(e.hid, "admin")
	saveProfile(t, e, admin, "vision", ep.URL, "good")
	const wrong = "tok-wrong-token-9999"
	r, raw := runTest(t, e, admin, "vision", `{"token":"`+wrong+`"}`)
	if r.OK || r.status() != "reach=ok token=FAIL model=null images=null json=null" {
		t.Fatal(r.status())
	}
	// Unsaved model that does not exist: fails at model, stores nothing.
	r2, raw2 := runTest(t, e, admin, "vision", `{"model":"nope"}`)
	if r2.status() != "reach=ok token=ok model=FAIL images=null json=null" {
		t.Fatal(r2.status())
	}
	// Unsaved base URL nobody listens on.
	r3, _ := runTest(t, e, admin, "vision", `{"base_url":"http://127.0.0.1:1"}`)
	if r3.status() != "reach=FAIL token=null model=null images=null json=null" {
		t.Fatal(r3.status())
	}
	// Nothing stored, saved token intact.
	_, body := e.do(admin, "GET", "/api/v1/settings/ai", "")
	if !strings.Contains(body, `"test_result":null`) || strings.Contains(body, `"tested_at":"`) {
		t.Fatal(body)
	}
	if cfg, _ := ai.For(context.Background(), e.q(), e.hid, ai.RoleVision); cfg.Token != goodToken || cfg.Model != "good" {
		t.Fatalf("%+v", cfg)
	}
	if c, b := e.do(admin, "POST", "/api/v1/settings/ai/vision/test", `{"token":"`+wrong+` x"}`); c != 422 && c != 400 {
		t.Fatal(c, b)
	}
	// The endpoint echoed both tokens in its error bodies; neither may show up.
	for name, s := range map[string]string{"resp1": raw, "resp2": raw2, "body": body, "logs": logs.String()} {
		if strings.Contains(s, wrong) || strings.Contains(s, goodToken) {
			t.Errorf("%s leaks a token: %s", name, s)
		}
	}
}

func TestSavedFailureMarksFailingAndSuccessClears(t *testing.T) {
	e, ep := newEnv(t, config.Config{}), newEndpoint(t)
	admin := e.session(e.hid, "admin")
	saveProfile(t, e, admin, "vision", ep.URL, "nope")
	runTest(t, e, admin, "vision", `{}`)
	_, body := e.do(admin, "GET", "/api/v1/settings/ai", "")
	if !strings.Contains(body, `"failing_since":"20`) {
		t.Fatal(body)
	}
	put(e, admin, "vision", `{"model":"good"}`)
	runTest(t, e, admin, "vision", `{}`)
	_, body = e.do(admin, "GET", "/api/v1/settings/ai", "")
	if !strings.Contains(body, `"failing_since":null`) {
		t.Fatal(body)
	}
}

func TestNoteResult(t *testing.T) {
	e, ep := newEnv(t, config.Config{}), newEndpoint(t)
	admin := e.session(e.hid, "admin")
	saveProfile(t, e, admin, "vision", ep.URL, "good")
	ctx, w := context.Background(), sqlc.New(e.d.Write)
	failing := func() bool {
		_, body := e.do(admin, "GET", "/api/v1/settings/ai", "")
		return strings.Contains(body, `"failing_since":"20`)
	}
	ai.NoteResult(ctx, w, e.hid, ai.RoleVision, &ai.Error{Kind: ai.KindRateLimited})
	if failing() {
		t.Fatal("rate limit marked failing")
	}
	ai.NoteResult(ctx, w, e.hid, ai.RoleVision, &ai.Error{Kind: ai.KindAuth})
	if !failing() {
		t.Fatal("auth error did not mark failing")
	}
	ai.NoteResult(ctx, w, e.hid, ai.RoleVision, nil)
	if failing() {
		t.Fatal("success did not clear")
	}
}

func TestModelsList(t *testing.T) {
	e, ep := newEnv(t, config.Config{}), newEndpoint(t)
	admin := e.session(e.hid, "admin")
	body := `{"base_url":"` + ep.URL + `","token":"` + goodToken + `"}`
	c, raw := e.do(admin, "POST", "/api/v1/settings/ai/vision/models", body)
	if c != 200 {
		t.Fatal(c, raw)
	}
	var out struct {
		Models []struct {
			ID          string
			Vision      bool
			VisionKnown bool `json:"vision_known"`
		}
	}
	json.Unmarshal([]byte(raw), &out)
	if len(out.Models) != 3 || out.Models[0].ID != "good" || !out.Models[0].Vision || out.Models[1].ID != "unknown" || out.Models[2].ID != "text-only" {
		t.Fatalf("%s", raw)
	}
	if c, raw := e.do(admin, "POST", "/api/v1/settings/ai/text/models", `{"base_url":"`+ep.URL+`","token":"tok-wrong-token-1"}`); c != 400 || strings.Contains(raw, "tok-wrong") {
		t.Fatal(c, raw)
	}
}

func TestConnectionTestRolesAndRateLimit(t *testing.T) {
	e, ep := newEnv(t, config.Config{}), newEndpoint(t)
	admin, member, viewer := e.session(e.hid, "admin"), e.session(e.hid, "member"), e.session(e.hid, "viewer")
	saveProfile(t, e, admin, "text", ep.URL, "good")
	for _, c := range []string{member, viewer} {
		for _, p := range []string{"test", "models"} {
			if code, _ := e.do(c, "POST", "/api/v1/settings/ai/text/"+p, `{}`); code != 403 {
				t.Fatalf("%s: %d", p, code)
			}
		}
	}
	if code, _ := e.do("", "POST", "/api/v1/settings/ai/text/test", `{}`); code != 401 {
		t.Fatal(code)
	}
	if code, _ := e.do(admin, "POST", "/api/v1/settings/ai/nope/test", `{}`); code != 404 {
		t.Fatal(code)
	}
	var limited bool
	for i := 0; i < 12; i++ {
		if code, _ := e.do(admin, "POST", "/api/v1/settings/ai/text/test", `{}`); code == 429 {
			limited = true
		}
	}
	if !limited {
		t.Fatal("no rate limit after 12 tests")
	}
}
