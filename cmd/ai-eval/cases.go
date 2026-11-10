package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/ai/prompt"
)

// expectedFile is <name>.expected.json.
type expectedFile struct {
	// Task is the AI feature to run; default item_draft for a photo and
	// text_to_items for text.
	Task      string                `json:"task"`
	Grounding prompt.Grounding      `json:"grounding"`
	Item      map[string]string     `json:"item"`      // label, receipt: what the item is
	Container string                `json:"container"` // contents
	Existing  []prompt.ExistingLine `json:"existing"`  // contents
	// Expect holds the expected values: field path → value for one-object
	// tasks; {"rows": [...]} or {"lines": [...]} for list tasks.
	Expect map[string]json.RawMessage `json:"expect"`
}

type evalCase struct {
	name   string
	task   string
	exp    expectedFile
	images [][]byte           // in request order
	roles  []prompt.PhotoRole // item_draft
	text   string
}

func (c *evalCase) vision() bool { return len(c.images) > 0 }

var tasks = []string{"item_draft", "label", "receipt", "contents", "row_completion", "text_to_items", "csv_mapping"}

func loadCases(dir, only string) ([]*evalCase, error) {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.expected.json"))
	sort.Strings(matches)
	var out []*evalCase
	for _, m := range matches {
		name := strings.TrimSuffix(filepath.Base(m), ".expected.json")
		if only != "" && !strings.Contains(name, only) {
			continue
		}
		c, err := loadCase(dir, name, m)
		if err != nil {
			return nil, fmt.Errorf("case %s: %w", name, err)
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no cases (*.expected.json) in %s", dir)
	}
	return out, nil
}

func loadCase(dir, name, expPath string) (*evalCase, error) {
	raw, err := os.ReadFile(expPath)
	if err != nil {
		return nil, err
	}
	c := &evalCase{name: name}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&c.exp); err != nil {
		return nil, fmt.Errorf("%s: %v", filepath.Base(expPath), err)
	}
	if len(c.exp.Expect) == 0 {
		return nil, fmt.Errorf("%s has no expect", filepath.Base(expPath))
	}
	base := filepath.Join(dir, name)
	if t, err := os.ReadFile(base + ".txt"); err == nil {
		c.text = string(t)
	}
	main := findImage(base)
	if main == "" && c.text == "" {
		return nil, fmt.Errorf("no %s.jpg or %s.txt next to the expected file", name, name)
	}
	c.task = c.exp.Task
	if c.task == "" {
		c.task = "text_to_items"
		if main != "" {
			c.task = "item_draft"
		}
	}
	if !contains(tasks, c.task) {
		return nil, fmt.Errorf("unknown task %q; one of %s", c.task, strings.Join(tasks, ", "))
	}
	photoTask := c.task == "item_draft" || c.task == "label" || c.task == "receipt" || c.task == "contents"
	if photoTask != (main != "") {
		return nil, fmt.Errorf("task %s needs a %s", c.task, map[bool]string{true: "photo (.jpg)", false: "text file (.txt)"}[photoTask])
	}
	if !photoTask {
		return c, nil
	}
	// Most informative first: label, item, receipt (J6.3).
	add := func(path string, role prompt.PhotoRole) error {
		img, err := loadImage(path)
		if err != nil {
			return fmt.Errorf("%s: %v", filepath.Base(path), err)
		}
		c.images = append(c.images, img)
		c.roles = append(c.roles, role)
		return nil
	}
	if c.task == "item_draft" {
		if p := findImage(base + ".label"); p != "" {
			if err := add(p, prompt.PhotoLabel); err != nil {
				return nil, err
			}
		}
	}
	if err := add(main, prompt.PhotoItem); err != nil {
		return nil, err
	}
	if c.task == "item_draft" {
		if p := findImage(base + ".receipt"); p != "" {
			if err := add(p, prompt.PhotoReceipt); err != nil {
				return nil, err
			}
		}
	}
	return c, nil
}

func findImage(base string) string {
	for _, ext := range []string{".jpg", ".jpeg", ".JPG", ".JPEG", ".png", ".PNG"} {
		if _, err := os.Stat(base + ext); err == nil {
			return base + ext
		}
	}
	return ""
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// result is one case's run.
type result struct {
	res ai.Result
	err error
	// flat is the answer by field path, for one-object tasks; rows is the
	// answer's rows for list tasks.
	flat map[string]any
	rows []map[string]any
}

// complete runs one task: request, validation against the request, decode.
func complete[In, Out any](ctx context.Context, cl *ai.Client, cfg ai.Config, t *prompt.Task[In, Out], in In, images [][]byte) (*Out, ai.Result, error) {
	req := ai.Request{System: t.System, User: t.User(in), Images: images, SchemaName: t.Name, Schema: t.Schema, MaxTokens: t.MaxTokens}
	res, err := cl.Complete(ctx, cfg, req, t.ValidateFor(in))
	if err != nil {
		return nil, res, err
	}
	out, err := t.DecodeFor(in, res.JSON)
	return out, res, err
}

func (c *evalCase) run(ctx context.Context, cl *ai.Client, cfg ai.Config) result {
	var r result
	g := c.exp.Grounding
	cur := g.Currency
	switch c.task {
	case "item_draft":
		a, res, err := complete(ctx, cl, cfg, prompt.ItemDraft, prompt.ItemDraftInput{Grounding: g, Photos: c.roles}, c.images)
		r.res, r.err = res, err
		if err == nil {
			r.flat = a.Fields(cur).Values
			if a.Purchase != nil && a.Purchase.WarrantyMonths != nil {
				r.flat["warranty_months"] = a.Purchase.WarrantyMonths.Value
			}
			r.flat["needs"] = needKinds(a.Needs)
		}
	case "label":
		a, res, err := complete(ctx, cl, cfg, prompt.Label, prompt.LabelInput{Grounding: g, Item: c.exp.Item}, c.images)
		r.res, r.err = res, err
		if err == nil {
			r.flat = a.Fields(c.exp.Item["category"]).Values
			if a.Category != nil {
				r.flat["category"] = a.Category.Value
			}
			r.flat["needs"] = needKinds(a.Needs)
		}
	case "receipt":
		a, res, err := complete(ctx, cl, cfg, prompt.Receipt, prompt.ReceiptInput{Grounding: g, Item: c.exp.Item}, c.images)
		r.res, r.err = res, err
		if err == nil {
			r.flat = a.Fields(cur).Values
			if a.ItemLine != nil {
				r.flat["item_line"] = a.ItemLine.Index
			}
			if a.WarrantyMonths != nil {
				r.flat["warranty_months"] = a.WarrantyMonths.Value
			}
			r.flat["line_count"] = int64(len(a.Lines))
		}
	case "contents":
		a, res, err := complete(ctx, cl, cfg, prompt.Contents, prompt.ContentsInput{Grounding: g, Container: c.exp.Container, Existing: c.exp.Existing}, c.images)
		r.res, r.err = res, err
		if err == nil {
			for _, l := range a.Lines {
				row := map[string]any{"name": l.Name, "worth_own_entry": l.WorthOwnEntry}
				if l.Quantity != nil {
					row["quantity"] = *l.Quantity
				}
				if l.Category != nil {
					row["category"] = *l.Category
				}
				r.rows = append(r.rows, row)
			}
		}
	case "row_completion":
		var in prompt.RowCompletionInput
		in.Grounding = g
		for i, line := range nonEmptyLines(c.text) {
			in.Rows = append(in.Rows, prompt.TypedRow{ID: fmt.Sprint(i + 1), Text: line})
		}
		a, res, err := complete(ctx, cl, cfg, prompt.RowCompletion, in, nil)
		r.res, r.err = res, err
		if err == nil {
			for _, row := range a.Rows {
				m := row.Fields().Values
				m["id"] = row.ID
				r.rows = append(r.rows, m)
			}
		}
	case "text_to_items":
		a, res, err := complete(ctx, cl, cfg, prompt.TextToItems, prompt.TextToItemsInput{Grounding: g, Text: c.text}, nil)
		r.res, r.err = res, err
		if err == nil {
			for i := range a.Rows {
				m := a.Rows[i].Fields(cur).Values
				if id, ok := a.Rows[i].Place(g); ok {
					m["place"] = id
				}
				r.rows = append(r.rows, m)
			}
		}
	case "csv_mapping":
		header, rows, err := readCSV(c.text)
		if err != nil {
			r.err = err
			return r
		}
		a, res, err := complete(ctx, cl, cfg, prompt.CSVMapping, prompt.CSVMappingInput{Grounding: g, Header: header, Rows: rows}, nil)
		r.res, r.err = res, err
		if err == nil {
			r.flat = map[string]any{"decimal_comma": a.DecimalComma}
			for _, col := range a.Columns {
				t := col.Target
				if col.Attribute != nil {
					t += ":" + *col.Attribute
				}
				r.flat[fmt.Sprintf("column.%d", col.Column)] = t
			}
			if a.Category != nil {
				r.flat["category"] = *a.Category
			}
			if a.DateFormat != nil {
				r.flat["date_format"] = *a.DateFormat
			}
		}
	}
	return r
}

func needKinds(needs []prompt.Need) any {
	if len(needs) == 0 {
		return nil
	}
	var out []any
	for _, n := range needs {
		out = append(out, string(n.Kind))
	}
	return out
}

func nonEmptyLines(s string) []string {
	var out []string
	for l := range strings.SplitSeq(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// readCSV reads a header and up to five sample rows, guessing the delimiter
// from the first line.
func readCSV(s string) ([]string, [][]string, error) {
	s = strings.TrimPrefix(s, "\uFEFF")
	first, _, _ := strings.Cut(s, "\n")
	delim := ','
	best := strings.Count(first, ",")
	for _, d := range []rune{';', '\t', '|'} {
		if n := strings.Count(first, string(d)); n > best {
			delim, best = d, n
		}
	}
	r := csv.NewReader(strings.NewReader(s))
	r.Comma = delim
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	recs, err := r.ReadAll()
	if err != nil || len(recs) == 0 {
		return nil, nil, fmt.Errorf("the .txt is not a CSV file: %v", err)
	}
	rows := recs[1:]
	if len(rows) > 5 {
		rows = rows[:5]
	}
	return recs[0], rows, nil
}
