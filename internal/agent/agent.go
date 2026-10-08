// Package agent watches a Valheim world for new saves and pushes atlas
// snapshots to the central Farsight app.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/ingest"
	"github.com/jumpingmushroom/farsight/internal/save"
)

// saveLatest and saveRead are package-level seams over the save package so
// tests can inject failures (including panics, to exercise Tick's
// recover) without touching real save files. saveRead keeps the byte
// arrays the extractor needs (the cartography tables' maps).
var (
	saveLatest = save.LatestSave
	saveRead   = func(worldsDir, worldName string, fn func(*save.ZDO)) (*save.World, error) {
		return save.ReadWith(worldsDir, worldName, save.ReadOptions{KeepBytes: extract.KeepBytes}, fn)
	}
)

// maxBackoff caps the exponential backoff between POST retries.
const maxBackoff = 5 * time.Minute

type Config struct {
	WorldsDir string
	WorldName string
	ServerID  string
	URL       string
	Token     string
	Poll      time.Duration // <= 0 => 15s

	// Client, if non-nil, is used instead of building a new ingest.Client
	// from URL/ServerID/Token, so a caller that needs an ingest.Client for
	// more than one consumer (e.g. the event sink) can share one instance.
	Client *ingest.Client
}

// pendingSnapshot is a fully built snapshot for a save id that has not yet
// been successfully posted. It is kept so a POST failure can be retried
// without re-parsing the save.
type pendingSnapshot struct {
	saveID string
	snap   *extract.Snapshot
	zdos   int
	took   time.Duration
}

type Agent struct {
	cfg    Config
	log    *slog.Logger
	ingest *ingest.Client
	now    func() time.Time

	lastSent  string // save id of the last snapshot successfully posted
	candidate string // legacy: save id seen last tick, must repeat before it's read
	failedID  string // save id that failed to parse; skipped until a new id appears

	pending    *pendingSnapshot
	backoff    time.Duration
	retryAfter time.Time

	lastLoggedErr string // last distinct LatestSave error already logged, to avoid log spam
}

func New(cfg Config, log *slog.Logger) *Agent {
	if cfg.Poll <= 0 { // a negative Poll would panic time.NewTicker in Run
		cfg.Poll = 15 * time.Second
	}
	client := cfg.Client
	if client == nil {
		client = ingest.New(cfg.URL, cfg.ServerID, cfg.Token)
	}
	return &Agent{cfg: cfg, log: log, ingest: client, now: time.Now}
}

// Tick runs one poll cycle: notice a new save, parse it once, and post it,
// retrying only the POST (with backoff) on failure. It never panics: any
// panic during the cycle is recovered and returned as an error.
func (a *Agent) Tick(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("agent: recovered panic in tick: %v", r)
		}
	}()

	id, format, latestErr := saveLatest(a.cfg.WorldsDir, a.cfg.WorldName)
	newID := latestErr == nil && id != a.lastSent && id != a.failedID

	// A newer save invalidates a pending retry for an older one.
	if newID && a.pending != nil && a.pending.saveID != id {
		a.clearPending()
	}

	// Retry (or make) the POST for an already-parsed snapshot before
	// anything else, so it never re-reads the save.
	if a.pending != nil {
		// An unexpected LatestSave error would otherwise be swallowed here
		// (this branch returns before it's ever examined below); surface
		// it, but only once per distinct error string, and still attempt
		// the pending POST retry.
		if latestErr != nil && !errors.Is(latestErr, save.ErrNoSave) && !errors.Is(latestErr, save.ErrSaveInProgress) {
			if msg := latestErr.Error(); msg != a.lastLoggedErr {
				a.log.Error("checking for a newer save failed while a snapshot POST is pending", "world", a.cfg.WorldName, "err", latestErr)
				a.lastLoggedErr = msg
			}
		}
		if !a.retryAfter.IsZero() && a.now().Before(a.retryAfter) {
			return nil
		}
		return a.sendPending(ctx)
	}

	if latestErr != nil {
		if errors.Is(latestErr, save.ErrNoSave) {
			a.log.Debug("no save found yet", "world", a.cfg.WorldName)
			return nil
		}
		if errors.Is(latestErr, save.ErrSaveInProgress) {
			a.log.Debug("save in progress, waiting", "world", a.cfg.WorldName)
			return nil
		}
		return latestErr
	}

	if id == a.lastSent {
		return nil
	}
	if id == a.failedID {
		return nil
	}
	if format == save.FormatLegacy && id != a.candidate {
		a.candidate = id
		return nil
	}

	start := a.now()
	e := extract.New()
	w, err := saveRead(a.cfg.WorldsDir, a.cfg.WorldName, e.Add)
	if errors.Is(err, save.ErrSaveChanged) {
		a.log.Info("save changed while reading; will retry", "save", id)
		return nil
	}
	if errors.Is(err, save.ErrSaveInProgress) {
		a.log.Info("save in progress while reading; will retry", "save", id)
		return nil
	}
	if err != nil {
		a.failedID = id
		a.log.Error("save failed to parse; skipping until the next save", "save", id, "err", err)
		return nil
	}

	snap := e.Finish(w, a.cfg.ServerID, a.now().UTC())
	a.pending = &pendingSnapshot{saveID: w.SaveID, snap: snap, zdos: w.ZDOCount, took: a.now().Sub(start)}
	a.backoff, a.retryAfter = 0, time.Time{}
	return a.sendPending(ctx)
}

// sendPending POSTs the current pending snapshot. On success it records
// lastSent and clears the pending state; on failure it bumps the backoff
// and returns the error, leaving pending set for the next retry.
func (a *Agent) sendPending(ctx context.Context) error {
	p := a.pending
	if err := a.ingest.Post(ctx, "snapshot", p.snap); err != nil {
		a.bumpBackoff()
		return fmt.Errorf("post %s: %w", p.saveID, err)
	}
	a.lastSent = p.saveID
	a.log.Info("snapshot sent", "save", p.saveID, "zdos", p.zdos,
		"markers", len(p.snap.Markers), "unknownPrefabs", p.snap.Stats.UnknownPrefabs,
		"explored", p.snap.Explored.Source,
		"took", p.took.Round(time.Millisecond))
	a.clearPending()
	return nil
}

func (a *Agent) clearPending() {
	a.pending = nil
	a.backoff, a.retryAfter = 0, time.Time{}
	a.lastLoggedErr = ""
}

// bumpBackoff advances the POST retry backoff: it starts at Poll, doubles
// on each further failure, and is capped at maxBackoff.
func (a *Agent) bumpBackoff() {
	if a.backoff == 0 {
		a.backoff = a.cfg.Poll
	} else {
		a.backoff *= 2
	}
	if a.backoff > maxBackoff {
		a.backoff = maxBackoff
	}
	a.retryAfter = a.now().Add(a.backoff)
}

func (a *Agent) Run(ctx context.Context) error {
	t := time.NewTicker(a.cfg.Poll)
	defer t.Stop()
	for {
		if err := a.Tick(ctx); err != nil {
			a.log.Error("tick failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}
