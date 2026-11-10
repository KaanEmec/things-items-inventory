// Package search keeps a trigram full-text index of items, contents lines,
// places and boxes and answers GET /api/v1/search.
//
// Text folding: everything indexed or queried goes through Fold first, so
// "ISI", "ısı" and "isi" are the same word, "Şişe" is "sise" and
// "WH-1000XM4" is "wh1000xm4".
package search

import (
	"strings"
	"unicode"
)

// foldTable maps letters to their folded form. It is written out by hand
// (no dependency): Turkish first, then the Latin letters with diacritics
// that people type on household labels. Letters not listed here are only
// lowercased, so Cyrillic, Greek, CJK and the like still work.
var foldTable = map[rune]string{
	// Turkish. Dotless ı and dotted İ both become plain i.
	'ı': "i", 'İ': "i", 'ş': "s", 'Ş': "s", 'ğ': "g", 'Ğ': "g",
	'ç': "c", 'Ç': "c", 'ö': "o", 'Ö': "o", 'ü': "u", 'Ü': "u",
	// Latin-1 and Latin Extended-A.
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a",
	'À': "a", 'Á': "a", 'Â': "a", 'Ã': "a", 'Ä': "a", 'Å': "a", 'Ā': "a", 'Ă': "a", 'Ą': "a",
	'æ': "ae", 'Æ': "ae", 'œ': "oe", 'Œ': "oe", 'ß': "ss", 'ẞ': "ss",
	'ć': "c", 'ĉ': "c", 'ċ': "c", 'č': "c", 'Ć': "c", 'Ĉ': "c", 'Ċ': "c", 'Č': "c",
	'ď': "d", 'đ': "d", 'Ď': "d", 'Đ': "d", 'ð': "d", 'Ð': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ĕ': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'È': "e", 'É': "e", 'Ê': "e", 'Ë': "e", 'Ē': "e", 'Ĕ': "e", 'Ė': "e", 'Ę': "e", 'Ě': "e",
	'ĝ': "g", 'ġ': "g", 'ģ': "g", 'Ĝ': "g", 'Ġ': "g", 'Ģ': "g",
	'ĥ': "h", 'ħ': "h", 'Ĥ': "h", 'Ħ': "h",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ĩ': "i", 'ī': "i", 'ĭ': "i", 'į': "i",
	'Ì': "i", 'Í': "i", 'Î': "i", 'Ï': "i", 'Ĩ': "i", 'Ī': "i", 'Ĭ': "i", 'Į': "i",
	'ĵ': "j", 'Ĵ': "j", 'ķ': "k", 'Ķ': "k",
	'ĺ': "l", 'ļ': "l", 'ľ': "l", 'ŀ': "l", 'ł': "l", 'Ĺ': "l", 'Ļ': "l", 'Ľ': "l", 'Ŀ': "l", 'Ł': "l",
	'ñ': "n", 'ń': "n", 'ņ': "n", 'ň': "n", 'Ñ': "n", 'Ń': "n", 'Ņ': "n", 'Ň': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ø': "o", 'ō': "o", 'ŏ': "o", 'ő': "o",
	'Ò': "o", 'Ó': "o", 'Ô': "o", 'Õ': "o", 'Ø': "o", 'Ō': "o", 'Ŏ': "o", 'Ő': "o",
	'ŕ': "r", 'ŗ': "r", 'ř': "r", 'Ŕ': "r", 'Ŗ': "r", 'Ř': "r",
	'ś': "s", 'ŝ': "s", 'š': "s", 'ș': "s", 'ţ': "t", 'ť': "t", 'ŧ': "t", 'ț': "t",
	'Ś': "s", 'Ŝ': "s", 'Š': "s", 'Ș': "s", 'Ţ': "t", 'Ť': "t", 'Ŧ': "t", 'Ț': "t",
	'ù': "u", 'ú': "u", 'û': "u", 'ũ': "u", 'ū': "u", 'ŭ': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'Ù': "u", 'Ú': "u", 'Û': "u", 'Ũ': "u", 'Ū': "u", 'Ŭ': "u", 'Ů': "u", 'Ű': "u", 'Ų': "u",
	'ŵ': "w", 'Ŵ': "w", 'ý': "y", 'ÿ': "y", 'ŷ': "y", 'Ý': "y", 'Ÿ': "y", 'Ŷ': "y",
	'ź': "z", 'ż': "z", 'ž': "z", 'Ź': "z", 'Ż': "z", 'Ž': "z",
	'þ': "th", 'Þ': "th",
}

// joiners are punctuation that glues a word together instead of splitting
// it: "wh-1000xm4", "o'neil", "usb_c" and "3.5mm" fold to one word.
func isJoiner(r rune) bool {
	switch r {
	case '-', '_', '\'', '’', '‘', '`', '.', '‐', '‑', '·':
		return true
	}
	return false
}

// Fold lowercases s, maps Turkish ı/İ to i, strips diacritics, removes
// joining punctuation ("-", "_", "'", ".") and turns every other run of
// non-letters into a single space. The result is trimmed.
func Fold(s string) string {
	f, _ := foldSpans(s)
	return f
}

// span is where a folded rune came from: rune indexes [from, to) of the
// original text.
type span struct{ from, to int }

// foldSpans is Fold that also reports, for each rune of the folded text,
// the rune range of the original it came from. Highlighting uses it to
// point back into the text people see.
func foldSpans(s string) (string, []span) {
	var b strings.Builder
	var spans []span
	pendingSpace := false
	var spaceAt span
	i := 0
	emit := func(str string, at int) {
		if pendingSpace && b.Len() > 0 {
			b.WriteByte(' ')
			spans = append(spans, spaceAt)
		}
		pendingSpace = false
		for _, r := range str {
			b.WriteRune(r)
			spans = append(spans, span{at, at + 1})
		}
	}
	for _, r := range s {
		switch {
		case foldTable[r] != "":
			emit(foldTable[r], i)
		case unicode.Is(unicode.Mn, r):
			// combining accent from decomposed text: drop
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			emit(string(unicode.ToLower(r)), i)
		case isJoiner(r):
			// glue: nothing
		default:
			if !pendingSpace {
				spaceAt = span{i, i + 1}
			}
			pendingSpace = true
		}
		i++
	}
	return b.String(), spans
}
