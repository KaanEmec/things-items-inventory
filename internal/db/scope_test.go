package db

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestQueriesAreHouseholdScoped enforces "every sqlc query on a household
// table takes household_id" (J2.2). A household table is any table whose
// migration declares a household_id column. A query that touches one must
// mention household_id outside comments (a filter, an inserted column, a
// join condition), or carry a "-- scope: global" comment with the reason,
// the one documented escape hatch (login by cookie, background workers,
// per-user listings and the like).
func TestQueriesAreHouseholdScoped(t *testing.T) {
	tables := householdTables(t)
	if len(tables) == 0 {
		t.Fatal("found no household tables in the migrations; the scanner is broken")
	}
	files, err := filepath.Glob("queries/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("no query files: %v", err)
	}
	nameRe := regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)`)
	tableRe := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE)\s+([a-z_][a-z0-9_]*)`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		idx := nameRe.FindAllStringSubmatchIndex(string(b), -1)
		for i, m := range idx {
			end := len(b)
			if i+1 < len(idx) {
				end = idx[i+1][0]
			}
			name, block := string(b[m[2]:m[3]]), string(b[m[0]:end])
			if strings.Contains(block, "scope: global") {
				continue
			}
			var code strings.Builder
			for _, line := range strings.Split(block, "\n") {
				if !strings.HasPrefix(strings.TrimSpace(line), "--") {
					code.WriteString(line + "\n")
				}
			}
			var touched []string
			for _, tm := range tableRe.FindAllStringSubmatch(code.String(), -1) {
				if tables[strings.ToLower(tm[1])] {
					touched = append(touched, tm[1])
				}
			}
			if len(touched) > 0 && !strings.Contains(code.String(), "household_id") {
				t.Errorf("%s: query %s touches household table(s) %v without household_id; "+
					"add the filter or, if it is deliberately cross-household, a `-- scope: global` comment",
					f, name, touched)
			}
		}
	}
}

func householdTables(t *testing.T) map[string]bool {
	t.Helper()
	files, _ := filepath.Glob("migrations/*.sql")
	create := regexp.MustCompile(`(?is)CREATE\s+(?:VIRTUAL\s+)?TABLE\s+(?:IF NOT EXISTS\s+)?([a-z_][a-z0-9_]*)\s*(?:USING\s+\w+\s*)?\((.*?)\n\)\s*;`)
	alter := regexp.MustCompile(`(?is)ALTER\s+TABLE\s+([a-z_][a-z0-9_]*)\s+ADD\s+COLUMN\s+household_id`)
	out := map[string]bool{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range create.FindAllStringSubmatch(string(b), -1) {
			if regexp.MustCompile(`(?m)^\s*household_id\b`).MatchString(m[2]) {
				out[strings.ToLower(m[1])] = true
			}
		}
		for _, m := range alter.FindAllStringSubmatch(string(b), -1) {
			out[strings.ToLower(m[1])] = true
		}
	}
	return out
}
