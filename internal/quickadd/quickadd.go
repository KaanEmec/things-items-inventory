// Package quickadd serves the quick-add sheet's AI row completion (J7.2):
// POST /api/v1/ai/complete-rows turns the rough names a person typed into
// suggestions (tidy name, brand, model, category, attributes) while they
// wait at the cell. It is a direct request, not a background job.
package quickadd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/ai/prompt"
	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/search"
)

// Limits of the endpoint.
const (
	MaxRows         = prompt.MaxRows
	maxNameRunes    = 200
	maxIDLen        = 64
	defaultTimeout  = 8 * time.Second
	defaultPerMin   = 30
	groundingBrands = 40
	groundingNames  = 40
)

// Service holds the cache and the rate limiter of the endpoint.
type Service struct {
	db      *db.DB
	ai      *ai.Service
	cache   *cache
	limit   *limiter
	timeout time.Duration
}

// New returns the service. The cache and the rate limit live in memory and
// are per household.
func New(d *db.DB, aiSvc *ai.Service) *Service {
	return &Service{db: d, ai: aiSvc, cache: newCache(), limit: newLimiter(defaultPerMin, time.Minute), timeout: defaultTimeout}
}

// SetTimeout changes the overall deadline of a request (for tests).
func (s *Service) SetTimeout(d time.Duration) { s.timeout = d }

// Routes registers POST /api/v1/ai/complete-rows.
//
// Request (member): {"rows":[{"id":"…","name":"wh1000xm4 black","values":{"category":"gadget","brand":"Sony",…}}],
// "category":"gadget"}. At most 20 rows. values holds what the person typed
// into the other cells by sheet column key; only its "category" is used,
// as the category the person picked for the row. category (optional) is the
// category picked for every row without its own. Omit both when the person
// has not picked one: the AI then suggests it.
//
// Response (200): {"rows":[{"id":"…","cells":{"name":"Sony WH-1000XM4 headphones","brand":"Sony",
// "model":"WH-1000XM4","category":"gadget","quantity":"2","attr.type":"headphones",…},
// "confidence":{"brand":"likely",…}}]}. Cell keys are the sheet's column keys
// (attributes as attr.<key>); values are text as a cell holds it. Rows the
// AI returned nothing for are omitted.
//
// Errors: 409 ai_not_set_up, 409 ai_paused, 429 rate_limited (Retry-After),
// 502 ai_failed (also when the 8 s deadline passes), 400/422 for bad bodies.
func (s *Service) Routes(mux *httpapi.Mux) {
	mux.HandleFunc("POST /api/v1/ai/complete-rows", auth.Require(auth.ActionInventoryWrite, s.completeRows))
}

type rowIn struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Values map[string]string `json:"values"`
}

type request struct {
	Rows     []rowIn `json:"rows"`
	Category string  `json:"category"`
}

type rowOut struct {
	ID         string            `json:"id"`
	Cells      map[string]string `json:"cells"`
	Confidence map[string]string `json:"confidence"`
}

type response struct {
	Rows []rowOut `json:"rows"`
}

func writeAIError(w http.ResponseWriter, status int, code, msg string) {
	httpapi.WriteJSON(w, status, struct {
		Error *httpapi.Error `json:"error"`
	}{httpapi.NewError(code, msg)})
}

func (s *Service) completeRows(w http.ResponseWriter, r *http.Request) {
	var in request
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if len(in.Rows) > MaxRows {
		httpapi.WriteError(w, httpapi.Unprocessable(fmt.Sprintf("Send at most %d rows at a time.", MaxRows), map[string]string{"rows": "Too many rows."}))
		return
	}
	hid := auth.HouseholdID(r.Context())

	// Work out what to ask: one entry per distinct normalised input.
	type job struct {
		key   string
		typed prompt.TypedRow
		ids   []string // client row ids sharing this input
	}
	var jobs []*job
	byKey := map[string]*job{}
	seen := map[string]bool{}
	out := response{Rows: []rowOut{}}
	answers := map[string]*entry{}
	for _, row := range in.Rows {
		if row.ID == "" || len(row.ID) > maxIDLen || seen[row.ID] {
			httpapi.WriteError(w, httpapi.Unprocessable("Every row needs a short, unique id.", map[string]string{"rows": "Bad row id."}))
			return
		}
		seen[row.ID] = true
		name := strings.Join(strings.Fields(row.Name), " ")
		if name == "" {
			continue
		}
		if rs := []rune(name); len(rs) > maxNameRunes {
			name = string(rs[:maxNameRunes])
		}
		cat := strings.TrimSpace(row.Values["category"])
		if cat == "" {
			cat = strings.TrimSpace(in.Category)
		}
		if _, ok := inventory.CategoryByKey(cat); !ok {
			cat = ""
		}
		key := cacheKey(name, cat)
		if e, ok := s.cache.get(hid, key); ok {
			answers[row.ID] = e
			continue
		}
		j := byKey[key]
		if j == nil {
			j = &job{key: key, typed: prompt.TypedRow{ID: strconv.Itoa(len(jobs) + 1), Text: name, Category: cat}}
			byKey[key] = j
			jobs = append(jobs, j)
		}
		j.ids = append(j.ids, row.ID)
	}

	if len(jobs) > 0 {
		if wait, ok := s.limit.allow(hid); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeAIError(w, http.StatusTooManyRequests, httpapi.CodeRateLimited, "Too many suggestions requested. Try again in a moment.")
			return
		}
		typed := make([]prompt.TypedRow, len(jobs))
		for i, j := range jobs {
			typed[i] = j.typed
		}
		ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
		defer cancel()
		got, err := s.ask(ctx, hid, typed)
		if err != nil {
			s.writeError(w, err)
			return
		}
		for _, j := range jobs {
			e := got[j.typed.ID]
			if e == nil {
				e = &entry{}
			}
			s.cache.put(hid, j.key, e)
			for _, id := range j.ids {
				answers[id] = e
			}
		}
	}

	for _, row := range in.Rows {
		if e := answers[row.ID]; e != nil && len(e.cells) > 0 {
			out.Rows = append(out.Rows, rowOut{ID: row.ID, Cells: e.cells, Confidence: e.confidence})
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (s *Service) writeError(w http.ResponseWriter, err error) {
	var ae *ai.Error
	switch {
	case errors.Is(err, ai.ErrNotSetUp):
		writeAIError(w, http.StatusConflict, "ai_not_set_up", "AI is not set up for this household.")
	case errors.Is(err, ai.ErrPaused):
		writeAIError(w, http.StatusConflict, "ai_paused", "AI is paused for this household.")
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ae) && ae.Kind == ai.KindTimeout:
		writeAIError(w, http.StatusBadGateway, "ai_failed", "The AI did not answer in time.")
	case errors.Is(err, context.Canceled):
		writeAIError(w, http.StatusBadGateway, "ai_failed", "The request was cancelled.")
	case errors.As(err, &ae):
		// ai.Error messages are written for people and never hold the token.
		writeAIError(w, http.StatusBadGateway, "ai_failed", ae.Message)
	default:
		httpapi.WriteError(w, err)
	}
}

// ask sends the rows to the text role in one call and returns the entries by
// TypedRow.ID. Rows the AI had nothing for are absent or empty.
func (s *Service) ask(ctx context.Context, hid string, rows []prompt.TypedRow) (map[string]*entry, error) {
	g, err := s.grounding(ctx, hid)
	if err != nil {
		return nil, err
	}
	in := prompt.RowCompletionInput{Grounding: g, Rows: rows}
	t := prompt.RowCompletion
	res, err := s.ai.Run(ctx, hid, ai.RoleText, ai.FeatureRowCompletion, ai.Request{
		System: t.System, User: t.User(in), SchemaName: t.Name, Schema: t.Schema, MaxTokens: t.MaxTokens,
	}, t.ValidateFor(in), ai.Ref{})
	if err != nil {
		return nil, err
	}
	ans, err := t.DecodeFor(in, res.JSON)
	if err != nil {
		return nil, &ai.Error{Kind: ai.KindInvalidOutput, Message: "The AI's answer could not be used."}
	}
	out := map[string]*entry{}
	for i := range ans.Rows {
		out[ans.Rows[i].ID] = toEntry(&ans.Rows[i], rows)
	}
	return out, nil
}

func (s *Service) grounding(ctx context.Context, hid string) (prompt.Grounding, error) {
	g := prompt.Grounding{}
	if err := s.db.Read.QueryRowContext(ctx, `SELECT language, currency FROM households WHERE id = ?`, hid).Scan(&g.Language, &g.Currency); err != nil {
		return g, err
	}
	list := func(q string, limit int) ([]string, error) {
		rows, err := s.db.Read.QueryContext(ctx, q, hid, limit)
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
		GROUP BY brand ORDER BY COUNT(*) DESC, brand LIMIT ?`, groundingBrands); err != nil {
		return g, err
	}
	g.Names, err = list(`SELECT name FROM items WHERE household_id = ? AND deleted_at IS NULL AND draft = 0
		GROUP BY name ORDER BY COUNT(*) DESC, name LIMIT ?`, groundingNames)
	return g, err
}

// toEntry turns one completed row into sheet cells.
func toEntry(row *prompt.CompletedRow, typed []prompt.TypedRow) *entry {
	f := row.Fields()
	e := &entry{cells: map[string]string{}, confidence: map[string]string{}}
	for path, v := range f.Values {
		text := cellText(v)
		if text == "" {
			continue
		}
		key := path
		if a, ok := strings.CutPrefix(path, "attributes."); ok {
			key = "attr." + a
		}
		e.cells[key] = text
		e.confidence[key] = string(f.Confidence[path])
	}
	// The sheet has no colour column; a category with a colour attribute gets it there.
	if c, ok := e.cells["color"]; ok {
		delete(e.cells, "color")
		cat, _ := f.Values["category"].(string)
		if cat == "" {
			for _, t := range typed {
				if t.ID == row.ID {
					cat = t.Category
				}
			}
		}
		if cd, ok := inventory.CategoryByKey(cat); ok {
			if _, has := cd.Attr("color"); has && e.cells["attr.color"] == "" {
				e.cells["attr.color"] = c
				e.confidence["attr.color"] = e.confidence["color"]
			}
		}
		delete(e.confidence, "color")
	}
	return e
}

func cellText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []string:
		return strings.Join(x, ", ")
	case []any:
		parts := make([]string, 0, len(x))
		for _, p := range x {
			parts = append(parts, cellText(p))
		}
		return strings.Join(parts, ", ")
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func cacheKey(name, category string) string {
	return prompt.RowCompletion.Version + "\x00" + category + "\x00" + search.Fold(name)
}

// Seed puts a recognisable cache entry in household hid for the
// cross-household test, and returns its text. The entry is for a name no
// other household types.
func (s *Service) Seed(_ context.Context, _ *db.DB, hid string) ([]string, error) {
	s.cache.put(hid, cacheKey("b-secret-quickadd-name", ""), &entry{
		cells: map[string]string{"brand": "b-secret-quickadd-brand"}, confidence: map[string]string{"brand": "sure"}})
	return []string{"b-secret-quickadd-brand", "b-secret-quickadd-name"}, nil
}
