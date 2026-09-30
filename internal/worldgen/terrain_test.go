package worldgen

import "testing"

// BenchmarkTerrainHeight sweeps a 2 km line through the world centre,
// crossing several biome borders, so it exercises both the single-biome
// fast path and the four-corner blend of TerrainHeight.
func BenchmarkTerrainHeight(b *testing.B) {
	g := New(12345, 2)
	b.ReportAllocs()
	b.ResetTimer()
	var s float32
	for i := 0; i < b.N; i++ {
		x := float32(i%2000) - 1000
		s += g.TerrainHeight(x, x*0.5)
	}
	_ = s
}
