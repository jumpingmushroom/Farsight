package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jumpingmushroom/farsight/internal/logwatch"
)

// noiseTypes are event types that only drive live state: they are never
// listed as activity, and they are the only ones PruneEvents deletes.
var noiseTypes = []any{logwatch.EvHeartbeat, logwatch.EvPlayersNow}

// StoredEvent is one row of the event log, its JSON body undecoded: log
// events (logwatch.Event) and world-save events (worldevents.Event) share
// the table.
type StoredEvent struct {
	ID   string
	Type string
	At   time.Time
	Body []byte
}

// InsertEventIfNew records e in the server's event log, unless an event
// with the same (serverID, e.ID) already exists — the dedupe check an
// agent replay relies on. It reports whether the event was newly
// inserted. tx may be nil to run directly on the database.
func (s *Store) InsertEventIfNew(ctx context.Context, tx *sql.Tx, serverID string, e logwatch.Event) (bool, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return false, fmt.Errorf("store: marshal event: %w", err)
	}
	return s.InsertRawEvent(ctx, tx, serverID, StoredEvent{ID: e.ID, Type: e.Type, At: e.At, Body: body})
}

// InsertRawEvent records e (body already encoded) unless (serverID, e.ID)
// exists, reporting whether it was inserted. tx may be nil.
func (s *Store) InsertRawEvent(ctx context.Context, tx *sql.Tx, serverID string, e StoredEvent) (bool, error) {
	res, err := s.conn(tx).ExecContext(ctx, `
		INSERT OR IGNORE INTO events (server_id, id, type, at, body)
		VALUES (?, ?, ?, ?, ?)`,
		serverID, e.ID, e.Type, millis(e.At), e.Body)
	if err != nil {
		return false, fmt.Errorf("store: insert event: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RecentEvents returns up to limit events for serverID, most recent first
// (ties by id, descending), excluding heartbeat and players_now events.
func (s *Store) RecentEvents(ctx context.Context, serverID string, limit int) ([]StoredEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, at, body FROM events
		WHERE server_id = ? AND type NOT IN (?, ?)
		ORDER BY at DESC, id DESC
		LIMIT ?`,
		append(append([]any{serverID}, noiseTypes...), limit)...)
	if err != nil {
		return nil, fmt.Errorf("store: recent events: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// EventsBetween returns serverID's events with from <= at < until, most
// recent first (ties by id, descending), excluding heartbeat and
// players_now events.
func (s *Store) EventsBetween(ctx context.Context, serverID string, from, until time.Time) ([]StoredEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, at, body FROM events
		WHERE server_id = ? AND type NOT IN (?, ?) AND at >= ? AND at < ?
		ORDER BY at DESC, id DESC`,
		append(append([]any{serverID}, noiseTypes...), millis(from), millis(until))...)
	if err != nil {
		return nil, fmt.Errorf("store: events between: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// EarliestEvent returns the time of serverID's oldest event other than
// heartbeat and players_now: when tracking began for it. ok is false if
// there is none.
func (s *Store) EarliestEvent(ctx context.Context, serverID string) (at time.Time, ok bool, err error) {
	var v int64
	err = s.db.QueryRowContext(ctx, `
		SELECT at FROM events WHERE server_id = ? AND type NOT IN (?, ?)
		ORDER BY at ASC LIMIT 1`,
		append([]any{serverID}, noiseTypes...)...).Scan(&v)
	switch {
	case err == sql.ErrNoRows:
		return time.Time{}, false, nil
	case err != nil:
		return time.Time{}, false, fmt.Errorf("store: earliest event: %w", err)
	}
	return fromMillis(v), true, nil
}

// LatestEventOfType returns serverID's newest event of type typ (ties by
// id, descending). ok is false if there is none.
func (s *Store) LatestEventOfType(ctx context.Context, serverID, typ string) (StoredEvent, bool, error) {
	var e StoredEvent
	var at int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, type, at, body FROM events
		WHERE server_id = ? AND type = ?
		ORDER BY at DESC, id DESC
		LIMIT 1`, serverID, typ).Scan(&e.ID, &e.Type, &at, &e.Body)
	switch {
	case err == sql.ErrNoRows:
		return StoredEvent{}, false, nil
	case err != nil:
		return StoredEvent{}, false, fmt.Errorf("store: latest %s event: %w", typ, err)
	}
	e.At = fromMillis(at)
	return e, true, nil
}

func scanEvents(rows *sql.Rows) ([]StoredEvent, error) {
	var out []StoredEvent
	for rows.Next() {
		var e StoredEvent
		var at int64
		if err := rows.Scan(&e.ID, &e.Type, &at, &e.Body); err != nil {
			return nil, err
		}
		e.At = fromMillis(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// RecentActivity returns up to limit log events for serverID, most recent
// first, excluding heartbeat and players_now events. World-save events
// decode with only their id, type and time set.
func (s *Store) RecentActivity(ctx context.Context, serverID string, limit int) ([]logwatch.Event, error) {
	evs, err := s.RecentEvents(ctx, serverID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]logwatch.Event, 0, len(evs))
	for _, se := range evs {
		var e logwatch.Event
		if err := json.Unmarshal(se.Body, &e); err != nil {
			return nil, fmt.Errorf("store: unmarshal event: %w", err)
		}
		out = append(out, e)
	}
	return out, nil
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

// PruneEvents deletes heartbeat and players_now events older than before,
// across all servers, and returns the number of rows deleted. Every other
// event is kept for good: the activity timeline reaches back to when
// tracking began.
func (s *Store) PruneEvents(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE at < ? AND type IN (?, ?)`,
		append([]any{millis(before)}, noiseTypes...)...)
	if err != nil {
		return 0, fmt.Errorf("store: prune events: %w", err)
	}
	return res.RowsAffected()
}
