package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"image/png"
	"io"
	"io/fs"
	"math"
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
	"github.com/jumpingmushroom/farsight/internal/logwatch"
	"github.com/jumpingmushroom/farsight/internal/tiles"
)

const (
	fixtureSnapshot = "../../web/tests/fixtures/snapshot.json"
	fixtureEvents   = "../../web/tests/fixtures/events.json"
)

func TestWriteFakeTiles(t *testing.T) {
	data := t.TempDir()
	dir, err := writeFakeTiles(data, 12345, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := tiles.SetDir(filepath.Join(data, "tiles"), 12345, 2); dir != want {
		t.Fatalf("dir = %s, want %s", dir, want)
	}
	if !tiles.Complete(dir) {
		t.Fatal("tiles.Complete = false")
	}
	n := 0
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".png") {
			return err
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1365 {
		t.Fatalf("wrote %d PNGs, want 1365", n)
	}
	for _, p := range []string{"0/0/0.png", "5/31/31.png", "3/0/7.png"} {
		f, err := os.Open(filepath.Join(dir, p))
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if b := img.Bounds(); b.Dx() != tiles.TileSize || b.Dy() != tiles.TileSize {
			t.Fatalf("%s: size %v", p, b)
		}
	}
}

func TestShiftTimes(t *testing.T) {
	base := time.Date(2026, 9, 29, 20, 0, 0, 0, time.UTC)
	since := base.Add(-40 * time.Minute)
	evs := []logwatch.Event{
		{ID: "a", Type: "server_boot", At: base.Add(-3 * time.Hour)},
		{ID: "b", Type: "player_leave", At: base.Add(-10 * time.Minute), Since: &since},
		{ID: "c", Type: "heartbeat", At: base},
		{ID: "d", Type: "world_saved", At: base.Add(-5 * time.Minute)},
	}
	snap := &extract.Snapshot{SavedAt: base.Add(-5 * time.Minute), ReadAt: base.Add(-4 * time.Minute)}
	now := time.Date(2026, 10, 3, 9, 30, 15, 0, time.UTC)

	d := shiftTimes(evs, snap, now)

	if want := now.Add(-time.Minute).Sub(base); d != want {
		t.Fatalf("delta = %v, want %v", d, want)
	}
	if got := evs[2].At; !got.Equal(now.Add(-time.Minute)) {
		t.Fatalf("newest event at %v, want now-1m", got)
	}
	if gap := evs[2].At.Sub(evs[0].At); gap != 3*time.Hour {
		t.Fatalf("gap = %v, want 3h", gap)
	}
	if got := evs[1].At.Sub(*evs[1].Since); got != 30*time.Minute {
		t.Fatalf("leave-since gap = %v, want 30m", got)
	}
	if !snap.SavedAt.Equal(evs[3].At) {
		t.Fatalf("snapshot savedAt %v, want the newest world_saved %v", snap.SavedAt, evs[3].At)
	}
	if got := snap.ReadAt.Sub(snap.SavedAt); got != time.Minute {
		t.Fatalf("readAt-savedAt = %v, want 1m", got)
	}
}

func TestShiftTimesWithoutEvents(t *testing.T) {
	saved := time.Date(2026, 9, 29, 19, 55, 0, 0, time.UTC)
	snap := &extract.Snapshot{SavedAt: saved, ReadAt: saved}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	shiftTimes(nil, snap, now)
	if want := now.Add(-6 * time.Minute); !snap.SavedAt.Equal(want) {
		t.Fatalf("savedAt = %v, want %v", snap.SavedAt, want)
	}
}

type received struct {
	mu    sync.Mutex
	auth  []string
	paths []string
	snap  extract.Snapshot
	evs   []logwatch.Event
}

func ingestStub(t *testing.T, rec *received) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.auth = append(rec.auth, r.Header.Get("Authorization"))
		rec.paths = append(rec.paths, r.URL.Path)
		if r.Header.Get("Content-Encoding") != "gzip" {
			t.Errorf("%s: not gzip", r.URL.Path)
		}
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		b, _ := io.ReadAll(zr)
		switch r.URL.Path {
		case "/ingest/demo/snapshot":
			if err := json.Unmarshal(b, &rec.snap); err != nil {
				t.Error(err)
			}
		case "/ingest/demo/events":
			var body struct {
				Events []logwatch.Event `json:"events"`
			}
			if err := json.Unmarshal(b, &body); err != nil {
				t.Error(err)
			}
			rec.evs = body.Events
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		io.WriteString(w, `{}`)
	}))
}

func TestRunPostsSnapshotAndEvents(t *testing.T) {
	rec := &received{}
	srv := ingestStub(t, rec)
	defer srv.Close()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	cfg := config{
		URL: srv.URL, Server: "demo", TokenEnv: "FARSIGHT_SEED_TOKEN",
		Snapshot: fixtureSnapshot, Events: fixtureEvents, Shift: true,
	}
	getenv := func(k string) string {
		if k == "FARSIGHT_SEED_TOKEN" {
			return "demo-token"
		}
		return ""
	}
	var out bytes.Buffer
	if err := run(context.Background(), cfg, getenv, now, &out); err != nil {
		t.Fatal(err)
	}
	if want := []string{"/ingest/demo/snapshot", "/ingest/demo/events"}; strings.Join(rec.paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %v, want %v", rec.paths, want)
	}
	for _, a := range rec.auth {
		if a != "Bearer demo-token" {
			t.Fatalf("auth = %q", a)
		}
	}
	if rec.snap.ServerID != "demo" || rec.snap.World.Seed != 12345 || rec.snap.World.GenVersion != 2 {
		t.Fatalf("snapshot = %+v", rec.snap.World)
	}
	if len(rec.evs) == 0 {
		t.Fatal("no events received")
	}
	newest := rec.evs[0].At
	for _, e := range rec.evs {
		if e.At.After(newest) {
			newest = e.At
		}
	}
	if !newest.Equal(now.Add(-time.Minute)) {
		t.Fatalf("newest event at %v, want now-1m", newest)
	}
}

func TestRunExploredRasterisesTheFixtureZones(t *testing.T) {
	rec := &received{}
	srv := ingestStub(t, rec)
	defer srv.Close()
	cfg := config{URL: srv.URL, Server: "demo", TokenEnv: "T", Snapshot: fixtureSnapshot, Explored: true}
	getenv := func(string) string { return "demo-token" }
	if err := run(context.Background(), cfg, getenv, time.Now(), io.Discard); err != nil {
		t.Fatal(err)
	}
	if rec.snap.Explored == nil || rec.snap.Explored.Source != explored.SourceZones {
		t.Fatalf("explored = %+v, want a zones mask", rec.snap.Explored)
	}
	m, err := explored.Decode(*rec.snap.Explored)
	if err != nil {
		t.Fatal(err)
	}
	// Every fixture marker is explored and the one outside crypt is not
	// (TestFixtures checks the same on zones).
	for _, mk := range rec.snap.Markers {
		if !m.At(float64(mk.X), float64(mk.Z)) {
			t.Errorf("marker %s not explored", mk.ID)
		}
	}
	for _, l := range rec.snap.Locations {
		if got, want := m.At(float64(l.X), float64(l.Z)), l.ID != "loc-311"; got != want {
			t.Errorf("location %s explored = %v, want %v", l.ID, got, want)
		}
	}

	// Without -explored the snapshot goes as written: no mask.
	rec2 := &received{}
	srv2 := ingestStub(t, rec2)
	defer srv2.Close()
	cfg.URL, cfg.Explored = srv2.URL, false
	if err := run(context.Background(), cfg, getenv, time.Now(), io.Discard); err != nil {
		t.Fatal(err)
	}
	if rec2.snap.Explored != nil {
		t.Fatal("explored sent without -explored")
	}
}

func TestRunNeedsToken(t *testing.T) {
	cfg := config{URL: "http://127.0.0.1:1", Server: "demo", TokenEnv: "FARSIGHT_SEED_TOKEN", Events: fixtureEvents}
	err := run(context.Background(), cfg, func(string) string { return "" }, time.Now(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "FARSIGHT_SEED_TOKEN") {
		t.Fatalf("err = %v, want a missing-token error naming the env var", err)
	}
}

func TestRunFakeTilesOnly(t *testing.T) {
	data := t.TempDir()
	cfg := config{Snapshot: fixtureSnapshot, FakeTiles: data}
	if err := run(context.Background(), cfg, func(string) string { return "" }, time.Now(), io.Discard); err != nil {
		t.Fatal(err)
	}
	if !tiles.Complete(tiles.SetDir(filepath.Join(data, "tiles"), 12345, 2)) {
		t.Fatal("fake tile set not complete")
	}
}

func TestRunRejectsNothingToDo(t *testing.T) {
	if err := run(context.Background(), config{}, func(string) string { return "" }, time.Now(), io.Discard); err == nil {
		t.Fatal("want a usage error")
	}
	if err := run(context.Background(), config{FakeTiles: t.TempDir()}, func(string) string { return "" }, time.Now(), io.Discard); err == nil {
		t.Fatal("want an error: tiles need -snapshot for the seed")
	}
}

func TestParseFlagsDefaults(t *testing.T) {
	cfg, err := parseFlags([]string{"-server", "demo", "-events", "e.json"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "http://127.0.0.1:8080" || cfg.TokenEnv != "FARSIGHT_SEED_TOKEN" || cfg.Shift {
		t.Fatalf("cfg = %+v", cfg)
	}
}

// TestFixtures checks the invariants the UI and e2e tests rely on.
func TestFixtures(t *testing.T) {
	snap, err := readSnapshot(fixtureSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	evs, err := readEvents(fixtureEvents)
	if err != nil {
		t.Fatal(err)
	}

	if snap.ServerID != "demo" || snap.World.Name != "Demo" || snap.World.Day != 214 || snap.SaveID == "" {
		t.Fatalf("world = %+v", snap.World)
	}
	defeated := 0
	for _, b := range snap.Bosses {
		if b.Defeated {
			defeated++
		}
	}
	if len(snap.Bosses) != 8 || defeated != 4 {
		t.Fatalf("bosses = %d, defeated %d", len(snap.Bosses), defeated)
	}

	explored := map[[2]int16]bool{}
	for _, z := range snap.ExploredZones {
		explored[z] = true
	}
	if len(explored) != len(snap.ExploredZones) {
		t.Fatal("duplicate explored zones")
	}
	zone := func(x, z float32) [2]int16 {
		f := func(v float32) int16 { return int16(math.Floor(float64(v+32) / 64)) }
		return [2]int16{f(x), f(z)}
	}
	outside := 0
	for _, l := range snap.Locations {
		if !explored[zone(l.X, l.Z)] {
			outside++
			if l.Type != "SunkenCrypt4" {
				t.Errorf("location %s outside explored zones", l.ID)
			}
		}
	}
	if outside != 1 {
		t.Fatalf("%d locations outside explored zones, want exactly the one crypt", outside)
	}
	counts := map[string]int{}
	for _, m := range snap.Markers {
		counts[m.Kind]++
		if !explored[zone(m.X, m.Z)] {
			t.Errorf("marker %s outside explored zones", m.ID)
		}
	}
	if counts["portal"] != 5 || counts["bed"] != 2 || counts["tombstone"] != 1 || counts["tame"] != 3 || counts["sign"] != 2 {
		t.Fatalf("marker counts = %v", counts)
	}

	ids := map[string]bool{}
	var newest, newestSave time.Time
	for _, e := range evs {
		if e.ID == "" || ids[e.ID] {
			t.Fatalf("event id %q empty or duplicated", e.ID)
		}
		ids[e.ID] = true
		if e.At.After(newest) {
			newest = e.At
		}
		if e.Type == logwatch.EvWorldSaved && e.At.After(newestSave) {
			newestSave = e.At
		}
	}
	if got := newest.Sub(newestSave); got != 5*time.Minute {
		t.Fatalf("newest world_saved is %v before the newest event, want 5m", got)
	}
	if !snap.SavedAt.Equal(newestSave) {
		t.Fatalf("snapshot savedAt %v, want the newest world_saved %v", snap.SavedAt, newestSave)
	}
}
