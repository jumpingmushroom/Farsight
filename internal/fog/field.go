package fog

import (
	"math"

	"github.com/jumpingmushroom/farsight/internal/explored"
)

const (
	quantum  = 0.5 // metres per field unit
	fieldMax = 127 // ±63.5 m: past the widest band (57.6 m), so clamping is invisible
	inf      = 1e20
)

// Field is the signed distance, per explored-mask cell, from the cell's
// centre to the explored edge: positive inside the explored area, negative
// outside. The edge runs halfway between an explored and an unexplored
// cell. Values are int8 in 0.5 m units, clamped to ±63.5 m (4 MiB).
type Field struct{ d []int8 }

// NewField builds m's field with two exact Euclidean distance transforms
// (inside to the nearest unexplored cell, outside to the nearest explored
// one).
func NewField(m *explored.Mask) *Field {
	const n = explored.Size
	in := make([]bool, n*n)
	out := make([]bool, n*n)
	for py := 0; py < n; py++ {
		for px := 0; px < n; px++ {
			i := py*n + px
			in[i] = m.Get(px, py)
			out[i] = !in[i]
		}
	}
	f := &Field{d: make([]int8, n*n)}
	sq := sqEDT(out, n, n)
	for i, e := range in {
		if e {
			f.d[i] = quantise((math.Sqrt(float64(sq[i])) - 0.5) * cellMetres)
		}
	}
	sq = sqEDT(in, n, n)
	for i, e := range in {
		if !e {
			f.d[i] = quantise(-(math.Sqrt(float64(sq[i])) - 0.5) * cellMetres)
		}
	}
	return f
}

func quantise(metres float64) int8 {
	return int8(max(-fieldMax, min(fieldMax, math.Round(metres/quantum))))
}

// at is cell (px, py)'s distance in metres; off the grid is deep fog.
func (f *Field) at(px, py int) float64 {
	if px < 0 || px >= explored.Size || py < 0 || py >= explored.Size {
		return -fieldMax * quantum
	}
	return float64(f.d[py*explored.Size+px]) * quantum
}

// Sample is the distance in metres at grid position (u, v) (in cells, cell
// centres on integers), bilinearly interpolated.
func (f *Field) Sample(u, v float64) float64 {
	fu, fv := math.Floor(u), math.Floor(v)
	tx, ty := u-fu, v-fv
	x, y := int(fu), int(fv)
	a, b := f.at(x, y), f.at(x+1, y)
	c, d := f.at(x, y+1), f.at(x+1, y+1)
	return (a*(1-tx)+b*tx)*(1-ty) + (c*(1-tx)+d*tx)*ty
}

// sqEDT is the exact squared Euclidean distance transform of a w×h grid
// (Felzenszwalb & Huttenlocher): for every cell, the squared distance in
// cells to the nearest feature cell, 0 on features, and >= 1e19 when the
// grid has none.
func sqEDT(feature []bool, w, h int) []float32 {
	g := make([]float32, w*h)
	for i, f := range feature {
		if !f {
			g[i] = inf
		}
	}
	n := max(w, h)
	f := make([]float64, n)
	d := make([]float64, n)
	v := make([]int, n)
	z := make([]float64, n+1)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			f[y] = float64(g[y*w+x])
		}
		edt1(f[:h], d[:h], v, z)
		for y := 0; y < h; y++ {
			g[y*w+x] = float32(d[y])
		}
	}
	for y := 0; y < h; y++ {
		row := g[y*w : (y+1)*w]
		for x := range row {
			f[x] = float64(row[x])
		}
		edt1(f[:w], d[:w], v, z)
		for x := range row {
			row[x] = float32(d[x])
		}
	}
	return g
}

// edt1 is the 1-D squared distance transform of sampled function f into d
// (the lower envelope of parabolas rooted at each sample).
func edt1(f, d []float64, v []int, z []float64) {
	k := 0
	v[0] = 0
	z[0], z[1] = math.Inf(-1), math.Inf(1)
	for q := 1; q < len(f); q++ {
		s := ((f[q] + float64(q*q)) - (f[v[k]] + float64(v[k]*v[k]))) / float64(2*q-2*v[k])
		for s <= z[k] {
			k--
			s = ((f[q] + float64(q*q)) - (f[v[k]] + float64(v[k]*v[k]))) / float64(2*q-2*v[k])
		}
		k++
		v[k] = q
		z[k], z[k+1] = s, math.Inf(1)
	}
	k = 0
	for q := range f {
		for z[k+1] < float64(q) {
			k++
		}
		dq := q - v[k]
		d[q] = float64(dq*dq) + f[v[k]]
	}
}
