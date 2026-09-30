package worldgen

import "testing"

func TestFastNoiseDeterministicAndBounded(t *testing.T) {
	a, b := newFastNoise(0), newFastNoise(0)
	for i := 0; i < 1000; i++ {
		x, y := float64(i)*1.37-500, float64(i)*0.91+200
		c1, c2 := a.getCellular(x, y), b.getCellular(x, y)
		s := a.getSimplexFractal(x, y)
		if c1 != c2 {
			t.Fatal("cellular noise must be deterministic")
		}
		if c1 < -1.5 || c1 > 1.5 || s < -1.5 || s > 1.5 {
			t.Fatalf("noise out of range at %v,%v: cellular %v simplex %v", x, y, c1, s)
		}
	}
	if a.getCellular(10.5, 3.25) == newFastNoise(1).getCellular(10.5, 3.25) {
		t.Error("different seeds should give different cellular noise")
	}
}
