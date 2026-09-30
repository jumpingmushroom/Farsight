package worldgen

import (
	"fmt"
	"math"
)

// Static WorldGenerator constants (WorldGenerator.cs).
const (
	ashlandsMinDistance float32 = 12000
	ashlandsYOffset     float32 = -4000
)

// MaxGenVersion is the highest world generator version this package ports
// (reference/valheim/World.cs sets m_worldGenVersion = 2 for a newly
// created world). versionSetup only branches on version <= 0 and
// version <= 1, so it is well-defined for any version >= 0, but a version
// higher than the game itself has ever produced is almost certainly a
// caller mistake (e.g. a save from a future game update this port hasn't
// been extended for), not a legitimate old-format world.
const MaxGenVersion int32 = 2

// Generator ports Valheim's WorldGenerator: biome classification, terrain
// height, lakes, rivers and streams. NewBase performs the constructor's
// offset draws only; New also runs the lake/river/stream pre-generation.
// After New returns, a Generator is read-only and safe for concurrent use.
type Generator struct {
	offset0, offset1, offset2, offset3, offset4 float32
	riverSeed, streamSeed                       int32

	maxMarshDistance    float32
	minDarklandNoise    float32
	minMountainDistance float32

	noiseGen *fastNoise

	// Lakes, rivers and streams (rivers.go); set by pregenerate.
	lakes           [][2]float32
	rivers, streams []River
	riverPoints     map[[2]int32][]riverPoint

	// GetRiverWeight's one-entry cache, reproduced only while pregenerating.
	pregenerating     bool
	cachedRiverGrid   [2]int32
	cachedRiverPoints []riverPoint
}

// NewBase mirrors WorldGenerator's private constructor: VersionSetup, then
// UnityEngine.Random.InitState(seed) and the offset/seed draws, in order,
// plus the FastNoise setup (reference/valheim/WorldGenerator.cs ~214-222):
// new FastNoise(seed) with NoiseType.Cellular / Euclidean / Distance /
// SetFractalOctaves(2) baked into newFastNoise, immediately followed by
// SetSeed(0) -- so the noise generator always ends up seeded with 0.
// It skips Pregenerate (rivers/lakes/streams).
func NewBase(seed int32, genVersion int32) *Generator {
	g := &Generator{
		maxMarshDistance:    6000,
		minDarklandNoise:    0.4,
		minMountainDistance: 1000,
	}
	g.versionSetup(genVersion)

	g.noiseGen = newFastNoise(seed)
	g.noiseGen.setSeed(0)

	r := NewURandom(seed)
	g.offset0 = float32(r.RangeInt(-10000, 10000))
	g.offset1 = float32(r.RangeInt(-10000, 10000))
	g.offset2 = float32(r.RangeInt(-10000, 10000))
	g.offset3 = float32(r.RangeInt(-10000, 10000))
	g.riverSeed = r.RangeInt(math.MinInt32, math.MaxInt32)
	g.streamSeed = r.RangeInt(math.MinInt32, math.MaxInt32)
	g.offset4 = float32(r.RangeInt(-10000, 10000))
	return g
}

// New mirrors the full WorldGenerator constructor for a non-menu world:
// NewBase followed by Pregenerate (lakes, rivers, streams). The river seeds
// were already drawn by NewBase; pre-generation uses its own URandoms, so
// the constructor's RNG state does not matter here.
func New(seed, genVersion int32) *Generator {
	g := NewBase(seed, genVersion)
	g.pregenerate()
	return g
}

// NewChecked is New with a genVersion range check: it returns an error
// without constructing a Generator when genVersion is negative or exceeds
// MaxGenVersion, instead of silently mis-generating a world (versionSetup's
// branches degrade gracefully for an unknown version, but that just means
// the bug would be invisible until the terrain doesn't match the game).
func NewChecked(seed, genVersion int32) (*Generator, error) {
	if genVersion < 0 || genVersion > MaxGenVersion {
		return nil, fmt.Errorf("worldgen: genVersion %d out of range [0, %d]", genVersion, MaxGenVersion)
	}
	return New(seed, genVersion), nil
}

// versionSetup ports WorldGenerator.VersionSetup.
func (g *Generator) versionSetup(version int32) {
	if version <= 0 {
		g.minMountainDistance = 1500
	}
	if version <= 1 {
		g.minDarklandNoise = 0.5
		g.maxMarshDistance = 8000
	}
}

// biomeNoise ports the repeated
//
//	DUtils.PerlinNoise((double)(float)((double)offset + (double)wx) * 0.0010000000474974513,
//	                   (double)(float)((double)offset + (double)wy) * 0.0010000000474974513)
//
// pattern used four times by GetBiome.
func (g *Generator) biomeNoise(offset, wx, wy float32) float32 {
	sx := float32(float64(offset) + float64(wx))
	sy := float32(float64(offset) + float64(wy))
	return Perlin(float32(float64(sx)*0.0010000000474974513), float32(float64(sy)*0.0010000000474974513))
}

// Biome ports WorldGenerator.GetBiome(float, float, oceanLevel: 0.02f,
// waterAlwaysOcean: false). The m_world.m_menu branch is not ported (there
// is no menu concept here).
func (g *Generator) Biome(wx, wy float32) Biome {
	const oceanLevel float32 = 0.02

	num := dLength(wx, wy)
	baseHeight := g.BaseHeight(wx, wy)
	num2 := float32(float64(worldAngle(wx, wy)) * 100.0)

	if isAshlands(wx, wy) {
		return AshLands
	}
	if baseHeight <= oceanLevel {
		return Ocean
	}
	if isDeepnorth(wx, wy) {
		return DeepNorth
	}
	if baseHeight > 0.4 {
		return Mountain
	}
	if g.biomeNoise(g.offset0, wx, wy) > 0.6 && num > 2000 && num < g.maxMarshDistance && baseHeight > 0.05 && baseHeight < 0.25 {
		return Swamp
	}
	if g.biomeNoise(g.offset4, wx, wy) > g.minDarklandNoise && num > float32(6000.0+float64(num2)) && num < 10000 {
		return Mistlands
	}
	if g.biomeNoise(g.offset1, wx, wy) > 0.4 && num > float32(3000.0+float64(num2)) && num < 8000 {
		return Plains
	}
	if g.biomeNoise(g.offset2, wx, wy) > 0.4 && num > float32(600.0+float64(num2)) && num < 6000 {
		return BlackForest
	}
	if num > float32(5000.0+float64(num2)) {
		return BlackForest
	}
	return Meadows
}

// isAshlands ports WorldGenerator.IsAshlands.
func isAshlands(x, y float32) bool {
	num := float64(worldAngle(x, y)) * 100.0
	yOff := float32(float64(y) + float64(ashlandsYOffset))
	return float64(dLength(x, yOff)) > float64(ashlandsMinDistance)+num
}

// isDeepnorth ports WorldGenerator.IsDeepnorth. Note it uses Unity's
// Vector2.magnitude (squares summed in float32, sqrt in float64), not
// DUtils.Length (squares summed in float64) -- the two are not
// interchangeable at float32 precision.
func isDeepnorth(x, y float32) bool {
	num := float32(float64(worldAngle(x, y)) * 100.0)
	yOff := float32(float64(y) + 4000.0)
	return vector2Magnitude(x, yOff) > float32(12000.0+float64(num))
}

// worldAngle ports WorldGenerator.WorldAngle.
func worldAngle(wx, wy float32) float32 {
	a1 := float32(math.Atan2(float64(wx), float64(wy)))
	a2 := float32(float64(a1) * 20.0)
	return float32(math.Sin(float64(a2)))
}

// BaseHeight ports WorldGenerator.GetBaseHeight(wx, wy, menuTerrain: false).
// The menuTerrain: true branch is not ported (see the Biome doc comment).
func (g *Generator) BaseHeight(wx, wy float32) float32 {
	num := float32(0)
	num2 := float32(1)

	num6 := dLength(wx, wy)
	num7 := float64(wx)
	num8 := float64(wy)
	num7 += 100000.0 + float64(g.offset0)
	num8 += 100000.0 + float64(g.offset1)

	num9 := float32(0)
	num9 = float32(float64(num9) + float64(Perlin(float32(num7*0.0020000000949949026*0.5), float32(num8*0.0020000000949949026*0.5)))*float64(Perlin(float32(num7*0.003000000026077032*0.5), float32(num8*0.003000000026077032*0.5)))*1.0)
	num9 = float32(float64(num9) + float64(Perlin(float32(num7*0.0020000000949949026*1.0), float32(num8*0.0020000000949949026*1.0)))*float64(Perlin(float32(num7*0.003000000026077032*1.0), float32(num8*0.003000000026077032*1.0)))*float64(num9)*0.8999999761581421)
	num9 = float32(float64(num9) + float64(Perlin(float32(num7*0.004999999888241291*1.0), float32(num8*0.004999999888241291*1.0)))*float64(Perlin(float32(num7*0.009999999776482582*1.0), float32(num8*0.009999999776482582*1.0)))*0.5*float64(num9))
	num9 = float32(float64(num9) - 0.07000000029802322)

	num10 := Perlin(float32(num7*0.0020000000949949026*0.25+0.12300000339746475), float32(num8*0.0020000000949949026*0.25+0.15123000741004944))
	num11 := Perlin(float32(num7*0.0020000000949949026*0.25+0.32100000977516174), float32(num8*0.0020000000949949026*0.25+0.23100000619888306))
	v := absF32(float32(float64(num10) - float64(num11)))
	num12 := float32(1.0 - float64(dLerpStep(0.02, 0.12, v)))
	num12 = float32(float64(num12) * float64(dSmoothStep(744, 1000, num6)))
	num9 = float32(float64(num9) * (1.0 - float64(num12)))

	if num6 > 10000 {
		t := dLerpStep(10000, 10500, num6)
		num9 = dLerp(num9, -0.2, t)
		const num13 float32 = 10490
		if num6 > num13 {
			t2 := uLerpStep(num13, 10500, num6)
			num9 = dLerp(num9, -2, t2)
		}
		return num9*num2 + num
	}
	if num6 < g.minMountainDistance && num9 > 0.28 {
		t3 := float32(dClamp01((float64(num9) - 0.2800000011920929) / 0.09999999403953552))
		lower := float32(float64(g.minMountainDistance) - 400.0)
		num9 = dLerp(dLerp(0.28, 0.38, t3), num9, dLerpStep(lower, g.minMountainDistance, num6))
	}
	return num9*num2 + num
}

// The following are literal ports of the DUtils.cs / Utils.cs overloads that
// WorldGenerator calls, named for their C# original ('d' for DUtils, 'u' for
// Utils) plus Unity's Vector2.magnitude.

// dLength ports DUtils.Length(float, float).
func dLength(x, y float32) float32 {
	return float32(math.Sqrt(float64(x)*float64(x) + float64(y)*float64(y)))
}

// dLerp ports DUtils.Lerp(float, float, float).
func dLerp(a, b, t float32) float32 {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	return float32(float64(a)*(1.0-float64(t)) + float64(b)*float64(t))
}

// dLerpStep ports DUtils.LerpStep(float, float, float).
func dLerpStep(l, h, v float32) float32 {
	return float32(dClamp01((float64(v) - float64(l)) / (float64(h) - float64(l))))
}

// dSmoothStep ports DUtils.SmoothStep(float, float, float).
func dSmoothStep(pMin, pMax, pX float32) float32 {
	num := float32(dClamp01((float64(pX) - float64(pMin)) / (float64(pMax) - float64(pMin))))
	return float32(float64(num) * float64(num) * (3.0 - 2.0*float64(num)))
}

// dClamp01 ports DUtils.Clamp01(double).
func dClamp01(v float64) float64 {
	if v > 1.0 {
		return 1.0
	}
	if v < 0.0 {
		return 0.0
	}
	return v
}

// uLerpStep ports Utils.LerpStep(float, float, float), which -- unlike
// DUtils.LerpStep -- does all of its arithmetic in float32.
func uLerpStep(l, h, v float32) float32 {
	return uClamp01((v - l) / (h - l))
}

// uClamp01 ports Utils.Clamp01(float).
func uClamp01(v float32) float32 {
	if v > 1 {
		return 1
	}
	if v < 0 {
		return 0
	}
	return v
}

// vector2Magnitude ports UnityEngine.Vector2.magnitude: the squares are
// summed in float32, then the sqrt happens in float64.
func vector2Magnitude(x, y float32) float32 {
	sq := x*x + y*y
	return float32(math.Sqrt(float64(sq)))
}

// absF32 ports Mathf.Abs(float).
func absF32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
