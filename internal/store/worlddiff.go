package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SnapshotKey names one stored snapshot: its save id and save time, the
// order snapshots are replayed in.
type SnapshotKey struct {
	SaveID  string
	SavedAt time.Time
}

// WorldDiffState returns the last snapshot of serverID whose differences
// from the one before it went into the event log. ok is false if world
// events have never been derived for it (the backfill hasn't run). tx may
// be nil.
func (s *Store) WorldDiffState(ctx context.Context, tx *sql.Tx, serverID string) (SnapshotKey, bool, error) {
	var st SnapshotKey
	var savedAt int64
	err := s.conn(tx).QueryRowContext(ctx, `
		SELECT save_id, saved_at FROM world_diff WHERE server_id = ?`, serverID).Scan(&st.SaveID, &savedAt)
	switch {
	case err == sql.ErrNoRows:
		return SnapshotKey{}, false, nil
	case err != nil:
		return SnapshotKey{}, false, fmt.Errorf("store: world diff state: %w", err)
	}
	st.SavedAt = fromMillis(savedAt)
	return st, true, nil
}

// PutWorldDiffState records st as serverID's last diffed snapshot. tx may
// be nil.
func (s *Store) PutWorldDiffState(ctx context.Context, tx *sql.Tx, serverID string, st SnapshotKey) error {
	_, err := s.conn(tx).ExecContext(ctx, `
		INSERT INTO world_diff (server_id, save_id, saved_at) VALUES (?, ?, ?)
		ON CONFLICT (server_id) DO UPDATE SET save_id = excluded.save_id, saved_at = excluded.saved_at`,
		serverID, st.SaveID, millis(st.SavedAt))
	if err != nil {
		return fmt.Errorf("store: put world diff state: %w", err)
	}
	return nil
}

// SnapshotsAfter returns the keys (no blobs) of serverID's snapshots that
// sort after `after` by (saved_at, save_id), oldest first. The zero
// SnapshotKey returns them all.
func (s *Store) SnapshotsAfter(ctx context.Context, serverID string, after SnapshotKey) ([]SnapshotKey, error) {
	ms := int64(-1 << 62)
	if !after.SavedAt.IsZero() {
		ms = millis(after.SavedAt)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT save_id, saved_at FROM snapshots
		WHERE server_id = ? AND (saved_at > ? OR (saved_at = ? AND save_id > ?))
		ORDER BY saved_at ASC, save_id ASC`, serverID, ms, ms, after.SaveID)
	if err != nil {
		return nil, fmt.Errorf("store: snapshots after: %w", err)
	}
	defer rows.Close()
	var out []SnapshotKey
	for rows.Next() {
		var st SnapshotKey
		var savedAt int64
		if err := rows.Scan(&st.SaveID, &savedAt); err != nil {
			return nil, err
		}
		st.SavedAt = fromMillis(savedAt)
		out = append(out, st)
	}
	return out, rows.Err()
}

// Snapshot returns one stored snapshot, decompressed. ok is false if it
// isn't stored (never was, or pruned).
func (s *Store) Snapshot(ctx context.Context, serverID, saveID string) (blob []byte, ok bool, err error) {
	var compressed []byte
	err = s.db.QueryRowContext(ctx, `
		SELECT blob FROM snapshots WHERE server_id = ? AND save_id = ?`, serverID, saveID).Scan(&compressed)
	switch {
	case err == sql.ErrNoRows:
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("store: snapshot: %w", err)
	}
	blob, err = gzipDecompress(compressed)
	if err != nil {
		return nil, false, fmt.Errorf("store: decompress snapshot: %w", err)
	}
	return blob, true, nil
}

// Tombstone is one distinct tombstone seen in a server's saves.
type Tombstone struct {
	ID        string
	Owner     string
	X, Z      float32
	FirstSeen time.Time // the save time it first appeared in
}

// InsertTombstone records t unless (serverID, t.ID) exists, reporting
// whether it was inserted. tx may be nil.
func (s *Store) InsertTombstone(ctx context.Context, tx *sql.Tx, serverID string, t Tombstone) (bool, error) {
	res, err := s.conn(tx).ExecContext(ctx, `
		INSERT OR IGNORE INTO tombstones (server_id, id, owner, x, z, first_seen)
		VALUES (?, ?, ?, ?, ?, ?)`,
		serverID, t.ID, t.Owner, t.X, t.Z, millis(t.FirstSeen))
	if err != nil {
		return false, fmt.Errorf("store: insert tombstone: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Tombstones returns owner's distinct tombstones on serverID, oldest first.
func (s *Store) Tombstones(ctx context.Context, serverID, owner string) ([]Tombstone, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner, x, z, first_seen FROM tombstones
		WHERE server_id = ? AND owner = ?
		ORDER BY first_seen ASC, id ASC`, serverID, owner)
	if err != nil {
		return nil, fmt.Errorf("store: tombstones: %w", err)
	}
	defer rows.Close()
	var out []Tombstone
	for rows.Next() {
		var t Tombstone
		var first int64
		if err := rows.Scan(&t.ID, &t.Owner, &t.X, &t.Z, &first); err != nil {
			return nil, err
		}
		t.FirstSeen = fromMillis(first)
		out = append(out, t)
	}
	return out, rows.Err()
}
