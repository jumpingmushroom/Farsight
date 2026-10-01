package fog

import "testing"

func BenchmarkNewField(b *testing.B) {
	m := westHalf()
	for b.Loop() {
		NewField(m)
	}
}

func BenchmarkNewClassMap(b *testing.B) {
	f := NewField(westHalf())
	for b.Loop() {
		NewClassMap(f)
	}
}

func BenchmarkBlendEdgeTile(b *testing.B) {
	f := NewField(westHalf())
	terrain := noiseTile(1)
	img := clone(terrain)
	for b.Loop() {
		copy(img.Pix, terrain.Pix)
		Blend(img, f, 5, 15, 15)
	}
}
