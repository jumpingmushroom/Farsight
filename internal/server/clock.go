package server

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/jumpingmushroom/farsight/internal/logwatch"
	"github.com/jumpingmushroom/farsight/internal/store"
	"github.com/jumpingmushroom/farsight/internal/weather"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

// sleepWake is how long after the server's sleep line the world wakes at
// its nextm (spec 2026-10-05, time and weather).
const sleepWake = 12 * time.Second

// periodsShown is how many weather periods the card carries: the current
// one and the next two.
const periodsShown = 3

// interval is a span with someone online; a zero until is a session
// still open.
type interval struct{ from, until time.Time }

// estimateClock is the world's netTime at now: anchorT at anchorAt plus
// the seconds between anchorAt and now during which at least one player
// was online (the world clock only runs then). Overlapping intervals
// count once. Open intervals run until openUntil (now, while someone is
// online); a zero openUntil means they are stale and count nothing.
func estimateClock(anchorT float64, anchorAt, now time.Time, online []interval, openUntil time.Time) float64 {
	var spans []interval
	for _, iv := range online {
		until := iv.until
		if until.IsZero() {
			if openUntil.IsZero() {
				continue
			}
			until = openUntil
		}
		from := maxT(iv.from, anchorAt)
		until = minT(until, now)
		if until.After(from) {
			spans = append(spans, interval{from, until})
		}
	}
	slices.SortFunc(spans, func(a, b interval) int { return a.from.Compare(b.from) })
	var total time.Duration
	var cur interval
	for i, sp := range spans {
		switch {
		case i == 0:
			cur = sp
		case !sp.from.After(cur.until):
			cur.until = maxT(cur.until, sp.until)
		default:
			total += cur.until.Sub(cur.from)
			cur = sp
		}
	}
	if len(spans) > 0 {
		total += cur.until.Sub(cur.from)
	}
	return anchorT + total.Seconds()
}

func maxT(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minT(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

type clockJSON struct {
	NetTime float64 `json:"netTime"` // the estimate at At
	At      string  `json:"at"`
	Running bool    `json:"running"`
	Source  string  `json:"source"` // "save" or "sleep": the anchor used
}

type weatherPeriodJSON struct {
	Start   int64             `json:"start"` // netTime the period starts at
	ByBiome map[string]string `json:"byBiome"`
}

type weatherJSON struct {
	PeriodSec int                 `json:"periodSec"`
	Periods   []weatherPeriodJSON `json:"periods"`
	Biomes    []string            `json:"biomes"` // explored, in legend order
	Home      string              `json:"home"`
}

// weatherFor is the weather of biomes (plus home, if it isn't among
// them, so the pill always has its line) for the period at netTime and
// the next two.
func weatherFor(netTime float64, biomes []worldgen.Biome, home worldgen.Biome) *weatherJSON {
	if !slices.Contains(biomes, home) {
		biomes = append(slices.Clone(biomes), home)
		slices.SortFunc(biomes, func(a, b worldgen.Biome) int {
			return slices.Index(legendOrder, a) - slices.Index(legendOrder, b)
		})
	}
	w := &weatherJSON{PeriodSec: weather.PeriodSec, Home: worldevents.BiomeName(home)}
	for _, b := range biomes {
		w.Biomes = append(w.Biomes, worldevents.BiomeName(b))
	}
	p := weather.Period(netTime)
	for i := int64(0); i < periodsShown; i++ {
		pj := weatherPeriodJSON{Start: (p + i) * weather.PeriodSec, ByBiome: map[string]string{}}
		for _, b := range biomes {
			pj.ByBiome[worldevents.BiomeName(b)] = weather.At(p+i, b)
		}
		w.Periods = append(w.Periods, pj)
	}
	return w
}

// clockAndWeather is the card's clock and weather for server id's world
// ws, with online its open sessions. The anchor is the save, or the
// newest sleep if it woke the world after the save.
//
// A boot after the save (a crash, which has no shutdown save, or a
// restart after a clean stop, whose shutdown save is the anchor) means
// the world was reloaded from the save: online time before the boot
// never reached it, and nor did a sleep before the boot.
func (s *server) clockAndWeather(ctx context.Context, id string, ws *worldState, online []store.Session) (*clockJSON, *weatherJSON, error) {
	now := s.Now()
	anchorT, anchorAt, source := ws.snap.World.NetTime, ws.snap.SavedAt, "save"
	boot, booted, err := s.latestBootSince(ctx, id, anchorAt)
	if err != nil {
		return nil, nil, err
	}
	// Only a sleep whose wake-up is after the save can win; bounding the
	// query there keeps it off the server's older history.
	se, ok, err := s.Store.LatestEventOfTypeSince(ctx, id, logwatch.EvTimeSkip, anchorAt.Add(-sleepWake))
	if err != nil {
		return nil, nil, err
	}
	if ok && !(booted && se.At.Before(boot)) {
		var ev logwatch.Event
		if err := json.Unmarshal(se.Body, &ev); err != nil {
			return nil, nil, err
		}
		if wake := se.At.Add(sleepWake); wake.After(anchorAt) {
			anchorT, anchorAt, source = ev.To, wake, "sleep"
		}
	}
	// Online time counts from the anchor, or from the boot if that is
	// later.
	countFrom := anchorAt
	if booted && boot.After(countFrom) {
		countFrom = boot
	}

	running, openUntil, err := s.openSessionsUntil(ctx, id, online, now)
	if err != nil {
		return nil, nil, err
	}
	var ivs []interval
	if now.After(countFrom) {
		sessions, err := s.Store.SessionsOverlapping(ctx, id, countFrom, now)
		if err != nil {
			return nil, nil, err
		}
		for _, ss := range sessions {
			iv := interval{from: ss.Since}
			if ss.Until != nil {
				iv.until = *ss.Until
			}
			ivs = append(ivs, iv)
		}
	}
	netTime := estimateClock(anchorT, countFrom, now, ivs, openUntil)
	c := &clockJSON{NetTime: netTime, At: rfc3339(now), Running: running, Source: source}

	biomes, home, err := ws.biomeSummary(s.biomeGrids)
	if err != nil {
		// No grid this time (it is retried on the next card): weather for
		// the home fallback only, rather than failing the card.
		s.warnGridFailure(id, ws.snap.World.Seed, ws.snap.World.GenVersion, err)
	}
	return c, weatherFor(netTime, biomes, home), nil
}

// latestBootSince is the time of server id's newest server_boot or
// server_starting event at or after since; ok is false if there is none.
func (s *server) latestBootSince(ctx context.Context, id string, since time.Time) (time.Time, bool, error) {
	var boot time.Time
	found := false
	for _, typ := range []string{logwatch.EvServerBoot, logwatch.EvServerStarting} {
		e, ok, err := s.Store.LatestEventOfTypeSince(ctx, id, typ, since)
		if err != nil {
			return time.Time{}, false, err
		}
		if ok && (!found || e.At.After(boot)) {
			boot, found = e.At, true
		}
	}
	return boot, found, nil
}

// openSessionsUntil is whether the clock is running and until when the open
// sessions count (zero: not at all). Open sessions alone run it until
// now, unless a players_now reading newer than every one of them says
// nobody is connected: then they are dangling (a missed leave line) and
// count only until that reading. A reading from before the newest join
// is ignored, as the count lags the join.
func (s *server) openSessionsUntil(ctx context.Context, id string, online []store.Session, now time.Time) (bool, time.Time, error) {
	if len(online) == 0 {
		return false, time.Time{}, nil
	}
	newest := online[0].Since
	for _, o := range online[1:] {
		newest = maxT(newest, o.Since)
	}
	pe, ok, err := s.Store.LatestEventOfTypeSince(ctx, id, logwatch.EvPlayersNow, newest.Add(time.Millisecond))
	if err != nil {
		return false, time.Time{}, err
	}
	if !ok {
		return true, now, nil
	}
	var ev logwatch.Event
	if err := json.Unmarshal(pe.Body, &ev); err != nil {
		return false, time.Time{}, err
	}
	if ev.Players != nil && *ev.Players == 0 {
		return false, pe.At, nil
	}
	return true, now, nil
}

// gridKey names a world's biome grid.
type gridKey struct{ seed, gen int32 }

// warnGridFailure logs a biome grid that wouldn't build, once per world:
// the card retries it on every poll.
func (s *server) warnGridFailure(id string, seed, gen int32, err error) {
	if _, seen := s.gridWarned.LoadOrStore(gridKey{seed, gen}, struct{}{}); seen {
		return
	}
	s.Log.Warn("server: biome grid for the card's weather", "server", id, "seed", seed, "gen", gen, "err", err)
}
