package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Final review: a cache-missing ingest token costs a bcrypt compare, so
// each attempt is charged to a per-IP limiter (burst 10) before it runs.
func TestIngestBadTokensAreRateLimited(t *testing.T) {
	e := newEnv(t)
	body := gzipBytes(t, []byte(`{"events":[]}`))

	// Cache alpha's good token first.
	if r := e.rawIngest("alpha", "events", "alpha-token", body, true); r.code != 200 {
		t.Fatalf("good: %d %s", r.code, r.body)
	}
	for i := 0; i < 10; i++ {
		if r := e.rawIngest("alpha", "events", "wrong", body, true); r.code != 401 {
			t.Fatalf("bad token %d: %d, want 401", i, r.code)
		}
	}
	r := e.rawIngest("alpha", "events", "wrong", body, true)
	if r.code != http.StatusTooManyRequests || string(bytes.TrimSpace(r.body)) != `{"error":"too many attempts"}` {
		t.Fatalf("11th bad token: %d %s, want 429 too many attempts", r.code, r.body)
	}
	// Cache hits never touch the limiter.
	for i := 0; i < 3; i++ {
		if r := e.rawIngest("alpha", "events", "alpha-token", body, true); r.code != 200 {
			t.Fatalf("cached good token while limited: %d, want 200", r.code)
		}
	}
	// A good but uncached token needs a compare, so it waits its turn.
	if r := e.rawIngest("beta", "events", "beta-token", body, true); r.code != 429 {
		t.Fatalf("uncached good token while limited: %d, want 429", r.code)
	}
}

// Unknown server ids are charged like bad tokens, so probing ids is
// limited too.
func TestIngestUnknownServerChargesLimiter(t *testing.T) {
	e := newEnv(t)
	body := gzipBytes(t, []byte(`{"events":[]}`))
	for i := 0; i < 10; i++ {
		if r := e.rawIngest("nope", "events", "x", body, true); r.code != 401 {
			t.Fatalf("unknown %d: %d, want 401", i, r.code)
		}
	}
	if r := e.rawIngest("alpha", "events", "wrong", body, true); r.code != 429 {
		t.Fatalf("after 10 unknown ids: %d, want 429", r.code)
	}
}

// A successful compare refunds its reserved token.
func TestIngestGoodTokenRefundsLimiter(t *testing.T) {
	e := newEnv(t)
	body := gzipBytes(t, []byte(`{"events":[]}`))
	for i := 0; i < 9; i++ {
		if r := e.rawIngest("alpha", "events", "wrong", body, true); r.code != 401 {
			t.Fatalf("bad %d: %d", i, r.code)
		}
	}
	if r := e.rawIngest("alpha", "events", "alpha-token", body, true); r.code != 200 {
		t.Fatalf("good with one token left: %d", r.code)
	}
	if r := e.rawIngest("alpha", "events", "wrong", body, true); r.code != 401 {
		t.Fatalf("bad after refund: %d, want 401", r.code)
	}
	if r := e.rawIngest("alpha", "events", "wrong", body, true); r.code != 429 {
		t.Fatalf("bad after exhausting: %d, want 429", r.code)
	}
}

// Final review: at most two bcrypt compares run at once, across unlock
// (real and dummy) and ingest.
func TestBcryptCompareConcurrencyBounded(t *testing.T) {
	var cur, peak, calls atomic.Int32
	orig := bcryptCompare
	bcryptCompare = func(hash, pw []byte) error {
		calls.Add(1)
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		cur.Add(-1)
		return orig(hash, pw)
	}
	t.Cleanup(func() { bcryptCompare = orig })

	e := newEnvBurst(t, 100)
	body := gzipBytes(t, []byte(`{"events":[]}`))
	wrongPass := mustJSON(t, map[string]string{"server": "alpha", "passphrase": "nope"})
	unknownSrv := mustJSON(t, map[string]string{"server": "gamma", "passphrase": "nope"})

	const n = 4
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); <-start; e.rawIngest("alpha", "events", "wrong", body, true) }()
		go func() { defer wg.Done(); <-start; e.do("POST", "/api/unlock", wrongPass, nil, "") }()
		go func() { defer wg.Done(); <-start; e.do("POST", "/api/unlock", unknownSrv, nil, "") }()
	}
	close(start)
	wg.Wait()

	if calls.Load() != 3*n {
		t.Fatalf("compares = %d, want %d", calls.Load(), 3*n)
	}
	if p := peak.Load(); p > 2 {
		t.Fatalf("peak concurrent compares = %d, want <= 2", p)
	}
}

// Waiting for a compare slot gives up when the request is cancelled.
func TestCompareHashRespectsCancellation(t *testing.T) {
	for i := 0; i < cap(bcryptSlots); i++ {
		bcryptSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < cap(bcryptSlots); i++ {
			<-bcryptSlots
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := compareHash(ctx, []byte("x"), []byte("y"))
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want deadline exceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("compareHash did not give up on a cancelled context")
	}
}

// Plan 5 final review: TrustProxy describes the public listener, which
// sits behind the ingress. The in-cluster ingest listener has no proxy in
// front of it, so its limiter must key on RemoteAddr: rotating a spoofed
// X-Forwarded-For must not buy fresh bcrypt attempts. The public handler
// still honours X-Forwarded-For.
func TestSplitIngestIgnoresForwardedFor(t *testing.T) {
	e, ingestSrv := newEnvSplit(t)
	e.cfg.TrustProxy = true
	body := gzipBytes(t, []byte(`{"events":[]}`))

	bad := func(xff string) int {
		t.Helper()
		req, err := http.NewRequest("POST", ingestSrv.URL+"/ingest/alpha/events", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer wrong")
		req.Header.Set("Content-Encoding", "gzip")
		req.Header.Set("X-Forwarded-For", xff)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	for i := 0; i < 10; i++ {
		if c := bad(fmt.Sprintf("198.51.100.%d", i+1)); c != http.StatusUnauthorized {
			t.Fatalf("ingest bad token %d: %d, want 401", i, c)
		}
	}
	if c := bad("198.51.100.99"); c != http.StatusTooManyRequests {
		t.Fatalf("ingest bad token with a fresh X-Forwarded-For: %d, want 429 (keyed on RemoteAddr)", c)
	}

	// The public listener still keys unlock on the forwarded address.
	unlockBody := mustJSON(t, map[string]string{"server": "alpha", "passphrase": "nope"})
	fail := func(xff string) int {
		return e.do("POST", "/api/unlock", unlockBody, map[string]string{"X-Forwarded-For": xff}, "").code
	}
	for i := 0; i < 3; i++ {
		if c := fail("203.0.113.1"); c != http.StatusUnauthorized {
			t.Fatalf("unlock attempt %d: %d, want 401", i, c)
		}
	}
	if c := fail("203.0.113.1"); c != http.StatusTooManyRequests {
		t.Fatalf("unlock, same forwarded client: %d, want 429", c)
	}
	if c := fail("203.0.113.2"); c != http.StatusUnauthorized {
		t.Fatalf("unlock, other forwarded client: %d, want 401", c)
	}
}
