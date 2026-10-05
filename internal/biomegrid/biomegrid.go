// Package biomegrid is the base biome of a world on a 1024² grid of 20 m
// cells, for the browser's cursor readout (spec 2026-10-05, cursor biome).
// Cell (gx, gz) = (floor(x/20)+512, floor(z/20)+512), row-major by gz
// (row 0 is the south edge), sampled at its centre.
package biomegrid

import (
	"bytes"
	"compress/gzip"
	"container/list"
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

type entry struct {
	k    key
	done chan struct{}
	gz   []byte
}

// Cache keeps the gzip'd grids of the max most recently used worlds. A
// build takes about a second; concurrent callers for one world share it.
type Cache struct {
	max   int
	build func(seed, gen int32) []byte

	mu      sync.Mutex
	order   *list.List // of *entry, most recent first
	entries map[key]*list.Element
}

func NewCache(max int) *Cache {
	return &Cache{
		max:     max,
		build:   func(seed, gen int32) []byte { return Gzip(Build(worldgen.NewBase(seed, gen).Biome)) },
		order:   list.New(),
		entries: map[key]*list.Element{},
	}
}

// Get returns (seed, gen)'s gzip'd grid, building it on a miss.
func (c *Cache) Get(seed, gen int32) []byte {
	k := key{seed, gen}
	c.mu.Lock()
	if el, ok := c.entries[k]; ok {
		c.order.MoveToFront(el)
		e := el.Value.(*entry)
		c.mu.Unlock()
		<-e.done
		return e.gz
	}
	e := &entry{k: k, done: make(chan struct{})}
	c.entries[k] = c.order.PushFront(e)
	for c.order.Len() > c.max {
		old := c.order.Back()
		c.order.Remove(old)
		delete(c.entries, old.Value.(*entry).k)
	}
	c.mu.Unlock()
	e.gz = c.build(seed, gen)
	close(e.done)
	return e.gz
}
