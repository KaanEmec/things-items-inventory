package inventory

import (
	"context"
	"database/sql"
	"net/http"
	"sort"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

const (
	overviewRecent     = 8
	overviewRows       = 4   // rows shown per "needs a look" group
	overviewWarnDays   = 60  // same window as the items filter warranty=ending
	overviewUnseenDays = 180 // "not seen for a long time"
)

// OverviewCounts are the numbers of the status strip. Each equals the count
// of the items list under the filter named next to it.
type OverviewCounts struct {
	All        int64 `json:"all"`         // GET /items
	Drafts     int64 `json:"drafts"`      // draft=1
	Packed     int64 `json:"packed"`      // status=packed
	ToSell     int64 `json:"to_sell"`     // status=to_sell
	InWarranty int64 `json:"in_warranty"` // warranty=active
}

// NeedsRow is one row of a "needs a look" group. Item groups fill ItemID,
// Name, Category, PlacePath and At (the date that put it there: warranty end,
// last seen, last change); the failed-jobs group fills ID (the job), Name
// (the item, when it has one), Detail (the error) and At.
type NeedsRow struct {
	ID        string  `json:"id"`
	ItemID    *string `json:"item_id"`
	Name      string  `json:"name"`
	Category  *string `json:"category"`
	PlacePath *string `json:"place_path"`
	At        *string `json:"at"`
	Detail    *string `json:"detail"`
}

// NeedsGroup is one "needs a look" group. Count is the full number, Rows the
// first few.
type NeedsGroup struct {
	Kind  string     `json:"kind"` // drafts | warranty_ending | unseen | failed_jobs | not_found
	Count int64      `json:"count"`
	Rows  []NeedsRow `json:"rows"`
}

// OverviewRoom is the boxes going to one destination room (RoomID nil:
// boxes without one).
type OverviewRoom struct {
	RoomID *string `json:"room_id"`
	Name   *string `json:"name"`
	Color  *string `json:"color"`
	Total  int64   `json:"total"`
	Packed int64   `json:"packed"` // packed, arrived or unpacked: no longer waiting to be packed
}

// OverviewMoving summarises the boxes.
type OverviewMoving struct {
	Total    int64            `json:"total"`
	ByStatus map[string]int64 `json:"by_status"` // empty, open, packed, arrived, unpacked (all present)
	Rooms    []OverviewRoom   `json:"rooms"`
}

// OverviewResponse is GET /api/v1/overview.
type OverviewResponse struct {
	Counts    OverviewCounts  `json:"counts"`
	Places    int64           `json:"places"` // places excluding boxes; 0 with Counts.All == 0 is the first-day state
	Recent    []HistoryEntry  `json:"recent"`
	NeedsLook []NeedsGroup    `json:"needs_look"`
	Moving    *OverviewMoving `json:"moving"` // null until the household has a box
}

// Overview gathers everything the Overview screen shows. q is the read pool
// or a transaction.
func Overview(ctx context.Context, q sqlc.DBTX, householdID string) (OverviewResponse, error) {
	sq := sqlc.New(q)
	nowT := clock()
	today := nullStr(nowT.Format("2006-01-02"))
	endingTo := nullStr(nowT.AddDate(0, 0, overviewWarnDays).Format("2006-01-02"))
	cutoff := nowT.AddDate(0, 0, -overviewUnseenDays).Format(timeLayout)

	var out OverviewResponse
	c, err := sq.OverviewCounts(ctx, sqlc.OverviewCountsParams{HouseholdID: householdID, Today: today, EndingTo: endingTo})
	if err != nil {
		return out, err
	}
	out.Counts = OverviewCounts{All: c.Total, Drafts: c.Drafts, Packed: c.Packed, ToSell: c.ToSell, InWarranty: c.InWarranty}
	if out.Places, err = sq.OverviewPlaceCount(ctx, householdID); err != nil {
		return out, err
	}
	if out.Recent, _, err = History(ctx, q, householdID, historyScope{}, "", overviewRecent); err != nil {
		return out, err
	}
	if out.Recent == nil {
		out.Recent = []HistoryEntry{}
	}

	drafts, err := sq.OverviewDrafts(ctx, sqlc.OverviewDraftsParams{HouseholdID: householdID, Limit: overviewRows})
	if err != nil {
		return out, err
	}
	g := NeedsGroup{Kind: "drafts", Count: c.Drafts, Rows: []NeedsRow{}}
	for _, r := range drafts {
		g.Rows = append(g.Rows, itemRowOf(r.ID, r.Name, r.Category, r.PlacePath, r.At))
	}
	out.NeedsLook = append(out.NeedsLook, g)

	ending, err := sq.OverviewWarrantyEnding(ctx, sqlc.OverviewWarrantyEndingParams{HouseholdID: householdID, Today: today, EndingTo: endingTo, RowLimit: overviewRows})
	if err != nil {
		return out, err
	}
	g = NeedsGroup{Kind: "warranty_ending", Count: c.WarrantyEnding, Rows: []NeedsRow{}}
	for _, r := range ending {
		g.Rows = append(g.Rows, itemRowOf(r.ID, r.Name, r.Category, r.PlacePath, r.At.String))
	}
	out.NeedsLook = append(out.NeedsLook, g)

	// places.daily_use exists, so this group is always offered.
	unseenN, err := sq.OverviewUnseenCount(ctx, sqlc.OverviewUnseenCountParams{HouseholdID: householdID, LastSeenAt: cutoff})
	if err != nil {
		return out, err
	}
	unseen, err := sq.OverviewUnseen(ctx, sqlc.OverviewUnseenParams{HouseholdID: householdID, Cutoff: cutoff, RowLimit: overviewRows})
	if err != nil {
		return out, err
	}
	g = NeedsGroup{Kind: "unseen", Count: unseenN, Rows: []NeedsRow{}}
	for _, r := range unseen {
		g.Rows = append(g.Rows, itemRowOf(r.ID, r.Name, r.Category, nullStr(r.PlacePath), r.At))
	}
	out.NeedsLook = append(out.NeedsLook, g)

	failedN, err := sq.OverviewFailedJobCount(ctx, householdID)
	if err != nil {
		return out, err
	}
	failed, err := sq.OverviewFailedJobs(ctx, sqlc.OverviewFailedJobsParams{HouseholdID: householdID, Limit: overviewRows})
	if err != nil {
		return out, err
	}
	g = NeedsGroup{Kind: "failed_jobs", Count: failedN, Rows: []NeedsRow{}}
	for _, r := range failed {
		at := r.At
		name := r.ItemName.String
		if name == "" {
			name = r.Kind
		}
		g.Rows = append(g.Rows, NeedsRow{ID: r.ID, ItemID: strPtr(r.ItemID), Name: name, At: &at, Detail: strPtr(r.LastError)})
	}
	out.NeedsLook = append(out.NeedsLook, g)

	boxes, err := sq.OverviewBoxes(ctx, householdID)
	if err != nil {
		return out, err
	}
	if len(boxes) > 0 {
		lostN, err := sq.OverviewNotFoundCount(ctx, householdID)
		if err != nil {
			return out, err
		}
		lost, err := sq.OverviewNotFound(ctx, sqlc.OverviewNotFoundParams{HouseholdID: householdID, Limit: overviewRows})
		if err != nil {
			return out, err
		}
		g = NeedsGroup{Kind: "not_found", Count: lostN, Rows: []NeedsRow{}}
		for _, r := range lost {
			g.Rows = append(g.Rows, itemRowOf(r.ID, r.Name, r.Category, sql.NullString{}, r.At))
		}
		if lostN > 0 {
			out.NeedsLook = append(out.NeedsLook, g)
		}
		m := &OverviewMoving{ByStatus: map[string]int64{"empty": 0, "open": 0, "packed": 0, "arrived": 0, "unpacked": 0}, Rooms: []OverviewRoom{}}
		rooms := map[string]*OverviewRoom{}
		for _, b := range boxes {
			m.Total += b.N
			m.ByStatus[b.BoxStatus.String] += b.N
			key := b.RoomID.String
			r := rooms[key]
			if r == nil {
				r = &OverviewRoom{RoomID: strPtr(b.RoomID), Name: strPtr(b.RoomName), Color: strPtr(b.RoomColor)}
				rooms[key] = r
			}
			r.Total += b.N
			switch b.BoxStatus.String {
			case "packed", "arrived", "unpacked":
				r.Packed += b.N
			}
		}
		for _, r := range rooms {
			m.Rooms = append(m.Rooms, *r)
		}
		sort.Slice(m.Rooms, func(i, j int) bool {
			a, b := m.Rooms[i], m.Rooms[j]
			if (a.RoomID == nil) != (b.RoomID == nil) {
				return a.RoomID != nil // boxes without a room last
			}
			if a.Total != b.Total {
				return a.Total > b.Total
			}
			if a.RoomID == nil {
				return false
			}
			return *a.RoomID < *b.RoomID
		})
		out.Moving = m
	}
	return out, nil
}

func itemRowOf(id, name, category string, path sql.NullString, at string) NeedsRow {
	return NeedsRow{ID: id, ItemID: &id, Name: name, Category: &category, PlacePath: strPtr(path), At: &at}
}

func (s *Service) getOverview(w http.ResponseWriter, r *http.Request) {
	o, err := Overview(r.Context(), s.db.Read, auth.HouseholdID(r.Context()))
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, o)
}
