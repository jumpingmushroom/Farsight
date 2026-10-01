package fog

import "testing"

// Reference values computed with the browser code (web/src/lib/fog.ts
// before Plan 6) in Node.
func TestMulberry32MatchesTheBrowser(t *testing.T) {
	r := mulberry32(noiseSeed)
	for i, want := range []float64{0.7248572071548551, 0.37774392520077527, 0.9134336351417005} {
		if got := r.next(); got != want {
			t.Fatalf("draw %d = %v, want %v", i, got, want)
		}
	}
}

func TestNoiseMatchesTheBrowser(t *testing.T) {
	// The browser's Uint8ClampedArray after the noise loop, hashed as
	// s = s*31 + byte (uint32) over R, G, B, A=255 per pixel.
	var s uint32
	for _, p := range noise {
		for _, b := range []uint8{p[0], p[1], p[2], 255} {
			s = s*31 + uint32(b)
		}
	}
	if s != 2230037077 {
		t.Fatalf("noise hash = %d, want 2230037077", s)
	}
	for _, c := range []struct {
		x, y int
		want [3]uint8
	}{{0, 0, [3]uint8{211, 194, 160}}, {1, 0, [3]uint8{205, 188, 154}}, {2, 0, [3]uint8{214, 197, 163}}, {5, 3, [3]uint8{209, 192, 158}}} {
		if got := noise[c.y*PatternSize+c.x]; got != c.want {
			t.Errorf("noise(%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestTextureHatchAndRepeat(t *testing.T) {
	px := func(x, y int) [3]uint8 { r, g, b := TexturePixel(x, y); return [3]uint8{r, g, b} }
	for _, c := range []struct {
		x, y int
		want [3]uint8
	}{
		{0, 0, [3]uint8{200, 182, 149}},     // hatched: x − y = 0
		{1, 0, [3]uint8{205, 188, 154}},     // plain noise
		{5, 3, [3]uint8{209, 192, 158}},     // plain noise
		{125, 125, [3]uint8{203, 185, 151}}, // hatched
	} {
		if got := px(c.x, c.y); got != c.want {
			t.Errorf("TexturePixel(%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
	if px(126, 0) != px(0, 0) || px(-1, -1) != px(125, 125) || px(1000, 2000) != px(1000%126, 2000%126) {
		t.Error("pattern does not repeat every 126 px")
	}
	// Hatching runs on x − y ≡ 0 (mod 9) across pattern borders too.
	for _, p := range [][2]int{{9, 0}, {130, 4}, {-9, 0}, {0, 126}} {
		n := noise[((p[1]%126+126)%126)*126+(p[0]%126+126)%126]
		if px(p[0], p[1]) == n {
			t.Errorf("(%d,%d) is not hatched", p[0], p[1])
		}
	}
}
