package server

import (
	"bytes"
	"image"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/fog"
)

// BenchmarkComposeEdgeTile is one Edge tile end to end: decode the z5
// terrain PNG, blend, encode. The spec's target is about 10 ms. The
// terrain is hillshade-like (smooth with fine noise), harder to compress
// than real tiles.
func BenchmarkComposeEdgeTile(b *testing.B) {
	dir := b.TempDir()
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	r := rand.New(rand.NewPCG(1, 2))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			o := y*img.Stride + x*4
			n := uint8(r.IntN(24))
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = uint8(x/2)+n, uint8(y/2)+n, 90+n, 255
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	p := filepath.Join(dir, "5", "16", "15.png")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, buf.Bytes(), 0o644)

	m := explored.New()
	for py := 0; py < explored.Size; py++ {
		for px := 0; px < 1050; px++ { // the edge at x ≈ 306 m crosses tile 16,15
			m.Set(px, py)
		}
	}
	f := fog.NewField(m)
	if c := fog.NewClassMap(f).At(5, 16, 15); c != fog.Edge {
		b.Fatalf("tile class = %v, want edge", c)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := composeTile(dir, f, fog.Edge, 5, 16, 15); err != nil {
			b.Fatal(err)
		}
	}
}
