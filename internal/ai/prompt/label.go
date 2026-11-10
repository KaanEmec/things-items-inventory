package prompt

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// LabelInput is the request for the label task (J6.4): one label photo of
// an item the household may already know something about.
type LabelInput struct {
	Grounding Grounding `json:"grounding"`
	// Item is what the item is believed to be so far (name, category,
	// brand, model by field path); empty for an unknown item.
	Item map[string]string `json:"item"`
}

// LabelAnswer is what a label states.
type LabelAnswer struct {
	ReadText   []string       `json:"read_text" desc:"Every line on the label, copied exactly, top to bottom."`
	Category   *CategoryField `json:"category" desc:"The category of the labelled thing when clear, else null."`
	Brand      *TextField     `json:"brand"`
	Model      *TextField     `json:"model" desc:"The model name or number as printed; not a part number, regulatory code or barcode."`
	Serial     *TextField     `json:"serial" desc:"As printed after S/N, SN, Serial, Seri No."`
	Made       *TextField     `json:"made" desc:"Manufacture date or year as printed, or null."`
	Attributes []AttrField    `json:"attributes" desc:"Printed specs that fit the category's keys."`
	Needs      []Need         `json:"needs"`
}

// Fields returns the label's values by ai_fields path. Attributes are kept
// for the item's category (the label's when itemCategory is ""); a printed
// manufacture year fills attributes.year when that category has it.
func (a *LabelAnswer) Fields(itemCategory string) Fields {
	f := newFields()
	f.text("brand", a.Brand)
	f.text("model", a.Model)
	f.text("serial", a.Serial)
	cat := a.Category
	if itemCategory != "" {
		cat = &CategoryField{Value: itemCategory, Confidence: Sure}
	}
	if cat != nil {
		var keep []AttrField
		c, _ := defaultCatalog.category(cat.Value)
		for _, af := range a.Attributes {
			if _, ok := c.Attr(af.Key); ok {
				keep = append(keep, af)
			}
		}
		f.attrs(defaultCatalog, cat, keep)
		if _, has := f.Values["attributes.year"]; !has && a.Made != nil {
			if _, ok := c.Attr("year"); ok {
				if y, ok := yearOf(a.Made.Value); ok {
					f.Values["attributes.year"] = y
					f.Confidence["attributes.year"] = a.Made.Confidence
				}
			}
		}
	}
	return f
}

var yearRe = regexp.MustCompile(`\b(19[5-9]\d|20\d\d)\b`)

func yearOf(s string) (int64, bool) {
	m := yearRe.FindString(s)
	if m == "" {
		return 0, false
	}
	y, _ := strconv.ParseInt(m, 10, 64)
	return y, true
}

func labelUser(cats catalog) func(LabelInput) string {
	return func(in LabelInput) string {
		var b strings.Builder
		b.WriteString(in.Grounding.render(cats, gAttributes))
		b.WriteString("\n<photos>\n1: label or nameplate\n</photos>")
		if len(in.Item) > 0 {
			b.WriteString("\nWhat the item is believed to be so far (may be an AI guess; the label wins):\n")
			b.WriteString(dataBlock("item", knownLines(in.Item)))
		}
		b.WriteString("\nRead the label.")
		return b.String()
	}
}

func knownLines(m map[string]string) string {
	var d strings.Builder
	for _, k := range []string{"name", "category", "brand", "model"} {
		if v := strings.TrimSpace(m[k]); v != "" {
			fmt.Fprintf(&d, "%s: %s\n", k, clip(oneLine(v), 200))
		}
	}
	return d.String()
}

func checkLabel(cats catalog) func(*LabelAnswer) error {
	return func(a *LabelAnswer) error {
		if err := checkReadText(a.ReadText); err != nil {
			return err
		}
		if err := checkTexts("", 200, "brand", a.Brand, "model", a.Model, "serial", a.Serial, "made", a.Made); err != nil {
			return err
		}
		if err := checkRead(a.ReadText, a.Model, a.Serial); err != nil {
			return err
		}
		if a.Brand != nil && a.Brand.Confidence == Sure && !strings.Contains(codeKey(strings.Join(a.ReadText, " ")), codeKey(a.Brand.Value)) {
			return fmt.Errorf("brand: %q is marked sure but is not in read_text; on a label, sure means printed", a.Brand.Value)
		}
		if _, err := cats.parseAttrs("attributes", a.Category, a.Attributes); err != nil {
			return err
		}
		return cats.checkNeeds(a.Needs, a.Category, nil)
	}
}
