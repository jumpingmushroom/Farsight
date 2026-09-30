package auth

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock is a mutex-protected, manually advanced clock so tests can
// control elapsed time deterministically (and so it's safe to share
// across goroutines in the concurrency test).
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(t time.Time) *fakeClock {
	return &fakeClock{t: t}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func testKey(b byte) []byte {
	return []byte(strings.Repeat(string(rune(b)), 32))
}

// tamperChar returns s with the byte at pos replaced by a different
// character from the base64url alphabet, so the result is still valid
// base64url but decodes to different bytes.
func tamperChar(s string, pos int) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	b := []byte(s)
	orig := b[pos]
	for i := 0; i < len(alphabet); i++ {
		if alphabet[i] != orig {
			b[pos] = alphabet[i]
			break
		}
	}
	return string(b)
}

func TestCodecRoundTripDedupSort(t *testing.T) {
	c := Codec{Key: testKey('k')}
	encoded := c.Encode([]string{"charlie", "alpha", "bravo", "alpha"})

	ids, ok := c.Decode(encoded)
	if !ok {
		t.Fatalf("Decode(%q) failed, want ok", encoded)
	}
	want := []string{"alpha", "bravo", "charlie"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
}

func TestCodecTamperedPayloadFails(t *testing.T) {
	c := Codec{Key: testKey('k')}
	encoded := c.Encode([]string{"alpha"})

	idx := strings.LastIndex(encoded, ".")
	payload, mac := encoded[:idx], encoded[idx:]
	tampered := tamperChar(payload, 0) + mac

	if _, ok := c.Decode(tampered); ok {
		t.Fatalf("Decode accepted a tampered payload")
	}
}

func TestCodecTamperedMACFails(t *testing.T) {
	c := Codec{Key: testKey('k')}
	encoded := c.Encode([]string{"alpha"})

	idx := strings.LastIndex(encoded, ".")
	payload, mac := encoded[:idx+1], encoded[idx+1:]
	tampered := payload + tamperChar(mac, 0)

	if _, ok := c.Decode(tampered); ok {
		t.Fatalf("Decode accepted a tampered MAC")
	}
}

func TestCodecDifferentKeyFails(t *testing.T) {
	encoder := Codec{Key: testKey('a')}
	decoder := Codec{Key: testKey('b')}

	encoded := encoder.Encode([]string{"alpha"})
	if _, ok := decoder.Decode(encoded); ok {
		t.Fatalf("Decode accepted a value signed with a different key")
	}
}

func TestCodecExpiredFails(t *testing.T) {
	key := testKey('k')
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	encoder := Codec{Key: key, Now: func() time.Time { return t0 }}
	encoded := encoder.Encode([]string{"alpha"})

	// Just before expiry: still valid.
	before := Codec{Key: key, Now: func() time.Time { return t0.Add(CookieMaxAge - time.Second) }}
	if _, ok := before.Decode(encoded); !ok {
		t.Fatalf("Decode rejected a not-yet-expired value")
	}

	// Exactly at expiry: exp <= now must reject.
	atExpiry := Codec{Key: key, Now: func() time.Time { return t0.Add(CookieMaxAge) }}
	if _, ok := atExpiry.Decode(encoded); ok {
		t.Fatalf("Decode accepted a value at the exact expiry instant")
	}

	// Well past expiry.
	after := Codec{Key: key, Now: func() time.Time { return t0.Add(CookieMaxAge + time.Hour) }}
	if _, ok := after.Decode(encoded); ok {
		t.Fatalf("Decode accepted an expired value")
	}
}

func TestCodecGarbageFails(t *testing.T) {
	c := Codec{Key: testKey('k')}
	cases := []string{
		"",
		"no-dot-here",
		"not!!valid.base64!!",
		".",
		"abc.",
		".abc",
		strings.Repeat("a", 8) + "." + strings.Repeat("b", 8),
	}
	for _, tc := range cases {
		if _, ok := c.Decode(tc); ok {
			t.Fatalf("Decode(%q) accepted garbage", tc)
		}
	}
}

func TestCodecEmptyKeyNeverDecodes(t *testing.T) {
	real := Codec{Key: testKey('k')}
	encoded := real.Encode([]string{"alpha"})

	empty := Codec{}
	if _, ok := empty.Decode(encoded); ok {
		t.Fatalf("Decode with an empty key accepted a value signed with a real key")
	}

	// Even a value produced with the same (empty) key must not decode:
	// this guards against a misconfigured empty key ever being usable.
	selfEncoded := empty.Encode([]string{"alpha"})
	if _, ok := empty.Decode(selfEncoded); ok {
		t.Fatalf("Decode with an empty key accepted a self-signed empty-key value")
	}
}

func TestCodecRejectsZeroIDs(t *testing.T) {
	key := testKey('k')
	c := Codec{Key: key}
	encoded := c.Encode(nil)
	if _, ok := c.Decode(encoded); ok {
		t.Fatalf("Decode accepted a value with zero ids")
	}
}

func TestLimiterBurstBlockRefill(t *testing.T) {
	const perMinute = 6 // one token per 10s
	const burst = 3
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := NewLimiter(perMinute, burst, clk.now)

	for i := 0; i < burst; i++ {
		if !l.Allow("k") {
			t.Fatalf("Allow #%d should succeed within burst", i)
		}
	}
	if l.Allow("k") {
		t.Fatalf("Allow should block once burst is exhausted")
	}
	if l.Peek("k") {
		t.Fatalf("Peek should report no token available")
	}

	clk.advance(time.Duration(60/perMinute) * time.Second)

	if !l.Peek("k") {
		t.Fatalf("Peek should report a token available after refill")
	}
	if !l.Allow("k") {
		t.Fatalf("Allow should succeed after refill")
	}
	if l.Allow("k") {
		t.Fatalf("Allow should block again immediately after consuming the refilled token")
	}
}

func TestLimiterKeysAreIndependent(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := NewLimiter(6, 1, clk.now)

	if !l.Allow("a") {
		t.Fatalf("Allow(a) should succeed")
	}
	if l.Allow("a") {
		t.Fatalf("Allow(a) should be exhausted")
	}
	if !l.Allow("b") {
		t.Fatalf("Allow(b) should be unaffected by key a")
	}
}

func TestLimiterIdleKeyEvicted(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := NewLimiter(60, 3, clk.now)

	l.Allow("stale")
	if len(l.buckets) != 1 {
		t.Fatalf("buckets = %d, want 1 after first access", len(l.buckets))
	}

	// More than 1h idle, and more than 1 minute since the last sweep, so
	// the next access triggers a sweep that drops the idle key.
	clk.advance(90 * time.Minute)
	l.Peek("fresh")

	if _, ok := l.buckets["stale"]; ok {
		t.Fatalf("idle key %q should have been evicted", "stale")
	}
	if _, ok := l.buckets["fresh"]; !ok {
		t.Fatalf("accessed key %q should be present", "fresh")
	}
	if len(l.buckets) != 1 {
		t.Fatalf("buckets = %d, want 1 after eviction", len(l.buckets))
	}
}

func TestLimiterConcurrentAccess(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := NewLimiter(600, 20, clk.now)
	keys := []string{"a", "b", "c", "d"}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := keys[i%len(keys)]
			for j := 0; j < 50; j++ {
				l.Allow(key)
				l.Peek(key)
			}
		}(i)
	}
	wg.Wait()
}

func TestLimiterRefund(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := NewLimiter(1, 2, clk.now)

	// Unknown key: no-op, and doesn't create a bucket.
	l.Refund("nobody")
	if _, ok := l.buckets["nobody"]; ok {
		t.Fatal("Refund created a bucket for an unknown key")
	}

	l.Allow("k")
	l.Allow("k")
	if l.Allow("k") {
		t.Fatal("burst should be exhausted")
	}
	l.Refund("k")
	if !l.Allow("k") {
		t.Fatal("refunded token should be usable")
	}
	if l.Allow("k") {
		t.Fatal("only one token was refunded")
	}

	// Capped at burst.
	l.Refund("k")
	l.Refund("k")
	l.Refund("k")
	for i := 0; i < 2; i++ {
		if !l.Allow("k") {
			t.Fatalf("Allow %d after refunds should succeed", i)
		}
	}
	if l.Allow("k") {
		t.Fatal("refunds must not exceed burst")
	}

	// Evicted key: no-op.
	clk.advance(90 * time.Minute)
	l.Peek("other") // triggers the sweep that evicts k
	l.Refund("k")
	if _, ok := l.buckets["k"]; ok {
		t.Fatal("Refund resurrected an evicted key")
	}
}
