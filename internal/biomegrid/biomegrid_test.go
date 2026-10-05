package biomegrid

import (
	"bytes"
	"compress/gzip"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// TestCacheGetPanicReleasesWaitersAndRetries is the M1 fix: a panicking
// build must not leave a key's waiters — concurrent ones already joined
// the in-flight entry, and future ones — blocked forever on a done that
// never closes, and a later Get for the same key must retry rather than
// replaying the broken entry. build blocks on proceed (after signalling
// entered) so the test can deterministically get a second Get to join the
// first's in-flight entry before it fails, instead of racing real
// goroutine scheduling.
func TestCacheGetPanicReleasesWaitersAndRetries(t *testing.T) {
	entered := make(chan struct{})
	proceed := make(chan struct{})
	var calls atomic.Int32
	c := NewCache(2)
	c.build = func(seed, gen int32) []byte {
		if calls.Add(1) == 1 {
			close(entered)
			<-proceed
			panic("boom")
		}
		return []byte{byte(seed)}
	}

	firstErr := make(chan error, 1)
	go func() {
		_, err := c.Get(1, 2)
		firstErr <- err
	}()
	<-entered // the first Get has created the in-flight entry and is now building

	waiterErr := make(chan error, 1)
	go func() {
		_, err := c.Get(1, 2) // joins the same in-flight entry
		waiterErr <- err
	}()
	time.Sleep(50 * time.Millisecond) // let the waiter reach the join point before build panics
	close(proceed)

	for name, ch := range map[string]chan error{"first": firstErr, "waiter": waiterErr} {
		select {
		case err := <-ch:
			if err == nil {
				t.Errorf("%s Get returned no error for a panicking build", name)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s Get blocked forever after build panicked", name)
		}
	}

	// The broken entry must have been removed: a later Get for the same
	// key calls build again instead of replaying the panic's (nil, err).
	gz, err := c.Get(1, 2)
	if err != nil || len(gz) == 0 {
		t.Fatalf("retry after panic: gz=%v err=%v", gz, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("build calls = %d, want 2 (one panicking, one retry)", calls.Load())
	}
}
