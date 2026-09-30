package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/ingest"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
)

type eventSink struct {
	mu     sync.Mutex
	got    []logwatch.Event
	status atomic.Int32
}

func (s *eventSink) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if st := s.status.Load(); st != 0 {
			w.WriteHeader(int(st))
			return
		}
		zr, _ := gzip.NewReader(r.Body)
		b, _ := io.ReadAll(zr)
		var body struct{ Events []logwatch.Event }
		if err := json.Unmarshal(b, &body); err != nil {
			t.Error(err)
		}
		s.mu.Lock()
		s.got = append(s.got, body.Events...)
		s.mu.Unlock()
	}
}

func (s *eventSink) n(typ string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := 0
	for _, e := range s.got {
		if typ == "" || e.Type == typ {
			c++
		}
	}
	return c
}

func eventually(t *testing.T, f func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if f() {
			return
		}
	}
	t.Fatal("timed out")
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSinkBatchesRetriesAndHeartbeats(t *testing.T) {
	es := &eventSink{}
	es.status.Store(http.StatusServiceUnavailable)
	srv := httptest.NewServer(es.handler(t))
	defer srv.Close()
	s := NewSink(ingest.New(srv.URL, "mv", "tok"), SinkConfig{Flush: 20 * time.Millisecond, MaxBatch: 2, Heartbeat: 50 * time.Millisecond}, quiet())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	var evs []logwatch.Event
	for i := 0; i < 5; i++ {
		e := logwatch.Event{Type: logwatch.EvWorldSaved, At: time.Unix(int64(1000+i), 0).UTC()}
		e.ID = logwatch.EventID(e)
		evs = append(evs, e)
	}
	s.Add(evs)
	time.Sleep(100 * time.Millisecond) // server failing: nothing delivered, nothing lost
	if es.n("") != 0 {
		t.Fatal("delivered while server was failing")
	}
	es.status.Store(0)
	eventually(t, func() bool { return es.n(logwatch.EvWorldSaved) == 5 && es.n(logwatch.EvHeartbeat) >= 1 })
	es.mu.Lock()
	var saved []logwatch.Event
	for _, e := range es.got {
		if e.Type == logwatch.EvWorldSaved {
			saved = append(saved, e)
		}
	}
	es.mu.Unlock()
	for i := range saved {
		if saved[i].ID != evs[i].ID {
			t.Fatalf("order not preserved at %d", i)
		}
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("Run returned %v", err)
	}
}

func TestSinkDropsOldestBeyondMaxQueue(t *testing.T) {
	s := NewSink(ingest.New("http://127.0.0.1:1", "mv", "t"), SinkConfig{MaxQueue: 3}, quiet())
	for i := 0; i < 5; i++ {
		s.Add([]logwatch.Event{{Type: logwatch.EvPlayersNow, ID: string(rune('a' + i))}})
	}
	if q := s.queued(); len(q) != 3 || q[0].ID != "c" || q[2].ID != "e" {
		t.Fatalf("queue = %+v", q)
	}
}

// TestSinkQueuesHeartbeatImmediatelyOnStart covers the Task 5 review
// fold-in: a heartbeat must be queued as soon as Run starts, not only on
// the first Heartbeat-interval tick, so a freshly (re)started sidecar is
// promptly visible as online. Heartbeat is set far longer than the
// test's budget, so any heartbeat observed must have come from the
// immediate queue-on-start, not the periodic ticker.
func TestSinkQueuesHeartbeatImmediatelyOnStart(t *testing.T) {
	es := &eventSink{}
	srv := httptest.NewServer(es.handler(t))
	defer srv.Close()
	s := NewSink(ingest.New(srv.URL, "mv", "tok"), SinkConfig{Flush: 10 * time.Millisecond, Heartbeat: time.Hour}, quiet())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	eventually(t, func() bool { return es.n(logwatch.EvHeartbeat) >= 1 })
}

// TestSinkBackoffCappedAtOneMinute covers the Task 5 review fold-in: the
// sink's own flush-retry backoff must cap at 1 minute, not the 5-minute
// cap the save agent uses for its snapshot POST retry, because central
// marks a server offline after 3 minutes without a heartbeat.
func TestSinkBackoffCappedAtOneMinute(t *testing.T) {
	b := time.Duration(0)
	base := 5 * time.Second
	for i := 0; i < 10; i++ {
		b = nextSinkBackoff(b, base)
		if b > sinkMaxBackoff {
			t.Fatalf("backoff exceeded cap at iteration %d: %v > %v", i, b, sinkMaxBackoff)
		}
	}
	if b != sinkMaxBackoff {
		t.Fatalf("backoff = %v after repeated failures, want it to have reached the %v cap", b, sinkMaxBackoff)
	}
}

// TestSinkRateLimitsDropWarningAndLogsDrain covers the Task 5 review
// fold-in: the first MaxQueue drop of an episode is logged immediately,
// further drops within the episode are rate-limited to at most once per
// dropLogInterval, and draining back under MaxQueue logs one info line
// and ends the episode (so the next drop logs immediately again).
func TestSinkRateLimitsDropWarningAndLogsDrain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	s := NewSink(ingest.New(srv.URL, "mv", "t"), SinkConfig{MaxQueue: 2, MaxBatch: 10}, logger)
	clock := time.Unix(1000, 0)
	s.now = func() time.Time { return clock }

	s.Add([]logwatch.Event{{ID: "a"}, {ID: "b"}, {ID: "c"}}) // len 3 > 2: drop "a"
	if n := strings.Count(buf.String(), "dropping oldest events"); n != 1 {
		t.Fatalf("first drop logged %d times, want 1: %s", n, buf.String())
	}

	clock = clock.Add(10 * time.Second)
	s.Add([]logwatch.Event{{ID: "d"}}) // drop "b": within dropLogInterval, must not log again
	if n := strings.Count(buf.String(), "dropping oldest events"); n != 1 {
		t.Fatalf("second drop (within interval) logged %d times, want still 1: %s", n, buf.String())
	}

	clock = clock.Add(dropLogInterval)
	s.Add([]logwatch.Event{{ID: "e"}}) // drop "c": past dropLogInterval, must log again
	if n := strings.Count(buf.String(), "dropping oldest events"); n != 2 {
		t.Fatalf("drop past interval logged %d times, want 2: %s", n, buf.String())
	}

	// Queue is now [d, e] (== MaxQueue). A successful flush drains it to
	// 0 (< MaxQueue), which must end the episode: one drain info line.
	if err := s.flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if n := strings.Count(buf.String(), "event queue drained"); n != 1 {
		t.Fatalf("drain logged %d times, want 1: %s", n, buf.String())
	}

	// A new episode's first drop must log immediately again, proving the
	// drain reset the episode state.
	s.Add([]logwatch.Event{{ID: "f"}, {ID: "g"}, {ID: "h"}})
	if n := strings.Count(buf.String(), "dropping oldest events"); n != 3 {
		t.Fatalf("first drop of new episode logged %d times total, want 3: %s", n, buf.String())
	}
}

// TestSinkMaxQueueDropDuringInFlightFlush covers the Task 5 review
// fold-in: a MaxQueue drop that happens while an older flush's POST is
// still in flight must only affect the live queue. The already
// snapshotted (in-flight) events must still be delivered exactly once,
// a dropped event that was never part of any batch must never be
// delivered, and the events added after the drop must survive intact
// and in order once the stale flush's seq-based removal runs.
func TestSinkMaxQueueDropDuringInFlightFlush(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	var received [][]logwatch.Event
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		zr, _ := gzip.NewReader(r.Body)
		b, _ := io.ReadAll(zr)
		var body struct{ Events []logwatch.Event }
		if err := json.Unmarshal(b, &body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		received = append(received, body.Events)
		mu.Unlock()
	}))
	defer srv.Close()

	s := NewSink(ingest.New(srv.URL, "mv", "tok"),
		SinkConfig{Flush: 300 * time.Millisecond, MaxBatch: 10, MaxQueue: 3, Heartbeat: time.Hour},
		quiet())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	s.Add([]logwatch.Event{{Type: logwatch.EvPlayersNow, ID: "a"}, {Type: logwatch.EvPlayersNow, ID: "b"}})

	<-entered // the first flush's POST (the immediate heartbeat, plus a, b) is now blocked in the handler

	// While that POST is in flight (and thus not holding the sink's
	// mutex), add beyond MaxQueue: the drop must only affect the live
	// queue, never the events already captured in the in-flight batch.
	s.Add([]logwatch.Event{{Type: logwatch.EvPlayersNow, ID: "c"}})
	s.Add([]logwatch.Event{{Type: logwatch.EvPlayersNow, ID: "d"}})
	s.Add([]logwatch.Event{{Type: logwatch.EvPlayersNow, ID: "e"}})
	s.Add([]logwatch.Event{{Type: logwatch.EvPlayersNow, ID: "f"}})

	if q := s.queued(); len(q) != 3 || q[0].ID != "d" || q[1].ID != "e" || q[2].ID != "f" {
		t.Fatalf("queue during in-flight flush = %+v, want exactly [d e f]", q)
	}

	close(release) // let the stale (heartbeat+a+b) POST complete

	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) >= 1
	})

	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("Run returned %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	seen := map[string]int{}
	for _, batch := range received {
		for _, e := range batch {
			seen[e.ID]++
		}
	}
	if seen["a"] != 1 || seen["b"] != 1 {
		t.Fatalf("a/b delivery counts = %+v, want exactly 1 each", seen)
	}
	if seen["c"] != 0 {
		t.Fatalf("c was delivered %d time(s), want 0: it was dropped before any flush ever sent it", seen["c"])
	}
	for _, id := range []string{"d", "e", "f"} {
		if seen[id] > 1 {
			t.Fatalf("%s delivered %d times, want at most 1 (no duplicates)", id, seen[id])
		}
	}
}

// recordingServer records every POST's events, in order, one slice per
// POST. respond, if set, may answer a POST itself (returning true).
type recordingServer struct {
	mu      sync.Mutex
	posts   [][]logwatch.Event
	respond func(n int, evs []logwatch.Event, w http.ResponseWriter) bool
}

func (rs *recordingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	zr, _ := gzip.NewReader(r.Body)
	b, _ := io.ReadAll(zr)
	var body struct{ Events []logwatch.Event }
	json.Unmarshal(b, &body)
	rs.mu.Lock()
	n := len(rs.posts)
	rs.mu.Unlock()
	if rs.respond != nil && rs.respond(n, body.Events, w) {
		rs.mu.Lock()
		rs.posts = append(rs.posts, nil) // counted, not delivered
		rs.mu.Unlock()
		return
	}
	rs.mu.Lock()
	rs.posts = append(rs.posts, body.Events)
	rs.mu.Unlock()
}

func (rs *recordingServer) delivered() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	var ids []string
	for _, p := range rs.posts {
		for _, e := range p {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

func ids(prefix string, n int) []logwatch.Event {
	out := make([]logwatch.Event, n)
	for i := range out {
		out[i] = logwatch.Event{Type: logwatch.EvPlayersNow, ID: fmt.Sprintf("%s%d", prefix, i)}
	}
	return out
}

// TestSinkDropsRejectedBatchAndContinues: a batch the server rejects as
// invalid (400, 413, 422) is logged at Error and dropped; later batches in
// the same flush and later flushes are still delivered.
func TestSinkDropsRejectedBatchAndContinues(t *testing.T) {
	for _, code := range []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			rs := &recordingServer{respond: func(_ int, evs []logwatch.Event, w http.ResponseWriter) bool {
				for _, e := range evs {
					if e.ID == "bad" {
						w.WriteHeader(code)
						io.WriteString(w, "invalid event bad")
						return true
					}
				}
				return false
			}}
			srv := httptest.NewServer(rs)
			defer srv.Close()
			var buf bytes.Buffer
			s := NewSink(ingest.New(srv.URL, "mv", "t"), SinkConfig{MaxBatch: 2}, slog.New(slog.NewTextHandler(&buf, nil)))

			s.Add([]logwatch.Event{{ID: "a"}, {ID: "b"}, {ID: "bad"}, {ID: "c"}, {ID: "d"}, {ID: "e"}})
			if err := s.flush(context.Background()); err != nil {
				t.Fatalf("flush = %v, want nil (a rejected batch is dropped, not retried)", err)
			}
			if got := strings.Join(rs.delivered(), ","); got != "a,b,d,e" {
				t.Fatalf("delivered %s, want a,b,d,e", got)
			}
			if q := s.queued(); len(q) != 0 {
				t.Fatalf("queue = %+v, want empty", q)
			}
			out := buf.String()
			if !strings.Contains(out, "level=ERROR") || !strings.Contains(out, strconv.Itoa(code)) || !strings.Contains(out, "invalid event bad") {
				t.Fatalf("want an Error log with the status and body: %s", out)
			}

			s.Add([]logwatch.Event{{ID: "f"}})
			if err := s.flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(rs.delivered(), ","); got != "a,b,d,e,f" {
				t.Fatalf("delivered %s, want a,b,d,e,f", got)
			}
		})
	}
}

// TestSinkRetriesTransientFailures: 401, 403, 408, 429 and 5xx keep the
// batch queued for a retry.
func TestSinkRetriesTransientFailures(t *testing.T) {
	for _, code := range []int{401, 403, 408, 429, 500, 503} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			rs := &recordingServer{respond: func(n int, _ []logwatch.Event, w http.ResponseWriter) bool {
				if n == 0 {
					w.WriteHeader(code)
					return true
				}
				return false
			}}
			srv := httptest.NewServer(rs)
			defer srv.Close()
			s := NewSink(ingest.New(srv.URL, "mv", "t"), SinkConfig{MaxBatch: 2}, quiet())
			s.Add(ids("e", 3))

			err := s.flush(context.Background())
			var se *ingest.StatusError
			if !errors.As(err, &se) || se.Code != code {
				t.Fatalf("flush = %v, want a %d StatusError", err, code)
			}
			if q := s.queued(); len(q) != 3 {
				t.Fatalf("queue = %d events after a %d, want all 3 kept", len(q), code)
			}
			if err := s.flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(rs.delivered(), ","); got != "e0,e1,e2" {
				t.Fatalf("delivered %s", got)
			}
		})
	}
}

// TestSinkRetries503InRun: under Run, a 503 is retried with backoff until
// it succeeds.
func TestSinkRetries503InRun(t *testing.T) {
	var fails atomic.Int32
	fails.Store(2)
	rs := &recordingServer{respond: func(_ int, _ []logwatch.Event, w http.ResponseWriter) bool {
		if fails.Add(-1) >= 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return true
		}
		return false
	}}
	srv := httptest.NewServer(rs)
	defer srv.Close()
	s := NewSink(ingest.New(srv.URL, "mv", "t"), SinkConfig{Flush: 10 * time.Millisecond, Heartbeat: time.Hour}, quiet())
	s.Add(ids("e", 3))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	eventually(t, func() bool { return len(rs.delivered()) == 4 }) // heartbeat + 3
}

// TestSinkDrainsBacklogInOneFlushHeartbeatFirst: one flush (one tick)
// sends full batches until the queue it started with is drained, with the
// pending heartbeat at the head of the first POST. Events added during the
// flush wait for the next one.
func TestSinkDrainsBacklogInOneFlushHeartbeatFirst(t *testing.T) {
	var s *Sink
	rs := &recordingServer{}
	rs.respond = func(n int, _ []logwatch.Event, _ http.ResponseWriter) bool {
		if n == 1 {
			s.Add(ids("late", 1)) // arrives mid-flush
		}
		return false
	}
	srv := httptest.NewServer(rs)
	defer srv.Close()
	s = NewSink(ingest.New(srv.URL, "mv", "t"), SinkConfig{MaxBatch: 500}, quiet())

	oslo, _ := time.LoadLocation("Europe/Oslo")
	s.addHeartbeat(time.Date(2026, 9, 29, 12, 0, 0, 0, oslo))
	backlog := ids("e", 2000)
	s.Add(backlog)
	if q := s.queued(); len(q) != 2000 {
		t.Fatalf("queue = %d, want 2000: heartbeats must not sit in the FIFO", len(q))
	}

	if err := s.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	rs.mu.Lock()
	posts := rs.posts
	rs.mu.Unlock()
	if len(posts) != 4 {
		t.Fatalf("%d POSTs, want 4", len(posts))
	}
	hb := posts[0][0]
	if hb.Type != logwatch.EvHeartbeat || hb.At.Location() != time.UTC || len(posts[0]) != 501 {
		t.Fatalf("first POST starts with %+v (%d events), want a UTC heartbeat then 500 events", hb, len(posts[0]))
	}
	got := rs.delivered()[1:]
	if len(got) != 2000 {
		t.Fatalf("delivered %d events, want 2000", len(got))
	}
	for i, e := range backlog {
		if got[i] != e.ID {
			t.Fatalf("order broken at %d: %s", i, got[i])
		}
	}
	if q := s.queued(); len(q) != 1 || q[0].ID != "late0" {
		t.Fatalf("queue = %+v, want just the event added mid-flush", q)
	}
}

// TestSinkHeartbeatNotBehindBacklogInRun: with a large backlog, the
// startup heartbeat is still on the very first POST.
func TestSinkHeartbeatNotBehindBacklogInRun(t *testing.T) {
	rs := &recordingServer{}
	srv := httptest.NewServer(rs)
	defer srv.Close()
	s := NewSink(ingest.New(srv.URL, "mv", "t"), SinkConfig{Flush: 10 * time.Millisecond, MaxBatch: 500, Heartbeat: time.Hour}, quiet())
	s.Add(ids("e", 2000))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	eventually(t, func() bool { return len(rs.delivered()) == 2001 })
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.posts[0][0].Type != logwatch.EvHeartbeat {
		t.Fatalf("first POST starts with %+v, want the heartbeat", rs.posts[0][0])
	}
}
