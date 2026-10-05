package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/jumpingmushroom/farsight/internal/config"
	"github.com/jumpingmushroom/farsight/internal/ingest"
	"github.com/jumpingmushroom/farsight/internal/store"
	"github.com/jumpingmushroom/farsight/internal/tileset"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
)

const testCookieKey = "0123456789abcdef0123456789abcdef"

func minHash(t *testing.T, s string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(s), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

func writeConfig(t *testing.T, dataDir string) string {
	t.Helper()
	body := fmt.Sprintf(`{"listen":"127.0.0.1:0","dataDir":%q,"servers":[{"id":"alpha","name":"Alpha","passphraseHash":%q,"agentTokenHash":%q}]}`,
		dataDir, minHash(t, "pass"), minHash(t, "token"))
	p := filepath.Join(t.TempDir(), "farsight.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeConfigSplit is writeConfig with an ingestListen of its own, also
// ":0" so the test doesn't depend on a fixed port being free.
func writeConfigSplit(t *testing.T, dataDir string) string {
	t.Helper()
	body := fmt.Sprintf(`{"listen":"127.0.0.1:0","ingestListen":"127.0.0.1:0","dataDir":%q,"servers":[{"id":"alpha","name":"Alpha","passphraseHash":%q,"agentTokenHash":%q}]}`,
		dataDir, minHash(t, "pass"), minHash(t, "token"))
	p := filepath.Join(t.TempDir(), "farsight.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func getenv(k string) string {
	if k == "FARSIGHT_COOKIE_KEY" {
		return testCookieKey
	}
	return ""
}

func TestRunServeHealthzAndShutdown(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	cfgPath := writeConfig(t, dataDir)
	var logs strings.Builder
	log := slog.New(slog.NewJSONHandler(&logs, nil))

	ctx, cancel := context.WithCancel(context.Background())
	addrc := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- runServe(ctx, cfgPath, getenv, log, func(a, ingestAddr string) {
			if ingestAddr != "" {
				t.Errorf("ingest addr = %q, want empty (single-listener config)", ingestAddr)
			}
			addrc <- a
		})
	}()

	var addr string
	select {
	case addr = <-addrc:
	case err := <-done:
		t.Fatalf("runServe returned early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("runServe never became ready")
	}

	res, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || strings.TrimSpace(string(b)) != "ok" {
		t.Fatalf("healthz = %d %q", res.StatusCode, b)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "farsight.db")); err != nil {
		t.Errorf("store not created in dataDir: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServe = %v, want nil after cancel", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("runServe did not shut down")
	}
	if _, err := http.Get("http://" + addr + "/healthz"); err == nil {
		t.Error("server still accepting after shutdown")
	}
	if strings.Contains(logs.String(), testCookieKey) {
		t.Error("log contains the cookie key")
	}
}

// Plan 5 Task 1: with ingestListen set, runServe serves /healthz on both
// listeners, but ingest only works on the ingest port, and both shut down
// together.
func TestRunServeSplitIngestListener(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	cfgPath := writeConfigSplit(t, dataDir)
	var logs strings.Builder
	log := slog.New(slog.NewJSONHandler(&logs, nil))

	ctx, cancel := context.WithCancel(context.Background())
	type addrPair struct{ pub, ingest string }
	addrc := make(chan addrPair, 1)
	done := make(chan error, 1)
	go func() {
		done <- runServe(ctx, cfgPath, getenv, log, func(a, ingestAddr string) { addrc <- addrPair{a, ingestAddr} })
	}()

	var addrs addrPair
	select {
	case addrs = <-addrc:
	case err := <-done:
		t.Fatalf("runServe returned early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("runServe never became ready")
	}
	if addrs.ingest == "" {
		t.Fatal("no ingest address reported")
	}
	if addrs.ingest == addrs.pub {
		t.Fatalf("ingest addr == public addr: %q", addrs.ingest)
	}

	for name, addr := range map[string]string{"public": addrs.pub, "ingest": addrs.ingest} {
		res, err := http.Get("http://" + addr + "/healthz")
		if err != nil {
			t.Fatalf("%s healthz: %v", name, err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || strings.TrimSpace(string(b)) != "ok" {
			t.Fatalf("%s healthz = %d %q", name, res.StatusCode, b)
		}
	}

	// Ingest works on the ingest port...
	client := ingest.New("http://"+addrs.ingest, "alpha", "token")
	if err := client.Post(context.Background(), "events", map[string]any{"events": []any{}}); err != nil {
		t.Fatalf("ingest on ingest port: %v", err)
	}
	// ...but not on the public port: the public handler's ingest route is
	// a JSON 404.
	pubClient := ingest.New("http://"+addrs.pub, "alpha", "token")
	err := pubClient.Post(context.Background(), "events", map[string]any{"events": []any{}})
	var se *ingest.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusNotFound {
		t.Fatalf("ingest on public port: %v, want a 404 StatusError", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServe = %v, want nil after cancel", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("runServe did not shut down")
	}
	for name, addr := range map[string]string{"public": addrs.pub, "ingest": addrs.ingest} {
		if _, err := http.Get("http://" + addr + "/healthz"); err == nil {
			t.Errorf("%s still accepting after shutdown", name)
		}
	}
	if !strings.Contains(logs.String(), `"addr":"`+addrs.ingest+`"`) {
		t.Errorf("log does not mention the ingest listener address %q:\n%s", addrs.ingest, logs.String())
	}
}

// Fix round 1: each server gets its own full shutdown budget, run
// concurrently, instead of sharing one deadline (under which a slow first
// Shutdown could starve a later one). Two servers each hold a slow
// in-flight request; both must still complete within their own budget,
// and the two shutdowns must overlap rather than run back to back.
func TestShutdownAllGivesEachServerItsOwnFullBudgetConcurrently(t *testing.T) {
	const handlerDelay = 150 * time.Millisecond
	const budget = 2 * time.Second // generous: only the elapsed-time check below cares about speed

	newSlowServer := func() (*http.Server, net.Listener) {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(handlerDelay)
			w.WriteHeader(http.StatusOK)
		})
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{Handler: mux}
		go srv.Serve(ln)
		return srv, ln
	}

	s1, ln1 := newSlowServer()
	s2, ln2 := newSlowServer()

	var wg sync.WaitGroup
	codes := make([]int, 2)
	get := func(i int, addr string) {
		defer wg.Done()
		res, err := http.Get("http://" + addr + "/")
		if err != nil {
			return
		}
		codes[i] = res.StatusCode
		res.Body.Close()
	}
	wg.Add(2)
	go get(0, ln1.Addr().String())
	go get(1, ln2.Addr().String())
	// Give both requests a moment to actually reach the handler (and
	// start sleeping) before shutdown starts draining connections.
	time.Sleep(20 * time.Millisecond)

	start := time.Now()
	if err := shutdownAll(budget, s1, s2); err != nil {
		t.Fatalf("shutdownAll: %v", err)
	}
	elapsed := time.Since(start)
	wg.Wait()

	if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
		t.Fatalf("in-flight request codes = %v, want both 200: each server needs its own full budget to drain", codes)
	}
	// Sequential shutdowns (the bug this fixes) would take roughly
	// 2*handlerDelay; concurrent ones take roughly 1*handlerDelay. Give a
	// wide margin for scheduling jitter while still telling them apart.
	if elapsed >= 2*handlerDelay {
		t.Errorf("shutdownAll took %v, want well under %v (both servers should drain concurrently)", elapsed, 2*handlerDelay)
	}
}

// A nil server is skipped, and every real error is joined into one.
func TestShutdownAllSkipsNilAndJoinsErrors(t *testing.T) {
	mux := http.NewServeMux()
	stuck := make(chan struct{}) // never closed: the handler never returns
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { <-stuck })
	blocked := &http.Server{Handler: mux}
	t.Cleanup(func() { close(stuck) })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go blocked.Serve(ln)
	// An in-flight request that never completes keeps Shutdown from
	// returning until its context expires.
	go func() {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			return
		}
		defer conn.Close()
		conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
		io.ReadAll(conn)
	}()
	time.Sleep(20 * time.Millisecond) // give the request time to reach the handler

	err = shutdownAll(50*time.Millisecond, nil, blocked, nil)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdownAll = %v, want a joined context.DeadlineExceeded", err)
	}
}

func TestRunServeSetupErrors(t *testing.T) {
	err := runServe(context.Background(), filepath.Join(t.TempDir(), "missing.json"), getenv, slog.New(slog.DiscardHandler), nil)
	var se setupError
	if !errors.As(err, &se) {
		t.Fatalf("missing config: %v, want a setupError", err)
	}

	cfgPath := writeConfig(t, t.TempDir())
	err = runServe(context.Background(), cfgPath, func(string) string { return "short" }, slog.New(slog.DiscardHandler), nil)
	if !errors.As(err, &se) || strings.Contains(err.Error(), "short") {
		t.Fatalf("short cookie key: %v, want a setupError without the key", err)
	}

	if _, err := parseServeFlags([]string{"-nope"}, io.Discard); !errors.As(err, &se) {
		t.Fatalf("bad flag: %v, want a setupError", err)
	}
	if p, err := parseServeFlags(nil, io.Discard); err != nil || p != defaultConfigPath {
		t.Fatalf("default config = %q %v", p, err)
	}
}

func TestEnsureTilesOnStartup(t *testing.T) {
	st, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	cfg := &config.Config{Servers: []config.Server{{ID: "alpha"}, {ID: "beta"}, {ID: "gamma"}}}
	now := time.Now().UTC()
	// alpha: renderable; beta: gen out of range (refused); gamma: no snapshot.
	if _, err := st.PutSnapshot(ctx, "alpha", "s1", now, now, []byte(`{"world":{"seed":7,"genVersion":2}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutSnapshot(ctx, "beta", "s1", now, now, []byte(`{"world":{"seed":8,"genVersion":99}}`)); err != nil {
		t.Fatal(err)
	}
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		<-ctx.Done()
		return ctx.Err()
	}
	tm := tileset.NewManager(t.TempDir(), render, nil)

	ensureTiles(ctx, cfg, st, tm, slog.New(slog.DiscardHandler))

	if s := tm.Status(7, 2); s.State != tileset.State("queued") {
		t.Errorf("alpha tiles = %+v, want queued", s)
	}
	if s := tm.Status(8, 99); s.State != tileset.State("refused") {
		t.Errorf("beta tiles = %+v, want refused", s)
	}
}

func TestBackfillWorldEventsOnStartup(t *testing.T) {
	st, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	cfg := &config.Config{Servers: []config.Server{{ID: "alpha"}, {ID: "beta"}}}
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	// alpha: two saves, Eikthyr defeated in between; beta: none.
	for i, keys := range []string{`[]`, `["defeated_eikthyr"]`} {
		at := t0.Add(time.Duration(i) * 20 * time.Minute)
		blob := fmt.Sprintf(`{"serverId":"alpha","saveId":"s%d","savedAt":%q,"world":{"seed":7,"genVersion":2},"globalKeys":%s}`, i, at.Format(time.RFC3339), keys)
		if _, err := st.PutSnapshot(ctx, "alpha", fmt.Sprintf("s%d", i), at, at, []byte(blob)); err != nil {
			t.Fatal(err)
		}
	}

	backfillWorldEvents(ctx, cfg, worldevents.NewDeriver(st, nil), slog.New(slog.DiscardHandler))

	evs, err := st.EventsBetween(ctx, "alpha", t0, t0.Add(time.Hour))
	if err != nil || len(evs) != 1 || evs[0].Type != worldevents.TypeBoss {
		t.Fatalf("alpha events = %+v err=%v", evs, err)
	}
	if k, ok, err := st.WorldDiffState(ctx, nil, "alpha"); err != nil || !ok || k.SaveID != "s1" {
		t.Fatalf("alpha state = %+v ok=%v err=%v", k, ok, err)
	}
	if _, ok, err := st.WorldDiffState(ctx, nil, "beta"); err != nil || ok {
		t.Fatalf("beta has no snapshots, so no state: ok=%v err=%v", ok, err)
	}
}
