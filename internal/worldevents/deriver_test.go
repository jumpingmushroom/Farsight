package worldevents

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/store"
)

// newStore opens an on-disk store: the in-memory one's shared cache
// reports "table is locked" to concurrent writers instead of waiting.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "farsight.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func put(t *testing.T, st *store.Store, s *extract.Snapshot) {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutSnapshot(context.Background(), "srv", s.SaveID, s.SavedAt, s.SavedAt, b); err != nil {
		t.Fatal(err)
	}
}

func storedTypes(t *testing.T, st *store.Store) []string {
	t.Helper()
	evs, err := st.EventsBetween(context.Background(), "srv", t0.Add(-time.Hour), t0.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for i := len(evs) - 1; i >= 0; i-- { // oldest first
		out = append(out, evs[i].Type)
	}
	return out
}

func TestCatchUpBackfillsThenFollows(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	d := NewDeriver(st, nil)

	s1 := world("chunked:1", t0) // baseline: Bjorn's tombstone predates tracking
	s2 := world("chunked:2", t0.Add(20*time.Minute))
	s2.Markers = append(s2.Markers, extract.Marker{ID: "tombstone-2", Kind: "tombstone", Owner: "Astrid", X: 700, Z: 700})
	put(t, st, s2) // stored out of order: replay goes by save time
	put(t, st, s1)

	n, err := d.CatchUp(ctx, "srv")
	if err != nil || n != 1 {
		t.Fatalf("backfill wrote %d (err %v), want 1", n, err)
	}
	if got := storedTypes(t, st); len(got) != 1 || got[0] != TypeTombstone {
		t.Fatalf("events = %v", got)
	}
	state, ok, err := st.WorldDiffState(ctx, nil, "srv")
	if err != nil || !ok || state.SaveID != "chunked:2" {
		t.Fatalf("state = %+v ok=%v err=%v", state, ok, err)
	}
	bjorn, _ := st.Tombstones(ctx, "srv", "Bjorn")
	astrid, _ := st.Tombstones(ctx, "srv", "Astrid")
	if len(bjorn) != 1 || !bjorn[0].FirstSeen.Equal(t0) || len(astrid) != 1 || !astrid[0].FirstSeen.Equal(s2.SavedAt) {
		t.Fatalf("tombstones: Bjorn %+v, Astrid %+v", bjorn, astrid)
	}

	if n, err := d.CatchUp(ctx, "srv"); err != nil || n != 0 {
		t.Fatalf("second run wrote %d (err %v), want 0", n, err)
	}

	s3 := world("chunked:3", t0.Add(40*time.Minute))
	s3.Markers = append(s3.Markers, extract.Marker{ID: "tombstone-2", Kind: "tombstone", Owner: "Astrid", X: 700, Z: 700})
	s3.GlobalKeys = append(s3.GlobalKeys, "defeated_gdking")
	put(t, st, s3)
	if n, err := d.CatchUp(ctx, "srv"); err != nil || n != 1 {
		t.Fatalf("follow-up wrote %d (err %v), want 1 (The Elder)", n, err)
	}
	if got := storedTypes(t, st); len(got) != 2 || got[1] != TypeBoss {
		t.Fatalf("events = %v", got)
	}
}

func TestCatchUpConcurrentRunsWriteEachEventOnce(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	s1 := world("chunked:1", t0)
	s2 := world("chunked:2", t0.Add(20*time.Minute))
	s2.GlobalKeys = append(s2.GlobalKeys, "defeated_gdking")
	put(t, st, s1)
	put(t, st, s2)

	var wg sync.WaitGroup
	total := make([]int, 4)
	for i := range total {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Two Derivers (as after a restart) and two runs each.
			d := NewDeriver(st, nil)
			n, err := d.CatchUp(ctx, "srv")
			if err != nil {
				t.Error(err)
			}
			total[i] = n
		}(i)
	}
	wg.Wait()
	sum := 0
	for _, n := range total {
		sum += n
	}
	if sum != 1 {
		t.Fatalf("events written across runs = %d, want 1", sum)
	}
	if got := storedTypes(t, st); len(got) != 1 {
		t.Fatalf("events = %v", got)
	}
}

// TestCatchUpAfterThePreviousSnapshotWasPruned exercises CatchUp's
// defensive new-baseline path (for a concurrent deriver or a manual
// delete): with the last diffed snapshot no longer stored, the next one
// becomes a new baseline whose tombstones are matched against the stored
// ones rather than reported again. PruneSnapshots itself never removes a
// server's world_diff snapshot (or anything newer), so that path can't be
// reached by pruning; this test removes the row directly, through a second
// database/sql connection to the same on-disk file.
func TestCatchUpAfterThePreviousSnapshotWasPruned(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "farsight.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	d := NewDeriver(st, nil)
	s1 := world("chunked:1", t0)
	put(t, st, s1)
	if _, err := d.CatchUp(ctx, "srv"); err != nil {
		t.Fatal(err)
	}
	s2 := world("chunked:2", t0.Add(20*time.Minute))
	s2.Markers = append(s2.Markers, extract.Marker{ID: "tombstone-2", Kind: "tombstone", Owner: "Ulf", X: 5, Z: 5})
	put(t, st, s2)

	// chunked:1 (the world_diff baseline) is gone before chunked:2 is
	// diffed: a second connection to the same file, standing in for
	// whatever deleted it.
	raw, err := sql.Open("sqlite", "file:"+path+"?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(ctx, `DELETE FROM snapshots WHERE server_id = ? AND save_id = ?`, "srv", "chunked:1"); err != nil {
		t.Fatal(err)
	}

	n, err := d.CatchUp(ctx, "srv")
	if err != nil || n != 0 {
		t.Fatalf("wrote %d (err %v), want 0: a new baseline", n, err)
	}
	bjorn, _ := st.Tombstones(ctx, "srv", "Bjorn")
	ulf, _ := st.Tombstones(ctx, "srv", "Ulf")
	if len(bjorn) != 1 || len(ulf) != 1 {
		t.Fatalf("tombstones: Bjorn %+v (want the one stored), Ulf %+v (want 1)", bjorn, ulf)
	}
}

// Fix round 2 (serve.go shutdown): CatchUp checks ctx between snapshots,
// so a long backfill stops promptly once ctx is cancelled instead of
// running to completion. AfterApply (test-only) paces the backfill
// deterministically, without a sleep.
func TestCatchUpChecksContextBetweenSnapshots(t *testing.T) {
	st := newStore(t)
	d := NewDeriver(st, nil)
	for i := 0; i < 20; i++ {
		put(t, st, world(fmt.Sprintf("chunked:%d", i), t0.Add(time.Duration(i)*time.Minute)))
	}

	cctx, cancel := context.WithCancel(context.Background())
	applied := make(chan struct{})
	release := make(chan struct{})
	n := 0
	d.AfterApply = func() {
		n++
		if n == 3 {
			close(applied)
			<-release
		}
	}

	done := make(chan struct{})
	var gotErr error
	go func() {
		_, gotErr = d.CatchUp(cctx, "srv")
		close(done)
	}()
	<-applied
	cancel()
	close(release)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CatchUp did not return after ctx was cancelled")
	}
	if !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", gotErr)
	}
	if n >= 20 {
		t.Fatalf("ran all %d snapshots despite cancellation", n)
	}
}

// Fix round 2 (serve.go shutdown): a CatchUp waiting for another one's
// per-server lock returns as soon as its own ctx is cancelled, instead of
// waiting for the lock holder to finish.
func TestCatchUpLockWaitRespectsContext(t *testing.T) {
	st := newStore(t)
	d := NewDeriver(st, nil)
	put(t, st, world("chunked:1", t0))

	entered := make(chan struct{})
	release := make(chan struct{})
	d.AfterApply = func() {
		close(entered)
		<-release
	}
	go d.CatchUp(context.Background(), "srv") // holds the lock until release
	<-entered

	cctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled: must not wait for the lock at all
	done := make(chan struct{})
	var n int
	var gotErr error
	go func() {
		n, gotErr = d.CatchUp(cctx, "srv")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("CatchUp did not return promptly while the lock was held")
	}
	close(release)
	if n != 0 || !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("n=%d err=%v, want 0, context.Canceled", n, gotErr)
	}
}

// M5: a panic anywhere in the diff path must not take the process down.
// AfterApply (test-only) is the seam used to inject one. CatchUp recovers
// it, logs the server id and a stack trace, and returns an error instead
// of letting the panic propagate.
func TestCatchUpRecoversPanicAndLogsStack(t *testing.T) {
	st := newStore(t)
	put(t, st, world("chunked:1", t0))

	var logs bytes.Buffer
	d := NewDeriver(st, slog.New(slog.NewTextHandler(&logs, nil)))
	d.AfterApply = func() { panic("kaboom") }

	n, err := d.CatchUp(context.Background(), "srv")
	if err == nil {
		t.Fatal("want an error from the recovered panic, got nil")
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}

	out := logs.String()
	for _, want := range []string{"srv", "kaboom", "goroutine"} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %q:\n%s", want, out)
		}
	}
}

// The per-server lock must still be released after a recovered panic, or
// every later CatchUp for that server deadlocks.
func TestCatchUpReleasesLockAfterPanic(t *testing.T) {
	st := newStore(t)
	put(t, st, world("chunked:1", t0))

	d := NewDeriver(st, nil)
	d.AfterApply = func() { panic("kaboom") }
	if _, err := d.CatchUp(context.Background(), "srv"); err == nil {
		t.Fatal("want an error from the recovered panic, got nil")
	}

	d.AfterApply = nil
	done := make(chan struct{})
	go func() {
		d.CatchUp(context.Background(), "srv")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("CatchUp did not return: the per-server lock was not released after the panic")
	}
}

// M5: CatchUpAsync (ingest's background path) must recover a panic the
// same way CatchUp does. If it didn't, this test's process would crash
// instead of failing.
func TestCatchUpAsyncRecoversPanicAndLogsStack(t *testing.T) {
	st := newStore(t)
	put(t, st, world("chunked:1", t0))

	var logs bytes.Buffer
	d := NewDeriver(st, slog.New(slog.NewTextHandler(&logs, nil)))
	d.AfterApply = func() { panic("kaboom") }

	d.CatchUpAsync("srv")
	d.Idle() // would hang or crash the test binary if the panic weren't recovered

	out := logs.String()
	for _, want := range []string{"srv", "kaboom", "goroutine"} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %q:\n%s", want, out)
		}
	}
}

// A save from a different world, or one that went back in time (a
// restored backup), starts a new baseline instead of being diffed
// against the previous save.
func TestCatchUpStartsANewBaselineWhenTheWorldChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*extract.Snapshot)
	}{
		{"new seed", func(s *extract.Snapshot) { s.World.Seed = 99 }},
		{"new name", func(s *extract.Snapshot) { s.World.Name = "Other" }},
		{"time went back", func(s *extract.Snapshot) { s.World.NetTime = 100 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newStore(t)
			ctx := context.Background()
			d := NewDeriver(st, nil)

			s1 := world("chunked:1", t0)
			s1.World = extract.WorldInfo{Name: "Midgard", Seed: 7, NetTime: 5000}
			put(t, st, s1)
			if _, err := d.CatchUp(ctx, "srv"); err != nil {
				t.Fatal(err)
			}

			s2 := world("chunked:2", t0.Add(20*time.Minute))
			s2.World = s1.World
			tc.change(s2)
			s2.Markers = append(s2.Markers, extract.Marker{ID: "portal-9", Kind: "portal", Label: "new", X: 20, Z: 20, Owner: "Astrid"})
			s2.GlobalKeys = append(s2.GlobalKeys, "defeated_gdking")
			put(t, st, s2)

			if n, err := d.CatchUp(ctx, "srv"); err != nil || n != 0 {
				t.Fatalf("wrote %d (err %v), want 0: a new baseline; events %v", n, err, storedTypes(t, st))
			}
			if state, _, _ := st.WorldDiffState(ctx, nil, "srv"); state.SaveID != "chunked:2" {
				t.Fatalf("state = %+v, want advanced to chunked:2", state)
			}
		})
	}
}

// --- a snapshot that keeps failing is skipped after maxSnapshotFailures
// tries, so it can't hold up world events and snapshot pruning forever ---

// TestCatchUpSkipsASnapshotThatKeepsFailingToDecode: the next save after
// the skipped one is a new baseline, a restart doesn't trip over it, and
// pruning can drop it.
func TestCatchUpSkipsASnapshotThatKeepsFailingToDecode(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	var logs bytes.Buffer
	d := NewDeriver(st, slog.New(slog.NewTextHandler(&logs, nil)))

	put(t, st, world("chunked:1", t0))
	if _, err := d.CatchUp(ctx, "srv"); err != nil {
		t.Fatal(err)
	}
	broken := t0.Add(20 * time.Minute)
	if _, err := st.PutSnapshot(ctx, "srv", "chunked:2", broken, broken, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	s3 := world("chunked:3", t0.Add(40*time.Minute))
	s3.Markers = append(s3.Markers, extract.Marker{ID: "portal-9", Kind: "portal", Label: "new", X: 20, Z: 20, Owner: "Astrid"})
	put(t, st, s3)

	for i := 1; i < maxSnapshotFailures; i++ {
		if _, err := d.CatchUp(ctx, "srv"); err == nil {
			t.Fatalf("try %d: no error, want the decode failure", i)
		}
		if state, _, _ := st.WorldDiffState(ctx, nil, "srv"); state.SaveID != "chunked:1" {
			t.Fatalf("try %d: state = %+v, want still at chunked:1", i, state)
		}
	}
	n, err := d.CatchUp(ctx, "srv")
	if err != nil || n != 0 {
		t.Fatalf("try %d wrote %d (err %v), want 0: the save after the skipped one is a new baseline", maxSnapshotFailures, n, err)
	}
	if state, _, _ := st.WorldDiffState(ctx, nil, "srv"); state.SaveID != "chunked:3" {
		t.Fatalf("state = %+v, want advanced to chunked:3", state)
	}
	if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "chunked:2") {
		t.Fatalf("no error logged naming the skipped snapshot:\n%s", logs.String())
	}

	// Only the newest snapshot is still needed.
	if _, err := st.PruneSnapshots(ctx, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if keys, _ := st.SnapshotsAfter(ctx, "srv", store.SnapshotKey{}); len(keys) != 1 || keys[0].SaveID != "chunked:3" {
		t.Fatalf("snapshots after pruning = %+v, want only chunked:3", keys)
	}
}

// The skip is recorded in world_diff, so a restarted Deriver (its failure
// counts gone) doesn't load the skipped snapshot as the previous one.
func TestCatchUpAfterASkipSurvivesARestart(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	d := NewDeriver(st, nil)

	put(t, st, world("chunked:1", t0))
	broken := t0.Add(20 * time.Minute)
	if _, err := st.PutSnapshot(ctx, "srv", "chunked:2", broken, broken, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	for range maxSnapshotFailures {
		d.CatchUp(ctx, "srv")
	}
	if keys, _ := st.SnapshotsAfter(ctx, "srv", mustState(t, st)); len(keys) != 0 {
		t.Fatalf("still to diff = %+v, want none: chunked:2 was skipped", keys)
	}

	s3 := world("chunked:3", t0.Add(40*time.Minute))
	s3.Markers = append(s3.Markers, extract.Marker{ID: "portal-9", Kind: "portal", Label: "new", X: 20, Z: 20, Owner: "Astrid"})
	put(t, st, s3)
	if n, err := NewDeriver(st, nil).CatchUp(ctx, "srv"); err != nil || n != 0 {
		t.Fatalf("after a restart wrote %d (err %v), want 0: chunked:3 is a new baseline", n, err)
	}
}

func mustState(t *testing.T, st *store.Store) store.SnapshotKey {
	t.Helper()
	state, ok, err := st.WorldDiffState(context.Background(), nil, "srv")
	if err != nil || !ok {
		t.Fatalf("state ok=%v err=%v", ok, err)
	}
	return state
}

// A diff that panics counts as a failure of that snapshot; failures only
// count while they are consecutive tries of the same snapshot, and a
// success in between starts again.
func TestCatchUpCountsRecoveredPanicsAsFailures(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	d := NewDeriver(st, nil)

	put(t, st, world("chunked:1", t0))
	if _, err := d.CatchUp(ctx, "srv"); err != nil {
		t.Fatal(err)
	}
	s2 := world("chunked:2", t0.Add(20*time.Minute))
	s2.Markers = append(s2.Markers, extract.Marker{ID: "portal-9", Kind: "portal", Label: "new", X: 20, Z: 20, Owner: "Astrid"})
	put(t, st, s2)

	d.beforeApply = func(saveID string) {
		if saveID == "chunked:2" {
			panic("kaboom")
		}
	}
	for i := 1; i < maxSnapshotFailures; i++ {
		if _, err := d.CatchUp(ctx, "srv"); err == nil || !strings.Contains(err.Error(), "kaboom") {
			t.Fatalf("try %d: err = %v, want the recovered panic", i, err)
		}
	}
	if n, err := d.CatchUp(ctx, "srv"); err != nil || n != 0 {
		t.Fatalf("try %d wrote %d (err %v), want chunked:2 skipped", maxSnapshotFailures, n, err)
	}

	// chunked:3 fails twice, then works: not skipped, diffed as usual.
	s3 := world("chunked:3", t0.Add(40*time.Minute))
	put(t, st, s3)
	s4 := world("chunked:4", t0.Add(60*time.Minute))
	s4.Markers = append(s4.Markers, extract.Marker{ID: "portal-10", Kind: "portal", Label: "newer", X: 30, Z: 30, Owner: "Astrid"})
	put(t, st, s4)
	fails := 0
	d.beforeApply = func(saveID string) {
		if saveID == "chunked:3" && fails < maxSnapshotFailures-1 {
			fails++
			panic("flaky")
		}
	}
	for range maxSnapshotFailures - 1 {
		if _, err := d.CatchUp(ctx, "srv"); err == nil {
			t.Fatal("no error, want the flaky panic")
		}
	}
	if n, err := d.CatchUp(ctx, "srv"); err != nil || n == 0 {
		t.Fatalf("wrote %d (err %v), want chunked:4's portal diffed against chunked:3", n, err)
	}
}
