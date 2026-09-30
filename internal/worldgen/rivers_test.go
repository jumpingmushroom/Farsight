package worldgen

import (
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
)

func TestRiversPlaced(t *testing.T) {
	g := New(names.StableHash("FjordSeed"), 2)
	if len(g.Lakes()) == 0 || len(g.Rivers()) == 0 || len(g.Streams()) == 0 {
		t.Fatalf("lakes=%d rivers=%d streams=%d, want all > 0", len(g.Lakes()), len(g.Rivers()), len(g.Streams()))
	}
	t.Logf("FjordSeed: %d lakes, %d rivers, %d streams", len(g.Lakes()), len(g.Rivers()), len(g.Streams()))
	// Regression canary: the counts the reviewed port produces for FjordSeed.
	if len(g.Lakes()) != 112 || len(g.Rivers()) != 147 || len(g.Streams()) != 2049 {
		t.Fatalf("lakes=%d rivers=%d streams=%d, want 112/147/2049", len(g.Lakes()), len(g.Rivers()), len(g.Streams()))
	}
}

func BenchmarkNewFjordSeed(b *testing.B) {
	seed := names.StableHash("FjordSeed")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		New(seed, 2)
	}
}
