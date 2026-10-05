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
// count once. Open intervals run until now when openNow (someone is
// online now); otherwise they are stale and count nothing.
func estimateClock(anchorT float64, anchorAt, now time.Time, online []interval, openNow bool) float64 {
	var spans []interval
	for _, iv := range online {
		until := iv.until
		if until.IsZero() {
			if !openNow {
				continue
			}
			until = now
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
func (s *server) clockAndWeather(ctx context.Context, id string, ws *worldState, online []store.Session) (*clockJSON, *weatherJSON, error) {
	now := s.Now()
	anchorT, anchorAt, source := ws.snap.World.NetTime, ws.snap.SavedAt, "save"
	se, ok, err := s.Store.LatestEventOfType(ctx, id, logwatch.EvTimeSkip)
	if err != nil {
		return nil, nil, err
	}
	if ok {
		var ev logwatch.Event
		if err := json.Unmarshal(se.Body, &ev); err != nil {
			return nil, nil, err
		}
		if wake := se.At.Add(sleepWake); wake.After(anchorAt) {
			anchorT, anchorAt, source = ev.To, wake, "sleep"
		}
	}

	running := len(online) > 0
	var ivs []interval
	if now.After(anchorAt) {
		sessions, err := s.Store.SessionsOverlapping(ctx, id, anchorAt, now)
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
	netTime := estimateClock(anchorT, anchorAt, now, ivs, running)
	c := &clockJSON{NetTime: netTime, At: rfc3339(now), Running: running, Source: source}

	biomes, home, err := ws.biomeSummary(s.biomeGrids)
	if err != nil {
		// No grid this time (it is retried on the next card): weather for
		// the home fallback only, rather than failing the card.
		s.Log.Warn("server: biome grid for the card's weather", "server", id, "err", err)
	}
	return c, weatherFor(netTime, biomes, home), nil
}
