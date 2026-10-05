package server

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/jumpingmushroom/farsight/internal/auth"
	"github.com/jumpingmushroom/farsight/internal/config"
	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/live"
	"github.com/jumpingmushroom/farsight/internal/store"
	"github.com/jumpingmushroom/farsight/internal/tileset"
)

const (
	maxUnlockBody = 4 << 10
	recentWindow  = 72 * time.Hour
	recentLimit   = 50
	activityLimit = 50
	saveTimesN    = 10
)

// saveInterval is the median gap, in whole seconds, between consecutive
// save times (the mean of the two middle gaps, rounded down, for an even
// count). ok is false with fewer than 3 times.
func saveInterval(times []time.Time) (int, bool) {
	if len(times) < 3 {
		return 0, false
	}
	gaps := make([]int, 0, len(times)-1)
	for i := 1; i < len(times); i++ {
		d := times[i-1].Sub(times[i])
		if d < 0 {
			d = -d
		}
		gaps = append(gaps, int(d/time.Second))
	}
	slices.Sort(gaps)
	m := len(gaps) / 2
	if len(gaps)%2 == 1 {
		return gaps[m], true
	}
	return (gaps[m-1] + gaps[m]) / 2, true
}

// dummyCost is the highest bcrypt cost among the configured passphrase
// hashes (bcrypt.DefaultCost if none parse), so comparing against the
// dummy costs as much as comparing against the dearest real hash.
func dummyCost(cfg *config.Config) int {
	return maxCost(cfg, func(s *config.Server) string { return s.PassphraseHash })
}

// dummyTokenCost is dummyCost for the agent token hashes.
func dummyTokenCost(cfg *config.Config) int {
	return maxCost(cfg, func(s *config.Server) string { return s.AgentTokenHash })
}

// maxCost is the highest bcrypt cost among the hashes field picks from
// the configured servers, or bcrypt.DefaultCost if none parse.
func maxCost(cfg *config.Config, field func(*config.Server) string) int {
	best := 0
	if cfg != nil {
		for i := range cfg.Servers {
			if c, err := bcrypt.Cost([]byte(field(&cfg.Servers[i]))); err == nil && c > best {
				best = c
			}
		}
	}
	if best == 0 {
		return bcrypt.DefaultCost
	}
	return best
}

// newDummyHash returns a function yielding a bcrypt hash of a random
// password at cost, generated once on first call.
func newDummyHash(cost int) func() []byte {
	return sync.OnceValue(func() []byte {
		pw := make([]byte, 32)
		// crypto/rand.Read never returns an error (Go >= 1.24 panics
		// instead of failing), so its result is safe to ignore.
		rand.Read(pw)
		h, err := bcrypt.GenerateFromPassword(pw, cost)
		if err != nil {
			panic(err)
		}
		return h
	})
}

func (s *server) unlock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Server     string `json:"server"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxUnlockBody)).Decode(&req); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "malformed body")
		return
	}

	// Reserve a token before the (slow) compare, so concurrent attempts
	// can't all slip past the limit before any failure is charged. A
	// success refunds it; a failure keeps the charge.
	ip := s.clientIP(r)
	if !s.Limiter.Allow(ip) {
		writeError(w, http.StatusTooManyRequests, "too many attempts")
		return
	}

	srv, known := s.lookup(req.Server)
	hash := s.dummyHash()
	if known {
		hash = []byte(srv.PassphraseHash)
	}
	match, err := compareHash(r.Context(), hash, []byte(req.Passphrase))
	if err != nil {
		// Cancelled while queued for a compare: nothing was checked.
		s.Limiter.Refund(ip)
		writeError(w, http.StatusServiceUnavailable, "busy")
		return
	}
	if !known || !match {
		writeError(w, http.StatusUnauthorized, "wrong passphrase")
		return
	}
	s.Limiter.Refund(ip)

	// Keep only ids still in the config, so removed servers drop out.
	ids := []string{srv.ID}
	for _, id := range s.unlockedIDs(r) {
		if _, ok := s.lookup(id); ok {
			ids = append(ids, id)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    s.Codec.Encode(ids),
		Path:     "/",
		MaxAge:   int(auth.CookieMaxAge / time.Second),
		HttpOnly: true,
		Secure:   s.Config.CookieSecure == nil || *s.Config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

type serverSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Players    int    `json:"players"`
	MaxPlayers int    `json:"maxPlayers"`
}

// liveState returns the server's live row (zero if none), its read-time
// status and the player count to show.
func (s *server) liveState(r *http.Request, id string) (store.Live, string, int, error) {
	l, _, err := s.Store.GetLive(r.Context(), nil, id)
	if err != nil {
		return store.Live{}, "", 0, err
	}
	status := live.Status(l, s.Now())
	players := l.Players
	if status == "offline" || status == "unknown" {
		players = 0
	}
	return l, status, players, nil
}

func (s *server) listServers(w http.ResponseWriter, r *http.Request) {
	unlocked := s.unlockedIDs(r)
	out := []serverSummary{}
	for i := range s.Config.Servers {
		srv := &s.Config.Servers[i]
		if !slices.Contains(unlocked, srv.ID) {
			continue
		}
		_, status, players, err := s.liveState(r, srv.ID)
		if err != nil {
			s.internalError(w, "list servers", srv.ID, err)
			return
		}
		out = append(out, serverSummary{ID: srv.ID, Name: srv.Name, Status: status, Players: players, MaxPlayers: srv.MaxPlayers})
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": out})
}

type onlineJSON struct {
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	PlatformID string `json:"platformId"`
	Since      string `json:"since"`
}

type recentJSON struct {
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	PlatformID string `json:"platformId"`
	Since      string `json:"since"`
	Until      string `json:"until"`
	Seconds    int64  `json:"seconds"`
}

type worldJSON struct {
	Name            string            `json:"name"`
	SeedName        string            `json:"seedName"`
	Day             int               `json:"day"`
	Bosses          []extract.Boss    `json:"bosses"`
	Modifiers       map[string]string `json:"modifiers"`
	Flags           []string          `json:"flags"`
	ExploredPct     float64           `json:"exploredPct"`
	SavedAt         string            `json:"savedAt"`
	ReadAt          string            `json:"readAt"`
	SaveIntervalSec *int              `json:"saveIntervalSec,omitempty"`
}

type tilesJSON struct {
	State string `json:"state"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
	Key   string `json:"key,omitempty"`
}

type cardJSONOut struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Crossplay      bool         `json:"crossplay"`
	Address        string       `json:"address,omitempty"`
	DiscordHint    string       `json:"discordHint,omitempty"`
	MaxPlayers     int          `json:"maxPlayers"`
	Status         string       `json:"status"`
	Version        string       `json:"version,omitempty"`
	NetworkVersion int          `json:"networkVersion,omitempty"`
	UpSince        string       `json:"upSince,omitempty"`
	LastHeartbeat  string       `json:"lastHeartbeat,omitempty"`
	Players        int          `json:"players"`
	JoinCode       string       `json:"joinCode,omitempty"`
	JoinCodeAt     string       `json:"joinCodeAt,omitempty"`
	TimeZone       string       `json:"timeZone"`
	Online         []onlineJSON `json:"online"`
	Recent         []recentJSON `json:"recent"`
	Activity       []eventJSON  `json:"activity"`
	World          *worldJSON   `json:"world,omitempty"`
	Tiles          tilesJSON    `json:"tiles"`
}

func (s *server) internalError(w http.ResponseWriter, what, id string, err error) {
	s.Log.Error("server: "+what, "server", id, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func (s *server) card(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, ok := s.unlocked(r, id)
	if !ok {
		notFound(w)
		return
	}
	c, err := s.buildCard(r, srv)
	if err != nil {
		s.internalError(w, "card", id, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *server) buildCard(r *http.Request, srv *config.Server) (*cardJSONOut, error) {
	ctx := r.Context()
	l, status, players, err := s.liveState(r, srv.ID)
	if err != nil {
		return nil, err
	}
	c := &cardJSONOut{
		ID: srv.ID, Name: srv.Name, Crossplay: srv.Crossplay, Address: srv.Address,
		DiscordHint: srv.DiscordHint, MaxPlayers: srv.MaxPlayers,
		Status: status, Version: l.Version, NetworkVersion: l.NetworkVersion,
		UpSince: optTime(l.UpSince), LastHeartbeat: optTime(l.LastHeartbeat),
		Players: players, JoinCode: l.JoinCode, JoinCodeAt: optTime(l.JoinCodeAt),
		TimeZone: srv.Location().String(),
		Online:   []onlineJSON{}, Recent: []recentJSON{}, Activity: []eventJSON{},
		Tiles: tilesJSON{State: string(tileset.StateNone)},
	}

	online, err := s.Store.Online(ctx, srv.ID)
	if err != nil {
		return nil, err
	}
	for _, o := range online {
		c.Online = append(c.Online, onlineJSON{Name: o.Name, Platform: o.Platform, PlatformID: o.PlatformID, Since: rfc3339(o.Since)})
	}

	recent, err := s.Store.Recent(ctx, srv.ID, s.Now().Add(-recentWindow), recentLimit)
	if err != nil {
		return nil, err
	}
	for _, o := range recent {
		rj := recentJSON{Name: o.Name, Platform: o.Platform, PlatformID: o.PlatformID, Since: rfc3339(o.Since), Seconds: o.Seconds}
		if o.Until != nil {
			rj.Until = rfc3339(*o.Until)
		}
		c.Recent = append(c.Recent, rj)
	}

	ws, ok, err := s.worlds.get(ctx, srv.ID)
	if err != nil {
		return nil, err
	}
	events, err := s.Store.RecentEvents(ctx, srv.ID, activityLimit)
	if err != nil {
		return nil, err
	}
	who, err := s.people(ctx, srv.ID)
	if err != nil {
		return nil, err
	}
	var mask *explored.Mask
	if ok {
		mask = ws.mask
	}
	for _, se := range events {
		if e, shown := toEventJSON(se, who, mask); shown {
			c.Activity = append(c.Activity, e)
		}
	}
	if !ok {
		return c, nil
	}
	snap := ws.snap
	wj := &worldJSON{
		Name: snap.World.Name, SeedName: snap.World.SeedName, Day: snap.World.Day,
		Bosses: cardBosses(snap), Modifiers: snap.World.Modifiers, Flags: snap.World.Flags,
		ExploredPct: ws.pct,
		SavedAt:     rfc3339(snap.SavedAt), ReadAt: rfc3339(snap.ReadAt),
	}
	if wj.Bosses == nil {
		wj.Bosses = []extract.Boss{}
	}
	if wj.Modifiers == nil {
		wj.Modifiers = map[string]string{}
	}
	if wj.Flags == nil {
		wj.Flags = []string{}
	}
	saves, err := s.Store.SaveTimes(ctx, srv.ID, saveTimesN)
	if err != nil {
		return nil, err
	}
	if n, ok := saveInterval(saves); ok {
		wj.SaveIntervalSec = &n
	}
	c.World = wj

	st := s.Tiles.Status(snap.World.Seed, snap.World.GenVersion)
	c.Tiles = tilesJSON{State: string(st.State), Done: st.Done, Total: st.Total, Key: st.Key}
	return c, nil
}

// snapshotJSON is GET /api/servers/{id}/snapshot. Markers, locations and
// bases are filtered to the explored mask (the cell under each point);
// players carry no positions. explored is the mask itself, for the
// browser's cursor readout, and fogKey names its fog tiles. The
// snapshot's exploredZones is ingest-only: it feeds the mask for agents
// that predate explored, and never reaches the browser.
type snapshotJSON struct {
	SavedAt   string           `json:"savedAt"`
	FogKey    string           `json:"fogKey"`
	Explored  explored.Encoded `json:"explored"`
	Markers   []extract.Marker `json:"markers"`
	Locations []extract.Marker `json:"locations"`
	Bases     []extract.Base   `json:"bases"`
	Players   []extract.Player `json:"players"`
}

func (s *server) snapshot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.unlocked(r, id); !ok {
		notFound(w)
		return
	}
	ws, ok, err := s.worlds.get(r.Context(), id)
	if err != nil {
		s.internalError(w, "snapshot", id, err)
		return
	}
	if !ok {
		notFound(w)
		return
	}
	snap := ws.snap
	in := func(x, z float32) bool { return ws.mask.At(float64(x), float64(z)) }
	inMarker := func(m extract.Marker) bool { return in(m.X, m.Z) }
	markers := keep(snap.Markers, inMarker)
	unpair(markers)
	writeJSON(w, http.StatusOK, snapshotJSON{
		SavedAt:   rfc3339(snap.SavedAt),
		FogKey:    ws.fogKey,
		Explored:  ws.enc,
		Markers:   markers,
		Locations: keep(snap.Locations, inMarker),
		Bases:     keep(snap.Bases, func(b extract.Base) bool { return in(b.X, b.Z) }),
		Players:   orEmpty(snap.Players),
	})
}

// keep returns the elements of s that pass, as a new, never-nil slice.
func keep[T any](s []T, pass func(T) bool) []T {
	out := []T{}
	for _, v := range s {
		if pass(v) {
			out = append(out, v)
		}
	}
	return out
}

// unpair blanks Pair, in place, on any marker whose named partner isn't
// also in markers: a kept portal whose partner was filtered out as
// unexplored must not reveal that the partner exists.
func unpair(markers []extract.Marker) {
	kept := make(map[string]bool, len(markers))
	for _, m := range markers {
		kept[m.ID] = true
	}
	for i := range markers {
		if markers[i].Pair != "" && !kept[markers[i].Pair] {
			markers[i].Pair = ""
		}
	}
}

// orEmpty turns a nil slice into an empty one so it encodes as [].
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
