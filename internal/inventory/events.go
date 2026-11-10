package inventory

// History (J3.6). RecordEvent is the one place that writes the events table;
// every write function of this package calls it inside its own transaction,
// so a change and its history commit or roll back together. Two kinds of
// subscribers hear about it:
//
//   - OnChange hooks run inside the transaction, right after the event row is
//     written (search keeps its index in step this way). An error aborts the
//     change.
//   - OnCommit subscribers (the event stream) run only after the transaction
//     committed. InTx collects the notices of a transaction and publishes them
//     on success, so a rolled-back write emits nothing.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

// Event kinds (spec §3).
var EventKinds = []string{"created", "edited", "moved", "status_changed", "photo_added", "confirmed", "seen",
	"packed", "opened", "arrived", "unpacked", "deleted", "restored", "settings_changed"}

// Change is what an OnChange hook is told: which household, what happened
// (an event kind) and to which item and/or place or box (empty when not
// involved).
type Change struct {
	HouseholdID string
	Kind        string
	ItemID      string
	PlaceID     string
}

// Notice is what a committed change looks like to the event stream: Type is
// "item", "place" or "box", ID the thing's ID and Kind the event kind.
type Notice struct {
	HouseholdID string
	Type        string
	ID          string
	Kind        string
}

var hooks struct {
	mu       sync.RWMutex
	next     int
	onChange map[int]func(ctx context.Context, tx *sql.Tx, c Change) error
	onCommit map[int]func(Notice)
}

// OnChange subscribes fn to every recorded change. fn runs inside the
// transaction of the change, after the event row is written; returning an
// error rolls the change back. Hooks must be quick and must not start their
// own transaction. The returned function unsubscribes (tests use it).
func OnChange(fn func(ctx context.Context, tx *sql.Tx, c Change) error) (unsubscribe func()) {
	hooks.mu.Lock()
	defer hooks.mu.Unlock()
	if hooks.onChange == nil {
		hooks.onChange = map[int]func(context.Context, *sql.Tx, Change) error{}
	}
	hooks.next++
	n := hooks.next
	hooks.onChange[n] = fn
	return func() { hooks.mu.Lock(); delete(hooks.onChange, n); hooks.mu.Unlock() }
}

// OnCommit subscribes fn to changes after their transaction committed (only
// transactions run through InTx are reported). fn must not block.
func OnCommit(fn func(Notice)) (unsubscribe func()) {
	hooks.mu.Lock()
	defer hooks.mu.Unlock()
	if hooks.onCommit == nil {
		hooks.onCommit = map[int]func(Notice){}
	}
	hooks.next++
	n := hooks.next
	hooks.onCommit[n] = fn
	return func() { hooks.mu.Lock(); delete(hooks.onCommit, n); hooks.mu.Unlock() }
}

func changeHooks() []func(context.Context, *sql.Tx, Change) error {
	hooks.mu.RLock()
	defer hooks.mu.RUnlock()
	out := make([]func(context.Context, *sql.Tx, Change) error, 0, len(hooks.onChange))
	for _, k := range sortedInts(hooks.onChange) {
		out = append(out, hooks.onChange[k])
	}
	return out
}

func sortedInts[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// ---- collecting notices until commit ---------------------------------------

var pending sync.Map // *sql.Tx -> *txNotices

type txNotices struct {
	mu sync.Mutex
	n  []Notice
}

func notify(tx *sql.Tx, n Notice) {
	if v, ok := pending.Load(tx); ok {
		c := v.(*txNotices)
		c.mu.Lock()
		c.n = append(c.n, n)
		c.mu.Unlock()
	}
}

// InTx runs fn in a write transaction like db.Tx, and after a successful
// commit tells the OnCommit subscribers what changed. Everything that writes
// through this package and wants the live stream to hear about it (the HTTP
// handlers, AI jobs, imports) should run its transaction through InTx.
func InTx(ctx context.Context, d *db.DB, fn func(tx *sql.Tx) error) error {
	var notices []Notice
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		c := &txNotices{}
		pending.Store(tx, c)
		defer pending.Delete(tx)
		err := fn(tx)
		c.mu.Lock()
		notices = c.n
		c.mu.Unlock()
		return err
	})
	if err != nil {
		return err
	}
	hooks.mu.RLock()
	subs := make([]func(Notice), 0, len(hooks.onCommit))
	for _, k := range sortedInts(hooks.onCommit) {
		subs = append(subs, hooks.onCommit[k])
	}
	hooks.mu.RUnlock()
	for _, n := range notices {
		for _, s := range subs {
			s(n)
		}
	}
	return nil
}

// ---- who is acting ---------------------------------------------------------

type ctxKey int

const (
	aiKey ctxKey = iota
	quietKey
)

// WithAI marks writes made with the returned context as done by the AI;
// startedBy is the user who started the job (may be empty).
func WithAI(ctx context.Context, startedBy string) context.Context {
	return context.WithValue(ctx, aiKey, startedBy)
}

// quiet suppresses events (first-run seeding).
func quiet(ctx context.Context) context.Context { return context.WithValue(ctx, quietKey, true) }

type actor struct{ kind, userID, tokenID, tokenName string }

// actorOf decides who the event is attributed to: the AI when the context says
// so, else the caller's identity (a token acts as its owner, "via" the token),
// else the user the write function was given.
func actorOf(ctx context.Context, userID string) actor {
	if v, ok := ctx.Value(aiKey).(string); ok {
		return actor{kind: "ai", userID: v}
	}
	if idt, ok := auth.FromContext(ctx); ok {
		if idt.Source == auth.SourceToken {
			return actor{kind: "token", userID: idt.UserID, tokenID: idt.TokenID, tokenName: idt.TokenName}
		}
		return actor{kind: "user", userID: idt.UserID}
	}
	return actor{kind: "user", userID: userID}
}

// ---- the choke point -------------------------------------------------------

// EventInput is one history entry. Before and After hold only what changed.
type EventInput struct {
	HouseholdID string
	Kind        string
	ItemID      string // empty for place-only events
	PlaceID     string // the place or box concerned (an item's current place for item events)
	PlaceKind   string // "box" makes the stream type "box" instead of "place"
	UserID      string // used when the context carries no identity
	Subject     string // "lines" for changes to contents lines (the event belongs to their container)
	Before      map[string]any
	After       map[string]any
}

// RecordEvent writes one event in tx and runs the OnChange hooks. It is the
// only code that inserts into events. Other packages (photos, boxes) call it
// for their own changes (photo_added, packed, ...).
func RecordEvent(ctx context.Context, tx *sql.Tx, in EventInput) error {
	if ctx.Value(quietKey) != nil {
		return nil
	}
	if in.HouseholdID == "" || (in.ItemID == "" && in.PlaceID == "") || !slices.Contains(EventKinds, in.Kind) {
		return fmt.Errorf("inventory: bad event %q for household %q", in.Kind, in.HouseholdID)
	}
	data := map[string]any{}
	if in.Subject != "" {
		data["subject"] = in.Subject
	}
	if len(in.Before) > 0 {
		data["before"] = in.Before
	}
	if len(in.After) > 0 {
		data["after"] = in.After
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	a := actorOf(ctx, in.UserID)
	err = sqlc.New(tx).InsertEvent(ctx, sqlc.InsertEventParams{ID: id.New(), HouseholdID: in.HouseholdID, At: now(),
		ActorKind: a.kind, UserID: nullStr(a.userID), TokenID: nullStr(a.tokenID), TokenName: nullStr(a.tokenName),
		Kind: in.Kind, ItemID: nullStr(in.ItemID), PlaceID: nullStr(in.PlaceID), Data: string(b)})
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	c := Change{HouseholdID: in.HouseholdID, Kind: in.Kind, ItemID: in.ItemID, PlaceID: in.PlaceID}
	for _, h := range changeHooks() {
		if err := h(ctx, tx, c); err != nil {
			return err
		}
	}
	switch {
	case in.ItemID != "":
		notify(tx, Notice{in.HouseholdID, "item", in.ItemID, in.Kind})
	case in.PlaceKind == "box":
		notify(tx, Notice{in.HouseholdID, "box", in.PlaceID, in.Kind})
	default:
		notify(tx, Notice{in.HouseholdID, "place", in.PlaceID, in.Kind})
	}
	return nil
}

// ---- recording item changes --------------------------------------------------

func placePathOf(ctx context.Context, q *sqlc.Queries, hid string, placeID sql.NullString) any {
	if !placeID.Valid {
		return nil
	}
	p, err := q.GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: hid, ID: placeID.String})
	if err != nil {
		return nil
	}
	return p.Path
}

func ns(n sql.NullString) any {
	if !n.Valid {
		return nil
	}
	return n.String
}

func ni(n sql.NullInt64) any {
	if !n.Valid {
		return nil
	}
	return n.Int64
}

// itemEdits lists the plainly edited fields of an item (everything but place,
// status and bookkeeping), as before/after maps with only the changed ones.
func itemEdits(a, b sqlc.Item) (before, after map[string]any) {
	before, after = map[string]any{}, map[string]any{}
	diff := func(k string, x, y any) {
		if x != y {
			before[k], after[k] = x, y
		}
	}
	diff("name", a.Name, b.Name)
	diff("category", a.Category, b.Category)
	diff("brand", ns(a.Brand), ns(b.Brand))
	diff("model", ns(a.Model), ns(b.Model))
	diff("serial", ns(a.Serial), ns(b.Serial))
	diff("color", ns(a.Color), ns(b.Color))
	diff("quantity", a.Quantity, b.Quantity)
	diff("condition", ns(a.Condition), ns(b.Condition))
	diff("notes", ns(a.Notes), ns(b.Notes))
	diff("purchase_date", ns(a.PurchaseDate), ns(b.PurchaseDate))
	diff("price_minor", ni(a.PriceMinor), ni(b.PriceMinor))
	diff("currency", ns(a.Currency), ns(b.Currency))
	diff("seller", ns(a.Seller), ns(b.Seller))
	diff("warranty_end", ns(a.WarrantyEnd), ns(b.WarrantyEnd))
	var am, bm map[string]any
	_ = json.Unmarshal([]byte(a.Attributes), &am)
	_ = json.Unmarshal([]byte(b.Attributes), &bm)
	for k := range am {
		if bv, ok := bm[k]; !ok || fmt.Sprint(bv) != fmt.Sprint(am[k]) {
			before["attributes."+k], after["attributes."+k] = am[k], bv
		}
	}
	for k, bv := range bm {
		if _, ok := am[k]; !ok {
			before["attributes."+k], after["attributes."+k] = nil, bv
		}
	}
	if a.Draft != b.Draft {
		before["draft"], after["draft"] = a.Draft != 0, b.Draft != 0
	}
	return before, after
}

// recordItemDiff records what changed between two states of an item: a
// "moved" event when the place changed, "status_changed" when the status
// did, "edited" (changed fields only) for the rest. Nothing changed and seen
// set records "seen"; nothing changed otherwise records nothing.
func recordItemDiff(ctx context.Context, tx *sql.Tx, hid, userID string, a, b sqlc.Item, seen bool) error {
	q := sqlc.New(tx)
	in := EventInput{HouseholdID: hid, ItemID: b.ID, PlaceID: b.PlaceID.String, UserID: userID}
	recorded := false
	if a.PlaceID != b.PlaceID {
		in.Kind = "moved"
		in.Before = map[string]any{"place_id": ns(a.PlaceID), "place_path": placePathOf(ctx, q, hid, a.PlaceID)}
		in.After = map[string]any{"place_id": ns(b.PlaceID), "place_path": placePathOf(ctx, q, hid, b.PlaceID)}
		if err := RecordEvent(ctx, tx, in); err != nil {
			return err
		}
		recorded = true
	}
	if a.Status != b.Status {
		in.Kind, in.Before, in.After = "status_changed", map[string]any{"status": a.Status}, map[string]any{"status": b.Status}
		if err := RecordEvent(ctx, tx, in); err != nil {
			return err
		}
		recorded = true
	}
	if before, after := itemEdits(a, b); len(after) > 0 {
		in.Kind, in.Before, in.After = "edited", before, after
		if err := RecordEvent(ctx, tx, in); err != nil {
			return err
		}
		recorded = true
	}
	if !recorded && seen {
		in.Kind, in.Before, in.After = "seen", nil, map[string]any{"last_seen_at": b.LastSeenAt}
		return RecordEvent(ctx, tx, in)
	}
	return nil
}
