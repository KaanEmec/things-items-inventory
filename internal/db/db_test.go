package db_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

var ctx = context.Background()

func count(t *testing.T, d *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestFreshAndReopen(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	first := count(t, d.Read, `SELECT count(*) FROM schema_migrations`)
	if first == 0 {
		t.Fatalf("migrations applied: %d", first)
	}
	var at string
	d.Read.QueryRow(`SELECT applied_at FROM schema_migrations WHERE version=1`).Scan(&at)
	for _, tbl := range []string{"households", "users", "memberships", "invites", "sessions"} {
		count(t, d.Read, `SELECT count(*) FROM `+tbl)
	}
	d.Close()

	d, err = db.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var at2 string
	d.Read.QueryRow(`SELECT applied_at FROM schema_migrations WHERE version=1`).Scan(&at2)
	if n := count(t, d.Read, `SELECT count(*) FROM schema_migrations`); n != first || at != at2 {
		t.Fatalf("second open changed migrations: n=%d %q vs %q", n, at, at2)
	}
	var mode string
	d.Read.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	if mode != "wal" {
		t.Fatalf("journal_mode %q", mode)
	}
}

func TestNewerDatabase(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Write.Exec(`INSERT INTO schema_migrations VALUES (9999, 'x')`); err != nil {
		t.Fatal(err)
	}
	d.Close()
	_, err = db.Open(ctx, dir)
	if err == nil || !strings.Contains(err.Error(), "database is newer than this binary") {
		t.Fatalf("got %v", err)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	d := dbtest.New(t)
	_, err := d.Write.Exec(`INSERT INTO memberships (household_id, user_id, role, created_at) VALUES ('nope','nope','owner','x')`)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("got %v", err)
	}
}

func TestWriterSingleConnection(t *testing.T) {
	d := dbtest.New(t)
	if n := d.Write.Stats().MaxOpenConnections; n != 1 {
		t.Fatalf("max open %d", n)
	}
	if _, err := d.Read.Exec(`CREATE TABLE x (a)`); err == nil {
		t.Fatal("read pool accepted a write")
	}
}

func TestQueriesWithDBAndTx(t *testing.T) {
	d := dbtest.New(t)
	now := "2026-11-10T09:30:00.000Z"
	q := sqlc.New(d.Write)
	h, err := q.CreateHousehold(ctx, sqlc.CreateHouseholdParams{ID: id.New(), Name: "Home", Language: "en", Currency: "EUR", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if h.DefaultWarrantyMonths != 24 || h.NextBoxNumber != 1 {
		t.Fatalf("defaults: %+v", h)
	}
	err = d.Tx(ctx, func(tx *sql.Tx) error {
		_, err := q.WithTx(tx).CreateUser(ctx, sqlc.CreateUserParams{ID: id.New(), Name: "K", Email: "k@example.com", PasswordHash: "h", CreatedAt: now, UpdatedAt: now})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlc.New(d.Read).GetUserByEmail(ctx, "k@example.com"); err != nil {
		t.Fatal(err)
	}
	if got, err := sqlc.New(d.Read).GetHousehold(ctx, h.ID); err != nil || got.Name != "Home" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestFTS5Trigram(t *testing.T) {
	d := dbtest.New(t)
	for _, s := range []string{
		`CREATE VIRTUAL TABLE t USING fts5(household_id UNINDEXED, title, body, tokenize='trigram')`,
		`INSERT INTO t VALUES ('h1', 'usb-c charger', 'anker nano 65w')`,
		`INSERT INTO t VALUES ('h1', 'winter jacket', 'blue')`,
	} {
		if _, err := d.Write.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	var title string
	if err := d.Write.QueryRow(`SELECT title FROM t WHERE t MATCH '"harg"'`).Scan(&title); err != nil || title != "usb-c charger" {
		t.Fatalf("substring match: %q %v", title, err)
	}
	if err := d.Write.QueryRow(`SELECT title FROM t WHERE t MATCH 'title:"jack" AND household_id:"h1"' `).Scan(&title); err != nil {
		// household_id is UNINDEXED so a column filter on it is not expected to work; plain WHERE is
		if err := d.Write.QueryRow(`SELECT title FROM t WHERE t MATCH '"jack"' AND household_id = 'h1'`).Scan(&title); err != nil || title != "winter jacket" {
			t.Fatalf("%q %v", title, err)
		}
	}
}

func TestMembershipInsert(t *testing.T) {
	d := dbtest.New(t)
	now := "2026-11-10T09:30:00.000Z"
	q := sqlc.New(d.Write)
	h, _ := q.CreateHousehold(ctx, sqlc.CreateHouseholdParams{ID: id.New(), Name: "H", Language: "en", Currency: "EUR", CreatedAt: now, UpdatedAt: now})
	u, _ := q.CreateUser(ctx, sqlc.CreateUserParams{ID: id.New(), Name: "K", Email: "k@x", PasswordHash: "h", CreatedAt: now, UpdatedAt: now})
	if _, err := d.Write.Exec(`INSERT INTO memberships (household_id, user_id, role, created_at) VALUES (?,?,?,?)`, h.ID, u.ID, "owner", now); err != nil {
		t.Fatal(err)
	}
}
