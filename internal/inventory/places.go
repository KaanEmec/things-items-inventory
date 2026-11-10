package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

// PathSep joins the names of a place's path, inside out.
const PathSep = " · "

// ---- JSON shapes (spec §5) ------------------------------------------------

// PlaceShort is the short place JSON embedded in items and elsewhere.
type PlaceShort struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Name      string  `json:"name"`
	Path      string  `json:"path"`
	HomeID    *string `json:"home_id"`
	BoxNumber *int64  `json:"box_number"`
	Color     *string `json:"color"`
}

// GoesTo is the destination room of a place or box.
type GoesTo struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Color *string `json:"color"`
}

// Counts are the numbers shown next to a place. ItemsBelow includes the
// items directly in the place.
type Counts struct {
	Items      int64 `json:"items"`
	ItemsBelow int64 `json:"items_below"`
	Lines      int64 `json:"lines"`
}

// Place is the full place JSON. Photo is the place's main photo (role
// `place`), filled from PlacePhotoLoader in one query per tree or list.
type Place struct {
	PlaceShort
	ParentID       *string    `json:"parent_id"`
	Sort           int64      `json:"sort"`
	GoesTo         *GoesTo    `json:"goes_to"`
	DailyUse       bool       `json:"daily_use"`
	Note           *string    `json:"note"`
	BoxStatus      *string    `json:"box_status"`
	Handling       []string   `json:"handling"`
	PackedAt       *string    `json:"packed_at"`
	LabelPrintedAt *string    `json:"label_printed_at"`
	Photo          *ItemPhoto `json:"photo"`
	Counts         *Counts    `json:"counts,omitempty"`
	DeletedAt      *string    `json:"deleted_at,omitempty"`
	CreatedAt      string     `json:"created_at"`
	UpdatedAt      string     `json:"updated_at"`
}

// Short returns the short JSON of a place row.
func Short(p sqlc.Place) PlaceShort {
	return PlaceShort{ID: p.ID, Kind: p.Kind, Name: p.Name, Path: p.Path, HomeID: strPtr(p.HomeID),
		BoxNumber: intPtr(p.BoxNumber), Color: strPtr(p.Color)}
}

// PlacePhotoLoader returns the main place photo of each given place, in one
// query. The server glue sets it at start-up (inventory and photos do not
// import each other); until then places carry no photo.
var PlacePhotoLoader func(ctx context.Context, q sqlc.DBTX, householdID string, placeIDs []string) (map[string]ItemPhoto, error)

// fillPhotos sets Photo on every place of the slice with one lookup.
func fillPhotos(ctx context.Context, q sqlc.DBTX, householdID string, places []Place) error {
	if PlacePhotoLoader == nil || len(places) == 0 {
		return nil
	}
	ids := make([]string, len(places))
	for i := range places {
		ids[i] = places[i].ID
	}
	m, err := PlacePhotoLoader(ctx, q, householdID, ids)
	if err != nil {
		return err
	}
	for i := range places {
		if p, ok := m[places[i].ID]; ok {
			places[i].Photo = &p
		}
	}
	return nil
}

func parseHandling(s string) []string {
	var h []string
	if json.Unmarshal([]byte(s), &h) != nil || h == nil {
		return []string{}
	}
	return h
}

func placeJSON(p sqlc.Place, goesTo *GoesTo, c *Counts) Place {
	return Place{PlaceShort: Short(p), ParentID: strPtr(p.ParentID), Sort: p.Sort, GoesTo: goesTo,
		DailyUse: p.DailyUse != 0, Note: strPtr(p.Note), BoxStatus: strPtr(p.BoxStatus),
		Handling: parseHandling(p.Handling), PackedAt: strPtr(p.PackedAt), LabelPrintedAt: strPtr(p.LabelPrintedAt),
		Counts: c, DeletedAt: strPtr(p.DeletedAt), CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

// lineageBounds returns the half-open range [lo, hi) that holds exactly the
// lineages at and below a place: lineages end with "/", and "0" is the next
// character after it.
func lineageBounds(lineage string) (lo, hi string) {
	return lineage, strings.TrimSuffix(lineage, "/") + "0"
}

// ---- reading ---------------------------------------------------------------

// GetPlace loads a place of the household, deleted or not.
func GetPlace(ctx context.Context, q sqlc.DBTX, householdID, placeID string) (sqlc.Place, error) {
	p, err := sqlc.New(q).GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: placeID})
	if isNoRows(err) {
		return p, httpapi.NotFound("That place does not exist.")
	}
	return p, err
}

func getLivePlace(ctx context.Context, q sqlc.DBTX, householdID, placeID string) (sqlc.Place, error) {
	p, err := GetPlace(ctx, q, householdID, placeID)
	if err == nil && p.DeletedAt.Valid {
		return p, httpapi.NotFound("That place was deleted.")
	}
	return p, err
}

// PlaceView returns the full JSON of a place with its counts.
func PlaceView(ctx context.Context, q sqlc.DBTX, householdID string, p sqlc.Place) (Place, error) {
	lo, hi := lineageBounds(p.Lineage)
	c, err := sqlc.New(q).GetPlaceCounts(ctx, sqlc.GetPlaceCountsParams{HouseholdID: householdID,
		PlaceID: nullStr(p.ID), LineageLo: lo, LineageHi: hi})
	if err != nil {
		return Place{}, err
	}
	var gt *GoesTo
	if p.GoesToID.Valid {
		if g, err := sqlc.New(q).GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: p.GoesToID.String}); err == nil {
			gt = &GoesTo{ID: g.ID, Name: g.Name, Color: strPtr(g.Color)}
		}
	}
	out := []Place{placeJSON(p, gt, &Counts{Items: c.ItemsDirect, ItemsBelow: c.ItemsBelow, Lines: c.LinesDirect})}
	if err := fillPhotos(ctx, q, householdID, out); err != nil {
		return Place{}, err
	}
	return out[0], nil
}

// Tree loads every live place of a home in one query, with counts per place:
// items directly in it, items in it and everything below it (rolled up here,
// no query per node) and contents lines. Places come ordered by sort, then
// name; parent_id links them.
func Tree(ctx context.Context, q sqlc.DBTX, householdID, homeID string) ([]Place, error) {
	rows, err := sqlc.New(q).ListHomeTree(ctx, sqlc.ListHomeTreeParams{HouseholdID: householdID, HomeID: nullStr(homeID)})
	if err != nil {
		return nil, err
	}
	index := make(map[string]int, len(rows))
	for i, r := range rows {
		index[r.ID] = i
	}
	children := make(map[int][]int, len(rows))
	var roots []int
	for i, r := range rows {
		if pi, ok := index[r.ParentID.String]; r.ParentID.Valid && ok {
			children[pi] = append(children[pi], i)
		} else {
			roots = append(roots, i)
		}
	}
	below := make([]int64, len(rows))
	var sum func(i int) int64
	sum = func(i int) int64 {
		n := rows[i].ItemsDirect
		for _, c := range children[i] {
			n += sum(c)
		}
		below[i] = n
		return n
	}
	for _, r := range roots {
		sum(r)
	}
	out := make([]Place, len(rows))
	for i, r := range rows {
		out[i] = Place{
			PlaceShort: PlaceShort{ID: r.ID, Kind: r.Kind, Name: r.Name, Path: r.Path, HomeID: strPtr(r.HomeID),
				BoxNumber: intPtr(r.BoxNumber), Color: strPtr(r.Color)},
			ParentID: strPtr(r.ParentID), Sort: r.Sort, DailyUse: r.DailyUse != 0, Note: strPtr(r.Note),
			BoxStatus: strPtr(r.BoxStatus), Handling: parseHandling(r.Handling), PackedAt: strPtr(r.PackedAt),
			LabelPrintedAt: strPtr(r.LabelPrintedAt), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			Counts: &Counts{Items: r.ItemsDirect, ItemsBelow: below[i], Lines: r.LinesDirect},
		}
		if r.GoesToID.Valid {
			out[i].GoesTo = &GoesTo{ID: r.GoesToID.String, Name: r.GoesToName.String, Color: strPtr(r.GoesToColor)}
		}
	}
	if err := fillPhotos(ctx, q, householdID, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ValidatePackingBox checks a value about to be written to
// memberships.packing_box_id: it must be a live box of the household. That
// column has no foreign key (SQLite cannot add one later), so every writer
// calls this first.
func ValidatePackingBox(ctx context.Context, q sqlc.DBTX, householdID, boxID string) error {
	p, err := sqlc.New(q).GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: boxID})
	if err != nil && !isNoRows(err) {
		return err
	}
	if isNoRows(err) || p.Kind != "box" || p.DeletedAt.Valid {
		return httpapi.Unprocessable("That box does not exist.", map[string]string{"box_id": "No such box."})
	}
	return nil
}

// ---- homes ---------------------------------------------------------------

// Home is the JSON of a home.
type Home struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Sort       int64   `json:"sort"`
	ArchivedAt *string `json:"archived_at"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

func homeJSON(h sqlc.Home) Home {
	return Home{ID: h.ID, Name: h.Name, Sort: h.Sort, ArchivedAt: strPtr(h.ArchivedAt), CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt}
}

func checkName(field, s, what string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n == 0 || n > 100 {
		return "", invalid(field, what+" is required (up to 100 characters).")
	}
	return s, nil
}

// CreateHome adds a home. copyRoomsFrom (optional) is another home whose
// floors and rooms are copied into the new one.
func CreateHome(ctx context.Context, tx *sql.Tx, householdID, name, copyRoomsFrom string) (sqlc.Home, error) {
	name, err := checkName("name", name, "The name")
	if err != nil {
		return sqlc.Home{}, err
	}
	q := sqlc.New(tx)
	var src []sqlc.ListHomeTreeRow
	if copyRoomsFrom != "" {
		if _, err := q.GetHome(ctx, sqlc.GetHomeParams{HouseholdID: householdID, ID: copyRoomsFrom}); err != nil {
			if isNoRows(err) {
				return sqlc.Home{}, invalid("copy_rooms_from", "No such home.")
			}
			return sqlc.Home{}, err
		}
		if src, err = q.ListHomeTree(ctx, sqlc.ListHomeTreeParams{HouseholdID: householdID, HomeID: nullStr(copyRoomsFrom)}); err != nil {
			return sqlc.Home{}, err
		}
	}
	next, err := q.NextHomeSort(ctx, householdID)
	if err != nil {
		return sqlc.Home{}, err
	}
	t := now()
	h, err := q.CreateHome(ctx, sqlc.CreateHomeParams{ID: id.New(), HouseholdID: householdID, CreatedAt: t, UpdatedAt: t, Name: name, Sort: next})
	if err != nil {
		return h, err
	}
	// Parents before children: order by depth, keeping the sort order.
	var rooms []sqlc.ListHomeTreeRow
	for _, r := range src {
		if r.Kind == "floor" || r.Kind == "room" {
			rooms = append(rooms, r)
		}
	}
	depth := func(r sqlc.ListHomeTreeRow) int { return strings.Count(r.Path, PathSep) }
	sort.SliceStable(rooms, func(i, j int) bool { return depth(rooms[i]) < depth(rooms[j]) })
	copied := map[string]string{} // old id -> new id
	for _, r := range rooms {
		in := PlaceInput{HomeID: h.ID, Kind: r.Kind, Name: r.Name, Sort: &r.Sort, Color: r.Color.String,
			Note: r.Note.String, DailyUse: r.DailyUse != 0}
		if r.ParentID.Valid {
			np, ok := copied[r.ParentID.String]
			if !ok {
				continue
			}
			in.ParentID = np
		}
		p, err := CreatePlace(ctx, tx, householdID, in)
		if err != nil {
			return h, err
		}
		copied[r.ID] = p.ID
	}
	return h, nil
}

// UpdateHome changes a home's name, order or archived state; nil leaves a
// field as it is.
func UpdateHome(ctx context.Context, tx *sql.Tx, householdID, homeID string, name *string, sortNo *int64, archived *bool) (sqlc.Home, error) {
	q := sqlc.New(tx)
	h, err := q.GetHome(ctx, sqlc.GetHomeParams{HouseholdID: householdID, ID: homeID})
	if isNoRows(err) {
		return h, httpapi.NotFound("That home does not exist.")
	} else if err != nil {
		return h, err
	}
	n, s, a := h.Name, h.Sort, h.ArchivedAt
	if name != nil {
		if n, err = checkName("name", *name, "The name"); err != nil {
			return h, err
		}
	}
	if sortNo != nil {
		s = *sortNo
	}
	if archived != nil {
		switch {
		case *archived && !a.Valid:
			left, err := q.CountHomeContents(ctx, sqlc.CountHomeContentsParams{HouseholdID: householdID, HomeID: nullStr(homeID)})
			if err != nil {
				return h, err
			}
			if left > 0 {
				if left == 1 {
					return h, httpapi.Conflict("1 thing is still in this home. Unpack or move it first.")
				}
				return h, httpapi.Conflict(fmt.Sprintf("%d things are still in this home. Unpack or move them first.", left))
			}
			a = nullStr(now())
		case !*archived:
			a = sql.NullString{}
		}
	}
	saved, err := q.UpdateHome(ctx, sqlc.UpdateHomeParams{Name: n, Sort: s, ArchivedAt: a, UpdatedAt: now(), HouseholdID: householdID, ID: homeID})
	if err != nil || h.ArchivedAt.Valid == saved.ArchivedAt.Valid {
		return saved, err
	}
	// Archiving or restoring changes what search shows: the index hears of it as a household event.
	return saved, RecordEvent(ctx, tx, EventInput{HouseholdID: householdID, Kind: "settings_changed", Subject: "home",
		Before: map[string]any{"archived": h.ArchivedAt.Valid}, After: map[string]any{"archived": saved.ArchivedAt.Valid}})
}

// SeedStarter creates the first-run home with a few common rooms. Call it in
// the transaction that creates the household.
func SeedStarter(ctx context.Context, tx *sql.Tx, householdID string) error {
	ctx = quiet(ctx) // first-run rooms are not history
	h, err := CreateHome(ctx, tx, householdID, "Home", "")
	if err != nil {
		return err
	}
	for _, room := range []string{"Living room", "Bedroom", "Kitchen", "Bathroom", "Hallway", "Storage"} {
		if _, err := CreatePlace(ctx, tx, householdID, PlaceInput{HomeID: h.ID, Kind: "room", Name: room}); err != nil {
			return err
		}
	}
	return nil
}

// ---- creating places ------------------------------------------------------

// PlaceInput describes a new place. Boxes (kind "box", no home, no parent)
// are created by the boxes feature through this function too; the HTTP
// endpoint refuses them.
type PlaceInput struct {
	HomeID, ParentID string
	Kind, Name       string
	Sort             *int64
	GoesToID         string
	Color            string
	DailyUse         bool
	Note             string
	BoxNumber        *int64
	BoxStatus        string
	Handling         []string
}

func checkColor(color, kind string) error {
	if color == "" {
		return nil
	}
	if kind != "room" {
		return invalid("color", "Only rooms have a colour.")
	}
	if !slices.Contains(RoomColors, color) {
		return invalid("color", "Use one of: "+strings.Join(RoomColors, ", ")+".")
	}
	return nil
}

func checkGoesTo(ctx context.Context, q sqlc.DBTX, householdID, goesTo string) error {
	if goesTo == "" {
		return nil
	}
	g, err := sqlc.New(q).GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: goesTo})
	if err != nil && !isNoRows(err) {
		return err
	}
	if isNoRows(err) || g.DeletedAt.Valid {
		return invalid("goes_to_id", "No such place.")
	}
	return nil
}

// CreatePlace adds a place under a parent (or at the top of a home).
func CreatePlace(ctx context.Context, tx *sql.Tx, householdID string, in PlaceInput) (sqlc.Place, error) {
	q := sqlc.New(tx)
	name, err := checkName("name", in.Name, "The name")
	if err != nil {
		return sqlc.Place{}, err
	}
	if !slices.Contains(PlaceKinds, in.Kind) {
		return sqlc.Place{}, invalid("kind", "Use one of: "+strings.Join(PlaceKinds, ", ")+".")
	}
	if err := checkColor(in.Color, in.Kind); err != nil {
		return sqlc.Place{}, err
	}
	if err := checkGoesTo(ctx, tx, householdID, in.GoesToID); err != nil {
		return sqlc.Place{}, err
	}
	placeID := id.New()
	var homeID, parentID sql.NullString
	path, lineage := name, "/"+placeID+"/"
	switch {
	case in.Kind == "box":
		if in.HomeID != "" || in.ParentID != "" {
			return sqlc.Place{}, invalid("kind", "A box has no home and no parent.")
		}
	case in.ParentID != "":
		parent, err := q.GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: in.ParentID})
		if err != nil && !isNoRows(err) {
			return sqlc.Place{}, err
		}
		if isNoRows(err) || parent.DeletedAt.Valid || parent.Kind == "box" {
			return sqlc.Place{}, invalid("parent_id", "No such place to put it in.")
		}
		if in.HomeID != "" && in.HomeID != parent.HomeID.String {
			return sqlc.Place{}, invalid("home_id", "The parent is in another home.")
		}
		homeID, parentID = parent.HomeID, nullStr(parent.ID)
		path, lineage = name+PathSep+parent.Path, parent.Lineage+placeID+"/"
	default:
		if in.HomeID == "" {
			return sqlc.Place{}, invalid("home_id", "Give a home or a parent place.")
		}
		if _, err := q.GetHome(ctx, sqlc.GetHomeParams{HouseholdID: householdID, ID: in.HomeID}); err != nil {
			if isNoRows(err) {
				return sqlc.Place{}, invalid("home_id", "No such home.")
			}
			return sqlc.Place{}, err
		}
		homeID = nullStr(in.HomeID)
	}
	sortNo := int64(0)
	if in.Sort != nil {
		sortNo = *in.Sort
	} else if in.Kind != "box" {
		if sortNo, err = q.NextPlaceSort(ctx, sqlc.NextPlaceSortParams{HouseholdID: householdID, HomeID: homeID, ParentID: parentID}); err != nil {
			return sqlc.Place{}, err
		}
	}
	if in.Kind == "room" && in.Color == "" {
		if in.Color, err = nextRoomColor(ctx, q, householdID, homeID.String); err != nil {
			return sqlc.Place{}, err
		}
	}
	handling := in.Handling
	if handling == nil {
		handling = []string{}
	}
	hb, _ := json.Marshal(handling)
	t := now()
	p, err := q.CreatePlace(ctx, sqlc.CreatePlaceParams{ID: placeID, HouseholdID: householdID, CreatedAt: t, UpdatedAt: t,
		HomeID: homeID, ParentID: parentID, Kind: in.Kind, Name: name, Sort: sortNo, Path: path, Lineage: lineage,
		GoesToID: nullStr(in.GoesToID), Color: nullStr(in.Color), DailyUse: boolInt(in.DailyUse), Note: nullStr(in.Note),
		BoxNumber: sql.NullInt64{Int64: derefInt(in.BoxNumber), Valid: in.BoxNumber != nil},
		BoxStatus: nullStr(in.BoxStatus), Handling: string(hb)})
	if err != nil {
		return p, fmt.Errorf("create place: %w", err)
	}
	after := map[string]any{"name": p.Name, "kind": p.Kind, "path": p.Path, "parent_id": ns(p.ParentID)}
	if err := RecordEvent(ctx, tx, EventInput{HouseholdID: householdID, Kind: "created", PlaceID: p.ID, PlaceKind: p.Kind, After: after}); err != nil {
		return p, err
	}
	return p, nil
}

// nextRoomColor picks the colour for a new room of a home: the first colour of
// the palette no live room of the home has, or, when all are taken, the one
// used least (the palette starts over). The colour belongs to the room; boxes
// show the colour of the room they go to.
func nextRoomColor(ctx context.Context, q *sqlc.Queries, householdID, homeID string) (string, error) {
	used, err := q.ListRoomColors(ctx, sqlc.ListRoomColorsParams{HouseholdID: householdID, HomeID: nullStr(homeID)})
	if err != nil {
		return "", err
	}
	n := map[string]int{}
	for _, c := range used {
		n[c.String]++
	}
	best := RoomColors[0]
	for _, c := range RoomColors {
		if n[c] < n[best] {
			best = c
		}
	}
	return best, nil
}

func derefInt(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// ---- changing places ------------------------------------------------------

// PlaceChange is a partial update of a place. A nil Optional leaves the field.
type PlaceChange struct {
	Name     Optional[string] `json:"name"`
	ParentID Optional[string] `json:"parent_id"` // null = top of HomeID (or of the current home)
	HomeID   Optional[string] `json:"home_id"`
	Sort     Optional[int64]  `json:"sort"`
	GoesToID Optional[string] `json:"goes_to_id"`
	Color    Optional[string] `json:"color"`
	DailyUse Optional[bool]   `json:"daily_use"`
	Note     Optional[string] `json:"note"`
}

// UpdatePlace renames, reorders, moves or edits a place. Moving or renaming
// rewrites path and lineage of everything below it in one statement, so items
// in the subtree follow. Moving a place under itself or one of its
// descendants is refused (422).
func UpdatePlace(ctx context.Context, tx *sql.Tx, householdID, placeID string, ch PlaceChange) (sqlc.Place, error) {
	q := sqlc.New(tx)
	row, err := getLivePlace(ctx, tx, householdID, placeID)
	if err != nil {
		return row, err
	}
	u := placeUpdate{Name: row.Name, HomeID: row.HomeID, GoesToID: row.GoesToID, Color: row.Color,
		DailyUse: row.DailyUse, Note: row.Note}
	if ch.Name.Set {
		if ch.Name.Null {
			return row, invalid("name", "The name is required.")
		}
		if u.Name, err = checkName("name", ch.Name.V, "The name"); err != nil {
			return row, err
		}
	}
	if ch.GoesToID.Set {
		u.GoesToID = sql.NullString{}
		if !ch.GoesToID.Null {
			if err := checkGoesTo(ctx, tx, householdID, ch.GoesToID.V); err != nil {
				return row, err
			}
			if ch.GoesToID.V == row.ID {
				return row, invalid("goes_to_id", "A place cannot go to itself.")
			}
			u.GoesToID = nullStr(ch.GoesToID.V)
		}
	}
	if ch.Color.Set {
		u.Color = sql.NullString{}
		if !ch.Color.Null {
			u.Color = nullStr(ch.Color.V)
		}
	}
	if err := checkColor(u.Color.String, row.Kind); err != nil {
		return row, err
	}
	if ch.DailyUse.Set && !ch.DailyUse.Null {
		u.DailyUse = boolInt(ch.DailyUse.V)
	}
	if ch.Note.Set {
		u.Note = sql.NullString{}
		if !ch.Note.Null {
			u.Note = nullStr(ch.Note.V)
		}
	}
	if ch.Sort.Set && !ch.Sort.Null {
		v := ch.Sort.V
		u.Sort = &v
	}
	if ch.ParentID.Set || ch.HomeID.Set {
		if row.Kind == "box" {
			return row, invalid("parent_id", "A box cannot be moved into a place.")
		}
		var parentID, homeID string
		if ch.ParentID.Set {
			parentID = ch.ParentID.V
		} else {
			parentID = row.ParentID.String
		}
		if ch.HomeID.Set {
			homeID = ch.HomeID.V
		} else {
			homeID = row.HomeID.String
		}
		if parentID != "" {
			parent, err := getLivePlace(ctx, tx, householdID, parentID)
			if err != nil {
				return row, invalid("parent_id", "No such place to move it into.")
			}
			if parent.Kind == "box" {
				return row, invalid("parent_id", "Places cannot be moved into a box.")
			}
			if strings.HasPrefix(parent.Lineage, row.Lineage) {
				return row, invalid("parent_id", "A place cannot be moved under itself or something inside it.")
			}
			u.Parent, u.HomeID = &parent, parent.HomeID
		} else {
			if _, err := q.GetHome(ctx, sqlc.GetHomeParams{HouseholdID: householdID, ID: homeID}); err != nil {
				if isNoRows(err) {
					return row, invalid("home_id", "No such home.")
				}
				return row, err
			}
			u.HomeID = nullStr(homeID)
		}
		u.Moved = parentID != row.ParentID.String || u.HomeID != row.HomeID
	}
	if !u.Moved && row.ParentID.Valid {
		parent, err := q.GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: row.ParentID.String})
		if err != nil {
			return row, err
		}
		u.Parent = &parent
	}
	saved, err := writePlace(ctx, q, householdID, row, u)
	if err != nil {
		return saved, err
	}
	return saved, recordPlaceDiff(ctx, tx, householdID, row, saved)
}

// recordPlaceDiff records a moved place as one "moved" event (its subtree
// follows without events of its own) and the other changed fields as "edited".
func recordPlaceDiff(ctx context.Context, tx *sql.Tx, hid string, a, b sqlc.Place) error {
	in := EventInput{HouseholdID: hid, PlaceID: b.ID, PlaceKind: b.Kind}
	moved := a.ParentID != b.ParentID || a.HomeID != b.HomeID
	if moved {
		in.Kind = "moved"
		in.Before = map[string]any{"parent_id": ns(a.ParentID), "home_id": ns(a.HomeID), "path": a.Path}
		in.After = map[string]any{"parent_id": ns(b.ParentID), "home_id": ns(b.HomeID), "path": b.Path}
		if err := RecordEvent(ctx, tx, in); err != nil {
			return err
		}
	}
	before, after := map[string]any{}, map[string]any{}
	diff := func(k string, x, y any) {
		if x != y {
			before[k], after[k] = x, y
		}
	}
	diff("name", a.Name, b.Name)
	diff("goes_to_id", ns(a.GoesToID), ns(b.GoesToID))
	diff("color", ns(a.Color), ns(b.Color))
	diff("daily_use", a.DailyUse != 0, b.DailyUse != 0)
	diff("note", ns(a.Note), ns(b.Note))
	if !moved {
		diff("sort", a.Sort, b.Sort)
	}
	if len(after) == 0 {
		return nil
	}
	in.Kind, in.Before, in.After = "edited", before, after
	return RecordEvent(ctx, tx, in)
}

// placeUpdate is a place's resolved new state.
type placeUpdate struct {
	Name     string
	Parent   *sqlc.Place // nil = top of HomeID
	HomeID   sql.NullString
	Moved    bool   // the parent or home changed
	Sort     *int64 // explicit position among siblings
	GoesToID sql.NullString
	Color    sql.NullString
	DailyUse int64
	Note     sql.NullString
}

func writePlace(ctx context.Context, q *sqlc.Queries, householdID string, row sqlc.Place, u placeUpdate) (sqlc.Place, error) {
	path, parentLineage := u.Name, "/"
	var parentID sql.NullString
	if u.Parent != nil {
		path, parentLineage, parentID = u.Name+PathSep+u.Parent.Path, u.Parent.Lineage, nullStr(u.Parent.ID)
	}
	lineage := parentLineage + row.ID + "/"
	sortNo := row.Sort
	if u.Moved {
		var err error
		if sortNo, err = q.NextPlaceSort(ctx, sqlc.NextPlaceSortParams{HouseholdID: householdID, HomeID: u.HomeID, ParentID: parentID}); err != nil {
			return row, err
		}
	}
	p, err := q.UpdatePlace(ctx, sqlc.UpdatePlaceParams{HomeID: u.HomeID, ParentID: parentID, Name: u.Name, Sort: sortNo,
		Path: path, Lineage: lineage, GoesToID: u.GoesToID, Color: u.Color, DailyUse: u.DailyUse, Note: u.Note,
		UpdatedAt: now(), HouseholdID: householdID, ID: row.ID})
	if err != nil {
		return p, err
	}
	if path != row.Path || lineage != row.Lineage || u.HomeID != row.HomeID {
		_, hi := lineageBounds(row.Lineage)
		err := q.RewriteSubtree(ctx, sqlc.RewriteSubtreeParams{NewLineage: lineage, OldLineage: row.Lineage, OldPath: row.Path,
			NewPath: path, HomeID: u.HomeID, HouseholdID: householdID, LineageHi: hi, SelfID: row.ID})
		if err != nil {
			return p, err
		}
	}
	if u.Sort != nil {
		if err := placeAt(ctx, q, householdID, p, *u.Sort); err != nil {
			return p, err
		}
		return q.GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: p.ID})
	}
	return p, nil
}

// placeAt puts p at position pos among its siblings and renumbers them.
func placeAt(ctx context.Context, q *sqlc.Queries, householdID string, p sqlc.Place, pos int64) error {
	sibs, err := q.ListSiblings(ctx, sqlc.ListSiblingsParams{HouseholdID: householdID, HomeID: p.HomeID, ParentID: p.ParentID})
	if err != nil {
		return err
	}
	order := make([]sqlc.Place, 0, len(sibs))
	for _, s := range sibs {
		if s.ID != p.ID {
			order = append(order, s)
		}
	}
	pos = max(0, min(pos, int64(len(order))))
	order = slices.Insert(order, int(pos), p)
	for i, s := range order {
		if s.Sort != int64(i) || s.ID == p.ID {
			if err := q.SetPlaceSort(ctx, sqlc.SetPlaceSortParams{Sort: int64(i), HouseholdID: householdID, ID: s.ID}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- deleting places ------------------------------------------------------

// DeletePlace soft-deletes a place. A place that still holds places, items or
// contents lines is refused with a not_empty error (409, with the counts)
// unless moveTo names the place that takes all of them.
func DeletePlace(ctx context.Context, tx *sql.Tx, householdID, userID, placeID, moveTo string) (sqlc.Place, error) {
	q := sqlc.New(tx)
	row, err := getLivePlace(ctx, tx, householdID, placeID)
	if err != nil {
		return row, err
	}
	lo, hi := lineageBounds(row.Lineage)
	c, err := q.GetPlaceCounts(ctx, sqlc.GetPlaceCountsParams{HouseholdID: householdID, PlaceID: nullStr(row.ID), LineageLo: lo, LineageHi: hi})
	if err != nil {
		return row, err
	}
	if c.ChildPlaces+c.ItemsDirect+c.LinesDirect > 0 {
		if moveTo == "" {
			return row, httpapi.NotEmpty(fmt.Sprintf("This place still holds %d places, %d items and %d contents lines. Say where they should go.",
				c.ChildPlaces, c.ItemsDirect, c.LinesDirect),
				map[string]string{"places": fmt.Sprint(c.ChildPlaces), "items": fmt.Sprint(c.ItemsDirect), "lines": fmt.Sprint(c.LinesDirect)})
		}
		target, err := getLivePlace(ctx, tx, householdID, moveTo)
		if err != nil {
			return row, invalid("move_to", "No such place to move the contents to.")
		}
		if strings.HasPrefix(target.Lineage, row.Lineage) {
			return row, invalid("move_to", "The contents cannot move into the place being deleted or something inside it.")
		}
		if target.Kind == "box" && c.ChildPlaces > 0 {
			return row, invalid("move_to", "Places cannot be moved into a box.")
		}
		kids, err := q.ListChildPlaces(ctx, sqlc.ListChildPlacesParams{HouseholdID: householdID, ParentID: nullStr(row.ID)})
		if err != nil {
			return row, err
		}
		for _, k := range kids {
			u := placeUpdate{Name: k.Name, Parent: &target, HomeID: target.HomeID, Moved: true,
				GoesToID: k.GoesToID, Color: k.Color, DailyUse: k.DailyUse, Note: k.Note}
			if _, err := writePlace(ctx, q, householdID, k, u); err != nil {
				return row, err
			}
		}
		t := now()
		if _, err := q.MovePlaceItems(ctx, sqlc.MovePlaceItemsParams{ToID: nullStr(target.ID), Now: t, UserID: nullStr(userID),
			HouseholdID: householdID, FromID: nullStr(row.ID)}); err != nil {
			return row, err
		}
		if _, err := q.MovePlaceLines(ctx, sqlc.MovePlaceLinesParams{ToID: target.ID, Now: t, HouseholdID: householdID, FromID: row.ID}); err != nil {
			return row, err
		}
	}
	t := now()
	if err := q.SoftDeletePlace(ctx, sqlc.SoftDeletePlaceParams{DeletedAt: nullStr(t), UpdatedAt: t, HouseholdID: householdID, ID: row.ID}); err != nil {
		return row, err
	}
	if row.Kind == "box" {
		if err := q.ClearPackingBox(ctx, sqlc.ClearPackingBoxParams{HouseholdID: householdID, PackingBoxID: nullStr(row.ID)}); err != nil {
			return row, err
		}
	}
	after := map[string]any{}
	if moveTo != "" {
		after["moved_contents_to"] = moveTo
	}
	if err := RecordEvent(ctx, tx, EventInput{HouseholdID: householdID, Kind: "deleted", PlaceID: row.ID, PlaceKind: row.Kind,
		UserID: userID, Before: map[string]any{"name": row.Name, "path": row.Path}, After: after}); err != nil {
		return row, err
	}
	return q.GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: row.ID})
}

// RestorePlace undoes a delete. Its parent must still be there.
func RestorePlace(ctx context.Context, tx *sql.Tx, householdID, placeID string) (sqlc.Place, error) {
	q := sqlc.New(tx)
	row, err := GetPlace(ctx, tx, householdID, placeID)
	if err != nil {
		return row, err
	}
	if !row.DeletedAt.Valid {
		return row, nil
	}
	if row.ParentID.Valid {
		parent, err := GetPlace(ctx, tx, householdID, row.ParentID.String)
		if err != nil {
			return row, err
		}
		if parent.DeletedAt.Valid {
			return row, httpapi.Conflict("The place it was in has been deleted too. Restore that first.")
		}
	}
	if err := q.RestorePlace(ctx, sqlc.RestorePlaceParams{UpdatedAt: now(), HouseholdID: householdID, ID: row.ID}); err != nil {
		return row, err
	}
	if err := RecordEvent(ctx, tx, EventInput{HouseholdID: householdID, Kind: "restored", PlaceID: row.ID, PlaceKind: row.Kind,
		After: map[string]any{"name": row.Name, "path": row.Path}}); err != nil {
		return row, err
	}
	return q.GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: row.ID})
}
