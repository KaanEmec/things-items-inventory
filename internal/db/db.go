// Package db opens the SQLite database and applies embedded migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB holds one write connection and a pool of read connections.
type DB struct {
	Write *sql.DB // exactly one connection; all writes go through it
	Read  *sql.DB // read-only pool
}

// Open opens <dataDir>/app.db (creating the directory and file) and applies
// pending migrations. It fails if the database has migrations this binary
// does not know.
func Open(ctx context.Context, dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(filepath.Join(dataDir, "app.db"))
	if err != nil {
		return nil, err
	}
	pragmas := []string{
		"journal_mode(WAL)", "foreign_keys(ON)", "busy_timeout(5000)", "synchronous(NORMAL)",
	}
	dsn := func(extra ...string) string {
		q := url.Values{}
		for _, p := range append(append([]string{}, pragmas...), extra...) {
			q.Add("_pragma", p)
		}
		return (&url.URL{Scheme: "file", Path: path, RawQuery: q.Encode()}).String()
	}

	w, err := sql.Open("sqlite", dsn())
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	r, err := sql.Open("sqlite", dsn("query_only(ON)"))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(8)
	d := &DB{Write: w, Read: r}
	if err := d.migrate(ctx); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// Close closes both handles.
func (d *DB) Close() error {
	return errors.Join(d.Read.Close(), d.Write.Close())
}

// Tx runs fn in a transaction on the write connection, committing if fn
// returns nil and rolling back otherwise.
func (d *DB) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range entries {
		name := e.Name()
		num, _, ok := strings.Cut(strings.TrimSuffix(name, ".sql"), "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || !strings.HasSuffix(name, ".sql") {
			return nil, fmt.Errorf("bad migration file name %q (want NNNN_name.sql)", name)
		}
		b, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		ms = append(ms, migration{v, name, string(b)})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	for i := 1; i < len(ms); i++ {
		if ms[i].version == ms[i-1].version {
			return nil, fmt.Errorf("duplicate migration version %d", ms[i].version)
		}
	}
	return ms, nil
}

func (d *DB) migrate(ctx context.Context) error {
	ms, err := loadMigrations()
	if err != nil {
		return err
	}
	if _, err := d.Write.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	rows, err := d.Write.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	applied := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	known := map[int]bool{}
	for _, m := range ms {
		known[m.version] = true
	}
	for v := range applied {
		if !known[v] {
			return fmt.Errorf("database is newer than this binary: it has migration %d, which this version does not know; upgrade the application", v)
		}
	}
	for _, m := range ms {
		if applied[m.version] {
			continue
		}
		err := d.Tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.sql); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
				m.version, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}
