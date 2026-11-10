// Package csvimport imports a CSV or TSV file as draft items (J7.5). The
// server keeps no upload: each step sends the file again.
//
//	POST /api/v1/imports/preview  multipart: file            -> encoding, delimiter, header, samples, proposed mapping
//	POST /api/v1/imports/dry-run  multipart: file, mapping   -> what would be added, skipped, unmatched places
//	POST /api/v1/imports          multipart: file, mapping   -> 202 {import_id, job_id, rows}; background job csv_import
//	POST /api/v1/imports/{id}/undo                           -> removes exactly the items that import added
//
// Imported items are drafts with source "import" and import_id set. Only the
// header and a few sample rows go to the AI, never the whole file.
package csvimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/ai/prompt"
	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
	"github.com/kaanemec/things-items-inventory/internal/search"
)

// Kind is the job kind.
const Kind = "csv_import"

const (
	maxFileBytes = 8 << 20
	maxRows      = 20000
	sampleRows   = 5
	batch        = 100
)

// Service owns the routes and the job handler.
type Service struct {
	d  *db.DB
	ai *ai.Service
	q  *jobs.Queue
}

// New creates the service and registers the csv_import handler on q.
func New(d *db.DB, a *ai.Service, q *jobs.Queue) *Service {
	s := &Service{d: d, ai: a, q: q}
	q.Register(Kind, jobs.Handler{Timeout: 10 * time.Minute, MaxAttempts: 3, Run: s.run})
	return s
}

// Routes registers the import routes.
func (s *Service) Routes(mux *httpapi.Mux) {
	mux.HandleFunc("POST /api/v1/imports/preview", auth.RequireSession(auth.ActionInventoryWrite, s.preview))
	mux.HandleFunc("POST /api/v1/imports/dry-run", auth.RequireSession(auth.ActionInventoryWrite, s.dryRun))
	mux.HandleFunc("POST /api/v1/imports", auth.RequireSession(auth.ActionInventoryWrite, s.start))
	mux.HandleFunc("POST /api/v1/imports/{id}/undo", auth.RequireSession(auth.ActionInventoryWrite, s.undo))
}

// file is an upload read and parsed.
type file struct {
	encoding string
	delim    rune
	records  [][]string
}

func readUpload(w http.ResponseWriter, r *http.Request, wantMapping bool) (f file, m Mapping, err error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileBytes+(1<<20))
	if err := r.ParseMultipartForm(maxFileBytes); err != nil {
		return f, m, httpapi.BadRequest("Upload the file as multipart form data (file, mapping).")
	}
	up, _, err := r.FormFile("file")
	if err != nil {
		return f, m, httpapi.Unprocessable("Choose a CSV or TSV file.", map[string]string{"file": "Required."})
	}
	defer up.Close()
	raw, err := io.ReadAll(io.LimitReader(up, maxFileBytes+1))
	if err != nil || len(raw) > maxFileBytes {
		return f, m, httpapi.Unprocessable("The file is too large (8 MB at most).", map[string]string{"file": "Too large."})
	}
	text, enc := decode(raw)
	f.encoding = enc
	if wantMapping {
		if err := json.Unmarshal([]byte(r.FormValue("mapping")), &m); err != nil {
			return f, m, httpapi.Unprocessable("The mapping is not valid JSON.", map[string]string{"mapping": "Invalid."})
		}
		if err := m.validate(); err != nil {
			return f, m, httpapi.Unprocessable(err.Error()+".", map[string]string{"mapping": err.Error()})
		}
	}
	f.delim = sniffDelimiter(text)
	if wantMapping && m.Delimiter != "" {
		f.delim = []rune(m.Delimiter)[0]
	}
	if f.records, err = parse(text, f.delim); err != nil || len(f.records) == 0 {
		return f, m, httpapi.Unprocessable("The file could not be read as CSV.", map[string]string{"file": "Unreadable."})
	}
	if len(f.records) > maxRows+1 {
		return f, m, httpapi.Unprocessable("The file has too many rows (20,000 at most).", map[string]string{"file": "Too many rows."})
	}
	return f, m, nil
}

type previewResponse struct {
	Encoding  string     `json:"encoding"`
	Delimiter string     `json:"delimiter"`
	HasHeader bool       `json:"has_header"`
	Header    []string   `json:"header"`
	Rows      [][]string `json:"rows"` // the first rows
	RowCount  int        `json:"row_count"`
	AI        bool       `json:"ai"` // the mapping is the AI's proposal
	Mapping   Mapping    `json:"mapping"`
}

func (s *Service) preview(w http.ResponseWriter, r *http.Request) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	f, _, err := readUpload(w, r, false)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	hasHeader := looksLikeHeader(f.records[0])
	var header []string
	data := f.records
	if hasHeader {
		header, data = f.records[0], f.records[1:]
	}
	samples := data[:min(len(data), sampleRows)]
	out := previewResponse{Encoding: f.encoding, Delimiter: string(f.delim), HasHeader: hasHeader, Header: header,
		Rows: samples, RowCount: len(data), Mapping: HeuristicMapping(header, samples)}
	if m, ok := s.aiMapping(ctx, hid, header, samples); ok {
		out.Mapping, out.AI = m, true
	}
	out.Mapping.HasHeader, out.Mapping.Delimiter = hasHeader, string(f.delim)
	if out.Header == nil {
		out.Header = []string{}
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// aiMapping asks the "everything else" role for a mapping from the header and
// a few sample rows; ok is false when AI is unavailable or answers badly.
func (s *Service) aiMapping(ctx context.Context, hid string, header []string, samples [][]string) (Mapping, bool) {
	var g prompt.Grounding
	if err := s.d.Read.QueryRowContext(ctx, `SELECT language, currency FROM households WHERE id = ?`, hid).Scan(&g.Language, &g.Currency); err != nil {
		return Mapping{}, false
	}
	in := prompt.CSVMappingInput{Grounding: g, Header: header, Rows: samples}
	t := prompt.CSVMapping
	res, err := s.ai.Run(ctx, hid, ai.RoleText, ai.FeatureCSVMapping, ai.Request{
		System: t.System, User: t.User(in), SchemaName: t.Name, Schema: t.Schema, MaxTokens: t.MaxTokens,
	}, t.ValidateFor(in), ai.Ref{})
	if err != nil {
		return Mapping{}, false
	}
	ans, err := t.DecodeFor(in, res.JSON)
	if err != nil {
		return Mapping{}, false
	}
	m := Mapping{DateFormat: "YYYY-MM-DD", DecimalComma: ans.DecimalComma}
	if ans.DateFormat != nil {
		m.DateFormat = *ans.DateFormat
	}
	if ans.Category != nil {
		m.Category = *ans.Category
	}
	width := len(ans.Columns)
	for i := 0; i < width; i++ {
		c, _ := ans.Column(i)
		col := Column{Target: c.Target, Confidence: string(c.Confidence)}
		if c.Attribute != nil {
			col.Attribute = *c.Attribute
		}
		m.Columns = append(m.Columns, col)
	}
	if m.validate() != nil {
		return Mapping{}, false
	}
	return m, true
}

func (s *Service) places(ctx context.Context, hid string) ([]Place, string, error) {
	var cur string
	if err := s.d.Read.QueryRowContext(ctx, `SELECT currency FROM households WHERE id = ?`, hid).Scan(&cur); err != nil {
		return nil, "", err
	}
	rows, err := s.d.Read.QueryContext(ctx, `SELECT id, path, name FROM places WHERE household_id = ? AND deleted_at IS NULL`, hid)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []Place
	for rows.Next() {
		var p Place
		if err := rows.Scan(&p.ID, &p.Path, &p.Name); err != nil {
			return nil, "", err
		}
		out = append(out, p)
	}
	return out, cur, rows.Err()
}

// prepare reads the upload and the mapping into items.
func (s *Service) prepare(w http.ResponseWriter, r *http.Request) (Prepared, error) {
	f, m, err := readUpload(w, r, true)
	if err != nil {
		return Prepared{}, err
	}
	var header []string
	data, first := f.records, 1
	if m.HasHeader {
		header, data, first = f.records[0], f.records[1:], 2
	}
	places, cur, err := s.places(r.Context(), auth.HouseholdID(r.Context()))
	if err != nil {
		return Prepared{}, err
	}
	return Prepare(m, header, data, first, places, cur, id.New), nil
}

type dryRunResponse struct {
	Rows     int       `json:"rows"`
	WillAdd  int       `json:"will_add"`
	Skipped  []Problem `json:"skipped"`  // not imported, with the reason
	Warnings []Problem `json:"warnings"` // imported, but a value was left out
	// Duplicates are rows that look like an item the household already has.
	Duplicates []duplicate      `json:"duplicates"`
	Unmatched  []unmatchedPlace `json:"unmatched_places"`
}

type unmatchedPlace struct {
	Value string `json:"value"`
	Rows  int    `json:"rows"`
}

type duplicate struct {
	Row     int        `json:"row"`
	Name    string     `json:"name"`
	Matches []dupMatch `json:"matches"`
}

type dupMatch struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
	Why   string `json:"why"` // brand_model | name
}

func fieldText(f map[string]json.RawMessage, k string) string {
	var v string
	json.Unmarshal(f[k], &v)
	return v
}

func (s *Service) dryRun(w http.ResponseWriter, r *http.Request) {
	p, err := s.prepare(w, r)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	out := dryRunResponse{Rows: len(p.Items) + len(p.Skipped), WillAdd: len(p.Items), Skipped: nz(p.Skipped), Warnings: nz(p.Warnings)}
	out.Duplicates = []duplicate{}
	out.Unmatched = []unmatchedPlace{}
	for _, it := range p.Items {
		ms, err := search.Similar(r.Context(), s.d.Read, auth.HouseholdID(r.Context()), fieldText(it.Fields, "brand"),
			fieldText(it.Fields, "model"), fieldText(it.Fields, "name"))
		if err != nil {
			httpapi.WriteError(w, err)
			return
		}
		if len(ms) > 0 {
			d := duplicate{Row: it.Row, Name: fieldText(it.Fields, "name"), Matches: []dupMatch{}}
			for _, m := range ms[:min(len(ms), 3)] {
				d.Matches = append(d.Matches, dupMatch{m.ID, m.Title, m.Path, m.Why})
			}
			out.Duplicates = append(out.Duplicates, d)
		}
	}
	for _, v := range slices.Sorted(keys(p.Unmatched)) {
		out.Unmatched = append(out.Unmatched, unmatchedPlace{v, p.Unmatched[v]})
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func nz(p []Problem) []Problem {
	if p == nil {
		return []Problem{}
	}
	return p
}

func keys(m map[string]int) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// Payload is the csv_import job payload.
type Payload struct {
	ImportID string         `json:"import_id"`
	Items    []PreparedItem `json:"items"`
}

// Result is the csv_import job result.
type Result struct {
	ImportID string   `json:"import_id"`
	Added    int      `json:"added"`
	Failed   int      `json:"failed"`
	Errors   []string `json:"errors,omitempty"`
}

func (s *Service) start(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hid, uid := auth.HouseholdID(ctx), auth.UserID(ctx)
	f, m, err := readUpload(w, r, true)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	var header []string
	data, first := f.records, 1
	if m.HasHeader {
		header, data, first = f.records[0], f.records[1:], 2
	}
	places, cur, err := s.places(ctx, hid)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	p := Prepare(m, header, data, first, places, cur, id.New)
	if len(p.Items) == 0 {
		httpapi.WriteError(w, httpapi.Unprocessable("No row has a name, so there is nothing to import.", map[string]string{"mapping": "No rows."}))
		return
	}
	payload := Payload{ImportID: id.New(), Items: p.Items}
	var jobID string
	err = inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
		resolved, err := s.resolvePlaces(ctx, tx, hid, m.Places, p.Unmatched)
		if err != nil {
			return err
		}
		for i, it := range payload.Items {
			if pid := resolved[it.Place]; it.Place != "" && pid != "" {
				payload.Items[i].Fields["place_id"], _ = json.Marshal(pid)
			}
			payload.Items[i].Place = ""
		}
		b, _ := json.Marshal(payload)
		jobID, err = s.q.Enqueue(ctx, tx, jobs.Job{HouseholdID: hid, Kind: Kind, Payload: b, CreatedBy: uid})
		return err
	})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusAccepted, map[string]any{"import_id": payload.ImportID, "job_id": jobID, "rows": len(payload.Items)})
}

// resolvePlaces turns the person's choices for unmatched place values into
// place IDs: an existing place of the household, or "create" (a new room in
// the first home). Values without a choice get no place.
func (s *Service) resolvePlaces(ctx context.Context, tx *sql.Tx, hid string, choices map[string]string, unmatched map[string]int) (map[string]string, error) {
	out := map[string]string{}
	var homeID string
	for _, v := range slices.Sorted(keys(unmatched)) {
		c := choices[v]
		switch {
		case c == "":
		case c == "create":
			if homeID == "" {
				homes, err := sqlc.New(tx).ListHomes(ctx, sqlc.ListHomesParams{HouseholdID: hid})
				if err != nil || len(homes) == 0 {
					return nil, httpapi.Conflict("Create a home first.")
				}
				homeID = homes[0].ID
			}
			pl, err := inventory.CreatePlace(ctx, tx, hid, inventory.PlaceInput{HomeID: homeID, Kind: "room", Name: v})
			if err != nil {
				return nil, err
			}
			out[v] = pl.ID
		default:
			pl, err := inventory.GetPlace(ctx, tx, hid, c)
			if err != nil {
				return nil, httpapi.Unprocessable("A chosen place does not exist.", map[string]string{"places": "Unknown place."})
			}
			out[v] = pl.ID
		}
	}
	return out, nil
}

// run is the csv_import handler: it creates the items in batches. A retry
// finds the rows it already made by their IDs.
func (s *Service) run(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var p Payload
	if err := json.Unmarshal(job.Payload, &p); err != nil || p.ImportID == "" {
		return nil, jobs.Permanent(errors.New("csv_import: bad payload"))
	}
	res := Result{ImportID: p.ImportID}
	for start := 0; start < len(p.Items); start += batch {
		part := p.Items[start:min(start+batch, len(p.Items))]
		err := inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
			for _, it := range part {
				// A savepoint keeps one bad row from failing the batch.
				if _, err := tx.ExecContext(ctx, `SAVEPOINT row`); err != nil {
					return err
				}
				_, created, err := inventory.CreateItem(ctx, tx, job.HouseholdID, job.CreatedBy, inventory.NewItem{
					ID: it.ID, Source: "import", ImportID: p.ImportID, Fields: it.Fields})
				if err != nil {
					if _, e := tx.ExecContext(ctx, `ROLLBACK TO row`); e != nil {
						return e
					}
					res.Failed++
					if len(res.Errors) < 20 {
						res.Errors = append(res.Errors, strings.TrimSpace(err.Error()))
					}
				} else if created {
					res.Added++
				}
				if _, err := tx.ExecContext(ctx, `RELEASE row`); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return json.Marshal(res)
}

// undo removes exactly the items the import added (soft delete, one history
// event each). It refuses while the import's job is still running.
func (s *Service) undo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hid, uid, imp := auth.HouseholdID(ctx), auth.UserID(ctx), r.PathValue("id")
	var running int
	if err := s.d.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE household_id = ? AND kind = ? AND state IN ('queued', 'running')
		AND json_extract(payload, '$.import_id') = ?`, hid, Kind, imp).Scan(&running); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if running > 0 {
		httpapi.WriteError(w, httpapi.Conflict("The import is still running. Undo it when it has finished."))
		return
	}
	var known int
	if err := s.d.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE household_id = ? AND import_id = ?`, hid, imp).Scan(&known); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if known == 0 {
		httpapi.WriteError(w, httpapi.NotFound("That import does not exist."))
		return
	}
	var removed int
	err := inventory.InTx(ctx, s.d, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM items WHERE household_id = ? AND import_id = ? AND deleted_at IS NULL`, hid, imp)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := inventory.DeleteItem(ctx, tx, hid, uid, id); err != nil {
				return err
			}
		}
		removed = len(ids)
		return nil
	})
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]int{"removed": removed})
}

// Seed creates an imported item in the household for the cross-household test.
func (s *Service) Seed(ctx context.Context, d *db.DB, hid string) (pathValues, markers []string, err error) {
	imp := id.New()
	err = inventory.InTx(ctx, d, func(tx *sql.Tx) error {
		_, _, err := inventory.CreateItem(ctx, tx, hid, "", inventory.NewItem{Source: "import", ImportID: imp,
			Fields: inventory.Fields{"name": []byte(`"Secret-Import-4K"`)}})
		return err
	})
	return []string{imp}, []string{"Secret-Import-4K"}, err
}
