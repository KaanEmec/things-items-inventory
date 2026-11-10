package drafts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

func result(r Result) (json.RawMessage, error) {
	b, err := json.Marshal(r)
	return b, err
}

// run is the draft_item handler.
func (s *Service) run(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var p Payload
	if err := json.Unmarshal(job.Payload, &p); err != nil || p.ItemID == "" {
		return nil, jobs.Permanent(errors.New("draft_item: bad payload"))
	}
	hid := job.HouseholdID
	item, err := inventory.GetItem(ctx, s.d.Read, hid, p.ItemID)
	var he *httpapi.Error
	if errors.As(err, &he) && he.Code == httpapi.CodeNotFound || err == nil && item.DeletedAt != nil {
		return result(Result{Skipped: "gone"})
	}
	if err != nil {
		return nil, err
	}
	prevRaw := []byte(item.AIDraft)

	links, err := photos.ItemLinks(ctx, s.d.Read, hid, []string{item.ID}, false)
	if err != nil {
		return nil, err
	}
	chosen := pickPhotos(links[item.ID])
	if len(chosen) == 0 {
		return resultWith(Result{Skipped: "no_photos"}, s.setMeta(ctx, hid, item.ID, "", prevRaw))
	}

	// Decide before showing "reading": no AI means a plain photo-only draft,
	// paused means wait quietly.
	if _, err := s.ai.For(ctx, sqlc.New(s.d.Read), hid, ai.RoleVision); errors.Is(err, ai.ErrNotSetUp) {
		return resultWith(Result{Skipped: "not_set_up", Photos: len(chosen)}, s.setMeta(ctx, hid, item.ID, "", prevRaw))
	}
	if paused, err := s.ai.IsPaused(ctx, hid); err == nil && paused {
		return nil, jobs.Later(ai.ErrPaused, PausedWait)
	}

	if err := s.setMeta(ctx, hid, item.ID, StateReading, prevRaw); err != nil {
		return nil, err
	}

	images := make([][]byte, 0, len(chosen))
	roles := make([]prompt.PhotoRole, 0, len(chosen))
	for _, l := range chosen {
		b, err := s.photos.AIBytes(ctx, hid, l.PhotoID)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			return nil, s.fail(ctx, hid, item.ID, prevRaw, CouldntRead, fmt.Errorf("photo %s unreadable: %w", l.PhotoID, err))
		}
		images = append(images, b)
		roles = append(roles, prompt.PhotoRole(l.Role))
	}

	g, err := s.grounding(ctx, hid)
	if err != nil {
		return nil, s.retry(ctx, job, hid, item.ID, prevRaw, "", err)
	}
	in := prompt.ItemDraftInput{Grounding: g, Photos: roles, Known: known(item, p.Hint)}
	task := prompt.ItemDraft
	res, err := s.ai.Run(ctx, hid, ai.RoleVision, ai.FeatureItemDraft, ai.Request{
		System: task.System, User: task.User(in), Images: images,
		SchemaName: task.Name, Schema: task.Schema, MaxTokens: task.MaxTokens,
	}, task.ValidateFor(in), ai.Ref{JobID: job.ID, ItemID: item.ID})
	if err != nil {
		return nil, s.handleAIError(ctx, job, item.ID, prevRaw, err)
	}
	ans, err := task.DecodeFor(in, res.JSON)
	if err != nil {
		return nil, s.fail(ctx, hid, item.ID, prevRaw, CouldntRead, err)
	}

	f := ans.Fields(g.Currency)
	var applied []string
	actx := inventory.WithAI(ctx, job.CreatedBy)
	err = inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
		cur, err := inventory.GetItem(ctx, tx, hid, item.ID)
		if err != nil {
			return err
		}
		prev := parseDraft(cur.AIDraft)
		values := map[string]any{}
		for path, v := range f.Values {
			// A field the AI filled earlier and was sure or likely about stays.
			if slices.Contains(cur.AIFields, path) && (prev.Confidence[path] == string(prompt.Sure) || prev.Confidence[path] == string(prompt.Likely)) {
				continue
			}
			values[path] = v
		}
		if applied, err = inventory.ApplyAI(actx, tx, hid, item.ID, values); err != nil {
			return err
		}
		after, err := inventory.GetItem(ctx, tx, hid, item.ID)
		if err != nil {
			return err
		}
		draft := Draft{Confidence: map[string]string{}, Needs: ans.Needs, PromptVersion: task.Version, Model: res.Model,
			RunAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z")}
		if draft.Needs == nil {
			draft.Needs = []prompt.Need{}
		}
		for _, path := range after.AIFields {
			if slices.Contains(applied, path) {
				draft.Confidence[path] = string(f.Confidence[path])
			} else if c, ok := prev.Confidence[path]; ok {
				draft.Confidence[path] = c
			}
		}
		raw, _ := json.Marshal(draft)
		return inventory.SetAIMeta(ctx, tx, hid, item.ID, StateReady, raw)
	})
	if err != nil {
		return nil, err
	}
	return result(Result{PromptVersion: task.Version, Model: res.Model, Photos: len(chosen), Applied: applied})
}

// resultWith returns the result unless the state write before it failed.
func resultWith(r Result, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, err
	}
	return result(r)
}

func (s *Service) handleAIError(ctx context.Context, job jobs.Job, itemID string, prevRaw []byte, err error) error {
	hid := job.HouseholdID
	switch {
	case errors.Is(err, ai.ErrPaused):
		_ = s.setMeta(ctx, hid, itemID, StateQueued, prevRaw)
		return jobs.Later(err, PausedWait)
	case errors.Is(err, ai.ErrNotSetUp):
		return s.noAI(ctx, hid, itemID, prevRaw)
	case ctx.Err() != nil:
		return err // shutting down or timed out: the queue decides
	}
	var ae *ai.Error
	if errors.As(err, &ae) {
		switch ae.Kind {
		case ai.KindInvalidOutput:
			return s.fail(ctx, hid, itemID, prevRaw, CouldntRead, err)
		case ai.KindRateLimited, ai.KindTimeout, ai.KindUnreachable, ai.KindOther:
			return s.retry(ctx, job, hid, itemID, prevRaw, ae.Message, err)
		}
		// Setup problems (token, model, credit): retrying cannot help.
		return s.fail(ctx, hid, itemID, prevRaw, ae.Message, err)
	}
	return s.retry(ctx, job, hid, itemID, prevRaw, "", err)
}

// noAI leaves the item a photo-only draft. The job ends without error.
func (s *Service) noAI(ctx context.Context, hid, itemID string, prevRaw []byte) error {
	return s.setMeta(ctx, hid, itemID, "", prevRaw)
}

// retry returns err for the worker to retry; on the last attempt it also
// marks the item failed.
func (s *Service) retry(ctx context.Context, job jobs.Job, hid, itemID string, prevRaw []byte, message string, err error) error {
	if job.Attempts >= MaxAttempts {
		if message == "" {
			message = CouldntRead
		}
		_ = s.setFailed(ctx, hid, itemID, prevRaw, message)
		return err
	}
	_ = s.setMeta(ctx, hid, itemID, StateQueued, prevRaw)
	return err
}

// fail marks the item failed with a message for the person and returns a
// permanent job error.
func (s *Service) fail(ctx context.Context, hid, itemID string, prevRaw []byte, message string, cause error) error {
	if err := s.setFailed(ctx, hid, itemID, prevRaw, message); err != nil {
		return err
	}
	return jobs.Permanent(cause)
}

func (s *Service) setFailed(ctx context.Context, hid, itemID string, prevRaw []byte, message string) error {
	d := parseDraft(prevRaw)
	d.Error = &message
	d.RunAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	raw, _ := json.Marshal(d)
	return s.setMeta(ctx, hid, itemID, StateFailed, raw)
}

// setMeta writes ai_state (and keeps ai_draft as given) through the normal
// write path so the live stream hears about it.
func (s *Service) setMeta(ctx context.Context, hid, itemID, state string, draft []byte) error {
	if len(draft) == 0 {
		draft = nil
	}
	return inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
		return inventory.SetAIMeta(ctx, tx, hid, itemID, state, draft)
	})
}

// pickPhotos chooses up to MaxPhotos photos, most informative first: label,
// then item, then receipt. Each kind gets a share (1 label, 2 item, 1
// receipt) before leftovers fill the rest, so a pile of item photos cannot
// push out the label or receipt.
func pickPhotos(ls []photos.LinkInfo) []photos.LinkInfo {
	order := []string{"label", "item", "receipt"}
	share := map[string]int{"label": 1, "item": 2, "receipt": 1}
	groups := map[string][]photos.LinkInfo{}
	seen := map[string]bool{}
	for _, l := range ls {
		if !slices.Contains(order, l.Role) || seen[l.PhotoID+l.Role] {
			continue
		}
		seen[l.PhotoID+l.Role] = true
		groups[l.Role] = append(groups[l.Role], l)
	}
	take := map[string]int{}
	total := 0
	for _, r := range order {
		n := min(share[r], len(groups[r]))
		take[r], total = n, total+n
	}
	for _, r := range order {
		for total < MaxPhotos && take[r] < len(groups[r]) {
			take[r]++
			total++
		}
	}
	var out []photos.LinkInfo
	for _, r := range order {
		out = append(out, groups[r][:take[r]]...)
	}
	return out
}

// known lists what a person already entered, plus their hint, for the prompt.
func known(it inventory.Item, hint string) map[string]string {
	k := map[string]string{}
	person := func(path string) bool { return !slices.Contains(it.AIFields, path) }
	put := func(path string, v *string) {
		if v != nil && strings.TrimSpace(*v) != "" && person(path) {
			k[path] = *v
		}
	}
	if it.Name != "" && it.Name != inventory.NewItemName && person("name") {
		k["name"] = it.Name
	}
	if it.Category != "" && it.Category != inventory.FallbackCategory && person("category") {
		k["category"] = it.Category
	}
	put("brand", it.Brand)
	put("model", it.Model)
	put("serial", it.Serial)
	put("color", it.Color)
	put("condition", it.Condition)
	for key, v := range it.Attributes {
		if path := "attributes." + key; person(path) {
			k[path] = fmt.Sprint(v)
		}
	}
	if h := strings.TrimSpace(hint); h != "" {
		k["hint"] = h
	}
	return k
}

// grounding tells the prompt about the household: language, currency and the
// brands and names already in use.
func (s *Service) grounding(ctx context.Context, hid string) (prompt.Grounding, error) {
	h, err := sqlc.New(s.d.Read).GetHousehold(ctx, hid)
	if err != nil {
		return prompt.Grounding{}, err
	}
	g := prompt.Grounding{Language: h.Language, Currency: h.Currency}
	for col, dst := range map[string]*[]string{"brand": &g.Brands, "name": &g.Names} {
		rows, err := s.d.Read.QueryContext(ctx, `SELECT `+col+` FROM items WHERE household_id = ? AND deleted_at IS NULL AND draft = 0
			AND `+col+` IS NOT NULL AND `+col+` <> '' GROUP BY `+col+` ORDER BY COUNT(*) DESC, `+col+` LIMIT 40`, hid)
		if err != nil {
			return g, err
		}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return g, err
			}
			*dst = append(*dst, v)
		}
		if err := rows.Close(); err != nil {
			return g, err
		}
	}
	return g, nil
}
