package inventory_test

import "testing"

func TestPurchaseDateGivesDefaultWarranty(t *testing.T) {
	f := newFx(t)
	e := f.e
	var it struct {
		Warranty struct{ End, Source *string }
	}
	e.must(200, "PATCH", "/api/v1/items/"+f.itemID, `{"purchase":{"date":"2024-03-14"}}`, &it)
	if it.Warranty.End == nil || *it.Warranty.End != "2026-03-14" || *it.Warranty.Source != "default" {
		t.Fatalf("%+v", it.Warranty)
	}
	e.must(200, "PATCH", "/api/v1/items/"+f.itemID, `{"purchase":{"date":"2025-01-01"}}`, &it) // the default follows the date
	if *it.Warranty.End != "2027-01-01" {
		t.Fatalf("%+v", it.Warranty)
	}
	e.must(200, "PATCH", "/api/v1/items/"+f.itemID, `{"warranty":{"end":"2030-05-05"}}`, &it) // a stated end is kept
	e.must(200, "PATCH", "/api/v1/items/"+f.itemID, `{"purchase":{"date":"2025-02-02"}}`, &it)
	if *it.Warranty.End != "2030-05-05" || *it.Warranty.Source != "stated" {
		t.Fatalf("%+v", it.Warranty)
	}
}
