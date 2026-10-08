package logwatch

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type collector struct {
	mu  sync.Mutex
	evs []Event
}

func (c *collector) emit(e []Event) { c.mu.Lock(); c.evs = append(c.evs, e...); c.mu.Unlock() }
func (c *collector) count(typ string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.evs {
		if e.Type == typ {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within 5s")
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func startWatcher(t *testing.T, dir string) (*collector, context.CancelFunc) {
	t.Helper()
	c := &collector{}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{Dir: dir, Loc: oslo(t), Poll: 20 * time.Millisecond, Emit: c.emit}
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return c, cancel
}

func TestWatcherReplayLiveRotationAndSupervisor(t *testing.T) {
	dir := t.TempDir()
	srv := filepath.Join(dir, "valheim-server-stdout---supervisor-aaaa.log")
	sup := filepath.Join(dir, "supervisord.log")
	appendLine(t, srv, "09/29/2026 10:00:00: PlayFab socket with remote ID playfab/X received local Platform ID Steam_1\n")
	appendLine(t, srv, "09/29/2026 10:00:05: Got character ZDOID from A : 11:1\n")
	appendLine(t, sup, "2026-09-29 09:00:00,000 INFO spawned: 'valheim-server' with pid 1\n")

	c, _ := startWatcher(t, dir)
	waitFor(t, func() bool { return c.count(EvPlayerJoin) == 1 && c.count(EvServerStarting) == 1 })

	// Live append, written in two parts to exercise partial-line buffering.
	appendLine(t, srv, "09/29/2026 10:01:00: Got character ZDOID from B : 22:1\n09/29/2026 10:02:00: Destroying abandoned non persistent zdo 11:5 ")
	time.Sleep(100 * time.Millisecond)
	if c.count(EvPlayerLeave) != 0 {
		t.Fatal("a partial line must not be parsed")
	}
	appendLine(t, srv, "owner 11\n")
	waitFor(t, func() bool { return c.count(EvPlayerLeave) == 1 })

	// Supervisor stop closes the remaining session (B).
	appendLine(t, sup, "2026-09-29 10:10:00,000 INFO stopped: valheim-server (exit status 0)\n")
	waitFor(t, func() bool { return c.count(EvPlayerLeave) == 2 && c.count(EvServerStopped) == 1 })

	// The server restarts under a new supervisor log name (new random suffix, newer mtime).
	time.Sleep(20 * time.Millisecond)
	srv2 := filepath.Join(dir, "valheim-server-stdout---supervisor-bbbb.log")
	appendLine(t, srv2, "09/29/2026 10:11:00: Valheim version: l-1.0.16 (network version 40)\n")
	waitFor(t, func() bool { return c.count(EvServerBoot) == 1 })
}

func TestWatcherMissingFilesThenAppear(t *testing.T) {
	dir := t.TempDir()
	c, _ := startWatcher(t, dir)
	time.Sleep(60 * time.Millisecond)
	appendLine(t, filepath.Join(dir, "valheim-server-stdout---supervisor-cccc.log"), "09/29/2026 10:00:00: Game server connected\n")
	waitFor(t, func() bool { return c.count(EvServerReady) == 1 })
}

// TestWatcherOnceReplaysAndReturns covers the one-shot mode used by
// farsight-logreplay: Run must perform a single replay pass over the
// already-existing file contents and return nil promptly, without waiting
// for a Poll tick and without continuing to watch for live appends.
func TestWatcherOnceReplaysAndReturns(t *testing.T) {
	dir := t.TempDir()
	srv := filepath.Join(dir, "valheim-server-stdout---supervisor-aaaa.log")
	sup := filepath.Join(dir, "supervisord.log")
	appendLine(t, srv, "09/29/2026 10:00:00: PlayFab socket with remote ID playfab/X received local Platform ID Steam_1\n")
	appendLine(t, srv, "09/29/2026 10:00:05: Got character ZDOID from A : 11:1\n")
	appendLine(t, sup, "2026-09-29 09:00:00,000 INFO spawned: 'valheim-server' with pid 1\n")

	c := &collector{}
	w := &Watcher{Dir: dir, Loc: oslo(t), Poll: time.Hour, Emit: c.emit, Once: true}

	start := time.Now()
	err := w.Run(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run() with Once = %v, want nil", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Run() with Once took %v, want it to return promptly rather than wait out Poll (1h)", elapsed)
	}
	if got := c.count(EvPlayerJoin); got != 1 {
		t.Fatalf("player_join count = %d, want 1 from the replay", got)
	}
	if got := c.count(EvServerStarting); got != 1 {
		t.Fatalf("server_starting count = %d, want 1 from the replay", got)
	}

	// Appending more content after Run has returned must produce no more
	// events: Once means replay-then-stop, not a live watch left running.
	appendLine(t, srv, "09/29/2026 10:01:00: Got character ZDOID from B : 22:1\n")
	time.Sleep(50 * time.Millisecond)
	if got := c.count(EvPlayerJoin); got != 1 {
		t.Fatalf("player_join count after Run returned = %d, want 1 (Once watcher must not keep running)", got)
	}
}

// TestResolveServerLogMtimeTieHysteresis covers coarse-mtime filesystems,
// where a freshly rotated file can tie the currently-open file's mtime.
// resolveServerLog must not flap: a tie is broken in favor of whichever
// path is already open, and only a strictly newer mtime moves the pick.
// With no file currently open, a tie is still broken deterministically
// (lexically-last name), not by glob/readdir order.
func TestResolveServerLogMtimeTieHysteresis(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "valheim-server-stdout---supervisor-aaaa.log")
	newPath := filepath.Join(dir, "valheim-server-stdout---supervisor-bbbb.log")
	if err := os.WriteFile(oldPath, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tie := time.Now().Truncate(time.Second)
	if err := os.Chtimes(oldPath, tie, tie); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, tie, tie); err != nil {
		t.Fatal(err)
	}

	resolve := resolveServerLog(dir)

	if got, err := resolve(oldPath); err != nil || got != oldPath {
		t.Fatalf("tie with %q currently open = (%q, %v), want (%q, nil)", oldPath, got, err, oldPath)
	}

	if got, err := resolve(""); err != nil || got != newPath {
		t.Fatalf("tie with nothing open = (%q, %v), want lexically-last (%q, nil)", got, err, newPath)
	}

	later := tie.Add(time.Second)
	if err := os.Chtimes(newPath, later, later); err != nil {
		t.Fatal(err)
	}
	if got, err := resolve(oldPath); err != nil || got != newPath {
		t.Fatalf("strictly newer mtime = (%q, %v), want (%q, nil)", got, err, newPath)
	}
}

// TestWatcherClosesFollowersOnCancel ensures Run does not leak open file
// handles: once ctx is cancelled and Run has returned, both followers'
// underlying *os.File must be closed (nil'd out).
func TestWatcherClosesFollowersOnCancel(t *testing.T) {
	dir := t.TempDir()
	appendLine(t, filepath.Join(dir, "valheim-server-stdout---supervisor-aaaa.log"), "09/29/2026 10:00:00: Game server connected\n")
	appendLine(t, filepath.Join(dir, "supervisord.log"), "2026-09-29 09:00:00,000 INFO spawned: 'valheim-server' with pid 1\n")

	w := &Watcher{Dir: dir, Loc: oslo(t), Emit: func([]Event) {}, testTick: make(chan time.Time), testPolled: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	// Force one poll and wait for it: the handshake orders Run's writes
	// before these reads (polling the fields directly is a data race).
	w.testTick <- time.Now()
	<-w.testPolled
	if w.srv == nil || w.srv.f == nil {
		t.Fatalf("server follower not open after a poll: %+v", w.srv)
	}

	cancel()
	<-done

	if w.srv == nil || w.srv.f != nil {
		t.Fatalf("server follower handle not closed after Run returned: %+v", w.srv)
	}
	if w.sup == nil || w.sup.f != nil {
		t.Fatalf("supervisor follower handle not closed after Run returned: %+v", w.sup)
	}
}

// step is one poll's worth of appends: server-log text and supervisord.log
// text, written together before the poll runs.
type step struct{ srv, sup string }

const (
	testSrvLog = "valheim-server-stdout---supervisor-aaaa.log"
	testSupLog = "supervisord.log"
)

// driveLive runs a live Watcher on a fresh dir, writing each step and then
// forcing exactly one poll, so the first step goes through the replay pass
// and every later step through the live path, one poll per step.
func driveLive(t *testing.T, loc *time.Location, steps []step) []Event {
	t.Helper()
	dir := t.TempDir()
	c := &collector{}
	w := &Watcher{Dir: dir, Loc: loc, Emit: c.emit, testTick: make(chan time.Time), testPolled: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	for _, st := range steps {
		appendLine(t, filepath.Join(dir, testSrvLog), st.srv)
		appendLine(t, filepath.Join(dir, testSupLog), st.sup)
		w.testTick <- time.Now()
		<-w.testPolled
	}
	cancel()
	<-done
	return c.evs
}

// replayOnce writes every step up front and replays the result in Once mode.
func replayOnce(t *testing.T, loc *time.Location, steps []step) []Event {
	t.Helper()
	dir := t.TempDir()
	for _, st := range steps {
		appendLine(t, filepath.Join(dir, testSrvLog), st.srv)
		appendLine(t, filepath.Join(dir, testSupLog), st.sup)
	}
	c := &collector{}
	if err := (&Watcher{Dir: dir, Loc: loc, Emit: c.emit, Once: true}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c.evs
}

func sameIDs(t *testing.T, live, replay []Event) {
	t.Helper()
	if len(live) != len(replay) {
		t.Fatalf("live %d events, replay %d\nlive:   %+v\nreplay: %+v", len(live), len(replay), live, replay)
	}
	for i := range live {
		if live[i].ID != replay[i].ID {
			t.Fatalf("event %d differs:\nlive:   %+v\nreplay: %+v", i, live[i], replay[i])
		}
	}
}

// TestWatcherLiveMatchesReplayInterleaved: within one live poll, server
// and supervisor raws are merged and ordered by time exactly as in replay.
// Here the supervisor stop (10:05) falls between server lines, so feeding
// all server raws first would close A at the 10:06 boot instead of at the
// 10:05 stop.
func TestWatcherLiveMatchesReplayInterleaved(t *testing.T) {
	steps := []step{
		{srv: "09/29/2026 09:59:00: Game server connected\n"},
		{
			srv: "09/29/2026 10:00:00: PlayFab socket with remote ID playfab/X received local Platform ID Steam_1\n" +
				"09/29/2026 10:00:05: Got character ZDOID from A : 11:1\n" +
				"09/29/2026 10:06:00: Valheim version: l-1.0.16 (network version 40)\n",
			sup: "2026-09-29 10:05:00,000 INFO stopped: valheim-server (exit status 0)\n" +
				"2026-09-29 10:05:30,000 INFO spawned: 'valheim-server' with pid 2\n",
		},
	}
	live, replay := driveLive(t, time.UTC, steps), replayOnce(t, time.UTC, steps)
	sameIDs(t, live, replay)
	l := ofType(live, EvPlayerLeave)
	if len(l) != 1 || l[0].Reason != "server_stopped" || !l[0].At.Equal(utc("2026-09-29T10:05:00Z")) {
		t.Fatalf("leave = %+v, want A closed by the 10:05 stop", l)
	}
}

// TestWatcherLiveMatchesReplayAcrossFallBack: each stream keeps its own
// clock across polls, so the repeated 02:00–03:00 hour on 2026-10-25 in
// Europe/Oslo resolves identically live and in replay.
func TestWatcherLiveMatchesReplayAcrossFallBack(t *testing.T) {
	steps := []step{
		{
			srv: "10/25/2026 01:49:00: PlayFab socket with remote ID playfab/X received local Platform ID Steam_1\n" +
				"10/25/2026 01:50:00: Got character ZDOID from A : 11:1\n",
			sup: "2026-10-25 01:40:00,000 INFO spawned: 'valheim-server' with pid 1\n",
		},
		{
			srv: "10/25/2026 02:30:00: PlayFab socket with remote ID playfab/Y received local Platform ID Steam_2\n" +
				"10/25/2026 02:30:05: Got character ZDOID from B : 22:1\n" +
				"10/25/2026 02:40:00: Destroying abandoned non persistent zdo 11:5 owner 11\n" +
				"10/25/2026 02:50:00:  Connections 1 ZDOS:34937  sent:0 recv:85\n",
			sup: "2026-10-25 02:45:00,000 INFO spawned: 'crond' with pid 7\n",
		},
		{
			srv: "10/25/2026 02:10:00: Destroying abandoned non persistent zdo 22:5 owner 22\n",
			sup: "2026-10-25 02:20:00,000 INFO stopped: valheim-server (exit status 0)\n",
		},
		{srv: "10/25/2026 03:05:00: Valheim version: l-1.0.16 (network version 40)\n"},
	}
	live, replay := driveLive(t, oslo(t), steps), replayOnce(t, oslo(t), steps)
	sameIDs(t, live, replay)

	l := ofType(live, EvPlayerLeave)
	if len(l) != 2 || l[0].Name != "A" || l[0].Seconds != 3000 || l[1].Name != "B" || l[1].Seconds != 2395 ||
		!l[1].At.Equal(utc("2026-10-25T01:10:00Z")) {
		t.Fatalf("leaves = %+v, want A 3000 s, B 2395 s ending 01:10Z", l)
	}
	if s := ofType(live, EvServerStopped); len(s) != 1 || !s[0].At.Equal(utc("2026-10-25T01:20:00Z")) {
		t.Fatalf("stopped = %+v, want 01:20Z (CET)", s)
	}
	if b := ofType(live, EvServerBoot); len(b) != 1 || !b[0].At.Equal(utc("2026-10-25T02:05:00Z")) {
		t.Fatalf("boot = %+v", b)
	}
}

// TestWatcherWarnsWhenLogTZLooksWrong: on the first replay, a newest
// server-log line more than 30 minutes away from the file's mtime means
// Loc doesn't match the TZ the game container logs in.
func TestWatcherWarnsWhenLogTZLooksWrong(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mtime time.Time
		warn  bool
	}{
		{"two hours off", time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), true},
		{"within 30 minutes", time.Date(2026, 9, 29, 10, 20, 0, 0, time.UTC), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			srv := filepath.Join(dir, testSrvLog)
			appendLine(t, srv, "09/29/2026 09:00:00: Game server connected\n09/29/2026 10:00:00: World saved ( 1.0ms )\n")
			if err := os.Chtimes(srv, tc.mtime, tc.mtime); err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			w := &Watcher{Dir: dir, Loc: time.UTC, Emit: func([]Event) {}, Once: true, Log: slog.New(slog.NewTextHandler(&buf, nil))}
			if err := w.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			out := buf.String()
			got := strings.Contains(out, "FARSIGHT_LOG_TZ")
			if got != tc.warn {
				t.Fatalf("warned = %v, want %v: %s", got, tc.warn, out)
			}
			if tc.warn && (!strings.Contains(out, "2026-09-29T10:00:00Z") || !strings.Contains(out, "2026-09-29T12:00:00Z") || !strings.Contains(out, "level=WARN")) {
				t.Fatalf("warning must be Warn and carry both times: %s", out)
			}
		})
	}
}

func TestWatcherEmitsRaids(t *testing.T) {
	dir := t.TempDir()
	srv := filepath.Join(dir, "valheim-server-stdout---supervisor-aaaa.log")
	appendLine(t, srv, "10/03/2026 21:14:05: Random event set:army_theelder\n")
	c, _ := startWatcher(t, dir)
	waitFor(t, func() bool { return c.count(EvRaid) == 1 })
	appendLine(t, srv, "10/03/2026 21:40:00: Random event set:foresttrolls\n")
	waitFor(t, func() bool { return c.count(EvRaid) == 2 })
}

// TestWatcherReplayReadsTheRotatedServerLog: supervisord rotates the
// server log to <name>.1. A replay after an agent restart must read that
// first, so a session that started before the rotation still gets its
// leave, even when the rotation split a line in two.
func TestWatcherReplayReadsTheRotatedServerLog(t *testing.T) {
	dir := t.TempDir()
	srv := filepath.Join(dir, "valheim-server-stdout---supervisor-aaaa.log")
	appendLine(t, srv+".1", "09/29/2026 10:00:00: PlayFab socket with remote ID playfab/X received local Platform ID Steam_1\n")
	appendLine(t, srv+".1", "09/29/2026 10:00:05: Got character ZDOID from A : 11:1\n")
	appendLine(t, srv+".1", "09/29/2026 10:30:00: Destroying abandoned non persistent zdo 11:5 ")
	appendLine(t, srv, "owner 11\n")

	c := &collector{}
	w := &Watcher{Dir: dir, Loc: oslo(t), Poll: time.Hour, Emit: c.emit, Once: true}
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.count(EvPlayerJoin) != 1 || c.count(EvPlayerLeave) != 1 {
		t.Fatalf("events = %+v, want A's join and leave", c.evs)
	}
	for _, e := range c.evs {
		if e.Type == EvPlayerLeave && (e.Since == nil || e.PlatformID != "1") {
			t.Fatalf("leave = %+v, want it paired with the join from the rotated file", e)
		}
	}
}
