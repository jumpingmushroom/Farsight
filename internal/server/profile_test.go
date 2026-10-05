package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/store"
)

// profileWorld seeds alpha with Alice (Steam 111): a session across
// midnight in Oslo two days ago and one open since an hour ago; Bob
// (Xbox 222) once; and a save holding Alice's bed, portal, tame,
// tombstone and base, plus things that are not hers or are unexplored.
func profileWorld(t *testing.T, e *env) {
	t.Helper()
	ctx := t.Context()
	until := mustTime("2026-09-27T23:30:00Z") // 01:30 on 28 Sep in Oslo
	for _, s := range []store.Session{
		{ServerID: "alpha", Name: "Alice", Platform: "Steam", PlatformID: "111", Since: mustTime("2026-09-27T21:00:00Z"), Until: &until, Seconds: 9000, Reason: "left"},
		{ServerID: "alpha", Name: "Bob", Platform: "Xbox", PlatformID: "222", Since: mustTime("2026-09-28T10:00:00Z"), Until: &until, Reason: "left"},
	} {
		if err := e.store.InsertClosedSession(ctx, nil, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.store.OpenSession(ctx, nil, store.Session{ServerID: "alpha", Name: "Alice", Platform: "Steam", PlatformID: "111", Since: at(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	snap := testSnapshot("s1", at(-3*time.Minute))
	snap.Bases[0].Builders = []extract.Builder{{ID: 1, Name: "Alice", Pieces: 60}, {ID: 2, Name: "Bob", Pieces: 40}}
	snap.Markers = append(snap.Markers,
		extract.Marker{ID: "bed-1", Kind: "bed", Owner: "Alice", X: 2, Z: 2},
		extract.Marker{ID: "bed-2", Kind: "bed", Owner: "Alice", X: -5000, Z: -5000}, // unexplored
		extract.Marker{ID: "portal-1", Kind: "portal", Label: "home", Owner: "Alice", X: 10, Z: 10, Pair: "portal-3"},
		extract.Marker{ID: "portal-3", Kind: "portal", Label: "home", X: -5000, Z: -5000, Pair: "portal-1"}, // unexplored partner
		extract.Marker{ID: "portal-2", Kind: "portal", Label: "bobs", Owner: "Bob", X: 11, Z: 11, Pair: "portal-4"},
		extract.Marker{ID: "portal-4", Kind: "portal", Label: "bobs", X: 40, Z: 40, Pair: "portal-2"}, // explored partner
		extract.Marker{ID: "tame-1", Kind: "tame", Species: "Lox", Label: "Big Mama", Namer: "Steam_111", X: 20, Z: 20},
		extract.Marker{ID: "tame-2", Kind: "tame", Species: "Wolf", Label: "Grey", Namer: "Xbox_222", X: 21, Z: 21},
		extract.Marker{ID: "tombstone-1", Kind: "tombstone", Owner: "Alice", X: 30, Z: 30},
	)
	if err := e.post("alpha", "alpha-token", "snapshot", snap); err != nil {
		t.Fatal(err)
	}
}

func TestProfile(t *testing.T) {
	e := newEnv(t)
	profileWorld(t, e)
	cookie := e.mustUnlock("alpha")

	r := e.get("/api/servers/alpha/players/111", cookie)
	if r.code != http.StatusOK {
		t.Fatalf("profile = %d %s", r.code, r.body)
	}
	var p profileJSON
	r.json(t, &p)
	if p.ID != "111" || p.Name != "Alice" || p.Platform != "Steam" || p.TimeZone != "Europe/Oslo" {
		t.Fatalf("identity = %+v", p)
	}
	if !p.Online || p.Since != rfc3339(at(-time.Hour)) || p.LastSeen != "" {
		t.Fatalf("status: online=%v since=%q lastSeen=%q", p.Online, p.Since, p.LastSeen)
	}
	if p.FirstSeen != "2026-09-27T21:00:00Z" || p.TrackedSince != p.FirstSeen || p.Sessions != 2 {
		t.Fatalf("first seen %q tracked since %q sessions %d", p.FirstSeen, p.TrackedSince, p.Sessions)
	}
	// Now is 14:00 on 29 Sep in Oslo: 23 … 29 Sep. The closed session is
	// 1 h on 27 Sep and 1.5 h on 28 Sep; the open one 1 h today.
	want := []int64{0, 0, 0, 0, 3600, 5400, 3600}
	if len(p.Days) != 7 || p.Days[0].Date != "2026-09-23" || p.Days[6].Date != "2026-09-29" {
		t.Fatalf("days = %+v", p.Days)
	}
	for i, d := range p.Days {
		if d.Seconds != want[i] {
			t.Fatalf("days = %+v, want seconds %v", p.Days, want)
		}
	}
	if p.WeekSeconds != 12600 || p.AllSeconds != 12600 {
		t.Fatalf("week %d all %d, want 12600 each", p.WeekSeconds, p.AllSeconds)
	}
	if p.Beds.Count != 1 || len(p.Beds.Near) != 1 || p.Beds.Near[0] != "Home" {
		t.Fatalf("beds = %+v", p.Beds)
	}
	if len(p.Bases) != 1 || p.Bases[0].Name != "Home" || p.Bases[0].Pieces != 100 || p.Bases[0].Biome == "" {
		t.Fatalf("bases = %+v", p.Bases)
	}
	if len(p.Portals) != 1 || p.Portals[0].Tag != "home" || p.Portals[0].Paired {
		t.Fatalf("portals = %+v, want home, unpaired (its partner is unexplored)", p.Portals)
	}
	if len(p.Tames) != 1 || p.Tames[0].Name != "Big Mama" || p.Tames[0].Species != "Lox" {
		t.Fatalf("tames = %+v", p.Tames)
	}
	if p.Deaths.Spotted != 1 || p.Deaths.Week != 1 || len(p.Deaths.Tombstones) != 1 ||
		p.Deaths.Tombstones[0].FirstSeen != rfc3339(at(-3*time.Minute)) {
		t.Fatalf("deaths = %+v", p.Deaths)
	}

	var bob profileJSON
	e.get("/api/servers/alpha/players/222", cookie).json(t, &bob)
	if bob.Online || bob.LastSeen != "2026-09-27T23:30:00Z" || len(bob.Tames) != 1 || len(bob.Portals) != 1 || !bob.Portals[0].Paired ||
		len(bob.Bases) != 1 || bob.Deaths.Spotted != 0 {
		t.Fatalf("bob = %+v", bob)
	}
}

func TestProfileNotFound(t *testing.T) {
	e := newEnv(t)
	profileWorld(t, e)
	cookie := e.mustUnlock("alpha")
	for _, c := range []struct{ path, cookie string }{
		{"/api/servers/alpha/players/999", cookie}, // unknown player
		{"/api/servers/alpha/players/111", ""},     // locked
		{"/api/servers/beta/players/111", cookie},  // beta not unlocked
		{"/api/servers/nope/players/111", cookie},  // unknown server
	} {
		if r := e.get(c.path, c.cookie); r.code != http.StatusNotFound || !contains(string(r.body), "not found") {
			t.Errorf("%s (cookie %v) = %d %s, want 404", c.path, c.cookie != "", r.code, r.body)
		}
	}
}

// Without a save yet, the profile still has playtime and empty save data.
func TestProfileWithoutASave(t *testing.T) {
	e := newEnv(t)
	if err := e.store.OpenSession(t.Context(), nil, store.Session{ServerID: "alpha", Name: "Alice", Platform: "Steam", PlatformID: "111", Since: at(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var p profileJSON
	r := e.get("/api/servers/alpha/players/111", e.mustUnlock("alpha"))
	r.json(t, &p)
	if r.code != http.StatusOK || p.WeekSeconds != 3600 || p.Bases == nil || p.Portals == nil || p.Tames == nil || p.Beds.Near == nil || p.Deaths.Tombstones == nil {
		t.Fatalf("profile = %d %+v", r.code, p)
	}
}
