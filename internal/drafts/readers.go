package drafts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/ai/prompt"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
	"github.com/kaanemec/things-items-inventory/internal/photos"
)

// Job kinds of the single-photo readers (J6.4 to J6.6).
const (
	KindLabel    = "label_read"
	KindReceipt  = "receipt_read"
	KindContents = "contents_list"
)

// ReadResult is the result of a label_read, receipt_read or contents_list job.
type ReadResult struct {
	PromptVersion string   `json:"prompt_version,omitempty"`
	Model         string   `json:"model,omitempty"`
	Applied       []string `json:"applied,omitempty"` // item fields the AI filled
	Lines         int      `json:"lines,omitempty"`   // contents lines added
	Skipped       string   `json:"skipped,omitempty"` // "gone", "no_photos", "not_set_up"
}

func (s *Service) registerReaders() {
	s.q.Register(KindLabel, jobs.Handler{Timeout: 4 * time.Minute, MaxAttempts: MaxAttempts, Run: s.runLabel})
	s.q.Register(KindReceipt, jobs.Handler{Timeout: 4 * time.Minute, MaxAttempts: MaxAttempts, Run: s.runReceipt})
	s.q.Register(KindContents, jobs.Handler{Timeout: 4 * time.Minute, MaxAttempts: MaxAttempts, Run: s.runContents})
}

func readResult(r ReadResult) (json.RawMessage, error) { return json.Marshal(r) }

// preflight reports why the vision call cannot happen now: skipped is
// "not_set_up" (finish quietly) or err is jobs.Later for a paused household.
func (s *Service) preflight(ctx context.Context, hid string) (skipped string, err error) {
	if _, err := s.ai.For(ctx, sqlc.New(s.d.Read), hid, ai.RoleVision); errors.Is(err, ai.ErrNotSetUp) {
		return "not_set_up", nil
	} else if err != nil {
		return "", err
	}
	if paused, err := s.ai.IsPaused(ctx, hid); err == nil && paused {
		return "", jobs.Later(ai.ErrPaused, PausedWait)
	}
	return "", nil
}

// aiFailure maps an ai.Service.Run error to what the queue should do.
func aiFailure(ctx context.Context, err error) error {
	var ae *ai.Error
	switch {
	case errors.Is(err, ai.ErrPaused):
		return jobs.Later(err, PausedWait)
	case errors.Is(err, ai.ErrNotSetUp):
		return jobs.Permanent(err)
	case ctx.Err() != nil:
		return err
	case errors.As(err, &ae):
		switch ae.Kind {
		case ai.KindRateLimited, ai.KindTimeout, ai.KindUnreachable, ai.KindOther:
			return err
		}
		return jobs.Permanent(err)
	}
	return err
}

// readTask runs one vision request through ai.Service.Run and decodes it.
func readTask[In, Out any](ctx context.Context, s *Service, job jobs.Job, feat ai.Feature, t *prompt.Task[In, Out],
	in In, images [][]byte, ref ai.Ref) (*Out, string, error) {
	res, err := s.ai.Run(ctx, job.HouseholdID, ai.RoleVision, feat, ai.Request{
		System: t.System, User: t.User(in), Images: images, SchemaName: t.Name, Schema: t.Schema, MaxTokens: t.MaxTokens,
	}, t.ValidateFor(in), ref)
	if err != nil {
		return nil, "", aiFailure(ctx, err)
	}
	out, err := t.DecodeFor(in, res.JSON)
	if err != nil {
		return nil, "", jobs.Permanent(err)
	}
	return out, res.Model, nil
}

// lastPhoto returns the newest photo with role on an item, or "".
func (s *Service) lastPhoto(ctx context.Context, hid, itemID, role string) (string, error) {
	links, err := photos.ItemLinks(ctx, s.d.Read, hid, []string{itemID}, false)
	if err != nil {
		return "", err
	}
	id := ""
	for _, l := range links[itemID] {
		if l.Role == role {
			id = l.PhotoID
		}
	}
	return id, nil
}

// liveItem loads an item; gone is true when it is missing or deleted.
func (s *Service) liveItem(ctx context.Context, hid, itemID string) (it inventory.Item, gone bool, err error) {
	it, err = inventory.GetItem(ctx, s.d.Read, hid, itemID)
	var he *httpapi.Error
	if errors.As(err, &he) && he.Code == httpapi.CodeNotFound || err == nil && it.DeletedAt != nil {
		return it, true, nil
	}
	return it, false, err
}

// believed is what the item is thought to be so far, AI guesses included.
func believed(it inventory.Item) map[string]string {
	m := map[string]string{}
	if it.Name != "" && it.Name != inventory.NewItemName {
		m["name"] = it.Name
	}
	if it.Category != "" && it.Category != inventory.FallbackCategory {
		m["category"] = it.Category
	}
	if it.Brand != nil {
		m["brand"] = *it.Brand
	}
	if it.Model != nil {
		m["model"] = *it.Model
	}
	return m
}

func sureOrLikely(c string) bool { return c == string(prompt.Sure) || c == string(prompt.Likely) }

// applyReading writes what a reader found to the item inside tx and updates
// ai_draft: the confidence of the applied fields and the needs (edit
// receives the draft after the confidences are set). ai_state is kept. The
// values a person edited are never touched (ApplyAI). skip lets the caller
// drop values before the write; it sees the draft as it was.
func (s *Service) applyReading(ctx context.Context, tx *sql.Tx, job jobs.Job, itemID string, f prompt.Fields,
	skip func(path string, prev Draft) bool, edit func(d *Draft, applied []string)) ([]string, error) {
	hid := job.HouseholdID
	cur, err := inventory.GetItem(ctx, tx, hid, itemID)
	if err != nil {
		return nil, err
	}
	prev := parseDraft(cur.AIDraft)
	values := map[string]any{}
	for p, v := range f.Values {
		if skip == nil || !skip(p, prev) {
			values[p] = v
		}
	}
	applied, err := inventory.ApplyAI(inventory.WithAI(ctx, job.CreatedBy), tx, hid, itemID, values)
	if err != nil {
		return nil, err
	}
	for _, p := range applied {
		prev.Confidence[p] = string(f.Confidence[p])
	}
	if edit != nil {
		edit(&prev, applied)
	}
	if prev.RunAt == "" {
		prev.RunAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	raw, _ := json.Marshal(prev)
	state := ""
	if cur.AIState != nil {
		state = *cur.AIState
	}
	return applied, inventory.SetAIMeta(ctx, tx, hid, itemID, state, raw)
}

func dropNeeds(needs []prompt.Need, kind prompt.NeedKind) []prompt.Need {
	return slices.DeleteFunc(slices.Clone(needs), func(n prompt.Need) bool { return n.Kind == kind })
}

// ---- J6.4 label ----

// runLabel reads the newest label photo of an item. A value read from the
// label replaces one guessed from the item photo; a person's value stays.
func (s *Service) runLabel(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var p Payload
	if err := json.Unmarshal(job.Payload, &p); err != nil || p.ItemID == "" {
		return nil, jobs.Permanent(errors.New("label_read: bad payload"))
	}
	hid := job.HouseholdID
	item, gone, err := s.liveItem(ctx, hid, p.ItemID)
	if err != nil {
		return nil, err
	}
	if gone {
		return readResult(ReadResult{Skipped: "gone"})
	}
	pid, err := s.lastPhoto(ctx, hid, item.ID, "label")
	if err != nil {
		return nil, err
	}
	if pid == "" {
		return readResult(ReadResult{Skipped: "no_photos"})
	}
	if skipped, err := s.preflight(ctx, hid); err != nil || skipped != "" {
		if err != nil {
			return nil, err
		}
		return readResult(ReadResult{Skipped: skipped})
	}
	img, err := s.photos.AIBytes(ctx, hid, pid)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, jobs.Permanent(err)
	}
	g, err := s.grounding(ctx, hid)
	if err != nil {
		return nil, err
	}
	in := prompt.LabelInput{Grounding: g, Item: believed(item)}
	ans, model, err := readTask(ctx, s, job, ai.FeatureLabel, prompt.Label, in, [][]byte{img}, ai.Ref{JobID: job.ID, ItemID: item.ID})
	if err != nil {
		return nil, err
	}
	cat := item.Category
	if cat == inventory.FallbackCategory {
		cat = ""
	}
	f := ans.Fields(cat)
	var applied []string
	err = inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
		applied, err = s.applyReading(ctx, tx, job, item.ID, f,
			func(path string, prev Draft) bool { // an unsure read never replaces a sure guess
				return f.Confidence[path] == prompt.Guess && sureOrLikely(prev.Confidence[path])
			},
			func(d *Draft, applied []string) {
				read := slices.Contains(applied, "model") || slices.Contains(applied, "serial")
				if read {
					d.Needs = dropNeeds(d.Needs, prompt.NeedLabelPhoto)
					return
				}
				for _, n := range ans.Needs {
					if n.Kind == prompt.NeedLabelPhoto {
						d.Needs = append(dropNeeds(d.Needs, prompt.NeedLabelPhoto), n)
						break
					}
				}
			})
		return err
	})
	if err != nil {
		return nil, err
	}
	return readResult(ReadResult{PromptVersion: prompt.Label.Version, Model: model, Applied: applied})
}

// ---- J6.5 receipt ----

// runReceipt reads the newest receipt photo of an item: purchase date,
// seller, currency, the item's price and the warranty end.
func (s *Service) runReceipt(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var p Payload
	if err := json.Unmarshal(job.Payload, &p); err != nil || p.ItemID == "" {
		return nil, jobs.Permanent(errors.New("receipt_read: bad payload"))
	}
	hid := job.HouseholdID
	item, gone, err := s.liveItem(ctx, hid, p.ItemID)
	if err != nil {
		return nil, err
	}
	if gone {
		return readResult(ReadResult{Skipped: "gone"})
	}
	pid, err := s.lastPhoto(ctx, hid, item.ID, "receipt")
	if err != nil {
		return nil, err
	}
	if pid == "" {
		return readResult(ReadResult{Skipped: "no_photos"})
	}
	if skipped, err := s.preflight(ctx, hid); err != nil || skipped != "" {
		if err != nil {
			return nil, err
		}
		return readResult(ReadResult{Skipped: skipped})
	}
	img, err := s.photos.AIBytes(ctx, hid, pid)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, jobs.Permanent(err)
	}
	g, err := s.grounding(ctx, hid)
	if err != nil {
		return nil, err
	}
	h, err := sqlc.New(s.d.Read).GetHousehold(ctx, hid)
	if err != nil {
		return nil, err
	}
	in := prompt.ReceiptInput{Grounding: g, Item: believed(item)}
	ans, model, err := readTask(ctx, s, job, ai.FeatureReceipt, prompt.Receipt, in, [][]byte{img}, ai.Ref{JobID: job.ID, ItemID: item.ID})
	if err != nil {
		return nil, err
	}
	f := ans.Fields(g.Currency)
	dateOK := validPurchaseDate(ans.Date)
	if !dateOK {
		delete(f.Values, "purchase_date") // a wrong or missing date is left for the person
	}
	var applied []string
	err = inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
		applied, err = s.applyReading(ctx, tx, job, item.ID, f, nil, func(d *Draft, applied []string) {
			d.OtherLines = ans.OtherLines()
			cur, _ := inventory.GetItem(ctx, tx, hid, item.ID)
			if cur.Purchase.Date != nil {
				d.Needs = dropNeeds(d.Needs, prompt.NeedReceiptPhoto)
				return
			}
			q := "The purchase date could not be read. Type it, or add a clearer receipt photo."
			field := "purchase_date"
			d.Needs = append(dropNeeds(d.Needs, prompt.NeedReceiptPhoto), prompt.Need{Kind: prompt.NeedReceiptPhoto, Field: &field, Question: q, Options: []string{}})
		})
		if err != nil {
			return err
		}
		w, err := setWarranty(ctx, tx, hid, job, item.ID, ans.WarrantyMonths, h.DefaultWarrantyMonths)
		if w != "" {
			applied = append(applied, w)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return readResult(ReadResult{PromptVersion: prompt.Receipt.Version, Model: model, Applied: applied})
}

// validPurchaseDate accepts a date the reader was not unsure about that is
// neither in the future nor absurdly old.
func validPurchaseDate(t *prompt.TextField) bool {
	if t == nil || t.Confidence == prompt.Guess {
		return false
	}
	d, err := time.Parse("2006-01-02", t.Value)
	return err == nil && d.Year() >= 1990 && !d.After(time.Now().AddDate(0, 0, 1))
}

// setWarranty sets warranty_end to the purchase date plus the stated term, or
// plus the household default when none is stated, and records which was
// used. It does nothing without a purchase date or when a person set the
// warranty end. It returns "warranty_end" when it wrote it.
func setWarranty(ctx context.Context, tx *sql.Tx, hid string, job jobs.Job, itemID string, stated *prompt.CountField, def int64) (string, error) {
	it, err := inventory.GetItem(ctx, tx, hid, itemID)
	if err != nil || it.Purchase.Date == nil {
		return "", err
	}
	wasDefault := it.Warranty.Source != nil && *it.Warranty.Source == "default"
	if it.Warranty.End != nil && !wasDefault && !slices.Contains(it.AIFields, "warranty_end") {
		return "", nil
	}
	d, err := time.Parse("2006-01-02", *it.Purchase.Date)
	if err != nil {
		return "", nil
	}
	months, isDefault := def, true
	if stated != nil && stated.Confidence != prompt.Guess && stated.Value > 0 && stated.Value <= 240 {
		months, isDefault = stated.Value, false
	}
	if months <= 0 {
		return "", nil
	}
	if wasDefault {
		if err := sqlc.New(tx).ClearDefaultWarranty(ctx, sqlc.ClearDefaultWarrantyParams{HouseholdID: hid, ID: itemID}); err != nil {
			return "", err
		}
	}
	end := d.AddDate(0, int(months), 0).Format("2006-01-02")
	applied, err := inventory.ApplyAI(inventory.WithAI(ctx, job.CreatedBy), tx, hid, itemID, map[string]any{"warranty_end": end})
	if err != nil || len(applied) == 0 {
		return "", err
	}
	if isDefault {
		if err := sqlc.New(tx).SetWarrantySourceDefault(ctx, sqlc.SetWarrantySourceDefaultParams{HouseholdID: hid, ID: itemID}); err != nil {
			return "", err
		}
	}
	return "warranty_end", nil
}

// ---- J6.6 contents ----

// ContentsPayload is the contents_list payload.
type ContentsPayload struct {
	PlaceID string `json:"place_id"`
}

// unreadContentsPhotos returns up to MaxPhotos contents photos of a place that
// no contents line uses yet, oldest first.
func (s *Service) unreadContentsPhotos(ctx context.Context, hid, placeID string) ([]string, error) {
	rows, err := s.d.Read.QueryContext(ctx, `SELECT l.photo_id FROM photo_links l
		WHERE l.household_id = ? AND l.place_id = ? AND l.role = 'contents'
		  AND NOT EXISTS (SELECT 1 FROM contents_lines c WHERE c.household_id = l.household_id AND c.place_id = l.place_id AND c.photo_id = l.photo_id)
		ORDER BY l.sort, l.id LIMIT ?`, hid, placeID, MaxPhotos)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// runContents lists what the new contents photos of a container show and
// saves the lines, marked AI-filled and linked to the first photo. Lines
// already logged are given to the prompt so they are not listed twice.
func (s *Service) runContents(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var p ContentsPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil || p.PlaceID == "" {
		return nil, jobs.Permanent(errors.New("contents_list: bad payload"))
	}
	hid := job.HouseholdID
	place, err := sqlc.New(s.d.Read).GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: hid, ID: p.PlaceID})
	if errors.Is(err, sql.ErrNoRows) || err == nil && place.DeletedAt.Valid {
		return readResult(ReadResult{Skipped: "gone"})
	}
	if err != nil {
		return nil, err
	}
	pids, err := s.unreadContentsPhotos(ctx, hid, p.PlaceID)
	if err != nil {
		return nil, err
	}
	if len(pids) == 0 {
		return readResult(ReadResult{Skipped: "no_photos"})
	}
	if skipped, err := s.preflight(ctx, hid); err != nil || skipped != "" {
		if err != nil {
			return nil, err
		}
		return readResult(ReadResult{Skipped: skipped})
	}
	images := make([][]byte, 0, len(pids))
	for _, pid := range pids {
		b, err := s.photos.AIBytes(ctx, hid, pid)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			return nil, jobs.Permanent(err)
		}
		images = append(images, b)
	}
	g, err := s.grounding(ctx, hid)
	if err != nil {
		return nil, err
	}
	existing, err := sqlc.New(s.d.Read).ListLines(ctx, sqlc.ListLinesParams{HouseholdID: hid, PlaceID: p.PlaceID})
	if err != nil {
		return nil, err
	}
	in := prompt.ContentsInput{Grounding: g, Container: place.Path}
	for _, l := range existing {
		in.Existing = append(in.Existing, prompt.ExistingLine{Name: l.Name, Quantity: l.Quantity})
	}
	ans, model, err := readTask(ctx, s, job, ai.FeatureContents, prompt.Contents, in, images, ai.Ref{JobID: job.ID})
	if err != nil {
		return nil, err
	}
	added := 0
	if len(ans.Lines) > 0 {
		err = inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
			inputs := make([]inventory.LineInput, len(ans.Lines))
			for i, l := range ans.Lines {
				inputs[i] = inventory.LineInput{Name: strings.TrimSpace(l.Name), Quantity: l.Quantity, AI: true, PhotoID: pids[0]}
				if l.Category != nil {
					inputs[i].Category = *l.Category
				}
			}
			rows, err := inventory.CreateLines(inventory.WithAI(ctx, job.CreatedBy), tx, hid, job.CreatedBy, p.PlaceID, inputs)
			if err != nil {
				return err
			}
			for i, r := range rows {
				if ans.Lines[i].WorthOwnEntry {
					if err := sqlc.New(tx).SetLineWorth(ctx, sqlc.SetLineWorthParams{HouseholdID: hid, ID: r.ID}); err != nil {
						return err
					}
				}
			}
			added = len(rows)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return readResult(ReadResult{PromptVersion: prompt.Contents.Version, Model: model, Lines: added})
}
