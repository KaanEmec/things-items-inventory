package search

import (
	"context"
	"database/sql"
	"sync"

	"github.com/kaanemec/things-items-inventory/internal/inventory"
)

var subscribeOnce sync.Once

// Subscribe makes every inventory write update the index in the same
// transaction. It is idempotent and called by New, so wiring the search
// feature is enough.
func Subscribe() {
	subscribeOnce.Do(func() { inventory.OnChange(onChange) })
}

// onChange maps a recorded change to index work. Item events carry ItemID;
// events with only a PlaceID are about a place or box or about the contents
// lines of one, so the place, everything below it and what is in it is
// refreshed (a rename or move changes every path below).
func onChange(ctx context.Context, tx *sql.Tx, c inventory.Change) error {
	switch {
	case c.ItemID != "":
		return IndexItem(ctx, tx, c.HouseholdID, c.ItemID)
	case c.PlaceID != "":
		return IndexPlaceTree(ctx, tx, c.HouseholdID, c.PlaceID)
	}
	return nil
}
