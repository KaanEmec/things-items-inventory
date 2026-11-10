package search

import (
	"context"
	"database/sql"
	"strings"
	"unicode/utf16"
)

// Mark is a highlighted range [Start, End) in a string, counted in UTF-16
// code units, so JavaScript can slice with it directly: text.slice(s, e).
type Mark [2]int

// Match is one search hit.
type Match struct {
	Kind         string `json:"kind"` // item | line | place | box
	ID           string `json:"id"`
	Title        string `json:"title"`
	TitleMarks   []Mark `json:"title_marks"`
	Snippet      string `json:"snippet"` // second line: brand · model · serial · notes (shortened), or the category
	SnippetMarks []Mark `json:"snippet_marks"`
	Path         string `json:"path"`
	BoxNumber    *int64 `json:"box_number,omitempty"`
	PlaceID      string `json:"place_id,omitempty"` // lines only: the place or box that holds the line
	Why          string `json:"why,omitempty"`      // Similar only: brand_model | name
}

// Results is the answer to a search: hits grouped by kind, each group best
// first. Top is the single best hit overall (a bare number puts that box
// there); nil when nothing matched.
type Results struct {
	Query  string  `json:"q"`
	Top    *Match  `json:"top"`
	Items  []Match `json:"items"`
	Lines  []Match `json:"lines"`
	Places []Match `json:"places"`
	Boxes  []Match `json:"boxes"`
}

// Options narrow a search.
type Options struct {
	Limit int      // per group; default 8, at most 50
	Kinds []string // subset of item, line, place, box; empty means all
}

const (
	defaultLimit = 8
	maxLimit     = 50
	minTrigram   = 3
	maxQueryLen  = 100
	snippetRunes = 100
)

type hit struct {
	Match
	tier  int
	score float64
}

// Search runs query against the household's index. Queries shorter than
// three characters (after folding) cannot use the trigram index and fall
// back to a prefix match on names and box numbers.
func Search(ctx context.Context, q Querier, householdID, query string, opt Options) (Results, error) {
	res := Results{Query: query, Items: []Match{}, Lines: []Match{}, Places: []Match{}, Boxes: []Match{}}
	r := []rune(query)
	if len(r) > maxQueryLen {
		r = r[:maxQueryLen]
	}
	fq := Fold(string(r))
	if fq == "" {
		return res, nil
	}
	limit := opt.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)
	want := map[string]bool{}
	for _, k := range opt.Kinds {
		want[k] = true
	}
	tokens := strings.Fields(fq)
	boxNo := int64(-1)
	if n, ok := strings.CutPrefix(fq, "box "); ok && isDigits(n) && len(n) <= 9 {
		boxNo = atoi(n) // "box 12" means box 12, like a bare "12"
	} else if isDigits(fq) && len(fq) <= 9 {
		boxNo = atoi(fq)
	}

	var best *hit
	for _, kind := range []string{KindItem, KindLine, KindPlace, KindBox} {
		if len(want) > 0 && !want[kind] {
			continue
		}
		hits, err := searchKind(ctx, q, householdID, kind, fq, tokens, boxNo, limit)
		if err != nil {
			return res, err
		}
		if kind == KindLine {
			if err := fillLinePlaces(ctx, q, householdID, hits); err != nil {
				return res, err
			}
		}
		out := make([]Match, len(hits))
		for i := range hits {
			hits[i].decorate(tokens)
			out[i] = hits[i].Match
			if best == nil || hits[i].tier < best.tier || (hits[i].tier == best.tier && hits[i].score < best.score) {
				best = &hits[i]
			}
		}
		switch kind {
		case KindItem:
			res.Items = out
		case KindLine:
			res.Lines = out
		case KindPlace:
			res.Places = out
		case KindBox:
			res.Boxes = out
		}
	}
	if best != nil {
		m := best.Match
		res.Top = &m
	}
	return res, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func atoi(s string) int64 {
	var n int64
	for _, c := range s {
		n = n*10 + int64(c-'0')
	}
	return n
}

// likeEscape makes s safe inside a LIKE pattern (folded text has no wildcard
// characters, this is belt and braces).
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func searchKind(ctx context.Context, q Querier, hid, kind, fq string, tokens []string, boxNo int64, limit int) ([]hit, error) {
	var sqlText strings.Builder
	var args []any
	short := len([]rune(strings.ReplaceAll(fq, " ", ""))) < minTrigram
	prefix := likeEscape(fq) + "%"
	wordPrefix := "% " + likeEscape(fq) + "%"
	if short {
		sqlText.WriteString(`SELECT d.kind, d.ref_id, d.display, d.detail, d.path, d.box_number,
			CASE WHEN d.box_number = ? THEN -1 WHEN d.name_fold = ? THEN 0 ELSE 1 END AS tier, 0.0 AS score
			FROM search_docs d
			WHERE d.household_id = ? AND d.kind = ?
			  AND (d.name_fold LIKE ? ESCAPE '\' OR d.name_fold LIKE ? ESCAPE '\' OR CAST(d.box_number AS TEXT) LIKE ? ESCAPE '\')
			ORDER BY tier, length(d.name_fold), d.touched DESC LIMIT ?`)
		args = []any{boxNo, fq, hid, kind, prefix, wordPrefix, prefix, limit}
	} else {
		var match []string
		var extra []any
		var filters strings.Builder
		for _, t := range tokens {
			if len([]rune(t)) >= minTrigram {
				match = append(match, `"`+t+`"`)
			} else {
				filters.WriteString(` AND (instr(search_fts.title, ?) > 0 OR instr(search_fts.body, ?) > 0)`)
				extra = append(extra, t, t)
			}
		}
		sqlText.WriteString(`SELECT d.kind, d.ref_id, d.display, d.detail, d.path, d.box_number,
			CASE WHEN d.box_number = ? THEN -1
			     WHEN d.name_fold = ? OR d.key_fold = ? THEN 0
			     WHEN d.name_fold LIKE ? ESCAPE '\' OR d.name_fold LIKE ? ESCAPE '\' OR d.key_fold LIKE ? ESCAPE '\' THEN 1
			     ELSE 2 END AS tier,
			bm25(search_fts) AS score
			FROM search_fts JOIN search_docs d ON d.id = search_fts.rowid
			WHERE search_fts MATCH ? AND search_fts.household_id = ? AND d.household_id = ? AND d.kind = ?`)
		sqlText.WriteString(filters.String())
		sqlText.WriteString(` ORDER BY tier, score, d.touched DESC LIMIT ?`)
		args = []any{boxNo, fq, fq, prefix, wordPrefix, prefix, strings.Join(match, " "), hid, hid, kind}
		args = append(args, extra...)
		args = append(args, limit)
	}
	rows, err := q.QueryContext(ctx, sqlText.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []hit
	for rows.Next() {
		var h hit
		var box sql.NullInt64
		if err := rows.Scan(&h.Kind, &h.ID, &h.Title, &h.Snippet, &h.Path, &box, &h.tier, &h.score); err != nil {
			return nil, err
		}
		if box.Valid {
			n := box.Int64
			h.BoxNumber = &n
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// decorate fills the marks of m for the folded query tokens, and shortens a
// long snippet around the first mark.
func (m *Match) decorate(tokens []string) {
	title := []rune(m.Title)
	m.TitleMarks = toUTF16(title, runeMarks(title, tokens))
	snip := []rune(m.Snippet)
	marks := runeMarks(snip, tokens)
	snip, marks = window(snip, marks, snippetRunes)
	m.Snippet = string(snip)
	m.SnippetMarks = toUTF16(snip, marks)
}

// runeMarks finds every occurrence of each token in the folded form of text
// and maps it back to rune ranges of text, merged where they touch.
func runeMarks(text []rune, tokens []string) [][2]int {
	folded, spans := foldSpans(string(text))
	fr := []rune(folded)
	var found [][2]int
	for _, t := range tokens {
		tr := []rune(t)
		if len(tr) == 0 {
			continue
		}
		for i := 0; i+len(tr) <= len(fr); i++ {
			if string(fr[i:i+len(tr)]) == t {
				found = append(found, [2]int{spans[i].from, spans[i+len(tr)-1].to})
			}
		}
	}
	return mergeMarks(found)
}

func mergeMarks(in [][2]int) [][2]int {
	if len(in) == 0 {
		return nil
	}
	for i := 1; i < len(in); i++ { // insertion sort: a handful of marks
		for j := i; j > 0 && in[j][0] < in[j-1][0]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
	out := [][2]int{in[0]}
	for _, m := range in[1:] {
		last := &out[len(out)-1]
		if m[0] <= last[1] {
			last[1] = max(last[1], m[1])
		} else {
			out = append(out, m)
		}
	}
	return out
}

// window shortens text to about maxRunes runes around the first mark, adding
// "…" where it cut, and shifts the marks to match.
func window(text []rune, marks [][2]int, maxRunes int) ([]rune, [][2]int) {
	if len(text) <= maxRunes {
		return text, marks
	}
	start := 0
	if len(marks) > 0 {
		start = max(0, marks[0][0]-30)
	}
	end := min(len(text), start+maxRunes)
	start = max(0, end-maxRunes)
	var out []rune
	lo := 0
	if start > 0 {
		out = append(out, '…')
		lo = 1
	}
	out = append(out, text[start:end]...)
	if end < len(text) {
		out = append(out, '…')
	}
	var shifted [][2]int
	for _, m := range marks {
		s, e := max(m[0]-start+lo, lo), min(m[1]-start+lo, lo+end-start)
		if s < e {
			shifted = append(shifted, [2]int{s, e})
		}
	}
	return out, shifted
}

// toUTF16 converts rune ranges of text into UTF-16 code unit ranges.
func toUTF16(text []rune, marks [][2]int) []Mark {
	out := make([]Mark, 0, len(marks))
	if len(marks) == 0 {
		return out
	}
	pos := make([]int, len(text)+1)
	for i, r := range text {
		pos[i+1] = pos[i] + len(utf16.Encode([]rune{r}))
	}
	for _, m := range marks {
		out = append(out, Mark{pos[m[0]], pos[m[1]]})
	}
	return out
}

// ---- similar ---------------------------------------------------------------

// Similar finds live items that look like the one being added: the same
// brand and model, or a close name. A generic name ("cable", "charger")
// never matches by name alone: the name needs two words, or one word that
// mixes letters and digits (a model code). Items in excludeIDs (the item
// itself) are skipped. Same brand and model comes first. Marks are empty.
func Similar(ctx context.Context, q Querier, householdID, brand, model, name string, excludeIDs ...string) ([]Match, error) {
	skip := map[string]bool{}
	for _, id := range excludeIDs {
		skip[id] = true
	}
	var out []Match
	seen := map[string]bool{}
	add := func(rows *sql.Rows, why string, keep func(nameFold string) bool) error {
		defer rows.Close()
		for rows.Next() {
			m := Match{TitleMarks: []Mark{}, SnippetMarks: []Mark{}, Why: why}
			var nameFold string
			var box sql.NullInt64
			if err := rows.Scan(&m.Kind, &m.ID, &m.Title, &m.Snippet, &m.Path, &box, &nameFold); err != nil {
				return err
			}
			if skip[m.ID] || seen[m.ID] || (keep != nil && !keep(nameFold)) {
				continue
			}
			seen[m.ID] = true
			out = append(out, m)
		}
		return rows.Err()
	}
	const cols = `d.kind, d.ref_id, d.display, d.detail, d.path, d.box_number, d.name_fold`

	fb, fm := Fold(brand), Fold(model)
	if fb != "" && fm != "" {
		rows, err := q.QueryContext(ctx, `SELECT `+cols+` FROM search_docs d
			WHERE d.household_id = ? AND d.kind = 'item' AND d.key_fold = ? AND d.brand_fold = ?
			ORDER BY d.touched DESC LIMIT 10`, householdID, fm, fb)
		if err != nil {
			return nil, err
		}
		if err := add(rows, "brand_model", nil); err != nil {
			return nil, err
		}
	}

	nameTokens := uniqueTokens(Fold(name))
	if distinctiveName(nameTokens) {
		var match []string
		for _, t := range nameTokens {
			if len([]rune(t)) >= minTrigram {
				match = append(match, `"`+t+`"`)
			}
		}
		if len(match) > 0 {
			rows, err := q.QueryContext(ctx, `SELECT `+cols+` FROM search_fts JOIN search_docs d ON d.id = search_fts.rowid
				WHERE search_fts MATCH ? AND search_fts.household_id = ? AND d.household_id = ? AND d.kind = 'item'
				ORDER BY bm25(search_fts) LIMIT 50`, strings.Join(match, " OR "), householdID, householdID)
			if err != nil {
				return nil, err
			}
			err = add(rows, "name", func(nameFold string) bool {
				return jaccard(nameTokens, uniqueTokens(nameFold)) >= 0.6
			})
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func uniqueTokens(folded string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range strings.Fields(folded) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// distinctiveName reports whether a name carries enough to identify a
// product: two or more words, or one word mixing letters and digits.
func distinctiveName(tokens []string) bool {
	switch len(tokens) {
	case 0:
		return false
	case 1:
		t := tokens[0]
		return len([]rune(t)) >= 4 && strings.ContainsAny(t, "0123456789") &&
			strings.IndexFunc(t, func(r rune) bool { return r < '0' || r > '9' }) >= 0
	}
	return true
}

func jaccard(a, b []string) float64 {
	set := map[string]bool{}
	for _, t := range a {
		set[t] = true
	}
	inter, union := 0, len(set)
	seen := map[string]bool{}
	for _, t := range b {
		if seen[t] {
			continue
		}
		seen[t] = true
		if set[t] {
			inter++
		} else {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// fillLinePlaces sets PlaceID on line hits (the place holding the line) with
// one query for all of them.
func fillLinePlaces(ctx context.Context, q Querier, hid string, hits []hit) error {
	if len(hits) == 0 {
		return nil
	}
	args := make([]any, 0, len(hits)+1)
	args = append(args, hid)
	for _, h := range hits {
		args = append(args, h.ID)
	}
	rows, err := q.QueryContext(ctx, `SELECT id, place_id FROM contents_lines WHERE household_id = ? AND id IN (`+
		strings.TrimSuffix(strings.Repeat("?,", len(hits)), ",")+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	place := map[string]string{}
	for rows.Next() {
		var id, pid string
		if err := rows.Scan(&id, &pid); err != nil {
			return err
		}
		place[id] = pid
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range hits {
		hits[i].PlaceID = place[hits[i].ID]
	}
	return nil
}
