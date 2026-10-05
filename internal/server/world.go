package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/jumpingmushroom/farsight/internal/biomegrid"
	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/fog"
	"github.com/jumpingmushroom/farsight/internal/store"
	"github.com/jumpingmushroom/farsight/internal/tileset"
	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

// worldState is everything derived from one stored snapshot, built once
// and shared by the card, snapshot and tile handlers. It is read-only once
// built (snap included), apart from the fog data built on first use.
type worldState struct {
	saveID string
	snap   *extract.Snapshot
	locs   []extract.Marker // extract.ClassifyLocations(snap.Locations)
	mask   *explored.Mask
	enc    explored.Encoded // mask as the snapshot API returns it
	fogKey string
	pct    float64

	fog *fogLazy // shared with the previous state when fogKey is unchanged

	biomesMu   sync.Mutex
	biomesDone bool
	biomes     []worldgen.Biome // explored, in legend order
	home       worldgen.Biome
}

// legendOrder is the map legend's biome order, which the card's weather
// follows.
var legendOrder = []worldgen.Biome{
	worldgen.Ocean, worldgen.Meadows, worldgen.BlackForest, worldgen.Swamp, worldgen.Mountain,
	worldgen.Plains, worldgen.Mistlands, worldgen.AshLands, worldgen.DeepNorth,
}

// minBiomeCells is how many explored 12 m cells make a biome explored
// for the weather table.
const minBiomeCells = 50

// exploredBiomes are the biomes under at least minBiomeCells of mask's
// explored cells (each sampled at its centre on the biome grid), in
// legend order.
func exploredBiomes(mask *explored.Mask, grid []byte) []worldgen.Biome {
	var counts [256]int
	for i, by := range mask.Bits() { // row-major, LSB first
		for j := 0; by != 0; j, by = j+1, by>>1 {
			if by&1 == 0 {
				continue
			}
			px, py := (i*8+j)%explored.Size, (i*8+j)/explored.Size
			x := float64((px - explored.Size/2) * explored.CellMetres)
			z := float64((py - explored.Size/2) * explored.CellMetres)
			counts[biomegrid.Lookup(grid, x, z)]++
		}
	}
	var out []worldgen.Biome
	for _, b := range legendOrder {
		if counts[biomegrid.Index(b)] >= minBiomeCells {
			out = append(out, b)
		}
	}
	return out
}

// homeBiome is the biome under the explored base with the most pieces;
// Meadows when there is none.
func homeBiome(bases []extract.Base, mask *explored.Mask, grid []byte) worldgen.Biome {
	best := -1
	for i, b := range bases {
		if mask.At(float64(b.X), float64(b.Z)) && (best < 0 || b.Pieces > bases[best].Pieces) {
			best = i
		}
	}
	if best < 0 {
		return worldgen.Meadows
	}
	idx := biomegrid.Lookup(grid, float64(bases[best].X), float64(bases[best].Z))
	for _, b := range legendOrder {
		if biomegrid.Index(b) == idx {
			return b
		}
	}
	return worldgen.Meadows
}

// biomeSummary returns the explored biomes and the home biome, computed
// once per state from the world's biome grid. A grid error isn't kept:
// the next call tries again, and this one gets no biomes and a Meadows
// home. A world worldgen can't generate has no grid: no biomes, Meadows.
func (w *worldState) biomeSummary(grids *biomegrid.Cache) ([]worldgen.Biome, worldgen.Biome, error) {
	w.biomesMu.Lock()
	defer w.biomesMu.Unlock()
	if w.biomesDone {
		return w.biomes, w.home, nil
	}
	w.home = worldgen.Meadows
	if seed, gen := w.snap.World.Seed, w.snap.World.GenVersion; !tileset.Refused(gen) {
		grid, err := grids.Grid(seed, gen)
		if err != nil {
			return nil, worldgen.Meadows, err
		}
		w.biomes = exploredBiomes(w.mask, grid)
		w.home = homeBiome(w.snap.Bases, w.mask, grid)
	}
	w.biomesDone = true
	return w.biomes, w.home, nil
}

// fogLazy builds a mask's distance field and tile classes once, on first
// use. The field and class map depend only on the mask, so worldCache.get
// points a new worldState's fog at the previous state's fogLazy whenever
// the fog key is unchanged, instead of rebuilding (about half a second,
// 4 MB) on every new save.
type fogLazy struct {
	mask *explored.Mask

	once    sync.Once
	field   *fog.Field
	classes *fog.ClassMap
}

func newFogLazy(mask *explored.Mask) *fogLazy { return &fogLazy{mask: mask} }

func (f *fogLazy) get() (*fog.Field, *fog.ClassMap) {
	f.once.Do(func() {
		f.field = fog.NewField(f.mask)
		f.classes = fog.NewClassMap(f.field)
	})
	return f.field, f.classes
}

// newWorldState derives the state for snap. A snapshot from an agent that
// predates the explored mask has only exploredZones; those are rasterised
// onto the same 12 m grid, so both formats feed one code path.
func newWorldState(snap *extract.Snapshot, log *slog.Logger) *worldState {
	locs := extract.ClassifyLocations(snap.Locations)
	// snap is shared read-only, so copy it rather than clearing the
	// caller's Locations in place. Nothing else reads ws.snap.Locations
	// (the snapshot and card handlers read w.locs instead), and the raw
	// locations are the bulk of a decoded snapshot's memory (Plan 8, I3),
	// so this state doesn't keep both copies alive for its lifetime.
	trimmed := *snap
	trimmed.Locations = nil
	w := &worldState{saveID: trimmed.SaveID, snap: &trimmed, locs: locs}
	if snap.Explored != nil {
		m, err := explored.Decode(*snap.Explored)
		if err == nil {
			// Re-encode rather than passing the agent's bits through: Decode
			// only checks the first maskBytes+1 decompressed bytes, so a
			// broken or hostile agent could pad bits (an extra gzip member,
			// a header comment) and still pass validation. Every snapshot
			// GET would then carry that padding to browsers.
			w.mask, w.enc = m, explored.Encode(m, snap.Explored.Source)
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
	w.fog = newFogLazy(w.mask)
	return w
}

// fogData returns the distance field and tile classes, building them on
// first use (about half a second, once per fog key).
func (w *worldState) fogData() (*fog.Field, *fog.ClassMap) { return w.fog.get() }

// cardBosses returns the boss list for snap, rebuilt from its
// GlobalKeys with extract.BossesFromKeys so the central app has the
// correct boss list (no invented Writhan entry) without depending on the
// agent being updated — updating the agent restarts the game server.
//
// A snapshot stored before GlobalKeys existed has no "globalKeys" key in
// its JSON at all, so it decodes to a nil slice; that's the only case this
// falls back to the snapshot's own stored Bosses, with any defeated_writhan
// entry (the invented 8th boss) filtered out.
func cardBosses(snap *extract.Snapshot) []extract.Boss {
	if snap.GlobalKeys != nil {
		return extract.BossesFromKeys(snap.GlobalKeys)
	}
	bosses := make([]extract.Boss, 0, len(snap.Bosses))
	for _, b := range snap.Bosses {
		if b.Key == "defeated_writhan" {
			continue
		}
		bosses = append(bosses, b)
	}
	return bosses
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
	st := newWorldState(&snap, c.log)
	if e.st != nil && e.st.fogKey == st.fogKey {
		// Exploration (and so the field and class map) is unchanged since
		// the last snapshot: share it instead of rebuilding.
		st.fog = e.st.fog
	}
	e.st = st
	return e.st, true, nil
}
