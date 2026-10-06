package server

import (
	"bytes"
	"errors"
	"log/slog"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/biomegrid"
	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
	"github.com/jumpingmushroom/farsight/internal/weather"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

func TestEstimateClock(t *testing.T) {
	a := t0 // the anchor time
	s := func(d int) time.Time { return a.Add(time.Duration(d) * time.Second) }
	iv := func(from, until int) interval { return interval{from: s(from), until: s(until)} }
	open := func(from int) interval { return interval{from: s(from)} }
	const stale = math.MinInt
	cases := []struct {
		name      string
		now       int
		online    []interval
		openUntil int // when open sessions stop counting; stale: they count nothing
		want      float64
	}{
		{"no online time", 600, nil, stale, 1000},
		{"one closed session", 600, []interval{iv(100, 200)}, stale, 1100},
		{"overlapping sessions count once", 600, []interval{iv(100, 300), iv(200, 400)}, stale, 1300},
		{"nested session counts once", 600, []interval{iv(100, 400), iv(200, 300)}, stale, 1300},
		{"unsorted sessions", 600, []interval{iv(300, 400), iv(100, 200)}, stale, 1200},
		{"started before the anchor counts from it", 600, []interval{iv(-500, 100)}, stale, 1100},
		{"ended before the anchor counts nothing", 600, []interval{iv(-500, -100)}, stale, 1000},
		{"open session counts until now", 600, []interval{open(100)}, 600, 1500},
		{"open session from before the anchor", 600, []interval{open(-100)}, 600, 1600},
		{"open session with the server not up counts nothing", 600, []interval{open(100)}, stale, 1000},
		{"closed session past now is cut at now", 600, []interval{iv(500, 900)}, stale, 1100},
		{"gaps with nobody online don't count", 600, []interval{iv(0, 100), iv(300, 350), open(550)}, 600, 1200},
		{"now before the anchor", -10, []interval{open(-100)}, -10, 1000},
		{"open session stops where players_now read 0", 600, []interval{open(100)}, 400, 1300},
		{"open session stopped before the anchor", 600, []interval{open(-300)}, -100, 1000},
		{"sub-second precision", 1, []interval{{from: a.Add(250 * time.Millisecond), until: a.Add(750 * time.Millisecond)}}, stale, 1000.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var openUntil time.Time
			if c.openUntil != stale {
				openUntil = s(c.openUntil)
			}
			if got := estimateClock(1000, a, s(c.now), c.online, openUntil); math.Abs(got-c.want) > 1e-9 {
				t.Errorf("estimateClock = %v, want %v", got, c.want)
			}
		})
	}
}

// biomeTestGrid: Swamp west of x = -1000, Ocean from there to x = 0,
// Meadows east of it.
func biomeTestGrid() []byte {
	return biomegrid.Build(func(x, z float32) worldgen.Biome {
		switch {
		case x < -1000:
			return worldgen.Swamp
		case x < 0:
			return worldgen.Ocean
		}
		return worldgen.Meadows
	})
}

// setBlock marks the w×h explored cells from (px, py).
func setBlock(m *explored.Mask, px, py, w, h int) {
	for y := py; y < py+h; y++ {
		for x := px; x < px+w; x++ {
			m.Set(x, y)
		}
	}
}

func TestExploredBiomes(t *testing.T) {
	grid := biomeTestGrid()
	m := explored.New()
	setBlock(m, 1024, 1024, 10, 10) // x, z 0…108: 100 Meadows cells
	setBlock(m, 1000, 1024, 5, 10)  // x −288…−240: 50 Ocean cells, kept
	setBlock(m, 924, 1024, 7, 7)    // x −1200…−1128: 49 Swamp cells, dropped
	got := exploredBiomes(m, grid)
	want := []worldgen.Biome{worldgen.Ocean, worldgen.Meadows} // legend order
	if !slices.Equal(got, want) {
		t.Fatalf("exploredBiomes = %v, want %v", got, want)
	}
	m.Set(924, 1031) // the 50th Swamp cell
	got = exploredBiomes(m, grid)
	want = []worldgen.Biome{worldgen.Ocean, worldgen.Meadows, worldgen.Swamp}
	if !slices.Equal(got, want) {
		t.Fatalf("with 50 Swamp cells = %v, want %v", got, want)
	}
	if got := exploredBiomes(explored.New(), grid); len(got) != 0 {
		t.Fatalf("nothing explored = %v", got)
	}
}

func TestHomeBiome(t *testing.T) {
	grid := biomeTestGrid()
	m := explored.New()
	setBlock(m, 1024, 1024, 10, 10) // Meadows
	setBlock(m, 1000, 1024, 5, 10)  // Ocean
	meadows := extract.Base{X: 50, Z: 50, Pieces: 10}
	ocean := extract.Base{X: -250, Z: 30, Pieces: 200}
	hidden := extract.Base{X: -3000, Z: 0, Pieces: 5000} // Swamp, unexplored
	cases := []struct {
		name  string
		bases []extract.Base
		want  worldgen.Biome
	}{
		{"no bases", nil, worldgen.Meadows},
		{"biggest base wins", []extract.Base{meadows, ocean}, worldgen.Ocean},
		{"unexplored bases don't count", []extract.Base{meadows, hidden}, worldgen.Meadows},
		{"only an unexplored base", []extract.Base{hidden}, worldgen.Meadows},
	}
	for _, c := range cases {
		if got := homeBiome(c.bases, m, grid); got != c.want {
			t.Errorf("%s: homeBiome = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWeatherFor(t *testing.T) {
	const netTime = 493470.0
	p := weather.Period(netTime)
	w := weatherFor(netTime, []worldgen.Biome{worldgen.Meadows, worldgen.Swamp}, worldgen.Meadows)
	if w.PeriodSec != 666 || len(w.Periods) != 3 || w.Home != "Meadows" || !slices.Equal(w.Biomes, []string{"Meadows", "Swamp"}) {
		t.Fatalf("weather = %+v", w)
	}
	for i, pj := range w.Periods {
		if pj.Start != (p+int64(i))*666 || len(pj.ByBiome) != 2 ||
			pj.ByBiome["Meadows"] != weather.At(p+int64(i), worldgen.Meadows) || pj.ByBiome["Swamp"] != "Rain" {
			t.Errorf("period %d = %+v", i, pj)
		}
	}
	// A home outside the explored list joins it, in legend order, so the
	// pill always has its weather.
	w = weatherFor(netTime, []worldgen.Biome{worldgen.Swamp}, worldgen.Meadows)
	if !slices.Equal(w.Biomes, []string{"Meadows", "Swamp"}) || w.Periods[0].ByBiome["Meadows"] == "" {
		t.Fatalf("home not added: %+v", w)
	}
}

// clockCard is the card's clock and weather, as the browser reads them.
type clockCard struct {
	Status string `json:"status"`
	Clock  *struct {
		NetTime float64 `json:"netTime"`
		At      string  `json:"at"`
		Running bool    `json:"running"`
		Source  string  `json:"source"`
	} `json:"clock"`
	Weather *struct {
		PeriodSec int `json:"periodSec"`
		Periods   []struct {
			Start   int64             `json:"start"`
			ByBiome map[string]string `json:"byBiome"`
		} `json:"periods"`
		Biomes []string `json:"biomes"`
		Home   string   `json:"home"`
	} `json:"weather"`
}

func (e *env) clockCard(cookie string) clockCard {
	e.t.Helper()
	var c clockCard
	r := e.get("/api/servers/alpha", cookie)
	if r.code != 200 {
		e.t.Fatalf("card: %d %s", r.code, r.body)
	}
	r.json(e.t, &c)
	return c
}

// postClockWorld posts the test events (Alice online since −20 min, a
// heartbeat at −1 min) and a snapshot saved at −60 s at netTime 493470,
// plus extra events.
func (e *env) postClockWorld(extra ...logwatch.Event) {
	e.t.Helper()
	if err := e.post("alpha", "alpha-token", "events", map[string]any{"events": append(testEvents(), extra...)}); err != nil {
		e.t.Fatal(err)
	}
	snap := testSnapshot("s1", at(-60*time.Second))
	snap.World.NetTime = 493470
	if err := e.post("alpha", "alpha-token", "snapshot", snap); err != nil {
		e.t.Fatal(err)
	}
}

func TestCardClockFromTheSave(t *testing.T) {
	e := newEnv(t)
	cookie := e.mustUnlock("alpha")
	if c := e.clockCard(cookie); c.Clock != nil || c.Weather != nil {
		t.Fatalf("no snapshot yet, but clock %+v weather %+v", c.Clock, c.Weather)
	}
	e.postClockWorld()
	c := e.clockCard(cookie)
	if c.Clock == nil {
		t.Fatal("no clock")
	}
	if math.Abs(c.Clock.NetTime-493530) > 1e-6 || !c.Clock.Running || c.Clock.Source != "save" || c.Clock.At != rfc3339(t0) {
		t.Errorf("clock = %+v, want 493530 running from the save at %s", *c.Clock, rfc3339(t0))
	}

	w := c.Weather
	if w == nil {
		t.Fatal("no weather")
	}
	p := weather.Period(c.Clock.NetTime)
	if w.PeriodSec != 666 || len(w.Periods) != 3 {
		t.Fatalf("weather = %+v", *w)
	}
	byName := map[string]worldgen.Biome{}
	for _, b := range []worldgen.Biome{worldgen.Ocean, worldgen.Meadows, worldgen.BlackForest, worldgen.Swamp, worldgen.Mountain,
		worldgen.Plains, worldgen.Mistlands, worldgen.AshLands, worldgen.DeepNorth} {
		byName[worldevents.BiomeName(b)] = b
	}
	// The base with most pieces ("Home", 100) sits in grid cell [0,20)²,
	// sampled at its centre.
	home := worldevents.BiomeName(worldgen.NewBase(testSeed, testGen).Biome(10, 10))
	if w.Home != home || !slices.Contains(w.Biomes, home) {
		t.Errorf("home = %q, biomes %v; want %q among them", w.Home, w.Biomes, home)
	}
	for i, pj := range w.Periods {
		if pj.Start != (p+int64(i))*666 || len(pj.ByBiome) != len(w.Biomes) {
			t.Errorf("period %d = %+v, biomes %v", i, pj, w.Biomes)
		}
		for _, name := range w.Biomes {
			b, ok := byName[name]
			if !ok || pj.ByBiome[name] != weather.At(p+int64(i), b) {
				t.Errorf("period %d %s = %q", i, name, pj.ByBiome[name])
			}
		}
	}

	// Four minutes on, the heartbeat is stale: offline, no clock.
	e.clock.Add(4 * time.Minute)
	if c := e.clockCard(cookie); c.Status != "offline" || c.Clock != nil || c.Weather != nil {
		t.Errorf("offline card: status %q clock %+v weather %+v", c.Status, c.Clock, c.Weather)
	}
}

func TestCardClockFromASleep(t *testing.T) {
	e := newEnv(t)
	e.postClockWorld(logwatch.Event{ID: "ts", Type: logwatch.EvTimeSkip, At: at(-30 * time.Second), To: 495270})
	c := e.clockCard(e.mustUnlock("alpha"))
	// Anchored at the wake-up, 12 s after the line: 18 s online since.
	if c.Clock == nil || math.Abs(c.Clock.NetTime-495288) > 1e-6 || c.Clock.Source != "sleep" || !c.Clock.Running {
		t.Fatalf("clock = %+v, want 495288 from the sleep", c.Clock)
	}
}

func TestCardClockIgnoresASleepBeforeTheSave(t *testing.T) {
	e := newEnv(t)
	e.postClockWorld(logwatch.Event{ID: "ts", Type: logwatch.EvTimeSkip, At: at(-90 * time.Second), To: 495270})
	c := e.clockCard(e.mustUnlock("alpha"))
	if c.Clock == nil || math.Abs(c.Clock.NetTime-493530) > 1e-6 || c.Clock.Source != "save" {
		t.Fatalf("clock = %+v, want 493530 from the save", c.Clock)
	}
}

func TestCardClockPausedWithNobodyOnline(t *testing.T) {
	e := newEnv(t)
	if err := e.post("alpha", "alpha-token", "events", map[string]any{"events": []logwatch.Event{
		{ID: "hb", Type: logwatch.EvHeartbeat, At: at(-time.Minute)},
	}}); err != nil {
		t.Fatal(err)
	}
	snap := testSnapshot("s1", at(-60*time.Second))
	snap.World.NetTime = 493470
	if err := e.post("alpha", "alpha-token", "snapshot", snap); err != nil {
		t.Fatal(err)
	}
	c := e.clockCard(e.mustUnlock("alpha"))
	if c.Status != "online" || c.Clock == nil || c.Clock.NetTime != 493470 || c.Clock.Running {
		t.Fatalf("status %q clock %+v, want a paused 493470", c.Status, c.Clock)
	}
}

// TestCardClockAcrossRestarts: the save at −20 min (netTime 493470) is
// the anchor; now is t0. A boot after the save means the world was
// reloaded from it, so online time before the boot (and a sleep before
// it) is lost. A players_now 0 newer than every open session stops the
// clock where it was read.
func TestCardClockAcrossRestarts(t *testing.T) {
	const saved = 493470.0
	ev := func(id, typ string, d time.Duration) logwatch.Event {
		e := logwatch.Event{ID: id, Type: typ, At: at(d)}
		switch typ {
		case logwatch.EvPlayerJoin:
			e.Name, e.Platform, e.PlatformID = "Alice", "Steam", "111"
		case logwatch.EvServerBoot:
			e.Version, e.NetworkVersion = "0.219.14", 34
		}
		return e
	}
	players := func(id string, d time.Duration, n int) logwatch.Event {
		e := ev(id, logwatch.EvPlayersNow, d)
		e.Players = ip(n)
		return e
	}
	sleep := func(id string, d time.Duration, to float64) logwatch.Event {
		e := ev(id, logwatch.EvTimeSkip, d)
		e.To = to
		return e
	}
	leave := func(id string, d time.Duration, since time.Duration) logwatch.Event {
		e := ev(id, logwatch.EvPlayerLeave, d)
		s := at(since)
		e.Name, e.Platform, e.PlatformID, e.Since, e.Seconds = "Alice", "Steam", "111", &s, int64((d-since)/time.Second)
		return e
	}
	// Up from −30 min, Alice on from −25 min.
	up := []logwatch.Event{
		ev("b1", logwatch.EvServerBoot, -30*time.Minute),
		ev("r1", logwatch.EvServerReady, -29*time.Minute),
		ev("j1", logwatch.EvPlayerJoin, -25*time.Minute),
	}
	// A crash after −11 min (the last heartbeat), back up at −10 min,
	// Alice on again from −8 min.
	crash := []logwatch.Event{
		ev("h1", logwatch.EvHeartbeat, -11*time.Minute),
		ev("b2", logwatch.EvServerBoot, -10*time.Minute),
		ev("r2", logwatch.EvServerReady, -571*time.Second),
		ev("j2", logwatch.EvPlayerJoin, -8*time.Minute),
		players("p2", -7*time.Minute, 1),
	}
	// A clean stop at the save, back up at −10 min, Alice on from −8 min.
	graceful := []logwatch.Event{
		leave("l1", -21*time.Minute, -25*time.Minute),
		ev("s1", logwatch.EvServerStopped, -20*time.Minute),
		ev("b2", logwatch.EvServerBoot, -10*time.Minute),
		ev("r2", logwatch.EvServerReady, -571*time.Second),
		ev("j2", logwatch.EvPlayerJoin, -8*time.Minute),
		players("p2", -7*time.Minute, 1),
	}
	hb := ev("h9", logwatch.EvHeartbeat, -time.Minute)
	cat := func(parts ...[]logwatch.Event) []logwatch.Event {
		var out []logwatch.Event
		for _, p := range parts {
			out = append(out, p...)
		}
		return append(out, hb)
	}
	cases := []struct {
		name    string
		events  []logwatch.Event
		want    float64
		source  string
		running bool
	}{
		{"a crash after the save counts only from the boot", cat(up, crash), saved + 480, "save", true},
		{"a sleep before the crash is lost with it",
			cat(up, []logwatch.Event{sleep("t1", -15*time.Minute, 500000)}, crash), saved + 480, "save", true},
		{"a graceful stop and restart", cat(up, graceful), saved + 480, "save", true},
		{"a sleep after the restart still wins",
			cat(up, crash, []logwatch.Event{sleep("t1", -5*time.Minute, 500000)}), 500000 + 288, "sleep", true},
		{"a players_now 0 after every open session stops the clock there",
			cat(up, []logwatch.Event{players("p1", -15*time.Minute, 0)}), saved + 300, "save", false},
		{"a players_now 0 from before the join doesn't",
			cat([]logwatch.Event{players("p0", -26*time.Minute, 0)}, up), saved + 1200, "save", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			if err := e.post("alpha", "alpha-token", "events", map[string]any{"events": c.events}); err != nil {
				t.Fatal(err)
			}
			snap := testSnapshot("s1", at(-20*time.Minute))
			snap.World.NetTime = saved
			if err := e.post("alpha", "alpha-token", "snapshot", snap); err != nil {
				t.Fatal(err)
			}
			got := e.clockCard(e.mustUnlock("alpha"))
			if got.Clock == nil {
				t.Fatalf("no clock (status %q)", got.Status)
			}
			if math.Abs(got.Clock.NetTime-c.want) > 1e-6 || got.Clock.Source != c.source || got.Clock.Running != c.running {
				t.Errorf("clock = %+v, want %v from the %s, running %v", *got.Clock, c.want, c.source, c.running)
			}
		})
	}
}

// A grid that won't build is logged once per world (seed, gen), not on
// every card poll.
func TestGridFailureLoggedOncePerWorld(t *testing.T) {
	var buf bytes.Buffer
	s := &server{Deps: Deps{Log: slog.New(slog.NewTextHandler(&buf, nil))}}
	boom := errors.New("boom")
	s.warnGridFailure("alpha", 1, 2, boom)
	s.warnGridFailure("alpha", 1, 2, boom)
	s.warnGridFailure("beta", 1, 2, boom)
	if n := strings.Count(buf.String(), "level=WARN"); n != 1 {
		t.Fatalf("%d warnings for one world, want 1:\n%s", n, buf.String())
	}
	s.warnGridFailure("alpha", 1, 3, boom)
	if n := strings.Count(buf.String(), "level=WARN"); n != 2 {
		t.Fatalf("%d warnings for two worlds, want 2:\n%s", n, buf.String())
	}
}
