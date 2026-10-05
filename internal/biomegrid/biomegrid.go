// Package biomegrid is the base biome of a world on a 1024² grid of 20 m
// cells, for the browser's cursor readout (spec 2026-10-05, cursor biome).
// Cell (gx, gz) = (floor(x/20)+512, floor(z/20)+512), row-major by gz
// (row 0 is the south edge), sampled at its centre.
package biomegrid

import (
	"bytes"
	"compress/gzip"
	"container/list"
	"fmt"
	"math"
	"sync"

	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

const (
	Size = 1024
	Cell = 20
)

var indexOf = map[worldgen.Biome]byte{
	worldgen.Meadows: 1, worldgen.BlackForest: 2, worldgen.Swamp: 3, worldgen.Mountain: 4,
	worldgen.Plains: 5, worldgen.Mistlands: 6, worldgen.AshLands: 7, worldgen.DeepNorth: 8,
	worldgen.Ocean: 9,
}

// Index is b's byte in the grid; 0 for none.
func Index(b worldgen.Biome) byte { return indexOf[b] }

// Build samples biome at every cell centre.
func Build(biome func(x, z float32) worldgen.Biome) []byte {
	grid := make([]byte, Size*Size)
	for gz := 0; gz < Size; gz++ {
		z := float32((gz-Size/2)*Cell + Cell/2)
		for gx := 0; gx < Size; gx++ {
			x := float32((gx-Size/2)*Cell + Cell/2)
			grid[gz*Size+gx] = Index(biome(x, z))
		}
	}
	return grid
}

// Gzip compresses grid at best compression (about 45 KB for a real world).
func Gzip(grid []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write(grid)
	zw.Close()
	return buf.Bytes()
}

type key struct{ seed, gen int32 }

// Lookup is the grid byte under world point (x, z); 0 off the grid.
func Lookup(grid []byte, x, z float64) byte {
	gx := int(math.Floor(x/Cell)) + Size/2
	gz := int(math.Floor(z/Cell)) + Size/2
	if gx < 0 || gx >= Size || gz < 0 || gz >= Size {
		return 0
	}
	return grid[gz*Size+gx]
}

type entry struct {
	k    key
	done chan struct{}
	grid []byte
	gz   []byte
	err  error
}

// Cache keeps the grids (raw and gzip'd) of the max most recently used
// worlds. A build takes about a second; concurrent callers for one world
// share it.
type Cache struct {
	max   int
	build func(seed, gen int32) []byte // the raw grid

	mu      sync.Mutex
	order   *list.List // of *entry, most recent first
	entries map[key]*list.Element
}

func NewCache(max int) *Cache {
	return &Cache{
		max:     max,
		build:   func(seed, gen int32) []byte { return Build(worldgen.NewBase(seed, gen).Biome) },
		order:   list.New(),
		entries: map[key]*list.Element{},
	}
}

// Get returns (seed, gen)'s gzip'd grid, building it on a miss.
func (c *Cache) Get(seed, gen int32) ([]byte, error) {
	e := c.fetch(seed, gen)
	return e.gz, e.err
}

// Grid returns (seed, gen)'s raw grid (Size² bytes, read-only), building
// it on a miss; it shares Get's entry.
func (c *Cache) Grid(seed, gen int32) ([]byte, error) {
	e := c.fetch(seed, gen)
	return e.grid, e.err
}

// fetch returns (seed, gen)'s finished entry, building it on a miss. If build
// panics, every waiter (concurrent and future, until the next call for the
// same key retries) gets the panic back as err instead of blocking
// forever on a done that never closes: the entry is removed before done
// closes, so a retry always calls build again rather than reusing the
// broken one.
func (c *Cache) fetch(seed, gen int32) *entry {
	k := key{seed, gen}
	c.mu.Lock()
	if el, ok := c.entries[k]; ok {
		c.order.MoveToFront(el)
		e := el.Value.(*entry)
		c.mu.Unlock()
		<-e.done
		return e
	}
	e := &entry{k: k, done: make(chan struct{})}
	el := c.order.PushFront(e)
	c.entries[k] = el
	for c.order.Len() > c.max {
		old := c.order.Back()
		c.order.Remove(old)
		delete(c.entries, old.Value.(*entry).k)
	}
	c.mu.Unlock()

	e.grid, e.gz, e.err = safeBuild(c.build, seed, gen)
	if e.err != nil {
		c.mu.Lock()
		// Remove this entry only if it's still the one cached for k: while
		// build ran unlocked, another key's miss could already have
		// evicted it (T3 in the review's deferred-minors ledger) and even
		// replaced it with a fresh entry for the same k; don't delete that
		// one.
		if cur, ok := c.entries[k]; ok && cur == el {
			c.order.Remove(el)
			delete(c.entries, k)
		}
		c.mu.Unlock()
	}
	close(e.done)
	return e
}

// safeBuild builds the grid and its gzip, turning a panic in build into an
// error, so every waiter is always released instead of blocking on a done
// that never closes.
func safeBuild(build func(seed, gen int32) []byte, seed, gen int32) (grid, gz []byte, err error) {
	defer func() {
		if p := recover(); p != nil {
			grid, gz, err = nil, nil, fmt.Errorf("biomegrid: build(%d, %d): panic: %v", seed, gen, p)
		}
	}()
	grid = build(seed, gen)
	return grid, Gzip(grid), nil
}
