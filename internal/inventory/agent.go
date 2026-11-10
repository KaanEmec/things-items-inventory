package inventory

import (
	"context"

	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
)

// Exported reads for the MCP endpoint (J9.3); the HTTP handlers do the same
// through the unexported scope types.

// ItemHistory lists an item's events, newest first. The item must exist in
// the household.
func ItemHistory(ctx context.Context, q sqlc.DBTX, householdID, itemID, cursor string, limit int) ([]HistoryEntry, string, error) {
	if _, err := getItemRow(ctx, sqlc.New(q), householdID, itemID); err != nil {
		return nil, "", err
	}
	return History(ctx, q, householdID, historyScope{itemID: itemID}, cursor, limit)
}

// PlaceHistory lists the events of a place or box, newest first.
func PlaceHistory(ctx context.Context, q sqlc.DBTX, householdID, placeID, cursor string, limit int) ([]HistoryEntry, string, error) {
	if _, err := GetPlace(ctx, q, householdID, placeID); err != nil {
		return nil, "", err
	}
	return History(ctx, q, householdID, historyScope{placeID: placeID}, cursor, limit)
}

// HouseholdHistory lists every event of the household, newest first.
func HouseholdHistory(ctx context.Context, q sqlc.DBTX, householdID, cursor string, limit int) ([]HistoryEntry, string, error) {
	return History(ctx, q, householdID, historyScope{}, cursor, limit)
}

// LiveHomes lists the household's homes that are not archived.
func LiveHomes(ctx context.Context, q sqlc.DBTX, householdID string) ([]Home, error) {
	rows, err := sqlc.New(q).ListHomes(ctx, sqlc.ListHomesParams{HouseholdID: householdID})
	if err != nil {
		return nil, err
	}
	out := make([]Home, len(rows))
	for i, h := range rows {
		out[i] = homeJSON(h)
	}
	return out, nil
}

// ChildPlaces lists the places directly inside p.
func ChildPlaces(ctx context.Context, q sqlc.DBTX, householdID string, p sqlc.Place) ([]Place, error) {
	kids, err := sqlc.New(q).ListChildPlaces(ctx, sqlc.ListChildPlacesParams{HouseholdID: householdID, ParentID: nullStr(p.ID)})
	if err != nil {
		return nil, err
	}
	out := make([]Place, len(kids))
	for i, k := range kids {
		out[i] = placeJSON(k, nil, nil)
	}
	return out, nil
}
