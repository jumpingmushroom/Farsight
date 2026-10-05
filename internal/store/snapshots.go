package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"time"
)

// PutSnapshot stores blob (gzip-compressed) as the save with the given
// serverID and saveID, along with the times the save was written
// (savedAt) and read from disk (readAt). It is idempotent on
// (serverID, saveID): a repeat call with the same key is a no-op and
// reports inserted=false.
func (s *Store) PutSnapshot(ctx context.Context, serverID, saveID string, savedAt, readAt time.Time, blob []byte) (inserted bool, err error) {
	compressed, err := gzipCompress(blob)
	if err != nil {
		return false, fmt.Errorf("store: compress snapshot: %w", err)
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO snapshots (server_id, save_id, saved_at, read_at, received_at, blob)
		VALUES (?, ?, ?, ?, ?, ?)`,
		serverID, saveID, millis(savedAt), millis(readAt), millis(time.Now()), compressed)
	if err != nil {
		return false, fmt.Errorf("store: put snapshot: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// LatestSnapshot returns the most recently saved snapshot (by savedAt)
// for serverID, decompressed. ok is false if the server has no
// snapshots.
func (s *Store) LatestSnapshot(ctx context.Context, serverID string) (blob []byte, savedAt time.Time, ok bool, err error) {
	var compressed []byte
	var savedAtMs int64
	err = s.db.QueryRowContext(ctx, `
		SELECT blob, saved_at FROM snapshots
		WHERE server_id = ?
		ORDER BY saved_at DESC, save_id DESC
		LIMIT 1`, serverID).Scan(&compressed, &savedAtMs)
	switch {
	case err == sql.ErrNoRows:
		return nil, time.Time{}, false, nil
	case err != nil:
		return nil, time.Time{}, false, fmt.Errorf("store: latest snapshot: %w", err)
	}

	blob, err = gzipDecompress(compressed)
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("store: decompress snapshot: %w", err)
	}
	return blob, fromMillis(savedAtMs), true, nil
}

// LatestSnapshotID returns the save id of the snapshot LatestSnapshot
// would return, without reading its blob, so a caller can cheaply tell
// whether a decoded copy it holds is still current. ok is false if the
// server has no snapshots.
func (s *Store) LatestSnapshotID(ctx context.Context, serverID string) (saveID string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT save_id FROM snapshots
		WHERE server_id = ?
		ORDER BY saved_at DESC, save_id DESC
		LIMIT 1`, serverID).Scan(&saveID)
	switch {
	case err == sql.ErrNoRows:
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("store: latest snapshot id: %w", err)
	}
	return saveID, true, nil
}

// PruneSnapshots deletes snapshots saved before the given time, except
// that it never deletes a snapshot the world-save event deriver still
// needs: for a server with no world_diff row, nothing is pruned (it
// hasn't diffed any snapshot yet); otherwise every snapshot at or after
// the one world_diff points to is kept regardless of age, and the age
// limit applies only to the snapshots strictly before it. It returns the
// number of rows deleted.
func (s *Store) PruneSnapshots(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM snapshots
		WHERE saved_at < ?
		AND EXISTS (
			SELECT 1 FROM world_diff wd
			WHERE wd.server_id = snapshots.server_id
			AND (snapshots.saved_at < wd.saved_at
				OR (snapshots.saved_at = wd.saved_at AND snapshots.save_id < wd.save_id))
		)`, millis(before))
	if err != nil {
		return 0, fmt.Errorf("store: prune snapshots: %w", err)
	}
	return res.RowsAffected()
}

func gzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gzipDecompress(data []byte) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	return io.ReadAll(gr)
}
