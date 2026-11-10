package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"unicode"
)

type outcome int

const (
	right outcome = iota
	empty
	wrong
)

type tally struct{ n [3]int }

type score struct {
	fields                  map[string]*tally
	failed, cases           int
	tokensIn, tokensOut     int
	cost                    float64
	modes                   map[string]int
	extraRows, expectedRows int
}

func newScore() *score {
	return &score{fields: map[string]*tally{}, modes: map[string]int{}}
}

func (s *score) count(field string, o outcome) {
	t := s.fields[field]
	if t == nil {
		t = &tally{}
		s.fields[field] = t
	}
	t.n[o]++
}

// add scores one case and returns a line per wrong field, for -v.
func (s *score) add(c *evalCase, r result) []string {
	s.cases++
	s.tokensIn += r.res.Usage.PromptTokens
	s.tokensOut += r.res.Usage.CompletionTokens
	s.cost += r.res.Usage.Cost
	if r.res.Mode != "" {
		s.modes[string(r.res.Mode)]++
	}
	if r.err != nil {
		s.failed++
	}
	var notes []string
	listKey := ""
	for _, k := range []string{"rows", "lines"} {
		if _, ok := c.exp.Expect[k]; ok {
			listKey = k
		}
	}
	if listKey == "" {
		for field, want := range c.exp.Expect {
			got, ok := r.flat[field]
			if !ok {
				got = nil
			}
			o := compare(field, want, got)
			s.count(field, o)
			if o == wrong {
				notes = append(notes, fmt.Sprintf("%s: want %s, got %v", field, want, show(got)))
			}
		}
		return notes
	}
	var expRows []map[string]json.RawMessage
	if err := json.Unmarshal(c.exp.Expect[listKey], &expRows); err != nil {
		notes = append(notes, fmt.Sprintf("expect.%s is not a list of objects: %v", listKey, err))
		return notes
	}
	s.expectedRows += len(expRows)
	used := make([]bool, len(r.rows))
	for _, er := range expRows {
		j := matchRow(er, r.rows, used)
		var got map[string]any
		if j >= 0 {
			used[j] = true
			got = r.rows[j]
		}
		for field, want := range er {
			var g any
			if got != nil {
				g = got[field]
			}
			o := compare(field, want, g)
			s.count(listKey+"."+field, o)
			if o == wrong {
				notes = append(notes, fmt.Sprintf("%s.%s: want %s, got %v", listKey, field, want, show(g)))
			}
		}
	}
	for j, u := range used {
		if !u {
			s.extraRows++
			notes = append(notes, fmt.Sprintf("extra %s: %v", strings.TrimSuffix(listKey, "s"), show(r.rows[j]["name"])))
		}
	}
	return notes
}

// matchRow finds the unused answer row for an expected row: by id when the
// expected row has one, otherwise the row whose name shares most words.
func matchRow(exp map[string]json.RawMessage, rows []map[string]any, used []bool) int {
	if raw, ok := exp["id"]; ok {
		var id string
		_ = json.Unmarshal(raw, &id)
		for j, r := range rows {
			if !used[j] && fmt.Sprint(r["id"]) == id {
				return j
			}
		}
		return -1
	}
	var names []string
	if raw, ok := exp["name"]; ok {
		names = alternatives(raw)
	}
	best, bestScore := -1, 0.0
	for j, r := range rows {
		if used[j] {
			continue
		}
		got, _ := r["name"].(string)
		for _, n := range names {
			if sc := overlap(n, got); sc > bestScore {
				best, bestScore = j, sc
			}
		}
	}
	if bestScore < 0.34 {
		return -1
	}
	return best
}

func alternatives(raw json.RawMessage) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var alt struct {
		OneOf []string `json:"oneOf"`
	}
	_ = json.Unmarshal(raw, &alt)
	return alt.OneOf
}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		out[w] = true
	}
	return out
}

// overlap is the share of words two names have in common (Jaccard).
func overlap(a, b string) float64 {
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	common := 0
	for w := range wa {
		if wb[w] {
			common++
		}
	}
	return float64(common) / float64(len(wa)+len(wb)-common)
}

// exactFields compare case- and space-exactly (after trimming): codes.
var exactFields = map[string]bool{"model": true, "serial": true}

// compare scores one field. want is the expected JSON: null means the field
// should stay empty; {"oneOf": [...]} lists accepted values; a list is
// compared as a set.
func compare(field string, want json.RawMessage, got any) outcome {
	var w any
	if err := json.Unmarshal(want, &w); err != nil {
		return wrong
	}
	if w == nil {
		if got == nil {
			return right
		}
		return wrong
	}
	if got == nil {
		return empty
	}
	if m, ok := w.(map[string]any); ok {
		if alts, ok := m["oneOf"].([]any); ok {
			for _, a := range alts {
				if equal(field, a, got) {
					return right
				}
			}
			return wrong
		}
	}
	if equal(field, w, got) {
		return right
	}
	return wrong
}

func equal(field string, want, got any) bool {
	base := field[strings.LastIndex(field, ".")+1:]
	switch w := want.(type) {
	case string:
		g, ok := got.(string)
		if !ok {
			return false
		}
		if exactFields[base] {
			return strings.TrimSpace(w) == strings.TrimSpace(g)
		}
		return fold(w) == fold(g)
	case float64:
		switch g := got.(type) {
		case int64:
			return math.Abs(w-float64(g)) < 1e-9
		case float64:
			return math.Abs(w-g) < 1e-6
		}
	case bool:
		g, ok := got.(bool)
		return ok && g == w
	case []any:
		g, ok := got.([]any)
		if !ok {
			return false
		}
		return sameSet(w, g)
	}
	return false
}

func fold(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func sameSet(a, b []any) bool {
	set := func(xs []any) map[string]bool {
		m := map[string]bool{}
		for _, x := range xs {
			m[fold(fmt.Sprint(x))] = true
		}
		return m
	}
	sa, sb := set(a), set(b)
	if len(sa) != len(sb) {
		return false
	}
	for k := range sa {
		if !sb[k] {
			return false
		}
	}
	return true
}

func show(v any) string {
	if v == nil {
		return "(empty)"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func pct(n, total int) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%3.0f%%", 100*float64(n)/float64(total))
}

func (s *score) print(w io.Writer) {
	names := make([]string, 0, len(s.fields))
	for f := range s.fields {
		names = append(names, f)
	}
	sort.Strings(names)
	fmt.Fprintf(w, "%-30s %4s %6s %6s %6s\n", "field", "n", "right", "empty", "wrong")
	for _, f := range names {
		t := s.fields[f]
		n := t.n[0] + t.n[1] + t.n[2]
		fmt.Fprintf(w, "%-30s %4d %6s %6s %6s\n", f, n, pct(t.n[right], n), pct(t.n[empty], n), pct(t.n[wrong], n))
	}
	if s.expectedRows > 0 {
		fmt.Fprintf(w, "extra rows or lines not in the expected answers: %d (against %d expected)\n", s.extraRows, s.expectedRows)
	}
	var modes []string
	for m, n := range s.modes {
		modes = append(modes, fmt.Sprintf("%s ×%d", m, n))
	}
	sort.Strings(modes)
	fmt.Fprintf(w, "\ncases: %d, failed: %d · modes: %s · tokens in %d, out %d", s.cases, s.failed, strings.Join(modes, ", "), s.tokensIn, s.tokensOut)
	if s.cost > 0 {
		fmt.Fprintf(w, " · cost $%.4f", s.cost)
	}
	fmt.Fprintln(w)

	var wr, em int
	for _, f := range []string{"brand", "model", "rows.brand", "rows.model"} {
		if t := s.fields[f]; t != nil {
			wr += t.n[wrong]
			em += t.n[empty]
		}
	}
	verdict := "PASS"
	if wr > 0 && wr >= em {
		verdict = "FAIL"
	}
	fmt.Fprintf(w, "brand and model: %d wrong, %d empty → %s (a wrong value must be rarer than an empty one)\n", wr, em, verdict)
}
