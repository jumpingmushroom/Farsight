package worldevents

import (
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
)

// fakeGeo: west of x = 0 is Swamp, east is Meadows; everything west of
// x = -5000 is unexplored.
type fakeGeo struct{}

func (fakeGeo) Biome(x, z float32) string {
	if x < 0 {
		return "Swamp"
	}
	return "Meadows"
}
func (fakeGeo) Explored(x, z float32) bool { return x > -5000 }

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// world is a small synthetic save: two paired portals, an unpaired one, a
// tombstone, two tames (one named), a base, a sunken crypt and Eikthyr
// defeated.
func world(saveID string, at time.Time) *extract.Snapshot {
	return &extract.Snapshot{
		ServerID: "srv", SaveID: saveID, SavedAt: at,
		GlobalKeys: []string{"defeated_eikthyr"},
		Locations: []extract.Marker{
			{ID: "loc-1", Kind: "dungeon", Label: "Sunken crypt", X: -1000, Z: -900},
			{ID: "loc-2", Kind: "dungeon", Label: "Burial chambers", X: 500, Z: 500},
			{ID: "loc-3", Kind: "trader", Label: "Haldor", X: -6000, Z: 0}, // unexplored
		},
		Markers: []extract.Marker{
			{ID: "portal-1", Kind: "portal", Label: "home", X: 10, Z: 10, Pair: "portal-2", Owner: "Astrid"},
			{ID: "portal-2", Kind: "portal", Label: "home", X: 3000, Z: 10, Pair: "portal-1", Owner: "Astrid"},
			{ID: "portal-3", Kind: "portal", Label: "copper", X: 900, Z: -1200, Owner: "Bjorn"},
			{ID: "tombstone-1", Kind: "tombstone", Owner: "Bjorn", X: -1100, Z: -800},
			{ID: "tame-1", Kind: "tame", Species: "Lox", Label: "Big Mama", X: 160, Z: -40},
			{ID: "tame-2", Kind: "tame", Species: "Hen", X: 95, Z: -60},
		},
		Bases: []extract.Base{
			{ID: "base-1", Name: "Astrid's base", X: 128, Z: -80, Radius: 60, Pieces: 400,
				Builders: []extract.Builder{{ID: 1, Name: "Astrid", Pieces: 300}, {ID: 2, Pieces: 100}}},
		},
	}
}

func types(evs []Event) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

func TestDiffFirstSaveAndUnchangedSaveYieldNothing(t *testing.T) {
	a := world("chunked:1", t0)
	if evs := Diff(nil, a, fakeGeo{}); len(evs) != 0 {
		t.Fatalf("first save: %+v", evs)
	}
	b := world("chunked:2", t0.Add(20*time.Minute))
	// Same world, objects a little shuffled and the tames wandered off.
	b.Markers[0], b.Markers[1] = b.Markers[1], b.Markers[0]
	b.Markers[4].X, b.Markers[4].Z = 400, 300
	if evs := Diff(a, b, fakeGeo{}); len(evs) != 0 {
		t.Fatalf("unchanged save: %+v", evs)
	}
}

func TestDiffNewTombstoneWithPlace(t *testing.T) {
	a := world("chunked:1", t0)
	b := world("chunked:2", t0.Add(20*time.Minute))
	b.Markers = append(b.Markers,
		extract.Marker{ID: "tombstone-2", Kind: "tombstone", Owner: "Astrid", X: -1050, Z: -950}, // 71 m from the crypt
		extract.Marker{ID: "tombstone-3", Kind: "tombstone", Owner: "Bjorn", X: 2000, Z: 2000},   // nothing near
		extract.Marker{ID: "tombstone-4", Kind: "tombstone", Owner: "Bjorn", X: -1102, Z: -801},  // Bjorn's old one, re-read
	)
	b.Markers = append(b.Markers[:3], b.Markers[4:]...) // drop tombstone-1: -1102,-801 is within MatchRadius of it
	evs := Diff(a, b, fakeGeo{})
	if len(evs) != 2 {
		t.Fatalf("events = %+v", evs)
	}
	e := evs[0]
	if e.Type != TypeTombstone || e.Owner != "Astrid" || e.Near != "a sunken crypt" || e.Biome != "Swamp" ||
		e.Pos == nil || e.Pos.X != -1050 || !e.At.Equal(b.SavedAt) || e.SaveID != "chunked:2" {
		t.Fatalf("tombstone = %+v", e)
	}
	if evs[1].Owner != "Bjorn" || evs[1].Near != "" || evs[1].Biome != "Meadows" {
		t.Fatalf("second tombstone = %+v", evs[1])
	}
	if again := Diff(a, b, fakeGeo{}); again[0].ID != e.ID || again[1].ID != evs[1].ID {
		t.Fatal("ids differ between runs")
	}
}

func TestDiffPortals(t *testing.T) {
	a := world("chunked:1", t0)
	a.Markers = append(a.Markers, extract.Marker{ID: "portal-4", Kind: "portal", Label: "swamp", X: -200, Z: -200})
	b := world("chunked:2", t0.Add(20*time.Minute))
	b.Markers = append(b.Markers,
		// The old "swamp" portal now has a partner that's new: one event, for the new one.
		extract.Marker{ID: "portal-4", Kind: "portal", Label: "swamp", X: -200, Z: -200, Pair: "portal-5"},
		extract.Marker{ID: "portal-5", Kind: "portal", Label: "swamp", X: -900, Z: 400, Pair: "portal-4", Owner: "Ulf"},
		extract.Marker{ID: "portal-6", Kind: "portal", Label: "lonely", X: 50, Z: 50, Owner: "Sigrun"},
	)
	evs := Diff(a, b, fakeGeo{})
	if got := types(evs); len(got) != 2 || got[0] != TypePortal || got[1] != TypePortal {
		t.Fatalf("types = %v (%+v)", got, evs)
	}
	if e := evs[0]; e.Tag != "swamp" || !e.Paired || e.Owner != "Ulf" || e.Pos.X != -900 {
		t.Fatalf("swamp = %+v", e)
	}
	if e := evs[1]; e.Tag != "lonely" || e.Paired || e.Owner != "Sigrun" {
		t.Fatalf("lonely = %+v", e)
	}

	// Later, two old unpaired portals become a pair (one was retagged).
	c := world("chunked:3", t0.Add(40*time.Minute))
	c.Markers = append(c.Markers,
		extract.Marker{ID: "portal-4", Kind: "portal", Label: "swamp", X: -200, Z: -200, Pair: "portal-5"},
		extract.Marker{ID: "portal-5", Kind: "portal", Label: "swamp", X: -900, Z: 400, Pair: "portal-4", Owner: "Ulf"},
		extract.Marker{ID: "portal-6", Kind: "portal", Label: "copper", X: 50, Z: 50, Pair: "portal-3", Owner: "Sigrun"},
	)
	c.Markers[2].Pair = "portal-6" // the copper portal from world()
	evs = Diff(b, c, fakeGeo{})
	// portal-6 changed tag, so it is a new portal (paired at once); copper's
	// partner is new, so copper itself reports nothing.
	if got := types(evs); len(got) != 1 || got[0] != TypePortal || !evs[0].Paired || evs[0].Tag != "copper" {
		t.Fatalf("retag: %+v", evs)
	}

	// Two old portals that become paired without either being new.
	d := world("chunked:4", t0.Add(60*time.Minute))
	d.Markers = append(d.Markers, extract.Marker{ID: "portal-4", Kind: "portal", Label: "copper", X: 2000, Z: 2000})
	e := world("chunked:5", t0.Add(80*time.Minute))
	e.Markers = append(e.Markers, extract.Marker{ID: "portal-4", Kind: "portal", Label: "copper", X: 2000, Z: 2000, Pair: "portal-3"})
	e.Markers[2].Pair = "portal-4"
	evs = Diff(d, e, fakeGeo{})
	// Fix round 1 (M4): one event per pair, not one per end.
	if got := types(evs); len(got) != 1 || got[0] != TypePortalPaired {
		t.Fatalf("pairing later: %+v", evs)
	}
	if evs[0].Tag != "copper" || evs[0].Owner != "Bjorn" {
		t.Fatalf("paired = %+v", evs[0])
	}
}

// Fix round 1 (I1): a portal's partner being in unexplored ground must
// never be revealed, whether as a new portal's paired flag or as a
// world_portal_paired event once both ends were already old.
func TestDiffPortalsHideUnexploredPartner(t *testing.T) {
	a := world("chunked:1", t0)
	a.Markers = append(a.Markers, extract.Marker{ID: "portal-9", Kind: "portal", Label: "hidden", X: -7000, Z: 5})
	b := world("chunked:2", t0.Add(20*time.Minute))
	b.Markers = append(b.Markers,
		// A brand-new portal paired with an existing one in unexplored ground.
		extract.Marker{ID: "portal-9", Kind: "portal", Label: "hidden", X: -7000, Z: 5, Pair: "portal-10"},
		extract.Marker{ID: "portal-10", Kind: "portal", Label: "hidden", X: 700, Z: 700, Pair: "portal-9"},
	)
	evs := Diff(a, b, fakeGeo{})
	if len(evs) != 1 {
		t.Fatalf("events = %+v", evs)
	}
	if e := evs[0]; e.Type != TypePortal || e.Tag != "hidden" || e.Paired {
		t.Fatalf("new portal with an unexplored partner must not read as paired: %+v", e)
	}

	// Two already-old portals become paired, but one sits in unexplored
	// ground: no world_portal_paired event reveals the pairing.
	c := world("chunked:3", t0.Add(40*time.Minute))
	c.Markers = append(c.Markers, extract.Marker{ID: "portal-9", Kind: "portal", Label: "copper", X: -7000, Z: 5})
	d := world("chunked:4", t0.Add(60*time.Minute))
	d.Markers = append(d.Markers, extract.Marker{ID: "portal-9", Kind: "portal", Label: "copper", X: -7000, Z: 5, Pair: "portal-3"})
	d.Markers[2].Pair = "portal-9" // world()'s portal-3
	if evs := Diff(c, d, fakeGeo{}); len(evs) != 0 {
		t.Fatalf("pairing with an unexplored end: %+v", evs)
	}
}

func TestDiffTames(t *testing.T) {
	a := world("chunked:1", t0)
	b := world("chunked:2", t0.Add(20*time.Minute))
	b.Markers = append(b.Markers,
		extract.Marker{ID: "tame-3", Kind: "tame", Species: "Wolf", Label: "Skoll", Namer: "Steam_1", X: -300, Z: 0},
		extract.Marker{ID: "tame-4", Kind: "tame", Species: "Wolf", X: -310, Z: 0}, // unnamed: not reported
	)
	// The agent update adds namers to existing tames: not a new tame.
	b.Markers[4].Namer = "Steam_2"
	evs := Diff(a, b, fakeGeo{})
	if len(evs) != 1 {
		t.Fatalf("events = %+v", evs)
	}
	if e := evs[0]; e.Type != TypeTame || e.Name != "Skoll" || e.Species != "Wolf" || e.Namer != "Steam_1" || e.Biome != "Swamp" {
		t.Fatalf("tame = %+v", e)
	}
	// A second Big Mama is a new tame too.
	c := world("chunked:3", t0.Add(40*time.Minute))
	c.Markers = append(c.Markers, extract.Marker{ID: "tame-5", Kind: "tame", Species: "Lox", Label: "Big Mama", X: 600, Z: 0})
	if evs := Diff(a, c, fakeGeo{}); len(evs) != 1 || evs[0].Name != "Big Mama" || evs[0].Pos.X != 600 {
		t.Fatalf("second Big Mama: %+v", evs)
	}
}

func TestDiffBases(t *testing.T) {
	a := world("chunked:1", t0)
	b := world("chunked:2", t0.Add(20*time.Minute))
	b.Bases[0].Pieces = 424 // +24: under GrowthMin
	b.Bases[0].X += 20      // the centre drifts as it grows
	b.Bases = append(b.Bases, extract.Base{ID: "base-2", Name: "Ulf's base", X: -2000, Z: 50, Radius: 30, Pieces: 60,
		Builders: []extract.Builder{{ID: 4, Name: "Ulf", Pieces: 60}}})
	evs := Diff(a, b, fakeGeo{})
	if len(evs) != 1 {
		t.Fatalf("events = %+v", evs)
	}
	if e := evs[0]; e.Type != TypeBaseNew || e.Name != "Ulf's base" || e.Pieces != 60 || e.Biome != "Swamp" ||
		len(e.Builders) != 1 || e.Builders[0] != "Ulf" {
		t.Fatalf("new base = %+v", e)
	}
	c := world("chunked:3", t0.Add(40*time.Minute))
	c.Bases[0].Pieces = 425 // +25 on chunked:1
	evs = Diff(a, c, fakeGeo{})
	if len(evs) != 1 || evs[0].Type != TypeBaseGrew || evs[0].Grew != 25 || evs[0].Pieces != 425 ||
		len(evs[0].Builders) != 1 || evs[0].Builders[0] != "Astrid" {
		t.Fatalf("grown base = %+v", evs)
	}
}

// Fix round 1 (M2): bases match one to one, so a merge or a split cannot
// double-match the same previous (or current) base.
func TestDiffBasesOneToOneMatching(t *testing.T) {
	// A merge: two old camps (400 + 100 pieces) are replaced by one new,
	// bigger camp (505) too far from either original centre (beyond its
	// radius plus 32 m) to match. Rather than crediting it with "grew by
	// 105" (most of which is Camp B's pre-existing pieces, not new
	// building), it is reported as a new base.
	a := world("chunked:1", t0)
	a.Bases = append(a.Bases,
		extract.Base{ID: "base-m1", Name: "Camp A", X: 6000, Z: 6000, Radius: 40, Pieces: 400},
		extract.Base{ID: "base-m2", Name: "Camp B", X: 6200, Z: 6000, Radius: 20, Pieces: 100},
	)
	b := world("chunked:2", t0.Add(20*time.Minute))
	b.Bases = append(b.Bases, extract.Base{ID: "base-merged", Name: "Camp A", X: 6100, Z: 6000, Radius: 60, Pieces: 505})
	evs := Diff(a, b, fakeGeo{})
	var merged *Event
	for i := range evs {
		if evs[i].Name == "Camp A" && evs[i].Pieces == 505 {
			merged = &evs[i]
		}
	}
	if merged == nil || merged.Type != TypeBaseNew {
		t.Fatalf("merged camp = %+v (events %+v)", merged, evs)
	}
	for _, e := range evs {
		if e.Type == TypeBaseGrew && e.Grew == 105 {
			t.Fatalf("growth inflated by the merge: %+v", e)
		}
	}

	// A split: one old base (300 pieces) becomes two current ones. Only the
	// nearer one (at the same spot, shrunk to 150: no event, since it
	// didn't grow) matches; the other, 60 m away but still within the old
	// base's radius (50) plus baseLink, can no longer also match the same
	// old base, so it is reported as new rather than silently ignored or
	// mis-matched.
	c := world("chunked:3", t0.Add(40*time.Minute))
	c.Bases = append(c.Bases, extract.Base{ID: "base-s", Name: "Old Hall", X: 8000, Z: 8000, Radius: 50, Pieces: 300})
	d := world("chunked:4", t0.Add(60*time.Minute))
	d.Bases = append(d.Bases,
		extract.Base{ID: "base-s1", Name: "Old Hall", X: 8000, Z: 8000, Radius: 50, Pieces: 150},
		extract.Base{ID: "base-s2", Name: "New Wing", X: 8060, Z: 8000, Radius: 20, Pieces: 160},
	)
	evs = Diff(c, d, fakeGeo{})
	if len(evs) != 1 || evs[0].Type != TypeBaseNew || evs[0].Name != "New Wing" || evs[0].Pieces != 160 {
		t.Fatalf("split's second half = %+v", evs)
	}
}

func TestDiffBosses(t *testing.T) {
	a := world("chunked:1", t0)
	b := world("chunked:2", t0.Add(20*time.Minute))
	b.GlobalKeys = []string{"defeated_eikthyr", "defeated_gdking", "killedtroll", "defeated_frozenking"}
	evs := Diff(a, b, fakeGeo{})
	if len(evs) != 2 || evs[0].Boss != "The Elder" || evs[1].Boss != "Kall Fimbulbringer" || evs[0].Pos != nil {
		t.Fatalf("bosses = %+v", evs)
	}
	// A previous snapshot stored before global keys were sent: its own
	// boss list counts.
	old := world("chunked:0", t0.Add(-20*time.Minute))
	old.GlobalKeys = nil
	old.Bosses = []extract.Boss{{Key: "defeated_eikthyr", Name: "Eikthyr", Defeated: true}}
	if evs := Diff(old, a, fakeGeo{}); len(evs) != 0 {
		t.Fatalf("keys appearing must not re-report Eikthyr: %+v", evs)
	}
}

// Fix round 1 (M3): the boss fallback to a snapshot's own boss list is
// symmetric. Both saves here predate global keys; a boss defeated between
// them must still be reported, not lost until the next keyed save (which
// would otherwise treat it as already defeated).
func TestDiffBossesBothPredateGlobalKeys(t *testing.T) {
	a := world("chunked:1", t0)
	a.GlobalKeys = nil
	a.Bosses = []extract.Boss{{Key: "defeated_eikthyr", Name: "Eikthyr", Defeated: true}}
	b := world("chunked:2", t0.Add(20*time.Minute))
	b.GlobalKeys = nil
	b.Bosses = []extract.Boss{
		{Key: "defeated_eikthyr", Name: "Eikthyr", Defeated: true},
		{Key: "defeated_gdking", Name: "The Elder", Defeated: true},
	}
	evs := Diff(a, b, fakeGeo{})
	if len(evs) != 1 || evs[0].Type != TypeBoss || evs[0].Boss != "The Elder" {
		t.Fatalf("bosses (both pre-keys) = %+v", evs)
	}
}

func TestNearWording(t *testing.T) {
	locs := world("x", t0).Locations
	locs = append(locs,
		extract.Marker{Kind: "boss_altar", Label: "Moder", X: 5000, Z: 5000},
		extract.Marker{Kind: "trader", Label: "Hildir", X: -4000, Z: 4000},
		// Fix round 1 (M5): explored itself, close to the unexplored
		// boundary, so it can be "near" a point just the other side of it.
		extract.Marker{Kind: "trader", Label: "Hildir2", X: -4800, Z: 0},
	)
	for _, c := range []struct {
		x, z float32
		want string
	}{
		{-1000, -700, "a sunken crypt"},
		{520, 480, "burial chambers"},
		{5100, 5100, "Moder’s altar"},
		{-4100, 4000, "Hildir"},
		{-6000, 50, ""}, // Haldor is there but unexplored
		{0, 0, ""},
		// Fix round 1 (M5): the event's own point must be explored, not
		// just the nearby location: Hildir2 is explored and 250 m away,
		// but the point itself (x = -5050) isn't.
		{-5050, 0, ""},
	} {
		if got := Near(locs, fakeGeo{}, c.x, c.z); got != c.want {
			t.Errorf("Near(%v, %v) = %q, want %q", c.x, c.z, got, c.want)
		}
	}
}
