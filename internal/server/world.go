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
