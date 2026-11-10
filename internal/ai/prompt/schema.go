package prompt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Schemas are generated from the answer types in this package by reflection:
//
//   - a struct is an object listing every field in `required`, with
//     additionalProperties false (OpenAI strict mode);
//   - a pointer is the same type or null;
//   - a slice is an array;
//   - `enum:"name"` takes the values from an enumSet built from the
//     inventory catalog, so a new category or attribute key reaches the schema
//     without touching a prompt;
//   - `desc:"…"` becomes the description the model reads.
//
// Properties keep the Go field order: models write JSON in schema order, so
// what the model should settle first (read_text, category) comes first.

// enumSet holds the enumerations a schema may refer to by name.
type enumSet map[string][]string

// kv and ordered marshal a JSON object with its keys in insertion order.
type kv struct {
	k string
	v any
}

type ordered []kv

func (o ordered) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(p.k)
		b.Write(k)
		b.WriteByte(':')
		v, err := json.Marshal(p.v)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func (o ordered) get(k string) (any, bool) {
	for _, p := range o {
		if p.k == k {
			return p.v, true
		}
	}
	return nil, false
}

func (o ordered) set(k string, v any) ordered {
	for i, p := range o {
		if p.k == k {
			o[i].v = v
			return o
		}
	}
	return append(o, kv{k, v})
}

// generateSchema returns the JSON Schema of v's type.
func generateSchema(v any, enums enumSet) (json.RawMessage, error) {
	s, err := schemaOf(reflect.TypeOf(v), "", "", enums)
	if err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

func schemaOf(t reflect.Type, enumName, desc string, enums enumSet) (ordered, error) {
	switch t.Kind() {
	case reflect.Pointer:
		inner, err := schemaOf(t.Elem(), enumName, desc, enums)
		if err != nil {
			return nil, err
		}
		return nullable(inner), nil
	case reflect.Struct:
		return objectSchema(t, desc, enums)
	case reflect.Slice:
		items, err := schemaOf(t.Elem(), enumName, "", enums)
		if err != nil {
			return nil, err
		}
		s := ordered{{"type", "array"}}
		if desc != "" {
			s = append(s, kv{"description", desc})
		}
		return append(s, kv{"items", items}), nil
	}
	var typ string
	switch t.Kind() {
	case reflect.String:
		typ = "string"
	case reflect.Int, reflect.Int32, reflect.Int64:
		typ = "integer"
	case reflect.Float32, reflect.Float64:
		typ = "number"
	case reflect.Bool:
		typ = "boolean"
	default:
		return nil, fmt.Errorf("prompt: no schema for %s", t)
	}
	s := ordered{{"type", typ}}
	if desc != "" {
		s = append(s, kv{"description", desc})
	}
	if enumName != "" {
		vals, ok := enums[enumName]
		if !ok || len(vals) == 0 {
			return nil, fmt.Errorf("prompt: unknown or empty enum %q", enumName)
		}
		e := make([]any, len(vals))
		for i, v := range vals {
			e[i] = v
		}
		s = append(s, kv{"enum", e})
	}
	return s, nil
}

func objectSchema(t reflect.Type, desc string, enums enumSet) (ordered, error) {
	var props ordered
	var required []string
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		ps, err := schemaOf(f.Type, f.Tag.Get("enum"), f.Tag.Get("desc"), enums)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", t.Name(), f.Name, err)
		}
		props = append(props, kv{name, ps})
		required = append(required, name)
	}
	s := ordered{{"type", "object"}}
	if desc != "" {
		s = append(s, kv{"description", desc})
	}
	return append(s, kv{"properties", props}, kv{"required", required}, kv{"additionalProperties", false}), nil
}

// nullable lets s also be null: scalars get a type list (and null in their
// enum), objects and arrays an anyOf.
func nullable(s ordered) ordered {
	typ, _ := s.get("type")
	if t, ok := typ.(string); ok && t != "object" && t != "array" {
		s = s.set("type", []string{t, "null"})
		if e, ok := s.get("enum"); ok {
			s = s.set("enum", append(slices.Clone(e.([]any)), nil))
		}
		return s
	}
	return ordered{{"anyOf", []any{s, ordered{{"type", "null"}}}}}
}

// --- validation of answers against a generated schema ---

// schemaNode is a parsed schema, as generic JSON.
type schemaNode = map[string]any

func parseSchema(raw json.RawMessage) (schemaNode, error) {
	var n schemaNode
	return n, json.Unmarshal(raw, &n)
}

// decodeAny decodes raw keeping numbers as json.Number.
func decodeAny(raw json.RawMessage) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, fmt.Errorf("the answer is not valid JSON: %v", err)
	}
	if d.More() {
		return nil, fmt.Errorf("the answer holds more than one JSON value")
	}
	return v, nil
}

// conform checks v against the schema subset this package generates: type,
// properties, required, additionalProperties, items, enum, anyOf. Error
// messages go back to the model, so they say what to change.
func conform(s schemaNode, v any, path string) error {
	if alts, ok := s["anyOf"].([]any); ok {
		var first error
		for _, a := range alts {
			err := conform(a.(schemaNode), v, path)
			if err == nil {
				return nil
			}
			if first == nil && v != nil {
				first = err
			}
		}
		if first != nil {
			return first
		}
		return fmt.Errorf("%s: has the wrong type", where(path))
	}
	if !typeMatches(s["type"], v) {
		return fmt.Errorf("%s: must be %s, not %s", where(path), typeWords(s["type"]), jsonKind(v))
	}
	if e, ok := s["enum"].([]any); ok {
		found := false
		for _, x := range e {
			if fmt.Sprint(x) == fmt.Sprint(v) && (x == nil) == (v == nil) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s: %s is not one of %s", where(path), short(v), enumWords(e))
		}
	}
	switch x := v.(type) {
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		for k := range x {
			if _, ok := props[k]; !ok {
				return fmt.Errorf("%s: unexpected key %q; use only the keys of the schema", where(path), k)
			}
		}
		req, _ := s["required"].([]any)
		for _, r := range req {
			if _, ok := x[r.(string)]; !ok {
				return fmt.Errorf("%s: key %q is missing (use null when there is no value)", where(path), r)
			}
		}
		for _, r := range req {
			k := r.(string)
			if err := conform(props[k].(schemaNode), x[k], join(path, k)); err != nil {
				return err
			}
		}
	case []any:
		items, _ := s["items"].(schemaNode)
		for i, e := range x {
			if err := conform(items, e, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func typeMatches(t any, v any) bool {
	switch tt := t.(type) {
	case string:
		return kindIs(tt, v)
	case []any:
		for _, x := range tt {
			if kindIs(x.(string), v) {
				return true
			}
		}
	}
	return false
}

func kindIs(t string, v any) bool {
	switch t {
	case "null":
		return v == nil
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "number":
		_, ok := v.(json.Number)
		return ok
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		_, err := n.Int64()
		return err == nil
	}
	return false
}

func jsonKind(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case map[string]any:
		return "an object"
	case []any:
		return "a list"
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return "an integer"
		}
		return "a number with a fraction"
	}
	return "unknown"
}

func typeWords(t any) string {
	switch tt := t.(type) {
	case string:
		return article(tt)
	case []any:
		w := make([]string, len(tt))
		for i, x := range tt {
			w[i] = article(x.(string))
		}
		return strings.Join(w, " or ")
	}
	return "?"
}

func article(t string) string {
	switch t {
	case "null":
		return "null"
	case "integer":
		return "a whole number"
	case "object", "array":
		if t == "array" {
			return "a list"
		}
		return "an object"
	}
	return "a " + t
}

func enumWords(e []any) string {
	w := make([]string, len(e))
	for i, x := range e {
		if x == nil {
			w[i] = "null"
		} else {
			w[i] = fmt.Sprintf("%q", x)
		}
	}
	return strings.Join(w, ", ")
}

func short(v any) string {
	b, _ := json.Marshal(v)
	if len(b) > 60 {
		return string(b[:57]) + "..."
	}
	return string(b)
}

func where(path string) string {
	if path == "" {
		return "the answer"
	}
	return path
}

func join(path, k string) string {
	if path == "" {
		return k
	}
	return path + "." + k
}

// --- strict-mode structure check ---

// strictKeywords are the only schema keywords generated schemas use. They are
// supported by OpenAI strict mode and by the providers OpenRouter routes
// structured output to; length, range, format and pattern keywords are not.
var strictKeywords = []string{"type", "description", "properties", "required", "additionalProperties", "items", "enum", "anyOf"}

// Limits from OpenAI's structured outputs documentation, kept on the safe side.
const (
	strictMaxDepth      = 5
	strictMaxProperties = 100
	strictMaxEnumValues = 500
)

// CheckStrict reports whether schema follows the rules of OpenAI-style strict
// structured output: the root is an object; every object lists all its
// properties in required and sets additionalProperties to false; optional
// values are nullable types, never missing keys; no unsupported keywords;
// nesting, property and enum counts within limits.
func CheckStrict(schema json.RawMessage) error {
	root, err := parseSchema(schema)
	if err != nil {
		return fmt.Errorf("schema is not JSON: %w", err)
	}
	if root["type"] != "object" {
		return fmt.Errorf("root must be type object (not anyOf or a type list)")
	}
	c := &strictCounter{}
	if err := checkStrict(root, "$", 0, c); err != nil {
		return err
	}
	if c.props > strictMaxProperties {
		return fmt.Errorf("%d object properties, more than %d", c.props, strictMaxProperties)
	}
	if c.enums > strictMaxEnumValues {
		return fmt.Errorf("%d enum values, more than %d", c.enums, strictMaxEnumValues)
	}
	return nil
}

type strictCounter struct{ props, enums int }

func checkStrict(s schemaNode, path string, depth int, c *strictCounter) error {
	for k := range s {
		if !slices.Contains(strictKeywords, k) {
			return fmt.Errorf("%s: keyword %q is not supported in strict mode", path, k)
		}
	}
	if d, ok := s["description"]; ok {
		if _, ok := d.(string); !ok {
			return fmt.Errorf("%s: description must be a string", path)
		}
	}
	if alts, ok := s["anyOf"]; ok {
		if len(s) != 1 {
			return fmt.Errorf("%s: anyOf must stand alone", path)
		}
		list, ok := alts.([]any)
		if !ok || len(list) < 2 {
			return fmt.Errorf("%s: anyOf needs at least two branches", path)
		}
		for i, a := range list {
			n, ok := a.(schemaNode)
			if !ok {
				return fmt.Errorf("%s: anyOf branch is not a schema", path)
			}
			if err := checkStrict(n, fmt.Sprintf("%s.anyOf[%d]", path, i), depth, c); err != nil {
				return err
			}
		}
		return nil
	}
	types, err := strictTypes(s["type"], path)
	if err != nil {
		return err
	}
	main := ""
	for _, t := range types {
		if t != "null" {
			if main != "" {
				return fmt.Errorf("%s: only one non-null type allowed, got %v", path, types)
			}
			main = t
		}
	}
	if (main == "object" || main == "array") && len(types) > 1 {
		return fmt.Errorf("%s: a nullable %s must use anyOf with {\"type\":\"null\"}", path, main)
	}
	if e, ok := s["enum"]; ok {
		list, ok := e.([]any)
		if !ok || len(list) == 0 {
			return fmt.Errorf("%s: enum must be a non-empty list", path)
		}
		c.enums += len(list)
		hasNull := false
		for _, v := range list {
			if v == nil {
				hasNull = true
				continue
			}
			if _, ok := v.(string); !ok || main != "string" {
				return fmt.Errorf("%s: enum values must be strings of a string type", path)
			}
		}
		if hasNull != slices.Contains(types, "null") {
			return fmt.Errorf("%s: a nullable enum must list null, and only a nullable one", path)
		}
	}
	switch main {
	case "object":
		if depth+1 > strictMaxDepth {
			return fmt.Errorf("%s: objects nested deeper than %d", path, strictMaxDepth)
		}
		if s["additionalProperties"] != false {
			return fmt.Errorf("%s: additionalProperties must be false", path)
		}
		props, ok := s["properties"].(map[string]any)
		if !ok || len(props) == 0 {
			return fmt.Errorf("%s: an object needs properties", path)
		}
		c.props += len(props)
		req, _ := s["required"].([]any)
		if len(req) != len(props) {
			return fmt.Errorf("%s: required must list all %d properties, lists %d", path, len(props), len(req))
		}
		for _, r := range req {
			if _, ok := props[fmt.Sprint(r)]; !ok {
				return fmt.Errorf("%s: required names unknown property %v", path, r)
			}
		}
		for k, p := range props {
			n, ok := p.(schemaNode)
			if !ok {
				return fmt.Errorf("%s.%s: not a schema", path, k)
			}
			if err := checkStrict(n, path+"."+k, depth+1, c); err != nil {
				return err
			}
		}
	case "array":
		items, ok := s["items"].(schemaNode)
		if !ok {
			return fmt.Errorf("%s: an array needs items", path)
		}
		if err := checkStrict(items, path+"[]", depth, c); err != nil {
			return err
		}
	default:
		for _, k := range []string{"properties", "required", "additionalProperties", "items"} {
			if _, ok := s[k]; ok {
				return fmt.Errorf("%s: %q on a %s", path, k, main)
			}
		}
	}
	return nil
}

func strictTypes(t any, path string) ([]string, error) {
	valid := []string{"string", "number", "integer", "boolean", "object", "array", "null"}
	var out []string
	switch tt := t.(type) {
	case string:
		out = []string{tt}
	case []any:
		for _, x := range tt {
			s, ok := x.(string)
			if !ok {
				return nil, fmt.Errorf("%s: type list holds a non-string", path)
			}
			out = append(out, s)
		}
	default:
		return nil, fmt.Errorf("%s: missing type", path)
	}
	for _, x := range out {
		if !slices.Contains(valid, x) {
			return nil, fmt.Errorf("%s: unknown type %q", path, x)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: empty type list", path)
	}
	return out, nil
}
