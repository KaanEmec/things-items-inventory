package inventory

import (
	"net/http"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

// recentPlacesLimit is how many places GET /me/recent-places returns.
const recentPlacesLimit = 8

// recentPlaces serves GET /me/recent-places: the last distinct places the
// caller put or moved items into, newest first, from the events table.
func (s *Service) recentPlaces(w http.ResponseWriter, r *http.Request) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	rows, err := sqlc.New(s.db.Read).ListRecentPlacesOf(ctx, sqlc.ListRecentPlacesOfParams{
		HouseholdID: hid, UserID: nullStr(auth.UserID(ctx)), RowLimit: recentPlacesLimit})
	if err != nil {
		fail(w, err)
		return
	}
	out := []Place{}
	for _, row := range rows {
		p, err := GetPlace(ctx, s.db.Read, hid, row.PlaceID.String)
		if err != nil {
			continue
		}
		v, err := PlaceView(ctx, s.db.Read, hid, p)
		if err != nil {
			fail(w, err)
			return
		}
		out = append(out, v)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}
