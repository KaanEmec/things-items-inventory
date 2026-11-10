package auth

import (
	"sync"
	"time"
)

// Login rate limits (fixed windows, in memory, reset on restart). Every
// attempt is counted when it starts, inside the lock, so parallel requests
// can not all slip under the limit; a successful login gives its attempt
// back, so only failures stay counted.
//
//	per email + client address: 10 attempts per 15 minutes
//	per client address:         30 attempts per 15 minutes
//
// The email limit is keyed on the address too, so a stranger can not lock
// the real owner out by guessing at their email from elsewhere. The windows
// start at the first counted attempt. Both limits are checked before any
// password is hashed. Other endpoints that check passwords or tokens use
// their own limiter instances.
const (
	loginWindow       = 15 * time.Minute
	loginMaxPerEmail  = 10
	loginMaxPerClient = 30
)

type window struct {
	start time.Time
	n     int
}

type limiter struct {
	mu      sync.Mutex
	now     func() time.Time
	email   map[string]*window
	client  map[string]*window
	lastGC  time.Time
	maxKeys int
}

func newLimiter(now func() time.Time) *limiter {
	return &limiter{now: now, email: map[string]*window{}, client: map[string]*window{}, maxKeys: 100_000}
}

func emailKey(email, client string) string { return email + "\x00" + client }

// begin reserves one attempt. It returns 0 when the attempt may go ahead
// (and is now counted), or how long the caller must wait.
func (l *limiter) begin(email, client string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.gc(now)
	ek := emailKey(email, client)
	var wait time.Duration
	for _, c := range []struct {
		m   map[string]*window
		key string
		max int
	}{{l.email, ek, loginMaxPerEmail}, {l.client, client, loginMaxPerClient}} {
		if w := c.m[c.key]; w != nil && now.Sub(w.start) < loginWindow && w.n >= c.max {
			wait = max(wait, loginWindow-now.Sub(w.start))
		}
	}
	if wait > 0 {
		return wait
	}
	for _, c := range []struct {
		m   map[string]*window
		key string
	}{{l.email, ek}, {l.client, client}} {
		w := c.m[c.key]
		if w == nil || now.Sub(w.start) >= loginWindow {
			if w == nil && len(c.m) >= l.maxKeys {
				continue // under a flood, stop remembering new keys rather than grow without bound
			}
			w = &window{start: now}
			c.m[c.key] = w
		}
		w.n++
	}
	return 0
}

// undo gives back the attempt reserved by begin (a successful login).
func (l *limiter) undo(email, client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range []struct {
		m   map[string]*window
		key string
	}{{l.email, emailKey(email, client)}, {l.client, client}} {
		if w := c.m[c.key]; w != nil && w.n > 0 {
			w.n--
		}
	}
}

func (l *limiter) gc(now time.Time) {
	if now.Sub(l.lastGC) < time.Minute {
		return
	}
	l.lastGC = now
	for _, m := range []map[string]*window{l.email, l.client} {
		for k, w := range m {
			if now.Sub(w.start) >= loginWindow {
				delete(m, k)
			}
		}
	}
}

// counter is a plain fixed-window request counter per key.
type counter struct {
	mu     sync.Mutex
	now    func() time.Time
	max    int
	window time.Duration
	m      map[string]*window
}

func newCounter(now func() time.Time, max int, dur time.Duration) *counter {
	return &counter{now: now, max: max, window: dur, m: map[string]*window{}}
}

// allow counts one request for key and reports whether it is within the limit.
func (c *counter) allow(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if len(c.m) > 10_000 {
		for k, w := range c.m {
			if now.Sub(w.start) >= c.window {
				delete(c.m, k)
			}
		}
	}
	w := c.m[key]
	if w == nil || now.Sub(w.start) >= c.window {
		w = &window{start: now}
		c.m[key] = w
	}
	w.n++
	return w.n <= c.max
}
