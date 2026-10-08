// Package live turns agent-reported events into the sessions and
// per-server live state the API serves: applying a batch of events
// transactionally, and sweeping servers whose heartbeat has gone stale.
package live

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/jumpingmushroom/farsight/internal/logwatch"
	"github.com/jumpingmushroom/farsight/internal/store"
)

// HeartbeatTimeout is how long a server can go without a heartbeat
// before it is considered offline (Status) or has its open sessions
// swept as lost (SweepStale).
const HeartbeatTimeout = 3 * time.Minute

// EventRetention is how long serve keeps heartbeat and players_now
// events before pruning them; every other event type (and every
// snapshot the world-diff backfill still needs) is kept indefinitely.
// Deduplication by event id for those two noise types only works inside
// this window, so Apply refuses any event too close to (or past) it:
// see maxEventAge.
const EventRetention = 14 * 24 * time.Hour

// maxEventAge is the oldest an event's At may be (relative to now) and
// still be applied: a day inside EventRetention, so a heartbeat or
// players_now event is refused well before serve's prune could have
// dropped its dedupe row, and a replayed weeks-old event of any type can
// never rewind live state or sessions.
const maxEventAge = EventRetention - 24*time.Hour

// maxFuture is how far past Now an event's At may be before it is
// treated as invalid.
const maxFuture = 10 * time.Minute

// invalidWarnInterval rate-limits Apply's warning about events dropped
// as invalid to once per server per interval: a misconfigured agent
// sends them in every batch.
const invalidWarnInterval = 10 * time.Minute

// Applier applies agent-reported events into the store's sessions and
// live tables, and sweeps servers that have gone quiet.
type Applier struct {
	Store *store.Store

	// Now, if set, is used as the current time; otherwise time.Now is
	// used. Tests set this to a fixed clock.
	Now func() time.Time

	// StartedAt, if set, is when the app started: SweepStale sweeps
	// nothing until HeartbeatTimeout after it, since a heartbeat that
	// is stale only because the app itself was down (a deploy) says
	// nothing about the game server, and the agent's retry backoff may
	// not have got one through yet.
	StartedAt time.Time

	// Log, if set, gets a warning when Apply drops events as invalid
	// (see validEvent), so a misconfigured agent (a wrong
	// FARSIGHT_LOG_TZ dates every event in the future) isn't silent.
	Log *slog.Logger

	// lastInvalidWarn is when each server's invalid events were last
	// warned about; guarded by mu.
	lastInvalidWarn map[string]time.Time

	// mu serializes Apply and SweepStale: both do a read-modify-write of
	// a server's live row, and concurrent batches for the same server
	// would otherwise race on that. The zero value is a usable, unlocked
	// mutex.
	mu sync.Mutex
}

func (a *Applier) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// knownEventTypes are the event types Apply recognizes; anything else is
// an "unknown type" and makes the event invalid.
var knownEventTypes = map[string]bool{
	logwatch.EvServerStarting: true,
	logwatch.EvServerBoot:     true,
	logwatch.EvServerReady:    true,
	logwatch.EvServerStopped:  true,
	logwatch.EvJoinCode:       true,
	logwatch.EvPlayersNow:     true,
	logwatch.EvPlayerJoin:     true,
	logwatch.EvPlayerLeave:    true,
	logwatch.EvWorldSaved:     true,
	logwatch.EvHeartbeat:      true,
	logwatch.EvRaid:           true,
	logwatch.EvTimeSkip:       true,
	logwatch.EvPlayerDeath:    true,
}

// validEvent reports whether e is well-formed enough to apply: it has an
// id, a known type, a non-zero At, an At no more than maxFuture past now,
// and an At no more than maxEventAge before now.
func validEvent(e logwatch.Event, now time.Time) bool {
	if e.ID == "" {
		return false
	}
	if !knownEventTypes[e.Type] {
		return false
	}
	if e.At.IsZero() {
		return false
	}
	if e.At.After(now.Add(maxFuture)) {
		return false
	}
	if e.At.Before(now.Add(-maxEventAge)) {
		return false
	}
	return true
}

// Apply records evs (in order) against serverID: each event is inserted
// into the event log (deduped by id), and, if newly inserted, folded
// into the server's sessions and live state. The whole batch runs in one
// transaction, so a store error rolls the batch back; applied and
// skipped are only meaningful when err is nil.
func (a *Applier) Apply(ctx context.Context, serverID string, evs []logwatch.Event) (applied, skipped int, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.now()
	invalid := 0
	var firstInvalid logwatch.Event
	defer func() {
		if invalid > 0 {
			a.warnInvalid(serverID, invalid, firstInvalid, now)
		}
	}()

	err = a.Store.Tx(ctx, func(tx *sql.Tx) error {
		live, ok, err := a.Store.GetLive(ctx, tx, serverID)
		if err != nil {
			return err
		}
		if !ok {
			live = store.Live{ServerID: serverID}
		}

		for _, e := range evs {
			if !validEvent(e, now) {
				if invalid == 0 {
					firstInvalid = e
				}
				invalid++
				skipped++
				continue
			}

			if e.Type == logwatch.EvPlayerDeath && e.Intro {
				skip, err := a.skippedIntro(ctx, tx, serverID, e)
				if err != nil {
					return err
				}
				if skip {
					skipped++
					continue
				}
			}

			isNew, err := a.Store.InsertEventIfNew(ctx, tx, serverID, e)
			if err != nil {
				return err
			}
			if !isNew {
				// A replayed join (the agent restarted) says the player
				// is still on: undo a stale-heartbeat close of it.
				if e.Type == logwatch.EvPlayerJoin {
					if err := a.Store.ReopenSession(ctx, tx, store.Session{
						ServerID: serverID, Name: e.Name, PlatformID: e.PlatformID, Since: e.At,
					}); err != nil {
						return err
					}
				}
				skipped++
				continue
			}

			lastSeen := maxTime(live.LastHeartbeat, live.LastEventAt)
			statusChanged := false

			switch e.Type {
			case logwatch.EvHeartbeat:
				live.LastHeartbeat = maxTime(live.LastHeartbeat, e.At)

			case logwatch.EvServerStopped:
				if _, err := a.Store.CloseAllOpen(ctx, tx, serverID, e.At, "server_stopped"); err != nil {
					return err
				}
				live.Status = "restarting"
				statusChanged = true
				endBoot(&live, e.At)

			case logwatch.EvServerStarting, logwatch.EvServerBoot:
				until := e.At
				if !lastSeen.IsZero() {
					until = minTime(e.At, lastSeen)
				}
				if _, err := a.Store.CloseAllOpen(ctx, tx, serverID, until, "server_lost"); err != nil {
					return err
				}
				live.Status = "starting"
				statusChanged = true
				endBoot(&live, e.At)
				live.UpSince = e.At
				if e.Type == logwatch.EvServerBoot {
					live.Version = e.Version
					live.NetworkVersion = e.NetworkVersion
				}

			case logwatch.EvServerReady:
				live.Status = "online"
				statusChanged = true

			case logwatch.EvPlayersNow:
				if e.Players != nil {
					live.Players = *e.Players
				}

			case logwatch.EvJoinCode:
				live.JoinCode = e.Code
				live.JoinCodeAt = e.At

			case logwatch.EvPlayerJoin:
				if err := a.Store.OpenSession(ctx, tx, store.Session{
					ServerID: serverID, Name: e.Name, Platform: e.Platform, PlatformID: e.PlatformID,
					Since: e.At,
				}); err != nil {
					return err
				}

			case logwatch.EvPlayerLeave:
				until := e.At
				found := false
				if e.Since != nil {
					found, err = a.Store.CloseSession(ctx, tx, store.Session{
						ServerID: serverID, Name: e.Name, Platform: e.Platform, PlatformID: e.PlatformID,
						Since: *e.Since, Until: &until, Seconds: e.Seconds, Reason: e.Reason,
					})
					if err != nil {
						return err
					}
				}
				if !found {
					since := until
					if e.Since != nil {
						since = *e.Since
					}
					if err := a.Store.InsertClosedSession(ctx, tx, store.Session{
						ServerID: serverID, Name: e.Name, Platform: e.Platform, PlatformID: e.PlatformID,
						Since: since, Until: &until, Seconds: e.Seconds, Reason: e.Reason,
					}); err != nil {
						return err
					}
				}

			case logwatch.EvWorldSaved, logwatch.EvRaid, logwatch.EvTimeSkip, logwatch.EvPlayerDeath:
				// No live-state change; the row already inserted into
				// events is enough.
			}

			if e.Type != logwatch.EvHeartbeat {
				live.LastEventAt = maxTime(live.LastEventAt, e.At)
			}
			if statusChanged {
				live.StatusAt = e.At
			}
			applied++
		}

		if applied == 0 {
			// Nothing changed; avoid materializing an empty live row for
			// a server we've never actually heard from.
			return nil
		}
		live.ServerID = serverID
		return a.Store.PutLive(ctx, tx, live)
	})
	if err != nil {
		return applied, skipped, err
	}
	return applied, skipped, nil
}

// endBoot clears what a restart at at invalidates in l: the player count
// (every session was just closed) and the previous boot's crossplay join
// code, which the new boot replaces with its own. The supervisor's log
// (stopped, starting) and the server's are read by separate followers,
// so the restart may be applied after the new boot's join code: a code
// logged after at is kept.
func endBoot(l *store.Live, at time.Time) {
	l.Players = 0
	if !l.JoinCodeAt.After(at) {
		l.JoinCode, l.JoinCodeAt = "", time.Time{}
	}
}

// skippedIntro reports whether e, a player_death flagged Intro (logged
// within logwatch.IntroWindow of its session's start), is a new
// character skipping the Valkyrie intro rather than a death. Only a
// character's first session has the intro, so it is a skip unless the
// server has a session of that character (its name, and its platform id
// when e has one) that started before e.At - IntroWindow: e's own
// session started after that, so any such session is an earlier one.
// The rule is deterministic, so a replayed death is judged the same way.
// A character whose earlier sessions all fell inside the window (a
// rejoin within seconds of a first join) is still treated as new.
func (a *Applier) skippedIntro(ctx context.Context, tx *sql.Tx, serverID string, e logwatch.Event) (bool, error) {
	before, err := a.Store.HasSessionBefore(ctx, tx, serverID, e.Name, e.PlatformID, e.At.Add(-logwatch.IntroWindow))
	return !before, err
}

// warnInvalid logs that n of serverID's events were dropped as invalid,
// unless it already did within invalidWarnInterval. first is the first
// of them, whose time says what is probably wrong. Called with mu held.
func (a *Applier) warnInvalid(serverID string, n int, first logwatch.Event, now time.Time) {
	if a.Log == nil || now.Sub(a.lastInvalidWarn[serverID]) < invalidWarnInterval {
		return
	}
	if a.lastInvalidWarn == nil {
		a.lastInvalidWarn = map[string]time.Time{}
	}
	a.lastInvalidWarn[serverID] = now
	hint := "missing id, unknown type or no time"
	switch {
	case first.At.IsZero() || first.ID == "" || !knownEventTypes[first.Type]:
	case first.At.After(now):
		hint = "dated in the future: check the agent's FARSIGHT_LOG_TZ matches the game container's TZ"
	default:
		hint = "older than the event retention window"
	}
	a.Log.Warn("dropped invalid events", "server", serverID, "invalid", n,
		"type", first.Type, "at", first.At, "offset", first.At.Sub(now).Round(time.Second), "hint", hint)
}

// SweepStale closes the open sessions of every server whose last known
// heartbeat is more than HeartbeatTimeout old (or unknown), using that
// server's last-seen time (heartbeat or event, whichever is later) as
// the close time. It returns the total number of sessions closed.
func (a *Applier) SweepStale(ctx context.Context) (closed int64, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.now()
	if !a.StartedAt.IsZero() && now.Sub(a.StartedAt) <= HeartbeatTimeout {
		return 0, nil
	}

	servers, err := a.Store.ServersWithOpenSessions(ctx)
	if err != nil {
		return 0, err
	}

	var total int64
	for _, serverID := range servers {
		err := a.Store.Tx(ctx, func(tx *sql.Tx) error {
			live, ok, err := a.Store.GetLive(ctx, tx, serverID)
			if err != nil {
				return err
			}
			if !ok {
				live = store.Live{ServerID: serverID}
			}

			if !live.LastHeartbeat.IsZero() && now.Sub(live.LastHeartbeat) <= HeartbeatTimeout {
				return nil
			}

			lastSeen := maxTime(live.LastHeartbeat, live.LastEventAt)
			n, err := a.Store.CloseAllOpen(ctx, tx, serverID, lastSeen, store.ReasonHeartbeatLost)
			if err != nil {
				return err
			}
			total += n
			return nil
		})
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// Status computes a server's read-time status from its live row: l is
// the zero Live when the server has no recorded state at all.
func Status(l store.Live, now time.Time) string {
	if l.LastHeartbeat.IsZero() && l.LastEventAt.IsZero() {
		return "unknown"
	}
	if now.Sub(l.LastHeartbeat) > HeartbeatTimeout {
		return "offline"
	}
	if l.Status == "" {
		return "online"
	}
	return l.Status
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
