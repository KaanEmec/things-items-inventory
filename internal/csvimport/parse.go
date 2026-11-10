package csvimport

import (
	"bytes"
	"encoding/csv"
	"io"
	"strings"
	"unicode/utf8"
)

// cp1252High maps the bytes 0x80-0x9F of Windows-1252 (0 = undefined).
var cp1252High = [32]rune{
	0x20AC, 0, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0, 0x017D, 0,
	0, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0, 0x017E, 0x0178,
}

// Windows-1254 differs from 1252 in six letters of 0xA0-0xFF.
var cp1254 = map[byte]rune{0xD0: 'Ğ', 0xDD: 'İ', 0xDE: 'Ş', 0xF0: 'ğ', 0xFD: 'ı', 0xFE: 'ş'}

// decode returns the file as UTF-8 text and the encoding it was read as:
// utf-8 (a BOM is dropped), windows-1254 when it holds one of the Turkish
// letters that differ, else windows-1252.
func decode(b []byte) (string, string) {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(b) {
		return string(b), "utf-8"
	}
	turkish := false
	for _, c := range b {
		if _, ok := cp1254[c]; ok {
			turkish = true
			break
		}
	}
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c < 0x80 || c >= 0xA0 && !(turkish && cp1254[c] != 0):
			sb.WriteRune(rune(c))
		case c >= 0xA0:
			sb.WriteRune(cp1254[c])
		case cp1252High[c-0x80] != 0:
			sb.WriteRune(cp1252High[c-0x80])
		default:
			sb.WriteRune('�')
		}
	}
	if turkish {
		return sb.String(), "windows-1254"
	}
	return sb.String(), "windows-1252"
}

// sniffDelimiter picks the delimiter that splits the first lines into the same
// number (above one) of cells most consistently.
func sniffDelimiter(text string) rune {
	lines := strings.Split(text, "\n")
	if len(lines) > 10 {
		lines = lines[:10]
	}
	best, bestScore := ',', 0
	for _, d := range []rune{',', ';', '\t', '|'} {
		first := strings.Count(lines[0], string(d))
		if first == 0 {
			continue
		}
		same := 0
		for _, l := range lines {
			if strings.TrimSpace(l) != "" && strings.Count(l, string(d)) == first {
				same++
			}
		}
		if score := same*1000 + first; score > bestScore {
			best, bestScore = d, score
		}
	}
	return best
}

// parse reads the records of the text.
func parse(text string, delim rune) ([][]string, error) {
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = delim
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	var out [][]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		blank := true
		for i := range rec {
			rec[i] = strings.TrimSpace(rec[i])
			if rec[i] != "" {
				blank = false
			}
		}
		if !blank {
			out = append(out, rec)
		}
	}
}

// looksLikeHeader is true when no first-row cell is a number or a date.
func looksLikeHeader(rec []string) bool {
	for _, c := range rec {
		if c != "" && (numRe.MatchString(c) || dateRe.MatchString(c)) {
			return false
		}
	}
	return true
}
