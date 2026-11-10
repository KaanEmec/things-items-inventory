package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
	"github.com/kaanemec/things-items-inventory/internal/photos"
)

// linkPhotos joins inventory and photos, which do not import each other.
func linkPhotos(d *db.DB, ph *photos.Service) {
	inventory.PhotoLoader = func(ctx context.Context, q sqlc.DBTX, hid string, ids []string, mainOnly bool) (map[string][]inventory.ItemPhoto, error) {
		links, err := photos.ItemLinks(ctx, q, hid, ids, mainOnly)
		if err != nil {
			return nil, err
		}
		out := make(map[string][]inventory.ItemPhoto, len(links))
		for id, ls := range links {
			for _, l := range ls {
				out[id] = append(out[id], inventory.ItemPhoto{LinkID: l.LinkID, PhotoID: l.PhotoID, SHA256: l.SHA256, Role: l.Role, Sort: l.Sort})
			}
		}
		return out, nil
	}
	inventory.PlacePhotoLoader = func(ctx context.Context, q sqlc.DBTX, hid string, ids []string) (map[string]inventory.ItemPhoto, error) {
		links, err := photos.PlaceMainLinks(ctx, q, hid, ids)
		if err != nil {
			return nil, err
		}
		out := make(map[string]inventory.ItemPhoto, len(links))
		for id, l := range links {
			out[id] = inventory.ItemPhoto{LinkID: l.LinkID, PhotoID: l.PhotoID, SHA256: l.SHA256, Role: l.Role, Sort: l.Sort}
		}
		return out, nil
	}
	inventory.LinkPromotedPhoto = ph.LinkItemPhoto

	// POST /photos for an item that does not exist yet is a capture: the
	// draft item is created first, by the same code as POST /items, so a
	// retry with the same client id finds the same item.
	ph.SetTargetHook(func(ctx context.Context, hid, uid string, t photos.Target, role string) (bool, error) {
		if t.ItemID == "" {
			_, err := inventory.GetPlace(ctx, d.Read, hid, t.PlaceID)
			return err == nil, nil // a missing place is a 404; real failures show up on the write
		}
		var one int
		if err := d.Read.QueryRowContext(ctx, `SELECT 1 FROM items WHERE id = ? AND household_id <> ?`, t.ItemID, hid).Scan(&one); err == nil {
			return false, nil // another household's item looks absent
		}
		f := inventory.Fields{"draft": []byte("true")}
		if t.PlaceID != "" {
			f["place_id"], _ = json.Marshal(t.PlaceID)
		}
		err := inventory.InTx(ctx, d, func(tx *sql.Tx) error {
			_, _, err := inventory.CreateItem(ctx, tx, hid, uid, inventory.NewItem{
				ID: t.ItemID, Source: "capture", AIState: "queued", Fields: f})
			return err
		})
		return err == nil, err
	})
}

type jobJSON struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	State      string     `json:"state"`
	Attempts   int        `json:"attempts"`
	LastError  *string    `json:"last_error"`
	Result     any        `json:"result"`
	ItemID     *string    `json:"item_id"`
	PlaceID    *string    `json:"place_id"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

func orNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// jobsFeature serves GET /api/v1/jobs/{id}, the polling fallback for job state.
func jobsFeature(q *jobs.Queue) Feature {
	return Feature{
		Name: "jobs",
		Routes: func(mux *httpapi.Mux) {
			mux.HandleFunc("GET /api/v1/jobs/{id}", auth.RequireSession(auth.ActionInventoryRead, func(w http.ResponseWriter, r *http.Request) {
				j, err := q.Get(r.Context(), auth.HouseholdID(r.Context()), r.PathValue("id"))
				if err == jobs.ErrNotFound {
					httpapi.WriteError(w, httpapi.NotFound("That job does not exist."))
					return
				}
				if err != nil {
					httpapi.WriteError(w, err)
					return
				}
				var result any
				if len(j.Result) > 0 {
					result = j.Result
				}
				httpapi.WriteJSON(w, http.StatusOK, jobJSON{ID: j.ID, Kind: j.Kind, State: j.State, Attempts: j.Attempts,
					LastError: orNil(j.LastError), Result: result, ItemID: orNil(j.ItemID), PlaceID: orNil(j.PlaceID),
					CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt})
			}))
		},
		Seed: func(ctx context.Context, _ *db.DB, hid string) (SeedResult, error) {
			id, err := q.Enqueue(ctx, nil, jobs.Job{HouseholdID: hid, Kind: "seed_test"})
			return SeedResult{PathValues: []string{id}}, err
		},
	}
}
