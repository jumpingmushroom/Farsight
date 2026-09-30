package worldgen

import "testing"

// BenchmarkHeight sweeps a 2 km line through the world centre (Meadows,
// Black Forest, ...); BenchmarkHeightAshlands samples the far south, the
// FastNoise-heavy path. New (river pre-generation) is excluded from timing.
func BenchmarkHeight(b *testing.B) {
	g := New(12345, 2)
	b.ReportAllocs()
	b.ResetTimer()
	var s float32
	for i := 0; i < b.N; i++ {
		x := float32(i%2000) - 1000
		s += g.Height(x, x*0.5)
	}
	_ = s
}

func BenchmarkHeightAshlands(b *testing.B) {
	g := New(12345, 2)
	b.ReportAllocs()
	b.ResetTimer()
	var s float32
	for i := 0; i < b.N; i++ {
		s += g.Height(float32(i%2000)-1000, -9000)
	}
	_ = s
}

func TestHeightOutsideWorld(t *testing.T) {
	g := New(12345, 2)
	if h := g.Height(10600, 0); h != -400 {
		t.Fatalf("Height beyond the 10500 m edge = %v, want -400", h)
	}
}
