package id

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	prev := ""
	seen := map[string]bool{}
	for i := 0; i < 5000; i++ {
		s := New()
		if len(s) != 26 {
			t.Fatalf("length %d: %q", len(s), s)
		}
		for _, c := range s {
			if !strings.ContainsRune(alphabet, c) {
				t.Fatalf("bad character %q in %q", c, s)
			}
		}
		if s[0] > '7' {
			t.Fatalf("first character overflows 128 bits: %q", s)
		}
		if s <= prev {
			t.Fatalf("not increasing: %q after %q", s, prev)
		}
		if seen[s] {
			t.Fatalf("duplicate %q", s)
		}
		seen[s], prev = true, s
	}
}
