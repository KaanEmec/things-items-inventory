package ai

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

// Feature names what an AI request was for (the ai_activity.feature values).
type Feature string

const (
	FeatureItemDraft     Feature = "item_draft"
	FeatureLabel         Feature = "label"
	FeatureReceipt       Feature = "receipt"
	FeatureContents      Feature = "contents"
	FeatureRowCompletion Feature = "row_completion"
	FeatureTextToItems   Feature = "text_to_items"
	FeatureCSVMapping    Feature = "csv_mapping"
	FeatureTest          Feature = "test"
)

// ErrPaused is returned by Run when the household has paused AI. Background
// jobs should wait instead of failing: see IsPaused.
var ErrPaused = errors.New("ai: paused for this household")

// Ref ties an activity row to the job or item the request was for. Both are optional.
type Ref struct{ JobID, ItemID string }

const maxActivityError = 500

// Run is the one entry point every AI feature calls instead of the client.
//
// It refuses with ErrPaused when the household paused AI and with
// *NotSetUpError (errors.Is(err, ErrNotSetUp)) when the role has no endpoint;
// neither sends a request or writes a row. Otherwise it calls the endpoint,
// records an ai_activity row (also on failure), updates the profile's failing
// marker and remembers the structured-output mode auto mode found. The row
// holds no prompt, photo or token.
func (s *Service) Run(ctx context.Context, householdID string, role Role, feature Feature, req Request,
	validate func(json.RawMessage) error, ref Ref) (Result, error) {
	paused, err := s.IsPaused(ctx, householdID)
	if err != nil {
		return Result{}, err
	}
	if paused {
		return Result{}, ErrPaused
	}
	q := sqlc.New(s.d.Write)
	cfg, err := s.For(ctx, q, householdID, role)
	if err != nil {
		return Result{}, err
	}
	start := time.Now()
	res, callErr := s.client.Complete(ctx, cfg, req, validate)
	took := time.Since(start)

	// The caller may have given up; the record and the bookkeeping still happen.
	bg := context.WithoutCancel(ctx)
	s.record(bg, householdID, role, feature, cfg, res, callErr, start, took, ref)
	NoteResult(bg, q, householdID, role, callErr)
	if callErr == nil && (cfg.Mode == ModeAuto || cfg.Mode == "") {
		if m := modeToStored(res.Mode); m.Valid {
			if sv, err := s.gather(bg, q, householdID, role); err == nil && sv.hasRow {
				if err := q.SetAIOutputModeIfUnset(bg, sqlc.SetAIOutputModeIfUnsetParams{
					OutputMode: m, HouseholdID: householdID, Role: string(sv.effRole)}); err != nil {
					slog.Warn("ai: store output mode", "err", err)
				}
			}
		}
	}
	return res, callErr
}

// IsPaused reports whether the household paused AI. A job handler that gets
// true (or ErrPaused from Run) should ask to run again later rather than fail.
func (s *Service) IsPaused(ctx context.Context, householdID string) (bool, error) {
	n, err := sqlc.New(s.d.Read).IsAIPaused(ctx, householdID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return n != 0, err
}

// record writes one activity row; its own failure is only logged.
func (s *Service) record(ctx context.Context, householdID string, role Role, feature Feature, cfg Config,
	res Result, callErr error, start time.Time, took time.Duration, ref Ref) {
	row := sqlc.InsertAIActivityParams{
		ID: id.New(), HouseholdID: householdID, At: start.UTC().Format(stampLayout),
		Feature: string(feature), Role: string(role), Model: cfg.Model, DurationMs: took.Milliseconds(),
		JobID:  sql.NullString{String: ref.JobID, Valid: ref.JobID != ""},
		ItemID: sql.NullString{String: ref.ItemID, Valid: ref.ItemID != ""},
	}
	if callErr == nil {
		row.Ok = 1
		if res.Model != "" {
			row.Model = res.Model
		}
		row.TokensIn = sql.NullInt64{Int64: int64(res.Usage.PromptTokens), Valid: true}
		row.TokensOut = sql.NullInt64{Int64: int64(res.Usage.CompletionTokens), Valid: true}
		row.Cost = sql.NullFloat64{Float64: res.Usage.Cost, Valid: res.Usage.Cost > 0}
	} else {
		code := string(KindOther)
		var ae *Error
		switch {
		case errors.As(callErr, &ae):
			code = string(ae.Kind)
		case errors.Is(callErr, context.DeadlineExceeded):
			code = string(KindTimeout)
		case errors.Is(callErr, context.Canceled):
			code = "canceled"
		}
		msg := personMessage(callErr, cfg.Token)
		if len(msg) > maxActivityError {
			msg = msg[:maxActivityError] + "..."
		}
		row.ErrorCode = sql.NullString{String: code, Valid: true}
		row.Error = sql.NullString{String: msg, Valid: true}
	}
	if err := sqlc.New(s.d.Write).InsertAIActivity(ctx, row); err != nil {
		slog.Warn("ai: record activity", "err", err)
	}
}

// activityView is one row as the API shows it.
type activityView struct {
	ID         string   `json:"id"`
	At         string   `json:"at"`
	Feature    string   `json:"feature"`
	Role       string   `json:"role"`
	Model      string   `json:"model"`
	TokensIn   *int64   `json:"tokens_in"`
	TokensOut  *int64   `json:"tokens_out"`
	Cost       *float64 `json:"cost"`
	DurationMs int64    `json:"duration_ms"`
	OK         bool     `json:"ok"`
	ErrorCode  *string  `json:"error_code"`
	Error      *string  `json:"error"`
	JobID      *string  `json:"job_id"`
	ItemID     *string  `json:"item_id"`
}

func viewActivity(r sqlc.AiActivity) activityView {
	v := activityView{ID: r.ID, At: r.At, Feature: r.Feature, Role: r.Role, Model: r.Model,
		DurationMs: r.DurationMs, OK: r.Ok != 0,
		ErrorCode: nullStr(r.ErrorCode), Error: nullStr(r.Error), JobID: nullStr(r.JobID), ItemID: nullStr(r.ItemID)}
	if r.TokensIn.Valid {
		v.TokensIn = &r.TokensIn.Int64
	}
	if r.TokensOut.Valid {
		v.TokensOut = &r.TokensOut.Int64
	}
	if r.Cost.Valid {
		v.Cost = &r.Cost.Float64
	}
	return v
}

type activityCursor struct {
	At string `json:"a"`
	ID string `json:"i"`
}

func (s *Service) activity(w http.ResponseWriter, r *http.Request) {
	cursorStr, limit, err := httpapi.ParsePaging(r)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	c := activityCursor{At: "9999-12-31T23:59:59.999Z", ID: "~"}
	if cursorStr != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursorStr)
		if err == nil {
			err = json.Unmarshal(raw, &c)
		}
		if err != nil || c.At == "" || c.ID == "" {
			httpapi.WriteError(w, httpapi.Invalid(map[string]string{"cursor": "That cursor is not valid."}))
			return
		}
	}
	rows, err := sqlc.New(s.d.Read).ListAIActivity(r.Context(), sqlc.ListAIActivityParams{
		HouseholdID: auth.HouseholdID(r.Context()), BeforeAt: c.At, BeforeID: c.ID, MaxRows: int64(limit) + 1})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		b, _ := json.Marshal(activityCursor{At: last.At, ID: last.ID})
		n := base64.RawURLEncoding.EncodeToString(b)
		next = &n
	}
	out := make([]activityView, len(rows))
	for i, row := range rows {
		out[i] = viewActivity(row)
	}
	httpapi.WriteJSON(w, http.StatusOK, struct {
		Data       []activityView `json:"data"`
		NextCursor *string        `json:"next_cursor"`
	}{out, next})
}

type usageView struct {
	Requests  int64   `json:"requests"`
	Failures  int64   `json:"failures"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	Cost      float64 `json:"cost"`
}

// usage totals the current calendar month (UTC) per role.
func (s *Service) usage(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	rows, err := sqlc.New(s.d.Read).AIUsageByRole(r.Context(), sqlc.AIUsageByRoleParams{
		HouseholdID: auth.HouseholdID(r.Context()),
		FromAt:      from.Format(stampLayout), ToAt: from.AddDate(0, 1, 0).Format(stampLayout)})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	roles := map[Role]usageView{RoleVision: {}, RoleText: {}}
	for _, row := range rows {
		roles[Role(row.Role)] = usageView{row.Requests, row.Failures, row.TokensIn, row.TokensOut, row.Cost}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"month": from.Format("2006-01"), "roles": roles})
}
