package agent

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jumpingmushroom/farsight/internal/ingest"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
)

// SinkConfig configures a Sink's batching, queue bound, and heartbeat
// interval. A zero value uses the documented defaults.
type SinkConfig struct {
	Flush     time.Duration // 0 => 5s: send at most this often
	MaxBatch  int           // 0 => 500 events per POST
	MaxQueue  int           // 0 => 20000; beyond it the oldest events are dropped (counted, logged)
	Heartbeat time.Duration // 0 => 60s; a heartbeat is sent at this interval
}

const (
	defaultFlush     = 5 * time.Second
	defaultMaxBatch  = 500
	defaultMaxQueue  = 20000
	defaultHeartbeat = 60 * time.Second

	// sinkMaxBackoff caps the flush-retry backoff. It is deliberately
	// shorter than agent.maxBackoff (5m, used for the save-snapshot POST
	// retry): central treats a server as offline after 3 minutes without
	// a heartbeat, so the sink must not go quiet for longer than that
	// even while its endpoint is down.
	sinkMaxBackoff = time.Minute

	// dropLogInterval rate-limits the repeated "queue full, dropping
	// oldest events" warning while a MaxQueue drop episode continues.
	dropLogInterval = time.Minute
)

// queuedEvent pairs a logwatch.Event with a monotonically increasing
// sequence number, so a flush that is in flight during a concurrent
// MaxQueue drop or further Add can still remove exactly the events it
// sent (by sequence) rather than by a stale queue index.
type queuedEvent struct {
	seq int64
	ev  logwatch.Event
}

// Sink batches logwatch.Events and POSTs them to the ingest API as kind
// "events". It never blocks the caller of Add on the network: events are
// queued and delivered by Run on a timer, with exponential backoff on
// POST failure. The queue is bounded; beyond MaxQueue the oldest events
// are dropped to keep memory bounded during a prolonged outage.
//
// Heartbeats never enter the queue: the latest pending one rides at the
// head of the next POST, so it never waits behind a backlog.
type Sink struct {
	ingest *ingest.Client
	cfg    SinkConfig
	log    *slog.Logger
	now    func() time.Time

	mu      sync.Mutex
	queue   []queuedEvent
	nextSeq int64
	hb      *logwatch.Event // latest heartbeat not yet sent; nil if none

	// dropping/dropped/lastDropLog track one "queue full" episode: dropped
	// is the count of events dropped since the episode started (reset
	// when the queue next drains back under MaxQueue), dropping is
	// whether such an episode is currently in progress, and lastDropLog
	// rate-limits the repeated warning to at most once per
	// dropLogInterval within an episode (the first drop is always
	// logged immediately).
	dropping    bool
	dropped     int
	lastDropLog time.Time
}

// NewSink returns a Sink that POSTs through c, applying cfg's defaults
// (see SinkConfig) for any zero fields. log may be nil.
func NewSink(c *ingest.Client, cfg SinkConfig, log *slog.Logger) *Sink {
	if cfg.Flush <= 0 {
		cfg.Flush = defaultFlush
	}
	if cfg.MaxBatch <= 0 {
		cfg.MaxBatch = defaultMaxBatch
	}
	if cfg.MaxQueue <= 0 {
		cfg.MaxQueue = defaultMaxQueue
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = defaultHeartbeat
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Sink{ingest: c, cfg: cfg, log: log, now: time.Now}
}

// Add appends evs to the queue. It is safe for concurrent use and never
// blocks on the network. If the queue would exceed MaxQueue, the oldest
// events are dropped to bound memory; see the Sink.dropping doc comment
// for the drop-logging policy.
func (s *Sink) Add(evs []logwatch.Event) {
	if len(evs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range evs {
		s.nextSeq++
		s.queue = append(s.queue, queuedEvent{seq: s.nextSeq, ev: e})
	}
	if over := len(s.queue) - s.cfg.MaxQueue; over > 0 {
		s.dropped += over
		s.queue = s.queue[over:]
		now := s.now()
		first := !s.dropping
		if first || now.Sub(s.lastDropLog) >= dropLogInterval {
			s.log.Warn("event queue full; dropping oldest events", "dropped", s.dropped, "queue", len(s.queue))
			s.lastDropLog = now
		}
		s.dropping = true
	}
}

// queued returns a copy of the current queue, for tests.
func (s *Sink) queued() []logwatch.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]logwatch.Event, len(s.queue))
	for i, q := range s.queue {
		out[i] = q.ev
	}
	return out
}

// addHeartbeat makes a heartbeat at now pending, replacing any older one
// not yet sent. It is not queued in the FIFO; flush puts it at the head of
// its first POST.
func (s *Sink) addHeartbeat(now time.Time) {
	e := logwatch.Event{Type: logwatch.EvHeartbeat, At: now.UTC()}
	e.ID = logwatch.EventID(e)
	s.mu.Lock()
	s.hb = &e
	s.mu.Unlock()
}

// rejected reports whether err is a response saying the payload itself
// (an event batch, or a snapshot) is unacceptable (400, 413, 422), so
// retrying the same payload can never succeed. Everything else (401, 403, 408, 429, 5xx, network errors, ...)
// is worth retrying.
func rejected(err error) (*ingest.StatusError, bool) {
	var se *ingest.StatusError
	if !errors.As(err, &se) {
		return nil, false
	}
	switch se.Code {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return se, true
	}
	return se, false
}

// nextSinkBackoff advances a flush-retry backoff: it starts at base,
// doubles on each further failure, and is capped at sinkMaxBackoff.
func nextSinkBackoff(cur, base time.Duration) time.Duration {
	if cur == 0 {
		cur = base
	} else {
		cur *= 2
	}
	if cur > sinkMaxBackoff {
		cur = sinkMaxBackoff
	}
	return cur
}

// Run flushes the queue on a Flush-interval timer and sends a heartbeat on
// a Heartbeat-interval timer (flushing right away when one is due, unless
// a retry backoff is pending), until ctx is cancelled. A heartbeat is also
// made pending immediately on start, so a freshly (re)started sidecar is
// visible as online on its first flush rather than a full Heartbeat
// interval later. On cancel, Run makes one final best-effort flush
// (bounded to 2s) and returns ctx.Err().
func (s *Sink) Run(ctx context.Context) error {
	flushT := time.NewTicker(s.cfg.Flush)
	defer flushT.Stop()
	hbT := time.NewTicker(s.cfg.Heartbeat)
	defer hbT.Stop()

	s.addHeartbeat(s.now())

	backoff := time.Duration(0)
	nextAttempt := time.Time{}

	attempt := func(now time.Time) {
		if !nextAttempt.IsZero() && now.Before(nextAttempt) {
			return
		}
		if err := s.flush(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			backoff = nextSinkBackoff(backoff, s.cfg.Flush)
			nextAttempt = now.Add(backoff)
			s.log.Warn("event POST failed; will retry", "err", err, "backoff", backoff)
			return
		}
		backoff = 0
		nextAttempt = time.Time{}
	}

	for {
		select {
		case <-ctx.Done():
			fctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			s.flush(fctx)
			cancel()
			return ctx.Err()
		case now := <-hbT.C:
			s.addHeartbeat(now)
			attempt(now)
		case now := <-flushT.C:
			attempt(now)
		}
	}
}

// flush drains the queue as it stood when flush was called: it POSTs
// batches of up to MaxBatch events, oldest first, until every event queued
// at the start has been sent, so a backlog clears in one tick. Events
// added meanwhile wait for the next flush. The pending heartbeat, if any,
// goes at the head of the first POST (on top of MaxBatch events), or alone
// when nothing is queued.
//
// Sent events are removed by sequence number, so a concurrent Add or
// MaxQueue drop during a POST can't cause the wrong events to be removed.
// A batch the server rejects as invalid (400, 413, 422) is logged at Error
// and dropped, and the drain continues. Any other failure stops the drain
// and is returned, leaving the unsent events (and the heartbeat) pending
// for a retry. When a removal ends an active drop episode (the queue is
// back under MaxQueue), one info line is logged and the episode's drop
// count reset. flush returns nil when there is nothing to send.
func (s *Sink) flush(ctx context.Context) error {
	s.mu.Lock()
	endSeq := s.nextSeq
	s.mu.Unlock()

	for {
		s.mu.Lock()
		hb := s.hb
		n := 0
		for n < len(s.queue) && n < s.cfg.MaxBatch && s.queue[n].seq <= endSeq {
			n++
		}
		if hb == nil && n == 0 {
			s.mu.Unlock()
			return nil
		}
		batch := make([]logwatch.Event, 0, n+1)
		if hb != nil {
			batch = append(batch, *hb)
		}
		var lastSeq int64
		if n > 0 {
			lastSeq = s.queue[n-1].seq
		}
		for i := 0; i < n; i++ {
			batch = append(batch, s.queue[i].ev)
		}
		s.mu.Unlock()

		if err := s.ingest.Post(ctx, "events", map[string]any{"events": batch}); err != nil {
			se, drop := rejected(err)
			if !drop {
				return err
			}
			s.log.Error("event batch rejected; dropping it", "status", se.Code, "body", se.Body, "events", len(batch))
		}

		s.mu.Lock()
		if s.hb == hb {
			s.hb = nil
		}
		i := 0
		for i < len(s.queue) && s.queue[i].seq <= lastSeq {
			i++
		}
		s.queue = s.queue[i:]
		if s.dropping && len(s.queue) < s.cfg.MaxQueue {
			s.log.Info("event queue drained below max", "dropped", s.dropped, "queue", len(s.queue))
			s.dropping = false
			s.dropped = 0
		}
		s.mu.Unlock()
	}
}
