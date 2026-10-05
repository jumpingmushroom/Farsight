# Time of day and weather

Date: 2026-10-05. Status: approved in chat (user asked for the design's section 08 time and weather pill and its dropdown; follows the weather-data spike). Depends on Plan 8 (biome grid).

The design (`design/World Atlas.dc.html :309-336`, section 08) has a pill under the map-updated pill: a sun or moon disc, "{Morning|Day|Evening|Night} · {clock}", a weather line, and a dropdown with a day-progress bar and a per-biome weather table. At night the map gets a tint.

## Facts (spike, 2026-10-05; data extracted from the live server's game files)

- World time is `netTime` (seconds). A day is 1800 s; day N = `floor(netTime/1800)`. Raw fraction `f = (netTime mod 1800)/1800`; it is night when `f ≤ 0.15` or `f ≥ 0.85` (9 of 30 minutes), as the game's `IsNight`.
- **The dedicated server only advances netTime while at least one player is connected** (`ZNet.UpdateNetTime`). Sleeping skips to the next morning.
- When players sleep, the server logs `Time {t}, day:{d}    nextm:{m}  skipspeed:{s}`, and about 12 s later the time is `m`. Real logs have it (e.g. `Time 487653.36…, day:270    nextm:488070.00…`).
- Weather: `period = floor(floor(netTime)/666)`; Unity `Random.InitState(period)`; draw `Range(0, Σweights)` over the biome's non-override entries (float32 sums); first entry whose running sum ≥ draw. `internal/worldgen/urandom.go` already ports InitState and `RangeFloat`. Weights per biome are in the spike's `weather-data.json` (Meadows: Clear 5, Rain/Misty/ThunderStorm/LightRain 0.2 each, and so on); they match the wiki's chances. Every biome uses the same draw each period.
- Not modelled: the Dark Meadows alt biome (never clear; 2–5 outer Meadows sectors), Ashlands overrides near the Ashlands coast, raid / boss / dungeon weather. These are local to where a player stands.
- No ground truth exists (clients pick the weather; nothing records it). Confidence comes from the data matching the wiki exactly.

## Clock estimate (server)

`Card.clock` estimates netTime **now** from anchors:

1. The latest save: `(snapshot.world.netTime, snapshot.savedAt)`.
2. The latest sleep line in the log, newer than that save: `(nextm, logTime + 12 s)`. The log watcher emits it as a new event kind `time_skip` with `{to: nextm}`.

From the newest anchor `(t0, at0)`: `netTime(now) = t0 + seconds between at0 and now during which at least one player was online` (sessions and live presence, as the Online list uses). `running` = someone is online now. A server that is offline or has no snapshot has no clock.

Card JSON (new field, omitted when unknown):

```json
"clock": {
  "netTime": 493512.4,      // estimate at `at`
  "at": "2026-10-05T18:20:00Z",
  "running": true,           // client ticks netTime forward 1 s/s while true
  "source": "save" | "sleep"
}
```

## Weather (server)

New package `internal/weather` with the spike's data embedded (only what is used: `environmentDuration`, per-biome non-override entries with weights, env → display name). `weather.At(netTime float64, biome worldgen.Biome) Env` and `weather.Period(netTime) int64`. Display names: Clear, Clear (forest mist) → "Forest mist", Fog, Light rain, Rain, Thunderstorm, Swamp rain → "Rain", Snow, Blizzard, Ash rain, Cinder rain, Ash storm, and the Deep North twilight variants mapped to Snow / Blizzard / Clear. The table lives in the package, with a test per biome.

`Card.weather` (omitted with no clock):

```json
"weather": {
  "periodSec": 666,
  "periods": [                          // the current period and the next two
    { "start": 492840, "byBiome": { "Meadows": "Clear", "Swamp": "Rain", … } },
    …
  ],
  "biomes": ["Meadows", "Black Forest", …],   // explored biomes, in legend order
  "home": "Meadows"                            // the pill's biome
}
```

- Explored biomes: biomes with at least 50 explored 12 m cells, counted by sampling the Plan 8 biome grid at each explored cell (computed once per worldState, lazily). Ocean is included when explored.
- `home`: the biome under the base with the most pieces; Meadows when there are no bases.

## UI

**Pill** (desktop: `top:106px; left:{pillL}`, under the map-updated pill, per the design; mobile: a compact chip under the top bar, opening the same content in a bottom sheet). Disc: sun icon (`sun`, accent-100 background) by day, moon (`moon`, cold 25%) at night. Title: "{label} · {HH:MM}". Line: "{home}: {weather lower-case}" (e.g. "Meadows: light rain").

- Clock `HH:MM` = the game's rescaled day fraction × 24 h (sun position): raw 0.15 → 06:00, raw 0.85 → 18:00, linear in between and across the night (`RescaleDayFraction`).
- Labels from that clock: Morning 06:00–11:00, Day 11:00–16:00, Evening 16:00–18:00, Night 18:00–06:00 (Night matches the game's `IsNight`).
- "Next" text: Morning → "Midday in ~N min", Day → "Evening in ~N min", Evening → "Night in ~N min", Night → "Dawn in ~N min", N in real minutes (1 game second = 1 real second).

**Dropdown** (design `:309-336`, 360 px): "Day {d} · {clock}" with the next text; the progress bar with the design's gradient and a marker at the raw fraction; the hour ticks 00/06/12/18/24 placed on the rescaled clock. Table: Biome | Now | Next | Then, where Next and Then are headed with their start ("from 18:42" in local wall time when running; "after Now" when not). Rows: explored biomes with legend swatches. Footer (replaces the design's): "Estimated from the world clock in the last save{ and the last sleep}. The world clock only runs while someone is online. Weather follows the game's own schedule; raids, dungeons and the Dark Meadows have their own."

**Paused**: when `running` is false, the title keeps the frozen clock, the line reads "{home}: {weather} · paused", and the dropdown says "Time is paused while nobody is online."

**Night tint**: when the clock says Night, the map tiles get `filter: brightness(.8) saturate(.8) hue-rotate(-12deg)`; Evening `brightness(.94) sepia(.15)` (design `nightFilter`). It combines with the existing state filters (offline, biomes-off greyscale) by concatenation. It is static (no animation), so it applies regardless of reduced-motion settings. No layer-panel toggle (YAGNI).

The client recomputes label, clock, progress and which period is "now" every second from `clock` while running (no extra requests); the weather names come from the server's three periods. When the client's estimate passes the third period's start (more than ~22 min without a fresh card), it shows "—".

## Not covered

Dark Meadows, Ashlands-coast and raid weather; per-player weather; history of past weather.

## Testing

- Go: `weather.At` against the spike calculator for fixed (netTime, biome) pairs (record 20 pairs from `weathercalc` into the test); period boundaries; float32 summation. Clock estimate: save-only, sleep-after-save, sleep-before-save ignored, offline gaps not counted, running flag. Log watcher: the sleep line parses (Oslo and UTC logs) into `time_skip`. Explored-biome counting and `home`.
- Web: label/clock/next from netTime at boundary values (raw 0.15, 0.85, 0, 0.5); paused rendering; Next/Then headers; night filter string composition.
- e2e: the pill renders on the seeded server with a known clock, the dropdown lists the seeded explored biomes.
