// Package store implements the SQLite-backed persistence used by the
// farsight central backend: world snapshots, the agent event dedupe log
// and activity feed, player sessions, and live per-server state.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database with the farsight schema applied.
type Store struct {
	db *sql.DB
}

// execer is satisfied by both *sql.DB and *sql.Tx, so store methods can
// run against either.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// conn returns tx if non-nil, otherwise the Store's own *sql.DB, so every
// method that accepts a *sql.Tx also works when tx is nil.
func (s *Store) conn(tx *sql.Tx) execer {
	if tx != nil {
		return tx
	}
	return s.db
}

var memCounter int64

// Open opens (and, if needed, creates and migrates) the farsight
// database at path. An empty path gives a fresh, isolated in-memory
// database — safe for concurrent use within the returned *Store, and
// independent of any other in-memory Store — which is intended for
// tests. A non-empty path opens (or creates) an on-disk database in WAL
// mode with a 5s busy timeout and foreign keys enabled.
//
// Both kinds begin every transaction IMMEDIATE (_txlock), taking the
// write lock up front: Tx callers read before they write, and a DEFERRED
// transaction that has read can't be upgraded once another connection
// has committed (SQLITE_BUSY/BUSY_SNAPSHOT, which busy_timeout does not
// retry).
func Open(path string) (*Store, error) {
	var dsn string
	if path == "" {
		n := atomic.AddInt64(&memCounter, 1)
		dsn = fmt.Sprintf("file:farsight_mem_%d_%d?mode=memory&cache=shared&_foreign_keys=1&_busy_timeout=5000&_txlock=immediate", os.Getpid(), n)
	} else {
		dsn = fileDSN(path, "_journal_mode=WAL&_foreign_keys=1&_busy_timeout=5000&_txlock=immediate")
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	// A cache=shared in-memory database only stays alive while at least
	// one connection to it is open; keep one idle so the pool doesn't
	// drop it between queries.
	db.SetMaxIdleConns(1)

	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return s, nil
}

// fileDSN returns a "file:" URI DSN for the on-disk database at path with
// the given (already encoded) query. Each path segment is percent-escaped
// so that characters such as '?', '#', '%' and spaces name the file
// rather than starting the query or fragment or being decoded by SQLite's
// URI parser. An absolute path becomes file:///abs/path; a relative one
// stays relative (to the working directory) as file:rel/path.
func fileDSN(path, rawQuery string) string {
	segs := strings.Split(filepath.ToSlash(path), "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	escaped := strings.Join(segs, "/")
	if strings.HasPrefix(escaped, "/") {
		escaped = "//" + escaped
	}
	return "file:" + escaped + "?" + rawQuery
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Tx runs fn inside a transaction, committing on success and rolling
// back if fn returns an error (or panics).
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback also failed: %v)", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// migrations holds one entry per schema version; each entry is the list
// of statements that take the database from version i to version i+1.
// Applying it is idempotent: every statement uses CREATE ... IF NOT
// EXISTS, so re-running migrate on an already-migrated database (e.g. on
// reopen) is a no-op beyond the schema_version bookkeeping.
var migrations = [][]string{
	{
		`CREATE TABLE IF NOT EXISTS snapshots (
			server_id   TEXT NOT NULL,
			save_id     TEXT NOT NULL,
			saved_at    INTEGER NOT NULL,
			read_at     INTEGER NOT NULL,
			received_at INTEGER NOT NULL,
			blob        BLOB NOT NULL,
			PRIMARY KEY (server_id, save_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_snapshots_server_saved ON snapshots (server_id, saved_at)`,

		`CREATE TABLE IF NOT EXISTS events (
			server_id TEXT NOT NULL,
			id        TEXT NOT NULL,
			type      TEXT NOT NULL,
			at        INTEGER NOT NULL,
			body      TEXT NOT NULL,
			PRIMARY KEY (server_id, id)
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS idx_events_server_at ON events (server_id, at)`,

		`CREATE TABLE IF NOT EXISTS sessions (
			server_id   TEXT NOT NULL,
			platform_id TEXT NOT NULL,
			name        TEXT NOT NULL,
			since       INTEGER NOT NULL,
			platform    TEXT NOT NULL,
			until       INTEGER,
			seconds     INTEGER NOT NULL DEFAULT 0,
			reason      TEXT NOT NULL DEFAULT '',
			UNIQUE (server_id, platform_id, name, since)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_server_until ON sessions (server_id, until)`,

		`CREATE TABLE IF NOT EXISTS live (
			server_id       TEXT PRIMARY KEY,
			status          TEXT NOT NULL DEFAULT '',
			version         TEXT NOT NULL DEFAULT '',
			join_code       TEXT NOT NULL DEFAULT '',
			network_version INTEGER NOT NULL DEFAULT 0,
			players         INTEGER NOT NULL DEFAULT 0,
			last_heartbeat  INTEGER,
			last_event_at   INTEGER,
			up_since        INTEGER,
			join_code_at    INTEGER,
			status_at       INTEGER
		)`,
	},
	// 2: world-save events (Plan 7). world_diff records, per server, the
	// last snapshot whose differences went into events (its presence means
	// the backfill has run); tombstones holds every distinct tombstone ever
	// seen in a save, with the save time it first appeared.
	{
		`CREATE TABLE IF NOT EXISTS world_diff (
			server_id TEXT PRIMARY KEY,
			save_id   TEXT NOT NULL,
			saved_at  INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tombstones (
			server_id  TEXT NOT NULL,
			id         TEXT NOT NULL,
			owner      TEXT NOT NULL,
			x          REAL NOT NULL,
			z          REAL NOT NULL,
			first_seen INTEGER NOT NULL,
			PRIMARY KEY (server_id, id)
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_server_platform ON sessions (server_id, platform_id)`,
	},
}

// migrate brings the database schema up to len(migrations), tracking the
// applied version in the schema_version table. Safe to call on every
// Open, including on an already-migrated database.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (v INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var v int
	err := s.db.QueryRowContext(ctx, `SELECT v FROM schema_version LIMIT 1`).Scan(&v)
	switch {
	case err == sql.ErrNoRows:
		v = 0
	case err != nil:
		return fmt.Errorf("read schema_version: %w", err)
	}

	if v >= len(migrations) {
		return nil
	}

	return s.Tx(ctx, func(tx *sql.Tx) error {
		for i := v; i < len(migrations); i++ {
			for _, stmt := range migrations[i] {
				if _, err := tx.ExecContext(ctx, stmt); err != nil {
					return fmt.Errorf("migration %d: %w", i+1, err)
				}
			}
		}
		if v == 0 {
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_version (v) VALUES (?)`, len(migrations))
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE schema_version SET v = ?`, len(migrations))
		return err
	})
}
