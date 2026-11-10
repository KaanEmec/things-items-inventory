package db

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestQueriesAreHouseholdScoped enforces "every sqlc query on a household
// table is scoped to a household" (J2.2). A household table is any table
// (including virtual/FTS tables) whose migration declares a household_id
// column. A query (and every subquery in it) that touches one must use
// household_id as a real condition of that statement: in a WHERE/ON
// comparison (`household_id = ?`, `i.household_id = ?`) or in an INSERT
// column list. A mention in the SELECT list, in a comment, in a string, in
// SET, or only inside a subquery's correlation does not count for the outer
// statement.
//
// The one escape hatch is a comment line directly after the `-- name:` line:
//
//	-- name: ClaimJob :one
//	-- scope: global (why this query deliberately spans households)
//
// The reason in parentheses is required. The scanner itself is tested by
// TestScopeScanner below.
func TestQueriesAreHouseholdScoped(t *testing.T) {
	tables := householdTables(t)
	if len(tables) == 0 {
		t.Fatal("found no household tables in the migrations; the scanner is broken")
	}
	files, err := filepath.Glob("queries/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("no query files: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, problem := range scanQueries(string(b), tables) {
			t.Errorf("%s: %s", f, problem)
		}
	}
}

var (
	nameRe = regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)`)
	// scopeRe: a "-- scope: global (reason)" line with a non-empty reason.
	scopeRe = regexp.MustCompile(`^--\s*scope:\s*global\s*\(\s*[^)\s][^)]*\)`)
	tokenRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.]*|\d+|<>|!=|<=|>=|\|\||\S`)
)

// scanQueries returns one problem per unscoped query in a sqlc query file.
func scanQueries(src string, tables map[string]bool) []string {
	var problems []string
	idx := nameRe.FindAllStringSubmatchIndex(src, -1)
	for i, m := range idx {
		end := len(src)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		name, block := src[m[2]:m[3]], src[m[0]:end]
		lines := strings.Split(block, "\n")
		if len(lines) > 1 && scopeRe.MatchString(strings.TrimSpace(lines[1])) {
			continue
		}
		for _, stmt := range splitSubqueries(cleanSQL(strings.Join(lines[1:], "\n"))) {
			touched, scoped := analyze(stmt, tables)
			if len(touched) > 0 && !scoped {
				problems = append(problems, "query "+name+" touches household table(s) "+strings.Join(touched, ", ")+
					" without using household_id in a WHERE/ON condition or INSERT column list; add the filter or, if it is "+
					"deliberately cross-household, put `-- scope: global (reason)` directly after the `-- name:` line")
			}
		}
	}
	return problems
}

// cleanSQL strips comments and string literals.
func cleanSQL(s string) string {
	s = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(s, " ")
	s = regexp.MustCompile(`--[^\n]*`).ReplaceAllString(s, " ")
	s = regexp.MustCompile(`'(?:[^']|'')*'`).ReplaceAllString(s, "''")
	return s
}

// splitSubqueries returns the statement with its parenthesised subqueries
// cut out, followed by each subquery (recursively, also cut down). Each is
// analysed on its own, so a subquery's household_id never counts for the
// outer statement.
func splitSubqueries(s string) []string {
	subRe := regexp.MustCompile(`(?is)^\s*(select|with)\b`)
	var out []string
	var outer strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '(' && subRe.MatchString(s[i+1:]) {
			depth, j := 0, i
			for ; j < len(s); j++ {
				if s[j] == '(' {
					depth++
				} else if s[j] == ')' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			if j >= len(s) {
				j = len(s) - 1
			}
			out = append(out, splitSubqueries(s[i+1:j])...)
			outer.WriteString("(?)")
			i = j
			continue
		}
		outer.WriteByte(s[i])
	}
	return append([]string{outer.String()}, out...)
}

// analyze reports the household tables a single statement (subqueries
// already removed) touches and whether it uses household_id as a condition
// or written column.
func analyze(stmt string, tables map[string]bool) (touched []string, scoped bool) {
	toks := tokenRe.FindAllString(stmt, -1)
	for i := range toks {
		toks[i] = strings.ToLower(toks[i])
	}
	clause, depth := "", 0
	insertCols := -1 // depth of an INSERT column list being read, or -1
	prev := ""
	at := func(i int) string {
		if i >= 0 && i < len(toks) {
			return toks[i]
		}
		return ""
	}
	addTable := func(name string) {
		if tables[name] {
			touched = append(touched, name)
		}
	}
	for i, tk := range toks {
		switch tk {
		case "(":
			depth++
		case ")":
			if depth == insertCols {
				insertCols = -1
			}
			depth--
		case "select", "where", "set", "values", "returning", "group", "order", "having", "limit", "update", "delete", "insert":
			clause = tk
			if tk == "update" && prev == "do" {
				clause = "set"
			} else if tk == "update" {
				addTable(strings.TrimPrefix(at(i+1), "only"))
			}
		case "from", "join":
			clause = "from"
			addTable(at(i + 1))
		case "into":
			clause = "into"
			addTable(at(i + 1))
			if at(i+2) == "(" {
				insertCols = depth + 1
			}
		case "on":
			if at(i+1) == "conflict" {
				clause = "conflict"
			} else {
				clause = "on"
			}
		case ",":
			if clause == "from" && depth == 0 {
				addTable(at(i + 1))
			}
		}
		base := tk[strings.LastIndex(tk, ".")+1:]
		if base != "household_id" {
			prev = tk
			continue
		}
		switch {
		case insertCols >= 0 && depth == insertCols:
			scoped = true
		case clause == "where" || clause == "on":
			if next := at(i + 1); next == "=" || next == "in" || prev == "=" || prev == "in" {
				scoped = true
			}
		}
		prev = tk
	}
	return touched, scoped
}

func householdTables(t *testing.T) map[string]bool {
	t.Helper()
	files, _ := filepath.Glob("migrations/*.sql")
	out := map[string]bool{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for name := range householdTablesIn(string(b)) {
			out[name] = true
		}
	}
	return out
}

// householdTablesIn finds the tables in one migration that have a
// household_id column: CREATE [VIRTUAL] TABLE with any terminator
// (STRICT, WITHOUT ROWID, ...) and FTS/USING modules, plus ALTER TABLE ADD
// COLUMN household_id.
func householdTablesIn(src string) map[string]bool {
	src = cleanSQL(src)
	out := map[string]bool{}
	create := regexp.MustCompile(`(?is)CREATE\s+(?:VIRTUAL\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?["\x60\[]?([a-z_][a-z0-9_]*)["\x60\]]?\s*(?:USING\s+\w+\s*)?\(`)
	for _, m := range create.FindAllStringSubmatchIndex(src, -1) {
		start := m[1] // just after the opening paren
		depth, end := 1, start
		for ; end < len(src) && depth > 0; end++ {
			if src[end] == '(' {
				depth++
			} else if src[end] == ')' {
				depth--
			}
		}
		body, d := src[start:end-1], 0
		var item strings.Builder
		items := []string{}
		for _, c := range body {
			switch {
			case c == '(':
				d++
			case c == ')':
				d--
			case c == ',' && d == 0:
				items = append(items, item.String())
				item.Reset()
				continue
			}
			item.WriteRune(c)
		}
		items = append(items, item.String())
		for _, it := range items {
			if regexp.MustCompile(`(?i)^\s*["\x60\[]?household_id\b`).MatchString(it) {
				out[strings.ToLower(src[m[2]:m[3]])] = true
			}
		}
	}
	alter := regexp.MustCompile(`(?is)ALTER\s+TABLE\s+([a-z_][a-z0-9_]*)\s+ADD\s+(?:COLUMN\s+)?household_id\b`)
	for _, m := range alter.FindAllStringSubmatch(src, -1) {
		out[strings.ToLower(m[1])] = true
	}
	return out
}

// TestScopeScanner proves the scanner itself: every known way to slip an
// unscoped query past it must be flagged, and correct queries must pass.
func TestScopeScanner(t *testing.T) {
	tables := map[string]bool{"items": true, "places": true, "jobs": true, "fts_items": true}
	bad := map[string]string{
		"mention only in a comment": "-- name: A :one\nSELECT * FROM items WHERE id = ?; -- household_id",
		"mention in a string":       "-- name: A :one\nSELECT * FROM items WHERE note = 'household_id' AND id = ?;",
		"only in the select list":   "-- name: A :one\nSELECT household_id, name FROM items WHERE id = ?;",
		"only in SET":               "-- name: A :exec\nUPDATE items SET household_id = ? WHERE id = ?;",
		"only a correlation inside a subquery": "-- name: A :many\n" +
			"SELECT * FROM items i WHERE EXISTS (SELECT 1 FROM places p WHERE p.household_id = i.household_id);",
		"subquery lacks the filter":             "-- name: A :many\nSELECT * FROM items WHERE household_id = ? AND place_id IN (SELECT id FROM places WHERE name = ?);",
		"join table unscoped":                   "-- name: A :many\nSELECT * FROM x JOIN items ON items.id = x.item_id WHERE x.id = ?;",
		"comma join":                            "-- name: A :many\nSELECT * FROM x, items WHERE x.id = ?;",
		"scope comment without reason":          "-- name: A :one\n-- scope: global\nSELECT * FROM items WHERE id = ?;",
		"scope comment empty reason":            "-- name: A :one\n-- scope: global ( )\nSELECT * FROM items WHERE id = ?;",
		"scope comment after SQL":               "-- name: A :one\nSELECT * FROM items WHERE id = ?;\n-- scope: global (too late)",
		"scope comment not directly after name": "-- name: A :one\n-- something else\n-- scope: global (late)\nSELECT * FROM items WHERE id = ?;",
		"scope comment of previous query":       "-- name: A :one\n-- scope: global (fine)\nSELECT 1 FROM jobs;\n\n-- name: B :one\nSELECT * FROM items WHERE id = ?;",
		"insert without household":              "-- name: A :exec\nINSERT INTO items (id, name) VALUES (?, ?);",
		"insert select unscoped":                "-- name: A :exec\nINSERT INTO items (id, name) SELECT id, name FROM places WHERE id = ?;",
		"fts table":                             "-- name: A :many\nSELECT rowid FROM fts_items WHERE fts_items MATCH ?;",
		"is null does not scope":                "-- name: A :many\nSELECT * FROM items WHERE household_id IS NULL;",
	}
	for name, sql := range bad {
		if got := scanQueries(sql, tables); len(got) == 0 {
			t.Errorf("%s: not flagged:\n%s", name, sql)
		}
	}
	good := map[string]string{
		"where":           "-- name: A :one\nSELECT * FROM items WHERE household_id = ? AND id = ?;",
		"qualified":       "-- name: A :one\nSELECT * FROM items i WHERE i.household_id = sqlc.arg(household_id) AND i.id = ?;",
		"reversed":        "-- name: A :one\nSELECT * FROM items WHERE ? = household_id;",
		"join on":         "-- name: A :many\nSELECT * FROM items i JOIN places p ON p.household_id = i.household_id AND p.id = i.place_id WHERE i.household_id = ?;",
		"insert columns":  "-- name: A :exec\nINSERT INTO items (id, household_id, name) VALUES (?, ?, ?);",
		"update":          "-- name: A :execrows\nUPDATE items SET name = ? WHERE household_id = ? AND id = ?;",
		"delete":          "-- name: A :exec\nDELETE FROM items WHERE household_id = ? AND id = ?;",
		"scoped subquery": "-- name: A :many\nSELECT * FROM items WHERE household_id = ? AND place_id IN (SELECT id FROM places WHERE household_id = ?);",
		"correlated subquery, scoped outer": "-- name: A :many\nSELECT * FROM items i WHERE i.household_id = ? AND " +
			"EXISTS (SELECT 1 FROM places p WHERE p.household_id = i.household_id AND p.id = i.place_id);",
		"global with reason": "-- name: A :one\n-- scope: global (the worker serves every household)\nUPDATE jobs SET state = 'x' WHERE id = ?;",
		"no household table": "-- name: A :one\nSELECT COUNT(*) FROM users;",
		"fts scoped":         "-- name: A :many\nSELECT rowid FROM fts_items WHERE household_id = ? AND fts_items MATCH ?;",
		"on conflict":        "-- name: A :exec\nINSERT INTO items (id, household_id) VALUES (?, ?) ON CONFLICT (household_id, id) DO UPDATE SET id = excluded.id;",
	}
	for name, sql := range good {
		if got := scanQueries(sql, tables); len(got) != 0 {
			t.Errorf("%s: wrongly flagged: %v\n%s", name, got, sql)
		}
	}

	// Table detector.
	migrations := map[string]bool{
		"CREATE TABLE a (\n id TEXT,\n household_id TEXT NOT NULL\n) STRICT;":                                 true,
		"CREATE TABLE a (\n household_id TEXT,\n id TEXT,\n PRIMARY KEY (household_id, id)\n) WITHOUT ROWID;": true,
		"CREATE TABLE a (id TEXT, household_id TEXT) STRICT, WITHOUT ROWID;":                                  true,
		"CREATE VIRTUAL TABLE a USING fts5(household_id UNINDEXED, name, tokenize='trigram');":                true,
		"CREATE TABLE a (id TEXT);\nALTER TABLE a ADD COLUMN household_id TEXT;":                              true,
		"CREATE TABLE a (id TEXT, note TEXT DEFAULT 'household_id');":                                         false,
		"CREATE TABLE a (id TEXT, ref TEXT REFERENCES b(household_id));":                                      false,
		"-- CREATE TABLE a (household_id TEXT);":                                                              false,
	}
	for sql, want := range migrations {
		if got := householdTablesIn(sql)["a"]; got != want {
			t.Errorf("householdTablesIn(%q) = %v, want %v", sql, got, want)
		}
	}
}
