# Plan 8: Cursor biome and the 1.0 location set

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show the biome under the cursor, and replace the single noisy Locations layer with Landmarks / Dungeons / Minor places, including Valheim 1.0 sites (Forge of Potential and others).

**Architecture:** The agent stops classifying locations and ships every location with its prefab name and an `unplaced` flag; `extract.ClassifyLocations` (one table) is applied by the central app per stored snapshot and by `worldevents`. A new `internal/biomegrid` package builds a 1024² byte grid from `worldgen.NewBase`, served gzip'd at `/tiles/{id}/{key}/biomes` from an in-memory cache; the web readout looks the cursor up in it.

**Tech Stack:** Go 1.2x (`~/.local/go/bin` on PATH), SvelteKit 5 + Leaflet, Vitest, Playwright (`make e2e`; headless Chromium needs `hack/playwright-libs.sh` libs via LD_LIBRARY_PATH).

**Spec:** `docs/superpowers/specs/2026-10-05-cursor-biome-and-locations-design.md`

## Global Constraints

- Coordinate labels stay `X · Z`.
- Biome grid: 1024×1024, 20 m cells, cell `(floor(x/20)+512, floor(z/20)+512)`, row-major by gz (row 0 = south), sampled at cell centre; byte 0 none, 1 Meadows, 2 Black Forest, 3 Swamp, 4 Mountains, 5 Plains, 6 Mistlands, 7 Ashlands, 8 Deep North, 9 Ocean.
- Biome display names exactly: Meadows, Black Forest, Swamp, Mountains, Plains, Mistlands, Ashlands, Deep North, Ocean (same as `worldevents.BiomeName`).
- Endpoint `GET /tiles/{id}/{key}/biomes`: unlock check, key must equal the current tile-set key, `Content-Type: application/octet-stream`, `Content-Encoding: gzip`, `Cache-Control: public, max-age=31536000, immutable`; anything else 404.
- Layers in order after `signs`: `landmarks` "Landmarks" (on), `dungeons` "Dungeons" (off), `minor` "Minor places" (off). Dungeons and minor places hidden below Leaflet zoom 3.
- Location table exactly as the spec's "The set" table; unique = Vendor_BlackForest, Hildir_camp, BogWitch_Camp, AncientUpgradeStation.
- Go tests: `go test ./...`; web: `cd web && npx vitest run` and `npx svelte-check`; commit after each task with the session trailer `Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T`.

---

### Task 1: One location table and `ClassifyLocations` (agent side)

**Files:**
- Modify: `internal/extract/tables.go` (replace `bossAltars`, `traders`, `dungeons`)
- Modify: `internal/extract/snapshot.go` (Marker gets `Group`, `Unplaced`)
- Modify: `internal/extract/extract.go:153-170` (emit raw locations)
- Create: `internal/extract/locations.go`, `internal/extract/locations_test.go`
- Modify: `internal/extract/extract_test.go:46-76`, `internal/extract/golden_test.go:68-77`

**Interfaces:**
- Produces: `func ClassifyLocations(raw []Marker) []Marker` — new slice (never nil), entries with Kind/Label/Group set, unmapped dropped, unplaced unique dropped, input order kept, IDs unchanged. `Marker.Group string \`json:"group,omitempty"\``, `Marker.Unplaced bool \`json:"unplaced,omitempty"\``. Group values `"landmarks"`, `"dungeons"`, `"minor"`. Kind values `boss_altar`, `trader`, `dungeon`, `landmark`; raw entries use Kind `"location"`.

- [ ] **Step 1: Write the failing test** `internal/extract/locations_test.go`:

```go
package extract

import "testing"

func TestClassifyLocations(t *testing.T) {
	raw := []Marker{
		{ID: "loc-1", Kind: "location", Type: "Eikthyrnir"},
		{ID: "loc-2", Kind: "location", Type: "Crypt3"},
		{ID: "loc-3", Kind: "location", Type: "Runestone_Meadows"},
		{ID: "loc-4", Kind: "location", Type: "BogWitch_Camp", Unplaced: true},
		{ID: "loc-5", Kind: "location", Type: "BogWitch_Camp"},
		{ID: "loc-6", Kind: "location", Type: "AncientUpgradeStation"},
		{ID: "loc-7", Kind: "location", Type: "MorgenHole2", Unplaced: true},
		{ID: "loc-8", Kind: "location", Type: "Hildir_crypt"},
		// An older agent's entry: already classified, no unplaced flag.
		{ID: "loc-9", Kind: "dungeon", Type: "SunkenCrypt4", Label: "Sunken crypt"},
	}
	got := ClassifyLocations(raw)
	want := []struct{ id, kind, label, group string }{
		{"loc-1", "boss_altar", "Eikthyr", "landmarks"},
		{"loc-2", "dungeon", "Burial chambers", "dungeons"},
		{"loc-5", "trader", "Bog Witch", "landmarks"},
		{"loc-6", "landmark", "Forge of Potential", "landmarks"},
		{"loc-7", "landmark", "Putrid hole", "minor"},
		{"loc-8", "dungeon", "Smouldering tomb", "landmarks"},
		{"loc-9", "dungeon", "Sunken crypt", "dungeons"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d: %+v", len(got), got)
	}
	for i, w := range want {
		g := got[i]
		if g.ID != w.id || g.Kind != w.kind || g.Label != w.label || g.Group != w.group {
			t.Errorf("%d = %+v, want %+v", i, g, w)
		}
	}
	if raw[0].Kind != "location" {
		t.Error("input was modified")
	}
	if ClassifyLocations(nil) == nil {
		t.Error("nil result")
	}
}

func TestLocationTableComplete(t *testing.T) {
	// Every prefab in the spec's table, with its group.
	want := map[string]string{
		"Eikthyrnir": "landmarks", "GDKing": "landmarks", "Bonemass": "landmarks", "Dragonqueen": "landmarks",
		"GoblinKing": "landmarks", "Mistlands_DvergrBossEntrance1": "landmarks", "FaderLocation": "landmarks", "DN_Bossroom": "landmarks",
		"Vendor_BlackForest": "landmarks", "Hildir_camp": "landmarks", "BogWitch_Camp": "landmarks",
		"AncientUpgradeStation": "landmarks", "StartTemple": "landmarks",
		"PlaceofMystery1": "landmarks", "PlaceofMystery2": "landmarks", "PlaceofMystery3": "landmarks",
		"Hildir_cave": "landmarks", "Hildir_crypt": "landmarks", "Hildir_plainsfortress": "landmarks",
		"CharredFortress": "landmarks", "NorthMemorialPlace": "landmarks",
		"Crypt2": "dungeons", "Crypt3": "dungeons", "Crypt4": "dungeons", "SunkenCrypt4": "dungeons",
		"TrollCave02": "dungeons", "MountainCave02": "dungeons",
		"Mistlands_DvergrTownEntrance1": "dungeons", "Mistlands_DvergrTownEntrance2": "dungeons",
		"MorkBorg": "dungeons", "TheHole01": "dungeons",
		"GoblinCamp2": "minor", "BearCave": "minor", "NorthVillage": "minor",
		"MorgenHole1": "minor", "MorgenHole2": "minor", "MorgenHole3": "minor",
	}
	if len(locationTable) != len(want) {
		t.Errorf("table has %d entries, want %d", len(locationTable), len(want))
	}
	for prefab, group := range want {
		if e, ok := locationTable[prefab]; !ok || e.Group != group {
			t.Errorf("%s = %+v, want group %s", prefab, e, group)
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/extract -run 'TestClassifyLocations|TestLocationTableComplete'` — expect FAIL (undefined).

- [ ] **Step 3: Implement.** In `snapshot.go` add to `Marker` after `Namer`:

```go
	// Group picks a location's map layer: "landmarks", "dungeons" or
	// "minor" (ClassifyLocations). Empty for everything else.
	Group string `json:"group,omitempty"`
	// Unplaced marks a location the game has planned but not yet built:
	// nobody has loaded its zone. Omitted (false) in older snapshots.
	Unplaced bool `json:"unplaced,omitempty"`
```

Replace the three maps in `tables.go` with:

```go
// locationEntry is how one location prefab shows on the atlas.
type locationEntry struct {
	Kind, Label, Group string
	// Unique sites (one per world) keep up to ~10 planned candidates until
	// a player loads one; only the placed one is real.
	Unique bool
}

// locationTable maps location prefabs to markers (spec 2026-10-05,
// cursor biome and locations). Prefabs not listed are not shown.
var locationTable = map[string]locationEntry{
	"Eikthyrnir":                    {"boss_altar", "Eikthyr", "landmarks", false},
	"GDKing":                        {"boss_altar", "The Elder", "landmarks", false},
	"Bonemass":                      {"boss_altar", "Bonemass", "landmarks", false},
	"Dragonqueen":                   {"boss_altar", "Moder", "landmarks", false},
	"GoblinKing":                    {"boss_altar", "Yagluth", "landmarks", false},
	"Mistlands_DvergrBossEntrance1": {"boss_altar", "The Queen", "landmarks", false},
	"FaderLocation":                 {"boss_altar", "Fader", "landmarks", false},
	"DN_Bossroom":                   {"boss_altar", "Kall Fimbulbringer", "landmarks", false},

	"Vendor_BlackForest": {"trader", "Haldor", "landmarks", true},
	"Hildir_camp":        {"trader", "Hildir", "landmarks", true},
	"BogWitch_Camp":      {"trader", "Bog Witch", "landmarks", true},

	"AncientUpgradeStation": {"landmark", "Forge of Potential", "landmarks", true},
	"StartTemple":           {"landmark", "Sacrificial stones", "landmarks", false},
	"PlaceofMystery1":       {"landmark", "Mysterious location", "landmarks", false},
	"PlaceofMystery2":       {"landmark", "Mysterious location", "landmarks", false},
	"PlaceofMystery3":       {"landmark", "Mysterious location", "landmarks", false},
	"Hildir_cave":           {"dungeon", "Howling cavern", "landmarks", false},
	"Hildir_crypt":          {"dungeon", "Smouldering tomb", "landmarks", false},
	"Hildir_plainsfortress": {"dungeon", "Sealed tower", "landmarks", false},
	"CharredFortress":       {"landmark", "Charred fortress", "landmarks", false},
	"NorthMemorialPlace":    {"landmark", "Memorial site", "landmarks", false},

	"Crypt2":                        {"dungeon", "Burial chambers", "dungeons", false},
	"Crypt3":                        {"dungeon", "Burial chambers", "dungeons", false},
	"Crypt4":                        {"dungeon", "Burial chambers", "dungeons", false},
	"SunkenCrypt4":                  {"dungeon", "Sunken crypt", "dungeons", false},
	"TrollCave02":                   {"dungeon", "Troll cave", "dungeons", false},
	"MountainCave02":                {"dungeon", "Frost cave", "dungeons", false},
	"Mistlands_DvergrTownEntrance1": {"dungeon", "Infested mine", "dungeons", false},
	"Mistlands_DvergrTownEntrance2": {"dungeon", "Infested mine", "dungeons", false},
	"MorkBorg":                      {"dungeon", "Mörkhalla", "dungeons", false},
	"TheHole01":                     {"dungeon", "Winding tunnels", "dungeons", false},

	"GoblinCamp2": {"landmark", "Fuling village", "minor", false},
	"BearCave":    {"dungeon", "Bear cave", "minor", false},
	"NorthVillage": {"landmark", "Abandoned village", "minor", false},
	"MorgenHole1": {"landmark", "Putrid hole", "minor", false},
	"MorgenHole2": {"landmark", "Putrid hole", "minor", false},
	"MorgenHole3": {"landmark", "Putrid hole", "minor", false},
}
```

(Run `gofmt -w` afterwards.) Create `locations.go`:

```go
package extract

// ClassifyLocations applies locationTable to a snapshot's locations: kind,
// label and group from each entry's prefab (Type), so snapshots from
// agents that classified locations themselves get the same answer.
// Unmapped prefabs are dropped, and so are the unplaced candidates of
// unique sites. The result is a new, never-nil slice in input order.
func ClassifyLocations(raw []Marker) []Marker {
	out := make([]Marker, 0, len(raw)/8)
	for _, m := range raw {
		e, ok := locationTable[m.Type]
		if !ok || (e.Unique && m.Unplaced) {
			continue
		}
		m.Kind, m.Label, m.Group = e.Kind, e.Label, e.Group
		out = append(out, m)
	}
	return out
}
```

In `extract.go` replace the location loop body with:

```go
	for i, l := range w.Locations {
		s.Locations = append(s.Locations, Marker{
			ID: fmt.Sprintf("loc-%d", i+1), Kind: "location", Type: names.Name(l.Hash),
			X: l.Pos[0], Y: l.Pos[1], Z: l.Pos[2], Unplaced: !l.Placed,
		})
	}
```

Check `names.Name` for an unknown hash (returns "" or a hex form) — either way it won't match the table; keep it. Update `extract_test.go`: the three test locations now give 3 raw entries (`Kind == "location"`), and `ClassifyLocations(s.Locations)` gives 2 (boss_altar Eikthyr, dungeon). Set `Placed: true` on those test `save.Location`s. In `golden_test.go` count altars over `ClassifyLocations(s.Locations)`. Also grep for any other `bossAltars`/`traders`/`dungeons` users (`grep -rn "bossAltars\|traders\[\|dungeons\[" internal`) and move them to `locationTable`.

- [ ] **Step 4: Run** `go test ./internal/extract/ ./internal/agent/...` — PASS. Run `go vet ./...`.

- [ ] **Step 5: Commit** `feat(extract): ship every location; one table classifies them`.

---

### Task 2: Classify on the central app and in world events

**Files:**
- Modify: `internal/server/world.go` (worldState gets `locs`)
- Modify: `internal/server/api.go:410-414`
- Modify: `internal/worldevents/diff.go:15-44`
- Test: `internal/server/world_test.go` or `server_test.go` (snapshot API), `internal/worldevents/diff_test.go`

**Interfaces:**
- Consumes: `extract.ClassifyLocations`.
- Produces: snapshot API `locations[]` entries carry `group` and the classified `kind`/`label`; raw `kind:"location"` and unplaced unique entries never reach the browser.

- [ ] **Step 1: Failing tests.** In the server tests (follow the existing snapshot-API test helpers in `helpers_test.go`), ingest a snapshot whose `Locations` are raw (`Kind:"location"`): an explored `AncientUpgradeStation`, an explored unplaced `BogWitch_Camp`, an explored `Runestone_Meadows`, and an unexplored `Crypt2`. Expect the API's `locations` to be exactly one entry: kind `landmark`, label `Forge of Potential`, group `landmarks`. Second case: a legacy pre-classified entry `{Kind:"dungeon", Type:"SunkenCrypt4", Label:"Sunken crypt"}` comes back with group `dungeons`. In worldevents, a Diff test where a new tombstone lies within `NearRadius` of a raw `{Kind:"location", Type:"Vendor_BlackForest"}` in explored ground: `Near` reads `Haldor`.

- [ ] **Step 2: Run** `go test ./internal/server ./internal/worldevents` — FAIL.

- [ ] **Step 3: Implement.** `worldState` gains `locs []extract.Marker // ClassifyLocations(snap.Locations)`, set in `newWorldState`. In `snapshot()`: `Locations: keep(ws.locs, inMarker)`. In `worldevents.Diff`, after the nil check: `cur2 := *cur; cur2.Locations = extract.ClassifyLocations(cur.Locations); d := differ{prev: prev, cur: &cur2, geo: geo}` (a shallow copy, so the caller's snapshot is untouched); update the `Near` doc comment to say it expects classified locations. Search for other readers of `snap.Locations` (`grep -rn "\.Locations" internal --include=*.go | grep -v _test`) and route them through the classified slice.

- [ ] **Step 4: Run** `go test ./...` — PASS.

- [ ] **Step 5: Commit** `feat(server): classify locations per stored snapshot and for world events`.

---

### Task 3: The biome grid and its endpoint

**Files:**
- Create: `internal/biomegrid/biomegrid.go`, `internal/biomegrid/biomegrid_test.go`
- Create: `internal/server/biomes.go`, `internal/server/biomes_test.go`
- Modify: `internal/server/server.go` (route + `server.biomes` field in `newServer`)

**Interfaces:**
- Produces: `biomegrid.Size = 1024`, `biomegrid.Cell = 20`, `func biomegrid.Index(b worldgen.Biome) byte`, `func biomegrid.Build(biome func(x, z float32) worldgen.Biome) []byte` (len Size², row-major gz), `func biomegrid.Gzip(grid []byte) []byte`, `type biomegrid.Cache` with `func NewCache(max int) *Cache` and `func (c *Cache) Get(seed, gen int32) []byte` (gzip bytes; builds with `worldgen.NewBase(seed, gen).Biome` once per key, concurrent callers share one build, keeps the `max` most recently used keys). Route `GET /tiles/{id}/{key}/biomes`.

- [ ] **Step 1: Failing tests** (`biomegrid_test.go`):

```go
package biomegrid

import (
	"bytes"
	"compress/gzip"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

func TestIndex(t *testing.T) {
	cases := map[worldgen.Biome]byte{
		worldgen.Meadows: 1, worldgen.BlackForest: 2, worldgen.Swamp: 3, worldgen.Mountain: 4,
		worldgen.Plains: 5, worldgen.Mistlands: 6, worldgen.AshLands: 7, worldgen.DeepNorth: 8,
		worldgen.Ocean: 9, 0: 0,
	}
	for b, want := range cases {
		if got := Index(b); got != want {
			t.Errorf("Index(%v) = %d, want %d", b, got, want)
		}
	}
}

func TestBuildOrientation(t *testing.T) {
	// East half Meadows, west half Ocean; north of z=0 Swamp overrides east.
	grid := Build(func(x, z float32) worldgen.Biome {
		switch {
		case x < 0:
			return worldgen.Ocean
		case z >= 0:
			return worldgen.Swamp
		}
		return worldgen.Meadows
	})
	if len(grid) != Size*Size {
		t.Fatalf("len %d", len(grid))
	}
	at := func(x, z float64) byte {
		gx, gz := int(x/Cell)+Size/2, int(z/Cell)+Size/2
		return grid[gz*Size+gx]
	}
	if at(-5000, -5000) != 9 || at(5000, -5000) != 1 || at(5000, 5000) != 3 {
		t.Fatalf("orientation wrong: %d %d %d", at(-5000, -5000), at(5000, -5000), at(5000, 5000))
	}
}

func TestBuildRealSeed(t *testing.T) {
	// The golden chunked world (MuleVikings): the spawn is Meadows, and the
	// world edge (outside 10 500 m) has no biome... check what Generator.Biome
	// returns past the edge and assert Index of it here.
	g := worldgen.NewBase(-1032944128, 2)
	grid := Build(g.Biome)
	if grid[(Size/2)*Size+Size/2] != 1 {
		t.Fatalf("spawn cell = %d, want Meadows", grid[(Size/2)*Size+Size/2])
	}
	z, _ := gzip.NewReader(bytes.NewReader(Gzip(grid)))
	back, _ := io.ReadAll(z)
	if !bytes.Equal(back, grid) {
		t.Fatal("gzip round trip")
	}
}

func TestCacheSingleFlight(t *testing.T) {
	var builds atomic.Int32
	c := NewCache(2)
	c.build = func(seed, gen int32) []byte { builds.Add(1); return []byte{byte(seed)} }
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.Get(1, 2) }()
	}
	wg.Wait()
	if builds.Load() != 1 {
		t.Fatalf("builds = %d", builds.Load())
	}
	c.Get(2, 2)
	c.Get(3, 2) // evicts (1,2)
	c.Get(1, 2)
	if builds.Load() != 4 {
		t.Fatalf("builds = %d after eviction", builds.Load())
	}
}
```

(Before finalising `TestBuildRealSeed`, check whether `Generator.Biome` at the cell centre `(10, 10)` is Meadows for this seed; if the spawn cell is ocean, pick another known point from `internal/worldgen/oracle_test.go`. Grid corners lie outside the world disc — record what `Biome` returns there, likely Ocean, and leave it: the client says "World edge" outside the disc anyway.)

- [ ] **Step 2: Run** `go test ./internal/biomegrid` — FAIL.

- [ ] **Step 3: Implement** `biomegrid.go`:

```go
// Package biomegrid is the base biome of a world on a 1024² grid of 20 m
// cells, for the browser's cursor readout (spec 2026-10-05, cursor biome).
// Cell (gx, gz) = (floor(x/20)+512, floor(z/20)+512), row-major by gz
// (row 0 is the south edge), sampled at its centre.
package biomegrid

import (
	"bytes"
	"compress/gzip"
	"container/list"
	"sync"

	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

const (
	Size = 1024
	Cell = 20
)

var indexOf = map[worldgen.Biome]byte{
	worldgen.Meadows: 1, worldgen.BlackForest: 2, worldgen.Swamp: 3, worldgen.Mountain: 4,
	worldgen.Plains: 5, worldgen.Mistlands: 6, worldgen.AshLands: 7, worldgen.DeepNorth: 8,
	worldgen.Ocean: 9,
}

// Index is b's byte in the grid; 0 for none.
func Index(b worldgen.Biome) byte { return indexOf[b] }

// Build samples biome at every cell centre.
func Build(biome func(x, z float32) worldgen.Biome) []byte {
	grid := make([]byte, Size*Size)
	for gz := 0; gz < Size; gz++ {
		z := float32((gz-Size/2)*Cell + Cell/2)
		for gx := 0; gx < Size; gx++ {
			x := float32((gx-Size/2)*Cell + Cell/2)
			grid[gz*Size+gx] = Index(biome(x, z))
		}
	}
	return grid
}

// Gzip compresses grid at best compression (about 45 KB for a real world).
func Gzip(grid []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write(grid)
	zw.Close()
	return buf.Bytes()
}

type key struct{ seed, gen int32 }

type entry struct {
	k    key
	done chan struct{}
	gz   []byte
}

// Cache keeps the gzip'd grids of the max most recently used worlds. A
// build takes about a second; concurrent callers for one world share it.
type Cache struct {
	max   int
	build func(seed, gen int32) []byte

	mu      sync.Mutex
	order   *list.List // of *entry, most recent first
	entries map[key]*list.Element
}

func NewCache(max int) *Cache {
	return &Cache{
		max:     max,
		build:   func(seed, gen int32) []byte { return Gzip(Build(worldgen.NewBase(seed, gen).Biome)) },
		order:   list.New(),
		entries: map[key]*list.Element{},
	}
}

// Get returns (seed, gen)'s gzip'd grid, building it on a miss.
func (c *Cache) Get(seed, gen int32) []byte {
	k := key{seed, gen}
	c.mu.Lock()
	if el, ok := c.entries[k]; ok {
		c.order.MoveToFront(el)
		e := el.Value.(*entry)
		c.mu.Unlock()
		<-e.done
		return e.gz
	}
	e := &entry{k: k, done: make(chan struct{})}
	c.entries[k] = c.order.PushFront(e)
	for c.order.Len() > c.max {
		old := c.order.Back()
		c.order.Remove(old)
		delete(c.entries, old.Value.(*entry).k)
	}
	c.mu.Unlock()
	e.gz = c.build(seed, gen)
	close(e.done)
	return e.gz
}
```

Note `worldgen.NewBase` — check its signature (it may also return an error or need `NewChecked` for generator-version gating; the tile manager refuses `gen > worldgen.MaxGenVersion`; the handler must 404 in that case instead of building — see Step 5).

- [ ] **Step 4: Run** `go test ./internal/biomegrid` — PASS.

- [ ] **Step 5: Endpoint test then handler.** `internal/server/biomes_test.go`, using the existing helpers that build a server with an ingested snapshot and an unlocked cookie (see `tiles_test.go` for the pattern): (a) locked → 404; (b) wrong key → 404; (c) right key → 200, the three headers above, body gunzips to `biomegrid.Size²` bytes; (d) a snapshot with `GenVersion > worldgen.MaxGenVersion` → 404. Run — FAIL. Then `internal/server/biomes.go`:

```go
package server

import (
	"net/http"

	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

// biomes serves GET /tiles/{id}/{key}/biomes: the world's base-biome grid
// (internal/biomegrid), gzip'd, for the cursor readout. key must be the
// server's current tile-set key, which names the seed and generator, so
// the response never changes and is cached for good.
func (s *server) biomes(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.unlocked(r, id); !ok {
		notFound(w)
		return
	}
	ws, ok, err := s.worlds.get(r.Context(), id)
	if err != nil {
		s.internalError(w, "biomes", id, err)
		return
	}
	if !ok {
		notFound(w)
		return
	}
	seed, gen := ws.snap.World.Seed, ws.snap.World.GenVersion
	if r.PathValue("key") != s.Tiles.Key(seed, gen) || gen > worldgen.MaxGenVersion {
		notFound(w)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(s.biomeGrids.Get(seed, gen))
}
```

Add `biomeGrids *biomegrid.Cache` to `server`, `biomeGrids: biomegrid.NewCache(4)` in `newServer`, and `mux.HandleFunc("GET /tiles/{id}/{key}/biomes", s.biomes)` next to the fog-tile route (check the existing `/tiles/` catch-all at server.go:139 doesn't shadow it, and that the gzip middleware isn't applied on top). Run `go test ./internal/server` — PASS.

- [ ] **Step 6: Commit** `feat(server): the biome grid, built per world and served gzip'd`.

---

### Task 4: Biome in the cursor readout (web)

**Files:**
- Create: `web/src/lib/biomes.ts`, `web/src/lib/biomes.test.ts`
- Modify: `web/src/lib/api.ts` (`biomesUrl`, `getBiomes`), `web/src/lib/state.svelte.ts` (hold the grid per tile key), `web/src/lib/components/ScaleReadout.svelte`, `web/src/lib/components/DesktopShell.svelte:429`
- Test: `web/src/lib/api.test.ts`, readout tests if a component test exists (else cover the pure `placeLabel` function)

**Interfaces:**
- Produces: `BIOME_NAMES: string[]` (index → name, `''` at 0), `biomeAt(grid: Uint8Array, x: number, z: number): string`, `placeLabel(cursor, {mask, grid}): string` with the spec's order: "Hover the map" / "World edge" / "Unexplored" / biome name / `''`. `getBiomes(id, key, f?)` → `Promise<Uint8Array>` (the browser un-gzips via Content-Encoding; just `new Uint8Array(await res.arrayBuffer())`, reject unless length is 1024²).

- [ ] **Step 1: Failing tests** in `biomes.test.ts`: `biomeAt` on a synthetic grid (fill cell for x=5000,z=5000 with 3 → "Swamp"; cell index `(Math.floor(z/20)+512)*1024 + Math.floor(x/20)+512`); off-grid (x=20000) → `''`; `placeLabel` covers all five states (no cursor; outside `insideWorld`; mask present and unexplored; explored with grid → name; explored with no grid → `''`).
- [ ] **Step 2: Run** `cd web && npx vitest run src/lib/biomes.test.ts` — FAIL.
- [ ] **Step 3: Implement** `biomes.ts` (reuse `insideWorld` from `geo.ts`, `isExplored` from `explored.ts`); move the readout's `place` logic to `placeLabel` and call it. In `api.ts`: `biomesUrl(id, key) = /tiles/${enc(id)}/${enc(key)}/biomes` and `getBiomes` with the same timeout/abort pattern as `getSnapshot`. In `AppState`, load the grid when `card.tiles.key` changes (keyed cache: `biomes = $state.raw<{key: string; grid: Uint8Array}>()`; a failed load leaves it undefined and is retried on the next key change only — the readout then just shows no biome). Pass `grid` to `ScaleReadout` from `DesktopShell`. Update the ScaleReadout header comment (it says "the client has no biome data").
- [ ] **Step 4: Run** `npx vitest run && npx svelte-check` — PASS.
- [ ] **Step 5: Commit** `feat(web): the biome under the cursor`.

---

### Task 5: Landmarks, Dungeons and Minor places layers (web)

**Files:**
- Modify: `web/src/lib/markers.ts` (LayerKey, LAYERS, PinType, KICK, LAYER_OF → by group, SORT, KIND_ORDER, `markerFor` cases, `layerCounts`), `web/src/lib/types.ts` (`Marker.group?`, `unplaced?`), `web/src/lib/icons/paths.ts` (+ `anvil`, `sparkles`, `circle-dot`, `castle`, `landmark` Lucide geometry), `web/src/lib/icons/paths.test.ts`, `web/src/lib/components/LayersPanel.svelte:46`, `web/src/lib/search.ts` (landmark searchable like trader: sub-line = kicker), `web/tests/fixtures/snapshot.json` (add `group` to every location; add a landmark and a minor-place entry)
- Test: `web/src/lib/markers.test.ts`, `web/src/lib/search.test.ts`

**Interfaces:**
- Consumes: API `locations[].group` ("landmarks" | "dungeons" | "minor"); kind `landmark`.
- Produces: `LayerKey` drops `'locations'`, adds `'landmarks' | 'dungeons' | 'minor'`; `PinType` adds `'landmark'`.

- [ ] **Step 1: Failing tests** in `markers.test.ts`: `LAYERS` keys end `signs, landmarks, dungeons, minor` with defaults on/off/off and labels "Landmarks", "Dungeons", "Minor places"; a `{kind:'landmark', type:'AncientUpgradeStation', label:'Forge of Potential', group:'landmarks'}` builds a `landmark` pin on layer `landmarks` with icon `anvil`, no minZoom, kicker and title both "Forge of Potential"; a dungeon with `group:'dungeons'` is on layer `dungeons` with minZoom 3; `Hildir_crypt` dungeon with group `landmarks` is on `landmarks` with **no** minZoom; a `group:'minor'` entry has minZoom 3; a location with no `group` (older server) falls back: boss_altar/trader → landmarks, dungeon → dungeons; `layerCounts` has the three keys. Search: "forge" finds the Forge of Potential.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement.** Layer of a location = `m.group` if valid, else the fallback above; minZoom 3 iff layer is `dungeons` or `minor`. Landmark icon by type: `AncientUpgradeStation` → `anvil`, `PlaceofMystery*` → `sparkles`, `StartTemple` → `circle-dot`, `CharredFortress` → `castle`, `NorthMemorialPlace` → `landmark`, any other landmark → `arch`. Pin: INK 26 (default branch of `pinFor`). Card: kicker = label, title = label, note = DUNGEON_NOTE. Layer icons: landmarks `flame`, dungeons `arch`, minor `mountain`. LayersPanel: the "Only where someone has been" sub-line goes on all three. Copy Lucide geometry from `node_modules/lucide-svelte/dist/icons/<name>.svelte` and add the names to the test's glob.
- [ ] **Step 4: Run** `npx vitest run && npx svelte-check` — PASS. Check `WorldTab`'s `nearestAltar` still works (kind `boss_altar` unchanged).
- [ ] **Step 5: Commit** `feat(web): Landmarks, Dungeons and Minor places layers`.

---

### Task 6: e2e, docs, final check

**Files:**
- Modify: `web/tests/e2e/global-setup.ts` / seeded snapshot (raw `kind:"location"` entries incl. an `AncientUpgradeStation` in explored ground), `web/tests/e2e/desktop.spec.ts`
- Modify: `README.md` (layers; limitation: base biomes only, no 1.0 alt-biome names), `design/DESIGN-NOTES.md` (§3.13 layer table, §3.14 readout now has biome, the locations table near the end → point to `locationTable`)

- [ ] **Step 1:** e2e: hovering the map over explored ground shows a biome name in the readout (pick a seeded point whose biome is known from the seeded seed — compute it with a throwaway Go call to `biomegrid`); the Layers panel lists Landmarks (on), Dungeons (off), Minor places (off); the Forge of Potential pin is present at default zoom.
- [ ] **Step 2:** `make e2e` — PASS (Chromium libs: `source hack/playwright-libs.sh` or the LD_LIBRARY_PATH the Makefile expects).
- [ ] **Step 3:** Docs as listed. `go test ./... && cd web && npx vitest run && npx svelte-check`.
- [ ] **Step 4: Commit** `test(e2e)/docs: biome readout and the location layers`.
