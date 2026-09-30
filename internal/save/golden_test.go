package save

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
)

func golden(t *testing.T, sub string) string {
	t.Helper()
	p := filepath.Join("..", "..", "testdata-golden", sub)
	if _, err := os.Stat(p); err != nil {
		t.Skip("golden data missing; run hack/pull-golden.sh")
	}
	return p
}

func TestGoldenChunked(t *testing.T) {
	dir := golden(t, "chunked")
	unknown := map[int32]bool{}
	portals := map[string]int{}
	w, err := Read(dir, "MuleVikings", func(z *ZDO) {
		if _, ok := names.Lookup(z.Prefab); !ok {
			unknown[z.Prefab] = true
		}
		if names.Name(z.Prefab) == "portal_wood" {
			portals[z.Strings[names.StableHash("tag")]]++
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Format != FormatChunked || w.ZDOCount < 400000 || len(unknown) != 0 {
		t.Fatalf("format=%s zdos=%d unknown=%d", w.Format, w.ZDOCount, len(unknown))
	}
	// The seed name is not asserted literally: real seeds stay out of the
	// repo. The seed must still be the stable hash of the seed name.
	if len(w.Meta.SeedName) != 9 || w.Meta.Seed != names.StableHash(w.Meta.SeedName) || len(w.Zones) < 5000 || len(w.Locations) < 10000 {
		t.Fatalf("meta=%+v zones=%d locs=%d", w.Meta, len(w.Zones), len(w.Locations))
	}
	if len(portals) < 8 {
		t.Fatalf("portal tags = %v", portals)
	}
}

func TestGoldenLegacy(t *testing.T) {
	dir := golden(t, "legacy")
	unknown := map[int32]bool{}
	w, err := Read(dir, "Mulennials", func(z *ZDO) {
		if _, ok := names.Lookup(z.Prefab); !ok {
			unknown[z.Prefab] = true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// The seed name is 10 bytes (the .Str() length prefix is 0x0a); the byte
	// after it belongs to the following Seed int32, not to the string.
	// Real seeds stay out of the repo, so only the length and the
	// seed/hash relationship are asserted.
	if w.Format != FormatLegacy || w.Version != 37 || w.ZDOCount < 1000000 || len(w.Meta.SeedName) != 10 ||
		w.Meta.Seed != names.StableHash(w.Meta.SeedName) {
		t.Fatalf("world = %+v", w)
	}
	if len(unknown) > 0 {
		t.Logf("%d unknown prefab hashes (content removed in 1.0?)", len(unknown))
	}
	if len(w.Locations) < 5000 || len(w.Zones) < 1000 {
		t.Fatalf("zones=%d locs=%d", len(w.Zones), len(w.Locations))
	}
}

func TestReadPrefersChunkedAndLatestSave(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := LatestSave(dir, "W"); err != ErrNoSave {
		t.Fatalf("err = %v, want ErrNoSave", err)
	}
	savetest.WriteLegacyWorld(t, dir, "W", "x", nil, nil, nil, nil)
	if _, f, err := LatestSave(dir, "W"); err != nil || f != FormatLegacy {
		t.Fatalf("got %s %v", f, err)
	}
	savetest.WriteChunkedWorld(t, dir, "W", 3, "x", []savetest.ZDO{{Prefab: "bed"}}, nil, nil, nil)
	id, f, err := LatestSave(dir, "W")
	if err != nil || f != FormatChunked || id != "chunked:3" {
		t.Fatalf("got %s %s %v", id, f, err)
	}
	w, err := Read(dir, "W", func(*ZDO) {})
	if err != nil || w.Format != FormatChunked {
		t.Fatalf("Read: %v %+v", err, w)
	}
}

func TestLatestSaveInProgressChunkedDoesNotFallBackToLegacy(t *testing.T) {
	dir := t.TempDir()
	// A stale legacy save exists...
	savetest.WriteLegacyWorld(t, dir, "W", "x", nil, nil, nil, nil)
	if _, f, err := LatestSave(dir, "W"); err != nil || f != FormatLegacy {
		t.Fatalf("got %s %v, want legacy fallback with no chunked files", f, err)
	}
	// ...but a chunked save is now mid-write: _main.* files exist with no .ok.
	savetest.WriteChunkedWorld(t, dir, "W", 1, "x", nil, nil, nil, nil)
	if err := os.Remove(filepath.Join(dir, "W", "_main.1.ok")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LatestSave(dir, "W"); err != ErrSaveInProgress {
		t.Fatalf("err = %v, want ErrSaveInProgress (must not fall back to the stale .db)", err)
	}
}
