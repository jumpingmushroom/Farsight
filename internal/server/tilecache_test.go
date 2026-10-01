package server

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func key(n int) tileCacheKey { return tileCacheKey{server: "alpha", tiles: "k", fog: "f", z: 5, x: n} }

func TestTileCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := newTileCache(10, 1)
	calls := 0
	compose := func(b string) func() ([]byte, error) {
		return func() ([]byte, error) { calls++; return []byte(b), nil }
	}
	c.get(key(1), compose("aaaa"))
	c.get(key(2), compose("bbbb"))
	c.get(key(1), compose("xxxx")) // a hit: refreshes 1
	c.get(key(3), compose("cccc")) // 12 bytes > 10: evicts 2, the least recent
	if calls != 3 || c.size != 8 {
		t.Fatalf("calls=%d size=%d", calls, c.size)
	}
	if b, _ := c.get(key(1), compose("zzzz")); string(b) != "aaaa" {
		t.Errorf("tile 1 = %q, want the cached aaaa", b)
	}
	if b, _ := c.get(key(2), compose("BBBB")); string(b) != "BBBB" {
		t.Errorf("tile 2 = %q, want a fresh compose", b)
	}
	c.get(key(9), compose("this is longer than ten bytes"))
	if _, ok := c.entries[key(9)]; ok {
		t.Error("a tile larger than the whole cache was kept")
	}
}

func TestTileCacheCoalescesConcurrentComposes(t *testing.T) {
	c := newTileCache(1<<20, 4)
	var calls atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]string, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, _ := c.get(key(1), func() ([]byte, error) {
				calls.Add(1)
				<-release
				return []byte("tile"), nil
			})
			results[i] = string(b)
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("compose ran %d times, want 1", calls.Load())
	}
	for i, r := range results {
		if r != "tile" {
			t.Fatalf("caller %d got %q", i, r)
		}
	}
}

func TestTileCacheBoundsConcurrentComposes(t *testing.T) {
	c := newTileCache(1<<20, 2)
	var running, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.get(key(i), func() ([]byte, error) {
				n := running.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				running.Add(-1)
				return []byte("t"), nil
			})
		}()
	}
	wg.Wait()
	if peak.Load() > 2 {
		t.Fatalf("%d composes at once, want at most 2", peak.Load())
	}
}

func TestTileCacheNeverCachesFailures(t *testing.T) {
	c := newTileCache(1<<20, 1)
	boom := errors.New("boom")
	if _, err := c.get(key(1), func() ([]byte, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.get(key(1), func() ([]byte, error) { panic("bad tile") }); err == nil {
		t.Fatal("a panicking compose must return an error")
	}
	if b, err := c.get(key(1), func() ([]byte, error) { return []byte("ok"), nil }); err != nil || string(b) != "ok" {
		t.Fatalf("after failures: %q %v", b, err)
	}
}
