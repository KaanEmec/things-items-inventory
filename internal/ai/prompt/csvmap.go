package prompt

import (
	"fmt"
	"slices"
	"strings"
)

// csvTargets are the fields a CSV column can map to (J7.5).
var csvTargets = []string{
	"name", "category", "brand", "model", "serial", "color", "quantity", "condition", "status",
	"place", "purchase_date", "price", "currency", "seller", "warranty_end",
	"notes", "attribute", "ignore",
}

// csvRepeatable targets may take several columns.
var csvRepeatable = []string{"notes", "attribute", "ignore"}

// dateFormats are the date layouts a mapping can name.
var dateFormats = []string{"YYYY-MM-DD", "DD.MM.YYYY", "DD/MM/YYYY", "MM/DD/YYYY", "DD-MM-YYYY", "YYYY/MM/DD"}

// CSVMappingInput is the request for the csv_mapping task: the header and a
// few sample rows only, never the whole file.
type CSVMappingInput struct {
	Grounding Grounding  `json:"grounding"`
	Header    []string   `json:"header"` // column names; "" for a file without a header
	Rows      [][]string `json:"rows"`   // sample rows, at most 5 are sent
}

// ColumnMapping maps one column.
type ColumnMapping struct {
	Column     int64      `json:"column" desc:"The column's index."`
	Target     string     `json:"target" enum:"csv_target"`
	Attribute  *string    `json:"attribute" enum:"attribute_key" desc:"The attribute key when target is attribute, else null."`
	Confidence Confidence `json:"confidence" enum:"confidence"`
}

// CSVMappingAnswer is the proposed mapping.
type CSVMappingAnswer struct {
	Columns      []ColumnMapping `json:"columns"`
	Category     *string         `json:"category" enum:"category" desc:"When every sample row is the same kind of thing, else null."`
	DateFormat   *string         `json:"date_format" enum:"date_format"`
	DecimalComma bool            `json:"decimal_comma"`
}

// Column returns the mapping for column i.
func (a *CSVMappingAnswer) Column(i int) (ColumnMapping, bool) {
	for _, c := range a.Columns {
		if c.Column == int64(i) {
			return c, true
		}
	}
	return ColumnMapping{}, false
}

const (
	csvMaxSamples = 5
	csvMaxColumns = 60
)

func csvMappingUser(cats catalog) func(CSVMappingInput) string {
	return func(in CSVMappingInput) string {
		var b strings.Builder
		b.WriteString(in.Grounding.render(cats, gAttributes))
		b.WriteString("\nThe spreadsheet, column by column: index, header, then sample values.\n")
		var d strings.Builder
		for i := range csvWidth(in) {
			h := ""
			if i < len(in.Header) {
				h = in.Header[i]
			}
			fmt.Fprintf(&d, "column %d: header %q; samples:", i, clip(oneLine(h), 80))
			n := 0
			for _, r := range in.Rows {
				if n == csvMaxSamples {
					break
				}
				n++
				v := ""
				if i < len(r) {
					v = r[i]
				}
				fmt.Fprintf(&d, " %q", clip(oneLine(v), 60))
			}
			d.WriteByte('\n')
		}
		b.WriteString(dataBlock("csv", d.String()))
		b.WriteString("\nMap every column.")
		return b.String()
	}
}

func csvWidth(in CSVMappingInput) int {
	w := len(in.Header)
	for i, r := range in.Rows {
		if i == csvMaxSamples {
			break
		}
		w = max(w, len(r))
	}
	return min(w, csvMaxColumns)
}

func checkCSVMapping(cats catalog) func(*CSVMappingAnswer) error {
	return func(a *CSVMappingAnswer) error {
		cols := map[int64]bool{}
		used := map[string]int64{}
		attrs := map[string]int64{}
		for i, c := range a.Columns {
			p := fmt.Sprintf("columns[%d]", i)
			if c.Column < 0 || c.Column >= csvMaxColumns {
				return fmt.Errorf("%s.column: %d is not a column index", p, c.Column)
			}
			if cols[c.Column] {
				return fmt.Errorf("%s.column: column %d is mapped twice", p, c.Column)
			}
			cols[c.Column] = true
			if (c.Target == "attribute") != (c.Attribute != nil) {
				return fmt.Errorf("%s.attribute: set it exactly when target is attribute", p)
			}
			if c.Attribute != nil {
				if !slices.Contains(cats.attrKeys(), *c.Attribute) {
					return fmt.Errorf("%s.attribute: %q is not an attribute key", p, *c.Attribute)
				}
				if a.Category != nil {
					cat, _ := cats.category(*a.Category)
					if _, ok := cat.Attr(*c.Attribute); !ok {
						return fmt.Errorf("%s.attribute: %q is not an attribute of %s", p, *c.Attribute, cat.Key)
					}
				}
				if j, dup := attrs[*c.Attribute]; dup {
					return fmt.Errorf("%s: attribute %q is already filled by column %d; send this one to notes or ignore", p, *c.Attribute, j)
				}
				attrs[*c.Attribute] = c.Column
			}
			if !slices.Contains(csvRepeatable, c.Target) {
				if j, dup := used[c.Target]; dup {
					return fmt.Errorf("%s: %s is already filled by column %d; send this one to notes or ignore", p, c.Target, j)
				}
				used[c.Target] = c.Column
			}
			if err := checkConfidence(p, c.Confidence); err != nil {
				return err
			}
		}
		return nil
	}
}

func checkCSVMappingIn(in CSVMappingInput, a *CSVMappingAnswer) error {
	w := csvWidth(in)
	for _, c := range a.Columns {
		if c.Column >= int64(w) {
			return fmt.Errorf("columns: column %d does not exist; the file has columns 0 to %d", c.Column, w-1)
		}
	}
	if len(a.Columns) != w {
		var missing []string
		for i := range w {
			if _, ok := a.Column(i); !ok {
				missing = append(missing, fmt.Sprint(i))
			}
		}
		return fmt.Errorf("columns: no mapping for columns %s; map every column, with ignore when it holds nothing useful", strings.Join(missing, ", "))
	}
	return nil
}
