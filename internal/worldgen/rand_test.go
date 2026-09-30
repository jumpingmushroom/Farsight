package worldgen

import (
	"math"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
)

func TestURandomSeedAndOutputs(t *testing.T) {
	r := NewURandom(1234)
	if r.s != [4]uint32{1234, 3159640283, 3392860520, 3460949513} {
		t.Fatalf("state = %v", r.s)
	}
	want := []uint32{3463400838, 3496203776, 3452947669, 1278673611, 4169168310}
	for i, w := range want {
		if got := r.Next(); got != w {
			t.Fatalf("Next #%d = %d, want %d", i, got, w)
		}
	}
}

func TestURandomRangeInt(t *testing.T) {
	r := NewURandom(1234)
	for i, w := range []int32{1315917191, 1348720129, 1305464022, 1278673611, 2021684663} {
		if got := r.RangeInt(0, math.MaxInt32); got != w {
			t.Fatalf("RangeInt #%d = %d, want %d", i, got, w)
		}
	}
}

func TestWorldOffsetsFromSeed(t *testing.T) {
	cases := []struct {
		seed          string
		o             [4]int32
		river, stream int32
		o4            int32
	}{
		{"Dedbtjdcv", [4]int32{-8087, 9698, -4921, -8635}, 1741748534, -2141061776, -116},
		{"FjordSeed", [4]int32{3119, 4451, 2181, -5622}, 1699072326, 1064140420, -5640},
		{"Qm4RtX8vLc", [4]int32{-5294, -2350, 1124, -454}, 276112436, 1036682745, 1866},
	}
	for _, c := range cases {
		r := NewURandom(names.StableHash(c.seed))
		var o [4]int32
		for i := range o {
			o[i] = r.RangeInt(-10000, 10000)
		}
		river := r.RangeInt(math.MinInt32, math.MaxInt32)
		stream := r.RangeInt(math.MinInt32, math.MaxInt32)
		o4 := r.RangeInt(-10000, 10000)
		if o != c.o || river != c.river || stream != c.stream || o4 != c.o4 {
			t.Errorf("%s: got %v %d %d %d", c.seed, o, river, stream, o4)
		}
	}
}

func TestRangeFloatIsUnityFormula(t *testing.T) {
	a, b := NewURandom(99), NewURandom(99)
	v := a.Value()
	if got, want := b.RangeFloat(60, 100), v*(60-100)+100; got != want {
		t.Fatalf("RangeFloat = %v, want %v", got, want)
	}
}

func TestPerlinMirroredAndInRange(t *testing.T) {
	p := Perlin(0.4, 0.5)
	if Perlin(-0.4, 0.5) != p || Perlin(0.4, -0.5) != p || Perlin(-0.4, -0.5) != p {
		t.Fatal("Perlin must mirror across both axes (abs inputs)")
	}
	if got := Perlin(0, 0); math.Abs(float64(got)-0.69/1.483) > 1e-6 {
		t.Fatalf("Perlin(0,0) = %v, want ~%v (raw noise is 0 at lattice points)", got, 0.69/1.483)
	}
	for x := float32(-50); x < 50; x += 0.37 {
		for y := float32(-50); y < 50; y += 0.53 {
			if v := Perlin(x, y); v < -0.21 || v > 1.14 {
				t.Fatalf("Perlin(%v,%v) = %v outside the theoretical range [-0.21, 1.14]", x, y, v)
			}
		}
	}
}
