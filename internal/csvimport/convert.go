package csvimport

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/ai/prompt"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/search"
)

var (
	numRe  = regexp.MustCompile(`^[-+]?\d+([.,]\d+)?$`)
	dateRe = regexp.MustCompile(`^\d{1,4}[-./]\d{1,2}[-./]\d{1,4}`)
)

// Column is the mapping of one file column. Target is a field name, "attribute"
// (with Attribute), "notes" or "ignore".
type Column struct {
	Target     string `json:"target"`
	Attribute  string `json:"attribute,omitempty"`
	Confidence string `json:"confidence,omitempty"` // set by the AI proposal
}

// Mapping tells how to read the file. Places maps a place value of the file
// that matched no place to an existing place ID or "create".
type Mapping struct {
	HasHeader    bool              `json:"has_header"`
	Delimiter    string            `json:"delimiter"`
	Columns      []Column          `json:"columns"`
	Category     string            `json:"category"`
	DateFormat   string            `json:"date_format"`
	DecimalComma bool              `json:"decimal_comma"`
	Places       map[string]string `json:"places,omitempty"`
}

// Place is a place of the household a file value can be matched to.
type Place struct{ ID, Path, Name string }

// Problem is a row that was skipped or lost a value.
type Problem struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

// Prepared is a file turned into items.
type Prepared struct {
	Items     []PreparedItem
	Skipped   []Problem
	Warnings  []Problem
	Unmatched map[string]int // place values that matched no place, with row counts
}

// PreparedItem is one item to create: its ID and API fields.
type PreparedItem struct {
	ID     string                     `json:"id"`
	Fields map[string]json.RawMessage `json:"fields"`
	Place  string                     `json:"place,omitempty"` // the file's value, to resolve
	Row    int                        `json:"row"`             // the file row it came from
}

var dateLayouts = map[string]string{"YYYY-MM-DD": "2006-01-02", "DD.MM.YYYY": "02.01.2006", "DD/MM/YYYY": "02/01/2006",
	"MM/DD/YYYY": "01/02/2006", "DD-MM-YYYY": "02-01-2006", "YYYY/MM/DD": "2006/01/02"}

func parseDate(s, format string) (string, bool) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " T"); i > 0 {
		s = s[:i]
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("2006-01-02"), true
	}
	if l, ok := dateLayouts[format]; ok {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}

var priceJunk = regexp.MustCompile(`[^0-9.,-]`)

func parsePrice(s string, decimalComma bool) (float64, bool) {
	s = priceJunk.ReplaceAllString(s, "")
	if decimalComma {
		s = strings.ReplaceAll(strings.ReplaceAll(s, ".", ""), ",", ".")
	} else {
		s = strings.ReplaceAll(s, ",", "")
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil && f >= 0 && f < 1e9
}

// match finds v among values by key or label, folded.
func match(v string, options []string) (string, bool) {
	fv := search.Fold(v)
	for _, o := range options {
		if search.Fold(o) == fv {
			return o, true
		}
	}
	return "", false
}

func coerceAttr(a inventory.Attribute, v string, decimalComma bool) (any, bool) {
	switch a.Type {
	case inventory.TypeText:
		return v, true
	case inventory.TypeInt:
		f, ok := parsePrice(v, decimalComma)
		return int64(f), ok
	case inventory.TypeNumber:
		return parsePrice(v, decimalComma)
	case inventory.TypeBool:
		switch search.Fold(v) {
		case "yes", "true", "1", "evet", "y":
			return true, true
		case "no", "false", "0", "hayir", "n":
			return false, true
		}
	case inventory.TypeEnum:
		o, ok := match(v, a.Values)
		return o, ok
	case inventory.TypeMulti:
		var out []any
		for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' || r == '/' }) {
			o, ok := match(strings.TrimSpace(p), a.Values)
			if !ok {
				return nil, false
			}
			out = append(out, o)
		}
		return out, len(out) > 0
	}
	return nil, false
}

// Prepare turns the data rows (header already removed) into items. firstRow is
// the file row number of rows[0]. places are the household's places, currency
// its default currency, newID makes item IDs.
func Prepare(m Mapping, header []string, rows [][]string, firstRow int, places []Place, currency string, newID func() string) Prepared {
	p := Prepared{Unmatched: map[string]int{}}
	byName := map[string]string{}
	for _, pl := range places {
		byName[search.Fold(pl.Path)] = pl.ID
		if _, dup := byName[search.Fold(pl.Name)]; !dup {
			byName[search.Fold(pl.Name)] = pl.ID
		}
	}
	for ri, rec := range rows {
		n := firstRow + ri
		vals := map[string]string{}
		attrs := map[string]string{}
		var notes []string
		for ci, col := range m.Columns {
			if ci >= len(rec) || rec[ci] == "" {
				continue
			}
			switch col.Target {
			case "ignore", "":
			case "notes":
				h := ""
				if ci < len(header) {
					h = header[ci] + ": "
				}
				notes = append(notes, h+rec[ci])
			case "attribute":
				attrs[col.Attribute] = rec[ci]
			default:
				if _, dup := vals[col.Target]; !dup {
					vals[col.Target] = rec[ci]
				}
			}
		}
		name := vals["name"]
		if name == "" {
			p.Skipped = append(p.Skipped, Problem{n, "no name"})
			continue
		}
		f := map[string]json.RawMessage{"draft": []byte("true")}
		set := func(k string, v any) { b, _ := json.Marshal(v); f[k] = b }
		set("name", name)
		for _, k := range []string{"brand", "model", "serial", "color"} {
			if vals[k] != "" {
				set(k, vals[k])
			}
		}
		cat := m.Category
		if v := vals["category"]; v != "" {
			cat = v
			var keys []string
			for _, c := range inventory.Categories {
				keys = append(keys, c.Key)
			}
			if k, ok := match(v, keys); ok {
				cat = k
			}
		}
		if cat != "" {
			set("category", cat)
		}
		if v := vals["quantity"]; v != "" {
			if q, ok := parsePrice(v, m.DecimalComma); ok {
				set("quantity", int64(q))
			} else {
				p.Warnings = append(p.Warnings, Problem{n, "bad quantity"})
			}
		}
		if v := vals["condition"]; v != "" {
			if o, ok := match(v, inventory.Conditions); ok {
				set("condition", o)
			}
		}
		if v := vals["status"]; v != "" {
			if o, ok := match(v, inventory.Statuses); ok {
				set("status", o)
			}
		}
		purchase := map[string]any{}
		if v := vals["purchase_date"]; v != "" {
			if d, ok := parseDate(v, m.DateFormat); ok {
				purchase["date"] = d
			} else {
				p.Warnings = append(p.Warnings, Problem{n, "bad purchase date"})
			}
		}
		if v := vals["currency"]; len(v) == 3 {
			currency := strings.ToUpper(v)
			purchase["currency"] = currency
		}
		if v := vals["price"]; v != "" {
			if f, ok := parsePrice(v, m.DecimalComma); ok {
				cur := currency
				if c, ok := purchase["currency"].(string); ok {
					cur = c
				}
				purchase["price_minor"] = prompt.MinorUnits(f, cur)
			} else {
				p.Warnings = append(p.Warnings, Problem{n, "bad price"})
			}
		}
		if vals["seller"] != "" {
			purchase["seller"] = vals["seller"]
		}
		if len(purchase) > 0 {
			set("purchase", purchase)
		}
		if v := vals["warranty_end"]; v != "" {
			if d, ok := parseDate(v, m.DateFormat); ok {
				set("warranty", map[string]any{"end": d})
			} else {
				p.Warnings = append(p.Warnings, Problem{n, "bad warranty date"})
			}
		}
		if c, ok := inventory.CategoryByKey(cat); ok && len(attrs) > 0 {
			out := map[string]any{}
			for _, k := range slices.Sorted(mapsKeys(attrs)) {
				a, has := c.Attr(k)
				if v, ok := coerceAttr(a, attrs[k], m.DecimalComma); has && ok {
					out[k] = v
				} else {
					notes = append(notes, k+": "+attrs[k])
				}
			}
			if len(out) > 0 {
				set("attributes", out)
			}
		} else {
			for _, k := range slices.Sorted(mapsKeys(attrs)) {
				notes = append(notes, k+": "+attrs[k])
			}
		}
		if len(notes) > 0 {
			set("notes", strings.Join(notes, "\n"))
		}
		it := PreparedItem{ID: newID(), Fields: f, Row: n}
		if v := vals["place"]; v != "" {
			if id, ok := byName[search.Fold(v)]; ok {
				set("place_id", id)
			} else {
				it.Place = v
				p.Unmatched[v]++
			}
		}
		p.Items = append(p.Items, it)
	}
	return p
}

func mapsKeys(m map[string]string) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

var headerTargets = map[string]string{
	"name": "name", "title": "name", "item": "name", "item name": "name", "product": "name", "isim": "name", "esya": "name", "ad": "name",
	"category": "category", "kategori": "category", "type": "category",
	"brand": "brand", "marka": "brand", "manufacturer": "brand", "make": "brand",
	"model": "model", "serial": "serial", "serial number": "serial", "serial no": "serial", "sn": "serial", "seri no": "serial",
	"color": "color", "colour": "color", "renk": "color",
	"quantity": "quantity", "qty": "quantity", "count": "quantity", "amount": "quantity", "adet": "quantity",
	"condition": "condition", "status": "status",
	"place": "place", "location": "place", "room": "place", "konum": "place", "yer": "place",
	"notes": "notes", "note": "notes", "comment": "notes", "comments": "notes", "description": "notes", "aciklama": "notes",
	"purchase date": "purchase_date", "purchase_date": "purchase_date", "date": "purchase_date", "purchased": "purchase_date", "bought": "purchase_date", "date purchased": "purchase_date",
	"price": "price", "cost": "price", "value": "price", "fiyat": "price", "purchase price": "price",
	"currency": "currency", "seller": "seller", "store": "seller", "shop": "seller", "vendor": "seller", "retailer": "seller",
	"warranty end": "warranty_end", "warranty_end": "warranty_end", "warranty": "warranty_end", "warranty expires": "warranty_end",
}

// HeuristicMapping maps columns whose header names a field; the rest are
// ignored. Dates and decimals are guessed from the sample rows.
func HeuristicMapping(header []string, samples [][]string) Mapping {
	m := Mapping{HasHeader: len(header) > 0, DateFormat: "YYYY-MM-DD"}
	width := len(header)
	for _, r := range samples {
		width = max(width, len(r))
	}
	used := map[string]bool{}
	for i := 0; i < width; i++ {
		c := Column{Target: "ignore"}
		if i < len(header) {
			h := strings.Join(strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(strings.ToLower(header[i]))), " ")
			if t, ok := headerTargets[h]; ok && (t == "notes" || !used[t]) {
				c.Target = t
				used[t] = true
			}
		}
		m.Columns = append(m.Columns, c)
	}
	if !used["name"] && width > 0 && len(header) == 0 {
		m.Columns[0].Target = "name"
	}
	for _, r := range samples {
		for _, v := range r {
			switch {
			case regexp.MustCompile(`^\d{1,2}\.\d{1,2}\.\d{4}`).MatchString(v):
				m.DateFormat = "DD.MM.YYYY"
			case regexp.MustCompile(`^\d{1,2}/\d{1,2}/\d{4}`).MatchString(v) && m.DateFormat == "YYYY-MM-DD":
				m.DateFormat = "DD/MM/YYYY"
			case regexp.MustCompile(`^\d+,\d{1,2}$`).MatchString(v):
				m.DecimalComma = true
			}
		}
	}
	return m
}

func (m Mapping) validate() error {
	hasName := false
	for _, c := range m.Columns {
		if c.Target == "name" {
			hasName = true
		}
		if c.Target == "attribute" && c.Attribute == "" {
			return fmt.Errorf("an attribute column needs its attribute key")
		}
	}
	if !hasName {
		return fmt.Errorf("map one column to name")
	}
	return nil
}
