package inventory_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
)

// A client may mark fields it accepted from an AI suggestion as AI-filled,
// but only fields the same request writes.
func TestClientAIFieldsOnlyForFieldsInTheRequest(t *testing.T) {
	e := newEnv(t)
	var got obj
	e.must(http.StatusCreated, "POST", "/api/v1/items",
		`{"name":"Sony WH-1000XM4","brand":"Sony","category":"gadget","attributes":{"type":"headphones"},"ai_fields":["name","brand","attributes.type"]}`, &got)
	ai := toStrings(got["ai_fields"])
	slices.Sort(ai)
	if !slices.Equal(ai, []string{"attributes.type", "brand", "name"}) {
		t.Fatalf("ai_fields after create: %v", ai)
	}
	id := got["id"].(string)

	// Patch: a field written without being listed leaves ai_fields; a listed one joins.
	e.must(http.StatusOK, "PATCH", "/api/v1/items/"+id, `{"brand":"Sony Corp.","model":"WH-1000XM4","ai_fields":["model"]}`, &got)
	ai = toStrings(got["ai_fields"])
	if slices.Contains(ai, "brand") || !slices.Contains(ai, "model") || !slices.Contains(ai, "name") {
		t.Fatalf("ai_fields after patch: %v", ai)
	}

	for _, body := range []string{
		`{"color":"black","ai_fields":["brand"]}`,           // not in this request
		`{"color":"black","ai_fields":["attributes.type"]}`, // no attributes in this request
		`{"color":"black","ai_fields":["status"]}`,          // not an AI-fillable field
		`{"attributes":{"type":"x"},"ai_fields":["attributes.size"]}`,
		`{"color":"black","ai_fields":"color"}`,
	} {
		if s, _ := httpapitest.Do(t, e.srv, "PATCH", "/api/v1/items/"+id, body); s != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", body, s)
		}
		if s, _ := httpapitest.Do(t, e.srv, "POST", "/api/v1/items", `{"name":"x","ai_fields":["brand"]}`); s != http.StatusUnprocessableEntity {
			t.Errorf("create with unlisted field: status %d", s)
		}
	}
	e.must(http.StatusOK, "GET", "/api/v1/items/"+id, "", &got)
	if slices.Contains(toStrings(got["ai_fields"]), "color") {
		t.Fatalf("rejected patch changed the item: %v", got)
	}
}
