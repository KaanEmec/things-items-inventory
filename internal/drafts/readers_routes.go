package drafts

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
)

func (s *Service) readerRoutes(mux *httpapi.Mux) {
	mux.HandleFunc("POST /api/v1/items/{id}/label/read", auth.RequireSession(auth.ActionInventoryWrite, s.itemReader(KindLabel)))
	mux.HandleFunc("POST /api/v1/items/{id}/receipt/read", auth.RequireSession(auth.ActionInventoryWrite, s.itemReader(KindReceipt)))
	mux.HandleFunc("POST /api/v1/places/{id}/contents/read", auth.RequireSession(auth.ActionInventoryWrite, s.contentsReader))
}

// queueReader adds a label_read or receipt_read job for the item, or reports
// the one already queued.
func (s *Service) queueReader(ctx context.Context, tx *sql.Tx, hid, uid, kind, itemID string, delay time.Duration) (string, bool, error) {
	var jobID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE household_id = ? AND kind = ? AND item_id = ? AND state = 'queued' LIMIT 1`,
		hid, kind, itemID).Scan(&jobID)
	if err == nil {
		return jobID, false, nil
	}
	if err != sql.ErrNoRows {
		return "", false, err
	}
	payload, _ := json.Marshal(Payload{ItemID: itemID})
	j := jobs.Job{HouseholdID: hid, Kind: kind, Payload: payload, CreatedBy: uid, ItemID: itemID}
	if delay > 0 {
		j.RunAfter = time.Now().Add(delay)
	}
	jobID, err = s.q.Enqueue(ctx, tx, j)
	return jobID, err == nil, err
}

// itemReader queues a label or receipt read for an item (a draft or a saved
// one: "Add a label photo", "Add receipt").
func (s *Service) itemReader(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		hid, uid := auth.HouseholdID(ctx), auth.UserID(ctx)
		item, err := inventory.GetItem(ctx, s.d.Read, hid, r.PathValue("id"))
		if err == nil && item.DeletedAt != nil {
			err = httpapi.NotFound("That item does not exist.")
		}
		if err == nil {
			err = s.aiReady(ctx, hid)
		}
		var out queuedJSON
		if err == nil {
			err = inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
				var err error
				out.JobID, out.Queued, err = s.queueReader(ctx, tx, hid, uid, kind, item.ID, 0)
				return err
			})
		}
		if err != nil {
			httpapi.WriteError(w, err)
			return
		}
		httpapi.WriteJSON(w, http.StatusAccepted, out)
	}
}

// contentsReader queues a contents_list job for a container: it reads the
// contents photos no line uses yet.
func (s *Service) contentsReader(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hid, uid := auth.HouseholdID(ctx), auth.UserID(ctx)
	place, err := inventory.GetPlace(ctx, s.d.Read, hid, r.PathValue("id"))
	if err == nil {
		err = s.aiReady(ctx, hid)
	}
	var out queuedJSON
	if err == nil {
		err = inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
			err := tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE household_id = ? AND kind = ? AND place_id = ? AND state = 'queued' LIMIT 1`,
				hid, KindContents, place.ID).Scan(&out.JobID)
			if err == nil {
				return nil
			}
			if err != sql.ErrNoRows {
				return err
			}
			payload, _ := json.Marshal(ContentsPayload{PlaceID: place.ID})
			out.JobID, err = s.q.Enqueue(ctx, tx, jobs.Job{HouseholdID: hid, Kind: KindContents, Payload: payload, CreatedBy: uid, PlaceID: place.ID})
			out.Queued = err == nil
			return err
		})
	}
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusAccepted, out)
}
