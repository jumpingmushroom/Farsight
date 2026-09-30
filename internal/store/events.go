package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jumpingmushroom/farsight/internal/logwatch"
)

// InsertEventIfNew records e in the server's event log, unless an event
// with the same (serverID, e.ID) already exists — the dedupe check an
// agent replay relies on. It reports whether the event was newly
// inserted. tx may be nil to run directly on the database.
func (s *Store) InsertEventIfNew(ctx context.Context, tx *sql.Tx, serverID string, e logwatch.Event) (bool, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return false, fmt.Errorf("store: marshal event: %w", err)
	}

	res, err := s.conn(tx).ExecContext(ctx, `
		INSERT OR IGNORE INTO events (server_id, id, type, at, body)
		VALUES (?, ?, ?, ?, ?)`,
		serverID, e.ID, e.Type, millis(e.At), body)
	if err != nil {
		return false, fmt.Errorf("store: insert event: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RecentActivity returns up to limit events for serverID, most recent
// first, excluding heartbeat and players_now events.
func (s *Store) RecentActivity(ctx context.Context, serverID string, limit int) ([]logwatch.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT body FROM events
		WHERE server_id = ? AND type NOT IN (?, ?)
		ORDER BY at DESC
		LIMIT ?`,
		serverID, logwatch.EvHeartbeat, logwatch.EvPlayersNow, limit)
	if err != nil {
		return nil, fmt.Errorf("store: recent activity: %w", err)
	}
	defer rows.Close()

	var out []logwatch.Event
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var e logwatch.Event
		if err := json.Unmarshal(body, &e); err != nil {
			return nil, fmt.Errorf("store: unmarshal event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SaveTimes returns up to limit world_saved event times for serverID,
// most recent first.
func (s *Store) SaveTimes(ctx context.Context, serverID string, limit int) ([]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT at FROM events
		WHERE server_id = ? AND type = ?
		ORDER BY at DESC
		LIMIT ?`, serverID, logwatch.EvWorldSaved, limit)
	if err != nil {
		return nil, fmt.Errorf("store: save times: %w", err)
	}
	defer rows.Close()

	var out []time.Time
	for rows.Next() {
		var at int64
		if err := rows.Scan(&at); err != nil {
			return nil, err
		}
		out = append(out, fromMillis(at))
	}
	return out, rows.Err()
}

// PruneEvents deletes events older than before, across all servers, and
// returns the number of rows deleted.
func (s *Store) PruneEvents(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE at < ?`, millis(before))
	if err != nil {
		return 0, fmt.Errorf("store: prune events: %w", err)
	}
	return res.RowsAffected()
}
