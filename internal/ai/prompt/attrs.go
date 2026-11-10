package prompt

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/inventory"
)

// catalog is the category attribute sets a task was built from. Tasks use
// inventory.Categories; tests build tasks from a changed copy.
type catalog []inventory.Category

func (c catalog) category(key string) (inventory.Category, bool) {
	for _, cat := range c {
		if cat.Key == key {
			return cat, true
		}
	}
	return inventory.Category{}, false
}

// keys returns the category keys.
func (c catalog) keys() []string {
	out := make([]string, len(c))
	for i, cat := range c {
		out[i] = cat.Key
	}
	return out
}

// attrKeys returns every attribute key of every set, once, in catalog order.
func (c catalog) attrKeys() []string {
	var out []string
	for _, cat := range c {
		for _, a := range cat.Attributes {
			if !slices.Contains(out, a.Key) {
				out = append(out, a.Key)
			}
		}
	}
	return out
}

// coreNeedFields are the item fields a need may point at, besides attributes.
var coreNeedFields = []string{"name", "category", "brand", "model", "serial", "color", "condition", "quantity"}

// enums returns the enumerations the answer schemas refer to.
func (c catalog) enums() enumSet {
	needFields := slices.Clone(coreNeedFields)
	for _, k := range c.attrKeys() {
		needFields = append(needFields, "attributes."+k)
	}
	return enumSet{
		"category":      c.keys(),
		"attribute_key": c.attrKeys(),
		"condition":     inventory.Conditions,
		"confidence":    Confidences,
		"need_kind":     NeedKinds,
		"need_field":    needFields,
		"csv_target":    csvTargets,
		"date_format":   dateFormats,
	}
}

// parseAttrs converts the answer's attributes for category into stored
// values (int64, float64, string, []any of strings, bool), keyed by
// attribute key. A key outside the category, a repeated key or a value of the
// wrong type is an error the model gets back.
func (c catalog) parseAttrs(path string, category *CategoryField, attrs []AttrField) (map[string]any, error) {
	if len(attrs) == 0 {
		return nil, nil
	}
	if category == nil {
		return nil, fmt.Errorf("%s: attributes need a category; set the category or leave attributes empty", path)
	}
	cat, ok := c.category(category.Value)
	if !ok {
		return nil, fmt.Errorf("%s: unknown category %q", path, category.Value)
	}
	out := make(map[string]any, len(attrs))
	for i, af := range attrs {
		p := fmt.Sprintf("%s[%d]", path, i)
		a, ok := cat.Attr(af.Key)
		if !ok {
			return nil, fmt.Errorf("%s: %q is not an attribute of %s; its keys are %s", p, af.Key, cat.Key, strings.Join(attrNames(cat), ", "))
		}
		if _, dup := out[a.Key]; dup {
			return nil, fmt.Errorf("%s: %q appears twice; give each key once", p, a.Key)
		}
		v, err := parseAttrValue(a, af.Value)
		if err != nil {
			return nil, fmt.Errorf("%s (%s): %v", p, a.Key, err)
		}
		out[a.Key] = v
	}
	return out, nil
}

func attrNames(cat inventory.Category) []string {
	out := make([]string, len(cat.Attributes))
	for i, a := range cat.Attributes {
		out[i] = a.Key
	}
	return out
}

// parseAttrValue turns the model's text into the decoded JSON value
// inventory.ValidateAttribute expects, and returns what that stores.
// Numbers must be bare: "1 TB" for storage_gb is rejected rather than read
// as 1.
func parseAttrValue(a inventory.Attribute, s string) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty value; leave the attribute out instead")
	}
	var v any
	switch a.Type {
	case inventory.TypeText:
		v = s
	case inventory.TypeInt, inventory.TypeNumber:
		f, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
		if err != nil {
			return nil, fmt.Errorf("%q must be a bare number without unit or text", s)
		}
		v = f
	case inventory.TypeBool:
		switch strings.ToLower(s) {
		case "true", "yes":
			v = true
		case "false", "no":
			v = false
		default:
			return nil, fmt.Errorf("%q must be true or false", s)
		}
	case inventory.TypeEnum:
		v = matchValue(a.Values, s)
	case inventory.TypeMulti:
		var list []any
		for part := range strings.FieldsFuncSeq(s, func(r rune) bool { return r == ',' || r == ';' }) {
			if part = strings.TrimSpace(part); part != "" {
				list = append(list, matchValue(a.Values, part))
			}
		}
		v = list
	}
	out, err := inventory.ValidateAttribute(a, v)
	if err != nil {
		return nil, fmt.Errorf("%q %v", s, err)
	}
	return out, nil
}

// matchValue returns the listed value s names, forgiving case and spaces
// ("Spring autumn" → spring_autumn), or s unchanged.
func matchValue(values []string, s string) string {
	norm := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", "_")
	norm = strings.ReplaceAll(norm, "-", "_")
	for _, v := range values {
		if v == norm {
			return v
		}
	}
	return s
}

// checkConfidence rejects an empty or unknown level (strict mode enforces
// it; json_object and prompt modes may not).
func checkConfidence(path string, c Confidence) error {
	if !slices.Contains(Confidences, string(c)) {
		return fmt.Errorf("%s.confidence: must be sure, likely or guess", path)
	}
	return nil
}

// checkText rejects blank values: an empty field is null, not "".
func checkText(path string, f *TextField, max int) error {
	if f == nil {
		return nil
	}
	if strings.TrimSpace(f.Value) == "" {
		return fmt.Errorf("%s: empty value; use null when there is no value", path)
	}
	if len([]rune(f.Value)) > max {
		return fmt.Errorf("%s: longer than %d characters; shorten it", path, max)
	}
	return nil
}

// checkNeeds checks the needs of an answer: at most three, options only for
// choose (2–4, valid for the field), and no choose for a field the answer
// already fills as sure.
func (c catalog) checkNeeds(needs []Need, category *CategoryField, sure func(field string) bool) error {
	if len(needs) > 3 {
		return fmt.Errorf("needs: %d needs; ask for at most 3, the most useful first", len(needs))
	}
	for i, n := range needs {
		p := fmt.Sprintf("needs[%d]", i)
		if strings.TrimSpace(n.Question) == "" || len([]rune(n.Question)) > 140 {
			return fmt.Errorf("%s.question: give a short question (1 to 140 characters)", p)
		}
		switch n.Kind {
		case NeedChoose:
			if n.Field == nil {
				return fmt.Errorf("%s: a choose need must name the field it fills", p)
			}
			if len(n.Options) < 2 || len(n.Options) > 4 {
				return fmt.Errorf("%s.options: choose needs 2 to 4 options, got %d", p, len(n.Options))
			}
			if sure != nil && sure(*n.Field) {
				return fmt.Errorf("%s: %s is already filled as sure; drop the need or leave the field null", p, *n.Field)
			}
			for j, o := range n.Options {
				if err := c.checkOption(*n.Field, o, category); err != nil {
					return fmt.Errorf("%s.options[%d]: %v", p, j, err)
				}
			}
		case NeedLabelPhoto, NeedReceiptPhoto:
			if len(n.Options) > 0 {
				return fmt.Errorf("%s.options: only choose needs have options; use an empty list", p)
			}
		default:
			return fmt.Errorf("%s.kind: unknown kind %q", p, n.Kind)
		}
	}
	return nil
}

func (c catalog) checkOption(field, o string, category *CategoryField) error {
	if strings.TrimSpace(o) == "" {
		return fmt.Errorf("empty option")
	}
	switch field {
	case "category":
		if _, ok := c.category(o); !ok {
			return fmt.Errorf("%q is not a category key", o)
		}
	case "condition":
		if !slices.Contains(inventory.Conditions, o) {
			return fmt.Errorf("%q is not a condition", o)
		}
	case "quantity":
		if n, err := strconv.Atoi(o); err != nil || n < 1 {
			return fmt.Errorf("%q is not a count", o)
		}
	}
	key, ok := strings.CutPrefix(field, "attributes.")
	if !ok {
		return nil
	}
	if category == nil {
		return fmt.Errorf("an attribute question needs the answer's category")
	}
	cat, _ := c.category(category.Value)
	a, ok := cat.Attr(key)
	if !ok {
		return fmt.Errorf("%s is not an attribute of %s", key, cat.Key)
	}
	_, err := parseAttrValue(a, o)
	return err
}
