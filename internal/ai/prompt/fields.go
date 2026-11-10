package prompt

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Fields is an answer turned into what inventory.ApplyAI takes: field paths
// as in items.ai_fields ("brand", "attributes.size", "purchase_date",
// "price_minor") mapped to values, plus the confidence of each, which goes
// into ai_draft.confidence (spec §6). Empty fields are absent.
type Fields struct {
	Values     map[string]any
	Confidence map[string]Confidence
}

func newFields() Fields {
	return Fields{Values: map[string]any{}, Confidence: map[string]Confidence{}}
}

func (f Fields) text(path string, t *TextField) {
	if t != nil {
		f.Values[path] = strings.TrimSpace(t.Value)
		f.Confidence[path] = t.Confidence
	}
}

func (f Fields) count(path string, c *CountField) {
	if c != nil {
		f.Values[path] = c.Value
		f.Confidence[path] = c.Confidence
	}
}

func (f Fields) category(c *CategoryField) {
	if c != nil {
		f.Values["category"] = c.Value
		f.Confidence["category"] = c.Confidence
	}
}

func (f Fields) condition(c *ConditionField) {
	if c != nil {
		f.Values["condition"] = c.Value
		f.Confidence["condition"] = c.Confidence
	}
}

// attrs adds attributes already checked by parseAttrs.
func (f Fields) attrs(cats catalog, category *CategoryField, attrs []AttrField) {
	vals, err := cats.parseAttrs("attributes", category, attrs)
	if err != nil {
		return
	}
	for _, a := range attrs {
		if v, ok := vals[a.Key]; ok {
			f.Values["attributes."+a.Key] = v
			f.Confidence["attributes."+a.Key] = a.Confidence
		}
	}
}

// purchase adds purchase fields; price becomes price_minor in the answer's
// currency, or defaultCurrency when the answer has none.
func (f Fields) purchase(p *PurchaseAnswer, defaultCurrency string) {
	if p == nil {
		return
	}
	f.text("purchase_date", p.Date)
	f.text("currency", p.Currency)
	f.text("seller", p.Seller)
	if p.Price != nil {
		cur := defaultCurrency
		if p.Currency != nil {
			cur = p.Currency.Value
		}
		f.Values["price_minor"] = MinorUnits(p.Price.Value, cur)
		f.Confidence["price_minor"] = p.Price.Confidence
	}
}

// zeroDecimal lists ISO 4217 currencies without minor units.
var zeroDecimal = []string{"BIF", "CLP", "DJF", "GNF", "ISK", "JPY", "KMF", "KRW", "PYG", "RWF", "UGX", "VND", "VUV", "XAF", "XOF", "XPF"}

// MinorUnits converts a price in major units to the minor units stored in
// items.price_minor.
func MinorUnits(price float64, currency string) int64 {
	if slices.Contains(zeroDecimal, strings.ToUpper(currency)) {
		return int64(math.Round(price))
	}
	return int64(math.Round(price * 100))
}

// PurchaseAnswer is what a receipt says about one item.
type PurchaseAnswer struct {
	Date           *TextField   `json:"date" desc:"YYYY-MM-DD"`
	Price          *NumberField `json:"price" desc:"Price of one unit in major units, a plain number."`
	Currency       *TextField   `json:"currency" desc:"ISO 4217 code, e.g. EUR, TRY."`
	Seller         *TextField   `json:"seller" desc:"The shop's name as a person would say it."`
	WarrantyMonths *CountField  `json:"warranty_months" desc:"Only when the receipt states a warranty term."`
}

func checkPurchase(path string, p *PurchaseAnswer) error {
	if p == nil {
		return nil
	}
	if err := checkTexts(path, 200, "date", p.Date, "currency", p.Currency, "seller", p.Seller); err != nil {
		return err
	}
	if err := checkDate(join(path, "date"), p.Date); err != nil {
		return err
	}
	if err := checkCurrency(join(path, "currency"), p.Currency); err != nil {
		return err
	}
	if p.Price != nil && (p.Price.Value < 0 || p.Price.Value > 1e9) {
		return fmt.Errorf("%s: %v is not a price", join(path, "price"), p.Price.Value)
	}
	if p.WarrantyMonths != nil && (p.WarrantyMonths.Value < 1 || p.WarrantyMonths.Value > 360) {
		return fmt.Errorf("%s: %d is not a warranty term in months", join(path, "warranty_months"), p.WarrantyMonths.Value)
	}
	return nil
}

// checkTexts runs checkText over name, field pairs under path, in order.
func checkTexts(path string, max int, pairs ...any) error {
	for i := 0; i+1 < len(pairs); i += 2 {
		if err := checkText(join(path, pairs[i].(string)), pairs[i+1].(*TextField), max); err != nil {
			return err
		}
	}
	return nil
}

func checkDate(path string, t *TextField) error {
	if t == nil {
		return nil
	}
	d, err := time.Parse(time.DateOnly, strings.TrimSpace(t.Value))
	if err != nil {
		return fmt.Errorf("%s: %q must be a date written YYYY-MM-DD", path, t.Value)
	}
	if d.Year() < 1950 || d.After(time.Now().AddDate(0, 0, 2)) {
		return fmt.Errorf("%s: %s is not a plausible purchase date", path, t.Value)
	}
	return nil
}

var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

func checkCurrency(path string, t *TextField) error {
	if t != nil && !currencyCode.MatchString(t.Value) {
		return fmt.Errorf("%s: %q must be a three-letter ISO 4217 code such as EUR or TRY", path, t.Value)
	}
	return nil
}

// codeKey folds a code for comparison with the text read: letters and
// digits only, upper case.
func codeKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	return b.String()
}

// checkRead enforces rule 3 in code: a serial, and a model claimed as sure,
// must appear in the text the model says it read.
func checkRead(readText []string, model, serial *TextField) error {
	read := codeKey(strings.Join(readText, " "))
	if serial != nil && !strings.Contains(read, codeKey(serial.Value)) {
		return fmt.Errorf("serial: %q does not appear in read_text. Copy a serial only from text you read, character by character, and list that text in read_text; otherwise use null", serial.Value)
	}
	if model != nil && model.Confidence == Sure && !strings.Contains(read, codeKey(model.Value)) {
		return fmt.Errorf("model: %q is marked sure but does not appear in read_text. Mark a model you recognise by its look as likely, or list the printed text in read_text", model.Value)
	}
	return nil
}

func checkReadText(lines []string) error {
	if len(lines) > 80 {
		return fmt.Errorf("read_text: %d lines; list at most 80", len(lines))
	}
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			return fmt.Errorf("read_text[%d]: empty line; leave it out", i)
		}
	}
	return nil
}
