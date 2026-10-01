package server

import (
	"container/list"
	"fmt"
	"sync"
)

// fogTileCacheBytes bounds the composed fog tiles kept in memory.
const fogTileCacheBytes = 64 << 20

// tileCacheKey names one composed tile. The tile-set key is part of it so
// a re-rendered terrain set never serves a stale composition.
type tileCacheKey struct {
	server, tiles, fog string
	z, x, y            int
}

// tileCache is a size-bounded LRU of composed fog tiles (encoded PNGs). It
// coalesces concurrent requests for one tile into a single composition and
// bounds how many compositions run at once.
type tileCache struct {
	max int64
	sem chan struct{}

	mu       sync.Mutex
	size     int64
	order    *list.List // of *cacheEntry, most recently used first
	entries  map[tileCacheKey]*list.Element
	inflight map[tileCacheKey]*flight
}

type cacheEntry struct {
	key tileCacheKey
	png []byte
}

// flight is one composition in progress; done closes once png/err are set.
type flight struct {
	done chan struct{}
	png  []byte
	err  error
}

// newTileCache keeps up to maxBytes of PNGs and runs at most workers
// compositions at once.
func newTileCache(maxBytes int64, workers int) *tileCache {
	return &tileCache{
		max:      maxBytes,
		sem:      make(chan struct{}, max(1, workers)),
		order:    list.New(),
		entries:  make(map[tileCacheKey]*list.Element),
		inflight: make(map[tileCacheKey]*flight),
	}
}

// get returns k's PNG, composing it with compose on a miss. Concurrent
// misses for one key share a single compose call; errors are returned to
// every waiter and never cached.
func (c *tileCache) get(k tileCacheKey, compose func() ([]byte, error)) ([]byte, error) {
	c.mu.Lock()
	if el, ok := c.entries[k]; ok {
		c.order.MoveToFront(el)
		b := el.Value.(*cacheEntry).png
		c.mu.Unlock()
		return b, nil
	}
	if f, ok := c.inflight[k]; ok {
		c.mu.Unlock()
		<-f.done
		return f.png, f.err
	}
	f := &flight{done: make(chan struct{})}
	c.inflight[k] = f
	c.mu.Unlock()

	c.sem <- struct{}{}
	f.png, f.err = safeCompose(compose)
	<-c.sem

	c.mu.Lock()
	delete(c.inflight, k)
	if f.err == nil {
		c.add(k, f.png)
	}
	c.mu.Unlock()
	close(f.done)
	return f.png, f.err
}

// safeCompose turns a panic into an error, so waiters are always released.
func safeCompose(compose func() ([]byte, error)) (b []byte, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("server: compose tile: panic: %v", p)
		}
	}()
	return compose()
}

// add stores b under k and evicts least recently used tiles past max.
// c.mu must be held.
func (c *tileCache) add(k tileCacheKey, b []byte) {
	if int64(len(b)) > c.max {
		return
	}
	c.entries[k] = c.order.PushFront(&cacheEntry{key: k, png: b})
	c.size += int64(len(b))
	for c.size > c.max {
		back := c.order.Back()
		e := back.Value.(*cacheEntry)
		c.order.Remove(back)
		delete(c.entries, e.key)
		c.size -= int64(len(e.png))
	}
}
