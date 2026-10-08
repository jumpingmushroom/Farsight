package worldevents

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
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
	locks map[string]chan struct{} // one per server, size 1: CatchUps for a server run one at a time

	// ctx is CatchUpAsync's own long-lived context (independent of any
	// request's); cancel stops it, for shutdown. CatchUp itself always
	// uses the ctx its caller passes in.
	ctx    context.Context
	cancel context.CancelFunc

	// wg tracks CatchUpAsync calls still running, so Idle (tests, and
	// shutdown) can wait for them without a sleep.
	wg sync.WaitGroup

	// AfterApply, if set, is called synchronously after each snapshot
	// CatchUp applies, before moving to the next one. For tests only: it
	// lets a test pace or observe a backfill deterministically, without a
	// sleep.
	AfterApply func()
}

// NewDeriver returns a Deriver over st. log may be nil.
func NewDeriver(st *store.Store, log *slog.Logger) *Deriver {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Deriver{store: st, log: log, locks: map[string]chan struct{}{}, ctx: ctx, cancel: cancel}
}

// sem returns serverID's lock: a size-1 channel semaphore (rather than a
// sync.Mutex) so acquiring it can be abandoned when ctx is done instead of
// blocking for however long the holder takes.
func (d *Deriver) sem(serverID string) chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	ch := d.locks[serverID]
	if ch == nil {
		ch = make(chan struct{}, 1)
		d.locks[serverID] = ch
	}
	return ch
}

// acquire holds serverID's lock until release is called, or reports ok
// false if ctx is done first (without taking the lock).
func (d *Deriver) acquire(ctx context.Context, serverID string) (release func(), ok bool) {
	ch := d.sem(serverID)
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, true
	case <-ctx.Done():
		return nil, false
	}
}

// Cancel stops any future CatchUpAsync work, and cancels any already
// running: a call to CatchUp it started returns (with ctx.Err()) instead
// of blocking on another server's lock or running further snapshots. It
// does not affect a ctx passed directly to CatchUp. Call Idle afterwards
// to wait for the in-flight work to actually return, e.g. before closing
// the store.
func (d *Deriver) Cancel() { d.cancel() }

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
// and returns the number of events written. It returns ctx.Err() at once
// if ctx is done before the per-server lock is acquired, and checks ctx
// again between snapshots, so a long backfill (or one waiting its turn
// behind another) stops promptly when ctx is cancelled instead of running
// to completion.
//
// A panic anywhere in the diff path (worldgen, explored, or elsewhere) is
// recovered here rather than crashing the process: both callers run this
// in the background (CatchUpAsync, and the startup backfill's own
// goroutine), where an unrecovered panic would take farsight down, and the
// startup backfill would then crash-loop on every restart. The recovered
// panic is logged with the server id and a stack trace, and returned as an
// error like any other CatchUp failure, so the caller skips this server
// and moves on exactly as it does for a decode or store error.
func (d *Deriver) CatchUp(ctx context.Context, serverID string) (n int, err error) {
	defer func() {
		if p := recover(); p != nil {
			d.log.Error("world events: recovered panic", "server", serverID, "panic", p, "stack", string(debug.Stack()))
			err = fmt.Errorf("worldevents: recovered panic for server %s: %v", serverID, p)
		}
	}()

	release, ok := d.acquire(ctx, serverID)
	if !ok {
		return 0, ctx.Err()
	}
	defer release()

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
		if err := ctx.Err(); err != nil {
			return written, err
		}
		cur, ok, err := d.load(ctx, serverID, k.SaveID)
		if err != nil {
			return written, err
		}
		if !ok {
			continue // removed since SnapshotsAfter listed it
		}
		if prev != nil && !sameWorld(prev, cur) {
			// Diffing across a world swap or a restored backup would
			// report everything the other world has as new.
			d.log.Info("world events: world changed, new baseline", "server", serverID, "saveId", cur.SaveID)
			prev = nil
		}
		n, err := d.apply(ctx, serverID, k, prev, cur, backfilled)
		if err != nil {
			return written, err
		}
		written += n
		prev, backfilled = cur, true
		if d.AfterApply != nil {
			d.AfterApply()
		}
	}
	if written > 0 {
		d.log.Info("world events", "server", serverID, "events", written, "saves", len(keys))
	}
	return written, nil
}

// sameWorld reports whether cur continues prev's world: the same name
// and seed, and a world clock that has not gone back. The clock only
// moves forward in play, so going back means a restored backup (or a
// fresh world under the same name and seed).
func sameWorld(prev, cur *extract.Snapshot) bool {
	return prev.World.Name == cur.World.Name && prev.World.Seed == cur.World.Seed &&
		cur.World.NetTime >= prev.World.NetTime
}

// CatchUpAsync starts CatchUp for serverID in the background and returns
// at once: ingest calls this so a slow backfill never holds up the
// response. It is still safe to race a concurrent CatchUp or CatchUpAsync
// for the same server (the per-server lock serialises them) or a process
// restart (every insert ignores existing rows); nothing it writes is
// request-scoped, so it runs with the Deriver's own context (cancelled by
// Cancel, for shutdown), independent of the request's. Errors are logged,
// not returned, except the one Cancel itself causes.
func (d *Deriver) CatchUpAsync(serverID string) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		if _, err := d.CatchUp(d.ctx, serverID); err != nil && d.ctx.Err() == nil {
			d.log.Error("world events", "server", serverID, "err", err)
		}
	}()
}

// Idle blocks until every CatchUpAsync call made so far has returned. For
// tests, which would otherwise race a background CatchUpAsync to read its
// result, or to close the store from under it.
func (d *Deriver) Idle() {
	d.wg.Wait()
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
