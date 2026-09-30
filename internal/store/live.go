package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Live is the current known state of one server, one row per server. A
// zero time.Time field means that timestamp is unknown.
type Live struct {
	ServerID       string
	Status         string
	Version        string
	JoinCode       string
	NetworkVersion int
	Players        int
	LastHeartbeat  time.Time
	LastEventAt    time.Time
	UpSince        time.Time
	JoinCodeAt     time.Time
	StatusAt       time.Time
}

// GetLive returns the live state for serverID. ok is false if the server
// has no recorded state yet. tx may be nil to run directly on the
// database.
func (s *Store) GetLive(ctx context.Context, tx *sql.Tx, serverID string) (Live, bool, error) {
	var l Live
	var lastHeartbeat, lastEventAt, upSince, joinCodeAt, statusAt sql.NullInt64
	err := s.conn(tx).QueryRowContext(ctx, `
		SELECT server_id, status, version, join_code, network_version, players,
		       last_heartbeat, last_event_at, up_since, join_code_at, status_at
		FROM live WHERE server_id = ?`, serverID).Scan(
		&l.ServerID, &l.Status, &l.Version, &l.JoinCode, &l.NetworkVersion, &l.Players,
		&lastHeartbeat, &lastEventAt, &upSince, &joinCodeAt, &statusAt)
	switch {
	case err == sql.ErrNoRows:
		return Live{}, false, nil
	case err != nil:
		return Live{}, false, fmt.Errorf("store: get live: %w", err)
	}

	l.LastHeartbeat = fromNullMillis(lastHeartbeat)
	l.LastEventAt = fromNullMillis(lastEventAt)
	l.UpSince = fromNullMillis(upSince)
	l.JoinCodeAt = fromNullMillis(joinCodeAt)
	l.StatusAt = fromNullMillis(statusAt)
	return l, true, nil
}

// PutLive replaces the live state for l.ServerID (inserting a new row if
// none exists). tx may be nil to run directly on the database.
func (s *Store) PutLive(ctx context.Context, tx *sql.Tx, l Live) error {
	_, err := s.conn(tx).ExecContext(ctx, `
		INSERT INTO live (server_id, status, version, join_code, network_version, players,
		                   last_heartbeat, last_event_at, up_since, join_code_at, status_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (server_id) DO UPDATE SET
			status          = excluded.status,
			version         = excluded.version,
			join_code       = excluded.join_code,
			network_version = excluded.network_version,
			players         = excluded.players,
			last_heartbeat  = excluded.last_heartbeat,
			last_event_at   = excluded.last_event_at,
			up_since        = excluded.up_since,
			join_code_at    = excluded.join_code_at,
			status_at       = excluded.status_at`,
		l.ServerID, l.Status, l.Version, l.JoinCode, l.NetworkVersion, l.Players,
		nullMillis(l.LastHeartbeat), nullMillis(l.LastEventAt), nullMillis(l.UpSince),
		nullMillis(l.JoinCodeAt), nullMillis(l.StatusAt))
	if err != nil {
		return fmt.Errorf("store: put live: %w", err)
	}
	return nil
}
