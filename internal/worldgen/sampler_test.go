package worldgen

import (
	"math"
	"math/rand"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
)

// samplerTestPoints is terrainDigestPoints() plus extra zone-edge and
// zone-corner points (multiples of 64 m offset by 32, i.e. zoneAxis's
// boundaries, +/- tiny offsets, including negative coordinates and float32
// neighbours of each edge).
func samplerTestPoints() [][2]float32 {
	pts := terrainDigestPoints()
	edges := []float32{-10016, -4128, -1056, -160, -96, -32, 32, 96, 160, 1056, 4128, 10016}
	var offs []float32
	for _, e := range edges {
		offs = append(offs, e, e-0.001, e+0.001, e-0.25, e+0.25,
			math.Nextafter32(e, float32(math.Inf(-1))), math.Nextafter32(e, float32(math.Inf(1))))
	}
	for _, x := range offs {
		for _, z := range offs {
			pts = append(pts, [2]float32{x, z})
		}
	}
	return pts
}

func TestSamplerMatchesTerrainHeight(t *testing.T) {
	g := New(names.StableHash("FjordSeed"), 2)
	pts := samplerTestPoints()

	shuffled := append([][2]float32(nil), pts...)
	rand.New(rand.NewSource(20260929)).Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	for _, order := range []struct {
		name string
		pts  [][2]float32
	}{{"row", pts}, {"shuffled", shuffled}} {
		t.Run(order.name, func(t *testing.T) {
			s := g.TerrainView().NewSampler()
			bad := 0
			for _, p := range order.pts {
				want := g.TerrainHeight(p[0], p[1])
				got := s.Height(p[0], p[1])
				if math.Float32bits(got) != math.Float32bits(want) {
					bad++
					if bad <= 10 {
						t.Errorf("Height(%v, %v) = %v (%08x), want %v (%08x)", p[0], p[1], got, math.Float32bits(got), want, math.Float32bits(want))
					}
				}
				if gb, wb := s.Biome(p[0], p[1]), g.Biome(p[0], p[1]); gb != wb {
					t.Errorf("Biome(%v, %v) = %v, want %v", p[0], p[1], gb, wb)
				}
			}
			if bad > 0 {
				t.Fatalf("%d of %d points differ", bad, len(order.pts))
			}
		})
	}
}

// benchRow returns 256 consecutive tile pixels at 2.56 m spacing, the way
// tiles.renderZ5Tile walks a row.
func benchRow() []float32 {
	const metresPerPx = float32(2 * 10500.0 / 8192)
	xs := make([]float32, 256)
	for i := range xs {
		xs[i] = -2000 + (float32(i)+0.5)*metresPerPx
	}
	return xs
}

func BenchmarkSamplerRow(b *testing.B) {
	g := New(names.StableHash("FjordSeed"), 2)
	s := g.TerrainView().NewSampler()
	xs := benchRow()
	var sink float32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, x := range xs {
			sink += s.Height(x, 1234.5)
		}
	}
	_ = sink
}

func BenchmarkTerrainHeightRow(b *testing.B) {
	g := New(names.StableHash("FjordSeed"), 2)
	xs := benchRow()
	var sink float32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, x := range xs {
			sink += g.TerrainHeight(x, 1234.5)
		}
	}
	_ = sink
}
