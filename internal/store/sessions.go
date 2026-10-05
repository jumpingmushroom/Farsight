package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"time"
)

// Session is one player's time online on a server, from the moment they
// were identified (Since) until they left (Until — nil while still
// online).
type Session struct {
	ServerID   string
	Name       string
	Platform   string
	PlatformID string
	Reason     string
	Since      time.Time
	Until      *time.Time
	Seconds    int64
}

// OpenSession records a session as having started. It is idempotent on
// (ServerID, PlatformID, Name, Since): a repeat call with the same key
// is silently ignored. tx may be nil to run directly on the database.
func (s *Store) OpenSession(ctx context.Context, tx *sql.Tx, sess Session) error {
	_, err := s.conn(tx).ExecContext(ctx, `
		INSERT OR IGNORE INTO sessions (server_id, platform_id, name, since, platform)
		VALUES (?, ?, ?, ?, ?)`,
		sess.ServerID, sess.PlatformID, sess.Name, millis(sess.Since), sess.Platform)
	if err != nil {
		return fmt.Errorf("store: open session: %w", err)
	}
	return nil
}

// CloseSession sets until, seconds and reason (from sess) on the open
// session row matching sess's exact key (ServerID, PlatformID, Name,
// Since). found is false if no such open row exists. tx may be nil to
// run directly on the database.
func (s *Store) CloseSession(ctx context.Context, tx *sql.Tx, sess Session) (found bool, err error) {
	if sess.Until == nil {
		return false, fmt.Errorf("store: close session: sess.Until is nil")
	}
	res, err := s.conn(tx).ExecContext(ctx, `
		UPDATE sessions SET until = ?, seconds = ?, reason = ?
		WHERE server_id = ? AND platform_id = ? AND name = ? AND since = ? AND until IS NULL`,
		nullMillis(derefTime(sess.Until)), sess.Seconds, sess.Reason,
		sess.ServerID, sess.PlatformID, sess.Name, millis(sess.Since))
	if err != nil {
		return false, fmt.Errorf("store: close session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// InsertClosedSession records a session that is already known to be
// closed (for example, a leave event observed with no matching join),
// inserting until, seconds and reason directly. It is idempotent on the
// same key as OpenSession. tx may be nil to run directly on the
// database.
func (s *Store) InsertClosedSession(ctx context.Context, tx *sql.Tx, sess Session) error {
	_, err := s.conn(tx).ExecContext(ctx, `
		INSERT OR IGNORE INTO sessions (server_id, platform_id, name, since, platform, until, seconds, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ServerID, sess.PlatformID, sess.Name, millis(sess.Since), sess.Platform,
		nullMillis(derefTime(sess.Until)), sess.Seconds, sess.Reason)
	if err != nil {
		return fmt.Errorf("store: insert closed session: %w", err)
	}
	return nil
}

// CloseAllOpen closes every open session on serverID as of until, for
// the given reason, with seconds computed as max(0, until-since). It
// returns the number of sessions closed. tx may be nil to run directly
// on the database.
func (s *Store) CloseAllOpen(ctx context.Context, tx *sql.Tx, serverID string, until time.Time, reason string) (int64, error) {
	untilMs := millis(until)
	res, err := s.conn(tx).ExecContext(ctx, `
		UPDATE sessions
		SET until = ?, reason = ?,
		    seconds = CASE WHEN ? > since THEN (? - since) / 1000 ELSE 0 END
		WHERE server_id = ? AND until IS NULL`,
		untilMs, reason, untilMs, untilMs, serverID)
	if err != nil {
		return 0, fmt.Errorf("store: close all open: %w", err)
	}
	return res.RowsAffected()
}

// Online returns the currently open sessions for serverID, ordered by
// Since ascending (oldest first).
func (s *Store) Online(ctx context.Context, serverID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT server_id, platform_id, name, since, platform, until, seconds, reason
		FROM sessions
		WHERE server_id = ? AND until IS NULL
		ORDER BY since ASC`, serverID)
	if err != nil {
		return nil, fmt.Errorf("store: online: %w", err)
	}
	defer rows.Close()
	return scanSessions(rows)
}

// Recent returns closed sessions for serverID with Until at or after
// after, ordered by Until descending (most recently ended first),
// limited to limit rows.
func (s *Store) Recent(ctx context.Context, serverID string, after time.Time, limit int) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT server_id, platform_id, name, since, platform, until, seconds, reason
		FROM sessions
		WHERE server_id = ? AND until IS NOT NULL AND until >= ?
		ORDER BY until DESC
		LIMIT ?`, serverID, millis(after), limit)
	if err != nil {
		return nil, fmt.Errorf("store: recent: %w", err)
	}
	defer rows.Close()
	return scanSessions(rows)
}

// PlayerSessions returns every session of platformID on serverID, oldest
// first.
func (s *Store) PlayerSessions(ctx context.Context, serverID, platformID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT server_id, platform_id, name, since, platform, until, seconds, reason
		FROM sessions
		WHERE server_id = ? AND platform_id = ?
		ORDER BY since ASC, name ASC`, serverID, platformID)
	if err != nil {
		return nil, fmt.Errorf("store: player sessions: %w", err)
	}
	defer rows.Close()
	return scanSessions(rows)
}

// SessionsOverlapping returns serverID's sessions that overlap
// [from, until): started before until, and still open or ended after
// from. Oldest first.
func (s *Store) SessionsOverlapping(ctx context.Context, serverID string, from, until time.Time) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT server_id, platform_id, name, since, platform, until, seconds, reason
		FROM sessions
		WHERE server_id = ? AND since < ? AND (until IS NULL OR until > ?)
		ORDER BY since ASC, name ASC`, serverID, millis(until), millis(from))
	if err != nil {
		return nil, fmt.Errorf("store: sessions overlapping: %w", err)
	}
	defer rows.Close()
	return scanSessions(rows)
}

// Player is one platform ID seen on a server, under the newest name its
// sessions carry.
type Player struct {
	PlatformID string
	Platform   string
	Name       string
	Names      []string  // every name its sessions carry, oldest first
	Online     bool      // has an open session
	LastSeen   time.Time // latest session end; zero if it never closed one
}

// Players returns every player with a platform ID on serverID: online
// players first (by name), then by LastSeen, newest first.
func (s *Store) Players(ctx context.Context, serverID string) ([]Player, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT server_id, platform_id, name, since, platform, until, seconds, reason
		FROM sessions
		WHERE server_id = ? AND platform_id <> ''
		ORDER BY since ASC, name ASC`, serverID)
	if err != nil {
		return nil, fmt.Errorf("store: players: %w", err)
	}
	defer rows.Close()
	sessions, err := scanSessions(rows)
	if err != nil {
		return nil, err
	}
	byID := map[string]*Player{}
	var order []string
	for _, sess := range sessions {
		p := byID[sess.PlatformID]
		if p == nil {
			p = &Player{PlatformID: sess.PlatformID}
			byID[sess.PlatformID] = p
			order = append(order, sess.PlatformID)
		}
		p.Name, p.Platform = sess.Name, sess.Platform // oldest first: the last one wins
		if !slices.Contains(p.Names, sess.Name) {
			p.Names = append(p.Names, sess.Name)
		}
		if sess.Until == nil {
			p.Online = true
		} else if sess.Until.After(p.LastSeen) {
			p.LastSeen = *sess.Until
		}
	}
	out := make([]Player, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Online != b.Online {
			return a.Online
		}
		if a.Online {
			return a.Name < b.Name
		}
		return a.LastSeen.After(b.LastSeen)
	})
	return out, nil
}

// ServersWithOpenSessions returns the IDs of all servers that currently
// have at least one open session.
func (s *Store) ServersWithOpenSessions(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT server_id FROM sessions WHERE until IS NULL ORDER BY server_id`)
	if err != nil {
		return nil, fmt.Errorf("store: servers with open sessions: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func scanSessions(rows *sql.Rows) ([]Session, error) {
	var out []Session
	for rows.Next() {
		var sess Session
		var sinceMs int64
		var until sql.NullInt64
		if err := rows.Scan(&sess.ServerID, &sess.PlatformID, &sess.Name, &sinceMs, &sess.Platform, &until, &sess.Seconds, &sess.Reason); err != nil {
			return nil, err
		}
		sess.Since = fromMillis(sinceMs)
		if until.Valid {
			t := fromMillis(until.Int64)
			sess.Until = &t
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
