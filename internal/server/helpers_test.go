package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/jumpingmushroom/farsight/internal/auth"
	"github.com/jumpingmushroom/farsight/internal/config"
	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/ingest"
	"github.com/jumpingmushroom/farsight/internal/live"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
	"github.com/jumpingmushroom/farsight/internal/store"
	"github.com/jumpingmushroom/farsight/internal/tiles"
	"github.com/jumpingmushroom/farsight/internal/tileset"
)

var t0 = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

const (
	testSeed int32 = 12345
	testGen  int32 = 2
)

// fakeClock is a settable clock shared by the handler, applier, codec and
// limiter, so tests never sleep.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type env struct {
	t       *testing.T
	clock   *fakeClock
	cfg     *config.Config
	store   *store.Store
	tiles   *tileset.Manager
	srv     *httptest.Server
	release chan struct{} // closed to let the fake render finish
}

func mustHash(t *testing.T, s string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(s), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

func newEnv(t *testing.T) *env { t.Helper(); return newEnvBurst(t, 3) }

// newEnvBurst is newEnv with the unlock limiter's burst set to burst.
func newEnvBurst(t *testing.T, burst int) *env {
	t.Helper()
	clock := &fakeClock{t: t0}

	cfg := &config.Config{
		Servers: []config.Server{
			{ID: "alpha", Name: "Alpha", Address: "alpha.example:2456", Crossplay: true, DiscordHint: "#alpha", MaxPlayers: 10,
				PassphraseHash: mustHash(t, "alpha-pass"), AgentTokenHash: mustHash(t, "alpha-token")},
			{ID: "beta", Name: "Beta", MaxPlayers: 5,
				PassphraseHash: mustHash(t, "beta-pass"), AgentTokenHash: mustHash(t, "beta-token")},
		},
		CookieKey: []byte("0123456789abcdef0123456789abcdef"),
	}

	st, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	e := &env{t: t, clock: clock, cfg: cfg, store: st, release: make(chan struct{})}
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		progress(0, 1)
		select {
		case <-e.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		p := filepath.Join(dir, "0", "0", "0.png")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte("\x89PNG fake"), 0o644); err != nil {
			return err
		}
		progress(1, 1)
		return os.WriteFile(filepath.Join(dir, "complete"), []byte(strconv.Itoa(tiles.RenderVersion)), 0o644)
	}
	e.tiles = tileset.NewManager(t.TempDir(), render, nil)

	h := New(Deps{
		Config:  cfg,
		Store:   st,
		Applier: &live.Applier{Store: st, Now: clock.Now},
		Tiles:   e.tiles,
		Codec:   auth.Codec{Key: cfg.CookieKey, Now: clock.Now},
		Limiter: auth.NewLimiter(1, burst, clock.Now),
		Now:     clock.Now,
	})
	e.srv = httptest.NewServer(h)
	t.Cleanup(e.srv.Close)
	return e
}

// newEnvSplit is newEnv, but builds the public and ingest handlers
// separately (as SplitIngest does in production) from one shared Deps, so
// both handlers are backed by a single *server and its limiter, token
// cache and bcrypt semaphore. It returns the public env (e.srv is the
// public listener) plus a second httptest.Server for the ingest handler.
func newEnvSplit(t *testing.T) (e *env, ingestSrv *httptest.Server) {
	t.Helper()
	clock := &fakeClock{t: t0}

	cfg := &config.Config{
		Servers: []config.Server{
			{ID: "alpha", Name: "Alpha", Address: "alpha.example:2456", Crossplay: true, DiscordHint: "#alpha", MaxPlayers: 10,
				PassphraseHash: mustHash(t, "alpha-pass"), AgentTokenHash: mustHash(t, "alpha-token")},
		},
		CookieKey: []byte("0123456789abcdef0123456789abcdef"),
	}

	st, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	e = &env{t: t, clock: clock, cfg: cfg, store: st, release: make(chan struct{})}
	render := func(ctx context.Context, seed, gen int32, dir string, progress func(int, int)) error {
		<-ctx.Done()
		return ctx.Err()
	}
	e.tiles = tileset.NewManager(t.TempDir(), render, nil)

	pub, ing := NewHandlers(Deps{
		Config:      cfg,
		Store:       st,
		Applier:     &live.Applier{Store: st, Now: clock.Now},
		Tiles:       e.tiles,
		Codec:       auth.Codec{Key: cfg.CookieKey, Now: clock.Now},
		Limiter:     auth.NewLimiter(1, 3, clock.Now),
		Now:         clock.Now,
		SplitIngest: true,
	})
	e.srv = httptest.NewServer(pub)
	t.Cleanup(e.srv.Close)
	ingestSrv = httptest.NewServer(ing)
	t.Cleanup(ingestSrv.Close)
	return e, ingestSrv
}

// runTiles starts the tile manager's worker for the rest of the test.
func (e *env) runTiles() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.tiles.Run(ctx); close(done) }()
	e.t.Cleanup(func() { cancel(); <-done })
}

type resp struct {
	code   int
	header http.Header
	body   []byte
}

func (r resp) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

// do sends a request; cookie, if non-empty, is sent as the farsight cookie.
func (e *env) do(method, path string, body []byte, hdr map[string]string, cookie string) resp {
	e.t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{code: res.StatusCode, header: res.Header, body: b}
}

func (e *env) get(path, cookie string) resp { return e.do("GET", path, nil, nil, cookie) }

// unlock POSTs /api/unlock and returns the response plus the new cookie
// value (empty if none was set).
func (e *env) unlock(server, pass, cookie string) (resp, *http.Cookie) {
	e.t.Helper()
	b, _ := json.Marshal(map[string]string{"server": server, "passphrase": pass})
	r := e.do("POST", "/api/unlock", b, map[string]string{"Content-Type": "application/json"}, cookie)
	for _, c := range (&http.Response{Header: r.header}).Cookies() {
		if c.Name == auth.CookieName {
			return r, c
		}
	}
	return r, nil
}

// mustUnlock unlocks each server in turn, returning the final cookie value.
func (e *env) mustUnlock(ids ...string) string {
	e.t.Helper()
	cookie := ""
	for _, id := range ids {
		r, c := e.unlock(id, id+"-pass", cookie)
		if r.code != http.StatusNoContent || c == nil {
			e.t.Fatalf("unlock %s: %d %s", id, r.code, r.body)
		}
		cookie = c.Value
	}
	return cookie
}

// post sends a real agent-side ingest request (gzip JSON + bearer).
func (e *env) post(server, token, kind string, v any) error {
	return ingest.New(e.srv.URL, server, token).Post(context.Background(), kind, v)
}

func gzipBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return buf.Bytes()
}

// rawIngest posts body to /ingest/{server}/{kind} with the given headers.
func (e *env) rawIngest(server, kind, token string, body []byte, gz bool) resp {
	hdr := map[string]string{"Content-Type": "application/json"}
	if token != "" {
		hdr["Authorization"] = "Bearer " + token
	}
	if gz {
		hdr["Content-Encoding"] = "gzip"
	}
	return e.do("POST", "/ingest/"+server+"/"+kind, body, hdr, "")
}

// testSnapshot builds an alpha snapshot: a 30x30 block of explored zones
// at the origin plus one explored zone far outside the world radius, one
// location in an explored zone and one in an unexplored zone.
func testSnapshot(saveID string, savedAt time.Time) extract.Snapshot {
	var zones [][2]int16
	for x := int16(0); x < 30; x++ {
		for z := int16(0); z < 30; z++ {
			zones = append(zones, [2]int16{x, z})
		}
	}
	zones = append(zones, [2]int16{200, 0}) // outside 10 500 m: not counted
	return extract.Snapshot{
		ServerID: "alpha",
		SaveID:   saveID,
		SavedAt:  savedAt,
		ReadAt:   savedAt.Add(5 * time.Second),
		World: extract.WorldInfo{
			Name: "Midgard", SeedName: "abcdef", Seed: testSeed, GenVersion: testGen, Day: 42,
			Modifiers: map[string]string{"combat": "hard"}, Flags: []string{"nomap"},
		},
		Bosses: []extract.Boss{
			{Key: "defeated_eikthyr", Name: "Eikthyr", Defeated: true},
			{Key: "defeated_gdking", Name: "The Elder", Defeated: false},
		},
		ExploredZones: zones,
		Locations: []extract.Marker{
			{ID: "loc-kept", Kind: "location", Type: "Eikthyrnir", X: 70, Z: 20},      // zone (1,0): explored
			{ID: "loc-dropped", Kind: "location", Type: "GDKing", X: -5000, Z: -5000}, // zone (-78,-78): not
		},
		Markers: []extract.Marker{{ID: "m1", Kind: "pin", X: 1, Z: 2, Label: "home"}},
		Bases:   []extract.Base{{ID: "b1", Name: "Home", X: 3, Z: 4, Radius: 20, Pieces: 100, Builders: []extract.Builder{}}},
		Players: []extract.Player{{ID: 1, Name: "Alice"}},
	}
}

func ip(n int) *int { return &n }

func at(d time.Duration) time.Time { return t0.Add(d) }

// testEvents is a realistic alpha batch: boot, ready, join code, two joins,
// one leave, three saves (gaps 720 s and 600 s), players_now and a
// heartbeat.
func testEvents() []logwatch.Event {
	bobSince := at(-10 * time.Minute)
	return []logwatch.Event{
		{ID: "e1", Type: logwatch.EvServerBoot, At: at(-30 * time.Minute), Version: "0.219.14", NetworkVersion: 34},
		{ID: "e2", Type: logwatch.EvServerReady, At: at(-29 * time.Minute)},
		{ID: "e3", Type: logwatch.EvJoinCode, At: at(-28 * time.Minute), Code: "123456"},
		{ID: "e4", Type: logwatch.EvWorldSaved, At: at(-26 * time.Minute)},
		{ID: "e5", Type: logwatch.EvPlayerJoin, At: at(-20 * time.Minute), Name: "Alice", Platform: "Steam", PlatformID: "111"},
		{ID: "e6", Type: logwatch.EvWorldSaved, At: at(-14 * time.Minute)},
		{ID: "e7", Type: logwatch.EvPlayerJoin, At: bobSince, Name: "Bob", Platform: "Xbox", PlatformID: "222"},
		{ID: "e8", Type: logwatch.EvPlayerLeave, At: at(-5 * time.Minute), Name: "Bob", Platform: "Xbox", PlatformID: "222", Since: &bobSince, Seconds: 300},
		{ID: "e9", Type: logwatch.EvWorldSaved, At: at(-4 * time.Minute)},
		{ID: "e10", Type: logwatch.EvPlayersNow, At: at(-4 * time.Minute), Players: ip(1)},
		{ID: "e11", Type: logwatch.EvHeartbeat, At: at(-1 * time.Minute)},
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
