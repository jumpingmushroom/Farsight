package server

import (
	"context"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/jumpingmushroom/farsight/internal/config"
	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/store"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
)

// profileDays is how many local days the profile's chart covers; "this
// week" and deaths "this week" use the same days.
const profileDays = 7

type profileDayJSON struct {
	Date    string `json:"date"` // local date, "2026-10-05"
	Seconds int64  `json:"seconds"`
}

type profileBedsJSON struct {
	Count int      `json:"count"`
	Near  []string `json:"near"` // nearest base name (or the biome) per bed, deduplicated
}

type profileBaseJSON struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Pieces int     `json:"pieces"`
	Biome  string  `json:"biome"`
	X      float32 `json:"x"`
	Z      float32 `json:"z"`
}

type profilePortalJSON struct {
	ID     string  `json:"id"`
	Tag    string  `json:"tag"`
	Paired bool    `json:"paired"`
	X      float32 `json:"x"`
	Z      float32 `json:"z"`
}

type profileTameJSON struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Species string  `json:"species"`
	X       float32 `json:"x"`
	Z       float32 `json:"z"`
}

type profileTombJSON struct {
	ID        string  `json:"id"`
	Biome     string  `json:"biome"`
	FirstSeen string  `json:"firstSeen"` // the save time it first appeared in
	X         float32 `json:"x"`
	Z         float32 `json:"z"`
}

type profileDeathsJSON struct {
	Spotted    int               `json:"spotted"` // distinct tombstones across every save seen
	Week       int               `json:"week"`    // of those, first seen in the profile's 7 days
	Tombstones []profileTombJSON `json:"tombstones"`
}

// profileJSON is GET /api/servers/{id}/players/{player}. Save data (beds,
// bases, portals, tames, tombstones) comes from the latest save, filtered
// to explored ground like the snapshot API.
type profileJSON struct {
	ID           string              `json:"id"` // the platform ID
	Name         string              `json:"name"`
	Platform     string              `json:"platform"`
	TimeZone     string              `json:"timeZone"`
	Online       bool                `json:"online"`
	Since        string              `json:"since,omitempty"`    // the open session's start
	LastSeen     string              `json:"lastSeen,omitempty"` // the latest session's end, when offline
	FirstSeen    string              `json:"firstSeen"`
	TrackedSince string              `json:"trackedSince"`
	WeekSeconds  int64               `json:"weekSeconds"`
	AllSeconds   int64               `json:"allSeconds"`
	Sessions     int                 `json:"sessions"`
	Days         []profileDayJSON    `json:"days"`
	Beds         profileBedsJSON     `json:"beds"`
	Bases        []profileBaseJSON   `json:"bases"`
	Portals      []profilePortalJSON `json:"portals"`
	Tames        []profileTameJSON   `json:"tames"`
	Deaths       profileDeathsJSON   `json:"deaths"`
}

func (s *server) profile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, ok := s.unlocked(r, id)
	if !ok {
		notFound(w)
		return
	}
	p, ok, err := s.buildProfile(r.Context(), srv, r.PathValue("player"))
	if err != nil {
		s.internalError(w, "profile", id, err)
		return
	}
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// buildProfile assembles playerID's profile; ok is false if the server has
// never seen that platform ID.
func (s *server) buildProfile(ctx context.Context, srv *config.Server, playerID string) (*profileJSON, bool, error) {
	sessions, err := s.Store.PlayerSessions(ctx, srv.ID, playerID)
	if err != nil || len(sessions) == 0 {
		return nil, false, err
	}
	now, loc := s.Now(), srv.Location()
	last := sessions[len(sessions)-1]
	p := &profileJSON{
		ID: playerID, Name: last.Name, Platform: last.Platform, TimeZone: loc.String(),
		FirstSeen: rfc3339(sessions[0].Since), Sessions: len(sessions),
		AllSeconds: totalSeconds(sessions, now),
		Beds:       profileBedsJSON{Near: []string{}},
		Bases:      []profileBaseJSON{}, Portals: []profilePortalJSON{}, Tames: []profileTameJSON{},
		Deaths: profileDeathsJSON{Tombstones: []profileTombJSON{}},
	}
	names := map[string]bool{} // every name the platform ID has played under
	var since, lastSeen time.Time
	for _, ss := range sessions {
		names[ss.Name] = true
		switch {
		case ss.Until == nil:
			p.Online = true
			if ss.Since.After(since) {
				since = ss.Since
			}
		case ss.Until.After(lastSeen):
			lastSeen = *ss.Until
		}
	}
	if p.Online {
		p.Since = rfc3339(since)
	} else {
		p.LastSeen = rfc3339(lastSeen)
	}

	starts, _ := localDays(now, loc, profileDays)
	for i, sec := range dayTotals(sessions, now, loc, profileDays) {
		p.Days = append(p.Days, profileDayJSON{Date: starts[i].Format(time.DateOnly), Seconds: sec})
		p.WeekSeconds += sec
	}
	p.TrackedSince = p.FirstSeen
	if first, ok, err := s.Store.EarliestEvent(ctx, srv.ID); err != nil {
		return nil, false, err
	} else if ok {
		p.TrackedSince = rfc3339(first)
	}

	// Every distinct tombstone of theirs ever seen in a save.
	var tombs []store.Tombstone
	for n := range names {
		ts, err := s.Store.Tombstones(ctx, srv.ID, n)
		if err != nil {
			return nil, false, err
		}
		tombs = append(tombs, ts...)
	}
	p.Deaths.Spotted = len(tombs)
	for _, t := range tombs {
		if !t.FirstSeen.Before(starts[0]) {
			p.Deaths.Week++
		}
	}

	ws, ok, err := s.worlds.get(ctx, srv.ID)
	if err != nil {
		return nil, false, err
	}
	if ok {
		s.addSaveData(p, ws, names, last.Platform+"_"+playerID, tombs)
	}
	return p, true, nil
}

// addSaveData fills in the profile's beds, bases, portals, tames and
// current tombstones from the latest save. Beds and tombstones link by
// owner name, bases by builder name, portals by creator (resolved to a
// name by the agent) and tames by namer, which is the platform user ID
// itself.
func (s *server) addSaveData(p *profileJSON, ws *worldState, names map[string]bool, namer string, tombs []store.Tombstone) {
	snap := ws.snap
	geo := worldevents.NewGeo(snap, ws.mask)
	in := func(x, z float32) bool { return ws.mask.At(float64(x), float64(z)) }
	byID := make(map[string]extract.Marker, len(snap.Markers))
	for _, m := range snap.Markers {
		byID[m.ID] = m
	}
	var bases []extract.Base
	for _, b := range snap.Bases {
		if !in(b.X, b.Z) {
			continue
		}
		bases = append(bases, b)
		for _, bl := range b.Builders {
			if names[bl.Name] {
				p.Bases = append(p.Bases, profileBaseJSON{ID: b.ID, Name: b.Name, Pieces: b.Pieces, Biome: geo.Biome(b.X, b.Z), X: b.X, Z: b.Z})
				break
			}
		}
	}
	for _, m := range snap.Markers {
		if !in(m.X, m.Z) {
			continue
		}
		switch {
		case m.Kind == "bed" && names[m.Owner]:
			p.Beds.Count++
			near := geo.Biome(m.X, m.Z)
			if b, ok := nearestBase(bases, m.X, m.Z); ok {
				near = b.Name
			}
			if !slices.Contains(p.Beds.Near, near) {
				p.Beds.Near = append(p.Beds.Near, near)
			}
		case m.Kind == "portal" && names[m.Owner]:
			// Paired only when the partner is explored too, as the snapshot
			// API unpairs: an unexplored partner stays secret.
			partner, ok := byID[m.Pair]
			paired := ok && in(partner.X, partner.Z)
			p.Portals = append(p.Portals, profilePortalJSON{ID: m.ID, Tag: m.Label, Paired: paired, X: m.X, Z: m.Z})
		case m.Kind == "tame" && m.Namer != "" && m.Namer == namer:
			p.Tames = append(p.Tames, profileTameJSON{ID: m.ID, Name: m.Label, Species: m.Species, X: m.X, Z: m.Z})
		case m.Kind == "tombstone" && names[m.Owner]:
			first := snap.SavedAt
			for _, t := range tombs {
				if t.Owner == m.Owner && math.Hypot(float64(t.X-m.X), float64(t.Z-m.Z)) <= worldevents.MatchRadius && t.FirstSeen.Before(first) {
					first = t.FirstSeen
				}
			}
			p.Deaths.Tombstones = append(p.Deaths.Tombstones, profileTombJSON{ID: m.ID, Biome: geo.Biome(m.X, m.Z), FirstSeen: rfc3339(first), X: m.X, Z: m.Z})
		}
	}
}

// nearestBase is the base whose centre is nearest (x, z), within
// worldevents.NearRadius.
func nearestBase(bases []extract.Base, x, z float32) (extract.Base, bool) {
	var best extract.Base
	bestD, found := 0.0, false
	for _, b := range bases {
		d := math.Hypot(float64(b.X-x), float64(b.Z-z))
		if d <= worldevents.NearRadius && (!found || d < bestD) {
			best, bestD, found = b, d, true
		}
	}
	return best, found
}
