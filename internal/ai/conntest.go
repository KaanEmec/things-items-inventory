package ai

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

// labelJPEG is a photo of a printed label: "MODEL KX-4821 / SERIAL 7Q93".
//
//go:embed assets/label.jpg
var labelJPEG []byte

const labelNeedle = "kx4821" // compared with spaces, dashes and case ignored

const testTimeout = 30 * time.Second

var (
	visionTestSchema = json.RawMessage(`{"type":"object","properties":{"label_text":{"type":"string"},"object":{"type":"string"}},"required":["label_text","object"],"additionalProperties":false}`)
	textTestSchema   = json.RawMessage(`{"type":"object","properties":{"brand":{"type":"string"},"model":{"type":"string"},"color":{"type":"string"},"category":{"type":"string"}},"required":["brand","model","color","category"],"additionalProperties":false}`)
)

// testHouseholdKey carries the household into runTest so the test request is
// logged as activity (feature "test") like any other.
type testHouseholdKey struct{}

type step struct {
	ID      string `json:"id"`
	OK      *bool  `json:"ok"` // null: not run because an earlier step failed
	Message string `json:"message"`
	MS      int64  `json:"ms"`
}

type testResult struct {
	OK       bool    `json:"ok"`
	Mode     *string `json:"mode"` // json_schema, json_object or prompt; null when none worked
	Steps    []step  `json:"steps"`
	TestedAt string  `json:"tested_at"`
}

type steps []step

func newSteps(role Role) steps {
	ids := []string{"reach", "token", "model"}
	if role == RoleVision {
		ids = append(ids, "images")
	}
	ids = append(ids, "json")
	out := make(steps, len(ids))
	for i, id := range ids {
		out[i] = step{ID: id, Message: "Not run, because an earlier step failed."}
	}
	return out
}

var okMessages = map[string]string{
	"reach":  "The endpoint answered.",
	"token":  "The token was accepted.",
	"model":  "The model was found.",
	"images": "The model read the label in the test photo.",
	"json":   "The model answered in valid JSON.",
}

// pass marks step id ok (with its default message unless one is given).
func (ss steps) pass(id, msg string, ms int64) {
	for i := range ss {
		if ss[i].ID == id {
			t := true
			ss[i].OK = &t
			ss[i].Message = cmpOr(msg, okMessages[id])
			ss[i].MS = ms
		}
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// fail marks step id failed, every earlier step ok, every later one not run.
func (ss steps) fail(id, msg string, ms int64) {
	i := slices.IndexFunc(ss, func(s step) bool { return s.ID == id })
	for j := 0; j < i; j++ {
		if ss[j].OK == nil {
			ss.pass(ss[j].ID, "", 0)
		}
	}
	f := false
	ss[i].OK, ss[i].Message, ss[i].MS = &f, msg, ms
	for j := i + 1; j < len(ss); j++ {
		ss[j].OK = nil
	}
}

func (ss steps) allOK() bool {
	for _, s := range ss {
		if s.OK == nil || !*s.OK {
			return false
		}
	}
	return true
}

type probeBody struct {
	BaseURL *string `json:"base_url"`
	Token   *string `json:"token"`
	Model   *string `json:"model"`
}

// probeConfig merges the unsaved values in the request over what is saved.
// unsaved is true when they differ from the saved ones.
func (s *Service) probeConfig(w http.ResponseWriter, r *http.Request, role Role) (cfg Config, sv saved, unsaved bool, err error) {
	var body probeBody
	if r.ContentLength != 0 {
		if err = httpapi.DecodeJSON(w, r, &body); err != nil {
			return
		}
	}
	ctx := r.Context()
	if sv, err = s.gather(ctx, sqlc.New(s.d.Write), auth.HouseholdID(ctx), role); err != nil {
		return
	}
	cfg = sv.cfg
	fields := map[string]string{}
	if body.BaseURL != nil {
		if n, e := NormalizeBaseURL(*body.BaseURL); e != nil {
			fields["base_url"] = e.Error()
		} else {
			unsaved = unsaved || n != cfg.BaseURL
			cfg.BaseURL = n
		}
	}
	if body.Token != nil {
		t := strings.TrimSpace(*body.Token)
		if !validToken(t) {
			fields["token"] = "That token is not valid."
		} else {
			unsaved = unsaved || t != cfg.Token
			cfg.Token = t
		}
	}
	if body.Model != nil {
		m := strings.TrimSpace(*body.Model)
		if len(m) > 200 || strings.IndexFunc(m, func(c rune) bool { return c < ' ' || c == 0x7f }) >= 0 {
			fields["model"] = "That model name is not valid."
		} else {
			unsaved = unsaved || m != cfg.Model
			cfg.Model = m
		}
	}
	if len(fields) > 0 {
		err = httpapi.Invalid(fields)
	}
	return
}

func (s *Service) models(w http.ResponseWriter, r *http.Request) {
	role, err := roleParam(r)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	hid := auth.HouseholdID(r.Context())
	if !s.modelsLimit.allow(hid) {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeRateLimited, "Too many requests. Wait a minute and try again."))
		return
	}
	cfg, _, _, err := s.probeConfig(w, r, role)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), testTimeout)
	defer cancel()
	list, err := s.client.Models(ctx, cfg)
	if err != nil {
		httpapi.WriteError(w, httpapi.BadRequest(personMessage(err, cfg.Token)))
		return
	}
	if role == RoleVision { // image-capable first, then unknown, then text-only
		rank := func(m Model) int {
			switch {
			case m.Vision:
				return 0
			case !m.VisionKnown:
				return 1
			}
			return 2
		}
		slices.SortStableFunc(list, func(a, b Model) int { return rank(a) - rank(b) })
	}
	type modelView struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Vision      bool   `json:"vision"`
		VisionKnown bool   `json:"vision_known"`
	}
	out := make([]modelView, len(list))
	for i, m := range list {
		out[i] = modelView{m.ID, m.Name, m.Vision, m.VisionKnown}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"models": out})
}

// personMessage turns a client error into text for the settings screen.
func personMessage(err error, token string) string {
	var ae *Error
	if errors.As(err, &ae) {
		return scrub(ae.Message, token)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "The endpoint took too long to answer."
	}
	return "The request could not be completed."
}

func (s *Service) test(w http.ResponseWriter, r *http.Request) {
	role, err := roleParam(r)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	ctx := r.Context()
	hid := auth.HouseholdID(ctx)
	if !s.testLimit.allow(hid) {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeRateLimited, "Too many tests. Wait a minute and try again."))
		return
	}
	cfg, sv, unsaved, err := s.probeConfig(w, r, role)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	tctx, cancel := context.WithTimeout(context.WithValue(ctx, testHouseholdKey{}, hid), testTimeout)
	defer cancel()
	res, mode, failKind := s.runTest(tctx, role, cfg)
	res.TestedAt = time.Now().UTC().Format(stampLayout)
	if mode != ModeAuto && mode != "" {
		m := modeToStored(mode).String
		res.Mode = &m
	}

	// Only the saved settings of this very role are recorded; a test of
	// unsaved values, or of server defaults, stores nothing.
	if !unsaved && sv.hasRow && sv.effRole == role {
		raw, _ := json.Marshal(res)
		out := TestOutcome{Result: raw, At: time.Now()}
		if res.OK {
			out.Mode = mode
			out.Capabilities = Capabilities{Images: role == RoleVision, JSONSchema: mode == ModeStrict}
		}
		q := sqlc.New(s.d.Write)
		if err := SaveTest(ctx, q, hid, role, out); err != nil {
			httpapi.WriteError(w, err)
			return
		}
		if failKind == KindAuth || failKind == KindModelNotFound {
			if err := MarkFailing(ctx, q, hid, role, time.Now()); err != nil {
				httpapi.WriteError(w, err)
				return
			}
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, res)
}

func ms(t time.Time) int64 { return time.Since(t).Milliseconds() }

// runTest runs the steps against cfg. It returns the mode that worked (when
// the test passed) and the kind of the client error that stopped it.
func (s *Service) runTest(ctx context.Context, role Role, cfg Config) (testResult, Mode, Kind) {
	ss := newSteps(role)
	done := func(ok bool, mode Mode, kind Kind) (testResult, Mode, Kind) {
		return testResult{OK: ok, Steps: ss}, mode, kind
	}
	kindOf := func(err error) (Kind, string) {
		var ae *Error
		if errors.As(err, &ae) {
			return ae.Kind, personMessage(err, cfg.Token)
		}
		return KindTimeout, personMessage(err, cfg.Token)
	}
	tokenDecided := false

	// 1-2: reach the endpoint and check the token, by listing models.
	t0 := time.Now()
	_, merr := s.client.Models(ctx, cfg)
	if merr == nil {
		ss.pass("reach", "", ms(t0))
		ss.pass("token", "", 0)
		tokenDecided = true
	} else {
		kind, msg := kindOf(merr)
		switch kind {
		case KindUnreachable, KindTimeout:
			ss.fail("reach", msg+" Check the base URL and that the server is running.", ms(t0))
			return done(false, "", kind)
		case KindAuth:
			ss.pass("reach", "", ms(t0))
			ss.fail("token", "The endpoint rejected the API token. Check that it is correct and has access to this model, then test again.", 0)
			return done(false, "", kind)
		default: // the endpoint answered but has no usable model list; the real request decides
			ss.pass("reach", "The endpoint answered, but it does not list its models.", ms(t0))
		}
	}
	if cfg.Model == "" {
		ss.fail("model", "Choose a model first.", 0)
		if !tokenDecided { // the token cannot be judged without a real request
			for i := range ss {
				if ss[i].ID == "token" {
					ss[i].OK, ss[i].Message = nil, "Not checked yet. Choose a model, then test again."
				}
			}
		}
		return done(false, "", "")
	}

	// 3-5: one real request that needs image input (vision) and structured output.
	req := Request{System: "You fill in inventory records. Answer with JSON only.", MaxTokens: 300}
	sawJSON := false
	var validate func(json.RawMessage) error
	if role == RoleVision {
		req.User = "This is a photo of a printed label. Copy the text printed on it into label_text, and say in object what kind of thing the photo shows."
		req.Images = [][]byte{labelJPEG}
		req.SchemaName, req.Schema = "label_reading", visionTestSchema
		validate = func(raw json.RawMessage) error {
			sawJSON = true
			var v struct {
				LabelText string `json:"label_text"`
			}
			if err := json.Unmarshal(raw, &v); err != nil {
				return errors.New("the answer does not follow the schema")
			}
			norm := strings.NewReplacer(" ", "", "-", "", "_", "").Replace(strings.ToLower(v.LabelText))
			if !strings.Contains(norm, labelNeedle) {
				return errors.New("label_text does not contain the text printed on the photo; read the photo again")
			}
			return nil
		}
	} else {
		req.User = "sony wh1000xm4 black"
		req.SchemaName, req.Schema = "item_completion", textTestSchema
		validate = func(raw json.RawMessage) error {
			var v map[string]any
			if err := json.Unmarshal(raw, &v); err != nil || len(v) == 0 {
				return errors.New("the answer must be a JSON object that follows the schema")
			}
			return nil
		}
	}
	cfg.Mode = ModeAuto
	t1 := time.Now()
	res, err := s.client.Complete(ctx, cfg, req, validate)
	if hid, ok := ctx.Value(testHouseholdKey{}).(string); ok {
		s.record(context.WithoutCancel(ctx), hid, role, FeatureTest, cfg, res, err, t1, time.Since(t1), Ref{})
	}
	if err == nil {
		if !tokenDecided {
			ss.pass("token", "", 0)
		}
		ss.pass("model", "", 0)
		if role == RoleVision {
			ss.pass("images", "", 0)
		}
		msg := "The model answered in valid JSON that follows the schema (strict mode)."
		switch res.Mode {
		case ModeJSONObject:
			msg = "The model answered in valid JSON (JSON mode). The app checks answers against the schema itself."
		case ModePrompt:
			msg = "The model has no strict JSON mode, so the app checks every answer itself. This works, but a retry may be needed now and then."
		}
		ss.pass("json", msg, ms(t1))
		return done(ss.allOK(), res.Mode, "")
	}
	kind, msg := kindOf(err)
	took := ms(t1)
	if !tokenDecided && kind != KindAuth && kind != KindUnreachable && kind != KindTimeout {
		ss.pass("token", "", 0)
	}
	switch kind {
	case KindAuth:
		ss.fail("token", "The endpoint rejected the API token. Check that it is correct and has access to this model, then test again.", took)
	case KindModelNotFound:
		ss.fail("model", "The endpoint does not know this model. Check the name, or pick one from the list.", took)
	case KindNoImageInput:
		ss.fail("images", "This model can't read images. Pick a model that accepts image input for photo understanding.", took)
	case KindStructuredUnsupport:
		ss.fail("json", "This model or provider does not support structured JSON answers, not even in prompt-only mode. Pick another model.", took)
	case KindInvalidOutput:
		if role == RoleVision && sawJSON {
			ss.fail("images", "The model answered, but did not read the label in the test photo. Pick a model with better image understanding for photo understanding.", took)
		} else {
			ss.fail("json", "The model's answers were not valid JSON, even after a retry. Pick a stronger model.", took)
		}
	case KindUnreachable:
		ss.fail("reach", msg+" Check the base URL and that the server is running.", took)
	case KindNoCredit:
		ss.fail("model", msg, took)
	case KindRateLimited:
		ss.fail("model", "The endpoint is rate limiting requests, so the model could not be checked. Wait a moment and test again.", took)
	case KindTimeout:
		ss.fail("model", "The model took too long to answer. Try again, or pick a faster model.", took)
	default:
		ss.fail("model", msg, took)
	}
	return done(false, "", kind)
}
