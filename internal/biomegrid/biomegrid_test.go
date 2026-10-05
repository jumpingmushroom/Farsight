package biomegrid

import (
	"bytes"
	"compress/gzip"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

func TestIndex(t *testing.T) {
	cases := map[worldgen.Biome]byte{
		worldgen.Meadows: 1, worldgen.BlackForest: 2, worldgen.Swamp: 3, worldgen.Mountain: 4,
		worldgen.Plains: 5, worldgen.Mistlands: 6, worldgen.AshLands: 7, worldgen.DeepNorth: 8,
		worldgen.Ocean: 9, 0: 0,
	}
	for b, want := range cases {
		if got := Index(b); got != want {
			t.Errorf("Index(%v) = %d, want %d", b, got, want)
		}
	}
}

func TestBuildOrientation(t *testing.T) {
	// East half Meadows, west half Ocean; north of z=0 Swamp overrides east.
	grid := Build(func(x, z float32) worldgen.Biome {
		switch {
		case x < 0:
			return worldgen.Ocean
		case z >= 0:
			return worldgen.Swamp
		}
		return worldgen.Meadows
	})
	if len(grid) != Size*Size {
		t.Fatalf("len %d", len(grid))
	}
	at := func(x, z float64) byte {
		gx, gz := int(x/Cell)+Size/2, int(z/Cell)+Size/2
		return grid[gz*Size+gx]
	}
	if at(-5000, -5000) != 9 || at(5000, -5000) != 1 || at(5000, 5000) != 3 {
		t.Fatalf("orientation wrong: %d %d %d", at(-5000, -5000), at(5000, -5000), at(5000, 5000))
	}
}

func TestBuildRealSeed(t *testing.T) {
	// The golden chunked world (MuleVikings): the spawn cell centre (10, 10)
	// is Meadows (verified against Generator.Biome directly before writing
	// this assertion).
	g := worldgen.NewBase(-1032944128, 2)
	grid := Build(g.Biome)
	if grid[(Size/2)*Size+Size/2] != 1 {
		t.Fatalf("spawn cell = %d, want Meadows", grid[(Size/2)*Size+Size/2])
	}
	z, _ := gzip.NewReader(bytes.NewReader(Gzip(grid)))
	back, _ := io.ReadAll(z)
	if !bytes.Equal(back, grid) {
		t.Fatal("gzip round trip")
	}
}

func TestCacheSingleFlight(t *testing.T) {
	var builds atomic.Int32
	c := NewCache(2)
	c.build = func(seed, gen int32) []byte { builds.Add(1); return []byte{byte(seed)} }
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.Get(1, 2) }()
	}
	wg.Wait()
	if builds.Load() != 1 {
		t.Fatalf("builds = %d", builds.Load())
	}
	c.Get(2, 2)
	c.Get(3, 2) // evicts (1,2)
	c.Get(1, 2)
	if builds.Load() != 4 {
		t.Fatalf("builds = %d after eviction", builds.Load())
	}
}
