package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
)

// Routes registers the /settings/ai endpoints.
func (s *Service) Routes(mux *httpapi.Mux) {
	mux.HandleFunc("GET /api/v1/settings/ai", auth.RequireSession(auth.ActionAIRead, s.get))
	mux.HandleFunc("GET /api/v1/settings/ai/activity", auth.RequireSession(auth.ActionAIRead, s.activity))
	mux.HandleFunc("GET /api/v1/settings/ai/usage", auth.RequireSession(auth.ActionAIRead, s.usage))
	mux.HandleFunc("PUT /api/v1/settings/ai/{role}", auth.RequireSession(auth.ActionAISettings, s.put))
	mux.HandleFunc("DELETE /api/v1/settings/ai/{role}", auth.RequireSession(auth.ActionAISettings, s.delete))
	mux.HandleFunc("POST /api/v1/settings/ai/{role}/test", auth.RequireSession(auth.ActionAISettings, s.test))
	mux.HandleFunc("POST /api/v1/settings/ai/{role}/models", auth.RequireSession(auth.ActionAISettings, s.models))
}

// profileView is one role as the API shows it. It never holds the token.
type profileView struct {
	Role         Role            `json:"role"`
	Source       string          `json:"source"` // "household" or "server"
	BaseURL      string          `json:"base_url"`
	Model        string          `json:"model"`
	TokenSet     bool            `json:"token_set"`
	TokenHint    string          `json:"token_hint"`
	SameAsVision *bool           `json:"same_as_vision,omitempty"` // text only
	OutputMode   string          `json:"output_mode,omitempty"`
	Capabilities Capabilities    `json:"capabilities"`
	TestResult   json.RawMessage `json:"test_result"`
	TestedAt     *string         `json:"tested_at"`
	FailingSince *string         `json:"failing_since"`
	UpdatedAt    *string         `json:"updated_at"`
	Paused       *bool           `json:"paused,omitempty"` // household AI pause; set by GET only
}

func nullStr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

func (s *Service) view(role Role, row *sqlc.AiProfile) profileView {
	v := profileView{Role: role, Source: "household", BaseURL: DefaultBaseURL, TestResult: json.RawMessage("null")}
	if role == RoleText {
		t := true
		v.SameAsVision = &t
	}
	if row == nil {
		if e, ok := s.serverDefault(role); ok {
			v.Source, v.BaseURL, v.Model, v.TokenSet = "server", e.BaseURL, e.Model, e.Token != ""
			if role == RoleText {
				f := false
				v.SameAsVision = &f
			}
		}
		return v
	}
	v.BaseURL = row.BaseUrl
	v.Model = row.Model.String
	v.TokenSet = len(row.TokenEnc) > 0
	v.TokenHint = row.TokenHint.String
	if role == RoleText {
		b := row.SameAsVision != 0
		v.SameAsVision = &b
	}
	if row.OutputMode.Valid {
		v.OutputMode = row.OutputMode.String
	}
	_ = json.Unmarshal([]byte(row.Capabilities), &v.Capabilities)
	if row.TestResult.Valid {
		v.TestResult = json.RawMessage(row.TestResult.String)
	}
	v.TestedAt, v.FailingSince = nullStr(row.TestedAt), nullStr(row.FailingSince)
	v.UpdatedAt = &row.UpdatedAt
	return v
}

func (s *Service) get(w http.ResponseWriter, r *http.Request) {
	rows, err := sqlc.New(s.d.Read).ListAIProfiles(r.Context(), auth.HouseholdID(r.Context()))
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	paused, err := s.IsPaused(r.Context(), auth.HouseholdID(r.Context()))
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	byRole := map[Role]*sqlc.AiProfile{}
	for i := range rows {
		byRole[Role(rows[i].Role)] = &rows[i]
	}
	vv, tv := s.view(RoleVision, byRole[RoleVision]), s.view(RoleText, byRole[RoleText])
	vv.Paused, tv.Paused = &paused, &paused
	httpapi.WriteJSON(w, http.StatusOK, map[Role]profileView{RoleVision: vv, RoleText: tv})
}

func roleParam(r *http.Request) (Role, error) {
	role, ok := ParseRole(r.PathValue("role"))
	if !ok {
		return "", httpapi.NotFound("No such AI role.")
	}
	return role, nil
}

type putBody struct {
	BaseURL      *string `json:"base_url"`
	Model        *string `json:"model"`
	Token        *string `json:"token"`
	SameAsVision *bool   `json:"same_as_vision"`
}

const maxTokenLen = 512

func validToken(t string) bool {
	if t == "" || len(t) > maxTokenLen {
		return false
	}
	for _, c := range t {
		if c <= ' ' || c > '~' { // printable ASCII, no spaces
			return false
		}
	}
	return true
}

func (s *Service) put(w http.ResponseWriter, r *http.Request) {
	role, err := roleParam(r)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	var body putBody
	if err := httpapi.DecodeJSON(w, r, &body); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	fields := map[string]string{}
	if body.SameAsVision != nil && role != RoleText {
		fields["same_as_vision"] = "Only the text role can use the photo understanding endpoint."
	}
	var baseURL string
	if body.BaseURL != nil {
		if baseURL, err = NormalizeBaseURL(*body.BaseURL); err != nil {
			fields["base_url"] = err.Error()
		}
	}
	var model string
	if body.Model != nil {
		model = strings.TrimSpace(*body.Model)
		if len(model) > 200 || strings.IndexFunc(model, unicode.IsControl) >= 0 {
			fields["model"] = "That model name is not valid."
		}
	}
	var token string
	if body.Token != nil {
		token = strings.TrimSpace(*body.Token)
		if token == "" {
			fields["token"] = "The token cannot be empty. To remove the saved profile, delete it."
		} else if !validToken(token) {
			fields["token"] = "That token is not valid."
		}
	}
	if len(fields) > 0 {
		httpapi.WriteError(w, httpapi.Invalid(fields))
		return
	}

	ctx := r.Context()
	hid, uid := auth.HouseholdID(ctx), auth.UserID(ctx)
	var saved sqlc.AiProfile
	err = inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		old, err := q.GetAIProfile(ctx, sqlc.GetAIProfileParams{HouseholdID: hid, Role: string(role)})
		exists := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		p := sqlc.UpsertAIProfileParams{
			HouseholdID: hid, Role: string(role), BaseUrl: DefaultBaseURL, SameAsVision: 1,
			UpdatedBy: sql.NullString{String: uid, Valid: uid != ""}, UpdatedAt: time.Now().UTC().Format(stampLayout),
		}
		if exists { // omitted fields keep their saved value
			p.BaseUrl, p.TokenEnc, p.TokenHint = old.BaseUrl, old.TokenEnc, old.TokenHint
			p.Model, p.SameAsVision = old.Model, old.SameAsVision
		}
		if body.BaseURL != nil {
			p.BaseUrl = baseURL
		}
		if body.Model != nil {
			p.Model = sql.NullString{String: model, Valid: model != ""}
		}
		if body.SameAsVision != nil {
			p.SameAsVision = 0
			if *body.SameAsVision {
				p.SameAsVision = 1
			}
		}
		if body.Token != nil {
			if p.TokenEnc, err = s.seal(token, hid, role); err != nil {
				return err
			}
			p.TokenHint = sql.NullString{}
			if len(token) >= 8 {
				p.TokenHint = sql.NullString{String: token[len(token)-4:], Valid: true}
			}
		}
		if saved, err = q.UpsertAIProfile(ctx, p); err != nil {
			return err
		}
		// The event lists what changed, never the token itself.
		before, after := map[string]any{"role": string(role)}, map[string]any{"role": string(role)}
		diff := func(k string, o, n any) {
			if !exists || o != n {
				before[k], after[k] = o, n
			}
		}
		if exists {
			diff("base_url", old.BaseUrl, p.BaseUrl)
			diff("model", old.Model.String, p.Model.String)
			diff("same_as_vision", old.SameAsVision != 0, p.SameAsVision != 0)
		} else {
			before["base_url"], after["base_url"] = nil, p.BaseUrl
			before["model"], after["model"] = nil, p.Model.String
		}
		if body.Token != nil {
			after["token_changed"] = true
		}
		if len(after) > 1 {
			if err := inventory.RecordEvent(ctx, tx, inventory.EventInput{HouseholdID: hid, UserID: uid,
				Kind: "settings_changed", Subject: "ai", Before: before, After: after}); err != nil {
				return err
			}
		}
		// A different endpoint, model or token invalidates the last test.
		if !exists || body.Token != nil || p.BaseUrl != old.BaseUrl || p.Model != old.Model {
			return q.ClearAITest(ctx, sqlc.ClearAITestParams{HouseholdID: hid, Role: string(role)})
		}
		return nil
	})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	// Re-read: the transaction may have cleared the test fields after the upsert.
	if row, err := sqlc.New(s.d.Write).GetAIProfile(ctx, sqlc.GetAIProfileParams{HouseholdID: hid, Role: string(role)}); err == nil {
		saved = row
	}
	httpapi.WriteJSON(w, http.StatusOK, s.view(role, &saved))
}

func (s *Service) delete(w http.ResponseWriter, r *http.Request) {
	role, err := roleParam(r)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	ctx := r.Context()
	hid, uid := auth.HouseholdID(ctx), auth.UserID(ctx)
	var n int64
	err = inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
		var err error
		if n, err = sqlc.New(tx).DeleteAIProfile(ctx, sqlc.DeleteAIProfileParams{HouseholdID: hid, Role: string(role)}); err != nil || n == 0 {
			return err
		}
		return inventory.RecordEvent(ctx, tx, inventory.EventInput{HouseholdID: hid, UserID: uid, Kind: "settings_changed", Subject: "ai",
			Before: map[string]any{"role": string(role)}, After: map[string]any{"role": string(role), "cleared": true}})
	})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if n == 0 {
		httpapi.WriteError(w, httpapi.NotFound("This household has no saved profile for that role."))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, s.view(role, nil))
}

// Seed creates a vision profile with recognisable text for the
// cross-household test and returns that text. It stores no token. The server
// package wraps it into a Feature.Seed.
func (s *Service) Seed(ctx context.Context, d *db.DB, householdID string) (markers []string, err error) {
	_, err = sqlc.New(d.Write).UpsertAIProfile(ctx, sqlc.UpsertAIProfileParams{
		HouseholdID: householdID, Role: string(RoleVision), BaseUrl: "https://b-secret-ai-host.example.com/v1",
		Model: sql.NullString{String: "b-secret-ai-model", Valid: true}, SameAsVision: 1,
		UpdatedAt: time.Now().UTC().Format(stampLayout)})
	if err != nil {
		return nil, err
	}
	err = sqlc.New(d.Write).InsertAIActivity(ctx, sqlc.InsertAIActivityParams{
		ID: id.New(), HouseholdID: householdID, At: time.Now().UTC().Format(stampLayout), Feature: string(FeatureItemDraft),
		Role: string(RoleVision), Model: "b-secret-activity-model", DurationMs: 1, Ok: 0,
		ErrorCode: sql.NullString{String: "other", Valid: true}, Error: sql.NullString{String: "b-secret-activity-error", Valid: true}})
	return []string{"b-secret-ai-host.example.com", "b-secret-ai-model", "b-secret-activity-model", "b-secret-activity-error"}, err
}
