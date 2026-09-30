package logwatch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// serverLogGlob matches the Valheim server's stdout log as supervisord
// names it: a fixed prefix, then a random suffix that changes on every pod
// restart.
const serverLogGlob = "valheim-server-stdout---supervisor-*.log"

const supervisorLogName = "supervisord.log"

// resolveServerLog returns the newest file (by mtime) matching
// serverLogGlob in dir, re-evaluated on every call. On a tie (possible on
// filesystems with coarse mtime resolution, where a freshly rotated file
// can share its mtime with the file still open), it prefers whichever path
// is already open (current) over flapping to a different candidate; with
// no current file, or if current isn't among the tied candidates, it picks
// deterministically by taking the lexically last name rather than relying
// on glob/readdir order.
func resolveServerLog(dir string) func(current string) (string, error) {
	pattern := filepath.Join(dir, serverLogGlob)
	return func(current string) (string, error) {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return "", err
		}
		type cand struct {
			path string
			mt   time.Time
		}
		var cands []cand
		for _, m := range matches {
			fi, err := os.Stat(m)
			if err != nil {
				continue // raced with rotation/removal; skip it this poll
			}
			cands = append(cands, cand{m, fi.ModTime()})
		}
		if len(cands) == 0 {
			return "", os.ErrNotExist
		}

		var bestMT time.Time
		for _, c := range cands {
			if c.mt.After(bestMT) {
				bestMT = c.mt
			}
		}
		var tied []string
		for _, c := range cands {
			if c.mt.Equal(bestMT) {
				tied = append(tied, c.path)
			}
		}
		if len(tied) == 1 {
			return tied[0], nil
		}
		for _, p := range tied {
			if p == current {
				return p, nil
			}
		}
		sort.Strings(tied)
		return tied[len(tied)-1], nil
	}
}

// resolveSupervisorLog returns the fixed supervisord.log path in dir.
func resolveSupervisorLog(dir string) func(current string) (string, error) {
	path := filepath.Join(dir, supervisorLogName)
	return func(current string) (string, error) { return path, nil }
}

// errTracker dedupes a follower's readNew errors so Run's log stays quiet
// under a steady-state condition. A missing file is logged once at Debug
// and then retried silently every poll, per the brief. Any other error
// (permission denied, an unexpected I/O failure, ...) is logged once at
// Warn per distinct error message, so a new failure is still surfaced but
// a repeated one doesn't spam the log every poll.
type errTracker struct {
	missingWarned bool
	lastWarn      string
}

func (e *errTracker) log(log *slog.Logger, role string, err error) {
	if err == nil {
		e.missingWarned, e.lastWarn = false, ""
		return
	}
	if os.IsNotExist(err) {
		if !e.missingWarned {
			log.Debug("log file not available yet, retrying", "role", role, "err", err)
			e.missingWarned = true
		}
		return
	}
	if msg := err.Error(); msg != e.lastWarn {
		log.Warn("reading log file", "role", role, "err", err)
		e.lastWarn = msg
	}
}

// tzSkewLimit is how far the newest server-log line may be from the
// file's mtime, on the first replay, before Run warns that Loc is probably
// not the TZ the game container logs in. The last line written is the
// last write, so the two normally agree to within seconds; a whole-hour
// gap means the wall-clock times are being read in the wrong zone.
const tzSkewLimit = 30 * time.Minute

// Watcher tails the Valheim server's stdout log and supervisord.log in Dir,
// replaying their existing content once and then following new lines
// across log rotation and game restarts, turning them into Events via one
// Sessionizer for the lifetime of the Watcher.
type Watcher struct {
	Dir  string         // directory holding both logs, e.g. /var/log/supervisor
	Loc  *time.Location // the game container's TZ (FARSIGHT_LOG_TZ); the logs carry local time
	Poll time.Duration  // 0 => 1s
	Emit func([]Event)  // called from the Run goroutine only
	Log  *slog.Logger   // may be nil
	Once bool           // one-shot: replay the existing files, emit, then return nil

	srv, sup *follower // set at the start of Run; exposed for tests only

	// Test hooks: testTick replaces the Poll ticker, and testPolled
	// receives once after every poll, so tests can drive polls one by one.
	testTick   chan time.Time
	testPolled chan struct{}
}

// Run polls both logs until ctx is cancelled, returning ctx.Err(). With
// Once set, it instead performs a single replay pass over the existing
// file contents, emits whatever Events that produces, and returns nil.
//
// Each poll, the replay pass included, parses each stream's new lines
// through that stream's own clock (kept for the Watcher's lifetime, so a
// DST fall-back hour resolves the same way live as in replay), merges the
// two streams' raws, stable-sorts them by time and feeds the Sessionizer.
func (w *Watcher) Run(ctx context.Context) error {
	poll := w.Poll
	if poll <= 0 {
		poll = time.Second
	}
	log := w.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	w.srv = &follower{resolve: resolveServerLog(w.Dir), role: "server", log: log}
	w.sup = &follower{resolve: resolveSupervisorLog(w.Dir), role: "supervisor", log: log}
	defer w.srv.close()
	defer w.sup.close()
	sess := NewSessionizer()
	srvClock, supClock := newClock(w.Loc), newClock(w.Loc)

	var srvErrs, supErrs errTracker

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	tick := ticker.C
	if w.testTick != nil {
		tick = w.testTick
	}

	replayed := false
	for {
		// In Once mode the first (replay) pass runs immediately, without
		// waiting out a Poll tick first.
		if !w.Once || replayed {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-tick:
			}
		}

		srvLines, srvErr := w.srv.readNew()
		srvErrs.log(log, "server", srvErr)
		supLines, supErr := w.sup.readNew()
		supErrs.log(log, "supervisor", supErr)

		var raws []Raw
		for _, l := range srvLines {
			raws = append(raws, srvClock.serverLine(l)...)
		}
		for _, l := range supLines {
			raws = append(raws, supClock.supervisorLine(l)...)
		}
		sort.SliceStable(raws, func(i, j int) bool { return raws[i].At.Before(raws[j].At) })
		var evs []Event
		for _, r := range raws {
			evs = append(evs, sess.Feed(r)...)
		}

		if !replayed {
			replayed = true
			w.checkTZ(log, srvClock.last)
		}

		if len(evs) > 0 {
			w.Emit(evs)
		}
		if w.testPolled != nil {
			w.testPolled <- struct{}{}
		}

		if w.Once {
			return nil
		}
	}
}

// checkTZ warns if the newest server-log timestamp read on the first
// replay is more than tzSkewLimit away from the server log's mtime.
func (w *Watcher) checkTZ(log *slog.Logger, newest time.Time) {
	if newest.IsZero() || w.srv.fi == nil {
		return
	}
	mtime := w.srv.fi.ModTime().UTC()
	skew := newest.Sub(mtime)
	if skew < 0 {
		skew = -skew
	}
	if skew <= tzSkewLimit {
		return
	}
	newestUTC := newest.UTC().Format(time.RFC3339)
	mtimeUTC := mtime.Format(time.RFC3339)
	log.Warn(fmt.Sprintf("newest server-log line reads as %s but the file was last written %s: "+
		"FARSIGHT_LOG_TZ (%s) probably doesn't match the game container's TZ", newestUTC, mtimeUTC, w.Loc),
		"newestLine", newestUTC, "fileMtime", mtimeUTC, "skew", skew, "tz", w.Loc.String())
}
