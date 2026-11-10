package textitems

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/ai/prompt/prompttest"
	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
	"github.com/kaanemec/things-items-inventory/internal/id"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
)

type env struct {
	srv     *httptest.Server
	placeID string
	answer  json.RawMessage
	reqs    int
}

func newEnv(t *testing.T, withAI bool) *env {
	d := dbtest.New(t)
	e := &env{answer: prompttest.Answer("text_to_items", "sentence_box12")}
	hid := id.New()
	ep := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		e.reqs++
		out, _ := json.Marshal(map[string]any{"model": "m", "choices": []any{map[string]any{"message": map[string]any{"content": string(e.answer)}}}})
		w.Write(out)
	}))
	t.Cleanup(ep.Close)
	cfg := config.Config{}
	if withAI {
		cfg.AI.Text = config.AIEndpoint{BaseURL: ep.URL, Token: "tok", Model: "tm"}
	}
	ctx := context.Background()
	const stamp = "2026-01-01T00:00:00.000Z"
	if _, err := sqlc.New(d.Write).CreateHousehold(ctx, sqlc.CreateHouseholdParams{ID: hid, Name: "H", Language: "en", Currency: "EUR", CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
		t.Fatal(err)
	}
	err := inventory.InTx(ctx, d, func(tx *sql.Tx) error {
		h, err := inventory.CreateHome(ctx, tx, hid, "Home", "")
		if err != nil {
			return err
		}
		p, err := inventory.CreatePlace(ctx, tx, hid, inventory.PlaceInput{HomeID: h.ID, Kind: "room", Name: "Study"})
		e.placeID = p.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(d, ai.NewService(d, cfg))
	e.srv = httpapitest.Server(t, func(mux *httpapi.Mux) {
		mux.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{UserID: "u1", HouseholdID: hid, Role: auth.RoleMember, Source: auth.SourceSession})))
			})
		})
	}, svc.Routes)
	return e
}

func (e *env) post(t *testing.T, text string) (int, response) {
	body, _ := json.Marshal(map[string]string{"text": text})
	status, raw := httpapitest.Do(t, e.srv, "POST", "/api/v1/ai/text-to-items", string(body))
	var out response
	json.Unmarshal([]byte(raw), &out)
	return status, out
}

func TestSentenceGivesRowsInThePlace(t *testing.T) {
	e := newEnv(t, true)
	status, out := e.post(t, "three hdmi cables, the old kindle and two pairs of ski gloves in box 12")
	if status != 200 || !out.AI || len(out.Rows) != 3 {
		t.Fatalf("%d %+v", status, out)
	}
	if out.Rows[0].Values["quantity"] != float64(3) || out.Rows[2].Values["quantity"] != float64(2) {
		t.Fatalf("%+v", out.Rows)
	}
	for _, r := range out.Rows {
		if r.PlaceID == nil || *r.PlaceID != e.placeID {
			t.Fatalf("place %v want %s", r.PlaceID, e.placeID)
		}
	}
}

func TestInventedPlaceIsDropped(t *testing.T) {
	e := newEnv(t, true)
	e.answer = json.RawMessage(strings.ReplaceAll(string(e.answer), `"p1"`, `"p9"`))
	_, out := e.post(t, "three hdmi cables")
	if len(out.Rows) != 3 {
		t.Fatalf("%+v", out)
	}
	for _, r := range out.Rows {
		if r.PlaceID != nil {
			t.Fatalf("invented place kept: %s", *r.PlaceID)
		}
	}
}

func TestWithoutAIOneRowPerLine(t *testing.T) {
	e := newEnv(t, false)
	text := "- 3x hdmi cable\nkindle\n\nusb stick x2\n4. ski gloves\n2019 macbook"
	status, out := e.post(t, text)
	if status != 200 || out.AI || len(out.Rows) != 5 {
		t.Fatalf("%d %+v", status, out)
	}
	if out.Rows[0].Values["name"] != "hdmi cable" || out.Rows[0].Values["quantity"] != float64(3) ||
		out.Rows[2].Values["name"] != "usb stick" || out.Rows[2].Values["quantity"] != float64(2) ||
		out.Rows[4].Values["name"] != "2019 macbook" || out.Rows[4].Values["quantity"] != nil {
		t.Fatalf("%+v", out.Rows)
	}
}

func TestBadText(t *testing.T) {
	e := newEnv(t, true)
	if s, _ := e.post(t, "  "); s != 422 {
		t.Fatal(s)
	}
	if s, _ := e.post(t, strings.Repeat("a", MaxChars+1)); s != 422 {
		t.Fatal(s)
	}
	if e.reqs != 0 {
		t.Fatal("AI called for bad text")
	}
}

func TestBusyHouseholdGetsFriendly429(t *testing.T) {
	svc := New(nil, nil)
	r1, _, ok1 := svc.acquire("h", "u1")
	_, _, ok2 := svc.acquire("h", "u2")
	_, wait, ok3 := svc.acquire("h", "u3")
	if !ok1 || !ok2 || ok3 || wait == 0 {
		t.Fatal(ok1, ok2, ok3, wait)
	}
	r1()
	if _, _, ok := svc.acquire("h", "u3"); !ok {
		t.Fatal("slot not freed")
	}
	for i := 0; i < perUserMax; i++ {
		svc.acquire("other", "u9")
		if r, _, ok := svc.acquire("x"+string(rune('a'+i)), "u9"); ok {
			r()
		}
	}
	if _, _, ok := svc.acquire("zz", "u9"); ok {
		t.Fatal("per-user limit not applied")
	}
}
