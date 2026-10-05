package worldevents

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/store"
)

// Deriver keeps a server's world-save events in step with its stored
// snapshots: CatchUp diffs every snapshot newer than the last one diffed
// against the one before it and writes the events and newly seen
// tombstones. The first run for a server (no world_diff row) is the
// backfill: it replays every stored snapshot, oldest first, the first one
// being the baseline. Re-running is harmless: ids are derived from the
// save and the object, and inserts ignore existing rows.
type Deriver struct {
	store *store.Store
	log   *slog.Logger

	mu    sync.Mutex
	locks map[string]*sync.Mutex // one per server: CatchUps for a server run one at a time
}

// NewDeriver returns a Deriver over st. log may be nil.
func NewDeriver(st *store.Store, log *slog.Logger) *Deriver {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Deriver{store: st, log: log, locks: map[string]*sync.Mutex{}}
}

func (d *Deriver) lock(serverID string) *sync.Mutex {
	d.mu.Lock()
	defer d.mu.Unlock()
	l := d.locks[serverID]
	if l == nil {
		l = &sync.Mutex{}
		d.locks[serverID] = l
	}
	return l
}

// load decodes one stored snapshot; ok is false if it isn't stored.
func (d *Deriver) load(ctx context.Context, serverID, saveID string) (*extract.Snapshot, bool, error) {
	blob, ok, err := d.store.Snapshot(ctx, serverID, saveID)
	if err != nil || !ok {
		return nil, false, err
	}
	var s extract.Snapshot
	if err := json.Unmarshal(blob, &s); err != nil {
		return nil, false, fmt.Errorf("worldevents: decode snapshot %s: %w", saveID, err)
	}
	return &s, true, nil
}

// CatchUp brings serverID's world events up to its newest stored snapshot
// and returns the number of events written.
func (d *Deriver) CatchUp(ctx context.Context, serverID string) (int, error) {
	l := d.lock(serverID)
	l.Lock()
	defer l.Unlock()

	state, backfilled, err := d.store.WorldDiffState(ctx, nil, serverID)
	if err != nil {
		return 0, err
	}
	keys, err := d.store.SnapshotsAfter(ctx, serverID, state)
	if err != nil || len(keys) == 0 {
		return 0, err
	}
	var prev *extract.Snapshot
	if backfilled {
		if prev, _, err = d.load(ctx, serverID, state.SaveID); err != nil {
			return 0, err
		}
		// prev nil here means the snapshot world_diff points to is no
		// longer stored: not something the server's own pruning does (it
		// never removes a server's world_diff snapshot, or anything
		// newer), but a defensive fallback for a concurrent Deriver that
		// raced this one, or a snapshot removed by some other means. The
		// next one becomes a new baseline (its tombstones are matched
		// against the stored ones instead of being reported as new).
	}
	written := 0
	for _, k := range keys {
		cur, ok, err := d.load(ctx, serverID, k.SaveID)
		if err != nil {
			return written, err
		}
		if !ok {
			continue // removed since SnapshotsAfter listed it
		}
		n, err := d.apply(ctx, serverID, k, prev, cur, backfilled)
		if err != nil {
			return written, err
		}
		written += n
		prev, backfilled = cur, true
	}
	if written > 0 {
		d.log.Info("world events", "server", serverID, "events", written, "saves", len(keys))
	}
	return written, nil
}

// apply writes cur's events (against prev) and its new tombstones, and
// advances the diff state to k, in one transaction. With prev nil, cur is
// a baseline: no events. Its tombstones are all new on the very first run
// (seenBefore false); after a gap they are new only if no stored tombstone
// of the same owner lies within MatchRadius.
func (d *Deriver) apply(ctx context.Context, serverID string, k store.SnapshotKey, prev, cur *extract.Snapshot, seenBefore bool) (int, error) {
	evs := Diff(prev, cur, NewGeo(cur, MaskOf(cur)))
	tombs := NewTombstones(prev, cur)
	if prev == nil && seenBefore {
		var err error
		if tombs, err = d.unseen(ctx, serverID, tombs); err != nil {
			return 0, err
		}
	}
	written := 0
	err := d.store.Tx(ctx, func(tx *sql.Tx) error {
		for _, e := range evs {
			body, err := json.Marshal(e)
			if err != nil {
				return err
			}
			ok, err := d.store.InsertRawEvent(ctx, tx, serverID, store.StoredEvent{ID: e.ID, Type: e.Type, At: e.At, Body: body})
			if err != nil {
				return err
			}
			if ok {
				written++
			}
		}
		for _, m := range tombs {
			t := store.Tombstone{ID: eventID("tombstone", cur.SaveID, m.Owner, round(m.X), round(m.Z)),
				Owner: m.Owner, X: m.X, Z: m.Z, FirstSeen: cur.SavedAt}
			if _, err := d.store.InsertTombstone(ctx, tx, serverID, t); err != nil {
				return err
			}
		}
		return d.store.PutWorldDiffState(ctx, tx, serverID, k)
	})
	return written, err
}

// unseen drops the tombstones that match a stored one (same owner, within
// MatchRadius).
func (d *Deriver) unseen(ctx context.Context, serverID string, tombs []extract.Marker) ([]extract.Marker, error) {
	var out []extract.Marker
	for _, m := range tombs {
		stored, err := d.store.Tombstones(ctx, serverID, m.Owner)
		if err != nil {
			return nil, err
		}
		seen := false
		for _, s := range stored {
			if dist(s.X, s.Z, m.X, m.Z) <= MatchRadius {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, m)
		}
	}
	return out, nil
}
