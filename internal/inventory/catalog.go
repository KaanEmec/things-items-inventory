package inventory

import (
	"fmt"
	"slices"
)

// The category attribute sets (spec §4) live here and only here. They are
// served by GET /api/v1/categories, checked when an item is written and used
// by the AI schemas. Every attribute is optional.

// AttrType is the type of one attribute.
type AttrType string

const (
	TypeText   AttrType = "text"
	TypeInt    AttrType = "int"
	TypeNumber AttrType = "number"
	TypeBool   AttrType = "bool"
	TypeEnum   AttrType = "enum"
	TypeMulti  AttrType = "multi" // several enum values
)

// Attribute describes one category attribute.
type Attribute struct {
	Key    string   `json:"key"`
	Type   AttrType `json:"type"`
	Values []string `json:"values,omitempty"` // enum and multi
	Hint   string   `json:"hint,omitempty"`   // how to fill it in, for forms and AI prompts
}

// Category is a kind of belonging and its attribute set.
type Category struct {
	Key        string      `json:"key"`
	Attributes []Attribute `json:"attributes"`
}

// Attr returns the attribute with the given key.
func (c Category) Attr(key string) (Attribute, bool) {
	for _, a := range c.Attributes {
		if a.Key == key {
			return a, true
		}
	}
	return Attribute{}, false
}

// FallbackCategory is where unknown categories land.
const FallbackCategory = "other"

// Categories lists every category, in the order forms show them.
var Categories = []Category{
	{Key: "gadget", Attributes: []Attribute{
		{Key: "type", Type: TypeEnum, Values: []string{"phone", "laptop", "tablet", "desktop", "monitor", "headphones", "speaker", "camera", "console", "watch", "charger", "router", "other"}},
		{Key: "storage_gb", Type: TypeInt},
		{Key: "ram_gb", Type: TypeInt},
		{Key: "screen_in", Type: TypeNumber, Hint: "inches"},
		{Key: "connectivity", Type: TypeMulti, Values: []string{"usb_c", "lightning", "micro_usb", "hdmi", "bluetooth", "wifi", "cellular", "ethernet"}},
		{Key: "power", Type: TypeText, Hint: "as printed, 65 W, 5 V 3 A"},
		{Key: "year", Type: TypeInt, Hint: "model or manufacture year"},
	}},
	{Key: "clothing", Attributes: []Attribute{
		{Key: "type", Type: TypeEnum, Values: []string{"top", "shirt", "sweater", "jacket", "coat", "trousers", "shorts", "skirt", "dress", "shoes", "underwear", "sportswear", "other"}},
		{Key: "size", Type: TypeText, Hint: "as on the tag (M, 42, 32/34)"},
		{Key: "material", Type: TypeText, Hint: "80% cotton, 20% polyester"},
		{Key: "season", Type: TypeEnum, Values: []string{"summer", "winter", "spring_autumn", "all"}},
		{Key: "care", Type: TypeText, Hint: "care label summary"},
	}},
	{Key: "accessory", Attributes: []Attribute{
		{Key: "type", Type: TypeEnum, Values: []string{"bag", "wallet", "belt", "jewellery", "watch", "glasses", "hat", "scarf", "gloves", "umbrella", "case", "other"}},
		{Key: "material", Type: TypeText},
		{Key: "size", Type: TypeText},
		{Key: "fits", Type: TypeText, Hint: "what it belongs to (iPhone 13, ski boots)"},
		{Key: "season", Type: TypeEnum, Values: []string{"summer", "winter", "spring_autumn", "all"}},
	}},
	{Key: "part", Attributes: []Attribute{
		{Key: "type", Type: TypeEnum, Values: []string{"cable", "adapter", "connector", "resistor", "capacitor", "ic", "module", "sensor", "battery", "fastener", "tool", "other"}},
		{Key: "value", Type: TypeText, Hint: "10 kΩ, 100 µF, AA"},
		{Key: "package", Type: TypeText, Hint: "0805, DIP-8, TO-220"},
		{Key: "ends", Type: TypeText, Hint: "cable/adapter ends, USB-C to HDMI"},
		{Key: "length_m", Type: TypeNumber},
		{Key: "rating", Type: TypeText, Hint: "5 V 3 A, 250 V"},
	}},
	{Key: FallbackCategory, Attributes: []Attribute{
		{Key: "type", Type: TypeText, Hint: "free (book, pan, lamp)"},
		{Key: "material", Type: TypeText},
		{Key: "dimensions", Type: TypeText, Hint: "40×30×20 cm"},
		{Key: "weight_kg", Type: TypeNumber},
		{Key: "power", Type: TypeText, Hint: "for powered things"},
	}},
}

// Enumerations of spec §3 used by the inventory.
var (
	Statuses   = []string{"in_use", "stored", "packed", "to_sell", "lent", "gone"}
	Conditions = []string{"new", "like_new", "good", "worn", "broken"}
	PlaceKinds = []string{"floor", "room", "furniture", "spot", "box"}
	RoomColors = []string{"red", "orange", "yellow", "green", "teal", "blue", "purple", "pink", "brown", "grey"}
	Sources    = []string{"manual", "capture", "quick_add", "text", "import", "agent"}
)

// CategoryByKey returns the category with the given key.
func CategoryByKey(key string) (Category, bool) {
	for _, c := range Categories {
		if c.Key == key {
			return c, true
		}
	}
	return Category{}, false
}

// ValidateAttribute checks v (a decoded JSON value) against a and returns the
// value to store: integers as int64, numbers as float64, enum values as given.
func ValidateAttribute(a Attribute, v any) (any, error) {
	switch a.Type {
	case TypeText:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("must be text")
		}
		if len(s) > 500 {
			return nil, fmt.Errorf("must be at most 500 characters")
		}
		return s, nil
	case TypeInt:
		n, ok := asInt(v)
		if !ok {
			return nil, fmt.Errorf("must be a whole number")
		}
		return n, nil
	case TypeNumber:
		f, ok := asFloat(v)
		if !ok {
			return nil, fmt.Errorf("must be a number")
		}
		return f, nil
	case TypeBool:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("must be true or false")
		}
		return b, nil
	case TypeEnum:
		s, ok := v.(string)
		if !ok || !slices.Contains(a.Values, s) {
			return nil, fmt.Errorf("must be one of %v", a.Values)
		}
		return s, nil
	case TypeMulti:
		list, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("must be a list, with values from %v", a.Values)
		}
		out := make([]any, 0, len(list))
		for _, e := range list {
			s, ok := e.(string)
			if !ok || !slices.Contains(a.Values, s) {
				return nil, fmt.Errorf("must only hold values from %v", a.Values)
			}
			if !slices.Contains(out, any(s)) {
				out = append(out, s)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown attribute type %q", a.Type)
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func asInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n == float64(int64(n)) && n > -1<<53 && n < 1<<53 {
			return int64(n), true
		}
	}
	return 0, false
}
