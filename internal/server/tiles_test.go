package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/fog"
)

// Fix round 1, item 1: composeTile must not keep a grown bytes.Buffer's
// whole backing array, or the tile cache's "64 MB" bound undercounts real
// heap by however much the buffer overshot (the review measured up to
// about 1.8x for a mix of fog and edge tiles).
func TestComposeTileReturnsRightSizedSlice(t *testing.T) {
	b, err := composeTile("", nil, fog.Fog, 5, 2, 16)
	if err != nil {
		t.Fatal(err)
	}
	if cap(b) != len(b) {
		t.Fatalf("cap=%d len=%d, want equal: composeTile must not retain a grown buffer's backing array", cap(b), len(b))
	}
}

// Fix round 1, item 2: FogTile(z, x, y) (internal/fog/compose.go) reads
// only TexturePixel(x*TileSize+px, y*TileSize+py): no mask, server or tile
// set. A Fog-class tile must therefore compose once and be shared across
// every server and fog key; Edge (and Clear, though Clear never reaches
// the cache) keep the full key, since they depend on the terrain set and,
// for Edge, the fog field.
func TestFogClassTilesShareCacheKeyAcrossServersAndFogKeys(t *testing.T) {
	c := newTileCache(1<<20, 1)
	var calls int
	compose := func() ([]byte, error) { calls++; return []byte("fog"), nil }

	c.get(fogCacheKey("alpha", "tk1", "fk1", fog.Fog, 5, 2, 16), compose)
	c.get(fogCacheKey("beta", "tk2", "fk2", fog.Fog, 5, 2, 16), compose)
	c.get(fogCacheKey("alpha", "tk1", "fk2", fog.Fog, 5, 2, 16), compose)
	if calls != 1 {
		t.Fatalf("Fog tile composed %d times across servers and fog keys, want 1", calls)
	}

	ke1 := fogCacheKey("alpha", "tk1", "fk1", fog.Edge, 5, 2, 16)
	ke2 := fogCacheKey("alpha", "tk1", "fk2", fog.Edge, 5, 2, 16)
	if ke1 == ke2 {
		t.Fatal("Edge tiles must keep the fog key in the cache key")
	}
	ke3 := fogCacheKey("beta", "tk1", "fk1", fog.Edge, 5, 2, 16)
	if ke1 == ke3 {
		t.Fatal("Edge tiles must keep the server in the cache key")
	}
}

// Fix round 1 (task 7 review), item 2: a fog key that is well-formed (16
// lowercase hex, fog.Key's exact shape) but not the server's current one is
// a stale link — an old tab, or a browser mid-poll just after a new save —
// so it redirects to the live key instead of 404ing. The redirect carries
// Cache-Control: no-store (the stale URL must never be cached as good) and
// never names anything the requester couldn't already have asked for
// directly: id and key have already passed the same unlocked and
// complete-tile-set checks every other tile request needs.
func TestFogTileStaleKeyRedirectsToTheLiveOne(t *testing.T) {
	e := newEnv(t)
	cookie := e.mustUnlock("alpha")
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	e.waitTiles()
	key := e.tiles.Key(testSeed, testGen)
	fk := e.snapshotView(cookie).FogKey

	stale := "0123456789abcdef"
	if stale == fk {
		t.Fatal("test fixture's fog key collides with the stale probe")
	}
	path := fmt.Sprintf("/tiles/alpha/%s/%s/0/0/0.png", key, stale)
	r := e.getNoRedirect(path, cookie)
	if r.code != 302 {
		t.Fatalf("stale key: %d, want 302", r.code)
	}
	if want := fmt.Sprintf("/tiles/alpha/%s/%s/0/0/0.png", key, fk); r.header.Get("Location") != want {
		t.Errorf("Location = %q, want %q", r.header.Get("Location"), want)
	}
	if cc := r.header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	// Following it serves the real tile.
	if r2 := e.get(path, cookie); r2.code != 200 {
		t.Errorf("following the redirect: %d, want 200", r2.code)
	}
}

// A fog key that doesn't even look like one (wrong length, wrong case, not
// hex) is as unrecognised as any other bad path segment: 404, not a
// redirect (which would otherwise accept arbitrary strings as "stale").
func TestFogTileMalformedFogKeyIs404(t *testing.T) {
	e := newEnv(t)
	cookie := e.mustUnlock("alpha")
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	e.waitTiles()
	key := e.tiles.Key(testSeed, testGen)
	for _, bad := range []string{
		"0123456789abcde",   // 15 chars
		"0123456789abcdef0", // 17 chars
		"0123456789ABCDEF",  // uppercase
		"0123456789abcdeg",  // not hex
	} {
		path := fmt.Sprintf("/tiles/alpha/%s/%s/0/0/0.png", key, bad)
		if r := e.getNoRedirect(path, cookie); r.code != 404 {
			t.Errorf("malformed fog key %q: %d, want 404", bad, r.code)
		}
	}
}

// A locked server never gets the redirect: the unlocked check runs before
// the fog-key check, so the live fog key is never handed to a requester who
// couldn't already fetch it another way.
func TestFogTileLockedServerIs404EvenWithStaleKey(t *testing.T) {
	e := newEnv(t)
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	e.waitTiles()
	key := e.tiles.Key(testSeed, testGen)
	path := fmt.Sprintf("/tiles/alpha/%s/0123456789abcdef/0/0/0.png", key)
	if r := e.getNoRedirect(path, ""); r.code != 404 {
		t.Errorf("no cookie: %d, want 404", r.code)
	}
	otherCookie := e.mustUnlock("beta")
	if r := e.getNoRedirect(path, otherCookie); r.code != 404 {
		t.Errorf("wrong server's cookie: %d, want 404", r.code)
	}
}
