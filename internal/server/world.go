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

	fog *fogLazy // shared with the previous state when fogKey is unchanged
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
	w := &worldState{saveID: snap.SaveID, snap: snap}
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
