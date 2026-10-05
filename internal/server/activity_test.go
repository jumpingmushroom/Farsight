package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
	"github.com/jumpingmushroom/farsight/internal/store"
)

// activityWorld seeds alpha (Europe/Oslo; now is 14:00 on 29 Sep there):
// testEvents (today), an extra autosave right after the last one, a raid,
// Ulf's join just after midnight on 27 Sep and one just before it (both
// local), and two saves, the second with a new tombstone (explored) and a
// new portal (unexplored), both Alice's. waitWorld joins the background
// world-event derivation (CatchUpAsync) before returning, so the caller
// sees the diff's events deterministically.
func activityWorld(t *testing.T, e *env) {
	t.Helper()
	evs := append(testEvents(),
		logwatch.Event{ID: "x1", Type: logwatch.EvWorldSaved, At: at(-3*time.Minute - 30*time.Second)},
		logwatch.Event{ID: "x2", Type: logwatch.EvRaid, At: at(-25 * time.Minute), Raid: "army_theelder"},
		logwatch.Event{ID: "x3", Type: logwatch.EvPlayerJoin, At: mustTime("2026-09-26T22:30:00Z"), Name: "Ulf", Platform: "Steam", PlatformID: "444"},
		logwatch.Event{ID: "x4", Type: logwatch.EvPlayerLeave, At: mustTime("2026-09-26T21:30:00Z"), Name: "Ulf", Platform: "Steam", PlatformID: "444"},
	)
	if err := e.post("alpha", "alpha-token", "events", map[string]any{"events": evs}); err != nil {
		t.Fatal(err)
	}
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-50*time.Minute))); err != nil {
		t.Fatal(err)
	}
	s2 := testSnapshot("s2", at(-3*time.Minute))
	s2.Markers = append(s2.Markers,
		extract.Marker{ID: "tombstone-1", Kind: "tombstone", Owner: "Alice", X: 5, Z: 5},
		extract.Marker{ID: "portal-1", Kind: "portal", Label: "far", Owner: "Alice", X: -5000, Z: -5000},
	)
	if err := e.post("alpha", "alpha-token", "snapshot", s2); err != nil {
		t.Fatal(err)
	}
	// s1 -> s2 diffs in the background; wait for it before the caller
	// reads events back.
	e.waitWorld()
}

func TestActivity(t *testing.T) {
	e := newEnv(t)
	activityWorld(t, e)
	cookie := e.mustUnlock("alpha")

	r := e.get("/api/servers/alpha/activity", cookie)
	if r.code != http.StatusOK {
		t.Fatalf("activity = %d %s", r.code, r.body)
	}
	var a activityJSONOut
	r.json(t, &a)
	// Three local days: 27 Sep 00:00 to 30 Sep 00:00 in Oslo (CEST).
	if a.TimeZone != "Europe/Oslo" || a.From != "2026-09-26T22:00:00Z" || a.Until != "2026-09-29T22:00:00Z" || a.Earliest != "2026-09-26T21:30:00Z" {
		t.Fatalf("window: tz %q from %q until %q earliest %q", a.TimeZone, a.From, a.Until, a.Earliest)
	}
	var types []string
	for _, ev := range a.Events {
		types = append(types, ev.Type)
	}
	// Newest first. The explored tombstone (at -3 min) leads; its
	// unexplored portal, at the very same save, is filtered out entirely
	// by the privacy rule below, not merely stripped of x/z. The -4 min
	// autosave collapses into the -3.5 min one.
	want := []string{
		logwatch.EvWorldSaved,  // -3.5 min
		logwatch.EvPlayerLeave, // -5: Bob
		logwatch.EvPlayerJoin,  // -10: Bob
		logwatch.EvWorldSaved,  // -14
		logwatch.EvPlayerJoin,  // -20: Alice
		logwatch.EvRaid,        // -25
		logwatch.EvWorldSaved,  // -26
		logwatch.EvJoinCode,    // -28
		logwatch.EvServerReady, // -29
		logwatch.EvServerBoot,  // -30
		logwatch.EvPlayerJoin,  // 00:30 on 27 Sep: Ulf
	}
	if len(types) != 1+len(want) || types[0] != "world_tombstone" {
		t.Fatalf("events = %v, want world_tombstone then %v", types, want)
	}
	for i, w := range want {
		if types[1+i] != w {
			t.Fatalf("events = %v, want world_tombstone then %v", types, want)
		}
	}
	// death:1 is the explored tombstone; portal:0 because its unexplored
	// portal was dropped entirely, including from this count.
	wantCounts := map[string]int{"session": 4, "death": 1, "boss": 0, "build": 0, "portal": 0, "tame": 0, "event": 1, "server": 6}
	for k, v := range wantCounts {
		if a.Counts[k] != v {
			t.Errorf("counts = %v, want %v", a.Counts, wantCounts)
			break
		}
	}
	byType := map[string]eventJSON{}
	for _, ev := range a.Events {
		byType[ev.Type] = ev
	}
	tomb := byType["world_tombstone"]
	if tomb.Category != "death" || tomb.Source != "save" || tomb.Owner != "Alice" || len(tomb.Who) != 1 || tomb.Who[0] != "111" ||
		tomb.X == nil || *tomb.X != 5 || tomb.Biome == "" || tomb.At != rfc3339(at(-3*time.Minute)) {
		t.Errorf("tombstone = %+v", tomb)
	}
	// The controller's privacy ruling: an event whose own point is
	// unexplored under the current mask must be absent entirely — not
	// just stripped of x/z — from both the list and the counts.
	if portal, ok := byType["world_portal"]; ok {
		t.Errorf("portal in unexplored ground = %+v, want it absent entirely", portal)
	}
	if raid := byType[logwatch.EvRaid]; raid.Category != "event" || raid.Source != "log" || raid.Raid != "army_theelder" || len(raid.Who) != 0 {
		t.Errorf("raid = %+v", raid)
	}
	if save := byType[logwatch.EvWorldSaved]; save.Category != "server" || save.Source != "log" {
		t.Errorf("autosave = %+v, want a server-log entry", save)
	}
	if len(a.People) != 3 || a.People[0].Name != "Alice" || !a.People[0].Online || a.People[0].ID != "111" {
		t.Errorf("people = %+v", a.People)
	}

	// Show earlier: the three days before.
	var earlier activityJSONOut
	e.get("/api/servers/alpha/activity?before="+a.From, cookie).json(t, &earlier)
	if earlier.From != "2026-09-23T22:00:00Z" || earlier.Until != a.From || len(earlier.Events) != 1 || earlier.Events[0].Name != "Ulf" {
		t.Errorf("earlier = %+v", earlier)
	}
	var today activityJSONOut
	e.get("/api/servers/alpha/activity?days=1", cookie).json(t, &today)
	if today.From != "2026-09-28T22:00:00Z" || len(today.Events) != 11 {
		t.Errorf("today: from %q, %d events", today.From, len(today.Events))
	}
}

func TestActivityHidesUnexploredWorldEvents(t *testing.T) {
	// A focused check of the controller's privacy ruling, independent of
	// the larger fixture above: an unexplored tombstone is absent from
	// both the events list and the counts; an explored one is present,
	// with x and z.
	e := newEnv(t)
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-20*time.Minute))); err != nil {
		t.Fatal(err)
	}
	s2 := testSnapshot("s2", at(-10*time.Minute))
	s2.Markers = append(s2.Markers,
		extract.Marker{ID: "tomb-explored", Kind: "tombstone", Owner: "Alice", X: 5, Z: 5},
		extract.Marker{ID: "tomb-unexplored", Kind: "tombstone", Owner: "Bob", X: -5000, Z: -5000},
	)
	if err := e.post("alpha", "alpha-token", "snapshot", s2); err != nil {
		t.Fatal(err)
	}
	e.waitWorld()

	cookie := e.mustUnlock("alpha")
	var a activityJSONOut
	e.get("/api/servers/alpha/activity", cookie).json(t, &a)

	var tombs []eventJSON
	for _, ev := range a.Events {
		if ev.Type == "world_tombstone" {
			tombs = append(tombs, ev)
		}
	}
	if len(tombs) != 1 {
		t.Fatalf("tombstones in the list = %+v, want exactly the explored one", tombs)
	}
	got := tombs[0]
	if got.Owner != "Alice" || got.X == nil || got.Z == nil || *got.X != 5 || *got.Z != 5 {
		t.Errorf("explored tombstone = %+v, want Alice's at (5,5) with x/z", got)
	}
	if a.Counts["death"] != 1 {
		t.Errorf("death count = %d, want 1 (the unexplored tombstone must not be counted)", a.Counts["death"])
	}
}

func TestActivityAndCardHideTimeSkip(t *testing.T) {
	// time_skip has no category: it must never reach either the activity
	// timeline or the card's activity list, though it is stored.
	e := newEnv(t)
	ev := logwatch.Event{ID: "ts1", Type: logwatch.EvTimeSkip, At: at(-5 * time.Minute), To: 488070.000010729}
	if err := e.post("alpha", "alpha-token", "events", map[string]any{"events": []logwatch.Event{ev}}); err != nil {
		t.Fatal(err)
	}
	cookie := e.mustUnlock("alpha")

	var a activityJSONOut
	e.get("/api/servers/alpha/activity", cookie).json(t, &a)
	if len(a.Events) != 0 {
		t.Errorf("activity events = %+v, want time_skip hidden", a.Events)
	}

	var c cardJSON
	e.get("/api/servers/alpha", cookie).json(t, &c)
	for _, got := range c.Activity {
		if got["type"] == logwatch.EvTimeSkip {
			t.Errorf("time_skip in card activity: %v", got)
		}
	}
}

func TestActivityRejectsBadParamsAndLockedServers(t *testing.T) {
	e := newEnv(t)
	cookie := e.mustUnlock("alpha")
	for _, q := range []string{"before=yesterday", "days=0", "days=15", "days=x"} {
		if r := e.get("/api/servers/alpha/activity?"+q, cookie); r.code != http.StatusBadRequest {
			t.Errorf("%s = %d %s, want 400", q, r.code, r.body)
		}
	}
	for _, path := range []string{"/api/servers/beta/activity", "/api/servers/beta/sessions/today", "/api/servers/nope/activity"} {
		if r := e.get(path, cookie); r.code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, r.code)
		}
	}
	if r := e.get("/api/servers/alpha/activity", ""); r.code != http.StatusNotFound {
		t.Errorf("no cookie = %d, want 404", r.code)
	}
	// No events at all: an empty, well-formed window.
	var a activityJSONOut
	r := e.get("/api/servers/alpha/activity", cookie)
	r.json(t, &a)
	if r.code != http.StatusOK || a.Events == nil || a.People == nil || a.Earliest != "" || len(a.Counts) != 8 {
		t.Errorf("empty activity = %d %s", r.code, r.body)
	}
}

func TestSessionsToday(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	crossing := mustTime("2026-09-28T23:00:00Z") // 01:00 on 29 Sep in Oslo
	yesterday := mustTime("2026-09-28T11:00:00Z")
	for _, s := range []store.Session{
		{ServerID: "alpha", Name: "Alice", Platform: "Steam", PlatformID: "111", Since: mustTime("2026-09-28T21:00:00Z"), Until: &crossing},
		{ServerID: "alpha", Name: "Bob", Platform: "Xbox", PlatformID: "222", Since: mustTime("2026-09-28T10:00:00Z"), Until: &yesterday},
	} {
		if err := e.store.InsertClosedSession(ctx, nil, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.store.OpenSession(ctx, nil, store.Session{ServerID: "alpha", Name: "Alice", Platform: "Steam", PlatformID: "111", Since: at(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var d todayJSON
	r := e.get("/api/servers/alpha/sessions/today", e.mustUnlock("alpha"))
	r.json(t, &d)
	if r.code != http.StatusOK || d.TimeZone != "Europe/Oslo" || d.DayStart != "2026-09-28T22:00:00Z" || d.DayEnd != "2026-09-29T22:00:00Z" || d.Now != rfc3339(t0) {
		t.Fatalf("today = %d %s", r.code, r.body)
	}
	if len(d.Players) != 1 {
		t.Fatalf("players = %+v, want only Alice", d.Players)
	}
	p := d.Players[0]
	if p.ID != "111" || p.Name != "Alice" || !p.Online || len(p.Spans) != 2 ||
		p.Spans[0].Since != d.DayStart || p.Spans[0].Until != "2026-09-28T23:00:00Z" ||
		p.Spans[1].Since != rfc3339(at(-time.Hour)) || p.Spans[1].Until != "" {
		t.Fatalf("Alice = %+v", p)
	}
}

// The card's activity carries world-save events with the timeline's fields.
func TestCardActivityHasWorldEvents(t *testing.T) {
	e := newEnv(t)
	activityWorld(t, e)
	var c cardJSONOut
	r := e.get("/api/servers/alpha", e.mustUnlock("alpha"))
	r.json(t, &c)
	if c.TimeZone != "Europe/Oslo" || len(c.Activity) == 0 {
		t.Fatalf("card = %s", r.body)
	}
	top := c.Activity[0]
	if top.Source != "save" || (top.Category != "death" && top.Category != "portal") || len(top.Who) != 1 {
		t.Fatalf("activity[0] = %+v", top)
	}
}
