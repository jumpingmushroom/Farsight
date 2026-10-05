package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/jumpingmushroom/farsight/internal/config"
	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
	"github.com/jumpingmushroom/farsight/internal/store"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
)

const (
	// activityDays is the timeline's default (and "Show earlier") window.
	activityDays = 3
	// maxActivityDays caps ?days=.
	maxActivityDays = 14
)

// Categories, the timeline's chips, in the design's order.
var categories = []string{"session", "death", "boss", "build", "portal", "tame", "event", "server"}

var categoryOf = map[string]string{
	logwatch.EvPlayerJoin:        "session",
	logwatch.EvPlayerLeave:       "session",
	logwatch.EvServerStarting:    "server",
	logwatch.EvServerBoot:        "server",
	logwatch.EvServerReady:       "server",
	logwatch.EvServerStopped:     "server",
	logwatch.EvWorldSaved:        "server",
	logwatch.EvJoinCode:          "server",
	logwatch.EvRaid:              "event",
	worldevents.TypeTombstone:    "death",
	worldevents.TypePortal:       "portal",
	worldevents.TypePortalPaired: "portal",
	worldevents.TypeTame:         "tame",
	worldevents.TypeBaseNew:      "build",
	worldevents.TypeBaseGrew:     "build",
	worldevents.TypeBoss:         "boss",
}

// worldTypes are the world-save event types. world_saved, despite its
// name, is the server log's autosave line.
var worldTypes = map[string]bool{
	worldevents.TypeTombstone:    true,
	worldevents.TypePortal:       true,
	worldevents.TypePortalPaired: true,
	worldevents.TypeTame:         true,
	worldevents.TypeBaseNew:      true,
	worldevents.TypeBaseGrew:     true,
	worldevents.TypeBoss:         true,
}

// eventJSON is one activity entry, in the card's activity list and the
// timeline alike. Log events carry their exact time, world-save events the
// save's. Who lists the platform IDs of the people it concerns (the
// people filter); x and z are set only for a place in explored ground.
type eventJSON struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Category string   `json:"category"`
	Source   string   `json:"source"` // "log" or "save"
	At       string   `json:"at"`
	Who      []string `json:"who"`

	Name       string `json:"name,omitempty"` // the player (log), the tame or base (save)
	Platform   string `json:"platform,omitempty"`
	PlatformID string `json:"platformId,omitempty"`
	Code       string `json:"code,omitempty"`
	Players    *int   `json:"players,omitempty"`
	Seconds    int64  `json:"seconds,omitempty"`
	Version    string `json:"version,omitempty"`
	Raid       string `json:"raid,omitempty"`

	Owner   string   `json:"owner,omitempty"`
	Tag     string   `json:"tag,omitempty"`
	Paired  bool     `json:"paired,omitempty"`
	Species string   `json:"species,omitempty"`
	Pieces  int      `json:"pieces,omitempty"`
	Grew    int      `json:"grew,omitempty"`
	Boss    string   `json:"boss,omitempty"`
	Biome   string   `json:"biome,omitempty"`
	Near    string   `json:"near,omitempty"`
	X       *float32 `json:"x,omitempty"`
	Z       *float32 `json:"z,omitempty"`
}

// people resolves the players an event concerns to platform IDs: by name
// (every name a platform ID has played under) and by platform user ID
// ("Steam_…", a tame's namer).
type people struct {
	byName map[string][]string
	byUser map[string]string
	list   []store.Player
}

func (s *server) people(ctx context.Context, serverID string) (*people, error) {
	ps, err := s.Store.Players(ctx, serverID)
	if err != nil {
		return nil, err
	}
	p := &people{byName: map[string][]string{}, byUser: map[string]string{}, list: ps}
	for _, pl := range ps {
		for _, n := range pl.Names {
			p.byName[n] = append(p.byName[n], pl.PlatformID)
		}
		p.byUser[pl.Platform+"_"+pl.PlatformID] = pl.PlatformID
	}
	return p, nil
}

func (p *people) names(ns ...string) []string {
	out := []string{}
	for _, n := range ns {
		for _, id := range p.byName[n] {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	return out
}

// toEventJSON decodes one stored event; ok is false for a type the
// activity views don't show, or for a world-save event whose own point
// lies in unexplored ground under mask: that event is dropped entirely
// (not merely stripped of x/z), so its type, owner and category count
// never reach the browser before the place is explored. mask is nil
// without a save, which is treated the same as unexplored. Events with no
// position (joins, leaves, server events, raids, boss kills) are
// unaffected.
func toEventJSON(se store.StoredEvent, who *people, mask *explored.Mask) (eventJSON, bool) {
	cat, ok := categoryOf[se.Type]
	if !ok {
		return eventJSON{}, false
	}
	out := eventJSON{ID: se.ID, Type: se.Type, Category: cat, Source: "log", At: rfc3339(se.At), Who: []string{}}
	if worldTypes[se.Type] {
		var e worldevents.Event
		if json.Unmarshal(se.Body, &e) != nil {
			return eventJSON{}, false
		}
		if e.Pos != nil {
			if mask == nil || !mask.At(float64(e.Pos.X), float64(e.Pos.Z)) {
				return eventJSON{}, false
			}
			x, z := e.Pos.X, e.Pos.Z
			out.X, out.Z = &x, &z
		}
		out.Source = "save"
		out.Owner, out.Tag, out.Paired, out.Name, out.Species = e.Owner, e.Tag, e.Paired, e.Name, e.Species
		out.Pieces, out.Grew, out.Boss, out.Biome, out.Near = e.Pieces, e.Grew, e.Boss, e.Biome, e.Near
		switch {
		case e.Namer != "":
			if id, ok := who.byUser[e.Namer]; ok {
				out.Who = []string{id}
			}
		case len(e.Builders) > 0:
			out.Who = who.names(e.Builders...)
		case e.Owner != "":
			out.Who = who.names(e.Owner)
		}
		return out, true
	}
	var e logwatch.Event
	if json.Unmarshal(se.Body, &e) != nil {
		return eventJSON{}, false
	}
	out.Name, out.Platform, out.PlatformID, out.Code = e.Name, e.Platform, e.PlatformID, e.Code
	out.Players, out.Seconds, out.Version, out.Raid = e.Players, e.Seconds, e.Version, e.Raid
	if e.PlatformID != "" {
		out.Who = []string{e.PlatformID}
	}
	return out, true
}

type personJSON struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
}

// activityJSONOut is GET /api/servers/{id}/activity: the events in
// [from, until), newest first, with consecutive autosaves collapsed to the
// newest, their counts per category, every player the server has seen
// (online first, then most recently seen) and when tracking began.
type activityJSONOut struct {
	TimeZone string         `json:"timeZone"`
	From     string         `json:"from"`
	Until    string         `json:"until"`
	Earliest string         `json:"earliest,omitempty"`
	Events   []eventJSON    `json:"events"`
	Counts   map[string]int `json:"counts"`
	People   []personJSON   `json:"people"`
}

// activityWindow is [from, until): until is before (or, without one, the
// end of today), and from the local midnight days-1 days before the day
// that ends at until.
func activityWindow(before time.Time, now time.Time, loc *time.Location, days int) (from, until time.Time) {
	if before.IsZero() {
		_, until = localDays(now, loc, 1)
	} else {
		until = before
	}
	starts, _ := localDays(until.Add(-time.Nanosecond), loc, days)
	return starts[0], until
}

func (s *server) activity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, ok := s.unlocked(r, id)
	if !ok {
		notFound(w)
		return
	}
	q := r.URL.Query()
	var before time.Time
	if v := q.Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad before")
			return
		}
		before = t
	}
	days := activityDays
	if v := q.Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxActivityDays {
			writeError(w, http.StatusBadRequest, "bad days")
			return
		}
		days = n
	}
	out, err := s.buildActivity(r.Context(), srv, before, days)
	if err != nil {
		s.internalError(w, "activity", id, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) buildActivity(ctx context.Context, srv *config.Server, before time.Time, days int) (*activityJSONOut, error) {
	loc := srv.Location()
	from, until := activityWindow(before, s.Now(), loc, days)
	out := &activityJSONOut{TimeZone: loc.String(), From: rfc3339(from), Until: rfc3339(until),
		Events: []eventJSON{}, Counts: map[string]int{}, People: []personJSON{}}
	for _, c := range categories {
		out.Counts[c] = 0
	}
	if first, ok, err := s.Store.EarliestEvent(ctx, srv.ID); err != nil {
		return nil, err
	} else if ok {
		out.Earliest = rfc3339(first)
	}
	who, err := s.people(ctx, srv.ID)
	if err != nil {
		return nil, err
	}
	for _, p := range who.list {
		out.People = append(out.People, personJSON{ID: p.PlatformID, Name: p.Name, Online: p.Online})
	}
	mask, err := s.currentMask(ctx, srv.ID)
	if err != nil {
		return nil, err
	}
	stored, err := s.Store.EventsBetween(ctx, srv.ID, from, until)
	if err != nil {
		return nil, err
	}
	for _, se := range stored {
		e, ok := toEventJSON(se, who, mask)
		if !ok {
			continue
		}
		// Autosaves come every ~20 minutes: a run of them with nothing in
		// between shows as its newest.
		if n := len(out.Events); n > 0 && e.Type == logwatch.EvWorldSaved && out.Events[n-1].Type == logwatch.EvWorldSaved {
			continue
		}
		out.Events = append(out.Events, e)
		out.Counts[e.Category]++
	}
	return out, nil
}

// currentMask is the latest save's explored mask, or nil without a save.
func (s *server) currentMask(ctx context.Context, serverID string) (*explored.Mask, error) {
	ws, ok, err := s.worlds.get(ctx, serverID)
	if err != nil || !ok {
		return nil, err
	}
	return ws.mask, nil
}

type spanJSON struct {
	Since string `json:"since"`
	Until string `json:"until,omitempty"` // absent while the session is open
}

type todayPlayerJSON struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Online bool       `json:"online"`
	Spans  []spanJSON `json:"spans"`
}

// todayJSON is GET /api/servers/{id}/sessions/today: who played today in
// the server's local time, each session clipped to [dayStart, now].
type todayJSON struct {
	TimeZone string            `json:"timeZone"`
	DayStart string            `json:"dayStart"`
	DayEnd   string            `json:"dayEnd"`
	Now      string            `json:"now"`
	Players  []todayPlayerJSON `json:"players"`
}

func (s *server) sessionsToday(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, ok := s.unlocked(r, id)
	if !ok {
		notFound(w)
		return
	}
	now, loc := s.Now(), srv.Location()
	starts, end := localDays(now, loc, 1)
	sessions, err := s.Store.SessionsOverlapping(r.Context(), srv.ID, starts[0], end)
	if err != nil {
		s.internalError(w, "sessions today", id, err)
		return
	}
	writeJSON(w, http.StatusOK, todayJSON{
		TimeZone: loc.String(), DayStart: rfc3339(starts[0]), DayEnd: rfc3339(end), Now: rfc3339(now),
		Players: todayPlayers(sessions, starts[0], now),
	})
}

// todayPlayers groups sessions by player (platform ID, or the name for a
// session without one), clipping each to [dayStart, now], in the order
// each player first appears (sessions come oldest first). The player's
// name is the one on their latest session.
func todayPlayers(sessions []store.Session, dayStart, now time.Time) []todayPlayerJSON {
	out := []todayPlayerJSON{}
	index := map[string]int{}
	for _, ss := range sessions {
		key := "id:" + ss.PlatformID
		if ss.PlatformID == "" {
			key = "name:" + ss.Name
		}
		i, ok := index[key]
		if !ok {
			i = len(out)
			index[key] = i
			out = append(out, todayPlayerJSON{ID: ss.PlatformID, Spans: []spanJSON{}})
		}
		p := &out[i]
		p.Name = ss.Name
		since := ss.Since
		if since.Before(dayStart) {
			since = dayStart
		}
		sp := spanJSON{Since: rfc3339(since)}
		if ss.Until == nil {
			p.Online = true
		} else {
			u := *ss.Until
			if u.After(now) {
				u = now
			}
			sp.Until = rfc3339(u)
		}
		p.Spans = append(p.Spans, sp)
	}
	return out
}
