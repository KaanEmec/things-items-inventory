package mcp

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/search"
)

const (
	defaultPage = 20
	maxPage     = 50
)

func page(n int) int {
	if n < 1 {
		return defaultPage
	}
	return min(n, maxPage)
}

// clean makes user text safe to put in a line of output: control characters
// and newlines become spaces and the result is quoted.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, s)
	return fmt.Sprintf("%q", s)
}

func itemLine(it inventory.Item) string {
	where := "not placed"
	if it.Place != nil {
		where = clean(it.Place.Path)
	}
	extra := ""
	if it.Draft {
		extra = ", draft"
	}
	return fmt.Sprintf("%s [%s, %s%s] at %s (id %s)", clean(it.Name), it.Category, it.Status, extra, where, it.ID)
}

func matchLine(m search.Match) string {
	s := fmt.Sprintf("%s: %s", m.Kind, clean(m.Title))
	if m.Path != "" {
		s += " at " + clean(m.Path)
	}
	return s + " (id " + m.ID + ")"
}

type cursorIn struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size, default 20, max 50"`
}

func (s *Service) readTools(srv *sdk.Server, call callFn) {
	type searchIn struct {
		Query string   `json:"query" jsonschema:"what to look for, e.g. 'ski gloves' or a box number"`
		Kinds []string `json:"kinds,omitempty" jsonschema:"limit to item, line, place or box"`
		Limit int      `json:"limit,omitempty" jsonschema:"hits per kind, default 8, max 50"`
	}
	add(srv, call, "search", "Find things by words: items, loose contents lines (things listed inside a place or box), places and boxes, each with its place path. Use this first for any 'where is my ...' question.",
		func(c caller, in searchIn) (result, error) {
			if strings.TrimSpace(in.Query) == "" {
				return result{}, httpapi.BadRequest("query is required.")
			}
			o := search.Options{Limit: in.Limit}
			for _, k := range in.Kinds {
				o.Kinds = append(o.Kinds, k)
			}
			res, err := search.Search(c.ctx, s.db.Read, c.hid, in.Query, o)
			if err != nil {
				return result{}, err
			}
			var b strings.Builder
			n := 0
			for _, g := range [][]search.Match{res.Items, res.Lines, res.Places, res.Boxes} {
				for _, m := range g {
					b.WriteString(matchLine(m) + "\n")
					n++
				}
			}
			if n == 0 {
				return result{text: "No matches for \"" + in.Query + "\".", data: res}, nil
			}
			return result{text: b.String(), data: res}, nil
		})

	type idIn struct {
		ID string `json:"id" jsonschema:"the item ID"`
	}
	add(srv, call, "get_item", "Full details of one item: place path, status, condition, warranty, attributes.",
		func(c caller, in idIn) (result, error) {
			it, err := inventory.GetItem(c.ctx, s.db.Read, c.hid, in.ID)
			if err != nil {
				return result{}, err
			}
			return result{text: itemLine(it), data: it}, nil
		})

	type placeIn struct {
		ID string `json:"id" jsonschema:"the place or box ID"`
	}
	add(srv, call, "get_place", "A place (room, furniture, spot) or box with what is directly inside it: child places, items and contents lines.",
		func(c caller, in placeIn) (result, error) {
			return s.placeResult(c, in.ID)
		})

	type boxIn struct {
		ID     string `json:"id,omitempty" jsonschema:"the box ID"`
		Number int64  `json:"number,omitempty" jsonschema:"or the number written on the box"`
	}
	add(srv, call, "get_box", "A moving box with its contents (items and lines), by ID or by the number on the box.",
		func(c caller, in boxIn) (result, error) {
			var (
				b   inventory.BoxDetail
				err error
			)
			switch {
			case in.ID != "":
				b, err = inventory.GetBox(c.ctx, s.db.Read, c.hid, in.ID)
			case in.Number > 0:
				b, err = inventory.GetBoxByNumber(c.ctx, s.db.Read, c.hid, in.Number)
			default:
				return result{}, httpapi.BadRequest("Give id or number.")
			}
			if err != nil {
				return result{}, err
			}
			return result{text: boxText(b), data: b}, nil
		})

	type listIn struct {
		cursorIn
		Category string `json:"category,omitempty" jsonschema:"category key"`
		Status   string `json:"status,omitempty" jsonschema:"in_use, stored, packed, to_sell, lent or gone"`
		PlaceID  string `json:"place_id,omitempty" jsonschema:"only items in this place or anywhere below it"`
		Draft    bool   `json:"draft,omitempty" jsonschema:"only drafts waiting for review"`
		Warranty string `json:"warranty,omitempty" jsonschema:"active, ending (within 60 days) or any"`
	}
	add(srv, call, "list_items", "List items, newest touched first, with optional filters. Paged.",
		func(c caller, in listIn) (result, error) {
			f := inventory.ItemFilter{Category: in.Category, PlaceID: in.PlaceID, Below: in.PlaceID != "", Draft: in.Draft,
				Warranty: in.Warranty, Cursor: in.Cursor, Limit: page(in.Limit)}
			if in.Status != "" {
				f.Statuses = []string{in.Status}
			}
			if in.PlaceID != "" {
				if _, err := inventory.GetPlace(c.ctx, s.db.Read, c.hid, in.PlaceID); err != nil {
					return result{}, err
				}
			}
			items, next, err := inventory.ListItems(c.ctx, s.db.Read, c.hid, f)
			if err != nil {
				return result{}, err
			}
			var b strings.Builder
			for _, it := range items {
				b.WriteString(itemLine(it) + "\n")
			}
			if next != "" {
				b.WriteString("More: pass cursor \"" + next + "\".\n")
			}
			if len(items) == 0 {
				b.WriteString("No items.")
			}
			return result{text: b.String(), data: httpapi.NewPage(items, next)}, nil
		})

	type treeIn struct {
		HomeID string `json:"home_id,omitempty" jsonschema:"one home; omit to list the homes and the tree of the first"`
	}
	add(srv, call, "list_places", "The places tree of a home (rooms, furniture, spots) with item counts, and the list of homes. Use it to find a place ID.",
		func(c caller, in treeIn) (result, error) {
			homes, err := inventory.LiveHomes(c.ctx, s.db.Read, c.hid)
			if err != nil {
				return result{}, err
			}
			hid := in.HomeID
			if hid == "" && len(homes) > 0 {
				hid = homes[0].ID
			}
			var places []inventory.Place
			if hid != "" {
				if _, err := sqlc.New(s.db.Read).GetHome(c.ctx, sqlc.GetHomeParams{HouseholdID: c.hid, ID: hid}); err != nil {
					return result{}, httpapi.NotFound("That home does not exist.")
				}
				if places, err = inventory.Tree(c.ctx, s.db.Read, c.hid, hid); err != nil {
					return result{}, err
				}
			}
			var b strings.Builder
			for _, h := range homes {
				fmt.Fprintf(&b, "home %s (id %s)\n", clean(h.Name), h.ID)
			}
			for _, p := range places {
				n := int64(0)
				if p.Counts != nil {
					n = p.Counts.Items
				}
				fmt.Fprintf(&b, "%s: %s, %d items (id %s)\n", p.Kind, clean(p.Path), n, p.ID)
			}
			return result{text: b.String(), data: map[string]any{"homes": homes, "places": places}}, nil
		})

	type histIn struct {
		cursorIn
		ItemID  string `json:"item_id,omitempty" jsonschema:"history of this item"`
		PlaceID string `json:"place_id,omitempty" jsonschema:"or of this place or box; omit both for the whole household"`
	}
	add(srv, call, "get_history", "What changed and who changed it, newest first. Moves show the old and new place path.",
		func(c caller, in histIn) (result, error) {
			var (
				es   []inventory.HistoryEntry
				next string
				err  error
			)
			switch {
			case in.ItemID != "":
				es, next, err = inventory.ItemHistory(c.ctx, s.db.Read, c.hid, in.ItemID, in.Cursor, page(in.Limit))
			case in.PlaceID != "":
				es, next, err = inventory.PlaceHistory(c.ctx, s.db.Read, c.hid, in.PlaceID, in.Cursor, page(in.Limit))
			default:
				es, next, err = inventory.HouseholdHistory(c.ctx, s.db.Read, c.hid, in.Cursor, page(in.Limit))
			}
			if err != nil {
				return result{}, err
			}
			var b strings.Builder
			for _, e := range es {
				who := clean(e.Actor.Name)
				if e.Actor.TokenName != nil {
					who += " via " + clean(*e.Actor.TokenName)
				}
				name := ""
				if e.ItemName != nil {
					name = " " + clean(*e.ItemName)
				}
				fmt.Fprintf(&b, "%s %s%s by %s\n", e.At, e.Kind, name, who)
			}
			if next != "" {
				b.WriteString("More: pass cursor \"" + next + "\".\n")
			}
			return result{text: b.String(), data: httpapi.NewPage(es, next)}, nil
		})
}

func (s *Service) placeResult(c caller, id string) (result, error) {
	p, err := inventory.GetPlace(c.ctx, s.db.Read, c.hid, id)
	if err != nil {
		return result{}, err
	}
	if p.Kind == "box" {
		b, err := inventory.GetBox(c.ctx, s.db.Read, c.hid, id)
		if err != nil {
			return result{}, err
		}
		return result{text: boxText(b), data: b}, nil
	}
	view, err := inventory.PlaceView(c.ctx, s.db.Read, c.hid, p)
	if err != nil {
		return result{}, err
	}
	kids, err := inventory.ChildPlaces(c.ctx, s.db.Read, c.hid, p)
	if err != nil {
		return result{}, err
	}
	items, next, err := inventory.ListItems(c.ctx, s.db.Read, c.hid, inventory.ItemFilter{PlaceID: id, Limit: maxPage})
	if err != nil {
		return result{}, err
	}
	lines, err := sqlc.New(s.db.Read).ListLines(c.ctx, sqlc.ListLinesParams{HouseholdID: c.hid, PlaceID: id})
	if err != nil {
		return result{}, err
	}
	ls := make([]inventory.Line, len(lines))
	for i, l := range lines {
		ls[i] = inventory.LineJSON(l)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s (id %s)\n", view.Kind, clean(view.Path), view.ID)
	for _, k := range kids {
		fmt.Fprintf(&b, "inside: %s (id %s)\n", clean(k.Name), k.ID)
	}
	for _, it := range items {
		b.WriteString(itemLine(it) + "\n")
	}
	for _, l := range ls {
		fmt.Fprintf(&b, "%d x %s\n", l.Quantity, clean(l.Name))
	}
	if next != "" {
		b.WriteString("More items: use list_items with place_id.\n")
	}
	return result{text: b.String(), data: map[string]any{"place": view, "children": kids, "items": items, "lines": ls, "items_truncated": next != ""}}, nil
}

func boxText(b inventory.BoxDetail) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s (id %s)\n", clean(b.Name), b.ID)
	for _, it := range b.Items {
		sb.WriteString(itemLine(it) + "\n")
	}
	for _, l := range b.Lines {
		fmt.Fprintf(&sb, "%d x %s\n", l.Quantity, clean(l.Name))
	}
	return sb.String()
}

// ---- write tools -------------------------------------------------------------

// fields builds the item Fields of the API from the non-empty arguments.
func fields(kv map[string]any) inventory.Fields {
	f := inventory.Fields{}
	for k, v := range kv {
		switch x := v.(type) {
		case string:
			if x == "" {
				continue
			}
		case int64:
			if x == 0 {
				continue
			}
		}
		b, _ := json.Marshal(v)
		f[k] = b
	}
	return f
}

func (s *Service) writeItem(c caller, fn func(tx *sql.Tx) (string, error)) (result, error) {
	var it inventory.Item
	err := inventory.InTx(c.ctx, s.db, func(tx *sql.Tx) error {
		id, err := fn(tx)
		if err != nil {
			return err
		}
		it, err = inventory.GetItem(c.ctx, tx, c.hid, id)
		return err
	})
	if err != nil {
		return result{}, err
	}
	return result{text: "Saved: " + itemLine(it), data: it}, nil
}

func (s *Service) writeTools(srv *sdk.Server, call callFn) {
	type itemFields struct {
		Name      string `json:"name,omitempty"`
		Category  string `json:"category,omitempty" jsonschema:"category key, e.g. from an existing item"`
		Brand     string `json:"brand,omitempty"`
		Model     string `json:"model,omitempty"`
		Serial    string `json:"serial,omitempty"`
		Color     string `json:"color,omitempty"`
		Quantity  int64  `json:"quantity,omitempty"`
		Condition string `json:"condition,omitempty"`
		Notes     string `json:"notes,omitempty"`
		PlaceID   string `json:"place_id,omitempty" jsonschema:"ID of the place or box it is in (from search or list_places)"`
	}
	kv := func(f itemFields) map[string]any {
		return map[string]any{"name": f.Name, "category": f.Category, "brand": f.Brand, "model": f.Model, "serial": f.Serial,
			"color": f.Color, "quantity": f.Quantity, "condition": f.Condition, "notes": f.Notes, "place_id": f.PlaceID}
	}
	add(srv, call, "add_item", "Add an item. Search first so you do not add a duplicate. It is saved as a draft for a person to review. Name is required.",
		func(c caller, in itemFields) (result, error) {
			m := kv(in)
			m["draft"] = true
			var it inventory.Item
			err := inventory.InTx(c.ctx, s.db, func(tx *sql.Tx) error {
				row, _, err := inventory.CreateItem(c.ctx, tx, c.hid, c.uid, inventory.NewItem{Source: "agent", Fields: fields(m)})
				if err != nil {
					return err
				}
				it, err = inventory.GetItem(c.ctx, tx, c.hid, row.ID)
				return err
			})
			if err != nil {
				return result{}, err
			}
			return result{text: "Added as a draft: " + itemLine(it), data: it}, nil
		})

	type updateIn struct {
		ID string `json:"id" jsonschema:"the item ID"`
		itemFields
	}
	add(srv, call, "update_item", "Change fields of an item; only the fields you pass change. To move it, prefer move_item.",
		func(c caller, in updateIn) (result, error) {
			return s.writeItem(c, func(tx *sql.Tx) (string, error) {
				row, err := inventory.UpdateItem(c.ctx, tx, c.hid, c.uid, in.ID, fields(kv(in.itemFields)))
				return row.ID, err
			})
		})

	type moveIn struct {
		ID      string `json:"id" jsonschema:"the item ID"`
		PlaceID string `json:"place_id" jsonschema:"the destination place or box ID"`
	}
	add(srv, call, "move_item", "Put an item in a different place or box. Use it for 'I moved the drill to the garage shelf'; find the place ID with search or list_places first.",
		func(c caller, in moveIn) (result, error) {
			if in.PlaceID == "" {
				return result{}, httpapi.BadRequest("place_id is required.")
			}
			return s.writeItem(c, func(tx *sql.Tx) (string, error) {
				row, err := inventory.MoveItem(c.ctx, tx, c.hid, c.uid, in.ID, &in.PlaceID)
				return row.ID, err
			})
		})

	type statusIn struct {
		ID     string `json:"id" jsonschema:"the item ID"`
		Status string `json:"status" jsonschema:"in_use, stored, packed, to_sell, lent or gone"`
	}
	add(srv, call, "set_status", "Set an item's status: in_use, stored, packed, to_sell, lent or gone. Nothing is deleted.",
		func(c caller, in statusIn) (result, error) {
			return s.writeItem(c, func(tx *sql.Tx) (string, error) {
				row, err := inventory.UpdateItem(c.ctx, tx, c.hid, c.uid, in.ID, fields(map[string]any{"status": in.Status}))
				return row.ID, err
			})
		})

	type linesIn struct {
		PlaceID string `json:"place_id" jsonschema:"the place or box that holds them"`
		Lines   []struct {
			Name     string `json:"name"`
			Quantity int64  `json:"quantity,omitempty" jsonschema:"default 1"`
			Category string `json:"category,omitempty"`
		} `json:"lines"`
	}
	add(srv, call, "add_contents_lines", "Record cheap or bulky things (cables, screws, socks) as lines inside a place or box, without making an item for each.",
		func(c caller, in linesIn) (result, error) {
			var li []inventory.LineInput
			for _, l := range in.Lines {
				q := l.Quantity
				if q == 0 {
					q = 1
				}
				li = append(li, inventory.LineInput{Name: l.Name, Quantity: &q, Category: l.Category})
			}
			var rows []sqlc.ContentsLine
			err := inventory.InTx(c.ctx, s.db, func(tx *sql.Tx) (err error) {
				rows, err = inventory.CreateLines(c.ctx, tx, c.hid, c.uid, in.PlaceID, li)
				return
			})
			if err != nil {
				return result{}, err
			}
			out := make([]inventory.Line, len(rows))
			for i, r := range rows {
				out[i] = inventory.LineJSON(r)
			}
			return result{text: fmt.Sprintf("Added %d line(s).", len(out)), data: out}, nil
		})

	type placeIn struct {
		Name     string `json:"name" jsonschema:"human name, e.g. '3rd drawer from top'"`
		Kind     string `json:"kind" jsonschema:"floor, room, furniture or spot"`
		ParentID string `json:"parent_id,omitempty" jsonschema:"the place it is inside"`
		HomeID   string `json:"home_id,omitempty" jsonschema:"the home, when it has no parent (a floor or room)"`
		Note     string `json:"note,omitempty"`
	}
	add(srv, call, "create_place", "Create a place inside a parent place (or directly in a home). Search or list_places first to avoid duplicates.",
		func(c caller, in placeIn) (result, error) {
			if in.Kind == "box" {
				return result{}, httpapi.BadRequest("Use create_box for boxes.")
			}
			var out inventory.Place
			err := inventory.InTx(c.ctx, s.db, func(tx *sql.Tx) error {
				p, err := inventory.CreatePlace(c.ctx, tx, c.hid, inventory.PlaceInput{HomeID: in.HomeID, ParentID: in.ParentID, Kind: in.Kind, Name: in.Name, Note: in.Note})
				if err != nil {
					return err
				}
				out, err = inventory.PlaceView(c.ctx, tx, c.hid, p)
				return err
			})
			if err != nil {
				return result{}, err
			}
			return result{text: fmt.Sprintf("Created %s %s (id %s)", out.Kind, out.Path, out.ID), data: out}, nil
		})

	type boxIn struct {
		GoesToID string `json:"goes_to_id,omitempty" jsonschema:"ID of the destination room"`
		Note     string `json:"note,omitempty"`
	}
	add(srv, call, "create_box", "Create the next numbered moving box, optionally with its destination room.",
		func(c caller, in boxIn) (result, error) {
			var out inventory.Place
			err := inventory.InTx(c.ctx, s.db, func(tx *sql.Tx) error {
				rows, err := inventory.CreateBoxes(c.ctx, tx, c.hid, 1, 0, 0, inventory.BoxInput{GoesToID: in.GoesToID, Note: in.Note})
				if err != nil {
					return err
				}
				out, err = inventory.PlaceView(c.ctx, tx, c.hid, rows[0])
				return err
			})
			if err != nil {
				return result{}, err
			}
			return result{text: fmt.Sprintf("Created %s (id %s)", out.Name, out.ID), data: out}, nil
		})
}
