package prompt

import (
	"fmt"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/inventory"
)

// Grounding is what the AI is told about the household, so answers fit it.
// The caller fills it from the household row and simple queries; each task
// renders only the parts it needs.
type Grounding struct {
	Language string   `json:"language"` // BCP 47 tag from households.language; "" means en
	Currency string   `json:"currency"` // households.currency, for order text and receipts
	Brands   []string `json:"brands"`   // brands used most in this household, most used first
	Names    []string `json:"names"`    // item names used most, most used first
	Places   []Place  `json:"places"`   // places and boxes, for text tasks
}

// Place is one place the AI may answer with, by ID.
type Place struct {
	ID   string `json:"id"`   // places.id
	Path string `json:"path"` // places.path, "3rd drawer · left closet · bedroom", "Box 12"
}

// Grounding limits keep prompts compact.
const (
	maxBrands = 40
	maxNames  = 40
	maxPlaces = 400
)

type part uint8

const (
	gCategories part = 1 << iota // category keys
	gAttributes                  // with attribute keys, types and values
	gSpellings                   // brands and names in use
	gMoney                       // currency and date order
	gPlaces                      // the places list (text_to_items)
)

// render writes the <household> block (and the <places> block) with the
// requested parts. Brands, names and paths were typed by people, so they are
// sanitized like any other data.
func (g Grounding) render(cats catalog, parts part) string {
	var b strings.Builder
	b.WriteString("<household>\n")
	fmt.Fprintf(&b, "Language for names, notes and questions: %s.\n", languageName(g.Language))
	if parts&gMoney != 0 {
		if g.Currency != "" {
			fmt.Fprintf(&b, "Usual currency: %s.\n", oneLine(g.Currency))
		}
		fmt.Fprintf(&b, "Dates written like 03/04/2025 are %s here.\n", dateOrder(g.Language))
	}
	if parts&(gCategories|gAttributes) != 0 {
		b.WriteString("Categories")
		if parts&gAttributes != 0 {
			b.WriteString(" and their attribute keys")
		}
		b.WriteString(":\n")
		for _, c := range cats {
			b.WriteString("- " + c.Key)
			if parts&gAttributes != 0 {
				b.WriteString(": ")
				for i, a := range c.Attributes {
					if i > 0 {
						b.WriteString("; ")
					}
					b.WriteString(describeAttr(a))
				}
			}
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "Conditions: %s.\n", strings.Join(inventory.Conditions, ", "))
	}
	if parts&gSpellings != 0 {
		if l := listLine(g.Brands, maxBrands); l != "" {
			fmt.Fprintf(&b, "Brands already in the inventory (reuse this spelling): %s\n", l)
		}
		if l := listLine(g.Names, maxNames); l != "" {
			fmt.Fprintf(&b, "Names already in the inventory (follow this style): %s\n", l)
		}
	}
	b.WriteString("</household>")
	if parts&gPlaces != 0 {
		b.WriteString("\n<places>\n")
		if len(g.Places) == 0 {
			b.WriteString("(none)\n")
		}
		for i, p := range g.Places {
			if i == maxPlaces {
				break
			}
			fmt.Fprintf(&b, "%s · %s\n", placeRef(i), clip(oneLine(p.Path), 120))
		}
		b.WriteString("</places>")
	}
	return b.String()
}

// placeRef is the short ID a place gets in the prompt: p1, p2, … Short IDs
// are copied without mistakes and cost few tokens; Resolve maps them back.
func placeRef(i int) string { return fmt.Sprintf("p%d", i+1) }

func describeAttr(a inventory.Attribute) string {
	s := a.Key + " ("
	switch a.Type {
	case inventory.TypeText:
		s += "text"
	case inventory.TypeInt:
		s += "whole number"
	case inventory.TypeNumber:
		s += "number"
	case inventory.TypeBool:
		s += "true or false"
	case inventory.TypeEnum:
		s += strings.Join(a.Values, "|")
	case inventory.TypeMulti:
		s += "any of " + strings.Join(a.Values, "|")
	}
	if a.Hint != "" {
		s += ", " + a.Hint
	}
	return s + ")"
}

func listLine(xs []string, max int) string {
	var out []string
	for _, x := range xs {
		if x = oneLine(x); x != "" {
			out = append(out, clip(x, 60))
		}
		if len(out) == max {
			break
		}
	}
	return strings.Join(out, ", ")
}

// languageName names the household language for the model; unknown tags are
// passed as they are, which models understand too.
func languageName(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		tag = "en"
	}
	base, _, _ := strings.Cut(strings.ToLower(tag), "-")
	names := map[string]string{
		"en": "English", "tr": "Turkish", "de": "German", "fr": "French", "es": "Spanish",
		"it": "Italian", "nl": "Dutch", "pt": "Portuguese", "pl": "Polish", "sv": "Swedish",
		"da": "Danish", "nb": "Norwegian", "fi": "Finnish", "cs": "Czech", "el": "Greek",
		"ru": "Russian", "uk": "Ukrainian", "ja": "Japanese", "zh": "Chinese", "ko": "Korean", "ar": "Arabic",
	}
	if n, ok := names[base]; ok {
		return n + " (" + oneLine(tag) + ")"
	}
	return oneLine(tag)
}

// dateOrder says how a household reads 03/04/2025.
func dateOrder(tag string) string {
	switch strings.ToLower(strings.TrimSpace(tag)) {
	case "en-us", "en-ph":
		return "month/day/year"
	}
	return "day/month/year"
}
