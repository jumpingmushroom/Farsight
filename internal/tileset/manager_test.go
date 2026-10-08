package tileset

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/tiles"
	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

// writeComplete drops the marker tiles.Complete accepts (see
// internal/tiles/tiles.go: filename "complete", content
// strconv.Itoa(RenderVersion)) so a fake RenderFunc can mark a directory
// done without actually rendering anything.
func writeComplete(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "complete"), []byte(strconv.Itoa(tiles.RenderVersion)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// waitFor polls cond until it's true or the deadline passes, failing the
// test otherwise. Used instead of long sleeps to sequence goroutines.
func waitFor(t *testing.T, deadline time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !cond() {
		t.Fatal("condition not met within deadline")
	}
}

func TestEnsureLifecycle(t *testing.T) {
	root := t.TempDir()
	started := make(chan struct{})
	proceed := make(chan struct{})
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		close(started)
		progress(1, 4)
		<-proceed
		progress(4, 4)
		writeComplete(t, dir)
		return nil
	}
	m := NewManager(root, render, nil)

	st := m.Ensure(1, 0)
	if st.State != StateQueued {
		t.Fatalf("state = %v, want queued", st.State)
	}
	wantKey := m.Key(1, 0)
	if st.Key != wantKey {
		t.Fatalf("key = %q, want %q", st.Key, wantKey)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- m.Run(ctx) }()

	<-started
	waitFor(t, 2*time.Second, func() bool {
		s := m.Status(1, 0)
		return s.State == StateRendering && s.Done == 1 && s.Total == 4
	})

	close(proceed)

	waitFor(t, 2*time.Second, func() bool {
		return m.Status(1, 0).State == StateComplete
	})

	cancel()
	<-runErr
}

func TestEnsureIdempotent(t *testing.T) {
	root := t.TempDir()
	var calls int32
	proceed := make(chan struct{})
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		atomic.AddInt32(&calls, 1)
		<-proceed
		writeComplete(t, dir)
		return nil
	}
	m := NewManager(root, render, nil)

	st1 := m.Ensure(2, 0)
	st2 := m.Ensure(2, 0)
	if st1.State != StateQueued || st2.State != StateQueued {
		t.Fatalf("states = %v, %v, want queued, queued", st1.State, st2.State)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	close(proceed)
	waitFor(t, 2*time.Second, func() bool { return m.Status(2, 0).State == StateComplete })

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("render called %d times, want 1", got)
	}
}

// TestEnsureConcurrentSameKeyRendersOnce is the concurrent counterpart to
// TestEnsureIdempotent: 32 goroutines call Ensure for the same fresh key
// at once, released together off a closed channel so they race through
// Ensure's "check map, stat disk, check map again" path concurrently.
// Exactly one of them may win the enqueue; the fake render's call count
// must be exactly 1 once the key finishes rendering.
func TestEnsureConcurrentSameKeyRendersOnce(t *testing.T) {
	root := t.TempDir()
	var calls int32
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		atomic.AddInt32(&calls, 1)
		writeComplete(t, dir)
		return nil
	}
	m := NewManager(root, render, nil)

	const n = 32
	start := make(chan struct{})
	var wg sync.WaitGroup
	statuses := make([]Status, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			statuses[i] = m.Ensure(42, 0)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, st := range statuses {
		switch st.State {
		case StateQueued, StateRendering, StateComplete:
		default:
			t.Fatalf("goroutine %d: state = %v, want queued/rendering/complete", i, st.State)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	waitFor(t, 2*time.Second, func() bool { return m.Status(42, 0).State == StateComplete })

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("render called %d times, want 1 (double-enqueue race)", got)
	}

	m.mu.Lock()
	qlen := len(m.queue)
	m.mu.Unlock()
	if qlen != 0 {
		t.Fatalf("queue length = %d after completion, want 0", qlen)
	}
}

func TestEnsureRefusedOutOfRangeGen(t *testing.T) {
	root := t.TempDir()
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		t.Fatal("render should not be called for a refused gen")
		return nil
	}
	m := NewManager(root, render, nil)

	st := m.Ensure(1, worldgen.MaxGenVersion+1)
	if st.State != StateRefused {
		t.Fatalf("state = %v, want refused", st.State)
	}
	if st.Key != "" {
		t.Fatalf("key = %q, want empty for refused", st.Key)
	}

	st = m.Status(1, worldgen.MaxGenVersion+1)
	if st.State != StateRefused {
		t.Fatalf("status state = %v, want refused", st.State)
	}

	st = m.Ensure(1, -1)
	if st.State != StateRefused {
		t.Fatalf("negative gen state = %v, want refused", st.State)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("root has entries, want none (refused must never touch disk): %v", entries)
	}
}

func TestRunSequentialNeverConcurrent(t *testing.T) {
	root := t.TempDir()
	var inFlight, maxInFlight int32
	release := make(chan struct{})
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxInFlight)
			if n <= old {
				break
			}
			if atomic.CompareAndSwapInt32(&maxInFlight, old, n) {
				break
			}
		}
		<-release
		atomic.AddInt32(&inFlight, -1)
		writeComplete(t, dir)
		return nil
	}
	m := NewManager(root, render, nil)

	m.Ensure(1, 0)
	m.Ensure(2, 0)
	m.Ensure(3, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	for i := 0; i < 3; i++ {
		waitFor(t, 2*time.Second, func() bool { return atomic.LoadInt32(&inFlight) == 1 })
		release <- struct{}{}
	}

	waitFor(t, 2*time.Second, func() bool {
		return m.Status(1, 0).State == StateComplete &&
			m.Status(2, 0).State == StateComplete &&
			m.Status(3, 0).State == StateComplete
	})

	if got := atomic.LoadInt32(&maxInFlight); got != 1 {
		t.Fatalf("max concurrent renders = %d, want 1", got)
	}
}

func TestEnsureRetryAfterFailure(t *testing.T) {
	root := t.TempDir()
	var calls int32
	wantErr := errors.New("boom")
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			// Leave stale progress behind before failing, so the test can
			// assert the retry doesn't keep showing it.
			progress(3, 4)
			return wantErr
		}
		writeComplete(t, dir)
		return nil
	}
	m := NewManager(root, render, nil)

	m.Ensure(9, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	waitFor(t, 2*time.Second, func() bool { return m.Status(9, 0).State == StateNone })
	if s := m.Status(9, 0); s.Done != 3 || s.Total != 4 {
		t.Fatalf("status after failure = %d/%d, want 3/4 (stale progress preserved on failure)", s.Done, s.Total)
	}

	st := m.Ensure(9, 0)
	if st.State != StateQueued {
		t.Fatalf("retry state = %v, want queued", st.State)
	}
	if st.Done != 0 || st.Total != 0 {
		t.Fatalf("retry progress = %d/%d, want 0/0 (stale progress from the failed attempt must not leak into the retry)", st.Done, st.Total)
	}
	if s := m.Status(9, 0); s.Done != 0 || s.Total != 0 {
		t.Fatalf("status right after re-enqueue = %d/%d, want 0/0", s.Done, s.Total)
	}

	waitFor(t, 2*time.Second, func() bool { return m.Status(9, 0).State == StateComplete })

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("render called %d times, want 2", got)
	}
}

func TestRunGCRemovesOldVersionKeepsCurrentAndUnrelated(t *testing.T) {
	root := t.TempDir()
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		writeComplete(t, dir)
		return nil
	}
	m := NewManager(root, render, nil)

	current := m.Dir(5, 2) // "{root}/5-2-r{tiles.RenderVersion}"
	stale := filepath.Join(root, "5-2-r0")
	// Different seed (50 vs 5): must never be swept by a prefix match on
	// "5-2-r" even though "50-2-r0" also starts with "5".
	unrelated := filepath.Join(root, "50-2-r0")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(unrelated, 0o755); err != nil {
		t.Fatal(err)
	}

	m.Ensure(5, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	waitFor(t, 2*time.Second, func() bool { return m.Status(5, 2).State == StateComplete })
	waitFor(t, 2*time.Second, func() bool {
		_, err := os.Stat(stale)
		return os.IsNotExist(err)
	})

	if _, err := os.Stat(current); err != nil {
		t.Fatalf("current dir removed by gc: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated dir removed by gc: %v", err)
	}
}

// After a render, the tile sets of worlds no server's latest save uses
// are removed, but never one in use, queued or rendering, nor anything in
// root that isn't a tile set.
func TestRunGCRemovesWorldsNoLongerInUse(t *testing.T) {
	root := t.TempDir()
	release := map[int32]chan struct{}{5: make(chan struct{}), 8: make(chan struct{})}
	rendering := make(chan int32, 2)
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		rendering <- seed
		<-release[seed]
		writeComplete(t, dir)
		return nil
	}
	m := NewManager(root, render, nil)
	m.InUse = func(context.Context) ([]World, error) {
		return []World{{Seed: 5, Gen: 2}, {Seed: 7, Gen: 0}}, nil
	}

	inUse, unused := m.Dir(7, 0), m.Dir(9, 0)
	writeComplete(t, inUse)
	writeComplete(t, unused)
	other := filepath.Join(root, "notes")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if st := m.Status(9, 0); st.State != StateComplete {
		t.Fatalf("unused set = %v, want complete before gc", st.State)
	}

	m.Ensure(5, 2)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }() // before TempDir's cleanup
	<-rendering
	// 8-0, not in use, is queued behind 5-2, with a partial render to resume.
	m.Ensure(8, 0)
	if err := os.MkdirAll(m.Dir(8, 0), 0o755); err != nil {
		t.Fatal(err)
	}
	close(release[5])

	if seed := <-rendering; seed != 8 {
		t.Fatalf("rendering %d, want 8", seed)
	}
	if _, err := os.Stat(unused); !os.IsNotExist(err) {
		t.Fatalf("unused set still there (err %v)", err)
	}
	if st := m.Status(9, 0); st.State != StateNone {
		t.Fatalf("unused set = %v after gc, want none", st.State)
	}
	for _, dir := range []string{m.Dir(5, 2), inUse, m.Dir(8, 0), other} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("%s removed by gc: %v", dir, err)
		}
	}
	close(release[8])
}

// If the worlds in use can't be listed, nothing is removed.
func TestRunGCKeepsEverythingWhenInUseFails(t *testing.T) {
	root := t.TempDir()
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		writeComplete(t, dir)
		return nil
	}
	m := NewManager(root, render, nil)
	m.InUse = func(context.Context) ([]World, error) { return nil, errors.New("store down") }
	unused := m.Dir(9, 0)
	writeComplete(t, unused)

	m.Ensure(5, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	waitFor(t, 2*time.Second, func() bool { return m.Status(5, 2).State == StateComplete })
	m.Ensure(6, 2) // a second render: the first's gc has certainly run by its end
	waitFor(t, 2*time.Second, func() bool { return m.Status(6, 2).State == StateComplete })
	if _, err := os.Stat(unused); err != nil {
		t.Fatalf("unused set removed although the worlds in use are unknown: %v", err)
	}
}

func TestRunReturnsOnCancelIdle(t *testing.T) {
	root := t.TempDir()
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		t.Fatal("render should not be called; nothing was queued")
		return nil
	}
	m := NewManager(root, render, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRunReturnsOnCancelInFlight(t *testing.T) {
	root := t.TempDir()
	started := make(chan struct{})
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	m := NewManager(root, render, nil)
	m.Ensure(7, 0)

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- m.Run(ctx) }()

	<-started
	cancel()

	select {
	case err := <-runErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancellation")
	}

	st := m.Status(7, 0)
	if st.State != StateNone {
		t.Fatalf("state after cancelled render = %v, want none", st.State)
	}
}

// TestDefaultRenderAlreadyComplete exercises DefaultRender's wiring
// (worldgen.NewChecked + tiles.Render) without running a real render: the
// target dir is pre-marked complete, so tiles.Render returns immediately.
// A full render is too slow to exercise in tests.
func TestDefaultRenderAlreadyComplete(t *testing.T) {
	root := t.TempDir()
	render := DefaultRender(0) // also exercises the workers<=0 default branch
	dir := tiles.SetDir(root, 1, 0)
	writeComplete(t, dir)

	var done, total int
	err := render(context.Background(), 1, 0, dir, func(d, tt int) { done, total = d, tt })
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	if done != total {
		t.Fatalf("done/total = %d/%d, want equal", done, total)
	}
}

func TestDefaultRenderRefusesOutOfRangeGen(t *testing.T) {
	root := t.TempDir()
	render := DefaultRender(1)
	dir := tiles.SetDir(root, 1, worldgen.MaxGenVersion+1)

	err := render(context.Background(), 1, worldgen.MaxGenVersion+1, dir, nil)
	if err == nil {
		t.Fatal("expected an error for an out-of-range genVersion")
	}
}
