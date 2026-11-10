// Package dbtest gives tests a fresh, fully migrated database.
package dbtest

import (
	"context"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/db"
)

// New opens a new temp-file database with all migrations applied and closes
// it when the test ends.
func New(t testing.TB) *db.DB {
	t.Helper()
	d, err := db.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}
