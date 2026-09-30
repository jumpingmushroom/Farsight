package worldgen

import "math"

// Lakes, rivers and streams, ported literally from
// reference/valheim/WorldGenerator.cs (Pregenerate ... GetWeight, ~259-700,
// and AddRivers, ~937-957). Vector2 arithmetic is float32 per component, as
// in Unity; every (float)/(double) cast of the decompiled C# is kept.
//
// RNG discipline: each C# block of the form
//
//	state = Random.state; Random.InitState(seed); ...; Random.state = state
//
// (PlaceRivers, and each of the two PlaceStreams calls) gets its own fresh
// URandom, which is threaded through every Random.Range call made inside
// that block (including the ones in FindRandomRiverEnd, FindStream*Point
// and RenderRivers), in source order.

// River mirrors WorldGenerator.River.
type River struct {
	P0, P1, Center              [2]float32
	WidthMin, WidthMax          float32
	CurveWidth, CurveWavelength float32
}

// riverPoint mirrors WorldGenerator.RiverPoint.
type riverPoint struct {
	p     [2]float32
	w, w2 float32
}

func newRiverPoint(p [2]float32, w float32) riverPoint {
	return riverPoint{p: p, w: w, w2: float32(float64(w) * float64(w))}
}

// riverAdd mirrors WorldGenerator.RiverAdd.
type riverAdd int

const (
	riverAddAll riverAdd = iota
	riverAddSkipDeepNorth
	riverAddOnlyDeepNorth
)

// Lakes returns the merged lake centres (WorldGenerator.GetLakes).
func (g *Generator) Lakes() [][2]float32 { return g.lakes }

// Rivers returns the lake-to-lake rivers (WorldGenerator.GetRivers).
func (g *Generator) Rivers() []River { return g.rivers }

// Streams returns the streams of the non-Deep-North pass
// (WorldGenerator.GetStreams). As in the C#, the Deep North pass's list is
// rendered into the river grid but not kept.
func (g *Generator) Streams() []River { return g.streams }

// pregenerate ports WorldGenerator.Pregenerate.
func (g *Generator) pregenerate() {
	g.riverPoints = map[[2]int32][]riverPoint{}
	g.cachedRiverGrid = [2]int32{-999999, -999999}
	g.cachedRiverPoints = nil
	g.pregenerating = true
	g.findLakes()
	g.rivers = g.placeRivers()
	g.streams = g.placeStreams(false)
	g.placeStreams(true)
	g.pregenerating = false
	g.cachedRiverPoints = nil
}

// Unity Vector2 helpers on [2]float32.

func v2Add(a, b [2]float32) [2]float32 { return [2]float32{a[0] + b[0], a[1] + b[1]} }
func v2Sub(a, b [2]float32) [2]float32 { return [2]float32{a[0] - b[0], a[1] - b[1]} }
func v2Mul(a [2]float32, d float32) [2]float32 {
	return [2]float32{a[0] * d, a[1] * d}
}

// v2Eq ports Vector2 operator ==: squared difference below kEpsilon^2.
func v2Eq(a, b [2]float32) bool {
	num := a[0] - b[0]
	num2 := a[1] - b[1]
	return num*num+num2*num2 < 9.99999944e-11
}

// v2Distance ports Vector2.Distance.
func v2Distance(a, b [2]float32) float32 {
	num := a[0] - b[0]
	num2 := a[1] - b[1]
	return float32(math.Sqrt(float64(num*num + num2*num2)))
}

// v2Normalized ports Vector2.normalized (Normalize: divide by magnitude if
// it exceeds 1e-5, else zero).
func v2Normalized(a [2]float32) [2]float32 {
	m := vector2Magnitude(a[0], a[1])
	if m > 1e-05 {
		return [2]float32{a[0] / m, a[1] / m}
	}
	return [2]float32{}
}

// fAdd is the C# (float)((double)a + (double)b).
func fAdd(a, b float64) float32 { return float32(a + b) }

// findLakes ports WorldGenerator.FindLakes (~277).
func (g *Generator) findLakes() {
	var list [][2]float32
	for num := float32(-10000); num <= 10000; num = fAdd(float64(num), 128.0) {
		for num2 := float32(-10000); num2 <= 10000; num2 = fAdd(float64(num2), 128.0) {
			if !(vector2Magnitude(num2, num) > 10000) && g.BaseHeight(num2, num) < 0.05 {
				list = append(list, [2]float32{num2, num})
			}
		}
	}
	g.lakes = mergePoints(list, 800)
}

// mergePoints ports WorldGenerator.MergePoints (~294). It consumes points.
func mergePoints(points [][2]float32, rng float32) [][2]float32 {
	var list [][2]float32
	for len(points) > 0 {
		vector := points[0]
		points = append(points[:0], points[1:]...) // RemoveAt(0)
		for len(points) > 0 {
			num := findClosest(points, vector, rng)
			if num == -1 {
				break
			}
			vector = v2Mul(v2Add(vector, points[num]), 0.5)
			points[num] = points[len(points)-1]
			points = points[:len(points)-1]
		}
		list = append(list, vector)
	}
	return list
}

// findClosest ports WorldGenerator.FindClosest (~317).
func findClosest(points [][2]float32, p [2]float32, maxDistance float32) int {
	result := -1
	num := float32(99999)
	for i := range points {
		if !v2Eq(points[i], p) {
			num2 := v2Distance(p, points[i])
			if num2 < maxDistance && num2 < num {
				result = i
				num = num2
			}
		}
	}
	return result
}

// placeStreams ports WorldGenerator.PlaceStreams (~336).
func (g *Generator) placeStreams(isDN bool) []River {
	r := NewURandom(g.streamSeed)
	var list []River
	for i := 0; i < 3000; i++ {
		p, _, ok := g.findStreamStartPoint(r, 100, 26, 31, !isDN)
		if !ok {
			continue
		}
		end, ok := g.findStreamEndPoint(r, 100, 36, 44, p, 80, 200, !isDN)
		if !ok {
			continue
		}
		center := v2Mul(v2Add(p, end), 0.5)
		h := g.pregenerationHeight(center[0], center[1], !isDN)
		if !(h < 26) && !(h > 44) {
			river := River{P0: p, P1: end, Center: center, WidthMax: 20, WidthMin: 20}
			num2 := v2Distance(river.P0, river.P1)
			river.CurveWidth = float32(float64(num2) / 15.0)
			river.CurveWavelength = float32(float64(num2) / 20.0)
			list = append(list, river)
		}
	}
	rule := riverAddSkipDeepNorth
	if isDN {
		rule = riverAddOnlyDeepNorth
	}
	g.renderRivers(r, list, rule)
	return list
}

// findStreamEndPoint ports WorldGenerator.FindStreamEndPoint (~370).
func (g *Generator) findStreamEndPoint(r *URandom, iterations int, minHeight, maxHeight float32, start [2]float32, minLength, maxLength float32, riverPreGen bool) ([2]float32, bool) {
	num := float32((float64(maxLength) - float64(minLength)) / float64(iterations))
	num2 := maxLength
	for i := 0; i < iterations; i++ {
		num2 = float32(float64(num2) - float64(num))
		f := r.RangeFloat(0, math.Pi*2)
		dir := [2]float32{float32(math.Sin(float64(f))), float32(math.Cos(float64(f)))}
		vector := v2Add(start, v2Mul(dir, num2))
		h := g.pregenerationHeight(vector[0], vector[1], riverPreGen)
		if h > minHeight && h < maxHeight {
			return vector, true
		}
	}
	return [2]float32{}, false
}

// findStreamStartPoint ports WorldGenerator.FindStreamStartPoint (~389).
func (g *Generator) findStreamStartPoint(r *URandom, iterations int, minHeight, maxHeight float32, riverPreGen bool) ([2]float32, float32, bool) {
	for i := 0; i < iterations; i++ {
		num := r.RangeFloat(-10000, 10000)
		num2 := r.RangeFloat(-10000, 10000)
		h := g.pregenerationHeight(num, num2, riverPreGen)
		if h > minHeight && h < maxHeight {
			return [2]float32{num, num2}, h, true
		}
	}
	return [2]float32{}, 0, false
}

// placeRivers ports WorldGenerator.PlaceRivers (~407).
func (g *Generator) placeRivers() []River {
	r := NewURandom(g.riverSeed)
	var list []River
	list2 := append([][2]float32(nil), g.lakes...)
	for len(list2) > 1 {
		vector := list2[0]
		num := g.findRandomRiverEnd(r, list, g.lakes, vector, 2000, 0.4, 128)
		if num == -1 && !haveRiverAt(list, vector) {
			num = g.findRandomRiverEnd(r, list, g.lakes, vector, 5000, 0.4, 128)
		}
		if num != -1 {
			river := River{P0: vector, P1: g.lakes[num]}
			river.Center = v2Mul(v2Add(river.P0, river.P1), 0.5)
			river.WidthMax = r.RangeFloat(60, 100)
			river.WidthMin = r.RangeFloat(60, river.WidthMax)
			num2 := v2Distance(river.P0, river.P1)
			river.CurveWidth = float32(float64(num2) / 15.0)
			river.CurveWavelength = float32(float64(num2) / 20.0)
			list = append(list, river)
		} else {
			list2 = list2[1:]
		}
	}
	g.renderRivers(r, list, riverAddAll)
	return list
}

// findClosestRiverEnd ports WorldGenerator.FindClosestRiverEnd (~442).
// Nothing in WorldGenerator calls it; it is ported for completeness.
func (g *Generator) findClosestRiverEnd(rivers []River, points [][2]float32, p [2]float32, maxDistance, heightLimit, checkStep float32) int {
	result := -1
	num := float32(99999)
	for i := range points {
		if !v2Eq(points[i], p) {
			num2 := v2Distance(p, points[i])
			if num2 < maxDistance && num2 < num && !haveRiverBetween(rivers, p, points[i]) && g.isRiverAllowed(p, points[i], checkStep, heightLimit) {
				result = i
				num = num2
			}
		}
	}
	return result
}

// findRandomRiverEnd ports WorldGenerator.FindRandomRiverEnd (~461).
func (g *Generator) findRandomRiverEnd(r *URandom, rivers []River, points [][2]float32, p [2]float32, maxDistance, heightLimit, checkStep float32) int {
	var list []int
	for i := range points {
		if !v2Eq(points[i], p) && v2Distance(p, points[i]) < maxDistance && !haveRiverBetween(rivers, p, points[i]) && g.isRiverAllowed(p, points[i], checkStep, heightLimit) {
			list = append(list, i)
		}
	}
	if len(list) == 0 {
		return -1
	}
	return list[r.RangeInt(0, int32(len(list)))]
}

// haveRiverAt ports WorldGenerator.HaveRiver(rivers, p0) (~477).
func haveRiverAt(rivers []River, p0 [2]float32) bool {
	for i := range rivers {
		if v2Eq(rivers[i].P0, p0) || v2Eq(rivers[i].P1, p0) {
			return true
		}
	}
	return false
}

// haveRiverBetween ports WorldGenerator.HaveRiver(rivers, p0, p1) (~489).
func haveRiverBetween(rivers []River, p0, p1 [2]float32) bool {
	for i := range rivers {
		rv := &rivers[i]
		if (v2Eq(rv.P0, p0) && v2Eq(rv.P1, p1)) || (v2Eq(rv.P0, p1) && v2Eq(rv.P1, p0)) {
			return true
		}
	}
	return false
}

// isRiverAllowed ports WorldGenerator.IsRiverAllowed (~501).
func (g *Generator) isRiverAllowed(p0, p1 [2]float32, step, heightLimit float32) bool {
	num := v2Distance(p0, p1)
	normalized := v2Normalized(v2Sub(p1, p0))
	flag := true
	for num2 := step; num2 <= float32(float64(num)-float64(step)); num2 = fAdd(float64(num2), float64(step)) {
		vector := v2Add(p0, v2Mul(normalized, num2))
		baseHeight := g.BaseHeight(vector[0], vector[1])
		if baseHeight > heightLimit {
			return false
		}
		if baseHeight > 0.05 {
			flag = false
		}
	}
	return !flag
}

// renderRivers ports WorldGenerator.RenderRivers (~537). The C# collects
// points into a local Dictionary<Vector2i, List<RiverPoint>> and then merges
// each key into m_riverPoints as old array + new points. The only order that
// affects results is the per-key point order (GetWeight sums in float32),
// which is insertion order in both the C# and here; the order in which keys
// are merged does not matter because each key is merged independently.
func (g *Generator) renderRivers(r *URandom, rivers []River, addRule riverAdd) {
	local := map[[2]int32][]riverPoint{}
	var keys [][2]int32
	for i := range rivers {
		river := &rivers[i]
		if addRule != riverAddAll {
			flag := isDeepnorth(river.P0[0], river.P0[1])
			if (flag && addRule == riverAddSkipDeepNorth) || (!flag && addRule == riverAddOnlyDeepNorth) {
				continue
			}
		}
		num := float32(float64(river.WidthMin) / 8.0)
		normalized := v2Normalized(v2Sub(river.P1, river.P0))
		vector := [2]float32{0 - normalized[1], normalized[0]}
		num2 := v2Distance(river.P0, river.P1)
		for num3 := float32(0); num3 <= num2; num3 = fAdd(float64(num3), float64(num)) {
			num4 := float32(float64(num3) / float64(river.CurveWavelength))
			num5 := float32(math.Sin(float64(num4)) * math.Sin(float64(num4)*0.634119987487793) * math.Sin(float64(num4)*0.3341200053691864) * float64(river.CurveWidth))
			rr := r.RangeFloat(river.WidthMin, river.WidthMax)
			p := v2Add(v2Add(river.P0, v2Mul(normalized, num3)), v2Mul(vector, num5))
			keys = addRiverPoint(local, keys, p, rr)
		}
	}
	for _, k := range keys {
		pts := local[k]
		if old, ok := g.riverPoints[k]; ok {
			merged := make([]riverPoint, 0, len(old)+len(pts))
			merged = append(merged, old...)
			g.riverPoints[k] = append(merged, pts...)
		} else {
			g.riverPoints[k] = pts
		}
	}
}

// addRiverPoint ports both WorldGenerator.AddRiverPoint overloads (~581,
// ~598). keys records first-insertion order of the local dictionary.
func addRiverPoint(riverPoints map[[2]int32][]riverPoint, keys [][2]int32, p [2]float32, r float32) [][2]int32 {
	riverGrid := riverGridOf(p[0], p[1])
	num := int32(math.Ceil(float64(float32(float64(r) / 64.0))))
	for i := riverGrid[1] - num; i <= riverGrid[1]+num; i++ {
		for j := riverGrid[0] - num; j <= riverGrid[0]+num; j++ {
			grid := [2]int32{j, i}
			if insideRiverGrid(grid, p, r) {
				v, ok := riverPoints[grid]
				if !ok {
					keys = append(keys, grid)
				}
				riverPoints[grid] = append(v, newRiverPoint(p, r))
			}
		}
	}
	return keys
}

// insideRiverGrid ports WorldGenerator.InsideRiverGrid (~610).
func insideRiverGrid(grid [2]int32, p [2]float32, r float32) bool {
	vector := [2]float32{float32(float64(grid[0]) * 64.0), float32(float64(grid[1]) * 64.0)}
	vector2 := v2Sub(p, vector)
	lim := float32(float64(r) + 32.0)
	if absF32(vector2[0]) < lim {
		return absF32(vector2[1]) < lim
	}
	return false
}

// riverGridOf ports WorldGenerator.GetRiverGrid (~621).
func riverGridOf(wx, wy float32) [2]int32 {
	x := int32(math.Floor(float64(float32((float64(wx) + 32.0) / 64.0))))
	y := int32(math.Floor(float64(float32((float64(wy) + 32.0) / 64.0))))
	return [2]int32{x, y}
}

// riverWeight ports WorldGenerator.GetRiverWeight (~628). The C# keeps a
// one-entry cache (m_cachedRiverGrid/m_cachedRiverPoints) that RenderRivers
// does not invalidate, so during pre-generation a query can see a grid
// cell's points as they were before the latest RenderRivers. That stale
// view can change stream placement, so it is reproduced while
// pre-generating (single-threaded). Afterwards the cache is bypassed: the
// map is read-only, so Height is safe for concurrent use and allocation-free.
func (g *Generator) riverWeight(wx, wy float32) (weight, width float32) {
	riverGrid := riverGridOf(wx, wy)
	if !g.pregenerating {
		if pts, ok := g.riverPoints[riverGrid]; ok {
			return riverPointsWeight(pts, wx, wy)
		}
		return 0, 0
	}
	if riverGrid == g.cachedRiverGrid {
		if g.cachedRiverPoints != nil {
			return riverPointsWeight(g.cachedRiverPoints, wx, wy)
		}
		return 0, 0
	}
	if pts, ok := g.riverPoints[riverGrid]; ok {
		weight, width = riverPointsWeight(pts, wx, wy)
		g.cachedRiverGrid = riverGrid
		g.cachedRiverPoints = pts
		return weight, width
	}
	g.cachedRiverGrid = riverGrid
	g.cachedRiverPoints = nil
	return 0, 0
}

// riverPointsWeight ports WorldGenerator.GetWeight (~670).
func riverPointsWeight(points []riverPoint, wx, wy float32) (weight, width float32) {
	var num, num2 float32
	for i := range points {
		rp := &points[i]
		dx := rp.p[0] - wx
		dy := rp.p[1] - wy
		num3 := dx*dx + dy*dy
		if num3 < rp.w2 {
			num4 := float32(math.Sqrt(float64(num3)))
			num5 := float32(1.0 - float64(num4)/float64(rp.w))
			if num5 > weight {
				weight = num5
			}
			num = float32(float64(num) + float64(rp.w)*float64(num5))
			num2 = float32(float64(num2) + float64(num5))
		}
	}
	if num2 > 0 {
		width = float32(float64(num) / float64(num2))
	}
	return weight, width
}

// addRivers ports WorldGenerator.AddRivers (~937).
func (g *Generator) addRivers(wx, wy, h float32) float32 {
	weight, width := g.riverWeight(wx, wy)
	if weight <= 0 {
		return h
	}
	t := dLerpStep(20, 60, width)
	num := dLerp(0.14, 0.12, t)
	num2 := dLerp(0.139, 0.128, t)
	if h > num {
		h = dLerp(h, num, weight)
	}
	if h > num2 {
		t2 := dLerpStep(0.85, 1, weight)
		h = dLerp(h, num2, t2)
	}
	return h
}
