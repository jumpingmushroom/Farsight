package extract

import (
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/save"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
)

// decoded is the snapshot's explored mask, decoded.
func decoded(t *testing.T, s *Snapshot) *explored.Mask {
	t.Helper()
	if s.Explored == nil {
		t.Fatal("snapshot has no explored mask")
	}
	m, err := explored.Decode(*s.Explored)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// cellIndex is the bitset index of the cell under world (x, z).
func cellIndex(x, z float64) int {
	px, py := explored.CellOf(x, z)
	return py*explored.Size + px
}

func TestKeepBytesOnlyForTables(t *testing.T) {
	if !KeepBytes(save.MapTablePrefab) || KeepBytes(h("bed")) || KeepBytes(h("sign")) {
		t.Fatal("KeepBytes must select exactly piece_cartographytable")
	}
}

func TestExploredFromTablesPlusRevealAroundPieces(t *testing.T) {
	e := New()
	t1 := z("piece_cartographytable", [3]float32{0, 30, 0})
	t1.Longs = map[int32]int64{h("creator"): 7}
	t1.ByteArrays = map[int32][]byte{save.MapDataKey: savetest.MapData(3, cellIndex(1200, 2400))}
	t2 := z("piece_cartographytable", [3]float32{0, 30, 0})
	t2.ByteArrays = map[int32][]byte{save.MapDataKey: savetest.MapData(2, cellIndex(-1200, 2400))}
	broken := z("piece_cartographytable", [3]float32{5000, 30, 0})
	broken.ByteArrays = map[int32][]byte{save.MapDataKey: []byte("not gzip")}
	unrecorded := z("piece_cartographytable", [3]float32{-5000, 30, 0})
	wall := z("wood_wall", [3]float32{-2000, 40, 500})
	wall.Longs = map[int32]int64{h("creator"): 7}
	for _, zz := range []*save.ZDO{t1, t2, broken, unrecorded, wall} {
		e.Add(zz)
	}
	s := e.Finish(&save.World{Zones: [][2]int16{{40, 40}}}, "x", time.Now())
	if s.Explored.Source != explored.SourceTables {
		t.Fatalf("source = %q, want tables", s.Explored.Source)
	}
	m := decoded(t, s)
	for _, p := range [][2]float64{{1200, 2400}, {-1200, 2400}, {0, 0}, {100, 0}, {-2000, 500}, {-2000, 600}} {
		if !m.At(p[0], p[1]) {
			t.Errorf("(%v, %v) not explored", p[0], p[1])
		}
	}
	// Not the zone (40, 40), not past the 100 m reveal, nothing from the
	// broken or unrecorded tables (only built pieces reveal around them,
	// and those two have no creator).
	for _, p := range [][2]float64{{2560, 2560}, {-2000, 620}, {5000, 0}, {-5000, 0}} {
		if m.At(p[0], p[1]) {
			t.Errorf("(%v, %v) explored", p[0], p[1])
		}
	}
}

func TestExploredFallsBackToShrunkZones(t *testing.T) {
	e := New()
	hut := z("wood_wall", [3]float32{3000, 30, 3000})
	hut.Longs = map[int32]int64{h("creator"): 9}
	e.Add(hut)
	var zones [][2]int16
	for x := int16(-6); x <= 6; x++ {
		for zz := int16(-6); zz <= 6; zz++ {
			zones = append(zones, [2]int16{x, zz})
		}
	}
	// The zones span [−416, 416) m; eroded by 20 cells (240 m) the centre
	// stays, the rim goes.
	s := e.Finish(&save.World{Zones: zones}, "x", time.Now())
	if s.Explored.Source != explored.SourceZones {
		t.Fatalf("source = %q, want zones", s.Explored.Source)
	}
	m := decoded(t, s)
	if !m.At(0, 0) || !m.At(150, -150) || m.At(300, 0) || m.At(0, -400) {
		t.Fatalf("zone fallback: (0,0)=%v (150,-150)=%v (300,0)=%v (0,-400)=%v", m.At(0, 0), m.At(150, -150), m.At(300, 0), m.At(0, -400))
	}
	if !m.At(3000, 3000) || !m.At(3090, 3000) || m.At(3200, 3000) {
		t.Fatal("no 100 m reveal around the built piece")
	}
}
