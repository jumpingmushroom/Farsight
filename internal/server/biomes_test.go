package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/biomegrid"
	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

// biomesPath is the live /tiles/{id}/{key}/biomes URL for the ingested
// testSnapshot world (alpha, testSeed/testGen).
func biomesPath(e *env, id string) string {
	return "/tiles/" + id + "/" + e.tiles.Key(testSeed, testGen) + "/biomes"
}

func TestBiomesLockedServerIs404(t *testing.T) {
	e := newEnv(t)
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	e.waitWorld()
	if r := e.get(biomesPath(e, "alpha"), ""); r.code != 404 {
		t.Errorf("no cookie: %d, want 404", r.code)
	}
	otherCookie := e.mustUnlock("beta")
	if r := e.get(biomesPath(e, "alpha"), otherCookie); r.code != 404 {
		t.Errorf("wrong server's cookie: %d, want 404", r.code)
	}
}

func TestBiomesWrongKeyIs404(t *testing.T) {
	e := newEnv(t)
	cookie := e.mustUnlock("alpha")
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	e.waitWorld()
	if r := e.get("/tiles/alpha/not-the-real-key/biomes", cookie); r.code != 404 {
		t.Errorf("wrong key: %d, want 404", r.code)
	}
}

func TestBiomesRightKeyServesTheGrid(t *testing.T) {
	e := newEnv(t)
	cookie := e.mustUnlock("alpha")
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	e.waitWorld()

	// An explicit Accept-Encoding header stops net/http's transport from
	// transparently decompressing the response and stripping the headers
	// that prove it: without it, e.get would hand back an already-gunzipped
	// body and no Content-Encoding to check.
	r := e.do("GET", biomesPath(e, "alpha"), nil, map[string]string{"Accept-Encoding": "gzip"}, cookie)
	if r.code != 200 {
		t.Fatalf("code = %d, want 200", r.code)
	}
	if ct := r.header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	if ce := r.header.Get("Content-Encoding"); ce != "gzip" {
		t.Errorf("Content-Encoding = %q", ce)
	}
	if cc := r.header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", cc)
	}
	z, err := gzip.NewReader(bytes.NewReader(r.body))
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	grid, err := io.ReadAll(z)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(grid) != biomegrid.Size*biomegrid.Size {
		t.Fatalf("len = %d, want %d", len(grid), biomegrid.Size*biomegrid.Size)
	}
}

func TestBiomesGenVersionTooHighIs404(t *testing.T) {
	e := newEnv(t)
	cookie := e.mustUnlock("beta")
	badGen := worldgen.MaxGenVersion + 1
	snap := testSnapshot("s1", at(-time.Minute))
	snap.ServerID = "beta"
	snap.World.GenVersion = badGen
	if err := e.post("beta", "beta-token", "snapshot", snap); err != nil {
		t.Fatal(err)
	}
	e.waitWorld()
	key := e.tiles.Key(snap.World.Seed, badGen)
	if r := e.get("/tiles/beta/"+key+"/biomes", cookie); r.code != 404 {
		t.Errorf("gen %d: %d, want 404", badGen, r.code)
	}
}
