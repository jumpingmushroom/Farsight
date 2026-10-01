package explored

import (
	"math"
	"testing"
)

func TestCellOfUsesBankersRounding(t *testing.T) {
	cases := []struct {
		x, z   float64
		px, py int
	}{
		{0, 0, 1024, 1024},
		{6, -6, 1024, 1024},   // ±0.5 rounds to the even 0
		{18, -18, 1026, 1022}, // ±1.5 rounds to ±2
		{30, -30, 1026, 1022}, // ±2.5 rounds to ±2
		{11.9, -12.1, 1025, 1023},
		{-6, 6.01, 1024, 1025},
	}
	for _, c := range cases {
		if px, py := CellOf(c.x, c.z); px != c.px || py != c.py {
			t.Errorf("CellOf(%v, %v) = %d,%d; want %d,%d", c.x, c.z, px, py, c.px, c.py)
		}
	}
}

func TestBitLayoutIsRowMajorLSBFirst(t *testing.T) {
	m := New()
	m.Set(3, 0)
	m.Set(0, 1)
	m.Set(Size-1, Size-1)
	b := m.Bits()
	if len(b) != Size*Size/8 || b[0] != 1<<3 || b[Size/8] != 1 || b[len(b)-1] != 0x80 || m.Count() != 3 {
		t.Fatalf("len=%d b[0]=%#x b[256]=%#x last=%#x count=%d", len(b), b[0], b[Size/8], b[len(b)-1], m.Count())
	}
	if !m.Get(3, 0) || m.Get(4, 0) || m.Get(-1, 0) || m.Get(0, Size) {
		t.Fatal("Get disagrees with Set, or an off-grid cell reads as explored")
	}
	m.Set(-1, 5) // off-grid: ignored, no panic
	m.Set(5, Size)
	if m.Count() != 3 {
		t.Fatalf("off-grid Set changed the mask: count %d", m.Count())
	}
}

func TestAtAddFlagsAndUnion(t *testing.T) {
	flags := make([]byte, Size*Size)
	flags[1024*Size+1025] = 1 // the cell under world (12, 0)
	a := New()
	if err := a.AddFlags(flags); err != nil {
		t.Fatal(err)
	}
	if !a.At(12, 0) || a.At(0, 0) || a.Count() != 1 {
		t.Fatalf("At(12,0)=%v At(0,0)=%v count=%d", a.At(12, 0), a.At(0, 0), a.Count())
	}
	if err := a.AddFlags(flags[:10]); err == nil {
		t.Fatal("AddFlags accepted the wrong length")
	}
	b := New()
	b.Set(7, 7)
	a.Union(b)
	if a.Count() != 2 || !a.Get(7, 7) || b.Count() != 1 {
		t.Fatalf("union: a=%d b=%d", a.Count(), b.Count())
	}
}

func TestRevealIsTheGamesNineCellDisc(t *testing.T) {
	m := New()
	m.Reveal(1024, 1024)
	// Lattice points with dx²+dy² ≤ 81 (Gauss's circle problem, r = 9).
	if m.Count() != 253 {
		t.Fatalf("reveal covers %d cells, want 253", m.Count())
	}
	if !m.Get(1024+9, 1024) || m.Get(1024+10, 1024) || !m.Get(1024+5, 1024+7) || m.Get(1024+7, 1024+7) {
		t.Fatal("reveal edge cells wrong")
	}
	edge := New()
	edge.Reveal(0, Size-1) // clipped at the grid corner, no panic
	if edge.Count() == 0 || edge.Count() >= 253 {
		t.Fatalf("corner reveal = %d cells", edge.Count())
	}
}

func TestFromZonesFillsEachZonesCells(t *testing.T) {
	m := FromZones([][2]int16{{0, 0}, {1, 0}, {-1, -1}, {200, 0}})
	// Zone 0 spans centres −24…24 (5 cells), zone 1 36…84 (5), zone −1
	// −96…−36 (6); zone 200 is off the grid.
	if m.Count() != 5*5+5*5+6*6 {
		t.Fatalf("count = %d, want %d", m.Count(), 5*5+5*5+6*6)
	}
	for _, p := range [][2]float64{{0, 0}, {29, -29}, {84, 24}, {-90, -90}, {-36, -36}} {
		if !m.At(p[0], p[1]) {
			t.Errorf("(%v, %v) not explored", p[0], p[1])
		}
	}
	for _, p := range [][2]float64{{-40, -30}, {90, 0}, {0, 40}} {
		if m.At(p[0], p[1]) {
			t.Errorf("(%v, %v) explored", p[0], p[1])
		}
	}
}

func TestErodeShrinksBySquare(t *testing.T) {
	m := New()
	for py := 100; py < 130; py++ {
		for px := 200; px < 230; px++ {
			m.Set(px, py)
		}
	}
	e := m.Erode(5)
	if e.Count() != 20*20 || !e.Get(205, 105) || e.Get(204, 105) || e.Get(225, 125) {
		t.Fatalf("eroded count %d", e.Count())
	}
	if m.Count() != 900 {
		t.Fatal("Erode modified its receiver")
	}
	if c := m.Erode(0); c.Count() != 900 {
		t.Fatalf("Erode(0) count %d", c.Count())
	}
	full := New()
	for i := range full.bits {
		full.bits[i] = 0xFF
	}
	// Off-grid counts as unexplored: the border k cells go.
	if got, want := full.Erode(3).Count(), (Size-6)*(Size-6); got != want {
		t.Fatalf("full eroded = %d, want %d", got, want)
	}
}

func TestPercentOfTheWorldDisc(t *testing.T) {
	if p := New().Percent(); p != 0 {
		t.Fatalf("empty = %v", p)
	}
	full := New()
	for i := range full.bits {
		full.bits[i] = 0xFF
	}
	if p := full.Percent(); p != 100 {
		t.Fatalf("full = %v", p)
	}
	north := New()
	var in, total int
	for py := 0; py < Size; py++ {
		for px := 0; px < Size; px++ {
			if math.Hypot(float64((px-1024)*12), float64((py-1024)*12)) > 10500 {
				continue
			}
			total++
			if py >= 1024 {
				north.Set(px, py)
				in++
			}
		}
	}
	if want := math.Round(float64(in)/float64(total)*1000) / 10; north.Percent() != want {
		t.Fatalf("north half = %v, want %v", north.Percent(), want)
	}
}
