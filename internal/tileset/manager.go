// Package tileset runs the background tile-render queue that backs the web
// API's world map endpoints: Ensure asks for a world's z0-z5 tile pyramid,
// Run drives a single renderer that works through the queue FIFO, and
// Status reports live progress so a handler can poll without blocking.
package tileset

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/jumpingmushroom/farsight/internal/tiles"
	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

// State is a tile set's lifecycle stage as reported by Status.
type State string

const (
	// StateNone means no complete tile set exists on disk and nothing is
	// queued or rendering for this key (either never asked for, or a
	// previous render failed and hasn't been retried yet).
	StateNone State = "none"
	// StateQueued means Ensure enqueued this key and it is waiting for the
	// single renderer to reach it.
	StateQueued State = "queued"
	// StateRendering means the renderer is actively working on this key.
	StateRendering State = "rendering"
	// StateComplete means tiles.Complete reports a fully rendered pyramid.
	StateComplete State = "complete"
	// StateRefused means the requested genVersion is out of range; the key
	// was never enqueued and disk was never touched.
	StateRefused State = "refused"
)

// Status is a snapshot of one tile set's render state. Done/Total are the
// last progress reported by RenderFunc; both are 0 until the first
// progress call, and after a process restart a complete tile set found on
// disk (rather than rendered by this Manager) reports 0/0 since no
// progress was ever observed. Key is set for every State except
// StateRefused, which is stateless and carries no key.
type Status struct {
	State State
	Done  int
	Total int
	Key   string
}

// RenderFunc renders one world's tile set into dir, reporting progress as
// it goes. It must honour ctx cancellation.
type RenderFunc func(ctx context.Context, seed, gen int32, dir string, progress func(done, total int)) error

// queueItem is a pending Ensure request waiting for Run's single worker.
type queueItem struct {
	seed, gen int32
}

// Manager owns a queue of pending tile-set renders, worked through
// one-at-a-time by Run, and a status map that Ensure/Status read and
// update. All of Manager's exported methods are safe for concurrent use
// (e.g. from HTTP handlers).
type Manager struct {
	root   string
	render RenderFunc
	log    *slog.Logger

	mu       sync.Mutex
	statuses map[string]Status
	queue    []queueItem
	// wake is signalled (non-blocking, buffered 1) whenever the queue gains
	// an item, so Run's dequeue-then-wait loop never misses an Ensure that
	// arrives while it's about to block.
	wake chan struct{}
}

// NewManager creates a Manager rooted at root (see Dir). A nil log
// discards all logging.
func NewManager(root string, render RenderFunc, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Manager{
		root:     root,
		render:   render,
		log:      log,
		statuses: make(map[string]Status),
		wake:     make(chan struct{}, 1),
	}
}

// DefaultRender returns the production RenderFunc: worldgen.NewChecked
// followed by tiles.Render over its TerrainView. workers <= 0 leaves one
// CPU free for the rest of the process (max(1, GOMAXPROCS(0)-1)) instead of
// tiles.Render's own default of using every CPU, since this render runs
// alongside a live web server and game server rather than as a standalone
// CLI.
func DefaultRender(workers int) RenderFunc {
	if workers <= 0 {
		workers = max(1, runtime.GOMAXPROCS(0)-1)
	}
	return func(ctx context.Context, seed, gen int32, dir string, progress func(done, total int)) error {
		g, err := worldgen.NewChecked(seed, gen)
		if err != nil {
			return err
		}
		return tiles.Render(ctx, g.TerrainView(), tiles.Options{Dir: dir, Workers: workers}, progress)
	}
}

// Dir returns the tile-set directory for (seed, gen) under root.
func (m *Manager) Dir(seed, gen int32) string {
	return tiles.SetDir(m.root, seed, gen)
}

// Key returns the tile-set directory's base name, the identifier used in
// Status and logs.
func (m *Manager) Key(seed, gen int32) string {
	return filepath.Base(m.Dir(seed, gen))
}

// Ensure asks for (seed, gen)'s tile set to exist, enqueueing a render if
// one isn't already complete, queued or rendering. It is idempotent: two
// concurrent Ensure calls for the same key result in exactly one render.
// A genVersion outside [0, worldgen.MaxGenVersion] is refused immediately,
// without enqueueing or touching disk.
func (m *Manager) Ensure(seed, gen int32) Status {
	if Refused(gen) {
		return Status{State: StateRefused}
	}
	key := m.Key(seed, gen)

	m.mu.Lock()
	if st, ok := m.statuses[key]; ok && (st.State == StateComplete || st.State == StateQueued || st.State == StateRendering) {
		m.mu.Unlock()
		return st
	}
	m.mu.Unlock()

	// Not known to be in flight or complete in memory: it may still be
	// complete on disk (e.g. rendered by a previous process before this
	// Manager started, or left over from before a restart). This cache
	// assumes only this Manager ever deletes tile-set directories, so once
	// we've observed one complete on disk it can't un-complete under us.
	if tiles.Complete(m.Dir(seed, gen)) {
		return m.setState(key, StateComplete)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Re-check under the lock: another goroutine's Ensure may have enqueued
	// (or even finished) this key while we were doing the disk I/O above,
	// which released the lock. Without this second check, two concurrent
	// Ensure calls for the same never-seen key can both fall through and
	// both append to the queue, rendering the key twice.
	if st, ok := m.statuses[key]; ok && (st.State == StateComplete || st.State == StateQueued || st.State == StateRendering) {
		return st
	}
	// Done/Total reset to 0 here (a fresh Status, not built from any prior
	// entry) so a retried key doesn't keep showing a previous attempt's
	// stale progress; a completed render keeps its last known totals
	// instead (setState, used for the rendering/complete/none transitions,
	// preserves whatever was already in the map).
	st := Status{State: StateQueued, Key: key}
	m.statuses[key] = st
	m.queue = append(m.queue, queueItem{seed: seed, gen: gen})
	m.signal()
	return st
}

// Status reports (seed, gen)'s current render state without enqueueing
// anything. A key that was never Ensured but is complete on disk reports
// StateComplete (Done/Total 0, since no progress was observed). A key that
// is neither known nor complete on disk reports StateNone.
func (m *Manager) Status(seed, gen int32) Status {
	if Refused(gen) {
		return Status{State: StateRefused}
	}
	key := m.Key(seed, gen)

	m.mu.Lock()
	st, ok := m.statuses[key]
	m.mu.Unlock()
	if ok && st.State != StateNone {
		return st
	}

	if tiles.Complete(m.Dir(seed, gen)) {
		return m.setState(key, StateComplete)
	}

	st.State = StateNone
	st.Key = key
	return st
}

// Run drives the single background renderer: pop the next queued key,
// render it, garbage-collect stale sibling directories on success, repeat.
// It returns ctx.Err() once ctx is cancelled, whether that happens while
// idle (blocked waiting for work) or with a render in flight -- a render
// cancelled this way goes back to StateNone (logged at Info, not Error) so
// a later Ensure retries it; tiles.Render's own resumability makes that
// retry cheap.
func (m *Manager) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		item, ok := m.dequeue()
		if !ok {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-m.wake:
				continue
			}
		}

		key := m.Key(item.seed, item.gen)
		m.setState(key, StateRendering)

		dir := m.Dir(item.seed, item.gen)
		progress := func(done, total int) { m.setProgress(key, done, total) }

		err := m.render(ctx, item.seed, item.gen, dir, progress)
		if err != nil {
			m.setState(key, StateNone)
			if ctx.Err() != nil {
				m.log.Info("tileset: render cancelled", "key", key, "err", err)
				return ctx.Err()
			}
			m.log.Error("tileset: render failed", "key", key, "err", err)
			continue
		}

		m.setState(key, StateComplete)
		m.log.Info("tileset: render complete", "key", key)
		m.gc(item.seed, item.gen, dir)
	}
}

// Refused reports whether gen is outside the range worldgen can generate.
// Exported so other handlers that key on (seed, gen) without going through
// Ensure (e.g. the biome grid endpoint) can refuse the same way, instead of
// duplicating the bound.
func Refused(gen int32) bool {
	return gen < 0 || gen > worldgen.MaxGenVersion
}

// dequeue pops the next queued item FIFO, or reports ok=false if the queue
// is empty.
func (m *Manager) dequeue() (queueItem, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.queue) == 0 {
		return queueItem{}, false
	}
	item := m.queue[0]
	m.queue = m.queue[1:]
	return item, true
}

// signal wakes Run if it's blocked waiting for work. Non-blocking: if a
// wake is already pending, this is a no-op (Run will still see the queue
// is non-empty next time it dequeues).
func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// setState updates key's State (and Key), leaving Done/Total as they were
// so a completed render still reports its last known totals and a failed
// one keeps whatever progress it made before erroring.
func (m *Manager) setState(key string, state State) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.statuses[key]
	st.State = state
	st.Key = key
	m.statuses[key] = st
	return st
}

// setProgress updates key's Done/Total; called from RenderFunc's progress
// callback, so it must stay cheap (a map write under a mutex).
func (m *Manager) setProgress(key string, done, total int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.statuses[key]
	st.Done, st.Total = done, total
	m.statuses[key] = st
}

// gc removes sibling directories under root left over from an earlier
// tiles.RenderVersion of the same (seed, gen): any directory whose name
// starts with "{seed}-{gen}-r" and isn't currentDir's own name. The prefix
// includes the "-" after gen (and after seed) so seed 12 can never match a
// sibling belonging to seed 123, or gen 2 a sibling belonging to gen 20.
// GC failures are logged at Warn and never fail the render that triggered
// them.
func (m *Manager) gc(seed, gen int32, currentDir string) {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		m.log.Warn("tileset: gc list failed", "err", err)
		return
	}
	prefix := fmt.Sprintf("%d-%d-r", seed, gen)
	current := filepath.Base(currentDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == current || !strings.HasPrefix(name, prefix) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(m.root, name)); err != nil {
			m.log.Warn("tileset: gc remove failed", "key", name, "err", err)
		}
	}
}
