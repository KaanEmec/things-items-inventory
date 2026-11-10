// Package drafts turns the photos of one thing into a filled-in draft item
// (J6.3): the `draft_item` background job, the capture hook that queues it
// and the two routes that (re)start it.
//
// An item's ai_state moves queued (UI: waiting) -> reading -> ready, or to
// failed; NULL means no AI was involved (not set up). ai_draft holds
// {confidence, needs, prompt_version, model, error, run_at} (spec §6).
package drafts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/ai/prompt"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
)

// Kind is the job kind.
const Kind = "draft_item"

// ai_state values this package sets. "waiting" in the UI is StateQueued; a
// draft AI never looked at has a NULL state.
const (
	StateQueued  = "queued"
	StateReading = "reading"
	StateReady   = "ready"
	StateFailed  = "failed"
)

// Limits and delays.
const (
	// MaxPhotos is the most photos sent in one request.
	MaxPhotos = 4
	// CaptureDelay lets label and receipt photos of the same item ride along.
	CaptureDelay = 4 * time.Second
	// PausedWait is how long a job waits when the household paused AI.
	PausedWait = 2 * time.Minute
	// MaxAttempts is the job's total number of tries.
	MaxAttempts = 4

	// CouldntRead is shown to the person when the AI gave nothing usable.
	CouldntRead = "The AI couldn't read this"

	maxHint = 300
)

// PhotoSource gives the AI-size image of a photo (*photos.Service).
type PhotoSource interface {
	AIBytes(ctx context.Context, householdID, photoID string) ([]byte, error)
}

// Service owns the job handler and the routes.
type Service struct {
	d      *db.DB
	ai     *ai.Service
	photos PhotoSource
	q      *jobs.Queue
	// Delay is the run_after of a capture job (default CaptureDelay).
	Delay time.Duration
}

// New creates the service and registers the draft_item handler on q.
func New(d *db.DB, a *ai.Service, p PhotoSource, q *jobs.Queue) *Service {
	s := &Service{d: d, ai: a, photos: p, q: q, Delay: CaptureDelay}
	q.Register(Kind, jobs.Handler{Timeout: 4 * time.Minute, MaxAttempts: MaxAttempts, Run: s.run})
	s.registerReaders()
	return s
}

// Payload is the job payload.
type Payload struct {
	ItemID string `json:"item_id"`
	// Hint is a short note from the person ("it's the 2019 model"), passed
	// to the prompt as data.
	Hint string `json:"hint,omitempty"`
}

// Result is the job result.
type Result struct {
	PromptVersion string   `json:"prompt_version,omitempty"`
	Model         string   `json:"model,omitempty"`
	Photos        int      `json:"photos"`
	Applied       []string `json:"applied,omitempty"` // fields the AI filled
	Skipped       string   `json:"skipped,omitempty"` // "gone", "no_photos", "not_set_up"
}

// Draft is the item's ai_draft.
type Draft struct {
	Confidence    map[string]string `json:"confidence"`
	Needs         []prompt.Need     `json:"needs"`
	PromptVersion string            `json:"prompt_version"`
	Model         string            `json:"model"`
	Error         *string           `json:"error"`
	RunAt         string            `json:"run_at"`
	// OtherLines are the receipt lines that are not this item (J6.5).
	OtherLines []prompt.ReceiptLine `json:"receipt_lines,omitempty"`
}

func parseDraft(raw []byte) Draft {
	var d Draft
	_ = json.Unmarshal(raw, &d)
	if d.Confidence == nil {
		d.Confidence = map[string]string{}
	}
	if d.Needs == nil {
		d.Needs = []prompt.Need{}
	}
	return d
}

// OnPhoto is the capture hook: an item/label/receipt photo arrived for
// itemID in tx. For a draft it queues a draft_item run (unless one is already
// queued) and marks the item as waiting. With no AI set up it does nothing,
// so the item stays a photo-only draft.
func (s *Service) OnPhoto(ctx context.Context, tx *sql.Tx, householdID, userID, itemID, role string) error {
	if role != "item" && role != "label" && role != "receipt" {
		return nil
	}
	var draft int
	err := tx.QueryRowContext(ctx, `SELECT draft FROM items WHERE household_id = ? AND id = ? AND deleted_at IS NULL`,
		householdID, itemID).Scan(&draft)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && draft == 0) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := s.ai.For(ctx, sqlc.New(tx), householdID, ai.RoleVision); errors.Is(err, ai.ErrNotSetUp) {
		return nil
	}
	if _, _, err = s.queue(ctx, tx, householdID, userID, itemID, "", s.Delay); err != nil {
		return err
	}
	switch role { // a label or receipt photo also gets its own reader
	case "label":
		_, _, err = s.queueReader(ctx, tx, householdID, userID, KindLabel, itemID, s.Delay)
	case "receipt":
		_, _, err = s.queueReader(ctx, tx, householdID, userID, KindReceipt, itemID, s.Delay)
	}
	return err
}

// queue adds a draft_item job for the item in tx, or reports the one already
// queued (created false). A run with a hint is always added: the hint would
// be lost otherwise.
func (s *Service) queue(ctx context.Context, tx *sql.Tx, householdID, userID, itemID, hint string, delay time.Duration) (jobID string, created bool, err error) {
	if hint == "" {
		err = tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE household_id = ? AND kind = ? AND item_id = ? AND state = 'queued' LIMIT 1`,
			householdID, Kind, itemID).Scan(&jobID)
		if err == nil {
			return jobID, false, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", false, err
		}
	}
	payload, _ := json.Marshal(Payload{ItemID: itemID, Hint: hint})
	j := jobs.Job{HouseholdID: householdID, Kind: Kind, Payload: payload, CreatedBy: userID, ItemID: itemID}
	if delay > 0 {
		j.RunAfter = time.Now().Add(delay)
	}
	if jobID, err = s.q.Enqueue(ctx, tx, j); err != nil {
		return "", false, err
	}
	var state, draftJSON sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT ai_state, ai_draft FROM items WHERE household_id = ? AND id = ?`,
		householdID, itemID).Scan(&state, &draftJSON); err != nil {
		return "", false, err
	}
	if state.String != StateReading { // a run in progress keeps showing "reading"
		var raw []byte
		if draftJSON.Valid {
			raw = []byte(draftJSON.String)
		}
		if err := inventory.SetAIMeta(ctx, tx, householdID, itemID, StateQueued, raw); err != nil {
			return "", false, err
		}
	}
	return jobID, true, nil
}
