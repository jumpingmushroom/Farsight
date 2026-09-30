package extract

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/save"
)

func TestGoldenMuleVikings(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata-golden", "chunked")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("golden data missing; run hack/pull-golden.sh")
	}
	e := New()
	w, err := save.Read(dir, "MuleVikings", e.Add)
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
	altars := 0
	for _, l := range s.Locations {
		if l.Kind == "boss_altar" {
			altars++
		}
	}
	if altars < 8 || s.World.Modifiers["portals"] != "casual" {
		t.Fatalf("altars=%d modifiers=%v", altars, s.World.Modifiers)
	}
}
