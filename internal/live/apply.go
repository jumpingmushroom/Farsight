// Package live turns agent-reported events into the sessions and
// per-server live state the API serves: applying a batch of events
// transactionally, and sweeping servers whose heartbeat has gone stale.
package live

import (
	"context"
	"database/sql"
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

// Applier applies agent-reported events into the store's sessions and
// live tables, and sweeps servers that have gone quiet.
type Applier struct {
	Store *store.Store

	// Now, if set, is used as the current time; otherwise time.Now is
	// used. Tests set this to a fixed clock.
	Now func() time.Time

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
				skipped++
				continue
			}

			isNew, err := a.Store.InsertEventIfNew(ctx, tx, serverID, e)
			if err != nil {
				return err
			}
			if !isNew {
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

			case logwatch.EvWorldSaved, logwatch.EvRaid, logwatch.EvTimeSkip:
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

// SweepStale closes the open sessions of every server whose last known
// heartbeat is more than HeartbeatTimeout old (or unknown), using that
// server's last-seen time (heartbeat or event, whichever is later) as
// the close time. It returns the total number of sessions closed.
func (a *Applier) SweepStale(ctx context.Context) (closed int64, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.now()

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
			n, err := a.Store.CloseAllOpen(ctx, tx, serverID, lastSeen, "server_lost")
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
