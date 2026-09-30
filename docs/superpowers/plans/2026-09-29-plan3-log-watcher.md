# Farsight Plan 3: Log Watcher Implementation Plan

> **Placeholders:** player names, platform IDs, join codes, IP addresses other than the public join address, and world seeds in this document are invented stand-ins. The real values were removed before the repository was published. Code snippets that assert specific seed names reflect the original private tests, which now check seed length and hash instead.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The farsight-agent sidecar tails the Valheim server log and the supervisor log, and turns them into events (joins and leaves with durations, player count, join code, boot, ready, stop, saves, heartbeat). It POSTs the events in batches to Farsight's ingest API.

**Architecture:**
- `internal/logwatch` parses log lines into raw records.
- A `Sessionizer` pairs those records into sessions using the same rules as the proven `valheim.mtail` program in the cloudcluster repo, for both Steam and crossplay servers.
- A follower tails the newest log file across restarts and supervisor rotation.
- `internal/ingest` is a shared HTTP client (bearer auth, gzip JSON). `internal/agent` gains an event sink with batching, backoff and a heartbeat.
- The binary runs the save watcher and the log watcher side by side.
- A dev CLI replays a log file into events for checking against real logs.

**Tech Stack:** Go, standard library only (`time/tzdata` embedded).

**Spec:** `docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md`. This plan covers the "Log tailer" bullet of the farsight-agent section and the `internal/logwatch` unit, and closes the Plan 1 carry-over in `agent.Tick`. Plan 4 implements the receiving end of the event contract defined here.

## Global Constraints

- Go module `github.com/jumpingmushroom/farsight`, standard library only.
- **Server log:** the newest file matching `{LogDir}/valheim-server-stdout---supervisor-*.log`, where `LogDir` defaults to `/var/log/supervisor`.
  - Lines start with `MM/DD/YYYY HH:MM:SS: ` in the container's local time. The timestamp carries no offset. `FARSIGHT_LOG_TZ` must equal the game container's `TZ` (default `UTC`): `mulevikings` sets `TZ=Europe/Oslo`, the other instances leave it unset and log UTC. (The fixtures below come from `mulevikings`, hence Europe/Oslo in the tests.)
  - Lines without a timestamp (Unity engine output) are ignored.
  - The file persists across the game's nightly in-pod restarts. supervisord rotates it at 50 MB to `….log.1`.
- **Supervisor log:** `{LogDir}/supervisord.log`, with lines like `2026-09-29 05:10:13,203 INFO stopped: valheim-server (exit status 0)` and `… INFO spawned: 'valheim-server' with pid 317320`. It uses the same local timezone. The game server itself logs nothing on shutdown; this file is the only source of stop events.
- **Permissions:** both server log files are root-owned with mode `0600`. The agent therefore runs as uid 0 with all capabilities dropped, a read-only root filesystem and no privilege escalation, exactly like the existing `mtail` sidecar. This amends the spec's "Runs non-root" (Task 6 updates the spec).
- **Event wire contract:** `POST {URL}/ingest/{serverID}/events` with body `{"events":[Event…]}`, gzip, `Authorization: Bearer {token}` and `Content-Type: application/json`.
  - `Event` JSON: `id` (deterministic, used for dedupe on the central side), `type`, `at` (RFC 3339 UTC), and optionally `name`, `platform`, `platformId`, `code`, `players`, `version`, `networkVersion`, `since`, `seconds` and `reason`.
  - Types: `server_starting`, `server_boot`, `server_ready`, `server_stopped`, `join_code`, `players_now`, `player_join`, `player_leave`, `world_saved`, `heartbeat`.
- **Replay:** on start the agent replays both whole files, so a restarted sidecar rebuilds today's state. Event IDs are deterministic, which lets the central app drop duplicates.
- **Commits:** `feat(pkg): …` / `fix(pkg): …` / `docs(spec): …`, each ending with a blank line and then `Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T`.

## File Structure

```
internal/logwatch/parse.go            # line -> []Raw (server log + supervisor log)
internal/logwatch/parse_test.go
internal/logwatch/session.go          # Sessionizer: []Raw -> []Event (mtail pairing rules)
internal/logwatch/session_test.go
internal/logwatch/follow.go           # follower: newest file by glob, rotation, partial lines
internal/logwatch/watcher.go          # Watcher: replay + tail both logs -> Emit(events)
internal/logwatch/watcher_test.go
internal/logwatch/testdata/valheim-crossplay.log   # committed (from cloudcluster fixtures)
internal/logwatch/testdata/valheim-1.0.log         # committed (from cloudcluster fixtures)
internal/ingest/client.go             # shared POST client (gzip JSON, bearer)
internal/ingest/client_test.go
internal/agent/agent.go               # modified: uses ingest.Client; Tick error surfacing fix
internal/agent/events.go              # Sink (batch, backoff, bounded queue) + heartbeat
internal/agent/events_test.go
cmd/farsight-agent/main.go            # modified: runs save + log watchers
cmd/farsight-logreplay/main.go        # dev CLI: log files -> events JSON
```

---

### Task 1: Log line parser

**Files:**
- Create: `internal/logwatch/parse.go`, `internal/logwatch/parse_test.go`
- Test fixtures (already present, commit them in this task): `internal/logwatch/testdata/valheim-crossplay.log`, `internal/logwatch/testdata/valheim-1.0.log`

**Interfaces:**
- Produces:
  ```go
  type RawKind string
  const (
      RawBoot RawKind = "boot"; RawReady = "ready"; RawStopped = "stopped"; RawStarting = "starting"
      RawJoinCode = "join_code"; RawPlayers = "players"; RawSaved = "saved"
      RawSteamConnect = "steam_connect"; RawSteamClose = "steam_close"
      RawIdentity = "identity"; RawSpawn = "spawn"; RawDestroy = "destroy"
  )
  type Raw struct {
      Kind RawKind; At time.Time // UTC
      Version string; NetVersion int
      Code string; Players int
      SteamID string; Name string; UID int64
      Platform, PlatformID string
  }
  func ParseServerLine(line string, loc *time.Location) []Raw
  func ParseSupervisorLine(line string, loc *time.Location) []Raw
  ```

- [ ] **Step 1: Write the failing tests** — `internal/logwatch/parse_test.go`

```go
package logwatch

import (
	"testing"
	"time"
)

func oslo(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func one(t *testing.T, got []Raw) Raw {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("got %d raws, want 1: %+v", len(got), got)
	}
	return got[0]
}

func TestParseServerLines(t *testing.T) {
	loc := oslo(t)
	at := time.Date(2026, 9, 15, 7, 39, 37, 0, time.UTC) // 09:39:37 CEST

	r := one(t, ParseServerLine("09/15/2026 09:39:37: Got character ZDOID from Thorgerdr : 100004242:1", loc))
	if r.Kind != RawSpawn || r.Name != "Thorgerdr" || r.UID != 100004242 || !r.At.Equal(at) {
		t.Fatalf("spawn = %+v", r)
	}
	if got := ParseServerLine("09/15/2026 20:52:53: Got character ZDOID from Thorvaldsson : 0:0", loc); len(got) != 0 {
		t.Fatalf("death line (uid 0) must be ignored: %+v", got)
	}
	r = one(t, ParseServerLine("09/15/2026 09:39:17: PlayFab socket with remote ID playfab/F0000000000000A2 received local Platform ID Steam_76561190000000007", loc))
	if r.Kind != RawIdentity || r.Platform != "Steam" || r.PlatformID != "76561190000000007" {
		t.Fatalf("identity = %+v", r)
	}
	r = one(t, ParseServerLine("09/15/2026 20:54:10: PlayFab socket with remote ID playfab/F0000000000000A1 received local Platform ID PlayStation_1000000000000000001", loc))
	if r.Platform != "PlayStation" || r.PlatformID != "1000000000000000001" {
		t.Fatalf("playstation identity = %+v", r)
	}
	r = one(t, ParseServerLine("09/15/2026 10:54:16: Destroying abandoned non persistent zdo 100004242:12183 owner 100004242", loc))
	if r.Kind != RawDestroy || r.UID != 100004242 {
		t.Fatalf("destroy = %+v", r)
	}
	r = one(t, ParseServerLine("09/15/2026 23:28:56: Destroying abandoned non persistent zdo -100004256:44 owner -100004256", loc))
	if r.UID != -100004256 {
		t.Fatalf("negative uid = %+v", r)
	}
	got := ParseServerLine(`09/15/2026 10:54:16: Player connection lost server "Mulevikings" that has join code 114544, now 0 player(s)`, loc)
	if len(got) != 2 || got[0].Kind != RawJoinCode || got[0].Code != "114544" || got[1].Kind != RawPlayers || got[1].Players != 0 {
		t.Fatalf("connection lost = %+v", got)
	}
	got = ParseServerLine(`09/28/2026 08:40:10: Session "Mulevikings" with join code 110100 and IP 203.0.113.10:2456 is active with 3 player(s)`, loc)
	if len(got) != 2 || got[0].Code != "110100" || got[1].Players != 3 {
		t.Fatalf("session active = %+v", got)
	}
	if got := ParseServerLine(`09/28/2026 08:40:01: New session server "Mulevikings" that has join code , now 0 player(s)`, loc); len(got) != 0 {
		t.Fatalf("empty-code new-session line must be ignored: %+v", got)
	}
	r = one(t, ParseServerLine("09/28/2026 08:39:48: Valheim version: l-1.0.16 (network version 40)", loc))
	if r.Kind != RawBoot || r.Version != "l-1.0.16" || r.NetVersion != 40 {
		t.Fatalf("boot = %+v", r)
	}
	if r := one(t, ParseServerLine("09/28/2026 08:40:00: Game server connected", loc)); r.Kind != RawReady {
		t.Fatalf("ready = %+v", r)
	}
	if got := ParseServerLine("09/28/2026 08:40:00: Game server connected failed", loc); len(got) != 0 {
		t.Fatalf("'connected failed' is not ready: %+v", got)
	}
	for _, l := range []string{
		"09/28/2026 09:09:48: World save (5/5) done. Total time [53ms]",
		"09/18/2026 11:05:52: World saved ( 5183.458ms )",
	} {
		if r := one(t, ParseServerLine(l, loc)); r.Kind != RawSaved {
			t.Fatalf("%q = %+v", l, r)
		}
	}
	if r := one(t, ParseServerLine("09/09/2026 15:43:52:  Connections 1 ZDOS:34937  sent:0 recv:85", loc)); r.Kind != RawPlayers || r.Players != 1 {
		t.Fatalf("connections = %+v", r)
	}
	if r := one(t, ParseServerLine("09/09/2026 15:34:01: Got connection SteamID 76561190000000007", loc)); r.Kind != RawSteamConnect || r.SteamID != "76561190000000007" {
		t.Fatalf("steam connect = %+v", r)
	}
	if r := one(t, ParseServerLine("09/09/2026 15:51:31: Closing socket 76561190000000007", loc)); r.Kind != RawSteamClose || r.SteamID != "76561190000000007" {
		t.Fatalf("steam close = %+v", r)
	}
	for _, l := range []string{"ZPlayFabSocket::Dispose. State: CLOSED", "", "garbage", "09/15/2026 20:37:04: RPC_Disconnect"} {
		if got := ParseServerLine(l, loc); len(got) != 0 {
			t.Fatalf("%q should parse to nothing: %+v", l, got)
		}
	}
}

func TestParseSupervisorLines(t *testing.T) {
	loc := oslo(t)
	r := one(t, ParseSupervisorLine("2026-09-29 05:10:13,203 INFO stopped: valheim-server (exit status 0)", loc))
	if r.Kind != RawStopped || !r.At.Equal(time.Date(2026, 9, 29, 3, 10, 13, 0, time.UTC)) {
		t.Fatalf("stopped = %+v", r)
	}
	if r := one(t, ParseSupervisorLine("2026-09-29 05:10:13,205 INFO spawned: 'valheim-server' with pid 317320", loc)); r.Kind != RawStarting {
		t.Fatalf("spawned = %+v", r)
	}
	if r := one(t, ParseSupervisorLine("2026-09-29 06:00:00,000 INFO exited: valheim-server (exit status 1; not expected)", loc)); r.Kind != RawStopped {
		t.Fatalf("exited = %+v", r)
	}
	for _, l := range []string{
		"2026-09-29 05:10:05,191 INFO waiting for valheim-server to stop",
		"2026-09-29 05:10:13,205 INFO spawned: 'crond' with pid 12",
		"2026-09-29 05:10:23,225 INFO success: valheim-server entered RUNNING state, process has stayed up for > than 10 seconds (startsecs)",
	} {
		if got := ParseSupervisorLine(l, loc); len(got) != 0 {
			t.Fatalf("%q should parse to nothing: %+v", l, got)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/logwatch/`
Expected: FAIL with `undefined: ParseServerLine`.

- [ ] **Step 3: Implement** — `internal/logwatch/parse.go`

```go
// Package logwatch turns Valheim server and supervisor log lines into
// player-session and server-lifecycle events. The line shapes and the pairing
// rules mirror valheim.mtail in the cloudcluster repo, which is proven on the
// live Steam and crossplay servers.
package logwatch

import (
	"regexp"
	"strconv"
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
	reSpawn      = regexp.MustCompile(`^Got character ZDOID from (\S+) : (-?\d+):\d+`)
	reDestroy    = regexp.MustCompile(`^Destroying abandoned non persistent zdo -?\d+:\d+ owner (-?\d+)`)

	superTS    = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}),\d+ INFO (.*)$`)
	reStopped  = regexp.MustCompile(`^(?:stopped|exited): valheim-server \(`)
	reStarting = regexp.MustCompile(`^spawned: 'valheim-server' `)
)

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// ParseServerLine parses one line of the Valheim server's stdout log.
func ParseServerLine(line string, loc *time.Location) []Raw {
	m := serverTS.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	t, err := time.ParseInLocation("01/02/2006 15:04:05", m[1], loc)
	if err != nil {
		return nil
	}
	at, msg := t.UTC(), m[2]
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
		if uid == 0 { // "ZDOID from X : 0:0" is logged on death, not a spawn
			return nil
		}
		return []Raw{{Kind: RawSpawn, At: at, Name: v[1], UID: uid}}
	case reDestroy.MatchString(msg):
		uid, _ := strconv.ParseInt(reDestroy.FindStringSubmatch(msg)[1], 10, 64)
		return []Raw{{Kind: RawDestroy, At: at, UID: uid}}
	}
	return nil
}

// ParseSupervisorLine parses one line of supervisord.log, keeping only the
// valheim-server stop/exit and spawn events (the game logs nothing on stop).
func ParseSupervisorLine(line string, loc *time.Location) []Raw {
	m := superTS.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", m[1], loc)
	if err != nil {
		return nil
	}
	switch {
	case reStopped.MatchString(m[2]):
		return []Raw{{Kind: RawStopped, At: t.UTC()}}
	case reStarting.MatchString(m[2]):
		return []Raw{{Kind: RawStarting, At: t.UTC()}}
	}
	return nil
}
```

- [ ] **Step 4: Run the tests** — `gofmt -w . && go vet ./... && go test ./internal/logwatch/` → `ok`.
- [ ] **Step 5: Commit** (`feat(logwatch): parse Valheim server and supervisor log lines`, adding the two testdata files, with the trailer).

---

### Task 2: Sessionizer, which pairs raw lines into events

**Files:**
- Create: `internal/logwatch/session.go`, `internal/logwatch/session_test.go`

**Interfaces:**
- Consumes: `Raw`, `ParseServerLine`, `ParseSupervisorLine`.
- Produces:
  ```go
  type Event struct {
      ID string `json:"id"`; Type string `json:"type"`; At time.Time `json:"at"`
      Name string `json:"name,omitempty"`; Platform string `json:"platform,omitempty"`; PlatformID string `json:"platformId,omitempty"`
      Code string `json:"code,omitempty"`; Players *int `json:"players,omitempty"`
      Version string `json:"version,omitempty"`; NetworkVersion int `json:"networkVersion,omitempty"`
      Since *time.Time `json:"since,omitempty"`; Seconds int64 `json:"seconds,omitempty"`; Reason string `json:"reason,omitempty"`
  }
  const (EvServerStarting = "server_starting"; EvServerBoot = "server_boot"; EvServerReady = "server_ready"
         EvServerStopped = "server_stopped"; EvJoinCode = "join_code"; EvPlayersNow = "players_now"
         EvPlayerJoin = "player_join"; EvPlayerLeave = "player_leave"; EvWorldSaved = "world_saved"; EvHeartbeat = "heartbeat")
  type Sessionizer struct{ /* unexported state */ }
  func NewSessionizer() *Sessionizer
  func (s *Sessionizer) Feed(r Raw) []Event
  func EventID(e Event) string // deterministic; Feed sets ID on every event it returns
  ```
- **Rules.** These mirror `valheim.mtail`, whose comments explain the trade-offs.
  - `identity`: set both `pending` and `last` to (platform, id).
  - `steam_connect(sid)`: `connect[sid]=At`, `lastSID=sid`.
  - `spawn(name, uid)`:
    - *Crossplay mode* (any identity seen, i.e. `last` is non-empty): if session `uid` is already open, ignore it (a respawn). Otherwise take `pending`, falling back to `last`, then clear `pending`. Open session key `u:{uid}` with name, platform, platformId and `since=At`, and emit `player_join`.
    - *Steam mode*: if `lastSID` has a connect time and no open session `s:{lastSID}`, open it with platform `Steam`, platformId `lastSID` and `since=At`, and emit `player_join`. If it is already open, ignore the spawn (a respawn).
  - `steam_close(sid)`: if session `s:{sid}` is open, emit `player_leave` with `reason:"left"`, `since` and `seconds = At − since` (whole seconds). Always delete the connect and session state for `sid`.
  - `destroy(uid)`: if session `u:{uid}` is open, emit `player_leave` (`reason:"left"`) and delete it. Otherwise do nothing; many Destroying lines arrive per leave.
  - `stopped`, `starting` and `boot`: first emit `player_leave` with `reason:"server_stopped"` for every open session, in session-open order, at this raw's `At`. Then clear all state (connects, sessions, pending, last, lastSID). Then emit `server_stopped`, `server_starting` or `server_boot` (with version and networkVersion) respectively.
  - `ready` becomes `server_ready`; `join_code` becomes `join_code{code}`; `players` becomes `players_now{players}`; `saved` becomes `world_saved`.
- **IDs:** `EventID` is the hex of the first 8 bytes of `sha256(type|at.UnixNano|name|platformId|code|players|version|reason)`. Replaying the same file yields the same IDs, and identical facts in the same second collapse, which is intended.

- [ ] **Step 1: Write the failing tests** — `internal/logwatch/session_test.go`

```go
package logwatch

import (
	"bufio"
	"os"
	"testing"
	"time"
)

func feedFile(t *testing.T, path string) []Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	loc := oslo(t)
	s := NewSessionizer()
	var out []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		for _, r := range ParseServerLine(sc.Text(), loc) {
			out = append(out, s.Feed(r)...)
		}
	}
	return out
}

func ofType(evs []Event, typ string) []Event {
	var out []Event
	for _, e := range evs {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func utc(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }

func TestCrossplayFixtureSessions(t *testing.T) {
	evs := feedFile(t, "testdata/valheim-crossplay.log")
	joins, leaves := ofType(evs, EvPlayerJoin), ofType(evs, EvPlayerLeave)
	if len(joins) != 2 || len(leaves) != 2 {
		t.Fatalf("joins=%+v leaves=%+v", joins, leaves)
	}
	j0, l0, j1, l1 := joins[0], leaves[0], joins[1], leaves[1]
	if j0.Name != "Thorgerdr" || j0.Platform != "Steam" || j0.PlatformID != "76561190000000007" || !j0.At.Equal(utc("2026-09-15T07:39:37Z")) {
		t.Fatalf("join 0 = %+v", j0)
	}
	if l0.Name != "Thorgerdr" || l0.Seconds != 4479 || l0.Reason != "left" || !l0.At.Equal(utc("2026-09-15T08:54:16Z")) {
		t.Fatalf("leave 0 = %+v", l0)
	}
	if j1.Name != "Thorvaldsson" || j1.PlatformID != "76561190000000003" || !j1.At.Equal(utc("2026-09-15T18:51:41Z")) {
		t.Fatalf("join 1 = %+v", j1)
	}
	if l1.Name != "Thorvaldsson" || l1.Seconds != 125 {
		t.Fatalf("leave 1 = %+v", l1)
	}
	codes := map[string]bool{}
	for _, e := range ofType(evs, EvJoinCode) {
		codes[e.Code] = true
	}
	if !codes["114544"] || !codes["113433"] || len(codes) != 2 {
		t.Fatalf("join codes = %v", codes)
	}
	pn := ofType(evs, EvPlayersNow)
	if len(pn) == 0 || *pn[len(pn)-1].Players != 0 {
		t.Fatalf("last players_now = %+v", pn[len(pn)-1])
	}
	for _, e := range evs {
		if e.ID == "" || len(e.ID) != 16 {
			t.Fatalf("event without a 16-hex id: %+v", e)
		}
	}
}

func TestSteamFixtureSessionAndBoot(t *testing.T) {
	evs := feedFile(t, "testdata/valheim-1.0.log")
	joins, leaves := ofType(evs, EvPlayerJoin), ofType(evs, EvPlayerLeave)
	if len(joins) != 1 || joins[0].Name != "Orm" || joins[0].Platform != "Steam" || joins[0].PlatformID != "76561190000000007" {
		t.Fatalf("joins = %+v", joins)
	}
	if len(leaves) != 1 || leaves[0].Seconds != 1011 || leaves[0].Reason != "left" {
		t.Fatalf("leaves = %+v", leaves)
	}
	boots := ofType(evs, EvServerBoot)
	if len(boots) != 1 || boots[0].Version != "l-1.0.7" || boots[0].NetworkVersion != 39 {
		t.Fatalf("boots = %+v", boots)
	}
	if len(ofType(evs, EvWorldSaved)) == 0 {
		t.Fatal("world_saved missing")
	}
}

func TestStopClosesOpenSessionsAndRespawnIsIgnored(t *testing.T) {
	s := NewSessionizer()
	at := func(sec int) time.Time { return time.Date(2026, 9, 29, 10, 0, sec, 0, time.UTC) }
	var evs []Event
	evs = append(evs, s.Feed(Raw{Kind: RawIdentity, At: at(0), Platform: "Steam", PlatformID: "1"})...)
	evs = append(evs, s.Feed(Raw{Kind: RawSpawn, At: at(5), Name: "A", UID: 11})...)
	evs = append(evs, s.Feed(Raw{Kind: RawSpawn, At: at(9), Name: "A", UID: 11})...) // respawn
	evs = append(evs, s.Feed(Raw{Kind: RawIdentity, At: at(10), Platform: "Xbox", PlatformID: "2"})...)
	evs = append(evs, s.Feed(Raw{Kind: RawSpawn, At: at(12), Name: "B", UID: 22})...)
	evs = append(evs, s.Feed(Raw{Kind: RawStopped, At: at(30)})...)
	evs = append(evs, s.Feed(Raw{Kind: RawDestroy, At: at(31), UID: 11})...) // after stop: nothing
	if j := ofType(evs, EvPlayerJoin); len(j) != 2 || j[1].Platform != "Xbox" {
		t.Fatalf("joins = %+v", j)
	}
	l := ofType(evs, EvPlayerLeave)
	if len(l) != 2 || l[0].Name != "A" || l[0].Reason != "server_stopped" || l[0].Seconds != 25 || l[1].Name != "B" || l[1].Seconds != 18 {
		t.Fatalf("leaves = %+v", l)
	}
	if last := evs[len(evs)-1]; last.Type != EvServerStopped {
		t.Fatalf("last event = %+v, want server_stopped", last)
	}
}

func TestEventIDDeterministic(t *testing.T) {
	a := feedFile(t, "testdata/valheim-crossplay.log")
	b := feedFile(t, "testdata/valheim-crossplay.log")
	if len(a) != len(b) {
		t.Fatal("replay length differs")
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("event %d id differs across replays", i)
		}
	}
}
```

Expected values come from reading the fixture by hand:
- Thorgerdr spawns at 09:39:37 and is destroyed at 10:54:16 CEST, a gap of 4479 s.
- Thorvaldsson spawns at 20:51:41 and is destroyed at 20:53:46, a gap of 125 s. Their `0:0` death line and the respawn are ignored.
- The PlayStation identity at 20:54:10 never spawns in the fixture.
- The Destroying lines at 23:28:56 carry a uid that spawned before the fixture starts, so they are ignored.
- Orm's Steam session runs from ZDOID at 15:34:40 to close at 15:51:31, which is 1011 s. Sessions are timed from spawn, not from connect.

- [ ] **Step 2: Run to verify it fails.** Then implement `session.go` to the rules above. Keep open sessions in an ordered slice of keys plus a map, so that stop-closing is deterministic.
- [ ] **Step 3: Run** `gofmt -w . && go vet ./... && go test ./internal/logwatch/ -v`. Expected: PASS.
- [ ] **Step 4: Commit** (`feat(logwatch): pair log lines into player sessions (mtail rules)`, with the trailer).

---

### Task 3: Follower and Watcher, tailing both logs across restarts and rotation

**Files:**
- Create: `internal/logwatch/follow.go`, `internal/logwatch/watcher.go`, `internal/logwatch/watcher_test.go`

**Interfaces:**
- Consumes: the parsers and `Sessionizer`.
- Produces:
  ```go
  type Watcher struct {
      Dir  string          // e.g. /var/log/supervisor
      Loc  *time.Location  // the game container's TZ (FARSIGHT_LOG_TZ)
      Poll time.Duration   // 0 => 1s
      Emit func([]Event)   // called from the Run goroutine only
      Log  *slog.Logger    // may be nil
  }
  func (w *Watcher) Run(ctx context.Context) error // returns ctx.Err() on cancel
  ```
- **Behaviour:**
  - The server log path is the newest file (by mtime) matching `valheim-server-stdout---supervisor-*.log`, re-resolved on every poll. The supervisor log is `supervisord.log`. A missing file is not an error: it's logged at debug level once and retried every poll.
  - **Replay.** On the first poll that finds files, read both files from offset 0 and parse every complete line. Stable-sort the parsed raws by `At`, feed them through one `Sessionizer`, and `Emit` the result in one call. This order matters, because a supervisor `stopped` must close sessions opened earlier in the server log.
  - **Live.** After replay, each poll reads newly appended bytes from each file, parses complete lines and feeds them in file order. Server-log raws go first, then supervisor raws; that interleaving is acceptable at 1 s granularity. It then emits once per poll if there is anything to emit.
  - **Partial lines** (no trailing `\n`) stay buffered until completed.
  - **Rotation and restart.** Track `(os.SameFile identity, offset)` per role. If the resolved path's file is no longer the same file (a new random suffix, or a rename to `.1`), or its size is below the offset (truncation), reopen and read the new file from 0. Its lines are appended in the live path; they are not re-replayed with sorting. Keep reading the old handle to EOF before switching, so nothing is lost when the file is renamed.
  - The Sessionizer lives for the lifetime of the Watcher. There is no persistence; a restarted agent replays.

- [ ] **Step 1: Write the failing tests** — `internal/logwatch/watcher_test.go`

```go
package logwatch

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type collector struct {
	mu  sync.Mutex
	evs []Event
}

func (c *collector) emit(e []Event) { c.mu.Lock(); c.evs = append(c.evs, e...); c.mu.Unlock() }
func (c *collector) count(typ string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.evs {
		if e.Type == typ {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within 5s")
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func startWatcher(t *testing.T, dir string) (*collector, context.CancelFunc) {
	t.Helper()
	c := &collector{}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{Dir: dir, Loc: oslo(t), Poll: 20 * time.Millisecond, Emit: c.emit}
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return c, cancel
}

func TestWatcherReplayLiveRotationAndSupervisor(t *testing.T) {
	dir := t.TempDir()
	srv := filepath.Join(dir, "valheim-server-stdout---supervisor-aaaa.log")
	sup := filepath.Join(dir, "supervisord.log")
	appendLine(t, srv, "09/29/2026 10:00:00: PlayFab socket with remote ID playfab/X received local Platform ID Steam_1\n")
	appendLine(t, srv, "09/29/2026 10:00:05: Got character ZDOID from A : 11:1\n")
	appendLine(t, sup, "2026-09-29 09:00:00,000 INFO spawned: 'valheim-server' with pid 1\n")

	c, _ := startWatcher(t, dir)
	waitFor(t, func() bool { return c.count(EvPlayerJoin) == 1 && c.count(EvServerStarting) == 1 })

	// Live append, written in two parts to exercise partial-line buffering.
	appendLine(t, srv, "09/29/2026 10:01:00: Got character ZDOID from B : 22:1\n09/29/2026 10:02:00: Destroying abandoned non persistent zdo 11:5 ")
	time.Sleep(100 * time.Millisecond)
	if c.count(EvPlayerLeave) != 0 {
		t.Fatal("a partial line must not be parsed")
	}
	appendLine(t, srv, "owner 11\n")
	waitFor(t, func() bool { return c.count(EvPlayerLeave) == 1 })

	// Supervisor stop closes the remaining session (B).
	appendLine(t, sup, "2026-09-29 10:10:00,000 INFO stopped: valheim-server (exit status 0)\n")
	waitFor(t, func() bool { return c.count(EvPlayerLeave) == 2 && c.count(EvServerStopped) == 1 })

	// The server restarts under a new supervisor log name (new random suffix, newer mtime).
	time.Sleep(20 * time.Millisecond)
	srv2 := filepath.Join(dir, "valheim-server-stdout---supervisor-bbbb.log")
	appendLine(t, srv2, "09/29/2026 10:11:00: Valheim version: l-1.0.16 (network version 40)\n")
	waitFor(t, func() bool { return c.count(EvServerBoot) == 1 })
}

func TestWatcherMissingFilesThenAppear(t *testing.T) {
	dir := t.TempDir()
	c, _ := startWatcher(t, dir)
	time.Sleep(60 * time.Millisecond)
	appendLine(t, filepath.Join(dir, "valheim-server-stdout---supervisor-cccc.log"), "09/29/2026 10:00:00: Game server connected\n")
	waitFor(t, func() bool { return c.count(EvServerReady) == 1 })
}
```

- [ ] **Step 2: Run to verify it fails.** Then implement `follow.go` (a per-role follower with `resolve func() (string, error)`, `readNew() ([]string, error)`, same-file and truncation detection, and a partial-line buffer) and `watcher.go` (replay, then the live loop, per the behaviour above).
- [ ] **Step 3: Run** `gofmt -w . && go vet ./... && go test ./internal/logwatch/ -count=3 -v`. Expected: PASS three times, with no flakes.
- [ ] **Step 4: Commit** (`feat(logwatch): follow server and supervisor logs across restarts`, with the trailer).

---

### Task 4: Shared ingest client, and the agent's Plan 1 carry-over fix

**Files:**
- Create: `internal/ingest/client.go`, `internal/ingest/client_test.go`
- Modify: `internal/agent/agent.go` (use `ingest.Client` for the snapshot POST; carry-over fix), `internal/agent/agent_test.go` (a test for the fix)

**Interfaces:**
- Produces:
  ```go
  package ingest
  type Client struct { /* unexported */ }
  func New(baseURL, serverID, token string) *Client       // trims trailing "/" from baseURL
  func (c *Client) Post(ctx context.Context, kind string, v any) error
  // POST {baseURL}/ingest/{serverID}/{kind}, JSON-encoded v, gzip body,
  // headers Authorization: Bearer, Content-Type: application/json, Content-Encoding: gzip.
  // 60 s client timeout. Non-2xx => error "status N: <first 512 bytes of body>".
  // On 2xx the body is drained before close.
  ```
- **Agent refactor.** `Agent` holds an `*ingest.Client` built from `Config.URL/ServerID/Token`, and `post` becomes `a.ingest.Post(ctx, "snapshot", snap)`. All existing agent tests must pass unchanged. They already assert the exact path, headers and gzip.
- **Carry-over fix (from the Plan 1 final review, parked).** In `Tick`, while a pending snapshot exists, an unexpected `LatestSave` error (anything other than `ErrNoSave` or `ErrSaveInProgress`) is currently swallowed. It must be logged at error level (once per distinct error string, to avoid log spam) before the pending POST is retried. Add a test: arrange a pending snapshot and a `LatestSave` failure, for example by making the worlds dir unreadable or by pointing it at a regular file. Assert that the log output contains the error and that the pending retry still happens.

- [ ] **Step 1: Write the failing tests** — `internal/ingest/client_test.go`

```go
package ingest

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostGzipJSONWithBearer(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ingest/mv/events" || r.Header.Get("Authorization") != "Bearer tok" ||
			r.Header.Get("Content-Encoding") != "gzip" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("bad request %s %v", r.URL.Path, r.Header)
		}
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		b, _ := io.ReadAll(zr)
		json.Unmarshal(b, &got)
	}))
	defer srv.Close()
	c := New(srv.URL+"/", "mv", "tok")
	if err := c.Post(context.Background(), "events", map[string]any{"events": []int{1, 2}}); err != nil {
		t.Fatal(err)
	}
	if len(got["events"].([]any)) != 2 {
		t.Fatalf("body = %v", got)
	}
}

func TestPostNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "bad token")
	}))
	defer srv.Close()
	err := New(srv.URL, "mv", "x").Post(context.Background(), "events", 1)
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "bad token") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails.** Implement `client.go`, refactor the agent, and add the carry-over test and fix.
- [ ] **Step 3: Run** `gofmt -w . && go vet ./... && go test ./internal/ingest/ ./internal/agent/ -v`. Expected: PASS, with every pre-existing agent test unchanged.
- [ ] **Step 4: Commit** (`feat(ingest): shared gzip JSON client; fix(agent): surface save errors while a POST retry is pending`, or two commits, each with the trailer).

---

### Task 5: Event sink, heartbeat, and running both watchers in the agent

**Files:**
- Create: `internal/agent/events.go`, `internal/agent/events_test.go`
- Modify: `cmd/farsight-agent/main.go`

**Interfaces:**
- Consumes: `ingest.Client`, `logwatch.Event`, `logwatch.Watcher`, `logwatch.EventID`.
- Produces:
  ```go
  type Sink struct { /* unexported */ }
  type SinkConfig struct {
      Flush     time.Duration // 0 => 5s: send at most this often
      MaxBatch  int           // 0 => 500 events per POST
      MaxQueue  int           // 0 => 20000; beyond it the oldest events are dropped (counted, logged)
      Heartbeat time.Duration // 0 => 60s; a heartbeat event is queued at this interval
  }
  func NewSink(c *ingest.Client, cfg SinkConfig, log *slog.Logger) *Sink
  func (s *Sink) Add(evs []logwatch.Event)   // safe for concurrent use; never blocks on the network
  func (s *Sink) Run(ctx context.Context) error // flush loop; on ctx cancel does one final best-effort flush (2 s timeout) then returns ctx.Err()
  ```
- **Behaviour:**
  - Every `Flush` interval, if the queue is non-empty, POST up to `MaxBatch` events as `{"events":[…]}` to kind `"events"`. On success, remove those events from the queue. On failure, keep them and back off: the delay starts at `Flush`, doubles, is capped at 5 min, and resets on success.
  - Heartbeats are `logwatch.Event{Type: EvHeartbeat, At: now}` with `ID = logwatch.EventID(ev)`.
  - Order is preserved.
- **`cmd/farsight-agent`:**
  - New env: `FARSIGHT_LOG_DIR` (default `/var/log/supervisor`) and `FARSIGHT_LOG_TZ` (default `UTC`; must equal the game container's `TZ`; loaded with `time.LoadLocation`; exit 2 if it's invalid). Setting `FARSIGHT_LOG_DIR=off` disables the log watcher.
  - Build one `ingest.Client` shared by the save watcher and the sink.
  - Run the save agent, the `logwatch.Watcher` (with `Emit: sink.Add`) and `sink.Run` concurrently under one `signal.NotifyContext`, using a `sync.WaitGroup` with standard library only. If any of them returns a non-context error, log it and cancel the others. The process exits 1 on error and 0 on a clean signal.

- [ ] **Step 1: Write the failing tests** — `internal/agent/events_test.go`

```go
package agent

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/ingest"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
)

type eventSink struct {
	mu     sync.Mutex
	got    []logwatch.Event
	status atomic.Int32
}

func (s *eventSink) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if st := s.status.Load(); st != 0 {
			w.WriteHeader(int(st))
			return
		}
		zr, _ := gzip.NewReader(r.Body)
		b, _ := io.ReadAll(zr)
		var body struct{ Events []logwatch.Event }
		if err := json.Unmarshal(b, &body); err != nil {
			t.Error(err)
		}
		s.mu.Lock()
		s.got = append(s.got, body.Events...)
		s.mu.Unlock()
	}
}

func (s *eventSink) n(typ string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := 0
	for _, e := range s.got {
		if typ == "" || e.Type == typ {
			c++
		}
	}
	return c
}

func eventually(t *testing.T, f func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if f() {
			return
		}
	}
	t.Fatal("timed out")
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSinkBatchesRetriesAndHeartbeats(t *testing.T) {
	es := &eventSink{}
	es.status.Store(http.StatusServiceUnavailable)
	srv := httptest.NewServer(es.handler(t))
	defer srv.Close()
	s := NewSink(ingest.New(srv.URL, "mv", "tok"), SinkConfig{Flush: 20 * time.Millisecond, MaxBatch: 2, Heartbeat: 50 * time.Millisecond}, quiet())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	var evs []logwatch.Event
	for i := 0; i < 5; i++ {
		e := logwatch.Event{Type: logwatch.EvWorldSaved, At: time.Unix(int64(1000+i), 0).UTC()}
		e.ID = logwatch.EventID(e)
		evs = append(evs, e)
	}
	s.Add(evs)
	time.Sleep(100 * time.Millisecond) // server failing: nothing delivered, nothing lost
	if es.n("") != 0 {
		t.Fatal("delivered while server was failing")
	}
	es.status.Store(0)
	eventually(t, func() bool { return es.n(logwatch.EvWorldSaved) == 5 && es.n(logwatch.EvHeartbeat) >= 1 })
	es.mu.Lock()
	var saved []logwatch.Event
	for _, e := range es.got {
		if e.Type == logwatch.EvWorldSaved {
			saved = append(saved, e)
		}
	}
	es.mu.Unlock()
	for i := range saved {
		if saved[i].ID != evs[i].ID {
			t.Fatalf("order not preserved at %d", i)
		}
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("Run returned %v", err)
	}
}

func TestSinkDropsOldestBeyondMaxQueue(t *testing.T) {
	s := NewSink(ingest.New("http://127.0.0.1:1", "mv", "t"), SinkConfig{MaxQueue: 3}, quiet())
	for i := 0; i < 5; i++ {
		s.Add([]logwatch.Event{{Type: logwatch.EvPlayersNow, ID: string(rune('a' + i))}})
	}
	if q := s.queued(); len(q) != 3 || q[0].ID != "c" || q[2].ID != "e" {
		t.Fatalf("queue = %+v", q)
	}
}
```

The test uses an unexported `func (s *Sink) queued() []logwatch.Event` that returns a copy of the queue. Add it in `events.go`.

- [ ] **Step 2: Run to verify it fails.** Implement `events.go`, then update `cmd/farsight-agent/main.go` per the Interfaces above.
- [ ] **Step 3: Run** `gofmt -w . && go vet ./... && go test ./... && go build ./cmd/...`. Expected: all ok.
- [ ] **Step 4: Commit** (`feat(agent): batched event sink with heartbeat; run save and log watchers together`, with the trailer).

---

### Task 6: Replay CLI, check against live logs, and spec amendments

**Files:**
- Create: `cmd/farsight-logreplay/main.go`, `hack/pull-logs.sh`
- Modify: `docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md`

**Interfaces:**
- Produces: `farsight-logreplay -dir DIR [-tz UTC]`. It runs a `logwatch.Watcher` on DIR, stops once replay has emitted (a one-shot mode: add `Once bool` to `Watcher`, meaning replay then return nil), and prints the events as JSON lines to stdout. On stderr it prints a summary: counts by type, distinct players, the last join code and the last players_now.

- [ ] **Step 1: Add `Watcher.Once`, with a test in `watcher_test.go`.** With `Once: true`, `Run` returns nil after the replay emit, and the collector holds the replayed events.
- [ ] **Step 2: Write `hack/pull-logs.sh`.** For a given instance (default `mulevikings`), it copies `/var/log/supervisor/valheim-server-stdout---supervisor-*.log` and `supervisord.log` from `deploy/<instance>-valheim -c valheim` into `testdata-golden/logs/<instance>/`, which is gitignored. Use `kubectl exec … cat`, read-only.
- [ ] **Step 3: Implement the CLI and run it against two live servers:**
  ```bash
  hack/pull-logs.sh mulevikings && hack/pull-logs.sh mulevikings-old
  go run ./cmd/farsight-logreplay -dir testdata-golden/logs/mulevikings > /tmp/mv-events.jsonl
  go run ./cmd/farsight-logreplay -dir testdata-golden/logs/mulevikings-old > /tmp/mo-events.jsonl
  ```
  Expected, on stderr for each: `server_boot`, `server_ready` and nightly `server_stopped`/`server_starting` pairs, one per day the pod has been up; `world_saved` about every 30 min; and joins and leaves with plausible names and durations. mulevikings also has `join_code` events with 6-digit codes. Record both summaries in the report. Spot-check one player's sessions for mulevikings against the Prometheus `valheim_player_session_seconds_total` if it's reachable. This is optional, so write "not checked" if it isn't.
- [ ] **Step 4: Amend the spec** (`docs(spec): …` commit), in the farsight-agent section:
  - Replace "Runs non-root" with: "runs as uid 0 with all capabilities dropped, `readOnlyRootFilesystem` and no privilege escalation, like the existing mtail sidecar. The supervisor log files are root-owned `0600`."
  - Change the log tailer bullet:
    - It follows both `valheim-server-stdout---supervisor-*.log` and `supervisord.log`.
    - Stop events come from supervisord (`stopped:`/`exited:` becomes `server_stopped`; `spawned:` becomes `server_starting`), because the game logs nothing on stop.
    - Log timestamps are container-local: `FARSIGHT_LOG_TZ` must equal the game container's `TZ` (default `UTC`; `mulevikings` sets Europe/Oslo, the others use UTC).
    - Sessions are timed from character spawn.
    - Replace the event list with the final types: `server_starting`, `server_boot{version,networkVersion}`, `server_ready`, `server_stopped`, `join_code{code}`, `players_now{players}`, `player_join{name,platform,platformId}`, `player_leave{name,platform,platformId,since,seconds,reason}`, `world_saved`, `heartbeat`.
    - Document the body `{"events":[…]}` and that the central side dedupes by `id`.
- [ ] **Step 5: Run** `gofmt -w . && go vet ./... && go test ./... && go build ./cmd/...`. Commit (`feat(cli): farsight-logreplay; hack/pull-logs.sh`, then the spec commit, each with the trailer). Put the two stderr summaries in the first commit's body.

---

## After this plan

- Plan 4 implements `POST /ingest/{server}/events`. It must be idempotent on `(serverId, event id)`. It turns `player_join` and `player_leave` into sessions and live state, and it treats a missing heartbeat for 3 minutes as the server being offline.
- Plan 5 runs the sidecar as uid 0 with all capabilities dropped, mounts `supervisor-logs` read-only, and sets `FARSIGHT_LOG_TZ` from the pod's `TZ`.
