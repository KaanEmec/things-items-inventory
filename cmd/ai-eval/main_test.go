package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/ai/prompt/prompttest"
)

func TestRefusesWithoutCases(t *testing.T) {
	for name, args := range map[string][]string{
		"no dir":    nil,
		"empty dir": {"-cases", t.TempDir()},
		"missing":   {"-cases", filepath.Join(t.TempDir(), "nope")},
	} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 2 {
			t.Errorf("%s: exit %d", name, code)
		}
		if !strings.Contains(errb.String(), "<name>.expected.json") {
			t.Errorf("%s: does not say what to put there:\n%s", name, errb.String())
		}
	}
}

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// End to end against a fake endpoint that answers with recorded fixtures.
func TestRunAgainstFakeEndpoint(t *testing.T) {
	answers := map[string]json.RawMessage{
		"List the belongings": prompttest.Answer("text_to_items", "sentence_box12"),
		"Describe the item":   prompttest.Answer("item_draft", "sony_headphones"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		user, _ := json.Marshal(body.Messages[len(body.Messages)-1].Content)
		ans := json.RawMessage(`{}`)
		for k, v := range answers {
			if strings.Contains(string(user), k) {
				ans = v
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "fake",
			"choices": []any{map[string]any{"message": map[string]any{"content": string(ans)}}},
			"usage":   map[string]any{"prompt_tokens": 100, "completion_tokens": 20},
		})
	}))
	defer srv.Close()
	t.Setenv("INVENTORY_AI_VISION_BASE_URL", srv.URL)
	t.Setenv("INVENTORY_AI_VISION_TOKEN", "test")
	t.Setenv("INVENTORY_AI_VISION_MODEL", "fake/vision")
	t.Setenv("INVENTORY_AI_TEXT_MODEL", "")

	dir := t.TempDir()
	write(t, filepath.Join(dir, "box12.txt"), "three hdmi cables, the old kindle and two pairs of ski gloves in box 12")
	write(t, filepath.Join(dir, "box12.expected.json"), `{
		"grounding": {"language": "en", "places": [{"id": "BOX12", "path": "Box 12"}]},
		"expect": {"rows": [
			{"name": "HDMI cable", "quantity": 3, "place": "BOX12"},
			{"name": {"oneOf": ["Kindle", "Amazon Kindle"]}, "brand": "Amazon", "model": "Kindle Paperwhite"},
			{"name": "Ski gloves", "quantity": 2},
			{"name": "Snowboard", "quantity": 1}
		]}}`)
	var img bytes.Buffer
	_ = jpeg.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2000, 1000)), nil)
	write(t, filepath.Join(dir, "sony.jpg"), img.String())
	write(t, filepath.Join(dir, "sony.label.jpg"), img.String())
	write(t, filepath.Join(dir, "sony.expected.json"), `{"grounding": {"language": "en"},
		"expect": {"brand": "Sony", "model": "WH-1000XM4", "serial": null, "color": "black", "attributes.connectivity": ["usb_c", "bluetooth"]}}`)

	var out, errb bytes.Buffer
	if code := run([]string{"-cases", dir, "-v"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	o := out.String()
	for _, want := range []string{
		"box12", "text_to_items", "sony", "item_draft", "strict",
		"rows.quantity                     3    67%    33%     0%",
		"rows.name                         4    75%    25%     0%",
		"rows.model                        1     0%   100%     0%",
		"serial                            1     0%     0%   100%",
		"attributes.connectivity           1   100%",
		"brand and model: 0 wrong, 1 empty → PASS",
	} {
		if !strings.Contains(o, want) {
			t.Errorf("output lacks %q:\n%s", want, o)
		}
	}
}

func TestShrink(t *testing.T) {
	got := shrink(image.NewRGBA(image.Rect(0, 0, 4000, 3000)), 1600).Bounds()
	if got.Dx() != 1600 || got.Dy() != 1200 {
		t.Errorf("shrunk to %v", got)
	}
}

func TestReadCSV(t *testing.T) {
	h, rows, err := readCSV("\ufeffÜrün;Adet\nMont;1\n\"Kot; mavi\";2\n")
	if err != nil || len(h) != 2 || h[0] != "Ürün" || len(rows) != 2 || rows[1][0] != "Kot; mavi" {
		t.Errorf("%v %v %v", h, rows, err)
	}
}
