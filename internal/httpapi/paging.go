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

// Page is the response shape of every list endpoint. NextCursor is empty
// when there are no more items.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

// NewPage builds a Page, making sure items encodes as [] and not null.
func NewPage[T any](items []T, nextCursor string) Page[T] {
	if items == nil {
		items = []T{}
	}
	return Page[T]{Items: items, NextCursor: nextCursor}
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
