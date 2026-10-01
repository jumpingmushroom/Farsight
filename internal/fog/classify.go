package fog

import "math"

// Class is how a tile is served, decided once per fog key.
type Class uint8

const (
	// Clear: alpha is 0 on every pixel (or the tile lies wholly outside
	// the world disc, where the terrain is transparent): the terrain as is.
	Clear Class = iota + 1
	// Fog: alpha is 1 on every pixel and the tile lies wholly inside the
	// disc: the texture alone, without reading terrain.
	Fog
	// Edge: anything else: terrain blended with fog.
	Edge
)

func (c Class) String() string {
	switch c {
	case Clear:
		return "clear"
	case Fog:
		return "fog"
	case Edge:
		return "edge"
	}
	return "unknown"
}

// ClassMap holds every tile's class at z0–MaxZoom.
type ClassMap struct{ levels [MaxZoom + 1][]Class }

// NewClassMap classifies every tile of every zoom from f.
func NewClassMap(f *Field) *ClassMap {
	c := &ClassMap{}
	for z := 0; z <= MaxZoom; z++ {
		n := 1 << z
		c.levels[z] = make([]Class, n*n)
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				c.levels[z][y*n+x] = f.classify(z, x, y)
			}
		}
	}
	return c
}

// At is tile (z, x, y)'s class; the coordinates must be in range.
func (c *ClassMap) At(z, x, y int) Class { return c.levels[z][y*(1<<z)+x] }

// classify bounds the field over every cell a pixel of tile (z, x, y) can
// sample (bilinear: floor(u) and floor(u)+1). Bilinear sampling never
// leaves the cells' range, so the class is exact where it says Clear or
// Fog and merely conservative where it says Edge.
func (f *Field) classify(z, x, y int) Class {
	span := worldSpan / float64(int(1)<<z)
	x0 := -worldRadius + float64(x)*span
	x1 := x0 + span
	z1 := worldRadius - float64(y)*span
	z0 := z1 - span
	near := math.Hypot(max(x0, min(0, x1)), max(z0, min(0, z1)))
	far := math.Hypot(max(-x0, x1), max(-z0, z1))
	if near > worldRadius {
		return Clear
	}
	mpp := MetresPerPixel(z)
	c0 := int(math.Floor((x0+mpp/2)/cellMetres)) + half
	c1 := int(math.Floor((x1-mpp/2)/cellMetres)) + half + 1
	r0 := int(math.Floor((z0+mpp/2)/cellMetres)) + half
	r1 := int(math.Floor((z1-mpp/2)/cellMetres)) + half + 1
	lo, hi := math.Inf(1), math.Inf(-1)
	for py := r0; py <= r1; py++ {
		for px := c0; px <= c1; px++ {
			d := f.at(px, py)
			lo, hi = min(lo, d), max(hi, d)
		}
	}
	switch {
	case hi <= 0 && far <= worldRadius:
		return Fog
	case lo >= EdgeWidth(z):
		return Clear
	}
	return Edge
}
