package logwatch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Event is one player-session or server-lifecycle fact produced by pairing
// raw log lines. The pairing rules mirror valheim.mtail in the cloudcluster
// repo.
type Event struct {
	ID   string    `json:"id"`
	Type string    `json:"type"`
	At   time.Time `json:"at"`

	Name       string `json:"name,omitempty"`
	Platform   string `json:"platform,omitempty"`
	PlatformID string `json:"platformId,omitempty"`

	Code    string `json:"code,omitempty"`
	Players *int   `json:"players,omitempty"`

	Version        string `json:"version,omitempty"`
	NetworkVersion int    `json:"networkVersion,omitempty"`

	Since   *time.Time `json:"since,omitempty"`
	Seconds int64      `json:"seconds,omitempty"`
	Reason  string     `json:"reason,omitempty"`

	// Raid is an event_raid's game event name ("army_theelder"). EventID
	// leaves it out, so IDs of every other type are unchanged; a raid's
	// type and second are unique enough.
	Raid string `json:"raid,omitempty"`

	// To is a time_skip's nextm: the netTime the server will wake up at
	// after the sleep. EventID leaves it out too, for the same reason.
	To float64 `json:"to,omitempty"`

	// Intro marks a player_death logged within IntroWindow of its
	// session's start: a new character skipping the Valkyrie intro is
	// respawned exactly as a dead one is, and only the app, which knows
	// whether the character has played before, can tell them apart.
	// EventID leaves it out, so a death's ID is the same either way.
	Intro bool `json:"intro,omitempty"`
}

const (
	EvServerStarting = "server_starting"
	EvServerBoot     = "server_boot"
	EvServerReady    = "server_ready"
	EvServerStopped  = "server_stopped"
	EvJoinCode       = "join_code"
	EvPlayersNow     = "players_now"
	EvPlayerJoin     = "player_join"
	EvPlayerLeave    = "player_leave"
	EvWorldSaved     = "world_saved"
	EvHeartbeat      = "heartbeat"
	EvRaid           = "event_raid"
	EvTimeSkip       = "time_skip"
	EvPlayerDeath    = "player_death"
)

// IntroWindow is how long after joining a 0:0 character line may be a
// skipped intro rather than a death: a new character arrives riding the
// Valkyrie (a flight of about 65 s), and skipping it respawns the player
// exactly as dying does. Such a death is sent flagged Intro.
const IntroWindow = 90 * time.Second

// session is one open player session, keyed either "s:{steamID}" (Steam
// mode) or "u:{uid}" (crossplay mode, keyed by the ZDO owner id).
type session struct {
	name       string
	platform   string
	platformID string
	since      time.Time
}

// Sessionizer pairs Raw facts, read in log order, into Events. It mirrors
// the state valheim.mtail keeps: a per-Steam-ID connect time, the most
// recently connected Steam ID, a pending/last PlayFab identity handshake for
// crossplay, and the open sessions themselves.
type Sessionizer struct {
	connect map[string]time.Time // steamID -> connect At
	lastSID string

	pendingPlatform, pendingID string // PlayFab identity awaiting its ZDOID line
	lastPlatform, lastID       string // most recent identity; non-empty marks crossplay

	sessions map[string]*session
	order    []string // open session keys, in open order
}

// NewSessionizer returns a Sessionizer with empty state.
func NewSessionizer() *Sessionizer {
	return &Sessionizer{
		connect:  map[string]time.Time{},
		sessions: map[string]*session{},
	}
}

// Feed consumes one Raw fact and returns zero or more Events, each with its
// ID already set.
func (s *Sessionizer) Feed(r Raw) []Event {
	var out []Event
	switch r.Kind {
	case RawIdentity:
		s.pendingPlatform, s.pendingID = r.Platform, r.PlatformID
		s.lastPlatform, s.lastID = r.Platform, r.PlatformID
	case RawSteamConnect:
		s.connect[r.SteamID] = r.At
		s.lastSID = r.SteamID
	case RawSpawn:
		out = s.spawn(r)
	case RawSteamClose:
		out = s.steamClose(r)
	case RawDestroy:
		out = s.destroy(r)
	case RawStopped, RawStarting, RawBoot:
		out = s.closeAllAndReset(r)
	case RawReady:
		out = []Event{s.emit(EvServerReady, r.At, Event{})}
	case RawJoinCode:
		out = []Event{s.emit(EvJoinCode, r.At, Event{Code: r.Code})}
	case RawPlayers:
		p := r.Players
		out = []Event{s.emit(EvPlayersNow, r.At, Event{Players: &p})}
	case RawSaved:
		out = []Event{s.emit(EvWorldSaved, r.At, Event{})}
	case RawRaid:
		out = []Event{s.emit(EvRaid, r.At, Event{Raid: r.Raid})}
	case RawTimeSkip:
		out = []Event{s.emit(EvTimeSkip, r.At, Event{To: r.To})}
	case RawDeath:
		out = s.death(r)
	}
	return out
}

// spawn opens a session on a ZDOID line. Crossplay is any instance where an
// identity handshake has been seen at all (last is non-empty); otherwise
// this falls back to the Steam pairing, keyed by the most recent connect.
func (s *Sessionizer) spawn(r Raw) []Event {
	if s.lastPlatform != "" || s.lastID != "" {
		key := fmt.Sprintf("u:%d", r.UID)
		if _, open := s.sessions[key]; open {
			return nil // respawn after death; the session is already open
		}
		platform, id := s.pendingPlatform, s.pendingID
		if platform == "" && id == "" {
			platform, id = s.lastPlatform, s.lastID
		}
		s.pendingPlatform, s.pendingID = "", ""
		s.open(key, session{name: r.Name, platform: platform, platformID: id, since: r.At})
		return []Event{s.emit(EvPlayerJoin, r.At, Event{Name: r.Name, Platform: platform, PlatformID: id})}
	}

	if s.lastSID == "" {
		return nil
	}
	if _, connected := s.connect[s.lastSID]; !connected {
		return nil
	}
	// Known trade-off, inherited from valheim.mtail: a Steam ZDOID line
	// carries no Steam ID, so it is attributed to lastSID. If an already
	// playing player respawns (death, or a bed respawn) between another
	// player's connect and that player's own spawn, the respawn's ZDOID
	// line opens the newcomer's session (under the respawning player's
	// name, from that moment), and the newcomer's real spawn is then
	// ignored as a respawn.
	key := fmt.Sprintf("s:%s", s.lastSID)
	if _, open := s.sessions[key]; open {
		return nil // respawn
	}
	s.open(key, session{name: r.Name, platform: "Steam", platformID: s.lastSID, since: r.At})
	return []Event{s.emit(EvPlayerJoin, r.At, Event{Name: r.Name, Platform: "Steam", PlatformID: s.lastSID})}
}

// death turns a 0:0 character line into a player_death. The line names the
// character only, so the platform identity comes from the newest open
// session under that name; with none open the event keeps just the name.
// A 0:0 within IntroWindow of the session's start may be a skipped intro,
// or a returning character dying early: it is flagged Intro, and the app
// drops it only for a character with no earlier session.
func (s *Sessionizer) death(r Raw) []Event {
	ev := Event{Name: r.Name}
	for i := len(s.order) - 1; i >= 0; i-- {
		sess := s.sessions[s.order[i]]
		if sess.name != r.Name {
			continue
		}
		ev.Intro = r.At.Sub(sess.since) < IntroWindow
		ev.Platform, ev.PlatformID = sess.platform, sess.platformID
		break
	}
	return []Event{s.emit(EvPlayerDeath, r.At, ev)}
}

// steamClose pairs a "Closing socket" line with its open Steam session.
// The connect time and any open session for sid are always cleared,
// matched or not.
func (s *Sessionizer) steamClose(r Raw) []Event {
	key := fmt.Sprintf("s:%s", r.SteamID)
	var out []Event
	if sess, open := s.sessions[key]; open {
		out = []Event{s.leaveEvent(r.At, *sess, "left")}
	}
	s.close(key)
	delete(s.connect, r.SteamID)
	return out
}

// destroy pairs a "Destroying abandoned non persistent zdo ... owner uid"
// line with its open crossplay session. Many such lines arrive per leave,
// one per orphaned ZDO; only the first, which finds the session still open,
// emits anything.
func (s *Sessionizer) destroy(r Raw) []Event {
	key := fmt.Sprintf("u:%d", r.UID)
	sess, open := s.sessions[key]
	if !open {
		return nil
	}
	ev := s.leaveEvent(r.At, *sess, "left")
	s.close(key)
	return []Event{ev}
}

// closeAllAndReset handles stopped/starting/boot: every open session is
// force-closed, in the order it was opened, then all state is cleared,
// then the server-lifecycle event itself is emitted.
func (s *Sessionizer) closeAllAndReset(r Raw) []Event {
	var out []Event
	for _, key := range s.order {
		sess := s.sessions[key]
		out = append(out, s.leaveEvent(r.At, *sess, "server_stopped"))
	}
	s.connect = map[string]time.Time{}
	s.sessions = map[string]*session{}
	s.order = nil
	s.pendingPlatform, s.pendingID = "", ""
	s.lastPlatform, s.lastID = "", ""
	s.lastSID = ""

	switch r.Kind {
	case RawStopped:
		out = append(out, s.emit(EvServerStopped, r.At, Event{}))
	case RawStarting:
		out = append(out, s.emit(EvServerStarting, r.At, Event{}))
	case RawBoot:
		out = append(out, s.emit(EvServerBoot, r.At, Event{Version: r.Version, NetworkVersion: r.NetVersion}))
	}
	return out
}

func (s *Sessionizer) leaveEvent(at time.Time, sess session, reason string) Event {
	since := sess.since
	secs := int64(at.Sub(since) / time.Second)
	if secs < 0 {
		// Only possible if timestamps go backwards (a misconfigured
		// FARSIGHT_LOG_TZ, or a clock step); never report a negative span.
		secs = 0
	}
	return s.emit(EvPlayerLeave, at, Event{
		Name: sess.name, Platform: sess.platform, PlatformID: sess.platformID,
		Since: &since, Seconds: secs, Reason: reason,
	})
}

func (s *Sessionizer) open(key string, sess session) {
	s.sessions[key] = &sess
	s.order = append(s.order, key)
}

func (s *Sessionizer) close(key string) {
	if _, ok := s.sessions[key]; !ok {
		return
	}
	delete(s.sessions, key)
	for i, k := range s.order {
		if k == key {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

// emit fills in Type, At and ID on a partially-built Event.
func (s *Sessionizer) emit(typ string, at time.Time, e Event) Event {
	e.Type, e.At = typ, at
	e.ID = EventID(e)
	return e
}

// EventID is the deterministic ID for an event: the first 8 bytes of
// sha256(type|at.UnixNano|name|platformId|code|players|version|reason), hex
// encoded. Replaying the same file yields the same IDs, and identical facts
// in the same second collapse onto the same ID, which is intended.
func EventID(e Event) string {
	players := ""
	if e.Players != nil {
		players = strconv.Itoa(*e.Players)
	}
	key := strings.Join([]string{
		e.Type,
		strconv.FormatInt(e.At.UnixNano(), 10),
		e.Name,
		e.PlatformID,
		e.Code,
		players,
		e.Version,
		e.Reason,
	}, "|")
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}
