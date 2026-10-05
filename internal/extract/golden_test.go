package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/save"
)

func TestGoldenMuleVikings(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata-golden", "chunked")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("golden data missing; run hack/pull-golden.sh")
	}
	e := New()
	w, err := save.ReadWith(dir, "MuleVikings", save.ReadOptions{KeepBytes: KeepBytes}, e.Add)
	if err != nil {
		t.Fatal(err)
	}
	s := e.Finish(w, "mulevikings", time.Now().UTC())
	count := map[string]int{}
	paired := 0
	for _, m := range s.Markers {
		count[m.Kind]++
		if m.Kind == "portal" && m.Pair != "" {
			paired++
		}
	}
	t.Logf("markers=%v bases=%d players=%d locations=%d", count, len(s.Bases), len(s.Players), len(s.Locations))
	if count["portal"] < 10 || paired < 10 || count["bed"] < 5 || count["tame"] < 1 {
		t.Fatalf("too few markers: %v (paired portals %d)", count, paired)
	}
	if len(s.Bases) < 2 || s.Bases[0].Name == "Base" {
		t.Fatalf("bases = %+v", s.Bases)
	}
	if len(s.Players) < 5 || s.Stats.UnknownPrefabs != 0 || s.World.SeedName == "" {
		t.Fatalf("players=%d unknown=%d seed=%q", len(s.Players), s.Stats.UnknownPrefabs, s.World.SeedName)
	}
	// Owners and namers (Plan 7): every portal owner is a player the save
	// names, and every namer is a platform user ID ("Platform_ID").
	known := map[string]bool{}
	for _, p := range s.Players {
		known[p.Name] = true
	}
	owned, named := 0, 0
	for _, m := range s.Markers {
		if m.Kind == "portal" && m.Owner != "" {
			owned++
			if !known[m.Owner] {
				t.Errorf("portal %s owner %q is not in players", m.ID, m.Owner)
			}
		}
		if m.Kind == "tame" && m.Namer != "" {
			named++
			if !strings.Contains(m.Namer, "_") {
				t.Errorf("tame %s namer %q is not Platform_ID", m.ID, m.Namer)
			}
		}
	}
	t.Logf("portals with an owner: %d of %d; tames with a namer: %d of %d", owned, count["portal"], named, count["tame"])
	if owned == 0 {
		t.Errorf("no portal has an owner")
	}
	// Boss altars survive explored-only filtering (I3, Finish keeps only
	// raw locations inside its own explored mask): this used to check all
	// 8 were present in the unfiltered location plan, which every world
	// has regardless of what's been explored. Now a location only reaches
	// Snapshot.Locations once it's been explored, so this save's players
	// having stood near at least one altar is what's left to check.
	altars := 0
	for _, l := range ClassifyLocations(s.Locations) {
		if l.Kind == "boss_altar" {
			altars++
		}
	}
	if altars == 0 || s.World.Modifiers["portals"] != "casual" {
		t.Fatalf("altars=%d modifiers=%v", altars, s.World.Modifiers)
	}
}

// TestGoldenExplored checks the real saves' masks: built from the tables,
// at least the tables' own cells, and every bed and portal on an explored
// cell (the 100 m reveal covers the ones built after the last "Record").
func TestGoldenExplored(t *testing.T) {
	for _, c := range []struct {
		sub, world string
		tableCells int
	}{{"chunked", "MuleVikings", 63338}, {"legacy", "Mulennials", 353206}} {
		dir := filepath.Join("..", "..", "testdata-golden", c.sub)
		if _, err := os.Stat(dir); err != nil {
			t.Skip("golden data missing; run hack/pull-golden.sh")
		}
		e := New()
		w, err := save.ReadWith(dir, c.world, save.ReadOptions{KeepBytes: KeepBytes}, e.Add)
		if err != nil {
			t.Fatal(err)
		}
		s := e.Finish(w, "x", time.Now().UTC())
		if s.Explored == nil || s.Explored.Source != explored.SourceTables {
			t.Fatalf("%s: explored = %+v, want tables", c.world, s.Explored)
		}
		m, err := explored.Decode(*s.Explored)
		if err != nil {
			t.Fatal(err)
		}
		if m.Count() < c.tableCells {
			t.Errorf("%s: %d cells, want >= %d", c.world, m.Count(), c.tableCells)
		}
		for _, mk := range s.Markers {
			if (mk.Kind == "bed" || mk.Kind == "portal") && !m.At(float64(mk.X), float64(mk.Z)) {
				t.Errorf("%s: %s at (%.0f, %.0f) is not explored", c.world, mk.ID, mk.X, mk.Z)
			}
		}
		t.Logf("%s: %d cells explored (%.1f%% of the world)", c.world, m.Count(), m.Percent())
	}
}
