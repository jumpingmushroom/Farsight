// Package logwatch turns Valheim server and supervisor log lines into
// player-session and server-lifecycle events. The line shapes and the pairing
// rules mirror valheim.mtail in the cloudcluster repo, which is proven on the
// live Steam and crossplay servers.
package logwatch

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // container images may lack zoneinfo; the logs are local time
)

type RawKind string

const (
	RawBoot         RawKind = "boot"
	RawReady        RawKind = "ready"
	RawStopped      RawKind = "stopped"
	RawStarting     RawKind = "starting"
	RawJoinCode     RawKind = "join_code"
	RawPlayers      RawKind = "players"
	RawSaved        RawKind = "saved"
	RawSteamConnect RawKind = "steam_connect"
	RawSteamClose   RawKind = "steam_close"
	RawIdentity     RawKind = "identity"
	RawSpawn        RawKind = "spawn"
	RawDestroy      RawKind = "destroy"
	RawRaid         RawKind = "raid"
	RawTimeSkip     RawKind = "time_skip"
	RawDeath        RawKind = "death"
)

// Raw is one fact read from one log line, before session pairing.
type Raw struct {
	Kind       RawKind
	At         time.Time // UTC
	Version    string
	NetVersion int
	Code       string
	Players    int
	SteamID    string
	Name       string
	UID        int64
	Platform   string
	PlatformID string
	Raid       string  // RawRaid: the game's event name, e.g. "army_theelder"
	To         float64 // RawTimeSkip: nextm, the netTime the server will wake up at
}

var (
	serverTS     = regexp.MustCompile(`^(\d{2}/\d{2}/\d{4} \d{2}:\d{2}:\d{2}): (.*)$`)
	reVersion    = regexp.MustCompile(`^Valheim version: (\S+) \(network version (\d+)\)`)
	reReady      = regexp.MustCompile(`^Game server connected$`)
	reActive     = regexp.MustCompile(`^Session ".*" with join code (\d+) and IP \S+ is active with (\d+) player\(s\)`)
	reJoinLost   = regexp.MustCompile(`^Player (?:joined|connection lost) server ".*" that has join code (\d*), now (\d+) player\(s\)`)
	reConns      = regexp.MustCompile(`^\s*Connections (\d+) ZDOS:`)
	reSaved      = regexp.MustCompile(`^(?:World saved \(|World save \(5/5\) done)`)
	reSteamConn  = regexp.MustCompile(`^Got connection SteamID (\d+)`)
	reSteamClose = regexp.MustCompile(`^Closing socket (\d+)`)
	reIdentity   = regexp.MustCompile(`received local Platform ID ([A-Za-z]+)_(\S+)$`)
	reSpawn      = regexp.MustCompile(`^Got character ZDOID from (.+?) : (-?\d+):\d+`)
	reDestroy    = regexp.MustCompile(`^Destroying abandoned non persistent zdo -?\d+:\d+ owner (-?\d+)`)
	reRaid       = regexp.MustCompile(`^Random event set:\s*(\S+)`)
	reTimeSkip   = regexp.MustCompile(`^Time ([0-9.]+), day:(\d+)\s+nextm:([0-9.]+)`)

	superTS    = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}),\d+ INFO (.*)$`)
	reStopped  = regexp.MustCompile(`^(?:stopped|exited): valheim-server \(`)
	reStarting = regexp.MustCompile(`^spawned: 'valheim-server' `)
)

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// clock turns one log stream's local wall-clock timestamps into instants.
// It remembers the last instant it produced, so that on the DST fall-back
// day, when an hour of wall-clock time repeats, it can tell the first pass
// of that hour from the second. Each stream (the server log, supervisord.log)
// needs its own clock, kept for the stream's lifetime.
type clock struct {
	loc  *time.Location
	last time.Time // last instant produced; zero before the first line
}

func newClock(loc *time.Location) *clock { return &clock{loc: loc} }

// at parses value (in layout, as local time in c.loc) and returns it as a
// UTC instant. A wall-clock time that occurs twice (the repeated hour on
// the fall-back day) resolves to its earlier instant, unless that is before
// the last instant this stream produced, in which case the log has already
// moved past the first pass and the later instant is chosen. A fresh clock
// therefore always picks the earlier one.
func (c *clock) at(layout, value string) (time.Time, bool) {
	t, err := time.ParseInLocation(layout, value, c.loc)
	if err != nil {
		return time.Time{}, false
	}
	earlier, later := t, t
	// A fall-back transition near t shows up as a larger UTC offset before
	// it than after it; the gap is how far apart the two candidates are.
	_, offBefore := t.Add(-12 * time.Hour).Zone()
	_, offAfter := t.Add(12 * time.Hour).Zone()
	if gap := time.Duration(offBefore-offAfter) * time.Second; gap > 0 {
		if e := t.Add(-gap); sameWall(e, t) {
			earlier = e
		}
		if l := t.Add(gap); sameWall(l, t) {
			later = l
		}
	}
	chosen := earlier
	if !earlier.Equal(later) && earlier.Before(c.last) {
		chosen = later
	}
	c.last = chosen
	return chosen.UTC(), true
}

// sameWall reports whether a and b (both in the clock's location) show the
// same local date and time of day.
func sameWall(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	ah, amin, as := a.Clock()
	bh, bmin, bs := b.Clock()
	return ay == by && am == bm && ad == bd && ah == bh && amin == bmin && as == bs
}

// ParseServerLine parses one line of the Valheim server's stdout log. It is
// stateless: an ambiguous fall-back-hour timestamp resolves to its earlier
// instant. The Watcher uses a per-stream clock instead (clock.serverLine).
func ParseServerLine(line string, loc *time.Location) []Raw {
	return newClock(loc).serverLine(line)
}

// serverLine parses one line of the Valheim server's stdout log, stamping
// it through c. Every timestamped line advances c, even one that yields no
// Raw, so the clock tracks the stream as closely as possible.
func (c *clock) serverLine(line string) []Raw {
	line = strings.TrimRight(line, "\r\n")
	m := serverTS.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	at, ok := c.at("01/02/2006 15:04:05", m[1])
	if !ok {
		return nil
	}
	msg := m[2]
	switch {
	case reVersion.MatchString(msg):
		v := reVersion.FindStringSubmatch(msg)
		return []Raw{{Kind: RawBoot, At: at, Version: v[1], NetVersion: atoi(v[2])}}
	case reReady.MatchString(msg):
		return []Raw{{Kind: RawReady, At: at}}
	case reActive.MatchString(msg):
		v := reActive.FindStringSubmatch(msg)
		return []Raw{{Kind: RawJoinCode, At: at, Code: v[1]}, {Kind: RawPlayers, At: at, Players: atoi(v[2])}}
	case reJoinLost.MatchString(msg):
		v := reJoinLost.FindStringSubmatch(msg)
		out := []Raw{}
		if v[1] != "" {
			out = append(out, Raw{Kind: RawJoinCode, At: at, Code: v[1]})
		}
		return append(out, Raw{Kind: RawPlayers, At: at, Players: atoi(v[2])})
	case reConns.MatchString(msg):
		return []Raw{{Kind: RawPlayers, At: at, Players: atoi(reConns.FindStringSubmatch(msg)[1])}}
	case reSaved.MatchString(msg):
		return []Raw{{Kind: RawSaved, At: at}}
	case reSteamConn.MatchString(msg):
		return []Raw{{Kind: RawSteamConnect, At: at, SteamID: reSteamConn.FindStringSubmatch(msg)[1]}}
	case reSteamClose.MatchString(msg):
		return []Raw{{Kind: RawSteamClose, At: at, SteamID: reSteamClose.FindStringSubmatch(msg)[1]}}
	case reIdentity.MatchString(msg):
		v := reIdentity.FindStringSubmatch(msg)
		return []Raw{{Kind: RawIdentity, At: at, Platform: v[1], PlatformID: v[2]}}
	case reSpawn.MatchString(msg):
		v := reSpawn.FindStringSubmatch(msg)
		uid, _ := strconv.ParseInt(v[2], 10, 64)
		if uid == 0 {
			// "ZDOID from X : 0:0": the game dropped X's character to
			// respawn it, almost always a death. The one common exception,
			// a new character skipping the intro, can't be told apart here:
			// the Sessionizer flags deaths soon after joining as Intro and
			// the app drops those on a character's first session.
			return []Raw{{Kind: RawDeath, At: at, Name: v[1]}}
		}
		return []Raw{{Kind: RawSpawn, At: at, Name: v[1], UID: uid}}
	case reDestroy.MatchString(msg):
		uid, _ := strconv.ParseInt(reDestroy.FindStringSubmatch(msg)[1], 10, 64)
		return []Raw{{Kind: RawDestroy, At: at, UID: uid}}
	case reRaid.MatchString(msg):
		return []Raw{{Kind: RawRaid, At: at, Raid: reRaid.FindStringSubmatch(msg)[1]}}
	case reTimeSkip.MatchString(msg):
		v := reTimeSkip.FindStringSubmatch(msg)
		to, err := strconv.ParseFloat(v[3], 64)
		if err != nil {
			return nil
		}
		return []Raw{{Kind: RawTimeSkip, At: at, To: to}}
	}
	return nil
}

// ParseSupervisorLine parses one line of supervisord.log, keeping only the
// valheim-server stop/exit and spawn events (the game logs nothing on stop).
// Like ParseServerLine it is stateless; the Watcher uses
// clock.supervisorLine.
func ParseSupervisorLine(line string, loc *time.Location) []Raw {
	return newClock(loc).supervisorLine(line)
}

// supervisorLine parses one line of supervisord.log, stamping it through c.
func (c *clock) supervisorLine(line string) []Raw {
	line = strings.TrimRight(line, "\r\n")
	m := superTS.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	at, ok := c.at("2006-01-02 15:04:05", m[1])
	if !ok {
		return nil
	}
	switch {
	case reStopped.MatchString(m[2]):
		return []Raw{{Kind: RawStopped, At: at}}
	case reStarting.MatchString(m[2]):
		return []Raw{{Kind: RawStarting, At: at}}
	}
	return nil
}
