package drafts

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/ai/prompt"
	"github.com/kaanemec/things-items-inventory/internal/ai/prompt/prompttest"
	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
	"github.com/kaanemec/things-items-inventory/internal/id"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
	"github.com/kaanemec/things-items-inventory/internal/photos"
)

const stamp = "2026-01-01T00:00:00.000Z"

// ---- fake endpoint ----

type endpoint struct {
	*httptest.Server
	mu     sync.Mutex
	reqs   []chatRequest
	answer func() json.RawMessage // the model's JSON answer
	before func()                 // runs while a request is open
}

type chatRequest struct {
	User   string
	Images [][]byte
}

func newEndpoint(t *testing.T, fixture string) *endpoint {
	e := &endpoint{}
	e.answer = func() json.RawMessage { return prompttest.Answer("item_draft", fixture) }
	e.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		var cr chatRequest
		for _, m := range body.Messages {
			if m.Role != "user" {
				continue
			}
			parts, _ := m.Content.([]any)
			for _, p := range parts {
				pm := p.(map[string]any)
				if pm["type"] == "text" {
					cr.User, _ = pm["text"].(string)
				} else {
					u := pm["image_url"].(map[string]any)["url"].(string)
					img, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(u, "data:image/jpeg;base64,"))
					cr.Images = append(cr.Images, img)
				}
			}
		}
		e.mu.Lock()
		e.reqs = append(e.reqs, cr)
		before, answer := e.before, e.answer
		e.mu.Unlock()
		if before != nil {
			before()
		}
		out, _ := json.Marshal(map[string]any{"model": "served-model",
			"choices": []any{map[string]any{"message": map[string]any{"content": string(answer())}}}})
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	}))
	t.Cleanup(e.Close)
	return e
}

func (e *endpoint) requests() []chatRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]chatRequest(nil), e.reqs...)
}

// ---- environment ----

type fakePhotos struct {
	mu    sync.Mutex
	bytes map[string][]byte
}

func (f *fakePhotos) AIBytes(_ context.Context, _, photoID string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.bytes[photoID]; ok {
		return b, nil
	}
	return nil, photos.ErrNotFound
}

type env struct {
	t   *testing.T
	d   *db.DB
	ep  *endpoint
	svc *Service
	q   *jobs.Queue
	ph  *fakePhotos
	hid string
	ctx context.Context
}

// newEnv builds a household whose vision endpoint is the fake one; with
// noAI the household has no endpoint at all.
func newEnv(t *testing.T, fixture string, noAI bool) *env {
	t.Helper()
	d := dbtest.New(t)
	e := &env{t: t, d: d, ctx: context.Background(), hid: id.New(), ph: &fakePhotos{bytes: map[string][]byte{}}}
	e.ep = newEndpoint(t, fixture)
	cfg := config.Config{}
	if !noAI {
		cfg.AI.Vision = config.AIEndpoint{BaseURL: e.ep.URL, Token: "tok-test", Model: "vm"}
	}
	if _, err := sqlc.New(d.Write).CreateHousehold(e.ctx, sqlc.CreateHouseholdParams{
		ID: e.hid, Name: "H", Language: "en", Currency: "EUR", CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
		t.Fatal(err)
	}
	e.q = jobs.New(d, jobs.Options{PollInterval: 20 * time.Millisecond, Backoff: []time.Duration{10 * time.Millisecond}})
	e.svc = New(d, ai.NewService(d, cfg), e.ph, e.q)
	e.svc.Delay = 0
	return e
}

func (e *env) draft() string {
	e.t.Helper()
	var itemID string
	err := inventory.InTx(e.ctx, e.d, func(tx *sql.Tx) error {
		it, _, err := inventory.CreateItem(e.ctx, tx, e.hid, "", inventory.NewItem{Source: "capture",
			Fields: inventory.Fields{"draft": []byte("true")}})
		itemID = it.ID
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return itemID
}

// photo attaches a photo with the given role; its bytes are the photo's ID.
func (e *env) photo(itemID, role string) string {
	e.t.Helper()
	pid := id.New()
	e.ph.mu.Lock()
	e.ph.bytes[pid] = []byte(role + ":" + pid)
	e.ph.mu.Unlock()
	err := e.d.Tx(e.ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		if _, err := q.InsertPhoto(e.ctx, sqlc.InsertPhotoParams{ID: pid, HouseholdID: e.hid, CreatedAt: stamp, UpdatedAt: stamp,
			Sha256: strings.Repeat("a", 64-len(pid)) + pid, Mime: "image/jpeg", Ext: "jpg", Bytes: 10}); err != nil {
			return err
		}
		var n int64
		tx.QueryRow(`SELECT COUNT(*) FROM photo_links WHERE item_id = ?`, itemID).Scan(&n)
		_, err := q.InsertPhotoLink(e.ctx, sqlc.InsertPhotoLinkParams{ID: id.New(), HouseholdID: e.hid, CreatedAt: stamp, UpdatedAt: stamp,
			PhotoID: pid, ItemID: sql.NullString{String: itemID, Valid: true}, Role: role, Sort: n})
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return pid
}

func (e *env) job(itemID string, attempts int) jobs.Job {
	p, _ := json.Marshal(Payload{ItemID: itemID})
	return jobs.Job{ID: id.New(), HouseholdID: e.hid, Kind: Kind, Payload: p, ItemID: itemID, Attempts: attempts}
}

func (e *env) item(itemID string) inventory.Item {
	e.t.Helper()
	it, err := inventory.GetItem(e.ctx, e.d.Read, e.hid, itemID)
	if err != nil {
		e.t.Fatal(err)
	}
	return it
}

func state(it inventory.Item) string {
	if it.AIState == nil {
		return "<null>"
	}
	return *it.AIState
}

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ---- tests ----

func TestHappyPath(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	itemID := e.draft()
	item := e.photo(itemID, "item")
	label := e.photo(itemID, "label")
	_ = item

	res, err := e.svc.run(e.ctx, e.job(itemID, 1))
	if err != nil {
		t.Fatal(err)
	}
	it := e.item(itemID)
	if it.Name != "Sony WH-1000XM4 headphones" || str(it.Brand) != "Sony" || str(it.Model) != "WH-1000XM4" || it.Category != "gadget" {
		t.Fatalf("fields not filled: %+v", it)
	}
	for _, f := range []string{"name", "brand", "model", "category", "attributes.type"} {
		if !has(it.AIFields, f) {
			t.Errorf("%s missing from ai_fields %v", f, it.AIFields)
		}
	}
	if state(it) != StateReady || !it.Draft {
		t.Fatalf("state %s draft %v", state(it), it.Draft)
	}
	d := parseDraft(it.AIDraft)
	if d.PromptVersion != prompt.ItemDraft.Version || d.Confidence["brand"] != "sure" || len(d.Needs) != 1 || d.Needs[0].Kind != prompt.NeedReceiptPhoto || d.Error != nil {
		t.Fatalf("ai_draft: %s", it.AIDraft)
	}
	var r Result
	json.Unmarshal(res, &r)
	if r.PromptVersion != prompt.ItemDraft.Version || r.Photos != 2 || !has(r.Applied, "brand") {
		t.Fatalf("result: %s", res)
	}
	reqs := e.ep.requests()
	if len(reqs) != 1 || len(reqs[0].Images) != 2 || string(reqs[0].Images[0]) != "label:"+label {
		t.Fatalf("requests: %d, first image %q", len(reqs), reqs[0].Images[0])
	}
	// The history names the AI as the actor.
	var kind string
	if err := e.d.Read.QueryRow(`SELECT actor_kind FROM events WHERE item_id = ? AND actor_kind = 'ai' LIMIT 1`, itemID).Scan(&kind); err != nil {
		t.Fatalf("no AI event: %v", err)
	}
}

func TestStateIsReadingWhileJobRuns(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	itemID := e.draft()
	e.photo(itemID, "item")
	var seen string
	e.ep.before = func() { seen = state(e.item(itemID)) }
	if _, err := e.svc.run(e.ctx, e.job(itemID, 1)); err != nil {
		t.Fatal(err)
	}
	if seen != StateReading {
		t.Fatalf("state during the call: %s", seen)
	}
}

func TestPersonEditWhileJobRunsWins(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	itemID := e.draft()
	e.photo(itemID, "item")
	e.ep.before = func() {
		err := inventory.InTx(e.ctx, e.d, func(tx *sql.Tx) error {
			_, err := inventory.UpdateItem(e.ctx, tx, e.hid, "", itemID, inventory.Fields{"brand": []byte(`"Bose"`)})
			return err
		})
		if err != nil {
			t.Error(err)
		}
	}
	if _, err := e.svc.run(e.ctx, e.job(itemID, 1)); err != nil {
		t.Fatal(err)
	}
	it := e.item(itemID)
	if str(it.Brand) != "Bose" || has(it.AIFields, "brand") {
		t.Fatalf("person's brand lost: %s %v", str(it.Brand), it.AIFields)
	}
	if it.Name != "Sony WH-1000XM4 headphones" || !has(it.AIFields, "name") {
		t.Fatalf("other fields should still be filled: %+v", it)
	}
	if _, ok := parseDraft(it.AIDraft).Confidence["brand"]; ok {
		t.Fatal("confidence kept for a person's field")
	}
}

func TestPersonValuesAreSentAsKnownAndHintAsData(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	itemID := e.draft()
	e.photo(itemID, "item")
	err := inventory.InTx(e.ctx, e.d, func(tx *sql.Tx) error {
		_, err := inventory.UpdateItem(e.ctx, tx, e.hid, "", itemID, inventory.Fields{"color": []byte(`"Midnight"`)})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	j := e.job(itemID, 1)
	j.Payload, _ = json.Marshal(Payload{ItemID: itemID, Hint: "bought in 2021"})
	if _, err := e.svc.run(e.ctx, j); err != nil {
		t.Fatal(err)
	}
	u := e.ep.requests()[0].User
	if !strings.Contains(u, "color: Midnight") || !strings.Contains(u, "hint: bought in 2021") {
		t.Fatalf("user prompt lacks known values:\n%s", u)
	}
	if it := e.item(itemID); str(it.Color) != "Midnight" {
		t.Fatalf("color overwritten: %s", str(it.Color))
	}
}

func TestInvalidAnswerFailsPermanently(t *testing.T) {
	e := newEnv(t, "invalid_serial_not_read", false)
	itemID := e.draft()
	e.photo(itemID, "item")
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	if err := e.q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.q.Stop(ctx)
	jobID := e.enqueue(itemID)
	j := e.waitJob(jobID, "failed")
	if j.Attempts != 1 {
		t.Fatalf("a permanent failure must not retry: attempts %d", j.Attempts)
	}
	it := e.item(itemID)
	d := parseDraft(it.AIDraft)
	if state(it) != StateFailed || d.Error == nil || *d.Error != CouldntRead {
		t.Fatalf("state %s draft %s", state(it), it.AIDraft)
	}
	if it.Name != "New item" {
		t.Fatalf("nothing should be filled: %s", it.Name)
	}
}

func TestUnreadablePhotoFailsPermanently(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	itemID := e.draft()
	pid := e.photo(itemID, "item")
	delete(e.ph.bytes, pid)
	_, err := e.svc.run(e.ctx, e.job(itemID, 1))
	if err == nil || len(e.ep.requests()) != 0 {
		t.Fatalf("err %v, requests %d", err, len(e.ep.requests()))
	}
	it := e.item(itemID)
	if state(it) != StateFailed || *parseDraft(it.AIDraft).Error != CouldntRead {
		t.Fatalf("state %s draft %s", state(it), it.AIDraft)
	}
}

func TestPausedDefersWithoutFailing(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	itemID := e.draft()
	e.photo(itemID, "item")
	if _, err := e.d.Write.Exec(`UPDATE households SET ai_paused = 1 WHERE id = ?`, e.hid); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	if err := e.q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.q.Stop(ctx)
	jobID := e.enqueue(itemID)
	deadline := time.Now().Add(5 * time.Second)
	for {
		j, _ := e.q.Get(e.ctx, e.hid, jobID)
		if j.Deferrals == 1 {
			if j.State != "queued" || j.Attempts != 0 && j.Attempts != 1 {
				t.Fatalf("job %s attempts %d", j.State, j.Attempts)
			}
			if !j.RunAfter.After(time.Now().Add(time.Minute)) {
				t.Fatalf("not deferred for minutes: %v", j.RunAfter)
			}
			break
		}
		if time.Now().After(deadline) || j.State == "failed" {
			t.Fatalf("job not deferred: %+v", j)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if it := e.item(itemID); state(it) != StateQueued || len(e.ep.requests()) != 0 {
		t.Fatalf("state %s, requests %d", state(it), len(e.ep.requests()))
	}
}

func TestNoAIProfileLeavesPhotoOnlyDraft(t *testing.T) {
	e := newEnv(t, "sony_headphones", true)
	itemID := e.draft()
	e.photo(itemID, "item")
	res, err := e.svc.run(e.ctx, e.job(itemID, 1))
	if err != nil {
		t.Fatal(err)
	}
	var r Result
	json.Unmarshal(res, &r)
	it := e.item(itemID)
	if r.Skipped != "not_set_up" || it.AIState != nil || it.Name != "New item" || !it.Draft || len(e.ep.requests()) != 0 {
		t.Fatalf("result %s item %+v", res, it)
	}
	// The capture hook queues nothing either.
	err = inventory.InTx(e.ctx, e.d, func(tx *sql.Tx) error { return e.svc.OnPhoto(e.ctx, tx, e.hid, "", itemID, "item") })
	if err != nil {
		t.Fatal(err)
	}
	if n := e.jobCount(itemID, "queued"); n != 0 || e.item(itemID).AIState != nil {
		t.Fatalf("queued %d", n)
	}
}

func TestSeveralPhotosInOneRequestInOrder(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	itemID := e.draft()
	r1 := e.photo(itemID, "receipt")
	i1 := e.photo(itemID, "item")
	i2 := e.photo(itemID, "item")
	i3 := e.photo(itemID, "item")
	l1 := e.photo(itemID, "label")
	if _, err := e.svc.run(e.ctx, e.job(itemID, 1)); err != nil {
		t.Fatal(err)
	}
	reqs := e.ep.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	var got []string
	for _, b := range reqs[0].Images {
		got = append(got, string(b))
	}
	want := []string{"label:" + l1, "item:" + i1, "item:" + i2, "receipt:" + r1}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("images\n got %v\nwant %v (third item photo %s must be dropped)", got, want, i3)
	}
	if !strings.Contains(reqs[0].User, "1: label") || !strings.Contains(reqs[0].User, "4: receipt") {
		t.Fatalf("prompt does not name the roles:\n%s", reqs[0].User)
	}
}

func TestPickPhotos(t *testing.T) {
	mk := func(roles ...string) (out []photos.LinkInfo) {
		for i, r := range roles {
			out = append(out, photos.LinkInfo{PhotoID: fmt.Sprint(i), Role: r})
		}
		return
	}
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"item"}, "item"},
		{[]string{"item", "item", "item", "item", "item"}, "item item item item"},
		{[]string{"item", "item", "item", "label", "receipt"}, "label item item receipt"},
		{[]string{"receipt", "contents", "item"}, "item receipt"},
		{[]string{"label", "label", "label", "item"}, "label label label item"},
	}
	for _, c := range cases {
		var got []string
		for _, l := range pickPhotos(mk(c.in...)) {
			got = append(got, l.Role)
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("%v: got %v, want %s", c.in, got, c.want)
		}
	}
}

func TestSecondRunFillsOnlyEmptyOrGuessed(t *testing.T) {
	e := newEnv(t, "phone_unsure", false)
	itemID := e.draft()
	e.photo(itemID, "item")
	if _, err := e.svc.run(e.ctx, e.job(itemID, 1)); err != nil {
		t.Fatal(err)
	}
	it := e.item(itemID)
	if it.Name != "Apple iPhone" || str(it.Color) != "Blue" || it.Model != nil {
		t.Fatalf("first run: %+v", it)
	}
	// Second run: the model is now certain. Color was "sure" before and
	// stays; the likely name and brand are replaced only if not sure/likely,
	// so they stay too; the empty model is filled.
	e.ep.mu.Lock()
	e.ep.answer = func() json.RawMessage {
		return json.RawMessage(`{"read_text":["iPhone 14"],"category":{"value":"gadget","confidence":"sure"},
		"brand":{"value":"Apple Inc","confidence":"sure"},"model":{"value":"iPhone 14","confidence":"sure"},
		"serial":null,"name":{"value":"Apple iPhone 14","confidence":"sure"},"color":{"value":"Red","confidence":"sure"},
		"condition":null,"quantity":null,"attributes":[],"purchase":null,"note":null,"needs":[]}`)
	}
	e.ep.mu.Unlock()
	if _, err := e.svc.run(e.ctx, e.job(itemID, 1)); err != nil {
		t.Fatal(err)
	}
	it = e.item(itemID)
	if str(it.Model) != "iPhone 14" || !has(it.AIFields, "model") {
		t.Fatalf("empty field not filled: %+v", it)
	}
	if it.Name != "Apple iPhone" || str(it.Brand) != "Apple" || str(it.Color) != "Blue" {
		t.Fatalf("earlier sure/likely AI values were overwritten: %s / %s / %s", it.Name, str(it.Brand), str(it.Color))
	}
	d := parseDraft(it.AIDraft)
	if d.Confidence["model"] != "sure" || d.Confidence["brand"] != "likely" || len(d.Needs) != 0 {
		t.Fatalf("ai_draft: %s", it.AIDraft)
	}
}

func TestSecondRunOverwritesGuesses(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	itemID := e.draft()
	e.photo(itemID, "item")
	e.ep.answer = func() json.RawMessage {
		return json.RawMessage(`{"read_text":[],"category":{"value":"gadget","confidence":"sure"},"brand":null,"model":null,"serial":null,
		"name":{"value":"Some headphones","confidence":"guess"},"color":null,"condition":null,"quantity":null,"attributes":[],"purchase":null,"note":null,"needs":[]}`)
	}
	if _, err := e.svc.run(e.ctx, e.job(itemID, 1)); err != nil {
		t.Fatal(err)
	}
	if it := e.item(itemID); it.Name != "Some headphones" {
		t.Fatalf("first: %s", it.Name)
	}
	e.ep.answer = func() json.RawMessage { return prompttest.Answer("item_draft", "sony_headphones") }
	if _, err := e.svc.run(e.ctx, e.job(itemID, 1)); err != nil {
		t.Fatal(err)
	}
	if it := e.item(itemID); it.Name != "Sony WH-1000XM4 headphones" {
		t.Fatalf("a guess should be replaced: %s", it.Name)
	}
}

func TestOnPhotoQueuesOnceAndMarksWaiting(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	itemID := e.draft()
	e.svc.Delay = CaptureDelay
	for i := 0; i < 3; i++ {
		err := inventory.InTx(e.ctx, e.d, func(tx *sql.Tx) error { return e.svc.OnPhoto(e.ctx, tx, e.hid, "", itemID, "item") })
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := e.jobCount(itemID, "queued"); n != 1 {
		t.Fatalf("%d queued jobs", n)
	}
	it := e.item(itemID)
	if state(it) != StateQueued {
		t.Fatal(state(it))
	}
	var run string
	e.d.Read.QueryRow(`SELECT run_after FROM jobs WHERE item_id = ?`, itemID).Scan(&run)
	if at, _ := time.Parse("2006-01-02T15:04:05.000Z", run); time.Until(at) < time.Second || time.Until(at) > 6*time.Second {
		t.Fatalf("run_after %s", run)
	}
	// A place photo, or a saved item, queues nothing.
	other := e.draft()
	err := inventory.InTx(e.ctx, e.d, func(tx *sql.Tx) error { return e.svc.OnPhoto(e.ctx, tx, e.hid, "", other, "place") })
	if err != nil || e.jobCount(other, "queued") != 0 {
		t.Fatal("place photo queued a job", err)
	}
}

func TestEndToEndThroughQueue(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	itemID := e.draft()
	e.photo(itemID, "item")
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	if err := e.q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.q.Stop(ctx)
	j := e.waitJob(e.enqueue(itemID), "done")
	var r Result
	json.Unmarshal(j.Result, &r)
	if r.PromptVersion != prompt.ItemDraft.Version {
		t.Fatalf("result %s", j.Result)
	}
	if it := e.item(itemID); state(it) != StateReady || str(it.Brand) != "Sony" {
		t.Fatalf("%s %+v", state(it), it)
	}
}

// ---- helpers for queue tests ----

func (e *env) enqueue(itemID string) string {
	e.t.Helper()
	var jobID string
	err := inventory.InTx(e.ctx, e.d, func(tx *sql.Tx) error {
		var err error
		jobID, _, err = e.svc.queue(e.ctx, tx, e.hid, "", itemID, "", 0)
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return jobID
}

func (e *env) waitJob(jobID, want string) jobs.Job {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		j, err := e.q.Get(e.ctx, e.hid, jobID)
		if err != nil {
			e.t.Fatal(err)
		}
		if j.State == want {
			return j
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("job still %s (want %s): %+v", j.State, want, j)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *env) jobCount(itemID, state string) (n int) {
	e.d.Read.QueryRow(`SELECT COUNT(*) FROM jobs WHERE item_id = ? AND kind = ? AND state = ?`, itemID, Kind, state).Scan(&n)
	return n
}

// ---- routes ----

func (e *env) server(hid string) *httptest.Server {
	return httpapitest.Server(e.t, func(mux *httpapi.Mux) {
		mux.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r = r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{UserID: "u1", HouseholdID: hid, Role: auth.RoleMember, Source: auth.SourceSession}))
				next.ServeHTTP(w, r)
			})
		})
	}, e.svc.Routes)
}

func TestRoutes(t *testing.T) {
	e := newEnv(t, "sony_headphones", true) // not set up yet
	srv := e.server(e.hid)
	itemID := e.draft()
	e.photo(itemID, "item")

	status, body := httpapitest.Do(t, srv, "POST", "/api/v1/items/"+itemID+"/ai", `{}`)
	if status != 409 || !strings.Contains(body, `"not_set_up"`) {
		t.Fatalf("not set up: %d %s", status, body)
	}
	status, body = httpapitest.Do(t, srv, "POST", "/api/v1/items/ai/read-waiting", "")
	if status != 409 || !strings.Contains(body, `"not_set_up"`) {
		t.Fatalf("read-waiting not set up: %d %s", status, body)
	}
}

func TestRoutesWithAI(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	srv := e.server(e.hid)
	itemID := e.draft()
	e.photo(itemID, "item")

	// No body is fine; a second call finds the queued run.
	status, body := httpapitest.Do(t, srv, "POST", "/api/v1/items/"+itemID+"/ai", "")
	var first queuedJSON
	json.Unmarshal([]byte(body), &first)
	if status != 202 || !first.Queued || first.JobID == "" {
		t.Fatalf("%d %s", status, body)
	}
	if st := state(e.item(itemID)); st != StateQueued {
		t.Fatal(st)
	}
	status, body = httpapitest.Do(t, srv, "POST", "/api/v1/items/"+itemID+"/ai", `{}`)
	var second queuedJSON
	json.Unmarshal([]byte(body), &second)
	if status != 202 || second.Queued || second.JobID != first.JobID {
		t.Fatalf("%d %s", status, body)
	}
	// A hint always queues a run and travels in the payload.
	status, body = httpapitest.Do(t, srv, "POST", "/api/v1/items/"+itemID+"/ai", `{"hint":"2021 model"}`)
	var third queuedJSON
	json.Unmarshal([]byte(body), &third)
	if status != 202 || !third.Queued || third.JobID == first.JobID {
		t.Fatalf("%d %s", status, body)
	}
	var payload string
	e.d.Read.QueryRow(`SELECT payload FROM jobs WHERE id = ?`, third.JobID).Scan(&payload)
	if !strings.Contains(payload, "2021 model") {
		t.Fatal(payload)
	}
	if s, _ := httpapitest.Do(t, srv, "POST", "/api/v1/items/"+itemID+"/ai", `{"hint":"`+strings.Repeat("x", 400)+`"}`); s != 400 {
		t.Fatalf("long hint: %d", s)
	}
	if s, _ := httpapitest.Do(t, srv, "POST", "/api/v1/items/"+itemID+"/ai", `{"nope":1}`); s != 400 {
		t.Fatalf("unknown field: %d", s)
	}

	// Another household's item looks absent; a missing one too.
	other := e.server(id.New())
	if s, b := httpapitest.Do(t, other, "POST", "/api/v1/items/"+itemID+"/ai", `{}`); s != 404 {
		t.Fatalf("other household: %d %s", s, b)
	}

	// A saved item is not read.
	saved := e.draft()
	err := inventory.InTx(e.ctx, e.d, func(tx *sql.Tx) error {
		_, err := inventory.ConfirmItem(e.ctx, tx, e.hid, "", saved)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if s, b := httpapitest.Do(t, srv, "POST", "/api/v1/items/"+saved+"/ai", `{}`); s != 409 || !strings.Contains(b, "not_a_draft") {
		t.Fatalf("saved item: %d %s", s, b)
	}

	// Paused.
	e.d.Write.Exec(`UPDATE households SET ai_paused = 1 WHERE id = ?`, e.hid)
	if s, b := httpapitest.Do(t, srv, "POST", "/api/v1/items/"+itemID+"/ai", `{}`); s != 409 || !strings.Contains(b, `"paused"`) {
		t.Fatalf("paused: %d %s", s, b)
	}
	if s, b := httpapitest.Do(t, srv, "POST", "/api/v1/items/ai/read-waiting", ""); s != 409 || !strings.Contains(b, `"paused"`) {
		t.Fatalf("read-waiting paused: %d %s", s, b)
	}
}

func TestReadWaiting(t *testing.T) {
	e := newEnv(t, "sony_headphones", false)
	srv := e.server(e.hid)

	waiting1 := e.draft()
	e.photo(waiting1, "item")
	waiting2 := e.draft()
	e.photo(waiting2, "label")
	_ = e.draft() // no photo: nothing to read
	read := e.draft()
	e.photo(read, "item")
	if _, err := e.svc.run(e.ctx, e.job(read, 1)); err != nil { // read once already
		t.Fatal(err)
	}
	queued := e.draft()
	e.photo(queued, "item")
	e.enqueue(queued) // already waiting for a run

	status, body := httpapitest.Do(t, srv, "POST", "/api/v1/items/ai/read-waiting", "")
	if status != 202 || body != "{\"queued\":2}\n" {
		t.Fatalf("%d %q", status, body)
	}
	for _, id := range []string{waiting1, waiting2} {
		if e.jobCount(id, "queued") != 1 || state(e.item(id)) != StateQueued {
			t.Fatalf("%s not queued", id)
		}
	}
	if e.jobCount(queued, "queued") != 1 || e.jobCount(read, "queued") != 0 {
		t.Fatal("wrong items queued")
	}
	// Idempotent: nothing left to queue.
	if _, body := httpapitest.Do(t, srv, "POST", "/api/v1/items/ai/read-waiting", ""); body != "{\"queued\":0}\n" {
		t.Fatal(body)
	}
}
