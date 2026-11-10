package drafts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
)

// Routes registers POST /items/{id}/ai and POST /items/ai/read-waiting.
func (s *Service) Routes(mux *httpapi.Mux) {
	mux.HandleFunc("POST /api/v1/items/{id}/ai", auth.RequireSession(auth.ActionInventoryWrite, s.readOne))
	mux.HandleFunc("POST /api/v1/items/ai/read-waiting", auth.RequireSession(auth.ActionInventoryWrite, s.readWaiting))
}

// aiReady is nil when a run can be queued, else a 409 whose fields say why:
// {"ai": "not_set_up"} or {"ai": "paused"}.
func (s *Service) aiReady(ctx context.Context, hid string) error {
	if _, err := s.ai.For(ctx, sqlc.New(s.d.Read), hid, ai.RoleVision); errors.Is(err, ai.ErrNotSetUp) {
		e := httpapi.Conflict("AI is not set up. Add a photo-understanding model in Settings first.")
		e.Fields = map[string]string{"ai": "not_set_up"}
		return e
	} else if err != nil {
		return err
	}
	paused, err := s.ai.IsPaused(ctx, hid)
	if err != nil {
		return err
	}
	if paused {
		e := httpapi.Conflict("AI is paused for this household. Resume it in Settings first.")
		e.Fields = map[string]string{"ai": "paused"}
		return e
	}
	return nil
}

type queuedJSON struct {
	JobID  string `json:"job_id"`
	Queued bool   `json:"queued"` // false: a run was already waiting
}

// readOne queues a run for one draft. Body (optional): {"hint": "..."}.
func (s *Service) readOne(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hid, uid := auth.HouseholdID(ctx), auth.UserID(ctx)
	item, err := inventory.GetItem(ctx, s.d.Read, hid, r.PathValue("id"))
	if err == nil && item.DeletedAt != nil {
		err = httpapi.NotFound("That item does not exist.")
	}
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	var body struct {
		Hint string `json:"hint"`
	}
	if err := decodeOptional(w, r, &body); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	body.Hint = strings.TrimSpace(body.Hint)
	if len([]rune(body.Hint)) > maxHint {
		httpapi.WriteError(w, httpapi.Invalid(map[string]string{"hint": "Keep the hint under 300 characters."}))
		return
	}
	if !item.Draft {
		e := httpapi.Conflict("This item is already saved. Only drafts are read by the AI.")
		e.Fields = map[string]string{"item": "not_a_draft"}
		httpapi.WriteError(w, e)
		return
	}
	if err := s.aiReady(ctx, hid); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	var out queuedJSON
	err = inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
		var err error
		out.JobID, out.Queued, err = s.queue(ctx, tx, hid, uid, item.ID, body.Hint, 0)
		return err
	})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusAccepted, out)
}

// readWaiting queues a run for every photo-only draft that was never read.
func (s *Service) readWaiting(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hid, uid := auth.HouseholdID(ctx), auth.UserID(ctx)
	if err := s.aiReady(ctx, hid); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	rows, err := s.d.Read.QueryContext(ctx, `SELECT i.id FROM items i
		WHERE i.household_id = ? AND i.draft = 1 AND i.deleted_at IS NULL AND i.ai_draft IS NULL
		  AND (i.ai_state IS NULL OR i.ai_state = 'queued')
		  AND EXISTS (SELECT 1 FROM photo_links l WHERE l.household_id = i.household_id AND l.item_id = i.id
		              AND l.role IN ('item', 'label', 'receipt'))
		  AND NOT EXISTS (SELECT 1 FROM jobs j WHERE j.household_id = i.household_id AND j.kind = ? AND j.item_id = i.id
		                  AND j.state IN ('queued', 'running'))
		ORDER BY i.created_at LIMIT 200`, hid, Kind)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			httpapi.WriteError(w, err)
			return
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	err = inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, _, err := s.queue(ctx, tx, hid, uid, id, "", 0); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusAccepted, map[string]any{"queued": len(ids)})
}

// decodeOptional reads a JSON body that may be absent.
func decodeOptional(w http.ResponseWriter, r *http.Request, dst any) error {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		return httpapi.BadRequest("The request body could not be read.")
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return httpapi.BadRequest("The request body is not valid JSON.")
	}
	return nil
}

// Seed creates a photo-only draft in the household for the cross-household
// test.
func (s *Service) Seed(ctx context.Context, d *db.DB, hid string) (pathValues, markers []string, err error) {
	err = inventory.InTx(ctx, d, func(tx *sql.Tx) error {
		item, _, err := inventory.CreateItem(ctx, tx, hid, "", inventory.NewItem{Source: "capture",
			Fields: inventory.Fields{"draft": []byte("true"), "name": []byte(`"Secret-Draft-8R"`)}})
		if err != nil {
			return err
		}
		pathValues, markers = []string{item.ID}, []string{"Secret-Draft-8R"}
		return nil
	})
	return pathValues, markers, err
}
