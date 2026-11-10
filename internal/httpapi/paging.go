package httpapi

import (
	"net/http"
	"strconv"
)

// Paging limits for list endpoints.
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// Page is the response shape of every list endpoint (spec §5):
// {"data":[…],"next_cursor":"…"|null}. NextCursor is nil, and encodes as
// null, when there are no more items.
type Page[T any] struct {
	Data       []T     `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

// NewPage builds a Page, making sure data encodes as [] and not null. An
// empty next means there is no next page.
func NewPage[T any](items []T, next string) Page[T] {
	if items == nil {
		items = []T{}
	}
	p := Page[T]{Data: items}
	if next != "" {
		p.NextCursor = &next
	}
	return p
}

// ParsePaging reads ?cursor=&limit=. The cursor is opaque to this package.
// A bad limit gives a bad_request error with fields.limit set.
func ParsePaging(r *http.Request) (cursor string, limit int, err error) {
	q := r.URL.Query()
	cursor, limit = q.Get("cursor"), DefaultLimit
	if s := q.Get("limit"); s != "" {
		n, convErr := strconv.Atoi(s)
		if convErr != nil || n < 1 || n > MaxLimit {
			return "", 0, Invalid(map[string]string{"limit": "Must be a number from 1 to " + strconv.Itoa(MaxLimit) + "."})
		}
		limit = n
	}
	return cursor, limit, nil
}
