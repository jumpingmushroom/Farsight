# Plan 9: Time of day and weather

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The design's time-and-weather pill and dropdown (section 08), with the night map tint, from an estimated live world clock and the game's own weather schedule.

**Architecture:** The log watcher turns the server's sleep line into a `time_skip` event. The central app estimates netTime now from the newest anchor (save or sleep) plus the seconds someone was online since, and adds `clock` and `weather` (three 666 s periods × explored biomes, computed by a new `internal/weather` package) to the server card. The web client ticks the clock locally and renders the pill, dropdown and tint.

**Tech Stack:** Go, SvelteKit 5, Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-10-05-time-and-weather-design.md`. Depends on Plan 8 (`internal/biomegrid`, merged first).

## Global Constraints

- Day 1800 s; weather period 666 s; `period = floor(floor(netTime)/666)`; draw with `worldgen.NewURandom(int32(period)).RangeFloat(0, total)` over non-override entries, float32 running sums, first with sum ≥ draw.
- Night = raw `f ≤ 0.15 || f ≥ 0.85`. Clock = rescaled fraction × 24 h: raw<0.15 → `f/0.15*0.25`; 0.15–0.85 → `0.25+(f-0.15)/0.7*0.5`; >0.85 → `0.75+(f-0.85)/0.15*0.25`.
- Labels by clock: Morning 06:00–11:00, Day 11:00–16:00, Evening 16:00–18:00, Night 18:00–06:00. Next text: "Midday in ~N min" / "Evening in ~N min" / "Night in ~N min" / "Dawn in ~N min".
- Sleep line regex on the log message: `^Time ([0-9.]+), day:(\d+)\s+nextm:([0-9.]+)`; anchor `(nextm, lineTime + 12 s)`.
- Card JSON: `clock {netTime, at, running, source: "save"|"sleep"}`, `weather {periodSec: 666, periods: [{start, byBiome}]×3, biomes: [...], home}`; both omitted when there is no snapshot or the server is offline.
- Explored biomes: ≥ 50 explored 12 m cells; legend order Ocean, Meadows, Black Forest, Swamp, Mountains, Plains, Mistlands, Ashlands, Deep North. Home = biome under the base with most pieces, else Meadows.
- Tint: Night `brightness(.8) saturate(.8) hue-rotate(-12deg)`, Evening `brightness(.94) sepia(.15)`, appended to the existing tile filter.
- Copy (exact): paused line suffix " · paused"; dropdown paused text "Time is paused while nobody is online."; footer "Estimated from the world clock in the last save{ and the last sleep}. The world clock only runs while someone is online. Weather follows the game's own schedule; raids, dungeons and the Dark Meadows have their own."
- Commits end with `Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T`.

Reference files (session scratchpad, copy into the repo where a task says so): weather data `/tmp/claude-99/-workspace-Farsight/fb0e5307-32a1-4ab3-87cf-a9ddd5d7e9fa/scratchpad/weather-data.keep.json`; golden table `/tmp/claude-99/-workspace-Farsight/fb0e5307-32a1-4ab3-87cf-a9ddd5d7e9fa/scratchpad/weather-golden.tsv` (period, biome key, env name; 1080 rows from the spike's calculator).

---

### Task 1: `internal/weather`

**Files:** Create `internal/weather/weather.go`, `internal/weather/names.go`, `internal/weather/weather_test.go`, `internal/weather/testdata/golden.tsv` (copy of the golden table).

**Interfaces:**
- Produces: `const PeriodSec = 666`, `const DaySec = 1800`; `func Period(netTime float64) int64`; `func At(period int64, b worldgen.Biome) string` (display name; "" for biomes with no entries, e.g. 0); `func EnvAt(period int64, b worldgen.Biome) string` (internal env name, for the golden test).

- [ ] **Step 1: Test.** `weather_test.go` reads `testdata/golden.tsv` and asserts `EnvAt(period, biomeOf(key)) == env` for every row (keys: Meadows, BlackForest, Swamp, Mountain, Plains, Ocean, Mistlands, AshLands, DeepNorth → worldgen constants). Plus: `Period(665.9) == 0`, `Period(666) == 1`, `Period(1331.99) == 1`; `At(p, worldgen.Swamp) == "Rain"` for any p; every env name reachable from the table has a display name (iterate entries, assert `displayName[env] != ""`).
- [ ] **Step 2: Run** `go test ./internal/weather` — FAIL.
- [ ] **Step 3: Implement.** A Go table (not embedded JSON) with only the non-override entries, weights as float32 exactly as the JSON (`0.2` → `float32(0.2)`), in the JSON's order:

```go
type entry struct {
	env    string
	weight float32
}

var biomeEntries = map[worldgen.Biome][]entry{
	worldgen.Meadows:     {{"Clear", 5}, {"Rain", 0.2}, {"Misty", 0.2}, {"ThunderStorm", 0.2}, {"LightRain", 0.2}},
	// … every biome from weather-data.keep.json "biomes", skipping entries with ashlandsOverride/deepnorthOverride
}

func EnvAt(period int64, b worldgen.Biome) string {
	es := biomeEntries[b]
	if len(es) == 0 {
		return ""
	}
	var total float32
	for _, e := range es {
		total += e.weight
	}
	draw := worldgen.NewURandom(int32(period)).RangeFloat(0, total)
	var sum float32
	for _, e := range es {
		sum += e.weight
		if sum >= draw {
			return e.env
		}
	}
	return es[len(es)-1].env
}
```

Check `RangeFloat`'s argument order against the spike note (`t*(min-max)+max`) and against the golden test — the golden table is the authority. `names.go`: env → display name, from the JSON's `environments[].displayName`, adjusted per spec: "Clear (forest mist)" → "Forest mist", SwampRain → "Rain", Snow → "Snow", SnowStorm → "Blizzard", Twilight_Snow → "Snow", Twilight_SnowStorm → "Blizzard", Twilight_Clear → "Clear", Ashlands_storm → "Ash storm". `Period` = `int64(math.Floor(math.Floor(netTime) / PeriodSec))`.
- [ ] **Step 4: Run** — PASS. **Step 5: Commit** `feat(weather): the game's weather schedule per biome`.

---

### Task 2: The sleep line as a `time_skip` event

**Files:** Modify `internal/logwatch/parse.go` (regex + `RawTimeSkip`), `internal/logwatch/session.go` (`EvTimeSkip = "time_skip"`, `Event.To float64 \`json:"to,omitempty"\``, emit), `internal/live/apply.go` (known type; it must be stored as an event but change no live state), tests in `parse_test.go`, `session_test.go`, `internal/live/apply_test.go`. Check `internal/server/activity.go`: `time_skip` has no category and must not appear in the card's activity or the timeline (add a test).

**Interfaces:** Produces stored events of type `time_skip` with `At` = line time and `To` = nextm.

- [ ] **Step 1: Tests.** Parse `09/29/2026 00:52:14: Time 487653.364537966, day:270    nextm:488070.000010729  skipspeed:34.7196227302775` with loc Europe/Oslo → one Raw{Kind: RawTimeSkip, At: 2026-09-28T22:52:14Z, To: 488070.000010729}; the Sessionizer turns it into an Event{Type: "time_skip", To: …}; the event ID is stable (follow how EvRaid builds IDs); `live.Apply` accepts it and stores it; the activity feed hides it.
- [ ] **Step 2: Run** — FAIL. **Step 3: Implement** following the `RawRaid` path exactly. **Step 4: Run** `go test ./internal/logwatch ./internal/live ./internal/server` — PASS. **Step 5: Commit** `feat(logwatch): the sleep line as a time_skip event`.

---

### Task 3: Clock estimate and weather on the card

**Files:** Create `internal/server/clock.go`, `internal/server/clock_test.go`. Modify `internal/server/api.go` (`cardJSONOut.Clock *clockJSON`, `.Weather *weatherJSON`), `internal/server/world.go` (lazy explored-biome list + home), `internal/biomegrid/biomegrid.go` (`Cache.Grid(seed, gen) []byte` returning the raw grid, kept alongside the gzip bytes; `func Lookup(grid []byte, x, z float64) byte`), `internal/store/events.go` (`LatestEventOfType(ctx, serverID, typ string) (StoredEvent, bool, error)`), tests.

**Interfaces:**
- Consumes: `weather.Period/At`, `biomegrid.Cache`, `store.SessionsOverlapping`, `store.Online`, stored `time_skip` events.
- Produces: `func estimateClock(anchorT float64, anchorAt, now time.Time, online []interval, openNow bool) (netTime float64)` (pure); card fields per Global Constraints.

- [ ] **Step 1: Tests** (`clock_test.go`, table-driven, pure function): no online time → netTime = anchor; two sessions overlapping each other count once; a session that started before the anchor counts only from the anchor; an open session counts until now; gaps with nobody online don't count. Card tests (existing card-test helpers): with a snapshot (netTime 493470, savedAt T) and a player online since T, at T+60 s the card has `clock.netTime ≈ 493530`, `running: true`, `source: "save"`; a `time_skip` event at T+30 s with To = 495270 gives `source: "sleep"`, netTime = 495270 + (now − (T+42 s)); a `time_skip` older than the save is ignored; offline server → no clock/weather. Weather: `periods` has 3 entries with starts `p*666`, `(p+1)*666`, `(p+2)*666` and `byBiome` keyed by display biome name for exactly `biomes`; `biomes` excludes a biome with < 50 explored cells; `home` is the biggest base's biome.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement.** Anchor: `snap.World.NetTime` at `snap.SavedAt`; the latest `time_skip` with `At + 12 s` after `SavedAt` wins (`To`, `At + 12 s`). Online intervals from `SessionsOverlapping(anchorAt, now)` (open sessions: until now), merged. `running` = `len(Online) > 0`. Explored biomes: iterate the explored mask's set cells (12 m), map each cell centre to the biome grid with `biomegrid.Lookup`, count per index, keep ≥ 50, order per Global Constraints; computed once per worldState (sync.Once). Biome display names via `worldevents.BiomeName`. Clock and weather are omitted when status is offline or there's no snapshot.
- [ ] **Step 4: Run** `go test ./...` — PASS. **Step 5: Commit** `feat(server): the world clock estimate and weather on the card`.

---

### Task 4: Time model (web, pure)

**Files:** Create `web/src/lib/worldtime.ts`, `web/src/lib/worldtime.test.ts`; modify `web/src/lib/types.ts` (Card `clock?`, `weather?`), `web/src/lib/derive.ts` (`mapFilter` gains a `tint: 'night' | 'evening' | undefined` argument appended last).

**Interfaces:** Produces `netTimeNow(clock, now: Date): number`; `dayFraction(t): number` (raw); `clockFraction(raw): number` (rescaled); `clockText(t): "HH:MM"`; `phase(t): 'morning'|'day'|'evening'|'night'`; `nextText(t): string`; `dayOf(t): number`; `periodIndex(weather, t): 0|1|2|-1`; `tintOf(phase)`.

- [ ] **Step 1: Tests** at boundaries: raw 0 → 00:00 night "Dawn in ~5 min" (270 s → rounds up to 5); raw 0.15 → 06:00 morning; raw 0.5 → 12:00 day "Evening in ~…"; raw 0.85 → 18:00 night; `netTimeNow` adds elapsed seconds only when running; `periodIndex` −1 past the third period; `mapFilter(true, undefined, 'night')` ends with the night filter.
- [ ] **Step 2: Run** — FAIL. **Step 3: Implement.** **Step 4: Run** `npx vitest run && npx svelte-check` — PASS. **Step 5: Commit** `feat(web): the world-time model`.

---

### Task 5: Pill, dropdown, mobile sheet, tint (web)

**Files:** Create `web/src/lib/components/WeatherPill.svelte`, `WeatherContent.svelte` (dropdown body, shared with mobile), `WeatherSheet.svelte`; modify `DesktopShell.svelte` (pill at `top:106px; left:{pillL}`, below `MapUpdatedPill`; 1 s ticker while running), `MobileShell.svelte` / `MobileTopBar.svelte` (chip + sheet), the tile filter call sites of `mapFilter`. Icons `sun`, `moon` from lucide-svelte (the UI already uses lucide-svelte components elsewhere — follow that).

- [ ] **Step 1: Tests** (Vitest component tests, following existing component tests if any; otherwise test `WeatherContent`'s view-model function): table rows are the card's `biomes` with legend swatches; Next/Then headers show "from HH:MM" (local wall time) when running and "after Now" when paused; paused line and text are the exact copy; footer includes " and the last sleep" only when `source === 'sleep'`; the pill is absent when the card has no clock.
- [ ] **Step 2: Run** — FAIL. **Step 3: Implement** to the design (`design/World Atlas.dc.html :309-336`, DESIGN-NOTES §1.8): button pill with 34 px disc, title 14 px bold, line 12 px muted, chevron; dropdown 360 px, radius 26, progress bar with the design's gradient and the 4 px marker at the raw fraction, hour ticks; `aria-expanded`; Escape and outside click close it. **Step 4: Run** `npx vitest run && npx svelte-check` — PASS. **Step 5: Commit** `feat(web): the time and weather pill`.

---

### Task 6: e2e, docs

- [ ] e2e seeds a snapshot with a known netTime and an online player (check `web/tests/e2e/global-setup.ts` for how events and snapshots are ingested; add a player_join so the clock runs) → the pill shows a label and clock; opening it lists the seeded explored biomes; mobile chip opens the sheet.
- [ ] README (time and weather; limitations: estimate, Dark Meadows/raids), DESIGN-NOTES §1.8 (time and weather now built), runbook note: deploy the central app before the agent (agents' `time_skip` events are rejected by an older app's known-type check).
- [ ] `go test ./... && cd web && npx vitest run && npx svelte-check && make e2e` — PASS. Commit `test(e2e)/docs: time and weather`.
