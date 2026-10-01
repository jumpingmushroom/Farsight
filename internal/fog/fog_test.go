package fog

import (
	"regexp"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/explored"
)

func TestKeyChangesOnlyWithExplorationOrStyle(t *testing.T) {
	a := explored.New()
	a.Set(10, 10)
	b := a.Clone()
	k := Key(explored.SourceTables, a)
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(k) {
		t.Fatalf("key %q is not 16 hex digits", k)
	}
	if Key(explored.SourceTables, b) != k {
		t.Fatal("same mask, different key")
	}
	if Key(explored.SourceZones, a) == k {
		t.Fatal("source is not part of the key")
	}
	b.Set(11, 10)
	if Key(explored.SourceTables, b) == k {
		t.Fatal("a newly explored cell kept the key")
	}
}

func TestEdgeWidthAndMetresPerPixel(t *testing.T) {
	if MetresPerPixel(0) != 21000.0/256 || MetresPerPixel(6) != 21000.0/16384 {
		t.Fatalf("mpp z0=%v z6=%v", MetresPerPixel(0), MetresPerPixel(6))
	}
	for z := 0; z <= 5; z++ {
		if EdgeWidth(z) != 57.6 {
			t.Errorf("EdgeWidth(%d) = %v, want 57.6", z, EdgeWidth(z))
		}
	}
	if EdgeWidth(6) != 24*21000.0/16384 {
		t.Errorf("EdgeWidth(6) = %v", EdgeWidth(6))
	}
}

func TestAlphaIsOpaqueAtAndPastTheEdge(t *testing.T) {
	const w = 57.6
	for _, d := range []float64{0, -0.001, -6, -63.5} {
		if Alpha(d, w) != 1 {
			t.Errorf("Alpha(%v) = %v, want exactly 1", d, Alpha(d, w))
		}
	}
	if Alpha(w, w) != 0 || Alpha(63.5, w) != 0 || Alpha(w/2, w) != 0.5 {
		t.Errorf("Alpha(w)=%v Alpha(63.5)=%v Alpha(w/2)=%v", Alpha(w, w), Alpha(63.5, w), Alpha(w/2, w))
	}
	prev := 1.0
	for d := 0.0; d <= w; d += 0.5 {
		if a := Alpha(d, w); a > prev {
			t.Fatalf("Alpha not monotone at %v", d)
		} else {
			prev = a
		}
	}
}
