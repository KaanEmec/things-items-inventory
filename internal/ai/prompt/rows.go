package prompt

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// --- row_completion (J7.2) ---

// MaxRows is how many rows one row_completion request may carry.
const MaxRows = 20

// TypedRow is one sheet row as the person typed it.
type TypedRow struct {
	ID       string `json:"id"`       // short and unique within the request, e.g. the row number
	Text     string `json:"text"`     // what the person typed in the name cell
	Category string `json:"category"` // the category the person picked, or ""
}

// RowCompletionInput is the request for the row_completion task.
type RowCompletionInput struct {
	Grounding Grounding  `json:"grounding"`
	Rows      []TypedRow `json:"rows"` // at most MaxRows
}

// CompletedRow is the suggestion for one row.
type CompletedRow struct {
	ID         string         `json:"id" desc:"The row's id, as given."`
	Category   *CategoryField `json:"category"`
	Brand      *TextField     `json:"brand"`
	Model      *TextField     `json:"model" desc:"Only a model the text names, in its official spelling."`
	Name       *TextField     `json:"name" desc:"Tidy search name in the household's language."`
	Color      *TextField     `json:"color"`
	Quantity   *CountField    `json:"quantity" desc:"Only a count the text states."`
	Attributes []AttrField    `json:"attributes"`
}

// RowCompletionAnswer holds one completed row per typed row.
type RowCompletionAnswer struct {
	Rows []CompletedRow `json:"rows"`
}

// Fields returns the row's suggestions by ai_fields path.
func (r *CompletedRow) Fields() Fields {
	f := newFields()
	f.category(r.Category)
	f.text("brand", r.Brand)
	f.text("model", r.Model)
	f.text("name", r.Name)
	f.text("color", r.Color)
	f.count("quantity", r.Quantity)
	f.attrs(defaultCatalog, r.Category, r.Attributes)
	return f
}

// Row returns the completed row with the given id.
func (a *RowCompletionAnswer) Row(id string) (*CompletedRow, bool) {
	for i := range a.Rows {
		if a.Rows[i].ID == id {
			return &a.Rows[i], true
		}
	}
	return nil, false
}

func rowCompletionUser(cats catalog) func(RowCompletionInput) string {
	return func(in RowCompletionInput) string {
		var b strings.Builder
		b.WriteString(in.Grounding.render(cats, gAttributes|gSpellings))
		b.WriteString("\nEach line is one row: id, a tab, what the person typed, and the category they picked if any.\n")
		var d strings.Builder
		for _, r := range in.Rows {
			fmt.Fprintf(&d, "%s\t%s", oneLine(r.ID), clip(oneLine(r.Text), 200))
			if c := oneLine(r.Category); c != "" {
				fmt.Fprintf(&d, "\t(category picked: %s)", c)
			}
			d.WriteByte('\n')
		}
		b.WriteString(dataBlock("rows", d.String()))
		b.WriteString("\nComplete every row.")
		return b.String()
	}
}

func checkFieldsCommon(path string, cats catalog, cat *CategoryField, brand, model, name, color *TextField, qty *CountField, attrs []AttrField) error {
	if err := checkTexts(path, 200, "brand", brand, "model", model, "name", name, "color", color); err != nil {
		return err
	}
	if name != nil && len([]rune(name.Value)) > 80 {
		return fmt.Errorf("%s: longer than 80 characters; use the short search name", join(path, "name"))
	}
	if qty != nil && (qty.Value < 1 || qty.Value > 10000) {
		return fmt.Errorf("%s: %d is not a count; use null when none is stated", join(path, "quantity"), qty.Value)
	}
	_, err := cats.parseAttrs(join(path, "attributes"), cat, attrs)
	return err
}

func checkRowCompletion(cats catalog) func(*RowCompletionAnswer) error {
	return func(a *RowCompletionAnswer) error {
		if len(a.Rows) > MaxRows {
			return fmt.Errorf("rows: %d rows; answer once per given row", len(a.Rows))
		}
		seen := map[string]bool{}
		for i, r := range a.Rows {
			p := fmt.Sprintf("rows[%d]", i)
			if seen[r.ID] {
				return fmt.Errorf("%s.id: %q answered twice; one answer per row", p, r.ID)
			}
			seen[r.ID] = true
			if err := checkFieldsCommon(p, cats, r.Category, r.Brand, r.Model, r.Name, r.Color, r.Quantity, r.Attributes); err != nil {
				return err
			}
		}
		return nil
	}
}

func checkRowCompletionIn(in RowCompletionInput, a *RowCompletionAnswer) error {
	want := map[string]TypedRow{}
	for _, r := range in.Rows {
		want[oneLine(r.ID)] = r
	}
	for i, r := range a.Rows {
		tr, ok := want[r.ID]
		if !ok {
			return fmt.Errorf("rows[%d].id: %q is not one of the given row ids", i, r.ID)
		}
		if tr.Category != "" && r.Category != nil && r.Category.Value != tr.Category {
			return fmt.Errorf("rows[%d].category: the person picked %q for this row; use it", i, tr.Category)
		}
	}
	if len(a.Rows) != len(want) {
		var missing []string
		for id := range want {
			if _, ok := a.Row(id); !ok {
				missing = append(missing, id)
			}
		}
		return fmt.Errorf("rows: no answer for row ids %s; answer every row, with null fields when unsure", strings.Join(missing, ", "))
	}
	return nil
}

// --- text_to_items (J7.3) ---

// TextToItemsInput is the request for the text_to_items task. Grounding
// should carry the household's places.
type TextToItemsInput struct {
	Grounding Grounding `json:"grounding"`
	Text      string    `json:"text"`
}

// TextRow is one thing found in the text.
type TextRow struct {
	Source     string          `json:"source" desc:"The words of the text this row came from, short."`
	Category   *CategoryField  `json:"category"`
	Brand      *TextField      `json:"brand"`
	Model      *TextField      `json:"model" desc:"Only a model the text names."`
	Name       *TextField      `json:"name" desc:"Search name in the household's language."`
	Color      *TextField      `json:"color"`
	Quantity   *CountField     `json:"quantity" desc:"Only a stated count."`
	Attributes []AttrField     `json:"attributes"`
	PlaceID    *string         `json:"place_id" desc:"An ID from the places block (p1, p2, ...), or null. Never a place name."`
	Purchase   *PurchaseAnswer `json:"purchase" desc:"Only for receipts and order confirmations."`
}

// TextToItemsAnswer is the list of things found in the text.
type TextToItemsAnswer struct {
	Rows []TextRow `json:"rows"`
}

// Fields returns the row's values by ai_fields path.
func (r *TextRow) Fields(defaultCurrency string) Fields {
	f := newFields()
	f.category(r.Category)
	f.text("brand", r.Brand)
	f.text("model", r.Model)
	f.text("name", r.Name)
	f.text("color", r.Color)
	f.count("quantity", r.Quantity)
	f.attrs(defaultCatalog, r.Category, r.Attributes)
	f.purchase(r.Purchase, defaultCurrency)
	return f
}

// Place returns the real place ID the row's place_id stands for. An ID that
// is not in the request's places is dropped (ok false), as is a null one.
func (r *TextRow) Place(g Grounding) (id string, ok bool) {
	if r.PlaceID == nil {
		return "", false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(*r.PlaceID, "p"))
	if err != nil || n < 1 || n > len(g.Places) || n > maxPlaces || placeRef(n-1) != *r.PlaceID {
		return "", false
	}
	return g.Places[n-1].ID, true
}

var placeRefRe = regexp.MustCompile(`^p[1-9][0-9]*$`)

func textToItemsUser(cats catalog) func(TextToItemsInput) string {
	return func(in TextToItemsInput) string {
		var b strings.Builder
		b.WriteString(in.Grounding.render(cats, gAttributes|gSpellings|gMoney|gPlaces))
		b.WriteString("\nThe text the person gave:\n")
		b.WriteString(dataBlock("text", clip(in.Text, 12000)))
		b.WriteString("\nList the belongings in the text.")
		return b.String()
	}
}

func checkTextToItems(cats catalog) func(*TextToItemsAnswer) error {
	return func(a *TextToItemsAnswer) error {
		if len(a.Rows) > 200 {
			return fmt.Errorf("rows: %d rows; at most 200 per answer", len(a.Rows))
		}
		for i, r := range a.Rows {
			p := fmt.Sprintf("rows[%d]", i)
			if strings.TrimSpace(r.Source) == "" {
				return fmt.Errorf("%s.source: copy the words this row came from", p)
			}
			if r.Name == nil {
				return fmt.Errorf("%s.name: every row needs a name; leave out things you can't name", p)
			}
			if err := checkFieldsCommon(p, cats, r.Category, r.Brand, r.Model, r.Name, r.Color, r.Quantity, r.Attributes); err != nil {
				return err
			}
			if r.PlaceID != nil && !placeRefRe.MatchString(*r.PlaceID) {
				return fmt.Errorf("%s.place_id: %q is not an ID from the places block (p1, p2, ...); use null when no listed place fits", p, *r.PlaceID)
			}
			if err := checkPurchase(p+".purchase", r.Purchase); err != nil {
				return err
			}
		}
		return nil
	}
}
