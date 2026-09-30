package worldgen

import "math"

// TerrainHeight ports HeightmapBuilder.Build, the non-m_distantLod path
// (reference/valheim/HeightmapBuilder.cs ~148-210). The game does not call
// GetHeight directly to build zone terrain: it takes the biome at the four
// corners of the enclosing 64 m zone heightmap and, where the corners
// differ, blends the four corner-biome heights bilinearly with
// DUtils.SmoothStep weights. Without this blend Mountain and Plains terrain
// near biome borders is off by several metres (Task 4's scratch check).
// The colour mask output and the m_distantLod smoothing pass are not
// ported: nothing downstream needs them.
//
// The "enclosing zone" is found by zoneAxis, the true floor of (p+32)/64 --
// i.e. geometric mesh coverage: the Mathf.FloorToInt semantics, deliberately
// NOT ZoneSystem.GetZone's Utils.FloorToInt, which only assigns objects to
// a zone for bookkeeping and is not what places this heightmap mesh under a
// world point. See zoneAxis's doc comment.
func (g *Generator) TerrainHeight(wx, wy float32) float32 {
	c := g.zoneCorners(zoneAxis(wx), zoneAxis(wy))
	return g.blendCorners(&c, wx, wy)
}

const (
	zoneWidth             = 64
	zoneScale     float32 = 1
	zoneHalfWidth float32 = float32(zoneWidth) * zoneScale * 0.5
)

// zoneCorners holds what TerrainHeight derives from the enclosing zone
// alone: the heightmap's corner position and the biomes at its four
// corners. It depends only on the zone indices, which is what lets Sampler
// cache it across calls in the same zone.
type zoneCorners struct {
	cornerX, cornerZ              float32
	biome, biome2, biome3, biome4 Biome
}

// zoneCorners computes the zone geometry (ZoneSystem.GetZone /
// GetZonePos / the zone prefab's Heightmap width=64, scale=1): the
// heightmap is centred on the zone centre, and its corner is
// centre - width*scale/2.
func (g *Generator) zoneCorners(zoneX, zoneZ int32) zoneCorners {
	centreX := float32(zoneX) * 64
	centreZ := float32(zoneZ) * 64
	cornerX := centreX - zoneHalfWidth
	cornerZ := centreZ - zoneHalfWidth
	return zoneCorners{
		cornerX: cornerX,
		cornerZ: cornerZ,
		biome:   g.Biome(cornerX, cornerZ),
		biome2:  g.Biome(cornerX+float32(zoneWidth)*zoneScale, cornerZ),
		biome3:  g.Biome(cornerX, cornerZ+float32(zoneWidth)*zoneScale),
		biome4:  g.Biome(cornerX+float32(zoneWidth)*zoneScale, cornerZ+float32(zoneWidth)*zoneScale),
	}
}

// blendCorners is the per-point half of TerrainHeight: the corner-biome
// heights at (wx, wy), bilinearly blended with DUtils.SmoothStep weights
// when the corners differ.
func (g *Generator) blendCorners(c *zoneCorners, wx, wy float32) float32 {
	if c.biome3 == c.biome && c.biome2 == c.biome && c.biome4 == c.biome {
		return g.biomeHeight(c.biome, wx, wy, false, true)
	}

	l := (wx - c.cornerX) / zoneScale
	k := (wy - c.cornerZ) / zoneScale
	t2 := dSmoothStepF32(0, 1, l/float32(zoneWidth))
	t := dSmoothStepF32(0, 1, k/float32(zoneWidth))

	h1 := g.biomeHeight(c.biome, wx, wy, false, true)
	h2 := g.biomeHeight(c.biome2, wx, wy, false, true)
	h3 := g.biomeHeight(c.biome3, wx, wy, false, true)
	h4 := g.biomeHeight(c.biome4, wx, wy, false, true)

	a := dLerpF32(h1, h2, t2)
	b := dLerpF32(h3, h4, t2)
	return dLerpF32(a, b, t)
}

// zoneAxis returns the zone index along one axis as the true floor of
// (p + 32) / 64: the zone whose 64 m heightmap mesh geometrically covers
// world coordinate p (a zone's mesh is centred on the zone centre and spans
// centre +/- 32 m). This is Mathf.FloorToInt's semantics -- a plain floor
// for every finite value in play here, since world coordinates are bounded
// to +/-10500 m, so an ordinary math.Floor gives the same zone index.
//
// This is deliberately NOT ZoneSystem.GetZone's per-axis
// Utils.FloorToInt((int)(f + 64000f) - 64000), which floors differently in
// a ~0.125 m band just below every zone edge (a float32 rounding quirk of
// adding and subtracting 64000). GetZone's job is assigning objects to a
// zone for bookkeeping, not determining which heightmap mesh geometrically
// covers a point; HeightmapBuilder.Build (TerrainHeight's source) never
// calls GetZone, so that quirk does not apply here.
func zoneAxis(p float32) int32 {
	return int32(math.Floor(float64((p + 32) / 64)))
}

// dSmoothStepF32 ports DUtils.SmoothStep(float, float, float).
func dSmoothStepF32(pMin, pMax, pX float32) float32 {
	num := float32(dClamp01(float64(pX-pMin) / float64(pMax-pMin)))
	return float32(float64(num) * float64(num) * (3.0 - 2.0*float64(num)))
}

// dLerpF32 ports DUtils.Lerp(float, float, float).
func dLerpF32(a, b, t float32) float32 {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	return float32(float64(a)*(1.0-float64(t)) + float64(b)*float64(t))
}

// TerrainView adapts a Generator to the tiles.Source interface (Task 6):
// Biome is g.Biome unchanged, Height is the corner-blended TerrainHeight
// rather than the raw GetHeight port.
type TerrainView struct{ g *Generator }

// TerrainView returns the tiles.Source view of g.
func (g *Generator) TerrainView() TerrainView {
	return TerrainView{g: g}
}

func (v TerrainView) Biome(wx, wy float32) Biome {
	return v.g.Biome(wx, wy)
}

func (v TerrainView) Height(wx, wy float32) float32 {
	return v.g.TerrainHeight(wx, wy)
}
