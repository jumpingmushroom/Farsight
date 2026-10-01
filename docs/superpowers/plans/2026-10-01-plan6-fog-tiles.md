# Farsight Plan 6 — Fog Tiles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Draw the fog of war into the map tiles on the server, from the game's own explored map (the cartography tables' shared map, plus 100 m around anything built), so zooming glides with Leaflet's native animation, the fog matches what the game shows, and unexplored terrain and pins never leave the server.

**Architecture:**
- **Agent:** `save` keeps byte arrays only for cartography tables, and a new `explored` package turns the tables' maps (or, without one, the generated zones shrunk by a calibrated 20 cells) plus the game's 100 m reveal around built pieces into a 2048² bitset at 12 m per cell. `extract` sends it as `snapshot.explored` (gzip + base64) next to the unchanged `exploredZones`.
- **Central app:** a per-server `worldState` cache decodes each new snapshot once (rasterising `exploredZones` for old agents), derives the fog key and `exploredPct`, and filters markers, locations and bases. A new `fog` package builds a signed distance field (exact EDT, int8 at 0.5 m), classifies every z0–z6 tile as clear, fog or edge, and draws the ported parchment texture.
- **Tiles:** `GET /tiles/{id}/{key}/{fog}/{z}/{x}/{y}.png` serves clear tiles straight from disk and composes the rest into a 64 MB LRU, coalescing concurrent requests and running at most GOMAXPROCS compositions at once.
- **Browser:** the fog canvas, skirt and zoom workarounds go. Tiles carry the fog key, `maxNativeZoom` is 6, the world disc is parchment-filled, and search and the cursor readout use the decoded 12 m mask.

**Tech Stack:** Go 1.27 standard library only (`image/png`, `compress/gzip`, `crypto/sha256`, `container/list`; no new modules), SvelteKit 2 / Svelte 5 / Leaflet 1.9.4 (no new npm packages; the browser's `DecompressionStream`), Vitest 5, Playwright 1.63.

**Spec:** `docs/superpowers/specs/2026-10-01-fog-tiles-design.md`. It amends the MVP spec `docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md` (fog was a client-side mask there).

## Global Constraints

- **The Farsight repo is PUBLIC.** No real player data goes into fixtures or tests: no player or world names from the real saves, no seeds, no positions copied from them. Fixtures are synthetic (`savetest`, hand-written snapshots). Golden tests read the gitignored `testdata-golden/` at run time only, assert counts and invariants, and `t.Skip` when the directory is absent, as the existing ones do.
- **Never commit `reference/` or `testdata-golden/`** (nor `web/build`, `web/node_modules`, `web/test-results`). Never quote the decompiled game code from `reference/` in code, comments or commits.
- **Branch:** all work happens on a local branch `feat/plan6-fog-tiles` cut from `public-main` in `/workspace/Farsight`. **Never push** either repo, and never deploy. The cloudcluster change in Task 8 goes on a local branch there too.
- **Commits:** Farsight: `git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit`. Cloudcluster: `git -c user.name="Johnny Dalen" -c user.email="johnny@jumpingmushroom.com" commit`, Conventional Commits with a scope. Every message ends with a blank line, then `Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T`. Stage the listed paths only (`git add <paths>`, `git rm <paths>`); never `git add -A` or `git add .`.
- **Commands:**
  - Go: `PATH=$HOME/.local/go/bin:$PATH go test ./...` from the repo root. There is no gcc here, so no `-race`, but CI runs `-race`: tests must be free of data races.
  - Web: `cd web && npm run check && npx vitest run`.
  - e2e: `cd web && PATH=$HOME/.local/go/bin:$PATH LD_LIBRARY_PATH="$(../hack/playwright-libs.sh --print)" FONTCONFIG_FILE="$(../hack/playwright-libs.sh --fontconfig)" npx playwright test`. `global-setup.ts` builds the web UI and both binaries itself.
  - `make` is not installed; don't use it.
- **Deploy order (spec "Rollout"), for whoever deploys after this plan:**
  1. **Central app first.** It accepts both snapshot formats and serves fog tiles, from the zones until the agents update, so zoom is smooth immediately. It restarts only the Farsight pod.
  2. **Agent second.** The sidecar image change restarts all three Valheim servers, so it **needs the owner's go-ahead**.
  3. **Cleanup later** (not in this plan): drop `exploredZones` from the snapshot API once both are live.
- **Performance:** an edge tile composes (decode, blend, encode) in about 10 ms on a current CPU. The dev box's 2013 Xeon E5-2660 v2 measured about 14 ms while this plan was written: record your number in the Task 6 commit. Composed tiles live in a 64 MB in-memory LRU (counted in PNG bytes). At most `GOMAXPROCS` compositions run at once. The field and tile classes are built once per fog key, which took about 0.6 s on the dev box.
- **Wire contract (exact names, shared by every task):**
  - Snapshot field `explored: {source: "tables"|"zones", cell: 12, size: 2048, bits}`. `bits` is base64 (standard alphabet, padded) of gzip of the row-major bitset (index `py*2048+px`, least significant bit first in each byte). `exploredZones` stays in the snapshot.
  - `GET /api/servers/{id}/snapshot` adds `fogKey` and `explored`, keeps `exploredZones`, and filters `markers`, `locations` and `bases` to the mask. `players` is unchanged.
  - Tiles: `GET /tiles/{id}/{key}/{fog}/{z}/{x}/{y}.png`, z0–z6, `Cache-Control: public, max-age=31536000, immutable`. The raw route `/tiles/{id}/{key}/{z}/{x}/{y}.png` is removed.
  - Fog key: the first 16 hex digits of SHA-256 over (source, size, bitset, `fog.StyleVersion`).
- **Grid facts (verified; don't re-derive):**
  - Cells are 12 m: `px = round(x/12) + 1024`, `py = round(z/12) + 1024` with banker's rounding (Go `math.RoundToEven`), index `py*2048 + px`, row 0 is the south edge.
  - The table prefab is `piece_cartographytable`; its byte array key is `names.StableHash("data")`. The value is gzip of a ZPackage: int32 version (2 or 3), int32 n = 4 194 304, n bool bytes, then pins.
  - The game's reveal is 9 cells: every cell within 9 cells (Euclidean) of the centre cell.
- **Fog constants (ported exactly from `web/src/lib/fog.ts`):**
  - Base `#cfbe9c`, grey noise ±9 (`NOISE` 18) per pixel from `mulberry32(0x5eedf06)`, over a 126 px pattern.
  - Hatch `rgba(120,98,66,.12)` on pixels where x − y ≡ 0 (mod 9).
  - The field is int8 at 0.5 m, clamped to ±63.5 m (±127 units; an int8 can't hold +128).
  - Band width `w = min(57.6 m, 24 px × metres-per-pixel)`, and `alpha = 1 − smoothstep(0, w, d)`.

## File Structure

```
internal/save/zdo.go, world.go, chunked.go, legacy.go   # T1: ReadOptions/ReadWith, ZDO.ByteArrays for chosen prefabs
internal/save/maptable.go (+_test)                      # T1: MapTablePrefab, MapDataKey, MapCells, DecodeMapData
internal/save/savetest/savetest.go                      # T1: ZDO.ByteArrays, MapData fixture builder
internal/explored/explored.go, encode.go (+tests)       # T2: Mask, CellOf, Reveal, FromZones, Erode, Percent, Encode/Decode
internal/explored/golden_test.go                        # T2: IoU calibration of ZoneShrinkCells
internal/extract/snapshot.go, extract.go (+tests)       # T3: Snapshot.Explored, KeepBytes, table union + reveal
internal/agent/agent.go (+test)                         # T3: saveRead keeps table byte arrays
internal/fog/fog.go, field.go, texture.go,              # T4: Key, StyleVersion, Alpha, EdgeWidth, Field (EDT),
             classify.go, compose.go (+tests)           #     texture port, ClassMap, FogTile, Blend, Upscale
internal/store/snapshots.go (+test)                     # T5: LatestSnapshotID
internal/server/world.go                                # T5: worldState / worldCache
internal/server/api.go, ingest.go, server.go            # T5: mask filtering, fogKey, exploredPct, explored validation
internal/server/tilecache.go (+test)                    # T6: 64 MB LRU, coalescing, GOMAXPROCS bound
internal/server/tiles.go (+bench)                       # T6: fog tile handler and composer (raw route removed)
cmd/farsight-seed/main.go (+test)                       # T7: -explored
web/src/lib/explored.ts (+test)                         # T7: 12 m mask decode, isExplored
web/src/lib/fog.ts, fog.test.ts                         # T7: deleted
web/src/lib/{types,api,markers,search}.ts,              # T7: SnapshotView.fogKey/explored/mask, tileUrl(id,key,fog)
  components/{AtlasMap,DesktopShell,MobileShell,ScaleReadout}.svelte
web/tests/e2e/{global-setup,helpers,desktop.spec,states.spec}.ts   # T7
README.md, docs/superpowers/specs/*.md                  # T8
/workspace/cloudcluster/docs/runbooks/farsight.md       # T8 (cloudcluster repo, local branch)
```

Task order follows the suggested shape, with one swap: the pure `fog` package (Task 4) comes before the central-app task (Task 5). The reason is that the fog key includes `fog.StyleVersion`, which the fog package owns, and Task 5 already serves the key.

---


### Task 1: `save` keeps cartography-table byte arrays; table blob decoder

`save.DecodeZDO` skips every byte array today. The explored mask needs the `data` byte array of `piece_cartographytable` ZDOs, and nothing else, so memory doesn't grow with the world.

**Files:**
- Modify: `internal/save/zdo.go`: `ZDO.ByteArrays`, plus an unexported `decodeZDO(…, keep)`. `DecodeZDO` keeps its signature and still skips.
- Modify: `internal/save/world.go`: `ReadOptions`, `ReadWith`. `Read` becomes `ReadWith(…, ReadOptions{}, …)`.
- Modify: `internal/save/chunked.go`, `internal/save/legacy.go`: thread `ReadOptions` through.
- Create: `internal/save/maptable.go`, `internal/save/maptable_test.go`
- Modify: `internal/save/savetest/savetest.go`: `ZDO.ByteArrays`, `MapData`.
- Modify (test call sites only): `internal/save/chunked_test.go`, `internal/save/legacy_test.go`, `internal/save/fuzz_test.go`, `internal/save/golden_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces (later tasks rely on these exact names):
  - `type save.ReadOptions struct { KeepBytes func(prefab int32) bool }`
  - `func save.ReadWith(worldsDir, worldName string, opt save.ReadOptions, fn func(*save.ZDO)) (*save.World, error)`. `save.Read` is unchanged for callers.
  - `save.ZDO.ByteArrays map[int32][]byte`: nil unless `KeepBytes(z.Prefab)`. Values are copies.
  - `var save.MapTablePrefab int32 = names.StableHash("piece_cartographytable")`, `var save.MapDataKey int32 = names.StableHash("data")`, `const save.MapCells = 2048 * 2048`
  - `func save.DecodeMapData(b []byte) ([]byte, error)`: exactly `MapCells` flag bytes (non-zero = explored, cell order `py*2048+px`).
  - `savetest.ZDO.ByteArrays map[string][]byte`, `func savetest.MapData(version int32, explored ...int) []byte`

- [ ] **Step 1: Create the branch**

```bash
cd /workspace/Farsight
git switch public-main
git status --short            # must print nothing
git switch -c feat/plan6-fog-tiles
```

- [ ] **Step 2: Extend the `savetest` fixture writer (test infrastructure the new tests need)**

In `internal/save/savetest/savetest.go`, add `"maps"` and `"slices"` to the imports (keeping them sorted: `"bytes"`, `"compress/gzip"`, `"fmt"`, `"maps"`, `"os"`, `"path/filepath"`, `"slices"`, `"testing"`).

Add the field at the end of `type ZDO struct`:

```go
	Strings map[string]string
	// ByteArrays are written in key order.
	ByteArrays map[string][]byte
}
```

In `EncodeZDO`, after the `if len(z.Strings) > 0 { flags |= 0x40 }` block, add:

```go
	if len(z.ByteArrays) > 0 {
		flags |= 0x80
	}
```

`EncodeZDO` ends with the strings block and then its closing `}`. Replace that closing `}` with the following, which writes the byte arrays, closes `EncodeZDO` and adds a new function `MapData`:

```go
	if len(z.ByteArrays) > 0 {
		count(len(z.ByteArrays))
		for _, k := range slices.Sorted(maps.Keys(z.ByteArrays)) {
			w.I32(names.StableHash(k))
			w.ByteArray(z.ByteArrays[k])
		}
	}
}

// MapData builds a cartography table's "data" byte array: gzip of a
// ZPackage with the given version, the 2048² cell count, one bool per cell
// (true at each index in explored) and an empty pin list.
func MapData(version int32, explored ...int) []byte {
	const cells = 2048 * 2048
	flags := make([]byte, cells)
	for _, i := range explored {
		flags[i] = 1
	}
	var p zpkg.Writer
	p.I32(version)
	p.I32(cells)
	p.Raw(flags)
	p.I32(0) // pins
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	gw.Write(p.Bytes())
	gw.Close()
	return gz.Bytes()
}
```

- [ ] **Step 3: Write the failing tests**

Create `internal/save/maptable_test.go`:

```go
package save

import (
	"bytes"
	"compress/gzip"
	"path/filepath"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

func TestDecodeMapDataVersions(t *testing.T) {
	for _, ver := range []int32{2, 3} {
		flags, err := DecodeMapData(savetest.MapData(ver, 0, 5, MapCells-1))
		if err != nil {
			t.Fatalf("v%d: %v", ver, err)
		}
		if len(flags) != MapCells {
			t.Fatalf("v%d: %d flags, want %d", ver, len(flags), MapCells)
		}
		n := 0
		for _, f := range flags {
			if f != 0 {
				n++
			}
		}
		if n != 3 || flags[0] != 1 || flags[5] != 1 || flags[MapCells-1] != 1 {
			t.Fatalf("v%d: %d explored, flags[0,5,last] = %d %d %d", ver, n, flags[0], flags[5], flags[MapCells-1])
		}
	}
}

// mapBlob gzips a hand-built header and cells.
func mapBlob(version, n int32, cells []byte) []byte {
	var p zpkg.Writer
	p.I32(version)
	p.I32(n)
	p.Raw(cells)
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	gw.Write(p.Bytes())
	gw.Close()
	return gz.Bytes()
}

func TestDecodeMapDataRejectsBadInput(t *testing.T) {
	good := savetest.MapData(3, 1)
	corrupt := bytes.Clone(good)
	for i := 10; i < len(corrupt); i++ {
		corrupt[i] = 0xFF // BFINAL=1, BTYPE=11: a reserved (invalid) deflate block
	}
	cases := map[string][]byte{
		"version 1":   mapBlob(1, MapCells, make([]byte, MapCells)),
		"wrong n":     mapBlob(3, 100, make([]byte, 100)),
		"short cells": mapBlob(3, MapCells, make([]byte, 1000)),
		"truncated":   good[:len(good)/2],
		"not gzip":    []byte("not a gzip stream"),
		"corrupt":     corrupt,
		"empty":       nil,
	}
	for name, b := range cases {
		if _, err := DecodeMapData(b); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestReadWithKeepsBytesOnlyForChosenPrefabs(t *testing.T) {
	blob := savetest.MapData(3, 42)
	zdos := []savetest.ZDO{
		{Prefab: "piece_cartographytable", ByteArrays: map[string][]byte{"data": blob}},
		{Prefab: "sign", Strings: map[string]string{"text": "hi"}, ByteArrays: map[string][]byte{"data": {1, 2, 3}}},
	}
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, filepath.Join(dir, "c"), "W", 1, "x", zdos, nil, nil, nil)
	savetest.WriteLegacyWorld(t, filepath.Join(dir, "l"), "W", "x", zdos, nil, nil, nil)
	keep := ReadOptions{KeepBytes: func(p int32) bool { return p == MapTablePrefab }}
	for _, sub := range []string{"c", "l"} {
		var tables, signs int
		_, err := ReadWith(filepath.Join(dir, sub), "W", keep, func(z *ZDO) {
			switch z.Prefab {
			case MapTablePrefab:
				tables++
				if !bytes.Equal(z.ByteArrays[MapDataKey], blob) {
					t.Errorf("%s: table data not kept intact (%d bytes)", sub, len(z.ByteArrays[MapDataKey]))
				}
			default:
				signs++
				if z.ByteArrays != nil || z.Strings[names.StableHash("text")] != "hi" {
					t.Errorf("%s: sign kept byte arrays %v, strings %v", sub, z.ByteArrays, z.Strings)
				}
			}
		})
		if err != nil || tables != 1 || signs != 1 {
			t.Fatalf("%s: err=%v tables=%d signs=%d", sub, err, tables, signs)
		}
		_, err = Read(filepath.Join(dir, sub), "W", func(z *ZDO) {
			if z.ByteArrays != nil {
				t.Errorf("%s: Read kept byte arrays for prefab %d", sub, z.Prefab)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
```

Append to `internal/save/golden_test.go`. It reads the gitignored saves at run time, skips when they're absent, and asserts only counts:

```go
// TestGoldenMapTables checks the cartography-table facts the explored mask
// rests on: MuleVikings has one table recording 63 338 cells, Mulennials
// five whose union is 353 206 cells.
func TestGoldenMapTables(t *testing.T) {
	for _, c := range []struct {
		sub, world    string
		tables, union int
	}{{"chunked", "MuleVikings", 1, 63338}, {"legacy", "Mulennials", 5, 353206}} {
		dir := golden(t, c.sub)
		union := make([]bool, MapCells)
		tables := 0
		keep := ReadOptions{KeepBytes: func(p int32) bool { return p == MapTablePrefab }}
		_, err := ReadWith(dir, c.world, keep, func(z *ZDO) {
			b, ok := z.ByteArrays[MapDataKey]
			if z.Prefab != MapTablePrefab || !ok {
				return
			}
			flags, err := DecodeMapData(b)
			if err != nil {
				t.Errorf("%s: table at %v: %v", c.world, z.Pos, err)
				return
			}
			tables++
			for i, f := range flags {
				if f != 0 {
					union[i] = true
				}
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, u := range union {
			if u {
				n++
			}
		}
		if tables != c.tables || n != c.union {
			t.Errorf("%s: %d tables, %d cells; want %d, %d", c.world, tables, n, c.tables, c.union)
		}
	}
}
```

- [ ] **Step 4: Run them to see them fail**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH go test ./internal/save/`
Expected: FAIL to compile, with `undefined: DecodeMapData`, `undefined: MapCells`, `undefined: ReadOptions`, `undefined: ReadWith`, `undefined: MapTablePrefab`, `undefined: MapDataKey` and `z.ByteArrays undefined`.

- [ ] **Step 5: Keep byte arrays for chosen prefabs**

`internal/save/zdo.go`: replace the import line `import "github.com/jumpingmushroom/farsight/internal/zpkg"` with:

```go
import (
	"bytes"

	"github.com/jumpingmushroom/farsight/internal/zpkg"
)
```

Add the field at the end of `type ZDO struct`:

```go
	Strings map[int32]string
	// ByteArrays holds the ZDO's byte arrays, but only for the prefabs a
	// reader's ReadOptions.KeepBytes selects; it is nil for every other ZDO.
	ByteArrays map[int32][]byte
}
```

Replace the doc comment and signature line of `DecodeZDO` with:

```go
// DecodeZDO reads one ZDO record written by the given world version. Its
// byte arrays are skipped.
func DecodeZDO(r *zpkg.Reader, version int32, z *ZDO) error {
	return decodeZDO(r, version, z, nil)
}

// decodeZDO is DecodeZDO, keeping the byte arrays when keep (if non-nil)
// selects the ZDO's prefab. They are copied: r's buffer is the whole file.
func decodeZDO(r *zpkg.Reader, version int32, z *ZDO, keep func(prefab int32) bool) error {
```

(The old `DecodeZDO` body becomes `decodeZDO`'s body unchanged, except for the byte-array block below.) Replace the byte-array block at the end with:

```go
	if flags&flagByteArrays != 0 {
		n := count()
		want := keep != nil && keep(z.Prefab)
		if want {
			z.ByteArrays = make(map[int32][]byte)
		}
		for i := 0; i < n; i++ {
			k := r.I32()
			b := r.ByteArray()
			if want && r.Err() == nil {
				z.ByteArrays[k] = bytes.Clone(b)
			}
		}
	}
	return r.Err()
}
```

The prefab hash is read before any data section, so `keep` sees the right prefab.

`internal/save/world.go`: replace `Read` with:

```go
// ReadOptions tunes how Read decodes ZDOs.
type ReadOptions struct {
	// KeepBytes, if non-nil, picks the prefabs whose byte arrays are kept
	// in ZDO.ByteArrays. Every other ZDO's byte arrays are skipped, so
	// memory doesn't grow with the world.
	KeepBytes func(prefab int32) bool
}

// Read streams every ZDO of the newest save to fn and returns the world
// metadata. fn's argument is reused between calls. Byte arrays are skipped.
func Read(worldsDir, worldName string, fn func(*ZDO)) (*World, error) {
	return ReadWith(worldsDir, worldName, ReadOptions{}, fn)
}

// ReadWith is Read with options.
func ReadWith(worldsDir, worldName string, opt ReadOptions, fn func(*ZDO)) (*World, error) {
	_, f, err := LatestSave(worldsDir, worldName)
	if err != nil {
		return nil, err
	}
	if f == FormatChunked {
		return readChunked(filepath.Join(worldsDir, worldName), opt, fn)
	}
	return readLegacy(filepath.Join(worldsDir, worldName+".db"), filepath.Join(worldsDir, worldName+".fwl"), opt, fn)
}
```

`internal/save/chunked.go`:
- `func readChunkFile(b []byte, fn func(*ZDO)) (int, error) {` becomes `func readChunkFile(b []byte, opt ReadOptions, fn func(*ZDO)) (int, error) {`, and inside it `if err := DecodeZDO(r, ver, &z); err != nil {` becomes `if err := decodeZDO(r, ver, &z, opt.KeepBytes); err != nil {`.
- `func readChunked(dir string, fn func(*ZDO)) (*World, error) {` becomes `func readChunked(dir string, opt ReadOptions, fn func(*ZDO)) (*World, error) {`, and inside it `got, err := readChunkFile(b, fn)` becomes `got, err := readChunkFile(b, opt, fn)`.

`internal/save/legacy.go`:
- `func readLegacy(dbPath, fwlPath string, fn func(*ZDO)) (*World, error) {` becomes `func readLegacy(dbPath, fwlPath string, opt ReadOptions, fn func(*ZDO)) (*World, error) {`.
- Inside it, `if err := DecodeZDO(r, ver, &z); err != nil {` becomes `if err := decodeZDO(r, ver, &z, opt.KeepBytes); err != nil {`.

Update the tests that call the unexported readers (exact, verified substitutions):

```bash
cd /workspace/Farsight
sed -i 's/readChunked(filepath.Join(dir, "Test"), /readChunked(filepath.Join(dir, "Test"), ReadOptions{}, /' internal/save/chunked_test.go
sed -i 's/\.fwl"), func(/.fwl"), ReadOptions{}, func(/' internal/save/legacy_test.go
sed -i 's/readChunkFile(data, func(\*ZDO) {})/readChunkFile(data, ReadOptions{}, func(*ZDO) {})/' internal/save/fuzz_test.go
grep -c "ReadOptions{}" internal/save/chunked_test.go internal/save/legacy_test.go internal/save/fuzz_test.go
```

Expected grep counts: `chunked_test.go:4`, `legacy_test.go:3`, `fuzz_test.go:1`.

- [ ] **Step 6: The table blob decoder**

Create `internal/save/maptable.go`:

```go
package save

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

// A cartography table (piece_cartographytable) keeps the shared map its
// players recorded in its byte array "data": a gzip-compressed ZPackage of
// int32 version (2 or 3), int32 n (MapCells), then n bytes, one bool per
// minimap cell, then pins (ignored here).
var (
	MapTablePrefab = names.StableHash("piece_cartographytable")
	MapDataKey     = names.StableHash("data")
)

// MapCells is the number of cells in the game's 2048×2048 minimap grid.
const MapCells = 2048 * 2048

// DecodeMapData decodes a table's "data" byte array into its MapCells
// explored flags (non-zero = explored), row-major from the south-west
// corner. Only the header and the cells are decompressed: the pins after
// them are never read, which also bounds memory to MapCells bytes.
func DecodeMapData(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("save: map data: %w", err)
	}
	defer zr.Close()
	head := make([]byte, 8)
	if _, err := io.ReadFull(zr, head); err != nil {
		return nil, fmt.Errorf("save: map data header: %w", err)
	}
	r := zpkg.NewReader(head)
	version, n := r.I32(), r.I32()
	if version != 2 && version != 3 {
		return nil, fmt.Errorf("save: map data version %d", version)
	}
	if n != MapCells {
		return nil, fmt.Errorf("save: map data has %d cells, want %d", n, MapCells)
	}
	flags := make([]byte, MapCells)
	if _, err := io.ReadFull(zr, flags); err != nil {
		return nil, fmt.Errorf("save: map data cells: %w", err)
	}
	return flags, nil
}
```

- [ ] **Step 7: Run the tests**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH gofmt -l internal/save && PATH=$HOME/.local/go/bin:$PATH go vet ./internal/save/... && PATH=$HOME/.local/go/bin:$PATH go test ./internal/save/... -run 'Map|ReadWith|Golden' -v`
Expected: `gofmt -l` prints nothing. These pass: `TestDecodeMapDataVersions`, `TestDecodeMapDataRejectsBadInput`, `TestReadWithKeepsBytesOnlyForChosenPrefabs`, and `TestGoldenMapTables` (or SKIP without `testdata-golden/`), with 1 table / 63 338 cells for MuleVikings and 5 tables / 353 206 cells for Mulennials.

Then run everything: `PATH=$HOME/.local/go/bin:$PATH go test ./...`. Expected: all `ok`. `TestDecodeAllSections` still passes, because `DecodeZDO` still discards byte arrays.

- [ ] **Step 8: Commit**

```bash
cd /workspace/Farsight
git add internal/save/zdo.go internal/save/world.go internal/save/chunked.go internal/save/legacy.go \
  internal/save/maptable.go internal/save/maptable_test.go internal/save/savetest/savetest.go \
  internal/save/chunked_test.go internal/save/legacy_test.go internal/save/fuzz_test.go internal/save/golden_test.go
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(save): keep cartography-table byte arrays; decode the table's map

ReadWith takes ReadOptions.KeepBytes, so only the chosen prefabs keep
their byte arrays (copied). DecodeMapData reads a table's "data": gzip
of version 2|3, 2048² cells, one bool each.

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
```

---


### Task 2: the `explored` package: mask, union, 100 m reveal, zone fallback, encoding

**Files:**
- Create: `internal/explored/explored.go`, `internal/explored/encode.go`
- Test: `internal/explored/explored_test.go`, `internal/explored/encode_test.go`, `internal/explored/golden_test.go`

**Interfaces:**
- Consumes (Task 1): `save.ReadWith`, `save.ReadOptions`, `save.MapTablePrefab`, `save.MapDataKey` and `save.DecodeMapData`. Only `golden_test.go` uses these; the package itself imports no `save`.
- Produces:
  - Constants: `explored.Size = 2048`, `explored.CellMetres = 12`, `explored.RevealCells = 9`, `explored.ZoneShrinkCells = 20`, `explored.WorldRadius = 10500.0`, `explored.SourceTables = "tables"`, `explored.SourceZones = "zones"`.
  - `type explored.Mask` with:
    - `New() *Mask`
    - `(*Mask).Get(px, py int) bool`, `Set(px, py int)`, `At(x, z float64) bool`
    - `Count() int`, `Bits() []byte`, `Clone() *Mask`, `Union(o *Mask)`
    - `AddFlags(flags []byte) error`, `Reveal(px, py int)`
    - `Erode(k int) *Mask`, `Percent() float64`
  - `func explored.CellOf(x, z float64) (px, py int)` and `func explored.FromZones(zones [][2]int16) *Mask`
  - `type explored.Encoded struct { Source string; Cell int; Size int; Bits string }`, with JSON tags `source`, `cell`, `size` and `bits`
  - `func explored.Encode(m *Mask, source string) Encoded` and `func explored.Decode(e Encoded) (*Mask, error)`

**How `ZoneShrinkCells` is chosen.** `TestGoldenZoneShrinkCalibration` reads the real MuleVikings save. Its reference is what the agent sends with tables: the tables' union plus the 100 m reveal around built pieces. It scores the zone fallback (`FromZones(zones).Erode(k)` plus the same reveal) by intersection-over-union for every k from 0 to 40, and fails unless the best k equals the constant.

The erosion is a square: a cell survives only if every cell within k along both axes is explored. A Euclidean (disc) erosion scored lower at its own best (0.708 vs 0.719).

While this plan was written, the sweep peaked at **k = 20 (IoU 0.7188)** on MuleVikings, up from 0.4354 at k = 0. Mulennials also peaks at k = 20 (0.6465), which `TestGoldenZoneFallbackOnMulennials` pins at ≥ 0.64. If the golden data ever changes and the test reports another best k, set `ZoneShrinkCells` to that k and say so in the commit.

- [ ] **Step 1: Write the failing tests**

Create `internal/explored/explored_test.go`:

```go
package explored

import (
	"math"
	"testing"
)

func TestCellOfUsesBankersRounding(t *testing.T) {
	cases := []struct {
		x, z   float64
		px, py int
	}{
		{0, 0, 1024, 1024},
		{6, -6, 1024, 1024},   // ±0.5 rounds to the even 0
		{18, -18, 1026, 1022}, // ±1.5 rounds to ±2
		{30, -30, 1026, 1022}, // ±2.5 rounds to ±2
		{11.9, -12.1, 1025, 1023},
		{-6, 6.01, 1024, 1025},
	}
	for _, c := range cases {
		if px, py := CellOf(c.x, c.z); px != c.px || py != c.py {
			t.Errorf("CellOf(%v, %v) = %d,%d; want %d,%d", c.x, c.z, px, py, c.px, c.py)
		}
	}
}

func TestBitLayoutIsRowMajorLSBFirst(t *testing.T) {
	m := New()
	m.Set(3, 0)
	m.Set(0, 1)
	m.Set(Size-1, Size-1)
	b := m.Bits()
	if len(b) != Size*Size/8 || b[0] != 1<<3 || b[Size/8] != 1 || b[len(b)-1] != 0x80 || m.Count() != 3 {
		t.Fatalf("len=%d b[0]=%#x b[256]=%#x last=%#x count=%d", len(b), b[0], b[Size/8], b[len(b)-1], m.Count())
	}
	if !m.Get(3, 0) || m.Get(4, 0) || m.Get(-1, 0) || m.Get(0, Size) {
		t.Fatal("Get disagrees with Set, or an off-grid cell reads as explored")
	}
	m.Set(-1, 5) // off-grid: ignored, no panic
	m.Set(5, Size)
	if m.Count() != 3 {
		t.Fatalf("off-grid Set changed the mask: count %d", m.Count())
	}
}

func TestAtAddFlagsAndUnion(t *testing.T) {
	flags := make([]byte, Size*Size)
	flags[1024*Size+1025] = 1 // the cell under world (12, 0)
	a := New()
	if err := a.AddFlags(flags); err != nil {
		t.Fatal(err)
	}
	if !a.At(12, 0) || a.At(0, 0) || a.Count() != 1 {
		t.Fatalf("At(12,0)=%v At(0,0)=%v count=%d", a.At(12, 0), a.At(0, 0), a.Count())
	}
	if err := a.AddFlags(flags[:10]); err == nil {
		t.Fatal("AddFlags accepted the wrong length")
	}
	b := New()
	b.Set(7, 7)
	a.Union(b)
	if a.Count() != 2 || !a.Get(7, 7) || b.Count() != 1 {
		t.Fatalf("union: a=%d b=%d", a.Count(), b.Count())
	}
}

func TestRevealIsTheGamesNineCellDisc(t *testing.T) {
	m := New()
	m.Reveal(1024, 1024)
	// Lattice points with dx²+dy² ≤ 81 (Gauss's circle problem, r = 9).
	if m.Count() != 253 {
		t.Fatalf("reveal covers %d cells, want 253", m.Count())
	}
	if !m.Get(1024+9, 1024) || m.Get(1024+10, 1024) || !m.Get(1024+5, 1024+7) || m.Get(1024+7, 1024+7) {
		t.Fatal("reveal edge cells wrong")
	}
	edge := New()
	edge.Reveal(0, Size-1) // clipped at the grid corner, no panic
	if edge.Count() == 0 || edge.Count() >= 253 {
		t.Fatalf("corner reveal = %d cells", edge.Count())
	}
}

func TestFromZonesFillsEachZonesCells(t *testing.T) {
	m := FromZones([][2]int16{{0, 0}, {1, 0}, {-1, -1}, {200, 0}})
	// Zone 0 spans centres −24…24 (5 cells), zone 1 36…84 (5), zone −1
	// −96…−36 (6); zone 200 is off the grid.
	if m.Count() != 5*5+5*5+6*6 {
		t.Fatalf("count = %d, want %d", m.Count(), 5*5+5*5+6*6)
	}
	for _, p := range [][2]float64{{0, 0}, {29, -29}, {84, 24}, {-90, -90}, {-36, -36}} {
		if !m.At(p[0], p[1]) {
			t.Errorf("(%v, %v) not explored", p[0], p[1])
		}
	}
	for _, p := range [][2]float64{{-40, -30}, {90, 0}, {0, 40}} {
		if m.At(p[0], p[1]) {
			t.Errorf("(%v, %v) explored", p[0], p[1])
		}
	}
}

func TestErodeShrinksBySquare(t *testing.T) {
	m := New()
	for py := 100; py < 130; py++ {
		for px := 200; px < 230; px++ {
			m.Set(px, py)
		}
	}
	e := m.Erode(5)
	if e.Count() != 20*20 || !e.Get(205, 105) || e.Get(204, 105) || e.Get(225, 125) {
		t.Fatalf("eroded count %d", e.Count())
	}
	if m.Count() != 900 {
		t.Fatal("Erode modified its receiver")
	}
	if c := m.Erode(0); c.Count() != 900 {
		t.Fatalf("Erode(0) count %d", c.Count())
	}
	full := New()
	for i := range full.bits {
		full.bits[i] = 0xFF
	}
	// Off-grid counts as unexplored: the border k cells go.
	if got, want := full.Erode(3).Count(), (Size-6)*(Size-6); got != want {
		t.Fatalf("full eroded = %d, want %d", got, want)
	}
}

func TestPercentOfTheWorldDisc(t *testing.T) {
	if p := New().Percent(); p != 0 {
		t.Fatalf("empty = %v", p)
	}
	full := New()
	for i := range full.bits {
		full.bits[i] = 0xFF
	}
	if p := full.Percent(); p != 100 {
		t.Fatalf("full = %v", p)
	}
	north := New()
	var in, total int
	for py := 0; py < Size; py++ {
		for px := 0; px < Size; px++ {
			if math.Hypot(float64((px-1024)*12), float64((py-1024)*12)) > 10500 {
				continue
			}
			total++
			if py >= 1024 {
				north.Set(px, py)
				in++
			}
		}
	}
	if want := math.Round(float64(in)/float64(total)*1000) / 10; north.Percent() != want {
		t.Fatalf("north half = %v, want %v", north.Percent(), want)
	}
}
```

Create `internal/explored/encode_test.go`:

```go
package explored

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"math/rand/v2"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	m := New()
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 5000; i++ {
		m.Set(r.IntN(Size), r.IntN(Size))
	}
	e := Encode(m, SourceTables)
	if e.Source != SourceTables || e.Cell != 12 || e.Size != 2048 || e.Bits == "" {
		t.Fatalf("encoded header = %+v", e)
	}
	got, err := Decode(e)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bits(), m.Bits()) {
		t.Fatal("round trip changed the bits")
	}
	// The wire form is plain gzip + base64 of the bitset.
	gz, _ := base64.StdEncoding.DecodeString(e.Bits)
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	raw.ReadFrom(zr)
	if !bytes.Equal(raw.Bytes(), m.Bits()) {
		t.Fatal("bits field is not gzip+base64 of the bitset")
	}
}

func gzB64(b []byte) string {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestDecodeRejectsBadEncodings(t *testing.T) {
	good := Encode(New(), SourceZones)
	cases := map[string]Encoded{
		"source": {Source: "guess", Cell: 12, Size: 2048, Bits: good.Bits},
		"cell":   {Source: SourceZones, Cell: 64, Size: 2048, Bits: good.Bits},
		"size":   {Source: SourceZones, Cell: 12, Size: 1024, Bits: good.Bits},
		"base64": {Source: SourceZones, Cell: 12, Size: 2048, Bits: "!!!"},
		"gzip":   {Source: SourceZones, Cell: 12, Size: 2048, Bits: base64.StdEncoding.EncodeToString([]byte("plain"))},
		"short":  {Source: SourceZones, Cell: 12, Size: 2048, Bits: gzB64(make([]byte, Size*Size/8-1))},
		"long":   {Source: SourceZones, Cell: 12, Size: 2048, Bits: gzB64(make([]byte, Size*Size/8+1))},
	}
	for name, e := range cases {
		if _, err := Decode(e); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := Decode(good); err != nil {
		t.Fatalf("good: %v", err)
	}
}
```

Create `internal/explored/golden_test.go`:

```go
package explored

import (
	"math/bits"
	"os"
	"path/filepath"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save"
)

// goldenTruth reads a real save (skipping when testdata-golden/ is absent)
// and returns what the agent sends with tables, the 100 m reveal around
// built pieces on its own, and the save's generated zones.
func goldenTruth(t *testing.T, sub, world string) (truth, reveal *Mask, zones [][2]int16) {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata-golden", sub)
	if _, err := os.Stat(dir); err != nil {
		t.Skip("golden data missing; run hack/pull-golden.sh")
	}
	creator := names.StableHash("creator")
	tables, reveal := New(), New()
	keep := save.ReadOptions{KeepBytes: func(p int32) bool { return p == save.MapTablePrefab }}
	w, err := save.ReadWith(dir, world, keep, func(z *save.ZDO) {
		if z.Longs[creator] != 0 {
			reveal.Reveal(CellOf(float64(z.Pos[0]), float64(z.Pos[2])))
		}
		if b, ok := z.ByteArrays[save.MapDataKey]; ok && z.Prefab == save.MapTablePrefab {
			flags, err := save.DecodeMapData(b)
			if err != nil {
				t.Fatal(err)
			}
			if err := tables.AddFlags(flags); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	truth = tables.Clone()
	truth.Union(reveal)
	return truth, reveal, w.Zones
}

func iou(a, b *Mask) float64 {
	var in, un int
	for i := range a.bits {
		in += bits.OnesCount8(a.bits[i] & b.bits[i])
		un += bits.OnesCount8(a.bits[i] | b.bits[i])
	}
	return float64(in) / float64(un)
}

func fallback(zones *Mask, reveal *Mask, k int) *Mask {
	m := zones.Erode(k)
	m.Union(reveal)
	return m
}

// TestGoldenZoneShrinkCalibration picks ZoneShrinkCells: the zone fallback
// (zones eroded by k cells, plus the reveal around built pieces) is scored
// by intersection-over-union against what the real tables give, for k in
// 0…40, on MuleVikings. The best k must be the constant. If it ever differs
// (new golden data), set ZoneShrinkCells to the k this test reports.
func TestGoldenZoneShrinkCalibration(t *testing.T) {
	truth, reveal, zoneList := goldenTruth(t, "chunked", "MuleVikings")
	zones := FromZones(zoneList)
	best, bestIoU := -1, -1.0
	for k := 0; k <= 40; k++ {
		s := iou(fallback(zones, reveal, k), truth)
		t.Logf("MuleVikings k=%2d IoU=%.4f", k, s)
		if s > bestIoU {
			best, bestIoU = k, s
		}
	}
	if best != ZoneShrinkCells {
		t.Fatalf("best shrink is %d cells (IoU %.4f); ZoneShrinkCells is %d", best, bestIoU, ZoneShrinkCells)
	}
	if bestIoU < 0.71 {
		t.Fatalf("IoU at the best shrink = %.4f, want >= 0.71", bestIoU)
	}
}

// TestGoldenZoneFallbackOnMulennials checks the constant carries over to
// the other save (measured 0.647 there, also its best k).
func TestGoldenZoneFallbackOnMulennials(t *testing.T) {
	truth, reveal, zoneList := goldenTruth(t, "legacy", "Mulennials")
	if s := iou(fallback(FromZones(zoneList), reveal, ZoneShrinkCells), truth); s < 0.64 {
		t.Fatalf("Mulennials IoU = %.4f, want >= 0.64", s)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH go test ./internal/explored/`
Expected: FAIL with `no non-test Go files` / `undefined: New`, `undefined: CellOf`, …

- [ ] **Step 3: Implement the mask**

Create `internal/explored/explored.go`:

```go
// Package explored is the atlas's explored mask: which cells of the game's
// 2048×2048 minimap grid (12 m per cell) have been explored. The agent
// builds it from the cartography tables' shared map (or, without one, from
// the generated zones) plus the game's 100 m reveal around every
// player-built piece; the central app filters pins and draws fog from it.
//
// Cell (px, py) is the game's Minimap.WorldToPixel: px = round(x/12) + 1024,
// py = round(z/12) + 1024 with Unity's banker's rounding, so row 0 is the
// south edge and cell (px, py) is centred on world ((px−1024)·12,
// (py−1024)·12). Cells are stored as a row-major bitset (index py·2048+px),
// least significant bit first in each byte.
package explored

import (
	"fmt"
	"math"
	"math/bits"
	"sync"
)

const (
	// Size is the grid's side in cells.
	Size = 2048
	// CellMetres is one cell's side in metres.
	CellMetres = 12
	// RevealCells is the game's reveal radius around a player (100 m) in
	// cells: Minimap.Explore uses ceil(100/12) and keeps the cells within
	// that many cells of the centre cell.
	RevealCells = 9
	// ZoneShrinkCells is how far the zone fallback erodes the generated
	// zones: the shrink with the highest intersection-over-union against
	// the real table data of the MuleVikings save (see
	// TestGoldenZoneShrinkCalibration).
	ZoneShrinkCells = 20
	// WorldRadius is the playable world's radius in metres.
	WorldRadius = 10500.0

	// SourceTables and SourceZones name where a mask came from.
	SourceTables = "tables"
	SourceZones  = "zones"

	half      = Size / 2
	maskBytes = Size * Size / 8
)

// Mask is a set of explored cells.
type Mask struct{ bits []byte }

// New returns an empty mask.
func New() *Mask { return &Mask{bits: make([]byte, maskBytes)} }

// CellOf is the cell under world point (x, z). It may lie off the grid.
func CellOf(x, z float64) (px, py int) {
	return int(math.RoundToEven(x/CellMetres)) + half, int(math.RoundToEven(z/CellMetres)) + half
}

func inGrid(px, py int) bool { return px >= 0 && px < Size && py >= 0 && py < Size }

// Get reports whether cell (px, py) is explored; off-grid cells are not.
func (m *Mask) Get(px, py int) bool {
	if !inGrid(px, py) {
		return false
	}
	i := py*Size + px
	return m.bits[i>>3]&(1<<(i&7)) != 0
}

// Set marks cell (px, py) explored; off-grid cells are ignored.
func (m *Mask) Set(px, py int) {
	if !inGrid(px, py) {
		return
	}
	i := py*Size + px
	m.bits[i>>3] |= 1 << (i & 7)
}

// At reports whether the cell under world point (x, z) is explored.
func (m *Mask) At(x, z float64) bool { return m.Get(CellOf(x, z)) }

// Count is the number of explored cells.
func (m *Mask) Count() int {
	n := 0
	for _, b := range m.bits {
		n += bits.OnesCount8(b)
	}
	return n
}

// Bits is the raw bitset (row-major, LSB first). Callers must not modify it.
func (m *Mask) Bits() []byte { return m.bits }

// Clone returns an independent copy of m.
func (m *Mask) Clone() *Mask {
	c := New()
	copy(c.bits, m.bits)
	return c
}

// Union adds every cell explored in o to m.
func (m *Mask) Union(o *Mask) {
	for i, b := range o.bits {
		m.bits[i] |= b
	}
}

// AddFlags adds a cartography table's explored flags (save.DecodeMapData:
// Size² bytes, non-zero = explored, same cell order) to m.
func (m *Mask) AddFlags(flags []byte) error {
	if len(flags) != Size*Size {
		return fmt.Errorf("explored: %d flags, want %d", len(flags), Size*Size)
	}
	for i, f := range flags {
		if f != 0 {
			m.bits[i>>3] |= 1 << (i & 7)
		}
	}
	return nil
}

// Reveal marks the game's 100 m reveal around cell (px, py): every cell
// within RevealCells cells of it (Euclidean, in whole cells), as
// Minimap.Explore does around a player.
func (m *Mask) Reveal(px, py int) {
	for dy := -RevealCells; dy <= RevealCells; dy++ {
		for dx := -RevealCells; dx <= RevealCells; dx++ {
			if dx*dx+dy*dy <= RevealCells*RevealCells {
				m.Set(px+dx, py+dy)
			}
		}
	}
}

// zoneCells is the inclusive range of grid indices whose cell centre lies
// in 64 m zone z, i.e. in [64z − 32, 64z + 32) metres (the zone of a point
// is floor((v + 32) / 64)).
func zoneCells(z int16) (lo, hi int) {
	lo = int(math.Ceil(float64(64*int(z)-32)/CellMetres)) + half
	hi = int(math.Ceil(float64(64*int(z)+32)/CellMetres)) - 1 + half
	return lo, hi
}

// FromZones rasterises 64 m zones onto the grid: every cell whose centre
// lies in one of the zones. Zones off the grid are ignored.
func FromZones(zones [][2]int16) *Mask {
	m := New()
	for _, zn := range zones {
		x0, x1 := zoneCells(zn[0])
		y0, y1 := zoneCells(zn[1])
		for py := max(y0, 0); py <= min(y1, Size-1); py++ {
			for px := max(x0, 0); px <= min(x1, Size-1); px++ {
				m.Set(px, py)
			}
		}
	}
	return m
}

// Erode returns m shrunk by k cells: a cell stays explored only if every
// cell within k cells of it along both axes (a (2k+1)² square) is explored.
// Off-grid cells count as unexplored. k <= 0 returns a copy.
func (m *Mask) Erode(k int) *Mask {
	if k <= 0 {
		return m.Clone()
	}
	rows := make([]bool, Size*Size) // eroded along x only
	run := make([]int, Size+1)      // prefix counts of unexplored cells
	for py := 0; py < Size; py++ {
		for px := 0; px < Size; px++ {
			run[px+1] = run[px]
			if !m.Get(px, py) {
				run[px+1]++
			}
		}
		for px := k; px < Size-k; px++ {
			rows[py*Size+px] = run[px+k+1]-run[px-k] == 0
		}
	}
	out := New()
	for px := 0; px < Size; px++ {
		for py := 0; py < Size; py++ {
			run[py+1] = run[py]
			if !rows[py*Size+px] {
				run[py+1]++
			}
		}
		for py := k; py < Size-k; py++ {
			if run[py+k+1]-run[py-k] == 0 {
				out.Set(px, py)
			}
		}
	}
	return out
}

// discRows is, per grid row, the inclusive px range of cells whose centre
// lies within WorldRadius (lo > hi for rows outside it), and the total.
var discRows = sync.OnceValues(func() ([Size][2]int, int) {
	var rows [Size][2]int
	total := 0
	for py := 0; py < Size; py++ {
		rows[py] = [2]int{0, -1}
		z := float64((py - half) * CellMetres)
		for px := 0; px < Size; px++ {
			x := float64((px - half) * CellMetres)
			if x*x+z*z <= WorldRadius*WorldRadius {
				if rows[py][1] < rows[py][0] {
					rows[py][0] = px
				}
				rows[py][1] = px
				total++
			}
		}
	}
	return rows, total
})

// Percent is the share of the cells inside the world disc (centre within
// WorldRadius) that are explored, × 100, rounded to one decimal place.
func (m *Mask) Percent() float64 {
	rows, total := discRows()
	n := 0
	for py, r := range rows {
		for px := r[0]; px <= r[1]; px++ {
			if m.Get(px, py) {
				n++
			}
		}
	}
	return math.Round(float64(n)/float64(total)*1000) / 10
}
```

Create `internal/explored/encode.go`:

```go
package explored

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Encoded is a mask as it travels in snapshots and the snapshot API:
// the bitset (Mask.Bits) gzip'd and then base64'd (standard alphabet,
// padded).
type Encoded struct {
	Source string `json:"source"` // SourceTables or SourceZones
	Cell   int    `json:"cell"`   // CellMetres
	Size   int    `json:"size"`   // Size
	Bits   string `json:"bits"`
}

// Encode packs m, recording where it came from.
func Encode(m *Mask, source string) Encoded {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(m.bits) // a bytes.Buffer never fails
	zw.Close()
	return Encoded{Source: source, Cell: CellMetres, Size: Size, Bits: base64.StdEncoding.EncodeToString(buf.Bytes())}
}

// Decode unpacks e, rejecting anything but this grid's exact encoding.
func Decode(e Encoded) (*Mask, error) {
	if e.Source != SourceTables && e.Source != SourceZones {
		return nil, fmt.Errorf("explored: unknown source %q", e.Source)
	}
	if e.Cell != CellMetres || e.Size != Size {
		return nil, fmt.Errorf("explored: grid %d×%d at %d m, want %d×%d at %d m", e.Size, e.Size, e.Cell, Size, Size, CellMetres)
	}
	gz, err := base64.StdEncoding.DecodeString(e.Bits)
	if err != nil {
		return nil, fmt.Errorf("explored: bits: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("explored: bits: %w", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(io.LimitReader(zr, maskBytes+1))
	if err != nil {
		return nil, fmt.Errorf("explored: bits: %w", err)
	}
	if len(raw) != maskBytes {
		return nil, errors.New("explored: bits are not a 2048×2048 bitset")
	}
	return &Mask{bits: raw}, nil
}
```

- [ ] **Step 4: Run the tests and read the calibration**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH gofmt -l internal/explored && PATH=$HOME/.local/go/bin:$PATH go vet ./internal/explored && PATH=$HOME/.local/go/bin:$PATH go test ./internal/explored -v 2>&1 | grep -E '^(---|ok|FAIL)|k=(18|19|20|21|22) '`
Expected: every test PASSes, with the golden ones SKIPped when `testdata-golden/` is absent. The log shows:

```
golden_test.go:..: MuleVikings k=18 IoU=0.7043
golden_test.go:..: MuleVikings k=19 IoU=0.7164
golden_test.go:..: MuleVikings k=20 IoU=0.7188
golden_test.go:..: MuleVikings k=21 IoU=0.7103
golden_test.go:..: MuleVikings k=22 IoU=0.6923
```

- [ ] **Step 5: Commit**

```bash
cd /workspace/Farsight
git add internal/explored/
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(explored): the 12 m explored mask

A 2048² bitset on the game's minimap grid: union of tables, the game's
9-cell reveal around built pieces, the zone fallback eroded by 20 cells
(best IoU against MuleVikings' real table map: 0.719), the world-disc
percentage, and the gzip+base64 wire encoding.

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
```

---


### Task 3: `extract` and the agent send `snapshot.explored`

**Files:**
- Modify: `internal/extract/snapshot.go` (field `Explored`), `internal/extract/extract.go` (`KeepBytes`, the tables' union, `exploredMask`)
- Modify: `internal/agent/agent.go` (`saveRead` keeps table byte arrays; log the mask's source)
- Test: `internal/extract/explored_test.go` (new), `internal/extract/golden_test.go`, `internal/agent/agent_test.go`

**Interfaces:**
- Consumes:
  - Task 1: `save.ReadWith`, `save.ReadOptions`, `save.MapTablePrefab`, `save.MapDataKey`, `save.DecodeMapData`
  - Task 2: `explored.New`, `explored.FromZones`, `(*Mask).Erode`, `explored.ZoneShrinkCells`, `explored.CellOf`, `(*Mask).Reveal`, `(*Mask).AddFlags`, `explored.Encode`, `explored.SourceTables`, `explored.SourceZones`
- Produces:
  - `extract.Snapshot.Explored *explored.Encoded` with JSON tag `explored,omitempty`. `Finish` always sets it; it is nil only in snapshots decoded from old agents.
  - `func extract.KeepBytes(prefab int32) bool`: true only for `save.MapTablePrefab`.
  - `ExploredZones` is unchanged and still sent, so an older central app keeps working.

- [ ] **Step 1: Write the failing tests**

Create `internal/extract/explored_test.go`:

```go
package extract

import (
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/save"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
)

// decoded is the snapshot's explored mask, decoded.
func decoded(t *testing.T, s *Snapshot) *explored.Mask {
	t.Helper()
	if s.Explored == nil {
		t.Fatal("snapshot has no explored mask")
	}
	m, err := explored.Decode(*s.Explored)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// cellIndex is the bitset index of the cell under world (x, z).
func cellIndex(x, z float64) int {
	px, py := explored.CellOf(x, z)
	return py*explored.Size + px
}

func TestKeepBytesOnlyForTables(t *testing.T) {
	if !KeepBytes(save.MapTablePrefab) || KeepBytes(h("bed")) || KeepBytes(h("sign")) {
		t.Fatal("KeepBytes must select exactly piece_cartographytable")
	}
}

func TestExploredFromTablesPlusRevealAroundPieces(t *testing.T) {
	e := New()
	t1 := z("piece_cartographytable", [3]float32{0, 30, 0})
	t1.Longs = map[int32]int64{h("creator"): 7}
	t1.ByteArrays = map[int32][]byte{save.MapDataKey: savetest.MapData(3, cellIndex(1200, 2400))}
	t2 := z("piece_cartographytable", [3]float32{0, 30, 0})
	t2.ByteArrays = map[int32][]byte{save.MapDataKey: savetest.MapData(2, cellIndex(-1200, 2400))}
	broken := z("piece_cartographytable", [3]float32{5000, 30, 0})
	broken.ByteArrays = map[int32][]byte{save.MapDataKey: []byte("not gzip")}
	unrecorded := z("piece_cartographytable", [3]float32{-5000, 30, 0})
	wall := z("wood_wall", [3]float32{-2000, 40, 500})
	wall.Longs = map[int32]int64{h("creator"): 7}
	for _, zz := range []*save.ZDO{t1, t2, broken, unrecorded, wall} {
		e.Add(zz)
	}
	s := e.Finish(&save.World{Zones: [][2]int16{{40, 40}}}, "x", time.Now())
	if s.Explored.Source != explored.SourceTables {
		t.Fatalf("source = %q, want tables", s.Explored.Source)
	}
	m := decoded(t, s)
	for _, p := range [][2]float64{{1200, 2400}, {-1200, 2400}, {0, 0}, {100, 0}, {-2000, 500}, {-2000, 600}} {
		if !m.At(p[0], p[1]) {
			t.Errorf("(%v, %v) not explored", p[0], p[1])
		}
	}
	// Not the zone (40, 40), not past the 100 m reveal, nothing from the
	// broken or unrecorded tables (only built pieces reveal around them,
	// and those two have no creator).
	for _, p := range [][2]float64{{2560, 2560}, {-2000, 620}, {5000, 0}, {-5000, 0}} {
		if m.At(p[0], p[1]) {
			t.Errorf("(%v, %v) explored", p[0], p[1])
		}
	}
}

func TestExploredFallsBackToShrunkZones(t *testing.T) {
	e := New()
	hut := z("wood_wall", [3]float32{3000, 30, 3000})
	hut.Longs = map[int32]int64{h("creator"): 9}
	e.Add(hut)
	var zones [][2]int16
	for x := int16(-6); x <= 6; x++ {
		for zz := int16(-6); zz <= 6; zz++ {
			zones = append(zones, [2]int16{x, zz})
		}
	}
	// The zones span [−416, 416) m; eroded by 20 cells (240 m) the centre
	// stays, the rim goes.
	s := e.Finish(&save.World{Zones: zones}, "x", time.Now())
	if s.Explored.Source != explored.SourceZones {
		t.Fatalf("source = %q, want zones", s.Explored.Source)
	}
	m := decoded(t, s)
	if !m.At(0, 0) || !m.At(150, -150) || m.At(300, 0) || m.At(0, -400) {
		t.Fatalf("zone fallback: (0,0)=%v (150,-150)=%v (300,0)=%v (0,-400)=%v", m.At(0, 0), m.At(150, -150), m.At(300, 0), m.At(0, -400))
	}
	if !m.At(3000, 3000) || !m.At(3090, 3000) || m.At(3200, 3000) {
		t.Fatal("no 100 m reveal around the built piece")
	}
}
```

In `internal/extract/golden_test.go`, add `"github.com/jumpingmushroom/farsight/internal/explored"` to the imports and change the read in `TestGoldenMuleVikings` to keep table byte arrays:

```go
	w, err := save.ReadWith(dir, "MuleVikings", save.ReadOptions{KeepBytes: KeepBytes}, e.Add)
```

Then append:

```go
// TestGoldenExplored checks the real saves' masks: built from the tables,
// at least the tables' own cells, and every bed and portal on an explored
// cell (the 100 m reveal covers the ones built after the last "Record").
func TestGoldenExplored(t *testing.T) {
	for _, c := range []struct {
		sub, world string
		tableCells int
	}{{"chunked", "MuleVikings", 63338}, {"legacy", "Mulennials", 353206}} {
		dir := filepath.Join("..", "..", "testdata-golden", c.sub)
		if _, err := os.Stat(dir); err != nil {
			t.Skip("golden data missing; run hack/pull-golden.sh")
		}
		e := New()
		w, err := save.ReadWith(dir, c.world, save.ReadOptions{KeepBytes: KeepBytes}, e.Add)
		if err != nil {
			t.Fatal(err)
		}
		s := e.Finish(w, "x", time.Now().UTC())
		if s.Explored == nil || s.Explored.Source != explored.SourceTables {
			t.Fatalf("%s: explored = %+v, want tables", c.world, s.Explored)
		}
		m, err := explored.Decode(*s.Explored)
		if err != nil {
			t.Fatal(err)
		}
		if m.Count() < c.tableCells {
			t.Errorf("%s: %d cells, want >= %d", c.world, m.Count(), c.tableCells)
		}
		for _, mk := range s.Markers {
			if (mk.Kind == "bed" || mk.Kind == "portal") && !m.At(float64(mk.X), float64(mk.Z)) {
				t.Errorf("%s: %s at (%.0f, %.0f) is not explored", c.world, mk.ID, mk.X, mk.Z)
			}
		}
		t.Logf("%s: %d cells explored (%.1f%% of the world)", c.world, m.Count(), m.Percent())
	}
}
```

In `internal/agent/agent_test.go`, add `"github.com/jumpingmushroom/farsight/internal/explored"` to the imports (just above the `extract` import), then append:

```go
func TestSnapshotCarriesTheTablesExploredMask(t *testing.T) {
	s := &sink{}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	dir := t.TempDir()
	px, py := explored.CellOf(600, -600)
	table := savetest.ZDO{Prefab: "piece_cartographytable", Pos: [3]float32{0, 30, 0},
		ByteArrays: map[string][]byte{"data": savetest.MapData(3, py*explored.Size+px)}}
	savetest.WriteChunkedWorld(t, dir, "W", 1, "seed", append([]savetest.ZDO{table}, zdos...), nil, nil, nil)
	if err := newAgent(t, dir, srv.URL).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.count() != 1 || s.posts[0].Explored == nil || s.posts[0].Explored.Source != explored.SourceTables {
		t.Fatalf("posts = %+v", s.posts)
	}
	m, err := explored.Decode(*s.posts[0].Explored)
	if err != nil || !m.At(600, -600) || m.Count() != 1 {
		t.Fatalf("mask: err=%v at=%v count=%d", err, m != nil && m.At(600, -600), m.Count())
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH go test ./internal/extract ./internal/agent`
Expected: FAIL to compile: `undefined: KeepBytes`, `s.Explored undefined (type *Snapshot has no field or method Explored)`.

- [ ] **Step 3: Implement**

Replace `internal/extract/snapshot.go`'s import line and `type Snapshot struct` with:

```go
import (
	"time"

	"github.com/jumpingmushroom/farsight/internal/explored"
)

type Snapshot struct {
	ServerID string `json:"serverId"`
	SaveID   string `json:"saveId"`
	// SavedAt is the game's own save time (save.World.SavedAt); ReadAt is
	// when this process extracted the snapshot from that save.
	SavedAt       time.Time  `json:"savedAt"`
	ReadAt        time.Time  `json:"readAt"`
	Format        string     `json:"format"`
	WorldVersion  int32      `json:"worldVersion"`
	World         WorldInfo  `json:"world"`
	GlobalKeys    []string   `json:"globalKeys"`
	Bosses        []Boss     `json:"bosses"`
	ExploredZones [][2]int16 `json:"exploredZones"`
	// Explored is the 12 m explored mask (cartography tables, or shrunk
	// zones, plus 100 m around built pieces). Nil in snapshots from agents
	// that predate it; ExploredZones stays for central apps that predate it.
	Explored  *explored.Encoded `json:"explored,omitempty"`
	Locations []Marker          `json:"locations"`
	Markers   []Marker          `json:"markers"`
	Bases     []Base            `json:"bases"`
	Players   []Player          `json:"players"`
	Stats     Stats             `json:"stats"`
}
```

In `internal/extract/extract.go`, add `"github.com/jumpingmushroom/farsight/internal/explored"` to the imports (before `names`). Then replace `type Extractor struct` (and add `KeepBytes` after it):

```go
type Extractor struct {
	markers []Marker
	counts  map[string]int
	pieces  []piece
	players map[int64]string
	unknown map[int32]bool
	tables  *explored.Mask // union of the cartography tables' maps; nil until one decodes
}

// KeepBytes picks the prefabs whose byte arrays Add needs (pass it as
// save.ReadOptions.KeepBytes): only cartography tables.
func KeepBytes(prefab int32) bool { return prefab == save.MapTablePrefab }
```

In `Add`, right after the `if id := z.Longs[kCreator]; id != 0 { … }` block, add:

```go
	if z.Prefab == save.MapTablePrefab {
		e.addTable(z)
	}
```

Add these two methods just above `// Finish builds the Snapshot`:

```go
// addTable adds a cartography table's recorded map to the union. A table
// nobody has recorded at has no "data"; an undecodable one is skipped.
func (e *Extractor) addTable(z *save.ZDO) {
	b, ok := z.ByteArrays[save.MapDataKey]
	if !ok {
		return
	}
	flags, err := save.DecodeMapData(b)
	if err != nil {
		return
	}
	if e.tables == nil {
		e.tables = explored.New()
	}
	e.tables.AddFlags(flags) // DecodeMapData returns exactly explored.Size² flags
}

// exploredMask is the snapshot's explored mask: the tables' union, or
// without one the generated zones eroded by explored.ZoneShrinkCells, plus
// the game's 100 m reveal around every player-built piece (someone stood
// there; it also covers what was built after the last "Record").
func (e *Extractor) exploredMask(w *save.World) *explored.Encoded {
	m, source := e.tables, explored.SourceTables
	if m == nil {
		m, source = explored.FromZones(w.Zones).Erode(explored.ZoneShrinkCells), explored.SourceZones
	}
	cells := map[[2]int]struct{}{}
	for _, p := range e.pieces {
		px, py := explored.CellOf(float64(p.X), float64(p.Z))
		cells[[2]int{px, py}] = struct{}{}
	}
	for c := range cells {
		m.Reveal(c[0], c[1])
	}
	enc := explored.Encode(m, source)
	return &enc
}
```

In `Finish`, the struct literal's last two lines become (gofmt aligns them):

```go
		World:    worldInfo(w),
		Explored: e.exploredMask(w),
		Stats:    Stats{ZDOs: w.ZDOCount, Pieces: len(e.pieces), UnknownPrefabs: len(e.unknown)},
	}
```

In `internal/agent/agent.go`, replace the seams' comment and `var` block with the following. Only the comment's last sentence and `saveRead` change:

```go
// saveLatest and saveRead are package-level seams over the save package so
// tests can inject failures (including panics, to exercise Tick's
// recover) without touching real save files. saveRead keeps the byte
// arrays the extractor needs (the cartography tables' maps).
var (
	saveLatest = save.LatestSave
	saveRead   = func(worldsDir, worldName string, fn func(*save.ZDO)) (*save.World, error) {
		return save.ReadWith(worldsDir, worldName, save.ReadOptions{KeepBytes: extract.KeepBytes}, fn)
	}
)
```

Tests that replace `saveRead` keep the same function signature. In `sendPending`'s `a.log.Info("snapshot sent", …)`, add after the `"markers", …, "unknownPrefabs", …,` line:

```go
		"explored", p.snap.Explored.Source,
```

- [ ] **Step 4: Run the tests**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH gofmt -l internal && PATH=$HOME/.local/go/bin:$PATH go vet ./internal/extract ./internal/agent && PATH=$HOME/.local/go/bin:$PATH go test ./internal/extract ./internal/agent -v -run 'Explored|KeepBytes|Tables'`
Expected: PASS. With the golden data, the log shows `MuleVikings: 63818 cells explored (2.7% of the world)` and `Mulennials: 357297 cells explored (14.9% of the world)`.

Then: `PATH=$HOME/.local/go/bin:$PATH go test ./...`. Expected: all `ok`. In `internal/server`, `TestEndToEnd` still passes: the central app doesn't read `explored` yet.

- [ ] **Step 5: Commit**

```bash
cd /workspace/Farsight
git add internal/extract/snapshot.go internal/extract/extract.go internal/extract/explored_test.go \
  internal/extract/golden_test.go internal/agent/agent.go internal/agent/agent_test.go
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(agent): send the explored mask with each snapshot

snapshot.explored is the union of the cartography tables' maps (or the
zones shrunk by 20 cells when no table has data) plus the game's 100 m
reveal around every built piece. exploredZones stays for older central
apps.

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
```

---


### Task 4: fog maths: distance field, alpha, texture port, tile classes, composition

A pure package with no I/O. Task 6 calls it from the tile handler, and Task 5 uses `fog.Key`.

**Files:**
- Create: `internal/fog/fog.go` (`StyleVersion`, `Key`, `MaxZoom`, `TileSize`, `MetresPerPixel`, `EdgeWidth`, `Alpha`)
- Create: `internal/fog/field.go` (`Field`, `NewField`, `Sample`, the exact EDT)
- Create: `internal/fog/texture.go` (`mulberry32`, `PatternSize`, `TexturePixel`)
- Create: `internal/fog/classify.go` (`Class`, `Clear`/`Fog`/`Edge`, `ClassMap`, `NewClassMap`)
- Create: `internal/fog/compose.go` (`FogTile`, `Blend`, `Upscale`)
- Test: `internal/fog/fog_test.go`, `field_test.go`, `texture_test.go`, `compose_test.go`, `bench_test.go`

**Interfaces:**
- Consumes (Task 2): `explored.Mask` (`Get`, `Bits`, `Clone`, `Set`, `Union`), `explored.New`, `explored.FromZones`, `explored.Size`, `explored.CellMetres`, `explored.WorldRadius`, `explored.SourceTables`, `explored.SourceZones`
- Produces:
  - Constants: `fog.StyleVersion = 1`, `fog.MaxZoom = 6`, `fog.TileSize = 256`, `fog.PatternSize = 126`
  - `func fog.Key(source string, m *explored.Mask) string` (16 lowercase hex)
  - `func fog.MetresPerPixel(z int) float64`, `func fog.EdgeWidth(z int) float64`, `func fog.Alpha(d, w float64) float64`
  - `type fog.Field`, `func fog.NewField(m *explored.Mask) *fog.Field`, `func (*fog.Field) Sample(u, v float64) float64` (metres; `u, v` in cells with cell centres on integers)
  - `func fog.TexturePixel(gx, gy int) (r, g, b uint8)`
  - `type fog.Class uint8`, with `fog.Clear`, `fog.Fog`, `fog.Edge` and `String()`
  - `type fog.ClassMap`, `func fog.NewClassMap(f *fog.Field) *fog.ClassMap`, `func (*fog.ClassMap) At(z, x, y int) fog.Class`
  - `func fog.FogTile(z, x, y int) *image.NRGBA`, `func fog.Blend(img *image.NRGBA, f *fog.Field, z, x, y int)`, `func fog.Upscale(parent *image.NRGBA, qx, qy int) *image.NRGBA`

**Definitions the code implements:**
- **Global pixel** (gx, gy) at zoom z has its centre at world x = −10500 + (gx + 0.5)·mpp(z) and world z = 10500 − (gy + 0.5)·mpp(z), where mpp(z) = 21000 / (256·2^z). That is the same pixel grid as the terrain pyramid and the browser's CRS. The texture is anchored to these global pixels, so it is seamless across tiles.
- **Field:** signed distance from each cell's centre to the explored edge, which runs halfway between an explored and an unexplored cell. For an explored cell it is (√EDT_to_nearest_unexplored − 0.5)·12 m; for an unexplored cell it is −(√EDT_to_nearest_explored − 0.5)·12 m. It is quantised to 0.5 m and clamped to ±127 units, and sampled bilinearly.
- **Classes:** a tile's class is decided from the minimum and maximum field value over every cell any of its pixels can sample. *Fog* needs max ≤ 0 and the tile wholly inside the world disc. *Clear* needs min ≥ w, or the tile wholly outside the disc (the terrain is transparent there). Anything else is *Edge*. `TestClassesAreSound` checks this against `Blend` on every z0–z4 tile.

- [ ] **Step 1: Write the failing tests**

Create `internal/fog/fog_test.go`:

```go
package fog

import (
	"regexp"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/explored"
)

func TestKeyChangesOnlyWithExplorationOrStyle(t *testing.T) {
	a := explored.New()
	a.Set(10, 10)
	b := a.Clone()
	k := Key(explored.SourceTables, a)
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(k) {
		t.Fatalf("key %q is not 16 hex digits", k)
	}
	if Key(explored.SourceTables, b) != k {
		t.Fatal("same mask, different key")
	}
	if Key(explored.SourceZones, a) == k {
		t.Fatal("source is not part of the key")
	}
	b.Set(11, 10)
	if Key(explored.SourceTables, b) == k {
		t.Fatal("a newly explored cell kept the key")
	}
}

func TestEdgeWidthAndMetresPerPixel(t *testing.T) {
	if MetresPerPixel(0) != 21000.0/256 || MetresPerPixel(6) != 21000.0/16384 {
		t.Fatalf("mpp z0=%v z6=%v", MetresPerPixel(0), MetresPerPixel(6))
	}
	for z := 0; z <= 5; z++ {
		if EdgeWidth(z) != 57.6 {
			t.Errorf("EdgeWidth(%d) = %v, want 57.6", z, EdgeWidth(z))
		}
	}
	if EdgeWidth(6) != 24*21000.0/16384 {
		t.Errorf("EdgeWidth(6) = %v", EdgeWidth(6))
	}
}

func TestAlphaIsOpaqueAtAndPastTheEdge(t *testing.T) {
	const w = 57.6
	for _, d := range []float64{0, -0.001, -6, -63.5} {
		if Alpha(d, w) != 1 {
			t.Errorf("Alpha(%v) = %v, want exactly 1", d, Alpha(d, w))
		}
	}
	if Alpha(w, w) != 0 || Alpha(63.5, w) != 0 || Alpha(w/2, w) != 0.5 {
		t.Errorf("Alpha(w)=%v Alpha(63.5)=%v Alpha(w/2)=%v", Alpha(w, w), Alpha(63.5, w), Alpha(w/2, w))
	}
	prev := 1.0
	for d := 0.0; d <= w; d += 0.5 {
		if a := Alpha(d, w); a > prev {
			t.Fatalf("Alpha not monotone at %v", d)
		} else {
			prev = a
		}
	}
}
```

Create `internal/fog/field_test.go`:

```go
package fog

import (
	"math/rand/v2"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/explored"
)

func bruteSqEDT(feature []bool, w, h int) []float64 {
	out := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			best := -1.0
			for fy := 0; fy < h; fy++ {
				for fx := 0; fx < w; fx++ {
					if !feature[fy*w+fx] {
						continue
					}
					d := float64((fx-x)*(fx-x) + (fy-y)*(fy-y))
					if best < 0 || d < best {
						best = d
					}
				}
			}
			out[y*w+x] = best
		}
	}
	return out
}

func TestSqEDTMatchesBruteForce(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	for _, c := range []struct {
		w, h    int
		density float64
	}{{1, 1, 1}, {7, 5, 0.2}, {31, 17, 0.05}, {17, 31, 0.5}, {64, 64, 0.01}, {40, 3, 0.1}, {9, 9, 0}} {
		feature := make([]bool, c.w*c.h)
		for i := range feature {
			feature[i] = r.Float64() < c.density
		}
		got := sqEDT(feature, c.w, c.h)
		want := bruteSqEDT(feature, c.w, c.h)
		for i := range want {
			if want[i] < 0 {
				if got[i] < 1e19 {
					t.Fatalf("%dx%d: cell %d = %v with no features, want >= 1e19", c.w, c.h, i, got[i])
				}
				continue
			}
			if float64(got[i]) != want[i] {
				t.Fatalf("%dx%d: cell %d = %v, want %v", c.w, c.h, i, got[i], want[i])
			}
		}
	}
}

// westHalf is explored for every cell with px <= 1023: the edge runs at
// world x = −6 m, halfway between cell 1023 (x = −12) and cell 1024 (x = 0).
func westHalf() *explored.Mask {
	m := explored.New()
	for py := 0; py < explored.Size; py++ {
		for px := 0; px < 1024; px++ {
			m.Set(px, py)
		}
	}
	return m
}

func TestFieldIsSignedHalfCellDistanceClamped(t *testing.T) {
	f := NewField(westHalf())
	for _, c := range []struct {
		px   int
		want float64
	}{{1023, 6}, {1022, 18}, {1024, -6}, {1025, -18}, {900, 63.5}, {1200, -63.5}} {
		if got := f.at(c.px, 700); got != c.want {
			t.Errorf("at(%d) = %v, want %v", c.px, got, c.want)
		}
	}
	if d := f.Sample(1023.5, 700); d != 0 {
		t.Errorf("Sample at the edge = %v, want 0", d)
	}
	if d := f.Sample(1023.25, 700.5); d != 3 {
		t.Errorf("Sample a quarter cell inside = %v, want 3", d)
	}
	if d := f.at(-1, 0); d != -63.5 {
		t.Errorf("off-grid = %v, want -63.5", d)
	}
}
```

Create `internal/fog/texture_test.go`. Its reference numbers were computed by running the browser's own `mulberry32` and noise loop from `web/src/lib/fog.ts` in Node:

```go
package fog

import "testing"

// Reference values computed with the browser code (web/src/lib/fog.ts
// before Plan 6) in Node.
func TestMulberry32MatchesTheBrowser(t *testing.T) {
	r := mulberry32(noiseSeed)
	for i, want := range []float64{0.7248572071548551, 0.37774392520077527, 0.9134336351417005} {
		if got := r.next(); got != want {
			t.Fatalf("draw %d = %v, want %v", i, got, want)
		}
	}
}

func TestNoiseMatchesTheBrowser(t *testing.T) {
	// The browser's Uint8ClampedArray after the noise loop, hashed as
	// s = s*31 + byte (uint32) over R, G, B, A=255 per pixel.
	var s uint32
	for _, p := range noise {
		for _, b := range []uint8{p[0], p[1], p[2], 255} {
			s = s*31 + uint32(b)
		}
	}
	if s != 2230037077 {
		t.Fatalf("noise hash = %d, want 2230037077", s)
	}
	for _, c := range []struct {
		x, y int
		want [3]uint8
	}{{0, 0, [3]uint8{211, 194, 160}}, {1, 0, [3]uint8{205, 188, 154}}, {2, 0, [3]uint8{214, 197, 163}}, {5, 3, [3]uint8{209, 192, 158}}} {
		if got := noise[c.y*PatternSize+c.x]; got != c.want {
			t.Errorf("noise(%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestTextureHatchAndRepeat(t *testing.T) {
	px := func(x, y int) [3]uint8 { r, g, b := TexturePixel(x, y); return [3]uint8{r, g, b} }
	for _, c := range []struct {
		x, y int
		want [3]uint8
	}{
		{0, 0, [3]uint8{200, 182, 149}},     // hatched: x − y = 0
		{1, 0, [3]uint8{205, 188, 154}},     // plain noise
		{5, 3, [3]uint8{209, 192, 158}},     // plain noise
		{125, 125, [3]uint8{203, 185, 151}}, // hatched
	} {
		if got := px(c.x, c.y); got != c.want {
			t.Errorf("TexturePixel(%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
	if px(126, 0) != px(0, 0) || px(-1, -1) != px(125, 125) || px(1000, 2000) != px(1000%126, 2000%126) {
		t.Error("pattern does not repeat every 126 px")
	}
	// Hatching runs on x − y ≡ 0 (mod 9) across pattern borders too.
	for _, p := range [][2]int{{9, 0}, {130, 4}, {-9, 0}, {0, 126}} {
		n := noise[((p[1]%126+126)%126)*126+(p[0]%126+126)%126]
		if px(p[0], p[1]) == n {
			t.Errorf("(%d,%d) is not hatched", p[0], p[1])
		}
	}
}
```

Create `internal/fog/compose_test.go`:

```go
package fog

import (
	"bytes"
	"image"
	"image/color"
	"math/rand/v2"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/explored"
)

// noiseTile is an opaque terrain tile of random colours.
func noiseTile(seed uint64) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, TileSize, TileSize))
	r := rand.New(rand.NewPCG(seed, 99))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.IntN(256))
		if i%4 == 3 {
			img.Pix[i] = 255
		}
	}
	return img
}

func clone(img *image.NRGBA) *image.NRGBA {
	c := *img
	c.Pix = bytes.Clone(img.Pix)
	return &c
}

func TestFogTileIsTheTextureAnchoredToWorldPixels(t *testing.T) {
	a, b := FogTile(3, 2, 5), FogTile(3, 3, 5)
	for _, c := range []struct {
		img  *image.NRGBA
		x, y int
	}{{a, 2, 5}, {b, 3, 5}} {
		for py := 0; py < TileSize; py++ {
			for px := 0; px < TileSize; px++ {
				p := c.img.NRGBAAt(px, py)
				r, g, bl := TexturePixel(c.x*TileSize+px, c.y*TileSize+py)
				if p.R != r || p.G != g || p.B != bl || p.A != 255 {
					t.Fatalf("tile %d,%d pixel %d,%d = %v, want texture", c.x, c.y, px, py, p)
				}
			}
		}
	}
}

func TestBlendDeepFogIsTextureAndClearIsTerrain(t *testing.T) {
	f := NewField(westHalf())
	// z5 tile 15,15 spans x [−656.25, 0]: explored except its east edge
	// (the edge is at x = −6). Tile 16,15 spans x [0, 656.25]: unexplored.
	for _, seed := range []uint64{1, 2} {
		terrain := noiseTile(seed)
		terrain.Pix[3] = 77 // a translucent pixel keeps its alpha
		west := clone(terrain)
		Blend(west, f, 5, 15, 15)
		if !bytes.Equal(west.Pix[:4*200], terrain.Pix[:4*200]) {
			t.Fatal("explored pixels far from the edge changed")
		}
		if west.Pix[4*255] == terrain.Pix[4*255] && west.Pix[4*255+1] == terrain.Pix[4*255+1] {
			t.Error("the pixel next to the edge is not fogged at all")
		}
		east := clone(terrain)
		Blend(east, f, 5, 16, 15)
		want := FogTile(5, 16, 15)
		want.Pix[3] = 77
		if !bytes.Equal(east.Pix, want.Pix) {
			t.Fatal("unexplored pixels are not the texture alone (terrain leaks)")
		}
	}
}

func TestClassesAreSound(t *testing.T) {
	m := explored.FromZones([][2]int16{{0, 0}, {1, 0}, {1, 1}, {-40, 20}, {60, -60}})
	for x := int16(-5); x <= 5; x++ {
		for z := int16(-5); z <= 5; z++ {
			m.Union(explored.FromZones([][2]int16{{x + 30, z + 30}}))
		}
	}
	f := NewField(m)
	c := NewClassMap(f)
	terrain := noiseTile(3)
	seen := map[Class]int{}
	for z := 0; z <= 4; z++ {
		n := 1 << z
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				cl := c.At(z, x, y)
				seen[cl]++
				got := clone(terrain)
				Blend(got, f, z, x, y)
				switch cl {
				case Clear:
					// Outside the world disc a Clear tile shows terrain that
					// is transparent there, so only pixels inside count.
					for py := 0; py < TileSize; py++ {
						for px := 0; px < TileSize; px++ {
							if insideDisc(z, x*TileSize+px, y*TileSize+py) && got.NRGBAAt(px, py) != terrain.NRGBAAt(px, py) {
								t.Fatalf("z%d %d,%d is Clear but Blend changes pixel %d,%d", z, x, y, px, py)
							}
						}
					}
				case Fog:
					if !bytes.Equal(got.Pix, FogTile(z, x, y).Pix) {
						t.Fatalf("z%d %d,%d is Fog but Blend shows terrain", z, x, y)
					}
				}
			}
		}
	}
	if seen[Clear] == 0 || seen[Fog] == 0 || seen[Edge] == 0 {
		t.Fatalf("classes seen = %v, want all three", seen)
	}
}

// insideDisc reports whether global pixel (gx, gy)'s centre at zoom z lies
// within the world radius.
func insideDisc(z, gx, gy int) bool {
	mpp := MetresPerPixel(z)
	wx := -worldRadius + (float64(gx)+0.5)*mpp
	wz := worldRadius - (float64(gy)+0.5)*mpp
	return wx*wx+wz*wz <= worldRadius*worldRadius
}

func TestClassMapOnTheWestHalf(t *testing.T) {
	c := NewClassMap(NewField(westHalf()))
	for _, k := range []struct {
		z, x, y int
		want    Class
	}{
		{5, 5, 16, Clear},  // deep in the explored west
		{5, 16, 16, Fog},   // unexplored, inside the disc
		{5, 15, 16, Edge},  // the explored edge runs through it
		{5, 31, 16, Edge},  // unexplored but crossing the world rim
		{5, 0, 0, Clear},   // wholly outside the disc
		{0, 0, 0, Edge},    // everything
		{6, 31, 33, Edge},  // z6 next to the edge
		{6, 33, 33, Fog},   // z6 east of it
		{6, 20, 40, Clear}, // z6 deep west
	} {
		if got := c.At(k.z, k.x, k.y); got != k.want {
			t.Errorf("z%d %d,%d = %v, want %v", k.z, k.x, k.y, got, k.want)
		}
	}
}

func TestUpscaleIsPremultipliedBilinear(t *testing.T) {
	parent := image.NewNRGBA(image.Rect(0, 0, TileSize, TileSize))
	for i := 0; i < len(parent.Pix); i += 4 {
		copy(parent.Pix[i:], []uint8{10, 20, 30, 255})
	}
	flat := Upscale(parent, 1, 0)
	for i := 0; i < len(flat.Pix); i += 4 {
		if !bytes.Equal(flat.Pix[i:i+4], []uint8{10, 20, 30, 255}) {
			t.Fatalf("flat upscale changed pixel %d: %v", i/4, flat.Pix[i:i+4])
		}
	}
	parent.SetNRGBA(0, 0, color.NRGBA{200, 0, 0, 255})
	parent.SetNRGBA(1, 0, color.NRGBA{0, 0, 0, 0}) // transparent: must not darken the red
	parent.SetNRGBA(0, 1, color.NRGBA{200, 0, 0, 255})
	parent.SetNRGBA(1, 1, color.NRGBA{0, 0, 0, 0})
	up := Upscale(parent, 0, 0)
	if p := up.NRGBAAt(0, 0); p != (color.NRGBA{200, 0, 0, 255}) {
		t.Errorf("(0,0) = %v, want the clamped corner pixel", p)
	}
	if p := up.NRGBAAt(1, 0); p != (color.NRGBA{200, 0, 0, 191}) {
		t.Errorf("(1,0) = %v, want red at 3/4 alpha", p)
	}
	if p := up.NRGBAAt(2, 0); p != (color.NRGBA{200, 0, 0, 64}) {
		t.Errorf("(2,0) = %v, want red at 1/4 alpha", p)
	}
}
```

Create `internal/fog/bench_test.go`:

```go
package fog

import "testing"

func BenchmarkNewField(b *testing.B) {
	m := westHalf()
	for b.Loop() {
		NewField(m)
	}
}

func BenchmarkNewClassMap(b *testing.B) {
	f := NewField(westHalf())
	for b.Loop() {
		NewClassMap(f)
	}
}

func BenchmarkBlendEdgeTile(b *testing.B) {
	f := NewField(westHalf())
	terrain := noiseTile(1)
	img := clone(terrain)
	for b.Loop() {
		copy(img.Pix, terrain.Pix)
		Blend(img, f, 5, 15, 15)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH go test ./internal/fog/`
Expected: FAIL to compile (`undefined: Key`, `undefined: sqEDT`, `undefined: mulberry32`, …).

- [ ] **Step 3: Implement**

Create `internal/fog/fog.go`:

```go
// Package fog draws the atlas's fog of war into map tiles: a signed
// distance field over the explored mask, a smooth alpha band just inside
// the explored edge, and the parchment texture the browser used to paint
// (spec 2026-10-01-fog-tiles-design.md).
package fog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/jumpingmushroom/farsight/internal/explored"
)

// StyleVersion identifies the fog's look and maths: the texture, the alpha
// curve, the edge width and the field. Bump it whenever a change would
// alter a composed tile's pixels. It is part of the fog key, so browsers
// and the tile cache then fetch every fog tile afresh.
const StyleVersion = 1

const (
	// MaxZoom is the highest zoom fog tiles are served at (terrain has
	// z0–z5; z6 terrain is its z5 parent upscaled).
	MaxZoom = 6
	// TileSize is a tile's side in pixels.
	TileSize = 256

	worldRadius = explored.WorldRadius
	worldSpan   = 2 * worldRadius
	half        = explored.Size / 2
	cellMetres  = float64(explored.CellMetres)

	maxEdge    = 57.6 // metres: the widest soft band
	edgePixels = 24   // the band's width in pixels, until maxEdge caps it
)

// Key is the fog key for a mask: the first 16 hex digits of SHA-256 over
// its source, grid size, bitset and StyleVersion. It changes only when
// exploration (or the fog style) changes, not on every save.
func Key(source string, m *explored.Mask) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00", source, explored.Size)
	h.Write(m.Bits())
	fmt.Fprintf(h, "\x00%d", StyleVersion)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// MetresPerPixel is a tile pixel's side at zoom z: the 21 km world square
// spans TileSize·2^z pixels.
func MetresPerPixel(z int) float64 { return worldSpan / TileSize / float64(int(1)<<z) }

// EdgeWidth is the soft band's width at zoom z: 24 pixels, at most 57.6 m.
func EdgeWidth(z int) float64 { return min(maxEdge, edgePixels*MetresPerPixel(z)) }

// Alpha is the fog's opacity at signed distance d (metres, positive inside
// the explored area) for band width w: 1 − smoothstep(0, w, d). It is
// exactly 1 at and beyond the explored edge (d <= 0) and 0 from w inwards.
func Alpha(d, w float64) float64 {
	t := d / w
	if t <= 0 {
		return 1
	}
	if t >= 1 {
		return 0
	}
	return 1 - t*t*(3-2*t)
}

// pixelCell is the grid position (in cells, cell centres on integers)
// under the centre of global pixel (gx, gy) at zoom z.
func pixelCell(z, gx, gy int) (u, v float64) {
	mpp := MetresPerPixel(z)
	wx := -worldRadius + (float64(gx)+0.5)*mpp
	wz := worldRadius - (float64(gy)+0.5)*mpp
	return wx/cellMetres + half, wz/cellMetres + half
}
```

Create `internal/fog/field.go`:

```go
package fog

import (
	"math"

	"github.com/jumpingmushroom/farsight/internal/explored"
)

const (
	quantum  = 0.5 // metres per field unit
	fieldMax = 127 // ±63.5 m: past the widest band (57.6 m), so clamping is invisible
	inf      = 1e20
)

// Field is the signed distance, per explored-mask cell, from the cell's
// centre to the explored edge: positive inside the explored area, negative
// outside. The edge runs halfway between an explored and an unexplored
// cell. Values are int8 in 0.5 m units, clamped to ±63.5 m (4 MiB).
type Field struct{ d []int8 }

// NewField builds m's field with two exact Euclidean distance transforms
// (inside to the nearest unexplored cell, outside to the nearest explored
// one).
func NewField(m *explored.Mask) *Field {
	const n = explored.Size
	in := make([]bool, n*n)
	out := make([]bool, n*n)
	for py := 0; py < n; py++ {
		for px := 0; px < n; px++ {
			i := py*n + px
			in[i] = m.Get(px, py)
			out[i] = !in[i]
		}
	}
	f := &Field{d: make([]int8, n*n)}
	sq := sqEDT(out, n, n)
	for i, e := range in {
		if e {
			f.d[i] = quantise((math.Sqrt(float64(sq[i])) - 0.5) * cellMetres)
		}
	}
	sq = sqEDT(in, n, n)
	for i, e := range in {
		if !e {
			f.d[i] = quantise(-(math.Sqrt(float64(sq[i])) - 0.5) * cellMetres)
		}
	}
	return f
}

func quantise(metres float64) int8 {
	return int8(max(-fieldMax, min(fieldMax, math.Round(metres/quantum))))
}

// at is cell (px, py)'s distance in metres; off the grid is deep fog.
func (f *Field) at(px, py int) float64 {
	if px < 0 || px >= explored.Size || py < 0 || py >= explored.Size {
		return -fieldMax * quantum
	}
	return float64(f.d[py*explored.Size+px]) * quantum
}

// Sample is the distance in metres at grid position (u, v) (in cells, cell
// centres on integers), bilinearly interpolated.
func (f *Field) Sample(u, v float64) float64 {
	fu, fv := math.Floor(u), math.Floor(v)
	tx, ty := u-fu, v-fv
	x, y := int(fu), int(fv)
	a, b := f.at(x, y), f.at(x+1, y)
	c, d := f.at(x, y+1), f.at(x+1, y+1)
	return (a*(1-tx)+b*tx)*(1-ty) + (c*(1-tx)+d*tx)*ty
}

// sqEDT is the exact squared Euclidean distance transform of a w×h grid
// (Felzenszwalb & Huttenlocher): for every cell, the squared distance in
// cells to the nearest feature cell, 0 on features, and >= 1e19 when the
// grid has none.
func sqEDT(feature []bool, w, h int) []float32 {
	g := make([]float32, w*h)
	for i, f := range feature {
		if !f {
			g[i] = inf
		}
	}
	n := max(w, h)
	f := make([]float64, n)
	d := make([]float64, n)
	v := make([]int, n)
	z := make([]float64, n+1)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			f[y] = float64(g[y*w+x])
		}
		edt1(f[:h], d[:h], v, z)
		for y := 0; y < h; y++ {
			g[y*w+x] = float32(d[y])
		}
	}
	for y := 0; y < h; y++ {
		row := g[y*w : (y+1)*w]
		for x := range row {
			f[x] = float64(row[x])
		}
		edt1(f[:w], d[:w], v, z)
		for x := range row {
			row[x] = float32(d[x])
		}
	}
	return g
}

// edt1 is the 1-D squared distance transform of sampled function f into d
// (the lower envelope of parabolas rooted at each sample).
func edt1(f, d []float64, v []int, z []float64) {
	k := 0
	v[0] = 0
	z[0], z[1] = math.Inf(-1), math.Inf(1)
	for q := 1; q < len(f); q++ {
		s := ((f[q] + float64(q*q)) - (f[v[k]] + float64(v[k]*v[k]))) / float64(2*q-2*v[k])
		for s <= z[k] {
			k--
			s = ((f[q] + float64(q*q)) - (f[v[k]] + float64(v[k]*v[k]))) / float64(2*q-2*v[k])
		}
		k++
		v[k] = q
		z[k], z[k+1] = s, math.Inf(1)
	}
	k = 0
	for q := range f {
		for z[k+1] < float64(q) {
			k++
		}
		dq := q - v[k]
		d[q] = float64(dq*dq) + f[v[k]]
	}
}
```

Create `internal/fog/texture.go`:

```go
package fog

import "math"

// The parchment texture, ported exactly from the browser's former fog
// canvas (web/src/lib/fog.ts before Plan 6): #cfbe9c with ±9 grey noise
// per pixel from mulberry32(0x5eedf06) over a 126 px pattern (row-major,
// one draw per pixel, rounded half-to-even and clamped as a canvas
// Uint8ClampedArray does), then 45° hatching rgba(120,98,66,.12) on the
// pixels where x − y ≡ 0 (mod 9). 126 is a multiple of 9, so the pattern
// repeats seamlessly; it is anchored to global pixels at each zoom.
const (
	PatternSize = 126
	hatchEvery  = 9
	noiseAmp    = 18 // ±9
	noiseSeed   = 0x5eedf06
	hatchAlpha  = 0.12
)

var (
	fogBase    = [3]float64{0xcf, 0xbe, 0x9c}
	hatchColor = [3]float64{120, 98, 66}
)

// mulberry32 is the browser's small PRNG, returning floats in [0, 1).
type mulberry32 uint32

func (m *mulberry32) next() float64 {
	*m += 0x6d2b79f5
	t := uint32(*m)
	t = (t ^ t>>15) * (t | 1)
	t ^= t + (t^t>>7)*(t|61)
	return float64(t^t>>14) / 4294967296
}

func clamp255(v float64) float64 { return max(0, min(255, v)) }

func mod(a, n int) int { return ((a % n) + n) % n }

// noise is the pattern before hatching.
var noise = func() (p [PatternSize * PatternSize][3]uint8) {
	r := mulberry32(noiseSeed)
	for i := range p {
		n := (r.next() - 0.5) * noiseAmp
		for c := range 3 {
			p[i][c] = uint8(clamp255(math.RoundToEven(fogBase[c] + n)))
		}
	}
	return p
}()

// pattern is the finished texture tile.
var pattern = func() (p [PatternSize * PatternSize][3]uint8) {
	for i := range p {
		p[i] = noise[i]
		if mod(i%PatternSize-i/PatternSize, hatchEvery) != 0 {
			continue
		}
		for c := range 3 {
			p[i][c] = uint8(clamp255(math.Round(float64(noise[i][c])*(1-hatchAlpha) + hatchColor[c]*hatchAlpha)))
		}
	}
	return p
}()

// TexturePixel is the fog colour at global pixel (gx, gy) of any zoom.
func TexturePixel(gx, gy int) (r, g, b uint8) {
	p := pattern[mod(gy, PatternSize)*PatternSize+mod(gx, PatternSize)]
	return p[0], p[1], p[2]
}
```

Create `internal/fog/classify.go`:

```go
package fog

import "math"

// Class is how a tile is served, decided once per fog key.
type Class uint8

const (
	// Clear: alpha is 0 on every pixel (or the tile lies wholly outside
	// the world disc, where the terrain is transparent): the terrain as is.
	Clear Class = iota + 1
	// Fog: alpha is 1 on every pixel and the tile lies wholly inside the
	// disc: the texture alone, without reading terrain.
	Fog
	// Edge: anything else: terrain blended with fog.
	Edge
)

func (c Class) String() string {
	switch c {
	case Clear:
		return "clear"
	case Fog:
		return "fog"
	case Edge:
		return "edge"
	}
	return "unknown"
}

// ClassMap holds every tile's class at z0–MaxZoom.
type ClassMap struct{ levels [MaxZoom + 1][]Class }

// NewClassMap classifies every tile of every zoom from f.
func NewClassMap(f *Field) *ClassMap {
	c := &ClassMap{}
	for z := 0; z <= MaxZoom; z++ {
		n := 1 << z
		c.levels[z] = make([]Class, n*n)
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				c.levels[z][y*n+x] = f.classify(z, x, y)
			}
		}
	}
	return c
}

// At is tile (z, x, y)'s class; the coordinates must be in range.
func (c *ClassMap) At(z, x, y int) Class { return c.levels[z][y*(1<<z)+x] }

// classify bounds the field over every cell a pixel of tile (z, x, y) can
// sample (bilinear: floor(u) and floor(u)+1). Bilinear sampling never
// leaves the cells' range, so the class is exact where it says Clear or
// Fog and merely conservative where it says Edge.
func (f *Field) classify(z, x, y int) Class {
	span := worldSpan / float64(int(1)<<z)
	x0 := -worldRadius + float64(x)*span
	x1 := x0 + span
	z1 := worldRadius - float64(y)*span
	z0 := z1 - span
	near := math.Hypot(max(x0, min(0, x1)), max(z0, min(0, z1)))
	far := math.Hypot(max(-x0, x1), max(-z0, z1))
	if near > worldRadius {
		return Clear
	}
	mpp := MetresPerPixel(z)
	c0 := int(math.Floor((x0+mpp/2)/cellMetres)) + half
	c1 := int(math.Floor((x1-mpp/2)/cellMetres)) + half + 1
	r0 := int(math.Floor((z0+mpp/2)/cellMetres)) + half
	r1 := int(math.Floor((z1-mpp/2)/cellMetres)) + half + 1
	lo, hi := math.Inf(1), math.Inf(-1)
	for py := r0; py <= r1; py++ {
		for px := c0; px <= c1; px++ {
			d := f.at(px, py)
			lo, hi = min(lo, d), max(hi, d)
		}
	}
	switch {
	case hi <= 0 && far <= worldRadius:
		return Fog
	case lo >= EdgeWidth(z):
		return Clear
	}
	return Edge
}
```

Create `internal/fog/compose.go`:

```go
package fog

import (
	"image"
	"math"
)

// FogTile draws tile (z, x, y) as fog alone: the texture, opaque. It is
// exactly what Blend gives on an opaque terrain tile where alpha is 1, so
// a Fog-class tile needs no terrain.
func FogTile(z, x, y int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, TileSize, TileSize))
	for py := 0; py < TileSize; py++ {
		row := img.Pix[py*img.Stride:]
		for px := 0; px < TileSize; px++ {
			o := px * 4
			row[o], row[o+1], row[o+2] = TexturePixel(x*TileSize+px, y*TileSize+py)
			row[o+3] = 255
		}
	}
	return img
}

// Blend fogs terrain tile img (tile (z, x, y), TileSize² at the origin) in
// place: each pixel's colour moves toward the texture by the fog's alpha
// at the pixel's centre, and keeps its own alpha (outside the world disc
// the terrain is transparent and stays so). A pixel with alpha 0 is left
// byte for byte; one with alpha 1 becomes the texture exactly.
func Blend(img *image.NRGBA, f *Field, z, x, y int) {
	w := EdgeWidth(z)
	var us [TileSize]float64
	for px := range us {
		us[px], _ = pixelCell(z, x*TileSize+px, 0)
	}
	for py := 0; py < TileSize; py++ {
		gy := y*TileSize + py
		_, v := pixelCell(z, 0, gy)
		row := img.Pix[py*img.Stride:]
		for px := 0; px < TileSize; px++ {
			a := Alpha(f.Sample(us[px], v), w)
			if a == 0 {
				continue
			}
			o := px * 4
			r, g, b := TexturePixel(x*TileSize+px, gy)
			if a == 1 {
				row[o], row[o+1], row[o+2] = r, g, b
				continue
			}
			row[o] = mix(row[o], r, a)
			row[o+1] = mix(row[o+1], g, a)
			row[o+2] = mix(row[o+2], b, a)
		}
	}
}

func mix(terrain, fog uint8, a float64) uint8 {
	return uint8(math.Round(float64(terrain)*(1-a) + float64(fog)*a))
}

// Upscale returns quadrant (qx, qy) of z5 tile parent as a TileSize z6
// tile: a 2× bilinear upscale (as a browser draws a scaled tile), in
// premultiplied alpha so the transparent world rim doesn't darken the
// coast, clamped at the parent's edges.
func Upscale(parent *image.NRGBA, qx, qy int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, TileSize, TileSize))
	for oy := 0; oy < TileSize; oy++ {
		sy0, sy1 := taps(qy, oy)
		for ox := 0; ox < TileSize; ox++ {
			sx0, sx1 := taps(qx, ox)
			var acc [4]float64
			for _, t := range [4]struct {
				x, y int
				w    float64
			}{{sx0, sy0, 0.5625}, {sx1, sy0, 0.1875}, {sx0, sy1, 0.1875}, {sx1, sy1, 0.0625}} {
				p := parent.Pix[t.y*parent.Stride+t.x*4:]
				a := float64(p[3]) * t.w
				acc[0] += float64(p[0]) * a
				acc[1] += float64(p[1]) * a
				acc[2] += float64(p[2]) * a
				acc[3] += a
			}
			o := oy*out.Stride + ox*4
			if acc[3] == 0 {
				continue
			}
			out.Pix[o] = uint8(math.Round(acc[0] / acc[3]))
			out.Pix[o+1] = uint8(math.Round(acc[1] / acc[3]))
			out.Pix[o+2] = uint8(math.Round(acc[2] / acc[3]))
			out.Pix[o+3] = uint8(math.Round(acc[3]))
		}
	}
	return out
}

// taps are the two parent pixels output pixel o of quadrant q samples: the
// nearer i (weight 3/4) and its neighbour on the side o's centre leans to
// (weight 1/4), clamped to the parent.
func taps(q, o int) (near, far int) {
	near = q*TileSize/2 + o/2
	far = near - 1
	if o%2 == 1 {
		far = near + 1
	}
	return near, max(0, min(TileSize-1, far))
}
```

- [ ] **Step 4: Run the tests and the benchmarks**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH gofmt -l internal/fog && PATH=$HOME/.local/go/bin:$PATH go vet ./internal/fog && PATH=$HOME/.local/go/bin:$PATH go test ./internal/fog -v 2>&1 | grep -E '^(---|ok|FAIL)'`
Expected: all PASS. These are the key ones:
- `TestNoiseMatchesTheBrowser`: the whole 126² noise pattern hashes to 2230037077, exactly as in the browser.
- `TestSqEDTMatchesBruteForce`
- `TestBlendDeepFogIsTextureAndClearIsTerrain`: no terrain leaks, and clear pixels are untouched.
- `TestClassesAreSound`

Run: `PATH=$HOME/.local/go/bin:$PATH go test ./internal/fog -run xxx -bench . -benchtime 5x`
Expected, roughly, on the dev box: `BenchmarkNewField` ≈ 500 ms/op, `BenchmarkNewClassMap` ≈ 110 ms/op, `BenchmarkBlendEdgeTile` ≈ 1.7 ms/op.

- [ ] **Step 5: Commit**

```bash
cd /workspace/Farsight
git add internal/fog/
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(fog): distance-field fog with the browser's parchment texture

An exact EDT (Felzenszwalb) over the explored mask gives a signed field,
int8 at 0.5 m clamped to ±63.5 m. Alpha is 1 − smoothstep(0, w, d), with
w = min(57.6 m, 24 px), so the band lies inside the explored area. The
texture is ported bit-exactly (mulberry32 0x5eedf06, ±9 grain, 9 px
hatch). Every z0–z6 tile is classified clear, fog or edge.

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
```

---


### Task 5: central app: one decoded world per snapshot, mask filtering, fogKey, exploredPct

Today every card, snapshot and tile request JSON-decodes the latest snapshot blob. This task adds a per-server `worldState`, rebuilt only when a newer snapshot is stored (checked with a cheap id query). It holds the decoded snapshot, the mask (from `explored`, or rasterised from `exploredZones` for old agents), `enc`, `fogKey` and `pct`, and builds the fog data lazily for Task 6.

**Files:**
- Modify: `internal/store/snapshots.go` (`LatestSnapshotID`; `save_id` as a tie-break in `LatestSnapshot`), `internal/store/store_test.go`
- Create: `internal/server/world.go`
- Modify: `internal/server/server.go` (`worlds` field), `internal/server/api.go`, `internal/server/ingest.go`, `internal/server/tiles.go`
- Test: `internal/server/helpers_test.go`, `internal/server/server_test.go`, `internal/server/e2e_test.go`

**Interfaces:**
- Consumes:
  - Task 2: `explored.Decode`, `explored.Encode`, `explored.FromZones`, `explored.Encoded`, `(*Mask).At`, `(*Mask).Percent`, `explored.SourceZones`, `explored.SourceTables`
  - Task 3: `extract.Snapshot.Explored`
  - Task 4: `fog.Key`, `fog.NewField`, `fog.NewClassMap`, `*fog.Field`, `*fog.ClassMap`
- Produces:
  - `func (*store.Store) LatestSnapshotID(ctx context.Context, serverID string) (saveID string, ok bool, err error)`
  - In `package server`:
    - `type worldState struct { saveID string; snap *extract.Snapshot; mask *explored.Mask; enc explored.Encoded; fogKey string; pct float64; … }` with `func (w *worldState) fogData() (*fog.Field, *fog.ClassMap)`
    - `type worldCache`, `func newWorldCache(*store.Store, *slog.Logger) *worldCache`, `func (c *worldCache) get(ctx context.Context, id string) (*worldState, bool, error)`
    - the field `server.worlds *worldCache`
    - `type snapshotJSON` and `func keep[T any](s []T, pass func(T) bool) []T`
  - API: `GET /api/servers/{id}/snapshot` → `{savedAt, fogKey, explored, exploredZones, markers, locations, bases, players}`. `world.exploredPct` in the card is now `mask.Percent()`.
  - Ingest answers 400 `invalid snapshot` when `explored` is present but doesn't decode.
  - Test helpers: `type snapshotView struct{FogKey; Explored}` and `func (e *env) snapshotView(cookie string) snapshotView`
  - `server.latestSnapshot` and the zone helpers (`totalZones`, `countZonesWithin`, `zoneInWorld`, `locationZone`, `exploredPct`) are deleted.

- [ ] **Step 1: Write the failing store test**

In `internal/store/store_test.go`, add before `func TestPruneSnapshotsKeepsLatest`:

```go
func TestLatestSnapshotIDMatchesLatestSnapshot(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, ok, err := s.LatestSnapshotID(ctx, "srv"); err != nil || ok {
		t.Fatalf("empty: ok=%v err=%v", ok, err)
	}
	for _, p := range []struct{ id, at string }{
		{"a", "2026-01-01T00:00:00Z"}, {"c", "2026-01-03T00:00:00Z"}, {"b", "2026-01-02T00:00:00Z"},
		{"d", "2026-01-03T00:00:00Z"}, // ties "c" on saved_at: save_id breaks the tie
	} {
		if _, err := s.PutSnapshot(ctx, "srv", p.id, ms(p.at), ms(p.at), []byte(p.id)); err != nil {
			t.Fatal(err)
		}
	}
	id, ok, err := s.LatestSnapshotID(ctx, "srv")
	blob, _, _, _ := s.LatestSnapshot(ctx, "srv")
	if err != nil || !ok || id != "d" || string(blob) != "d" {
		t.Fatalf("id=%q blob=%q ok=%v err=%v, want d and d", id, blob, ok, err)
	}
}
```

- [ ] **Step 2: Write the failing server tests**

`internal/server/helpers_test.go`: give `testSnapshot` an unexplored marker and base. Replace its doc comment and its `Markers:`/`Bases:` lines so it reads:

```go
// testSnapshot builds an alpha snapshot in the pre-mask format (only
// exploredZones): a 30x30 block of explored zones at the origin plus one
// zone far outside the world radius, and one location, marker and base in
// explored cells and one of each far outside them.
func testSnapshot(saveID string, savedAt time.Time) extract.Snapshot {
	var zones [][2]int16
	for x := int16(0); x < 30; x++ {
		for z := int16(0); z < 30; z++ {
			zones = append(zones, [2]int16{x, z})
		}
	}
	zones = append(zones, [2]int16{200, 0}) // outside 10 500 m: not counted
	return extract.Snapshot{
		ServerID: "alpha",
		SaveID:   saveID,
		SavedAt:  savedAt,
		ReadAt:   savedAt.Add(5 * time.Second),
		World: extract.WorldInfo{
			Name: "Midgard", SeedName: "abcdef", Seed: testSeed, GenVersion: testGen, Day: 42,
			Modifiers: map[string]string{"combat": "hard"}, Flags: []string{"nomap"},
		},
		Bosses: []extract.Boss{
			{Key: "defeated_eikthyr", Name: "Eikthyr", Defeated: true},
			{Key: "defeated_gdking", Name: "The Elder", Defeated: false},
		},
		ExploredZones: zones,
		Locations: []extract.Marker{
			{ID: "loc-kept", Kind: "location", Type: "Eikthyrnir", X: 70, Z: 20},      // zone (1,0): explored
			{ID: "loc-dropped", Kind: "location", Type: "GDKing", X: -5000, Z: -5000}, // zone (-78,-78): not
		},
		Markers: []extract.Marker{
			{ID: "m1", Kind: "pin", X: 1, Z: 2, Label: "home"},
			{ID: "m-far", Kind: "pin", X: -5000, Z: -5000, Label: "far"},
		},
		Bases: []extract.Base{
			{ID: "b1", Name: "Home", X: 3, Z: 4, Radius: 20, Pieces: 100, Builders: []extract.Builder{}},
			{ID: "b-far", Name: "Far", X: -5000, Z: 5000, Radius: 20, Pieces: 50, Builders: []extract.Builder{}},
		},
		Players: []extract.Player{{ID: 1, Name: "Alice"}},
	}
}
```

`internal/server/server_test.go`:

1. Add `"regexp"` to the standard imports (after `"net/http/httptest"`). Add `"github.com/jumpingmushroom/farsight/internal/explored"` after the `config` import.
2. In `TestCard`, replace the line `wantPct := math.Round(900/float64(referenceZoneCount())*100*10) / 10` with:

   ```go
   	// The 30x30 zones cover cells −2…157 on each axis (centres −24…1884 m).
   	wantPct := math.Round(160*160/float64(referenceCellCount())*1000) / 10
   ```

3. Replace `referenceZoneCount` (with its comment) by:

```go
// referenceCellCount counts 12 m cells whose centre lies within 10 500 m,
// independently of the implementation.
func referenceCellCount() int {
	n := 0
	for px := 0; px < 2048; px++ {
		for py := 0; py < 2048; py++ {
			x, z := float64((px-1024)*12), float64((py-1024)*12)
			if x*x+z*z <= 10500*10500 {
				n++
			}
		}
	}
	return n
}
```

4. Delete `TestExploredPct` and `TestLocationZone` entirely.
5. Rename `TestTotalZonesAndSaveInterval` to `TestSaveInterval`, and delete its first three lines (the `totalZones` check), so it starts:

   ```go
   func TestSaveInterval(t *testing.T) {
   	ts := func(secs ...int) []time.Time {
   ```

6. Replace `TestSnapshotAPIFiltersLocations` (with its `// Case 8` comment) by these tests and helpers:

```go
// Case 8: the snapshot API filters markers, locations and bases to the
// explored mask, and carries the mask and its fog key.
func TestSnapshotAPIFiltersToTheExploredMask(t *testing.T) {
	e := newEnv(t)
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-3*time.Minute))); err != nil {
		t.Fatal(err)
	}
	cookie := e.mustUnlock("alpha")
	r := e.get("/api/servers/alpha/snapshot", cookie)
	if r.code != 200 {
		t.Fatalf("snapshot: %d %s", r.code, r.body)
	}
	var s struct {
		SavedAt       string           `json:"savedAt"`
		FogKey        string           `json:"fogKey"`
		Explored      explored.Encoded `json:"explored"`
		ExploredZones [][2]int         `json:"exploredZones"`
		Markers       []map[string]any `json:"markers"`
		Locations     []map[string]any `json:"locations"`
		Bases         []map[string]any `json:"bases"`
		Players       []map[string]any `json:"players"`
	}
	r.json(t, &s)
	ids := func(items []map[string]any) string {
		var out []string
		for _, it := range items {
			out = append(out, it["id"].(string))
		}
		return strings.Join(out, ",")
	}
	if ids(s.Locations) != "loc-kept" || ids(s.Markers) != "m1" || ids(s.Bases) != "b1" {
		t.Errorf("kept locations=%s markers=%s bases=%s", ids(s.Locations), ids(s.Markers), ids(s.Bases))
	}
	if s.SavedAt != rfc(at(-3*time.Minute)) || len(s.ExploredZones) != 901 || len(s.Players) != 1 {
		t.Errorf("snapshot body: savedAt=%s zones=%d players=%d", s.SavedAt, len(s.ExploredZones), len(s.Players))
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(s.FogKey) || s.Explored.Source != explored.SourceZones {
		t.Errorf("fogKey=%q explored.source=%q", s.FogKey, s.Explored.Source)
	}
	m, err := explored.Decode(s.Explored)
	if err != nil || !m.At(70, 20) || m.At(-5000, -5000) {
		t.Errorf("explored mask: err=%v", err)
	}
	if r.header.Get("Cache-Control") != "no-store" {
		t.Errorf("cache-control = %q", r.header.Get("Cache-Control"))
	}
}

// snapshotView is the part of the snapshot API the fog tests read.
type snapshotView struct {
	FogKey   string           `json:"fogKey"`
	Explored explored.Encoded `json:"explored"`
}

func (e *env) snapshotView(cookie string) snapshotView {
	e.t.Helper()
	r := e.get("/api/servers/alpha/snapshot", cookie)
	if r.code != 200 {
		e.t.Fatalf("snapshot: %d %s", r.code, r.body)
	}
	var v snapshotView
	r.json(e.t, &v)
	return v
}

// An old-format snapshot (exploredZones only) and a new one (explored)
// both yield a fog key; the key follows exploration, not saves.
func TestOldAndNewSnapshotFormatsBothYieldAFogKey(t *testing.T) {
	e := newEnv(t)
	cookie := e.mustUnlock("alpha")
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("old", at(-3*time.Minute))); err != nil {
		t.Fatal(err)
	}
	old := e.snapshotView(cookie)
	if old.FogKey == "" || old.Explored.Source != explored.SourceZones {
		t.Fatalf("old format: %+v", old)
	}

	m := explored.New()
	for py := 1000; py < 1100; py++ {
		for px := 1000; px < 1100; px++ {
			m.Set(px, py)
		}
	}
	enc := explored.Encode(m, explored.SourceTables)
	s1 := testSnapshot("new-1", at(-2*time.Minute))
	s1.Explored = &enc
	if err := e.post("alpha", "alpha-token", "snapshot", s1); err != nil {
		t.Fatal(err)
	}
	v1 := e.snapshotView(cookie)
	if v1.Explored.Source != explored.SourceTables || v1.FogKey == old.FogKey || v1.Explored.Bits != enc.Bits {
		t.Fatalf("new format: source=%q key=%q (old %q)", v1.Explored.Source, v1.FogKey, old.FogKey)
	}
	var c cardJSON
	e.get("/api/servers/alpha", cookie).json(t, &c)
	if c.World == nil || c.World.ExploredPct != m.Percent() {
		t.Fatalf("card exploredPct = %+v, want %v", c.World, m.Percent())
	}

	s2 := testSnapshot("new-2", at(-time.Minute)) // a later save, same exploration
	s2.Explored = &enc
	if err := e.post("alpha", "alpha-token", "snapshot", s2); err != nil {
		t.Fatal(err)
	}
	if v2 := e.snapshotView(cookie); v2.FogKey != v1.FogKey {
		t.Fatalf("fog key changed without new exploration: %q -> %q", v1.FogKey, v2.FogKey)
	}
}

func TestIngestRejectsABadExploredMask(t *testing.T) {
	e := newEnv(t)
	good := explored.Encode(explored.New(), explored.SourceTables)
	for name, bad := range map[string]explored.Encoded{
		"cell": {Source: good.Source, Cell: 64, Size: good.Size, Bits: good.Bits},
		"bits": {Source: good.Source, Cell: good.Cell, Size: good.Size, Bits: "AAAA"},
	} {
		snap := testSnapshot("bad-"+name, at(-time.Minute))
		snap.Explored = &bad
		err := e.post("alpha", "alpha-token", "snapshot", snap)
		var se *ingest.StatusError
		if !errors.As(err, &se) || se.Code != 400 {
			t.Errorf("%s: err = %v, want 400", name, err)
		}
	}
}
```

`internal/server/e2e_test.go`: the agent now sends a zone-fallback mask (two zones erode to nothing), so the bed and portals must be player-built for their 100 m reveal to keep the markers. Replace the comment and the first three `zdos` entries:

```go
	// --- Snapshot: the Plan 1 save agent against a synthetic chunked world.
	// No cartography table, so the mask is the zone fallback (two zones
	// shrink to nothing) plus 100 m around the built bed and portals, which
	// keeps all four markers explored.
	worlds := t.TempDir()
	zdos := []savetest.ZDO{
		{Pos: [3]float32{10, 30, 20}, Prefab: "bed", Strings: map[string]string{"ownerName": "Astrid"}, Longs: map[string]int64{"owner": 42, "creator": 42}},
		{Pos: [3]float32{100, 30, 200}, Prefab: "portal_wood", Strings: map[string]string{"tag": "home"}, Longs: map[string]int64{"creator": 42}},
		{Pos: [3]float32{-300, 30, 400}, Prefab: "portal_wood", Strings: map[string]string{"tag": "home"}, Longs: map[string]int64{"creator": 42}},
```

Then extend the `var snap struct` in the snapshot-API check and assert on it:

```go
		ExploredZones [][2]int16 `json:"exploredZones"`
		FogKey        string     `json:"fogKey"`
		Explored      struct {
			Source string `json:"source"`
		} `json:"explored"`
	}
	r.json(t, &snap)
	if snap.Explored.Source != "zones" || len(snap.FogKey) != 16 {
		t.Errorf("explored source = %q, fogKey = %q", snap.Explored.Source, snap.FogKey)
	}
```

- [ ] **Step 3: Run them to see them fail**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH go test ./internal/store ./internal/server`
Expected:
- store: FAIL to compile, `s.LatestSnapshotID undefined`.
- server: FAIL. The new tests compile but fail: `fogKey=""`, the markers aren't filtered (`markers=m1,m-far`), and `TestIngestRejectsABadExploredMask` gets 200.

- [ ] **Step 4: Implement the store query**

In `internal/store/snapshots.go`, change `LatestSnapshot`'s `ORDER BY saved_at DESC` to `ORDER BY saved_at DESC, save_id DESC`. Then add after `LatestSnapshot`:

```go
// LatestSnapshotID returns the save id of the snapshot LatestSnapshot
// would return, without reading its blob, so a caller can cheaply tell
// whether a decoded copy it holds is still current. ok is false if the
// server has no snapshots.
func (s *Store) LatestSnapshotID(ctx context.Context, serverID string) (saveID string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT save_id FROM snapshots
		WHERE server_id = ?
		ORDER BY saved_at DESC, save_id DESC
		LIMIT 1`, serverID).Scan(&saveID)
	switch {
	case err == sql.ErrNoRows:
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("store: latest snapshot id: %w", err)
	}
	return saveID, true, nil
}
```

- [ ] **Step 5: Implement the world cache**

Create `internal/server/world.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/fog"
	"github.com/jumpingmushroom/farsight/internal/store"
)

// worldState is everything derived from one stored snapshot, built once
// and shared by the card, snapshot and tile handlers. It is read-only once
// built (snap included), apart from the fog data built on first use.
type worldState struct {
	saveID string
	snap   *extract.Snapshot
	mask   *explored.Mask
	enc    explored.Encoded // mask as the snapshot API returns it
	fogKey string
	pct    float64

	fogOnce sync.Once
	field   *fog.Field
	classes *fog.ClassMap
}

// newWorldState derives the state for snap. A snapshot from an agent that
// predates the explored mask has only exploredZones; those are rasterised
// onto the same 12 m grid, so both formats feed one code path.
func newWorldState(snap *extract.Snapshot, log *slog.Logger) *worldState {
	w := &worldState{saveID: snap.SaveID, snap: snap}
	if snap.Explored != nil {
		m, err := explored.Decode(*snap.Explored)
		if err == nil {
			w.mask, w.enc = m, *snap.Explored
		} else {
			// Ingest validates the mask, so this is a damaged row.
			log.Warn("server: stored explored mask unreadable; using zones", "server", snap.ServerID, "saveId", snap.SaveID, "err", err)
		}
	}
	if w.mask == nil {
		w.mask = explored.FromZones(snap.ExploredZones)
		w.enc = explored.Encode(w.mask, explored.SourceZones)
	}
	w.fogKey = fog.Key(w.enc.Source, w.mask)
	w.pct = w.mask.Percent()
	return w
}

// fogData returns the distance field and tile classes, building them on
// first use (about half a second, once per fog key).
func (w *worldState) fogData() (*fog.Field, *fog.ClassMap) {
	w.fogOnce.Do(func() {
		w.field = fog.NewField(w.mask)
		w.classes = fog.NewClassMap(w.field)
	})
	return w.field, w.classes
}

// worldCache holds each server's current worldState.
type worldCache struct {
	store *store.Store
	log   *slog.Logger

	mu sync.Mutex
	m  map[string]*worldEntry
}

type worldEntry struct {
	mu sync.Mutex // serialises rebuilds for one server
	st *worldState
}

func newWorldCache(st *store.Store, log *slog.Logger) *worldCache {
	return &worldCache{store: st, log: log, m: make(map[string]*worldEntry)}
}

// get returns server id's current state, decoding the newest snapshot only
// when it differs from the cached one. ok is false if there is none.
func (c *worldCache) get(ctx context.Context, id string) (*worldState, bool, error) {
	saveID, ok, err := c.store.LatestSnapshotID(ctx, id)
	if err != nil || !ok {
		return nil, false, err
	}
	c.mu.Lock()
	e := c.m[id]
	if e == nil {
		e = &worldEntry{}
		c.m[id] = e
	}
	c.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.st != nil && e.st.saveID == saveID {
		return e.st, true, nil
	}
	blob, _, ok, err := c.store.LatestSnapshot(ctx, id)
	if err != nil || !ok {
		return nil, false, err
	}
	var snap extract.Snapshot
	if err := json.Unmarshal(blob, &snap); err != nil {
		return nil, false, err
	}
	e.st = newWorldState(&snap, c.log)
	return e.st, true, nil
}
```

`internal/server/server.go`: add the field and construct it.

```go
type server struct {
	Deps
	tokens *tokenCache
	worlds *worldCache
```

```go
		tokens:         newTokenCache(),
		worlds:         newWorldCache(d.Store, d.Log),
```

- [ ] **Step 6: Use it in the API, ingest and the (still raw) tile route**

`internal/server/api.go`:
1. Imports: remove `"math"`, and add `"github.com/jumpingmushroom/farsight/internal/explored"` after the `config` import.
2. In the `const` block, delete the blank line and `zoneSize    = 64` / `worldRadius = 10500`. The block now ends at `saveTimesN    = 10`.
3. Delete everything from `// totalZones is the number of 64 m zones` down to just before `// saveInterval is the median gap`: that is `totalZones`, `countZonesWithin`, `zoneInWorld`, `locationZone` and `exploredPct`.
4. Delete `latestSnapshot` (with its comment).
5. In `buildCard`, replace the block from `snap, ok, err := s.latestSnapshot(r, srv.ID)` down to `ExploredPct: exploredPct(snap.ExploredZones),` with:

   ```go
   	ws, ok, err := s.worlds.get(ctx, srv.ID)
   	if err != nil {
   		return nil, err
   	}
   	if !ok {
   		return c, nil
   	}
   	snap := ws.snap
   	wj := &worldJSON{
   		Name: snap.World.Name, SeedName: snap.World.SeedName, Day: snap.World.Day,
   		Bosses: snap.Bosses, Modifiers: snap.World.Modifiers, Flags: snap.World.Flags,
   		ExploredPct: ws.pct,
   ```

   `snap` is shared and read-only: the existing `if wj.Bosses == nil { wj.Bosses = … }` lines assign to `wj` only, which is fine.
6. Replace the whole `func (s *server) snapshot` with:

```go
// snapshotJSON is GET /api/servers/{id}/snapshot. Markers, locations and
// bases are filtered to the explored mask (the cell under each point);
// players carry no positions. explored is the mask itself, for the
// browser's search and cursor readout, and fogKey names its fog tiles.
// exploredZones stays until every client has moved to explored.
type snapshotJSON struct {
	SavedAt       string           `json:"savedAt"`
	FogKey        string           `json:"fogKey"`
	Explored      explored.Encoded `json:"explored"`
	ExploredZones [][2]int16       `json:"exploredZones"`
	Markers       []extract.Marker `json:"markers"`
	Locations     []extract.Marker `json:"locations"`
	Bases         []extract.Base   `json:"bases"`
	Players       []extract.Player `json:"players"`
}

func (s *server) snapshot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.unlocked(r, id); !ok {
		notFound(w)
		return
	}
	ws, ok, err := s.worlds.get(r.Context(), id)
	if err != nil {
		s.internalError(w, "snapshot", id, err)
		return
	}
	if !ok {
		notFound(w)
		return
	}
	snap := ws.snap
	in := func(x, z float32) bool { return ws.mask.At(float64(x), float64(z)) }
	inMarker := func(m extract.Marker) bool { return in(m.X, m.Z) }
	writeJSON(w, http.StatusOK, snapshotJSON{
		SavedAt:       rfc3339(snap.SavedAt),
		FogKey:        ws.fogKey,
		Explored:      ws.enc,
		ExploredZones: orEmpty(snap.ExploredZones),
		Markers:       keep(snap.Markers, inMarker),
		Locations:     keep(snap.Locations, inMarker),
		Bases:         keep(snap.Bases, func(b extract.Base) bool { return in(b.X, b.Z) }),
		Players:       orEmpty(snap.Players),
	})
}

// keep returns the elements of s that pass, as a new, never-nil slice.
func keep[T any](s []T, pass func(T) bool) []T {
	out := []T{}
	for _, v := range s {
		if pass(v) {
			out = append(out, v)
		}
	}
	return out
}
```

`internal/server/ingest.go`: add `"github.com/jumpingmushroom/farsight/internal/explored"` to the imports. In `ingestSnapshot`, right after the `snap.ServerID != id || …` check, add:

```go
	if snap.Explored != nil {
		if _, err := explored.Decode(*snap.Explored); err != nil {
			writeError(w, http.StatusBadRequest, "invalid snapshot")
			return
		}
	}
```

`internal/server/tiles.go` (the raw route lives until Task 6): replace

```go
	snap, ok, err := s.latestSnapshot(r, id)
	if err != nil {
		s.internalError(w, "tile", id, err)
		return
	}
	if !ok {
		notFound(w)
		return
	}
	seed, gen := snap.World.Seed, snap.World.GenVersion
```

with

```go
	ws, ok, err := s.worlds.get(r.Context(), id)
	if err != nil {
		s.internalError(w, "tile", id, err)
		return
	}
	if !ok {
		notFound(w)
		return
	}
	seed, gen := ws.snap.World.Seed, ws.snap.World.GenVersion
```

- [ ] **Step 7: Run the tests**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH gofmt -l internal && PATH=$HOME/.local/go/bin:$PATH go vet ./internal/... && PATH=$HOME/.local/go/bin:$PATH go test ./internal/store ./internal/server -v -run 'Latest|Snapshot|Fog|Explored|Card|EndToEnd|SaveInterval' 2>&1 | grep -E '^(---|ok|FAIL)'`
Expected: all PASS, including `TestLatestSnapshotIDMatchesLatestSnapshot`, `TestSnapshotAPIFiltersToTheExploredMask`, `TestOldAndNewSnapshotFormatsBothYieldAFogKey`, `TestIngestRejectsABadExploredMask`, `TestCard`, `TestEndToEnd`.

Then `PATH=$HOME/.local/go/bin:$PATH go test ./...`. Expected: all `ok`.

- [ ] **Step 8: Commit**

```bash
cd /workspace/Farsight
git add internal/store/snapshots.go internal/store/store_test.go internal/server/world.go internal/server/server.go \
  internal/server/api.go internal/server/ingest.go internal/server/tiles.go \
  internal/server/helpers_test.go internal/server/server_test.go internal/server/e2e_test.go
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(server): filter pins to the explored mask; fogKey and exploredPct

Each server's latest snapshot is decoded once into a worldState: its
12 m mask (from explored, or rasterised from exploredZones for older
agents), the fog key and the world-disc percentage. The snapshot API
returns fogKey and explored and filters markers, locations and bases
to the mask; ingest rejects an undecodable mask.

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
```

---


### Task 6: the fog tile endpoint and composer

**Files:**
- Create: `internal/server/tilecache.go`, `internal/server/tilecache_test.go`
- Replace: `internal/server/tiles.go` (the raw `tile` handler is removed)
- Create: `internal/server/tiles_bench_test.go`
- Modify: `internal/server/server.go` (route, `fogTiles` field, package comment)
- Test: `internal/server/helpers_test.go`, `internal/server/server_test.go`

**Interfaces:**
- Consumes:
  - Task 4: `fog.MaxZoom`, `fog.TileSize`, `fog.Clear`, `fog.Fog`, `fog.Edge`, `fog.Class`, `*fog.Field`, `(*fog.ClassMap).At`, `fog.FogTile`, `fog.Blend`, `fog.Upscale`, `fog.TexturePixel`, `fog.NewField`, `fog.NewClassMap`
  - Task 5: `s.worlds.get`, `worldState.fogKey`, `worldState.snap`, `worldState.fogData()`, `env.snapshotView`
  - Existing: `tiles.MaxZoom` (5), `tileset.Manager.Key/Dir/Status`, `parseCoord`
- Produces:
  - Route `GET /tiles/{id}/{key}/{fog}/{z}/{x}/{y}` → `(*server).fogTile`
  - `const fogTileCacheBytes = 64 << 20`
  - `type tileCacheKey struct { server, tiles, fog string; z, x, y int }`
  - `func newTileCache(maxBytes int64, workers int) *tileCache` and `func (c *tileCache) get(k tileCacheKey, compose func() ([]byte, error)) ([]byte, error)`
  - `func composeTile(dir string, f *fog.Field, c fog.Class, z, x, y int) ([]byte, error)` and `func readTerrain(dir string, z, x, y int) (*image.NRGBA, error)`
  - Test helpers: `var testTerrain color.NRGBA`, `func writeTestTiles(dir string) error`, `func (e *env) waitTiles()`, `func decodeTile(t, b) *image.NRGBA`

**Serving rules** (spec §2 "Serving"):
- It is a 404 unless the server is unlocked, `key` is its current *complete* tile set and `fog` is its current fog key.
- A **Clear** tile at z0–z5 is the terrain file served unchanged (`http.ServeFile`).
- A **Fog** tile is the texture alone; it reads no terrain.
- An **Edge** tile is the terrain, decoded, blended and encoded. z6 terrain is its z5 parent's quadrant upscaled 2×.
- Composed PNGs go to the LRU, keyed by (server, tile key, fog key, z, x, y). The tile key is in the cache key so a re-rendered terrain set can never serve a stale composition.
- PNGs are encoded at `png.BestSpeed`, and the headers are `Content-Type: image/png` and `Cache-Control: public, max-age=31536000, immutable`.

- [ ] **Step 1: Write the failing tests**

Create `internal/server/tilecache_test.go`:

```go
package server

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func key(n int) tileCacheKey { return tileCacheKey{server: "alpha", tiles: "k", fog: "f", z: 5, x: n} }

func TestTileCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := newTileCache(10, 1)
	calls := 0
	compose := func(b string) func() ([]byte, error) {
		return func() ([]byte, error) { calls++; return []byte(b), nil }
	}
	c.get(key(1), compose("aaaa"))
	c.get(key(2), compose("bbbb"))
	c.get(key(1), compose("xxxx")) // a hit: refreshes 1
	c.get(key(3), compose("cccc")) // 12 bytes > 10: evicts 2, the least recent
	if calls != 3 || c.size != 8 {
		t.Fatalf("calls=%d size=%d", calls, c.size)
	}
	if b, _ := c.get(key(1), compose("zzzz")); string(b) != "aaaa" {
		t.Errorf("tile 1 = %q, want the cached aaaa", b)
	}
	if b, _ := c.get(key(2), compose("BBBB")); string(b) != "BBBB" {
		t.Errorf("tile 2 = %q, want a fresh compose", b)
	}
	c.get(key(9), compose("this is longer than ten bytes"))
	if _, ok := c.entries[key(9)]; ok {
		t.Error("a tile larger than the whole cache was kept")
	}
}

func TestTileCacheCoalescesConcurrentComposes(t *testing.T) {
	c := newTileCache(1<<20, 4)
	var calls atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]string, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, _ := c.get(key(1), func() ([]byte, error) {
				calls.Add(1)
				<-release
				return []byte("tile"), nil
			})
			results[i] = string(b)
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("compose ran %d times, want 1", calls.Load())
	}
	for i, r := range results {
		if r != "tile" {
			t.Fatalf("caller %d got %q", i, r)
		}
	}
}

func TestTileCacheBoundsConcurrentComposes(t *testing.T) {
	c := newTileCache(1<<20, 2)
	var running, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.get(key(i), func() ([]byte, error) {
				n := running.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				running.Add(-1)
				return []byte("t"), nil
			})
		}()
	}
	wg.Wait()
	if peak.Load() > 2 {
		t.Fatalf("%d composes at once, want at most 2", peak.Load())
	}
}

func TestTileCacheNeverCachesFailures(t *testing.T) {
	c := newTileCache(1<<20, 1)
	boom := errors.New("boom")
	if _, err := c.get(key(1), func() ([]byte, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.get(key(1), func() ([]byte, error) { panic("bad tile") }); err == nil {
		t.Fatal("a panicking compose must return an error")
	}
	if b, err := c.get(key(1), func() ([]byte, error) { return []byte("ok"), nil }); err != nil || string(b) != "ok" {
		t.Fatalf("after failures: %q %v", b, err)
	}
}
```

`internal/server/helpers_test.go`: add `"image"`, `"image/color"` and `"image/png"` to the imports (after `"encoding/json"`). In `newEnvBurst`'s fake `render`, replace the block that writes `"\x89PNG fake"` to `0/0/0.png` (from `p := filepath.Join(dir, "0", "0", "0.png")` through its `os.WriteFile` check) with:

```go
		if err := writeTestTiles(dir); err != nil {
			return err
		}
```

Add after `newEnvBurst`:

```go
// testTerrain is the colour of every tile writeTestTiles writes.
var testTerrain = color.NRGBA{R: 40, G: 120, B: 60, A: 255}

// writeTestTiles writes the few terrain tiles the tests read, each a solid
// testTerrain PNG: z0 (everything), z5 16,15 (x and z 0…656 m: the
// explored block's south-west corner runs through it) and z5 17,14
// (656…1312 m: deep inside the block).
func writeTestTiles(dir string) error {
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = testTerrain.R, testTerrain.G, testTerrain.B, testTerrain.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	for _, t := range [][3]int{{0, 0, 0}, {5, 16, 15}, {5, 17, 14}} {
		p := filepath.Join(dir, strconv.Itoa(t[0]), strconv.Itoa(t[1]), strconv.Itoa(t[2])+".png")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// waitTiles starts the tile worker, lets the fake render finish and waits
// for the set to be complete.
func (e *env) waitTiles() {
	e.t.Helper()
	e.runTiles()
	close(e.release)
	deadline := time.Now().Add(5 * time.Second)
	for e.tiles.Status(testSeed, testGen).State != tileset.StateComplete {
		if time.Now().After(deadline) {
			e.t.Fatal("render did not complete")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
```

`internal/server/server_test.go`:
1. Imports: add `"fmt"`, `"image"`, `"image/color"`, `"image/draw"`, `"image/png"`, `"os"` and `"path/filepath"` to the standard group, and `"github.com/jumpingmushroom/farsight/internal/fog"` after the `explored` import.
2. In `TestLockedRoutesAre404`, the tile path becomes `"/tiles/alpha/" + key + "/0123456789abcdef/0/0/0.png",`.
3. Replace `TestTiles` (and its `// Case 9: tiles.` comment) with:

```go
// Case 9: fog tiles.
func TestFogTiles(t *testing.T) {
	e := newEnv(t)
	cookie := e.mustUnlock("alpha")
	key := e.tiles.Key(testSeed, testGen)
	url := func(fogKey string, z, x, y int) string {
		return fmt.Sprintf("/tiles/alpha/%s/%s/%d/%d/%d.png", key, fogKey, z, x, y)
	}
	if r := e.get(url("0123456789abcdef", 0, 0, 0), cookie); r.code != 404 {
		t.Fatalf("before snapshot: %d", r.code)
	}
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-2*time.Minute))); err != nil {
		t.Fatal(err)
	}
	fk := e.snapshotView(cookie).FogKey
	if r := e.get(url(fk, 0, 0, 0), cookie); r.code != 404 {
		t.Fatalf("tiles still queued: %d", r.code)
	}
	e.waitTiles()

	immutable := func(r resp) {
		t.Helper()
		if cc := r.header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
			t.Errorf("cache-control = %q", cc)
		}
		if ct := r.header.Get("Content-Type"); ct != "image/png" {
			t.Errorf("content-type = %q", ct)
		}
	}

	// Clear (z5 17,14 lies deep inside the explored block): the terrain
	// file, byte for byte.
	r := e.get(url(fk, 5, 17, 14), cookie)
	disk, err := os.ReadFile(filepath.Join(e.tiles.Dir(testSeed, testGen), "5", "17", "14.png"))
	if err != nil || r.code != 200 || !bytes.Equal(r.body, disk) {
		t.Fatalf("clear tile: %d, same as disk %v (%v)", r.code, bytes.Equal(r.body, disk), err)
	}
	immutable(r)

	// Fog (z5 2,16, 8.5 km west of anything explored): the texture alone.
	// Its terrain file was never written, so this path reads no terrain.
	r = e.get(url(fk, 5, 2, 16), cookie)
	if r.code != 200 {
		t.Fatalf("fog tile: %d %s", r.code, r.body)
	}
	immutable(r)
	img := decodeTile(t, r.body)
	for _, p := range [][2]int{{0, 0}, {17, 200}, {255, 255}} {
		cr, cg, cb := fog.TexturePixel(2*256+p[0], 16*256+p[1])
		if c := img.NRGBAAt(p[0], p[1]); c != (color.NRGBA{cr, cg, cb, 255}) {
			t.Errorf("fog tile pixel %v = %v, want the texture", p, c)
		}
	}

	// Edge (z5 16,15: the block's west and south rims run through it):
	// terrain in the middle, partly fogged at the rim.
	r = e.get(url(fk, 5, 16, 15), cookie)
	if r.code != 200 {
		t.Fatalf("edge tile: %d %s", r.code, r.body)
	}
	immutable(r)
	img = decodeTile(t, r.body)
	if c := img.NRGBAAt(128, 128); c != testTerrain {
		t.Errorf("edge tile centre = %v, want terrain %v", c, testTerrain)
	}
	if c := img.NRGBAAt(0, 128); c == testTerrain {
		t.Errorf("edge tile west rim = %v, want fog blended in", c)
	}
	if again := e.get(url(fk, 5, 16, 15), cookie); !bytes.Equal(again.body, r.body) {
		t.Error("a repeat request composed a different tile")
	}

	// z6 32,30: from its z5 parent 16,15, upscaled.
	r = e.get(url(fk, 6, 32, 30), cookie)
	if r.code != 200 {
		t.Fatalf("z6 tile: %d %s", r.code, r.body)
	}
	if c := decodeTile(t, r.body).NRGBAAt(255, 0); c != testTerrain {
		t.Errorf("z6 tile inside the block = %v, want terrain", c)
	}

	for _, p := range []string{
		url("0123456789abcdef", 5, 17, 14),               // stale fog key
		"/tiles/alpha/1-0-r1/" + fk + "/0/0/0.png",       // wrong tiles key
		url(fk, 7, 0, 0),                                 // z out of range
		url(fk, 0, 1, 0),                                 // x out of range for z
		"/tiles/alpha/" + key + "/" + fk + "/0/0/-1.png", // negative
		"/tiles/alpha/" + key + "/" + fk + "/0/0/0",      // no .png
		"/tiles/alpha/" + key + "/" + fk + "/0/0/0.jpg",  // not .png
		"/tiles/alpha/" + key + "/" + fk + "/a/0/0.png",  // not a number
		url(fk, 5, 15, 15),                               // edge tile, terrain missing on disk
		"/tiles/alpha/" + key + "/0/0/0.png",             // the removed raw route
	} {
		if r := e.get(p, cookie); r.code != 404 {
			t.Errorf("%s: %d, want 404", p, r.code)
		}
	}
	if r := e.get(url(fk, 5, 17, 14), ""); r.code != 404 {
		t.Errorf("no cookie: %d", r.code)
	}

	// New exploration: a new fog key; the old one is gone.
	m := explored.New()
	for py := 1000; py < 1100; py++ {
		for px := 1000; px < 1100; px++ {
			m.Set(px, py)
		}
	}
	enc := explored.Encode(m, explored.SourceTables)
	s2 := testSnapshot("s2", at(-time.Minute))
	s2.Explored = &enc
	if err := e.post("alpha", "alpha-token", "snapshot", s2); err != nil {
		t.Fatal(err)
	}
	fk2 := e.snapshotView(cookie).FogKey
	if fk2 == fk || e.get(url(fk, 0, 0, 0), cookie).code != 404 || e.get(url(fk2, 0, 0, 0), cookie).code != 200 {
		t.Errorf("after new exploration: key %q -> %q", fk, fk2)
	}

	var c cardJSON
	e.get("/api/servers/alpha", cookie).json(t, &c)
	if c.Tiles.State != "complete" || c.Tiles.Key != key || c.Tiles.Done != 1 || c.Tiles.Total != 1 {
		t.Errorf("card tiles = %+v", c.Tiles)
	}
}

func decodeTile(t *testing.T, b []byte) *image.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 256 || b.Dy() != 256 {
		t.Fatalf("tile is %v", b)
	}
	out := image.NewNRGBA(img.Bounds())
	draw.Draw(out, out.Rect, img, image.Point{}, draw.Src)
	return out
}
```

4. In `TestGzipJSONCompressesLargePayloadsOnly`, replace the wait loop and the tile request (from `e.runTiles()` through the `tile := e.do(…"/0/0/0.png"…)` line) with:

```go
	e.waitTiles()
	fk := e.snapshotView(cookie).FogKey
	tile := e.do("GET", "/tiles/alpha/"+key+"/"+fk+"/0/0/0.png", nil, map[string]string{"Accept-Encoding": "gzip"}, cookie)
```

Create `internal/server/tiles_bench_test.go`:

```go
package server

import (
	"bytes"
	"image"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/fog"
)

// BenchmarkComposeEdgeTile is one Edge tile end to end: decode the z5
// terrain PNG, blend, encode. The spec's target is about 10 ms. The
// terrain is hillshade-like (smooth with fine noise), harder to compress
// than real tiles.
func BenchmarkComposeEdgeTile(b *testing.B) {
	dir := b.TempDir()
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	r := rand.New(rand.NewPCG(1, 2))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			o := y*img.Stride + x*4
			n := uint8(r.IntN(24))
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = uint8(x/2)+n, uint8(y/2)+n, 90+n, 255
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	p := filepath.Join(dir, "5", "16", "15.png")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, buf.Bytes(), 0o644)

	m := explored.New()
	for py := 0; py < explored.Size; py++ {
		for px := 0; px < 1050; px++ { // the edge at x ≈ 306 m crosses tile 16,15
			m.Set(px, py)
		}
	}
	f := fog.NewField(m)
	if c := fog.NewClassMap(f).At(5, 16, 15); c != fog.Edge {
		b.Fatalf("tile class = %v, want edge", c)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := composeTile(dir, f, fog.Edge, 5, 16, 15); err != nil {
			b.Fatal(err)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH go test ./internal/server`
Expected: FAIL to compile, with `undefined: newTileCache`, `undefined: tileCacheKey` and `undefined: composeTile`.

- [ ] **Step 3: The tile cache**

Create `internal/server/tilecache.go`:

```go
package server

import (
	"container/list"
	"fmt"
	"sync"
)

// fogTileCacheBytes bounds the composed fog tiles kept in memory.
const fogTileCacheBytes = 64 << 20

// tileCacheKey names one composed tile. The tile-set key is part of it so
// a re-rendered terrain set never serves a stale composition.
type tileCacheKey struct {
	server, tiles, fog string
	z, x, y            int
}

// tileCache is a size-bounded LRU of composed fog tiles (encoded PNGs). It
// coalesces concurrent requests for one tile into a single composition and
// bounds how many compositions run at once.
type tileCache struct {
	max int64
	sem chan struct{}

	mu       sync.Mutex
	size     int64
	order    *list.List // of *cacheEntry, most recently used first
	entries  map[tileCacheKey]*list.Element
	inflight map[tileCacheKey]*flight
}

type cacheEntry struct {
	key tileCacheKey
	png []byte
}

// flight is one composition in progress; done closes once png/err are set.
type flight struct {
	done chan struct{}
	png  []byte
	err  error
}

// newTileCache keeps up to maxBytes of PNGs and runs at most workers
// compositions at once.
func newTileCache(maxBytes int64, workers int) *tileCache {
	return &tileCache{
		max:      maxBytes,
		sem:      make(chan struct{}, max(1, workers)),
		order:    list.New(),
		entries:  make(map[tileCacheKey]*list.Element),
		inflight: make(map[tileCacheKey]*flight),
	}
}

// get returns k's PNG, composing it with compose on a miss. Concurrent
// misses for one key share a single compose call; errors are returned to
// every waiter and never cached.
func (c *tileCache) get(k tileCacheKey, compose func() ([]byte, error)) ([]byte, error) {
	c.mu.Lock()
	if el, ok := c.entries[k]; ok {
		c.order.MoveToFront(el)
		b := el.Value.(*cacheEntry).png
		c.mu.Unlock()
		return b, nil
	}
	if f, ok := c.inflight[k]; ok {
		c.mu.Unlock()
		<-f.done
		return f.png, f.err
	}
	f := &flight{done: make(chan struct{})}
	c.inflight[k] = f
	c.mu.Unlock()

	c.sem <- struct{}{}
	f.png, f.err = safeCompose(compose)
	<-c.sem

	c.mu.Lock()
	delete(c.inflight, k)
	if f.err == nil {
		c.add(k, f.png)
	}
	c.mu.Unlock()
	close(f.done)
	return f.png, f.err
}

// safeCompose turns a panic into an error, so waiters are always released.
func safeCompose(compose func() ([]byte, error)) (b []byte, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("server: compose tile: panic: %v", p)
		}
	}()
	return compose()
}

// add stores b under k and evicts least recently used tiles past max.
// c.mu must be held.
func (c *tileCache) add(k tileCacheKey, b []byte) {
	if int64(len(b)) > c.max {
		return
	}
	c.entries[k] = c.order.PushFront(&cacheEntry{key: k, png: b})
	c.size += int64(len(b))
	for c.size > c.max {
		back := c.order.Back()
		e := back.Value.(*cacheEntry)
		c.order.Remove(back)
		delete(c.entries, e.key)
		c.size -= int64(len(e.png))
	}
}
```

- [ ] **Step 4: The handler and composer**

Replace `internal/server/tiles.go` with:

```go
package server

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/jumpingmushroom/farsight/internal/fog"
	"github.com/jumpingmushroom/farsight/internal/tiles"
	"github.com/jumpingmushroom/farsight/internal/tileset"
)

// fogTile serves GET /tiles/{id}/{key}/{fog}/{z}/{x}/{y}.png (z0–z6): the
// server's terrain with its fog of war drawn in. Every failure is the same
// 404 as a locked server, including a key that isn't the current complete
// tile set and a fog key that isn't the current one. A Clear tile at z0–z5
// is the terrain file itself; every other tile is composed, PNG-encoded
// and kept in the tile cache.
func (s *server) fogTile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.unlocked(r, id); !ok {
		notFound(w)
		return
	}
	yStr, ok := strings.CutSuffix(r.PathValue("y"), ".png")
	if !ok {
		notFound(w)
		return
	}
	z, okZ := parseCoord(r.PathValue("z"), fog.MaxZoom+1)
	if !okZ {
		notFound(w)
		return
	}
	x, okX := parseCoord(r.PathValue("x"), 1<<z)
	y, okY := parseCoord(yStr, 1<<z)
	if !okX || !okY {
		notFound(w)
		return
	}

	ws, ok, err := s.worlds.get(r.Context(), id)
	if err != nil {
		s.internalError(w, "tile", id, err)
		return
	}
	if !ok {
		notFound(w)
		return
	}
	seed, gen := ws.snap.World.Seed, ws.snap.World.GenVersion
	key := r.PathValue("key")
	if key != s.Tiles.Key(seed, gen) || s.Tiles.Status(seed, gen).State != tileset.StateComplete ||
		r.PathValue("fog") != ws.fogKey {
		notFound(w)
		return
	}
	field, classes := ws.fogData()
	class := classes.At(z, x, y)
	dir := s.Tiles.Dir(seed, gen)

	if class == fog.Clear && z <= tiles.MaxZoom {
		// Built only from parsed integers: no request string reaches the path.
		p := terrainPath(dir, z, x, y)
		if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
			notFound(w)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(w, r, p)
		return
	}
	b, err := s.fogTiles.get(tileCacheKey{server: id, tiles: key, fog: ws.fogKey, z: z, x: x, y: y},
		func() ([]byte, error) { return composeTile(dir, field, class, z, x, y) })
	if errors.Is(err, fs.ErrNotExist) {
		notFound(w)
		return
	}
	if err != nil {
		s.internalError(w, "tile", id, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(b)
}

// composeTile draws tile (z, x, y) of class c from the tile set in dir:
// the texture alone for Fog (no terrain read), the terrain for Clear (only
// z6 gets here: its terrain is upscaled), terrain blended with fog for
// Edge. PNG at BestSpeed, like the terrain tiles.
func composeTile(dir string, f *fog.Field, c fog.Class, z, x, y int) ([]byte, error) {
	var img *image.NRGBA
	if c == fog.Fog {
		img = fog.FogTile(z, x, y)
	} else {
		t, err := readTerrain(dir, z, x, y)
		if err != nil {
			return nil, err
		}
		if c == fog.Edge {
			fog.Blend(t, f, z, x, y)
		}
		img = t
	}
	var buf bytes.Buffer
	if err := pngEncoder.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pngEncoder reuses its compressor state across tiles.
var pngEncoder = png.Encoder{CompressionLevel: png.BestSpeed, BufferPool: &pngBuffers{}}

type pngBuffers struct{ p sync.Pool }

func (b *pngBuffers) Get() *png.EncoderBuffer {
	eb, _ := b.p.Get().(*png.EncoderBuffer)
	return eb
}

func (b *pngBuffers) Put(eb *png.EncoderBuffer) { b.p.Put(eb) }

// readTerrain decodes terrain tile (z, x, y). z6 has no file: it is its z5
// parent's quadrant upscaled 2×.
func readTerrain(dir string, z, x, y int) (*image.NRGBA, error) {
	if z > tiles.MaxZoom {
		parent, err := readTerrain(dir, tiles.MaxZoom, x>>1, y>>1)
		if err != nil {
			return nil, err
		}
		return fog.Upscale(parent, x&1, y&1), nil
	}
	f, err := os.Open(terrainPath(dir, z, x, y))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	return toNRGBA(img)
}

// toNRGBA converts a decoded terrain tile to a TileSize² NRGBA at the
// origin. An opaque tile decodes as *image.RGBA, which at alpha 255 has
// the same bytes as NRGBA.
func toNRGBA(src image.Image) (*image.NRGBA, error) {
	b := src.Bounds()
	if b.Dx() != fog.TileSize || b.Dy() != fog.TileSize {
		return nil, fmt.Errorf("server: terrain tile is %dx%d", b.Dx(), b.Dy())
	}
	if n, ok := src.(*image.NRGBA); ok && b.Min == (image.Point{}) {
		return n, nil
	}
	out := image.NewNRGBA(image.Rect(0, 0, fog.TileSize, fog.TileSize))
	if r, ok := src.(*image.RGBA); ok && b.Min == (image.Point{}) && r.Opaque() {
		for y := 0; y < fog.TileSize; y++ {
			copy(out.Pix[y*out.Stride:(y+1)*out.Stride], r.Pix[y*r.Stride:])
		}
		return out, nil
	}
	draw.Draw(out, out.Rect, src, b.Min, draw.Src)
	return out, nil
}

func terrainPath(dir string, z, x, y int) string {
	return filepath.Join(dir, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".png")
}

// parseCoord parses a plain non-negative decimal integer below limit.
func parseCoord(s string, limit int) (int, bool) {
	if s == "" || len(s) > 3 || strings.TrimLeft(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n >= limit {
		return 0, false
	}
	return n, true
}
```

`internal/server/server.go`:
- Package comment: `// and map-data API, and world map tile serving.` becomes `// and map-data API, and fogged world map tile serving.`
- Imports: add `"runtime"` after `"net/http"`.
- Struct fields (gofmt aligns them):

```go
type server struct {
	Deps
	tokens   *tokenCache
	worlds   *worldCache
	fogTiles *tileCache
```

- In `newServer`, after `worlds: …`:

```go
		fogTiles:       newTileCache(fogTileCacheBytes, runtime.GOMAXPROCS(0)),
```

- In `NewHandlers`, the tile route `mux.HandleFunc("GET /tiles/{id}/{key}/{z}/{x}/{y}", s.tile)` becomes:

```go
	mux.HandleFunc("GET /tiles/{id}/{key}/{fog}/{z}/{x}/{y}", s.fogTile)
```

The old five-segment URL now falls through to the `/tiles/` prefix and gets the shared JSON 404.

- [ ] **Step 5: Run the tests**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH gofmt -l internal && PATH=$HOME/.local/go/bin:$PATH go vet ./internal/server && PATH=$HOME/.local/go/bin:$PATH go test ./internal/server -v -run 'TileCache|FogTiles|Locked|Gzip' 2>&1 | grep -E '^(---|ok|FAIL)'`
Expected: PASS for the four `TestTileCache*` tests, `TestFogTiles`, `TestLockedRoutesAre404` and `TestGzipJSONCompressesLargePayloadsOnly`.

Then `PATH=$HOME/.local/go/bin:$PATH go test ./...`. Expected: all `ok`.

- [ ] **Step 6: Benchmark the edge tile**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH go test ./internal/server -run xxx -bench ComposeEdgeTile -benchtime 100x -benchmem`
Expected: about 10 ms/op on a current CPU. The dev box (Xeon E5-2660 v2, 2.2 GHz, 2013) measured 14.1 ms/op, 958 KB/op, 55 allocs/op. In that run PNG encode was about 60 % of the time, decode 25 % and blend 15 %. Note your number for the commit message.

- [ ] **Step 7: Commit**

```bash
cd /workspace/Farsight
git add internal/server/tilecache.go internal/server/tilecache_test.go internal/server/tiles.go \
  internal/server/tiles_bench_test.go internal/server/server.go internal/server/helpers_test.go internal/server/server_test.go
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(server): serve fog tiles at /tiles/{id}/{key}/{fog}/{z}/{x}/{y}.png

Clear tiles are the terrain files; fog tiles are the texture alone; edge
tiles blend decoded terrain with the fog. z6 upscales its z5 parent.
Composed PNGs (BestSpeed) live in a 64 MB LRU; concurrent requests for
one tile share a composition and at most GOMAXPROCS run at once. The raw
terrain route is gone. Edge tile: <N> ms/op on <CPU>.

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
```

(Replace `<N> ms/op on <CPU>` with the Step 6 result before committing.)

---


### Task 7: browser: fog from the tiles, the 12 m mask for search and readout; e2e data with a mask

**Files:**
- Modify: `cmd/farsight-seed/main.go`, `cmd/farsight-seed/main_test.go` (`-explored`)
- Create: `web/src/lib/explored.ts`, `web/src/lib/explored.test.ts`
- Delete: `web/src/lib/fog.ts`, `web/src/lib/fog.test.ts`
- Modify: `web/src/lib/types.ts`, `web/src/lib/api.ts`, `web/src/lib/api.test.ts`, `web/src/lib/markers.ts`, `web/src/lib/search.ts`
- Modify: `web/src/lib/components/AtlasMap.svelte` (rewritten), `DesktopShell.svelte`, `MobileShell.svelte`, `ScaleReadout.svelte`
- Modify (tests): `web/src/lib/testing/markers-fixture.ts`, `web/src/lib/markers.test.ts`, `web/src/lib/search.test.ts`, `web/src/lib/state.svelte.test.ts`, `web/src/lib/leaflet-internals.test.ts`
- Modify (e2e): `web/tests/e2e/global-setup.ts`, `web/tests/e2e/helpers.ts`, `web/tests/e2e/desktop.spec.ts`, `web/tests/e2e/states.spec.ts`

**Interfaces:**
- Consumes:
  - Task 2: `explored.Encode`, `explored.FromZones`, `explored.SourceZones`, `explored.Decode`
  - Task 5: snapshot API `{fogKey, explored: {source, cell, size, bits}}`, with markers, locations and bases already filtered
  - Task 6: the tile route `/tiles/{id}/{key}/{fog}/{z}/{x}/{y}.png`
- Produces:
  - `farsight-seed -explored`: fills a snapshot's missing `explored` from its `exploredZones` (source `zones`) before posting.
  - TS:
    - `export interface Explored { source: 'tables' | 'zones'; cell: number; size: number; bits: string }`
    - `SnapshotView` gains `fogKey: string`, `explored: Explored`, `exploredZones?: …` and `mask?: Uint8Array`
    - `tileUrl(id, key, fog)`
  - `web/src/lib/explored.ts`: `SIZE`, `CELL`, `MASK_BYTES`, `roundHalfEven`, `cellOf(x, z): [px, py]`, `emptyMask()`, `setCell(mask, px, py)`, `isExplored(mask, x, z)`, `decodeExplored(e): Promise<Uint8Array | undefined>`
  - `fixtureMask(hide?: string[]): Uint8Array` (markers-fixture)
  - e2e helpers: `screenPixel(page, x, y)`, `worldToScreen(page, x, z)`, `tilesLoaded(page)`
- Removed: everything in `fog.ts` (`zoneMask`, `maskForZones`, `createFogLayer`, `ZN`, `ZOFF`, …), the `fog` pane, AtlasMap's `fog` prop, `zoomAnimation: false`, `data-zoom`, the skirt, and `MapMarker.zone`.

#### 7a: e2e data gets a mask (`farsight-seed -explored`)

- [ ] **Step 1: Write the failing test**

In `cmd/farsight-seed/main_test.go`, add `"github.com/jumpingmushroom/farsight/internal/explored"` to the imports (before `extract`), and add before `func TestRunNeedsToken`:

```go
func TestRunExploredRasterisesTheFixtureZones(t *testing.T) {
	rec := &received{}
	srv := ingestStub(t, rec)
	defer srv.Close()
	cfg := config{URL: srv.URL, Server: "demo", TokenEnv: "T", Snapshot: fixtureSnapshot, Explored: true}
	getenv := func(string) string { return "demo-token" }
	if err := run(context.Background(), cfg, getenv, time.Now(), io.Discard); err != nil {
		t.Fatal(err)
	}
	if rec.snap.Explored == nil || rec.snap.Explored.Source != explored.SourceZones {
		t.Fatalf("explored = %+v, want a zones mask", rec.snap.Explored)
	}
	m, err := explored.Decode(*rec.snap.Explored)
	if err != nil {
		t.Fatal(err)
	}
	// Every fixture marker is explored and the one outside crypt is not
	// (TestFixtures checks the same on zones).
	for _, mk := range rec.snap.Markers {
		if !m.At(float64(mk.X), float64(mk.Z)) {
			t.Errorf("marker %s not explored", mk.ID)
		}
	}
	for _, l := range rec.snap.Locations {
		if got, want := m.At(float64(l.X), float64(l.Z)), l.ID != "loc-311"; got != want {
			t.Errorf("location %s explored = %v, want %v", l.ID, got, want)
		}
	}

	// Without -explored the snapshot goes as written: no mask.
	rec2 := &received{}
	srv2 := ingestStub(t, rec2)
	defer srv2.Close()
	cfg.URL, cfg.Explored = srv2.URL, false
	if err := run(context.Background(), cfg, getenv, time.Now(), io.Discard); err != nil {
		t.Fatal(err)
	}
	if rec2.snap.Explored != nil {
		t.Fatal("explored sent without -explored")
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH go test ./cmd/farsight-seed/`
Expected: FAIL to compile: `unknown field Explored in struct literal of type config`.

- [ ] **Step 3: Implement the flag**

`cmd/farsight-seed/main.go`:
- In the doc comment, the posting example line becomes `//	FARSIGHT_SEED_TOKEN=... farsight-seed -server demo -shift -explored \`.
- Imports: add `"github.com/jumpingmushroom/farsight/internal/explored"` before `extract`.
- In `type config`, add `Explored  bool` after `Shift     bool`.
- In `parseFlags`, after the `-shift` flag:

```go
	fs.BoolVar(&c.Explored, "explored", false, "give a snapshot without an explored mask one rasterised from its exploredZones, as a current agent sends")
```

- In `run`, just before `cl := ingest.New(c.URL, c.Server, token)`:

```go
	if c.Explored && snap != nil && snap.Explored == nil {
		enc := explored.Encode(explored.FromZones(snap.ExploredZones), explored.SourceZones)
		snap.Explored = &enc
	}
```

- [ ] **Step 4: Run the tests and commit**

Run: `cd /workspace/Farsight && PATH=$HOME/.local/go/bin:$PATH gofmt -l cmd && PATH=$HOME/.local/go/bin:$PATH go test ./cmd/farsight-seed/ -v -run 'Explored|Fixtures|Run' 2>&1 | grep -E '^(---|ok|FAIL)'`
Expected: all PASS.

```bash
cd /workspace/Farsight
git add cmd/farsight-seed/main.go cmd/farsight-seed/main_test.go
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(seed): -explored gives the posted snapshot a mask from its zones

So e2e data takes the new-format path (snapshot.explored), as a current
agent's would.

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
```

#### 7b: the browser

- [ ] **Step 5: Write the failing unit tests**

Create `web/src/lib/explored.test.ts`. It uses the web `CompressionStream`, which Node 24 has, so no Node types are needed:

```ts
import { describe, expect, test } from 'vitest';
import { MASK_BYTES, cellOf, decodeExplored, emptyMask, isExplored, roundHalfEven, setCell } from './explored';

/** base64 of gzip of `bytes`, as the server sends `explored.bits`. */
async function gzipBase64(bytes: Uint8Array<ArrayBuffer>): Promise<string> {
	const stream = new Blob([bytes]).stream().pipeThrough(new CompressionStream('gzip'));
	const gz = new Uint8Array(await new Response(stream).arrayBuffer());
	let s = '';
	for (const b of gz) s += String.fromCharCode(b);
	return btoa(s);
}

describe('cells', () => {
	test('banker’s rounding, as the game and the server', () => {
		expect([0.5, 1.5, 2.5, -0.5, -1.5, -2.5, 2.4, -2.6].map(roundHalfEven)).toEqual([0, 2, 2, 0, -2, -2, 2, -3]);
		expect(cellOf(0, 0)).toEqual([1024, 1024]);
		expect(cellOf(6, -6)).toEqual([1024, 1024]);
		expect(cellOf(18, -18)).toEqual([1026, 1022]);
		expect(cellOf(11.9, -12.1)).toEqual([1025, 1023]);
	});

	test('isExplored reads the bit under a point; off-grid is unexplored', () => {
		const m = emptyMask();
		setCell(m, 1025, 1024);
		expect(m[(1024 * 2048 + 1025) >> 3]).toBe(1 << 1);
		expect(isExplored(m, 12, 0)).toBe(true);
		expect(isExplored(m, 17.9, 5.9)).toBe(true);
		expect(isExplored(m, 0, 0)).toBe(false);
		expect(isExplored(m, 30000, 0)).toBe(false);
		setCell(m, -1, 0); // ignored
		expect(m.every((b, i) => i === (1024 * 2048 + 1025) >> 3 || b === 0)).toBe(true);
	});
});

describe('decodeExplored', () => {
	test('decodes gzip+base64 bits', async () => {
		const m = emptyMask();
		setCell(m, 3, 0);
		setCell(m, 2047, 2047);
		const bits = await gzipBase64(m);
		const out = await decodeExplored({ source: 'tables', cell: 12, size: 2048, bits });
		expect(out?.length).toBe(MASK_BYTES);
		expect(out?.[0]).toBe(1 << 3);
		expect(out?.[MASK_BYTES - 1]).toBe(0x80);
	});

	test('rejects other grids, bad base64, bad gzip and short bitsets', async () => {
		const ok = await gzipBase64(emptyMask());
		expect(await decodeExplored(undefined)).toBeUndefined();
		expect(await decodeExplored({ source: 'zones', cell: 64, size: 2048, bits: ok })).toBeUndefined();
		expect(await decodeExplored({ source: 'zones', cell: 12, size: 1024, bits: ok })).toBeUndefined();
		expect(await decodeExplored({ source: 'zones', cell: 12, size: 2048, bits: '!!!' })).toBeUndefined();
		expect(await decodeExplored({ source: 'zones', cell: 12, size: 2048, bits: btoa('plain') })).toBeUndefined();
		const short = await gzipBase64(new Uint8Array(10));
		expect(await decodeExplored({ source: 'zones', cell: 12, size: 2048, bits: short })).toBeUndefined();
	});
});
```

In `web/src/lib/api.test.ts`, replace the `'200 -> the snapshot'` test with these two tests:

```ts
	test('200 -> the snapshot', async () => {
		const snap = {
			savedAt: '2026-09-30T00:00:00Z',
			fogKey: '0123456789abcdef',
			explored: { source: 'zones', cell: 12, size: 2048, bits: '' },
			markers: [],
			locations: [],
			bases: [],
			players: []
		};
		const fake = vi.fn().mockResolvedValue(jsonResponse(snap));
		const result = await getSnapshot('a', fake as unknown as typeof fetch);
		expect(result).toEqual(snap);
	});

	test('200 -> the explored mask decoded onto the snapshot', async () => {
		const bits = new Uint8Array(512 * 1024);
		bits[1] = 4;
		const gz = new Uint8Array(await new Response(new Blob([bits]).stream().pipeThrough(new CompressionStream('gzip'))).arrayBuffer());
		let b64 = '';
		for (const b of gz) b64 += String.fromCharCode(b);
		const snap = {
			savedAt: '2026-09-30T00:00:00Z',
			fogKey: '0123456789abcdef',
			explored: { source: 'tables', cell: 12, size: 2048, bits: btoa(b64) },
			markers: [],
			locations: [],
			bases: [],
			players: []
		};
		const fake = vi.fn().mockResolvedValue(jsonResponse(snap));
		const result = await getSnapshot('a', fake as unknown as typeof fetch);
		expect(result?.fogKey).toBe('0123456789abcdef');
		expect(result?.mask?.length).toBe(512 * 1024);
		expect(result?.mask?.[1]).toBe(4);
	});
```

and replace the `tileUrl` describe block with:

```ts
describe('tileUrl', () => {
	test('escapes id, key and fog key', () => {
		expect(tileUrl('a b', 'k', 'f/0')).toBe('/tiles/a%20b/k/f%2F0/{z}/{x}/{y}.png');
	});
});
```

- [ ] **Step 6: Run them to see them fail**

Run: `cd /workspace/Farsight/web && npx vitest run src/lib/explored.test.ts src/lib/api.test.ts`
Expected: FAIL: `Failed to resolve import "./explored"`, and the `api.test.ts` cases fail (`result?.mask` is undefined, and the `tileUrl` arity is wrong).

- [ ] **Step 7: Implement the mask module, types and API client**

Create `web/src/lib/explored.ts`:

```ts
// The explored mask (spec 2026-10-01 §1): the game's 2048×2048 minimap
// grid, 12 m per cell. Cell (px, py) = (round(x/12) + 1024, round(z/12) +
// 1024) with banker's rounding (Unity's Mathf.RoundToInt), row-major from
// the south edge, one bit per cell, least significant bit first. The
// server sends it gzip'd and base64'd; the fog itself is drawn into the
// tiles server-side, so the browser only uses the mask for search, marker
// filtering and the cursor readout.

import type { Explored } from './types';

export const SIZE = 2048;
export const CELL = 12;
const HALF = SIZE / 2;
export const MASK_BYTES = (SIZE * SIZE) / 8;

/** Round half to even, as Unity's Mathf.RoundToInt (and Go's math.RoundToEven). */
export function roundHalfEven(v: number): number {
	const f = Math.floor(v);
	const d = v - f;
	if (d > 0.5) return f + 1;
	if (d < 0.5) return f;
	return f % 2 === 0 ? f : f + 1;
}

/** The cell under world point (x, z); it may lie off the grid. */
export function cellOf(x: number, z: number): [number, number] {
	return [roundHalfEven(x / CELL) + HALF, roundHalfEven(z / CELL) + HALF];
}

/** An empty mask (all unexplored). */
export function emptyMask(): Uint8Array<ArrayBuffer> {
	return new Uint8Array(MASK_BYTES);
}

/** Marks cell (px, py) explored; off-grid cells are ignored. */
export function setCell(mask: Uint8Array, px: number, py: number): void {
	if (px < 0 || px >= SIZE || py < 0 || py >= SIZE) return;
	const i = py * SIZE + px;
	mask[i >> 3] |= 1 << (i & 7);
}

/** Whether the cell under world point (x, z) is explored; off-grid is not. */
export function isExplored(mask: Uint8Array, x: number, z: number): boolean {
	const [px, py] = cellOf(x, z);
	if (px < 0 || px >= SIZE || py < 0 || py >= SIZE) return false;
	const i = py * SIZE + px;
	return (mask[i >> 3] & (1 << (i & 7))) !== 0;
}

/**
 * Decodes the snapshot API's `explored` (base64 of gzip of the bitset).
 * Undefined when it is missing, malformed, or the browser lacks
 * DecompressionStream: the server has already filtered the pins, so the
 * mask only refines search and the cursor readout.
 */
export async function decodeExplored(e: Explored | undefined): Promise<Uint8Array | undefined> {
	if (!e || e.cell !== CELL || e.size !== SIZE || !e.bits || typeof DecompressionStream !== 'function') return undefined;
	try {
		const bin = atob(e.bits);
		const gz = Uint8Array.from(bin, (c) => c.charCodeAt(0));
		const stream = new Blob([gz]).stream().pipeThrough(new DecompressionStream('gzip'));
		const out = new Uint8Array(await new Response(stream).arrayBuffer());
		return out.length === MASK_BYTES ? out : undefined;
	} catch {
		return undefined;
	}
}
```

`web/src/lib/types.ts`: replace `export interface SnapshotView {` and its `savedAt` / `exploredZones` lines with:

```ts
/** The explored mask as the API sends it (see explored.ts). */
export interface Explored {
	source: 'tables' | 'zones';
	cell: number;
	size: number;
	/** base64 of gzip of the bitset. */
	bits: string;
}

export interface SnapshotView {
	savedAt: string;
	/** Names the fog tiles drawn from this snapshot's mask. */
	fogKey: string;
	explored: Explored;
	/** Kept by the server until every client has moved to `explored`; unused. */
	exploredZones?: [number, number][];
	/** `explored`, decoded by getSnapshot (client-side only; absent if it can't be). */
	mask?: Uint8Array;
	markers: Marker[];
```

(The remaining fields, `locations`, `bases` and `players`, are unchanged.)

`web/src/lib/api.ts`:
- Add `import { decodeExplored } from './explored';` above the `types` import.
- In `getSnapshot`, replace `return (await res.json()) as SnapshotView;` with:

```ts
		const snap = (await res.json()) as SnapshotView;
		const mask = await decodeExplored(snap.explored);
		return mask ? { ...snap, mask } : snap;
```

- Replace `tileUrl` with:

```ts
/** The fog tiles of tile set `key` under fog key `fog` (spec 2026-10-01 §2). */
export function tileUrl(id: string, key: string, fog: string): string {
	return `/tiles/${encodeURIComponent(id)}/${encodeURIComponent(key)}/${encodeURIComponent(fog)}/{z}/{x}/{y}.png`;
}
```

- [ ] **Step 8: Move markers, search and the readout to the 12 m mask; drop the fog canvas**

`web/src/lib/markers.ts`:
- Imports: replace `import { dir8, distance, zoneOf } from './geo';` with the two lines `import { isExplored } from './explored';` and `import { dir8, distance } from './geo';`. Delete `import { ZN, ZOFF } from './fog';`.
- Delete the `zone: [number, number];` field from `interface MapMarker`, and the line `zone: zoneOf(d.x, d.z),` from `finish`.
- Delete `function explored(m, mask)`, and make `fogVisible`:

```ts
function fogVisible(m: MapMarker, mask: Uint8Array | undefined, fog: boolean): boolean {
	return !fog || !mask || isExplored(mask, m.x, m.z);
}
```

`web/src/lib/search.ts`: `import { isExplored } from './fog';` becomes `import { isExplored } from './explored';`.

`web/src/lib/components/ScaleReadout.svelte`:
- `import { isExplored } from '$lib/fog';` becomes `import { isExplored } from '$lib/explored';`.
- In the header comment, `"Unexplored" when the fog is on and the zone isn't explored` becomes `"Unexplored" when the fog is on and the 12 m cell isn't explored`.

`web/src/lib/components/DesktopShell.svelte` and `web/src/lib/components/MobileShell.svelte`, in both:
- Delete `import { maskForZones } from '$lib/fog';`.
- `const mask = $derived(app.snapshot ? maskForZones(app.snapshot.exploredZones) : undefined);` becomes `const mask = $derived(app.snapshot?.mask);`.
- Delete the `fog={fog && view?.overlay.kind !== 'charting'}` attribute on `<AtlasMap>`. The `fog` constant stays; the marker layer, search and readout use it.

Replace `web/src/lib/components/AtlasMap.svelte` entirely:

```svelte
<!--
  The world map (DESIGN-NOTES §1.1 item 1, §5.5–§5.8, §7.3; fog per spec
  2026-10-01): Leaflet with the world CRS and the server's fog tiles (terrain
  with the fog of war drawn in) over a parchment-filled world disc, so a tile
  still loading looks fogged, never bare. The map is created once; the tile
  layer is replaced when the server, tile key or fog key changes. Leaflet's
  own zoom animation is on: the fog is in the tiles, so it can't lag them.

  Panes: disc 150 (below tiles 200), portalLines 380, markers use Leaflet's
  markerPane (600). The popover is DOM, not a pane.
-->
<script lang="ts">
	import L from 'leaflet';
	import { onMount, untrack } from 'svelte';
	import { tileUrl } from '$lib/api';
	import { CRS, MAX_BOUNDS, WORLD_BOUNDS, WORLD_RADIUS, toLatLng } from '$lib/geo';
	import type { Card, SnapshotView } from '$lib/types';

	let {
		card,
		snapshot,
		padLeft = 0,
		defaultZoom = 1.75,
		dim = 1,
		filter = '',
		onready,
		onclick,
		onmove
	}: {
		/** Undefined while a server switch loads: the map stays mounted, without tiles. */
		card: Card | undefined;
		/** Tiles are drawn only while this is a loaded snapshot (it names the fog tiles). */
		snapshot: SnapshotView | null | undefined;
		padLeft: number;
		/** The default view's zoom (§7.3 ruling: 1.75 desktop, 1.5 mobile). */
		defaultZoom?: number;
		dim: number;
		filter: string;
		onready?: (map: L.Map) => void;
		onclick?: () => void;
		onmove?: () => void;
	} = $props();

	let el: HTMLDivElement;
	let map = $state.raw<L.Map | undefined>(undefined);

	// The tile URL carries the snapshot's fog key: no tiles until a snapshot
	// is loaded — not while it loads (undefined), not before the first save
	// (null), and not when its fetch fails. The world disc stays.
	const tileSrc = $derived(
		snapshot?.fogKey && card && card.tiles.state === 'complete' && card.tiles.key
			? tileUrl(card.id, card.tiles.key, snapshot.fogKey)
			: undefined
	);

	/** Container point at the centre of the map area right of the panel. */
	function visibleCentre(m: L.Map): L.Point {
		const size = m.getSize();
		return L.point(size.x / 2 + padLeft / 2, size.y / 2);
	}

	/**
	 * Puts world (x, z) at the visible centre at `zoom`, or `dy` px below it
	 * (negative = above; the mobile shell puts a selected marker at y ≈ 300).
	 */
	export function centerOn(x: number, z: number, zoom: number, dy = 0): void {
		if (!map) return;
		const p = map.project(toLatLng(x, z), zoom).subtract([padLeft / 2, dy]);
		map.setView(map.unproject(p, zoom), zoom);
	}

	/** The default view: the world centred in the visible area at `defaultZoom`. */
	export function resetView(): void {
		centerOn(0, 0, defaultZoom);
	}

	/** Zooms by `delta` about the visible centre. */
	export function zoomBy(delta: number): void {
		if (!map) return;
		map.setZoomAround(visibleCentre(map), map.getZoom() + delta);
	}

	/**
	 * The side panel floats over the left `padLeft` px, so maxBounds limits
	 * the *visible* area right of it: Leaflet's bounds offset (used by
	 * setView, zoom and panInsideBounds) sees the view without that strip.
	 * When the world is smaller than the visible area this centres it at the
	 * visible centre (§1.1: x = 900 with the panel open) instead of under the
	 * panel. `_getBoundsOffset` is internal to Leaflet 1.9.4 (pinned).
	 *
	 * `padB` does the same for a strip at the bottom (the mobile docked
	 * marker card, see `setPadBottom`); it is 0 on desktop, so the desktop
	 * limit is unchanged.
	 */
	let pad = untrack(() => padLeft);
	let padB = 0;
	function padBoundsLimit(m: L.Map): void {
		type Limiter = { _getBoundsOffset(px: L.Bounds, b: L.LatLngBounds, zoom?: number): L.Point };
		const lm = m as unknown as Limiter;
		const orig = lm._getBoundsOffset;
		lm._getBoundsOffset = function (this: L.Map, px, b, zoom) {
			return orig.call(this, L.bounds(px.min!.add([pad, 0]), px.max!.subtract([0, padB])), b, zoom);
		};
	}
	/**
	 * Treats the bottom `px` of the map as covered (mobile: the docked card
	 * area), so maxBounds lets the view pan far enough to put a marker at the
	 * centre of what's left (y ≈ 300). Synchronous, so a following `centerOn`
	 * is limited by it. Back to 0 re-applies the normal limit.
	 */
	export function setPadBottom(px: number): void {
		const next = Math.max(0, Math.round(px));
		if (next === padB) return;
		padB = next;
		if (next === 0) map?.panInsideBounds(MAX_BOUNDS);
	}

	$effect(() => {
		const p = padLeft;
		if (p === pad) return;
		pad = p;
		untrack(() => map?.panInsideBounds(MAX_BOUNDS));
	});

	onMount(() => {
		const m = L.map(el, {
			crs: CRS,
			zoomSnap: 0.25,
			zoomDelta: 0.75,
			wheelPxPerZoomLevel: 90,
			minZoom: 1,
			maxZoom: 6,
			maxBounds: MAX_BOUNDS,
			maxBoundsViscosity: 0.8,
			attributionControl: false,
			zoomControl: false
		});
		padBoundsLimit(m);
		m.createPane('disc').style.zIndex = '150';
		m.createPane('portalLines').style.zIndex = '380';

		L.circle([0, 0], {
			radius: WORLD_RADIUS,
			pane: 'disc',
			className: 'world-disc',
			interactive: false
		}).addTo(m);

		// The default view: the world centre at the visible centre.
		const z0 = untrack(() => defaultZoom);
		const start = m.project(toLatLng(0, 0), z0).subtract([pad / 2, 0]);
		m.setView(m.unproject(start, z0), z0, { animate: false });

		m.on('click', () => onclick?.());
		m.on('move zoom', () => onmove?.());
		map = m;
		onready?.(m);
		return () => {
			map = undefined;
			m.remove();
		};
	});

	// Tile layer: replaced when the server, tile key or fog key changes.
	// Fog tiles exist natively up to zoom 6.
	$effect(() => {
		const m = map;
		const src = tileSrc;
		if (!m || !src) return;
		const layer = L.tileLayer(src, {
			tileSize: 256,
			minZoom: 0,
			maxNativeZoom: 6,
			maxZoom: 6,
			noWrap: true,
			bounds: WORLD_BOUNDS,
			keepBuffer: 2
		}).addTo(m);
		return () => {
			layer.remove();
		};
	});

	// Tile-pane filter (layers off, offline, stale; §3.22).
	$effect(() => {
		const pane = map?.getPane('tilePane');
		if (pane) pane.style.filter = filter;
	});
</script>

<div
	class="atlas-map"
	bind:this={el}
	style:filter={dim < 1 ? `brightness(${dim})` : undefined}
	data-testid="atlas-map"
></div>

<style>
	.atlas-map {
		position: absolute;
		inset: 0;
		isolation: isolate;
		touch-action: none;
		user-select: none;
		transition: filter 0.4s;
	}
	.atlas-map :global(.leaflet-tile-pane) {
		transition: filter 0.4s;
	}
	/* Parchment (#cfbe9c, the fog colour): tiles that are still loading look
	   fogged rather than empty. */
	.atlas-map :global(.world-disc) {
		fill: #cfbe9c;
		fill-opacity: 1;
		stroke: color-mix(in srgb, var(--color-text) 12%, transparent);
		stroke-opacity: 1;
		stroke-width: 5px;
		filter: drop-shadow(0 30px 80px rgba(0, 0, 0, 0.35));
	}
</style>
```

(The section between `zoomBy` and `onMount` is unchanged from the current file: `pad`, `padB`, `padBoundsLimit`, `setPadBottom` and the `padLeft` effect.)

Delete the fog canvas:

```bash
cd /workspace/Farsight
git rm web/src/lib/fog.ts web/src/lib/fog.test.ts
```

Replace `web/src/lib/leaflet-internals.test.ts` (the fog-layer block goes):

```ts
import './testing/leaflet-node';
import L from 'leaflet';
import { describe, expect, test } from 'vitest';

// AtlasMap overrides Leaflet's internal Map#_getBoundsOffset so maxBounds
// limit the area right of the side panel. Fail loudly if an upgrade drops it.
describe('Leaflet internals used by AtlasMap', () => {
	test('Map.prototype._getBoundsOffset exists', () => {
		expect(typeof (L.Map.prototype as unknown as Record<string, unknown>)._getBoundsOffset).toBe('function');
	});
});
```

- [ ] **Step 9: Update the unit-test fixtures to the 12 m mask**

`web/src/lib/testing/markers-fixture.ts`: replace the header comment and imports, and `fixtureSnapshot`'s first lines, so the file starts:

```ts
// A hand-written snapshot with one of each marker kind (invented names), for
// markers/search tests. Every marker's 12 m cell is explored except the
// tombstone's and Haldor's, so fog filtering can be tested.

import { cellOf, emptyMask, setCell } from '../explored';
import type { Marker, SnapshotView, WorldCard } from '../types';
```

Add `fixtureMask` right after `const FOGGED = new Set(['tombstone-1', 'loc-3']);`:

```ts
/**
 * The fixture's explored mask: the cell under every marker and location
 * except the fogged ones and those in `hide`, plus the cell at (0, 0)
 * (base-1).
 */
export function fixtureMask(hide: string[] = []): Uint8Array {
	const m = emptyMask();
	for (const p of [...MARKERS, ...LOCATIONS]) {
		if (!FOGGED.has(p.id) && !hide.includes(p.id)) setCell(m, ...cellOf(p.x, p.z));
	}
	setCell(m, ...cellOf(0, 0));
	return m;
}
```

Replace the start of `fixtureSnapshot`, up to and including its `markers:` line, with the following. The `locations`, `bases` and `players` entries after it are unchanged:

```ts
export function fixtureSnapshot(): SnapshotView {
	return {
		savedAt: '2026-09-30T10:00:00Z',
		fogKey: '0123456789abcdef',
		explored: { source: 'tables', cell: 12, size: 2048, bits: '' },
		mask: fixtureMask(),
		markers: MARKERS.map((m) => ({ ...m })),
```

(Its old `const all`, `const zones` and `zones.push` lines and the `exploredZones: zones,` entry are deleted.)

`web/src/lib/markers.test.ts`:
- Delete the imports of `./fog` and `./geo`.
- The fixture import becomes `import { fixtureMask, fixtureSnapshot, fixtureWorld } from './testing/markers-fixture';`.
- In `'positions, zones and the where line (U+2212 minus)'`, rename the test to `'positions and the where line (U+2212 minus)'` and delete its `expect(m.zone)…` line.
- In `describe('visibleMarkers')`, replace `const snap = fixtureSnapshot();` and `const mask = zoneMask(snap.exploredZones);` with `const mask = fixtureMask();`, and rename the first test to `'fog on hides fogged-out cells; fog off shows them'`.
- In `layerCounts`, delete `const snap = fixtureSnapshot();`, and replace both `maskForZones(snap.exploredZones)` with `fixtureMask()`.
- In `'no pair when an end is hidden'`, replace the four lines from `const snap = fixtureSnapshot();` through `const vis = …zoneMask(zones)…` with:

```ts
		const vis = visibleMarkers(all, ALL_ON, fixtureMask(['portal-2']), true, 4);
```

`web/src/lib/search.test.ts`:
- Delete `import { maskForZones } from './fog';`.
- The fixture import becomes `import { fixtureMask, fixtureSnapshot, fixtureWorld } from './testing/markers-fixture';`.
- `const mask = maskForZones(snap.exploredZones);` becomes `const mask = fixtureMask();`.
- `mini` becomes:

```ts
function mini(markers: SnapshotView['markers'], extra: Partial<SnapshotView> = {}): SnapshotView {
	return {
		savedAt: '',
		fogKey: '0123456789abcdef',
		explored: { source: 'tables', cell: 12, size: 2048, bits: '' },
		markers,
		locations: [],
		bases: [],
		players: [],
		...extra
	};
}
```

`web/src/lib/state.svelte.test.ts`: in `const SNAPSHOT: SnapshotView`, replace `exploredZones: [],` with:

```ts
	fogKey: '0123456789abcdef',
	explored: { source: 'tables', cell: 12, size: 2048, bits: '' },
```

- [ ] **Step 10: Run the web checks**

Run: `cd /workspace/Farsight/web && npm run check && npx vitest run`
Expected: `svelte-check` reports `0 ERRORS 0 WARNINGS`. Vitest passes all 15 files (`fog.test.ts` is gone and `explored.test.ts` is new). The test count is lower because the fog tests were deleted.

Also check that nothing in `src` references the old fog module or zone masks:

Run: `cd /workspace/Farsight/web && grep -rn "lib/fog\|'./fog'\|maskForZones\|zoneMask\|fs-fog\|zoomAnimation" src`
Expected: no output.

- [ ] **Step 11: Update the e2e suite**

`web/tests/e2e/global-setup.ts`:
- The posting `run(seed, …)` call becomes:

```ts
		run(seed, ['-url', base, '-server', 'demo', '-shift', '-explored', '-snapshot', snapshot, '-events', events]);
```

- In the header comment, item 3's last sentence becomes:

```
 *     is awaited, then demo's snapshot and events are posted with -shift (the
 *     newest event lands at now − 1 min) and -explored (the snapshot carries
 *     a 12 m explored mask rasterised from its zones, as a current agent's).
```

`web/tests/e2e/helpers.ts`: append

```ts
/** The RGB of the page's pixel at (x, y), read from a screenshot. */
export async function screenPixel(page: Page, x: number, y: number): Promise<[number, number, number]> {
	const png = await page.screenshot({ clip: { x: Math.round(x), y: Math.round(y), width: 1, height: 1 } });
	return page.evaluate(async (bytes) => {
		const bmp = await createImageBitmap(new Blob([new Uint8Array(bytes)], { type: 'image/png' }));
		const ctx = new OffscreenCanvas(1, 1).getContext('2d')!;
		ctx.drawImage(bmp, 0, 0);
		const d = ctx.getImageData(0, 0, 1, 1).data;
		return [d[0], d[1], d[2]] as [number, number, number];
	}, Array.from(png as unknown as Uint8Array));
}

/** Screen position of world point (x, z), from the world disc's on-screen box (radius 10 500 m). */
export async function worldToScreen(page: Page, x: number, z: number): Promise<{ x: number; y: number }> {
	const box = (await page.locator('path.world-disc').boundingBox())!;
	const r = box.width / 2;
	return { x: box.x + r + (x / 10500) * r, y: box.y + box.height / 2 - (z / 10500) * r };
}

/** Waits until every tile image on the map has loaded. */
export async function tilesLoaded(page: Page): Promise<void> {
	await expect
		.poll(() =>
			page
				.locator('img.leaflet-tile')
				.evaluateAll((imgs) => imgs.length > 0 && imgs.every((i) => (i as HTMLImageElement).complete && (i as HTMLImageElement).naturalWidth === 256))
		)
		.toBe(true);
}
```

`screenPixel` passes the screenshot to the page as a number array, so no Node `Buffer` typing is needed (the repo has no `@types/node`). It decodes the PNG with `createImageBitmap` on a `Blob`, which is no fetch, so the CSP and the request guard are unaffected.

`web/tests/e2e/desktop.spec.ts`:
- The helpers import becomes `import { checkGuard, expect, pinsOfKind, screenPixel, test, tilesLoaded, unlock, watchGuard, worldToScreen } from './helpers';`.
- Replace everything from `test('3 · tiles load from the seeded tile set'` up to (not including) `test('4 · online tab`. That range is test 3 and the two obsolete fog-canvas tests, `'fix · a fast wheel zoom-out never bares terrain past the shrinking fog canvas'` and `'fix · fog and terrain stay at the same zoom in every frame of a fast wheel zoom'`. Replace it with:

```ts
test('3 · fog tiles load with the snapshot’s fog key; no fog canvas; zoom animation on', async ({ page }) => {
	const tileUrls: string[] = [];
	page.on('request', (r) => {
		if (r.url().includes('/tiles/')) tileUrls.push(new URL(r.url()).pathname);
	});
	const snapshot = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/servers/demo/snapshot');
	await unlock(page);
	const { fogKey } = (await (await snapshot).json()) as { fogKey: string };
	expect(fogKey).toMatch(/^[0-9a-f]{16}$/);
	await tilesLoaded(page);
	expect(tileUrls.length).toBeGreaterThan(0);
	for (const u of tileUrls) expect(u).toMatch(new RegExp(`^/tiles/demo/12345-2-r\\d+/${fogKey}/\\d+/\\d+/\\d+\\.png$`));
	await expect(page.locator('canvas.fs-fog')).toHaveCount(0);
	await expect(page.locator('.fs-fog-skirt')).toHaveCount(0);
	// Leaflet adds its zoom-animation proxy only when zoomAnimation is on;
	// a zoom then animates the map pane.
	await expect(page.locator('.leaflet-proxy')).toHaveCount(1);
	const animating = page.waitForSelector('.leaflet-map-pane.leaflet-zoom-anim', { state: 'attached' });
	await page.getByRole('button', { name: 'Zoom in' }).click();
	await animating;
});

test('fog · terrain deep in unexplored land is fog-coloured on screen, explored land is not', async ({ page }) => {
	await unlock(page);
	await tilesLoaded(page);
	// x = −8 000 m is 6 km west of every explored zone (they span −1 920…4 600 m).
	const fogged = await worldToScreen(page, -8000, 0);
	const [r, g, b] = await screenPixel(page, fogged.x, fogged.y);
	// #cfbe9c ± the grain (±9) and hatching; the fake terrain there is meadow or forest green.
	expect(Math.abs(r - 0xcf), `r ${r}`).toBeLessThanOrEqual(20);
	expect(Math.abs(g - 0xbe), `g ${g}`).toBeLessThanOrEqual(20);
	expect(Math.abs(b - 0x9c), `b ${b}`).toBeLessThanOrEqual(20);
	// (−500, 300) is three zones inside the explored area, away from pins:
	// the flat fake terrain shows there (meadow green, 163,178,92).
	const clear = await worldToScreen(page, -500, 300);
	const [cr, cg, cb] = await screenPixel(page, clear.x, clear.y);
	expect(Math.abs(cr - 0xcf) + Math.abs(cg - 0xbe) + Math.abs(cb - 0x9c), `rgb ${cr},${cg},${cb}`).toBeGreaterThan(60);
});
```

- Replace the start of test 11, up to and including its `expect(crypts…).toEqual(['loc-140']);` line, with:

```ts
test('11 · the crypt outside the explored cells never appears', async ({ page }) => {
	const snapshot = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/servers/demo/snapshot');
	await unlock(page);
	// The server filters markers, locations and bases by the 12 m explored
	// mask: only the inside crypt ships, every marker and base is inside.
	const body = await (await snapshot).json();
	expect(body.explored).toMatchObject({ source: 'zones', cell: 12, size: 2048 });
	const crypts = (body.locations as { id: string; type: string }[]).filter((l) => l.type === 'SunkenCrypt4');
	expect(crypts.map((l) => l.id)).toEqual(['loc-140']);
	expect((body.locations as { id: string }[]).map((l) => l.id)).not.toContain('loc-311');
	expect(body.markers).toHaveLength(13);
	expect(body.bases).toHaveLength(2);
```

The sample points were checked against the fixture:
- (−8 000, 0) is 6 km west of every explored zone; the zones span x −1 920…4 600 m.
- (−500, 300) has every zone within three zones explored and the nearest pin 608 m away.
- (1 800, 0) is **not** used: it falls on the seam between two fake-tile quadrants at z = 0.

`web/tests/e2e/states.spec.ts`:
- Rename `'fog guard: no terrain tiles while the snapshot is loading, then tiles under fog'` to `'fog guard: no tiles while the snapshot is loading, then fog tiles'`, and replace its comment with:

```ts
// Fog is the core promise: the tiles carry it, and no tile is requested
// before the snapshot names its fog key.
```

- In it, delete both `canvas.fs-fog` assertions. After the `img.leaflet-tile-loaded` check, add:

```ts
	for (const u of tileRequests) expect(new URL(u).pathname).toMatch(/^\/tiles\/demo\/[^/]+\/[0-9a-f]{16}\//);
```

- Rename `'fog guard: a failing snapshot fetch never shows terrain tiles'` to `'fog guard: a failing snapshot fetch never requests tiles'`, and delete its `canvas.fs-fog` assertion.

- [ ] **Step 12: Run the e2e suite**

Run: `cd /workspace/Farsight/web && npm run check && PATH=$HOME/.local/go/bin:$PATH LD_LIBRARY_PATH="$(../hack/playwright-libs.sh --print)" FONTCONFIG_FILE="$(../hack/playwright-libs.sh --fontconfig)" npx playwright test`
Then: `grep -rn "fs-fog\|data-zoom\|zoomAnimation" tests/e2e`. Expected: only three lines of test 3 in `desktop.spec.ts`: the two `toHaveCount(0)` assertions, and the comment above the `.leaflet-proxy` check.

Expected from the Playwright run: `38 passed`. This includes `3 · fog tiles load with the snapshot’s fog key; no fog canvas; zoom animation on` and `fog · terrain deep in unexplored land is fog-coloured on screen, explored land is not`. While this plan was written, the fogged pixel read about #cfbe9c ± grain and the explored one read the fake meadow green (162,177,91).

- [ ] **Step 13: Commit**

```bash
cd /workspace/Farsight
git add web/src/lib/explored.ts web/src/lib/explored.test.ts web/src/lib/types.ts web/src/lib/api.ts web/src/lib/api.test.ts \
  web/src/lib/markers.ts web/src/lib/search.ts web/src/lib/components/AtlasMap.svelte \
  web/src/lib/components/DesktopShell.svelte web/src/lib/components/MobileShell.svelte web/src/lib/components/ScaleReadout.svelte \
  web/src/lib/testing/markers-fixture.ts web/src/lib/markers.test.ts web/src/lib/search.test.ts \
  web/src/lib/state.svelte.test.ts web/src/lib/leaflet-internals.test.ts \
  web/tests/e2e/global-setup.ts web/tests/e2e/helpers.ts web/tests/e2e/desktop.spec.ts web/tests/e2e/states.spec.ts
git status --short   # fog.ts and fog.test.ts show as D (staged by git rm); nothing unstaged
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
feat(web): fog comes from the tiles; Leaflet's zoom animation is back

The fog canvas, its skirt and the zoom-sync workarounds are gone. Tile
URLs carry the snapshot's fog key, native up to z6, over a parchment
world disc. Search, pin filtering and the cursor readout use the 12 m
explored mask decoded from the snapshot.

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
```

---


### Task 8: docs: README, the specs, and the cloudcluster runbook note

No code. Every edit is exact text.

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md` (the fog, tile-route, snapshot and API-contract lines)
- Modify: `docs/superpowers/specs/2026-10-01-fog-tiles-design.md` (append "Implementation notes")
- Modify (cloudcluster repo, local branch `docs/farsight-fog-tiles`): `/workspace/cloudcluster/docs/runbooks/farsight.md`

**Interfaces:**
- Consumes: the names and numbers fixed in Tasks 1–7 (`ZoneShrinkCells = 20`, the ±63.5 m clamp, the route, `fogKey` and `explored`, the 64 MB cache).
- Produces: documentation only.

- [ ] **Step 1: README**

In `README.md`, replace the sentence `Fog covers the zones nobody has explored yet.` (end of the "World atlas" bullet) with:

```
The fog of war follows the game's own map: what has been recorded at a cartography table, plus 100 m around anything built. It is drawn into the map tiles on the server, so unexplored terrain never reaches the browser.
```

- [ ] **Step 2: The MVP spec**

In `docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md`:

1. Replace

```
  tiles" state. Terrain tiles are never fogged. **Fog is a client-side
  mask** built from `exploredZones`, so a new save never re-renders terrain.
  The browser draws it as one viewport-sized canvas layer (a tiled layer
  leaves 1 px seams at fractional zoom).
  Tiles are served per server and key at `/tiles/{id}/{key}/{z}/{x}/{y}.png`,
  where `key` is the tile set's directory name
  (`{seed}-{genVersion}-r{renderVersion}`), only while it is that server's
  current complete set, and with immutable caching.
```

with

```
  tiles" state. The rendered terrain tiles on disk are never fogged. Since
  `2026-10-01-fog-tiles-design.md` the **fog is drawn into the served
  tiles** from the 12 m explored mask (it was a client-side canvas before).
  Tiles are served per server, key and fog key at
  `/tiles/{id}/{key}/{fog}/{z}/{x}/{y}.png` (z0–z6), where `key` is the tile
  set's directory name (`{seed}-{genVersion}-r{renderVersion}`), only while
  it is that server's current complete set and `fog` its current fog key,
  with immutable caching.
```

2. In the snapshot block, after the line `  exploredZones: [[x,z]…],                          // 64 m zones`, add:

```
  explored?: { source, cell, size, bits },          // 12 m mask: "tables"|"zones", 12, 2048, base64(gzip(bitset)) (fog spec)
```

3. In the API contract, replace the snapshot and tile lines

```
GET  /api/servers/{id}/snapshot           -> {savedAt, exploredZones, markers, locations, bases, players}
                                             (locations filtered to explored zones; 404 if no snapshot)
GET  /tiles/{id}/{key}/{z}/{x}/{y}.png    -> PNG (only when key is the server's current complete set;
                                             Cache-Control: public, max-age=31536000, immutable)
```

with

```
GET  /api/servers/{id}/snapshot           -> {savedAt, fogKey, explored, exploredZones, markers, locations, bases, players}
                                             (markers, locations and bases filtered to the explored mask; 404 if no snapshot)
GET  /tiles/{id}/{key}/{fog}/{z}/{x}/{y}.png -> PNG, z0–z6 (only when key is the server's current complete set
                                             and fog its current fog key; Cache-Control: public, max-age=31536000, immutable)
```

4. Replace the `exploredPct` definition line with:

```
`exploredPct` = explored 12 m cells ÷ cells whose centre lies within 10 500 m (a constant computed once), × 100, rounded to 1 decimal place.
```

- [ ] **Step 3: The fog spec's implementation notes**

Append to `docs/superpowers/specs/2026-10-01-fog-tiles-design.md`:

```markdown

## Implementation notes (Plan 6)

Decisions the plan made where this spec left room:

- **Zone shrink:** 20 cells, as a square erosion. Against MuleVikings' real
  table map it scores IoU 0.719, up from 0.435 unshrunk. Mulennials also
  peaks at 20 (0.647). A Euclidean erosion peaked lower (0.708). The golden
  test `TestGoldenZoneShrinkCalibration` pins the constant.
- **Old snapshots:** the central app rasterises `exploredZones` without the
  shrink. It has no piece positions to add the 100 m reveal back.
- **Field:** the explored edge runs halfway between an explored and an
  unexplored cell centre. The int8 field is clamped to ±127 half-metres
  (±63.5 m), because an int8 can't hold +128. 63.5 m is still past the widest
  band (57.6 m).
- **Tile classes** come from the field's min and max over every cell a
  tile's pixels can sample:
  - *Fog* also needs the tile wholly inside the world disc.
  - Tiles crossing the rim are *edge*, so the terrain's own alpha is used.
  - Tiles wholly outside the disc are *clear*.
- **Fog key:** it hashes the raw bitset, not its gzip form, so it can't
  change with compression.
- **Cache:** the in-memory cache key also includes the tile-set key, so a
  re-rendered terrain set never serves a stale composition.
- **z6 terrain** is the z5 parent quadrant upscaled 2× bilinearly, in
  premultiplied alpha.
- **API:** `fogKey` and `explored` are in the snapshot API only; the card
  keeps `exploredPct`. The browser decodes `explored` with
  `DecompressionStream`. Where that is missing, the mask is absent and only
  the server's pin filtering applies.
- **Performance:** an edge tile measured about 14 ms (decode, blend, encode
  at BestSpeed) on the 2013 dev Xeon. The field and the tile classes take
  about 0.6 s once per fog key.
```

- [ ] **Step 4: Check and commit the Farsight docs**

Run: `cd /workspace/Farsight && git diff --stat`
Expected: only `README.md` and the two spec files are changed.

```bash
cd /workspace/Farsight
git add README.md docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md docs/superpowers/specs/2026-10-01-fog-tiles-design.md
git -c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
docs: fog in the tiles: README, MVP spec amendments, implementation notes

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
```

- [ ] **Step 5: The cloudcluster runbook note (local branch, never pushed)**

```bash
cd /workspace/cloudcluster
git switch main
git status --short            # must print nothing; stop and ask if it doesn't
git switch -c docs/farsight-fog-tiles
```

In `/workspace/cloudcluster/docs/runbooks/farsight.md`, insert this new section immediately before the line `## Ingest is in-cluster only`:

```markdown
## What the map shows

Farsight shows a world as far as it has been **recorded at a cartography
table**, plus 100 m around anything players have built. That is the game's
shared map, not each player's own map, which lives on their PC. Exploration
appears only after someone records it, so tell players to use a cartography
table now and then. A world with no recorded table falls back to the
generated zones, shrunk to roughly what players have seen.

The app draws the fog into the map tiles, so unexplored terrain and pins
never reach the browser. Fog tiles are cached in the app's memory (64 MB) and
by browsers for a year. Their URL changes whenever exploration does.

```

In the same file, in `## Bump the image digests`, insert this paragraph right after the paragraph that ends `boot after a restart can re-download the server through SteamCMD.`:

```markdown

**Fog tiles roll out app first.** Bump the app first: it accepts both
snapshot formats, draws fog from the generated zones until the agents
update, and restarts only the farsight pod. Bump the agent second, at a
quiet time and with the owner's go-ahead, because that restarts all three
Valheim servers. Once both run the new images, a later app release can drop
`exploredZones` from the snapshot API.
```

```bash
cd /workspace/cloudcluster
git diff --stat               # only docs/runbooks/farsight.md
git add docs/runbooks/farsight.md
git -c user.name="Johnny Dalen" -c user.email="johnny@jumpingmushroom.com" commit -F - <<'EOF'
docs(farsight): what the map shows; fog-tiles rollout order

Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T
EOF
```

Do not push. The digest bumps themselves happen after the Farsight branch is merged and its images are built. They follow the order above, and the agent bump needs the owner's go-ahead.

- [ ] **Step 6: Final verification on the Farsight branch**

```bash
cd /workspace/Farsight
PATH=$HOME/.local/go/bin:$PATH gofmt -l ./cmd ./internal ./web    # prints nothing
PATH=$HOME/.local/go/bin:$PATH go vet ./...
PATH=$HOME/.local/go/bin:$PATH go test ./...
cd web && npm run check && npx vitest run
PATH=$HOME/.local/go/bin:$PATH LD_LIBRARY_PATH="$(../hack/playwright-libs.sh --print)" FONTCONFIG_FILE="$(../hack/playwright-libs.sh --fontconfig)" npx playwright test
cd .. && git status --short   # clean; no reference/ or testdata-golden/ ever staged
git log --oneline public-main..HEAD
```

Expected: all green, `38 passed`, a clean tree, and the nine Plan 6 commits on `feat/plan6-fog-tiles` (Tasks 1–6, 7a, 7b, 8), with nothing pushed.

---


## Self-Review

**How this plan was checked.** Tasks 1–7 were carried out end to end on a scratch copy of `public-main` (outside the repo), and every code block above is copied from that copy after `gofmt`.
- `go vet ./...` and `go test ./...` were all `ok`, with the golden tests running against `testdata-golden/`.
- `svelte-check` reported 0 errors and 0 warnings, and Vitest passed 15 files.
- Playwright: 38 passed.

The calibration numbers (k = 20, IoU 0.7188 / 0.6465), the browser texture hash (2230037077) and the benchmark figures are the measured values from that run.

**1. Spec coverage**

| Spec requirement | Task |
|---|---|
| §1 table blob: gzip ZPackage, version 2/3, n = 2048², bools, pins ignored | 1 (`DecodeMapData`) |
| §1 byte arrays kept only for requested prefabs | 1 (`ReadOptions.KeepBytes`), 3 (`extract.KeepBytes`) |
| §1 12 m grid, banker's rounding, row 0 south | 2 (`CellOf`, bit layout tests) |
| §1 union of all tables | 2 (`AddFlags`), 3 (`addTable`) |
| §1 100 m disc around every player-built piece | 2 (`Reveal`, 9 cells as the game), 3 (`exploredMask`) |
| §1 zone fallback shrunk by a calibrated number of cells (IoU on MuleVikings) | 2 (`FromZones`, `Erode`, `ZoneShrinkCells = 20`, golden calibration test) |
| §1 `explored: {source, cell: 12, size: 2048, bits}`, row-major LSB-first, gzip + base64; `exploredZones` kept | 2 (`Encode`/`Decode`), 3 (`Snapshot.Explored`) |
| §2 server accepts both formats; zones rasterised onto the 12 m grid | 5 (`newWorldState`), 7 (e2e uses the new format, server tests the old) |
| §2 fog key = 16 hex of SHA-256 over (source, size, bits, style version) | 4 (`fog.Key`), 5 (served) |
| §2 exact EDT, clamped, int8 at 0.5 m, once per fog key | 4 (`NewField`), 6 (`fogData` once) |
| §2 alpha = 1 − smoothstep(0, w, d), w = min(57.6, 24 px·mpp), opaque at d ≤ 0 | 4 (`Alpha`, `EdgeWidth`, tests) |
| §2 texture port, world-pixel anchored, seamless; disc alpha kept | 4 (`texture.go`, `FogTile`, `Blend` keeps alpha; tests vs browser values) |
| §2 z0–z6; z6 = z5 parent upscaled, fog at z6 resolution | 4 (`Upscale`), 6 (`readTerrain`) |
| §2 route, 404 rules (unlock, current complete key, current fog key) | 6 (`fogTile`, `TestFogTiles`, `TestLockedRoutesAre404`) |
| §2 classify clear/fog/edge; clear = terrain file; fog = no terrain read | 4 (`ClassMap`), 6 (file served byte for byte; fog tile served with no terrain file on disk) |
| §2 BestSpeed, 64 MB LRU keyed by (server, fog key, z, x, y), coalescing, GOMAXPROCS bound | 6 (`tilecache.go`, its tests); the key also carries the tile-set key |
| §2 immutable Cache-Control; raw route removed | 6 |
| §2 snapshot API filters markers, locations and bases by mask cell; players unchanged; returns `fogKey`, `explored` | 5 |
| §2 exploredPct over the 10.5 km disc, one decimal | 2 (`Percent`), 5 (card) |
| §3 remove fog canvas, skirt, `data-zoom`, zoom-sync workarounds; zoom animation on | 7 (fog.ts deleted, AtlasMap rewritten, e2e checks `.leaflet-proxy` and the zoom-anim class) |
| §3 tile URL from `fogKey`; `maxNativeZoom: 6` | 7 |
| §3 world disc filled `#cfbe9c` | 7 (AtlasMap style) |
| §3 `isExplored` and marker filtering on the 12 m mask; charting overlay unchanged | 7 (`explored.ts`, markers, search, ScaleReadout; `mapView`/charting untouched) |
| Testing: Go list | 1, 2, 4, 5, 6 (each item has a named test; benchmark in 6) |
| Testing: web list | 7 (`explored.test.ts`, `api.test.ts`, e2e tests 3, fog pixel and 11) |
| Rollout order; runbook note about recording at tables | Global Constraints, 8 (runbook) |

**2. Placeholder scan.** No "TBD", "TODO" or "similar to Task N" appears. Every code step has complete code or an exact replacement. The one deliberate fill-in is the Task 6 commit message's `<N> ms/op on <CPU>`, which is a measurement only the executor can take; Step 6 says where it comes from.

**3. Type and name consistency (checked across tasks):**

- **save:** `save.ReadWith` / `save.ReadOptions{KeepBytes}` (T1 → T2 golden, T3) · `save.MapTablePrefab`, `save.MapDataKey`, `save.DecodeMapData` (T1 → T2, T3)
- **explored:**
  - `explored.Encoded{Source, Cell, Size, Bits}` with JSON `source/cell/size/bits` (T2 → T3, T5, T7 seed; TS `Explored` in T7)
  - `explored.SourceTables` / `SourceZones` = `"tables"` / `"zones"` (T2 → T3, T5, T7; TS union `'tables' | 'zones'`)
  - `explored.ZoneShrinkCells` (T2 → T3)
  - `(*Mask).At` / `Percent` / `Erode` / `Reveal` / `AddFlags` / `Bits` / `Clone` / `Union` (T2 → T3, T4, T5)
- **extract and fog:**
  - `extract.Snapshot.Explored *explored.Encoded` (T3 → T5, T7 seed)
  - `fog.Key(source, *Mask)` (T4 → T5)
  - `fog.NewField`, `fog.NewClassMap`, `(*ClassMap).At`, `fog.Clear/Fog/Edge`, `fog.FogTile`, `fog.Blend`, `fog.Upscale`, `fog.TexturePixel`, `fog.MaxZoom` (T4 → T5/T6)
- **server and store:**
  - `worldState{saveID, snap, mask, enc, fogKey, pct}` and `fogData()` (T5 → T6)
  - `s.worlds.get(ctx, id)` (T5 → T6)
  - `(*store.Store).LatestSnapshotID` (T5)
  - `tileCacheKey{server, tiles, fog, z, x, y}`, `newTileCache`, `get` (T6)
- **Wire JSON and TS:**
  - Snapshot API JSON `fogKey`, `explored`, `exploredZones` (T5) = TS `SnapshotView.fogKey`, `.explored`, `.exploredZones?` (T7)
  - `tileUrl(id, key, fog)` (T7) builds `/tiles/{id}/{key}/{fog}/{z}/{x}/{y}.png`, which is the route registered in T6
- **Test helpers:** `env.snapshotView` (T5 → T6), `env.waitTiles`, `writeTestTiles`, `testTerrain` (T6), `fixtureMask` (T7), `screenPixel` / `worldToScreen` / `tilesLoaded` (T7)
