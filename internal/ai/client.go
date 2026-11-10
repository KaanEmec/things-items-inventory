// Package ai talks to any OpenAI-compatible chat completions endpoint and
// returns JSON that has been validated by the caller. Standard library only.
package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Mode is how structured output is requested.
type Mode string

const (
	ModeAuto       Mode = "auto"        // try strict, then json_object, then prompt
	ModeStrict     Mode = "strict"      // response_format json_schema, strict
	ModeJSONObject Mode = "json_object" // response_format json_object, schema in prompt
	ModePrompt     Mode = "prompt"      // "answer with JSON only"
)

type Config struct {
	BaseURL, Token, Model string
	Mode                  Mode // empty means auto
}

type Request struct {
	System, User string
	Images       [][]byte // JPEG bytes, sent as base64 data URLs
	SchemaName   string
	Schema       json.RawMessage
	MaxTokens    int
}

type Usage struct {
	PromptTokens, CompletionTokens int
	Cost                           float64
}

type Result struct {
	JSON  json.RawMessage
	Mode  Mode // the mode that worked; store it per profile
	Model string
	Usage Usage
}

type Model struct {
	ID, Name    string
	Vision      bool
	VisionKnown bool // false when the endpoint does not report modalities
}

// Client is safe for concurrent use.
type Client struct {
	http       *http.Client
	Timeout    time.Duration // per HTTP attempt, default 120s
	MaxRetries int           // extra attempts for 429/5xx, default 3
	sleep      func(context.Context, time.Duration) error
	jitter     func(time.Duration) time.Duration
}

func New(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{
		http: httpClient, Timeout: 120 * time.Second, MaxRetries: 3,
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-t.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		jitter: func(d time.Duration) time.Duration { return time.Duration(rand.Int64N(int64(d)/2 + 1)) },
	}
}

// Complete sends the request and returns validated JSON. With ModeAuto it
// falls back strict -> json_object -> prompt when the endpoint rejects the
// structured-output parameter.
func (c *Client) Complete(ctx context.Context, cfg Config, req Request, validate func(json.RawMessage) error) (Result, error) {
	ladder := []Mode{cfg.Mode}
	switch cfg.Mode {
	case "", ModeAuto:
		ladder = []Mode{ModeStrict, ModeJSONObject, ModePrompt}
	}
	var err error
	for i, mode := range ladder {
		var res Result
		res, err = c.completeMode(ctx, cfg, req, mode, validate)
		if err == nil {
			return res, nil
		}
		var ae *Error
		if !errors.As(err, &ae) || ae.Kind != KindStructuredUnsupport || i == len(ladder)-1 {
			break
		}
	}
	return Result{}, err
}

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func (c *Client) completeMode(ctx context.Context, cfg Config, req Request, mode Mode, validate func(json.RawMessage) error) (Result, error) {
	msgs := buildMessages(req, mode)
	res := Result{Mode: mode}
	var lastProblem string
	for attempt := 0; attempt < 2; attempt++ {
		body := map[string]any{"model": cfg.Model, "messages": msgs}
		if req.MaxTokens > 0 {
			body["max_tokens"] = req.MaxTokens
		}
		switch mode {
		case ModeStrict:
			name := req.SchemaName
			if name == "" {
				name = "answer"
			}
			body["response_format"] = map[string]any{"type": "json_schema",
				"json_schema": map[string]any{"name": name, "strict": true, "schema": req.Schema}}
		case ModeJSONObject:
			body["response_format"] = map[string]any{"type": "json_object"}
		}
		if isOpenRouter(cfg.BaseURL) {
			body["provider"] = map[string]any{"require_parameters": true}
		}
		raw, err := c.do(ctx, cfg, http.MethodPost, "/chat/completions", body)
		if err != nil {
			return Result{}, err
		}
		var out struct {
			Model   string `json:"model"`
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				Prompt     int     `json:"prompt_tokens"`
				Completion int     `json:"completion_tokens"`
				Cost       float64 `json:"cost"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
			return Result{}, &Error{Kind: KindOther, Message: "The endpoint answered, but not in the OpenAI chat completions format."}
		}
		res.Model = out.Model
		if res.Model == "" {
			res.Model = cfg.Model
		}
		res.Usage.PromptTokens += out.Usage.Prompt
		res.Usage.CompletionTokens += out.Usage.Completion
		res.Usage.Cost += out.Usage.Cost

		content := out.Choices[0].Message.Content
		js, err := extractJSON(content)
		if err == nil && validate != nil {
			err = validate(js)
		}
		if err == nil {
			res.JSON = js
			return res, nil
		}
		lastProblem = scrub(err.Error(), cfg.Token)
		msgs = append(msgs, message{"assistant", content},
			message{"user", "Your answer was not accepted: " + lastProblem + "\nReply again with only the corrected JSON."})
	}
	return Result{}, &Error{Kind: KindInvalidOutput, Message: "The model's answer did not match the expected format, even after one retry: " + lastProblem}
}

func buildMessages(req Request, mode Mode) []message {
	sys := req.System
	switch mode {
	case ModeJSONObject:
		sys += "\n\nAnswer with a single JSON object that matches this JSON Schema:\n" + string(req.Schema)
	case ModePrompt:
		sys += "\n\nAnswer with JSON only, no other text, no code fences. The JSON must match this JSON Schema:\n" + string(req.Schema)
	}
	var user any = req.User
	if len(req.Images) > 0 {
		parts := []map[string]any{{"type": "text", "text": req.User}}
		for _, img := range req.Images {
			parts = append(parts, map[string]any{"type": "image_url",
				"image_url": map[string]string{"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(img)}})
		}
		user = parts
	}
	var msgs []message
	if strings.TrimSpace(sys) != "" {
		msgs = append(msgs, message{"system", sys})
	}
	return append(msgs, message{"user", user})
}

// extractJSON returns the JSON value in s, tolerating code fences and
// surrounding prose.
func extractJSON(s string) (json.RawMessage, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:] // drop the language tag
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			rest = rest[:j]
		}
		s = strings.TrimSpace(rest)
	}
	if !json.Valid([]byte(s)) {
		start := strings.IndexAny(s, "{[")
		end := strings.LastIndexAny(s, "}]")
		if start < 0 || end <= start {
			return nil, errors.New("the answer contains no JSON")
		}
		s = s[start : end+1]
	}
	var v json.RawMessage
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("the answer is not valid JSON: %v", err)
	}
	return v, nil
}

// Models lists the endpoint's models.
func (c *Client) Models(ctx context.Context, cfg Config) ([]Model, error) {
	raw, err := c.do(ctx, cfg, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Architecture struct {
				InputModalities []string `json:"input_modalities"`
			} `json:"architecture"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &Error{Kind: KindOther, Message: "The endpoint's model list is not in the expected format."}
	}
	models := make([]Model, 0, len(out.Data))
	for _, d := range out.Data {
		m := Model{ID: d.ID, Name: d.Name}
		if m.Name == "" {
			m.Name = d.ID
		}
		if mods := d.Architecture.InputModalities; len(mods) > 0 {
			m.VisionKnown = true
			for _, x := range mods {
				if x == "image" {
					m.Vision = true
				}
			}
		}
		models = append(models, m)
	}
	return models, nil
}

func isOpenRouter(base string) bool {
	u, err := url.Parse(base)
	return err == nil && strings.EqualFold(u.Hostname(), "openrouter.ai")
}

// do performs one logical HTTP call with retries for 429 and 5xx and returns
// the response body of the first 2xx answer.
func (c *Client) do(ctx context.Context, cfg Config, method, path string, body any) ([]byte, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, &Error{Kind: KindOther, Message: "Could not encode the request."}
		}
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + path
	for attempt := 0; ; attempt++ {
		status, hdr, data, err := c.once(ctx, cfg, method, endpoint, payload)
		if err != nil {
			return nil, err
		}
		if status >= 200 && status < 300 {
			return data, nil
		}
		cerr := classify(status, string(data), cfg.Token)
		if (status != 429 && status < 500) || attempt >= c.MaxRetries || cerr.Kind == KindNoCredit {
			return nil, cerr
		}
		wait := backoff(attempt)
		wait += c.jitter(wait)
		if ra, ok := retryAfter(hdr.Get("Retry-After")); ok {
			wait = ra
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func (c *Client) once(ctx context.Context, cfg Config, method, endpoint string, payload []byte) (int, http.Header, []byte, error) {
	rctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	hreq, err := http.NewRequestWithContext(rctx, method, endpoint, rd)
	if err != nil {
		return 0, nil, nil, &Error{Kind: KindOther, Message: "The base URL is not valid."}
	}
	hreq.Header.Set("Authorization", "Bearer "+cfg.Token)
	if payload != nil {
		hreq.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(hreq)
	var data []byte
	if err == nil {
		defer resp.Body.Close()
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxBody))
	}
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		if be := asBlocked(err); be != nil {
			return 0, nil, nil, be
		}
		if rctx.Err() != nil {
			return 0, nil, nil, &Error{Kind: KindTimeout, Message: "The endpoint took too long to answer."}
		}
		return 0, nil, nil, &Error{Kind: KindUnreachable, Message: "Could not reach the endpoint. Check the base URL and your network."}
	}
	return resp.StatusCode, resp.Header, data, nil
}

func backoff(attempt int) time.Duration {
	return 500 * time.Millisecond << min(attempt, 6)
}

func retryAfter(v string) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	var d time.Duration
	if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 {
		d = time.Duration(n * float64(time.Second))
	} else if t, err := http.ParseTime(v); err == nil {
		d = max(time.Until(t), 0)
	} else {
		return 0, false
	}
	return min(d, 60*time.Second), true
}
