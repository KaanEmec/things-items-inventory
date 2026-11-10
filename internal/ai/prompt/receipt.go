package prompt

import (
	"fmt"
	"strings"
)

// ReceiptInput is the request for the receipt task (J6.5).
type ReceiptInput struct {
	Grounding Grounding `json:"grounding"`
	// Item names the item the receipt is for (name, brand, model by field
	// path), so the right line is picked on a receipt with several products.
	Item map[string]string `json:"item"`
}

// ReceiptLine is one product line of a receipt.
type ReceiptLine struct {
	Text     string   `json:"text" desc:"The line exactly as printed."`
	Name     *string  `json:"name" desc:"Search name in the household's language, or null when too cryptic."`
	Quantity *int64   `json:"quantity"`
	Price    *float64 `json:"price" desc:"Price of one unit in major units, a plain number."`
}

// ItemLine points at the line that is the requested item.
type ItemLine struct {
	Index      int64      `json:"index" desc:"0-based index in lines."`
	Confidence Confidence `json:"confidence" enum:"confidence"`
}

// ReceiptAnswer is what a receipt states.
type ReceiptAnswer struct {
	Lines          []ReceiptLine `json:"lines" desc:"Product lines in printed order; no totals, tax, discounts, shipping or payment lines."`
	ItemLine       *ItemLine     `json:"item_line"`
	Date           *TextField    `json:"date" desc:"YYYY-MM-DD"`
	Seller         *TextField    `json:"seller"`
	Currency       *TextField    `json:"currency" desc:"ISO 4217 code."`
	WarrantyMonths *CountField   `json:"warranty_months" desc:"Only when the receipt states a warranty term."`
}

// Purchase returns the purchase details for the requested item: the
// receipt's date, seller and currency, the matched line's price, and a
// stated warranty term.
func (a *ReceiptAnswer) Purchase() *PurchaseAnswer {
	p := &PurchaseAnswer{Date: a.Date, Seller: a.Seller, Currency: a.Currency, WarrantyMonths: a.WarrantyMonths}
	if a.ItemLine != nil && a.ItemLine.Index >= 0 && a.ItemLine.Index < int64(len(a.Lines)) {
		if l := a.Lines[a.ItemLine.Index]; l.Price != nil {
			p.Price = &NumberField{Value: *l.Price, Confidence: a.ItemLine.Confidence}
		}
	}
	return p
}

// Fields returns purchase_date, price_minor, currency and seller.
func (a *ReceiptAnswer) Fields(defaultCurrency string) Fields {
	f := newFields()
	f.purchase(a.Purchase(), defaultCurrency)
	return f
}

// OtherLines returns the lines that are not the item ("This receipt also
// lists: …").
func (a *ReceiptAnswer) OtherLines() []ReceiptLine {
	var out []ReceiptLine
	for i, l := range a.Lines {
		if a.ItemLine == nil || int64(i) != a.ItemLine.Index {
			out = append(out, l)
		}
	}
	return out
}

func receiptUser(cats catalog) func(ReceiptInput) string {
	return func(in ReceiptInput) string {
		var b strings.Builder
		b.WriteString(in.Grounding.render(cats, gMoney))
		b.WriteString("\n<photos>\n1: receipt, invoice or order confirmation\n</photos>")
		if k := knownLines(in.Item); k != "" {
			b.WriteString("\nThe item this receipt is for:\n")
			b.WriteString(dataBlock("item", k))
		} else {
			b.WriteString("\nThe request names no item.")
		}
		b.WriteString("\nRead the receipt.")
		return b.String()
	}
}

func checkReceipt(a *ReceiptAnswer) error {
	if len(a.Lines) > 100 {
		return fmt.Errorf("lines: %d lines; list at most 100 product lines", len(a.Lines))
	}
	for i, l := range a.Lines {
		p := fmt.Sprintf("lines[%d]", i)
		if strings.TrimSpace(l.Text) == "" {
			return fmt.Errorf("%s.text: empty; copy the printed line", p)
		}
		if l.Name != nil && strings.TrimSpace(*l.Name) == "" {
			return fmt.Errorf("%s.name: empty; use null", p)
		}
		if l.Quantity != nil && *l.Quantity < 1 {
			return fmt.Errorf("%s.quantity: %d is not a count", p, *l.Quantity)
		}
		if l.Price != nil && (*l.Price < 0 || *l.Price > 1e9) {
			return fmt.Errorf("%s.price: %v is not a unit price", p, *l.Price)
		}
	}
	if a.ItemLine != nil {
		if a.ItemLine.Index < 0 || a.ItemLine.Index >= int64(len(a.Lines)) {
			return fmt.Errorf("item_line.index: %d is not an index of lines (0 to %d)", a.ItemLine.Index, len(a.Lines)-1)
		}
		if err := checkConfidence("item_line", a.ItemLine.Confidence); err != nil {
			return err
		}
	}
	if err := checkTexts("", 200, "date", a.Date, "seller", a.Seller, "currency", a.Currency); err != nil {
		return err
	}
	return checkPurchase("", a.Purchase())
}
