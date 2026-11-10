package ai

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
	"testing"
	"time"
)

const secret = "sk-SECRET-TOKEN-123"

var schema = json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)

func validName(raw json.RawMessage) error {
	var v struct{ Name string }
	if err := json.Unmarshal(raw, &v); err != nil || v.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

// fake records requests and answers via handler(n, body).
type fake struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []map[string]any
	hdrs []http.Header
}

func newFake(t *testing.T, h func(n int, body map[string]any, w http.ResponseWriter)) *fake {
	f := &fake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		f.mu.Lock()
		f.reqs = append(f.reqs, m)
		f.hdrs = append(f.hdrs, r.Header)
		n := len(f.reqs)
		f.mu.Unlock()
		h(n, m, w)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) cfg(mode Mode) Config {
	return Config{BaseURL: f.srv.URL, Token: secret, Model: "m1", Mode: mode}
}

func reply(w http.ResponseWriter, content string) {
	b, _ := json.Marshal(map[string]any{
		"model":   "m1-actual",
		"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "cost": 0.001},
	})
	w.Write(b)
}

func fail(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":{"message":%q}}`, msg)
}

func testClient() (*Client, *[]time.Duration) {
	c := New(nil)
	var sleeps []time.Duration
	c.sleep = func(ctx context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return ctx.Err()
	}
	c.jitter = func(time.Duration) time.Duration { return 0 }
	return c, &sleeps
}

var req = Request{System: "sys", User: "usr", SchemaName: "thing", Schema: schema}

func rf(m map[string]any) map[string]any {
	v, _ := m["response_format"].(map[string]any)
	return v
}

func TestModes(t *testing.T) {
	for _, mode := range []Mode{ModeStrict, ModeJSONObject, ModePrompt} {
		t.Run(string(mode), func(t *testing.T) {
			f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { reply(w, `{"name":"lamp"}`) })
			c, _ := testClient()
			res, err := c.Complete(context.Background(), f.cfg(mode), req, validName)
			if err != nil {
				t.Fatal(err)
			}
			if string(res.JSON) != `{"name":"lamp"}` || res.Mode != mode || res.Model != "m1-actual" {
				t.Fatalf("bad result %+v", res)
			}
			if res.Usage != (Usage{10, 5, 0.001}) {
				t.Fatalf("usage %+v", res.Usage)
			}
			if got := f.hdrs[0].Get("Authorization"); got != "Bearer "+secret {
				t.Fatalf("auth header %q", got)
			}
			r := rf(f.reqs[0])
			sys := f.reqs[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
			switch mode {
			case ModeStrict:
				js := r["json_schema"].(map[string]any)
				if r["type"] != "json_schema" || js["strict"] != true || js["name"] != "thing" {
					t.Fatalf("response_format %v", r)
				}
			case ModeJSONObject:
				if r["type"] != "json_object" || !strings.Contains(sys, `"required"`) {
					t.Fatalf("json_object %v %q", r, sys)
				}
			case ModePrompt:
				if r != nil || !strings.Contains(sys, "JSON only") {
					t.Fatalf("prompt %v %q", r, sys)
				}
			}
		})
	}
}

func TestAutoLadder(t *testing.T) {
	f := newFake(t, func(_ int, m map[string]any, w http.ResponseWriter) {
		if r := rf(m); r != nil {
			fail(w, 400, "response_format "+fmt.Sprint(r["type"])+" is not supported")
			return
		}
		reply(w, "Here you go:\n```json\n{\"name\":\"lamp\"}\n```")
	})
	c, _ := testClient()
	res, err := c.Complete(context.Background(), f.cfg(ModeAuto), req, validName)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != ModePrompt || len(f.reqs) != 3 || string(res.JSON) != `{"name":"lamp"}` {
		t.Fatalf("mode %s reqs %d json %s", res.Mode, len(f.reqs), res.JSON)
	}
}

func TestAutoStopsAtJSONObject(t *testing.T) {
	f := newFake(t, func(_ int, m map[string]any, w http.ResponseWriter) {
		if r := rf(m); r != nil && r["type"] == "json_schema" {
			fail(w, 400, "json_schema unsupported")
			return
		}
		reply(w, `{"name":"x"}`)
	})
	c, _ := testClient()
	res, err := c.Complete(context.Background(), f.cfg(""), req, validName)
	if err != nil || res.Mode != ModeJSONObject {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestExplicitModeDoesNotFallBack(t *testing.T) {
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { fail(w, 400, "json_schema unsupported") })
	c, _ := testClient()
	_, err := c.Complete(context.Background(), f.cfg(ModeStrict), req, validName)
	assertKind(t, err, KindStructuredUnsupport)
	if len(f.reqs) != 1 {
		t.Fatalf("reqs %d", len(f.reqs))
	}
}

func TestRepairRetry(t *testing.T) {
	f := newFake(t, func(n int, _ map[string]any, w http.ResponseWriter) {
		if n == 1 {
			reply(w, `{"name":""}`)
			return
		}
		reply(w, `{"name":"fixed"}`)
	})
	c, _ := testClient()
	res, err := c.Complete(context.Background(), f.cfg(ModeStrict), req, validName)
	if err != nil || string(res.JSON) != `{"name":"fixed"}` {
		t.Fatalf("%v %s", err, res.JSON)
	}
	msgs := f.reqs[1]["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)["content"].(string)
	if len(msgs) != 4 || !strings.Contains(last, "name is required") {
		t.Fatalf("repair conversation %v", msgs)
	}
	if res.Usage.PromptTokens != 20 {
		t.Fatalf("usage not summed: %+v", res.Usage)
	}
}

func TestInvalidOutputAfterOneRetry(t *testing.T) {
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { reply(w, "not json at all") })
	c, _ := testClient()
	_, err := c.Complete(context.Background(), f.cfg(ModePrompt), req, validName)
	assertKind(t, err, KindInvalidOutput)
	if len(f.reqs) != 2 {
		t.Fatalf("reqs %d", len(f.reqs))
	}
}

func TestErrorKinds(t *testing.T) {
	cases := []struct {
		status int
		msg    string
		kind   Kind
	}{
		{401, "bad key", KindAuth},
		{403, "forbidden", KindAuth},
		{404, "No such model", KindModelNotFound},
		{400, "The model `zzz` does not exist", KindModelNotFound},
		{402, "Insufficient credits", KindNoCredit},
		{429, "You exceeded your current quota", KindNoCredit},
		{404, "No endpoints found that support image input", KindNoImageInput},
		{400, "this model does not support image_url content", KindNoImageInput},
		{400, "response_format is not supported", KindStructuredUnsupport},
		{404, "No endpoints found that can handle the requested parameters", KindStructuredUnsupport},
		{418, "teapot", KindOther},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d_%s", tc.status, tc.kind), func(t *testing.T) {
			f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { fail(w, tc.status, tc.msg+" "+secret) })
			c, _ := testClient()
			_, err := c.Complete(context.Background(), f.cfg(ModePrompt), req, validName)
			assertKind(t, err, tc.kind)
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("token leaked: %v", err)
			}
			if tc.status == 429 && len(f.reqs) != 1 {
				t.Fatal("quota errors must not be retried")
			}
		})
	}
}

func TestRateLimitedAfterRetries(t *testing.T) {
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { fail(w, 429, "slow down") })
	c, sleeps := testClient()
	_, err := c.Complete(context.Background(), f.cfg(ModePrompt), req, validName)
	assertKind(t, err, KindRateLimited)
	if len(f.reqs) != 4 || len(*sleeps) != 3 {
		t.Fatalf("reqs %d sleeps %v", len(f.reqs), *sleeps)
	}
	s := *sleeps
	if s[0] != 500*time.Millisecond || s[1] != time.Second || s[2] != 2*time.Second {
		t.Fatalf("backoff %v", s)
	}
}

func TestRetryAfterAnd5xxThenSuccess(t *testing.T) {
	f := newFake(t, func(n int, _ map[string]any, w http.ResponseWriter) {
		switch n {
		case 1:
			w.Header().Set("Retry-After", "7")
			fail(w, 429, "wait")
		case 2:
			fail(w, 503, "down")
		default:
			reply(w, `{"name":"ok"}`)
		}
	})
	c, sleeps := testClient()
	if _, err := c.Complete(context.Background(), f.cfg(ModePrompt), req, validName); err != nil {
		t.Fatal(err)
	}
	if len(*sleeps) != 2 || (*sleeps)[0] != 7*time.Second || (*sleeps)[1] != time.Second {
		t.Fatalf("sleeps %v", *sleeps)
	}
}

func TestClientErrorsNotRetried(t *testing.T) {
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { fail(w, 401, "no") })
	c, sleeps := testClient()
	c.Complete(context.Background(), f.cfg(ModePrompt), req, validName)
	if len(f.reqs) != 1 || len(*sleeps) != 0 {
		t.Fatal("401 was retried")
	}
}

func TestCancellationStopsRetries(t *testing.T) {
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { fail(w, 503, "down") })
	ctx, cancel := context.WithCancel(context.Background())
	c, _ := testClient()
	c.sleep = func(ctx context.Context, d time.Duration) error { cancel(); return ctx.Err() }
	_, err := c.Complete(ctx, f.cfg(ModePrompt), req, validName)
	if !errors.Is(err, context.Canceled) || len(f.reqs) != 1 {
		t.Fatalf("err %v reqs %d", err, len(f.reqs))
	}
}

func TestTimeout(t *testing.T) {
	release := make(chan struct{})
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { <-release })
	defer close(release)
	c, _ := testClient()
	c.Timeout = 50 * time.Millisecond
	_, err := c.Complete(context.Background(), f.cfg(ModePrompt), req, validName)
	assertKind(t, err, KindTimeout)
}

func TestUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c, _ := testClient()
	_, err := c.Complete(context.Background(), Config{BaseURL: url, Token: secret, Model: "m", Mode: ModePrompt}, req, validName)
	assertKind(t, err, KindUnreachable)
	if strings.Contains(err.Error(), secret) {
		t.Fatal("token leaked")
	}
}

func TestOpenRouterFlag(t *testing.T) {
	if !isOpenRouter("https://openrouter.ai/api/v1") || isOpenRouter("https://api.openai.com/v1") ||
		isOpenRouter("https://openrouter.ai.evil.example/v1") || isOpenRouter("http://localhost/openrouter.ai") {
		t.Fatal("host detection wrong")
	}
	// A local fake never gets the flag.
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { reply(w, `{"name":"a"}`) })
	c, _ := testClient()
	if _, err := c.Complete(context.Background(), f.cfg(ModeStrict), req, validName); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.reqs[0]["provider"]; ok {
		t.Fatal("provider sent to non-OpenRouter host")
	}
	// Present when the host is openrouter.ai: route the client there.
	var got map[string]any
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		rec := httptest.NewRecorder()
		reply(rec, `{"name":"a"}`)
		return rec.Result(), nil
	})}
	cfg := Config{BaseURL: "https://openrouter.ai/api/v1", Token: secret, Model: "m", Mode: ModeStrict}
	if _, err := c.Complete(context.Background(), cfg, req, validName); err != nil {
		t.Fatal(err)
	}
	p, _ := got["provider"].(map[string]any)
	if p["require_parameters"] != true {
		t.Fatalf("provider %v", got["provider"])
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestImagesEncoded(t *testing.T) {
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { reply(w, `{"name":"a"}`) })
	c, _ := testClient()
	r := req
	r.Images = [][]byte{{0xff, 0xd8, 0x01}, {0xff, 0xd8, 0x02}}
	if _, err := c.Complete(context.Background(), f.cfg(ModePrompt), r, validName); err != nil {
		t.Fatal(err)
	}
	msgs := f.reqs[0]["messages"].([]any)
	parts := msgs[1].(map[string]any)["content"].([]any)
	if len(parts) != 3 || parts[0].(map[string]any)["text"] != "usr" {
		t.Fatalf("parts %v", parts)
	}
	want := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0x02})
	if got := parts[2].(map[string]any)["image_url"].(map[string]any)["url"]; got != want {
		t.Fatalf("url %v", got)
	}
}

func TestModels(t *testing.T) {
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
		fmt.Fprint(w, `{"data":[
		 {"id":"a/vis","name":"Vision","architecture":{"input_modalities":["text","image"]}},
		 {"id":"b/txt","name":"Text","architecture":{"input_modalities":["text"]}},
		 {"id":"c/plain"}]}`)
	})
	c, _ := testClient()
	ms, err := c.Models(context.Background(), f.cfg(""))
	if err != nil {
		t.Fatal(err)
	}
	want := []Model{{"a/vis", "Vision", true, true}, {"b/txt", "Text", false, true}, {"c/plain", "c/plain", false, false}}
	if fmt.Sprint(ms) != fmt.Sprint(want) {
		t.Fatalf("%v", ms)
	}
	if f.hdrs[0].Get("Authorization") != "Bearer "+secret {
		t.Fatal("no auth on models")
	}
}

func TestModelsAuthError(t *testing.T) {
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { fail(w, 401, "bad "+secret) })
	c, _ := testClient()
	_, err := c.Models(context.Background(), f.cfg(""))
	assertKind(t, err, KindAuth)
	if strings.Contains(err.Error(), secret) {
		t.Fatal("token leaked")
	}
}

func TestValidateErrorWithTokenIsScrubbed(t *testing.T) {
	f := newFake(t, func(_ int, _ map[string]any, w http.ResponseWriter) { reply(w, `{"name":"a"}`) })
	c, _ := testClient()
	_, err := c.Complete(context.Background(), f.cfg(ModePrompt), req, func(json.RawMessage) error { return errors.New("bad " + secret) })
	assertKind(t, err, KindInvalidOutput)
	if strings.Contains(err.Error(), secret) {
		t.Fatal("token leaked")
	}
}

func assertKind(t *testing.T, err error, kind Kind) {
	t.Helper()
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != kind {
		t.Fatalf("want kind %s, got %v", kind, err)
	}
}
