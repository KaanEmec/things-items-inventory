// Package textitems turns free text into item rows (J7.3):
// POST /api/v1/ai/text-to-items sends a sentence, a pasted list or an order
// confirmation to the "everything else" AI role and returns rows for the
// quick-add sheet. Nothing is saved. Without AI set up the text is split
// into one row per line with plain code.
package textitems

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/ai/prompt"
	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

// MaxChars is the longest text one request takes; the client splits longer
// text at line breaks and sends the parts one after the other.
const MaxChars = 12000

const (
	maxPlacesSent = 300
	timeout       = 90 * time.Second
)

// Limits: at most maxInFlight AI calls at once per household, and perUserMax
// requests per perUserWindow per person.
const (
	maxInFlight   = 2
	perUserMax    = 20
	perUserWindow = time.Minute
)

// Service serves the endpoint.
type Service struct {
	d  *db.DB
	ai *ai.Service

	mu       sync.Mutex
	inFlight map[string]int         // household -> running calls
	hits     map[string][]time.Time // user -> recent request times
}

// New returns the service.
func New(d *db.DB, a *ai.Service) *Service {
	return &Service{d: d, ai: a, inFlight: map[string]int{}, hits: map[string][]time.Time{}}
}

// acquire takes an in-flight slot for the household and counts the request
// against the person's rate. It returns a release func, or the seconds to
// wait and ok false.
func (s *Service) acquire(hid, uid string) (release func(), wait int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	h := s.hits[uid]
	for len(h) > 0 && now.Sub(h[0]) >= perUserWindow {
		h = h[1:]
	}
	if len(h) >= perUserMax {
		s.hits[uid] = h
		return nil, int((perUserWindow - now.Sub(h[0])).Seconds()) + 1, false
	}
	if s.inFlight[hid] >= maxInFlight {
		return nil, 5, false
	}
	s.hits[uid] = append(h, now)
	s.inFlight[hid]++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.inFlight[hid]--; s.inFlight[hid] <= 0 {
			delete(s.inFlight, hid)
		}
	}, 0, true
}

// Routes registers POST /api/v1/ai/text-to-items.
//
// Request (member): {"text": "..."} up to MaxChars characters.
// Response (200): {"ai": true, "rows": [{"source": "...", "values": {"name":
// "HDMI cable", "quantity": 3, "category": "part", "brand": ..., "attributes.size": ...,
// "purchase_date": "2025-03-14", "price_minor": 1999, "currency": "EUR", "seller": ...},
// "confidence": {"name": "likely", ...}, "place_id": "<real id>" | null}]}.
// "ai": false means AI is not set up and the rows are plain line splits
// (name and quantity only). Unknown places are dropped (place_id null).
// Errors: 409 ai_paused, 422 text too long or empty, 502 ai_failed.
func (s *Service) Routes(mux *httpapi.Mux) {
	mux.HandleFunc("POST /api/v1/ai/text-to-items", auth.Require(auth.ActionInventoryWrite, s.handle))
}

// Row is one suggested row.
type Row struct {
	Source     string            `json:"source"`
	Values     map[string]any    `json:"values"`
	Confidence map[string]string `json:"confidence"`
	PlaceID    *string           `json:"place_id"`
}

type response struct {
	AI   bool  `json:"ai"`
	Rows []Row `json:"rows"`
}

func (s *Service) handle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		httpapi.WriteError(w, httpapi.Unprocessable("Type or paste some text first.", map[string]string{"text": "Required."}))
		return
	}
	if len([]rune(text)) > MaxChars {
		httpapi.WriteError(w, httpapi.Unprocessable(fmt.Sprintf("Send at most %d characters at a time.", MaxChars), map[string]string{"text": "Too long."}))
		return
	}
	hid := auth.HouseholdID(r.Context())
	release, wait, ok := s.acquire(hid, auth.UserID(r.Context()))
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(wait))
		httpapi.WriteJSON(w, http.StatusTooManyRequests, struct {
			Error *httpapi.Error `json:"error"`
		}{httpapi.NewError(httpapi.CodeRateLimited, "The AI is busy with other requests. Try again in a moment.")})
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	rows, aiUsed, err := s.convert(ctx, hid, text)
	if err != nil {
		writeError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, response{AI: aiUsed, Rows: rows})
}

// convert asks the AI, or splits the lines when AI is not set up.
func (s *Service) convert(ctx context.Context, hid, text string) ([]Row, bool, error) {
	g, err := s.grounding(ctx, hid)
	if err != nil {
		return nil, false, err
	}
	in := prompt.TextToItemsInput{Grounding: g, Text: text}
	t := prompt.TextToItems
	res, err := s.ai.Run(ctx, hid, ai.RoleText, ai.FeatureTextToItems, ai.Request{
		System: t.System, User: t.User(in), SchemaName: t.Name, Schema: t.Schema, MaxTokens: t.MaxTokens,
	}, t.ValidateFor(in), ai.Ref{})
	if errors.Is(err, ai.ErrNotSetUp) {
		return PlainRows(text), false, nil
	}
	if err != nil {
		return nil, false, err
	}
	ans, err := t.DecodeFor(in, res.JSON)
	if err != nil {
		return nil, false, &ai.Error{Kind: ai.KindInvalidOutput, Message: "The AI's answer could not be used."}
	}
	rows := make([]Row, 0, len(ans.Rows))
	for i := range ans.Rows {
		r := &ans.Rows[i]
		f := r.Fields(g.Currency)
		row := Row{Source: r.Source, Values: f.Values, Confidence: map[string]string{}}
		for p, c := range f.Confidence {
			row.Confidence[p] = string(c)
		}
		if id, ok := r.Place(g); ok { // an invented place is dropped
			row.PlaceID = &id
		}
		rows = append(rows, row)
	}
	return rows, true, nil
}

func (s *Service) grounding(ctx context.Context, hid string) (prompt.Grounding, error) {
	g := prompt.Grounding{}
	if err := s.d.Read.QueryRowContext(ctx, `SELECT language, currency FROM households WHERE id = ?`, hid).Scan(&g.Language, &g.Currency); err != nil {
		return g, err
	}
	list := func(q string) ([]string, error) {
		rows, err := s.d.Read.QueryContext(ctx, q, hid)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, rows.Err()
	}
	var err error
	if g.Brands, err = list(`SELECT brand FROM items WHERE household_id = ? AND deleted_at IS NULL AND brand IS NOT NULL AND brand <> ''
		GROUP BY brand ORDER BY COUNT(*) DESC, brand LIMIT 40`); err != nil {
		return g, err
	}
	if g.Names, err = list(`SELECT name FROM items WHERE household_id = ? AND deleted_at IS NULL AND draft = 0
		GROUP BY name ORDER BY COUNT(*) DESC, name LIMIT 40`); err != nil {
		return g, err
	}
	rows, err := s.d.Read.QueryContext(ctx, `SELECT id, path FROM places WHERE household_id = ? AND deleted_at IS NULL
		ORDER BY (box_number IS NULL), box_number, path LIMIT ?`, hid, maxPlacesSent)
	if err != nil {
		return g, err
	}
	defer rows.Close()
	for rows.Next() {
		var p prompt.Place
		if err := rows.Scan(&p.ID, &p.Path); err != nil {
			return g, err
		}
		g.Places = append(g.Places, p)
	}
	return g, rows.Err()
}

var (
	bulletRe   = regexp.MustCompile(`^\s*(?:[-*•·]|\d{1,3}[.)])\s+`)
	leadQtyRe  = regexp.MustCompile(`^(\d{1,3})\s*[x×]\s+(.+)$`)
	trailQtyRe = regexp.MustCompile(`^(.+?)\s+[x×]\s*(\d{1,3})$`)
)

// PlainRows is the no-AI split: one row per non-empty line, with a leading
// or trailing quantity ("3x hdmi cable", "hdmi cable x3").
func PlainRows(text string) []Row {
	rows := []Row{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(bulletRe.ReplaceAllString(line, ""))
		if line == "" {
			continue
		}
		name, qty := line, int64(0)
		if m := leadQtyRe.FindStringSubmatch(line); m != nil {
			name, qty = m[2], atoi(m[1])
		} else if m := trailQtyRe.FindStringSubmatch(line); m != nil {
			name, qty = m[1], atoi(m[2])
		}
		if len(name) > 200 {
			name = name[:200]
		}
		v := map[string]any{"name": strings.TrimSpace(name)}
		if qty > 0 {
			v["quantity"] = qty
		}
		rows = append(rows, Row{Source: line, Values: v, Confidence: map[string]string{}})
	}
	return rows
}

func atoi(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

func writeError(w http.ResponseWriter, err error) {
	var ae *ai.Error
	fail := func(status int, code, msg string) {
		httpapi.WriteJSON(w, status, struct {
			Error *httpapi.Error `json:"error"`
		}{httpapi.NewError(code, msg)})
	}
	switch {
	case errors.Is(err, ai.ErrPaused):
		fail(http.StatusConflict, "ai_paused", "AI is paused for this household.")
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ae) && ae.Kind == ai.KindTimeout:
		fail(http.StatusBadGateway, "ai_failed", "The AI did not answer in time.")
	case errors.Is(err, context.Canceled):
		fail(http.StatusBadGateway, "ai_failed", "The request was cancelled.")
	case errors.As(err, &ae):
		fail(http.StatusBadGateway, "ai_failed", ae.Message)
	default:
		httpapi.WriteError(w, err)
	}
}
