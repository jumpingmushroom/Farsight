package fog

import (
	"image"
	"math"
)

// FogTile draws tile (z, x, y) as fog alone: the texture, opaque. It is
// exactly what Blend gives on an opaque terrain tile where alpha is 1, so
// a Fog-class tile needs no terrain.
func FogTile(z, x, y int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, TileSize, TileSize))
	for py := 0; py < TileSize; py++ {
		row := img.Pix[py*img.Stride:]
		for px := 0; px < TileSize; px++ {
			o := px * 4
			row[o], row[o+1], row[o+2] = TexturePixel(x*TileSize+px, y*TileSize+py)
			row[o+3] = 255
		}
	}
	return img
}

// Blend fogs terrain tile img (tile (z, x, y), TileSize² at the origin) in
// place: each pixel's colour moves toward the texture by the fog's alpha
// at the pixel's centre, and keeps its own alpha (outside the world disc
// the terrain is transparent and stays so). A pixel with alpha 0 is left
// byte for byte; one with alpha 1 becomes the texture exactly.
func Blend(img *image.NRGBA, f *Field, z, x, y int) {
	w := EdgeWidth(z)
	var us [TileSize]float64
	for px := range us {
		us[px], _ = pixelCell(z, x*TileSize+px, 0)
	}
	for py := 0; py < TileSize; py++ {
		gy := y*TileSize + py
		_, v := pixelCell(z, 0, gy)
		row := img.Pix[py*img.Stride:]
		for px := 0; px < TileSize; px++ {
			a := Alpha(f.fogDistance(us[px], v), w)
			if a == 0 {
				continue
			}
			o := px * 4
			r, g, b := TexturePixel(x*TileSize+px, gy)
			if a == 1 {
				row[o], row[o+1], row[o+2] = r, g, b
				continue
			}
			row[o] = mix(row[o], r, a)
			row[o+1] = mix(row[o+1], g, a)
			row[o+2] = mix(row[o+2], b, a)
		}
	}
}

func mix(terrain, fog uint8, a float64) uint8 {
	return uint8(math.Round(float64(terrain)*(1-a) + float64(fog)*a))
}

// Upscale returns quadrant (qx, qy) of z5 tile parent as a TileSize z6
// tile: a 2× bilinear upscale (as a browser draws a scaled tile), in
// premultiplied alpha so the transparent world rim doesn't darken the
// coast, clamped at the parent's edges.
func Upscale(parent *image.NRGBA, qx, qy int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, TileSize, TileSize))
	for oy := 0; oy < TileSize; oy++ {
		sy0, sy1 := taps(qy, oy)
		for ox := 0; ox < TileSize; ox++ {
			sx0, sx1 := taps(qx, ox)
			var acc [4]float64
			for _, t := range [4]struct {
				x, y int
				w    float64
			}{{sx0, sy0, 0.5625}, {sx1, sy0, 0.1875}, {sx0, sy1, 0.1875}, {sx1, sy1, 0.0625}} {
				p := parent.Pix[t.y*parent.Stride+t.x*4:]
				a := float64(p[3]) * t.w
				acc[0] += float64(p[0]) * a
				acc[1] += float64(p[1]) * a
				acc[2] += float64(p[2]) * a
				acc[3] += a
			}
			o := oy*out.Stride + ox*4
			if acc[3] == 0 {
				continue
			}
			out.Pix[o] = uint8(math.Round(acc[0] / acc[3]))
			out.Pix[o+1] = uint8(math.Round(acc[1] / acc[3]))
			out.Pix[o+2] = uint8(math.Round(acc[2] / acc[3]))
			out.Pix[o+3] = uint8(math.Round(acc[3]))
		}
	}
	return out
}

// taps are the two parent pixels output pixel o of quadrant q samples: the
// nearer i (weight 3/4) and its neighbour on the side o's centre leans to
// (weight 1/4), clamped to the parent.
func taps(q, o int) (near, far int) {
	near = q*TileSize/2 + o/2
	far = near - 1
	if o%2 == 1 {
		far = near + 1
	}
	return near, max(0, min(TileSize-1, far))
}
