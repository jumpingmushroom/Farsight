package fog

import (
	"bytes"
	"image"
	"image/color"
	"math/rand/v2"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/explored"
)

// noiseTile is an opaque terrain tile of random colours.
func noiseTile(seed uint64) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, TileSize, TileSize))
	r := rand.New(rand.NewPCG(seed, 99))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.IntN(256))
		if i%4 == 3 {
			img.Pix[i] = 255
		}
	}
	return img
}

func clone(img *image.NRGBA) *image.NRGBA {
	c := *img
	c.Pix = bytes.Clone(img.Pix)
	return &c
}

func TestFogTileIsTheTextureAnchoredToWorldPixels(t *testing.T) {
	a, b := FogTile(3, 2, 5), FogTile(3, 3, 5)
	for _, c := range []struct {
		img  *image.NRGBA
		x, y int
	}{{a, 2, 5}, {b, 3, 5}} {
		for py := 0; py < TileSize; py++ {
			for px := 0; px < TileSize; px++ {
				p := c.img.NRGBAAt(px, py)
				r, g, bl := TexturePixel(c.x*TileSize+px, c.y*TileSize+py)
				if p.R != r || p.G != g || p.B != bl || p.A != 255 {
					t.Fatalf("tile %d,%d pixel %d,%d = %v, want texture", c.x, c.y, px, py, p)
				}
			}
		}
	}
}

func TestBlendDeepFogIsTextureAndClearIsTerrain(t *testing.T) {
	f := NewField(westHalf())
	// z5 tile 15,15 spans x [−656.25, 0]: explored except its east edge
	// (the edge is at x = −6). Tile 16,15 spans x [0, 656.25]: unexplored.
	for _, seed := range []uint64{1, 2} {
		terrain := noiseTile(seed)
		terrain.Pix[3] = 77 // a translucent pixel keeps its alpha
		west := clone(terrain)
		Blend(west, f, 5, 15, 15)
		if !bytes.Equal(west.Pix[:4*200], terrain.Pix[:4*200]) {
			t.Fatal("explored pixels far from the edge changed")
		}
		if west.Pix[4*255] == terrain.Pix[4*255] && west.Pix[4*255+1] == terrain.Pix[4*255+1] {
			t.Error("the pixel next to the edge is not fogged at all")
		}
		east := clone(terrain)
		Blend(east, f, 5, 16, 15)
		want := FogTile(5, 16, 15)
		want.Pix[3] = 77
		if !bytes.Equal(east.Pix, want.Pix) {
			t.Fatal("unexplored pixels are not the texture alone (terrain leaks)")
		}
	}
}

func TestClassesAreSound(t *testing.T) {
	m := explored.FromZones([][2]int16{{0, 0}, {1, 0}, {1, 1}, {-40, 20}, {60, -60}})
	for x := int16(-5); x <= 5; x++ {
		for z := int16(-5); z <= 5; z++ {
			m.Union(explored.FromZones([][2]int16{{x + 30, z + 30}}))
		}
	}
	f := NewField(m)
	c := NewClassMap(f)
	terrain := noiseTile(3)
	seen := map[Class]int{}
	for z := 0; z <= 4; z++ {
		n := 1 << z
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				cl := c.At(z, x, y)
				seen[cl]++
				got := clone(terrain)
				Blend(got, f, z, x, y)
				switch cl {
				case Clear:
					// Outside the world disc a Clear tile shows terrain that
					// is transparent there, so only pixels inside count.
					for py := 0; py < TileSize; py++ {
						for px := 0; px < TileSize; px++ {
							if insideDisc(z, x*TileSize+px, y*TileSize+py) && got.NRGBAAt(px, py) != terrain.NRGBAAt(px, py) {
								t.Fatalf("z%d %d,%d is Clear but Blend changes pixel %d,%d", z, x, y, px, py)
							}
						}
					}
				case Fog:
					if !bytes.Equal(got.Pix, FogTile(z, x, y).Pix) {
						t.Fatalf("z%d %d,%d is Fog but Blend shows terrain", z, x, y)
					}
				}
			}
		}
	}
	if seen[Clear] == 0 || seen[Fog] == 0 || seen[Edge] == 0 {
		t.Fatalf("classes seen = %v, want all three", seen)
	}
}

// insideDisc reports whether global pixel (gx, gy)'s centre at zoom z lies
// within the world radius.
func insideDisc(z, gx, gy int) bool {
	mpp := MetresPerPixel(z)
	wx := -worldRadius + (float64(gx)+0.5)*mpp
	wz := worldRadius - (float64(gy)+0.5)*mpp
	return wx*wx+wz*wz <= worldRadius*worldRadius
}

func TestClassMapOnTheWestHalf(t *testing.T) {
	c := NewClassMap(NewField(westHalf()))
	for _, k := range []struct {
		z, x, y int
		want    Class
	}{
		{5, 5, 16, Clear},  // deep in the explored west
		{5, 16, 16, Fog},   // unexplored, inside the disc
		{5, 15, 16, Edge},  // the explored edge runs through it
		{5, 31, 16, Edge},  // unexplored but crossing the world rim
		{5, 0, 0, Clear},   // wholly outside the disc
		{0, 0, 0, Edge},    // everything
		{6, 31, 33, Edge},  // z6 next to the edge
		{6, 33, 33, Fog},   // z6 east of it
		{6, 20, 40, Clear}, // z6 deep west
	} {
		if got := c.At(k.z, k.x, k.y); got != k.want {
			t.Errorf("z%d %d,%d = %v, want %v", k.z, k.x, k.y, got, k.want)
		}
	}
}

func TestUpscaleIsPremultipliedBilinear(t *testing.T) {
	parent := image.NewNRGBA(image.Rect(0, 0, TileSize, TileSize))
	for i := 0; i < len(parent.Pix); i += 4 {
		copy(parent.Pix[i:], []uint8{10, 20, 30, 255})
	}
	flat := Upscale(parent, 1, 0)
	for i := 0; i < len(flat.Pix); i += 4 {
		if !bytes.Equal(flat.Pix[i:i+4], []uint8{10, 20, 30, 255}) {
			t.Fatalf("flat upscale changed pixel %d: %v", i/4, flat.Pix[i:i+4])
		}
	}
	parent.SetNRGBA(0, 0, color.NRGBA{200, 0, 0, 255})
	parent.SetNRGBA(1, 0, color.NRGBA{0, 0, 0, 0}) // transparent: must not darken the red
	parent.SetNRGBA(0, 1, color.NRGBA{200, 0, 0, 255})
	parent.SetNRGBA(1, 1, color.NRGBA{0, 0, 0, 0})
	up := Upscale(parent, 0, 0)
	if p := up.NRGBAAt(0, 0); p != (color.NRGBA{200, 0, 0, 255}) {
		t.Errorf("(0,0) = %v, want the clamped corner pixel", p)
	}
	if p := up.NRGBAAt(1, 0); p != (color.NRGBA{200, 0, 0, 191}) {
		t.Errorf("(1,0) = %v, want red at 3/4 alpha", p)
	}
	if p := up.NRGBAAt(2, 0); p != (color.NRGBA{200, 0, 0, 64}) {
		t.Errorf("(2,0) = %v, want red at 1/4 alpha", p)
	}
}
