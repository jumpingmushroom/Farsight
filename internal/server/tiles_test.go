package server

import (
	"testing"

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
