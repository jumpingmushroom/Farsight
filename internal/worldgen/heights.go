package worldgen

import "math"

// Per-biome terrain height, ported literally from
// reference/valheim/WorldGenerator.cs (GetBiomeHeight and the Get*Height
// family, ~1018-1455). Every (float)/(double) cast of the decompiled C# is
// kept: C# float is float32, double is float64. The m_world.m_menu branches
// and the Color mask outputs are not ported (mask values never feed back
// into the height).

// heightMultiplier ports WorldGenerator.GetHeightMultiplier.
const heightMultiplier float32 = 200

// Height ports WorldGenerator.GetHeight(float, float): GetBiome followed by
// GetBiomeHeight with preGeneration=false. The result is in world metres;
// the water line is at 30.
func (g *Generator) Height(wx, wy float32) float32 {
	return g.biomeHeight(g.Biome(wx, wy), wx, wy, false, true)
}

// pregenerationHeight ports WorldGenerator.GetPregenerationHeight.
func (g *Generator) pregenerationHeight(wx, wy float32, riverPreGen bool) float32 {
	return g.biomeHeight(g.Biome(wx, wy), wx, wy, true, riverPreGen)
}

// biomeHeight ports WorldGenerator.GetBiomeHeight (~1018-1071). The unused
// GetBiomeSector call and the m_menu branch are omitted.
func (g *Generator) biomeHeight(biome Biome, wx, wy float32, preGeneration, riverPreDN bool) float32 {
	num := float32(0)
	var num2 float32
	if !preGeneration {
		num2 = float32(float64(heightMultiplier) * createAshlandsGap(wx, wy) * createDeepNorthGap(wx, wy))
	} else {
		num2 = heightMultiplier
	}
	if dLength(wx, wy) > 10500 {
		return -2 * heightMultiplier
	}
	scale := func(h float32) float32 {
		return float32(float64(h)*float64(num2) + float64(num))
	}
	switch biome {
	case Swamp:
		return scale(g.marshHeight(wx, wy))
	case DeepNorth:
		if preGeneration {
			return scale(g.deepNorthHeightPregenerate(wx, wy, riverPreDN))
		}
		return scale(g.deepNorthHeight(wx, wy))
	case Mountain:
		return scale(g.snowMountainHeight(wx, wy))
	case BlackForest:
		return scale(g.forestHeight(wx, wy))
	case Ocean:
		return scale(g.oceanHeight(wx, wy))
	case AshLands:
		if preGeneration {
			return scale(g.ashlandsHeightPregenerate(wx, wy))
		}
		return scale(g.ashlandsHeight(wx, wy))
	case Plains:
		return scale(g.plainsHeight(wx, wy))
	case Meadows:
		return scale(g.meadowsHeight(wx, wy))
	case Mistlands:
		if preGeneration {
			return scale(g.forestHeight(wx, wy))
		}
		return scale(g.mistlandsHeight(wx, wy))
	default:
		return 0
	}
}

// pd ports DUtils.PerlinNoise(double, double): the arguments are narrowed
// to float and passed to Mathf.PerlinNoise.
func pd(x, y float64) float64 {
	return float64(Perlin(float32(x), float32(y)))
}

// marshHeight ports WorldGenerator.GetMarshHeight (~1073).
func (g *Generator) marshHeight(wx, wy float32) float32 {
	wx2, wy2 := wx, wy
	num := float32(0.137)
	wx = float32(float64(wx) + 100000.0)
	wy = float32(float64(wy) + 100000.0)
	num2 := float64(wx)
	num3 := float64(wy)
	num4 := float32(pd(num2*0.03999999910593033, num3*0.03999999910593033) * pd(num2*0.07999999821186066, num3*0.07999999821186066))
	num = float32(float64(num) + float64(num4)*0.029999999329447746)
	num = g.addRivers(wx2, wy2, num)
	num = float32(float64(num) + pd(num2*0.10000000149011612, num3*0.10000000149011612)*0.009999999776482582)
	return float32(float64(num) + pd(num2*0.4000000059604645, num3*0.4000000059604645)*0.003000000026077032)
}

// detailNoise ports the num3 block shared by the Meadows, Forest, Plains,
// Ashlands-pregenerate, SnowMountain, DeepNorth and DeepNorth-pregenerate
// height functions:
//
//	float n = (float)(P(x*0.01, y*0.01) * P(x*0.02, y*0.02));
//	n = (float)(n + P(x*0.05, y*0.05) * P(x*0.1, y*0.1) * n * 0.5);
func detailNoise(x, y float64) float32 {
	n := float32(pd(x*0.009999999776482582, y*0.009999999776482582) * pd(x*0.019999999552965164, y*0.019999999552965164))
	return float32(float64(n) + pd(x*0.05000000074505806, y*0.05000000074505806)*pd(x*0.10000000149011612, y*0.10000000149011612)*float64(n)*0.5)
}

// offset3Coords ports the repeated
//
//	wx = (float)((double)wx + 100000.0 + (double)m_offset3);
func (g *Generator) offset3Coords(wx, wy float32) (float64, float64) {
	x := float32(float64(wx) + 100000.0 + float64(g.offset3))
	y := float32(float64(wy) + 100000.0 + float64(g.offset3))
	return float64(x), float64(y)
}

// meadowsHeight ports WorldGenerator.GetMeadowsHeight (~1088).
func (g *Generator) meadowsHeight(wx, wy float32) float32 {
	baseHeight := g.BaseHeight(wx, wy)
	num, num2 := g.offset3Coords(wx, wy)
	num3 := detailNoise(num, num2)
	num4 := baseHeight
	num4 = float32(float64(num4) + float64(num3)*0.10000000149011612)
	num5 := float32(0.15)
	num6 := float32(float64(num4) - float64(num5))
	num7 := float32(dClamp01(float64(baseHeight) / 0.4000000059604645))
	if num6 > 0 {
		num4 = float32(float64(num4) - float64(num6)*((1.0-float64(num7))*0.75))
	}
	num4 = g.addRivers(wx, wy, num4)
	num4 = float32(float64(num4) + pd(num*0.10000000149011612, num2*0.10000000149011612)*0.009999999776482582)
	return float32(float64(num4) + pd(num*0.4000000059604645, num2*0.4000000059604645)*0.003000000026077032)
}

// forestHeight ports WorldGenerator.GetForestHeight (~1113).
func (g *Generator) forestHeight(wx, wy float32) float32 {
	baseHeight := g.BaseHeight(wx, wy)
	num, num2 := g.offset3Coords(wx, wy)
	num3 := detailNoise(num, num2)
	baseHeight = float32(float64(baseHeight) + float64(num3)*0.10000000149011612)
	baseHeight = g.addRivers(wx, wy, baseHeight)
	baseHeight = float32(float64(baseHeight) + pd(num*0.10000000149011612, num2*0.10000000149011612)*0.009999999776482582)
	return float32(float64(baseHeight) + pd(num*0.4000000059604645, num2*0.4000000059604645)*0.003000000026077032)
}

// mistlandsHeight ports WorldGenerator.GetMistlandsHeight (~1130); the mask
// (num5) is not computed.
func (g *Generator) mistlandsHeight(wx, wy float32) float32 {
	baseHeight := g.BaseHeight(wx, wy)
	num, num2 := g.offset3Coords(wx, wy)
	// float * float: DUtils.PerlinNoise returns float, the product is float.
	num3 := Perlin(float32(num*0.019999999552965164*0.699999988079071), float32(num2*0.019999999552965164*0.699999988079071)) *
		Perlin(float32(num*0.03999999910593033*0.699999988079071), float32(num2*0.03999999910593033*0.699999988079071))
	num3 = float32(float64(num3) + pd(num*0.029999999329447746*0.699999988079071, num2*0.029999999329447746*0.699999988079071)*pd(num*0.05000000074505806*0.699999988079071, num2*0.05000000074505806*0.699999988079071)*float64(num3)*0.5)
	if num3 > 0 {
		num3 = float32(math.Pow(float64(num3), 1.5))
	}
	baseHeight = float32(float64(baseHeight) + float64(num3)*0.4000000059604645)
	baseHeight = g.addRivers(wx, wy, baseHeight)
	num4 := float32(dClamp01(float64(num3) * 7.0))
	baseHeight = float32(float64(baseHeight) + pd(num*0.10000000149011612, num2*0.10000000149011612)*0.029999999329447746*float64(num4))
	baseHeight = float32(float64(baseHeight) + pd(num*0.4000000059604645, num2*0.4000000059604645)*0.009999999776482582*float64(num4))
	a := float32(float64(baseHeight) + pd(num*0.4000000059604645, num2*0.4000000059604645)*0.0020000000949949026)
	num6 := baseHeight
	num6 = float32(float64(num6) * 400.0)
	num6 = float32(math.Ceil(float64(num6)))
	num6 = float32(float64(num6) / 400.0)
	return dLerp(a, num6, num4)
}

// plainsHeight ports WorldGenerator.GetPlainsHeight (~1161). Note the
// float32 subtraction for num6 and the operator grouping in the tilt, both
// of which differ from GetMeadowsHeight.
func (g *Generator) plainsHeight(wx, wy float32) float32 {
	baseHeight := g.BaseHeight(wx, wy)
	num, num2 := g.offset3Coords(wx, wy)
	num3 := detailNoise(num, num2)
	num4 := baseHeight
	num4 = float32(float64(num4) + float64(num3)*0.10000000149011612)
	num5 := float32(0.15)
	num6 := num4 - num5
	num7 := float32(dClamp01(float64(baseHeight) / 0.4000000059604645))
	if num6 > 0 {
		num4 = float32(float64(num4) - float64(num6)*(1.0-float64(num7))*0.75)
	}
	num4 = g.addRivers(wx, wy, num4)
	num4 = float32(float64(num4) + pd(num*0.10000000149011612, num2*0.10000000149011612)*0.009999999776482582)
	return float32(float64(num4) + pd(num*0.4000000059604645, num2*0.4000000059604645)*0.003000000026077032)
}

// ashlandsHeightPregenerate ports WorldGenerator.GetAshlandsHeightPregenerate (~1200).
func (g *Generator) ashlandsHeightPregenerate(wx, wy float32) float32 {
	baseHeight := g.BaseHeight(wx, wy)
	num, num2 := g.offset3Coords(wx, wy)
	num3 := detailNoise(num, num2)
	baseHeight = float32(float64(baseHeight) + float64(num3)*0.10000000149011612)
	baseHeight = float32(float64(baseHeight) + 0.10000000149011612)
	baseHeight = float32(float64(baseHeight) + pd(num*0.10000000149011612, num2*0.10000000149011612)*0.009999999776482582)
	baseHeight = float32(float64(baseHeight) + pd(num*0.4000000059604645, num2*0.4000000059604645)*0.003000000026077032)
	return g.addRivers(wx, wy, baseHeight)
}

// ashlandsHeight ports WorldGenerator.GetAshlandsHeight(wx, wy, out mask,
// cheap: false) (~1218). It works almost entirely in double and is the only
// caller of the FastNoise port.
func (g *Generator) ashlandsHeight(wx, wy float32) float32 {
	num := float64(wx)
	num2 := float64(wy)
	a := float64(g.BaseHeight(float32(num), float32(num2)))
	num3 := float64(worldAngle(float32(num), float32(num2))) * 100.0
	value := dLengthD(num, num2+float64(ashlandsYOffset)-float64(ashlandsYOffset)*0.3) - (float64(ashlandsMinDistance) + num3)
	value = math.Abs(value) / 1000.0
	value = 1.0 - dClamp01(value)
	value = dMathfLikeSmoothStep(0.1, 1.0, value)
	num4 := math.Abs(num)
	num4 = 1.0 - dClamp01(num4/7500.0)
	value *= num4
	num5 := dLengthD(num, num2) - 10150.0
	num5 = 1.0 - dClamp01(num5/600.0)
	num += float64(100000 + g.offset3)
	num2 += float64(100000 + g.offset3)
	num6 := 0.0
	num7 := 1.0
	num8 := 0.33000001311302185
	for i := 0; i < 5; i++ {
		num6 += num7 * dMathfLikeSmoothStep(0.0, 1.0, g.noiseGen.getCellular(num*num8, num2*num8))
		num8 *= 2.0
		num7 *= 0.5
	}
	num6 = dRemap(num6, -1.0, 1.0, 0.0, 1.0)
	num10 := dLerpD(value, dBlendOverlay(value, num6), 0.5)
	// num11 (the detail noise) is computed by the C# but never used; its
	// PerlinNoise calls have no side effects, so it is omitted.
	num12 := dLerpD(a, 0.15000000596046448, 0.75)
	num12 += num10 * 0.5
	num12 = dLerpD(-1.0, num12, dMathfLikeSmoothStep(0.0, 1.0, num5))
	num13 := 0.15
	num14 := 0.0
	num15 := 1.0
	num16 := 8.0
	for j := 0; j < 3; j++ {
		num14 += num15 * g.noiseGen.getCellular(num*num16, num2*num16)
		num16 *= 2.0
		num15 *= 0.5
	}
	num14 = dRemap(num14, -1.0, 1.0, 0.0, 1.0)
	num14 = dClamp01(math.Pow(num14, 4.0) * 2.0)
	simplexFractal := g.noiseGen.getSimplexFractal(num*0.075, num2*0.075)
	simplexFractal = dRemap(simplexFractal, -1.0, 1.0, 0.0, 1.0)
	simplexFractal = math.Pow(simplexFractal, 1.399999976158142)
	num12 *= simplexFractal
	num18 := dFbmD(float32(num*0.009999999776482582), float32(num2*0.009999999776482582), 3, 2.0, 0.5)
	num18 *= dClamp01(dRemap(value, 0.0, 0.5, 0.5, 1.0))
	num18 = dLerpStepD(0.699999988079071, 1.0, num18)
	num18 = math.Pow(num18, 2.0)
	num19 := dBlendOverlay(num18, num14)
	num19 *= dClamp01((num12 - num13 - 0.02) / 0.01)
	x := pd(num*0.05+5124.0, num2*0.05+5000.0)
	x = math.Pow(x, 2.0)
	x = dRemap(x, 0.0, 1.0, 0.009999999776482582, 0.054999999701976776)
	b := float64(mathfClamp(float32(num12-x), float32(num13+0.009999999776482582), 5000))
	num12 = dLerpD(num12, b, num19)
	return float32(num12)
}

// edgeHeight ports WorldGenerator.GetEdgeHeight (~1291). GetBiomeHeight
// does not call it; it is ported for completeness.
func (g *Generator) edgeHeight(wx, wy float32) float32 {
	num := dLength(wx, wy)
	const num2 float32 = 10490
	if num > num2 {
		num3 := dLerpStep(num2, 10500, num)
		return float32(-2.0 * float64(num3))
	}
	t := dLerpStep(10000, 10100, num)
	baseHeight := g.BaseHeight(wx, wy)
	baseHeight = dLerp(baseHeight, 0, t)
	return g.addRivers(wx, wy, baseHeight)
}

// oceanHeight ports WorldGenerator.GetOceanHeight.
func (g *Generator) oceanHeight(wx, wy float32) float32 {
	return g.BaseHeight(wx, wy)
}

// baseHeightTilt ports WorldGenerator.BaseHeightTilt.
func (g *Generator) baseHeightTilt(wx, wy float32) float32 {
	baseHeight := g.BaseHeight(float32(float64(wx)-1.0), wy)
	baseHeight2 := g.BaseHeight(float32(float64(wx)+1.0), wy)
	baseHeight3 := g.BaseHeight(wx, float32(float64(wy)-1.0))
	baseHeight4 := g.BaseHeight(wx, float32(float64(wy)+1.0))
	return float32(float64(absF32(float32(float64(baseHeight2)-float64(baseHeight)))) + float64(absF32(float32(float64(baseHeight3)-float64(baseHeight4)))))
}

// snowMountainHeight ports WorldGenerator.GetSnowMountainHeight(wx, wy,
// menu: false) (~1321).
func (g *Generator) snowMountainHeight(wx, wy float32) float32 {
	baseHeight := g.BaseHeight(wx, wy)
	num := g.baseHeightTilt(wx, wy)
	num2, num3 := g.offset3Coords(wx, wy)
	num4 := float32(float64(baseHeight) - 0.4000000059604645)
	baseHeight = float32(float64(baseHeight) + float64(num4))
	num5 := detailNoise(num2, num3)
	baseHeight = float32(float64(baseHeight) + float64(num5)*0.20000000298023224)
	baseHeight = g.addRivers(wx, wy, baseHeight)
	baseHeight = float32(float64(baseHeight) + pd(num2*0.10000000149011612, num3*0.10000000149011612)*0.009999999776482582)
	baseHeight = float32(float64(baseHeight) + pd(num2*0.4000000059604645, num3*0.4000000059604645)*0.003000000026077032)
	return float32(float64(baseHeight) + pd(num2*0.20000000298023224, num3*0.20000000298023224)*2.0*float64(num))
}

// deepNorthHeightPregenerate ports WorldGenerator.GetDeepNorthHeightPregenerate (~1342).
func (g *Generator) deepNorthHeightPregenerate(wx, wy float32, riverPregen bool) float32 {
	wx2, wy2 := wx, wy
	num := g.BaseHeight(wx, wy)
	if !riverPregen {
		num += 0.1
	}
	wx = float32(float64(wx) + 100000.0 + float64(g.offset3))
	wy = float32(float64(wy) + 100000.0 + float64(g.offset3))
	num2 := float64(wx)
	num3 := float64(wy)
	num4 := max(0, float32(float64(num)-0.4000000059604645))
	num = float32(float64(num) + float64(num4))
	num5 := detailNoise(num2, num3)
	num = float32(float64(num) + float64(num5)*0.20000000298023224)
	num = float32(float64(num) * 1.2000000476837158)
	num = g.addRivers(wx2, wy2, num)
	// DUtils.PerlinNoise(float, float) with float32 products wx * 0.1f.
	num = float32(float64(num) + float64(Perlin(wx*0.1, wy*0.1))*0.009999999776482582)
	return float32(float64(num) + float64(Perlin(wx*0.4, wy*0.4))*0.003000000026077032)
}

// deepNorthHeight ports WorldGenerator.GetDeepNorthHeight (~1366); the
// mask (num9, an Fbm) is not computed.
func (g *Generator) deepNorthHeight(wx, wy float32) float32 {
	num := g.BaseHeight(wx, wy) + 0.1
	num2, num3 := g.offset3Coords(wx, wy)
	num4 := detailNoise(num2, num3)
	num5 := num
	num5 = float32(float64(num5) + float64(num4)*0.10000000149011612)
	num6 := float32(0.15)
	num7 := float32(float64(num5) - float64(num6))
	num8 := float32(dClamp01(float64(num) / 0.4000000059604645))
	if num7 > 0 {
		num5 = float32(float64(num5) - float64(num7)*((1.0-float64(num8))*0.75))
	}
	num5 = g.addRivers(wx, wy, num5)
	num5 = float32(float64(num5) + pd(num2*0.10000000149011612, num3*0.10000000149011612)*0.009999999776482582)
	return float32(float64(num5) + pd(num2*0.4000000059604645, num3*0.4000000059604645)*0.003000000026077032)
}

// createAshlandsGap ports WorldGenerator.CreateAshlandsGap.
func createAshlandsGap(wx, wy float32) float64 {
	num := float64(worldAngle(wx, wy)) * 100.0
	value := float64(dLength(wx, wy+ashlandsYOffset)) - (float64(ashlandsMinDistance) + num)
	value = dClamp01(math.Abs(value) / 400.0)
	return dMathfLikeSmoothStep(0.0, 1.0, float64(float32(value)))
}

// createDeepNorthGap ports WorldGenerator.CreateDeepNorthGap.
func createDeepNorthGap(wx, wy float32) float64 {
	num := float64(worldAngle(wx, wy)) * 100.0
	value := float64(dLength(wx, wy+4000)) - (12000.0 + num)
	value = dClamp01(math.Abs(value) / 400.0)
	return dMathfLikeSmoothStep(0.0, 1.0, float64(float32(value)))
}

// deepNorthWaveFade ports WorldGenerator.DeepNorthWaveFade.
func deepNorthWaveFade(wx, wy float32) float64 {
	num := float64(worldAngle(wx, wy)) * 100.0
	return dClamp01((float64(dLength(wx, wy+4000)) - (12000.0 + num)) / 200.0)
}

// Double-precision DUtils overloads used by the height functions.

// dLengthD ports DUtils.Length(double, double).
func dLengthD(x, y float64) float64 {
	return math.Sqrt(x*x + y*y)
}

// dLerpD ports DUtils.Lerp(double, double, double).
func dLerpD(a, b, t float64) float64 {
	if t <= 0.0 {
		return a
	}
	if t >= 1.0 {
		return b
	}
	return a*(1.0-t) + b*t
}

// dLerpStepD ports DUtils.LerpStep(double, double, double).
func dLerpStepD(l, h, v float64) float64 {
	return dClamp01((v - l) / (h - l))
}

// dMathfLikeSmoothStep ports DUtils.MathfLikeSmoothStep, including its
// narrowing of the result to float.
func dMathfLikeSmoothStep(from, to, t float64) float64 {
	t = dClamp01(t)
	t = -2.0*t*t*t + 3.0*t*t
	return float64(float32(to*t + from*(1.0-t)))
}

// dBlendOverlay ports DUtils.BlendOverlay.
func dBlendOverlay(a, b float64) float64 {
	result := 2.0 * a * b
	result2 := 1.0 - 2.0*(1.0-a)*(1.0-b)
	if !(a < 0.5) {
		return result2
	}
	return result
}

// dRemap ports DUtils.Remap.
func dRemap(value, inLow, inHigh, outLow, outHigh float64) float64 {
	return dLerpD(outLow, outHigh, dInverseLerp(inLow, inHigh, value))
}

// dInverseLerp ports DUtils.InverseLerp.
func dInverseLerp(a, b, value float64) float64 {
	if a == b {
		return 0.0
	}
	return dClamp01((value - a) / (b - a))
}

// dFbmD ports DUtils.Fbm(Vector2, int, double, double); px/py are the
// Vector2's float components.
func dFbmD(px, py float32, octaves int, lacunarity, gain float64) float64 {
	num := 0.0
	num2 := 1.0
	num3 := float64(px)
	num4 := float64(py)
	for i := 0; i < octaves; i++ {
		num += num2 * pd(num3, num4)
		num2 *= gain
		num3 *= lacunarity
		num4 *= lacunarity
	}
	return num
}

// mathfClamp ports Mathf.Clamp(float, float, float).
func mathfClamp(value, lo, hi float32) float32 {
	if value < lo {
		return lo
	}
	if value > hi {
		return hi
	}
	return value
}
