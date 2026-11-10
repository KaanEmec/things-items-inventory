package drafts

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/ai/prompt/prompttest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/id"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
)

func (e *env) readerJob(kind, itemID string) jobs.Job {
	j := e.job(itemID, 1)
	j.Kind = kind
	return j
}

func (e *env) answer(task, name string) {
	e.ep.answer = func() json.RawMessage { return prompttest.Answer(task, name) }
}

func (e *env) apply(itemID string, ai bool, fields map[string]any) {
	e.t.Helper()
	err := inventory.InTx(e.ctx, e.d, func(tx *sql.Tx) error {
		if ai {
			_, err := inventory.ApplyAI(e.ctx, tx, e.hid, itemID, fields)
			return err
		}
		f := inventory.Fields{}
		for k, v := range fields {
			f[k] = []byte(`"` + v.(string) + `"`)
		}
		_, err := inventory.UpdateItem(e.ctx, tx, e.hid, "", itemID, f)
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

func needKinds(it inventory.Item) string {
	var ks []string
	for _, n := range parseDraft(it.AIDraft).Needs {
		ks = append(ks, string(n.Kind))
	}
	return strings.Join(ks, ",")
}

func TestLabelOverridesGuessAndKeepsPersonEdit(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	e.answer("label", "charger_nameplate")
	itemID := e.draft()
	e.photo(itemID, "label")
	e.apply(itemID, true, map[string]any{"name": "Charger", "model": "A1234"}) // a guess from the item photo
	e.apply(itemID, false, map[string]any{"serial": "MINE-1"})                 // a person's value
	// the item draft asked for a label photo
	err := inventory.InTx(e.ctx, e.d, func(tx *sql.Tx) error {
		return inventory.SetAIMeta(e.ctx, tx, e.hid, itemID, StateReady, []byte(`{"confidence":{},"needs":[{"kind":"label_photo","field":"model","question":"XM4 or XM5?","options":[]}]}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.runLabel(e.ctx, e.readerJob(KindLabel, itemID)); err != nil {
		t.Fatal(err)
	}
	it := e.item(itemID)
	if str(it.Model) != "A1947" || str(it.Serial) != "MINE-1" || str(it.Brand) != "Apple" {
		t.Fatalf("model %s serial %s brand %s", str(it.Model), str(it.Serial), str(it.Brand))
	}
	if needKinds(it) != "" || state(it) != StateReady {
		t.Fatalf("needs %q state %s", needKinds(it), state(it))
	}
	if c := parseDraft(it.AIDraft).Confidence["model"]; c != "sure" {
		t.Fatalf("confidence %q", c)
	}
}

func TestUnreadableLabelChangesNothing(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	e.answer("label", "unreadable")
	itemID := e.draft()
	e.photo(itemID, "label")
	e.apply(itemID, true, map[string]any{"model": "A1234"})
	if _, err := e.svc.runLabel(e.ctx, e.readerJob(KindLabel, itemID)); err != nil {
		t.Fatal(err)
	}
	it := e.item(itemID)
	if str(it.Model) != "A1234" || str(it.Serial) != "<nil>" || needKinds(it) != "label_photo" {
		t.Fatalf("model %s serial %s needs %q", str(it.Model), str(it.Serial), needKinds(it))
	}
}

func TestLabelWithoutPhotoOrAI(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	itemID := e.draft()
	if _, err := e.svc.runLabel(e.ctx, e.readerJob(KindLabel, itemID)); err != nil || len(e.ep.requests()) != 0 {
		t.Fatalf("%v %d", err, len(e.ep.requests()))
	}
	e2 := newEnv(t, "sony_headphones", true)
	id2 := e2.draft()
	e2.photo(id2, "label")
	if _, err := e2.svc.runLabel(e2.ctx, e2.readerJob(KindLabel, id2)); err != nil || len(e2.ep.requests()) != 0 {
		t.Fatalf("%v", err)
	}
}

func TestReceiptFillsPurchaseAndDefaultWarranty(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	e.answer("receipt", "mediamarkt")
	itemID := e.draft()
	e.photo(itemID, "receipt")
	if _, err := e.svc.runReceipt(e.ctx, e.readerJob(KindReceipt, itemID)); err != nil {
		t.Fatal(err)
	}
	it := e.item(itemID)
	if str(it.Purchase.Date) != "2024-03-14" || str(it.Purchase.Seller) != "MediaMarkt" || *it.Purchase.PriceMinor != 27900 {
		t.Fatalf("%+v", it.Purchase)
	}
	if str(it.Warranty.End) != "2026-03-14" || str(it.Warranty.Source) != "default" {
		t.Fatalf("%+v", it.Warranty)
	}
	d := parseDraft(it.AIDraft)
	if len(d.OtherLines) != 1 || needKinds(it) != "" {
		t.Fatalf("%+v %q", d.OtherLines, needKinds(it))
	}
}

func TestReceiptStatedWarrantyAndPersonDate(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	e.answer("receipt", "tr_teknosa")
	itemID := e.draft()
	e.photo(itemID, "receipt")
	err := inventory.InTx(e.ctx, e.d, func(tx *sql.Tx) error { // a person's date wins
		_, err := inventory.UpdateItem(e.ctx, tx, e.hid, "", itemID, inventory.Fields{"purchase": []byte(`{"date":"2025-01-10"}`)})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.runReceipt(e.ctx, e.readerJob(KindReceipt, itemID)); err != nil {
		t.Fatal(err)
	}
	it := e.item(itemID)
	if str(it.Purchase.Date) != "2025-01-10" || str(it.Warranty.End) != "2027-01-10" || str(it.Warranty.Source) != "stated" {
		t.Fatalf("%+v %+v", it.Purchase, it.Warranty)
	}
}

func TestReceiptWithoutDateGivesNoWarranty(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	e.answer("receipt", "not_a_receipt")
	itemID := e.draft()
	e.photo(itemID, "receipt")
	if _, err := e.svc.runReceipt(e.ctx, e.readerJob(KindReceipt, itemID)); err != nil {
		t.Fatal(err)
	}
	it := e.item(itemID)
	if it.Warranty.End != nil || it.Purchase.Date != nil || needKinds(it) != "receipt_photo" {
		t.Fatalf("%+v %+v %q", it.Purchase, it.Warranty, needKinds(it))
	}
}

func (e *env) container() string {
	e.t.Helper()
	var placeID string
	err := inventory.InTx(e.ctx, e.d, func(tx *sql.Tx) error {
		h, err := inventory.CreateHome(e.ctx, tx, e.hid, "Home", "")
		if err != nil {
			return err
		}
		p, err := inventory.CreatePlace(e.ctx, tx, e.hid, inventory.PlaceInput{HomeID: h.ID, Kind: "room", Name: "Study"})
		placeID = p.ID
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return placeID
}

func (e *env) placePhoto(placeID string) string {
	e.t.Helper()
	pid := id.New()
	e.ph.mu.Lock()
	e.ph.bytes[pid] = []byte("contents:" + pid)
	e.ph.mu.Unlock()
	err := e.d.Tx(e.ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		if _, err := q.InsertPhoto(e.ctx, sqlc.InsertPhotoParams{ID: pid, HouseholdID: e.hid, CreatedAt: stamp, UpdatedAt: stamp,
			Sha256: strings.Repeat("b", 64-len(pid)) + pid, Mime: "image/jpeg", Ext: "jpg", Bytes: 10}); err != nil {
			return err
		}
		_, err := q.InsertPhotoLink(e.ctx, sqlc.InsertPhotoLinkParams{ID: id.New(), HouseholdID: e.hid, CreatedAt: stamp, UpdatedAt: stamp,
			PhotoID: pid, PlaceID: sql.NullString{String: placeID, Valid: true}, Role: "contents"})
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return pid
}

func TestContentsListSavesLinesOnce(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	e.answer("contents", "cable_box")
	place := e.container()
	pid := e.placePhoto(place)
	job := e.readerJob(KindContents, "")
	job.Payload = []byte(`{"place_id":"` + place + `"}`)
	if _, err := e.svc.runContents(e.ctx, job); err != nil {
		t.Fatal(err)
	}
	lines, err := sqlc.New(e.d.Read).ListLines(e.ctx, sqlc.ListLinesParams{HouseholdID: e.hid, PlaceID: place})
	if err != nil || len(lines) != 4 {
		t.Fatalf("%v %d", err, len(lines))
	}
	for _, l := range lines {
		if l.Ai != 1 || l.PhotoID.String != pid {
			t.Fatalf("%+v", l)
		}
		if (l.Name == "Raspberry Pi 4") != (l.WorthItem == 1) {
			t.Fatalf("worth flag on %q: %d", l.Name, l.WorthItem)
		}
	}
	// the same photo is not read again
	n := len(e.ep.requests())
	if res, err := e.svc.runContents(e.ctx, job); err != nil || !strings.Contains(string(res), "no_photos") || len(e.ep.requests()) != n {
		t.Fatalf("%s %v", res, err)
	}
}

func TestPausedReaderWaits(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	itemID := e.draft()
	e.photo(itemID, "receipt")
	if _, err := e.d.Write.Exec(`UPDATE households SET ai_paused = 1 WHERE id = ?`, e.hid); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.runReceipt(e.ctx, e.readerJob(KindReceipt, itemID))
	if !errors.Is(err, ai.ErrPaused) || len(e.ep.requests()) != 0 {
		t.Fatalf("%v", err)
	}
}
