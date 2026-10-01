package explored

import (
	"math/bits"
	"os"
	"path/filepath"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save"
)

// goldenTruth reads a real save (skipping when testdata-golden/ is absent)
// and returns what the agent sends with tables, the 100 m reveal around
// built pieces on its own, and the save's generated zones.
func goldenTruth(t *testing.T, sub, world string) (truth, reveal *Mask, zones [][2]int16) {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata-golden", sub)
	if _, err := os.Stat(dir); err != nil {
		t.Skip("golden data missing; run hack/pull-golden.sh")
	}
	creator := names.StableHash("creator")
	tables, reveal := New(), New()
	keep := save.ReadOptions{KeepBytes: func(p int32) bool { return p == save.MapTablePrefab }}
	w, err := save.ReadWith(dir, world, keep, func(z *save.ZDO) {
		if z.Longs[creator] != 0 {
			reveal.Reveal(CellOf(float64(z.Pos[0]), float64(z.Pos[2])))
		}
		if b, ok := z.ByteArrays[save.MapDataKey]; ok && z.Prefab == save.MapTablePrefab {
			flags, err := save.DecodeMapData(b)
			if err != nil {
				t.Fatal(err)
			}
			if err := tables.AddFlags(flags); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	truth = tables.Clone()
	truth.Union(reveal)
	return truth, reveal, w.Zones
}

func iou(a, b *Mask) float64 {
	var in, un int
	for i := range a.bits {
		in += bits.OnesCount8(a.bits[i] & b.bits[i])
		un += bits.OnesCount8(a.bits[i] | b.bits[i])
	}
	return float64(in) / float64(un)
}

func fallback(zones *Mask, reveal *Mask, k int) *Mask {
	m := zones.Erode(k)
	m.Union(reveal)
	return m
}

// TestGoldenZoneShrinkCalibration picks ZoneShrinkCells: the zone fallback
// (zones eroded by k cells, plus the reveal around built pieces) is scored
// by intersection-over-union against what the real tables give, for k in
// 0…40, on MuleVikings. The best k must be the constant. If it ever differs
// (new golden data), set ZoneShrinkCells to the k this test reports.
func TestGoldenZoneShrinkCalibration(t *testing.T) {
	truth, reveal, zoneList := goldenTruth(t, "chunked", "MuleVikings")
	zones := FromZones(zoneList)
	best, bestIoU := -1, -1.0
	for k := 0; k <= 40; k++ {
		s := iou(fallback(zones, reveal, k), truth)
		t.Logf("MuleVikings k=%2d IoU=%.4f", k, s)
		if s > bestIoU {
			best, bestIoU = k, s
		}
	}
	if best != ZoneShrinkCells {
		t.Fatalf("best shrink is %d cells (IoU %.4f); ZoneShrinkCells is %d", best, bestIoU, ZoneShrinkCells)
	}
	if bestIoU < 0.71 {
		t.Fatalf("IoU at the best shrink = %.4f, want >= 0.71", bestIoU)
	}
}

// TestGoldenZoneFallbackOnMulennials checks the constant carries over to
// the other save (measured 0.647 there, also its best k).
func TestGoldenZoneFallbackOnMulennials(t *testing.T) {
	truth, reveal, zoneList := goldenTruth(t, "legacy", "Mulennials")
	if s := iou(fallback(FromZones(zoneList), reveal, ZoneShrinkCells), truth); s < 0.64 {
		t.Fatalf("Mulennials IoU = %.4f, want >= 0.64", s)
	}
}
