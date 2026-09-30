package worldgen

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save"
)

// Location prefab -> the biome the game places it in. Confirmed by the
// 2026-09-29 spike (1,440/1,440 on MuleVikings).
var locationBiome = map[string]Biome{
	"Eikthyrnir": Meadows, "GDKing": BlackForest, "Bonemass": Swamp, "Dragonqueen": Mountain,
	"GoblinKing": Plains, "Mistlands_DvergrBossEntrance1": Mistlands, "FaderLocation": AshLands,
	"DN_Bossroom": DeepNorth, "Vendor_BlackForest": BlackForest,
	"Crypt2": BlackForest, "Crypt3": BlackForest, "Crypt4": BlackForest, "TrollCave02": BlackForest,
	"BearCave": BlackForest, "SunkenCrypt4": Swamp, "GoblinCamp2": Plains, "MountainCave02": Mountain,
	"DN_hut01": DeepNorth, "Runestone_DeepNorth": DeepNorth,
}

// goldenWorld reads the golden save at testdata-golden/{sub}/{name} and
// keeps only the ZDOs whose prefab is in prefabs (and whose Y position is
// non-zero, i.e. actually placed on the terrain). prefabs may be nil when
// the caller only wants w.Locations (biomeOracle).
func goldenWorld(t *testing.T, sub, name string, prefabs map[string]bool) (*save.World, []save.ZDO) {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata-golden", sub)
	if _, err := os.Stat(dir); err != nil {
		t.Skip("golden data missing; run hack/pull-golden.sh")
	}
	var keep []save.ZDO
	w, err := save.Read(dir, name, func(z *save.ZDO) {
		if _, ok := prefabs[names.Name(z.Prefab)]; ok && z.Pos[1] != 0 {
			keep = append(keep, save.ZDO{Pos: z.Pos, Prefab: z.Prefab})
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return w, keep
}

// Naturally placed vegetation whose position is the generated ground height.
// Used by the height oracle (Task 4); declared here so goldenWorld can filter.
var vegetationGround = map[string]bool{
	"Beech1": true, "Pinetree_01": true, "FirTree": true, "SwampTree1": true, "Birch1": true,
	"Oak1": true, "Bush01": true, "shrub_2": true, "Pickable_Stone": true, "Pickable_Branch": true,
}

// mulennialsPrefabs extends vegetationGround with naturally placed
// vegetation from Ashlands and Mistlands, the only way to get n >= 500
// samples in those biomes from the 2023 Mulennials save (chunked saves such
// as MuleVikings don't carry enough Ashlands/Mistlands vegetation).
var mulennialsPrefabs = func() map[string]bool {
	m := map[string]bool{
		"AshlandsTree1": true, "AshlandsTree3": true, "cliff_ashlands6": true,
		"rock_mistlands1": true, "YggdrasilRoot": true, "Pickable_Mushroom_JotunPuffs": true,
	}
	for k, v := range vegetationGround {
		m[k] = v
	}
	return m
}()

func biomeOracle(t *testing.T, sub, name string, minPct float64) {
	w, _ := goldenWorld(t, sub, name, nil)
	g := NewBase(w.Meta.Seed, w.Meta.GenVersion)
	total, ok := 0, 0
	miss := map[string]int{}
	for _, l := range w.Locations {
		want, known := locationBiome[names.Name(l.Hash)]
		if !known {
			continue
		}
		total++
		if got := g.Biome(l.Pos[0], l.Pos[2]); got == want {
			ok++
		} else {
			miss[names.Name(l.Hash)+"->"+got.String()]++
		}
	}
	pct := 100 * float64(ok) / float64(total)
	t.Logf("%s: %d/%d locations in expected biome (%.2f%%), misses %v", name, ok, total, pct, miss)
	if total < 500 || pct < minPct {
		t.Fatalf("biome oracle failed: %.2f%% < %.2f%%", pct, minPct)
	}
}

func TestBiomeOracleMuleVikings(t *testing.T) { biomeOracle(t, "chunked", "MuleVikings", 100) }

// Mulennials was generated in 2023, before the Ashlands and Deep North rules
// existed, so far-south and far-north locations may disagree with today's
// GetBiome. The inner world must still match.
func TestBiomeOracleMulennials(t *testing.T) { biomeOracle(t, "legacy", "Mulennials", 97) }

func heightOracle(t *testing.T, sub, name string, maxMedian float64) {
	w, veg := goldenWorld(t, sub, name, vegetationGround)
	if w.Meta.GenVersion < 2 {
		t.Skipf("%s uses worldGenVersion %d; height oracle runs on current-generation worlds", name, w.Meta.GenVersion)
	}
	g := New(w.Meta.Seed, w.Meta.GenVersion)
	perBiome := map[Biome][]float64{}
	var all []float64
	for _, z := range veg {
		d := math.Abs(float64(g.Height(z.Pos[0], z.Pos[2]) - z.Pos[1]))
		b := g.Biome(z.Pos[0], z.Pos[2])
		perBiome[b] = append(perBiome[b], d)
		all = append(all, d)
	}
	med := func(v []float64) float64 { sort.Float64s(v); return v[len(v)/2] }
	p90 := func(v []float64) float64 { sort.Float64s(v); return v[len(v)*9/10] }
	for b, v := range perBiome {
		t.Logf("%-12s n=%-6d median=%.3f m p90=%.3f m", b, len(v), med(v), p90(v))
	}
	m := med(all)
	t.Logf("%s: n=%d median=%.3f m p90=%.3f m", name, len(all), m, p90(all))
	if len(all) < 10000 || m > maxMedian {
		t.Fatalf("height oracle failed: median %.3f m > %.3f m", m, maxMedian)
	}
}

// The spec's target: median height error under 1 m, rivers included.
func TestHeightOracleMuleVikings(t *testing.T) { heightOracle(t, "chunked", "MuleVikings", 1.0) }

// terrainOracle is heightOracle's counterpart for the corner-blended
// TerrainHeight (Task 5b): the game builds zone terrain by blending the
// four corner biomes of each 64 m zone (HeightmapBuilder.Build), not by
// calling GetHeight at each point, so this is the oracle that actually
// matches what the tile renderer draws. It fails if the overall median
// exceeds maxMedian or if any biome with n >= 500 samples exceeds
// maxBiomeMedian. Samples whose biome is in exclude are skipped entirely
// (used to drop DeepNorth on the Mulennials save, generated before the
// current Deep North rules existed). Unlike heightOracle, this does not
// skip on genVersion < 2: TerrainHeight only calls Biome and the ported
// per-biome height functions, none of which are genVersion-gated, so a
// legacy (gen 1) save is just as valid an oracle as a current one.
func terrainOracle(t *testing.T, sub, name string, prefabs map[string]bool, maxMedian, maxBiomeMedian float64, exclude ...Biome) {
	w, veg := goldenWorld(t, sub, name, prefabs)
	excl := map[Biome]bool{}
	for _, b := range exclude {
		excl[b] = true
	}
	g := New(w.Meta.Seed, w.Meta.GenVersion)
	perBiome := map[Biome][]float64{}
	var all []float64
	for _, z := range veg {
		b := g.Biome(z.Pos[0], z.Pos[2])
		if excl[b] {
			continue
		}
		d := math.Abs(float64(g.TerrainHeight(z.Pos[0], z.Pos[2]) - z.Pos[1]))
		perBiome[b] = append(perBiome[b], d)
		all = append(all, d)
	}
	med := func(v []float64) float64 { sort.Float64s(v); return v[len(v)/2] }
	p90 := func(v []float64) float64 { sort.Float64s(v); return v[len(v)*9/10] }
	failed := false
	for b, v := range perBiome {
		bm := med(v)
		t.Logf("%-12s n=%-6d median=%.3f m p90=%.3f m", b, len(v), bm, p90(v))
		if len(v) >= 500 && bm > maxBiomeMedian {
			t.Errorf("terrain oracle failed: biome %s median %.3f m > %.3f m (n=%d)", b, bm, maxBiomeMedian, len(v))
			failed = true
		}
	}
	m := med(all)
	t.Logf("%s: n=%d median=%.3f m p90=%.3f m", name, len(all), m, p90(all))
	if len(all) < 10000 || m > maxMedian {
		t.Errorf("terrain oracle failed: overall median %.3f m > %.3f m", m, maxMedian)
		failed = true
	}
	if failed {
		t.FailNow()
	}
}

// The Task 4 scratch check measured about 0.02 m for every biome once
// corner blending is applied, so both bars are set well above that.
func TestTerrainOracleMuleVikings(t *testing.T) {
	terrainOracle(t, "chunked", "MuleVikings", vegetationGround, 0.1, 0.5)
}

// TestTerrainOracleMulennials is the only committed oracle coverage for
// Ashlands and Mistlands: MuleVikings (a modern chunked save) doesn't carry
// enough vegetation in those biomes, but the 2023 Mulennials save does.
// DeepNorth is excluded because that world predates the current Deep North
// rules. The reviewer's probe measured about 0.001-0.005 m for Ashlands and
// about 0.04-0.06 m for Mistlands, well under the 0.1 m bar required of
// every biome with n >= 500 samples.
func TestTerrainOracleMulennials(t *testing.T) {
	terrainOracle(t, "legacy", "Mulennials", mulennialsPrefabs, 0.1, 0.1, DeepNorth)
}

// TestRiverOracleMuleVikings is the river-sensitive check: vegetation that
// sits inside a river's influence (GetWeight weight > 0). Without rivers
// this subset's median error is about 4 m; with them it is about 0.03 m.
func TestRiverOracleMuleVikings(t *testing.T) {
	w, veg := goldenWorld(t, "chunked", "MuleVikings", vegetationGround)
	g := New(w.Meta.Seed, w.Meta.GenVersion)
	var d []float64
	for _, z := range veg {
		if wgt, _ := g.riverWeight(z.Pos[0], z.Pos[2]); wgt > 0 {
			d = append(d, math.Abs(float64(g.Height(z.Pos[0], z.Pos[2])-z.Pos[1])))
		}
	}
	if len(d) < 1000 {
		t.Fatalf("only %d samples inside river influence", len(d))
	}
	sort.Float64s(d)
	med, p90 := d[len(d)/2], d[len(d)*9/10]
	t.Logf("MuleVikings near-river: n=%d median=%.3f m p90=%.3f m", len(d), med, p90)
	if med > 0.1 || p90 > 1.0 {
		t.Fatalf("river oracle failed: median %.3f m (max 0.1), p90 %.3f m (max 1.0)", med, p90)
	}
}
