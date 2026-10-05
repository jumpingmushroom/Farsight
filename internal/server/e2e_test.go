package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/agent"
	"github.com/jumpingmushroom/farsight/internal/auth"
	"github.com/jumpingmushroom/farsight/internal/config"
	"github.com/jumpingmushroom/farsight/internal/ingest"
	"github.com/jumpingmushroom/farsight/internal/live"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
	"github.com/jumpingmushroom/farsight/internal/store"
	"github.com/jumpingmushroom/farsight/internal/tileset"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
)

// syncBuffer is a bytes.Buffer safe for the concurrent writes of the
// handler goroutines' logger.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// e2eCard is the subset of the card the end-to-end test checks.
type e2eCard struct {
	Status        string `json:"status"`
	Players       int    `json:"players"`
	Version       string `json:"version"`
	LastHeartbeat string `json:"lastHeartbeat"`
	JoinCode      string `json:"joinCode"`
	Online        []struct {
		Name, Platform, PlatformID, Since string
	} `json:"online"`
	Recent []struct {
		Name, Platform, PlatformID, Since, Until string
		Seconds                                  int64
	} `json:"recent"`
	World *struct {
		Name   string `json:"name"`
		Day    int    `json:"day"`
		Bosses []struct {
			Key      string `json:"key"`
			Defeated bool   `json:"defeated"`
		} `json:"bosses"`
	} `json:"world"`
	Tiles struct {
		State string `json:"state"`
	} `json:"tiles"`
}

// TestEndToEnd drives the real agent-side code (save agent, log watcher,
// event sink, ingest client) against the full handler, then checks what a
// visitor sees through the API.
func TestEndToEnd(t *testing.T) {
	// The sink stamps heartbeats with the real clock, so the shared clock
	// starts at the real now (truncated to whole seconds, as log lines are).
	base := time.Now().UTC().Truncate(time.Second)
	clock := &fakeClock{t: base}

	cfg := &config.Config{
		Servers: []config.Server{{
			ID: "alpha", Name: "Alpha", Crossplay: true, MaxPlayers: 10,
			PassphraseHash: mustHash(t, "alpha-pass"), AgentTokenHash: mustHash(t, "alpha-token"),
		}},
		CookieKey: []byte("0123456789abcdef0123456789abcdef"),
	}
	st, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	// A render that never completes: this test is about the data path, and
	// Run is not started, so the set just stays queued.
	noRender := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		<-ctx.Done()
		return ctx.Err()
	}
	tm := tileset.NewManager(t.TempDir(), noRender, nil)

	logs := &syncBuffer{}
	applier := &live.Applier{Store: st, Now: clock.Now}
	world := worldevents.NewDeriver(st, nil)
	// Ingest derives world events in the background (CatchUpAsync); join it
	// before the store closes, as newEnvBurst does (t.Cleanup is LIFO, so
	// this, registered after st.Close's cleanup, runs first).
	t.Cleanup(world.Idle)
	h := New(Deps{
		Config:  cfg,
		Store:   st,
		Applier: applier,
		Tiles:   tm,
		World:   world,
		Codec:   auth.Codec{Key: cfg.CookieKey, Now: clock.Now},
		Limiter: auth.NewLimiter(5, 5, clock.Now),
		Now:     clock.Now,
		Log:     slog.New(slog.NewJSONHandler(logs, nil)),
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	e := &env{t: t, clock: clock, cfg: cfg, store: st, tiles: tm, srv: srv}
	ctx := context.Background()
	client := ingest.New(srv.URL, "alpha", "alpha-token")

	// --- Snapshot: the Plan 1 save agent against a synthetic chunked world.
	// No cartography table, so the mask is the zone fallback (two zones
	// shrink to nothing) plus 100 m around the built bed and portals, which
	// keeps all four markers explored.
	worlds := t.TempDir()
	zdos := []savetest.ZDO{
		{Pos: [3]float32{10, 30, 20}, Prefab: "bed", Strings: map[string]string{"ownerName": "Astrid"}, Longs: map[string]int64{"owner": 42, "creator": 42}},
		{Pos: [3]float32{100, 30, 200}, Prefab: "portal_wood", Strings: map[string]string{"tag": "home"}, Longs: map[string]int64{"creator": 42}},
		{Pos: [3]float32{-300, 30, 400}, Prefab: "portal_wood", Strings: map[string]string{"tag": "home"}, Longs: map[string]int64{"creator": 42}},
		{Pos: [3]float32{15, 30, 25}, Prefab: "Wolf", Ints: map[string]int32{"tamed": 1}, Strings: map[string]string{"TamedName": "Fang"}},
	}
	savetest.WriteChunkedWorld(t, worlds, "W", 3, "e2eseed", zdos,
		[][2]int16{{0, 0}, {1, 0}}, []string{"defeated_eikthyr"}, nil)
	ag := agent.New(agent.Config{WorldsDir: worlds, WorldName: "W", ServerID: "alpha", Client: client},
		slog.New(slog.DiscardHandler))
	if err := ag.Tick(ctx); err != nil {
		t.Fatalf("agent tick: %v", err)
	}
	if _, _, ok, err := st.LatestSnapshot(ctx, "alpha"); err != nil || !ok {
		t.Fatalf("snapshot not stored: ok=%v err=%v", ok, err)
	}

	// --- Events: a crossplay log replayed once through the watcher and sink.
	logDir := t.TempDir()
	ts := func(d time.Duration) string { return base.Add(d).Format("01/02/2006 15:04:05") }
	lines := []string{
		ts(-30*time.Minute) + ": Valheim version: l-1.0.16 (network version 40)",
		ts(-29*time.Minute) + ": Game server connected",
		ts(-28*time.Minute) + ": Session \"Alpha\" with join code 123456 and IP 203.0.113.7:2456 is active with 0 player(s)",
		// Astrid joins at -20m and leaves at -10m (600 s).
		ts(-20*time.Minute-5*time.Second) + ": PlayFab socket with remote ID playfab/AAAA received local Platform ID Steam_111",
		ts(-20*time.Minute) + ": Got character ZDOID from Astrid : 11:1",
		ts(-10*time.Minute) + ": Destroying abandoned non persistent zdo 11:5 owner 11",
		// Bjorn joins at -5m and is still online.
		ts(-5*time.Minute-5*time.Second) + ": PlayFab socket with remote ID playfab/BBBB received local Platform ID Xbox_222",
		ts(-5*time.Minute) + ": Got character ZDOID from Bjorn : 22:1",
	}
	if err := os.WriteFile(filepath.Join(logDir, "valheim-server-stdout---supervisor-e2e.log"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := agent.NewSink(client, agent.SinkConfig{Flush: 20 * time.Millisecond}, nil)
	w := &logwatch.Watcher{Dir: logDir, Loc: time.UTC, Emit: sink.Add, Once: true}
	if err := w.Run(ctx); err != nil {
		t.Fatalf("watcher: %v", err)
	}
	sctx, stop := context.WithCancel(ctx)
	sinkDone := make(chan struct{})
	go func() { sink.Run(sctx); close(sinkDone) }()

	cookie := e.mustUnlock("alpha")
	getCard := func() e2eCard {
		t.Helper()
		r := e.get("/api/servers/alpha", cookie)
		if r.code != http.StatusOK {
			t.Fatalf("card: %d %s", r.code, r.body)
		}
		var c e2eCard
		r.json(t, &c)
		return c
	}

	// Run the sink until everything (and its first heartbeat) is delivered.
	var card e2eCard
	deadline := time.Now().Add(10 * time.Second)
	for {
		card = getCard()
		if card.Status == "online" && len(card.Online) == 1 && len(card.Recent) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("events not delivered: %+v", card)
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	<-sinkDone

	if card.Version != "l-1.0.16" || card.JoinCode != "123456" || card.LastHeartbeat == "" {
		t.Errorf("card version/joinCode/heartbeat = %q %q %q", card.Version, card.JoinCode, card.LastHeartbeat)
	}
	if o := card.Online[0]; o.Name != "Bjorn" || o.Platform != "Xbox" || o.PlatformID != "222" || o.Since != rfc3339(base.Add(-5*time.Minute)) {
		t.Errorf("online = %+v", o)
	}
	if r := card.Recent[0]; r.Name != "Astrid" || r.Platform != "Steam" || r.PlatformID != "111" || r.Seconds != 600 ||
		r.Since != rfc3339(base.Add(-20*time.Minute)) || r.Until != rfc3339(base.Add(-10*time.Minute)) {
		t.Errorf("recent = %+v", r)
	}
	if card.World == nil {
		t.Fatal("card has no world")
	}
	if card.World.Name != "W" || card.World.Day != 10 {
		t.Errorf("world name/day = %q %d, want W 10", card.World.Name, card.World.Day)
	}
	bosses := map[string]bool{}
	for _, b := range card.World.Bosses {
		bosses[b.Key] = b.Defeated
	}
	if len(bosses) < 2 || !bosses["defeated_eikthyr"] || bosses["defeated_gdking"] {
		t.Errorf("bosses = %+v", card.World.Bosses)
	}
	if card.Tiles.State != "queued" {
		t.Errorf("tiles state = %q, want queued", card.Tiles.State)
	}

	// The snapshot API returns the save's markers.
	r := e.get("/api/servers/alpha/snapshot", cookie)
	if r.code != http.StatusOK {
		t.Fatalf("snapshot api: %d %s", r.code, r.body)
	}
	var snap struct {
		Markers []struct {
			ID, Kind, Label, Owner, Species, Pair string
		} `json:"markers"`
		FogKey   string `json:"fogKey"`
		Explored struct {
			Source string `json:"source"`
		} `json:"explored"`
	}
	r.json(t, &snap)
	if snap.Explored.Source != "zones" || len(snap.FogKey) != 16 {
		t.Errorf("explored source = %q, fogKey = %q", snap.Explored.Source, snap.FogKey)
	}
	kinds := map[string]int{}
	for _, m := range snap.Markers {
		kinds[m.Kind]++
		switch m.Kind {
		case "bed":
			if m.Owner != "Astrid" {
				t.Errorf("bed = %+v", m)
			}
		case "portal":
			if m.Label != "home" || m.Pair == "" {
				t.Errorf("portal = %+v", m)
			}
		case "tame":
			if m.Species != "Wolf" || m.Label != "Fang" {
				t.Errorf("tame = %+v", m)
			}
		}
	}
	if kinds["bed"] != 1 || kinds["portal"] != 2 || kinds["tame"] != 1 {
		t.Errorf("markers = %+v", snap.Markers)
	}

	// --- Heartbeats stop: 4 min later the sweep closes Bjorn's session.
	clock.Add(4 * time.Minute)
	if n, err := applier.SweepStale(ctx); err != nil || n != 1 {
		t.Fatalf("SweepStale = %d, %v; want 1 closed", n, err)
	}
	card = getCard()
	if card.Status != "offline" || len(card.Online) != 0 || len(card.Recent) != 2 {
		t.Fatalf("after sweep: %+v", card)
	}
	if r := card.Recent[0]; r.Name != "Bjorn" || r.Until != card.LastHeartbeat {
		t.Errorf("swept session = %+v, want Bjorn ending at the last heartbeat %s", r, card.LastHeartbeat)
	}
	// The API contract has no reason field; check it in the store.
	recent, err := st.Recent(ctx, "alpha", base.Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, s := range recent {
		reasons[s.Name] = s.Reason
	}
	if reasons["Bjorn"] != "server_lost" || reasons["Astrid"] != "left" {
		t.Errorf("reasons = %v", reasons)
	}

	// --- One structured Info line per ingest, and never a secret.
	out := logs.String()
	for _, secret := range []string{"alpha-token", "alpha-pass", string(cfg.CookieKey)} {
		if strings.Contains(out, secret) {
			t.Errorf("log contains a secret %q:\n%s", secret, out)
		}
	}
	var sawSnapshot, sawEvents bool
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if rec["msg"] != "ingest" {
			continue
		}
		if rec["level"] != "INFO" || rec["server"] != "alpha" {
			t.Errorf("ingest log line = %v", rec)
		}
		if b, _ := rec["bytes"].(float64); b <= 0 {
			t.Errorf("ingest log line without compressed bytes: %v", rec)
		}
		switch rec["kind"] {
		case "snapshot":
			sawSnapshot = rec["stored"] == true
		case "events":
			if _, ok := rec["applied"]; !ok {
				t.Errorf("events log line without applied: %v", rec)
			}
			if _, ok := rec["skipped"]; !ok {
				t.Errorf("events log line without skipped: %v", rec)
			}
			sawEvents = true
		default:
			t.Errorf("ingest log line with kind %v", rec["kind"])
		}
	}
	if !sawSnapshot || !sawEvents {
		t.Errorf("missing ingest log lines (snapshot %v, events %v):\n%s", sawSnapshot, sawEvents, out)
	}
}
