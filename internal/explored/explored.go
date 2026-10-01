// Package explored is the atlas's explored mask: which cells of the game's
// 2048×2048 minimap grid (12 m per cell) have been explored. The agent
// builds it from the cartography tables' shared map (or, without one, from
// the generated zones) plus the game's 100 m reveal around every
// player-built piece; the central app filters pins and draws fog from it.
//
// Cell (px, py) is the game's Minimap.WorldToPixel: px = round(x/12) + 1024,
// py = round(z/12) + 1024 with Unity's banker's rounding, so row 0 is the
// south edge and cell (px, py) is centred on world ((px−1024)·12,
// (py−1024)·12). Cells are stored as a row-major bitset (index py·2048+px),
// least significant bit first in each byte.
package explored

import (
	"fmt"
	"math"
	"math/bits"
	"sync"
)

const (
	// Size is the grid's side in cells.
	Size = 2048
	// CellMetres is one cell's side in metres.
	CellMetres = 12
	// RevealCells is the game's reveal radius around a player (100 m) in
	// cells: Minimap.Explore uses ceil(100/12) and keeps the cells within
	// that many cells of the centre cell.
	RevealCells = 9
	// ZoneShrinkCells is how far the zone fallback erodes the generated
	// zones: the shrink with the highest intersection-over-union against
	// the real table data of the MuleVikings save (see
	// TestGoldenZoneShrinkCalibration).
	ZoneShrinkCells = 20
	// WorldRadius is the playable world's radius in metres.
	WorldRadius = 10500.0

	// SourceTables and SourceZones name where a mask came from.
	SourceTables = "tables"
	SourceZones  = "zones"

	half      = Size / 2
	maskBytes = Size * Size / 8
)

// Mask is a set of explored cells.
type Mask struct{ bits []byte }

// New returns an empty mask.
func New() *Mask { return &Mask{bits: make([]byte, maskBytes)} }

// CellOf is the cell under world point (x, z). It may lie off the grid.
func CellOf(x, z float64) (px, py int) {
	return int(math.RoundToEven(x/CellMetres)) + half, int(math.RoundToEven(z/CellMetres)) + half
}

func inGrid(px, py int) bool { return px >= 0 && px < Size && py >= 0 && py < Size }

// Get reports whether cell (px, py) is explored; off-grid cells are not.
func (m *Mask) Get(px, py int) bool {
	if !inGrid(px, py) {
		return false
	}
	i := py*Size + px
	return m.bits[i>>3]&(1<<(i&7)) != 0
}

// Set marks cell (px, py) explored; off-grid cells are ignored.
func (m *Mask) Set(px, py int) {
	if !inGrid(px, py) {
		return
	}
	i := py*Size + px
	m.bits[i>>3] |= 1 << (i & 7)
}

// At reports whether the cell under world point (x, z) is explored.
func (m *Mask) At(x, z float64) bool { return m.Get(CellOf(x, z)) }

// Count is the number of explored cells.
func (m *Mask) Count() int {
	n := 0
	for _, b := range m.bits {
		n += bits.OnesCount8(b)
	}
	return n
}

// Bits is the raw bitset (row-major, LSB first). Callers must not modify it.
func (m *Mask) Bits() []byte { return m.bits }

// Clone returns an independent copy of m.
func (m *Mask) Clone() *Mask {
	c := New()
	copy(c.bits, m.bits)
	return c
}

// Union adds every cell explored in o to m.
func (m *Mask) Union(o *Mask) {
	for i, b := range o.bits {
		m.bits[i] |= b
	}
}

// AddFlags adds a cartography table's explored flags (save.DecodeMapData:
// Size² bytes, non-zero = explored, same cell order) to m.
func (m *Mask) AddFlags(flags []byte) error {
	if len(flags) != Size*Size {
		return fmt.Errorf("explored: %d flags, want %d", len(flags), Size*Size)
	}
	for i, f := range flags {
		if f != 0 {
			m.bits[i>>3] |= 1 << (i & 7)
		}
	}
	return nil
}

// Reveal marks the game's 100 m reveal around cell (px, py): every cell
// within RevealCells cells of it (Euclidean, in whole cells), as
// Minimap.Explore does around a player.
func (m *Mask) Reveal(px, py int) {
	for dy := -RevealCells; dy <= RevealCells; dy++ {
		for dx := -RevealCells; dx <= RevealCells; dx++ {
			if dx*dx+dy*dy <= RevealCells*RevealCells {
				m.Set(px+dx, py+dy)
			}
		}
	}
}

// zoneCells is the inclusive range of grid indices whose cell centre lies
// in 64 m zone z, i.e. in [64z − 32, 64z + 32) metres (the zone of a point
// is floor((v + 32) / 64)).
func zoneCells(z int16) (lo, hi int) {
	lo = int(math.Ceil(float64(64*int(z)-32)/CellMetres)) + half
	hi = int(math.Ceil(float64(64*int(z)+32)/CellMetres)) - 1 + half
	return lo, hi
}

// FromZones rasterises 64 m zones onto the grid: every cell whose centre
// lies in one of the zones. Zones off the grid are ignored.
func FromZones(zones [][2]int16) *Mask {
	m := New()
	for _, zn := range zones {
		x0, x1 := zoneCells(zn[0])
		y0, y1 := zoneCells(zn[1])
		for py := max(y0, 0); py <= min(y1, Size-1); py++ {
			for px := max(x0, 0); px <= min(x1, Size-1); px++ {
				m.Set(px, py)
			}
		}
	}
	return m
}

// Erode returns m shrunk by k cells: a cell stays explored only if every
// cell within k cells of it along both axes (a (2k+1)² square) is explored.
// Off-grid cells count as unexplored. k <= 0 returns a copy.
func (m *Mask) Erode(k int) *Mask {
	if k <= 0 {
		return m.Clone()
	}
	rows := make([]bool, Size*Size) // eroded along x only
	run := make([]int, Size+1)      // prefix counts of unexplored cells
	for py := 0; py < Size; py++ {
		for px := 0; px < Size; px++ {
			run[px+1] = run[px]
			if !m.Get(px, py) {
				run[px+1]++
			}
		}
		for px := k; px < Size-k; px++ {
			rows[py*Size+px] = run[px+k+1]-run[px-k] == 0
		}
	}
	out := New()
	for px := 0; px < Size; px++ {
		for py := 0; py < Size; py++ {
			run[py+1] = run[py]
			if !rows[py*Size+px] {
				run[py+1]++
			}
		}
		for py := k; py < Size-k; py++ {
			if run[py+k+1]-run[py-k] == 0 {
				out.Set(px, py)
			}
		}
	}
	return out
}

// discRows is, per grid row, the inclusive px range of cells whose centre
// lies within WorldRadius (lo > hi for rows outside it), and the total.
var discRows = sync.OnceValues(func() ([Size][2]int, int) {
	var rows [Size][2]int
	total := 0
	for py := 0; py < Size; py++ {
		rows[py] = [2]int{0, -1}
		z := float64((py - half) * CellMetres)
		for px := 0; px < Size; px++ {
			x := float64((px - half) * CellMetres)
			if x*x+z*z <= WorldRadius*WorldRadius {
				if rows[py][1] < rows[py][0] {
					rows[py][0] = px
				}
				rows[py][1] = px
				total++
			}
		}
	}
	return rows, total
})

// Percent is the share of the cells inside the world disc (centre within
// WorldRadius) that are explored, × 100, rounded to one decimal place.
func (m *Mask) Percent() float64 {
	rows, total := discRows()
	n := 0
	for py, r := range rows {
		for px := r[0]; px <= r[1]; px++ {
			if m.Get(px, py) {
				n++
			}
		}
	}
	return math.Round(float64(n)/float64(total)*1000) / 10
}
