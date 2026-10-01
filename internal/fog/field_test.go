package fog

import (
	"math/rand/v2"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/explored"
)

func bruteSqEDT(feature []bool, w, h int) []float64 {
	out := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			best := -1.0
			for fy := 0; fy < h; fy++ {
				for fx := 0; fx < w; fx++ {
					if !feature[fy*w+fx] {
						continue
					}
					d := float64((fx-x)*(fx-x) + (fy-y)*(fy-y))
					if best < 0 || d < best {
						best = d
					}
				}
			}
			out[y*w+x] = best
		}
	}
	return out
}

func TestSqEDTMatchesBruteForce(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	for _, c := range []struct {
		w, h    int
		density float64
	}{{1, 1, 1}, {7, 5, 0.2}, {31, 17, 0.05}, {17, 31, 0.5}, {64, 64, 0.01}, {40, 3, 0.1}, {9, 9, 0}} {
		feature := make([]bool, c.w*c.h)
		for i := range feature {
			feature[i] = r.Float64() < c.density
		}
		got := sqEDT(feature, c.w, c.h)
		want := bruteSqEDT(feature, c.w, c.h)
		for i := range want {
			if want[i] < 0 {
				if got[i] < 1e19 {
					t.Fatalf("%dx%d: cell %d = %v with no features, want >= 1e19", c.w, c.h, i, got[i])
				}
				continue
			}
			if float64(got[i]) != want[i] {
				t.Fatalf("%dx%d: cell %d = %v, want %v", c.w, c.h, i, got[i], want[i])
			}
		}
	}
}

// westHalf is explored for every cell with px <= 1023: the edge runs at
// world x = −6 m, halfway between cell 1023 (x = −12) and cell 1024 (x = 0).
func westHalf() *explored.Mask {
	m := explored.New()
	for py := 0; py < explored.Size; py++ {
		for px := 0; px < 1024; px++ {
			m.Set(px, py)
		}
	}
	return m
}

func TestFieldIsSignedHalfCellDistanceClamped(t *testing.T) {
	f := NewField(westHalf())
	for _, c := range []struct {
		px   int
		want float64
	}{{1023, 6}, {1022, 18}, {1024, -6}, {1025, -18}, {900, 63.5}, {1200, -63.5}} {
		if got := f.at(c.px, 700); got != c.want {
			t.Errorf("at(%d) = %v, want %v", c.px, got, c.want)
		}
	}
	if d := f.Sample(1023.5, 700); d != 0 {
		t.Errorf("Sample at the edge = %v, want 0", d)
	}
	if d := f.Sample(1023.25, 700.5); d != 3 {
		t.Errorf("Sample a quarter cell inside = %v, want 3", d)
	}
	if d := f.at(-1, 0); d != -63.5 {
		t.Errorf("off-grid = %v, want -63.5", d)
	}
}
