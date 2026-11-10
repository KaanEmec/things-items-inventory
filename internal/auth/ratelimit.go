package auth

import (
	"sync"
	"time"
)

// Login rate limits (fixed windows, in memory, reset on restart). Only
// failed attempts count; a successful login clears that email's counter.
//
//	per email:           10 failures per 15 minutes
//	per client address:  30 failures per 15 minutes
//
// The windows start at the first failure and the counter resets when the
// window ends. Both limits are checked before any password is hashed.
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

// retryAfter returns how long the caller must wait, or 0 if an attempt may
// go ahead.
func (l *limiter) retryAfter(email, client string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.gc(now)
	var wait time.Duration
	for _, c := range []struct {
		m   map[string]*window
		key string
		max int
	}{{l.email, email, loginMaxPerEmail}, {l.client, client, loginMaxPerClient}} {
		if w := c.m[c.key]; w != nil && now.Sub(w.start) < loginWindow && w.n >= c.max {
			wait = max(wait, loginWindow-now.Sub(w.start))
		}
	}
	return wait
}

func (l *limiter) fail(email, client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for _, c := range []struct {
		m   map[string]*window
		key string
	}{{l.email, email}, {l.client, client}} {
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
}

func (l *limiter) success(email string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.email, email)
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
