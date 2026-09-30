// Package auth implements the HMAC-signed "which servers did this
// visitor unlock" cookie and a per-key unlock-attempt rate limiter for
// the farsight central backend. It is deliberately standalone (stdlib
// only) so it can be unit tested without a database or HTTP server.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

// CookieName is the name of the cookie that carries the signed set of
// unlocked server ids.
const CookieName = "farsight"

// CookieMaxAge is how long an unlock is remembered: it is both the
// lifetime baked into the signed value's "exp" field and the Max-Age
// the HTTP layer (Task 7) should set on the cookie itself.
const CookieMaxAge = 365 * 24 * time.Hour

// Codec encodes and decodes the farsight unlock cookie value: a
// base64url-encoded JSON payload of unlocked server ids and an
// expiry, followed by "." and a base64url-encoded HMAC-SHA256 of the
// payload keyed by Key.
//
// Key is raw key material of at least 32 bytes; the caller (config)
// is responsible for that validation. Now, if set, overrides
// time.Now for both stamping and checking expiry, and is used for
// testing.
type Codec struct {
	Key []byte
	Now func() time.Time
}

// cookiePayload is the JSON structure signed and carried inside the
// cookie value.
type cookiePayload struct {
	S   []string `json:"s"`
	Exp int64    `json:"exp"`
}

func (c Codec) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Encode returns the signed cookie value for ids: ids are deduplicated
// and sorted before being stored, and the value expires CookieMaxAge
// from now.
func (c Codec) Encode(ids []string) string {
	payload := cookiePayload{
		S:   dedupSorted(ids),
		Exp: c.now().Add(CookieMaxAge).Unix(),
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		// cookiePayload is always marshalable.
		return ""
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	return payloadB64 + "." + base64.RawURLEncoding.EncodeToString(sign(c.Key, payloadJSON))
}

// Decode verifies and parses a cookie value produced by Encode. It
// returns ok=false for a malformed value, a bad MAC, an expired
// value, a value with zero ids, or when Key is empty (an empty key
// must never be usable to sign or accept a cookie).
func (c Codec) Decode(v string) (ids []string, ok bool) {
	if len(c.Key) == 0 {
		return nil, false
	}

	idx := strings.LastIndex(v, ".")
	if idx < 0 {
		return nil, false
	}
	payloadB64, macB64 := v[:idx], v[idx+1:]

	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, false
	}
	mac, err := base64.RawURLEncoding.DecodeString(macB64)
	if err != nil {
		return nil, false
	}

	if !hmac.Equal(mac, sign(c.Key, payloadJSON)) {
		return nil, false
	}

	var payload cookiePayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, false
	}
	if len(payload.S) == 0 {
		return nil, false
	}
	if payload.Exp <= c.now().Unix() {
		return nil, false
	}

	return payload.S, true
}

func sign(key, payload []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return mac.Sum(nil)
}

// dedupSorted returns the unique elements of ids in sorted order.
func dedupSorted(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// tokenBucket is the per-key state for Limiter: tokens available as of
// updated, refilled continuously up to the bucket's burst capacity.
type tokenBucket struct {
	tokens  float64
	updated time.Time
}

// Limiter is a per-key token bucket rate limiter, used to throttle
// failed unlock attempts. It is safe for concurrent use.
type Limiter struct {
	mu    sync.Mutex
	rate  float64 // tokens added per second
	burst float64 // bucket capacity, and a new key's starting balance
	now   func() time.Time

	buckets   map[string]*tokenBucket
	lastSweep time.Time
}

// idleEvictAfter is how long an untouched key's bucket is kept before
// a sweep drops it, bounding the limiter's memory use.
const idleEvictAfter = time.Hour

// sweepInterval is the minimum time between lazy sweeps.
const sweepInterval = time.Minute

// NewLimiter returns a Limiter that allows perMinute tokens per minute
// per key, up to burst tokens banked. now, if non-nil, is used in
// place of time.Now (for testing); a nil now means time.Now.
func NewLimiter(perMinute, burst int, now func() time.Time) *Limiter {
	return &Limiter{
		rate:    float64(perMinute) / 60,
		burst:   float64(burst),
		now:     now,
		buckets: make(map[string]*tokenBucket),
	}
}

func (l *Limiter) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

// Allow reports whether key has a token available, and if so consumes
// it. A new key starts with a full bucket.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock()
	l.sweepLocked(now)
	b := l.refillLocked(key, now)

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Peek reports whether key currently has a token available, without
// consuming one.
func (l *Limiter) Peek(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock()
	l.sweepLocked(now)
	b := l.refillLocked(key, now)
	return b.tokens >= 1
}

// Refund gives key back one token (capped at the bucket's burst), for a
// caller that reserved a token with Allow and then didn't need it. It is a
// no-op for a key with no bucket (never seen, or already evicted).
func (l *Limiter) Refund(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, ok := l.buckets[key]; !ok {
		return
	}
	b := l.refillLocked(key, l.clock())
	b.tokens++
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
}

// refillLocked returns key's bucket, creating a full one if it
// doesn't exist yet, and brings its token balance up to date as of
// now. l.mu must be held.
func (l *Limiter) refillLocked(key string, now time.Time) *tokenBucket {
	b, ok := l.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: l.burst, updated: now}
		l.buckets[key] = b
		return b
	}

	if elapsed := now.Sub(b.updated).Seconds(); elapsed > 0 {
		b.tokens += elapsed * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.updated = now
	}
	return b
}

// sweepLocked drops buckets idle for longer than idleEvictAfter, but
// runs at most once per sweepInterval. l.mu must be held.
func (l *Limiter) sweepLocked(now time.Time) {
	if !l.lastSweep.IsZero() && now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now

	for key, b := range l.buckets {
		if now.Sub(b.updated) >= idleEvictAfter {
			delete(l.buckets, key)
		}
	}
}
