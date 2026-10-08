package store

import (
	"context"
	"database/sql"
	"fmt"
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

// ReasonHeartbeatLost is the reason a session gets when it is closed
// only because its server's heartbeat went stale. That close is a
// guess, so the agent's own word overrides it: CloseSession replaces it
// with the real end, and ReopenSession undoes it.
const ReasonHeartbeatLost = "heartbeat_lost"

// CloseSession sets until, seconds and reason (from sess) on the session
// row matching sess's exact key (ServerID, PlatformID, Name, Since) that
// is open or was closed as ReasonHeartbeatLost. found is false if no
// such row exists. tx may be nil to run directly on the database.
func (s *Store) CloseSession(ctx context.Context, tx *sql.Tx, sess Session) (found bool, err error) {
	if sess.Until == nil {
		return false, fmt.Errorf("store: close session: sess.Until is nil")
	}
	res, err := s.conn(tx).ExecContext(ctx, `
		UPDATE sessions SET until = ?, seconds = ?, reason = ?
		WHERE server_id = ? AND platform_id = ? AND name = ? AND since = ?
		  AND (until IS NULL OR reason = ?)`,
		nullMillis(derefTime(sess.Until)), sess.Seconds, sess.Reason,
		sess.ServerID, sess.PlatformID, sess.Name, millis(sess.Since), ReasonHeartbeatLost)
	if err != nil {
		return false, fmt.Errorf("store: close session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ReopenSession reopens the session row matching sess's exact key if it
// was closed as ReasonHeartbeatLost. tx may be nil to run directly on
// the database.
func (s *Store) ReopenSession(ctx context.Context, tx *sql.Tx, sess Session) error {
	_, err := s.conn(tx).ExecContext(ctx, `
		UPDATE sessions SET until = NULL, seconds = 0, reason = ''
		WHERE server_id = ? AND platform_id = ? AND name = ? AND since = ? AND reason = ?`,
		sess.ServerID, sess.PlatformID, sess.Name, millis(sess.Since), ReasonHeartbeatLost)
	if err != nil {
		return fmt.Errorf("store: reopen session: %w", err)
	}
	return nil
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

// HasSessionBefore reports whether serverID has a session, open or
// closed, of the character name that started before before. A non-empty
// platformID narrows it to that account's character of that name. tx may
// be nil to run directly on the database.
func (s *Store) HasSessionBefore(ctx context.Context, tx *sql.Tx, serverID, name, platformID string, before time.Time) (bool, error) {
	var one int
	err := s.conn(tx).QueryRowContext(ctx, `
		SELECT 1 FROM sessions
		WHERE server_id = ? AND name = ? AND (? = '' OR platform_id = ?) AND since < ?
		LIMIT 1`,
		serverID, name, platformID, platformID, millis(before)).Scan(&one)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("store: has session before: %w", err)
	}
	return true, nil
}

// SessionsOverlapping returns serverID's sessions that overlap
// [from, until): started before until, and still open or ended after
// from. Oldest first.
func (s *Store) SessionsOverlapping(ctx context.Context, serverID string, from, until time.Time) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, sessionsOverlappingSQL, serverID, millis(until), serverID, millis(from), millis(until))
	if err != nil {
		return nil, fmt.Errorf("store: sessions overlapping: %w", err)
	}
	defer rows.Close()
	return scanSessions(rows)
}

// sessionsOverlappingSQL is the open sessions plus the closed ones ending
// after from: two range scans of the (server_id, until) index, so the
// cost follows the window rather than the server's whole history (an
// OR of the two would only use the index's server_id prefix).
const sessionsOverlappingSQL = `
		SELECT server_id, platform_id, name, since, platform, until, seconds, reason
		FROM sessions
		WHERE server_id = ? AND until IS NULL AND since < ?
		UNION ALL
		SELECT server_id, platform_id, name, since, platform, until, seconds, reason
		FROM sessions
		WHERE server_id = ? AND until > ? AND since < ?
		ORDER BY since ASC, name ASC`

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
// players first (by name), then by LastSeen, newest first; ties keep the
// order of each player's first session.
//
// Every card poll and activity request asks for this, so the sessions
// are aggregated in SQL, one row per name a platform ID has played
// under, rather than read in full: the rows are few however long the
// history grows.
func (s *Store) Players(ctx context.Context, serverID string) ([]Player, error) {
	rows, err := s.db.QueryContext(ctx, playersSQL, serverID)
	if err != nil {
		return nil, fmt.Errorf("store: players: %w", err)
	}
	defer rows.Close()

	byID := map[string]*Player{}
	newest := map[string]int64{} // platform ID -> since of its newest session
	var order []string
	for rows.Next() {
		var id, name, platform string
		var first, last int64
		var online bool
		var lastUntil sql.NullInt64
		if err := rows.Scan(&id, &name, &platform, &first, &last, &online, &lastUntil); err != nil {
			return nil, fmt.Errorf("store: players: %w", err)
		}
		// Rows come in order of each name's first session, so a player's
		// first row is its first session, and its names fill oldest first.
		p := byID[id]
		if p == nil {
			p = &Player{PlatformID: id}
			byID[id] = p
			order = append(order, id)
		}
		p.Names = append(p.Names, name)
		// The newest session's name and platform win (ties by name, as
		// sessions are ordered everywhere else).
		if n, seen := newest[id]; !seen || last > n || (last == n && name > p.Name) {
			newest[id] = last
			p.Name, p.Platform = name, platform
		}
		p.Online = p.Online || online
		if lastUntil.Valid {
			if t := fromMillis(lastUntil.Int64); t.After(p.LastSeen) {
				p.LastSeen = t
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: players: %w", err)
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

// playersSQL is one row per (platform ID, name) on a server: the platform
// of that name's newest session, its first and newest session start,
// whether any of its sessions is open and its latest end. Ordered by
// first session, then name and platform ID.
const playersSQL = `
		WITH s AS (
			SELECT platform_id, name, since, until,
			       FIRST_VALUE(platform) OVER (PARTITION BY platform_id, name ORDER BY since DESC) AS platform
			FROM sessions
			WHERE server_id = ? AND platform_id <> ''
		)
		SELECT platform_id, name, MAX(platform), MIN(since), MAX(since), MAX(until IS NULL), MAX(until)
		FROM s
		GROUP BY platform_id, name
		ORDER BY MIN(since) ASC, name ASC, platform_id ASC`

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
