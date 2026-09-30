package server

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/jumpingmushroom/farsight/internal/auth"
	"github.com/jumpingmushroom/farsight/internal/config"
	"github.com/jumpingmushroom/farsight/internal/ingest"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
	"github.com/jumpingmushroom/farsight/internal/tileset"
)

// Case 1: ingest auth and body checks.
func TestIngestAuthAndBodyErrors(t *testing.T) {
	e := newEnv(t)
	body := gzipBytes(t, []byte(`{"events":[]}`))

	if r := e.rawIngest("alpha", "events", "", body, true); r.code != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", r.code)
	}
	if r := e.rawIngest("alpha", "events", "beta-token", body, true); r.code != http.StatusUnauthorized {
		t.Errorf("wrong token: %d, want 401", r.code)
	}
	// An unknown server is indistinguishable from a bad token.
	bad := e.rawIngest("alpha", "events", "wrong", body, true)
	if r := e.rawIngest("gamma", "events", "alpha-token", body, true); r.code != http.StatusUnauthorized || !bytes.Equal(r.body, bad.body) {
		t.Errorf("unknown server: %d %s, want 401 %s", r.code, r.body, bad.body)
	}
	if r := e.rawIngest("gamma", "events", "", body, true); r.code != http.StatusUnauthorized || !bytes.Equal(r.body, bad.body) {
		t.Errorf("unknown server, no token: %d %s, want 401 %s", r.code, r.body, bad.body)
	}
	if r := e.rawIngest("alpha", "events", "alpha-token", []byte(`{"events":[]}`), false); r.code != http.StatusBadRequest {
		t.Errorf("not gzip: %d, want 400", r.code)
	}
	if r := e.rawIngest("alpha", "events", "alpha-token", gzipBytes(t, []byte(`{"events":[`)), true); r.code != http.StatusBadRequest {
		t.Errorf("malformed json: %d, want 400", r.code)
	}

	// 17 MiB of whitespace decompresses past the 16 MiB events cap.
	big := append([]byte(`{"events":[`), bytes.Repeat([]byte(" "), 17<<20)...)
	big = append(big, "]}"...)
	if r := e.rawIngest("alpha", "events", "alpha-token", gzipBytes(t, big), true); r.code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized: %d, want 413", r.code)
	}

	if r := e.rawIngest("alpha", "events", "alpha-token", body, true); r.code != http.StatusOK {
		t.Errorf("good request: %d %s, want 200", r.code, r.body)
	}
	// Second good request is served from the token cache; still 200.
	if r := e.rawIngest("alpha", "events", "alpha-token", body, true); r.code != http.StatusOK {
		t.Errorf("cached good request: %d, want 200", r.code)
	}
	// alpha's token is not valid for beta.
	if r := e.rawIngest("beta", "events", "alpha-token", body, true); r.code != http.StatusUnauthorized {
		t.Errorf("cross-server token: %d, want 401", r.code)
	}
}

// Case 2: snapshot stored, idempotent, triggers tiles.
func TestIngestSnapshotStoredOnceAndEnsuresTiles(t *testing.T) {
	e := newEnv(t)
	snap := testSnapshot("save-1", at(-2*time.Minute))

	r := e.do("POST", "/ingest/alpha/snapshot", gzipBytes(t, mustJSON(t, snap)),
		map[string]string{"Authorization": "Bearer alpha-token", "Content-Encoding": "gzip"}, "")
	if r.code != 200 {
		t.Fatalf("snapshot: %d %s", r.code, r.body)
	}
	var out map[string]any
	r.json(t, &out)
	if out["stored"] != true {
		t.Fatalf("first POST stored = %v, want true", out["stored"])
	}

	r = e.do("POST", "/ingest/alpha/snapshot", gzipBytes(t, mustJSON(t, snap)),
		map[string]string{"Authorization": "Bearer alpha-token", "Content-Encoding": "gzip"}, "")
	r.json(t, &out)
	if r.code != 200 || out["stored"] != false {
		t.Fatalf("second POST: %d stored=%v, want 200 false", r.code, out["stored"])
	}

	if st := e.tiles.Status(testSeed, testGen); st.State != tileset.StateQueued {
		t.Fatalf("tiles state = %v, want queued", st.State)
	}

	if _, _, ok, _ := e.store.LatestSnapshot(t.Context(), "alpha"); !ok {
		t.Fatal("no snapshot stored")
	}

	// Via the real agent client too.
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("save-2", at(-1*time.Minute))); err != nil {
		t.Fatalf("ingest client: %v", err)
	}

	// serverId mismatch, missing saveId, zero savedAt -> 400.
	bad := []func(){
		func() { snap.ServerID = "beta" },
		func() { snap.ServerID = "alpha"; snap.SaveID = "" },
		func() { snap.SaveID = "x"; snap.SavedAt = time.Time{} },
	}
	for i, mut := range bad {
		mut()
		err := e.post("alpha", "alpha-token", "snapshot", snap)
		var se *ingest.StatusError
		if !errors.As(err, &se) || se.Code != 400 {
			t.Errorf("bad snapshot %d: err = %v, want 400", i, err)
		}
	}
}

// Case 3: events counts and replay.
func TestIngestEventsCountsAndReplay(t *testing.T) {
	e := newEnv(t)
	evs := append(testEvents(), logwatch.Event{Type: logwatch.EvHeartbeat, At: at(0)}) // no id: skipped

	send := func() map[string]int {
		r := e.do("POST", "/ingest/alpha/events", gzipBytes(t, mustJSON(t, map[string]any{"events": evs})),
			map[string]string{"Authorization": "Bearer alpha-token", "Content-Encoding": "gzip"}, "")
		if r.code != 200 {
			t.Fatalf("events: %d %s", r.code, r.body)
		}
		var out map[string]int
		r.json(t, &out)
		return out
	}
	if got := send(); got["applied"] != 11 || got["skipped"] != 1 {
		t.Fatalf("first = %v, want applied 11 skipped 1", got)
	}
	if got := send(); got["applied"] != 0 || got["skipped"] != 12 {
		t.Fatalf("replay = %v, want applied 0 skipped 12", got)
	}
	// The agent's own client shape works too.
	if err := e.post("alpha", "alpha-token", "events", map[string]any{"events": evs}); err != nil {
		t.Fatal(err)
	}
}

// Case 4: server-scoped routes are 404 without the cookie, identical to
// an unknown id.
func TestLockedRoutesAre404(t *testing.T) {
	e := newEnv(t)
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s", at(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	key := e.tiles.Key(testSeed, testGen)
	unknown := e.get("/api/servers/nope", "")
	if unknown.code != 404 {
		t.Fatalf("unknown id: %d", unknown.code)
	}
	for _, p := range []string{
		"/api/servers/alpha",
		"/api/servers/alpha/snapshot",
		"/tiles/alpha/" + key + "/0/0/0.png",
	} {
		for _, cookie := range []string{"", "garbage", strings.Repeat("x", 5000)} {
			r := e.get(p, cookie)
			if r.code != 404 || !bytes.Equal(r.body, unknown.body) {
				t.Errorf("%s cookie=%.10q: %d %s, want 404 %s", p, cookie, r.code, r.body, unknown.body)
			}
		}
	}
	// A cookie for beta does not open alpha.
	beta := e.mustUnlock("beta")
	if r := e.get("/api/servers/alpha", beta); r.code != 404 {
		t.Errorf("alpha with beta cookie: %d", r.code)
	}

	var list struct {
		Servers []any `json:"servers"`
	}
	r := e.get("/api/servers", "")
	r.json(t, &list)
	if r.code != 200 || list.Servers == nil || len(list.Servers) != 0 {
		t.Errorf("/api/servers without cookie: %d %s", r.code, r.body)
	}
}

// Case 5: unlock, wrong passphrase, rate limiting, cookie attributes.
func TestUnlock(t *testing.T) {
	e := newEnv(t)

	r, c := e.unlock("alpha", "alpha-pass", "")
	if r.code != 204 || c == nil {
		t.Fatalf("right passphrase: %d %s", r.code, r.body)
	}
	if !c.HttpOnly || !c.Secure || c.Path != "/" || c.SameSite != http.SameSiteLaxMode || c.MaxAge != int(auth.CookieMaxAge/time.Second) {
		t.Errorf("cookie attrs: %+v", c)
	}
	if ids, ok := (auth.Codec{Key: e.cfg.CookieKey, Now: e.clock.Now}).Decode(c.Value); !ok || len(ids) != 1 || ids[0] != "alpha" {
		t.Errorf("cookie ids = %v %v", ids, ok)
	}

	wrong, _ := e.unlock("alpha", "nope", "")
	if wrong.code != 401 || !contains(string(wrong.body), "wrong passphrase") {
		t.Fatalf("wrong: %d %s", wrong.code, wrong.body)
	}
	unk, _ := e.unlock("gamma", "alpha-pass", "")
	if unk.code != 401 || !bytes.Equal(unk.body, wrong.body) {
		t.Fatalf("unknown server: %d %s, want same as wrong %s", unk.code, unk.body, wrong.body)
	}
	if r, _ := e.unlock("beta", "nope", ""); r.code != 401 {
		t.Fatalf("third failure: %d", r.code)
	}
	// burst (3) failures used up: even the right passphrase is refused.
	r, c = e.unlock("alpha", "alpha-pass", "")
	if r.code != 429 || c != nil || !contains(string(r.body), "too many attempts") {
		t.Fatalf("after burst: %d %s", r.code, r.body)
	}
	// A minute later one token has refilled.
	e.clock.Add(time.Minute)
	if r, _ := e.unlock("alpha", "alpha-pass", ""); r.code != 204 {
		t.Fatalf("after refill: %d", r.code)
	}

	// Malformed body -> 400.
	if r := e.do("POST", "/api/unlock", []byte(`{nope`), nil, ""); r.code != 400 {
		t.Errorf("malformed: %d", r.code)
	}
	if r := e.get("/api/unlock", ""); r.code == 204 || r.code == 200 {
		t.Errorf("GET unlock: %d", r.code)
	}
}

func TestUnlockTrustProxyUsesForwardedFor(t *testing.T) {
	e := newEnv(t)
	e.cfg.TrustProxy = true
	body := mustJSON(t, map[string]string{"server": "alpha", "passphrase": "nope"})
	fail := func(xff string) int {
		return e.do("POST", "/api/unlock", body, map[string]string{"X-Forwarded-For": xff}, "").code
	}
	// The client controls every hop but the last (the one the trusted
	// proxy appended), so rotating the earlier hops must not dodge the
	// limiter: all of these key on 10.0.0.1.
	for i, xff := range []string{" spoof-a , 10.0.0.1", "spoof-b, 10.0.0.1 ", "10.0.0.1, "} {
		if c := fail(xff); c != 401 {
			t.Fatalf("attempt %d (%q): %d, want 401", i, xff, c)
		}
	}
	if c := fail("spoof-c, 10.0.0.1"); c != 429 {
		t.Fatalf("same client, rotated first hop: %d, want 429", c)
	}
	if c := fail("10.0.0.1"); c != 429 {
		t.Fatalf("same client, single hop: %d, want 429", c)
	}
	if c := fail("10.0.0.1, 10.0.0.2"); c != 401 {
		t.Fatalf("other client: %d, want 401", c)
	}
}

func TestClientIPUsesRightmostForwardedFor(t *testing.T) {
	s := newServer(Deps{Config: &config.Config{TrustProxy: true}})
	for _, tc := range []struct{ xff, want string }{
		{"spoof, real", "real"},
		{" a , b , c ", "c"},
		{"real, ", "real"},
		{"real,,  ", "real"},
		{" , ", "192.0.2.7"},
		{"", "192.0.2.7"},
	} {
		r := httptest.NewRequest("POST", "/api/unlock", nil)
		r.RemoteAddr = "192.0.2.7:4242"
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		if got := s.clientIP(r); got != tc.want {
			t.Errorf("X-Forwarded-For %q: clientIP = %q, want %q", tc.xff, got, tc.want)
		}
	}

	// A proxy that adds its own header line rather than extending the
	// client's: the last line's last entry wins.
	r := httptest.NewRequest("POST", "/api/unlock", nil)
	r.RemoteAddr = "192.0.2.7:4242"
	r.Header.Add("X-Forwarded-For", "spoof-1, spoof-2")
	r.Header.Add("X-Forwarded-For", "real")
	if got := s.clientIP(r); got != "real" {
		t.Errorf("two header lines: clientIP = %q, want real", got)
	}

	s.Config.TrustProxy = false
	r = httptest.NewRequest("POST", "/api/unlock", nil)
	r.RemoteAddr = "192.0.2.7:4242"
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	if got := s.clientIP(r); got != "192.0.2.7" {
		t.Errorf("untrusted proxy: clientIP = %q, want RemoteAddr host", got)
	}
}

// Case 6: /api/servers lists only unlocked servers; unlocking merges.
func TestServersListAndCookieMerge(t *testing.T) {
	e := newEnv(t)
	cookie := e.mustUnlock("beta")

	type summary struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Players    *int   `json:"players"`
		MaxPlayers int    `json:"maxPlayers"`
	}
	var list struct {
		Servers []summary `json:"servers"`
	}
	r := e.get("/api/servers", cookie)
	r.json(t, &list)
	if len(list.Servers) != 1 || list.Servers[0].ID != "beta" || list.Servers[0].Name != "Beta" ||
		list.Servers[0].Status != "unknown" || list.Servers[0].Players == nil || list.Servers[0].MaxPlayers != 5 {
		t.Fatalf("list = %s", r.body)
	}
	if r.header.Get("Cache-Control") != "no-store" || !strings.HasPrefix(r.header.Get("Content-Type"), "application/json") {
		t.Errorf("headers: %v", r.header)
	}

	r2, c := e.unlock("alpha", "alpha-pass", cookie)
	if r2.code != 204 || c == nil {
		t.Fatalf("second unlock: %d", r2.code)
	}
	r = e.get("/api/servers", c.Value)
	list.Servers = nil
	r.json(t, &list)
	if len(list.Servers) != 2 || list.Servers[0].ID != "alpha" || list.Servers[1].ID != "beta" {
		t.Fatalf("merged list (config order) = %s", r.body)
	}

	// Beta's card with no snapshot or events: no world, tiles none.
	r = e.get("/api/servers/beta", c.Value)
	if r.code != 200 {
		t.Fatalf("beta card: %d", r.code)
	}
	var card map[string]any
	r.json(t, &card)
	if _, ok := card["world"]; ok {
		t.Errorf("world present without snapshot: %s", r.body)
	}
	for _, k := range []string{"upSince", "joinCode", "joinCodeAt", "version", "networkVersion", "lastHeartbeat", "address", "discordHint"} {
		if _, ok := card[k]; ok {
			t.Errorf("%s present on empty card", k)
		}
	}
	tl, _ := card["tiles"].(map[string]any)
	if tl["state"] != "none" || tl["done"] != 0.0 || tl["total"] != 0.0 {
		t.Errorf("tiles = %v", card["tiles"])
	}
	if card["status"] != "unknown" || card["players"] != 0.0 {
		t.Errorf("status/players = %v/%v", card["status"], card["players"])
	}
	for _, k := range []string{"online", "recent", "activity"} {
		if arr, ok := card[k].([]any); !ok || len(arr) != 0 {
			t.Errorf("%s = %v, want []", k, card[k])
		}
	}
	if r := e.get("/api/servers/beta/snapshot", c.Value); r.code != 404 {
		t.Errorf("beta snapshot without data: %d", r.code)
	}
}

type cardJSON struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Crossplay      bool   `json:"crossplay"`
	Address        string `json:"address"`
	DiscordHint    string `json:"discordHint"`
	MaxPlayers     int    `json:"maxPlayers"`
	Status         string `json:"status"`
	Version        string `json:"version"`
	NetworkVersion int    `json:"networkVersion"`
	UpSince        string `json:"upSince"`
	LastHeartbeat  string `json:"lastHeartbeat"`
	Players        int    `json:"players"`
	JoinCode       string `json:"joinCode"`
	JoinCodeAt     string `json:"joinCodeAt"`
	Online         []struct {
		Name, Platform, PlatformID, Since string
	} `json:"online"`
	Recent []struct {
		Name, Platform, PlatformID, Since, Until string
		Seconds                                  int64
	} `json:"recent"`
	Activity []map[string]any `json:"activity"`
	World    *struct {
		Name            string            `json:"name"`
		SeedName        string            `json:"seedName"`
		Day             int               `json:"day"`
		Bosses          []map[string]any  `json:"bosses"`
		Modifiers       map[string]string `json:"modifiers"`
		Flags           []string          `json:"flags"`
		ExploredPct     float64           `json:"exploredPct"`
		SavedAt         string            `json:"savedAt"`
		ReadAt          string            `json:"readAt"`
		SaveIntervalSec *int              `json:"saveIntervalSec"`
	} `json:"world"`
	Tiles struct {
		State string `json:"state"`
		Done  int    `json:"done"`
		Total int    `json:"total"`
		Key   string `json:"key"`
	} `json:"tiles"`
}

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Case 7: the full card.
func TestCard(t *testing.T) {
	e := newEnv(t)
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-3*time.Minute))); err != nil {
		t.Fatal(err)
	}
	if err := e.post("alpha", "alpha-token", "events", map[string]any{"events": testEvents()}); err != nil {
		t.Fatal(err)
	}
	cookie := e.mustUnlock("alpha")

	r := e.get("/api/servers/alpha", cookie)
	if r.code != 200 {
		t.Fatalf("card: %d %s", r.code, r.body)
	}
	var c cardJSON
	r.json(t, &c)

	if c.ID != "alpha" || c.Name != "Alpha" || !c.Crossplay || c.Address != "alpha.example:2456" || c.DiscordHint != "#alpha" || c.MaxPlayers != 10 {
		t.Errorf("identity: %s", r.body)
	}
	if c.Status != "online" || c.Players != 1 || c.Version != "0.219.14" || c.NetworkVersion != 34 {
		t.Errorf("status: %s", r.body)
	}
	if c.UpSince != rfc(at(-30*time.Minute)) || c.LastHeartbeat != rfc(at(-time.Minute)) ||
		c.JoinCode != "123456" || c.JoinCodeAt != rfc(at(-28*time.Minute)) {
		t.Errorf("times/code: %s", r.body)
	}
	if len(c.Online) != 1 || c.Online[0].Name != "Alice" || c.Online[0].Platform != "Steam" ||
		c.Online[0].PlatformID != "111" || c.Online[0].Since != rfc(at(-20*time.Minute)) {
		t.Errorf("online: %+v", c.Online)
	}
	if len(c.Recent) != 1 || c.Recent[0].Name != "Bob" || c.Recent[0].Seconds != 300 ||
		c.Recent[0].Until != rfc(at(-5*time.Minute)) || c.Recent[0].Since != rfc(at(-10*time.Minute)) {
		t.Errorf("recent: %+v", c.Recent)
	}

	// Activity: 9 non-heartbeat/players_now events, newest first.
	if len(c.Activity) != 9 {
		t.Fatalf("activity len = %d: %v", len(c.Activity), c.Activity)
	}
	if c.Activity[0]["type"] != logwatch.EvWorldSaved || c.Activity[0]["at"] != rfc(at(-4*time.Minute)) {
		t.Errorf("activity[0] = %v", c.Activity[0])
	}
	if _, ok := c.Activity[0]["seconds"]; ok {
		t.Errorf("seconds present on world_saved: %v", c.Activity[0])
	}
	var sawLeave, sawBoot, sawCode bool
	for _, a := range c.Activity {
		switch a["type"] {
		case logwatch.EvPlayerLeave:
			sawLeave = a["name"] == "Bob" && a["platform"] == "Xbox" && a["seconds"] == 300.0
		case logwatch.EvServerBoot:
			sawBoot = a["version"] == "0.219.14"
		case logwatch.EvJoinCode:
			sawCode = a["code"] == "123456"
		case logwatch.EvHeartbeat, logwatch.EvPlayersNow:
			t.Errorf("activity has %v", a["type"])
		}
	}
	if !sawLeave || !sawBoot || !sawCode {
		t.Errorf("activity details: leave=%v boot=%v code=%v: %v", sawLeave, sawBoot, sawCode, c.Activity)
	}

	if c.World == nil {
		t.Fatalf("no world: %s", r.body)
	}
	w := c.World
	if w.Name != "Midgard" || w.SeedName != "abcdef" || w.Day != 42 || w.Modifiers["combat"] != "hard" ||
		len(w.Flags) != 1 || w.SavedAt != rfc(at(-3*time.Minute)) || w.ReadAt != rfc(at(-3*time.Minute+5*time.Second)) {
		t.Errorf("world: %s", r.body)
	}
	if len(w.Bosses) != 2 || w.Bosses[0]["key"] != "defeated_eikthyr" || w.Bosses[0]["defeated"] != true || w.Bosses[1]["defeated"] != false {
		t.Errorf("bosses: %v", w.Bosses)
	}
	wantPct := math.Round(900/float64(referenceZoneCount())*100*10) / 10
	if w.ExploredPct != wantPct || wantPct == 0 {
		t.Errorf("exploredPct = %v, want %v", w.ExploredPct, wantPct)
	}
	if w.SaveIntervalSec == nil || *w.SaveIntervalSec != 660 {
		t.Errorf("saveIntervalSec = %v, want 660", w.SaveIntervalSec)
	}
	if c.Tiles.State != "queued" || c.Tiles.Key != e.tiles.Key(testSeed, testGen) {
		t.Errorf("tiles: %+v", c.Tiles)
	}

	// Four minutes on, the last heartbeat is stale: offline.
	e.clock.Add(4 * time.Minute)
	r = e.get("/api/servers/alpha", cookie)
	r.json(t, &c)
	if c.Status != "offline" {
		t.Errorf("stale status = %q, want offline", c.Status)
	}
}

// referenceZoneCount counts 64 m zones whose centre lies within 10 500 m,
// independently of the implementation.
func referenceZoneCount() int {
	n := 0
	for x := -200; x <= 200; x++ {
		for z := -200; z <= 200; z++ {
			if math.Hypot(float64(x*64), float64(z*64)) <= 10500 {
				n++
			}
		}
	}
	return n
}

func TestExploredPct(t *testing.T) {
	var all [][2]int16
	for x := -170; x <= 170; x++ {
		for z := -170; z <= 170; z++ {
			all = append(all, [2]int16{int16(x), int16(z)}) // includes out-of-world zones
		}
	}
	all = append(all, [2]int16{0, 0}, [2]int16{1, 1}) // duplicates
	if got := exploredPct(all); got != 100 {
		t.Errorf("all zones = %v, want 100", got)
	}
	if got := exploredPct(nil); got != 0 {
		t.Errorf("none = %v, want 0", got)
	}
}

func TestTotalZonesAndSaveInterval(t *testing.T) {
	if totalZones != referenceZoneCount() || totalZones < 84000 || totalZones > 85200 {
		t.Errorf("totalZones = %d, reference %d", totalZones, referenceZoneCount())
	}
	ts := func(secs ...int) []time.Time {
		var out []time.Time
		for _, s := range secs {
			out = append(out, t0.Add(-time.Duration(s)*time.Second))
		}
		return out
	}
	cases := []struct {
		in   []time.Time
		want int
		ok   bool
	}{
		{ts(0, 100), 0, false},
		{ts(0, 100, 300), 150, true},        // gaps 100, 200 -> mean of middle two
		{ts(0, 100, 300, 310), 100, true},   // gaps 100, 200, 10 -> 100
		{ts(0, 10, 25, 45, 60), 15, true},   // gaps 10,15,20,15 -> (15+15)/2
		{ts(0, 1, 2, 4), 1, true},           // gaps 1,1,2 -> 1
		{ts(0, 100, 201), 100, true},        // gaps 100, 101 -> 100 (floor)
		{ts(0, 700, 1400, 2000), 700, true}, // gaps 700,700,600
	}
	for i, c := range cases {
		got, ok := saveInterval(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("case %d: %d %v, want %d %v", i, got, ok, c.want, c.ok)
		}
	}
}

// Case 8: the snapshot API filters locations to explored zones.
func TestSnapshotAPIFiltersLocations(t *testing.T) {
	e := newEnv(t)
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-3*time.Minute))); err != nil {
		t.Fatal(err)
	}
	cookie := e.mustUnlock("alpha")
	r := e.get("/api/servers/alpha/snapshot", cookie)
	if r.code != 200 {
		t.Fatalf("snapshot: %d %s", r.code, r.body)
	}
	var s struct {
		SavedAt       string           `json:"savedAt"`
		ExploredZones [][2]int         `json:"exploredZones"`
		Markers       []map[string]any `json:"markers"`
		Locations     []map[string]any `json:"locations"`
		Bases         []map[string]any `json:"bases"`
		Players       []map[string]any `json:"players"`
	}
	r.json(t, &s)
	if len(s.Locations) != 1 || s.Locations[0]["id"] != "loc-kept" {
		t.Errorf("locations = %v", s.Locations)
	}
	if s.SavedAt != rfc(at(-3*time.Minute)) || len(s.ExploredZones) != 901 || len(s.Markers) != 1 ||
		len(s.Bases) != 1 || len(s.Players) != 1 {
		t.Errorf("snapshot body: savedAt=%s zones=%d markers=%d bases=%d players=%d",
			s.SavedAt, len(s.ExploredZones), len(s.Markers), len(s.Bases), len(s.Players))
	}
	if r.header.Get("Cache-Control") != "no-store" {
		t.Errorf("cache-control = %q", r.header.Get("Cache-Control"))
	}
}

func TestLocationZone(t *testing.T) {
	cases := []struct {
		x, z   float32
		zx, zz int
	}{
		{0, 0, 0, 0}, {31.9, -32, 0, 0}, {32, -32.1, 1, -1}, {70, 20, 1, 0}, {-96.5, 95, -2, 1},
	}
	for _, c := range cases {
		if zx, zz := locationZone(c.x, c.z); zx != c.zx || zz != c.zz {
			t.Errorf("zone(%v,%v) = %d,%d want %d,%d", c.x, c.z, zx, zz, c.zx, c.zz)
		}
	}
}

// Case 9: tiles.
func TestTiles(t *testing.T) {
	e := newEnv(t)
	cookie := e.mustUnlock("alpha")
	key := e.tiles.Key(testSeed, testGen)
	tile := "/tiles/alpha/" + key + "/0/0/0.png"

	if r := e.get(tile, cookie); r.code != 404 {
		t.Fatalf("before snapshot: %d", r.code)
	}
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	if r := e.get(tile, cookie); r.code != 404 {
		t.Fatalf("queued: %d", r.code)
	}

	e.runTiles()
	close(e.release)
	deadline := time.Now().Add(5 * time.Second)
	for e.tiles.Status(testSeed, testGen).State != tileset.StateComplete {
		if time.Now().After(deadline) {
			t.Fatal("render did not complete")
		}
		time.Sleep(2 * time.Millisecond)
	}

	r := e.get(tile, cookie)
	if r.code != 200 || string(r.body) != "\x89PNG fake" {
		t.Fatalf("complete: %d %q", r.code, r.body)
	}
	if cc := r.header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("cache-control = %q", cc)
	}
	if ct := r.header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q", ct)
	}

	for _, p := range []string{
		"/tiles/alpha/1-0-r1/0/0/0.png",      // wrong key
		"/tiles/alpha/" + key + "/6/0/0.png", // z out of range
		"/tiles/alpha/" + key + "/0/1/0.png", // x out of range for z
		"/tiles/alpha/" + key + "/0/0/-1.png",
		"/tiles/alpha/" + key + "/0/0/0",     // no .png
		"/tiles/alpha/" + key + "/1/0/0.png", // in range but missing on disk
		"/tiles/alpha/" + key + "/0/0/0.jpg",
		"/tiles/alpha/" + key + "/a/0/0.png",
	} {
		if r := e.get(p, cookie); r.code != 404 {
			t.Errorf("%s: %d, want 404", p, r.code)
		}
	}
	if r := e.get(tile, ""); r.code != 404 {
		t.Errorf("no cookie: %d", r.code)
	}

	// Card reports the complete set.
	var c cardJSON
	e.get("/api/servers/alpha", cookie).json(t, &c)
	if c.Tiles.State != "complete" || c.Tiles.Key != key || c.Tiles.Done != 1 || c.Tiles.Total != 1 {
		t.Errorf("card tiles = %+v", c.Tiles)
	}
}

// Case 10: healthz, plus UI fallback and unknown routes.
func TestHealthzAndRouting(t *testing.T) {
	e := newEnv(t)
	r := e.get("/healthz", "")
	if r.code != 200 || strings.TrimSpace(string(r.body)) != "ok" {
		t.Fatalf("healthz: %d %q", r.code, r.body)
	}
	if r := e.get("/", ""); r.code != 404 {
		t.Errorf("/ without UI: %d", r.code)
	}
	if r := e.get("/api/nope", ""); r.code != 404 {
		t.Errorf("/api/nope: %d", r.code)
	}

	ui := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ui:" + r.URL.Path)) })
	h := New(Deps{Config: e.cfg, Store: e.store, Tiles: e.tiles, UI: ui, Now: e.clock.Now,
		Codec: auth.Codec{Key: e.cfg.CookieKey}, Limiter: auth.NewLimiter(1, 3, nil)})
	for path, want := range map[string]string{"/": "ui:/", "/s/alpha": "ui:/s/alpha"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Body.String() != want {
			t.Errorf("%s: %q, want %q", path, rec.Body.String(), want)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/nope", nil))
	if rec.Code != 404 {
		t.Errorf("/api/nope with UI: %d", rec.Code)
	}
}

func TestRecoverMiddleware(t *testing.T) {
	h := recoverer(nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 {
		t.Fatalf("code = %d", rec.Code)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Fix round 1: the dummy hash's cost matches the configured passphrase
// hashes, so an unknown id costs the same bcrypt work as a known one.
func TestDummyHashMatchesConfiguredCost(t *testing.T) {
	e := newEnv(t)
	s := newServer(Deps{Config: e.cfg})
	if c, err := bcrypt.Cost(s.dummyHash()); err != nil || c != bcrypt.MinCost {
		t.Fatalf("dummy cost = %d %v, want %d", c, err, bcrypt.MinCost)
	}
	if &s.dummyHash()[0] != &s.dummyHash()[0] {
		t.Error("dummy hash recomputed")
	}

	h5, _ := bcrypt.GenerateFromPassword([]byte("x"), 5)
	mixed := *e.cfg
	mixed.Servers = append([]config.Server(nil), e.cfg.Servers...)
	mixed.Servers[1].PassphraseHash = string(h5)
	if c, _ := bcrypt.Cost(newServer(Deps{Config: &mixed}).dummyHash()); c != 5 {
		t.Errorf("mixed dummy cost = %d, want 5", c)
	}

	none := config.Config{Servers: []config.Server{{ID: "x", PassphraseHash: "not-bcrypt"}}}
	if got := dummyCost(&none); got != bcrypt.DefaultCost {
		t.Errorf("fallback cost = %d, want %d", got, bcrypt.DefaultCost)
	}
}

// Fix round 1: concurrent failures can't exceed the burst, because each
// attempt reserves its token before the bcrypt compare.
func TestUnlockConcurrentFailuresBoundedByBurst(t *testing.T) {
	const burst, n = 2, 12
	e := newEnvBurst(t, burst)
	body := mustJSON(t, map[string]string{"server": "alpha", "passphrase": "nope"})

	codes := make(chan int, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req, _ := http.NewRequest("POST", e.srv.URL+"/api/unlock", bytes.NewReader(body))
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				codes <- 0
				return
			}
			res.Body.Close()
			codes <- res.StatusCode
		}()
	}
	close(start)
	wg.Wait()
	close(codes)
	var n401, n429 int
	for c := range codes {
		switch c {
		case 401:
			n401++
		case 429:
			n429++
		default:
			t.Errorf("unexpected status %d", c)
		}
	}
	if n401 > burst || n401+n429 != n {
		t.Fatalf("401s = %d, 429s = %d; want at most %d 401s", n401, n429, burst)
	}
}

// A successful unlock refunds its reserved token.
func TestUnlockSuccessDoesNotConsumeToken(t *testing.T) {
	e := newEnvBurst(t, 1)
	for i := 0; i < 3; i++ {
		if r, _ := e.unlock("alpha", "alpha-pass", ""); r.code != 204 {
			t.Fatalf("success %d: %d", i, r.code)
		}
	}
	if r, _ := e.unlock("alpha", "nope", ""); r.code != 401 {
		t.Fatalf("first failure: %d", r.code)
	}
	if r, _ := e.unlock("alpha", "alpha-pass", ""); r.code != 429 {
		t.Fatalf("after failure with burst 1: %d, want 429", r.code)
	}
}

// Fix round 1: ids of servers no longer in the config drop out of the
// cookie on the next unlock.
func TestUnlockDropsStaleCookieIDs(t *testing.T) {
	e := newEnv(t)
	codec := auth.Codec{Key: e.cfg.CookieKey, Now: e.clock.Now}
	old := codec.Encode([]string{"gone", "beta"})
	r, c := e.unlock("alpha", "alpha-pass", old)
	if r.code != 204 || c == nil {
		t.Fatalf("unlock: %d", r.code)
	}
	ids, ok := codec.Decode(c.Value)
	if !ok || !slices.Equal(ids, []string{"alpha", "beta"}) {
		t.Fatalf("cookie ids = %v %v, want [alpha beta]", ids, ok)
	}
}

// Fix round 1: the compressed-size cap returns 413.
func TestIngestCompressedCap(t *testing.T) {
	old := maxCompressed
	maxCompressed = 1 << 10
	t.Cleanup(func() { maxCompressed = old })

	e := newEnv(t)
	noise := make([]byte, 8<<10)
	rand.Read(noise)
	payload := mustJSON(t, map[string]any{"events": []any{}, "pad": base64.StdEncoding.EncodeToString(noise)})
	gz := gzipBytes(t, payload)
	if len(gz) <= 1<<10 {
		t.Fatalf("test body only %d bytes compressed", len(gz))
	}
	if r := e.rawIngest("alpha", "events", "alpha-token", gz, true); r.code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over compressed cap: %d, want 413", r.code)
	}
}

// Plan 5 Task 1: with SplitIngest, the public handler never serves
// /ingest/, and the ingest handler serves nothing but ingest and healthz,
// sharing the same token cache and limiter as the public handler would.
func TestSplitIngestSeparatesPublicAndIngestHandlers(t *testing.T) {
	e, ingestSrv := newEnvSplit(t)
	body := gzipBytes(t, []byte(`{"events":[]}`))

	// The public handler returns the shared JSON 404 for /ingest/, even
	// with a fully valid request.
	if r := e.rawIngest("alpha", "events", "alpha-token", body, true); r.code != http.StatusNotFound {
		t.Errorf("public /ingest: %d, want 404", r.code)
	}
	unknown := e.get("/api/nope", "")
	pubIngest404 := e.rawIngest("alpha", "events", "alpha-token", body, true)
	if !bytes.Equal(pubIngest404.body, unknown.body) {
		t.Errorf("public /ingest 404 body = %s, want the shared 404 body %s", pubIngest404.body, unknown.body)
	}

	// The ingest handler accepts the exact same request.
	req, err := http.NewRequest("POST", ingestSrv.URL+"/ingest/alpha/events", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer alpha-token")
	req.Header.Set("Content-Encoding", "gzip")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("ingest handler /ingest: %d, want 200", res.StatusCode)
	}

	// The ingest handler's /healthz works...
	res, err = http.Get(ingestSrv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("ingest handler /healthz: %d, want 200", res.StatusCode)
	}

	// ...but nothing else does, including the public API.
	res, err = http.Get(ingestSrv.URL + "/api/servers")
	if err != nil {
		t.Fatal(err)
	}
	var errBody map[string]string
	json.NewDecoder(res.Body).Decode(&errBody)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound || errBody["error"] == "" {
		t.Errorf("ingest handler /api/servers: %d %v, want 404 JSON error", res.StatusCode, errBody)
	}

	// Fix round 1: a bare "/ingest" (no trailing slash) redirects to the
	// canonical "/ingest/" and lands on the same shared 404 — never on an
	// ingest handler (which would answer with "unauthorized", not
	// "not found").
	res, err = http.Get(e.srv.URL + "/ingest")
	if err != nil {
		t.Fatal(err)
	}
	errBody = nil
	json.NewDecoder(res.Body).Decode(&errBody)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound || errBody["error"] != "not found" {
		t.Errorf("bare /ingest: %d %v, want 404 {error: not found}", res.StatusCode, errBody)
	}

	// Fix round 1: SplitIngest rejects the whole /ingest/ subtree on the
	// public handler regardless of method, not just unregistered ones.
	for _, method := range []string{"HEAD", "OPTIONS", "PUT"} {
		req, err := http.NewRequest(method, e.srv.URL+"/ingest/alpha/events", nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s /ingest/alpha/events on the public handler: %d, want 404", method, res.StatusCode)
		}
	}

	// Fix round 1: an encoded "%2f" can't smuggle a path past the
	// catch-all; net/http decodes it into the same subtree, still 404.
	res, err = http.Get(e.srv.URL + "/ingest%2Falpha%2Fevents")
	if err != nil {
		t.Fatal(err)
	}
	errBody = nil
	json.NewDecoder(res.Body).Decode(&errBody)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound || errBody["error"] != "not found" {
		t.Errorf("encoded /ingest%%2Falpha%%2Fevents: %d %v, want 404 {error: not found}", res.StatusCode, errBody)
	}
}

// Plan 5 Task 1: gzip compression on the two JSON API routes, and never on
// tiles.
func TestGzipJSONCompressesLargePayloadsOnly(t *testing.T) {
	e := newEnv(t)
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	cookie := e.mustUnlock("alpha")

	// The snapshot payload (900+ explored zones) is comfortably over 1 KiB:
	// with Accept-Encoding: gzip it comes back compressed.
	plain := e.get("/api/servers/alpha/snapshot", cookie)
	if plain.code != 200 {
		t.Fatalf("plain snapshot: %d %s", plain.code, plain.body)
	}
	if plain.header.Get("Content-Encoding") != "" {
		t.Errorf("plain response has Content-Encoding %q", plain.header.Get("Content-Encoding"))
	}
	if plain.header.Get("Vary") != "Accept-Encoding" {
		t.Errorf("plain response Vary = %q, want Accept-Encoding", plain.header.Get("Vary"))
	}
	if len(plain.body) < 1024 {
		t.Fatalf("test snapshot body only %d bytes, need >= 1024 to exercise gzip", len(plain.body))
	}

	gz := e.do("GET", "/api/servers/alpha/snapshot", nil, map[string]string{"Accept-Encoding": "gzip"}, cookie)
	if gz.code != 200 {
		t.Fatalf("gzip snapshot: %d %s", gz.code, gz.body)
	}
	if gz.header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", gz.header.Get("Content-Encoding"))
	}
	if gz.header.Get("Vary") != "Accept-Encoding" {
		t.Errorf("gzip response Vary = %q, want Accept-Encoding", gz.header.Get("Vary"))
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz.body))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	if !bytes.Equal(got, plain.body) {
		t.Errorf("gunzipped body != plain body:\ngot  %s\nwant %s", got, plain.body)
	}

	// beta's card (no snapshot or events posted) is well under 1 KiB: it
	// stays plain even when the client accepts gzip.
	betaCookie := e.mustUnlock("beta")
	small := e.do("GET", "/api/servers/beta", nil, map[string]string{"Accept-Encoding": "gzip"}, betaCookie)
	if small.code != 200 {
		t.Fatalf("small card: %d %s", small.code, small.body)
	}
	if len(small.body) >= 1024 {
		t.Fatalf("beta card body is %d bytes, want < 1024 to exercise the no-compress path", len(small.body))
	}
	if small.header.Get("Content-Encoding") != "" {
		t.Errorf("small response has Content-Encoding %q, want none", small.header.Get("Content-Encoding"))
	}
	if small.header.Get("Vary") != "Accept-Encoding" {
		t.Errorf("small response Vary = %q, want Accept-Encoding", small.header.Get("Vary"))
	}

	// A tile (PNG) is never gzipped, and doesn't get the Vary header either.
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s2", at(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	key := e.tiles.Key(testSeed, testGen)
	e.runTiles()
	close(e.release)
	deadline := time.Now().Add(5 * time.Second)
	for e.tiles.Status(testSeed, testGen).State != tileset.StateComplete {
		if time.Now().After(deadline) {
			t.Fatal("render did not complete")
		}
		time.Sleep(2 * time.Millisecond)
	}
	tile := e.do("GET", "/tiles/alpha/"+key+"/0/0/0.png", nil, map[string]string{"Accept-Encoding": "gzip"}, cookie)
	if tile.code != 200 {
		t.Fatalf("tile: %d", tile.code)
	}
	if tile.header.Get("Content-Encoding") != "" {
		t.Errorf("tile Content-Encoding = %q, want none", tile.header.Get("Content-Encoding"))
	}
	if tile.header.Get("Vary") != "" {
		t.Errorf("tile Vary = %q, want none", tile.header.Get("Vary"))
	}
}
