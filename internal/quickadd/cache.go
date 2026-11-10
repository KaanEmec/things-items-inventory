package quickadd

import (
	"sync"
	"time"
)

// entry is the cells and confidence suggested for one normalised input.
type entry struct {
	cells      map[string]string
	confidence map[string]string
	at         time.Time
}

const (
	cacheTTL           = time.Hour
	cacheMaxPerHouse   = 500
	cacheMaxHouseholds = 256
)

// cache keeps answers per household, bounded in age and size, so the same
// text is never sent to the AI twice. Households never share entries.
type cache struct {
	mu  sync.Mutex
	now func() time.Time
	by  map[string]map[string]*entry
}

func newCache() *cache { return &cache{now: time.Now, by: map[string]map[string]*entry{}} }

func (c *cache) get(hid, key string) (*entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.by[hid][key]
	if !ok {
		return nil, false
	}
	if c.now().Sub(e.at) > cacheTTL {
		delete(c.by[hid], key)
		return nil, false
	}
	return e, true
}

func (c *cache) put(hid, key string, e *entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	e.at = now
	m := c.by[hid]
	if m == nil {
		if len(c.by) >= cacheMaxHouseholds {
			c.dropHousehold(now)
		}
		m = map[string]*entry{}
		c.by[hid] = m
	}
	m[key] = e
	if len(m) > cacheMaxPerHouse {
		oldestKey, oldest := "", now
		for k, v := range m {
			if now.Sub(v.at) > cacheTTL {
				delete(m, k)
			} else if v.at.Before(oldest) || oldestKey == "" {
				oldestKey, oldest = k, v.at
			}
		}
		if len(m) > cacheMaxPerHouse {
			delete(m, oldestKey)
		}
	}
}

// dropHousehold frees a slot: households whose entries all expired first,
// otherwise the one with the oldest newest entry.
func (c *cache) dropHousehold(now time.Time) {
	victim, victimAt := "", now.Add(time.Second)
	for hid, m := range c.by {
		var newest time.Time
		for _, v := range m {
			if v.at.After(newest) {
				newest = v.at
			}
		}
		if now.Sub(newest) > cacheTTL {
			delete(c.by, hid)
			return
		}
		if newest.Before(victimAt) {
			victim, victimAt = hid, newest
		}
	}
	delete(c.by, victim)
}

// limiter allows n calls per window per household.
type limiter struct {
	mu     sync.Mutex
	n      int
	window time.Duration
	now    func() time.Time
	hits   map[string][]time.Time
}

func newLimiter(n int, window time.Duration) *limiter {
	return &limiter{n: n, window: window, now: time.Now, hits: map[string][]time.Time{}}
}

// allow records a call and reports true, or reports how long to wait.
func (l *limiter) allow(hid string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	h := l.hits[hid]
	i := 0
	for i < len(h) && now.Sub(h[i]) >= l.window {
		i++
	}
	h = h[i:]
	if len(h) >= l.n {
		l.hits[hid] = h
		return l.window - now.Sub(h[0]), false
	}
	l.hits[hid] = append(h, now)
	if len(l.hits) > 1024 { // forget households that went quiet
		for k, v := range l.hits {
			if len(v) == 0 || now.Sub(v[len(v)-1]) >= l.window {
				delete(l.hits, k)
			}
		}
	}
	return 0, true
}
