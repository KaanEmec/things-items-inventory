package search

import "testing"

func TestFold(t *testing.T) {
	for in, want := range map[string]string{
		"":                         "",
		"ISI":                      "isi",
		"ısı":                      "isi",
		"İSİ":                      "isi",
		"isi":                      "isi",
		"Şişe":                     "sise",
		"ÖĞÜT Çay":                 "ogut cay",
		"Café Crème":               "cafe creme",
		"Straße":                   "strasse",
		"WH-1000XM4":               "wh1000xm4",
		"wh1000xm4":                "wh1000xm4",
		"USB-C  charger!":          "usbc charger",
		"  O'Neil_x.y  ":           "oneilxy",
		"3.5mm / 6,3mm":            "35mm 6 3mm",
		"Kış Montu (Mavi)":         "kis montu mavi",
		"café":                    "cafe", // decomposed accent
		"Привет Мир":               "привет мир",
		"Çocuk Odası → 3. Çekmece": "cocuk odasi 3 cekmece",
	} {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFoldSpansPointBackIntoTheOriginal(t *testing.T) {
	text := []rune("Sony WH-1000XM4 Şişe")
	marks := runeMarks(text, []string{"wh1000"})
	if len(marks) != 1 || string(text[marks[0][0]:marks[0][1]]) != "WH-1000" {
		t.Fatalf("marks %v", marks)
	}
	marks = runeMarks(text, []string{"sise"})
	if len(marks) != 1 || string(text[marks[0][0]:marks[0][1]]) != "Şişe" {
		t.Fatalf("marks %v", marks)
	}
}

func TestMarksAreUTF16(t *testing.T) {
	// 😀 is two UTF-16 units; the mark of "abc" after it starts at 3.
	m := Match{Title: "😀 abc"}
	m.decorate([]string{"abc"})
	if len(m.TitleMarks) != 1 || m.TitleMarks[0] != (Mark{3, 6}) {
		t.Fatalf("%v", m.TitleMarks)
	}
}
