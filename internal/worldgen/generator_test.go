package worldgen

import (
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
)

func TestNewBaseOffsets(t *testing.T) {
	g := NewBase(names.StableHash("FjordSeed"), 2)
	if g.offset0 != 3119 || g.offset1 != 4451 || g.offset2 != 2181 || g.offset3 != -5622 || g.offset4 != -5640 ||
		g.riverSeed != 1699072326 || g.streamSeed != 1064140420 {
		t.Fatalf("offsets = %+v", g)
	}
}

// Analytic landmarks only; real biome correctness is the golden oracle.
func TestBiomeLandmarks(t *testing.T) {
	g := NewBase(names.StableHash("FjordSeed"), 2)
	// |(0, -10000-4000)| = 14000 > 12000 + 100*angle: Ashlands is checked first.
	if b := g.Biome(0, -10000); b != AshLands {
		t.Errorf("far south = %s, want AshLands", b)
	}
	// Past 10490 m the base height is forced towards -2, and (10499,0) is
	// neither Ashlands nor Deep North (|(10499,±4000)| ≈ 11235 < 11900).
	if b := g.Biome(10499, 0); b != Ocean {
		t.Errorf("world edge = %s, want Ocean", b)
	}
}

func TestNewCheckedRejectsOutOfRangeGenVersion(t *testing.T) {
	seed := names.StableHash("FjordSeed")
	for _, v := range []int32{0, 1, MaxGenVersion} {
		if g, err := NewChecked(seed, v); err != nil || g == nil {
			t.Errorf("genVersion %d: err=%v g=%v, want a Generator and no error", v, err, g)
		}
	}
	for _, v := range []int32{-1, MaxGenVersion + 1, 99} {
		if g, err := NewChecked(seed, v); err == nil || g != nil {
			t.Errorf("genVersion %d: err=%v g=%v, want an error and a nil Generator", v, err, g)
		}
	}
}
