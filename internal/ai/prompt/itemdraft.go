package prompt

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// PhotoRole says what a photo sent with a request shows (spec §3 photo roles).
type PhotoRole string

const (
	PhotoItem    PhotoRole = "item"
	PhotoLabel   PhotoRole = "label"
	PhotoReceipt PhotoRole = "receipt"
)

// ItemDraftInput is the request for the item_draft task (J6.3).
type ItemDraftInput struct {
	Grounding Grounding `json:"grounding"`
	// Photos gives the role of each image, in the order the images are sent
	// (label first, then item, then receipt).
	Photos []PhotoRole `json:"photos"`
	// Known holds values a person already entered, by field path ("name",
	// "brand", "attributes.size"); the AI may use them as context.
	Known map[string]string `json:"known"`
}

// ItemDraftAnswer is the item_draft answer (spec §6). Fields turns it into
// what ApplyAI and ai_draft take.
type ItemDraftAnswer struct {
	ReadText   []string        `json:"read_text" desc:"Every line of text readable on the thing and its labels, copied exactly. Empty if none."`
	Category   *CategoryField  `json:"category"`
	Brand      *TextField      `json:"brand"`
	Model      *TextField      `json:"model" desc:"As printed. Null when it could be one of several look-alike models."`
	Serial     *TextField      `json:"serial" desc:"Only when printed and listed in read_text."`
	Name       *TextField      `json:"name" desc:"Search name in the household's language, e.g. Sony WH-1000XM4 headphones."`
	Color      *TextField      `json:"color"`
	Condition  *ConditionField `json:"condition" desc:"Only when visible: sealed or tagged (new), visible wear or damage."`
	Quantity   *CountField     `json:"quantity" desc:"Null for one thing; a count only of identical things."`
	Attributes []AttrField     `json:"attributes" desc:"Only keys of the chosen category."`
	Purchase   *PurchaseAnswer `json:"purchase" desc:"Null unless a receipt photo is included."`
	Note       *string         `json:"note" desc:"One short sentence for something useful with no field, or null."`
	Needs      []Need          `json:"needs"`
}

// Fields returns the answer's values and confidence by ai_fields path;
// note becomes notes and price becomes price_minor (in the answer's currency
// or defaultCurrency). The warranty term is in Purchase.WarrantyMonths.
func (a *ItemDraftAnswer) Fields(defaultCurrency string) Fields {
	f := newFields()
	f.category(a.Category)
	f.text("brand", a.Brand)
	f.text("model", a.Model)
	f.text("serial", a.Serial)
	f.text("name", a.Name)
	f.text("color", a.Color)
	f.condition(a.Condition)
	f.count("quantity", a.Quantity)
	f.attrs(defaultCatalog, a.Category, a.Attributes)
	f.purchase(a.Purchase, defaultCurrency)
	if a.Note != nil && strings.TrimSpace(*a.Note) != "" {
		f.Values["notes"] = strings.TrimSpace(*a.Note)
	}
	return f
}

// Empty reports whether the AI could tell nothing about the thing.
func (a *ItemDraftAnswer) Empty() bool {
	return a.Category == nil && a.Name == nil && a.Brand == nil && a.Model == nil
}

func itemDraftUser(cats catalog) func(ItemDraftInput) string {
	return func(in ItemDraftInput) string {
		var b strings.Builder
		b.WriteString(in.Grounding.render(cats, gAttributes|gSpellings|gMoney))
		b.WriteString("\n<photos>\n")
		if len(in.Photos) == 0 {
			b.WriteString("1: item\n")
		}
		for i, r := range in.Photos {
			fmt.Fprintf(&b, "%d: %s\n", i+1, photoWords(r))
		}
		b.WriteString("</photos>")
		if len(in.Known) > 0 {
			keys := make([]string, 0, len(in.Known))
			for k := range in.Known {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var d strings.Builder
			for _, k := range keys {
				fmt.Fprintf(&d, "%s: %s\n", oneLine(k), clip(oneLine(in.Known[k]), 200))
			}
			b.WriteString("\nThe person already entered these values. They are facts about this item; don't contradict them.\n")
			b.WriteString(dataBlock("known", d.String()))
		}
		b.WriteString("\nDescribe the item in the photos.")
		return b.String()
	}
}

func photoWords(r PhotoRole) string {
	switch r {
	case PhotoLabel:
		return "label or nameplate of the item"
	case PhotoReceipt:
		return "receipt or invoice for the item"
	}
	return "the item"
}

func checkItemDraft(cats catalog) func(*ItemDraftAnswer) error {
	return func(a *ItemDraftAnswer) error {
		if err := checkReadText(a.ReadText); err != nil {
			return err
		}
		if err := checkTexts("", 200, "brand", a.Brand, "model", a.Model, "serial", a.Serial, "name", a.Name, "color", a.Color); err != nil {
			return err
		}
		if a.Name != nil && len([]rune(a.Name.Value)) > 80 {
			return fmt.Errorf("name: longer than 80 characters; use the short search name")
		}
		if err := checkRead(a.ReadText, a.Model, a.Serial); err != nil {
			return err
		}
		if a.Quantity != nil && (a.Quantity.Value < 1 || a.Quantity.Value > 10000) {
			return fmt.Errorf("quantity: %d is not a count; use null for a single thing", a.Quantity.Value)
		}
		if _, err := cats.parseAttrs("attributes", a.Category, a.Attributes); err != nil {
			return err
		}
		if err := checkPurchase("purchase", a.Purchase); err != nil {
			return err
		}
		if a.Note != nil && len([]rune(*a.Note)) > 300 {
			return fmt.Errorf("note: longer than 300 characters; one short sentence")
		}
		return cats.checkNeeds(a.Needs, a.Category, func(field string) bool {
			return sureIn(a.Fields(""), field)
		})
	}
}

func sureIn(f Fields, field string) bool {
	if field == "notes" {
		return false
	}
	return f.Confidence[field] == Sure
}

func checkItemDraftIn(in ItemDraftInput, a *ItemDraftAnswer) error {
	if a.Purchase != nil && !slices.Contains(in.Photos, PhotoReceipt) {
		return fmt.Errorf("purchase: no receipt photo was sent, so purchase must be null")
	}
	return nil
}
