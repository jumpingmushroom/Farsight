# Fog in the tiles, from the game's real explored map

Status: approved design, 2026-10-01. Amends the MVP spec
(`2026-09-29-farsight-atlas-mvp-design.md`): "terrain tiles are never
fogged" and "fog is a client-side mask" are replaced by this document.

## Why

Two problems with the live atlas:

1. **Zoom is heavy and the fog can lag the terrain.** The fog is one
   large Canvas 2D layer (the view plus 50% padding, at device
   resolution) repainted after every zoom step: a texture fill, the
   punch-out, a blur and an upload to the GPU. Profiling the live site
   showed 200–500 ms frames during a fast zoom, with the compositor
   saturated; Leaflet's zoom animation had to be switched off to keep
   fog and terrain in step.
2. **Farsight reveals about 2.3× what the game does.** "Explored" today
   is the save's generated zones. The game generates every zone within
   about two zones of a player, but the in-game map reveals only 100 m.
   Measured on the real saves: MuleVikings 21.4 km² shown vs 9.1 km²
   on the game's map; Mulennials 114.9 km² vs 50.9 km².

## Goals

- Smooth, gliding zoom (Leaflet's native animation) with fog that can
  never lag or drift from the terrain, on any device.
- Fog matches the game's shared map: never show terrain past it.
- Same look: parchment `#cfbe9c`, the same grain and 45° hatching; the
  edge becomes a smooth, distance-based fade instead of blurred squares.
- Unexplored terrain and pins never leave the server.

## Non-goals

- Per-player exploration (it lives in player profiles on their PCs).
- New fog styles or animation.
- Making the in-browser fog faster: the fog canvas is removed.

## Design

### 1. The explored mask (agent)

**Source: cartography tables.** A `piece_cartographytable` ZDO holds
the shared map in byte array `data` (StableHash of `"data"`):
gzip-compressed ZPackage of `int32 version` (2 or 3), `int32 n`, then
`n` bytes, one bool per cell, then pins (ignored). `n` is 2048×2048.
Cells are the game's minimap grid: 12 m per cell, `px = round(x/12) +
1024`, `py = round(z/12) + 1024`, index `py*2048 + px` (row 0 is the
south edge). 12 m is verified on the real saves: every bed, portal and
table in MuleVikings (31/31) lies on an explored cell, and no other
cell size scores as well.

- `save.ZDO` decoding keeps byte arrays only for prefabs the caller asks
  for (the cartography table), so memory doesn't grow with the world.
- The mask is the union of all tables' maps, **plus a 100 m disc around
  every player-built piece** (the game's reveal radius: someone stood
  there). Real data shows why: 8 of 167 beds, portals and tables in
  Mulennials lie outside the recorded map (built after the last
  "Record").
- **Fallback** when no table has data: the generated zones, shrunk by a
  number of cells chosen to best match the real table data of the
  MuleVikings save (highest intersection-over-union), plus the 100 m
  discs.
- Snapshot field: `explored: {source: "tables"|"zones", cell: 12,
  size: 2048, bits}` where `bits` is the row-major bitset (LSB-first per
  byte), gzip'd and base64'd. `exploredZones` stays in the snapshot so
  an older central app keeps working.

### 2. Fog tiles (central app)

**Mask on the server.** New snapshots carry `explored`; old ones only
`exploredZones`, which the server rasterises onto the same 12 m grid
(each 64 m zone fills its cells), so both formats feed one code path.

**Fog key** = first 16 hex of SHA-256 over (source, size, bits, fog
style version). It changes only when exploration changes, not on every
autosave.

**Distance field.** Once per fog key: signed distance in metres from
each cell to the explored edge (positive inside), by an exact Euclidean
distance transform, clamped to ±64 m and stored as int8 at 0.5 m
(about 4 MB per server).

**Alpha.** Per tile pixel, bilinear-sample the distance `d` at the
pixel's world point; `w` = min(57.6 m, 24 px × metres-per-pixel at the
tile's zoom); `alpha = 1 − smoothstep(0, w, d)`. Fog is fully opaque at
and beyond the explored edge, and the soft band lies inside the
explored area, so nothing past the game's map is ever visible.

**Look.** Port the browser's current texture exactly: base `#cfbe9c`,
±9 grey noise per pixel from mulberry32 seed `0x5eedf06` over a
126 px pattern, hatch `rgba(120,98,66,.12)` 1 px lines where
`x − y ≡ 0 (mod 9)`; the pattern is anchored to world pixels at the
tile's zoom, so it is seamless across tiles. Outside the world disc the
pixel keeps the terrain tile's own alpha.

**Zoom 6.** Fog tiles are served natively at z0–z6. z6 terrain is the
z5 parent quadrant upscaled 2×; the fog is drawn at z6 resolution, so
the hatching stays crisp at max zoom.

**Serving** `GET /tiles/{id}/{key}/{fog}/{z}/{x}/{y}.png`:

- 404 unless the server is unlocked, `key` is its current complete tile
  set and `fog` is its current fog key.
- Each tile is classified once per fog key from the distance field over
  its footprint: *clear* (alpha 0 everywhere) serves the terrain file
  unchanged (z0–z5); *fog* (alpha 1 everywhere) composes fog only,
  without reading terrain; *edge* decodes terrain, blends, encodes.
- PNG encoding at `png.BestSpeed`. Composed tiles go into a 64 MB
  in-memory LRU keyed by (server, fog key, z, x, y), with concurrent
  requests for one key coalesced and compositions bounded by
  `GOMAXPROCS`.
- `Cache-Control: public, max-age=31536000, immutable`.
- The old raw route `/tiles/{id}/{key}/{z}/{x}/{y}.png` is removed.

**Pins and stats.** The snapshot API filters markers, locations and
bases to the explored mask (by the mask cell under each point; the
players list has no positions and is unchanged). It returns `fogKey` and the mask itself
(`explored`, the same encoding) for the browser's search and cursor
readout. `exploredPct` = explored cells inside the 10.5 km disc ÷ all
cells inside it, × 100, one decimal.

### 3. Browser

- Remove the fog canvas layer, the skirt, `data-zoom`, and the
  zoom-sync workarounds; re-enable Leaflet's zoom animation.
- Tile URL from `fogKey`; `maxNativeZoom: 6`.
- The world disc under the tiles is filled `#cfbe9c`, so tiles still
  loading look fogged, not empty.
- `isExplored` and marker filtering use the 12 m mask (decoded once per
  snapshot); the charting overlay is unchanged.

## Testing

Go:

- Table blob decode (versions 2 and 3, length check, truncated or
  corrupt gzip rejected) from a synthetic fixture; union of tables; the
  100 m disc rule; the zone fallback and its shrink, checked for IoU
  against the local MuleVikings save (skipped when `testdata-golden/`
  is absent, as today).
- Distance transform against a brute-force reference on small grids;
  alpha is exactly 1 at `d ≤ 0`; noise and hatching are deterministic
  and seamless across tile borders.
- Tile pixels: a deep-fog pixel carries no terrain information (equal
  to the fog texture alone); a clear pixel equals the terrain
  byte-for-byte; each class is served by its path.
- Handler: stale fog key, locked server and the removed raw route all
  404; cache headers; an old-format snapshot still yields a fog key.
- Benchmark: an edge tile composes in under about 10 ms.

Web (Vitest and Playwright):

- Tile URLs carry the fog key; no fog canvas; zoom animation on.
- Markers in unexplored cells are absent from the snapshot response.
- A screenshot pixel deep in unexplored terrain is fog-coloured.
- Mask decoding and `isExplored` at 12 m.

## Rollout

1. **Central app** accepts both snapshot formats and serves fog tiles
   (from zones until agents update): smooth zoom immediately. Restarts
   only the Farsight app.
2. **Agent** sends the table mask. The sidecar image changes, which
   restarts all three Valheim servers: needs the owner's go.
3. **Cleanup:** drop `exploredZones` from the snapshot API once both are
   live.

The runbook gains a note: Farsight shows what has been recorded at a
cartography table (plus 100 m around anything built), so players should
record now and then.

## Risks

- Tables lag real exploration until someone records; mitigated by the
  100 m rule around built pieces.
- Tile re-fetches after exploration changes: bounded to tiles people
  view, and cached immutably per fog key.
- The seed name is shown on the server card, so a determined player can
  still regenerate the world elsewhere; this design stops casual
  spoilers from the atlas itself.
