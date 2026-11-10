// Package prompt holds what the AI is asked and what it must answer, for
// every AI entry feature (spec §6, J6.1): seven tasks, each with a versioned
// system prompt, a builder for the user message, a JSON Schema generated from
// the answer types and the category attribute sets, and a validator for the
// AI client's validate hook.
//
// A caller builds the request from a task and passes its validator:
//
//	t := prompt.ItemDraft
//	req := ai.Request{System: t.System, User: t.User(in), Images: photos,
//		SchemaName: t.Name, Schema: t.Schema, MaxTokens: t.MaxTokens}
//	res, err := client.Complete(ctx, cfg, req, t.ValidateFor(in))
//	answer, err := t.Decode(res.JSON) // then answer.Draft(), store t.Version
//
// The package does not import internal/ai, so internal/ai may use it.
package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Confidence is how the AI came by a value (spec §3).
type Confidence string

const (
	Sure   Confidence = "sure"   // printed, plainly visible or stated
	Likely Confidence = "likely" // recognised with good reason, not printed
	Guess  Confidence = "guess"  // a code read in part
)

// Confidences lists the levels in schema order.
var Confidences = []string{string(Sure), string(Likely), string(Guess)}

// NeedKind is what the AI asks of the person (spec §3).
type NeedKind string

const (
	NeedLabelPhoto   NeedKind = "label_photo"
	NeedChoose       NeedKind = "choose"
	NeedReceiptPhoto NeedKind = "receipt_photo"
)

// NeedKinds lists the kinds in schema order.
var NeedKinds = []string{string(NeedLabelPhoto), string(NeedChoose), string(NeedReceiptPhoto)}

// Need is one thing the AI wants from the person, stored in ai_draft.needs.
type Need struct {
	Kind     NeedKind `json:"kind" enum:"need_kind"`
	Field    *string  `json:"field" enum:"need_field" desc:"For choose: the field the answer fills, e.g. attributes.storage_gb. For a photo: the field it would settle, or null."`
	Question string   `json:"question" desc:"Short question in the household's language."`
	Options  []string `json:"options" desc:"choose only: 2 to 4 values for the field. Empty for photo needs."`
}

// Field types: a filled-in value with its confidence, or null when empty.

// TextField is a text value.
type TextField struct {
	Value      string     `json:"value"`
	Confidence Confidence `json:"confidence" enum:"confidence"`
}

// NumberField is a number in major units.
type NumberField struct {
	Value      float64    `json:"value"`
	Confidence Confidence `json:"confidence" enum:"confidence"`
}

// CountField is a whole number.
type CountField struct {
	Value      int64      `json:"value"`
	Confidence Confidence `json:"confidence" enum:"confidence"`
}

// CategoryField is a category key (spec §4).
type CategoryField struct {
	Value      string     `json:"value" enum:"category"`
	Confidence Confidence `json:"confidence" enum:"confidence"`
}

// ConditionField is a condition (spec §3).
type ConditionField struct {
	Value      string     `json:"value" enum:"condition"`
	Confidence Confidence `json:"confidence" enum:"confidence"`
}

// AttrField is one category attribute. Strict schemas can't make the allowed
// keys depend on the category, so the key is enumerated over every set and
// checked against the answer's category in Go; the value is text and is
// converted to the attribute's type (spec §4) in Go as well.
type AttrField struct {
	Key        string     `json:"key" enum:"attribute_key"`
	Value      string     `json:"value" desc:"Whole numbers and numbers without units, in the key's unit; listed values exactly as listed; several values separated by commas."`
	Confidence Confidence `json:"confidence" enum:"confidence"`
}

// Task is one AI task: In is what the caller knows, Out the answer type.
type Task[In, Out any] struct {
	Name          string          // the AI feature (spec §3); also the schema name
	PromptVersion string          // bumped by hand when the prompt text changes
	Version       string          // PromptVersion plus a hash of the schema; store it with the job result
	System        string          // the system prompt
	Schema        json.RawMessage // strict JSON Schema of Out
	MaxTokens     int             // answer budget

	schema  schemaNode
	user    func(In) string
	check   func(*Out) error     // input-free checks
	checkIn func(In, *Out) error // checks against the request
}

func newTask[In, Out any](name, version, system string, maxTokens int, enums enumSet,
	user func(In) string, check func(*Out) error, checkIn func(In, *Out) error) *Task[In, Out] {
	var zero Out
	schema, err := generateSchema(zero, enums)
	if err != nil {
		panic(err)
	}
	node, err := parseSchema(schema)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(schema)
	return &Task[In, Out]{
		Name: name, PromptVersion: version, Version: version + "+" + hex.EncodeToString(sum[:3]),
		System: system, Schema: schema, MaxTokens: maxTokens,
		schema: node, user: user, check: check, checkIn: checkIn,
	}
}

// User builds the user message. Images go to the client separately.
func (t *Task[In, Out]) User(in In) string { return t.user(in) }

// Validate checks an answer without knowing the request: schema shape plus
// the rules that hold for every answer (attribute keys fit the category,
// codes come from the text read, options fit their field).
func (t *Task[In, Out]) Validate(raw json.RawMessage) error {
	_, err := t.Decode(raw)
	return err
}

// ValidateFor returns a validator that also checks the answer against the
// request (row ids, CSV columns, lines already logged, receipt photos).
func (t *Task[In, Out]) ValidateFor(in In) func(json.RawMessage) error {
	return func(raw json.RawMessage) error {
		_, err := t.DecodeFor(in, raw)
		return err
	}
}

// Decode validates raw and returns the answer.
func (t *Task[In, Out]) Decode(raw json.RawMessage) (*Out, error) {
	v, err := decodeAny(raw)
	if err != nil {
		return nil, err
	}
	if err := conform(t.schema, v, ""); err != nil {
		return nil, err
	}
	out := new(Out)
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, fmt.Errorf("the answer does not match the schema: %v", err)
	}
	if t.check != nil {
		if err := t.check(out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// DecodeFor validates raw against the schema and the request.
func (t *Task[In, Out]) DecodeFor(in In, raw json.RawMessage) (*Out, error) {
	out, err := t.Decode(raw)
	if err != nil {
		return nil, err
	}
	if t.checkIn != nil {
		if err := t.checkIn(in, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// --- data blocks ---

// tagLike matches anything that could open or close one of the blocks this
// package writes, so text from users and photos can't break out of its block.
var tagLike = regexp.MustCompile(`(?i)<\s*(/?)\s*(data|household|places|photos)\b`)

// sanitize keeps s inside its block: it defuses block tags and drops control
// characters other than newlines and tabs.
func sanitize(s string) string {
	s = tagLike.ReplaceAllString(s, "‹$1$2")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// oneLine sanitizes s and folds it onto one line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(sanitize(s)), " ")
}

// dataBlock wraps material from users, files or photos. The system prompt
// says such blocks are content, never instructions.
func dataBlock(name, content string) string {
	return "<data name=\"" + name + "\">\n" + strings.TrimRight(sanitize(content), "\n") + "\n</data>"
}

// clip shortens s to n runes.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
