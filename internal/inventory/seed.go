package inventory

import (
	"context"
	"database/sql"

	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

// SeedTestData creates a home, two places, an item (moved once, so there is
// history) and a contents line in the
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
		// History rows carrying the markers (paths of both places in a move).
		if _, err := MoveItem(ctx, tx, householdID, "", item.ID, &room.ID); err != nil {
			return err
		}
		// A box going to the room, with the item in it (moved there last).
		boxes, err := CreateBoxes(ctx, tx, householdID, 1, 0, 0, BoxInput{GoesToID: room.ID, Note: "Secret-BoxNote-7Q"})
		if err != nil {
			return err
		}
		if _, err := MoveItem(ctx, tx, householdID, "", item.ID, &boxes[0].ID); err != nil {
			return err
		}
		pathValues = []string{h.ID, room.ID, drawer.ID, item.ID, lines[0].ID, boxes[0].ID, id.New()}
		markers = []string{"Secret-Home-7Q", "Secret-Room-7Q", "Secret-Drawer-7Q", "Secret-Item-7Q", "Secret-Brand-7Q", "Secret-Line-7Q", "Secret-BoxNote-7Q"}
		return nil
	})
	return pathValues, markers, err
}
