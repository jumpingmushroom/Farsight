package fog

import "math"

// The parchment texture, ported from the browser's former fog canvas
// (web/src/lib/fog.ts before Plan 6): #cfbe9c with ±9 grey noise per pixel
// from mulberry32(0x5eedf06) over a 126 px pattern (row-major, one draw
// per pixel, rounded half-to-even and clamped as a canvas
// Uint8ClampedArray does) — this part is bit-exact with the browser, as
// TestNoiseMatchesTheBrowser checks against values computed from the
// browser's own code. The 45° hatch then follows the spec's per-pixel rule
// (rgba(120,98,66,.12) where x − y ≡ 0 mod 9) rather than the browser's
// antialiased canvas stroke, which paints a similar but not bit-identical
// band across three pixels. 126 is a multiple of 9, so the pattern repeats
// seamlessly; it is anchored to global pixels at each zoom.
const (
	PatternSize = 126
	hatchEvery  = 9
	noiseAmp    = 18 // ±9
	noiseSeed   = 0x5eedf06
	hatchAlpha  = 0.12
)

var (
	fogBase    = [3]float64{0xcf, 0xbe, 0x9c}
	hatchColor = [3]float64{120, 98, 66}
)

// mulberry32 is the browser's small PRNG, returning floats in [0, 1).
type mulberry32 uint32

func (m *mulberry32) next() float64 {
	*m += 0x6d2b79f5
	t := uint32(*m)
	t = (t ^ t>>15) * (t | 1)
	t ^= t + (t^t>>7)*(t|61)
	return float64(t^t>>14) / 4294967296
}

func clamp255(v float64) float64 { return max(0, min(255, v)) }

func mod(a, n int) int { return ((a % n) + n) % n }

// noise is the pattern before hatching.
var noise = func() (p [PatternSize * PatternSize][3]uint8) {
	r := mulberry32(noiseSeed)
	for i := range p {
		n := (r.next() - 0.5) * noiseAmp
		for c := range 3 {
			p[i][c] = uint8(clamp255(math.RoundToEven(fogBase[c] + n)))
		}
	}
	return p
}()

// pattern is the finished texture tile.
var pattern = func() (p [PatternSize * PatternSize][3]uint8) {
	for i := range p {
		p[i] = noise[i]
		if mod(i%PatternSize-i/PatternSize, hatchEvery) != 0 {
			continue
		}
		for c := range 3 {
			p[i][c] = uint8(clamp255(math.Round(float64(noise[i][c])*(1-hatchAlpha) + hatchColor[c]*hatchAlpha)))
		}
	}
	return p
}()

// TexturePixel is the fog colour at global pixel (gx, gy) of any zoom.
func TexturePixel(gx, gy int) (r, g, b uint8) {
	p := pattern[mod(gy, PatternSize)*PatternSize+mod(gx, PatternSize)]
	return p[0], p[1], p[2]
}
