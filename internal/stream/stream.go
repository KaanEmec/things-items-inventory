// Package stream is the live event stream (J3.6): a small in-process hub and
// the GET /api/v1/stream handler that sends server-sent events to the
// browsers of one household.
//
// Messages are "event" (an item, place or box changed) and "job" (a
// background job changed state), each with a JSON body {type, id, kind}:
//
//	event: event
//	data: {"type":"item","id":"01J...","kind":"moved"}
//
//	event: job
//	data: {"type":"job","id":"01J...","kind":"running","job_kind":"draft_item","item_id":"01J..."}
//
// The browser reacts by refetching what it shows. Nothing here is a source
// of truth: a missed message costs a stale screen until the next one, which
// is why slow clients are dropped (they reconnect) rather than waited for.
package stream

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
)

// Message is one thing to tell a household's browsers.
type Message struct {
	HouseholdID string `json:"-"`
	Event       string `json:"-"`    // SSE event name: "event" or "job"
	Type        string `json:"type"` // item | place | box | job
	ID          string `json:"id"`
	Kind        string `json:"kind"`               // event kind, or job state
	JobKind     string `json:"job_kind,omitempty"` // job messages only
	ItemID      string `json:"item_id,omitempty"`  // job messages only: the subject
	PlaceID     string `json:"place_id,omitempty"` // job messages only
}

const (
	clientBuffer      = 64
	defaultHeartbeat  = 25 * time.Second
	defaultWriteLimit = 10 * time.Second
)

type client struct {
	household string
	ch        chan []byte // closed by the hub (under its lock) when the client is dropped
}

// Hub fans messages out to connected clients. The zero value is not usable;
// call New.
type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
	closed  bool
	done    chan struct{}

	// Heartbeat and WriteTimeout can be shortened in tests.
	Heartbeat    time.Duration
	WriteTimeout time.Duration
}

// New returns a running hub. Close it at shutdown.
func New() *Hub {
	return &Hub{clients: map[*client]struct{}{}, done: make(chan struct{}),
		Heartbeat: defaultHeartbeat, WriteTimeout: defaultWriteLimit}
}

// Close ends every stream and refuses new ones. Call it before shutting the
// HTTP server down, which otherwise waits for the open streams.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	close(h.done)
	for c := range h.clients {
		delete(h.clients, c)
		close(c.ch)
	}
}

// Clients reports how many streams are open (tests and metrics).
func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) add(household string) (*client, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, false
	}
	c := &client{household: household, ch: make(chan []byte, clientBuffer)}
	h.clients[c] = struct{}{}
	return c, true
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.ch)
	}
}

// Publish sends m to the clients of m.HouseholdID, and only to them. It never
// blocks: a client whose buffer is full is dropped.
func (h *Hub) Publish(m Message) {
	if m.HouseholdID == "" {
		return
	}
	if m.Event == "" {
		m.Event = "event"
	}
	body, err := json.Marshal(m)
	if err != nil {
		return
	}
	frame := []byte("event: " + m.Event + "\ndata: " + string(body) + "\n\n")
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.household != m.HouseholdID {
			continue
		}
		select {
		case c.ch <- frame:
		default: // too slow: drop it, the browser reconnects and refetches
			delete(h.clients, c)
			close(c.ch)
		}
	}
}

// AttachInventory publishes every committed inventory change. It returns the
// unsubscribe function.
func (h *Hub) AttachInventory() (unsubscribe func()) {
	return inventory.OnCommit(func(n inventory.Notice) {
		h.Publish(Message{HouseholdID: n.HouseholdID, Event: "event", Type: n.Type, ID: n.ID, Kind: n.Kind})
	})
}

// OnJob is for jobs.Options.OnChange: it publishes a job's state change.
func (h *Hub) OnJob(j jobs.Job) {
	h.Publish(Message{HouseholdID: j.HouseholdID, Event: "job", Type: "job", ID: j.ID, Kind: j.State,
		JobKind: j.Kind, ItemID: j.ItemID, PlaceID: j.PlaceID})
}

// Routes registers GET /api/v1/stream (any member, session cookie only).
func (h *Hub) Routes(mux *httpapi.Mux) {
	mux.HandleFunc("GET /api/v1/stream", auth.RequireSession(auth.ActionInventoryRead, h.serve))
}

func (h *Hub) serve(w http.ResponseWriter, r *http.Request) {
	// Only event-stream clients: a plain GET would hang on a stream that
	// never ends.
	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeBadRequest, "This endpoint is an event stream; send Accept: text/event-stream."))
		return
	}
	rc := http.NewResponseController(w)
	c, ok := h.add(auth.HouseholdID(r.Context()))
	if !ok {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeInternal, "The server is shutting down."))
		return
	}
	defer h.remove(c)

	hd := w.Header()
	hd.Set("Content-Type", "text/event-stream")
	hd.Set("Cache-Control", "no-cache, no-transform")
	hd.Set("Connection", "keep-alive")
	hd.Set("X-Accel-Buffering", "no")      // reverse proxies: do not buffer
	hd.Set("Content-Encoding", "identity") // the gzip middleware leaves it alone
	// The server's write timeout would end a stream that lives for hours;
	// each write sets its own deadline instead.
	write := func(b []byte) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(h.WriteTimeout))
		if _, err := w.Write(b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	w.WriteHeader(http.StatusOK)
	if !write([]byte("retry: 3000\n: connected\n\n")) {
		return
	}
	tick := time.NewTicker(h.Heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.done:
			return
		case frame, open := <-c.ch:
			if !open { // dropped or hub closed
				return
			}
			if !write(frame) {
				return
			}
		case <-tick.C:
			if !write([]byte(": ping\n\n")) {
				return
			}
		}
	}
}
