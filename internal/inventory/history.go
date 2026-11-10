package inventory

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

// HistoryActor is who did it, with what an avatar needs.
type HistoryActor struct {
	Kind      string  `json:"kind"` // user | ai | token
	UserID    *string `json:"user_id"`
	Name      string  `json:"name"`
	Initials  string  `json:"initials"`
	TokenName *string `json:"token_name"` // "via <name>" for access tokens
}

// HistoryEntry is one event as the API shows it.
type HistoryEntry struct {
	ID        string          `json:"id"`
	At        string          `json:"at"`
	Kind      string          `json:"kind"`
	Actor     HistoryActor    `json:"actor"`
	ItemID    *string         `json:"item_id"`
	ItemName  *string         `json:"item_name"` // current name; null when the item is gone
	PlaceID   *string         `json:"place_id"`
	PlaceName *string         `json:"place_name"`
	Summary   map[string]any  `json:"summary"`
	Data      json.RawMessage `json:"data"` // {"subject"?, "before"?, "after"?}
}

// historyScope picks which events a listing returns.
type historyScope struct{ itemID, placeID string }

func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		if r := []rune(w)[0]; unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, unicode.ToUpper(r))
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

func encodeCursor(at, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at + "|" + id))
}

func decodeCursor(c string) (at, id string, err error) {
	if c == "" {
		return "", "", nil
	}
	b, derr := base64.RawURLEncoding.DecodeString(c)
	at, id, ok := strings.Cut(string(b), "|")
	if derr != nil || !ok || at == "" || id == "" {
		return "", "", httpapi.Invalid(map[string]string{"cursor": "That cursor is not valid."})
	}
	return at, id, nil
}

// History lists events of one scope in a household, newest first. q is the
// read pool or a transaction. The returned cursor is empty on the last page.
func History(ctx context.Context, q sqlc.DBTX, householdID string, sc historyScope, cursor string, limit int) ([]HistoryEntry, string, error) {
	cAt, cID, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > httpapi.MaxLimit {
		limit = httpapi.DefaultLimit
	}
	sq := sqlc.New(q)
	n := int64(limit + 1)
	var rows []sqlc.Event
	switch {
	case sc.itemID != "":
		rows, err = sq.ListEventsItem(ctx, sqlc.ListEventsItemParams{HouseholdID: householdID, ItemID: nullStr(sc.itemID), CursorAt: cAt, CursorID: cID, RowLimit: n})
	case sc.placeID != "":
		rows, err = sq.ListEventsPlace(ctx, sqlc.ListEventsPlaceParams{HouseholdID: householdID, PlaceID: nullStr(sc.placeID), CursorAt: cAt, CursorID: cID, RowLimit: n})
	default:
		rows, err = sq.ListEventsHousehold(ctx, sqlc.ListEventsHouseholdParams{HouseholdID: householdID, CursorAt: cAt, CursorID: cID, RowLimit: n})
	}
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = encodeCursor(last.At, last.ID)
	}
	out, err := enrich(ctx, sq, householdID, rows)
	return out, next, err
}

func enrich(ctx context.Context, q *sqlc.Queries, hid string, rows []sqlc.Event) ([]HistoryEntry, error) {
	var userIDs []string
	for _, r := range rows {
		if r.UserID.Valid && !slices.Contains(userIDs, r.UserID.String) {
			userIDs = append(userIDs, r.UserID.String)
		}
	}
	names := map[string]string{}
	if len(userIDs) > 0 {
		us, err := q.GetUserNames(ctx, userIDs)
		if err != nil {
			return nil, err
		}
		for _, u := range us {
			names[u.ID] = u.Name
		}
	}
	itemNames, placeKinds, placeNames := map[string]*string{}, map[string]string{}, map[string]*string{}
	out := make([]HistoryEntry, 0, len(rows))
	for _, r := range rows {
		e := HistoryEntry{ID: r.ID, At: r.At, Kind: r.Kind, ItemID: strPtr(r.ItemID), PlaceID: strPtr(r.PlaceID),
			Data: json.RawMessage(r.Data)}
		e.Actor = HistoryActor{Kind: r.ActorKind, UserID: strPtr(r.UserID), TokenName: strPtr(r.TokenName)}
		switch r.ActorKind {
		case "ai":
			e.Actor.Name, e.Actor.Initials = "AI", "AI"
		default:
			e.Actor.Name = names[r.UserID.String]
			if e.Actor.Name == "" {
				e.Actor.Name = "Someone"
			}
			e.Actor.Initials = initials(e.Actor.Name)
		}
		if r.ItemID.Valid {
			name, seen := itemNames[r.ItemID.String]
			if !seen {
				it, err := q.GetItemRow(ctx, sqlc.GetItemRowParams{HouseholdID: hid, ID: r.ItemID.String})
				if err != nil && !isNoRows(err) {
					return nil, err
				}
				if err == nil {
					name = &it.Name
				}
				itemNames[r.ItemID.String] = name
			}
			e.ItemName = name
		}
		if r.PlaceID.Valid {
			name, seen := placeNames[r.PlaceID.String]
			if !seen {
				p, err := q.GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: hid, ID: r.PlaceID.String})
				if err != nil && !isNoRows(err) {
					return nil, err
				}
				if err == nil {
					name = &p.Name
					placeKinds[p.ID] = p.Kind
				}
				placeNames[r.PlaceID.String] = name
			}
			e.PlaceName = name
		}
		e.Summary = summarize(r, placeKinds[r.PlaceID.String])
		out = append(out, e)
	}
	return out, nil
}

// summarize is the short machine-readable form of an event, for the UI to
// phrase in the household's language: {"subject": "item|place|box|lines",
// ...}. moved: from/to (place paths); status_changed: from/to (statuses);
// edited: fields (sorted names); created: name.
func summarize(r sqlc.Event, placeKind string) map[string]any {
	var d struct {
		Subject string         `json:"subject"`
		Before  map[string]any `json:"before"`
		After   map[string]any `json:"after"`
	}
	_ = json.Unmarshal([]byte(r.Data), &d)
	s := map[string]any{}
	switch {
	case d.Subject != "":
		s["subject"] = d.Subject
	case r.ItemID.Valid:
		s["subject"] = "item"
	case placeKind == "box":
		s["subject"] = "box"
	default:
		s["subject"] = "place"
	}
	switch r.Kind {
	case "moved":
		k := "place_path"
		if !r.ItemID.Valid {
			k = "path"
		}
		s["from"], s["to"] = d.Before[k], d.After[k]
	case "status_changed":
		s["from"], s["to"] = d.Before["status"], d.After["status"]
	case "edited":
		fields := make([]string, 0, len(d.After))
		for k := range d.After {
			fields = append(fields, k)
		}
		slices.Sort(fields)
		s["fields"] = fields
	case "created":
		s["name"] = d.After["name"]
		if c, ok := d.After["count"]; ok {
			s["count"] = c
		}
	}
	return s
}

// ---- HTTP ----------------------------------------------------------------

func (s *Service) historyOf(w http.ResponseWriter, r *http.Request, check func(ctx context.Context, q sqlc.DBTX, hid string) (historyScope, error)) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	cursor, limit, err := httpapi.ParsePaging(r)
	if err != nil {
		fail(w, err)
		return
	}
	sc, err := check(ctx, s.db.Read, hid)
	if err != nil {
		fail(w, err)
		return
	}
	entries, next, err := History(ctx, s.db.Read, hid, sc, cursor, limit)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, httpapi.NewPage(entries, next))
}

func (s *Service) itemHistory(w http.ResponseWriter, r *http.Request) {
	s.historyOf(w, r, func(ctx context.Context, q sqlc.DBTX, hid string) (historyScope, error) {
		_, err := getItemRow(ctx, sqlc.New(q), hid, r.PathValue("id"))
		return historyScope{itemID: r.PathValue("id")}, err
	})
}

func (s *Service) placeHistory(w http.ResponseWriter, r *http.Request) {
	s.historyOf(w, r, func(ctx context.Context, q sqlc.DBTX, hid string) (historyScope, error) {
		_, err := GetPlace(ctx, q, hid, r.PathValue("id"))
		return historyScope{placeID: r.PathValue("id")}, err
	})
}

func (s *Service) householdHistory(w http.ResponseWriter, r *http.Request) {
	s.historyOf(w, r, func(context.Context, sqlc.DBTX, string) (historyScope, error) { return historyScope{}, nil })
}
