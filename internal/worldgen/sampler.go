package worldgen

// Sampler is a tiles.Source like TerrainView that caches the last zone's corner biomes.
// TerrainHeight recomputes the four corner biomes of the enclosing 64 m
// zone on every call; a tile row walks ~25 pixels per zone, so reusing them
// while the zone is unchanged saves most of that work. Results are
// bit-identical to TerrainHeight: the cache is keyed on the same zoneAxis
// indices and both paths share zoneCorners/blendCorners.
//
// A Sampler is not safe for concurrent use; give each goroutine its own.
type Sampler struct {
	g            *Generator
	zoneX, zoneZ int32
	valid        bool
	c            zoneCorners
}

// NewSampler returns a fresh Sampler over v's Generator.
func (v TerrainView) NewSampler() *Sampler {
	return &Sampler{g: v.g}
}

// Biome is g.Biome, uncached.
func (s *Sampler) Biome(wx, wy float32) Biome {
	return s.g.Biome(wx, wy)
}

// Height equals g.TerrainHeight(wx, wy) bit-for-bit.
func (s *Sampler) Height(wx, wy float32) float32 {
	zx, zz := zoneAxis(wx), zoneAxis(wy)
	if !s.valid || zx != s.zoneX || zz != s.zoneZ {
		s.c = s.g.zoneCorners(zx, zz)
		s.zoneX, s.zoneZ, s.valid = zx, zz, true
	}
	return s.g.blendCorners(&s.c, wx, wy)
}
