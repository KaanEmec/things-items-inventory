package stream

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
)

// server serves the hub; the caller's household comes from X-Household.
func server(t *testing.T, h *Hub) *httptest.Server {
	t.Helper()
	return httpapitest.Server(t, func(mux *httpapi.Mux) {
		mux.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if hid := r.Header.Get("X-Household"); hid != "" {
					r = r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{UserID: "u", HouseholdID: hid, Role: auth.RoleViewer, Source: auth.SourceSession}))
				}
				next.ServeHTTP(w, r)
			})
		})
		h.Routes(mux)
	})
}

type conn struct {
	res    *http.Response
	lines  chan string
	cancel context.CancelFunc
}

func connect(t *testing.T, srv *httptest.Server, household string) *conn {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/stream", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("X-Household", household)
	res, err := srv.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	c := &conn{res: res, lines: make(chan string, 256), cancel: cancel}
	go func() {
		defer close(c.lines)
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			c.lines <- sc.Text()
		}
	}()
	return c
}

func (c *conn) close() { c.cancel(); c.res.Body.Close() }

// next returns the next "data:" payload (with its event name), skipping
// comments, or fails after a timeout.
func (c *conn) next(t *testing.T, within time.Duration) (event string, m Message) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				t.Fatal("stream closed")
			}
			if v, ok := strings.CutPrefix(l, "event: "); ok {
				event = v
			}
			if v, ok := strings.CutPrefix(l, "data: "); ok {
				if err := json.Unmarshal([]byte(v), &m); err != nil {
					t.Fatalf("bad data %q", v)
				}
				return event, m
			}
		case <-deadline:
			t.Fatal("no message")
		}
	}
}

func (c *conn) expectSilence(t *testing.T) {
	t.Helper()
	select {
	case l, ok := <-c.lines:
		if ok && strings.HasPrefix(l, "data:") {
			t.Fatalf("unexpected message %q", l)
		}
	case <-time.After(150 * time.Millisecond):
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 400 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestHeadersAndHouseholdIsolation(t *testing.T) {
	h := New()
	defer h.Close()
	srv := server(t, h)
	a, b := connect(t, srv, "hA"), connect(t, srv, "hB")
	defer a.close()
	defer b.close()
	if ct := a.res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	if ce := a.res.Header.Get("Content-Encoding"); ce == "gzip" || a.res.Header.Get("Cache-Control") == "" {
		t.Fatalf("encoding %q, cache %q", ce, a.res.Header.Get("Cache-Control"))
	}
	waitFor(t, "2 clients", func() bool { return h.Clients() == 2 })

	h.Publish(Message{HouseholdID: "hA", Event: "event", Type: "item", ID: "i1", Kind: "moved"})
	ev, m := a.next(t, 2*time.Second)
	if ev != "event" || m.Type != "item" || m.ID != "i1" || m.Kind != "moved" {
		t.Fatalf("got %q %+v", ev, m)
	}
	b.expectSilence(t)

	h.OnJob(jobs.Job{ID: "j1", HouseholdID: "hB", Kind: "draft_item", State: "running", ItemID: "i9"})
	ev, m = b.next(t, 2*time.Second)
	if ev != "job" || m.Type != "job" || m.ID != "j1" || m.Kind != "running" || m.ItemID != "i9" {
		t.Fatalf("got %q %+v", ev, m)
	}
	a.expectSilence(t)
}

func TestPlainGetAndAnonymousAreRefused(t *testing.T) {
	h := New()
	defer h.Close()
	srv := server(t, h)
	if st, _ := httpapitest.Do(t, srv, "GET", "/api/v1/stream", ""); st != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", st)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/stream", nil)
	req.Header.Set("X-Household", "hA")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("no Accept header: %d", res.StatusCode)
	}
}

func TestHeartbeat(t *testing.T) {
	h := New()
	h.Heartbeat = 20 * time.Millisecond
	defer h.Close()
	c := connect(t, server(t, h), "hA")
	defer c.close()
	deadline := time.After(2 * time.Second)
	for pings := 0; pings < 2; {
		select {
		case l := <-c.lines:
			if l == ": ping" {
				pings++
			}
		case <-deadline:
			t.Fatal("no heartbeat")
		}
	}
}

func TestSlowClientIsDroppedAndPublishNeverBlocks(t *testing.T) {
	h := New()
	defer h.Close()
	slow, ok := h.add("hA")
	if !ok {
		t.Fatal("add")
	}
	fast, _ := h.add("hA")
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range clientBuffer * 3 { // nobody reads slow.ch
			h.Publish(Message{HouseholdID: "hA", Type: "item", ID: "x", Kind: "edited"})
			for len(fast.ch) > 0 {
				<-fast.ch
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on a slow client")
	}
	if h.Clients() != 1 {
		t.Fatalf("%d clients, want only the fast one", h.Clients())
	}
	n := 0
	for range slow.ch { // closed by the hub after its buffer
		n++
	}
	if n != clientBuffer {
		t.Fatalf("slow client had %d buffered frames", n)
	}
}

func TestConnectDisconnectCyclesLeakNothing(t *testing.T) {
	h := New()
	srv := server(t, h)
	// Settle the server's own goroutines first.
	c := connect(t, srv, "hA")
	c.close()
	waitFor(t, "first disconnect", func() bool { return h.Clients() == 0 })
	before := runtime.NumGoroutine()

	var wg sync.WaitGroup
	for w := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				c := connect(t, srv, "hA")
				if w%2 == 0 {
					h.Publish(Message{HouseholdID: "hA", Type: "item", ID: "i", Kind: "edited"})
				}
				c.close()
			}
		}()
	}
	wg.Wait()
	waitFor(t, "all disconnected", func() bool { return h.Clients() == 0 })
	srv.CloseClientConnections()
	waitFor(t, "goroutines to exit", func() bool { return runtime.NumGoroutine() <= before+3 })

	// Shutdown: open streams end by themselves, new ones are refused.
	open := []*conn{connect(t, srv, "hA"), connect(t, srv, "hB")}
	waitFor(t, "2 clients", func() bool { return h.Clients() == 2 })
	h.Close()
	for _, c := range open {
		select {
		case _, ok := <-c.lines:
			for ok {
				_, ok = <-c.lines
			}
		case <-time.After(3 * time.Second):
			t.Fatal("stream still open after Close")
		}
		c.close()
	}
	if h.Clients() != 0 {
		t.Fatal("clients after Close")
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/stream", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Household", "hA")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode < 500 {
		t.Fatalf("after Close: %d", res.StatusCode)
	}
	h.Close() // twice is fine
}

// End to end with the inventory: a committed write reaches the household's
// stream, a rolled-back one reaches nobody.
func TestInventoryChangesPublishAfterCommit(t *testing.T) {
	d := dbtest.New(t)
	const stamp = "2026-01-01T00:00:00.000Z"
	mk := func(hid string) {
		if _, err := sqlc.New(d.Write).CreateHousehold(context.Background(), sqlc.CreateHouseholdParams{ID: hid, Name: hid, Language: "en", Currency: "EUR", CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
			t.Fatal(err)
		}
	}
	hA, hB := "01J00000000000000000000A00", "01J00000000000000000000B00"
	mk(hA)
	mk(hB)
	h := New()
	defer h.Close()
	defer h.AttachInventory()()
	srv := server(t, h)
	a, b := connect(t, srv, hA), connect(t, srv, hB)
	defer a.close()
	defer b.close()
	waitFor(t, "2 clients", func() bool { return h.Clients() == 2 })

	ctx := context.Background()
	// Rolled back: nothing.
	_ = inventory.InTx(ctx, d, func(tx *sql.Tx) error {
		if _, err := inventory.CreatePlace(ctx, tx, hA, inventory.PlaceInput{Kind: "box", Name: "Box 1"}); err != nil {
			return err
		}
		return context.Canceled
	})
	a.expectSilence(t)
	b.expectSilence(t)

	var boxID string
	if err := inventory.InTx(ctx, d, func(tx *sql.Tx) error {
		p, err := inventory.CreatePlace(ctx, tx, hA, inventory.PlaceInput{Kind: "box", Name: "Box 2"})
		boxID = p.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ev, m := a.next(t, 2*time.Second)
	if ev != "event" || m.Type != "box" || m.ID != boxID || m.Kind != "created" {
		t.Fatalf("got %q %+v", ev, m)
	}
	b.expectSilence(t)
}
