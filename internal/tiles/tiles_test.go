package tiles

import (
	"context"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

// flat: meadows island of radius 3000 m at 60 m, ocean elsewhere.
type flat struct{}

func (flat) Biome(x, z float32) worldgen.Biome {
	if x*x+z*z < 3000*3000 {
		return worldgen.Meadows
	}
	return worldgen.Ocean
}
func (flat) Height(x, z float32) float32 {
	if x*x+z*z < 3000*3000 {
		return 60
	}
	return 10
}

func itoa(n int) string { return strconv.Itoa(n) }

func decodeAt(t *testing.T, dir string, z, x, y, px, py int) color.NRGBA {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, itoa(z), itoa(x), itoa(y)+".png"))
	if err != nil {
		t.Fatalf("open z%d/%d/%d: %v", z, x, y, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode z%d/%d/%d: %v", z, x, y, err)
	}
	return color.NRGBAModel.Convert(img.At(px, py)).(color.NRGBA)
}

func TestRenderPyramid(t *testing.T) {
	dir := t.TempDir()
	var last, total int
	err := Render(context.Background(), flat{}, Options{Dir: dir, Workers: 4}, func(d, n int) { last, total = d, n })
	if err != nil {
		t.Fatal(err)
	}
	if total != 1365 || last != 1365 || !Complete(dir) {
		t.Fatalf("progress %d/%d complete=%v", last, total, Complete(dir))
	}
	for z, n := 0, 1; z <= MaxZoom; z, n = z+1, n*2 {
		for _, xy := range [][2]int{{0, 0}, {n - 1, n - 1}, {n / 2, n / 2}} {
			f, err := os.Open(filepath.Join(dir, itoa(z), itoa(xy[0]), itoa(xy[1])+".png"))
			if err != nil {
				t.Fatalf("z%d tile %v missing: %v", z, xy, err)
			}
			if info, statErr := f.Stat(); statErr != nil {
				t.Fatalf("z%d tile %v stat: %v", z, xy, statErr)
			} else if perm := info.Mode().Perm(); perm != 0o644 {
				t.Fatalf("z%d tile %v mode %v, want 0644 (world-readable for serving)", z, xy, perm)
			}
			img, err := png.Decode(f)
			f.Close()
			if err != nil || img.Bounds().Dx() != TileSize || img.Bounds().Dy() != TileSize {
				t.Fatalf("z%d tile %v bad: %v", z, xy, err)
			}
		}
	}
	// Centre of the world is land (meadows green), a corner is outside the world (transparent).
	c := decodeAt(t, dir, 0, 0, 0, 128, 128)
	if c.A == 0 || c.G < c.B {
		t.Fatalf("world centre pixel %v should be opaque green land", c)
	}
	if corner := decodeAt(t, dir, 0, 0, 0, 0, 0); corner.A != 0 {
		t.Fatalf("outside-world pixel %v should be transparent", corner)
	}
}

func TestRenderResumesAndCancels(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	err := Render(ctx, flat{}, Options{Dir: dir, Workers: 1}, func(d, _ int) {
		n = d
		if d == 10 {
			cancel()
		}
	})
	if err == nil || Complete(dir) {
		t.Fatalf("cancelled render: err=%v complete=%v", err, Complete(dir))
	}
	var first int
	if err := Render(context.Background(), flat{}, Options{Dir: dir, Workers: 2}, func(d, _ int) {
		if first == 0 {
			first = d
		}
	}); err != nil || !Complete(dir) {
		t.Fatalf("resume: err=%v complete=%v", err, Complete(dir))
	}
	if first < n {
		t.Fatalf("resume restarted progress at %d; %d tiles already existed", first, n)
	}
}

// TestRenderProgressMonotonic guards against a data race where the "done"
// counter was incremented (via sync/atomic) outside the mutex that guards
// the progress callback: two workers could then deliver their counts to the
// caller out of order (e.g. 1009 reported before 1008). With enough workers
// contending on a 40-core box this reproduced reliably; run this test with
// -count=N to keep it honest against scheduler timing.
func TestRenderProgressMonotonic(t *testing.T) {
	dir := t.TempDir()
	prev := -1
	err := Render(context.Background(), flat{}, Options{Dir: dir, Workers: 16}, func(d, n int) {
		// Errorf, not Fatalf: this runs on a worker goroutine holding
		// Render's progress lock, and Fatalf's Goexit would deadlock it.
		if n != 1365 {
			t.Errorf("progress total %d, want 1365", n)
		}
		if d <= prev {
			t.Errorf("progress went backwards or stalled: %d after %d", d, prev)
		}
		prev = d
	})
	if err != nil {
		t.Fatal(err)
	}
	if prev != 1365 {
		t.Fatalf("final progress %d, want 1365", prev)
	}
}

// Final review: progress counts the whole z0..z5 pyramid (1024 + 256 + 64
// + 16 + 4 + 1 = 1365 tiles), so done == total is reported exactly once,
// as the last call, and only once the tile set is complete -- not when
// z5 finishes with the downsampling still to run.
func TestRenderProgressReachesTotalOnlyAtEnd(t *testing.T) {
	for _, resume := range []bool{false, true} {
		dir := t.TempDir()
		if resume {
			// Every z5 tile present, pyramid and marker missing: the
			// "crashed right before z0-z4" case.
			if err := Render(context.Background(), flat{}, Options{Dir: dir, Workers: 4}, nil); err != nil {
				t.Fatal(err)
			}
			os.Remove(filepath.Join(dir, completeMarker))
			for z := 0; z < MaxZoom; z++ {
				os.RemoveAll(filepath.Join(dir, itoa(z)))
			}
		}
		type call struct{ d, n int }
		var calls []call
		err := Render(context.Background(), flat{}, Options{Dir: dir, Workers: 4}, func(d, n int) {
			if d == n && !Complete(dir) {
				t.Errorf("resume=%v: progress %d/%d reported before the tile set is complete", resume, d, n)
			}
			calls = append(calls, call{d, n})
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) == 0 || calls[len(calls)-1] != (call{1365, 1365}) {
			t.Fatalf("resume=%v: last progress call %v, want {1365 1365}", resume, calls[len(calls)-1:])
		}
		for i, c := range calls[:len(calls)-1] {
			if c.d >= c.n {
				t.Fatalf("resume=%v: call %d of %d reported %d/%d before the end", resume, i, len(calls), c.d, c.n)
			}
		}
	}
}

// TestRenderResumesOverCorruptTile plants a non-PNG file at the z5 tile
// path a crash or truncated write might leave behind, and checks that
// Render treats it as missing (tileExists requires a decodable PNG) rather
// than failing or leaving it corrupt.
func TestRenderResumesOverCorruptTile(t *testing.T) {
	dir := t.TempDir()
	corruptDir := filepath.Join(dir, "5", "0")
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, "0.png"), []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Render(context.Background(), flat{}, Options{Dir: dir, Workers: 4}, func(int, int) {}); err != nil {
		t.Fatal(err)
	}
	if !Complete(dir) {
		t.Fatal("expected Complete(dir) true after resuming over a corrupt tile")
	}

	f, err := os.Open(filepath.Join(corruptDir, "0.png"))
	if err != nil {
		t.Fatalf("open re-rendered tile: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode re-rendered tile: %v", err)
	}
	if img.Bounds().Dx() != TileSize || img.Bounds().Dy() != TileSize {
		t.Fatalf("re-rendered tile bounds %v, want %dx%d", img.Bounds(), TileSize, TileSize)
	}
}

// TestRenderSweepsStaleTempFiles plants a leftover ".tmp-*.png" file, the
// pattern writeTilePNG's os.CreateTemp leaves behind when a process is
// killed mid-write, and checks that Render removes it at the start of the
// run rather than leaving it on disk forever.
func TestRenderSweepsStaleTempFiles(t *testing.T) {
	dir := t.TempDir()
	staleDir := filepath.Join(dir, "5", "0")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(staleDir, ".tmp-123456.png")
	if err := os.WriteFile(stale, []byte("partial write"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Render(context.Background(), flat{}, Options{Dir: dir, Workers: 4}, func(int, int) {}); err != nil {
		t.Fatal(err)
	}
	if !Complete(dir) {
		t.Fatal("expected Complete(dir) true after sweeping a stale temp file")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale temp file %s still present (stat err=%v), want it swept", stale, err)
	}
}

// TestCompleteRejectsDifferentRenderVersion checks that a marker left by a
// different RenderVersion (e.g. an older tile set on disk after the
// renderer's drawing logic changed) reads as not complete, so Render
// re-renders it rather than trusting stale pixels.
func TestCompleteRejectsDifferentRenderVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, completeMarker), []byte(strconv.Itoa(RenderVersion+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if Complete(dir) {
		t.Fatal("Complete: marker from RenderVersion+1 should read as not complete")
	}

	if err := os.WriteFile(filepath.Join(dir, completeMarker), []byte(strconv.Itoa(RenderVersion)), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Complete(dir) {
		t.Fatal("Complete: marker matching the current RenderVersion should read as complete")
	}
}

func TestSetDirFormat(t *testing.T) {
	got := SetDir("/tiles", 12345, 2)
	want := fmt.Sprintf("/tiles/12345-2-r%d", RenderVersion)
	if got != want {
		t.Fatalf("SetDir(%q, %d, %d) = %q, want %q", "/tiles", 12345, 2, got, want)
	}

	// A negative seed (StableHash's usual range) still produces one clean
	// path segment, not something filepath.Join or a slippy-map server
	// would choke on.
	if got := SetDir("/tiles", -4242, 0); got != fmt.Sprintf("/tiles/-4242-0-r%d", RenderVersion) {
		t.Fatalf("SetDir with a negative seed = %q", got)
	}
}
