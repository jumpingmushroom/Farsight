# Farsight Plan 7 — Player Profiles and the Activity Timeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** "Profile →" on every Online and Recently online row opens a player's profile (playtime, the last 7 days, first and last seen, beds, bases, portals, named tames, deaths), and "Full timeline →" opens a filterable, day-grouped Activity view of server-log and world-save events back to when tracking began.

**Architecture:**
- **Agent:** `extract` adds a portal's `owner` (its creator resolved to a name) and a tame's `namer` (`TamedNameAuthor`); `logwatch` turns "Random event set:" lines into `event_raid` events.
- **Central app, world events:** a new `worldevents` package diffs each stored snapshot against the previous one into `world_*` events in the existing `events` table, plus a `tombstones` table for death history. It runs on every snapshot ingest and once per server at startup (the backfill).
- **Central app, endpoints:** three new endpoints (profile, activity, today's sessions) behind the usual unlock check. Local days use a new per-server `timeZone`, and the card's `activity` takes the timeline's entry shape.
- **Browser:**
  - The profile replaces the desktop side panel; the Activity view takes its place at 520 px. On mobile both are full-height sheets.
  - The open view is mirrored in the URL hash (`#p=`, `#activity`), so browser back closes it.
  - Dates use the server's zone.
  - The timeline's filters are shared with the side panel's short list.

**Tech Stack:** Go 1.27 standard library only (`database/sql` on modernc SQLite, `encoding/json`, `crypto/sha256`; no new modules), SvelteKit 2 / Svelte 5 / Leaflet 1.9.4 with lucide-svelte (no new npm packages; `Intl.DateTimeFormat` for time zones), Vitest 5, Playwright 1.63.

**Spec:** `docs/superpowers/specs/2026-10-05-player-profile-design.md` and `docs/superpowers/specs/2026-10-05-activity-timeline-design.md`, implemented together. Design source: `design/World Atlas.dc.html` `:116-185` (desktop profile aside), `:636-745` (mobile profile), `:837-908` (activity timeline), and `design/DESIGN-NOTES.md` (tokens, components; §1.5, §1.8, §3.7, §3.8).

## Global Constraints

- **The Farsight repo is PUBLIC.** No real player data goes into fixtures or tests: no player or world names from the real saves, no seeds, no positions copied from them. Fixtures are synthetic (`savetest`, hand-written snapshots and events). Golden tests read the gitignored `testdata-golden/` at run time only, assert counts and invariants, and `t.Skip` when the directory is absent.
- **Never commit `reference/` or `testdata-golden/`** (nor `web/build`, `web/node_modules`, `web/test-results`). Never quote the decompiled game code from `reference/` in code, comments or commits.
- **Branch:** all work happens on `feat/profile-timeline` in `/workspace/Farsight` (cut from `public-main`; it already holds the two specs). **Never push**, and never deploy.
- **Commits:** `git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit`, Conventional Commits with a scope as in the history (`feat(server): …`). Every message ends with a blank line, then `Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T`. Stage the listed paths only (`git add <paths>`); never `git add -A` or `git add .`. After each commit `git status --short` prints nothing.
- **Commands:**
  - Go: `PATH=$HOME/.local/go/bin:$PATH go test ./...` from the repo root. There is no gcc here, so no `-race`, but CI runs `-race`: tests must be free of data races.
  - Web: `cd web && npm run check && npx vitest run`.
  - e2e: `cd web && PATH=$HOME/.local/go/bin:$PATH LD_LIBRARY_PATH="$(../hack/playwright-libs.sh --print)" FONTCONFIG_FILE="$(../hack/playwright-libs.sh --fontconfig)" npx playwright test`. `global-setup.ts` builds the web UI and both binaries itself.
  - Formatting: `PATH=$HOME/.local/go/bin:$PATH gofmt -l ./cmd ./internal ./web` prints nothing after every task. Every Go block in this plan is already gofmt-formatted.
  - `make` is not installed; don't use it.
- **Deploy order (specs' "Rollout"), for whoever deploys after this plan:**
  1. **Central app first.** It restarts only the Farsight pod.
     - Profiles go live except portals and tames ("None yet."), and the timeline except raids.
     - On start it backfills world events from every stored snapshot. Snapshots are pruned after 14 days, so deploy before **14 Oct 2026** to keep the history from 30 Sep. From this release on, events other than heartbeats and players_now are never pruned.
     - In the same change as the image bump (or after it, never before: the old app rejects unknown config fields), set `"timeZone": "Europe/Oslo"` on the mulevikings server in `farsight.json`, matching its agent's `FARSIGHT_LOG_TZ`. The other servers log in UTC, the default.
  2. **Agent second.** The sidecar image change (portal `owner`, tame `namer`, raid events) restarts the game servers, so it **needs the owner's go-ahead**. Portals and tames then fill in from the next save, and the Raids chip from the next raid. Matching never uses owner or namer, so the update itself reports no new portals or tames.
- **Game facts (verified; don't re-derive):**
  - Valheim 1.0 has eight bosses, the last being Kall Fimbulbringer (`defeated_frozenking`). `extract.BossesFromKeys` is the source of truth; the card rebuilds bosses from the snapshot's GlobalKeys (`cardBosses`) and the diff does the same.
  - A tame's `TamedNameAuthor` is the namer's platform user ID as the server log prints it (`Steam_7656…`), or "host" when the namer wasn't signed in to a platform.
  - A portal's `creator` is the player ID that beds and tombstones carry as `owner`; the save's player table maps it to a name.
  - The server log line is `MM/DD/YYYY HH:MM:SS: Random event set:<name>`; it is also logged again when a saved event resumes after a restart.
- **History starts 30 Sep 2026**, when tracking began. Nothing hard-codes that date: "tracked since" and "Tracking began" come from the server's earliest stored event.
- **Wire contract (exact names, shared by every task):**
  - Event types: log `player_join`, `player_leave`, `server_starting`, `server_boot`, `server_ready`, `server_stopped`, `world_saved`, `join_code`, `event_raid`; world `world_tombstone`, `world_portal`, `world_portal_paired`, `world_tame`, `world_base_new`, `world_base_grew`, `world_boss`. Categories (the chips, in this order): `session`, `death`, `boss`, `build`, `portal`, `tame`, `event`, `server`.
  - `GET /api/servers/{id}/players/{player}` (`{player}` = the sessions' `platformId`), `GET /api/servers/{id}/activity?before=<rfc3339>&days=3` (1–14), `GET /api/servers/{id}/sessions/today`. All 404 for a locked or unknown server; the profile also for an unknown player.
  - Card JSON gains `timeZone`; `activity[]` entries are the timeline's `eventJSON` (Task 6), with the old fields under the same names.
  - Snapshot markers: `owner` on portals, `namer` on tames.
  - Browser hash: `#s=<server>&p=<playerId>` (profile) and `#s=<server>&activity` (timeline).
  - Config: per-server `timeZone` (IANA, default `UTC`).
- **Design rules:** the "Profile →" ghost button uses the body font, bold 12 px, `--cold-ink`. "Full timeline →" stays in Caprasimo (`.btn`, no override), as the design has it. "Reset filters" uses the body font. Discs and colours follow DESIGN-NOTES §2.3 (ember = accent-100/accent-600, sage = accent-2-100/accent-2-700, cold = `--cold` 22 %). Mobile touch targets are at least 40–44 px.

## File Structure

```
internal/extract/{snapshot,extract}.go (+tests)          # T1: Marker.Namer; portal Owner from creator
internal/logwatch/{parse,session}.go (+tests)            # T2: RawRaid, EvRaid ("event_raid"), Event.Raid
internal/live/apply.go (+test)                           # T2: the app accepts event_raid
internal/store/store.go                                  # T3: schema v2 (world_diff, tombstones, sessions index)
internal/store/events.go                                 # T3: StoredEvent, RecentEvents, EventsBetween, EarliestEvent, narrower prune
internal/store/sessions.go                               # T3: PlayerSessions, SessionsOverlapping, Players
internal/store/worlddiff.go (+tests)                     # T3: SnapshotKey, diff state, SnapshotsAfter, Snapshot, tombstones
internal/worldevents/{event,geo,diff,deriver}.go (+tests) # T4: Diff, Near, NewGeo, Deriver.CatchUp
internal/server/{server,ingest}.go, cmd/farsight/serve.go  # T4: CatchUp on ingest, backfill at startup
internal/config/config.go (+test)                        # T5: Server.TimeZone, Location()
internal/server/{playtime,profile}.go (+tests)           # T5: day buckets, the profile endpoint
internal/server/activity.go (+test), api.go              # T6: eventJSON, activity and today endpoints, card activity
web/src/lib/{types,api,share,state.svelte}.ts            # T7: Profile, getProfile, View, openView/closeView
web/src/lib/{zoned,profile}.ts (+tests)                  # T7: server-zone dates; the profile view model
web/src/lib/components/Profile{Content,Panel,Sheet}.svelte # T7
web/src/lib/components/{PlayersTab,PeekSheet,DesktopShell,MobileShell}.svelte # T7 (+T8)
web/src/lib/{timeline,filters.svelte}.ts (+tests)        # T8: event text/icons, filters, day groups, today bars
web/src/lib/{derive,markers}.ts                          # T8: activityRows on the timeline model; markerAt
web/src/lib/components/Activity{Icon,List,Content,Panel,Sheet}.svelte, MenuSheet.svelte # T8
cmd/farsight-seed/main.go (+test)                        # T9: repeatable -snapshot
web/tests/fixtures/{snapshot,events}.json                # T9: owners, a namer, four days of history, a raid
web/tests/e2e/{global-setup,desktop.spec,mobile.spec}.ts # T9
README.md, design/DESIGN-NOTES.md, both specs            # T9: docs
```

Task order follows the suggested shape, with two adjustments.
- The per-server time zone is added in Task 5 rather than with the store queries: Task 5 is its first consumer, and the store stays zone-free (it returns rows; the server does the local-day maths).
- The `live` applier's acceptance of `event_raid` sits in Task 2 with the parser, because a raid that the central app rejects as an unknown type never reaches the timeline. It ships with the app, before the agent.

---

### Task 1: Agent `extract`: portal owners and tame namers

Portals and tames get the links the profile needs. A portal's `creator` (a player ID, the same long that beds and tombstones carry as `owner`) is resolved to a name through the save's own player table once every ZDO is in, because a portal can come before the bed that names its creator. A tame's `TamedNameAuthor` is copied as it is: it is the namer's platform user ID ("Steam_7656…"), which matches the server log's identity. The game writes "host" when the namer wasn't signed in to a platform; that names nobody.

This ships with the agent update (it restarts the game servers). Until then snapshots carry neither field and the profile shows "None yet." for portals and tames.

**Files:**
- Modify: `internal/extract/snapshot.go` (`Marker.Namer`)
- Modify: `internal/extract/extract.go` (`kNameAuth`, `Extractor.portalCreator`, `Add`, `Finish`)
- Test: `internal/extract/extract_test.go`, `internal/extract/golden_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `extract.Marker.Owner` is now set on portals too (the creator's name; empty when no bed or tombstone names that player).
  - `extract.Marker.Namer string` with JSON `namer,omitempty`: a tame's `TamedNameAuthor` ("Steam_…"); never "host".

- [ ] **Step 1: Write the failing tests**

Append to the end of `internal/extract/extract_test.go`, after a blank line:

```go
func TestPortalOwnerAndTameNamer(t *testing.T) {
	e := New()
	// The portal comes before the bed that names its creator: Finish
	// resolves owners after every ZDO is in.
	portal := z("portal_wood", [3]float32{1, 0, 1})
	portal.Strings = map[int32]string{h("tag"): "copper"}
	portal.Longs = map[int32]int64{h("creator"): 42}
	stranger := z("portal_wood", [3]float32{2, 0, 2})
	stranger.Longs = map[int32]int64{h("creator"): 7} // no bed or tombstone names 7
	bed := z("bed", [3]float32{3, 0, 3})
	bed.Longs = map[int32]int64{h("owner"): 42}
	bed.Strings = map[int32]string{h("ownerName"): "Astrid"}
	named := z("Lox", [3]float32{4, 0, 4})
	named.Ints = map[int32]int32{h("tamed"): 1}
	named.Strings = map[int32]string{h("TamedName"): "Big Mama", h("TamedNameAuthor"): "Steam_76561190000000001"}
	byHost := z("Wolf", [3]float32{5, 0, 5})
	byHost.Ints = map[int32]int32{h("tamed"): 1}
	byHost.Strings = map[int32]string{h("TamedName"): "Grey", h("TamedNameAuthor"): "host"}
	for _, zz := range []*save.ZDO{portal, stranger, bed, named, byHost} {
		e.Add(zz)
	}
	s := e.Finish(&save.World{}, "x", time.Now())
	got := map[string]Marker{}
	for _, m := range s.Markers {
		got[m.Kind+":"+m.Label] = m
	}
	if o := got["portal:copper"].Owner; o != "Astrid" {
		t.Errorf("copper owner = %q, want Astrid", o)
	}
	if o := got["portal:"].Owner; o != "" {
		t.Errorf("unnamed creator's portal owner = %q, want empty", o)
	}
	if n := got["tame:Big Mama"].Namer; n != "Steam_76561190000000001" {
		t.Errorf("Big Mama namer = %q", n)
	}
	if n := got["tame:Grey"].Namer; n != "" {
		t.Errorf(`a "host" namer = %q, want empty`, n)
	}
}
```

In `internal/extract/golden_test.go`, in the imports, replace:

```go
	"path/filepath"
	"testing"
```

with:

```go
	"path/filepath"
	"strings"
	"testing"
```

In `internal/extract/golden_test.go`, in `TestGoldenMuleVikings`, after the players check, replace:

```go
	if len(s.Players) < 5 || s.Stats.UnknownPrefabs != 0 || s.World.SeedName == "" {
		t.Fatalf("players=%d unknown=%d seed=%q", len(s.Players), s.Stats.UnknownPrefabs, s.World.SeedName)
	}
```

with:

```go
	if len(s.Players) < 5 || s.Stats.UnknownPrefabs != 0 || s.World.SeedName == "" {
		t.Fatalf("players=%d unknown=%d seed=%q", len(s.Players), s.Stats.UnknownPrefabs, s.World.SeedName)
	}
	// Owners and namers (Plan 7): every portal owner is a player the save
	// names, and every namer is a platform user ID ("Platform_ID").
	known := map[string]bool{}
	for _, p := range s.Players {
		known[p.Name] = true
	}
	owned, named := 0, 0
	for _, m := range s.Markers {
		if m.Kind == "portal" && m.Owner != "" {
			owned++
			if !known[m.Owner] {
				t.Errorf("portal %s owner %q is not in players", m.ID, m.Owner)
			}
		}
		if m.Kind == "tame" && m.Namer != "" {
			named++
			if !strings.Contains(m.Namer, "_") {
				t.Errorf("tame %s namer %q is not Platform_ID", m.ID, m.Namer)
			}
		}
	}
	t.Logf("portals with an owner: %d of %d; tames with a namer: %d of %d", owned, count["portal"], named, count["tame"])
	if owned == 0 {
		t.Errorf("no portal has an owner")
	}
```

- [ ] **Step 2: Run them to make sure they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/extract/`

Expected: FAIL, the build stops with `got["tame:Big Mama"].Namer undefined (type Marker has no field or method Namer)`.

- [ ] **Step 3: Add `Marker.Namer`**

In `internal/extract/snapshot.go`, at the end of `type Marker`, replace:

```go
	Pair    string  `json:"pair,omitempty"`
}
```

with:

```go
	Pair    string  `json:"pair,omitempty"`
	// Namer is a tame's TamedNameAuthor: the platform user ID ("Steam_…")
	// of whoever named it. Empty when unnamed or named by the host.
	Namer string `json:"namer,omitempty"`
}
```

- [ ] **Step 4: Record creators and namers in the extractor**

In `internal/extract/extract.go`, in the key hashes, replace:

```go
	kCreator   = names.StableHash("creator")
)
```

with:

```go
	kCreator   = names.StableHash("creator")
	kNameAuth  = names.StableHash("TamedNameAuthor")
)
```

In `internal/extract/extract.go`, at the end of `type Extractor`, replace:

```go
	tables  *explored.Mask // union of the cartography tables' maps; nil until one decodes
}
```

with:

```go
	tables  *explored.Mask // union of the cartography tables' maps; nil until one decodes
	// portalCreator maps a portal's index in markers to its creator's
	// player ID; Finish resolves it to a name once every bed and tombstone
	// has filled players.
	portalCreator map[int]int64
}
```

In `internal/extract/extract.go`, in `New`, replace:

```go
	return &Extractor{counts: map[string]int{}, players: map[int64]string{}, unknown: map[int32]bool{}}
```

with:

```go
	return &Extractor{counts: map[string]int{}, players: map[int64]string{}, unknown: map[int32]bool{}, portalCreator: map[int]int64{}}
```

In `internal/extract/extract.go`, in `Add`, replace:

```go
	case strings.HasPrefix(name, "portal"):
		e.mark("portal", z).Label = z.Strings[kTag]
```

with:

```go
	case strings.HasPrefix(name, "portal"):
		e.mark("portal", z).Label = z.Strings[kTag]
		if id := z.Longs[kCreator]; id != 0 {
			e.portalCreator[len(e.markers)-1] = id
		}
```

In `internal/extract/extract.go`, in `Add`, the tame branch, replace:

```go
		m := e.mark("tame", z)
		m.Species, m.Label = name, z.Strings[kTamedName]
```

with:

```go
		m := e.mark("tame", z)
		m.Species, m.Label = name, z.Strings[kTamedName]
		// The game writes "host" when the namer wasn't signed in to a
		// platform; that names nobody.
		if a := z.Strings[kNameAuth]; a != "host" {
			m.Namer = a
		}
```

In `internal/extract/extract.go`, in `Finish`, replace:

```go
	pairPortals(s.Markers)
	for i, l := range w.Locations {
```

with:

```go
	pairPortals(s.Markers)
	for i, id := range e.portalCreator {
		s.Markers[i].Owner = e.players[id]
	}
	for i, l := range w.Locations {
```

- [ ] **Step 5: Run the tests to make sure they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test -v -run "Owner|Golden" ./internal/extract/`

Expected: PASS. With `testdata-golden/` present the golden test logs `portals with an owner: 16 of 16; tames with a namer: 6 of 12` for MuleVikings (it skips without the directory).

- [ ] **Step 6: Commit**

```bash
cd /workspace/Farsight
git add internal/extract/snapshot.go \
  internal/extract/extract.go \
  internal/extract/extract_test.go \
  internal/extract/golden_test.go
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(extract): portal owners from the creator, tame namers from TamedNameAuthor

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
git status --short   # must print nothing
```

---

### Task 2: `logwatch`: raids from "Random event set:", through the agent and into the event log

The game logs `Random event set:<name>` when a random event (a raid) starts. The parser turns it into a `RawRaid`, the sessionizer into an `event_raid` event carrying the game's event name, and the agent's sink sends it like any other event (the sink is type-agnostic, so `internal/agent` needs no change). The central app's applier must accept the new type or it would drop it as invalid; that part ships with the app, before the agent.

`EventID` is unchanged: the raid name isn't part of the key, so no existing event's id changes. A raid's type and second are unique enough.

**Files:**
- Modify: `internal/logwatch/parse.go` (`RawRaid`, `Raw.Raid`, `reRaid`)
- Modify: `internal/logwatch/session.go` (`Event.Raid`, `EvRaid`, `Feed`)
- Modify: `internal/live/apply.go` (accept `event_raid`)
- Test: `internal/logwatch/parse_test.go`, `internal/logwatch/session_test.go`, `internal/logwatch/watcher_test.go`, `internal/live/apply_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `logwatch.RawRaid RawKind = "raid"`, `logwatch.Raw.Raid string`.
  - `logwatch.EvRaid = "event_raid"`, `logwatch.Event.Raid string` with JSON `raid,omitempty` (e.g. `"army_theelder"`).
  - `live.Applier.Apply` stores `event_raid` events (no live-state change).

- [ ] **Step 1: Write the failing tests**

Append to the end of `internal/logwatch/parse_test.go`, after a blank line:

```go
func TestParseRandomEventSet(t *testing.T) {
	loc := oslo(t)
	r := one(t, ParseServerLine("10/03/2026 21:14:05: Random event set:army_theelder", loc))
	if r.Kind != RawRaid || r.Raid != "army_theelder" || !r.At.Equal(time.Date(2026, 10, 3, 19, 14, 5, 0, time.UTC)) {
		t.Fatalf("raid = %+v", r)
	}
	r = one(t, ParseServerLine("10/03/2026 21:14:05: Random event set: foresttrolls\r", loc))
	if r.Raid != "foresttrolls" {
		t.Fatalf("raid with a space and CRLF = %+v", r)
	}
	if got := ParseServerLine("10/03/2026 21:14:05: Random event set:", loc); len(got) != 0 {
		t.Fatalf("a nameless event line must be ignored: %+v", got)
	}
}
```

Append to the end of `internal/logwatch/session_test.go`, after a blank line:

```go
func TestRaidEvent(t *testing.T) {
	at := time.Date(2026, 10, 3, 19, 14, 5, 0, time.UTC)
	s := NewSessionizer()
	evs := s.Feed(Raw{Kind: RawRaid, At: at, Raid: "army_theelder"})
	if len(evs) != 1 {
		t.Fatalf("events = %+v", evs)
	}
	e := evs[0]
	if e.Type != EvRaid || e.Raid != "army_theelder" || !e.At.Equal(at) || e.ID == "" {
		t.Fatalf("raid event = %+v", e)
	}
	if again := s.Feed(Raw{Kind: RawRaid, At: at, Raid: "army_theelder"}); again[0].ID != e.ID {
		t.Fatalf("raid IDs differ on replay: %s vs %s", again[0].ID, e.ID)
	}
}
```

Append to the end of `internal/logwatch/watcher_test.go`, after a blank line:

```go
func TestWatcherEmitsRaids(t *testing.T) {
	dir := t.TempDir()
	srv := filepath.Join(dir, "valheim-server-stdout---supervisor-aaaa.log")
	appendLine(t, srv, "10/03/2026 21:14:05: Random event set:army_theelder\n")
	c, _ := startWatcher(t, dir)
	waitFor(t, func() bool { return c.count(EvRaid) == 1 })
	appendLine(t, srv, "10/03/2026 21:40:00: Random event set:foresttrolls\n")
	waitFor(t, func() bool { return c.count(EvRaid) == 2 })
}
```

Append to the end of `internal/live/apply_test.go`, after a blank line:

```go
func TestApplyStoresRaidEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := parseTime("2026-10-03T20:00:00Z")
	a := &Applier{Store: s, Now: fixedNow(now)}
	ev := logwatch.Event{ID: "r1", Type: logwatch.EvRaid, At: parseTime("2026-10-03T19:14:05Z"), Raid: "army_theelder"}
	applied, skipped, err := a.Apply(ctx, "srv", []logwatch.Event{ev})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || skipped != 0 {
		t.Fatalf("applied=%d skipped=%d, want 1,0", applied, skipped)
	}
	got, err := s.RecentActivity(ctx, "srv", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != logwatch.EvRaid || got[0].Raid != "army_theelder" {
		t.Fatalf("stored = %+v", got)
	}
}
```

- [ ] **Step 2: Run them to make sure they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/logwatch/ ./internal/live/`

Expected: FAIL: both packages stop at build, with `undefined: RawRaid` and `undefined: logwatch.EvRaid`.

- [ ] **Step 3: Parse the line**

In `internal/logwatch/parse.go`, replace:

```go
	RawDestroy      RawKind = "destroy"
)
```

with:

```go
	RawDestroy      RawKind = "destroy"
	RawRaid         RawKind = "raid"
)
```

In `internal/logwatch/parse.go`, at the end of `type Raw`, replace:

```go
	Platform   string
	PlatformID string
}
```

with:

```go
	Platform   string
	PlatformID string
	Raid       string // RawRaid: the game's event name, e.g. "army_theelder"
}
```

In `internal/logwatch/parse.go`, replace:

```go
	reDestroy    = regexp.MustCompile(`^Destroying abandoned non persistent zdo -?\d+:\d+ owner (-?\d+)`)
```

with:

```go
	reDestroy    = regexp.MustCompile(`^Destroying abandoned non persistent zdo -?\d+:\d+ owner (-?\d+)`)
	reRaid       = regexp.MustCompile(`^Random event set:\s*(\S+)`)
```

In `internal/logwatch/parse.go`, at the end of the `switch` in `serverLine`, replace:

```go
		return []Raw{{Kind: RawDestroy, At: at, UID: uid}}
	}
```

with:

```go
		return []Raw{{Kind: RawDestroy, At: at, UID: uid}}
	case reRaid.MatchString(msg):
		return []Raw{{Kind: RawRaid, At: at, Raid: reRaid.FindStringSubmatch(msg)[1]}}
	}
```

- [ ] **Step 4: Emit `event_raid`**

In `internal/logwatch/session.go`, at the end of `type Event`, replace:

```go
	Reason  string     `json:"reason,omitempty"`
}
```

with:

```go
	Reason  string     `json:"reason,omitempty"`

	// Raid is an event_raid's game event name ("army_theelder"). EventID
	// leaves it out, so IDs of every other type are unchanged; a raid's
	// type and second are unique enough.
	Raid string `json:"raid,omitempty"`
}
```

In `internal/logwatch/session.go`, replace:

```go
	EvHeartbeat      = "heartbeat"
)
```

with:

```go
	EvHeartbeat      = "heartbeat"
	EvRaid           = "event_raid"
)
```

In `internal/logwatch/session.go`, in `Feed`, replace:

```go
	case RawSaved:
		out = []Event{s.emit(EvWorldSaved, r.At, Event{})}
	}
```

with:

```go
	case RawSaved:
		out = []Event{s.emit(EvWorldSaved, r.At, Event{})}
	case RawRaid:
		out = []Event{s.emit(EvRaid, r.At, Event{Raid: r.Raid})}
	}
```

- [ ] **Step 5: Let the central app store raids**

In `internal/live/apply.go`, in `knownEventTypes`, replace:

```go
	logwatch.EvHeartbeat:      true,
}
```

with:

```go
	logwatch.EvHeartbeat:      true,
	logwatch.EvRaid:           true,
}
```

In `internal/live/apply.go`, in `Apply`, replace:

```go
			case logwatch.EvWorldSaved:
				// No live-state change; the row already inserted into
				// events is enough.
```

with:

```go
			case logwatch.EvWorldSaved, logwatch.EvRaid:
				// No live-state change; the row already inserted into
				// events is enough.
```

- [ ] **Step 6: Run the tests to make sure they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/logwatch/ ./internal/live/ ./internal/agent/`

Expected: `ok` for all three packages.

- [ ] **Step 7: Commit**

```bash
cd /workspace/Farsight
git add internal/logwatch/parse.go \
  internal/logwatch/session.go \
  internal/logwatch/parse_test.go \
  internal/logwatch/session_test.go \
  internal/logwatch/watcher_test.go \
  internal/live/apply.go \
  internal/live/apply_test.go
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(logwatch): raids from "Random event set:" as event_raid

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
git status --short   # must print nothing
```

---

### Task 3: Store: event paging, session queries, world-diff state and tombstones (schema v2)

Everything the profile and the timeline read, plus the bookkeeping the world-save diff (Task 4) writes:

- **Schema v2:** a `world_diff` row per server (the last snapshot whose differences went into events; no row means the backfill hasn't run) and a `tombstones` table (every distinct tombstone ever seen in a save, with the save time it first appeared). An index on `sessions (server_id, platform_id)` serves the profile.
- **Events:** world-save events share the `events` table with log events, so the store returns raw rows (`StoredEvent`, body undecoded) and the server decodes by type. Ordering gets a tiebreak (`at DESC, id DESC`): a save's several events share one time.
- **Retention:** `PruneEvents` now deletes only heartbeat and players_now events. Everything else stays, so the timeline reaches back to when tracking began (30 Sep 2026) instead of losing it after 14 days. Snapshots are still pruned after 14 days; the `tombstones` table is what keeps the death history.
- **Sessions:** a player's sessions, the sessions overlapping a window (today, in the server's zone), and every player seen, under their newest name with all the names they played under.

The session maths (day buckets, totals) lives in the server (Task 5), where the time zone is known; the store only returns rows.

**Files:**
- Modify: `internal/store/store.go` (migration 2)
- Modify: `internal/store/events.go` (`StoredEvent`, `InsertRawEvent`, `RecentEvents`, `EventsBetween`, `EarliestEvent`; `RecentActivity` on top of `RecentEvents`; `PruneEvents` narrowed)
- Modify: `internal/store/sessions.go` (`PlayerSessions`, `SessionsOverlapping`, `Player`, `Players`)
- Create: `internal/store/worlddiff.go` (`SnapshotKey`, `WorldDiffState`, `PutWorldDiffState`, `SnapshotsAfter`, `Snapshot`, `Tombstone`, `InsertTombstone`, `Tombstones`)
- Test: `internal/store/store_test.go` (prune test replaced), `internal/store/history_test.go`, `internal/store/worlddiff_test.go`

**Interfaces:**
- Consumes: `logwatch.EvHeartbeat`, `logwatch.EvPlayersNow` (existing).
- Produces (later tasks use these exact names):
  - `type store.StoredEvent struct { ID, Type string; At time.Time; Body []byte }`
  - `func (*Store) InsertRawEvent(ctx, tx *sql.Tx, serverID string, e StoredEvent) (bool, error)`
  - `func (*Store) RecentEvents(ctx, serverID string, limit int) ([]StoredEvent, error)`: newest first, no heartbeat/players_now.
  - `func (*Store) EventsBetween(ctx, serverID string, from, until time.Time) ([]StoredEvent, error)`: `from <= at < until`, newest first, ties by id descending.
  - `func (*Store) EarliestEvent(ctx, serverID string) (time.Time, bool, error)`
  - `func (*Store) PlayerSessions(ctx, serverID, platformID string) ([]Session, error)`: oldest first.
  - `func (*Store) SessionsOverlapping(ctx, serverID string, from, until time.Time) ([]Session, error)`
  - `type store.Player struct { PlatformID, Platform, Name string; Names []string; Online bool; LastSeen time.Time }` and `func (*Store) Players(ctx, serverID string) ([]Player, error)`: online first (by name), then most recently seen.
  - `type store.SnapshotKey struct { SaveID string; SavedAt time.Time }`
  - `func (*Store) WorldDiffState(ctx, tx *sql.Tx, serverID string) (SnapshotKey, bool, error)`, `func (*Store) PutWorldDiffState(ctx, tx *sql.Tx, serverID string, k SnapshotKey) error`
  - `func (*Store) SnapshotsAfter(ctx, serverID string, after SnapshotKey) ([]SnapshotKey, error)`: by (saved_at, save_id), oldest first; the zero key lists all.
  - `func (*Store) Snapshot(ctx, serverID, saveID string) ([]byte, bool, error)`
  - `type store.Tombstone struct { ID, Owner string; X, Z float32; FirstSeen time.Time }`, `func (*Store) InsertTombstone(ctx, tx *sql.Tx, serverID string, t Tombstone) (bool, error)`, `func (*Store) Tombstones(ctx, serverID, owner string) ([]Tombstone, error)`: oldest first.

- [ ] **Step 1: Write the failing tests**

Replace the prune test in `internal/store/store_test.go` (it asserted the old prune-everything behaviour), then add two new test files.

In `internal/store/store_test.go`, replace:

```go
func TestPruneEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.InsertEventIfNew(ctx, nil, "srv", logwatch.Event{ID: "old", Type: logwatch.EvPlayerJoin, At: ms("2026-01-01T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertEventIfNew(ctx, nil, "srv", logwatch.Event{ID: "new", Type: logwatch.EvPlayerJoin, At: ms("2026-01-03T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}

	n, err := s.PruneEvents(ctx, ms("2026-01-02T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pruned %d, want 1", n)
	}

	got, err := s.RecentActivity(ctx, "srv", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("remaining events = %+v", got)
	}
}
```

with:

```go
func TestPruneEventsDeletesOnlyOldHeartbeatsAndPlayersNow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, e := range []logwatch.Event{
		{ID: "old-hb", Type: logwatch.EvHeartbeat, At: ms("2026-01-01T00:00:00Z")},
		{ID: "old-now", Type: logwatch.EvPlayersNow, At: ms("2026-01-01T00:00:00Z")},
		{ID: "old-join", Type: logwatch.EvPlayerJoin, At: ms("2026-01-01T00:00:00Z")},
		{ID: "new-hb", Type: logwatch.EvHeartbeat, At: ms("2026-01-03T00:00:00Z")},
		{ID: "new-join", Type: logwatch.EvPlayerJoin, At: ms("2026-01-03T00:00:00Z")},
	} {
		if _, err := s.InsertEventIfNew(ctx, nil, "srv", e); err != nil {
			t.Fatal(err)
		}
	}

	n, err := s.PruneEvents(ctx, ms("2026-01-02T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("pruned %d, want 2 (the old heartbeat and players_now)", n)
	}

	got, err := s.RecentActivity(ctx, "srv", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "new-join" || got[1].ID != "old-join" {
		t.Fatalf("remaining activity = %+v, want new-join then old-join", got)
	}
}
```

Create `internal/store/history_test.go`:

```go
package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/logwatch"
)

func TestMigrationTwoUpgradesAVersionOneDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "farsight.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Roll the file back to schema version 1.
	for _, q := range []string{`DROP TABLE world_diff`, `DROP TABLE tombstones`, `DROP INDEX idx_sessions_server_platform`, `UPDATE schema_version SET v = 1`} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var v int
	if err := s.db.QueryRowContext(ctx, `SELECT v FROM schema_version`).Scan(&v); err != nil || v != 2 {
		t.Fatalf("schema version = %d (err %v), want 2", v, err)
	}
	if _, ok, err := s.WorldDiffState(ctx, nil, "srv"); err != nil || ok {
		t.Fatalf("world_diff after upgrade: ok=%v err=%v", ok, err)
	}
	if _, err := s.InsertTombstone(ctx, nil, "srv", Tombstone{ID: "t", Owner: "A", FirstSeen: ms("2026-10-01T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}
}

func TestEventsBetweenWindowOrderAndNoise(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, e := range []logwatch.Event{
		{ID: "before", Type: logwatch.EvPlayerJoin, At: ms("2026-10-01T23:59:59Z")},
		{ID: "a", Type: logwatch.EvPlayerJoin, At: ms("2026-10-02T00:00:00Z")},
		{ID: "b", Type: logwatch.EvWorldSaved, At: ms("2026-10-02T12:00:00Z")},
		{ID: "c", Type: logwatch.EvPlayerLeave, At: ms("2026-10-02T12:00:00Z")},
		{ID: "hb", Type: logwatch.EvHeartbeat, At: ms("2026-10-02T13:00:00Z")},
		{ID: "at-until", Type: logwatch.EvPlayerJoin, At: ms("2026-10-03T00:00:00Z")},
	} {
		if _, err := s.InsertEventIfNew(ctx, nil, "srv", e); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.InsertRawEvent(ctx, nil, "srv", StoredEvent{ID: "w", Type: "world_boss", At: ms("2026-10-02T06:00:00Z"), Body: []byte(`{"boss":"Moder"}`)}); err != nil {
		t.Fatal(err)
	}
	got, err := s.EventsBetween(ctx, "srv", ms("2026-10-02T00:00:00Z"), ms("2026-10-03T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range got {
		ids = append(ids, e.ID)
	}
	want := []string{"c", "b", "w", "a"} // newest first, ties by id descending
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
	if string(got[2].Body) != `{"boss":"Moder"}` || got[2].Type != "world_boss" {
		t.Fatalf("raw event = %+v", got[2])
	}

	first, ok, err := s.EarliestEvent(ctx, "srv")
	if err != nil || !ok || !first.Equal(ms("2026-10-01T23:59:59Z")) {
		t.Fatalf("earliest = %v ok=%v err=%v", first, ok, err)
	}
	if _, ok, err := s.EarliestEvent(ctx, "other"); err != nil || ok {
		t.Fatalf("earliest on an empty server: ok=%v err=%v", ok, err)
	}
}

func TestPlayerSessionsAndOverlapping(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	closed := func(name, id, since, until string) Session {
		u := ms(until)
		return Session{ServerID: "srv", Name: name, Platform: "Steam", PlatformID: id, Since: ms(since), Until: &u, Reason: "left"}
	}
	for _, sess := range []Session{
		closed("Astrid", "1", "2026-10-01T10:00:00Z", "2026-10-01T11:00:00Z"),
		closed("Astrid", "1", "2026-10-01T23:00:00Z", "2026-10-02T01:00:00Z"), // across midnight
		closed("Bjorn", "2", "2026-10-02T08:00:00Z", "2026-10-02T09:00:00Z"),
		closed("Bjorn", "2", "2026-10-03T08:00:00Z", "2026-10-03T09:00:00Z"),
	} {
		if err := s.InsertClosedSession(ctx, nil, sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.OpenSession(ctx, nil, Session{ServerID: "srv", Name: "Astrid", Platform: "Steam", PlatformID: "1", Since: ms("2026-10-02T20:00:00Z")}); err != nil {
		t.Fatal(err)
	}

	astrid, err := s.PlayerSessions(ctx, "srv", "1")
	if err != nil || len(astrid) != 3 || astrid[2].Until != nil {
		t.Fatalf("Astrid's sessions = %+v err=%v", astrid, err)
	}

	day, err := s.SessionsOverlapping(ctx, "srv", ms("2026-10-02T00:00:00Z"), ms("2026-10-03T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if len(day) != 3 {
		t.Fatalf("2 Oct sessions = %+v, want the one across midnight, Bjorn's and the open one", day)
	}
	if !day[0].Since.Equal(ms("2026-10-01T23:00:00Z")) || day[1].Name != "Bjorn" || day[2].Until != nil {
		t.Fatalf("2 Oct sessions = %+v", day)
	}
}

func TestPlayersNewestNameOnlineFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := func(v string) *time.Time { t := ms(v); return &t }
	for _, sess := range []Session{
		{ServerID: "srv", Name: "OldName", Platform: "Steam", PlatformID: "1", Since: ms("2026-10-01T10:00:00Z"), Until: u("2026-10-01T11:00:00Z")},
		{ServerID: "srv", Name: "Astrid", Platform: "Steam", PlatformID: "1", Since: ms("2026-10-02T10:00:00Z"), Until: u("2026-10-02T11:00:00Z")},
		{ServerID: "srv", Name: "Ulf", Platform: "Steam", PlatformID: "4", Since: ms("2026-10-03T10:00:00Z"), Until: u("2026-10-03T11:00:00Z")},
		{ServerID: "srv", Name: "Nobody", Platform: "", PlatformID: "", Since: ms("2026-10-03T10:00:00Z"), Until: u("2026-10-03T11:00:00Z")},
	} {
		if err := s.InsertClosedSession(ctx, nil, sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.OpenSession(ctx, nil, Session{ServerID: "srv", Name: "Bjorn", Platform: "Xbox", PlatformID: "2", Since: ms("2026-10-01T09:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	ps, err := s.Players(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 3 {
		t.Fatalf("players = %+v, want 3 (no empty platform ID)", ps)
	}
	if ps[0].Name != "Bjorn" || !ps[0].Online || ps[0].Platform != "Xbox" {
		t.Fatalf("first = %+v, want Bjorn online", ps[0])
	}
	if ps[1].Name != "Ulf" || ps[2].Name != "Astrid" || !ps[2].LastSeen.Equal(ms("2026-10-02T11:00:00Z")) ||
		len(ps[2].Names) != 2 || ps[2].Names[0] != "OldName" {
		t.Fatalf("players = %+v, want Ulf then Astrid (renamed from OldName)", ps)
	}
}
```

Create `internal/store/worlddiff_test.go`:

```go
package store

import (
	"context"
	"testing"
)

func TestWorldDiffStateRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, ok, err := s.WorldDiffState(ctx, nil, "srv"); err != nil || ok {
		t.Fatalf("fresh state: ok=%v err=%v", ok, err)
	}
	for _, k := range []SnapshotKey{
		{SaveID: "chunked:1", SavedAt: ms("2026-10-01T10:00:00Z")},
		{SaveID: "chunked:2", SavedAt: ms("2026-10-01T10:20:00Z")},
	} {
		if err := s.PutWorldDiffState(ctx, nil, "srv", k); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := s.WorldDiffState(ctx, nil, "srv")
	if err != nil || !ok || got.SaveID != "chunked:2" || !got.SavedAt.Equal(ms("2026-10-01T10:20:00Z")) {
		t.Fatalf("state = %+v ok=%v err=%v", got, ok, err)
	}
}

func TestSnapshotsAfterAndSnapshot(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	put := func(id, at, body string) {
		t.Helper()
		if _, err := s.PutSnapshot(ctx, "srv", id, ms(at), ms(at), []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	put("b", "2026-10-01T10:00:00Z", "B")
	put("a", "2026-10-01T10:00:00Z", "A") // same save time: ordered by id
	put("c", "2026-10-01T11:00:00Z", "C")
	if _, err := s.PutSnapshot(ctx, "other", "x", ms("2026-10-01T09:00:00Z"), ms("2026-10-01T09:00:00Z"), []byte("X")); err != nil {
		t.Fatal(err)
	}

	all, err := s.SnapshotsAfter(ctx, "srv", SnapshotKey{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].SaveID != "a" || all[1].SaveID != "b" || all[2].SaveID != "c" {
		t.Fatalf("all = %+v, want a, b, c", all)
	}
	rest, err := s.SnapshotsAfter(ctx, "srv", all[0])
	if err != nil || len(rest) != 2 || rest[0].SaveID != "b" {
		t.Fatalf("after a = %+v err=%v, want b, c", rest, err)
	}
	none, err := s.SnapshotsAfter(ctx, "srv", all[2])
	if err != nil || len(none) != 0 {
		t.Fatalf("after c = %+v err=%v", none, err)
	}

	blob, ok, err := s.Snapshot(ctx, "srv", "b")
	if err != nil || !ok || string(blob) != "B" {
		t.Fatalf("snapshot b = %q ok=%v err=%v", blob, ok, err)
	}
	if _, ok, err := s.Snapshot(ctx, "srv", "gone"); err != nil || ok {
		t.Fatalf("missing snapshot: ok=%v err=%v", ok, err)
	}
}

func TestTombstonesDistinctByIDOldestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, tb := range []Tombstone{
		{ID: "t2", Owner: "Bjorn", X: 5, Z: 6, FirstSeen: ms("2026-10-02T10:00:00Z")},
		{ID: "t1", Owner: "Bjorn", X: 1, Z: 2, FirstSeen: ms("2026-10-01T10:00:00Z")},
		{ID: "t3", Owner: "Astrid", X: 9, Z: 9, FirstSeen: ms("2026-10-01T10:00:00Z")},
	} {
		if ok, err := s.InsertTombstone(ctx, nil, "srv", tb); err != nil || !ok {
			t.Fatalf("insert %s: ok=%v err=%v", tb.ID, ok, err)
		}
	}
	if ok, err := s.InsertTombstone(ctx, nil, "srv", Tombstone{ID: "t1", Owner: "Bjorn", FirstSeen: ms("2026-10-05T10:00:00Z")}); err != nil || ok {
		t.Fatalf("duplicate insert: ok=%v err=%v", ok, err)
	}
	got, err := s.Tombstones(ctx, "srv", "Bjorn")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "t1" || got[0].X != 1 || !got[0].FirstSeen.Equal(ms("2026-10-01T10:00:00Z")) || got[1].ID != "t2" {
		t.Fatalf("Bjorn's tombstones = %+v", got)
	}
}
```

- [ ] **Step 2: Run them to make sure they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/store/`

Expected: FAIL at build: `s.WorldDiffState undefined`, `undefined: Tombstone`, `undefined: SnapshotKey` and so on.

- [ ] **Step 3: Add schema version 2**

In `internal/store/store.go`, at the end of `migrations`, replace:

```go
			status_at       INTEGER
		)`,
	},
}
```

with:

```go
			status_at       INTEGER
		)`,
	},
	// 2: world-save events (Plan 7). world_diff records, per server, the
	// last snapshot whose differences went into events (its presence means
	// the backfill has run); tombstones holds every distinct tombstone ever
	// seen in a save, with the save time it first appeared.
	{
		`CREATE TABLE IF NOT EXISTS world_diff (
			server_id TEXT PRIMARY KEY,
			save_id   TEXT NOT NULL,
			saved_at  INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tombstones (
			server_id  TEXT NOT NULL,
			id         TEXT NOT NULL,
			owner      TEXT NOT NULL,
			x          REAL NOT NULL,
			z          REAL NOT NULL,
			first_seen INTEGER NOT NULL,
			PRIMARY KEY (server_id, id)
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_server_platform ON sessions (server_id, platform_id)`,
	},
}
```

- [ ] **Step 4: Rewrite the event queries**

`events.go` changes throughout (raw rows, the shared scan, the narrower prune), so replace it whole.

Replace the whole of `internal/store/events.go` with:

```go
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
	var v sql.NullInt64
	err = s.db.QueryRowContext(ctx, `
		SELECT MIN(at) FROM events WHERE server_id = ? AND type NOT IN (?, ?)`,
		append([]any{serverID}, noiseTypes...)...).Scan(&v)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("store: earliest event: %w", err)
	}
	if !v.Valid {
		return time.Time{}, false, nil
	}
	return fromMillis(v.Int64), true, nil
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
```

- [ ] **Step 5: Add the session queries**

In `internal/store/sessions.go`, in the imports, replace:

```go
	"fmt"
	"time"
)
```

with:

```go
	"fmt"
	"slices"
	"sort"
	"time"
)
```

In `internal/store/sessions.go`, just before `ServersWithOpenSessions`, replace:

```go
// ServersWithOpenSessions returns
```

with:

```go
// PlayerSessions returns every session of platformID on serverID, oldest
// first.
func (s *Store) PlayerSessions(ctx context.Context, serverID, platformID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT server_id, platform_id, name, since, platform, until, seconds, reason
		FROM sessions
		WHERE server_id = ? AND platform_id = ?
		ORDER BY since ASC`, serverID, platformID)
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
		ORDER BY since ASC`, serverID, millis(until), millis(from))
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
		ORDER BY since ASC`, serverID)
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

// ServersWithOpenSessions returns
```

- [ ] **Step 6: Add the world-diff state, snapshot listing and tombstones**

Create `internal/store/worlddiff.go`:

```go
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SnapshotKey names one stored snapshot: its save id and save time, the
// order snapshots are replayed in.
type SnapshotKey struct {
	SaveID  string
	SavedAt time.Time
}

// WorldDiffState returns the last snapshot of serverID whose differences
// from the one before it went into the event log. ok is false if world
// events have never been derived for it (the backfill hasn't run). tx may
// be nil.
func (s *Store) WorldDiffState(ctx context.Context, tx *sql.Tx, serverID string) (SnapshotKey, bool, error) {
	var st SnapshotKey
	var savedAt int64
	err := s.conn(tx).QueryRowContext(ctx, `
		SELECT save_id, saved_at FROM world_diff WHERE server_id = ?`, serverID).Scan(&st.SaveID, &savedAt)
	switch {
	case err == sql.ErrNoRows:
		return SnapshotKey{}, false, nil
	case err != nil:
		return SnapshotKey{}, false, fmt.Errorf("store: world diff state: %w", err)
	}
	st.SavedAt = fromMillis(savedAt)
	return st, true, nil
}

// PutWorldDiffState records st as serverID's last diffed snapshot. tx may
// be nil.
func (s *Store) PutWorldDiffState(ctx context.Context, tx *sql.Tx, serverID string, st SnapshotKey) error {
	_, err := s.conn(tx).ExecContext(ctx, `
		INSERT INTO world_diff (server_id, save_id, saved_at) VALUES (?, ?, ?)
		ON CONFLICT (server_id) DO UPDATE SET save_id = excluded.save_id, saved_at = excluded.saved_at`,
		serverID, st.SaveID, millis(st.SavedAt))
	if err != nil {
		return fmt.Errorf("store: put world diff state: %w", err)
	}
	return nil
}

// SnapshotsAfter returns the keys (no blobs) of serverID's snapshots that
// sort after `after` by (saved_at, save_id), oldest first. The zero
// SnapshotKey returns them all.
func (s *Store) SnapshotsAfter(ctx context.Context, serverID string, after SnapshotKey) ([]SnapshotKey, error) {
	ms := int64(-1 << 62)
	if !after.SavedAt.IsZero() {
		ms = millis(after.SavedAt)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT save_id, saved_at FROM snapshots
		WHERE server_id = ? AND (saved_at > ? OR (saved_at = ? AND save_id > ?))
		ORDER BY saved_at ASC, save_id ASC`, serverID, ms, ms, after.SaveID)
	if err != nil {
		return nil, fmt.Errorf("store: snapshots after: %w", err)
	}
	defer rows.Close()
	var out []SnapshotKey
	for rows.Next() {
		var st SnapshotKey
		var savedAt int64
		if err := rows.Scan(&st.SaveID, &savedAt); err != nil {
			return nil, err
		}
		st.SavedAt = fromMillis(savedAt)
		out = append(out, st)
	}
	return out, rows.Err()
}

// Snapshot returns one stored snapshot, decompressed. ok is false if it
// isn't stored (never was, or pruned).
func (s *Store) Snapshot(ctx context.Context, serverID, saveID string) (blob []byte, ok bool, err error) {
	var compressed []byte
	err = s.db.QueryRowContext(ctx, `
		SELECT blob FROM snapshots WHERE server_id = ? AND save_id = ?`, serverID, saveID).Scan(&compressed)
	switch {
	case err == sql.ErrNoRows:
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("store: snapshot: %w", err)
	}
	blob, err = gzipDecompress(compressed)
	if err != nil {
		return nil, false, fmt.Errorf("store: decompress snapshot: %w", err)
	}
	return blob, true, nil
}

// Tombstone is one distinct tombstone seen in a server's saves.
type Tombstone struct {
	ID        string
	Owner     string
	X, Z      float32
	FirstSeen time.Time // the save time it first appeared in
}

// InsertTombstone records t unless (serverID, t.ID) exists, reporting
// whether it was inserted. tx may be nil.
func (s *Store) InsertTombstone(ctx context.Context, tx *sql.Tx, serverID string, t Tombstone) (bool, error) {
	res, err := s.conn(tx).ExecContext(ctx, `
		INSERT OR IGNORE INTO tombstones (server_id, id, owner, x, z, first_seen)
		VALUES (?, ?, ?, ?, ?, ?)`,
		serverID, t.ID, t.Owner, t.X, t.Z, millis(t.FirstSeen))
	if err != nil {
		return false, fmt.Errorf("store: insert tombstone: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Tombstones returns owner's distinct tombstones on serverID, oldest first.
func (s *Store) Tombstones(ctx context.Context, serverID, owner string) ([]Tombstone, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner, x, z, first_seen FROM tombstones
		WHERE server_id = ? AND owner = ?
		ORDER BY first_seen ASC, id ASC`, serverID, owner)
	if err != nil {
		return nil, fmt.Errorf("store: tombstones: %w", err)
	}
	defer rows.Close()
	var out []Tombstone
	for rows.Next() {
		var t Tombstone
		var first int64
		if err := rows.Scan(&t.ID, &t.Owner, &t.X, &t.Z, &first); err != nil {
			return nil, err
		}
		t.FirstSeen = fromMillis(first)
		out = append(out, t)
	}
	return out, rows.Err()
}
```

- [ ] **Step 7: Run the tests to make sure they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/store/ ./internal/live/ ./internal/server/`

Expected: `ok` for all three (the server and live packages use `RecentActivity`, which now sits on `RecentEvents`).

- [ ] **Step 8: Commit**

```bash
cd /workspace/Farsight
git add internal/store/store.go \
  internal/store/events.go \
  internal/store/sessions.go \
  internal/store/worlddiff.go \
  internal/store/store_test.go \
  internal/store/history_test.go \
  internal/store/worlddiff_test.go
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(store): event paging, session queries, world-diff state and tombstones

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
git status --short   # must print nothing
```

---

### Task 4: Central app: world-save events from consecutive saves, with an idempotent backfill

A new package, `internal/worldevents`, compares each stored snapshot with the one before it and writes the differences to the `events` table:

| type | when | fields |
|---|---|---|
| `world_tombstone` | a tombstone with no tombstone of the same owner within 4 m in the previous save | owner, pos, biome, near |
| `world_portal` | a portal with no portal of the same tag within 4 m before | tag, paired, owner, pos, biome, near |
| `world_portal_paired` | an existing portal that became paired with another existing one | tag, owner, pos, biome, near |
| `world_tame` | a named tame beyond the previous count of that (species, name) | name, species, namer, pos, biome, near |
| `world_base_new` | a base no previous base matches | name, pieces, builders, pos, biome, near |
| `world_base_grew` | a matched base that grew by 25 pieces or more | name, pieces, grew, builders, pos, biome, near |
| `world_boss` | a boss key newly set (`extract.BossesFromKeys`) | boss |

Why these matching rules:
- Marker and base ids are per-save rankings (`portal-3`, `base-1`), not stable ids, so objects are matched by what doesn't change: owner or tag and position for tombstones and portals, the nearest previous centre for bases (within its radius plus 32 m).
- Tames walk about, so they're counted per (species, name). Unnamed tames (bred or freshly tamed animals) are not reported.
- Owners and namers are never part of a match: the agent update that adds them would otherwise report every portal and tame again.
- A snapshot stored before global keys were sent falls back to its own boss list (as `cardBosses` does), so the first save with keys doesn't re-report every boss.

Each event's time is the save's time, its id derived from the save and the object (so replays write the same ids). Its place is the biome (from the world generator's base pass) and the nearest boss altar, trader or dungeon within 300 m in explored ground ("a sunken crypt"). The first save of a server is a baseline and yields no events: what's in it predates tracking.

`Deriver.CatchUp` replays every stored snapshot newer than the last one diffed, one transaction per snapshot (events, newly seen tombstones, the new `world_diff` row). With no `world_diff` row yet, that is the backfill of every stored snapshot (all from 30 Sep 2026 on). If the last diffed snapshot was pruned, the next one becomes a new baseline whose tombstones are matched against the stored ones. Runs for one server are serialised; concurrent Derivers (a restart) are harmless because every insert ignores existing rows.

Snapshot ingest calls `CatchUp` after storing (a failure is logged, never returned to the agent), and `serve` runs it once per configured server at startup, in the background.

**Files:**
- Create: `internal/worldevents/event.go` (types, `Event`, ids), `geo.go` (`Geo`, `NewGeo`, `MaskOf`, `BiomeName`), `diff.go` (`Diff`, `NewTombstones`, `Near`), `deriver.go` (`Deriver`)
- Test: `internal/worldevents/diff_test.go`, `internal/worldevents/deriver_test.go`
- Modify: `internal/server/server.go` (`Deps.World`), `internal/server/ingest.go` (CatchUp after storing)
- Test: `internal/server/worldevents_test.go`
- Modify: `cmd/farsight/serve.go` (one Deriver, startup backfill), `cmd/farsight/serve_test.go`

**Interfaces:**
- Consumes (Task 3): `store.StoredEvent`, `(*Store).InsertRawEvent`, `WorldDiffState`, `PutWorldDiffState`, `SnapshotsAfter`, `Snapshot`, `Tombstone`, `InsertTombstone`, `Tombstones`, `Tx`; `extract.BossesFromKeys`.
- Produces:
  - Event types `worldevents.TypeTombstone` (`"world_tombstone"`), `TypePortal`, `TypePortalPaired`, `TypeTame`, `TypeBaseNew`, `TypeBaseGrew`, `TypeBoss`.
  - Constants `worldevents.GrowthMin = 25`, `MatchRadius = 4`, `NearRadius = 300`.
  - `type worldevents.Event` with JSON `id, type, at, saveId, pos{x,z}, owner, namer, builders, tag, paired, name, species, pieces, grew, boss, biome, near` (the stored event body).
  - `type worldevents.Geo interface { Biome(x, z float32) string; Explored(x, z float32) bool }`, `func NewGeo(snap *extract.Snapshot, mask *explored.Mask) Geo`, `func MaskOf(snap *extract.Snapshot) *explored.Mask`, `func BiomeName(worldgen.Biome) string` (display names: "Black Forest", "Mountains"…).
  - `func Diff(prev, cur *extract.Snapshot, geo Geo) []Event`, `func NewTombstones(prev, cur *extract.Snapshot) []extract.Marker`, `func Near(locs []extract.Marker, geo Geo, x, z float32) string`.
  - `func NewDeriver(st *store.Store, log *slog.Logger) *Deriver`, `func (*Deriver) CatchUp(ctx, serverID string) (int, error)`.
  - `server.Deps.World *worldevents.Deriver` (nil: the handler makes its own).

- [ ] **Step 1: Write the failing tests for the diff and the deriver**

The diff tests use a small synthetic world and a fake `Geo` (west of x = 0 is Swamp, west of x = -5000 is unexplored). The deriver tests use an on-disk store: the in-memory one's shared cache reports "table is locked" to concurrent writers instead of waiting.

Create `internal/worldevents/diff_test.go`:

```go
package worldevents

import (
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
)

// fakeGeo: west of x = 0 is Swamp, east is Meadows; everything west of
// x = -5000 is unexplored.
type fakeGeo struct{}

func (fakeGeo) Biome(x, z float32) string {
	if x < 0 {
		return "Swamp"
	}
	return "Meadows"
}
func (fakeGeo) Explored(x, z float32) bool { return x > -5000 }

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// world is a small synthetic save: two paired portals, an unpaired one, a
// tombstone, two tames (one named), a base, a sunken crypt and Eikthyr
// defeated.
func world(saveID string, at time.Time) *extract.Snapshot {
	return &extract.Snapshot{
		ServerID: "srv", SaveID: saveID, SavedAt: at,
		GlobalKeys: []string{"defeated_eikthyr"},
		Locations: []extract.Marker{
			{ID: "loc-1", Kind: "dungeon", Label: "Sunken crypt", X: -1000, Z: -900},
			{ID: "loc-2", Kind: "dungeon", Label: "Burial chambers", X: 500, Z: 500},
			{ID: "loc-3", Kind: "trader", Label: "Haldor", X: -6000, Z: 0}, // unexplored
		},
		Markers: []extract.Marker{
			{ID: "portal-1", Kind: "portal", Label: "home", X: 10, Z: 10, Pair: "portal-2", Owner: "Astrid"},
			{ID: "portal-2", Kind: "portal", Label: "home", X: 3000, Z: 10, Pair: "portal-1", Owner: "Astrid"},
			{ID: "portal-3", Kind: "portal", Label: "copper", X: 900, Z: -1200, Owner: "Bjorn"},
			{ID: "tombstone-1", Kind: "tombstone", Owner: "Bjorn", X: -1100, Z: -800},
			{ID: "tame-1", Kind: "tame", Species: "Lox", Label: "Big Mama", X: 160, Z: -40},
			{ID: "tame-2", Kind: "tame", Species: "Hen", X: 95, Z: -60},
		},
		Bases: []extract.Base{
			{ID: "base-1", Name: "Astrid's base", X: 128, Z: -80, Radius: 60, Pieces: 400,
				Builders: []extract.Builder{{ID: 1, Name: "Astrid", Pieces: 300}, {ID: 2, Pieces: 100}}},
		},
	}
}

func types(evs []Event) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

func TestDiffFirstSaveAndUnchangedSaveYieldNothing(t *testing.T) {
	a := world("chunked:1", t0)
	if evs := Diff(nil, a, fakeGeo{}); len(evs) != 0 {
		t.Fatalf("first save: %+v", evs)
	}
	b := world("chunked:2", t0.Add(20*time.Minute))
	// Same world, objects a little shuffled and the tames wandered off.
	b.Markers[0], b.Markers[1] = b.Markers[1], b.Markers[0]
	b.Markers[4].X, b.Markers[4].Z = 400, 300
	if evs := Diff(a, b, fakeGeo{}); len(evs) != 0 {
		t.Fatalf("unchanged save: %+v", evs)
	}
}

func TestDiffNewTombstoneWithPlace(t *testing.T) {
	a := world("chunked:1", t0)
	b := world("chunked:2", t0.Add(20*time.Minute))
	b.Markers = append(b.Markers,
		extract.Marker{ID: "tombstone-2", Kind: "tombstone", Owner: "Astrid", X: -1050, Z: -950}, // 71 m from the crypt
		extract.Marker{ID: "tombstone-3", Kind: "tombstone", Owner: "Bjorn", X: 2000, Z: 2000},   // nothing near
		extract.Marker{ID: "tombstone-4", Kind: "tombstone", Owner: "Bjorn", X: -1102, Z: -801},  // Bjorn's old one, re-read
	)
	b.Markers = append(b.Markers[:3], b.Markers[4:]...) // drop tombstone-1: -1102,-801 is within MatchRadius of it
	evs := Diff(a, b, fakeGeo{})
	if len(evs) != 2 {
		t.Fatalf("events = %+v", evs)
	}
	e := evs[0]
	if e.Type != TypeTombstone || e.Owner != "Astrid" || e.Near != "a sunken crypt" || e.Biome != "Swamp" ||
		e.Pos == nil || e.Pos.X != -1050 || !e.At.Equal(b.SavedAt) || e.SaveID != "chunked:2" {
		t.Fatalf("tombstone = %+v", e)
	}
	if evs[1].Owner != "Bjorn" || evs[1].Near != "" || evs[1].Biome != "Meadows" {
		t.Fatalf("second tombstone = %+v", evs[1])
	}
	if again := Diff(a, b, fakeGeo{}); again[0].ID != e.ID || again[1].ID != evs[1].ID {
		t.Fatal("ids differ between runs")
	}
}

func TestDiffPortals(t *testing.T) {
	a := world("chunked:1", t0)
	a.Markers = append(a.Markers, extract.Marker{ID: "portal-4", Kind: "portal", Label: "swamp", X: -200, Z: -200})
	b := world("chunked:2", t0.Add(20*time.Minute))
	b.Markers = append(b.Markers,
		// The old "swamp" portal now has a partner that's new: one event, for the new one.
		extract.Marker{ID: "portal-4", Kind: "portal", Label: "swamp", X: -200, Z: -200, Pair: "portal-5"},
		extract.Marker{ID: "portal-5", Kind: "portal", Label: "swamp", X: -900, Z: 400, Pair: "portal-4", Owner: "Ulf"},
		extract.Marker{ID: "portal-6", Kind: "portal", Label: "lonely", X: 50, Z: 50, Owner: "Sigrun"},
	)
	evs := Diff(a, b, fakeGeo{})
	if got := types(evs); len(got) != 2 || got[0] != TypePortal || got[1] != TypePortal {
		t.Fatalf("types = %v (%+v)", got, evs)
	}
	if e := evs[0]; e.Tag != "swamp" || !e.Paired || e.Owner != "Ulf" || e.Pos.X != -900 {
		t.Fatalf("swamp = %+v", e)
	}
	if e := evs[1]; e.Tag != "lonely" || e.Paired || e.Owner != "Sigrun" {
		t.Fatalf("lonely = %+v", e)
	}

	// Later, two old unpaired portals become a pair (one was retagged).
	c := world("chunked:3", t0.Add(40*time.Minute))
	c.Markers = append(c.Markers,
		extract.Marker{ID: "portal-4", Kind: "portal", Label: "swamp", X: -200, Z: -200, Pair: "portal-5"},
		extract.Marker{ID: "portal-5", Kind: "portal", Label: "swamp", X: -900, Z: 400, Pair: "portal-4", Owner: "Ulf"},
		extract.Marker{ID: "portal-6", Kind: "portal", Label: "copper", X: 50, Z: 50, Pair: "portal-3", Owner: "Sigrun"},
	)
	c.Markers[2].Pair = "portal-6" // the copper portal from world()
	evs = Diff(b, c, fakeGeo{})
	// portal-6 changed tag, so it is a new portal (paired at once); copper's
	// partner is new, so copper itself reports nothing.
	if got := types(evs); len(got) != 1 || got[0] != TypePortal || !evs[0].Paired || evs[0].Tag != "copper" {
		t.Fatalf("retag: %+v", evs)
	}

	// Two old portals that become paired without either being new.
	d := world("chunked:4", t0.Add(60*time.Minute))
	d.Markers = append(d.Markers, extract.Marker{ID: "portal-4", Kind: "portal", Label: "copper", X: 2000, Z: 2000})
	e := world("chunked:5", t0.Add(80*time.Minute))
	e.Markers = append(e.Markers, extract.Marker{ID: "portal-4", Kind: "portal", Label: "copper", X: 2000, Z: 2000, Pair: "portal-3"})
	e.Markers[2].Pair = "portal-4"
	evs = Diff(d, e, fakeGeo{})
	if got := types(evs); len(got) != 2 || got[0] != TypePortalPaired || got[1] != TypePortalPaired {
		t.Fatalf("pairing later: %+v", evs)
	}
	if evs[0].Tag != "copper" || evs[0].Owner != "Bjorn" {
		t.Fatalf("paired = %+v", evs[0])
	}
}

func TestDiffTames(t *testing.T) {
	a := world("chunked:1", t0)
	b := world("chunked:2", t0.Add(20*time.Minute))
	b.Markers = append(b.Markers,
		extract.Marker{ID: "tame-3", Kind: "tame", Species: "Wolf", Label: "Skoll", Namer: "Steam_1", X: -300, Z: 0},
		extract.Marker{ID: "tame-4", Kind: "tame", Species: "Wolf", X: -310, Z: 0}, // unnamed: not reported
	)
	// The agent update adds namers to existing tames: not a new tame.
	b.Markers[4].Namer = "Steam_2"
	evs := Diff(a, b, fakeGeo{})
	if len(evs) != 1 {
		t.Fatalf("events = %+v", evs)
	}
	if e := evs[0]; e.Type != TypeTame || e.Name != "Skoll" || e.Species != "Wolf" || e.Namer != "Steam_1" || e.Biome != "Swamp" {
		t.Fatalf("tame = %+v", e)
	}
	// A second Big Mama is a new tame too.
	c := world("chunked:3", t0.Add(40*time.Minute))
	c.Markers = append(c.Markers, extract.Marker{ID: "tame-5", Kind: "tame", Species: "Lox", Label: "Big Mama", X: 600, Z: 0})
	if evs := Diff(a, c, fakeGeo{}); len(evs) != 1 || evs[0].Name != "Big Mama" || evs[0].Pos.X != 600 {
		t.Fatalf("second Big Mama: %+v", evs)
	}
}

func TestDiffBases(t *testing.T) {
	a := world("chunked:1", t0)
	b := world("chunked:2", t0.Add(20*time.Minute))
	b.Bases[0].Pieces = 424 // +24: under GrowthMin
	b.Bases[0].X += 20      // the centre drifts as it grows
	b.Bases = append(b.Bases, extract.Base{ID: "base-2", Name: "Ulf's base", X: -2000, Z: 50, Radius: 30, Pieces: 60,
		Builders: []extract.Builder{{ID: 4, Name: "Ulf", Pieces: 60}}})
	evs := Diff(a, b, fakeGeo{})
	if len(evs) != 1 {
		t.Fatalf("events = %+v", evs)
	}
	if e := evs[0]; e.Type != TypeBaseNew || e.Name != "Ulf's base" || e.Pieces != 60 || e.Biome != "Swamp" ||
		len(e.Builders) != 1 || e.Builders[0] != "Ulf" {
		t.Fatalf("new base = %+v", e)
	}
	c := world("chunked:3", t0.Add(40*time.Minute))
	c.Bases[0].Pieces = 425 // +25 on chunked:1
	evs = Diff(a, c, fakeGeo{})
	if len(evs) != 1 || evs[0].Type != TypeBaseGrew || evs[0].Grew != 25 || evs[0].Pieces != 425 ||
		len(evs[0].Builders) != 1 || evs[0].Builders[0] != "Astrid" {
		t.Fatalf("grown base = %+v", evs)
	}
}

func TestDiffBosses(t *testing.T) {
	a := world("chunked:1", t0)
	b := world("chunked:2", t0.Add(20*time.Minute))
	b.GlobalKeys = []string{"defeated_eikthyr", "defeated_gdking", "killedtroll", "defeated_frozenking"}
	evs := Diff(a, b, fakeGeo{})
	if len(evs) != 2 || evs[0].Boss != "The Elder" || evs[1].Boss != "Kall Fimbulbringer" || evs[0].Pos != nil {
		t.Fatalf("bosses = %+v", evs)
	}
	// A previous snapshot stored before global keys were sent: its own
	// boss list counts.
	old := world("chunked:0", t0.Add(-20*time.Minute))
	old.GlobalKeys = nil
	old.Bosses = []extract.Boss{{Key: "defeated_eikthyr", Name: "Eikthyr", Defeated: true}}
	if evs := Diff(old, a, fakeGeo{}); len(evs) != 0 {
		t.Fatalf("keys appearing must not re-report Eikthyr: %+v", evs)
	}
}

func TestNearWording(t *testing.T) {
	locs := world("x", t0).Locations
	locs = append(locs,
		extract.Marker{Kind: "boss_altar", Label: "Moder", X: 5000, Z: 5000},
		extract.Marker{Kind: "trader", Label: "Hildir", X: -4000, Z: 4000},
	)
	for _, c := range []struct {
		x, z float32
		want string
	}{
		{-1000, -700, "a sunken crypt"},
		{520, 480, "burial chambers"},
		{5100, 5100, "Moder’s altar"},
		{-4100, 4000, "Hildir"},
		{-6000, 50, ""}, // Haldor is there but unexplored
		{0, 0, ""},
	} {
		if got := Near(locs, fakeGeo{}, c.x, c.z); got != c.want {
			t.Errorf("Near(%v, %v) = %q, want %q", c.x, c.z, got, c.want)
		}
	}
}
```

Create `internal/worldevents/deriver_test.go`:

```go
package worldevents

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/store"
)

// newStore opens an on-disk store: the in-memory one's shared cache
// reports "table is locked" to concurrent writers instead of waiting.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "farsight.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func put(t *testing.T, st *store.Store, s *extract.Snapshot) {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutSnapshot(context.Background(), "srv", s.SaveID, s.SavedAt, s.SavedAt, b); err != nil {
		t.Fatal(err)
	}
}

func storedTypes(t *testing.T, st *store.Store) []string {
	t.Helper()
	evs, err := st.EventsBetween(context.Background(), "srv", t0.Add(-time.Hour), t0.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for i := len(evs) - 1; i >= 0; i-- { // oldest first
		out = append(out, evs[i].Type)
	}
	return out
}

func TestCatchUpBackfillsThenFollows(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	d := NewDeriver(st, nil)

	s1 := world("chunked:1", t0) // baseline: Bjorn's tombstone predates tracking
	s2 := world("chunked:2", t0.Add(20*time.Minute))
	s2.Markers = append(s2.Markers, extract.Marker{ID: "tombstone-2", Kind: "tombstone", Owner: "Astrid", X: 700, Z: 700})
	put(t, st, s2) // stored out of order: replay goes by save time
	put(t, st, s1)

	n, err := d.CatchUp(ctx, "srv")
	if err != nil || n != 1 {
		t.Fatalf("backfill wrote %d (err %v), want 1", n, err)
	}
	if got := storedTypes(t, st); len(got) != 1 || got[0] != TypeTombstone {
		t.Fatalf("events = %v", got)
	}
	state, ok, err := st.WorldDiffState(ctx, nil, "srv")
	if err != nil || !ok || state.SaveID != "chunked:2" {
		t.Fatalf("state = %+v ok=%v err=%v", state, ok, err)
	}
	bjorn, _ := st.Tombstones(ctx, "srv", "Bjorn")
	astrid, _ := st.Tombstones(ctx, "srv", "Astrid")
	if len(bjorn) != 1 || !bjorn[0].FirstSeen.Equal(t0) || len(astrid) != 1 || !astrid[0].FirstSeen.Equal(s2.SavedAt) {
		t.Fatalf("tombstones: Bjorn %+v, Astrid %+v", bjorn, astrid)
	}

	if n, err := d.CatchUp(ctx, "srv"); err != nil || n != 0 {
		t.Fatalf("second run wrote %d (err %v), want 0", n, err)
	}

	s3 := world("chunked:3", t0.Add(40*time.Minute))
	s3.Markers = append(s3.Markers, extract.Marker{ID: "tombstone-2", Kind: "tombstone", Owner: "Astrid", X: 700, Z: 700})
	s3.GlobalKeys = append(s3.GlobalKeys, "defeated_gdking")
	put(t, st, s3)
	if n, err := d.CatchUp(ctx, "srv"); err != nil || n != 1 {
		t.Fatalf("follow-up wrote %d (err %v), want 1 (The Elder)", n, err)
	}
	if got := storedTypes(t, st); len(got) != 2 || got[1] != TypeBoss {
		t.Fatalf("events = %v", got)
	}
}

func TestCatchUpConcurrentRunsWriteEachEventOnce(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	s1 := world("chunked:1", t0)
	s2 := world("chunked:2", t0.Add(20*time.Minute))
	s2.GlobalKeys = append(s2.GlobalKeys, "defeated_gdking")
	put(t, st, s1)
	put(t, st, s2)

	var wg sync.WaitGroup
	total := make([]int, 4)
	for i := range total {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Two Derivers (as after a restart) and two runs each.
			d := NewDeriver(st, nil)
			n, err := d.CatchUp(ctx, "srv")
			if err != nil {
				t.Error(err)
			}
			total[i] = n
		}(i)
	}
	wg.Wait()
	sum := 0
	for _, n := range total {
		sum += n
	}
	if sum != 1 {
		t.Fatalf("events written across runs = %d, want 1", sum)
	}
	if got := storedTypes(t, st); len(got) != 1 {
		t.Fatalf("events = %v", got)
	}
}

func TestCatchUpAfterThePreviousSnapshotWasPruned(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	d := NewDeriver(st, nil)
	s1 := world("chunked:1", t0)
	put(t, st, s1)
	if _, err := d.CatchUp(ctx, "srv"); err != nil {
		t.Fatal(err)
	}
	s2 := world("chunked:2", t0.Add(20*time.Minute))
	s2.Markers = append(s2.Markers, extract.Marker{ID: "tombstone-2", Kind: "tombstone", Owner: "Ulf", X: 5, Z: 5})
	put(t, st, s2)
	// chunked:1 is pruned before chunked:2 is diffed.
	if _, err := st.PruneSnapshots(ctx, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	n, err := d.CatchUp(ctx, "srv")
	if err != nil || n != 0 {
		t.Fatalf("wrote %d (err %v), want 0: a new baseline", n, err)
	}
	bjorn, _ := st.Tombstones(ctx, "srv", "Bjorn")
	ulf, _ := st.Tombstones(ctx, "srv", "Ulf")
	if len(bjorn) != 1 || len(ulf) != 1 {
		t.Fatalf("tombstones: Bjorn %+v (want the one stored), Ulf %+v (want 1)", bjorn, ulf)
	}
}
```

- [ ] **Step 2: Run them to make sure they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/worldevents/`

Expected: FAIL at build: `undefined: Diff`, `undefined: NewDeriver`, `undefined: TypeTombstone`…

- [ ] **Step 3: Add the event types and ids**

Create `internal/worldevents/event.go`:

```go
// Package worldevents turns the differences between two consecutive world
// saves into timeline events (a new tombstone, portal, tame or base, a
// base that grew, a boss defeated) and keeps the event log in step with
// the stored snapshots: Diff is the pure comparison, Deriver replays
// stored snapshots through it.
package worldevents

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"
)

// Event types, as stored in the events table's type column.
const (
	TypeTombstone    = "world_tombstone"
	TypePortal       = "world_portal"
	TypePortalPaired = "world_portal_paired"
	TypeTame         = "world_tame"
	TypeBaseNew      = "world_base_new"
	TypeBaseGrew     = "world_base_grew"
	TypeBoss         = "world_boss"
)

const (
	// GrowthMin is the smallest growth, in pieces, reported for a base.
	GrowthMin = 25
	// MatchRadius is how close (metres) a tombstone or portal must be to
	// one with the same owner or tag in the previous save to be the same
	// object. Neither moves; the slack absorbs float noise.
	MatchRadius = 4
	// NearRadius is how close (metres) a known location must be to be
	// named in an event's place ("near a sunken crypt").
	NearRadius = 300
	// baseLink is the slack (metres) beyond a previous base's radius
	// within which a base's centre is taken to be the same base.
	baseLink = 32
)

// Pos is a world position (x east, z north), in metres.
type Pos struct {
	X float32 `json:"x"`
	Z float32 `json:"z"`
}

// Event is one world-save event. At is the save's time, never the moment
// it happened. Only the fields its type uses are set.
type Event struct {
	ID     string    `json:"id"`
	Type   string    `json:"type"`
	At     time.Time `json:"at"`
	SaveID string    `json:"saveId"`
	Pos    *Pos      `json:"pos,omitempty"`

	Owner    string   `json:"owner,omitempty"`    // tombstone owner; portal creator (name)
	Namer    string   `json:"namer,omitempty"`    // tame namer (platform user ID, "Steam_…")
	Builders []string `json:"builders,omitempty"` // base builders (names)

	Tag     string `json:"tag,omitempty"`     // portal tag
	Paired  bool   `json:"paired,omitempty"`  // world_portal: paired in the save it appeared in
	Name    string `json:"name,omitempty"`    // tame or base name
	Species string `json:"species,omitempty"` // tame species (prefab name)
	Pieces  int    `json:"pieces,omitempty"`  // base pieces now
	Grew    int    `json:"grew,omitempty"`    // world_base_grew: pieces added
	Boss    string `json:"boss,omitempty"`    // world_boss: the boss's name

	Biome string `json:"biome,omitempty"` // display name: "Swamp", "Black Forest"…
	Near  string `json:"near,omitempty"`  // a known location within NearRadius: "a sunken crypt"
}

// eventID is the deterministic id of the typ event for the object named by
// key in save saveID, so replaying a save pair writes the same ids.
func eventID(typ, saveID string, key ...any) string {
	parts := []string{typ, saveID}
	for _, k := range key {
		parts = append(parts, fmt.Sprint(k))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "w" + hex.EncodeToString(sum[:8])
}

// round is a coordinate rounded to whole metres, for ids.
func round(v float32) int { return int(math.Round(float64(v))) }

func dist(ax, az, bx, bz float32) float64 {
	return math.Hypot(float64(ax-bx), float64(az-bz))
}
```

- [ ] **Step 4: Add the geography: biomes and the explored mask**

Create `internal/worldevents/geo.go`:

```go
package worldevents

import (
	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

// Geo answers the place questions events need about one save's world.
type Geo interface {
	// Biome is the display name of the biome at (x, z): "Black Forest".
	Biome(x, z float32) string
	// Explored reports whether (x, z) is in explored ground.
	Explored(x, z float32) bool
}

// biomeNames are the biomes' display names, as the map legend shows them.
var biomeNames = map[worldgen.Biome]string{
	worldgen.Meadows:     "Meadows",
	worldgen.BlackForest: "Black Forest",
	worldgen.Swamp:       "Swamp",
	worldgen.Mountain:    "Mountains",
	worldgen.Plains:      "Plains",
	worldgen.Mistlands:   "Mistlands",
	worldgen.AshLands:    "Ashlands",
	worldgen.DeepNorth:   "Deep North",
	worldgen.Ocean:       "Ocean",
}

// BiomeName is b's display name ("" for none).
func BiomeName(b worldgen.Biome) string { return biomeNames[b] }

type snapGeo struct {
	gen  *worldgen.Generator
	mask *explored.Mask
}

// NewGeo is the Geo of snap's world, explored per mask. Biomes come from
// the world generator's base pass (worldgen.NewBase): GetBiome needs no
// rivers, so the lake and river pre-generation is skipped.
func NewGeo(snap *extract.Snapshot, mask *explored.Mask) Geo {
	return snapGeo{gen: worldgen.NewBase(snap.World.Seed, snap.World.GenVersion), mask: mask}
}

func (g snapGeo) Biome(x, z float32) string { return BiomeName(g.gen.Biome(x, z)) }

func (g snapGeo) Explored(x, z float32) bool { return g.mask.At(float64(x), float64(z)) }

// MaskOf is snap's explored mask: the agent's 12 m mask, or for agents
// that predate it the generated zones, as the central app's tiles use.
func MaskOf(snap *extract.Snapshot) *explored.Mask {
	if snap.Explored != nil {
		if m, err := explored.Decode(*snap.Explored); err == nil {
			return m
		}
	}
	return explored.FromZones(snap.ExploredZones)
}
```

- [ ] **Step 5: Add the diff**

Create `internal/worldevents/diff.go`:

```go
package worldevents

import (
	"strings"

	"github.com/jumpingmushroom/farsight/internal/extract"
)

// Diff returns the events between two consecutive saves of one world, in a
// stable order: tombstones, portals, tames, bases, bosses. prev nil (the
// first save seen) yields none: everything in it predates tracking. Place
// wording comes from geo, which describes cur's world.
func Diff(prev, cur *extract.Snapshot, geo Geo) []Event {
	if prev == nil {
		return nil
	}
	d := differ{prev: prev, cur: cur, geo: geo}
	d.tombstones()
	d.portals()
	d.tames()
	d.bases()
	d.bosses()
	return d.out
}

type differ struct {
	prev, cur *extract.Snapshot
	geo       Geo
	out       []Event
}

func (d *differ) add(e Event) {
	e.At, e.SaveID = d.cur.SavedAt, d.cur.SaveID
	d.out = append(d.out, e)
}

// place fills in the biome and nearby location for an event at (x, z).
func (d *differ) place(e *Event, x, z float32) {
	e.Pos = &Pos{X: x, Z: z}
	e.Biome = d.geo.Biome(x, z)
	e.Near = Near(d.cur.Locations, d.geo, x, z)
}

func ofKind(ms []extract.Marker, kind string) []extract.Marker {
	var out []extract.Marker
	for _, m := range ms {
		if m.Kind == kind {
			out = append(out, m)
		}
	}
	return out
}

// match returns the marker in prev that is the same object as m: same
// key, within MatchRadius.
func match(m extract.Marker, prev []extract.Marker, key func(extract.Marker) string) (extract.Marker, bool) {
	for _, p := range prev {
		if key(p) == key(m) && dist(p.X, p.Z, m.X, m.Z) <= MatchRadius {
			return p, true
		}
	}
	return extract.Marker{}, false
}

func ownerKey(m extract.Marker) string { return m.Owner }
func labelKey(m extract.Marker) string { return m.Label }

// NewTombstones returns cur's tombstones with no match (same owner, within
// MatchRadius) in prev; all of them when prev is nil.
func NewTombstones(prev, cur *extract.Snapshot) []extract.Marker {
	var before []extract.Marker
	if prev != nil {
		before = ofKind(prev.Markers, "tombstone")
	}
	var out []extract.Marker
	for _, m := range ofKind(cur.Markers, "tombstone") {
		if _, ok := match(m, before, ownerKey); !ok {
			out = append(out, m)
		}
	}
	return out
}

func (d *differ) tombstones() {
	for _, m := range NewTombstones(d.prev, d.cur) {
		e := Event{ID: eventID(TypeTombstone, d.cur.SaveID, m.Owner, round(m.X), round(m.Z)), Type: TypeTombstone, Owner: m.Owner}
		d.place(&e, m.X, m.Z)
		d.add(e)
	}
}

// portals reports new portals (matched by tag and position, not owner:
// snapshots from agents before Plan 7 have no owner), and old portals that
// became paired with another old portal. A portal paired with a new one is
// covered by the new one's event.
func (d *differ) portals() {
	before := ofKind(d.prev.Markers, "portal")
	now := ofKind(d.cur.Markers, "portal")
	isNew := map[string]bool{}
	for _, m := range now {
		if _, ok := match(m, before, labelKey); !ok {
			isNew[m.ID] = true
		}
	}
	for _, m := range now {
		if isNew[m.ID] {
			e := Event{ID: eventID(TypePortal, d.cur.SaveID, m.Label, round(m.X), round(m.Z)), Type: TypePortal,
				Tag: m.Label, Paired: m.Pair != "", Owner: m.Owner}
			d.place(&e, m.X, m.Z)
			d.add(e)
			continue
		}
		p, _ := match(m, before, labelKey)
		if p.Pair == "" && m.Pair != "" && !isNew[m.Pair] {
			e := Event{ID: eventID(TypePortalPaired, d.cur.SaveID, m.Label, round(m.X), round(m.Z)), Type: TypePortalPaired,
				Tag: m.Label, Owner: m.Owner}
			d.place(&e, m.X, m.Z)
			d.add(e)
		}
	}
}

// tames reports named tames. Tames walk about, so they are counted per
// (species, name) rather than matched by position; a key with more tames
// than before reports its extra ones (the last in save order). Unnamed
// tames (bred or freshly tamed animals) are not reported.
func (d *differ) tames() {
	key := func(m extract.Marker) string { return m.Species + "\x00" + m.Label }
	had := map[string]int{}
	for _, m := range ofKind(d.prev.Markers, "tame") {
		if m.Label != "" {
			had[key(m)]++
		}
	}
	byKey := map[string][]extract.Marker{}
	var order []string
	for _, m := range ofKind(d.cur.Markers, "tame") {
		if m.Label == "" {
			continue
		}
		k := key(m)
		if byKey[k] == nil {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], m)
	}
	for _, k := range order {
		ms := byKey[k]
		for i := had[k]; i < len(ms); i++ {
			m := ms[i]
			e := Event{ID: eventID(TypeTame, d.cur.SaveID, m.Species, m.Label, i), Type: TypeTame,
				Name: m.Label, Species: m.Species, Namer: m.Namer}
			d.place(&e, m.X, m.Z)
			d.add(e)
		}
	}
}

// bases matches each base to the previous base whose centre is nearest,
// among those within that base's radius plus baseLink: base ids are a
// size ranking, not stable across saves. Unmatched bases are new; matched
// ones that grew by GrowthMin or more pieces are reported.
func (d *differ) bases() {
	for _, b := range d.cur.Bases {
		var best *extract.Base
		bestD := 0.0
		for i := range d.prev.Bases {
			p := &d.prev.Bases[i]
			dd := dist(p.X, p.Z, b.X, b.Z)
			if dd <= float64(p.Radius)+baseLink && (best == nil || dd < bestD) {
				best, bestD = p, dd
			}
		}
		var builders []string
		for _, bl := range b.Builders {
			if bl.Name != "" {
				builders = append(builders, bl.Name)
			}
		}
		switch {
		case best == nil:
			e := Event{ID: eventID(TypeBaseNew, d.cur.SaveID, round(b.X), round(b.Z)), Type: TypeBaseNew,
				Name: b.Name, Pieces: b.Pieces, Builders: builders}
			d.place(&e, b.X, b.Z)
			d.add(e)
		case b.Pieces-best.Pieces >= GrowthMin:
			e := Event{ID: eventID(TypeBaseGrew, d.cur.SaveID, round(b.X), round(b.Z)), Type: TypeBaseGrew,
				Name: b.Name, Pieces: b.Pieces, Grew: b.Pieces - best.Pieces, Builders: builders}
			d.place(&e, b.X, b.Z)
			d.add(e)
		}
	}
}

// defeated is the set of bosses snap shows defeated: from its global keys
// (extract.BossesFromKeys), or, for a snapshot stored before global keys
// were sent, from its own boss list.
func defeated(snap *extract.Snapshot) map[string]bool {
	out := map[string]bool{}
	bosses := snap.Bosses
	if snap.GlobalKeys != nil {
		bosses = extract.BossesFromKeys(snap.GlobalKeys)
	}
	for _, b := range bosses {
		if b.Defeated {
			out[b.Key] = true
		}
	}
	return out
}

func (d *differ) bosses() {
	was := defeated(d.prev)
	for _, b := range extract.BossesFromKeys(d.cur.GlobalKeys) {
		if b.Defeated && !was[b.Key] {
			d.add(Event{ID: eventID(TypeBoss, d.cur.SaveID, b.Key), Type: TypeBoss, Boss: b.Name})
		}
	}
}

// Near names the known location (boss altar, trader or dungeon) nearest
// to (x, z) within NearRadius, in explored ground only, for "near …"
// wording: "a sunken crypt", "burial chambers", "Haldor", "Moder’s altar".
// "" if there is none.
func Near(locs []extract.Marker, geo Geo, x, z float32) string {
	var best *extract.Marker
	bestD := 0.0
	for i := range locs {
		l := &locs[i]
		dd := dist(l.X, l.Z, x, z)
		if dd > NearRadius || !geo.Explored(l.X, l.Z) {
			continue
		}
		if best == nil || dd < bestD {
			best, bestD = l, dd
		}
	}
	if best == nil {
		return ""
	}
	switch best.Kind {
	case "trader":
		return best.Label
	case "boss_altar":
		return best.Label + "’s altar"
	}
	name := strings.ToLower(best.Label)
	if strings.HasSuffix(name, "s") { // "burial chambers"
		return name
	}
	return "a " + name
}
```

- [ ] **Step 6: Add the deriver**

Create `internal/worldevents/deriver.go`:

```go
package worldevents

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/store"
)

// Deriver keeps a server's world-save events in step with its stored
// snapshots: CatchUp diffs every snapshot newer than the last one diffed
// against the one before it and writes the events and newly seen
// tombstones. The first run for a server (no world_diff row) is the
// backfill: it replays every stored snapshot, oldest first, the first one
// being the baseline. Re-running is harmless: ids are derived from the
// save and the object, and inserts ignore existing rows.
type Deriver struct {
	store *store.Store
	log   *slog.Logger

	mu    sync.Mutex
	locks map[string]*sync.Mutex // one per server: CatchUps for a server run one at a time
}

// NewDeriver returns a Deriver over st. log may be nil.
func NewDeriver(st *store.Store, log *slog.Logger) *Deriver {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Deriver{store: st, log: log, locks: map[string]*sync.Mutex{}}
}

func (d *Deriver) lock(serverID string) *sync.Mutex {
	d.mu.Lock()
	defer d.mu.Unlock()
	l := d.locks[serverID]
	if l == nil {
		l = &sync.Mutex{}
		d.locks[serverID] = l
	}
	return l
}

// load decodes one stored snapshot; ok is false if it isn't stored.
func (d *Deriver) load(ctx context.Context, serverID, saveID string) (*extract.Snapshot, bool, error) {
	blob, ok, err := d.store.Snapshot(ctx, serverID, saveID)
	if err != nil || !ok {
		return nil, false, err
	}
	var s extract.Snapshot
	if err := json.Unmarshal(blob, &s); err != nil {
		return nil, false, fmt.Errorf("worldevents: decode snapshot %s: %w", saveID, err)
	}
	return &s, true, nil
}

// CatchUp brings serverID's world events up to its newest stored snapshot
// and returns the number of events written.
func (d *Deriver) CatchUp(ctx context.Context, serverID string) (int, error) {
	l := d.lock(serverID)
	l.Lock()
	defer l.Unlock()

	state, backfilled, err := d.store.WorldDiffState(ctx, nil, serverID)
	if err != nil {
		return 0, err
	}
	keys, err := d.store.SnapshotsAfter(ctx, serverID, state)
	if err != nil || len(keys) == 0 {
		return 0, err
	}
	var prev *extract.Snapshot
	if backfilled {
		if prev, _, err = d.load(ctx, serverID, state.SaveID); err != nil {
			return 0, err
		}
		// prev nil here means the last diffed snapshot was pruned: the next
		// one becomes a new baseline (its tombstones are matched against
		// the stored ones instead).
	}
	written := 0
	for _, k := range keys {
		cur, ok, err := d.load(ctx, serverID, k.SaveID)
		if err != nil {
			return written, err
		}
		if !ok {
			continue // pruned since SnapshotsAfter listed it
		}
		n, err := d.apply(ctx, serverID, k, prev, cur, backfilled)
		if err != nil {
			return written, err
		}
		written += n
		prev, backfilled = cur, true
	}
	if written > 0 {
		d.log.Info("world events", "server", serverID, "events", written, "saves", len(keys))
	}
	return written, nil
}

// apply writes cur's events (against prev) and its new tombstones, and
// advances the diff state to k, in one transaction. With prev nil, cur is
// a baseline: no events. Its tombstones are all new on the very first run
// (seenBefore false); after a gap they are new only if no stored tombstone
// of the same owner lies within MatchRadius.
func (d *Deriver) apply(ctx context.Context, serverID string, k store.SnapshotKey, prev, cur *extract.Snapshot, seenBefore bool) (int, error) {
	evs := Diff(prev, cur, NewGeo(cur, MaskOf(cur)))
	tombs := NewTombstones(prev, cur)
	if prev == nil && seenBefore {
		var err error
		if tombs, err = d.unseen(ctx, serverID, tombs); err != nil {
			return 0, err
		}
	}
	written := 0
	err := d.store.Tx(ctx, func(tx *sql.Tx) error {
		for _, e := range evs {
			body, err := json.Marshal(e)
			if err != nil {
				return err
			}
			ok, err := d.store.InsertRawEvent(ctx, tx, serverID, store.StoredEvent{ID: e.ID, Type: e.Type, At: e.At, Body: body})
			if err != nil {
				return err
			}
			if ok {
				written++
			}
		}
		for _, m := range tombs {
			t := store.Tombstone{ID: eventID("tombstone", cur.SaveID, m.Owner, round(m.X), round(m.Z)),
				Owner: m.Owner, X: m.X, Z: m.Z, FirstSeen: cur.SavedAt}
			if _, err := d.store.InsertTombstone(ctx, tx, serverID, t); err != nil {
				return err
			}
		}
		return d.store.PutWorldDiffState(ctx, tx, serverID, k)
	})
	return written, err
}

// unseen drops the tombstones that match a stored one (same owner, within
// MatchRadius).
func (d *Deriver) unseen(ctx context.Context, serverID string, tombs []extract.Marker) ([]extract.Marker, error) {
	var out []extract.Marker
	for _, m := range tombs {
		stored, err := d.store.Tombstones(ctx, serverID, m.Owner)
		if err != nil {
			return nil, err
		}
		seen := false
		for _, s := range stored {
			if dist(s.X, s.Z, m.X, m.Z) <= MatchRadius {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, m)
		}
	}
	return out, nil
}
```

- [ ] **Step 7: Run the package tests to make sure they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/worldevents/`

Expected: `ok`.

- [ ] **Step 8: Write the failing ingest and startup tests**

Create `internal/server/worldevents_test.go`:

```go
package server

import (
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
)

// Ingesting a save diffs it against the previous one into world events;
// re-posting a save writes nothing new.
func TestIngestDerivesWorldEvents(t *testing.T) {
	e := newEnv(t)
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-30*time.Minute))); err != nil {
		t.Fatal(err)
	}
	s2 := testSnapshot("s2", at(-10*time.Minute))
	s2.Markers = append(s2.Markers, extract.Marker{ID: "tombstone-1", Kind: "tombstone", Owner: "Alice", X: 5, Z: 5})
	for i := 0; i < 2; i++ {
		if err := e.post("alpha", "alpha-token", "snapshot", s2); err != nil {
			t.Fatal(err)
		}
	}
	evs, err := e.store.EventsBetween(t.Context(), "alpha", at(-time.Hour), at(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != worldevents.TypeTombstone || !evs[0].At.Equal(s2.SavedAt) {
		t.Fatalf("events = %+v", evs)
	}
	tombs, err := e.store.Tombstones(t.Context(), "alpha", "Alice")
	if err != nil || len(tombs) != 1 {
		t.Fatalf("Alice's tombstones = %+v err=%v", tombs, err)
	}
}
```

In `cmd/farsight/serve_test.go`, in the imports, replace:

```go
	"github.com/jumpingmushroom/farsight/internal/tileset"
)
```

with:

```go
	"github.com/jumpingmushroom/farsight/internal/tileset"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
)
```

Append to the end of `cmd/farsight/serve_test.go`, after a blank line:

```go
func TestBackfillWorldEventsOnStartup(t *testing.T) {
	st, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	cfg := &config.Config{Servers: []config.Server{{ID: "alpha"}, {ID: "beta"}}}
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	// alpha: two saves, Eikthyr defeated in between; beta: none.
	for i, keys := range []string{`[]`, `["defeated_eikthyr"]`} {
		at := t0.Add(time.Duration(i) * 20 * time.Minute)
		blob := fmt.Sprintf(`{"serverId":"alpha","saveId":"s%d","savedAt":%q,"world":{"seed":7,"genVersion":2},"globalKeys":%s}`, i, at.Format(time.RFC3339), keys)
		if _, err := st.PutSnapshot(ctx, "alpha", fmt.Sprintf("s%d", i), at, at, []byte(blob)); err != nil {
			t.Fatal(err)
		}
	}

	backfillWorldEvents(ctx, cfg, worldevents.NewDeriver(st, nil), slog.New(slog.DiscardHandler))

	evs, err := st.EventsBetween(ctx, "alpha", t0, t0.Add(time.Hour))
	if err != nil || len(evs) != 1 || evs[0].Type != worldevents.TypeBoss {
		t.Fatalf("alpha events = %+v err=%v", evs, err)
	}
	if k, ok, err := st.WorldDiffState(ctx, nil, "alpha"); err != nil || !ok || k.SaveID != "s1" {
		t.Fatalf("alpha state = %+v ok=%v err=%v", k, ok, err)
	}
	if _, ok, err := st.WorldDiffState(ctx, nil, "beta"); err != nil || ok {
		t.Fatalf("beta has no snapshots, so no state: ok=%v err=%v", ok, err)
	}
}
```

- [ ] **Step 9: Run them to make sure they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/server/ ./cmd/farsight/`

Expected: FAIL: `TestIngestDerivesWorldEvents` finds no events (`events = []`), and `cmd/farsight` stops at build with `undefined: backfillWorldEvents`.

- [ ] **Step 10: Derive on ingest**

In `internal/server/server.go`, in the imports, replace:

```go
	"github.com/jumpingmushroom/farsight/internal/tileset"
)
```

with:

```go
	"github.com/jumpingmushroom/farsight/internal/tileset"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
)
```

In `internal/server/server.go`, in `type Deps`, replace:

```go
	Tiles   *tileset.Manager
	Codec   auth.Codec
```

with:

```go
	Tiles   *tileset.Manager
	// World derives world-save events from stored snapshots; nil means a
	// Deriver of the handler's own over Store. serve passes the one its
	// startup backfill uses, so the two share per-server locks.
	World   *worldevents.Deriver
	Codec   auth.Codec
```

In `internal/server/server.go`, in `newServer`, replace:

```go
	if d.IngestLimiter == nil {
		d.IngestLimiter = auth.NewLimiter(10, 10, d.Now)
	}
```

with:

```go
	if d.IngestLimiter == nil {
		d.IngestLimiter = auth.NewLimiter(10, 10, d.Now)
	}
	if d.World == nil {
		d.World = worldevents.NewDeriver(d.Store, d.Log)
	}
```

In `internal/server/ingest.go`, in `ingestSnapshot`, replace:

```go
	s.Tiles.Ensure(snap.World.Seed, snap.World.GenVersion)
```

with:

```go
	s.Tiles.Ensure(snap.World.Seed, snap.World.GenVersion)
	// Diff the new save against the previous one into world events. Also
	// idempotent; a failure is logged, not the agent's problem: the next
	// save (or a restart's backfill) catches up.
	if _, err := s.World.CatchUp(r.Context(), id); err != nil {
		s.Log.Error("server: world events", "server", id, "err", err)
	}
```

- [ ] **Step 11: Backfill at startup**

In `cmd/farsight/serve.go`, in the imports, replace:

```go
	"github.com/jumpingmushroom/farsight/internal/tileset"
	"github.com/jumpingmushroom/farsight/web"
```

with:

```go
	"github.com/jumpingmushroom/farsight/internal/tileset"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
	"github.com/jumpingmushroom/farsight/web"
```

In `cmd/farsight/serve.go`, in `runServe`, replace:

```go
	applier := &live.Applier{Store: st, Now: time.Now}
```

with:

```go
	applier := &live.Applier{Store: st, Now: time.Now}
	world := worldevents.NewDeriver(st, log)
```

In `cmd/farsight/serve.go`, in the `server.Deps`, replace:

```go
		Tiles:         tiles,
		Codec:         auth.Codec{Key: cfg.CookieKey},
```

with:

```go
		Tiles:         tiles,
		World:         world,
		Codec:         auth.Codec{Key: cfg.CookieKey},
```

In `cmd/farsight/serve.go`, with the other background loops, replace:

```go
	loop(func(ctx context.Context) { every(ctx, pruneInterval, true, func() { prune(ctx, st, log) }) })
```

with:

```go
	loop(func(ctx context.Context) { every(ctx, pruneInterval, true, func() { prune(ctx, st, log) }) })
	loop(func(ctx context.Context) { backfillWorldEvents(ctx, cfg, world, log) })
```

In `cmd/farsight/serve.go`, just before `ensureTiles`, replace:

```go
// ensureTiles calls Ensure
```

with:

```go
// backfillWorldEvents brings every configured server's world-save events
// up to date once at startup: the first run for a server replays all its
// stored snapshots, later runs only what arrived while farsight was down.
// Errors are logged and the server skipped; ingest catches up later.
func backfillWorldEvents(ctx context.Context, cfg *config.Config, world *worldevents.Deriver, log *slog.Logger) {
	for _, s := range cfg.Servers {
		if ctx.Err() != nil {
			return
		}
		if _, err := world.CatchUp(ctx, s.ID); err != nil && ctx.Err() == nil {
			log.Error("startup world events", "server", s.ID, "err", err)
		}
	}
}

// ensureTiles calls Ensure
```

- [ ] **Step 12: Run the tests to make sure they pass**

Run: `gofmt -l ./cmd ./internal; PATH=$HOME/.local/go/bin:$PATH go vet ./... && PATH=$HOME/.local/go/bin:$PATH go test ./internal/worldevents/ ./internal/server/ ./cmd/farsight/`

Expected: `gofmt -l` prints nothing (run it as `$HOME/.local/go/bin/gofmt` if `gofmt` isn't on your PATH); `ok` for all three packages.

- [ ] **Step 13: Commit**

```bash
cd /workspace/Farsight
git add internal/worldevents \
  internal/server/server.go \
  internal/server/ingest.go \
  internal/server/worldevents_test.go \
  cmd/farsight/serve.go \
  cmd/farsight/serve_test.go
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(worldevents): world-save events from consecutive saves, with a backfill

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
git status --short   # must print nothing
```

---

### Task 5: Central app: per-server time zone and the player profile endpoint

`GET /api/servers/{id}/players/{player}` returns one player's profile. `{player}` is the platform ID as the sessions table stores it (`76561190000000001`, without the platform). 404 for a locked or unknown server and for a platform ID with no sessions, the same body as every other 404.

- **Time zone.** Local days need the server's log zone. The agent knows it (`FARSIGHT_LOG_TZ`), the central app doesn't, so each server in `farsight.json` gets an optional `timeZone` (IANA, default `UTC`), validated at load. `Server.Location()` also resolves a `Server` built without `Load`, as the handler tests build theirs.
- **Session maths** (`playtime.go`): a session's span is [since, until), an open one runs to now. The seven local days end with today and are calendar days, so a DST day is 23 or 25 hours long. A session across midnight is split. "This week" is the sum of those seven days, so the tile and the bars always agree.
- **Linking.** Every name the platform ID has played under links beds and tombstones (owner), bases (builders) and portals (owner, from Task 1). Tames link by namer, `platform + "_" + platformId`.
- **Explored only.** Save data comes from the latest save, filtered to explored ground like the snapshot API, and a portal is "paired" only when its partner is explored too. "N spotted" counts every distinct tombstone in the `tombstones` table (Task 4); "this week" counts those first seen in the seven days.
- **Beds.** A bed is named after the nearest explored base within 300 m, otherwise after its biome.
- **Current tombstones.** Each one in the latest save shows the save it first appeared in.

**Files:**
- Modify: `internal/config/config.go` (`Server.TimeZone`, `Server.Location`, validation), `internal/config/config_test.go`
- Create: `internal/server/playtime.go` (`span`, `overlapSeconds`, `localDays`, `dayTotals`, `totalSeconds`), `internal/server/playtime_test.go`
- Create: `internal/server/profile.go` (`profileJSON`, handler), `internal/server/profile_test.go`
- Modify: `internal/server/server.go` (route), `internal/server/helpers_test.go` (alpha is in Europe/Oslo)

**Interfaces:**
- Consumes: `store.PlayerSessions`, `EarliestEvent`, `Tombstones` (Task 3); `worldevents.NewGeo`, `MatchRadius`, `NearRadius` (Task 4); `extract.Marker.Owner/Namer` (Task 1); `s.worlds.get` (existing).
- Produces:
  - `config.Server.TimeZone string` (JSON `timeZone`), `func (*config.Server) Location() *time.Location`.
  - In package `server`: `func localDays(now time.Time, loc *time.Location, n int) (starts []time.Time, end time.Time)`, `func dayTotals(sessions []store.Session, now time.Time, loc *time.Location, n int) []int64`, `func totalSeconds([]store.Session, time.Time) int64`, `func span(store.Session, time.Time) (time.Time, time.Time)`, `func overlapSeconds(a0, a1, b0, b1 time.Time) int64`; test helpers `mustTime(string) time.Time`, `oslo(*testing.T)`.
  - Profile JSON (TS `Profile`, Task 7): `id, name, platform, timeZone, online, since?, lastSeen?, firstSeen, trackedSince, weekSeconds, allSeconds, sessions, days[{date "2026-10-05", seconds}] (7, oldest first), beds{count, near[]}, bases[{id, name, pieces, biome, x, z}], portals[{id, tag, paired, x, z}], tames[{id, name, species, x, z}], deaths{spotted, week, tombstones[{id, biome, firstSeen, x, z}]}`.

- [ ] **Step 1: Write the failing config test**

In `internal/config/config_test.go`, in the imports, replace:

```go
	"testing"

	"golang.org/x/crypto/bcrypt"
```

with:

```go
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
```

Append to the end of `internal/config/config_test.go`, after a blank line:

```go
// Plan 7: each server's timeZone (its agent's FARSIGHT_LOG_TZ) defaults to
// UTC and must be a loadable IANA name.
func TestTimeZone(t *testing.T) {
	good := `"passphraseHash":"` + hash(t, "pw") + `","agentTokenHash":"` + hash(t, "tok") + `"`
	c, err := Load(write(t, `{"servers":[{"id":"a","name":"x",`+good+`},{"id":"b","name":"y","timeZone":"Europe/Oslo",`+good+`}]}`), env)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := c.Server("a")
	b, _ := c.Server("b")
	if a.TimeZone != "UTC" || a.Location() != time.UTC {
		t.Errorf("a: timeZone %q, location %v; want UTC", a.TimeZone, a.Location())
	}
	if b.Location().String() != "Europe/Oslo" {
		t.Errorf("b: location %v", b.Location())
	}
	if _, err := Load(write(t, `{"servers":[{"id":"a","name":"x","timeZone":"Mars/Olympus",`+good+`}]}`), env); err == nil || !strings.Contains(err.Error(), "timeZone") {
		t.Errorf("bad timeZone: err = %v", err)
	}
	// A Server built without Load (as tests do) still resolves its zone.
	if loc := (&Server{TimeZone: "Europe/Oslo"}).Location(); loc.String() != "Europe/Oslo" {
		t.Errorf("unloaded server location = %v", loc)
	}
	if loc := (&Server{}).Location(); loc != time.UTC {
		t.Errorf("zero server location = %v", loc)
	}
}
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/config/`

Expected: FAIL at build: `a.Location undefined (type *Server has no field or method Location)`.

- [ ] **Step 3: Add `timeZone` to the server config**

In `internal/config/config.go`, in the imports, replace:

```go
	"os"
	"regexp"

	"golang.org/x/crypto/bcrypt"
```

with:

```go
	"os"
	"regexp"
	"time"
	_ "time/tzdata" // timeZone names must load without a system zoneinfo

	"golang.org/x/crypto/bcrypt"
```

In `internal/config/config.go`, at the end of `type Server`, replace:

```go
	AgentTokenHash string `json:"agentTokenHash"` // bcrypt
}
```

with:

```go
	AgentTokenHash string `json:"agentTokenHash"` // bcrypt
	// TimeZone is the IANA zone the game server logs in: the same value as
	// its agent's FARSIGHT_LOG_TZ. Profiles and the activity timeline count
	// days in it. Default "UTC".
	TimeZone string `json:"timeZone,omitempty"`

	loc *time.Location
}

// Location is the server's TimeZone, loaded (UTC when it is unset, or
// can't be loaded in a Server built without Load).
func (s *Server) Location() *time.Location {
	if s.loc != nil {
		return s.loc
	}
	if s.TimeZone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(s.TimeZone)
	if err != nil {
		return time.UTC
	}
	return loc
}
```

In `internal/config/config.go`, in `Load`, in the per-server loop, replace:

```go
		if s.MaxPlayers == 0 {
			s.MaxPlayers = 10
		}
```

with:

```go
		if s.MaxPlayers == 0 {
			s.MaxPlayers = 10
		}
		if s.TimeZone == "" {
			s.TimeZone = "UTC"
		}
		loc, err := time.LoadLocation(s.TimeZone)
		if err != nil {
			return nil, fmt.Errorf("config: server %q: timeZone: %w", s.ID, err)
		}
		s.loc = loc
```

- [ ] **Step 4: Run the config tests to make sure they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/config/`

Expected: `ok`.

- [ ] **Step 5: Write the failing session-maths and profile tests**

Give the test server `alpha` a zone first, so day boundaries are tested away from UTC. In `internal/server/helpers_test.go`:

In `internal/server/helpers_test.go`, in `newEnvBurst`, replace:

```go
				PassphraseHash: mustHash(t, "alpha-pass"), AgentTokenHash: mustHash(t, "alpha-token")},
			{ID: "beta", Name: "Beta", MaxPlayers: 5,
```

with:

```go
				PassphraseHash: mustHash(t, "alpha-pass"), AgentTokenHash: mustHash(t, "alpha-token"), TimeZone: "Europe/Oslo"},
			{ID: "beta", Name: "Beta", MaxPlayers: 5,
```

Create `internal/server/playtime_test.go`:

```go
package server

import (
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/store"
)

func oslo(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func sess(since, until string) store.Session {
	s := store.Session{Since: mustTime(since)}
	if until != "" {
		u := mustTime(until)
		s.Until = &u
	}
	return s
}

func mustTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		panic(err)
	}
	return t
}

func TestDayTotalsSplitAtLocalMidnightAndCountTheOpenSession(t *testing.T) {
	loc := oslo(t)
	now := mustTime("2026-10-05T12:00:00+02:00") // Monday noon in Oslo
	sessions := []store.Session{
		sess("2026-09-28T10:00:00+02:00", "2026-09-28T11:00:00+02:00"), // before the window
		sess("2026-09-29T23:00:00+02:00", "2026-09-30T01:30:00+02:00"), // 1 h on 29 Sep, 1.5 h on 30 Sep
		sess("2026-10-03T22:00:00Z", "2026-10-04T00:00:00Z"),           // 00:00–02:00 Oslo on 4 Oct
		sess("2026-10-05T10:00:00+02:00", ""),                          // open: 2 h so far today
	}
	got := dayTotals(sessions, now, loc, 7)
	want := []int64{3600, 5400, 0, 0, 0, 7200, 7200} // 29 Sep, 30 Sep, 1–3 Oct, 4 Oct, 5 Oct (today)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("day totals = %v, want %v", got, want)
		}
	}
	if total := totalSeconds(sessions, now); total != 3600+9000+7200+7200 {
		t.Fatalf("total = %d", total)
	}
}

func TestLocalDaysAcrossTheDSTChange(t *testing.T) {
	loc := oslo(t)
	// 25 Oct 2026: clocks go back in Oslo, so that day is 25 hours long.
	starts, end := localDays(mustTime("2026-10-26T09:00:00+01:00"), loc, 2)
	if starts[0].Format(time.RFC3339) != "2026-10-25T00:00:00+02:00" || starts[1].Format(time.RFC3339) != "2026-10-26T00:00:00+01:00" ||
		end.Format(time.RFC3339) != "2026-10-27T00:00:00+01:00" {
		t.Fatalf("starts %v end %v", starts, end)
	}
	whole := sess("2026-10-25T00:00:00+02:00", "2026-10-26T00:00:00+01:00")
	if got := dayTotals([]store.Session{whole}, mustTime("2026-10-26T09:00:00+01:00"), loc, 2); got[0] != 25*3600 || got[1] != 0 {
		t.Fatalf("DST day = %v, want 25 h then 0", got)
	}
}

func TestSpanClampsBackwardsSessions(t *testing.T) {
	s := sess("2026-10-05T10:00:00Z", "2026-10-05T09:00:00Z")
	if total := totalSeconds([]store.Session{s}, mustTime("2026-10-05T12:00:00Z")); total != 0 {
		t.Fatalf("backwards session counted %d s", total)
	}
}
```

Create `internal/server/profile_test.go`:

```go
package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/store"
)

// profileWorld seeds alpha with Alice (Steam 111): a session across
// midnight in Oslo two days ago and one open since an hour ago; Bob
// (Xbox 222) once; and a save holding Alice's bed, portal, tame,
// tombstone and base, plus things that are not hers or are unexplored.
func profileWorld(t *testing.T, e *env) {
	t.Helper()
	ctx := t.Context()
	until := mustTime("2026-09-27T23:30:00Z") // 01:30 on 28 Sep in Oslo
	for _, s := range []store.Session{
		{ServerID: "alpha", Name: "Alice", Platform: "Steam", PlatformID: "111", Since: mustTime("2026-09-27T21:00:00Z"), Until: &until, Seconds: 9000, Reason: "left"},
		{ServerID: "alpha", Name: "Bob", Platform: "Xbox", PlatformID: "222", Since: mustTime("2026-09-28T10:00:00Z"), Until: &until, Reason: "left"},
	} {
		if err := e.store.InsertClosedSession(ctx, nil, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.store.OpenSession(ctx, nil, store.Session{ServerID: "alpha", Name: "Alice", Platform: "Steam", PlatformID: "111", Since: at(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	snap := testSnapshot("s1", at(-3*time.Minute))
	snap.Bases[0].Builders = []extract.Builder{{ID: 1, Name: "Alice", Pieces: 60}, {ID: 2, Name: "Bob", Pieces: 40}}
	snap.Markers = append(snap.Markers,
		extract.Marker{ID: "bed-1", Kind: "bed", Owner: "Alice", X: 2, Z: 2},
		extract.Marker{ID: "bed-2", Kind: "bed", Owner: "Alice", X: -5000, Z: -5000}, // unexplored
		extract.Marker{ID: "portal-1", Kind: "portal", Label: "home", Owner: "Alice", X: 10, Z: 10, Pair: "portal-3"},
		extract.Marker{ID: "portal-3", Kind: "portal", Label: "home", X: -5000, Z: -5000, Pair: "portal-1"}, // unexplored partner
		extract.Marker{ID: "portal-2", Kind: "portal", Label: "bobs", Owner: "Bob", X: 11, Z: 11, Pair: "portal-4"},
		extract.Marker{ID: "portal-4", Kind: "portal", Label: "bobs", X: 40, Z: 40, Pair: "portal-2"}, // explored partner
		extract.Marker{ID: "tame-1", Kind: "tame", Species: "Lox", Label: "Big Mama", Namer: "Steam_111", X: 20, Z: 20},
		extract.Marker{ID: "tame-2", Kind: "tame", Species: "Wolf", Label: "Grey", Namer: "Xbox_222", X: 21, Z: 21},
		extract.Marker{ID: "tombstone-1", Kind: "tombstone", Owner: "Alice", X: 30, Z: 30},
	)
	if err := e.post("alpha", "alpha-token", "snapshot", snap); err != nil {
		t.Fatal(err)
	}
}

func TestProfile(t *testing.T) {
	e := newEnv(t)
	profileWorld(t, e)
	cookie := e.mustUnlock("alpha")

	r := e.get("/api/servers/alpha/players/111", cookie)
	if r.code != http.StatusOK {
		t.Fatalf("profile = %d %s", r.code, r.body)
	}
	var p profileJSON
	r.json(t, &p)
	if p.ID != "111" || p.Name != "Alice" || p.Platform != "Steam" || p.TimeZone != "Europe/Oslo" {
		t.Fatalf("identity = %+v", p)
	}
	if !p.Online || p.Since != rfc3339(at(-time.Hour)) || p.LastSeen != "" {
		t.Fatalf("status: online=%v since=%q lastSeen=%q", p.Online, p.Since, p.LastSeen)
	}
	if p.FirstSeen != "2026-09-27T21:00:00Z" || p.TrackedSince != p.FirstSeen || p.Sessions != 2 {
		t.Fatalf("first seen %q tracked since %q sessions %d", p.FirstSeen, p.TrackedSince, p.Sessions)
	}
	// Now is 14:00 on 29 Sep in Oslo: 23 … 29 Sep. The closed session is
	// 1 h on 27 Sep and 1.5 h on 28 Sep; the open one 1 h today.
	want := []int64{0, 0, 0, 0, 3600, 5400, 3600}
	if len(p.Days) != 7 || p.Days[0].Date != "2026-09-23" || p.Days[6].Date != "2026-09-29" {
		t.Fatalf("days = %+v", p.Days)
	}
	for i, d := range p.Days {
		if d.Seconds != want[i] {
			t.Fatalf("days = %+v, want seconds %v", p.Days, want)
		}
	}
	if p.WeekSeconds != 12600 || p.AllSeconds != 12600 {
		t.Fatalf("week %d all %d, want 12600 each", p.WeekSeconds, p.AllSeconds)
	}
	if p.Beds.Count != 1 || len(p.Beds.Near) != 1 || p.Beds.Near[0] != "Home" {
		t.Fatalf("beds = %+v", p.Beds)
	}
	if len(p.Bases) != 1 || p.Bases[0].Name != "Home" || p.Bases[0].Pieces != 100 || p.Bases[0].Biome == "" {
		t.Fatalf("bases = %+v", p.Bases)
	}
	if len(p.Portals) != 1 || p.Portals[0].Tag != "home" || p.Portals[0].Paired {
		t.Fatalf("portals = %+v, want home, unpaired (its partner is unexplored)", p.Portals)
	}
	if len(p.Tames) != 1 || p.Tames[0].Name != "Big Mama" || p.Tames[0].Species != "Lox" {
		t.Fatalf("tames = %+v", p.Tames)
	}
	if p.Deaths.Spotted != 1 || p.Deaths.Week != 1 || len(p.Deaths.Tombstones) != 1 ||
		p.Deaths.Tombstones[0].FirstSeen != rfc3339(at(-3*time.Minute)) {
		t.Fatalf("deaths = %+v", p.Deaths)
	}

	var bob profileJSON
	e.get("/api/servers/alpha/players/222", cookie).json(t, &bob)
	if bob.Online || bob.LastSeen != "2026-09-27T23:30:00Z" || len(bob.Tames) != 1 || len(bob.Portals) != 1 || !bob.Portals[0].Paired ||
		len(bob.Bases) != 1 || bob.Deaths.Spotted != 0 {
		t.Fatalf("bob = %+v", bob)
	}
}

func TestProfileNotFound(t *testing.T) {
	e := newEnv(t)
	profileWorld(t, e)
	cookie := e.mustUnlock("alpha")
	for _, c := range []struct{ path, cookie string }{
		{"/api/servers/alpha/players/999", cookie}, // unknown player
		{"/api/servers/alpha/players/111", ""},     // locked
		{"/api/servers/beta/players/111", cookie},  // beta not unlocked
		{"/api/servers/nope/players/111", cookie},  // unknown server
	} {
		if r := e.get(c.path, c.cookie); r.code != http.StatusNotFound || !contains(string(r.body), "not found") {
			t.Errorf("%s (cookie %v) = %d %s, want 404", c.path, c.cookie != "", r.code, r.body)
		}
	}
}

// Without a save yet, the profile still has playtime and empty save data.
func TestProfileWithoutASave(t *testing.T) {
	e := newEnv(t)
	if err := e.store.OpenSession(t.Context(), nil, store.Session{ServerID: "alpha", Name: "Alice", Platform: "Steam", PlatformID: "111", Since: at(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var p profileJSON
	r := e.get("/api/servers/alpha/players/111", e.mustUnlock("alpha"))
	r.json(t, &p)
	if r.code != http.StatusOK || p.WeekSeconds != 3600 || p.Bases == nil || p.Portals == nil || p.Tames == nil || p.Beds.Near == nil || p.Deaths.Tombstones == nil {
		t.Fatalf("profile = %d %+v", r.code, p)
	}
}
```

- [ ] **Step 6: Run them to make sure they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/server/`

Expected: FAIL at build: `undefined: dayTotals`, `undefined: profileJSON`…

- [ ] **Step 7: Add the session maths**

Create `internal/server/playtime.go`:

```go
package server

import (
	"time"

	"github.com/jumpingmushroom/farsight/internal/store"
)

// span is a session's [since, end): an open session runs until now. A
// session that (through a clock step) ends before it starts is empty.
func span(s store.Session, now time.Time) (time.Time, time.Time) {
	end := now
	if s.Until != nil {
		end = *s.Until
	}
	if end.Before(s.Since) {
		end = s.Since
	}
	return s.Since, end
}

// overlapSeconds is how many whole seconds [a0, a1) and [b0, b1) share.
func overlapSeconds(a0, a1, b0, b1 time.Time) int64 {
	lo, hi := a0, a1
	if b0.After(lo) {
		lo = b0
	}
	if b1.Before(hi) {
		hi = b1
	}
	if !hi.After(lo) {
		return 0
	}
	return int64(hi.Sub(lo) / time.Second)
}

// localDays returns the starts of the n local days (in loc) that end with
// the day containing now, oldest first, and the end of that day. Days are
// calendar days: one with a DST change is 23 or 25 hours long.
func localDays(now time.Time, loc *time.Location, n int) (starts []time.Time, end time.Time) {
	t := now.In(loc)
	today := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	for i := n - 1; i >= 0; i-- {
		starts = append(starts, today.AddDate(0, 0, -i))
	}
	return starts, today.AddDate(0, 0, 1)
}

// dayTotals is the time sessions spent online in each of the n local days
// ending today (oldest first): a session across midnight is split between
// the two days, and an open one counts up to now.
func dayTotals(sessions []store.Session, now time.Time, loc *time.Location, n int) []int64 {
	starts, end := localDays(now, loc, n)
	out := make([]int64, n)
	for _, s := range sessions {
		a, b := span(s, now)
		for i, d0 := range starts {
			d1 := end
			if i+1 < n {
				d1 = starts[i+1]
			}
			out[i] += overlapSeconds(a, b, d0, d1)
		}
	}
	return out
}

// totalSeconds is the time sessions spent online, an open one up to now.
func totalSeconds(sessions []store.Session, now time.Time) int64 {
	var n int64
	for _, s := range sessions {
		a, b := span(s, now)
		n += int64(b.Sub(a) / time.Second)
	}
	return n
}
```

- [ ] **Step 8: Add the profile handler and route**

Create `internal/server/profile.go`:

```go
package server

import (
	"context"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/jumpingmushroom/farsight/internal/config"
	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/store"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
)

// profileDays is how many local days the profile's chart covers; "this
// week" and deaths "this week" use the same days.
const profileDays = 7

type profileDayJSON struct {
	Date    string `json:"date"` // local date, "2026-10-05"
	Seconds int64  `json:"seconds"`
}

type profileBedsJSON struct {
	Count int      `json:"count"`
	Near  []string `json:"near"` // nearest base name (or the biome) per bed, deduplicated
}

type profileBaseJSON struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Pieces int     `json:"pieces"`
	Biome  string  `json:"biome"`
	X      float32 `json:"x"`
	Z      float32 `json:"z"`
}

type profilePortalJSON struct {
	ID     string  `json:"id"`
	Tag    string  `json:"tag"`
	Paired bool    `json:"paired"`
	X      float32 `json:"x"`
	Z      float32 `json:"z"`
}

type profileTameJSON struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Species string  `json:"species"`
	X       float32 `json:"x"`
	Z       float32 `json:"z"`
}

type profileTombJSON struct {
	ID        string  `json:"id"`
	Biome     string  `json:"biome"`
	FirstSeen string  `json:"firstSeen"` // the save time it first appeared in
	X         float32 `json:"x"`
	Z         float32 `json:"z"`
}

type profileDeathsJSON struct {
	Spotted    int               `json:"spotted"` // distinct tombstones across every save seen
	Week       int               `json:"week"`    // of those, first seen in the profile's 7 days
	Tombstones []profileTombJSON `json:"tombstones"`
}

// profileJSON is GET /api/servers/{id}/players/{player}. Save data (beds,
// bases, portals, tames, tombstones) comes from the latest save, filtered
// to explored ground like the snapshot API.
type profileJSON struct {
	ID           string              `json:"id"` // the platform ID
	Name         string              `json:"name"`
	Platform     string              `json:"platform"`
	TimeZone     string              `json:"timeZone"`
	Online       bool                `json:"online"`
	Since        string              `json:"since,omitempty"`    // the open session's start
	LastSeen     string              `json:"lastSeen,omitempty"` // the latest session's end, when offline
	FirstSeen    string              `json:"firstSeen"`
	TrackedSince string              `json:"trackedSince"`
	WeekSeconds  int64               `json:"weekSeconds"`
	AllSeconds   int64               `json:"allSeconds"`
	Sessions     int                 `json:"sessions"`
	Days         []profileDayJSON    `json:"days"`
	Beds         profileBedsJSON     `json:"beds"`
	Bases        []profileBaseJSON   `json:"bases"`
	Portals      []profilePortalJSON `json:"portals"`
	Tames        []profileTameJSON   `json:"tames"`
	Deaths       profileDeathsJSON   `json:"deaths"`
}

func (s *server) profile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, ok := s.unlocked(r, id)
	if !ok {
		notFound(w)
		return
	}
	p, ok, err := s.buildProfile(r.Context(), srv, r.PathValue("player"))
	if err != nil {
		s.internalError(w, "profile", id, err)
		return
	}
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// buildProfile assembles playerID's profile; ok is false if the server has
// never seen that platform ID.
func (s *server) buildProfile(ctx context.Context, srv *config.Server, playerID string) (*profileJSON, bool, error) {
	sessions, err := s.Store.PlayerSessions(ctx, srv.ID, playerID)
	if err != nil || len(sessions) == 0 {
		return nil, false, err
	}
	now, loc := s.Now(), srv.Location()
	last := sessions[len(sessions)-1]
	p := &profileJSON{
		ID: playerID, Name: last.Name, Platform: last.Platform, TimeZone: loc.String(),
		FirstSeen: rfc3339(sessions[0].Since), Sessions: len(sessions),
		AllSeconds: totalSeconds(sessions, now),
		Beds:       profileBedsJSON{Near: []string{}},
		Bases:      []profileBaseJSON{}, Portals: []profilePortalJSON{}, Tames: []profileTameJSON{},
		Deaths: profileDeathsJSON{Tombstones: []profileTombJSON{}},
	}
	names := map[string]bool{} // every name the platform ID has played under
	var since, lastSeen time.Time
	for _, ss := range sessions {
		names[ss.Name] = true
		switch {
		case ss.Until == nil:
			p.Online = true
			if ss.Since.After(since) {
				since = ss.Since
			}
		case ss.Until.After(lastSeen):
			lastSeen = *ss.Until
		}
	}
	if p.Online {
		p.Since = rfc3339(since)
	} else {
		p.LastSeen = rfc3339(lastSeen)
	}

	starts, _ := localDays(now, loc, profileDays)
	for i, sec := range dayTotals(sessions, now, loc, profileDays) {
		p.Days = append(p.Days, profileDayJSON{Date: starts[i].Format(time.DateOnly), Seconds: sec})
		p.WeekSeconds += sec
	}
	p.TrackedSince = p.FirstSeen
	if first, ok, err := s.Store.EarliestEvent(ctx, srv.ID); err != nil {
		return nil, false, err
	} else if ok {
		p.TrackedSince = rfc3339(first)
	}

	// Every distinct tombstone of theirs ever seen in a save.
	var tombs []store.Tombstone
	for n := range names {
		ts, err := s.Store.Tombstones(ctx, srv.ID, n)
		if err != nil {
			return nil, false, err
		}
		tombs = append(tombs, ts...)
	}
	p.Deaths.Spotted = len(tombs)
	for _, t := range tombs {
		if !t.FirstSeen.Before(starts[0]) {
			p.Deaths.Week++
		}
	}

	ws, ok, err := s.worlds.get(ctx, srv.ID)
	if err != nil {
		return nil, false, err
	}
	if ok {
		s.addSaveData(p, ws, names, last.Platform+"_"+playerID, tombs)
	}
	return p, true, nil
}

// addSaveData fills in the profile's beds, bases, portals, tames and
// current tombstones from the latest save. Beds and tombstones link by
// owner name, bases by builder name, portals by creator (resolved to a
// name by the agent) and tames by namer, which is the platform user ID
// itself.
func (s *server) addSaveData(p *profileJSON, ws *worldState, names map[string]bool, namer string, tombs []store.Tombstone) {
	snap := ws.snap
	geo := worldevents.NewGeo(snap, ws.mask)
	in := func(x, z float32) bool { return ws.mask.At(float64(x), float64(z)) }
	byID := make(map[string]extract.Marker, len(snap.Markers))
	for _, m := range snap.Markers {
		byID[m.ID] = m
	}
	var bases []extract.Base
	for _, b := range snap.Bases {
		if !in(b.X, b.Z) {
			continue
		}
		bases = append(bases, b)
		for _, bl := range b.Builders {
			if names[bl.Name] {
				p.Bases = append(p.Bases, profileBaseJSON{ID: b.ID, Name: b.Name, Pieces: b.Pieces, Biome: geo.Biome(b.X, b.Z), X: b.X, Z: b.Z})
				break
			}
		}
	}
	for _, m := range snap.Markers {
		if !in(m.X, m.Z) {
			continue
		}
		switch {
		case m.Kind == "bed" && names[m.Owner]:
			p.Beds.Count++
			near := geo.Biome(m.X, m.Z)
			if b, ok := nearestBase(bases, m.X, m.Z); ok {
				near = b.Name
			}
			if !slices.Contains(p.Beds.Near, near) {
				p.Beds.Near = append(p.Beds.Near, near)
			}
		case m.Kind == "portal" && names[m.Owner]:
			// Paired only when the partner is explored too, as the snapshot
			// API unpairs: an unexplored partner stays secret.
			partner, ok := byID[m.Pair]
			paired := ok && in(partner.X, partner.Z)
			p.Portals = append(p.Portals, profilePortalJSON{ID: m.ID, Tag: m.Label, Paired: paired, X: m.X, Z: m.Z})
		case m.Kind == "tame" && m.Namer != "" && m.Namer == namer:
			p.Tames = append(p.Tames, profileTameJSON{ID: m.ID, Name: m.Label, Species: m.Species, X: m.X, Z: m.Z})
		case m.Kind == "tombstone" && names[m.Owner]:
			first := snap.SavedAt
			for _, t := range tombs {
				if t.Owner == m.Owner && math.Hypot(float64(t.X-m.X), float64(t.Z-m.Z)) <= worldevents.MatchRadius && t.FirstSeen.Before(first) {
					first = t.FirstSeen
				}
			}
			p.Deaths.Tombstones = append(p.Deaths.Tombstones, profileTombJSON{ID: m.ID, Biome: geo.Biome(m.X, m.Z), FirstSeen: rfc3339(first), X: m.X, Z: m.Z})
		}
	}
}

// nearestBase is the base whose centre is nearest (x, z), within
// worldevents.NearRadius.
func nearestBase(bases []extract.Base, x, z float32) (extract.Base, bool) {
	var best extract.Base
	bestD, found := 0.0, false
	for _, b := range bases {
		d := math.Hypot(float64(b.X-x), float64(b.Z-z))
		if d <= worldevents.NearRadius && (!found || d < bestD) {
			best, bestD, found = b, d, true
		}
	}
	return best, found
}
```

In `internal/server/server.go`, in `NewHandlers`, replace:

```go
	mux.Handle("GET /api/servers/{id}/snapshot", gzipJSON(s.Log, http.HandlerFunc(s.snapshot)))
```

with:

```go
	mux.Handle("GET /api/servers/{id}/snapshot", gzipJSON(s.Log, http.HandlerFunc(s.snapshot)))
	mux.Handle("GET /api/servers/{id}/players/{player}", gzipJSON(s.Log, http.HandlerFunc(s.profile)))
```

- [ ] **Step 9: Run the tests to make sure they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH gofmt -l ./internal && PATH=$HOME/.local/go/bin:$PATH go test ./internal/config/ ./internal/server/`

Expected: `gofmt -l` prints nothing; `ok` for both packages.

- [ ] **Step 10: Commit**

```bash
cd /workspace/Farsight
git add internal/config/config.go \
  internal/config/config_test.go \
  internal/server/playtime.go \
  internal/server/playtime_test.go \
  internal/server/profile.go \
  internal/server/profile_test.go \
  internal/server/server.go \
  internal/server/helpers_test.go
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(server): per-server time zone and the player profile endpoint

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
git status --short   # must print nothing
```

---

### Task 6: Central app: the activity timeline and today's sessions; the card's activity in the same shape

Two endpoints, behind the same unlock check (404 otherwise):

- `GET /api/servers/{id}/activity?before=<rfc3339>&days=3`: the events in `[from, until)`, newest first.
  - Without `before`, `until` is the end of today in the server's zone; `from` is the local midnight `days − 1` days before the day that ends at `until`.
  - The response's `from` is the next page's `before`, so "Show earlier" walks back three local days at a time.
  - `days` is 1–14 (400 otherwise, and for a malformed `before`).
  - A run of autosaves with nothing in between collapses to the newest, before counting.
  - Also returned: `counts` per category (all eight, zero included), `people` (every player seen: online first, then most recently seen) and `earliest` (when tracking began; absent before any event).
- `GET /api/servers/{id}/sessions/today`: who played today in the server's zone.
  - One entry per player (platform ID, or the name for a session without one), each session clipped to `[dayStart, now]`.
  - An open session has no `until`.

Each entry, in the timeline and now in the card's `activity` alike, is one `eventJSON`:
- **Fields:** the old card fields (`type, at, name, platform, code, players, seconds, version`) keep their names.
- **New:** `id`, `category`, `source` (`log` or `save`), `who` (the platform IDs the event concerns, for the people filter), `raid`, and the world fields.
- **Who:** world events resolve `owner` and `builders` by every name a player has played under, and `namer` by `platform_platformId`.
- **Position:** `x`/`z` are sent only when the place is explored in the current save, so the browser never learns where unexplored things are.
- **`world_saved`** is a server-log event despite its name: the decoder keys on the explicit set of world types, not on the `world_` prefix.

Category of each type: `player_join`/`player_leave` → `session`; `world_tombstone` → `death`; `world_boss` → `boss`; `world_base_new`/`world_base_grew` → `build`; `world_portal`/`world_portal_paired` → `portal`; `world_tame` → `tame`; `event_raid` → `event`; `server_starting`, `server_boot`, `server_ready`, `server_stopped`, `world_saved`, `join_code` → `server`. Other types aren't shown.

**Files:**
- Create: `internal/server/activity.go` (`categories`, `categoryOf`, `worldTypes`, `eventJSON`, `people`, `toEventJSON`, `activityJSONOut`, `activityWindow`, handlers, `todayJSON`, `todayPlayers`)
- Modify: `internal/server/api.go` (card `timeZone`; `activity` is `[]eventJSON`; `activityJSON` removed)
- Modify: `internal/server/server.go` (routes)
- Test: `internal/server/activity_test.go`

**Interfaces:**
- Consumes: `store.RecentEvents`, `EventsBetween`, `EarliestEvent`, `Players` (with `Names`), `SessionsOverlapping` (Task 3); `worldevents.Event` and its `Type*` constants (Task 4); `localDays`, `mustTime` (Task 5); `config.Server.Location` (Task 5); `logwatch.EvRaid` (Task 2).
- Produces (TS types in Tasks 7–8 mirror these exactly):
  - Card JSON gains `timeZone` (IANA); `activity[]` entries are `eventJSON`.
  - `eventJSON`: `id, type, category ("session"|"death"|"boss"|"build"|"portal"|"tame"|"event"|"server"), source ("log"|"save"), at, who[]`, optional `name, platform, platformId, code, players, seconds, version, raid, owner, tag, paired, species, pieces, grew, boss, biome, near, x, z`.
  - Activity JSON: `{timeZone, from, until, earliest?, events[], counts{category: n}, people[{id, name, online}]}`.
  - Today JSON: `{timeZone, dayStart, dayEnd, now, players[{id, name, online, spans[{since, until?}]}]}`.

- [ ] **Step 1: Write the failing tests**

The seed: alpha in Europe/Oslo, where now (12:00 UTC on 29 Sep) is 14:00. It gets `testEvents`, an autosave just after the last one, a raid, Ulf's join just after local midnight on 27 Sep and a leave just before it, and two saves, the second adding Alice's tombstone (explored) and portal (unexplored).

Create `internal/server/activity_test.go`:

```go
package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
	"github.com/jumpingmushroom/farsight/internal/store"
)

// activityWorld seeds alpha (Europe/Oslo; now is 14:00 on 29 Sep there):
// testEvents (today), an extra autosave right after the last one, a raid,
// Ulf's join just after midnight on 27 Sep and one just before it (both
// local), and two saves, the second with a new tombstone (explored) and a
// new portal (unexplored), both Alice's.
func activityWorld(t *testing.T, e *env) {
	t.Helper()
	evs := append(testEvents(),
		logwatch.Event{ID: "x1", Type: logwatch.EvWorldSaved, At: at(-3*time.Minute - 30*time.Second)},
		logwatch.Event{ID: "x2", Type: logwatch.EvRaid, At: at(-25 * time.Minute), Raid: "army_theelder"},
		logwatch.Event{ID: "x3", Type: logwatch.EvPlayerJoin, At: mustTime("2026-09-26T22:30:00Z"), Name: "Ulf", Platform: "Steam", PlatformID: "444"},
		logwatch.Event{ID: "x4", Type: logwatch.EvPlayerLeave, At: mustTime("2026-09-26T21:30:00Z"), Name: "Ulf", Platform: "Steam", PlatformID: "444"},
	)
	if err := e.post("alpha", "alpha-token", "events", map[string]any{"events": evs}); err != nil {
		t.Fatal(err)
	}
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-50*time.Minute))); err != nil {
		t.Fatal(err)
	}
	s2 := testSnapshot("s2", at(-3*time.Minute))
	s2.Markers = append(s2.Markers,
		extract.Marker{ID: "tombstone-1", Kind: "tombstone", Owner: "Alice", X: 5, Z: 5},
		extract.Marker{ID: "portal-1", Kind: "portal", Label: "far", Owner: "Alice", X: -5000, Z: -5000},
	)
	if err := e.post("alpha", "alpha-token", "snapshot", s2); err != nil {
		t.Fatal(err)
	}
}

func TestActivity(t *testing.T) {
	e := newEnv(t)
	activityWorld(t, e)
	cookie := e.mustUnlock("alpha")

	r := e.get("/api/servers/alpha/activity", cookie)
	if r.code != http.StatusOK {
		t.Fatalf("activity = %d %s", r.code, r.body)
	}
	var a activityJSONOut
	r.json(t, &a)
	// Three local days: 27 Sep 00:00 to 30 Sep 00:00 in Oslo (CEST).
	if a.TimeZone != "Europe/Oslo" || a.From != "2026-09-26T22:00:00Z" || a.Until != "2026-09-29T22:00:00Z" || a.Earliest != "2026-09-26T21:30:00Z" {
		t.Fatalf("window: tz %q from %q until %q earliest %q", a.TimeZone, a.From, a.Until, a.Earliest)
	}
	var types []string
	for _, ev := range a.Events {
		types = append(types, ev.Type)
	}
	// Newest first. The save at -3 min gave two events (same time, so in
	// id order); the -4 min autosave collapses into the -3.5 min one.
	want := []string{
		logwatch.EvWorldSaved,  // -3.5 min
		logwatch.EvPlayerLeave, // -5: Bob
		logwatch.EvPlayerJoin,  // -10: Bob
		logwatch.EvWorldSaved,  // -14
		logwatch.EvPlayerJoin,  // -20: Alice
		logwatch.EvRaid,        // -25
		logwatch.EvWorldSaved,  // -26
		logwatch.EvJoinCode,    // -28
		logwatch.EvServerReady, // -29
		logwatch.EvServerBoot,  // -30
		logwatch.EvPlayerJoin,  // 00:30 on 27 Sep: Ulf
	}
	if len(types) != 2+len(want) || !(types[0] == "world_portal" && types[1] == "world_tombstone" || types[0] == "world_tombstone" && types[1] == "world_portal") {
		t.Fatalf("events = %v", types)
	}
	for i, w := range want {
		if types[2+i] != w {
			t.Fatalf("events = %v, want the two world events then %v", types, want)
		}
	}
	wantCounts := map[string]int{"session": 4, "death": 1, "boss": 0, "build": 0, "portal": 1, "tame": 0, "event": 1, "server": 6}
	for k, v := range wantCounts {
		if a.Counts[k] != v {
			t.Errorf("counts = %v, want %v", a.Counts, wantCounts)
			break
		}
	}
	byType := map[string]eventJSON{}
	for _, ev := range a.Events {
		byType[ev.Type] = ev
	}
	tomb := byType["world_tombstone"]
	if tomb.Category != "death" || tomb.Source != "save" || tomb.Owner != "Alice" || len(tomb.Who) != 1 || tomb.Who[0] != "111" ||
		tomb.X == nil || *tomb.X != 5 || tomb.Biome == "" || tomb.At != rfc3339(at(-3*time.Minute)) {
		t.Errorf("tombstone = %+v", tomb)
	}
	if portal := byType["world_portal"]; portal.X != nil || portal.Z != nil || portal.Tag != "far" || len(portal.Who) != 1 {
		t.Errorf("portal in unexplored ground = %+v, want no x/z", portal)
	}
	if raid := byType[logwatch.EvRaid]; raid.Category != "event" || raid.Source != "log" || raid.Raid != "army_theelder" || len(raid.Who) != 0 {
		t.Errorf("raid = %+v", raid)
	}
	if save := byType[logwatch.EvWorldSaved]; save.Category != "server" || save.Source != "log" {
		t.Errorf("autosave = %+v, want a server-log entry", save)
	}
	if len(a.People) != 3 || a.People[0].Name != "Alice" || !a.People[0].Online || a.People[0].ID != "111" {
		t.Errorf("people = %+v", a.People)
	}

	// Show earlier: the three days before.
	var earlier activityJSONOut
	e.get("/api/servers/alpha/activity?before="+a.From, cookie).json(t, &earlier)
	if earlier.From != "2026-09-23T22:00:00Z" || earlier.Until != a.From || len(earlier.Events) != 1 || earlier.Events[0].Name != "Ulf" {
		t.Errorf("earlier = %+v", earlier)
	}
	var today activityJSONOut
	e.get("/api/servers/alpha/activity?days=1", cookie).json(t, &today)
	if today.From != "2026-09-28T22:00:00Z" || len(today.Events) != 12 {
		t.Errorf("today: from %q, %d events", today.From, len(today.Events))
	}
}

func TestActivityRejectsBadParamsAndLockedServers(t *testing.T) {
	e := newEnv(t)
	cookie := e.mustUnlock("alpha")
	for _, q := range []string{"before=yesterday", "days=0", "days=15", "days=x"} {
		if r := e.get("/api/servers/alpha/activity?"+q, cookie); r.code != http.StatusBadRequest {
			t.Errorf("%s = %d %s, want 400", q, r.code, r.body)
		}
	}
	for _, path := range []string{"/api/servers/beta/activity", "/api/servers/beta/sessions/today", "/api/servers/nope/activity"} {
		if r := e.get(path, cookie); r.code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, r.code)
		}
	}
	if r := e.get("/api/servers/alpha/activity", ""); r.code != http.StatusNotFound {
		t.Errorf("no cookie = %d, want 404", r.code)
	}
	// No events at all: an empty, well-formed window.
	var a activityJSONOut
	r := e.get("/api/servers/alpha/activity", cookie)
	r.json(t, &a)
	if r.code != http.StatusOK || a.Events == nil || a.People == nil || a.Earliest != "" || len(a.Counts) != 8 {
		t.Errorf("empty activity = %d %s", r.code, r.body)
	}
}

func TestSessionsToday(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	crossing := mustTime("2026-09-28T23:00:00Z") // 01:00 on 29 Sep in Oslo
	yesterday := mustTime("2026-09-28T11:00:00Z")
	for _, s := range []store.Session{
		{ServerID: "alpha", Name: "Alice", Platform: "Steam", PlatformID: "111", Since: mustTime("2026-09-28T21:00:00Z"), Until: &crossing},
		{ServerID: "alpha", Name: "Bob", Platform: "Xbox", PlatformID: "222", Since: mustTime("2026-09-28T10:00:00Z"), Until: &yesterday},
	} {
		if err := e.store.InsertClosedSession(ctx, nil, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.store.OpenSession(ctx, nil, store.Session{ServerID: "alpha", Name: "Alice", Platform: "Steam", PlatformID: "111", Since: at(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var d todayJSON
	r := e.get("/api/servers/alpha/sessions/today", e.mustUnlock("alpha"))
	r.json(t, &d)
	if r.code != http.StatusOK || d.TimeZone != "Europe/Oslo" || d.DayStart != "2026-09-28T22:00:00Z" || d.DayEnd != "2026-09-29T22:00:00Z" || d.Now != rfc3339(t0) {
		t.Fatalf("today = %d %s", r.code, r.body)
	}
	if len(d.Players) != 1 {
		t.Fatalf("players = %+v, want only Alice", d.Players)
	}
	p := d.Players[0]
	if p.ID != "111" || p.Name != "Alice" || !p.Online || len(p.Spans) != 2 ||
		p.Spans[0].Since != d.DayStart || p.Spans[0].Until != "2026-09-28T23:00:00Z" ||
		p.Spans[1].Since != rfc3339(at(-time.Hour)) || p.Spans[1].Until != "" {
		t.Fatalf("Alice = %+v", p)
	}
}

// The card's activity carries world-save events with the timeline's fields.
func TestCardActivityHasWorldEvents(t *testing.T) {
	e := newEnv(t)
	activityWorld(t, e)
	var c cardJSONOut
	r := e.get("/api/servers/alpha", e.mustUnlock("alpha"))
	r.json(t, &c)
	if c.TimeZone != "Europe/Oslo" || len(c.Activity) == 0 {
		t.Fatalf("card = %s", r.body)
	}
	top := c.Activity[0]
	if top.Source != "save" || (top.Category != "death" && top.Category != "portal") || len(top.Who) != 1 {
		t.Fatalf("activity[0] = %+v", top)
	}
}
```

- [ ] **Step 2: Run them to make sure they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/server/`

Expected: FAIL at build: `undefined: activityJSONOut`, `undefined: todayJSON`, `undefined: eventJSON`.

- [ ] **Step 3: Add the activity and today handlers**

Create `internal/server/activity.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/jumpingmushroom/farsight/internal/config"
	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
	"github.com/jumpingmushroom/farsight/internal/store"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
)

const (
	// activityDays is the timeline's default (and "Show earlier") window.
	activityDays = 3
	// maxActivityDays caps ?days=.
	maxActivityDays = 14
)

// Categories, the timeline's chips, in the design's order.
var categories = []string{"session", "death", "boss", "build", "portal", "tame", "event", "server"}

var categoryOf = map[string]string{
	logwatch.EvPlayerJoin:        "session",
	logwatch.EvPlayerLeave:       "session",
	logwatch.EvServerStarting:    "server",
	logwatch.EvServerBoot:        "server",
	logwatch.EvServerReady:       "server",
	logwatch.EvServerStopped:     "server",
	logwatch.EvWorldSaved:        "server",
	logwatch.EvJoinCode:          "server",
	logwatch.EvRaid:              "event",
	worldevents.TypeTombstone:    "death",
	worldevents.TypePortal:       "portal",
	worldevents.TypePortalPaired: "portal",
	worldevents.TypeTame:         "tame",
	worldevents.TypeBaseNew:      "build",
	worldevents.TypeBaseGrew:     "build",
	worldevents.TypeBoss:         "boss",
}

// worldTypes are the world-save event types. world_saved, despite its
// name, is the server log's autosave line.
var worldTypes = map[string]bool{
	worldevents.TypeTombstone:    true,
	worldevents.TypePortal:       true,
	worldevents.TypePortalPaired: true,
	worldevents.TypeTame:         true,
	worldevents.TypeBaseNew:      true,
	worldevents.TypeBaseGrew:     true,
	worldevents.TypeBoss:         true,
}

// eventJSON is one activity entry, in the card's activity list and the
// timeline alike. Log events carry their exact time, world-save events the
// save's. Who lists the platform IDs of the players it concerns (the
// people filter); x and z are set only for a place in explored ground.
type eventJSON struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Category string   `json:"category"`
	Source   string   `json:"source"` // "log" or "save"
	At       string   `json:"at"`
	Who      []string `json:"who"`

	Name       string `json:"name,omitempty"` // the player (log), the tame or base (save)
	Platform   string `json:"platform,omitempty"`
	PlatformID string `json:"platformId,omitempty"`
	Code       string `json:"code,omitempty"`
	Players    *int   `json:"players,omitempty"`
	Seconds    int64  `json:"seconds,omitempty"`
	Version    string `json:"version,omitempty"`
	Raid       string `json:"raid,omitempty"`

	Owner   string   `json:"owner,omitempty"`
	Tag     string   `json:"tag,omitempty"`
	Paired  bool     `json:"paired,omitempty"`
	Species string   `json:"species,omitempty"`
	Pieces  int      `json:"pieces,omitempty"`
	Grew    int      `json:"grew,omitempty"`
	Boss    string   `json:"boss,omitempty"`
	Biome   string   `json:"biome,omitempty"`
	Near    string   `json:"near,omitempty"`
	X       *float32 `json:"x,omitempty"`
	Z       *float32 `json:"z,omitempty"`
}

// people resolves the players an event concerns to platform IDs: by name
// (every name a platform ID has played under) and by platform user ID
// ("Steam_…", a tame's namer).
type people struct {
	byName map[string][]string
	byUser map[string]string
	list   []store.Player
}

func (s *server) people(ctx context.Context, serverID string) (*people, error) {
	ps, err := s.Store.Players(ctx, serverID)
	if err != nil {
		return nil, err
	}
	p := &people{byName: map[string][]string{}, byUser: map[string]string{}, list: ps}
	for _, pl := range ps {
		for _, n := range pl.Names {
			p.byName[n] = append(p.byName[n], pl.PlatformID)
		}
		p.byUser[pl.Platform+"_"+pl.PlatformID] = pl.PlatformID
	}
	return p, nil
}

func (p *people) names(ns ...string) []string {
	out := []string{}
	for _, n := range ns {
		for _, id := range p.byName[n] {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	return out
}

// toEventJSON decodes one stored event; ok is false for a type the
// activity views don't show. mask (nil without a save) gates x and z.
func toEventJSON(se store.StoredEvent, who *people, mask *explored.Mask) (eventJSON, bool) {
	cat, ok := categoryOf[se.Type]
	if !ok {
		return eventJSON{}, false
	}
	out := eventJSON{ID: se.ID, Type: se.Type, Category: cat, Source: "log", At: rfc3339(se.At), Who: []string{}}
	if worldTypes[se.Type] {
		var e worldevents.Event
		if json.Unmarshal(se.Body, &e) != nil {
			return eventJSON{}, false
		}
		out.Source = "save"
		out.Owner, out.Tag, out.Paired, out.Name, out.Species = e.Owner, e.Tag, e.Paired, e.Name, e.Species
		out.Pieces, out.Grew, out.Boss, out.Biome, out.Near = e.Pieces, e.Grew, e.Boss, e.Biome, e.Near
		if e.Pos != nil && mask != nil && mask.At(float64(e.Pos.X), float64(e.Pos.Z)) {
			x, z := e.Pos.X, e.Pos.Z
			out.X, out.Z = &x, &z
		}
		switch {
		case e.Namer != "":
			if id, ok := who.byUser[e.Namer]; ok {
				out.Who = []string{id}
			}
		case len(e.Builders) > 0:
			out.Who = who.names(e.Builders...)
		case e.Owner != "":
			out.Who = who.names(e.Owner)
		}
		return out, true
	}
	var e logwatch.Event
	if json.Unmarshal(se.Body, &e) != nil {
		return eventJSON{}, false
	}
	out.Name, out.Platform, out.PlatformID, out.Code = e.Name, e.Platform, e.PlatformID, e.Code
	out.Players, out.Seconds, out.Version, out.Raid = e.Players, e.Seconds, e.Version, e.Raid
	if e.PlatformID != "" {
		out.Who = []string{e.PlatformID}
	}
	return out, true
}

type personJSON struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
}

// activityJSONOut is GET /api/servers/{id}/activity: the events in
// [from, until), newest first, with consecutive autosaves collapsed to the
// newest, their counts per category, every player the server has seen
// (online first, then most recently seen) and when tracking began.
type activityJSONOut struct {
	TimeZone string         `json:"timeZone"`
	From     string         `json:"from"`
	Until    string         `json:"until"`
	Earliest string         `json:"earliest,omitempty"`
	Events   []eventJSON    `json:"events"`
	Counts   map[string]int `json:"counts"`
	People   []personJSON   `json:"people"`
}

// activityWindow is [from, until): until is before (or, without one, the
// end of today), and from the local midnight days-1 days before the day
// that ends at until.
func activityWindow(before time.Time, now time.Time, loc *time.Location, days int) (from, until time.Time) {
	if before.IsZero() {
		_, until = localDays(now, loc, 1)
	} else {
		until = before
	}
	starts, _ := localDays(until.Add(-time.Nanosecond), loc, days)
	return starts[0], until
}

func (s *server) activity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, ok := s.unlocked(r, id)
	if !ok {
		notFound(w)
		return
	}
	q := r.URL.Query()
	var before time.Time
	if v := q.Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad before")
			return
		}
		before = t
	}
	days := activityDays
	if v := q.Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxActivityDays {
			writeError(w, http.StatusBadRequest, "bad days")
			return
		}
		days = n
	}
	out, err := s.buildActivity(r.Context(), srv, before, days)
	if err != nil {
		s.internalError(w, "activity", id, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) buildActivity(ctx context.Context, srv *config.Server, before time.Time, days int) (*activityJSONOut, error) {
	loc := srv.Location()
	from, until := activityWindow(before, s.Now(), loc, days)
	out := &activityJSONOut{TimeZone: loc.String(), From: rfc3339(from), Until: rfc3339(until),
		Events: []eventJSON{}, Counts: map[string]int{}, People: []personJSON{}}
	for _, c := range categories {
		out.Counts[c] = 0
	}
	if first, ok, err := s.Store.EarliestEvent(ctx, srv.ID); err != nil {
		return nil, err
	} else if ok {
		out.Earliest = rfc3339(first)
	}
	who, err := s.people(ctx, srv.ID)
	if err != nil {
		return nil, err
	}
	for _, p := range who.list {
		out.People = append(out.People, personJSON{ID: p.PlatformID, Name: p.Name, Online: p.Online})
	}
	mask, err := s.currentMask(ctx, srv.ID)
	if err != nil {
		return nil, err
	}
	stored, err := s.Store.EventsBetween(ctx, srv.ID, from, until)
	if err != nil {
		return nil, err
	}
	for _, se := range stored {
		e, ok := toEventJSON(se, who, mask)
		if !ok {
			continue
		}
		// Autosaves come every ~20 minutes: a run of them with nothing in
		// between shows as its newest.
		if n := len(out.Events); n > 0 && e.Type == logwatch.EvWorldSaved && out.Events[n-1].Type == logwatch.EvWorldSaved {
			continue
		}
		out.Events = append(out.Events, e)
		out.Counts[e.Category]++
	}
	return out, nil
}

// currentMask is the latest save's explored mask, or nil without a save.
func (s *server) currentMask(ctx context.Context, serverID string) (*explored.Mask, error) {
	ws, ok, err := s.worlds.get(ctx, serverID)
	if err != nil || !ok {
		return nil, err
	}
	return ws.mask, nil
}

type spanJSON struct {
	Since string `json:"since"`
	Until string `json:"until,omitempty"` // absent while the session is open
}

type todayPlayerJSON struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Online bool       `json:"online"`
	Spans  []spanJSON `json:"spans"`
}

// todayJSON is GET /api/servers/{id}/sessions/today: who played today in
// the server's local time, each session clipped to [dayStart, now].
type todayJSON struct {
	TimeZone string            `json:"timeZone"`
	DayStart string            `json:"dayStart"`
	DayEnd   string            `json:"dayEnd"`
	Now      string            `json:"now"`
	Players  []todayPlayerJSON `json:"players"`
}

func (s *server) sessionsToday(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, ok := s.unlocked(r, id)
	if !ok {
		notFound(w)
		return
	}
	now, loc := s.Now(), srv.Location()
	starts, end := localDays(now, loc, 1)
	sessions, err := s.Store.SessionsOverlapping(r.Context(), srv.ID, starts[0], end)
	if err != nil {
		s.internalError(w, "sessions today", id, err)
		return
	}
	writeJSON(w, http.StatusOK, todayJSON{
		TimeZone: loc.String(), DayStart: rfc3339(starts[0]), DayEnd: rfc3339(end), Now: rfc3339(now),
		Players: todayPlayers(sessions, starts[0], now),
	})
}

// todayPlayers groups sessions by player (platform ID, or the name for a
// session without one), clipping each to [dayStart, now], in the order
// each player first appears (sessions come oldest first). The player's
// name is the one on their latest session.
func todayPlayers(sessions []store.Session, dayStart, now time.Time) []todayPlayerJSON {
	out := []todayPlayerJSON{}
	index := map[string]int{}
	for _, ss := range sessions {
		key := "id:" + ss.PlatformID
		if ss.PlatformID == "" {
			key = "name:" + ss.Name
		}
		i, ok := index[key]
		if !ok {
			i = len(out)
			index[key] = i
			out = append(out, todayPlayerJSON{ID: ss.PlatformID, Spans: []spanJSON{}})
		}
		p := &out[i]
		p.Name = ss.Name
		since := ss.Since
		if since.Before(dayStart) {
			since = dayStart
		}
		sp := spanJSON{Since: rfc3339(since)}
		if ss.Until == nil {
			p.Online = true
		} else {
			u := *ss.Until
			if u.After(now) {
				u = now
			}
			sp.Until = rfc3339(u)
		}
		p.Spans = append(p.Spans, sp)
	}
	return out
}
```

In `internal/server/server.go`, in `NewHandlers`, replace:

```go
	mux.Handle("GET /api/servers/{id}/players/{player}", gzipJSON(s.Log, http.HandlerFunc(s.profile)))
```

with:

```go
	mux.Handle("GET /api/servers/{id}/players/{player}", gzipJSON(s.Log, http.HandlerFunc(s.profile)))
	mux.Handle("GET /api/servers/{id}/activity", gzipJSON(s.Log, http.HandlerFunc(s.activity)))
	mux.Handle("GET /api/servers/{id}/sessions/today", gzipJSON(s.Log, http.HandlerFunc(s.sessionsToday)))
```

- [ ] **Step 4: Give the card the same entries and its time zone**

In `internal/server/api.go`, delete `activityJSON` (the whole type), then make the three edits below.

In `internal/server/api.go`, delete:

```go
type activityJSON struct {
	Type     string `json:"type"`
	At       string `json:"at"`
	Name     string `json:"name,omitempty"`
	Platform string `json:"platform,omitempty"`
	Code     string `json:"code,omitempty"`
	Players  *int   `json:"players,omitempty"`
	Seconds  int64  `json:"seconds,omitempty"`
	Version  string `json:"version,omitempty"`
}

```

In `internal/server/api.go`, the card struct (gofmt realigns it), replace:

```go
type cardJSONOut struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Crossplay      bool           `json:"crossplay"`
	Address        string         `json:"address,omitempty"`
	DiscordHint    string         `json:"discordHint,omitempty"`
	MaxPlayers     int            `json:"maxPlayers"`
	Status         string         `json:"status"`
	Version        string         `json:"version,omitempty"`
	NetworkVersion int            `json:"networkVersion,omitempty"`
	UpSince        string         `json:"upSince,omitempty"`
	LastHeartbeat  string         `json:"lastHeartbeat,omitempty"`
	Players        int            `json:"players"`
	JoinCode       string         `json:"joinCode,omitempty"`
	JoinCodeAt     string         `json:"joinCodeAt,omitempty"`
	Online         []onlineJSON   `json:"online"`
	Recent         []recentJSON   `json:"recent"`
	Activity       []activityJSON `json:"activity"`
	World          *worldJSON     `json:"world,omitempty"`
	Tiles          tilesJSON      `json:"tiles"`
}

```

with:

```go
type cardJSONOut struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Crossplay      bool         `json:"crossplay"`
	Address        string       `json:"address,omitempty"`
	DiscordHint    string       `json:"discordHint,omitempty"`
	MaxPlayers     int          `json:"maxPlayers"`
	Status         string       `json:"status"`
	Version        string       `json:"version,omitempty"`
	NetworkVersion int          `json:"networkVersion,omitempty"`
	UpSince        string       `json:"upSince,omitempty"`
	LastHeartbeat  string       `json:"lastHeartbeat,omitempty"`
	Players        int          `json:"players"`
	JoinCode       string       `json:"joinCode,omitempty"`
	JoinCodeAt     string       `json:"joinCodeAt,omitempty"`
	TimeZone       string       `json:"timeZone"`
	Online         []onlineJSON `json:"online"`
	Recent         []recentJSON `json:"recent"`
	Activity       []eventJSON  `json:"activity"`
	World          *worldJSON   `json:"world,omitempty"`
	Tiles          tilesJSON    `json:"tiles"`
}

```

In `internal/server/api.go`, in `buildCard`, the literal, replace:

```go
		Online: []onlineJSON{}, Recent: []recentJSON{}, Activity: []activityJSON{},
```

with:

```go
		TimeZone: srv.Location().String(),
		Online:   []onlineJSON{}, Recent: []recentJSON{}, Activity: []eventJSON{},
```

In `internal/server/api.go`, in `buildCard`, the activity part (the world state is now read first, for the mask), replace:

```go
	events, err := s.Store.RecentActivity(ctx, srv.ID, activityLimit)
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		c.Activity = append(c.Activity, activityJSON{
			Type: e.Type, At: rfc3339(e.At), Name: e.Name, Platform: e.Platform,
			Code: e.Code, Players: e.Players, Seconds: e.Seconds, Version: e.Version,
		})
	}

	ws, ok, err := s.worlds.get(ctx, srv.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return c, nil
	}
```

with:

```go
	ws, ok, err := s.worlds.get(ctx, srv.ID)
	if err != nil {
		return nil, err
	}
	events, err := s.Store.RecentEvents(ctx, srv.ID, activityLimit)
	if err != nil {
		return nil, err
	}
	who, err := s.people(ctx, srv.ID)
	if err != nil {
		return nil, err
	}
	var mask *explored.Mask
	if ok {
		mask = ws.mask
	}
	for _, se := range events {
		if e, shown := toEventJSON(se, who, mask); shown {
			c.Activity = append(c.Activity, e)
		}
	}
	if !ok {
		return c, nil
	}
```

- [ ] **Step 5: Run the tests to make sure they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH gofmt -l ./internal && PATH=$HOME/.local/go/bin:$PATH go vet ./internal/server/ && PATH=$HOME/.local/go/bin:$PATH go test ./internal/server/`

Expected: `gofmt -l` prints nothing; `ok`. The existing `TestCard` still passes: the old activity fields keep their names.

- [ ] **Step 6: Commit**

```bash
cd /workspace/Farsight
git add internal/server/activity.go \
  internal/server/activity_test.go \
  internal/server/api.go \
  internal/server/server.go
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(server): activity timeline and today's sessions; card activity in the same shape

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
git status --short   # must print nothing
```

---

### Task 7: Web: the player profile (desktop panel, mobile sheet, `#p=`)

"Profile →" on every Online and Recently online row opens the player's profile.

- **Desktop:** it replaces the side panel in the same 344 px box, with a back arrow.
- **Mobile:** a full-height sheet with "‹ PLAYER".
- **URL:** `#s=<server>&p=<playerId>`. Opening the profile adds a history entry, so browser back closes it, and a linked profile opens on load.

The open view lives in `AppState.view`:
- `openView` adds a history entry by setting `location.hash`. SvelteKit leaves hash changes alone, and `history.state` keeps its navigation index.
- A `hashchange` listener follows back and forward.
- `closeView` goes back through the entry it added, or replaces the hash for a view opened from a link.
- Switching server closes the view.

Days and clock times are in the server's zone (`zoned.ts`, on `Intl` with a cached formatter per zone), not the viewer's.

The profile body (`ProfileContent.svelte`) is shared by both wrappers. It shows a skeleton while loading, "This player hasn’t been seen on this server." for a 404, and "None yet." / "No bases yet." for empty sections. "Map →" centres the map at zoom 4 and selects the item's marker when it is on the map; on mobile it closes the sheet first.

**Files:**
- Modify: `web/src/lib/types.ts` (`Card.timeZone`, `Marker.namer`, `Profile`), `web/src/lib/api.ts` (`getProfile`)
- Modify: `web/src/lib/share.ts` (`View`, `parseHash` view, `hashFor(server, view?)`, `sameView`)
- Modify: `web/src/lib/state.svelte.ts` (`view`, `openView`, `closeView`, hash sync)
- Create: `web/src/lib/zoned.ts`, `web/src/lib/profile.ts`
- Modify: `web/src/lib/derive.ts` (`recentList` keeps `platformId`)
- Create: `web/src/lib/components/ProfileContent.svelte`, `ProfilePanel.svelte`, `ProfileSheet.svelte`
- Modify: `web/src/lib/components/PlayersTab.svelte`, `PeekSheet.svelte`, `DesktopShell.svelte`, `MobileShell.svelte`
- Test: `web/src/lib/share.test.ts`, `state.svelte.test.ts`, `zoned.test.ts`, `profile.test.ts`, `api.test.ts`, `derive.test.ts`

**Interfaces:**
- Consumes: the profile JSON (Task 5) and the card's `timeZone` (Task 6).
- Produces:
  - TS `Profile` (mirrors Task 5's JSON), `Card.timeZone?: string`, `Marker.namer?: string`.
  - `getProfile(id: string, player: string, f?, timeoutMs?): Promise<Profile | null>` (404 → null).
  - `type View = { kind: 'profile'; player: string } | { kind: 'activity' }`; `parseHash(hash).view`; `hashFor(server: string, view?: View): string` (`#s=x&p=…` / `#s=x&activity`); `sameView(a, b)`.
  - `app.view: View | undefined`, `app.openView(v: View)`, `app.closeView()`; `AppDeps.onHashChange`, `AppDeps.history.back`.
  - `zoned.ts`: `zoned(at, tz): Zoned`, `dayKey(at, tz)` ("2026-10-05"), `prevDayKey(key)`, `zClock(at, tz)` ("14:20"), `zDate` ("30 Sep 2026"), `zWeekday` ("Tue 29 Sep"), `zDayMonth` ("30 Sep"), `zDayRef(at, tz, now)` ("today 14:20"), `weekdayInitial(key)`.
  - `profile.ts`: `fmtPlaytime(sec, big?)`, `hoursLabel(sec)`, `dayBars(days)`, `profileView(p, now): ProfileView`.
  - `recentList(card)` items gain `platformId`.
  - Components: `ProfileContent {serverId, player, mobile?, onmap(x, z, id)}`, `ProfilePanel {serverId, player, onback, onmap}`, `ProfileSheet {serverId, player, onclose, onmap}`.

- [ ] **Step 1: Write the failing unit tests**

In `web/src/lib/share.test.ts`, replace:

```ts
import { hashFor, parseHash } from './share';
```

with:

```ts
import { hashFor, parseHash, sameView } from './share';
```

Append to the end of `web/src/lib/share.test.ts`, after a blank line:

```ts
describe('views (Plan 7)', () => {
	test('a profile', () => {
		expect(parseHash('#s=demo&p=76561190000000001')).toEqual({
			server: 'demo',
			view: { kind: 'profile', player: '76561190000000001' }
		});
		expect(hashFor('demo', { kind: 'profile', player: 'a b' })).toBe('#s=demo&p=a%20b');
	});
	test('the activity timeline', () => {
		expect(parseHash('#s=demo&activity')).toEqual({ server: 'demo', view: { kind: 'activity' } });
		expect(hashFor('demo', { kind: 'activity' })).toBe('#s=demo&activity');
	});
	test('a profile wins over activity; an empty p is no view', () => {
		expect(parseHash('#s=d&activity&p=1').view).toEqual({ kind: 'profile', player: '1' });
		expect(parseHash('#s=d&p=').view).toBeUndefined();
	});
	test('round trip', () => {
		for (const v of [undefined, { kind: 'activity' } as const, { kind: 'profile', player: 'Xbox_2' } as const]) {
			expect(parseHash(hashFor('x', v)).view).toEqual(v);
		}
	});
	test('sameView', () => {
		expect(sameView(undefined, undefined)).toBe(true);
		expect(sameView({ kind: 'activity' }, undefined)).toBe(false);
		expect(sameView({ kind: 'profile', player: '1' }, { kind: 'profile', player: '1' })).toBe(true);
		expect(sameView({ kind: 'profile', player: '1' }, { kind: 'profile', player: '2' })).toBe(false);
	});
});
```

In `web/src/lib/state.svelte.test.ts`, in `setup`, replace:

```ts
	// SvelteKit keeps its navigation index in history.state; hash updates keep it.
	const history = { replaceState, state: KIT_STATE };
```

with:

```ts
	// SvelteKit keeps its navigation index in history.state; hash updates keep it.
	const back = vi.fn();
	const history = { replaceState, state: KIT_STATE, back };
	let hashChanged: () => void = () => {};
	const onHashChange = (cb: () => void) => {
		hashChanged = cb;
		return () => (hashChanged = () => {});
	};
```

In `web/src/lib/state.svelte.test.ts`, at the end of `setup`, replace:

```ts
	const app = new AppState({ fetch: server.fetch, storage, location, history, visibility, root, log });
	return { app, server, location, replaceState, replaced, storage, visibility, root, log };
```

with:

```ts
	const app = new AppState({ fetch: server.fetch, storage, location, history, visibility, root, log, onHashChange });
	return { app, server, location, replaceState, replaced, storage, visibility, root, log, back, hashChanged: () => hashChanged() };
```

Append to the end of `web/src/lib/state.svelte.test.ts`, after a blank line:

```ts
describe('views (Plan 7)', () => {
	test('openView adds a history entry; closeView goes back', async () => {
		const { app, location, back, hashChanged } = setup('#s=a');
		app.start();
		await flush();
		app.openView({ kind: 'profile', player: '111' });
		expect(app.view).toEqual({ kind: 'profile', player: '111' });
		expect(location.hash).toBe('#s=a&p=111');
		app.closeView();
		expect(app.view).toBeUndefined();
		expect(back).toHaveBeenCalledTimes(1);
		// The browser then lands back on #s=a.
		location.hash = '#s=a';
		hashChanged();
		expect(app.view).toBeUndefined();
	});

	test('browser back from an open view closes it; forward reopens it', async () => {
		const { app, location, hashChanged } = setup('#s=a');
		app.start();
		await flush();
		app.openView({ kind: 'activity' });
		location.hash = '#s=a';
		hashChanged();
		expect(app.view).toBeUndefined();
		location.hash = '#s=a&activity';
		hashChanged();
		expect(app.view).toEqual({ kind: 'activity' });
	});

	test('a linked profile opens on load and closes by replacing the hash', async () => {
		const { app, replaceState, back } = setup('#s=b&p=222');
		app.start();
		await flush();
		expect(app.currentId).toBe('b');
		expect(app.view).toEqual({ kind: 'profile', player: '222' });
		expect(replaceState).toHaveBeenLastCalledWith(KIT_STATE, '', '#s=b&p=222');
		app.closeView();
		expect(back).not.toHaveBeenCalled();
		expect(replaceState).toHaveBeenLastCalledWith(KIT_STATE, '', '#s=b');
	});

	test('opening a view over another replaces it; switching server closes it', async () => {
		const { app, location, replaceState } = setup('#s=a');
		app.start();
		await flush();
		app.openView({ kind: 'activity' });
		app.openView({ kind: 'profile', player: '1' });
		expect(replaceState).toHaveBeenLastCalledWith(KIT_STATE, '', '#s=a&p=1');
		expect(location.hash).toBe('#s=a&p=1');
		app.select('b');
		expect(app.view).toBeUndefined();
		expect(location.hash).toBe('#s=b');
	});

	test('a hash for another server is ignored', async () => {
		const { app, location, hashChanged } = setup('#s=a');
		app.start();
		await flush();
		location.hash = '#s=b&activity';
		hashChanged();
		expect(app.view).toBeUndefined();
	});
});
```

Create `web/src/lib/zoned.test.ts`:

```ts
import { describe, expect, test } from 'vitest';
import { dayKey, prevDayKey, weekdayInitial, zClock, zDate, zDayRef, zWeekday, zoned } from './zoned';

describe('zoned', () => {
	test('wall clock in the server zone, not the viewer’s', () => {
		// 22:30 UTC is already 00:30 the next day in Oslo (CEST).
		expect(zoned('2026-10-05T22:30:00Z', 'Europe/Oslo')).toEqual({ y: 2026, m: 10, d: 6, hh: 0, mm: 30, wd: 2 });
		expect(dayKey('2026-10-05T22:30:00Z', 'Europe/Oslo')).toBe('2026-10-06');
		expect(dayKey('2026-10-05T22:30:00Z', 'UTC')).toBe('2026-10-05');
		expect(zClock('2026-10-05T22:30:00Z', 'Europe/Oslo')).toBe('00:30');
	});
	test('an unknown zone falls back to UTC', () => {
		expect(zClock('2026-10-05T22:30:00Z', 'Mars/Olympus')).toBe('22:30');
	});
	test('labels', () => {
		expect(zDate('2026-09-30T08:00:00Z', 'UTC')).toBe('30 Sep 2026');
		expect(zWeekday('2026-09-29T08:00:00Z', 'UTC')).toBe('Tue 29 Sep');
		expect(prevDayKey('2026-10-01')).toBe('2026-09-30');
		expect(weekdayInitial('2026-10-05')).toBe('M');
	});
	test('zDayRef', () => {
		const now = new Date('2026-10-05T12:00:00Z');
		expect(zDayRef('2026-10-05T03:12:00Z', 'UTC', now)).toBe('today 03:12');
		expect(zDayRef('2026-10-04T23:50:00Z', 'UTC', now)).toBe('yesterday 23:50');
		expect(zDayRef('2026-10-04T23:50:00Z', 'Europe/Oslo', now)).toBe('today 01:50');
		expect(zDayRef('2026-09-28T03:12:00Z', 'UTC', now)).toBe('28 Sep 03:12');
	});
});
```

Create `web/src/lib/profile.test.ts`:

```ts
import { describe, expect, test } from 'vitest';
import { dayBars, fmtPlaytime, hoursLabel, profileView } from './profile';
import type { Profile } from './types';

const NOW = new Date('2026-10-05T12:00:00Z');

function makeProfile(over: Partial<Profile> = {}): Profile {
	return {
		id: '111',
		name: 'astrid',
		platform: 'Steam',
		timeZone: 'Europe/Oslo',
		online: true,
		since: '2026-10-05T10:48:00Z',
		firstSeen: '2026-09-30T18:00:00Z',
		trackedSince: '2026-09-30T06:00:00Z',
		weekSeconds: 51600,
		allSeconds: 763200,
		sessions: 96,
		days: [
			{ date: '2026-09-29', seconds: 9000 },
			{ date: '2026-09-30', seconds: 0 },
			{ date: '2026-10-01', seconds: 10800 },
			{ date: '2026-10-02', seconds: 16200 },
			{ date: '2026-10-03', seconds: 3600 },
			{ date: '2026-10-04', seconds: 60 },
			{ date: '2026-10-05', seconds: 9720 }
		],
		beds: { count: 2, near: ['Longhouse', 'Plains'] },
		bases: [{ id: 'base-1', name: 'Longhouse', pieces: 2184, biome: 'Meadows', x: 1, z: 2 }],
		portals: [{ id: 'portal-1', tag: 'home', paired: true, x: 1, z: 2 }],
		tames: [],
		deaths: {
			spotted: 41,
			week: 3,
			tombstones: [{ id: 'tombstone-1', biome: 'Swamp', firstSeen: '2026-10-05T12:20:00Z', x: 3, z: 4 }]
		},
		...over
	};
}

describe('fmtPlaytime / hoursLabel', () => {
	test('formats', () => {
		expect(fmtPlaytime(0)).toBe('0m');
		expect(fmtPlaytime(38 * 60)).toBe('38m');
		expect(fmtPlaytime(9 * 3600 + 5 * 60)).toBe('9h 05m');
		expect(fmtPlaytime(14 * 3600 + 20 * 60)).toBe('14h 20m');
		expect(fmtPlaytime(212 * 3600 + 59 * 60, true)).toBe('212h');
		expect(fmtPlaytime(6 * 3600 + 10 * 60, true)).toBe('6h 10m');
		expect(hoursLabel(0)).toBe('');
		expect(hoursLabel(60)).toBe('<0.1');
		expect(hoursLabel(9000)).toBe('2.5');
		expect(hoursLabel(10800)).toBe('3');
	});
});

describe('dayBars', () => {
	test('weekday initials, today last, the busiest day at 78%', () => {
		const bars = dayBars(makeProfile().days);
		expect(bars.map((b) => b.d).join('')).toBe('TWTFSSM');
		expect(bars[3].pct).toBe(78);
		expect(bars[1]).toEqual({ d: 'W', label: '', pct: 0, today: false });
		expect(bars[6].today).toBe(true);
		expect(bars[6].label).toBe('2.7');
	});
	test('a quiet week stays low (max is at least an hour)', () => {
		const bars = dayBars([{ date: '2026-10-05', seconds: 1800 }]);
		expect(bars[0].pct).toBe(39);
	});
});

describe('profileView', () => {
	test('online', () => {
		const v = profileView(makeProfile(), NOW);
		expect(v.initial).toBe('A');
		expect(v.status).toBe('Online now · 1h 12m');
		expect(v.stats).toEqual([
			{ k: 'This week', v: '14h 20m' },
			{ k: 'All time', v: '212h' },
			{ k: 'Sessions', v: '96' }
		]);
		expect(v.first).toBe('30 Sep 2026');
		expect(v.last).toBe('Online now');
		expect(v.beds).toBe('2 · Longhouse, Plains');
		expect(v.tracked).toBe('All time and first seen count from 30 Sep 2026, when tracking began.');
		expect(v.portalCount).toBe('1 portal');
		expect(v.tameCount).toBe('');
		expect(v.deathLine).toBe('41 spotted · 3 this week');
		expect(v.tombs[0].text).toBe('Tombstone in the Swamp · since save 14:20');
		expect(v.bases[0].sub).toBe('2,184 pieces · Meadows');
	});
	test('offline', () => {
		const v = profileView(
			makeProfile({ online: false, since: undefined, lastSeen: '2026-10-05T10:00:00Z', beds: { count: 0, near: [] } }),
			NOW
		);
		expect(v.status).toBe('Last seen 2 h ago');
		expect(v.last).toBe('today 12:00');
		expect(v.beds).toBe('None placed');
	});
	test('an older tombstone names its day', () => {
		const p = makeProfile();
		p.deaths.tombstones[0].firstSeen = '2026-10-03T12:20:00Z';
		expect(profileView(p, NOW).tombs[0].text).toBe('Tombstone in the Swamp · since save 3 Oct 14:20');
	});
});
```

In `web/src/lib/api.test.ts`, replace:

```ts
import { ApiError, getCard, getSnapshot, listServers, tileUrl, unlock } from './api';
```

with:

```ts
import { ApiError, getCard, getProfile, getSnapshot, listServers, tileUrl, unlock } from './api';
```

Append to the end of `web/src/lib/api.test.ts`, after a blank line:

```ts
describe('getProfile (Plan 7)', () => {
	test('escapes the ids; 404 -> null', async () => {
		const fake = vi.fn().mockResolvedValue(textResponse('not found', 404));
		expect(await getProfile('a b', 'Xbox/2', fake as unknown as typeof fetch)).toBeNull();
		expect(fake.mock.calls[0][0]).toBe('/api/servers/a%20b/players/Xbox%2F2');
	});
	test('200 -> the profile; 500 -> ApiError', async () => {
		const ok = vi.fn().mockResolvedValue(jsonResponse({ id: '1', name: 'A' }));
		expect(await getProfile('a', '1', ok as unknown as typeof fetch)).toMatchObject({ id: '1', name: 'A' });
		const bad = vi.fn().mockResolvedValue(textResponse('boom', 500));
		await expect(getProfile('a', '1', bad as unknown as typeof fetch)).rejects.toMatchObject({ status: 500 });
	});
});
```

In `web/src/lib/derive.test.ts`, in the `recentList` test, replace:

```ts
		const alina = list.find((r) => r.name === 'Alina');
		expect(alina?.until).toBe(iso(-500));
```

with:

```ts
		const alina = list.find((r) => r.name === 'Alina');
		expect(alina?.until).toBe(iso(-500));
		expect(alina?.platformId).toBe('2');
```

- [ ] **Step 2: Run them to make sure they fail**

Run: `cd web && npx vitest run src/lib/share.test.ts src/lib/state.svelte.test.ts src/lib/zoned.test.ts src/lib/profile.test.ts src/lib/api.test.ts src/lib/derive.test.ts`

Expected: FAIL: `zoned.test.ts` and `profile.test.ts` can't import their modules, `getProfile` is not a function, and the view and `platformId` assertions fail.

- [ ] **Step 3: Types and the API call**

In `web/src/lib/types.ts`, in `Card`, replace:

```ts
	joinCode?: string;
	joinCodeAt?: string;
	online: OnlinePlayer[];
```

with:

```ts
	joinCode?: string;
	joinCodeAt?: string;
	/** The server's log time zone (IANA): local days and clock times in profiles and the timeline. */
	timeZone?: string;
	online: OnlinePlayer[];
```

In `web/src/lib/types.ts`, in `Marker`, replace:

```ts
	type?: string;
	pair?: string;
}
```

with:

```ts
	type?: string;
	pair?: string;
	/** A tame's namer: their platform user ID ("Steam_…"). */
	namer?: string;
}
```

Append to the end of `web/src/lib/types.ts`, after a blank line:

```ts
/** GET /api/servers/{id}/players/{player} (Plan 7). Times are RFC 3339 UTC. */
export interface Profile {
	/** The platform ID (the sessions' platformId). */
	id: string;
	name: string;
	platform: string;
	timeZone: string;
	online: boolean;
	/** The open session's start, when online. */
	since?: string;
	/** The latest session's end, when offline. */
	lastSeen?: string;
	firstSeen: string;
	trackedSince: string;
	weekSeconds: number;
	allSeconds: number;
	sessions: number;
	/** Seven local days, oldest first, today last. */
	days: { date: string; seconds: number }[];
	beds: { count: number; near: string[] };
	bases: { id: string; name: string; pieces: number; biome: string; x: number; z: number }[];
	portals: { id: string; tag: string; paired: boolean; x: number; z: number }[];
	tames: { id: string; name: string; species: string; x: number; z: number }[];
	deaths: {
		spotted: number;
		week: number;
		tombstones: { id: string; biome: string; firstSeen: string; x: number; z: number }[];
	};
}
```

In `web/src/lib/api.ts`, replace:

```ts
import type { Card, ServerSummary, SnapshotView } from './types';
```

with:

```ts
import type { Card, Profile, ServerSummary, SnapshotView } from './types';
```

In `web/src/lib/api.ts`, replace:

```ts
const UNLOCK_TIMEOUT_MS = 15_000;
```

with:

```ts
const UNLOCK_TIMEOUT_MS = 15_000;
const PROFILE_TIMEOUT_MS = 20_000;
```

In `web/src/lib/api.ts`, before `unlock`, replace:

```ts
export async function unlock(
```

with:

```ts
/** A player's profile; null when the server doesn't know them (404). */
export async function getProfile(
	id: string,
	player: string,
	f: typeof fetch = fetch,
	timeoutMs = PROFILE_TIMEOUT_MS
): Promise<Profile | null> {
	return withTimeout(timeoutMs, async (signal) => {
		const res = await f(`/api/servers/${encodeURIComponent(id)}/players/${encodeURIComponent(player)}`, jsonInit(signal));
		if (res.status === 404) return null;
		if (!res.ok) throw new ApiError(res.status, await res.text());
		return (await res.json()) as Profile;
	});
}

export async function unlock(
```

- [ ] **Step 4: Views in the hash and in the app state**

Replace the whole of `web/src/lib/share.ts` with:

```ts
// Share-link and view hash parsing: `/#s=<id>&k=<passphrase>`, plus the
// open view: `&p=<playerId>` (a player's profile) or `&activity` (the
// activity timeline). Per the plan's global constraints, the passphrase is
// only ever read from the hash and is never stored or logged; the caller
// replaces the URL with `hashFor(server)` once it has been used.

/** A view over the map that browser back closes (Plan 7). */
export type View = { kind: 'profile'; player: string } | { kind: 'activity' };

export interface ShareLink {
	server?: string;
	key?: string;
	view?: View;
}

export function parseHash(hash: string): ShareLink {
	if (!hash || hash === '#') return {};
	const raw = hash.startsWith('#') ? hash.slice(1) : hash;
	const params = new URLSearchParams(raw);
	const result: ShareLink = {};
	const server = params.get('s');
	const key = params.get('k');
	const player = params.get('p');
	if (server) result.server = server;
	if (key) result.key = key;
	if (player) result.view = { kind: 'profile', player };
	else if (params.has('activity')) result.view = { kind: 'activity' };
	return result;
}

export function hashFor(server: string, view?: View): string {
	const base = `#s=${encodeURIComponent(server)}`;
	if (view?.kind === 'profile') return `${base}&p=${encodeURIComponent(view.player)}`;
	if (view?.kind === 'activity') return `${base}&activity`;
	return base;
}

export function sameView(a: View | undefined, b: View | undefined): boolean {
	if (!a || !b) return a === b;
	if (a.kind === 'profile' && b.kind === 'profile') return a.player === b.player;
	return a.kind === b.kind;
}
```

In `web/src/lib/state.svelte.ts`, in the imports, replace:

```ts
import { hashFor, parseHash } from './share';
```

with:

```ts
import { hashFor, parseHash, sameView, type View } from './share';
```

In `web/src/lib/state.svelte.ts`, in `AppDeps`, replace:

```ts
	location?: Pick<Location, 'hash'>;
	history?: Pick<History, 'replaceState' | 'state'>;
```

with:

```ts
	location?: Pick<Location, 'hash'>;
	history?: Pick<History, 'replaceState' | 'state' | 'back'>;
	/** Calls `cb` on every `hashchange` (browser back and forward); returns an unsubscribe. */
	onHashChange?: (cb: () => void) => () => void;
```

In `web/src/lib/state.svelte.ts`, in `AppState`'s fields, replace:

```ts
	/** Set when the unlock dialog should be shown (share link failed, "Add a server…"). */
	unlockPrompt = $state<UnlockPrompt | undefined>(undefined);
```

with:

```ts
	/** Set when the unlock dialog should be shown (share link failed, "Add a server…"). */
	unlockPrompt = $state<UnlockPrompt | undefined>(undefined);
	/** The open view (a profile or the activity timeline), mirrored in the URL hash. */
	view = $state<View | undefined>(undefined);
```

In `web/src/lib/state.svelte.ts`, in the private fields, replace:

```ts
	private hist: Pick<History, 'replaceState' | 'state'> | undefined;
```

with:

```ts
	private hist: Pick<History, 'replaceState' | 'state' | 'back'> | undefined;
	private onHashChange: (cb: () => void) => () => void;
	/** The open view added a history entry (openView), so closing it goes back. */
	private pushed = false;
```

In `web/src/lib/state.svelte.ts`, at the end of the constructor, replace:

```ts
		this.log = deps.log ?? ((m, e) => console.warn(m, e));
	}
```

with:

```ts
		this.log = deps.log ?? ((m, e) => console.warn(m, e));
		this.onHashChange =
			deps.onHashChange ??
			((cb) => {
				if (typeof window === 'undefined') return () => {};
				window.addEventListener('hashchange', cb);
				return () => window.removeEventListener('hashchange', cb);
			});
	}
```

In `web/src/lib/state.svelte.ts`, in `start`, replace:

```ts
		const offVisible = this.vis.onVisible(() => {
			this.now = new Date();
			void this.refresh();
		});
```

with:

```ts
		const offVisible = this.vis.onVisible(() => {
			this.now = new Date();
			void this.refresh();
		});
		const offHash = this.onHashChange(() => this.syncView());
```

In `web/src/lib/state.svelte.ts`, in `start`'s `stop`, replace:

```ts
			offVisible();
			clearTimeout(this.toastTimer);
```

with:

```ts
			offVisible();
			offHash();
			clearTimeout(this.toastTimer);
```

In `web/src/lib/state.svelte.ts`, at the end of `boot`, replace:

```ts
		const pick = this.servers.find((s) => s.id === link.server)?.id ?? this.servers[0]?.id;
		if (pick !== undefined) this.select(pick);
	}
```

with:

```ts
		const pick = this.servers.find((s) => s.id === link.server)?.id ?? this.servers[0]?.id;
		if (pick === undefined) return;
		this.select(pick);
		// A linked profile or timeline opens over its server. It added no
		// history entry, so closing it replaces the hash instead of going back.
		if (pick === link.server && link.view) {
			this.view = link.view;
			this.replaceHash(pick);
		}
	}
```

In `web/src/lib/state.svelte.ts`, in `select`, replace:

```ts
	select(id: string): void {
		this.gen++;
		this.currentId = id;
```

with:

```ts
	select(id: string): void {
		this.gen++;
		this.currentId = id;
		this.view = undefined;
		this.pushed = false;
```

In `web/src/lib/state.svelte.ts`, after `closeUnlock`, replace:

```ts
	closeUnlock(): void {
		this.unlockPrompt = undefined;
	}
```

with:

```ts
	closeUnlock(): void {
		this.unlockPrompt = undefined;
	}

	/**
	 * Opens a profile or the activity timeline. The first view opened adds a
	 * history entry (so browser back closes it); opening another over it
	 * replaces that entry.
	 */
	openView(v: View): void {
		const id = this.currentId;
		if (id === undefined || sameView(v, this.view)) return;
		const replace = this.view !== undefined;
		this.view = v;
		if (replace || !this.loc) {
			this.replaceHash(id);
			return;
		}
		this.pushed = true;
		this.loc.hash = hashFor(id, v);
	}

	/** Closes the open view: back through its history entry, or by replacing the hash. */
	closeView(): void {
		if (this.view === undefined) return;
		this.view = undefined;
		if (this.pushed && this.hist) {
			this.pushed = false;
			this.hist.back();
			return;
		}
		if (this.currentId !== undefined) this.replaceHash(this.currentId);
	}

	/** Follows the hash after browser back or forward. */
	private syncView(): void {
		const link = parseHash(this.loc?.hash ?? '');
		if (link.server !== this.currentId) return;
		if (!sameView(link.view, this.view)) this.view = link.view;
		if (!link.view) this.pushed = false;
	}
```

In `web/src/lib/state.svelte.ts`, in `replaceHash` (the hash keeps the open view), replace:

```ts
			this.hist?.replaceState(this.hist.state, '', hashFor(id));
```

with:

```ts
			this.hist?.replaceState(this.hist.state, '', hashFor(id, this.view));
```

- [ ] **Step 5: Zoned dates and the profile view model**

Create `web/src/lib/zoned.ts`:

```ts
// Dates and clock times in a server's log time zone (Plan 7): profiles and
// the activity timeline count days the way the server does, wherever the
// viewer is. Built on Intl with a cached formatter per zone; an unknown
// zone falls back to UTC.

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export interface Zoned {
	y: number;
	/** 1–12 */
	m: number;
	d: number;
	hh: number;
	mm: number;
	/** 0 = Sunday */
	wd: number;
}

const formatters = new Map<string, Intl.DateTimeFormat>();

function formatter(tz: string): Intl.DateTimeFormat {
	let f = formatters.get(tz);
	if (!f) {
		const opts: Intl.DateTimeFormatOptions = {
			year: 'numeric',
			month: 'numeric',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit',
			hourCycle: 'h23'
		};
		try {
			f = new Intl.DateTimeFormat('en-US', { ...opts, timeZone: tz });
		} catch {
			f = new Intl.DateTimeFormat('en-US', { ...opts, timeZone: 'UTC' });
		}
		formatters.set(tz, f);
	}
	return f;
}

/** The wall-clock fields of `at` in `tz`. */
export function zoned(at: string | Date, tz: string): Zoned {
	const parts: Record<string, number> = {};
	for (const p of formatter(tz).formatToParts(new Date(at))) {
		if (p.type !== 'literal') parts[p.type] = Number(p.value);
	}
	const wd = new Date(Date.UTC(parts.year, parts.month - 1, parts.day)).getUTCDay();
	return { y: parts.year, m: parts.month, d: parts.day, hh: parts.hour, mm: parts.minute, wd };
}

const pad2 = (n: number) => String(n).padStart(2, '0');

/** "2026-10-05": the local date, for grouping and comparing days. */
export function dayKey(at: string | Date, tz: string): string {
	const z = zoned(at, tz);
	return `${z.y}-${pad2(z.m)}-${pad2(z.d)}`;
}

/** The day before `key` ("2026-10-01" → "2026-09-30"). */
export function prevDayKey(key: string): string {
	const d = new Date(`${key}T12:00:00Z`);
	d.setUTCDate(d.getUTCDate() - 1);
	return d.toISOString().slice(0, 10);
}

/** "14:20" */
export function zClock(at: string | Date, tz: string): string {
	const z = zoned(at, tz);
	return `${pad2(z.hh)}:${pad2(z.mm)}`;
}

/** "30 Sep 2026" */
export function zDate(at: string | Date, tz: string): string {
	const z = zoned(at, tz);
	return `${z.d} ${MONTHS[z.m - 1]} ${z.y}`;
}

/** "Tue 29 Sep" */
export function zWeekday(at: string | Date, tz: string): string {
	const z = zoned(at, tz);
	return `${WEEKDAYS[z.wd]} ${z.d} ${MONTHS[z.m - 1]}`;
}

/** "30 Sep" */
export function zDayMonth(at: string | Date, tz: string): string {
	const z = zoned(at, tz);
	return `${z.d} ${MONTHS[z.m - 1]}`;
}

/** "today 14:20" | "yesterday 14:20" | "28 Sep 14:20" */
export function zDayRef(at: string | Date, tz: string, now: Date): string {
	const k = dayKey(at, tz);
	const today = dayKey(now, tz);
	const clock = zClock(at, tz);
	if (k === today) return `today ${clock}`;
	if (k === prevDayKey(today)) return `yesterday ${clock}`;
	return `${zDayMonth(at, tz)} ${clock}`;
}

/** The weekday initial of a local date key: "2026-10-05" → "M". */
export function weekdayInitial(key: string): string {
	return WEEKDAYS[new Date(`${key}T12:00:00Z`).getUTCDay()][0];
}
```

Create `web/src/lib/profile.ts`:

```ts
// The player profile's view model (Plan 7, design `:116-185`): status line,
// stat tiles, the 7-day chart, first/last seen, beds and the section
// lines. Times and days use the server's zone (profile.timeZone).

import { fmtInt, fmtLastSeen, fmtSession } from './format';
import type { Profile } from './types';
import { weekdayInitial, zDate, zDayRef } from './zoned';

export interface ProfileDayBar {
	/** Weekday initial: "M". */
	d: string;
	/** Hours, "" for none: "2.5", "3", "<0.1". */
	label: string;
	/** Bar height, % of the chart (the busiest day is 78). */
	pct: number;
	today: boolean;
}

export interface ProfileView {
	initial: string;
	status: string;
	stats: { k: string; v: string }[];
	days: ProfileDayBar[];
	first: string;
	last: string;
	beds: string;
	tracked: string;
	portalCount: string;
	tameCount: string;
	deathLine: string;
	tombs: { id: string; text: string; x: number; z: number }[];
	bases: { id: string; name: string; sub: string; x: number; z: number }[];
}

const pad2 = (n: number) => String(n).padStart(2, '0');

/** Playtime: "38m", "9h 05m"; with `big`, ten hours and up as "212h". */
export function fmtPlaytime(sec: number, big = false): string {
	const h = Math.floor(sec / 3600);
	const m = Math.floor((sec % 3600) / 60);
	if (h === 0) return `${m}m`;
	if (big && h >= 10) return `${h}h`;
	return `${h}h ${pad2(m)}m`;
}

/** A chart label: hours to one decimal, whole hours bare, "" for none. */
export function hoursLabel(sec: number): string {
	if (sec <= 0) return '';
	const h = Math.round((sec / 3600) * 10) / 10;
	if (h === 0) return '<0.1';
	return Number.isInteger(h) ? String(h) : h.toFixed(1);
}

export function dayBars(days: Profile['days']): ProfileDayBar[] {
	const max = Math.max(...days.map((d) => d.seconds / 3600), 1);
	return days.map((d, i) => ({
		d: weekdayInitial(d.date),
		label: hoursLabel(d.seconds),
		pct: Math.round((d.seconds / 3600 / max) * 78),
		today: i === days.length - 1
	}));
}

export function profileView(p: Profile, now: Date): ProfileView {
	const tz = p.timeZone;
	const sessionSec = p.since ? Math.max(0, (now.getTime() - new Date(p.since).getTime()) / 1000) : 0;
	const lastSeen = p.lastSeen ? fmtLastSeen(p.lastSeen, now) : '';
	return {
		initial: Array.from(p.name.trim())[0]?.toUpperCase() ?? '?',
		status: p.online ? `Online now · ${fmtSession(sessionSec)}` : lastSeen.charAt(0).toUpperCase() + lastSeen.slice(1),
		stats: [
			{ k: 'This week', v: fmtPlaytime(p.weekSeconds) },
			{ k: 'All time', v: fmtPlaytime(p.allSeconds, true) },
			{ k: 'Sessions', v: fmtInt(p.sessions) }
		],
		days: dayBars(p.days),
		first: zDate(p.firstSeen, tz),
		last: p.online ? 'Online now' : p.lastSeen ? zDayRef(p.lastSeen, tz, now) : '—',
		beds: p.beds.count ? `${p.beds.count} · ${p.beds.near.join(', ')}` : 'None placed',
		tracked: `All time and first seen count from ${zDate(p.trackedSince, tz)}, when tracking began.`,
		portalCount: `${p.portals.length} ${p.portals.length === 1 ? 'portal' : 'portals'}`,
		tameCount: p.tames.length ? String(p.tames.length) : '',
		deathLine: `${p.deaths.spotted} spotted · ${p.deaths.week} this week`,
		tombs: p.deaths.tombstones.map((t) => ({
			id: t.id,
			text: `Tombstone in the ${t.biome} · since save ${zDayRef(t.firstSeen, tz, now).replace(/^today /, '')}`,
			x: t.x,
			z: t.z
		})),
		bases: p.bases.map((b) => ({ id: b.id, name: b.name, sub: `${fmtInt(b.pieces)} pieces · ${b.biome}`, x: b.x, z: b.z }))
	};
}
```

In `web/src/lib/derive.ts`, replace:

```ts
export function recentList(card: Card): { name: string; until: string }[] {
	const onlineNames = new Set(card.online.map((p) => p.name));
	const newestByName = new Map<string, string>();
	for (const r of card.recent) {
		if (onlineNames.has(r.name)) continue;
		const prev = newestByName.get(r.name);
		if (!prev || new Date(r.until).getTime() > new Date(prev).getTime()) {
			newestByName.set(r.name, r.until);
		}
	}
	return Array.from(newestByName, ([name, until]) => ({ name, until }))
		.sort((a, b) => new Date(b.until).getTime() - new Date(a.until).getTime())
		.slice(0, 5);
}
```

with:

```ts
export function recentList(card: Card): { name: string; until: string; platformId: string }[] {
	const onlineNames = new Set(card.online.map((p) => p.name));
	const newestByName = new Map<string, { until: string; platformId: string }>();
	for (const r of card.recent) {
		if (onlineNames.has(r.name)) continue;
		const prev = newestByName.get(r.name);
		if (!prev || new Date(r.until).getTime() > new Date(prev.until).getTime()) {
			newestByName.set(r.name, { until: r.until, platformId: r.platformId });
		}
	}
	return Array.from(newestByName, ([name, r]) => ({ name, ...r }))
		.sort((a, b) => new Date(b.until).getTime() - new Date(a.until).getTime())
		.slice(0, 5);
}
```

- [ ] **Step 6: Run the unit tests to make sure they pass**

Run: `cd web && npx vitest run`

Expected: every test file passes.

- [ ] **Step 7: The profile components**

Create `web/src/lib/components/ProfileContent.svelte`:

```svelte
<!--
  A player's profile (Plan 7; design `:116-185` desktop, `:636-745`
  mobile), shared by ProfilePanel (desktop) and ProfileSheet (mobile):
  avatar initial with the online dot, name and status; three stat tiles;
  the 7-day chart (hours online per local day, today in the accent);
  first seen, last seen, beds; Bases; Portals placed; Tames they named;
  Deaths; the tracked-since note and the design's source footnote.
  Fetches the profile when `player` changes; shows a skeleton while
  loading and "not found" for a player the server doesn't know. "Map →"
  calls `onmap` with the item's position and marker id.
-->
<script lang="ts">
	import { getProfile } from '$lib/api';
	import { profileView } from '$lib/profile';
	import { app } from '$lib/state.svelte';
	import type { Profile } from '$lib/types';
	import MarkerIcon from './MarkerIcon.svelte';

	let {
		serverId,
		player,
		mobile = false,
		onmap
	}: {
		serverId: string;
		player: string;
		mobile?: boolean;
		onmap: (x: number, z: number, id: string) => void;
	} = $props();

	const uid = $props.id();
	let profile = $state<Profile | null | undefined>(undefined);
	let failed = $state(false);

	$effect(() => {
		const id = serverId;
		const p = player;
		let live = true;
		profile = undefined;
		failed = false;
		getProfile(id, p)
			.then((r) => {
				if (live) profile = r;
			})
			.catch(() => {
				if (live) failed = true;
			});
		return () => {
			live = false;
		};
	});

	const v = $derived(profile ? profileView(profile, app.now) : undefined);
</script>

<div class="profile" class:mobile aria-busy={profile === undefined && !failed}>
	{#if failed}
		<p class="state">Couldn’t load this profile. Try again in a moment.</p>
	{:else if profile === null}
		<p class="state">This player hasn’t been seen on this server.</p>
	{:else if !profile || !v}
		<div class="skeleton" data-testid="profile-skeleton">
			<span class="sk disc"></span>
			<span class="sk line"></span>
			<span class="sk block"></span>
			<span class="sk block tall"></span>
		</div>
	{:else}
		<div class="who">
			<span class="avatar" aria-hidden="true">{v.initial}<span class="dot" class:on={profile.online}></span></span>
			<div class="who-text">
				<h2 class="name" id="{uid}-name">{profile.name}</h2>
				<div class="status" class:on={profile.online}>{v.status}</div>
			</div>
		</div>

		<dl class="stats">
			{#each v.stats as s (s.k)}
				<div class="stat"><dt>{s.k}</dt><dd>{s.v}</dd></div>
			{/each}
		</dl>

		<section class="chart" aria-labelledby="{uid}-week">
			<div class="row-head"><h3 id="{uid}-week">Last 7 days</h3><span class="aside">hours online</span></div>
			<ol class="bars" aria-label="Hours online per day">
				{#each v.days as d, i (i)}
					<li class="bar-col" aria-label="{d.d}: {d.label || '0'} h">
						<span class="bar-label">{d.label}</span>
						<span class="bar" class:today={d.today} style:height="{d.pct}%"></span>
					</li>
				{/each}
			</ol>
			<div class="bar-days" aria-hidden="true">
				{#each v.days as d, i (i)}<span class:today={d.today}>{d.d}</span>{/each}
			</div>
		</section>

		<dl class="facts">
			<dt>First seen</dt><dd>{v.first}</dd>
			<dt>Last seen</dt><dd>{v.last}</dd>
			<dt>Beds</dt><dd>{v.beds}</dd>
		</dl>
		<p class="note">{v.tracked}</p>

		<section aria-labelledby="{uid}-bases">
			<h3 id="{uid}-bases">Bases</h3>
			{#each v.bases as b (b.id)}
				<button class="item" type="button" onclick={() => onmap(b.x, b.z, b.id)}>
					<span class="base-disc" aria-hidden="true"><MarkerIcon name="home" size={15} color="#26231f" /></span>
					<span class="item-text"><b>{b.name}</b><span class="sub">{b.sub}</span></span>
					<span class="map">Map →</span>
				</button>
			{:else}
				<p class="none">No bases yet.</p>
			{/each}
		</section>

		<section aria-labelledby="{uid}-portals">
			<div class="row-head"><h3 id="{uid}-portals">Portals placed</h3><span class="aside">{v.portalCount}</span></div>
			{#if profile.portals.length}
				<div class="chips">
					{#each profile.portals as p (p.id)}
						<button class="portal" class:unpaired={!p.paired} type="button" onclick={() => onmap(p.x, p.z, p.id)}>
							<span class="portal-disc" aria-hidden="true"><MarkerIcon name="portal" size={11} color="#f5ead8" /></span>{p.tag || 'untagged'}
						</button>
					{/each}
				</div>
			{:else}
				<p class="none">None yet.</p>
			{/if}
		</section>

		<section aria-labelledby="{uid}-tames">
			<div class="row-head"><h3 id="{uid}-tames">Tames they named</h3><span class="aside">{v.tameCount}</span></div>
			{#if profile.tames.length}
				<div class="chips">
					{#each profile.tames as t (t.id)}<span class="tag tag-accent-2">{t.name} · {t.species}</span>{/each}
				</div>
			{:else}
				<p class="none">None yet.</p>
			{/if}
		</section>

		<section aria-labelledby="{uid}-deaths">
			<div class="row-head"><h3 id="{uid}-deaths">Deaths</h3><span class="aside">{v.deathLine}</span></div>
			{#each v.tombs as t (t.id)}
				<button class="item tomb" type="button" onclick={() => onmap(t.x, t.z, t.id)}>
					<span class="tomb-disc" aria-hidden="true"><MarkerIcon name="skull" size={14} color="#f5ead8" /></span>
					<span class="item-text tomb-text">{t.text}</span>
					<span class="map">Map →</span>
				</button>
			{/each}
		</section>

		<p class="foot">
			Playtime comes from the server log. Bases, beds, portals and tames come from who placed or named them in the world
			save. Deaths count tombstones seen in saves, so a death recovered between two saves is missed.
		</p>
	{/if}
</div>

<style>
	.profile {
		display: flex;
		flex-direction: column;
		gap: 18px;
		padding: 4px 18px 20px;
	}
	.profile.mobile {
		padding: 4px 16px var(--sheet-bottom, 20px);
	}
	.state {
		margin: 24px 0;
		font-size: 14px;
		color: var(--muted);
	}
	.skeleton {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.sk {
		display: block;
		border-radius: 18px;
		background: color-mix(in srgb, var(--color-text) 8%, transparent);
	}
	.sk.disc {
		width: 60px;
		height: 60px;
		border-radius: 50%;
	}
	.sk.line {
		width: 60%;
		height: 22px;
	}
	.sk.block {
		height: 64px;
	}
	.sk.tall {
		height: 120px;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 14px;
	}
	.avatar {
		position: relative;
		width: 60px;
		height: 60px;
		flex: none;
		border-radius: 50%;
		background: var(--color-accent-200);
		color: var(--color-accent-800);
		display: grid;
		place-items: center;
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 26px;
	}
	.dot {
		position: absolute;
		right: 0;
		bottom: 0;
		width: 15px;
		height: 15px;
		box-sizing: border-box;
		border-radius: 50%;
		background: var(--color-neutral-400);
		border: 3px solid var(--color-surface);
	}
	.dot.on {
		background: var(--color-accent);
	}
	.who-text {
		flex: 1;
		min-width: 0;
	}
	.name {
		margin: 0;
		font-size: 26px;
		line-height: 1.1;
		letter-spacing: normal;
		overflow-wrap: anywhere;
	}
	.status {
		font-size: 13px;
		font-weight: 700;
		color: var(--muted);
	}
	.status.on {
		color: var(--color-accent-700);
	}
	.stats {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 8px;
		margin: 0;
	}
	.stat {
		padding: 10px;
		min-width: 0;
		border-radius: 18px;
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
	}
	.stat dt {
		font-size: 11.5px;
		color: var(--muted);
	}
	.stat dd {
		margin: 0;
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 17px;
		line-height: 1.15;
		white-space: nowrap;
	}
	section {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.chart {
		gap: 8px;
	}
	.row-head {
		display: flex;
		justify-content: space-between;
		align-items: baseline;
		gap: 8px;
	}
	h3 {
		margin: 0;
		font-size: 16px;
		line-height: 1.3;
		letter-spacing: normal;
	}
	.aside {
		font-size: 12px;
		color: var(--muted);
	}
	.bars {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(7, minmax(0, 1fr));
		gap: 6px;
		align-items: end;
		height: 92px;
	}
	.bar-col {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: flex-end;
		gap: 4px;
		height: 100%;
	}
	.bar-label {
		font-size: 10.5px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}
	.bar {
		width: 100%;
		min-height: 3px;
		border-radius: 8px;
		background: var(--color-neutral-400);
	}
	.bar.today {
		background: var(--color-accent);
	}
	.bar-days {
		display: grid;
		grid-template-columns: repeat(7, minmax(0, 1fr));
		gap: 6px;
		font-size: 11px;
		text-align: center;
		color: var(--muted);
	}
	.bar-days .today {
		font-weight: 700;
		color: var(--color-text);
	}
	.facts {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		column-gap: 14px;
		row-gap: 5px;
		margin: 0;
		font-size: 13.5px;
	}
	.facts dt {
		color: var(--muted);
	}
	.facts dd {
		margin: 0;
		font-weight: 700;
	}
	.note,
	.foot {
		margin: -8px 0 0;
		font-size: 11.5px;
		line-height: 1.45;
		color: var(--muted);
	}
	.foot {
		margin: 0;
	}
	.none {
		margin: 0;
		font-size: 13px;
		color: var(--muted);
	}
	.item {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 48px;
		padding: 6px 10px;
		border: 0;
		border-radius: 16px;
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
		color: var(--color-text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.item:hover {
		background: color-mix(in srgb, var(--color-text) 10%, transparent);
	}
	.item-text {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		font-size: 14px;
	}
	.sub {
		font-size: 12px;
		color: var(--muted);
	}
	.map {
		font-size: 12px;
		font-weight: 700;
		color: var(--cold-ink);
		white-space: nowrap;
	}
	.base-disc,
	.tomb-disc {
		width: 30px;
		height: 30px;
		flex: none;
		box-sizing: border-box;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: #f5ead8;
		border: 2px solid #26231f;
	}
	.tomb {
		min-height: 44px;
		background: var(--color-accent-100);
		color: var(--color-accent-800);
	}
	.tomb:hover {
		background: var(--color-accent-200);
	}
	.tomb-disc {
		width: 28px;
		height: 28px;
		background: #c67139;
		border-color: #f5ead8;
	}
	.tomb-text {
		font-size: 13.5px;
		font-weight: 600;
	}
	.tomb .map {
		color: inherit;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.chips .tag {
		font-size: 13px;
	}
	.portal {
		display: flex;
		align-items: center;
		gap: 6px;
		height: 32px;
		padding: 0 12px 0 6px;
		border-radius: 999px;
		border: 1.5px solid var(--color-divider);
		background: transparent;
		color: var(--color-text);
		font: inherit;
		font-size: 13px;
		font-weight: 600;
		cursor: pointer;
	}
	.mobile .portal {
		height: 40px;
	}
	.portal.unpaired {
		border-color: var(--color-accent);
	}
	.portal-disc {
		width: 20px;
		height: 20px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: #3d7eab;
	}
</style>
```

Create `web/src/lib/components/ProfilePanel.svelte`:

```svelte
<!--
  Desktop profile (Plan 7; design `:116-185`): replaces the side panel's
  content in the same 344 px box, opaque surface, with a back arrow (to the
  server card) and the "PLAYER" kicker over the scrolling ProfileContent.
-->
<script lang="ts">
	import ChevronLeft from 'lucide-svelte/icons/chevron-left';
	import ProfileContent from './ProfileContent.svelte';

	let {
		serverId,
		player,
		onback,
		onmap
	}: {
		serverId: string;
		player: string;
		onback: () => void;
		onmap: (x: number, z: number, id: string) => void;
	} = $props();
</script>

<aside class="panel" aria-label="Player profile" data-testid="profile-panel">
	<div class="head">
		<button class="btn btn-secondary btn-icon" type="button" aria-label="Back" onclick={onback}>
			<ChevronLeft size={17} strokeWidth={2.75} />
		</button>
		<span class="kicker">Player</span>
	</div>
	<div class="body">
		<ProfileContent {serverId} {player} {onmap} />
	</div>
</aside>

<style>
	.panel {
		position: absolute;
		left: 16px;
		top: 16px;
		bottom: 16px;
		width: 344px;
		display: flex;
		flex-direction: column;
		border-radius: 30px;
		background: var(--color-surface);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-lg);
		overflow: hidden;
		z-index: 550;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 14px 14px 8px;
	}
	.kicker {
		font-size: 12px;
		font-weight: 700;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.body {
		flex: 1;
		min-height: 0;
		overflow: auto;
	}
</style>
```

Create `web/src/lib/components/ProfileSheet.svelte`:

```svelte
<!--
  Mobile profile (Plan 7; design `:636-745`): a full-height surface sheet
  over the dimmed map with the grab handle (drag down to close), a 48 px
  "‹" back button and the "PLAYER" kicker, then ProfileContent at touch
  sizes. A modal dialog: focus is trapped and returns to the opener; Esc
  (the shell) closes it.
-->
<script lang="ts">
	import ChevronLeft from 'lucide-svelte/icons/chevron-left';
	import { focusTrap } from '$lib/actions/focusTrap';
	import BottomSheet from './BottomSheet.svelte';
	import ProfileContent from './ProfileContent.svelte';

	let {
		serverId,
		player,
		onclose,
		onmap
	}: {
		serverId: string;
		player: string;
		onclose: () => void;
		onmap: (x: number, z: number, id: string) => void;
	} = $props();
</script>

<BottomSheet snap="full" surface="surface" {onclose}>
	<div class="sheet" role="dialog" aria-modal="true" aria-label="Player profile" tabindex="-1" use:focusTrap={{ initial: '.back' }}>
		<div class="head" data-sheet-drag>
			<button class="back" type="button" aria-label="Back" onclick={onclose}>
				<ChevronLeft size={20} strokeWidth={2.75} />
			</button>
			<span class="kicker">Player</span>
		</div>
		<ProfileContent {serverId} {player} {onmap} mobile />
	</div>
</BottomSheet>

<style>
	.sheet {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		display: flex;
		flex-direction: column;
		outline: none;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 0 12px 4px 4px;
	}
	.back {
		width: 48px;
		height: 48px;
		display: grid;
		place-items: center;
		padding: 0;
		border: 0;
		border-radius: 50%;
		background: transparent;
		color: var(--color-text);
		cursor: pointer;
	}
	.kicker {
		font-size: 12px;
		font-weight: 700;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}
</style>
```

- [ ] **Step 8: "Profile →" on the rows**

In `web/src/lib/components/PlayersTab.svelte`, in the header comment, replace:

```svelte
  No "Profile →": rows are not clickable in the MVP. Relative times derive
  from `now`.
```

with:

```svelte
  Each row with a platform ID has "Profile →" (Plan 7), which opens the
  player's profile in place of the panel. Relative times derive from `now`.
```

In `web/src/lib/components/PlayersTab.svelte`, in the imports, replace:

```svelte
	import { fmtLastSeen, fmtSession } from '$lib/format';
	import type { Card } from '$lib/types';
```

with:

```svelte
	import { fmtLastSeen, fmtSession } from '$lib/format';
	import { app } from '$lib/state.svelte';
	import type { Card } from '$lib/types';
```

In `web/src/lib/components/PlayersTab.svelte`, in the online row, replace:

```svelte
						<div class="sub">online {fmtSession(sessionSeconds(p.since, now))}</div>
					</div>
				</li>
```

with:

```svelte
						<div class="sub">online {fmtSession(sessionSeconds(p.since, now))}</div>
					</div>
					{#if p.platformId}
						<button class="btn btn-ghost profile" type="button" aria-label="Profile of {p.name}" onclick={() => app.openView({ kind: 'profile', player: p.platformId })}>Profile →</button>
					{/if}
				</li>
```

In `web/src/lib/components/PlayersTab.svelte`, in the recent row, replace:

```svelte
						<div class="sub">{fmtLastSeen(r.until, now)}</div>
					</div>
				</li>
```

with:

```svelte
						<div class="sub">{fmtLastSeen(r.until, now)}</div>
					</div>
					{#if r.platformId}
						<button class="btn btn-ghost profile" type="button" aria-label="Profile of {r.name}" onclick={() => app.openView({ kind: 'profile', player: r.platformId })}>Profile →</button>
					{/if}
				</li>
```

In `web/src/lib/components/PlayersTab.svelte`, in the styles (the design's ghost button in the body font, `--cold-ink`), replace:

```svelte
	.label {
		font-size: 12px;
```

with:

```svelte
	.profile {
		flex: none;
		font-family: var(--font-body);
		font-size: 12px;
		font-weight: 700;
		color: var(--cold-ink);
		white-space: nowrap;
	}
	.label {
		font-size: 12px;
```

In `web/src/lib/components/PeekSheet.svelte`, in the header comment, replace:

```svelte
    recent-activity rows. No "Full timeline →" (§1.5) and no per-player
```

with:

```svelte
    recent-activity rows. Rows have "Profile →" (Plan 7). No per-player
```

In `web/src/lib/components/PeekSheet.svelte`, in the imports, replace:

```svelte
	import { fmtLastSeen, fmtSession } from '$lib/format';
	import type { Snap } from '$lib/mobile';
```

with:

```svelte
	import { fmtLastSeen, fmtSession } from '$lib/format';
	import type { Snap } from '$lib/mobile';
	import { app } from '$lib/state.svelte';
```

In `web/src/lib/components/PeekSheet.svelte`, in the pulled online row, replace:

```svelte
										<div class="row-sub">online {fmtSession(sessionSeconds(p.since, now))}</div>
									</div>
								</li>
```

with:

```svelte
										<div class="row-sub">online {fmtSession(sessionSeconds(p.since, now))}</div>
									</div>
									{#if p.platformId}
										<button class="profile" type="button" aria-label="Profile of {p.name}" onclick={() => app.openView({ kind: 'profile', player: p.platformId })}>Profile →</button>
									{/if}
								</li>
```

In `web/src/lib/components/PeekSheet.svelte`, in the pulled recent row, replace:

```svelte
										<div class="row-sub">{fmtLastSeen(r.until, now)}</div>
									</div>
								</li>
```

with:

```svelte
										<div class="row-sub">{fmtLastSeen(r.until, now)}</div>
									</div>
									{#if r.platformId}
										<button class="profile" type="button" aria-label="Profile of {r.name}" onclick={() => app.openView({ kind: 'profile', player: r.platformId })}>Profile →</button>
									{/if}
								</li>
```

In `web/src/lib/components/PeekSheet.svelte`, at the top of the styles (a 44 px touch target), replace:

```svelte
<style>
	.players {
```

with:

```svelte
<style>
	.profile {
		flex: none;
		min-height: 44px;
		padding: 0 8px;
		border: 0;
		border-radius: 999px;
		background: transparent;
		font: inherit;
		font-size: 13px;
		font-weight: 700;
		color: var(--cold-ink);
		white-space: nowrap;
		cursor: pointer;
	}
	.players {
```

- [ ] **Step 9: Show the profile in the shells**

In `web/src/lib/components/DesktopShell.svelte`, in the imports, replace:

```svelte
	import MarkerLayer from './MarkerLayer.svelte';
	import ScaleReadout from './ScaleReadout.svelte';
```

with:

```svelte
	import MarkerLayer from './MarkerLayer.svelte';
	import ProfilePanel from './ProfilePanel.svelte';
	import ScaleReadout from './ScaleReadout.svelte';
```

In `web/src/lib/components/DesktopShell.svelte`, replace:

```svelte
	const padLeft = $derived(panelOpen ? PANEL_W : 0);
```

with:

```svelte
	/** A profile (Plan 7) takes the panel's place, open or collapsed. */
	const profilePlayer = $derived(app.view?.kind === 'profile' ? app.view.player : undefined);
	const padLeft = $derived(panelOpen || profilePlayer !== undefined ? PANEL_W : 0);
```

In `web/src/lib/components/DesktopShell.svelte`, replace:

```svelte
	const pillL = $derived(panelOpen ? PANEL_W : 190);
```

with:

```svelte
	const pillL = $derived(padLeft > 0 ? padLeft : 190);
```

In `web/src/lib/components/DesktopShell.svelte`, before `pickResult`, replace:

```svelte
	/** Search pick (§5.2): centre on the marker at zoom 4.25 and select it. */
```

with:

```svelte
	/** A profile's "Map →": centre on the item at zoom 4, selecting its marker when it's on the map. */
	function mapTo(x: number, z: number, id: string): void {
		atlas?.centerOn(x, z, 4);
		if (all.some((m) => m.id === id)) select(id);
	}

	/** Search pick (§5.2): centre on the marker at zoom 4.25 and select it. */
```

In `web/src/lib/components/DesktopShell.svelte`, at the end of `onkeydown` (Esc closes the view last), replace:

```svelte
		if (selectedId !== undefined) select(undefined);
	}
```

with:

```svelte
		if (selectedId !== undefined) {
			select(undefined);
			return;
		}
		if (app.view) app.closeView();
	}
```

In `web/src/lib/components/DesktopShell.svelte`, in the markup, replace:

```svelte
	{#if panelOpen}
		<SidePanel bind:tab oncollapse={() => (panelOpen = false)} onjoin={openJoin} onshowaltar={showAltar} />
```

with:

```svelte
	{#if profilePlayer !== undefined && app.currentId}
		<ProfilePanel serverId={app.currentId} player={profilePlayer} onback={() => app.closeView()} onmap={mapTo} />
	{:else if panelOpen}
		<SidePanel bind:tab oncollapse={() => (panelOpen = false)} onjoin={openJoin} onshowaltar={showAltar} />
```

In `web/src/lib/components/DesktopShell.svelte`, replace:

```svelte
	<ScaleReadout {map} {mask} left={panelOpen ? PANEL_W : 16} />
```

with:

```svelte
	<ScaleReadout {map} {mask} left={padLeft > 0 ? padLeft : 16} />
```

In `web/src/lib/components/MobileShell.svelte`, in the imports, replace:

```svelte
	import PeekSheet from './PeekSheet.svelte';
```

with:

```svelte
	import PeekSheet from './PeekSheet.svelte';
	import ProfileSheet from './ProfileSheet.svelte';
```

In `web/src/lib/components/MobileShell.svelte`, replace:

```svelte
	const dim = $derived(mobileDim(snap, overlay));
```

with:

```svelte
	/** A profile (Plan 7) opens as a full-height sheet over everything. */
	const profilePlayer = $derived(app.view?.kind === 'profile' ? app.view.player : undefined);
	const dim = $derived(app.view ? 0.55 : mobileDim(snap, overlay));
```

In `web/src/lib/components/MobileShell.svelte`, replace:

```svelte
	const zoomShown = $derived(markersOn && snap === 'peek' && overlay === 'none' && !cardShown);
```

with:

```svelte
	const zoomShown = $derived(markersOn && snap === 'peek' && overlay === 'none' && !cardShown && !app.view);
```

In `web/src/lib/components/MobileShell.svelte`, before `pickResult`, replace:

```svelte
	/** Search pick (§5.2, Mobile ruling): close the menu, select the marker at zoom 4.25. */
```

with:

```svelte
	/** A profile's "Map →": close the sheet, then centre on the item (selecting its marker when on the map). */
	function mapTo(x: number, z: number, id: string): void {
		app.closeView();
		snap = 'peek';
		const m = all.find((mm) => mm.id === id);
		if (m) {
			focusMarker(m, 4);
		} else if (map) {
			atlas?.centerOn(x, z, 4, centerDy(map.getSize().y));
		}
	}

	/** Search pick (§5.2, Mobile ruling): close the menu, select the marker at zoom 4.25. */
```

In `web/src/lib/components/MobileShell.svelte`, in `onkeydown` (Esc closes the view first), replace:

```svelte
		if (e.key !== 'Escape' || e.defaultPrevented || app.unlockPrompt) return;
		if (overlay !== 'none') {
```

with:

```svelte
		if (e.key !== 'Escape' || e.defaultPrevented || app.unlockPrompt) return;
		if (app.view) {
			e.preventDefault();
			app.closeView();
		} else if (overlay !== 'none') {
```

In `web/src/lib/components/MobileShell.svelte`, at the end of the markup, replace:

```svelte
	{:else if overlay === 'join' && card}
		<JoinSheet {card} onclose={() => closeOverlay()} oncopy={toast} />
	{/if}
```

with:

```svelte
	{:else if overlay === 'join' && card}
		<JoinSheet {card} onclose={() => closeOverlay()} oncopy={toast} />
	{/if}

	{#if profilePlayer !== undefined && app.currentId}
		<ProfileSheet serverId={app.currentId} player={profilePlayer} onclose={() => app.closeView()} onmap={mapTo} />
	{/if}
```

- [ ] **Step 10: Check types and run the unit tests**

Run: `cd web && npm run check && npx vitest run`

Expected: svelte-check: 0 errors, 0 warnings; every Vitest file passes.

- [ ] **Step 11: Run the end-to-end suite (nothing seeded for profiles yet; nothing may regress)**

Run: `cd web && PATH=$HOME/.local/go/bin:$PATH LD_LIBRARY_PATH="$(../hack/playwright-libs.sh --print)" FONTCONFIG_FILE="$(../hack/playwright-libs.sh --fontconfig)" npx playwright test`

Expected: `42 passed`.

- [ ] **Step 12: Commit**

```bash
cd /workspace/Farsight
git add web/src/lib/types.ts \
  web/src/lib/api.ts \
  web/src/lib/api.test.ts \
  web/src/lib/share.ts \
  web/src/lib/share.test.ts \
  web/src/lib/state.svelte.ts \
  web/src/lib/state.svelte.test.ts \
  web/src/lib/zoned.ts \
  web/src/lib/zoned.test.ts \
  web/src/lib/profile.ts \
  web/src/lib/profile.test.ts \
  web/src/lib/derive.ts \
  web/src/lib/derive.test.ts \
  web/src/lib/components/ProfileContent.svelte \
  web/src/lib/components/ProfilePanel.svelte \
  web/src/lib/components/ProfileSheet.svelte \
  web/src/lib/components/PlayersTab.svelte \
  web/src/lib/components/PeekSheet.svelte \
  web/src/lib/components/DesktopShell.svelte \
  web/src/lib/components/MobileShell.svelte
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(web): player profiles: desktop panel, mobile sheet, #p= links

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
git status --short   # must print nothing
```

---

### Task 8: Web: the Activity timeline (desktop panel, mobile sheet, `#activity`, chips, people, "Show earlier")

"Full timeline →" in the desktop side panel's Recent activity (`btn btn-ghost` in Caprasimo, as in the design), and a "Full timeline" row in the mobile menu, open the Activity view.

- **Desktop:** a 520 px opaque aside in the panel's place, with "Reset filters".
- **Mobile:** a full-height sheet.
- **URL:** `#s=<server>&activity`, through the same `openView`/`closeView` as the profile.

Content (design `:837-908`), top to bottom:
1. **Who was on today:** a row per player on a 00–24 axis in the server's zone. The current session is orange, earlier ones grey, with a "Now · HH:MM" line and the legend.
2. **Show:** eight category chips with counts, each a toggle (`aria-pressed`).
3. **People:** "Everyone" plus a chip per player, multi-select.
4. **The list,** grouped by local day ("Today · Mon 5 Oct · in-game day 214", "Yesterday · …", then "Sat 3 Oct"). Each entry has its time, icon disc, text and source line: "Server log", or "World save · 14:20" with "Show on map →" when the event has a position.
5. **The end:** "Show earlier" (three more days) or "Tracking began 30 Sep", "Nothing matches these filters." when filters hide everything, and the "Where each entry comes from" footnote.

Filters: the people filter keeps server-wide events (no `who`); the chip counts follow the people filter, not the chips. The filters are shared (`filters.svelte.ts`): the side panel's short list follows them too, and its world-save rows show "save HH:MM".

Text per event type (all in `timeline.ts`):

| type | text |
|---|---|
| `player_join` / `player_leave` | "Ragnar joined" / "Bjørn left after 3h 05m" ("Bjørn left" without seconds) |
| `server_starting`, `server_boot` / `server_ready` / `server_stopped` | "Server starting" / "Server is up" / "Server stopped" |
| `world_saved` / `join_code` | "Autosave finished · map updated" / "New join code 252 289" |
| `event_raid` | "Raid: The forest is moving" (unmapped names as they are) |
| `world_tombstone` | "New tombstone: Ragnar, near a sunken crypt in the Swamp" / "…, in the Mistlands" |
| `world_portal` / `world_portal_paired` | "New portal “copper”, not paired with anything yet" / "… paired with “copper”"; "Portal “copper” now paired with “copper”" |
| `world_tame` | "New tame: Big Mama (Lox)" |
| `world_base_new` / `world_base_grew` | "New base: Lox Ranch (Plains)" / "Lox Ranch grew by 120 pieces" |
| `world_boss` | "Moder defeated" |

**Files:**
- Modify: `web/src/lib/types.ts` (`Category`, `Activity` reshaped, `ActivityPage`, `TodaySessions`), `web/src/lib/api.ts` (`getActivity`, `getSessionsToday`)
- Create: `web/src/lib/timeline.ts`, `web/src/lib/filters.svelte.ts`
- Modify: `web/src/lib/derive.ts` (`activityRows` on the timeline model, with filters), `web/src/lib/markers.ts` (`markerAt`)
- Create: `web/src/lib/components/ActivityIcon.svelte`, `ActivityContent.svelte`, `ActivityPanel.svelte`, `ActivitySheet.svelte`
- Modify: `web/src/lib/components/ActivityList.svelte` (replaced), `PlayersTab.svelte`, `MenuSheet.svelte`, `DesktopShell.svelte`, `MobileShell.svelte`
- Test: `web/src/lib/timeline.test.ts`, `filters.svelte.test.ts`, `derive.test.ts`, `markers.test.ts`, `api.test.ts`

**Interfaces:**
- Consumes: the activity and today JSON and `eventJSON` (Task 6); `View`, `app.openView`/`closeView` (Task 7); `zoned.ts` (Task 7); `MarkerIcon` (existing).
- Produces:
  - TS `Category`, `Activity` (= Task 6's `eventJSON`), `ActivityPage`, `TodaySessions`.
  - `getActivity(id, before?, f?, timeoutMs?): Promise<ActivityPage>`, `getSessionsToday(id, f?, timeoutMs?): Promise<TodaySessions>`.
  - `timeline.ts`: `CATEGORIES`, `RAID_MESSAGES`, `ActivityIcon`, `Tone`, `leftAfter`, `place`, `eventText`, `eventIcon`, `eventTone`, `sourceText`, `passes(e, off, people)`, `chipCounts(events, people)`, `groupByDay(events, tz, now, gameDay?)`, `todayRows(t, now, people?)`.
  - `filters` (`ActivityFilters`: `off`, `people`, `toggle`, `togglePerson`, `everyone`, `reset`, `active`).
  - `activityRows(card, limit, off?, people?)` rows: `{icon, tone, text, at, source}`.
  - `markerAt(all, x, z, radius = 5): MapMarker | undefined`.
  - `ActivityList` prop `full` (shows "Full timeline →"); `MenuSheet` prop `ontimeline`.

- [ ] **Step 1: Write the failing unit tests**

Create `web/src/lib/timeline.test.ts`:

```ts
import { describe, expect, test } from 'vitest';
import { chipCounts, eventIcon, eventText, eventTone, groupByDay, leftAfter, passes, place, sourceText, todayRows } from './timeline';
import type { Activity, TodaySessions } from './types';

const CATEGORY: Record<string, Activity['category']> = {
	player_join: 'session',
	player_leave: 'session',
	event_raid: 'event',
	world_tombstone: 'death',
	world_portal: 'portal',
	world_portal_paired: 'portal',
	world_tame: 'tame',
	world_base_new: 'build',
	world_base_grew: 'build',
	world_boss: 'boss'
};

function ev(type: string, at: string, extra: Partial<Activity> = {}): Activity {
	const save = type.startsWith('world_') && type !== 'world_saved';
	return { id: `${type}@${at}`, type, category: CATEGORY[type] ?? 'server', source: save ? 'save' : 'log', at, who: [], ...extra };
}

describe('eventText', () => {
	test('server log events', () => {
		expect(eventText(ev('player_join', 'x', { name: 'Ragnar' }))).toBe('Ragnar joined');
		expect(eventText(ev('player_leave', 'x', { name: 'Bjørn', seconds: 3 * 3600 + 5 * 60 }))).toBe('Bjørn left after 3h 05m');
		expect(eventText(ev('player_leave', 'x', { name: 'Bjørn' }))).toBe('Bjørn left');
		expect(eventText(ev('server_starting', 'x'))).toBe('Server starting');
		expect(eventText(ev('server_ready', 'x'))).toBe('Server is up');
		expect(eventText(ev('world_saved', 'x'))).toBe('Autosave finished · map updated');
		expect(eventText(ev('join_code', 'x', { code: '252289' }))).toBe('New join code 252 289');
		expect(eventText(ev('event_raid', 'x', { raid: 'army_theelder' }))).toBe('Raid: The forest is moving');
		expect(eventText(ev('event_raid', 'x', { raid: 'army_gjall' }))).toBe('Raid: army_gjall');
	});
	test('world save events', () => {
		expect(eventText(ev('world_tombstone', 'x', { owner: 'Ragnar', near: 'a sunken crypt', biome: 'Swamp' }))).toBe(
			'New tombstone: Ragnar, near a sunken crypt in the Swamp'
		);
		expect(eventText(ev('world_tombstone', 'x', { owner: 'Alina', biome: 'Mistlands' }))).toBe('New tombstone: Alina, in the Mistlands');
		expect(eventText(ev('world_portal', 'x', { tag: 'copper' }))).toBe('New portal “copper”, not paired with anything yet');
		expect(eventText(ev('world_portal', 'x', { tag: 'copper', paired: true }))).toBe('New portal “copper”, paired with “copper”');
		expect(eventText(ev('world_portal_paired', 'x', { tag: 'copper' }))).toBe('Portal “copper” now paired with “copper”');
		expect(eventText(ev('world_tame', 'x', { name: 'Big Mama', species: 'Lox' }))).toBe('New tame: Big Mama (Lox)');
		expect(eventText(ev('world_base_new', 'x', { name: 'Lox Ranch', biome: 'Plains' }))).toBe('New base: Lox Ranch (Plains)');
		expect(eventText(ev('world_base_grew', 'x', { name: 'Lox Ranch', grew: 120 }))).toBe('Lox Ranch grew by 120 pieces');
		expect(eventText(ev('world_boss', 'x', { boss: 'Moder' }))).toBe('Moder defeated');
	});
	test('helpers', () => {
		expect(leftAfter(40 * 60)).toBe('40m');
		expect(place({})).toBe('');
		expect(place({ near: 'Haldor' })).toBe('near Haldor');
	});
});

describe('icons, tones and sources', () => {
	test('per event', () => {
		expect(eventIcon(ev('player_leave', 'x'))).toBe('log-out');
		expect(eventIcon(ev('world_saved', 'x'))).toBe('map');
		expect(eventIcon(ev('world_tame', 'x'))).toBe('paw-print');
		expect(eventIcon(ev('event_raid', 'x'))).toBe('alert');
		expect(eventTone(ev('player_join', 'x'))).toBe('ember');
		expect(eventTone(ev('player_leave', 'x'))).toBe('neutral');
		expect(eventTone(ev('world_boss', 'x'))).toBe('sage');
		expect(eventTone(ev('world_portal', 'x'))).toBe('cold');
		expect(sourceText(ev('world_boss', '2026-10-05T12:20:00Z'), 'Europe/Oslo')).toBe('World save · 14:20');
		expect(sourceText(ev('player_join', '2026-10-05T12:20:00Z'), 'Europe/Oslo')).toBe('Server log');
	});
});

describe('filters', () => {
	const join = ev('player_join', 'x', { who: ['111'] });
	const tomb = ev('world_tombstone', 'x', { who: ['222'] });
	const save = ev('world_saved', 'x');
	test('categories off hide; chosen people keep their events and the server’s', () => {
		expect(passes(join, ['session'], [])).toBe(false);
		expect(passes(join, [], ['222'])).toBe(false);
		expect(passes(tomb, [], ['222'])).toBe(true);
		expect(passes(save, [], ['222'])).toBe(true);
		expect(passes(save, ['server'], [])).toBe(false);
	});
	test('chip counts follow the people filter, not the chips', () => {
		const counts = chipCounts([join, tomb, save], ['111']);
		expect(counts.session).toBe(1);
		expect(counts.death).toBe(0);
		expect(counts.server).toBe(1);
		expect(Object.keys(counts)).toEqual(['session', 'death', 'boss', 'build', 'portal', 'tame', 'event', 'server']);
	});
});

describe('groupByDay', () => {
	test('today, yesterday and older, in the server’s zone', () => {
		const now = new Date('2026-09-29T12:44:00Z'); // 14:44 in Oslo
		const groups = groupByDay(
			[
				ev('player_join', '2026-09-29T12:39:00Z'),
				ev('player_leave', '2026-09-28T22:30:00Z'), // 00:30 on the 29th in Oslo
				ev('world_boss', '2026-09-28T20:50:00Z'),
				ev('server_ready', '2026-09-27T14:20:00Z')
			],
			'Europe/Oslo',
			now,
			214
		);
		expect(groups.map((g) => [g.label, g.sub, g.items.length])).toEqual([
			['Today', 'Tue 29 Sep · in-game day 214', 2],
			['Yesterday', 'Mon 28 Sep', 1],
			['Sun 27 Sep', '', 1]
		]);
	});
});

describe('todayRows', () => {
	const t: TodaySessions = {
		timeZone: 'Europe/Oslo',
		dayStart: '2026-09-28T22:00:00Z',
		dayEnd: '2026-09-29T22:00:00Z',
		now: '2026-09-29T12:44:00Z',
		players: [
			{ id: '1', name: 'Johnny', online: true, spans: [{ since: '2026-09-29T06:10:00Z', until: '2026-09-29T07:40:00Z' }, { since: '2026-09-29T11:32:00Z' }] },
			{ id: '2', name: 'Alina', online: false, spans: [{ since: '2026-09-28T22:00:00Z', until: '2026-09-28T23:30:00Z' }] }
		]
	};
	test('bars on the 00–24 axis; the open one runs to now', () => {
		const { rows, nowPct, nowClock } = todayRows(t, new Date('2026-09-29T12:44:00Z'));
		expect(nowClock).toBe('14:44');
		expect(nowPct).toBeCloseTo((14 + 44 / 60) / 24 * 100, 5);
		const [first, open] = rows[0].bars;
		expect(first.left).toBeCloseTo((8 + 10 / 60) / 24 * 100, 5);
		expect(first.width).toBeCloseTo(1.5 / 24 * 100, 5);
		expect(first.live).toBe(false);
		expect(first.title).toBe('Johnny · 08:10–09:40');
		expect(open.live).toBe(true);
		expect(open.title).toBe('Johnny · 13:32–now');
		expect(open.left + open.width).toBeCloseTo(nowPct, 5);
		expect(rows[1].bars[0].left).toBe(0);
	});
	test('the people filter applies', () => {
		expect(todayRows(t, new Date('2026-09-29T12:44:00Z'), ['2']).rows.map((r) => r.name)).toEqual(['Alina']);
	});
});
```

Create `web/src/lib/filters.svelte.test.ts`:

```ts
import { describe, expect, test } from 'vitest';
import { ActivityFilters } from './filters.svelte';

describe('ActivityFilters', () => {
	test('toggles categories and people; reset clears both', () => {
		const f = new ActivityFilters();
		expect(f.active).toBe(false);
		f.toggle('server');
		f.toggle('death');
		f.toggle('server');
		expect(f.off).toEqual(['death']);
		f.togglePerson('111');
		f.togglePerson('222');
		f.togglePerson('111');
		expect(f.people).toEqual(['222']);
		expect(f.active).toBe(true);
		f.everyone();
		expect(f.people).toEqual([]);
		f.reset();
		expect(f.off).toEqual([]);
		expect(f.active).toBe(false);
	});
});
```

In `web/src/lib/derive.test.ts`, the whole `activityRows` block (entries now carry `id`, `category`, `source` and `who`), replace:

```ts
describe('activityRows', () => {
	test('keeps only the newest world_saved, maps copy, applies the limit', () => {
		const activity: Activity[] = [
			{ type: 'player_join', at: iso(-10), name: 'Halvor' },
			{ type: 'world_saved', at: iso(-20) },
			{ type: 'world_saved', at: iso(-40) },
			{ type: 'player_leave', at: iso(-60), name: 'Bjorn' },
			{ type: 'server_ready', at: iso(-80), version: '1.0.16' },
			{ type: 'server_stopped', at: iso(-100) },
			{ type: 'server_starting', at: iso(-120) },
			{ type: 'join_code', at: iso(-140), code: '318742' }
		];
		const card = makeCard({ activity });
		const rows = activityRows(card, 8);
		expect(rows.filter((r) => r.text.includes('map updated')).length).toBe(1);
		expect(rows.find((r) => r.text === 'Halvor joined')?.icon).toBe('log-in');
		expect(rows.find((r) => r.text === 'Halvor joined')?.tone).toBe('ember');
		expect(rows.find((r) => r.text === 'Bjorn left')?.icon).toBe('log-out');
		expect(rows.find((r) => r.text === 'Server is up · version 1.0.16')).toBeTruthy();
		expect(rows.find((r) => r.text === 'Server stopped')).toBeTruthy();
		expect(rows.find((r) => r.text === 'Server starting')).toBeTruthy();
		expect(rows.find((r) => r.text === 'New join code 318 742')).toBeTruthy();
	});

	test('applies the limit', () => {
		const activity: Activity[] = Array.from({ length: 10 }, (_, i) => ({
			type: 'player_join',
			at: iso(-i * 10),
			name: `P${i}`
		}));
		const card = makeCard({ activity });
		expect(activityRows(card, 3).length).toBe(3);
	});

	test('server_ready without a version', () => {
		const card = makeCard({ activity: [{ type: 'server_ready', at: iso(-5) }] });
		expect(activityRows(card, 8)[0].text).toBe('Server is up');
	});
});

```

with:

```ts
describe('activityRows', () => {
	function act(type: string, at: string, extra: Partial<Activity> = {}): Activity {
		const category = type.startsWith('player_') ? 'session' : type === 'world_tombstone' ? 'death' : 'server';
		const source = type.startsWith('world_') && type !== 'world_saved' ? 'save' : 'log';
		return { id: `${type}${at}`, type, category, source, at, who: [], ...extra };
	}

	test('keeps only the newest world_saved, maps copy, applies the limit', () => {
		const activity: Activity[] = [
			act('player_join', iso(-10), { name: 'Halvor' }),
			act('world_saved', iso(-20)),
			act('world_saved', iso(-40)),
			act('player_leave', iso(-60), { name: 'Bjorn' }),
			act('server_ready', iso(-80), { version: '1.0.16' }),
			act('server_stopped', iso(-100)),
			act('server_starting', iso(-120)),
			act('join_code', iso(-140), { code: '318742' })
		];
		const card = makeCard({ activity });
		const rows = activityRows(card, 8);
		expect(rows.filter((r) => r.text.includes('map updated')).length).toBe(1);
		expect(rows.find((r) => r.text === 'Halvor joined')?.icon).toBe('log-in');
		expect(rows.find((r) => r.text === 'Halvor joined')?.tone).toBe('ember');
		expect(rows.find((r) => r.text === 'Bjorn left')?.icon).toBe('log-out');
		expect(rows.find((r) => r.text === 'Server is up · version 1.0.16')).toBeTruthy();
		expect(rows.find((r) => r.text === 'Server stopped')).toBeTruthy();
		expect(rows.find((r) => r.text === 'Server starting')).toBeTruthy();
		expect(rows.find((r) => r.text === 'New join code 318 742')).toBeTruthy();
	});

	test('applies the limit', () => {
		const activity: Activity[] = Array.from({ length: 10 }, (_, i) => act('player_join', iso(-i * 10), { name: `P${i}` }));
		const card = makeCard({ activity });
		expect(activityRows(card, 3).length).toBe(3);
	});

	test('server_ready without a version', () => {
		const card = makeCard({ activity: [act('server_ready', iso(-5))] });
		expect(activityRows(card, 8)[0].text).toBe('Server is up');
	});

	test('world events, and the timeline’s filters (Plan 7)', () => {
		const activity: Activity[] = [
			act('world_tombstone', iso(-5), { owner: 'Halvor', biome: 'Swamp', who: ['1'] }),
			act('player_join', iso(-10), { name: 'Bjorn', who: ['2'] }),
			act('world_saved', iso(-20))
		];
		const card = makeCard({ activity });
		const all = activityRows(card, 8);
		expect(all[0]).toMatchObject({ text: 'New tombstone: Halvor, in the Swamp', icon: 'skull', tone: 'ember', source: 'save' });
		expect(activityRows(card, 8, ['death']).map((r) => r.text)).toEqual(['Bjorn joined', 'Autosave finished · map updated']);
		expect(activityRows(card, 8, [], ['2']).map((r) => r.text)).toEqual(['Bjorn joined', 'Autosave finished · map updated']);
	});
});

```

In `web/src/lib/markers.test.ts`, in the imports, replace:

```ts
	layerCounts,
	pinHtml,
```

with:

```ts
	layerCounts,
	markerAt,
	pinHtml,
```

Append to the end of `web/src/lib/markers.test.ts`, after a blank line:

```ts
describe('markerAt (Plan 7)', () => {
	test('the nearest marker within the radius', () => {
		const all = build();
		expect(markerAt(all, 101, 99)?.id).toBe('portal-1');
		expect(markerAt(all, -2003, 1002)?.id).toBe('tombstone-1');
		expect(markerAt(all, 106, 100)).toBeUndefined();
		expect(markerAt(all, 106, 100, 10)?.id).toBe('portal-1');
	});
});
```

In `web/src/lib/api.test.ts`, replace:

```ts
import { ApiError, getCard, getProfile, getSnapshot, listServers, tileUrl, unlock } from './api';
```

with:

```ts
import { ApiError, getActivity, getCard, getProfile, getSessionsToday, getSnapshot, listServers, tileUrl, unlock } from './api';
```

Append to the end of `web/src/lib/api.test.ts`, after a blank line:

```ts
describe('getActivity / getSessionsToday (Plan 7)', () => {
	test('the first page has no before; an earlier page passes it', async () => {
		const fake = vi.fn(async (_url: string) => jsonResponse({ events: [] }));
		await getActivity('a', undefined, fake as unknown as typeof fetch);
		await getActivity('a', '2026-09-26T22:00:00Z', fake as unknown as typeof fetch);
		expect(fake.mock.calls[0][0]).toBe('/api/servers/a/activity');
		expect(fake.mock.calls[1][0]).toBe('/api/servers/a/activity?before=2026-09-26T22%3A00%3A00Z');
	});
	test('sessions today; errors throw', async () => {
		const fake = vi.fn().mockResolvedValue(jsonResponse({ players: [] }));
		await getSessionsToday('a', fake as unknown as typeof fetch);
		expect(fake.mock.calls[0][0]).toBe('/api/servers/a/sessions/today');
		const bad = vi.fn().mockResolvedValue(textResponse('not found', 404));
		await expect(getActivity('a', undefined, bad as unknown as typeof fetch)).rejects.toBeInstanceOf(ApiError);
	});
});
```

- [ ] **Step 2: Run them to make sure they fail**

Run: `cd web && npx vitest run src/lib/timeline.test.ts src/lib/filters.svelte.test.ts src/lib/derive.test.ts src/lib/markers.test.ts src/lib/api.test.ts`

Expected: FAIL: `timeline.ts` and `filters.svelte.ts` don't exist, `markerAt` and `getActivity` are not functions, and the world-event `activityRows` test fails.

- [ ] **Step 3: Types and the API calls**

In `web/src/lib/types.ts`, the old `Activity`, replace:

```ts
export interface Activity {
	type: string;
	at: string;
	name?: string;
	platform?: string;
	code?: string;
	players?: number;
	seconds?: number;
	version?: string;
}

```

with:

```ts
/** An activity category: the timeline's chips (Plan 7). */
export type Category = 'session' | 'death' | 'boss' | 'build' | 'portal' | 'tame' | 'event' | 'server';

/**
 * One activity entry, in the card's `activity` and the timeline alike.
 * Log events carry their exact time, world-save events (`source: 'save'`)
 * the save's time. Only the fields its type uses are set.
 */
export interface Activity {
	id: string;
	type: string;
	category: Category;
	source: 'log' | 'save';
	at: string;
	/** Platform IDs of the players it concerns (the people filter). */
	who: string[];
	/** The player (log events), or the tame or base (world events). */
	name?: string;
	platform?: string;
	platformId?: string;
	code?: string;
	players?: number;
	seconds?: number;
	version?: string;
	/** event_raid: the game's event name, e.g. "army_theelder". */
	raid?: string;
	owner?: string;
	tag?: string;
	paired?: boolean;
	species?: string;
	pieces?: number;
	grew?: number;
	boss?: string;
	biome?: string;
	/** A known location near the place: "a sunken crypt". */
	near?: string;
	/** Set only for a place in explored ground ("Show on map →"). */
	x?: number;
	z?: number;
}

```

Append to the end of `web/src/lib/types.ts`, after a blank line:

```ts
/** GET /api/servers/{id}/activity: events in [from, until), newest first. */
export interface ActivityPage {
	timeZone: string;
	from: string;
	until: string;
	/** When tracking began; absent before any event. */
	earliest?: string;
	events: Activity[];
	counts: Record<Category, number>;
	/** Every player seen: online first, then most recently seen. */
	people: { id: string; name: string; online: boolean }[];
}

/** GET /api/servers/{id}/sessions/today: today's sessions in the server's zone. */
export interface TodaySessions {
	timeZone: string;
	dayStart: string;
	dayEnd: string;
	now: string;
	players: { id: string; name: string; online: boolean; spans: { since: string; until?: string }[] }[];
}
```

In `web/src/lib/api.ts`, replace:

```ts
import type { Card, Profile, ServerSummary, SnapshotView } from './types';
```

with:

```ts
import type { ActivityPage, Card, Profile, ServerSummary, SnapshotView, TodaySessions } from './types';
```

In `web/src/lib/api.ts`, replace:

```ts
const PROFILE_TIMEOUT_MS = 20_000;
```

with:

```ts
const PROFILE_TIMEOUT_MS = 20_000;
const ACTIVITY_TIMEOUT_MS = 20_000;
```

In `web/src/lib/api.ts`, before `unlock`, replace:

```ts
export async function unlock(
```

with:

```ts
/** Three local days of activity ending at `before` (the start of an earlier page), or at the end of today. */
export async function getActivity(
	id: string,
	before?: string,
	f: typeof fetch = fetch,
	timeoutMs = ACTIVITY_TIMEOUT_MS
): Promise<ActivityPage> {
	const q = before ? `?before=${encodeURIComponent(before)}` : '';
	return withTimeout(timeoutMs, async (signal) => {
		const res = await f(`/api/servers/${encodeURIComponent(id)}/activity${q}`, jsonInit(signal));
		if (!res.ok) throw new ApiError(res.status, await res.text());
		return (await res.json()) as ActivityPage;
	});
}

export async function getSessionsToday(id: string, f: typeof fetch = fetch, timeoutMs = ACTIVITY_TIMEOUT_MS): Promise<TodaySessions> {
	return withTimeout(timeoutMs, async (signal) => {
		const res = await f(`/api/servers/${encodeURIComponent(id)}/sessions/today`, jsonInit(signal));
		if (!res.ok) throw new ApiError(res.status, await res.text());
		return (await res.json()) as TodaySessions;
	});
}

export async function unlock(
```

- [ ] **Step 4: The timeline model and the shared filters**

The raid messages are the game's start messages for the event names known to us; anything else shows its raw name, as the spec allows.

Create `web/src/lib/timeline.ts`:

```ts
// The activity timeline's model (Plan 7, design `:837-908`): categories and
// their chips, each event's text, icon, tone and source line, the people
// and category filters, day groups in the server's zone, and the "who was
// on today" bars. The side panel's short list (derive.ts activityRows)
// uses the same text, icons and filters.

import { fmtCode } from './format';
import type { Activity, Category, TodaySessions } from './types';
import { dayKey, prevDayKey, zClock, zWeekday } from './zoned';

export type ActivityIcon = 'log-in' | 'log-out' | 'power' | 'map' | 'skull' | 'flame' | 'home' | 'portal' | 'paw-print' | 'alert';
export type Tone = 'ember' | 'neutral' | 'sage' | 'cold';

/** The chips, in the design's order. */
export const CATEGORIES: { key: Category; label: string; icon: ActivityIcon }[] = [
	{ key: 'session', label: 'Joins & leaves', icon: 'log-in' },
	{ key: 'death', label: 'Deaths', icon: 'skull' },
	{ key: 'boss', label: 'Bosses', icon: 'flame' },
	{ key: 'build', label: 'Building', icon: 'home' },
	{ key: 'portal', label: 'Portals', icon: 'portal' },
	{ key: 'tame', label: 'Tames', icon: 'paw-print' },
	{ key: 'event', label: 'Raids & events', icon: 'alert' },
	{ key: 'server', label: 'Server', icon: 'power' }
];

/**
 * The game's start message for a random event, by its name in the log
 * ("Random event set:<name>"). Names not listed show as they are.
 */
export const RAID_MESSAGES: Record<string, string> = {
	army_eikthyr: 'Eikthyr rallies the creatures of the forest',
	army_theelder: 'The forest is moving',
	army_bonemass: 'A foul smell from the swamp',
	army_moder: 'A cold wind blows from the mountains',
	army_goblin: 'The horde is attacking',
	foresttrolls: 'The ground is shaking',
	skeletons: 'Skeleton surprise',
	wolves: 'You are being hunted',
	surtlings: 'There’s a smell of sulfur in the air',
	bats: 'You stirred the cauldron'
};

const pad2 = (n: number) => String(n).padStart(2, '0');

/** A session length for "left after …": "3h 05m", "40m". */
export function leftAfter(sec: number): string {
	const h = Math.floor(sec / 3600);
	const m = Math.floor((sec % 3600) / 60);
	return h > 0 ? `${h}h ${pad2(m)}m` : `${m}m`;
}

/** "near a sunken crypt in the Swamp", "in the Swamp", or "". */
export function place(e: Pick<Activity, 'near' | 'biome'>): string {
	if (!e.biome) return e.near ? `near ${e.near}` : '';
	return e.near ? `near ${e.near} in the ${e.biome}` : `in the ${e.biome}`;
}

export function eventText(e: Activity): string {
	switch (e.type) {
		case 'player_join':
			return `${e.name} joined`;
		case 'player_leave':
			return e.seconds ? `${e.name} left after ${leftAfter(e.seconds)}` : `${e.name} left`;
		case 'server_ready':
			return e.version ? `Server is up · version ${e.version}` : 'Server is up';
		case 'server_stopped':
			return 'Server stopped';
		case 'server_starting':
		case 'server_boot':
			return 'Server starting';
		case 'world_saved':
			return 'Autosave finished · map updated';
		case 'join_code':
			return `New join code ${e.code ? fmtCode(e.code) : ''}`;
		case 'event_raid':
			return `Raid: ${RAID_MESSAGES[e.raid ?? ''] ?? e.raid ?? 'unknown event'}`;
		case 'world_tombstone': {
			const where = place(e);
			return `New tombstone: ${e.owner || 'someone'}${where ? `, ${where}` : ''}`;
		}
		case 'world_portal':
			return e.paired ? `New portal “${e.tag}”, paired with “${e.tag}”` : `New portal “${e.tag}”, not paired with anything yet`;
		case 'world_portal_paired':
			return `Portal “${e.tag}” now paired with “${e.tag}”`;
		case 'world_tame':
			return `New tame: ${e.name} (${e.species})`;
		case 'world_base_new':
			return e.biome ? `New base: ${e.name} (${e.biome})` : `New base: ${e.name}`;
		case 'world_base_grew':
			return `${e.name} grew by ${e.grew} pieces`;
		case 'world_boss':
			return `${e.boss} defeated`;
		default:
			return e.type;
	}
}

export function eventIcon(e: Activity): ActivityIcon {
	if (e.type === 'player_join') return 'log-in';
	if (e.type === 'player_leave') return 'log-out';
	if (e.type === 'world_saved') return 'map';
	return CATEGORIES.find((c) => c.key === e.category)?.icon ?? 'power';
}

/** The icon disc's tone (design `tone`): joins and deaths and raids ember, bosses and tames sage, portals cold. */
export function eventTone(e: Activity): Tone {
	switch (e.category) {
		case 'session':
			return e.type === 'player_join' ? 'ember' : 'neutral';
		case 'death':
		case 'event':
			return 'ember';
		case 'boss':
		case 'tame':
			return 'sage';
		case 'portal':
			return 'cold';
		default:
			return 'neutral';
	}
}

/** "Server log", or "World save · 14:20" (the save's time in the server's zone). */
export function sourceText(e: Activity, tz: string): string {
	return e.source === 'save' ? `World save · ${zClock(e.at, tz)}` : 'Server log';
}

/** Whether `e` passes the filters: its category is on, and it concerns a chosen player (or nobody in particular). */
export function passes(e: Activity, off: readonly Category[], people: readonly string[]): boolean {
	if (off.includes(e.category)) return false;
	if (people.length === 0 || e.who.length === 0) return true;
	return e.who.some((id) => people.includes(id));
}

/** Per-chip counts: the events passing the people filter, by category. */
export function chipCounts(events: readonly Activity[], people: readonly string[]): Record<Category, number> {
	const out = Object.fromEntries(CATEGORIES.map((c) => [c.key, 0])) as Record<Category, number>;
	for (const e of events) if (passes(e, [], people)) out[e.category]++;
	return out;
}

export interface DayGroup {
	key: string;
	label: string;
	sub: string;
	items: Activity[];
}

/**
 * Groups events (newest first) by local day in `tz`: "Today · Tue 29 Sep ·
 * in-game day 214" (the day only when `gameDay` is known), "Yesterday ·
 * Mon 28 Sep", then "Sun 27 Sep".
 */
export function groupByDay(events: readonly Activity[], tz: string, now: Date, gameDay?: number): DayGroup[] {
	const today = dayKey(now, tz);
	const yesterday = prevDayKey(today);
	const groups: DayGroup[] = [];
	for (const e of events) {
		const key = dayKey(e.at, tz);
		let g = groups[groups.length - 1];
		if (!g || g.key !== key) {
			const date = zWeekday(e.at, tz);
			if (key === today) g = { key, label: 'Today', sub: gameDay ? `${date} · in-game day ${gameDay}` : date, items: [] };
			else if (key === yesterday) g = { key, label: 'Yesterday', sub: date, items: [] };
			else g = { key, label: date, sub: '', items: [] };
			groups.push(g);
		}
		g.items.push(e);
	}
	return groups;
}

export interface TodayBar {
	/** Left edge and width, % of the 00–24 axis. */
	left: number;
	width: number;
	live: boolean;
	title: string;
}

export interface TodayRow {
	id: string;
	name: string;
	bars: TodayBar[];
}

/** "Who was on today": a row per player, a bar per session on the day's axis; open sessions run to `now`. */
export function todayRows(t: TodaySessions, now: Date, people: readonly string[] = []): { rows: TodayRow[]; nowPct: number; nowClock: string } {
	const d0 = new Date(t.dayStart).getTime();
	const span = new Date(t.dayEnd).getTime() - d0;
	const pct = (ms: number) => Math.max(0, Math.min(100, ((ms - d0) / span) * 100));
	const nowMs = Math.min(now.getTime(), new Date(t.dayEnd).getTime());
	const rows = t.players
		.filter((p) => people.length === 0 || people.includes(p.id))
		.map((p) => ({
			id: p.id,
			name: p.name,
			bars: p.spans.map((s) => {
				const a = new Date(s.since).getTime();
				const b = s.until ? new Date(s.until).getTime() : nowMs;
				return {
					left: pct(a),
					width: Math.max(0, pct(b) - pct(a)),
					live: !s.until,
					title: `${p.name} · ${zClock(s.since, t.timeZone)}–${s.until ? zClock(s.until, t.timeZone) : 'now'}`
				};
			})
		}));
	return { rows, nowPct: pct(nowMs), nowClock: zClock(new Date(nowMs), t.timeZone) };
}
```

Create `web/src/lib/filters.svelte.ts`:

```ts
// The activity filters (Plan 7), shared by the timeline and the side
// panel's short activity list: categories switched off, and the players
// chosen (none = everyone).

import type { Category } from './types';

export class ActivityFilters {
	off = $state<Category[]>([]);
	people = $state<string[]>([]);

	toggle(c: Category): void {
		this.off = this.off.includes(c) ? this.off.filter((x) => x !== c) : [...this.off, c];
	}

	togglePerson(id: string): void {
		this.people = this.people.includes(id) ? this.people.filter((x) => x !== id) : [...this.people, id];
	}

	everyone(): void {
		this.people = [];
	}

	reset(): void {
		this.off = [];
		this.people = [];
	}

	get active(): boolean {
		return this.off.length > 0 || this.people.length > 0;
	}
}

export const filters = new ActivityFilters();
```

- [ ] **Step 5: The short list on the same model; `markerAt`**

In `web/src/lib/derive.ts`, in the imports, replace:

```ts
import { fmtClock, fmtCode, fmtDayRef, fmtMapAge } from './format';
import type { Card, Marker, ServerSummary, Status, WorldCard } from './types';
```

with:

```ts
import { fmtClock, fmtDayRef, fmtMapAge } from './format';
import { eventIcon, eventText, eventTone, passes, type ActivityIcon, type Tone } from './timeline';
import type { Activity, Card, Category, Marker, ServerSummary, Status, WorldCard } from './types';
```

In `web/src/lib/derive.ts`, the whole Activity section, replace:

```ts
// --- Activity (§3.8) --------------------------------------------------------

export interface ActivityRow {
	icon: 'log-in' | 'log-out' | 'power' | 'map';
	tone: 'ember' | 'neutral';
	text: string;
	at: string;
}

function toActivityRow(a: Card['activity'][number]): ActivityRow | undefined {
	switch (a.type) {
		case 'player_join':
			return { icon: 'log-in', tone: 'ember', text: `${a.name} joined`, at: a.at };
		case 'player_leave':
			return { icon: 'log-out', tone: 'neutral', text: `${a.name} left`, at: a.at };
		case 'server_ready':
			return {
				icon: 'power',
				tone: 'neutral',
				text: a.version ? `Server is up · version ${a.version}` : 'Server is up',
				at: a.at
			};
		case 'server_stopped':
			return { icon: 'power', tone: 'neutral', text: 'Server stopped', at: a.at };
		case 'server_starting':
		case 'server_boot':
			return { icon: 'power', tone: 'neutral', text: 'Server starting', at: a.at };
		case 'world_saved':
			return { icon: 'map', tone: 'neutral', text: 'Autosave finished · map updated', at: a.at };
		case 'join_code':
			return {
				icon: 'power',
				tone: 'neutral',
				text: `New join code ${a.code ? fmtCode(a.code) : ''}`,
				at: a.at
			};
		default:
			return undefined;
	}
}

/** Keeps only the newest `world_saved` row (activity is `at desc`) and caps at `limit`. */
export function activityRows(card: Card, limit: number): ActivityRow[] {
	const rows: ActivityRow[] = [];
	let sawWorldSaved = false;
	for (const a of card.activity) {
		if (rows.length >= limit) break;
		if (a.type === 'world_saved') {
			if (sawWorldSaved) continue;
			sawWorldSaved = true;
		}
		const row = toActivityRow(a);
		if (row) rows.push(row);
	}
	return rows;
}

```

with:

```ts
// --- Activity (§3.8; Plan 7) ------------------------------------------------

export interface ActivityRow {
	icon: ActivityIcon;
	tone: Tone;
	text: string;
	at: string;
	source: Activity['source'];
}

/**
 * The side panel's short list: the card's activity (newest first) through
 * the timeline's filters, only the newest `world_saved` row kept, capped at
 * `limit`. Text, icon and tone are the timeline's.
 */
export function activityRows(
	card: Card,
	limit: number,
	off: readonly Category[] = [],
	people: readonly string[] = []
): ActivityRow[] {
	const rows: ActivityRow[] = [];
	let sawWorldSaved = false;
	for (const a of card.activity) {
		if (rows.length >= limit) break;
		if (!passes(a, off, people)) continue;
		if (a.type === 'world_saved') {
			if (sawWorldSaved) continue;
			sawWorldSaved = true;
		}
		rows.push({ icon: eventIcon(a), tone: eventTone(a), text: eventText(a), at: a.at, source: a.source });
	}
	return rows;
}

```

Append to the end of `web/src/lib/markers.ts`, after a blank line:

```ts
/**
 * The marker nearest (x, z) within `radius` metres, if any: a timeline
 * event knows its place but not the marker (marker ids are per save).
 */
export function markerAt(all: readonly MapMarker[], x: number, z: number, radius = 5): MapMarker | undefined {
	let best: MapMarker | undefined;
	let bestD = radius;
	for (const m of all) {
		const d = Math.hypot(m.x - x, m.z - z);
		if (d <= bestD) {
			best = m;
			bestD = d;
		}
	}
	return best;
}
```

- [ ] **Step 6: Run the unit tests to make sure they pass**

Run: `cd web && npx vitest run`

Expected: every test file passes.

- [ ] **Step 7: The components**

`ActivityList.svelte` changes throughout (icons, filters, the heading link), so replace it whole.

Create `web/src/lib/components/ActivityIcon.svelte`:

```svelte
<!--
  An activity icon (Plan 7): Lucide log-in, log-out, power, map and
  triangle-alert, or a marker icon (skull, flame, home, portal, paw-print)
  from the shared table, all at stroke-width 2.75 in currentColor.
-->
<script lang="ts">
	import LogIn from 'lucide-svelte/icons/log-in';
	import LogOut from 'lucide-svelte/icons/log-out';
	import MapIcon from 'lucide-svelte/icons/map';
	import Power from 'lucide-svelte/icons/power';
	import TriangleAlert from 'lucide-svelte/icons/triangle-alert';
	import type { ActivityIcon } from '$lib/timeline';
	import MarkerIcon from './MarkerIcon.svelte';

	let { name, size = 14 }: { name: ActivityIcon; size?: number } = $props();

	const LUCIDE = { 'log-in': LogIn, 'log-out': LogOut, power: Power, map: MapIcon, alert: TriangleAlert };
</script>

{#if name === 'log-in' || name === 'log-out' || name === 'power' || name === 'map' || name === 'alert'}
	{@const Icon = LUCIDE[name]}
	<Icon {size} strokeWidth={2.75} />
{:else}
	<MarkerIcon {name} {size} />
{/if}
```

Replace the whole of `web/src/lib/components/ActivityList.svelte` with:

```svelte
<!--
  Recent activity (DESIGN-NOTES §3.8, Plan 7): the card's activity through
  the timeline's filters (shared with the Activity view), the newest
  autosave row kept, up to `limit` rows (8 desktop, 3 mobile). Log rows
  show how long ago; world-save rows "save HH:MM" in the server's zone.
  With `full`, the heading carries "Full timeline →", which opens the
  Activity view. `mobile` is the pulled-sheet look (§3.18 item 5: 32 px
  discs, 14.5 px text, 48 px rows, an 18 px heading).
-->
<script lang="ts">
	import { activityRows } from '$lib/derive';
	import { filters } from '$lib/filters.svelte';
	import { fmtActivityTime } from '$lib/format';
	import { app } from '$lib/state.svelte';
	import type { Card } from '$lib/types';
	import { zClock } from '$lib/zoned';
	import ActivityIcon from './ActivityIcon.svelte';

	let {
		card,
		now,
		limit = 8,
		mobile = false,
		full = false
	}: { card: Card; now: Date; limit?: number; mobile?: boolean; full?: boolean } = $props();

	const uid = $props.id();

	const rows = $derived(activityRows(card, limit, filters.off, filters.people));
	const tz = $derived(card.timeZone ?? 'UTC');
</script>

<section class="activity" class:mobile aria-labelledby="{uid}-activity-title">
	<div class="head">
		<h3 class="title" id="{uid}-activity-title">Recent activity</h3>
		{#if full}
			<button class="btn btn-ghost full" type="button" onclick={() => app.openView({ kind: 'activity' })}>Full timeline →</button>
		{/if}
	</div>
	{#if rows.length === 0}
		<p class="empty">{filters.active ? 'Nothing matches these filters.' : 'No server activity yet.'}</p>
	{:else}
		<ul>
			{#each rows as a, i (i)}
				<li class="row">
					<span class="disc {a.tone}" aria-hidden="true"><ActivityIcon name={a.icon} size={mobile ? 15 : 14} /></span>
					<span class="text">{a.text}</span>
					<time class="time" datetime={a.at}>{a.source === 'save' ? `save ${zClock(a.at, tz)}` : fmtActivityTime(a.at, now)}</time>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<style>
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		padding: 18px 6px 6px;
	}
	.title {
		margin: 0;
		font-size: 17px;
		line-height: 1.55;
		letter-spacing: normal;
	}
	.full {
		font-size: 13px;
		font-weight: 700;
		color: var(--cold-ink);
		white-space: nowrap;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.row {
		display: flex;
		align-items: flex-start;
		gap: 10px;
		padding: 7px 8px;
		border-radius: 14px;
	}
	.row:hover {
		background: color-mix(in srgb, var(--color-text) 5%, transparent);
	}
	.disc {
		width: 28px;
		height: 28px;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
	}
	.disc.ember {
		background: var(--color-accent-100);
		color: var(--color-accent-600);
	}
	.disc.neutral {
		background: color-mix(in srgb, var(--color-text) 8%, transparent);
		color: var(--color-text);
	}
	.disc.sage {
		background: var(--color-accent-2-100);
		color: var(--color-accent-2-700);
	}
	.disc.cold {
		background: color-mix(in srgb, var(--cold) 22%, transparent);
		color: var(--cold);
	}
	.text {
		flex: 1;
		min-width: 0;
		font-size: 14px;
		line-height: 1.35;
		padding-top: 4px;
	}
	.time {
		font-size: 12px;
		color: var(--muted);
		white-space: nowrap;
		padding-top: 5px;
	}
	.mobile .head {
		padding: 14px 4px 4px;
	}
	.mobile .title {
		font-size: 18px;
	}
	.mobile .row {
		align-items: center;
		gap: 12px;
		min-height: 48px;
		padding: 0 4px;
	}
	.mobile .disc {
		width: 32px;
		height: 32px;
	}
	.mobile .text {
		font-size: 14.5px;
		line-height: 1.3;
		padding-top: 0;
	}
	.mobile .time {
		padding-top: 0;
	}
	.empty {
		margin: 0;
		padding: 4px 8px;
		font-size: 13px;
		color: var(--muted);
	}
</style>
```

Create `web/src/lib/components/ActivityContent.svelte`:

```svelte
<!--
  The activity timeline (Plan 7; design `:837-908`), shared by
  ActivityPanel (desktop) and ActivitySheet (mobile): who was on today on a
  00–24 axis in the server's zone; the eight category chips with counts
  and the people chips (filters shared with the side panel's short list);
  the events grouped by day, newest first; "Show earlier" (three more
  days) until tracking began; and the "where each entry comes from"
  footnote. `days` reports how many days are loaded, for the header.
-->
<script lang="ts">
	import { getActivity, getSessionsToday } from '$lib/api';
	import { filters } from '$lib/filters.svelte';
	import { app } from '$lib/state.svelte';
	import { CATEGORIES, chipCounts, eventIcon, eventText, eventTone, groupByDay, passes, sourceText, todayRows } from '$lib/timeline';
	import type { ActivityPage, TodaySessions } from '$lib/types';
	import { zClock, zDayMonth } from '$lib/zoned';
	import ActivityIcon from './ActivityIcon.svelte';

	let {
		serverId,
		gameDay,
		mobile = false,
		days = $bindable(3),
		onmap
	}: {
		serverId: string;
		gameDay?: number;
		mobile?: boolean;
		days?: number;
		onmap: (x: number, z: number) => void;
	} = $props();

	const uid = $props.id();
	let pages = $state<ActivityPage[]>([]);
	let today = $state<TodaySessions>();
	let failed = $state(false);
	let loadingMore = $state(false);

	$effect(() => {
		const id = serverId;
		let live = true;
		pages = [];
		today = undefined;
		failed = false;
		Promise.all([getActivity(id), getSessionsToday(id)])
			.then(([p, t]) => {
				if (!live) return;
				pages = [p];
				today = t;
			})
			.catch(() => {
				if (live) failed = true;
			});
		return () => {
			live = false;
		};
	});

	const events = $derived(pages.flatMap((p) => p.events));
	const tz = $derived(pages[0]?.timeZone ?? 'UTC');
	const people = $derived(pages[0]?.people ?? []);
	const oldest = $derived(pages.at(-1));
	const more = $derived(!!oldest?.earliest && new Date(oldest.from).getTime() > new Date(oldest.earliest).getTime());
	const counts = $derived(chipCounts(events, filters.people));
	const groups = $derived(groupByDay(events.filter((e) => passes(e, filters.off, filters.people)), tz, app.now, gameDay));
	const whoToday = $derived(today ? todayRows(today, app.now, filters.people) : undefined);

	$effect(() => {
		days = Math.max(1, pages.length) * 3;
	});

	async function earlier(): Promise<void> {
		if (!oldest || loadingMore) return;
		loadingMore = true;
		try {
			const p = await getActivity(serverId, oldest.from);
			if (p.timeZone && pages.at(-1) === oldest) pages = [...pages, p];
		} catch {
			app.showToast('Couldn’t load earlier activity', 'Try again in a moment.');
		} finally {
			loadingMore = false;
		}
	}
</script>

<div class="activity" class:mobile>
	<div class="filters">
		<h3 class="label" id="{uid}-today">Who was on today</h3>
		<div class="today" aria-labelledby="{uid}-today">
			{#if whoToday && whoToday.rows.length}
				<ul class="today-rows">
					{#each whoToday.rows as r (r.id + r.name)}
						<li class="today-row">
							<span class="today-name">{r.name}</span>
							<span class="track">
								{#each r.bars as b, i (i)}
									<span class="span" class:live={b.live} title={b.title} style:left="{b.left}%" style:width="{b.width}%"></span>
								{/each}
								<span class="now" style:left="{whoToday.nowPct}%"></span>
							</span>
						</li>
					{/each}
				</ul>
				<div class="axis" aria-hidden="true"><span>00</span><span>06</span><span>12</span><span>18</span><span>24</span></div>
				<div class="legend">
					<span><i class="key live"></i>Online now</span>
					<span><i class="key"></i>Earlier session</span>
					<span><i class="key now-key"></i>Now · {whoToday.nowClock}</span>
				</div>
			{:else if today}
				<p class="muted">Nobody has played today yet.</p>
			{/if}
		</div>

		<h3 class="label" id="{uid}-show">Show</h3>
		<div class="chips" role="group" aria-labelledby="{uid}-show">
			{#each CATEGORIES as c (c.key)}
				{@const on = !filters.off.includes(c.key)}
				<button class="chip" class:on type="button" aria-pressed={on} onclick={() => filters.toggle(c.key)}>
					<span class="chip-icon"><ActivityIcon name={c.icon} size={14} /></span>{c.label}<span class="count">{counts[c.key]}</span>
				</button>
			{/each}
		</div>
		<div class="chips" role="group" aria-label="People">
			<button class="person" class:on={filters.people.length === 0} type="button" aria-pressed={filters.people.length === 0} onclick={() => filters.everyone()}>
				<span class="initial">★</span>Everyone
			</button>
			{#each people as p (p.id)}
				{@const on = filters.people.includes(p.id)}
				<button class="person" class:on type="button" aria-pressed={on} onclick={() => filters.togglePerson(p.id)}>
					<span class="initial">{Array.from(p.name)[0]?.toUpperCase() ?? '?'}</span>{p.name}
				</button>
			{/each}
		</div>
	</div>

	<div class="list">
		{#if failed}
			<p class="end">Couldn’t load the activity. Try again in a moment.</p>
		{:else if pages.length === 0}
			<p class="end" aria-busy="true">Loading…</p>
		{:else}
			{#each groups as g (g.key)}
				<section class="day" aria-label={g.label}>
					<h4 class="day-head"><span class="day-label">{g.label}</span><span class="day-sub">{g.sub}</span></h4>
					<ol class="entries">
						{#each g.items as e (e.id)}
							<li class="entry">
								<time class="clock" datetime={e.at}>{zClock(e.at, tz)}</time>
								<span class="rail"><span class="disc {eventTone(e)}" aria-hidden="true"><ActivityIcon name={eventIcon(e)} /></span><span class="line"></span></span>
								<span class="body">
									<span class="text">{eventText(e)}</span>
									<span class="src">
										<span class="src-dot" class:save={e.source === 'save'}></span>{sourceText(e, tz)}
										{#if e.x !== undefined && e.z !== undefined}
											{@const x = e.x}
											{@const z = e.z}
											<button class="on-map" type="button" onclick={() => onmap(x, z)}>Show on map →</button>
										{/if}
									</span>
								</span>
							</li>
						{/each}
					</ol>
				</section>
			{/each}
			{#if events.length > 0 && groups.length === 0}
				<p class="end">Nothing matches these filters.</p>
			{:else if events.length === 0}
				<p class="end">Nothing happened in these days.</p>
			{/if}
			{#if more}
				<button class="btn btn-secondary earlier" type="button" disabled={loadingMore} onclick={earlier}>Show earlier</button>
			{:else if oldest?.earliest}
				<p class="end">Tracking began {zDayMonth(oldest.earliest, tz)}</p>
			{/if}
		{/if}
		<div class="foot">
			<b>Where each entry comes from.</b>
			<span><i class="src-dot"></i><b>Server log</b> (live, exact time): joins, leaves, restarts, saves and raids.</span>
			<span
				><i class="src-dot save"></i><b>World save</b> (spotted by comparing two saves): tombstones, new portals, tames, base growth
				and bosses. The time is the save it first appeared in, never the exact moment.</span
			>
			<span>“Show on map” only appears for places in explored areas.</span>
		</div>
	</div>
</div>

<style>
	.activity {
		display: flex;
		flex-direction: column;
	}
	.filters {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 6px 20px 16px;
	}
	.mobile .filters {
		gap: 12px;
		padding: 4px 16px 14px;
	}
	.label {
		margin: 0;
		font-family: var(--font-body);
		font-size: 12px;
		font-weight: 700;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.muted {
		margin: 0;
		font-size: 13px;
		color: var(--muted);
	}
	.today {
		display: flex;
		flex-direction: column;
		gap: 7px;
	}
	.today-rows {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 7px;
	}
	.today-row {
		display: grid;
		grid-template-columns: 52px minmax(0, 1fr);
		gap: 8px;
		align-items: center;
	}
	.today-name {
		font-size: 12.5px;
		font-weight: 700;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.track {
		position: relative;
		height: 14px;
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
	}
	.span {
		position: absolute;
		top: 0;
		bottom: 0;
		min-width: 4px;
		border-radius: 999px;
		background: var(--color-neutral-400);
	}
	.span.live {
		background: var(--color-accent);
	}
	.now {
		position: absolute;
		top: -3px;
		bottom: -3px;
		width: 2px;
		background: var(--cold);
	}
	.axis {
		display: flex;
		justify-content: space-between;
		padding-left: 60px;
		font-size: 11px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}
	.legend {
		display: flex;
		gap: 14px;
		flex-wrap: wrap;
		font-size: 12px;
		color: var(--muted);
	}
	.legend span {
		display: flex;
		align-items: center;
		gap: 6px;
	}
	.key {
		width: 14px;
		height: 8px;
		border-radius: 999px;
		background: var(--color-neutral-400);
	}
	.key.live {
		background: var(--color-accent);
	}
	.key.now-key {
		width: 2px;
		height: 12px;
		border-radius: 0;
		background: var(--cold);
	}
	.chips {
		display: flex;
		gap: 6px;
		flex-wrap: wrap;
	}
	.mobile .chips {
		flex-wrap: nowrap;
		overflow-x: auto;
		scrollbar-width: none;
		margin-right: -16px;
		padding-right: 16px;
	}
	.chip {
		flex: none;
		display: flex;
		align-items: center;
		gap: 6px;
		height: 32px;
		padding: 0 12px 0 8px;
		border-radius: 999px;
		border: 1.5px solid var(--color-divider);
		background: transparent;
		color: var(--muted);
		font: inherit;
		font-size: 13px;
		font-weight: 600;
		cursor: pointer;
	}
	.chip.on {
		border-color: var(--cold);
		background: color-mix(in srgb, var(--cold) 16%, transparent);
		color: var(--color-text);
	}
	.chip-icon {
		display: grid;
		opacity: 0.6;
	}
	.chip.on .chip-icon {
		opacity: 1;
	}
	.count {
		font-size: 11.5px;
		opacity: 0.7;
		font-variant-numeric: tabular-nums;
	}
	.person {
		flex: none;
		display: flex;
		align-items: center;
		gap: 6px;
		height: 30px;
		padding: 0 12px 0 4px;
		border: 0;
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-text) 7%, transparent);
		color: var(--color-text);
		font: inherit;
		font-size: 13px;
		font-weight: 600;
		cursor: pointer;
	}
	.mobile .chip,
	.mobile .person {
		height: 40px;
	}
	.initial {
		width: 22px;
		height: 22px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: var(--color-accent-200);
		color: var(--color-accent-800);
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 11px;
	}
	.person.on {
		background: var(--color-text);
		color: var(--color-bg);
	}
	.person.on .initial {
		background: var(--color-bg);
		color: var(--color-text);
	}
	.list {
		border-top: 1px solid var(--color-divider);
		padding-bottom: 16px;
	}
	.mobile .list {
		padding-bottom: var(--sheet-bottom, 30px);
	}
	.day-head {
		position: sticky;
		top: 0;
		z-index: 1;
		display: flex;
		align-items: baseline;
		gap: 8px;
		margin: 0;
		padding: 10px 20px 8px;
		background: var(--color-surface);
		font-size: 16px;
		line-height: 1.3;
		letter-spacing: normal;
	}
	.mobile .day-head {
		padding: 10px 12px 8px;
	}
	.day-sub {
		font-family: var(--font-body);
		font-size: 12px;
		color: var(--muted);
	}
	.entries {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.entry {
		display: grid;
		grid-template-columns: 44px 30px minmax(0, 1fr);
		column-gap: 10px;
		padding: 0 20px;
	}
	.mobile .entry {
		padding: 0 12px;
	}
	.clock {
		font-size: 12.5px;
		font-variant-numeric: tabular-nums;
		color: var(--muted);
		padding-top: 7px;
		text-align: right;
	}
	.rail {
		display: flex;
		flex-direction: column;
		align-items: center;
	}
	.disc {
		width: 30px;
		height: 30px;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
	}
	.disc.ember {
		background: var(--color-accent-100);
		color: var(--color-accent-600);
	}
	.disc.neutral {
		background: color-mix(in srgb, var(--color-text) 8%, transparent);
		color: var(--color-text);
	}
	.disc.sage {
		background: var(--color-accent-2-100);
		color: var(--color-accent-2-700);
	}
	.disc.cold {
		background: color-mix(in srgb, var(--cold) 22%, transparent);
		color: var(--cold);
	}
	.line {
		flex: 1;
		width: 2px;
		min-height: 10px;
		background: var(--color-divider);
	}
	.body {
		display: flex;
		flex-direction: column;
		padding: 5px 0 14px;
		min-width: 0;
	}
	.text {
		font-size: 14px;
		line-height: 1.35;
		text-wrap: pretty;
	}
	.src {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
		margin-top: 3px;
		font-size: 12px;
		color: var(--muted);
	}
	.src-dot {
		display: inline-block;
		width: 6px;
		height: 6px;
		margin-right: -3px;
		border-radius: 50%;
		background: var(--color-accent);
	}
	.src-dot.save {
		background: var(--cold);
	}
	.on-map {
		border: 0;
		background: transparent;
		padding: 0;
		font: inherit;
		font-size: 12px;
		font-weight: 700;
		color: var(--cold-ink);
		cursor: pointer;
	}
	.mobile .on-map {
		min-height: 44px;
	}
	.end {
		margin: 0;
		padding: 28px 20px;
		font-size: 14px;
		color: var(--muted);
		text-align: center;
	}
	.earlier {
		display: block;
		margin: 16px auto;
		font-family: var(--font-body);
		font-weight: 700;
	}
	.foot {
		display: flex;
		flex-direction: column;
		gap: 6px;
		padding: 12px 20px 0;
		font-size: 11.5px;
		line-height: 1.45;
		color: var(--muted);
	}
	.foot span {
		display: block;
	}
	.foot .src-dot {
		margin-right: 6px;
	}
</style>
```

Create `web/src/lib/components/ActivityPanel.svelte`:

```svelte
<!--
  Desktop Activity view (Plan 7; design `:61-113`): a 520 px opaque aside in
  the side panel's place, with a back arrow, "Activity", the "Last N days ·
  from the server log and world saves" line and "Reset filters", over the
  scrolling ActivityContent.
-->
<script lang="ts">
	import ChevronLeft from 'lucide-svelte/icons/chevron-left';
	import { filters } from '$lib/filters.svelte';
	import ActivityContent from './ActivityContent.svelte';

	let {
		serverId,
		gameDay,
		onback,
		onmap
	}: {
		serverId: string;
		gameDay?: number;
		onback: () => void;
		onmap: (x: number, z: number) => void;
	} = $props();

	let days = $state(3);
</script>

<aside class="panel" aria-label="Activity" data-testid="activity-panel">
	<div class="head">
		<button class="btn btn-secondary btn-icon" type="button" aria-label="Back" onclick={onback}>
			<ChevronLeft size={17} strokeWidth={2.75} />
		</button>
		<div class="title-block">
			<h2 class="title">Activity</h2>
			<div class="sub">Last {days} days · from the server log and world saves</div>
		</div>
		<button class="btn btn-ghost reset" type="button" onclick={() => filters.reset()}>Reset filters</button>
	</div>
	<div class="body">
		<ActivityContent {serverId} {gameDay} {onmap} bind:days />
	</div>
</aside>

<style>
	.panel {
		position: absolute;
		left: 16px;
		top: 16px;
		bottom: 16px;
		width: 520px;
		max-width: calc(100vw - 32px);
		display: flex;
		flex-direction: column;
		border-radius: 30px;
		background: var(--color-surface);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-lg);
		overflow: hidden;
		z-index: 550;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 16px 16px 10px 12px;
	}
	.title-block {
		flex: 1;
		min-width: 0;
	}
	.title {
		margin: 0;
		font-size: 22px;
		line-height: 1.1;
		letter-spacing: normal;
	}
	.sub {
		font-size: 12px;
		color: var(--muted);
	}
	.reset {
		flex: none;
		font-family: var(--font-body);
		font-size: 13px;
		font-weight: 700;
		white-space: nowrap;
	}
	.body {
		flex: 1;
		min-height: 0;
		overflow: auto;
	}
</style>
```

Create `web/src/lib/components/ActivitySheet.svelte`:

```svelte
<!--
  Mobile Activity view (Plan 7; design `:842-904`): a full-height surface
  sheet with the grab handle (drag down to close), a 48 px back button,
  "Activity" and "Last N days · log + saves", over ActivityContent at touch
  sizes. A modal dialog: focus is trapped and returns to the opener; Esc
  (the shell) closes it.
-->
<script lang="ts">
	import ChevronLeft from 'lucide-svelte/icons/chevron-left';
	import { focusTrap } from '$lib/actions/focusTrap';
	import ActivityContent from './ActivityContent.svelte';
	import BottomSheet from './BottomSheet.svelte';

	let {
		serverId,
		gameDay,
		onclose,
		onmap
	}: {
		serverId: string;
		gameDay?: number;
		onclose: () => void;
		onmap: (x: number, z: number) => void;
	} = $props();

	let days = $state(3);
</script>

<BottomSheet snap="full" surface="surface" {onclose}>
	<div class="sheet" role="dialog" aria-modal="true" aria-label="Activity" tabindex="-1" use:focusTrap={{ initial: '.back' }}>
		<div class="head" data-sheet-drag>
			<button class="back" type="button" aria-label="Back" onclick={onclose}>
				<ChevronLeft size={20} strokeWidth={2.75} />
			</button>
			<div class="title-block">
				<h2 class="title">Activity</h2>
				<div class="sub">Last {days} days · log + saves</div>
			</div>
		</div>
		<ActivityContent {serverId} {gameDay} {onmap} mobile bind:days />
	</div>
</BottomSheet>

<style>
	.sheet {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		display: flex;
		flex-direction: column;
		outline: none;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 0 12px 8px 8px;
	}
	.back {
		width: 48px;
		height: 48px;
		flex: none;
		display: grid;
		place-items: center;
		padding: 0;
		border: 0;
		border-radius: 50%;
		background: transparent;
		color: var(--color-text);
		cursor: pointer;
	}
	.title-block {
		flex: 1;
	}
	.title {
		margin: 0;
		font-size: 22px;
		line-height: 1.1;
		letter-spacing: normal;
	}
	.sub {
		font-size: 12px;
		color: var(--muted);
	}
</style>
```

- [ ] **Step 8: Open it from the side panel and the menu**

In `web/src/lib/components/PlayersTab.svelte`, replace:

```svelte
	<ActivityList {card} {now} limit={8} />
```

with:

```svelte
	<ActivityList {card} {now} limit={8} full />
```

In `web/src/lib/components/MenuSheet.svelte`, in the header comment, replace:

```svelte
    and "Show portal connections"), then the theme row "Theme · Dark/Light";
```

with:

```svelte
    and "Show portal connections"), "Full timeline" (Plan 7: opens the
    Activity view), then the theme row "Theme · Dark/Light";
```

In `web/src/lib/components/MenuSheet.svelte`, in the props, replace:

```svelte
		onpick,
		onclose
	}: {
```

with:

```svelte
		onpick,
		ontimeline,
		onclose
	}: {
```

In `web/src/lib/components/MenuSheet.svelte`, in the prop types, replace:

```svelte
		onpick: (m: MapMarker) => void;
		onclose: () => void;
```

with:

```svelte
		onpick: (m: MapMarker) => void;
		/** "Full timeline" (Plan 7): the shell closes the menu and opens the Activity view. */
		ontimeline: () => void;
		onclose: () => void;
```

In `web/src/lib/components/MenuSheet.svelte`, in the markup, replace:

```svelte
			<LayersPanel id="{uid}-layers" variant="mobile" {layers} {portalLinks} {counts} {ontoggle} {onlinks} />
```

with:

```svelte
			<LayersPanel id="{uid}-layers" variant="mobile" {layers} {portalLinks} {counts} {ontoggle} {onlinks} />
			<button class="timeline" type="button" onclick={ontimeline}>
				<span class="timeline-label">Full timeline</span><span class="timeline-sub">Who was on, deaths, portals, raids…</span>
			</button>
```

In `web/src/lib/components/MenuSheet.svelte`, in the styles, replace:

```svelte
	.theme {
		display: flex;
```

with:

```svelte
	.timeline {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		justify-content: center;
		min-height: 52px;
		padding: 6px 12px;
		border-radius: 18px;
		border: 1.5px solid var(--color-divider);
		background: transparent;
		color: var(--color-text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.timeline-label {
		font-size: 14px;
		font-weight: 600;
	}
	.timeline-sub {
		font-size: 12px;
		color: var(--muted);
	}
	.theme {
		display: flex;
```

In `web/src/lib/components/DesktopShell.svelte`, in the imports, replace:

```svelte
	import ChartingCard from './ChartingCard.svelte';
```

with:

```svelte
	import ActivityPanel from './ActivityPanel.svelte';
	import ChartingCard from './ChartingCard.svelte';
```

In `web/src/lib/components/DesktopShell.svelte`, in the `$lib/markers` import, replace:

```svelte
		layerCounts,
		markersKey,
```

with:

```svelte
		layerCounts,
		markerAt,
		markersKey,
```

In `web/src/lib/components/DesktopShell.svelte`, replace:

```svelte
	const PANEL_W = 376; // 16 + 344 + 16
```

with:

```svelte
	const PANEL_W = 376; // 16 + 344 + 16
	const ACTIVITY_W = 552; // 16 + 520 + 16
```

In `web/src/lib/components/DesktopShell.svelte`, the lines Task 7 added, replace:

```svelte
	/** A profile (Plan 7) takes the panel's place, open or collapsed. */
	const profilePlayer = $derived(app.view?.kind === 'profile' ? app.view.player : undefined);
	const padLeft = $derived(panelOpen || profilePlayer !== undefined ? PANEL_W : 0);
```

with:

```svelte
	/** A profile or the Activity view (Plan 7) takes the panel's place, open or collapsed. */
	const profilePlayer = $derived(app.view?.kind === 'profile' ? app.view.player : undefined);
	const activityOpen = $derived(app.view?.kind === 'activity');
	const padLeft = $derived(activityOpen ? ACTIVITY_W : panelOpen || profilePlayer !== undefined ? PANEL_W : 0);
```

In `web/src/lib/components/DesktopShell.svelte`, before `pickResult`, replace:

```svelte
	/** Search pick (§5.2): centre on the marker at zoom 4.25 and select it. */
```

with:

```svelte
	/** The timeline's "Show on map →": centre at zoom 4, selecting the marker there if any. */
	function showOnMap(x: number, z: number): void {
		atlas?.centerOn(x, z, 4);
		const m = markerAt(all, x, z);
		if (m) select(m.id);
	}

	/** Search pick (§5.2): centre on the marker at zoom 4.25 and select it. */
```

In `web/src/lib/components/DesktopShell.svelte`, in the markup, replace:

```svelte
		<ProfilePanel serverId={app.currentId} player={profilePlayer} onback={() => app.closeView()} onmap={mapTo} />
```

with:

```svelte
		<ProfilePanel serverId={app.currentId} player={profilePlayer} onback={() => app.closeView()} onmap={mapTo} />
	{:else if activityOpen && app.currentId}
		<ActivityPanel serverId={app.currentId} gameDay={card?.world?.day} onback={() => app.closeView()} onmap={showOnMap} />
```

In `web/src/lib/components/MobileShell.svelte`, in the `$lib/markers` import, replace:

```svelte
		layerCounts,
		markersKey,
```

with:

```svelte
		layerCounts,
		markerAt,
		markersKey,
```

In `web/src/lib/components/MobileShell.svelte`, replace:

```svelte
	import AtlasMap from './AtlasMap.svelte';
```

with:

```svelte
	import ActivitySheet from './ActivitySheet.svelte';
	import AtlasMap from './AtlasMap.svelte';
```

In `web/src/lib/components/MobileShell.svelte`, before `pickResult`, replace:

```svelte
	/** Search pick (§5.2, Mobile ruling): close the menu, select the marker at zoom 4.25. */
```

with:

```svelte
	/** The timeline's "Show on map →": close the sheet, centre there, selecting the marker if any. */
	function showOnMap(x: number, z: number): void {
		app.closeView();
		snap = 'peek';
		const m = markerAt(all, x, z);
		if (m) {
			focusMarker(m, 4);
		} else if (map) {
			atlas?.centerOn(x, z, 4, centerDy(map.getSize().y));
		}
	}

	/** Search pick (§5.2, Mobile ruling): close the menu, select the marker at zoom 4.25. */
```

In `web/src/lib/components/MobileShell.svelte`, on `<MenuSheet>`, replace:

```svelte
			onpick={pickResult}
			onclose={() => closeOverlay()}
		/>
```

with:

```svelte
			onpick={pickResult}
			ontimeline={() => {
				closeOverlay(false);
				app.openView({ kind: 'activity' });
			}}
			onclose={() => closeOverlay()}
		/>
```

In `web/src/lib/components/MobileShell.svelte`, at the end of the markup, replace:

```svelte
		<ProfileSheet serverId={app.currentId} player={profilePlayer} onclose={() => app.closeView()} onmap={mapTo} />
	{/if}
```

with:

```svelte
		<ProfileSheet serverId={app.currentId} player={profilePlayer} onclose={() => app.closeView()} onmap={mapTo} />
	{:else if app.view?.kind === 'activity' && app.currentId}
		<ActivitySheet serverId={app.currentId} gameDay={card?.world?.day} onclose={() => app.closeView()} onmap={showOnMap} />
	{/if}
```

- [ ] **Step 9: Check types and run the unit tests**

Run: `cd web && npm run check && npx vitest run`

Expected: svelte-check: 0 errors, 0 warnings; every Vitest file passes.

- [ ] **Step 10: Run the end-to-end suite**

Run: `cd web && PATH=$HOME/.local/go/bin:$PATH LD_LIBRARY_PATH="$(../hack/playwright-libs.sh --print)" FONTCONFIG_FILE="$(../hack/playwright-libs.sh --fontconfig)" npx playwright test`

Expected: `42 passed` (the fixtures still have one save, so no world events yet; Task 9 seeds them).

- [ ] **Step 11: Commit**

```bash
cd /workspace/Farsight
git add web/src/lib/types.ts \
  web/src/lib/api.ts \
  web/src/lib/api.test.ts \
  web/src/lib/timeline.ts \
  web/src/lib/timeline.test.ts \
  web/src/lib/filters.svelte.ts \
  web/src/lib/filters.svelte.test.ts \
  web/src/lib/derive.ts \
  web/src/lib/derive.test.ts \
  web/src/lib/markers.ts \
  web/src/lib/markers.test.ts \
  web/src/lib/components/ActivityIcon.svelte \
  web/src/lib/components/ActivityList.svelte \
  web/src/lib/components/ActivityContent.svelte \
  web/src/lib/components/ActivityPanel.svelte \
  web/src/lib/components/ActivitySheet.svelte \
  web/src/lib/components/PlayersTab.svelte \
  web/src/lib/components/MenuSheet.svelte \
  web/src/lib/components/DesktopShell.svelte \
  web/src/lib/components/MobileShell.svelte
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(web): the activity timeline: chips, people, show earlier, #activity

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
git status --short   # must print nothing
```

---

### Task 9: End-to-end: seeded history and two saves; profile and timeline e2e; docs

The e2e server gets data that exercises everything:

- **Four days of sessions.** Astrid on 27 and 28 Sep (2 h 30 m and 2 h) as well as the open session; Ulf on 28 Sep (1 h 30 m) as well as the 40 minutes on the 29th; Sigrun on 25 Sep (1 h). Sigrun's is four days back, past the timeline's first three-day page.
- **A raid** at 19:20.
- **Owners and a namer.** Portal owners (Astrid: home and mountain; Bjorn: copper) and Big Mama's namer (Astrid's Steam ID) in `snapshot.json`.
- **Two saves.** `global-setup.ts` derives the earlier save from `snapshot.json`: a day before, without Bjorn's tombstone, the copper portal, Big Mama or the Eastwatch base, with 124 fewer Longhouse pieces and Moder not yet defeated. Posting both makes the server derive one event of each world kind.
- **Time zone.** The demo server is in Europe/Oslo.

`farsight-seed -snapshot` becomes repeatable: the snapshots are posted in order, all shifted by the same delta.

The day a seeded event falls on depends on the time of day the suite runs (times are shifted to now), so the tests assert totals, counts and texts, not day groups. Biomes come from the real world generator at seed 12345: the tombstone at (−1200, −600) and Longhouse are in the Meadows, Eastwatch in the Plains.

The existing desktop test 4 now finds the newest save's world events at the top of Recent activity. "Astrid joined" falls off the eight rows, so it checks "Sigrun joined" and "Moder defeated" instead.

On mobile, the pulled sheet is opened with a tap on the handle, not a drag: a tap right after a touch drag can be taken as the tap that stops the fling, and Chromium fires no click for it (1 run in 8 failed that way while this plan was written).

**Files:**
- Modify: `cmd/farsight-seed/main.go` (repeatable `-snapshot`), `cmd/farsight-seed/main_test.go`
- Modify: `web/tests/fixtures/snapshot.json`, `web/tests/fixtures/events.json`
- Modify: `web/tests/e2e/global-setup.ts`, `web/tests/e2e/desktop.spec.ts`, `web/tests/e2e/mobile.spec.ts`
- Modify: `README.md`, `design/DESIGN-NOTES.md`, both Plan 7 specs (implementation notes)

**Interfaces:**
- Consumes: everything above. The e2e tests use the accessible names Tasks 7–8 define: the buttons "Profile of {name}", "Full timeline →", "Full timeline", "Back", "Reset filters", "Show earlier", "Show on map →"; complementary landmarks "Player profile" and "Activity" (desktop); dialogs "Player profile" and "Activity" (mobile); regions "Bases", "Portals placed", "Tames they named", "Deaths"; group "People"; `.stat`, `.status`, `.facts`, `.today-row`, `.count`.
- Produces: `farsight-seed` config `Snapshots []string`; `shiftTimes(evs []logwatch.Event, snaps []*extract.Snapshot, now time.Time) time.Duration`.

- [ ] **Step 1: Write the failing seed tests**

In `cmd/farsight-seed/main_test.go`, in `TestShiftTimes`, replace:

```go
	d := shiftTimes(evs, snap, now)
```

with:

```go
	d := shiftTimes(evs, []*extract.Snapshot{snap}, now)
```

In `cmd/farsight-seed/main_test.go`, in `TestShiftTimesWithoutEvents`, replace:

```go
	shiftTimes(nil, snap, now)
```

with:

```go
	shiftTimes(nil, []*extract.Snapshot{snap}, now)
```

In `cmd/farsight-seed/main_test.go`, in `TestRunPostsSnapshotAndEvents`, replace:

```go
		Snapshot: fixtureSnapshot, Events: fixtureEvents, Shift: true,
```

with:

```go
		Snapshots: []string{fixtureSnapshot}, Events: fixtureEvents, Shift: true,
```

In `cmd/farsight-seed/main_test.go`, in `TestRunExploredRasterisesTheFixtureZones`, replace:

```go
	cfg := config{URL: srv.URL, Server: "demo", TokenEnv: "T", Snapshot: fixtureSnapshot, Explored: true}
```

with:

```go
	cfg := config{URL: srv.URL, Server: "demo", TokenEnv: "T", Snapshots: []string{fixtureSnapshot}, Explored: true}
```

In `cmd/farsight-seed/main_test.go`, in `TestRunFakeTilesOnly`, replace:

```go
	cfg := config{Snapshot: fixtureSnapshot, FakeTiles: data}
```

with:

```go
	cfg := config{Snapshots: []string{fixtureSnapshot}, FakeTiles: data}
```

In `cmd/farsight-seed/main_test.go`, before `TestParseFlagsDefaults`, replace:

```go
func TestParseFlagsDefaults(t *testing.T) {
```

with:

```go
// Plan 7: -snapshot repeats; the snapshots go in order, shifted together.
func TestRunPostsSnapshotsInOrderWithOneShift(t *testing.T) {
	var mu sync.Mutex
	var saved []time.Time
	var ids []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var s extract.Snapshot
		if r.URL.Path == "/ingest/demo/snapshot" {
			if err := json.NewDecoder(zr).Decode(&s); err != nil {
				t.Error(err)
			}
			mu.Lock()
			saved, ids = append(saved, s.SavedAt), append(ids, s.SaveID)
			mu.Unlock()
		}
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	write := func(name, saveID string, at time.Time) string {
		p := filepath.Join(dir, name)
		b, _ := json.Marshal(extract.Snapshot{ServerID: "x", SaveID: saveID, SavedAt: at, ReadAt: at})
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	base := time.Date(2026, 9, 29, 19, 55, 0, 0, time.UTC)
	first := write("a.json", "chunked:1", base.Add(-24*time.Hour))
	second := write("b.json", "chunked:2", base)
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	cfg := config{URL: srv.URL, Server: "demo", TokenEnv: "T", Snapshots: []string{first, second}, Shift: true}
	if err := run(context.Background(), cfg, func(string) string { return "tok" }, now, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "chunked:1" || ids[1] != "chunked:2" {
		t.Fatalf("posted %v, want chunked:1 then chunked:2", ids)
	}
	if !saved[1].Equal(now.Add(-6*time.Minute)) || saved[1].Sub(saved[0]) != 24*time.Hour {
		t.Fatalf("saved at %v, want the newest at now-6m and a day apart", saved)
	}
}

func TestParseFlagsRepeatsSnapshot(t *testing.T) {
	cfg, err := parseFlags([]string{"-server", "demo", "-snapshot", "a.json", "-snapshot", "b.json"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Snapshots) != 2 || cfg.Snapshots[0] != "a.json" || cfg.Snapshots[1] != "b.json" {
		t.Fatalf("snapshots = %v", cfg.Snapshots)
	}
}

func TestParseFlagsDefaults(t *testing.T) {
```

In `cmd/farsight-seed/main_test.go`, at the end of `TestFixtures`, replace:

```go
	if !snap.SavedAt.Equal(newestSave) {
		t.Fatalf("snapshot savedAt %v, want the newest world_saved %v", snap.SavedAt, newestSave)
	}
}
```

with:

```go
	if !snap.SavedAt.Equal(newestSave) {
		t.Fatalf("snapshot savedAt %v, want the newest world_saved %v", snap.SavedAt, newestSave)
	}

	// Plan 7: sessions reach back four days (past the timeline's first
	// three-day page), every leave matches its join, and the save names
	// portal creators and a tame's namer by the players' platform IDs.
	var oldest time.Time
	ids = map[string]bool{}
	for _, e := range evs {
		if oldest.IsZero() || e.At.Before(oldest) {
			oldest = e.At
		}
		if e.Type == logwatch.EvPlayerLeave && (e.Since == nil || int64(e.At.Sub(*e.Since)/time.Second) != e.Seconds) {
			t.Errorf("leave %s: since %v, seconds %d", e.ID, e.Since, e.Seconds)
		}
		if e.PlatformID != "" {
			ids[e.Platform+"_"+e.PlatformID] = true
		}
	}
	if got := newest.Sub(oldest); got < 4*24*time.Hour {
		t.Errorf("history spans %v, want at least 4 days", got)
	}
	owners, namers := 0, 0
	for _, m := range snap.Markers {
		if m.Kind == "portal" && m.Owner != "" {
			owners++
		}
		if m.Namer != "" {
			namers++
			if !ids[m.Namer] {
				t.Errorf("tame %s namer %q is no player's platform user ID", m.ID, m.Namer)
			}
		}
	}
	if owners != 3 || namers != 1 {
		t.Errorf("portals with an owner %d (want 3), tames with a namer %d (want 1)", owners, namers)
	}
}
```

- [ ] **Step 2: Run them to make sure they fail**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./cmd/farsight-seed/`

Expected: FAIL at build: `unknown field Snapshots in struct literal of type config`.

- [ ] **Step 3: Make `-snapshot` repeatable**

In `cmd/farsight-seed/main.go`, in the package comment, replace:

```go
//	FARSIGHT_SEED_TOKEN=... farsight-seed -server demo -shift -explored \
//	    -snapshot web/tests/fixtures/snapshot.json -events web/tests/fixtures/events.json
```

with:

```go
//	FARSIGHT_SEED_TOKEN=... farsight-seed -server demo -shift -explored \
//	    -snapshot web/tests/fixtures/snapshot.json -events web/tests/fixtures/events.json
//
// -snapshot may be repeated: the snapshots are posted in order, all shifted
// by the same delta, so the server diffs them into world-save events.
```

In `cmd/farsight-seed/main.go`, in `type config`, replace:

```go
	TokenEnv  string
	Snapshot  string
	Events    string
```

with:

```go
	TokenEnv  string
	Snapshots []string
	Events    string
```

In `cmd/farsight-seed/main.go`, in `parseFlags`, replace:

```go
	fs.StringVar(&c.Snapshot, "snapshot", "", "snapshot JSON file (an extract.Snapshot); also gives the world seed for -fake-tiles/-real-tiles")
```

with:

```go
	fs.Func("snapshot", "snapshot JSON file (an extract.Snapshot); repeatable, posted in order; the first gives the world seed for -fake-tiles/-real-tiles", func(v string) error {
		c.Snapshots = append(c.Snapshots, v)
		return nil
	})
```

In `cmd/farsight-seed/main.go`, in `run`, replace:

```go
	if tilesWanted && c.Snapshot == "" {
```

with:

```go
	if tilesWanted && len(c.Snapshots) == 0 {
```

In `cmd/farsight-seed/main.go`, replace:

```go
	if posting && c.Snapshot == "" && c.Events == "" {
```

with:

```go
	if posting && len(c.Snapshots) == 0 && c.Events == "" {
```

In `cmd/farsight-seed/main.go`, replace:

```go
	var snap *extract.Snapshot
	if c.Snapshot != "" {
		s, err := readSnapshot(c.Snapshot)
		if err != nil {
			return err
		}
		snap = s
	}
```

with:

```go
	var snaps []*extract.Snapshot
	for _, path := range c.Snapshots {
		s, err := readSnapshot(path)
		if err != nil {
			return err
		}
		snaps = append(snaps, s)
	}
```

In `cmd/farsight-seed/main.go`, replace:

```go
		dir, err := writeFakeTiles(c.FakeTiles, snap.World.Seed, snap.World.GenVersion)
```

with:

```go
		dir, err := writeFakeTiles(c.FakeTiles, snaps[0].World.Seed, snaps[0].World.GenVersion)
```

In `cmd/farsight-seed/main.go`, replace:

```go
		dir, err := renderRealTiles(ctx, c.RealTiles, snap.World.Seed, snap.World.GenVersion, out)
```

with:

```go
		dir, err := renderRealTiles(ctx, c.RealTiles, snaps[0].World.Seed, snaps[0].World.GenVersion, out)
```

In `cmd/farsight-seed/main.go`, the posting part of `run`, replace:

```go
	if c.Shift {
		d := shiftTimes(evs, snap, now)
		fmt.Fprintf(out, "shifted times by %s\n", d.Round(time.Second))
	}
	if c.Explored && snap != nil && snap.Explored == nil {
		enc := explored.Encode(explored.FromZones(snap.ExploredZones), explored.SourceZones)
		snap.Explored = &enc
	}
	cl := ingest.New(c.URL, c.Server, token)
	if snap != nil {
		snap.ServerID = c.Server
```

with:

```go
	if c.Shift {
		d := shiftTimes(evs, snaps, now)
		fmt.Fprintf(out, "shifted times by %s\n", d.Round(time.Second))
	}
	cl := ingest.New(c.URL, c.Server, token)
	for _, snap := range snaps {
		if c.Explored && snap.Explored == nil {
			enc := explored.Encode(explored.FromZones(snap.ExploredZones), explored.SourceZones)
			snap.Explored = &enc
		}
		snap.ServerID = c.Server
```

In `cmd/farsight-seed/main.go`, `shiftTimes`, replace:

```go
// shiftTimes moves every event time (at, since) and the snapshot's
// savedAt/readAt by one delta, chosen so the newest event lands at
// now - 1 min; relative gaps are preserved. With no events, the snapshot
// is placed as if its save were 5 min before such a newest event. It
// returns the delta.
func shiftTimes(evs []logwatch.Event, snap *extract.Snapshot, now time.Time) time.Duration {
	var newest time.Time
	for _, e := range evs {
		if e.At.After(newest) {
			newest = e.At
		}
	}
	if newest.IsZero() {
		if snap == nil {
			return 0
		}
		newest = snap.SavedAt.Add(5 * time.Minute)
	}
	d := now.Add(-time.Minute).Sub(newest)
	for i := range evs {
		evs[i].At = evs[i].At.Add(d)
		if evs[i].Since != nil {
			s := evs[i].Since.Add(d)
			evs[i].Since = &s
		}
	}
	if snap != nil {
		snap.SavedAt = snap.SavedAt.Add(d)
		snap.ReadAt = snap.ReadAt.Add(d)
	}
	return d
}

```

with:

```go
// shiftTimes moves every event time (at, since) and every snapshot's
// savedAt/readAt by one delta, chosen so the newest event lands at
// now - 1 min; relative gaps are preserved. With no events, the newest
// snapshot is placed as if its save were 5 min before such a newest
// event. It returns the delta.
func shiftTimes(evs []logwatch.Event, snaps []*extract.Snapshot, now time.Time) time.Duration {
	var newest time.Time
	for _, e := range evs {
		if e.At.After(newest) {
			newest = e.At
		}
	}
	if newest.IsZero() {
		for _, s := range snaps {
			if t := s.SavedAt.Add(5 * time.Minute); t.After(newest) {
				newest = t
			}
		}
		if newest.IsZero() {
			return 0
		}
	}
	d := now.Add(-time.Minute).Sub(newest)
	for i := range evs {
		evs[i].At = evs[i].At.Add(d)
		if evs[i].Since != nil {
			s := evs[i].Since.Add(d)
			evs[i].Since = &s
		}
	}
	for _, s := range snaps {
		s.SavedAt = s.SavedAt.Add(d)
		s.ReadAt = s.ReadAt.Add(d)
	}
	return d
}

```

- [ ] **Step 4: Run the seed tests again**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./cmd/farsight-seed/`

Expected: `TestRunPostsSnapshotsInOrderWithOneShift` and the flag test pass; `TestFixtures` still FAILS: `history spans 3h0m0s, want at least 4 days` and `portals with an owner 0 (want 3), tames with a namer 0 (want 1)`. The fixtures come next.

- [ ] **Step 5: Seed history, a raid, owners and a namer**

In `web/tests/fixtures/snapshot.json`, add the owners and the namer (four edits):

In `web/tests/fixtures/snapshot.json`, `portal-1`, replace:

```json
      "z": -70.25,
      "label": "home",
```

with:

```json
      "z": -70.25,
      "label": "home",
      "owner": "Astrid",
```

In `web/tests/fixtures/snapshot.json`, `portal-3`, replace:

```json
      "z": -95.75,
      "label": "mountain",
```

with:

```json
      "z": -95.75,
      "label": "mountain",
      "owner": "Astrid",
```

In `web/tests/fixtures/snapshot.json`, `portal-5`, replace:

```json
      "z": -1200.5,
      "label": "copper"
```

with:

```json
      "z": -1200.5,
      "label": "copper",
      "owner": "Bjorn"
```

In `web/tests/fixtures/snapshot.json`, `tame-1`, replace:

```json
      "species": "Lox",
      "label": "Big Mama"
```

with:

```json
      "species": "Lox",
      "label": "Big Mama",
      "namer": "Steam_76561190000000001"
```

In `web/tests/fixtures/events.json`, at the top of the list: four days of earlier sessions, replace:

```json
  "events": [
    {"id": "demo-01",
```

with:

```json
  "events": [
    {"id": "demo-h1", "type": "player_join", "at": "2026-09-25T12:00:00Z", "name": "Sigrun", "platform": "Steam", "platformId": "76561190000000003"},
    {"id": "demo-h2", "type": "player_leave", "at": "2026-09-25T13:00:00Z", "name": "Sigrun", "platform": "Steam", "platformId": "76561190000000003", "since": "2026-09-25T12:00:00Z", "seconds": 3600, "reason": "left"},
    {"id": "demo-h3", "type": "player_join", "at": "2026-09-27T20:00:00Z", "name": "Astrid", "platform": "Steam", "platformId": "76561190000000001"},
    {"id": "demo-h4", "type": "player_leave", "at": "2026-09-27T22:30:00Z", "name": "Astrid", "platform": "Steam", "platformId": "76561190000000001", "since": "2026-09-27T20:00:00Z", "seconds": 9000, "reason": "left"},
    {"id": "demo-h5", "type": "player_join", "at": "2026-09-28T10:00:00Z", "name": "Ulf", "platform": "Steam", "platformId": "76561190000000004"},
    {"id": "demo-h6", "type": "player_leave", "at": "2026-09-28T11:30:00Z", "name": "Ulf", "platform": "Steam", "platformId": "76561190000000004", "since": "2026-09-28T10:00:00Z", "seconds": 5400, "reason": "left"},
    {"id": "demo-h7", "type": "player_join", "at": "2026-09-28T21:00:00Z", "name": "Astrid", "platform": "Steam", "platformId": "76561190000000001"},
    {"id": "demo-h8", "type": "player_leave", "at": "2026-09-28T23:00:00Z", "name": "Astrid", "platform": "Steam", "platformId": "76561190000000001", "since": "2026-09-28T21:00:00Z", "seconds": 7200, "reason": "left"},
    {"id": "demo-01",
```

In `web/tests/fixtures/events.json`, before `demo-10`: a raid, replace:

```json
    {"id": "demo-10", "type": "player_join",
```

with:

```json
    {"id": "demo-h9", "type": "event_raid", "at": "2026-09-29T19:20:00Z", "raid": "army_theelder"},
    {"id": "demo-10", "type": "player_join",
```

- [ ] **Step 6: Run the seed tests to make sure they pass**

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./cmd/farsight-seed/`

Expected: `ok`.

- [ ] **Step 7: Post two saves in the e2e setup**

In `web/tests/e2e/global-setup.ts`, in the header comment, replace:

```ts
 *  3. `farsight-seed -fake-tiles` writes a complete tile set before serve
 *     starts, so the snapshot post finds the map ready; serve starts, /healthz
 *     is awaited, then demo's snapshot and events are posted with -shift (the
 *     newest event lands at now − 1 min) and -explored (the snapshot carries
 *     a 12 m explored mask rasterised from its zones, as a current agent's).
```

with:

```ts
 *  3. `farsight-seed -fake-tiles` writes a complete tile set before serve
 *     starts, so the snapshot post finds the map ready; serve starts, /healthz
 *     is awaited, then demo's snapshots and events are posted with -shift (the
 *     newest event lands at now − 1 min) and -explored (each snapshot carries
 *     a 12 m explored mask rasterised from its zones, as a current agent's).
 *     demo gets two saves (Plan 7): `earlierSnapshot` (fixture minus Bjorn's
 *     tombstone, the copper portal, Big Mama, Eastwatch, 124 Longhouse pieces
 *     and Moder's key), a day before snapshot.json, so the server derives one
 *     world-save event of each kind from the pair.
```

In `web/tests/e2e/global-setup.ts`, after the constants, replace:

```ts
const HEALTH_TIMEOUT_MS = 20_000;
```

with:

```ts
const HEALTH_TIMEOUT_MS = 20_000;

interface FixtureSnapshot {
	saveId: string;
	savedAt: string;
	readAt: string;
	globalKeys: string[];
	bosses: { key: string; defeated: boolean }[];
	markers: { id: string }[];
	bases: { id: string; pieces: number }[];
}

/**
 * The save before snapshot.json (Plan 7): a day earlier, without Bjorn's
 * tombstone, the copper portal, Big Mama or the Eastwatch base, with 124
 * fewer Longhouse pieces and Moder not yet defeated. Diffing the two gives
 * a new tombstone, portal, tame and base, a base that grew, and a boss.
 */
function earlierSnapshot(fixture: string): FixtureSnapshot {
	const s = JSON.parse(readFileSync(fixture, 'utf8')) as FixtureSnapshot;
	const dayBefore = (iso: string) => new Date(new Date(iso).getTime() - 24 * 3600 * 1000).toISOString();
	return {
		...s,
		saveId: 'chunked:213',
		savedAt: dayBefore(s.savedAt),
		readAt: dayBefore(s.readAt),
		globalKeys: s.globalKeys.filter((k) => k !== 'defeated_dragon'),
		bosses: s.bosses.map((b) => (b.key === 'defeated_dragon' ? { ...b, defeated: false } : b)),
		markers: s.markers.filter((m) => !['tombstone-1', 'portal-5', 'tame-1'].includes(m.id)),
		bases: s.bases.filter((b) => b.id !== 'base-2').map((b) => (b.id === 'base-1' ? { ...b, pieces: b.pieces - 124 } : b))
	};
}
```

In `web/tests/e2e/global-setup.ts`, demo's config, replace:

```ts
					discordHint: 'ask in #demo',
					maxPlayers: 10,
```

with:

```ts
					discordHint: 'ask in #demo',
					maxPlayers: 10,
					timeZone: 'Europe/Oslo',
```

In `web/tests/e2e/global-setup.ts`, the demo seed, replace:

```ts
		run(seed, ['-url', base, '-server', 'demo', '-shift', '-explored', '-snapshot', snapshot, '-events', events]);
```

with:

```ts
		const earlier = join(dir, 'snapshot-earlier.json');
		writeFileSync(earlier, JSON.stringify(earlierSnapshot(snapshot)));
		run(seed, ['-url', base, '-server', 'demo', '-shift', '-explored', '-snapshot', earlier, '-snapshot', snapshot, '-events', events]);
```

- [ ] **Step 8: Write the e2e tests**

In `web/tests/e2e/desktop.spec.ts`, in test 4, replace:

```ts
	const activity = page.getByRole('region', { name: 'Recent activity' });
	await expect(activity).toContainText('Astrid joined');
	await expect(activity.getByRole('listitem').filter({ hasText: 'Autosave finished' })).toHaveCount(1);
});
```

with:

```ts
	const activity = page.getByRole('region', { name: 'Recent activity' });
	// Plan 7: the newest save's world events lead the list now.
	await expect(activity).toContainText('Sigrun joined');
	await expect(activity).toContainText('Moder defeated');
	await expect(activity.getByRole('listitem').filter({ hasText: 'Autosave finished' })).toHaveCount(1);
});
```

Append to the end of `web/tests/e2e/desktop.spec.ts`, after a blank line:

```ts
// --- Plan 7: player profiles and the activity timeline ----------------------

const profilePanel = (page: Page) => page.getByRole('complementary', { name: 'Player profile' });
const activityPanel = (page: Page) => page.getByRole('complementary', { name: 'Activity' });
const stat = (root: ReturnType<typeof profilePanel>, k: string) => root.locator('.stat', { hasText: k }).locator('dd');

test('profile · Online → Profile: Astrid’s figures from the seed, Map →, browser back', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	await page.getByRole('list', { name: 'Online now' }).getByRole('button', { name: 'Profile of Astrid' }).click();
	await expect(page).toHaveURL(/#s=demo&p=76561190000000001$/);
	const profile = profilePanel(page);
	await expect(profile.getByRole('heading', { name: 'Astrid' })).toBeVisible();
	await expect(profile.locator('.status')).toHaveText(/^Online now · 1h \d+m$/);
	// Three sessions: 2 h 30 m and 2 h on earlier days, and the open one.
	await expect(stat(profile, 'Sessions')).toHaveText('3');
	await expect(stat(profile, 'All time')).toHaveText(/^5h \d\dm$/);
	await expect(profile.getByRole('list', { name: 'Hours online per day' }).getByRole('listitem')).toHaveCount(7);
	await expect(profile.locator('.facts')).toContainText('1 · Longhouse');
	await expect(profile.getByRole('region', { name: 'Bases' })).toContainText('2,184 pieces · Meadows');
	// home and mountain are hers; mountain's partner is unexplored, so it shows unpaired.
	await expect(profile.getByRole('region', { name: 'Portals placed' })).toContainText('2 portals');
	await expect(profile.getByRole('region', { name: 'Tames they named' })).toContainText('Big Mama · Lox');
	await expect(profile.getByRole('region', { name: 'Deaths' })).toContainText('0 spotted · 0 this week');

	await profile.getByRole('region', { name: 'Bases' }).getByRole('button', { name: /Longhouse/ }).click();
	await expect(markerCard(page)).toContainText('Longhouse');

	await page.goBack();
	await expect(profile).toBeHidden();
	await expect(page).toHaveURL(/#s=demo$/);
	await expect(serverCard(page)).toBeVisible();
});

test('profile · Recently online → Ulf; Bjorn’s tombstone; a linked profile; an unknown player', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	await page.getByRole('list', { name: 'Recently online' }).getByRole('button', { name: 'Profile of Ulf' }).click();
	const profile = profilePanel(page);
	await expect(profile.locator('.status')).toHaveText(/^Last seen /);
	await expect(stat(profile, 'Sessions')).toHaveText('2');
	await expect(stat(profile, 'All time')).toHaveText('2h 10m');
	await expect(profile.getByRole('region', { name: 'Bases' })).toContainText('Eastwatch');
	await expect(profile.getByRole('region', { name: 'Portals placed' })).toContainText('None yet.');
	await profile.getByRole('button', { name: 'Back' }).click();
	await expect(profile).toBeHidden();
	await expect(page).toHaveURL(/#s=demo$/);

	await page.getByRole('list', { name: 'Online now' }).getByRole('button', { name: 'Profile of Bjorn' }).click();
	const deaths = profilePanel(page).getByRole('region', { name: 'Deaths' });
	await expect(deaths).toContainText('1 spotted · 1 this week');
	await deaths.getByRole('button', { name: /Tombstone in the Meadows · since save/ }).click();
	await expect(markerCard(page)).toContainText('Bjorn');

	// A linked profile opens on load; an unknown player says so.
	await page.goto('about:blank');
	await page.goto('/#s=demo&p=76561190000000004');
	await expect(profilePanel(page).getByRole('heading', { name: 'Ulf' })).toBeVisible();
	await page.goto('about:blank');
	await page.goto('/#s=demo&p=nobody');
	await expect(profilePanel(page)).toContainText('This player hasn’t been seen on this server.');
});

test('activity · Full timeline: world-save events, chips, people, the short list follows, back', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	await page.getByRole('region', { name: 'Recent activity' }).getByRole('button', { name: 'Full timeline →' }).click();
	await expect(page).toHaveURL(/#s=demo&activity$/);
	const tl = activityPanel(page);
	for (const text of [
		'New tombstone: Bjorn, in the Meadows',
		'New portal “copper”, not paired with anything yet',
		'New tame: Big Mama (Lox)',
		'New base: Eastwatch (Plains)',
		'Longhouse grew by 124 pieces',
		'Moder defeated',
		'Raid: The forest is moving',
		'Sigrun joined'
	]) {
		await expect(tl).toContainText(text);
	}
	await expect(tl.getByRole('listitem').filter({ hasText: 'Moder defeated' })).toContainText(/World save · \d\d:\d\d/);
	await expect(tl.getByRole('listitem').filter({ hasText: 'Autosave finished' }).first()).toContainText('Server log');
	const today = tl.locator('.today-row');
	for (const name of ['Astrid', 'Bjorn', 'Sigrun']) await expect(today.filter({ hasText: name })).toHaveCount(1);

	const deaths = tl.getByRole('button', { name: /^Deaths/ });
	await expect(deaths).toHaveAttribute('aria-pressed', 'true');
	await expect(deaths.locator('.count')).toHaveText('1');
	await deaths.click();
	await expect(deaths).toHaveAttribute('aria-pressed', 'false');
	await expect(tl).not.toContainText('New tombstone');

	await tl.getByRole('group', { name: 'People' }).getByRole('button', { name: /Ulf/ }).click();
	await expect(tl).toContainText('Ulf left after 40m');
	await expect(tl).not.toContainText('Sigrun joined');
	await expect(tl).toContainText('Autosave finished'); // the server's own events stay
	await tl.getByRole('button', { name: 'Reset filters' }).click();
	await expect(tl).toContainText('New tombstone');
	await expect(tl).toContainText('Sigrun joined');

	// The side panel's short list shares the filters.
	await deaths.click();
	await tl.getByRole('button', { name: 'Back' }).click();
	await expect(tl).toBeHidden();
	await expect(page).toHaveURL(/#s=demo$/);
	const short = page.getByRole('region', { name: 'Recent activity' });
	await expect(short).toContainText('Moder defeated');
	await expect(short).not.toContainText('New tombstone');
});

test('activity · Show earlier reaches the start of tracking; Show on map →', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	await page.getByRole('region', { name: 'Recent activity' }).getByRole('button', { name: 'Full timeline →' }).click();
	const tl = activityPanel(page);
	await expect(tl).toContainText('Moder defeated');
	await expect(tl).not.toContainText('Sigrun left after 1h 00m');
	await tl.getByRole('button', { name: 'Show earlier' }).click();
	await expect(tl).toContainText('Sigrun left after 1h 00m');
	await expect(tl).toContainText(/Tracking began \d+ [A-Z][a-z]{2}/);
	await expect(tl.getByRole('button', { name: 'Show earlier' })).toHaveCount(0);
	await expect(tl.getByText(/^Last 6 days/)).toBeVisible();

	await tl.getByRole('listitem').filter({ hasText: 'New tombstone: Bjorn' }).getByRole('button', { name: 'Show on map →' }).click();
	await expect(markerCard(page)).toContainText('Bjorn');
	await expect(tl).toBeVisible();
});
```

Append to the end of `web/tests/e2e/mobile.spec.ts`, after a blank line:

```ts
// --- Plan 7: player profiles and the activity timeline ----------------------

test('profile · pulled sheet → Profile: the full-height sheet; Map → docks the card; back closes', async ({ page }) => {
	await unlock(page);
	// A tap, not a drag: a tap right after a touch drag can land as the
	// fling-stopping tap, which fires no click.
	await handle(page).tap();
	await expect(pulled(page)).toBeVisible();
	await pulled(page).getByRole('button', { name: 'Profile of Astrid' }).tap();
	const sheet = page.getByRole('dialog', { name: 'Player profile' });
	await expect(sheet.getByRole('heading', { name: 'Astrid' })).toBeVisible();
	await expect(sheet.locator('.stat', { hasText: 'Sessions' }).locator('dd')).toHaveText('3');
	await expect(page).toHaveURL(/#s=demo&p=76561190000000001$/);
	await page.goBack();
	await expect(sheet).toBeHidden();

	await pulled(page).getByRole('button', { name: 'Profile of Astrid' }).tap();
	await sheet.getByRole('region', { name: 'Bases' }).getByRole('button', { name: /Longhouse/ }).tap();
	await expect(sheet).toBeHidden();
	await expect(page.getByTestId('mobile-marker-card')).toContainText('Longhouse');
	await expect(page).toHaveURL(/#s=demo$/);
});

test('activity · menu → Full timeline: chips, Show earlier, Show on map →', async ({ page }) => {
	await unlock(page);
	await page.getByRole('button', { name: 'Search and layers' }).tap();
	await page.getByRole('dialog', { name: 'Search and layers' }).getByRole('button', { name: /Full timeline/ }).tap();
	const sheet = page.getByRole('dialog', { name: 'Activity' });
	await expect(sheet).toContainText('Moder defeated');
	await expect(page).toHaveURL(/#s=demo&activity$/);
	const bosses = sheet.getByRole('button', { name: /^Bosses/ });
	await bosses.tap();
	await expect(sheet).not.toContainText('Moder defeated');
	await bosses.tap();
	await sheet.getByRole('button', { name: 'Show earlier' }).tap();
	await expect(sheet).toContainText('Sigrun left after 1h 00m');

	await sheet.getByRole('listitem').filter({ hasText: 'New portal “copper”' }).getByRole('button', { name: 'Show on map →' }).tap();
	await expect(sheet).toBeHidden();
	await expect(page.getByTestId('mobile-marker-card')).toContainText('copper');
});
```

- [ ] **Step 9: Run the whole end-to-end suite**

Run: `cd web && PATH=$HOME/.local/go/bin:$PATH LD_LIBRARY_PATH="$(../hack/playwright-libs.sh --print)" FONTCONFIG_FILE="$(../hack/playwright-libs.sh --fontconfig)" npx playwright test`

Expected: `48 passed` (42 existing, 4 desktop and 2 mobile new). For flakiness, also run `npx playwright test --grep "profile|activity|online tab" --repeat-each 6` with the same environment: `42 passed`.

- [ ] **Step 10: Commit the seed, fixtures and e2e tests**

```bash
cd /workspace/Farsight
git add cmd/farsight-seed/main.go \
  cmd/farsight-seed/main_test.go \
  web/tests/fixtures/snapshot.json \
  web/tests/fixtures/events.json \
  web/tests/e2e/global-setup.ts \
  web/tests/e2e/desktop.spec.ts \
  web/tests/e2e/mobile.spec.ts
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
test(e2e): seeded history and two saves; profile and timeline e2e

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
git status --short   # must print nothing
```

- [ ] **Step 11: Docs: README, design notes, spec implementation notes**

In `README.md`, replace:

```markdown
- **Server card.** Shows who's online and who was recently online, recent activity, the in-game day, bosses defeated, world rules, and the live crossplay join code.
```

with:

```markdown
- **Server card.** Shows who's online and who was recently online, recent activity, the in-game day, bosses defeated, world rules, and the live crossplay join code.
- **Player profiles.** Playtime this week and since tracking began, the last 7 days hour by hour, and each player's bases, beds, portals, named tames and deaths, linked from the save by who placed or named them.
- **Activity timeline.** Joins, leaves, restarts and raids from the server log, plus what changed between world saves: new tombstones, portals, tames and bases, bases that grew and bosses defeated. Filter it by kind and by player, back to when tracking began.
```

In `README.md`, replace:

```markdown
Without the `webui` build tag, `farsight` serves a placeholder page instead of the UI. Container images are built from `Dockerfile` and `Dockerfile.agent`.
```

with:

```markdown
Without the `webui` build tag, `farsight` serves a placeholder page instead of the UI. Container images are built from `Dockerfile` and `Dockerfile.agent`.

Each server in `farsight.json` may set `timeZone`, an IANA name such as `"Europe/Oslo"` (default `"UTC"`). Set it to the same zone as that server's agent's `FARSIGHT_LOG_TZ`: profiles and the activity timeline count days in it.
```

In `design/DESIGN-NOTES.md`, §1.5, replace:

```markdown
`:837-908`. It has a desktop 520-px aside and a mobile full-height sheet, plus the "Where each entry comes from" note. It is not built in the MVP. In the MVP, the "Full timeline →" links in the side panel and the mobile sheet **must be omitted**.
```

with:

```markdown
`:837-908`. It has a desktop 520-px aside and a mobile full-height sheet, plus the "Where each entry comes from" note. It is not built in the MVP. In the MVP, the "Full timeline →" links in the side panel and the mobile sheet **must be omitted**.

**Built in Plan 7** (`docs/superpowers/specs/2026-10-05-activity-timeline-design.md`): "Full timeline →" is back in the side panel's Recent activity, and on mobile the menu sheet has a "Full timeline" row.
```

In `design/DESIGN-NOTES.md`, §1.8, replace:

```markdown
`:636-745`. It contains the mobile player profile, the time and weather dropdown, and the **world rules card**. The world rules card is reused in the World tab and the join dialog, so **it is in the MVP** (spec 06: "world rules card"). Also out: the night map tint (`nightFilter`), the time pill and the "Profile →" links.
```

with:

```markdown
`:636-745`. It contains the mobile player profile, the time and weather dropdown, and the **world rules card**. The world rules card is reused in the World tab and the join dialog, so **it is in the MVP** (spec 06: "world rules card"). Also out: the night map tint (`nightFilter`), the time pill and the "Profile →" links.

**Profiles built in Plan 7** (`docs/superpowers/specs/2026-10-05-player-profile-design.md`): "Profile →" is on Online and Recently online rows, desktop and mobile. The time and weather pieces stay out.
```

Append to the end of `docs/superpowers/specs/2026-10-05-player-profile-design.md`, after a blank line:

```markdown
## Implementation notes (Plan 7)

Decisions the plan made where this spec left room:

- **Player id:** the sessions' `platformId` (no platform prefix) in the URL
  (`#p=`) and the API. A tame's namer (`TamedNameAuthor`, "Steam_…") is
  matched as `platform + "_" + platformId`; "host" names nobody.
- **Names:** save data links by every name the platform ID has played under,
  not only the latest.
- **Time zone:** each server in the config gets `timeZone` (IANA, default
  `UTC`), set to its agent's `FARSIGHT_LOG_TZ`; the app can't read the
  agent's environment.
- **"This week"** is the chart's seven local days (today and the six before),
  not a rolling 168 hours, so the tile and the bars agree. Deaths "this week"
  use the same days.
- **Explored only:** beds, bases, portals, tames and current tombstones are
  filtered to explored ground, like the snapshot API. A portal shows as
  paired only when its partner is explored. "N spotted" counts every
  tombstone.
- **Tombstone history** is kept incrementally: the world-save diff that
  writes timeline events also records every distinct tombstone, with the
  save it first appeared in, in a `tombstones` table. Stored snapshots are
  pruned after 14 days; the table isn't.
- **Beds:** the nearest explored base within 300 m names a bed, otherwise
  its biome.
- **Tracked since** is the server's earliest stored event (the first session
  if there is none), shown as a note under first and last seen.
```

Append to the end of `docs/superpowers/specs/2026-10-05-activity-timeline-design.md`, after a blank line:

```markdown
## Implementation notes (Plan 7)

Decisions the plan made where this spec left room:

- **Matching across saves:** marker and base ids are per-save rankings, so
  tombstones and portals match by owner or tag within 4 m, bases by the
  previous base whose centre is nearest (within its radius plus 32 m), and
  tames by counting (species, name): tames walk about. Unnamed tames (bred
  or freshly tamed animals) aren't reported. Owners and namers aren't part
  of any match, so the agent update that adds them reports nothing.
- **Baseline:** the first stored save of a server yields no events; what is
  in it predates tracking.
- **Portals:** a new portal's event says whether it is paired already;
  "now paired" is only for two existing portals that pair, so one new pair
  is reported once.
- **Retention:** only heartbeat and players_now events are pruned after 14
  days now; everything else stays, so the timeline reaches back to when
  tracking began.
- **Paging** is by the server's local days: without `before` the window ends
  at the end of today; the response's `from` is the next page's `before`.
  "Tracking began" shows once `from` reaches `earliest`.
- **Autosaves:** a run of autosaves with nothing between them is collapsed
  to its newest, in the API, before counting.
- **People** chips list every player the server has seen (online first) and
  are multi-select ("Everyone" clears them). Chip counts and "Who was on
  today" follow the people filter; the category chips don't change counts.
- **Raids:** the event names mapped to messages are the ones known from the
  game; anything else shows as its raw name ("Raid: army_gjall"). A raid's
  event id leaves the name out, so no other event's id changed.
- **Derivation** runs inside snapshot ingest (a failure is logged, not
  returned to the agent) and once per server at startup.
- **The card's activity** uses the timeline's entry shape, so the side
  panel's short list follows the same filters; its world-save rows show
  "save HH:MM".
```

- [ ] **Step 12: Final verification**

From the repo root:

Run: `PATH=$HOME/.local/go/bin:$PATH gofmt -l ./cmd ./internal ./web && PATH=$HOME/.local/go/bin:$PATH go vet ./... && PATH=$HOME/.local/go/bin:$PATH go test ./... && (cd web && npm run check && npx vitest run)`

Expected: `gofmt -l` prints nothing, `go vet` is quiet, every Go package `ok` (golden tests run when `testdata-golden/` is present), svelte-check 0 errors and 0 warnings, all 22 Vitest files pass.

- [ ] **Step 13: Commit the docs**

```bash
cd /workspace/Farsight
git add README.md \
  design/DESIGN-NOTES.md \
  docs/superpowers/specs/2026-10-05-player-profile-design.md \
  docs/superpowers/specs/2026-10-05-activity-timeline-design.md
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
docs: player profiles and the activity timeline: README, design notes, implementation notes

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
git status --short   # must print nothing
```

Then check the branch: `git status --short` is clean (no `reference/` or `testdata-golden/` ever staged), and `git log --oneline public-main..HEAD` shows the specs commit followed by the ten Plan 7 commits (Tasks 1–8, then two for Task 9). Nothing is pushed.

---


---

## Self-Review

**How this plan was checked.** Every step was executed mechanically against a fresh clone of `feat/profile-timeline`, outside the repo:
- Each step's edits were applied (every `replace` matched its text exactly once).
- Each "make sure it fails" run was required to fail with the quoted error, and each "make sure it passes" run to pass.
- Each commit's `git add` list was required to leave `git status --short` empty.
- `gofmt -l` was required to be empty after every task.

The final tree was byte-identical to the working copy in which the code had been built and tested. There:
- `go vet ./...` and `go test ./...` were all `ok`, with the golden tests running against `testdata-golden/`.
- svelte-check reported 0 errors and 0 warnings; Vitest passed 22 files (392 tests).
- Playwright: `42 passed` after Tasks 7 and 8, `48 passed` after Task 9. `--repeat-each 6` of the new and changed e2e tests: `42 passed`.

The golden count (16 of 16 portals with an owner, 6 of 12 tames with a namer on MuleVikings) and the biomes in the e2e expectations (Meadows, Plains, Black Forest at seed 12345) are measured values from that run.

**1. Spec coverage**

| Spec requirement | Task |
|---|---|
| Profile: player = platform ID from the log, shown under the latest name | 3 (`Players`), 5 (`PlayerSessions`, last session's name) |
| Beds and tombstones by owner name; bases by builder; portals by creator → name; tames by namer platform ID; shared names link to both | 1 (owner, namer), 5 (`addSaveData`, every name played under) |
| History counts from tracking start; "tracked since" note; nothing estimated | 3 (`EarliestEvent`), 5 (`trackedSince`), 7 (`v.tracked` note) |
| Status, This week, All time, Sessions, Last 7 days (local days, midnight split, open session), first/last seen | 5 (`playtime.go`, tests across midnight, DST and an open session), 7 (`profileView`, `dayBars`) |
| Beds (count, nearest base), Bases (name, pieces, biome, position), Portals placed, Tames they named | 5, 7 |
| Deaths: distinct tombstones across saves, this week, current ones with the save they appeared in | 3 (`tombstones` table), 4 (written by the diff), 5, 7 |
| Profile API, unlock check, 404s; tombstone history computed once per new save and kept per server | 5 (`TestProfileNotFound`), 4 (incremental per save) |
| Agent: portal `owner`, tame `namer`; app first, "None yet" until then | 1, 7 (empty sections), Global Constraints (deploy order) |
| "Profile →" on Online and Recently online rows (ghost, body font, `--cold-ink`) | 7 (`PlayersTab`, `PeekSheet`) |
| Desktop: replaces the panel (344 px) with a back arrow; mobile: full-height sheet "‹ PLAYER" | 7 (`ProfilePanel`, `ProfileSheet`) |
| `#p=<playerId>`; browser back closes; linkable | 7 (`openView`, `closeView`, hash sync; e2e `goBack`, linked profile) |
| Layout: avatar with dot, stat tiles, 7-day chart (today in accent), facts, sections, footnote | 7 (`ProfileContent`) |
| "Map →" centres (closing the sheet on mobile); loading skeleton, not found, "None yet." | 7, 9 (e2e) |
| Timeline events: joins and leaves, server, deaths, portals (new, paired later), tames, building (new, grew ≥ 25), bosses, raids; texts | 2, 4, 6, 8 (`eventText`, table in Task 8) |
| Server-log events: exact time, "Server log"; world-save events: save time, "World save · 14:20", "Show on map →" when explored | 6 (`source`, x/z only when explored), 8 (`sourceText`, the button) |
| Place wording: nearest known location within 300 m, the biome, else the biome alone | 4 (`Near`, `NewGeo`), 8 (`place`) |
| Each event records the players it concerns | 4 (owner, namer, builders), 6 (`who`) |
| Diff each new snapshot against the previous one into `events`, with derived ids; idempotent | 4 (`Diff`, `eventID`, `Deriver`; `TestCatchUpConcurrentRunsWriteEachEventOnce`) |
| Backfill on start when not derived yet, replaying all stored snapshots in order once | 4 (`backfillWorldEvents`, `world_diff`; `TestBackfillWorldEventsOnStartup`) |
| Bases matched across saves; growth under 25 not reported | 4 (`bases`, `TestDiffBases`) |
| Raids: `Random event set:<name>` → `event_raid`, in the agent update | 2 |
| Activity API: `before`, `days=3`, newest first, counts per category, `earliest`; sessions/today in local time; unlock check | 6 |
| "Full timeline →" (desktop panel), main menu "Full timeline" (mobile); `#activity`; back closes; the short list follows the filters | 8 |
| Header "Activity", "Last 3 days · from the server log and world saves", "Reset filters" | 8 (`ActivityPanel`; the line grows to "Last 6 days" after "Show earlier") |
| Who was on today: 00–24 axis, current orange, earlier grey, "Now · HH:MM", legend | 6 (`todayPlayers`), 8 (`todayRows`) |
| Eight chips with counts; People: Everyone and a chip per player; server events stay | 8 (`passes`, `chipCounts`) |
| Day groups "Today · Tue 29 Sep · in-game day 214", "Yesterday …"; time, icon, text, source | 8 (`groupByDay`) |
| End of list: "Show earlier" or "Tracking began 30 Sep"; "Nothing matches these filters"; footnote | 6 (`from`/`earliest`), 8 |
| Testing (Go): save-diff rules, unchanged save, later pairing, growth under 25, idempotent backfill, paging and counts, today across midnight and open, raid parsing | 2, 4, 6 |
| Testing (web): Vitest for formatting, chart model, chips, people, day grouping; Playwright desktop and mobile | 7, 8, 9 |
| Rollout: app first, agent later | Global Constraints |

**2. Placeholder scan.** No "TBD", "TODO", "similar to Task N" or "add error handling" remains. Every code step is either a whole file or an exact `replace` that matches once. The only free text in commands is the environment prefix spelled out in Global Constraints.

**3. Type and name consistency (checked across tasks):**

- **Agent:** `extract.Marker.Namer`/`Owner` (T1 → T4 diff, T5 profile, T9 fixtures `namer`, `owner`; TS `Marker.namer` T7). `logwatch.EvRaid` = `"event_raid"`, `Event.Raid` (T2 → T6 `eventJSON.Raid` → TS `Activity.raid` T8).
- **Store:**
  - `store.StoredEvent`, `InsertRawEvent`, `RecentEvents`, `EventsBetween`, `EarliestEvent` (T3 → T4, T5, T6).
  - `PlayerSessions`, `SessionsOverlapping`, `Players`/`Player.Names` (T3 → T5, T6).
  - `SnapshotKey`, `WorldDiffState`, `PutWorldDiffState`, `SnapshotsAfter`, `Snapshot`, `Tombstone`, `InsertTombstone`, `Tombstones` (T3 → T4, T5).
- **worldevents:** `TypeTombstone` … `TypeBoss`, `MatchRadius`, `NearRadius`, `Event`, `NewGeo`, `MaskOf`, `Diff`, `NewTombstones`, `Near`, `NewDeriver`, `CatchUp` (T4 → T5, T6, serve).
- **Server:**
  - `config.Server.Location` (T5 → T6).
  - `localDays`, `dayTotals`, `totalSeconds`, `mustTime` (T5 → T6).
  - `eventJSON` fields = TS `Activity` (T6 → T8). The profile JSON = TS `Profile` (T5 → T7). Activity JSON = `ActivityPage`; today JSON = `TodaySessions` (T6 → T8).
- **Browser:**
  - `View`, `hashFor(server, view?)`, `sameView`, `app.openView`/`closeView` (T7 → T8).
  - `zoned.ts` exports (T7 → T8).
  - `recentList` `platformId` (T7).
  - `activityRows(card, limit, off, people)` → `ActivityRow.source` (T8 → `ActivityList`).
  - `markerAt` (T8 → shells).
  - Accessible names used by the e2e tests (T9) match the components (T7, T8): "Profile of {name}", "Full timeline →", "Player profile", "Activity", "People", "Show on map →".
