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

- The server writes `biomes.bin` next to each tile set (`tileset` dir for `(seed, gen)`): a 1024×1024 grid of one byte per cell, cell size 20 m (world span ±10 240 m, same orientation as the explored mask), row-major from the north-west corner, gzip-compressed. Byte value = biome index (0 = outside the disc/none, 1 Meadows, 2 Black Forest, 3 Swamp, 4 Mountains, 5 Plains, 6 Mistlands, 7 Ashlands, 8 Deep North, 9 Ocean). Sample at the cell centre with `worldgen` `Sampler.Biome`.
- Generated lazily: first request for a tile set without the file computes it (one at a time, single-flight per key), writes it atomically, then serves it. Existing tile sets do not need a re-render, and RenderVersion is not bumped.
- Endpoint: `GET /tiles/{id}/{key}/biomes` — same unlock check as tiles, `Content-Type: application/octet-stream`, `Content-Encoding: gzip`, long-lived immutable cache headers (the key already names the seed and generator). 404 while the tile set itself isn't ready.
- Expected size: a few hundred KB compressed; fetched once per seed and kept in memory.

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

- The agent sends every location as `{type, x, y, z, placed}` (prefab name; `kind`/`label` empty). ~12k entries per world; snapshots are gzip-stored, so the cost is small. One agent rollout, then mapping changes are server-only.
- The location table (prefab → kind, label, group, unique flag) moves to one place in `extract` and the server applies it when building the snapshot response; unmapped types are dropped. Snapshots from older agents (already classified, no `placed`) pass through as they are: their entries count as placed.
- Visibility rule, server side: explored-mask filter as today, **and** for unique sites (`unique` in the table) only `placed` entries.

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

- The `locations` layer is replaced by three layers in this order: Landmarks (on, icon `flame`/existing location icon), Dungeons (off, `arch`), Minor places (off, `mountain`). Saved layer preferences under the old `locations` key map to Landmarks.
- New pin type `landmark` (INK disc, icon per type where Lucide has one: Forge → `anvil`, Mysterious location → `sparkles`, Sacrificial stones → `circle-dot`, Charred fortress → `castle`, Memorial site → `landmark`); kicker = label, card note as for dungeons.
- Dungeon zoom gating (hidden below zoom 3) applies to `dungeons` and `minor`.
- Layer counts and search include the new kinds.

## Testing

- Go: biome grid encode/decode and orientation (known points in the golden seed: spawn = Meadows, an ocean point, a point outside the disc); the lazy-generation single-flight; the endpoint (auth, 404 before ready, headers). Location classification table (every mapped prefab, unique filtering on `placed`, legacy pre-classified snapshots).
- Web: readout states incl. biome; layer split and preference migration; landmark pins.
- e2e: hover readout shows a biome on the seeded world; the Dungeons layer is off by default.
- Live check after deploy: the Bog Witch shows only when placed; the Forge of Potential appears on MuleVikings if explored.
