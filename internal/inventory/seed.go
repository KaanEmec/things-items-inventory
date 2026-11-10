package inventory

import (
	"context"
	"database/sql"

	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

// SeedTestData creates a home, two places, an item and a contents line in the
// household, for the cross-household test. It returns the IDs to try in
// every {param} and marker strings that must never leak.
func SeedTestData(ctx context.Context, d *db.DB, householdID string) (pathValues, markers []string, err error) {
	err = d.Tx(ctx, func(tx *sql.Tx) error {
		h, err := CreateHome(ctx, tx, householdID, "Secret-Home-7Q", "")
		if err != nil {
			return err
		}
		room, err := CreatePlace(ctx, tx, householdID, PlaceInput{HomeID: h.ID, Kind: "room", Name: "Secret-Room-7Q"})
		if err != nil {
			return err
		}
		drawer, err := CreatePlace(ctx, tx, householdID, PlaceInput{ParentID: room.ID, Kind: "furniture", Name: "Secret-Drawer-7Q"})
		if err != nil {
			return err
		}
		item, _, err := CreateItem(ctx, tx, householdID, "", NewItem{Fields: Fields{
			"name": rawJSON("Secret-Item-7Q"), "place_id": rawJSON(drawer.ID), "brand": rawJSON("Secret-Brand-7Q")}})
		if err != nil {
			return err
		}
		lines, err := CreateLines(ctx, tx, householdID, "", drawer.ID, []LineInput{{Name: "Secret-Line-7Q"}})
		if err != nil {
			return err
		}
		pathValues = []string{h.ID, room.ID, drawer.ID, item.ID, lines[0].ID, id.New()}
		markers = []string{"Secret-Home-7Q", "Secret-Room-7Q", "Secret-Drawer-7Q", "Secret-Item-7Q", "Secret-Brand-7Q", "Secret-Line-7Q"}
		return nil
	})
	return pathValues, markers, err
}
