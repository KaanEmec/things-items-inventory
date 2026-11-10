package inventory

// Moving boxes (J8.1). A box is a place of kind "box" with no home and no
// parent, so items, contents lines, photos, history and search treat it like
// any other place. This file adds what is special about boxes: numbers,
// destination room, handling marks, status and the "packing now" box of a
// person.

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

// Handling marks and box statuses (spec section 3).
var (
	HandlingMarks = []string{"heavy", "fragile", "soft", "this_side_up"}
	BoxStatuses   = []string{"empty", "open", "packed", "arrived", "unpacked"}
)

// MaxBoxesPerRequest bounds one create call.
const MaxBoxesPerRequest = 200

func boxName(n int64) string { return "Box " + strconv.FormatInt(n, 10) }

func checkHandling(h []string) ([]string, error) {
	for _, m := range h {
		if !slices.Contains(HandlingMarks, m) {
			return nil, invalid("handling", "Use any of: "+strings.Join(HandlingMarks, ", ")+".")
		}
	}
	out := []string{}
	for _, m := range HandlingMarks { // canonical order, no duplicates
		if slices.Contains(h, m) {
			out = append(out, m)
		}
	}
	return out, nil
}

// checkDestination requires a live room (or floor) of any home.
func checkDestination(ctx context.Context, q sqlc.DBTX, householdID, goesTo string) error {
	if goesTo == "" {
		return nil
	}
	g, err := sqlc.New(q).GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: goesTo})
	if err != nil && !isNoRows(err) {
		return err
	}
	if isNoRows(err) || g.DeletedAt.Valid || (g.Kind != "room" && g.Kind != "floor") {
		return invalid("goes_to_id", "Choose a room.")
	}
	return nil
}

// ---- creating ----------------------------------------------------------------

// BoxInput describes new boxes.
type BoxInput struct {
	GoesToID string
	Handling []string
	Note     string
}

// CreateBoxes makes boxes in the transaction. Either count > 0 (that many boxes
// with the next numbers of the household) or from..to (an inclusive range;
// from must not be below the next free number, a number is never reused).
// Numbers come from households.next_box_number inside the transaction, so
// concurrent creates never collide.
func CreateBoxes(ctx context.Context, tx *sql.Tx, householdID string, count, from, to int64, in BoxInput) ([]sqlc.Place, error) {
	q := sqlc.New(tx)
	handling, err := checkHandling(in.Handling)
	if err != nil {
		return nil, err
	}
	if err := checkDestination(ctx, tx, householdID, in.GoesToID); err != nil {
		return nil, err
	}
	var first, n int64
	switch {
	case count > 0 && (from != 0 || to != 0):
		return nil, invalid("count", "Give either count or from and to.")
	case count > 0:
		if count > MaxBoxesPerRequest {
			return nil, invalid("count", fmt.Sprintf("Create up to %d boxes at a time.", MaxBoxesPerRequest))
		}
		next, err := q.ReserveBoxNumbers(ctx, sqlc.ReserveBoxNumbersParams{Count: count, UpdatedAt: now(), HouseholdID: householdID})
		if err != nil {
			return nil, err
		}
		first, n = next-count, count
	case from >= 1 && to >= from:
		if to-from+1 > MaxBoxesPerRequest {
			return nil, invalid("to", fmt.Sprintf("Create up to %d boxes at a time.", MaxBoxesPerRequest))
		}
		next, err := q.PeekBoxNumber(ctx, householdID)
		if err != nil {
			return nil, err
		}
		if from < next {
			return nil, httpapi.Conflict(fmt.Sprintf("Box numbers below %d are already used. Start at %d or higher.", next, next))
		}
		if err := q.SkipBoxNumbers(ctx, sqlc.SkipBoxNumbersParams{Next: to + 1, UpdatedAt: now(), HouseholdID: householdID}); err != nil {
			return nil, err
		}
		first, n = from, to-from+1
	default:
		return nil, invalid("count", "Give count, or from and to with from at least 1 and to not below from.")
	}
	out := make([]sqlc.Place, 0, n)
	for i := int64(0); i < n; i++ {
		num := first + i
		p, err := CreatePlace(ctx, tx, householdID, PlaceInput{Kind: "box", Name: boxName(num), BoxNumber: &num,
			BoxStatus: "empty", GoesToID: in.GoesToID, Handling: handling, Note: in.Note})
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ---- reading -----------------------------------------------------------------

// BoxDetail is a box with what is in it, for the one-request box read.
type BoxDetail struct {
	Place
	Items []Item `json:"items"`
	Lines []Line `json:"lines"`
}

func liveBox(ctx context.Context, q sqlc.DBTX, householdID, boxID string) (sqlc.Place, error) {
	p, err := sqlc.New(q).GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: boxID})
	if err != nil && !isNoRows(err) {
		return p, err
	}
	if isNoRows(err) || p.Kind != "box" || p.DeletedAt.Valid {
		return p, httpapi.NotFound("That box does not exist.")
	}
	return p, nil
}

// GetBox returns a box with its items, contents lines and destination room.
func GetBox(ctx context.Context, q sqlc.DBTX, householdID, boxID string) (BoxDetail, error) {
	p, err := liveBox(ctx, q, householdID, boxID)
	if err != nil {
		return BoxDetail{}, err
	}
	return boxDetail(ctx, q, householdID, p)
}

// GetBoxByNumber is GetBox for the box with the given number.
func GetBoxByNumber(ctx context.Context, q sqlc.DBTX, householdID string, number int64) (BoxDetail, error) {
	p, err := sqlc.New(q).GetBoxByNumber(ctx, sqlc.GetBoxByNumberParams{HouseholdID: householdID, BoxNumber: sql.NullInt64{Int64: number, Valid: true}})
	if isNoRows(err) {
		return BoxDetail{}, httpapi.NotFound("There is no box with that number.")
	} else if err != nil {
		return BoxDetail{}, err
	}
	return boxDetail(ctx, q, householdID, p)
}

func boxDetail(ctx context.Context, q sqlc.DBTX, householdID string, p sqlc.Place) (BoxDetail, error) {
	view, err := PlaceView(ctx, q, householdID, p)
	if err != nil {
		return BoxDetail{}, err
	}
	items, _, err := ListItems(ctx, q, householdID, ItemFilter{PlaceID: p.ID, Sort: "name"})
	if err != nil {
		return BoxDetail{}, err
	}
	lines, err := sqlc.New(q).ListLines(ctx, sqlc.ListLinesParams{HouseholdID: householdID, PlaceID: p.ID})
	if err != nil {
		return BoxDetail{}, err
	}
	return BoxDetail{Place: view, Items: items, Lines: linesJSON(lines)}, nil
}

// BoxFilter selects boxes for ListBoxes.
type BoxFilter struct {
	Status   string // "" = any
	GoesToID string // "" = any
	Cursor   string // the last number of the previous page
	Limit    int
}

// ListBoxes returns boxes by number, with destination room and counts, in one
// query, and the cursor of the next page ("" at the end).
func ListBoxes(ctx context.Context, q sqlc.DBTX, householdID string, f BoxFilter) ([]Place, string, error) {
	if f.Status != "" && !slices.Contains(BoxStatuses, f.Status) {
		return nil, "", httpapi.Invalid(map[string]string{"status": "Use one of: " + strings.Join(BoxStatuses, ", ") + "."})
	}
	var after int64
	if f.Cursor != "" {
		n, err := strconv.ParseInt(f.Cursor, 10, 64)
		if err != nil || n < 0 {
			return nil, "", httpapi.Invalid(map[string]string{"cursor": "That cursor is not valid."})
		}
		after = n
	}
	limit := f.Limit
	if limit <= 0 {
		limit = httpapi.DefaultLimit
	}
	rows, err := sqlc.New(q).ListBoxes(ctx, sqlc.ListBoxesParams{HouseholdID: householdID, Status: f.Status, GoesToID: f.GoesToID,
		AfterNumber: sql.NullInt64{Int64: after, Valid: true}, RowLimit: int64(limit) + 1})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = strconv.FormatInt(rows[len(rows)-1].BoxNumber.Int64, 10)
	}
	out := make([]Place, len(rows))
	for i, r := range rows {
		out[i] = Place{
			PlaceShort: PlaceShort{ID: r.ID, Kind: r.Kind, Name: r.Name, Path: r.Path, HomeID: strPtr(r.HomeID),
				BoxNumber: intPtr(r.BoxNumber), Color: strPtr(r.Color)},
			ParentID: strPtr(r.ParentID), Sort: r.Sort, DailyUse: r.DailyUse != 0, Note: strPtr(r.Note),
			BoxStatus: strPtr(r.BoxStatus), Handling: parseHandling(r.Handling), PackedAt: strPtr(r.PackedAt),
			LabelPrintedAt: strPtr(r.LabelPrintedAt), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			Counts: &Counts{Items: r.ItemsDirect, ItemsBelow: r.ItemsDirect, Lines: r.LinesDirect},
		}
		if r.GoesToID.Valid {
			out[i].GoesTo = &GoesTo{ID: r.GoesToID.String, Name: r.GoesToName.String, Color: strPtr(r.GoesToColor)}
		}
	}
	if err := fillPhotos(ctx, q, householdID, out); err != nil {
		return nil, "", err
	}
	return out, next, nil
}

// ---- changing ----------------------------------------------------------------

// BoxChange is a partial update of a box. A field that is not Set stays.
type BoxChange struct {
	GoesToID  Optional[string]   `json:"goes_to_id"`
	Handling  Optional[[]string] `json:"handling"`
	Note      Optional[string]   `json:"note"`
	BoxStatus Optional[string]   `json:"box_status"`
}

// UpdateBox changes the destination room, handling marks, note and/or status
// of a box. Edits are recorded as one "edited" event; a status change as
// "packed", "opened", "arrived" or "unpacked".
func UpdateBox(ctx context.Context, tx *sql.Tx, householdID, userID, boxID string, ch BoxChange) (sqlc.Place, error) {
	q := sqlc.New(tx)
	row, err := liveBox(ctx, tx, householdID, boxID)
	if err != nil {
		return row, err
	}
	goesTo, note, handling := row.GoesToID, row.Note, row.Handling
	if ch.GoesToID.Set {
		goesTo = sql.NullString{}
		if !ch.GoesToID.Null {
			if err := checkDestination(ctx, tx, householdID, ch.GoesToID.V); err != nil {
				return row, err
			}
			goesTo = nullStr(ch.GoesToID.V)
		}
	}
	if ch.Note.Set {
		note = sql.NullString{}
		if !ch.Note.Null {
			note = nullStr(strings.TrimSpace(ch.Note.V))
		}
	}
	if ch.Handling.Set {
		h, err := checkHandling(ch.Handling.V)
		if err != nil {
			return row, err
		}
		handling = string(rawJSON(h))
	}
	var wantStatus string
	if ch.BoxStatus.Set {
		if ch.BoxStatus.Null || !slices.Contains(BoxStatuses, ch.BoxStatus.V) || ch.BoxStatus.V == "empty" {
			return row, invalid("box_status", "Use one of: open, packed, arrived, unpacked.")
		}
		wantStatus = ch.BoxStatus.V
	}
	saved, err := q.UpdateBoxFields(ctx, sqlc.UpdateBoxFieldsParams{GoesToID: goesTo, Handling: handling, Note: note,
		UpdatedAt: now(), HouseholdID: householdID, ID: row.ID})
	if err != nil {
		return saved, err
	}
	before, after := map[string]any{}, map[string]any{}
	if ns(row.GoesToID) != ns(saved.GoesToID) {
		before["goes_to_id"], after["goes_to_id"] = ns(row.GoesToID), ns(saved.GoesToID)
	}
	if ns(row.Note) != ns(saved.Note) {
		before["note"], after["note"] = ns(row.Note), ns(saved.Note)
	}
	if row.Handling != saved.Handling {
		before["handling"], after["handling"] = parseHandling(row.Handling), parseHandling(saved.Handling)
	}
	if len(after) > 0 {
		if err := RecordEvent(ctx, tx, EventInput{HouseholdID: householdID, Kind: "edited", PlaceID: row.ID, PlaceKind: "box",
			UserID: userID, Before: before, After: after}); err != nil {
			return saved, err
		}
	}
	if wantStatus != "" {
		return setBoxStatus(ctx, tx, householdID, userID, saved, wantStatus)
	}
	return saved, nil
}

// CloseBox marks a box packed, recording when and by whom. Closing a packed
// box again changes nothing; a box that already arrived or was unpacked cannot
// be closed (409).
func CloseBox(ctx context.Context, tx *sql.Tx, householdID, userID, boxID string) (sqlc.Place, error) {
	row, err := liveBox(ctx, tx, householdID, boxID)
	if err != nil {
		return row, err
	}
	switch row.BoxStatus.String {
	case "arrived", "unpacked":
		return row, httpapi.Conflict("This box has already arrived.")
	}
	return setBoxStatus(ctx, tx, householdID, userID, row, "packed")
}

var statusEvent = map[string]string{"open": "opened", "packed": "packed", "arrived": "arrived", "unpacked": "unpacked"}

// setBoxStatus changes a box's status and records the event for it.
func setBoxStatus(ctx context.Context, tx *sql.Tx, householdID, userID string, row sqlc.Place, to string) (sqlc.Place, error) {
	if row.BoxStatus.String == to {
		return row, nil
	}
	packedAt, packedBy := row.PackedAt, row.PackedBy
	switch to {
	case "packed":
		packedAt, packedBy = nullStr(now()), nullStr(userID)
		if idt := actorOf(ctx, userID); idt.userID != "" {
			packedBy = nullStr(idt.userID)
		}
	case "open", "empty":
		packedAt, packedBy = sql.NullString{}, sql.NullString{}
	}
	saved, err := sqlc.New(tx).SetBoxStatus(ctx, sqlc.SetBoxStatusParams{BoxStatus: nullStr(to), PackedAt: packedAt, PackedBy: packedBy,
		UpdatedAt: now(), HouseholdID: householdID, ID: row.ID})
	if err != nil {
		return saved, err
	}
	return saved, RecordEvent(ctx, tx, EventInput{HouseholdID: householdID, Kind: statusEvent[to], PlaceID: row.ID, PlaceKind: "box",
		UserID: userID, Before: map[string]any{"box_status": ns(row.BoxStatus)}, After: map[string]any{"box_status": to}})
}

// openIfEmpty is called after something was put into a place (an item moved
// or created there, contents lines added): an empty box becomes open.
func openIfEmpty(ctx context.Context, tx *sql.Tx, householdID, userID string, placeID sql.NullString) error {
	if !placeID.Valid {
		return nil
	}
	p, err := sqlc.New(tx).GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: placeID.String})
	if isNoRows(err) {
		return nil
	} else if err != nil {
		return err
	}
	if p.Kind != "box" || p.DeletedAt.Valid || p.BoxStatus.String != "empty" {
		return nil
	}
	_, err = setBoxStatus(ctx, tx, householdID, userID, p, "open")
	return err
}

// ---- packing now ---------------------------------------------------------------

// PackingBox is the box a person is packing now, as /me and the packing-box
// endpoint show it.
type PackingBox struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	BoxNumber *int64  `json:"box_number"`
	BoxStatus *string `json:"box_status"`
	GoesTo    *GoesTo `json:"goes_to"`
}

// PackingBoxOf returns the person's packing-now box, nil when there is none
// (or it was deleted).
func PackingBoxOf(ctx context.Context, q sqlc.DBTX, householdID, userID string) (*PackingBox, error) {
	m, err := sqlc.New(q).GetMembership(ctx, sqlc.GetMembershipParams{HouseholdID: householdID, UserID: userID})
	if isNoRows(err) || (err == nil && !m.PackingBoxID.Valid) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	p, err := liveBox(ctx, q, householdID, m.PackingBoxID.String)
	if err != nil {
		return nil, nil
	}
	pb := &PackingBox{ID: p.ID, Name: p.Name, BoxNumber: intPtr(p.BoxNumber), BoxStatus: strPtr(p.BoxStatus)}
	if p.GoesToID.Valid {
		if g, err := sqlc.New(q).GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: p.GoesToID.String}); err == nil {
			pb.GoesTo = &GoesTo{ID: g.ID, Name: g.Name, Color: strPtr(g.Color)}
		}
	}
	return pb, nil
}

// SetPackingBox sets the person's packing-now box by ID or, when boxID is
// empty and number is not nil, by number. Nothing is created: an unknown box
// is a 422. Both empty clears it.
func SetPackingBox(ctx context.Context, tx *sql.Tx, householdID, userID, boxID string, number *int64) error {
	q := sqlc.New(tx)
	if boxID == "" && number != nil {
		p, err := q.GetBoxByNumber(ctx, sqlc.GetBoxByNumberParams{HouseholdID: householdID, BoxNumber: sql.NullInt64{Int64: *number, Valid: true}})
		if isNoRows(err) {
			return invalid("number", "There is no box with that number.")
		} else if err != nil {
			return err
		}
		boxID = p.ID
	}
	if boxID != "" {
		if err := ValidatePackingBox(ctx, tx, householdID, boxID); err != nil {
			return err
		}
	}
	return q.SetPackingBox(ctx, sqlc.SetPackingBoxParams{PackingBoxID: nullStr(boxID), HouseholdID: householdID, UserID: userID})
}
