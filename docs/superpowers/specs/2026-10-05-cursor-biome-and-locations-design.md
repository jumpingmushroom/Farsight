# Cursor biome and the 1.0 location set

Date: 2026-10-05. Status: approved in chat (user feedback round after Plan 7).

Two fixes from user feedback on the live atlas:

1. The cursor readout shows `X · Z` and "Unexplored", but never the biome under the pointer (design §3.14, dropped as [GAP] in the MVP).
2. The single "Dungeons & locations" layer is noisy (≈940 burial chambers, troll caves and sunken crypts per world) and misses Valheim 1.0 sites such as the Forge of Potential.

Time of day and weather (design section 08) are a separate spec, after the weather-data spike.

## Coordinates: X and Z stay

Valheim's ground plane is X (east) / Z (north); Y is height. The game's own map and console use X/Z, so the labels stay. No change.

## 1. Biome under the cursor

### Data: a biome grid per seed

- The server builds a biome grid per `(seed, gen)`: 1024×1024 cells of one byte, 20 m per cell, covering ±10 240 m. Cell `(gx, gz)` = `(floor(x/20) + 512, floor(z/20) + 512)`, row-major by `gz` (row 0 is the south edge, the same orientation as the explored mask), sampled at the cell centre with `worldgen.NewBase(seed, gen).Biome`. Byte value = biome index: 0 none/outside the grid, 1 Meadows, 2 Black Forest, 3 Swamp, 4 Mountains, 5 Plains, 6 Mistlands, 7 Ashlands, 8 Deep North, 9 Ocean.
- Measured on MuleVikings: under 1 s on one core, 44 KB gzip'd. So it is built on first request, kept gzip'd in memory (a small cache keyed by seed and gen, single-flight per key), and never written to disk. No tile-set re-render, and RenderVersion is not bumped.
- Endpoint: `GET /tiles/{id}/{key}/biomes`. It uses the same unlock check as tiles, and `key` must equal the server's current tile-set key (the set itself need not be complete). Served as `Content-Type: application/octet-stream`, `Content-Encoding: gzip`, `Cache-Control: public, max-age=31536000, immutable`. Anything else is a 404.

### Client

- `ScaleReadout` gets the decoded grid. The place slot shows, in order: "Hover the map" (no cursor), "World edge" (outside the disc), "Unexplored" (fog on and the 12 m cell unexplored), else the biome name, else nothing while the grid is loading.
- Biome names: Meadows, Black Forest, Swamp, Mountains, Plains, Mistlands, Ashlands, Deep North, Ocean.
- Marker card "where" lines and search sub-lines do **not** change in this round (the design wants biome there too; adding it later is a lookup in the same grid).

### Not covered

Valheim 1.0 "alt biomes" (sector modifiers that rename or restyle regions) need the game's serialized AltBiome data and sector computation. The readout shows the base biome. Call it out in the README limitations.

## 2. Locations

### Facts (from both golden worlds and the wiki)

- `w.Locations` is the world's full location plan, including zones nobody has loaded (`Placed=false`). Unique sites (Bog Witch, Haldor, Hildir, Forge of Potential) keep up to ~10 candidates until one is placed; the others are then removed. MuleVikings still has 10 unplaced Bog Witch candidates.
- Boss altars have 3–5 real sites per boss, all of which exist when loaded.

### Pipeline change: classify on the server

Today the agent maps prefab → kind and drops everything else, so every mapping change needs an agent rollout. New:

- The agent sends each location in `Snapshot.Locations` as a `Marker` with `kind: "location"`, `type` = prefab name, no label, and the new `unplaced: true` when the save's `Placed` flag is false (omitted otherwise, so older snapshots read as placed). IDs stay positional among the sent entries (`loc-N`). A world plans about 12k locations total, but `extract.Finish` keeps only the ones inside its own explored mask (the same cell test the server uses, `explored.Mask.At`) — mostly unplaced candidates of unique sites and sites in ground nobody has found, so what's actually sent is a small fraction of that; snapshots are gzip-stored besides, so the cost is small either way. (I3: without this filter the gzip'd payload measured about 6× the old agent's, since the server was the only place filtering by the explored mask; filtering in the agent avoids shipping and decoding the ~90% that never survives it.) One agent rollout, then mapping changes are server-only.
- `extract.ClassifyLocations(raw []Marker) []Marker` is the one place that applies the table: it looks up each entry by `type` (so snapshots from older agents, already classified, are re-labelled the same way), sets `kind`, `label` and the new `group`, drops unmapped types, and drops unplaced entries of unique sites. The central app applies it once per stored snapshot (in `worldState`), and `worldevents` applies it before its "near a location" lookup.
- The snapshot API then filters to the explored mask, as today — now mostly a no-op, since the agent already dropped what wouldn't pass it.

### The set

`kind` stays as today (`boss_altar`, `trader`, `dungeon`) plus a new `landmark`. Each mapped prefab also gets a `group` that picks the layer.

| group (layer, default) | prefab → label |
|---|---|
| `landmarks` — "Landmarks" (on) | boss altars as today; traders: Vendor_BlackForest → Haldor, Hildir_camp → Hildir, BogWitch_Camp → Bog Witch (unique); AncientUpgradeStation → Forge of Potential (unique); StartTemple → Sacrificial stones; PlaceofMystery1/2/3 → Mysterious location; Hildir_cave → Howling cavern, Hildir_crypt → Smouldering tomb, Hildir_plainsfortress → Sealed tower; CharredFortress → Charred fortress; NorthMemorialPlace → Memorial site |
| `dungeons` — "Dungeons" (off) | Crypt2/3/4 → Burial chambers; SunkenCrypt4 → Sunken crypt; TrollCave02 → Troll cave; MountainCave02 → Frost cave; Mistlands_DvergrTownEntrance1/2 → Infested mine; MorkBorg → Mörkhalla; TheHole01 → Winding tunnels |
| `minor` — "Minor places" (off) | GoblinCamp2 → Fuling village; BearCave → Bear cave; NorthVillage → Abandoned village; MorgenHole1/2/3 → Putrid hole |

Runestones, ruins, houses, shipwrecks and the rest stay unmapped.

`kind`: boss altars `boss_altar`, the three traders `trader`, Hildir's three and everything in `dungeons` `dungeon`, everything else `landmark`.

### Client

- The `locations` layer is replaced by three layers in this order: Landmarks (on, icon `flame`/existing location icon), Dungeons (off, `arch`), Minor places (off, `mountain`). (Layer switches are not persisted, so nothing to migrate.)
- New pin type `landmark` (INK disc, icon per type where Lucide has one: Forge → `anvil`, Mysterious location → `sparkles`, Sacrificial stones → `circle-dot`, Charred fortress → `castle`, Memorial site → `landmark`); kicker = label, card note as for dungeons.
- Dungeon zoom gating (hidden below zoom 3) applies to `dungeons` and `minor`.
- Layer counts and search include the new kinds.

## Testing

- Go: biome grid encode/decode and orientation (known points in the golden seed: spawn = Meadows, an ocean point, a point outside the disc); the in-memory cache and single-flight; the endpoint (auth, stale key 404, headers). Location classification table (every mapped prefab, unique filtering on `placed`, legacy pre-classified snapshots).
- Web: readout states incl. biome; the layer split; landmark pins.
- e2e: hover readout shows a biome on the seeded world; the Dungeons layer is off by default.
- Live check after deploy: the Bog Witch shows only when placed; the Forge of Potential appears on MuleVikings if explored.
