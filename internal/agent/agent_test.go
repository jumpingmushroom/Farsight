package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/save"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
)

type sink struct {
	mu     sync.Mutex
	posts  []extract.Snapshot
	status int
}

func (s *sink) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/ingest/mv/snapshot" ||
			r.Header.Get("Authorization") != "Bearer sekrit" || r.Header.Get("Content-Encoding") != "gzip" {
			t.Errorf("bad request: %s %s %v", r.Method, r.URL.Path, r.Header)
		}
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		b, _ := io.ReadAll(zr)
		var snap extract.Snapshot
		if err := json.Unmarshal(b, &snap); err != nil {
			t.Error(err)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.posts = append(s.posts, snap)
		if s.status != 0 {
			w.WriteHeader(s.status)
		}
	}
}

func (s *sink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.posts)
}

func newAgent(t *testing.T, dir, url string) *Agent {
	return New(Config{WorldsDir: dir, WorldName: "W", ServerID: "mv", URL: url, Token: "sekrit"},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

var zdos = []savetest.ZDO{{Prefab: "portal_wood", Strings: map[string]string{"tag": "home"}}}

func TestChunkedPostsOnceImmediately(t *testing.T) {
	s := &sink{}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, dir, "W", 5, "seed", zdos, nil, nil, nil)
	a := newAgent(t, dir, srv.URL)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := a.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if s.count() != 1 || s.posts[0].SaveID != "chunked:5" || s.posts[0].ServerID != "mv" || len(s.posts[0].Markers) != 1 {
		t.Fatalf("posts = %+v", s.posts)
	}
	savetest.WriteChunkedWorld(t, dir, "W", 6, "seed", zdos, nil, nil, nil)
	if err := a.Tick(ctx); err != nil || s.count() != 2 || s.posts[1].SaveID != "chunked:6" {
		t.Fatalf("second save: err=%v posts=%d", err, s.count())
	}
}

func TestLegacyWaitsForStableSave(t *testing.T) {
	s := &sink{}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	dir := t.TempDir()
	savetest.WriteLegacyWorld(t, dir, "W", "seed", zdos, nil, nil, nil)
	a := newAgent(t, dir, srv.URL)
	ctx := context.Background()
	if err := a.Tick(ctx); err != nil || s.count() != 0 {
		t.Fatalf("first tick must only record a candidate: err=%v posts=%d", err, s.count())
	}
	if err := a.Tick(ctx); err != nil || s.count() != 1 {
		t.Fatalf("second tick must post: err=%v posts=%d", err, s.count())
	}
}

// TestFailedPostIsRetried covers the backoff-retry contract: the first
// failed POST returns an error; retrying before the backoff elapses is a
// silent no-op (no new POST attempt); once the backoff elapses, Tick
// retries the POST and, on success, it counts as a second attempt.
func TestFailedPostIsRetried(t *testing.T) {
	s := &sink{status: http.StatusInternalServerError}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, dir, "W", 1, "seed", zdos, nil, nil, nil)
	a := newAgent(t, dir, srv.URL)
	clock := time.Now()
	a.now = func() time.Time { return clock }
	ctx := context.Background()

	if err := a.Tick(ctx); err == nil {
		t.Fatal("want error on 500")
	}
	if s.count() != 1 {
		t.Fatalf("posts after first failure = %d, want 1", s.count())
	}

	// Retrying before the backoff elapses must not re-POST.
	if err := a.Tick(ctx); err != nil || s.count() != 1 {
		t.Fatalf("immediate retry: err=%v posts=%d, want no new post before backoff elapses", err, s.count())
	}

	// Advance past the backoff (starts at Poll) and the retried POST succeeds.
	clock = clock.Add(a.cfg.Poll)
	s.status = 0
	if err := a.Tick(ctx); err != nil || s.count() != 2 {
		t.Fatalf("retry after backoff: err=%v posts=%d", err, s.count())
	}
}

// TestPostRetryDoesNotReReadSave proves the retried POST reuses the
// already-built snapshot: it removes every save file between the failed
// POST and the retry, so any re-read would fail.
func TestPostRetryDoesNotReReadSave(t *testing.T) {
	s := &sink{status: http.StatusInternalServerError}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, dir, "W", 1, "seed", zdos, nil, nil, nil)
	a := newAgent(t, dir, srv.URL)
	clock := time.Now()
	a.now = func() time.Time { return clock }
	ctx := context.Background()

	if err := a.Tick(ctx); err == nil {
		t.Fatal("want error on 500")
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "W", "*"))
	for _, m := range matches {
		if err := os.RemoveAll(m); err != nil {
			t.Fatal(err)
		}
	}

	clock = clock.Add(a.cfg.Poll)
	s.status = 0
	if err := a.Tick(ctx); err != nil || s.count() != 2 {
		t.Fatalf("retry after files vanished: err=%v posts=%d, want success without re-reading", err, s.count())
	}
}

// TestCorruptSaveParsedOnceThenSkipped proves a save that fails to parse
// with an error other than ErrSaveChanged/ErrSaveInProgress is recorded and
// skipped on later ticks (only one read attempt, one error-level log
// message) until LatestSave reports a different id.
func TestCorruptSaveParsedOnceThenSkipped(t *testing.T) {
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, dir, "W", 1, "seed", zdos, nil, nil, nil)
	// Truncate the chunk file so decoding fails with a real parse error
	// (not ErrSaveChanged/ErrSaveInProgress).
	chunkPath := filepath.Join(dir, "W", "1e_1e__1_1.chunk")
	b, err := os.ReadFile(chunkPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chunkPath, b[:len(b)-2], 0o644); err != nil {
		t.Fatal(err)
	}

	reads := 0
	orig := saveRead
	saveRead = func(worldsDir, worldName string, fn func(*save.ZDO)) (*save.World, error) {
		reads++
		return orig(worldsDir, worldName, fn)
	}
	defer func() { saveRead = orig }()

	a := newAgent(t, dir, "http://127.0.0.1:1")
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := a.Tick(ctx); err != nil {
			t.Fatalf("tick %d: corrupt save must be logged and skipped, not returned as an error: %v", i, err)
		}
	}
	if reads != 1 {
		t.Fatalf("reads = %d, want exactly 1 (corrupt save skipped after first failure)", reads)
	}

	// A new save id must be tried again (the POST itself fails against the
	// unreachable URL, which is irrelevant here: only the read is checked).
	savetest.WriteChunkedWorld(t, dir, "W", 2, "seed", zdos, nil, nil, nil)
	_ = a.Tick(ctx)
	if reads != 2 {
		t.Fatalf("reads after new save id = %d, want 2", reads)
	}
}

func TestNoSaveIsQuiet(t *testing.T) {
	a := newAgent(t, t.TempDir(), "http://127.0.0.1:1")
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("missing save must not error: %v", err)
	}
}

// TestTickRecoversFromPanic verifies Tick turns a panic during the read
// step into a returned error instead of crashing the agent process.
func TestTickRecoversFromPanic(t *testing.T) {
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, dir, "W", 1, "seed", zdos, nil, nil, nil)
	a := newAgent(t, dir, "http://127.0.0.1:1")

	orig := saveRead
	saveRead = func(worldsDir, worldName string, fn func(*save.ZDO)) (*save.World, error) {
		panic("boom")
	}
	defer func() { saveRead = orig }()

	if err := a.Tick(context.Background()); err == nil {
		t.Fatal("want an error, not a panic, when the read step panics")
	}
}

// TestPendingRetrySurfacesLatestSaveError covers the Plan 1 carry-over bug:
// while a snapshot POST is pending, an unexpected LatestSave error (neither
// ErrNoSave nor ErrSaveInProgress) must be logged at error level, and the
// pending POST retry must still be attempted. It also proves the same
// distinct error string is only logged once, to avoid log spam.
func TestPendingRetrySurfacesLatestSaveError(t *testing.T) {
	s := &sink{status: http.StatusInternalServerError}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, dir, "W", 1, "seed", zdos, nil, nil, nil)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	a := New(Config{WorldsDir: dir, WorldName: "W", ServerID: "mv", URL: srv.URL, Token: "sekrit"}, logger)
	clock := time.Now()
	a.now = func() time.Time { return clock }
	ctx := context.Background()

	// Build the pending snapshot; the POST fails (500), leaving it pending.
	if err := a.Tick(ctx); err == nil {
		t.Fatal("want error on 500")
	}
	if s.count() != 1 {
		t.Fatalf("posts after first failure = %d, want 1", s.count())
	}

	orig := saveLatest
	defer func() { saveLatest = orig }()
	saveLatest = func(worldsDir, worldName string) (string, save.Format, error) {
		return "", "", errors.New("boom: worlds dir unreadable")
	}

	// Two more ticks, past the backoff each time, with the same LatestSave
	// error and the POST still failing: the retry must still be attempted
	// each time, but the error should only be logged once.
	clock = clock.Add(a.cfg.Poll)
	if err := a.Tick(ctx); err == nil {
		t.Fatal("want error: pending POST retry still fails with 500")
	}
	if s.count() != 2 {
		t.Fatalf("posts after second tick = %d, want 2 (pending retry must still happen)", s.count())
	}

	clock = clock.Add(a.backoff)
	if err := a.Tick(ctx); err == nil {
		t.Fatal("want error: pending POST retry still fails with 500")
	}
	if s.count() != 3 {
		t.Fatalf("posts after third tick = %d, want 3 (pending retry must still happen)", s.count())
	}

	logged := strings.Count(buf.String(), "boom: worlds dir unreadable")
	if logged != 1 {
		t.Fatalf("error logged %d times, want exactly 1: log=%s", logged, buf.String())
	}
	if !strings.Contains(buf.String(), "level=ERROR") {
		t.Fatalf("expected an ERROR level log line, got: %s", buf.String())
	}

	// Now let the POST succeed: the pending retry clears it despite the
	// still-failing LatestSave.
	clock = clock.Add(a.backoff)
	s.status = 0
	if err := a.Tick(ctx); err != nil {
		t.Fatalf("pending retry should succeed once the POST does: %v", err)
	}
	if s.count() != 4 {
		t.Fatalf("posts after success = %d, want 4", s.count())
	}

	// clearPending must also reset lastLoggedErr, so a recurring failure
	// is re-logged after the pending episode ends (Task 5 ruling).
	if a.lastLoggedErr != "" {
		t.Fatalf("lastLoggedErr = %q, want empty after clearPending", a.lastLoggedErr)
	}

	// Start a second, distinct pending POST episode (a new save version)
	// and drive it into the same "pending retry with a LatestSave error"
	// situation, with the exact same error message as before. It must be
	// logged again, proving the dedup state doesn't leak across episodes.
	savetest.WriteChunkedWorld(t, dir, "W", 2, "seed", zdos, nil, nil, nil)
	saveLatest = orig
	s.status = http.StatusInternalServerError
	clock = clock.Add(a.cfg.Poll)
	if err := a.Tick(ctx); err == nil {
		t.Fatal("want error: new pending POST fails with 500")
	}
	if s.count() != 5 {
		t.Fatalf("posts after new pending POST = %d, want 5", s.count())
	}

	saveLatest = func(worldsDir, worldName string) (string, save.Format, error) {
		return "", "", errors.New("boom: worlds dir unreadable")
	}
	clock = clock.Add(a.cfg.Poll)
	if err := a.Tick(ctx); err == nil {
		t.Fatal("want error: pending POST retry still fails with 500")
	}
	if s.count() != 6 {
		t.Fatalf("posts after tick = %d, want 6", s.count())
	}
	if logged := strings.Count(buf.String(), "boom: worlds dir unreadable"); logged != 2 {
		t.Fatalf("error logged %d times across two pending episodes, want 2: log=%s", logged, buf.String())
	}
}

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

// TestRunExitsOnContextCancel verifies the poll loop stops promptly when
// its context is cancelled, returning ctx.Err().
func TestRunExitsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	a := newAgent(t, dir, "http://127.0.0.1:1")
	a.cfg.Poll = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after ctx cancel")
	}
}
