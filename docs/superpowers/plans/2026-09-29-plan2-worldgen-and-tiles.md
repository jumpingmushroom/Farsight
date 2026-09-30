# Farsight Plan 2: World Generation and Map Tiles Implementation Plan

> **Placeholders:** player names, platform IDs, join codes, IP addresses other than the public join address, and world seeds in this document are invented stand-ins. The real values were removed before the repository was published. Code snippets that assert specific seed names reflect the original private tests, which now check seed length and hash instead.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Generate a Valheim world's terrain (biome and height, including lakes and rivers) from its seed, matching the game, and render it to a zoomable PNG tile pyramid.

**Architecture:** `internal/worldgen` is a faithful Go port of Valheim's `WorldGenerator`, plus the two native Unity pieces it relies on (`UnityEngine.Random` and `Mathf.PerlinNoise`) and the managed `FastNoise` subset used by the Ashlands. Correctness is proven against real saves: every location instance must land in its expected biome, and the terrain height must match the `y` of naturally placed vegetation. `internal/tiles` samples a height source into z5 tiles (32×32 tiles of 256 px) and builds z4–z0 by downsampling. It is resumable and reports progress. A dev CLI renders a world to disk.

**Tech Stack:** Go, standard library only (`image/png`).

**Spec:** `docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md`. This plan covers the `worldgen` and `tiles` units. Plan 4 uses `tiles.Render` from the central app.

## Global Constraints

- Go module `github.com/jumpingmushroom/farsight`, standard library only.
- **Port literally.** A world-generation function is translated line by line from the decompiled C#, keeping every `float`/`double` cast. C# `float` is Go `float32` and C# `double` is Go `float64`. `(float)expr` becomes `float32(expr)` and `(double)x` becomes `float64(x)`. Don't simplify or reorder floating-point arithmetic, and don't "fix" apparent quirks. Bit-level parity is what makes the oracle tests pass.
- **Unity natives:**
  - `Mathf.Sin/Cos/Sqrt/Abs/Atan2` becomes `float32(math.X(float64(v)))`.
  - `Mathf.FloorToInt`/`CeilToInt` becomes `int(math.Floor/Ceil(float64(v)))`.
  - `Mathf.Clamp01` clamps to [0,1] in float32.
  - `Vector2.magnitude` is `float32(math.Sqrt(float64(x*x + y*y)))`, with the squares done in float32.
  - `Mathf.PerlinNoise` is `worldgen.Perlin`, which works in float32.
  - `UnityEngine.Random` is `worldgen.URandom`.
- **Reference source is never committed.** `hack/decompile.sh` produces `reference/` (gitignored) from the live server's assemblies. Ports cite `reference/valheim/WorldGenerator.cs` line ranges. The Go code carries a one-line comment naming the C# function it ports.
- World constants: radius 10500 m, water level 30 m, zone size 64 m.
- Real-world golden data comes from Plan 1: `testdata-golden/chunked/MuleVikings` (seed `FjordSeed`, worldGenVersion 2) and `testdata-golden/legacy/Mulennials` (seed `Qm4RtX8vLc`). Tests that need it call `t.Skip` when it's absent (`hack/pull-golden.sh` fetches it).
- Tiles: 256 px, zoom 0–5, where z5 is 32×32 tiles (≈2.56 m/px). Files go at `{dir}/{z}/{x}/{y}.png`, with y=0 at the north edge. A `complete` marker file is written last.
- Commits: `feat(pkg): …`, and every message ends with a blank line and then `Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T`.

## File Structure

```
.gitignore                         # + reference/
hack/decompile.sh                  # server assemblies -> reference/{valheim,utils}/*.cs
internal/worldgen/urandom.go       # UnityEngine.Random (xorshift128)
internal/worldgen/perlin.go        # Mathf.PerlinNoise
internal/worldgen/rand_test.go
internal/worldgen/biome.go         # Biome enum + colours-free names
internal/worldgen/generator.go     # Generator: New, offsets, VersionSetup, WorldAngle, BaseHeight, Biome
internal/worldgen/generator_test.go
internal/worldgen/oracle_test.go   # golden-save oracles (biome + height)
internal/worldgen/fastnoise.go     # FastNoise subset (cellular + simplex fractal) for Ashlands
internal/worldgen/heights.go       # GetBiomeHeight and per-biome height functions, Height()
internal/worldgen/rivers.go        # lakes, rivers, streams, AddRivers
internal/tiles/tiles.go            # Render, Options, palette, downsampling
internal/tiles/tiles_test.go
cmd/farsight-render/main.go        # dev CLI: seed -> tiles + preview.png
```

---

### Task 1: Reference source, Unity random and Perlin noise

**Files:**
- Create: `hack/decompile.sh`, `internal/worldgen/urandom.go`, `internal/worldgen/perlin.go`, `internal/worldgen/rand_test.go`
- Modify: `.gitignore` (append `reference/`)

**Interfaces:**
- Produces:
  - `worldgen.URandom` with `NewURandom(seed int32) *URandom`, `(r) Next() uint32`, `(r) Value() float32`, `(r) RangeInt(min, max int32) int32`, `(r) RangeFloat(min, max float32) float32`.
  - `worldgen.Perlin(x, y float32) float32`.
- Unity facts:
  - `InitState(seed)`: `s0=uint32(seed)`, then `s1=1812433253*s0+1`, `s2=1812433253*s1+1`, `s3=1812433253*s2+1`.
  - `Next`: `t=s0^(s0<<11)`, shift the state down one slot, then `s3=s3^(s3>>19)^t^(t>>8)`.
  - `Value = float32(Next()&0x7FFFFF) / float32(0x7FFFFF)`.
  - `RangeFloat(min,max) = t*(min-max)+max` with `t=Value()`. This really is `t*(min-max)+max`, not `min+t*(max-min)`.
  - `RangeInt(min,max)`: with 64-bit maths, `min + int64(Next()) % (max-min)` when max>min; `min - r % (max-min)` when max<min; `min` when they are equal (a draw is still consumed).
- Perlin facts: Ken Perlin's 2002 improved noise at z=0 with the standard permutation table, computed in float32. The inputs go through `abs()` first, and the result is `(raw+0.69)/1.483`.
- The test vectors for seed 1234 and for `Dedbtjdcv` were captured from real Unity and published by the vegvisr project. The `Dedbtjdcv` offsets were independently reproduced by the Farsight spike. They are facts about Unity's RNG, and no code is copied.

- [ ] **Step 1: Write the decompile script**

`hack/decompile.sh`

```bash
#!/usr/bin/env bash
# Decompiles the live Valheim server's assemblies into reference/ (gitignored)
# so world-generation ports can cite exact C# line ranges. Read-only on the cluster.
set -euo pipefail
: "${KUBECONFIG:=$HOME/.kube/cloudcluster.yaml}"; export KUBECONFIG
ROOT=$(cd "$(dirname "$0")/.." && pwd)
TOOLS=$HOME/.local/farsight-tools
export DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 DOTNET_ROLL_FORWARD=Major DOTNET_CLI_TELEMETRY_OPTOUT=1
export DOTNET_ROOT=$TOOLS/dotnet PATH=$TOOLS/dotnet:$TOOLS/bin:$PATH
if ! command -v ilspycmd >/dev/null; then
  mkdir -p "$TOOLS"
  curl -sSL https://dot.net/v1/dotnet-install.sh -o "$TOOLS/dotnet-install.sh"
  bash "$TOOLS/dotnet-install.sh" --channel 8.0 --install-dir "$TOOLS/dotnet" >/dev/null
  dotnet tool install ilspycmd --tool-path "$TOOLS/bin" --version 8.2.0.7535 >/dev/null
fi
DLL=$(mktemp -d)
kubectl -n gameservers exec deploy/mulevikings-valheim -c valheim -- \
  tar cf - -C /opt/valheim/server/valheim_server_Data/Managed . | tar xf - -C "$DLL"
mkdir -p "$ROOT/reference/valheim" "$ROOT/reference/utils"
ilspycmd -p -o "$ROOT/reference/valheim" -r "$DLL" "$DLL/assembly_valheim.dll" >/dev/null
ilspycmd -p -o "$ROOT/reference/utils" -r "$DLL" "$DLL/assembly_utils.dll" >/dev/null
rm -rf "$DLL"
ls "$ROOT/reference/valheim/WorldGenerator.cs" "$ROOT/reference/utils/FastNoise.cs" "$ROOT/reference/utils/DUtils.cs"
```

Run: `chmod +x hack/decompile.sh && echo 'reference/' >> .gitignore && hack/decompile.sh`
Expected: the three paths are printed. Then `git status --short` must not list `reference/`.

- [ ] **Step 2: Write the failing tests** — `internal/worldgen/rand_test.go`

```go
package worldgen

import (
	"math"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
)

func TestURandomSeedAndOutputs(t *testing.T) {
	r := NewURandom(1234)
	if r.s != [4]uint32{1234, 3159640283, 3392860520, 3460949513} {
		t.Fatalf("state = %v", r.s)
	}
	want := []uint32{3463400838, 3496203776, 3452947669, 1278673611, 4169168310}
	for i, w := range want {
		if got := r.Next(); got != w {
			t.Fatalf("Next #%d = %d, want %d", i, got, w)
		}
	}
}

func TestURandomRangeInt(t *testing.T) {
	r := NewURandom(1234)
	for i, w := range []int32{1315917191, 1348720129, 1305464022, 1278673611, 2021684663} {
		if got := r.RangeInt(0, math.MaxInt32); got != w {
			t.Fatalf("RangeInt #%d = %d, want %d", i, got, w)
		}
	}
}

func TestWorldOffsetsFromSeed(t *testing.T) {
	cases := []struct {
		seed          string
		o             [4]int32
		river, stream int32
		o4            int32
	}{
		{"Dedbtjdcv", [4]int32{-8087, 9698, -4921, -8635}, 1741748534, -2141061776, -116},
		{"FjordSeed", [4]int32{3119, 4451, 2181, -5622}, 1699072326, 1064140420, -5640},
		{"Qm4RtX8vLc", [4]int32{-5294, -2350, 1124, -454}, 276112436, 1036682745, 1866},
	}
	for _, c := range cases {
		r := NewURandom(names.StableHash(c.seed))
		var o [4]int32
		for i := range o {
			o[i] = r.RangeInt(-10000, 10000)
		}
		river := r.RangeInt(math.MinInt32, math.MaxInt32)
		stream := r.RangeInt(math.MinInt32, math.MaxInt32)
		o4 := r.RangeInt(-10000, 10000)
		if o != c.o || river != c.river || stream != c.stream || o4 != c.o4 {
			t.Errorf("%s: got %v %d %d %d", c.seed, o, river, stream, o4)
		}
	}
}

func TestRangeFloatIsUnityFormula(t *testing.T) {
	a, b := NewURandom(99), NewURandom(99)
	v := a.Value()
	if got, want := b.RangeFloat(60, 100), v*(60-100)+100; got != want {
		t.Fatalf("RangeFloat = %v, want %v", got, want)
	}
}

func TestPerlinMirroredAndInRange(t *testing.T) {
	p := Perlin(0.4, 0.5)
	if Perlin(-0.4, 0.5) != p || Perlin(0.4, -0.5) != p || Perlin(-0.4, -0.5) != p {
		t.Fatal("Perlin must mirror across both axes (abs inputs)")
	}
	if got := Perlin(0, 0); math.Abs(float64(got)-0.69/1.483) > 1e-6 {
		t.Fatalf("Perlin(0,0) = %v, want ~%v (raw noise is 0 at lattice points)", got, 0.69/1.483)
	}
	for x := float32(-50); x < 50; x += 0.37 {
		for y := float32(-50); y < 50; y += 0.53 {
			if v := Perlin(x, y); v < -0.1 || v > 1.1 {
				t.Fatalf("Perlin(%v,%v) = %v out of Unity's approximate range", x, y, v)
			}
		}
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/worldgen/`
Expected: FAIL with `undefined: NewURandom`.

- [ ] **Step 4: Implement** — `internal/worldgen/urandom.go`

```go
// Package worldgen ports Valheim's WorldGenerator (biomes, heights, lakes,
// rivers) and the Unity natives it depends on.
package worldgen

// URandom reproduces UnityEngine.Random (xorshift128) bit for bit.
type URandom struct{ s [4]uint32 }

func NewURandom(seed int32) *URandom {
	s0 := uint32(seed)
	s1 := 1812433253*s0 + 1
	s2 := 1812433253*s1 + 1
	s3 := 1812433253*s2 + 1
	return &URandom{s: [4]uint32{s0, s1, s2, s3}}
}

func (r *URandom) Next() uint32 {
	t := r.s[0] ^ (r.s[0] << 11)
	r.s[0], r.s[1], r.s[2] = r.s[1], r.s[2], r.s[3]
	r.s[3] = r.s[3] ^ (r.s[3] >> 19) ^ t ^ (t >> 8)
	return r.s[3]
}

// Value is Random.value: the low 23 bits over 2^23-1.
func (r *URandom) Value() float32 {
	return float32(r.Next()&0x7FFFFF) / float32(0x7FFFFF)
}

// RangeFloat is Random.Range(float, float). Unity computes t*(min-max)+max.
func (r *URandom) RangeFloat(min, max float32) float32 {
	t := r.Value()
	return t*(min-max) + max
}

// RangeInt is Random.Range(int, int), max-exclusive, with Unity's plain
// modulo (no rejection sampling).
func (r *URandom) RangeInt(min, max int32) int32 {
	v := int64(r.Next())
	lo, hi := int64(min), int64(max)
	switch {
	case hi > lo:
		return int32(lo + v%(hi-lo))
	case hi < lo:
		return int32(lo - v%(hi-lo))
	default:
		return min
	}
}
```

`internal/worldgen/perlin.go`

```go
package worldgen

import "math"

var perm = [256]int32{
	151, 160, 137, 91, 90, 15, 131, 13, 201, 95, 96, 53, 194, 233, 7, 225, 140, 36, 103, 30, 69, 142,
	8, 99, 37, 240, 21, 10, 23, 190, 6, 148, 247, 120, 234, 75, 0, 26, 197, 62, 94, 252, 219, 203,
	117, 35, 11, 32, 57, 177, 33, 88, 237, 149, 56, 87, 174, 20, 125, 136, 171, 168, 68, 175, 74, 165,
	71, 134, 139, 48, 27, 166, 77, 146, 158, 231, 83, 111, 229, 122, 60, 211, 133, 230, 220, 105, 92,
	41, 55, 46, 245, 40, 244, 102, 143, 54, 65, 25, 63, 161, 1, 216, 80, 73, 209, 76, 132, 187, 208,
	89, 18, 169, 200, 196, 135, 130, 116, 188, 159, 86, 164, 100, 109, 198, 173, 186, 3, 64, 52, 217,
	226, 250, 124, 123, 5, 202, 38, 147, 118, 126, 255, 82, 85, 212, 207, 206, 59, 227, 47, 16, 58,
	17, 182, 189, 28, 42, 223, 183, 170, 213, 119, 248, 152, 2, 44, 154, 163, 70, 221, 153, 101, 155,
	167, 43, 172, 9, 129, 22, 39, 253, 19, 98, 108, 110, 79, 113, 224, 232, 178, 185, 112, 104, 218,
	246, 97, 228, 251, 34, 242, 193, 238, 210, 144, 12, 191, 179, 162, 241, 81, 51, 145, 235, 249, 14,
	239, 107, 49, 192, 214, 31, 181, 199, 106, 157, 184, 84, 204, 176, 115, 121, 50, 45, 127, 4, 150,
	254, 138, 236, 205, 93, 222, 114, 67, 29, 24, 72, 243, 141, 128, 195, 78, 66, 215, 61, 156, 180,
}

func p(i int32) int32 { return perm[i&255] }

func fade(t float32) float32 { return t * t * t * (t*(t*6-15) + 10) }

func lerp(t, a, b float32) float32 { return a + t*(b-a) }

func grad(hash int32, x, y float32) float32 {
	h := hash & 15
	u := y
	if h < 8 {
		u = x
	}
	var v float32
	switch {
	case h < 4:
		v = y
	case h == 12 || h == 14:
		v = x
	}
	if h&1 != 0 {
		u = -u
	}
	if h&2 != 0 {
		v = -v
	}
	return u + v
}

// Perlin reproduces UnityEngine.Mathf.PerlinNoise(x, y).
func Perlin(x, y float32) float32 {
	x = float32(math.Abs(float64(x)))
	y = float32(math.Abs(float64(y)))
	xf := float32(math.Floor(float64(x)))
	yf := float32(math.Floor(float64(y)))
	xi, yi := int32(xf), int32(yf)
	x -= xf
	y -= yf
	a := p(xi) + yi
	b := p(xi+1) + yi
	aa, ba := p(p(a)), p(p(b))
	ab, bb := p(p(a+1)), p(p(b+1))
	u, v := fade(x), fade(y)
	res := lerp(v,
		lerp(u, grad(aa, x, y), grad(ba, x-1, y)),
		lerp(u, grad(ab, x, y-1), grad(bb, x-1, y-1)))
	return (res + 0.69) / 1.483
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test ./internal/worldgen/`
Expected: `ok`.

- [ ] **Step 6: Commit** (`feat(worldgen): Unity random and Perlin noise ports, decompile script`, with the trailer).

---

### Task 2: Generator, biome and base height, with the biome oracle

**Files:**
- Create: `internal/worldgen/biome.go`, `internal/worldgen/generator.go`, `internal/worldgen/generator_test.go`, `internal/worldgen/oracle_test.go`

**Interfaces:**
- Consumes: `URandom`, `Perlin`; `save.Read`, `names.Name` (tests only).
- Produces:
  - `type Biome uint16` with constants matching `Heightmap.Biome`: `Meadows=1`, `Swamp=2`, `Mountain=4`, `BlackForest=8`, `Plains=16`, `AshLands=32`, `DeepNorth=64`, `Ocean=256`, `Mistlands=512`. `(b Biome) String()` returns those names.
  - `type Generator struct` (unexported fields), `NewBase(seed int32, genVersion int32) *Generator`. This constructor does no river pre-generation; Task 5 adds `New`.
  - `(g *Generator) Biome(wx, wy float32) Biome`
  - `(g *Generator) BaseHeight(wx, wy float32) float32`
  - `worldAngle(wx, wy float32) float32`
  - The helper functions ported literally from `reference/utils/DUtils.cs` (and `Utils.cs` where it's referenced): `dLength`, `dLerp`, `dLerpStep`, `dSmoothStep`, `dClamp01`, plus the float and double overloads that `WorldGenerator` calls, each named for its C# original. Check every signature and body in the reference rather than assuming the textbook definition.
- Port sources: `reference/valheim/WorldGenerator.cs`:
  - the constructor offset draws (≈lines 205–236, without `Pregenerate`, and without the FastNoise setup, which Task 3 adds);
  - `VersionSetup` (≈245–257) and its field defaults (`maxMarshDistance=6000`, `minDarklandNoise=0.4`, `m_minMountainDistance=1000`, `ashlandsMinDistance=12000`, `ashlandsYOffset=-4000`);
  - `IsAshlands`, `IsDeepnorth`, `GetBiome(float,float,...)` using the default `oceanLevel=0.02` and `waterAlwaysOcean=false` (≈751–833);
  - `WorldAngle` (≈879);
  - `GetBaseHeight` with `menuTerrain=false` (≈884–935).

  The line numbers come from the 2026-09-29 decompile. Locate the functions by name if they have shifted.
- Offsets are C# `float` fields assigned from `Random.Range(int,int)`. Store them as `float32(int)`. `m_riverSeed` and `m_streamSeed` are `int32`.

- [ ] **Step 1: Write the failing unit tests** — `internal/worldgen/generator_test.go`

```go
package worldgen

import (
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
)

func TestNewBaseOffsets(t *testing.T) {
	g := NewBase(names.StableHash("FjordSeed"), 2)
	if g.offset0 != 3119 || g.offset1 != 4451 || g.offset2 != 2181 || g.offset3 != -5622 || g.offset4 != -5640 ||
		g.riverSeed != 1699072326 || g.streamSeed != 1064140420 {
		t.Fatalf("offsets = %+v", g)
	}
}

// Analytic landmarks only; real biome correctness is the golden oracle.
func TestBiomeLandmarks(t *testing.T) {
	g := NewBase(names.StableHash("FjordSeed"), 2)
	// |(0, -10000-4000)| = 14000 > 12000 + 100*angle: Ashlands is checked first.
	if b := g.Biome(0, -10000); b != AshLands {
		t.Errorf("far south = %s, want AshLands", b)
	}
	// Past 10490 m the base height is forced towards -2, and (10499,0) is
	// neither Ashlands nor Deep North (|(10499,±4000)| ≈ 11235 < 11900).
	if b := g.Biome(10499, 0); b != Ocean {
		t.Errorf("world edge = %s, want Ocean", b)
	}
}
```

- [ ] **Step 2: Write the failing oracle test** — `internal/worldgen/oracle_test.go`

```go
package worldgen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save"
)

// Location prefab -> the biome the game places it in. Confirmed by the
// 2026-09-29 spike (1,440/1,440 on MuleVikings).
var locationBiome = map[string]Biome{
	"Eikthyrnir": Meadows, "GDKing": BlackForest, "Bonemass": Swamp, "Dragonqueen": Mountain,
	"GoblinKing": Plains, "Mistlands_DvergrBossEntrance1": Mistlands, "FaderLocation": AshLands,
	"DN_Bossroom": DeepNorth, "Vendor_BlackForest": BlackForest,
	"Crypt2": BlackForest, "Crypt3": BlackForest, "Crypt4": BlackForest, "TrollCave02": BlackForest,
	"BearCave": BlackForest, "SunkenCrypt4": Swamp, "GoblinCamp2": Plains, "MountainCave02": Mountain,
	"DN_hut01": DeepNorth, "Runestone_DeepNorth": DeepNorth,
}

func goldenWorld(t *testing.T, sub, name string) (*save.World, []save.ZDO) {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata-golden", sub)
	if _, err := os.Stat(dir); err != nil {
		t.Skip("golden data missing; run hack/pull-golden.sh")
	}
	var keep []save.ZDO
	w, err := save.Read(dir, name, func(z *save.ZDO) {
		if _, ok := vegetationGround[names.Name(z.Prefab)]; ok && z.Pos[1] != 0 {
			keep = append(keep, save.ZDO{Pos: z.Pos, Prefab: z.Prefab})
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return w, keep
}

// Naturally placed vegetation whose position is the generated ground height.
// Used by the height oracle (Task 4); declared here so goldenWorld can filter.
var vegetationGround = map[string]bool{
	"Beech1": true, "Pinetree_01": true, "FirTree": true, "SwampTree1": true, "Birch1": true,
	"Oak1": true, "Bush01": true, "shrub_2": true, "Pickable_Stone": true, "Pickable_Branch": true,
}

func biomeOracle(t *testing.T, sub, name string, minPct float64) {
	w, _ := goldenWorld(t, sub, name)
	g := NewBase(w.Meta.Seed, w.Meta.GenVersion)
	total, ok := 0, 0
	miss := map[string]int{}
	for _, l := range w.Locations {
		want, known := locationBiome[names.Name(l.Hash)]
		if !known {
			continue
		}
		total++
		if got := g.Biome(l.Pos[0], l.Pos[2]); got == want {
			ok++
		} else {
			miss[names.Name(l.Hash)+"->"+got.String()]++
		}
	}
	pct := 100 * float64(ok) / float64(total)
	t.Logf("%s: %d/%d locations in expected biome (%.2f%%), misses %v", name, ok, total, pct, miss)
	if total < 500 || pct < minPct {
		t.Fatalf("biome oracle failed: %.2f%% < %.2f%%", pct, minPct)
	}
}

func TestBiomeOracleMuleVikings(t *testing.T) { biomeOracle(t, "chunked", "MuleVikings", 100) }

// Mulennials was generated in 2023, before the Ashlands and Deep North rules
// existed, so far-south and far-north locations may disagree with today's
// GetBiome. The inner world must still match.
func TestBiomeOracleMulennials(t *testing.T) { biomeOracle(t, "legacy", "Mulennials", 97) }
```

- [ ] **Step 3: Run to verify they fail**

Run: `go test ./internal/worldgen/`
Expected: FAIL with `undefined: NewBase`.

- [ ] **Step 4: Implement `biome.go`**

```go
package worldgen

type Biome uint16

const (
	Meadows     Biome = 1
	Swamp       Biome = 2
	Mountain    Biome = 4
	BlackForest Biome = 8
	Plains      Biome = 16
	AshLands    Biome = 32
	DeepNorth   Biome = 64
	Ocean       Biome = 256
	Mistlands   Biome = 512
)

func (b Biome) String() string {
	switch b {
	case Meadows:
		return "Meadows"
	case Swamp:
		return "Swamp"
	case Mountain:
		return "Mountain"
	case BlackForest:
		return "BlackForest"
	case Plains:
		return "Plains"
	case AshLands:
		return "AshLands"
	case DeepNorth:
		return "DeepNorth"
	case Ocean:
		return "Ocean"
	case Mistlands:
		return "Mistlands"
	}
	return "None"
}
```

- [ ] **Step 5: Implement `generator.go`** by porting the functions listed under Interfaces, literally, following the Global Constraints' casting rules. The struct holds `offset0..offset4 float32`, `riverSeed`, `streamSeed int32`, and `maxMarshDistance`, `minDarklandNoise`, `minMountainDistance float32`, with the defaults above changed by `versionSetup(genVersion)`. `NewBase` draws, in order: `offset0..3 = float32(r.RangeInt(-10000,10000))`, then `riverSeed = r.RangeInt(MinInt32, MaxInt32)`, then `streamSeed`, then `offset4`. `DUtils.PerlinNoise(double x, double y)` is `Perlin(float32(x), float32(y))`.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/worldgen/ -run 'Offsets|Landmarks|BiomeOracle' -v`
Expected: PASS, with the oracle log showing MuleVikings at `1642/1642`-ish (100.00%) and Mulennials ≥ 97%. If MuleVikings is below 100%, the port has a cast or overload wrong. Diff it against the reference; don't lower the threshold. Record both log lines in the report.

- [ ] **Step 7: Commit** (`feat(worldgen): generator, biome and base height ported from WorldGenerator`, with the trailer).

---

### Task 3: FastNoise subset for the Ashlands

**Files:**
- Create: `internal/worldgen/fastnoise.go`, plus a test in `internal/worldgen/fastnoise_test.go`
- Modify: `internal/worldgen/generator.go` (build the noise generator in `NewBase`, as the C# constructor does)

**Interfaces:**
- Produces: an unexported `fastNoise` type with `newFastNoise(seed int32) *fastNoise`, `setSeed(int32)`, `getCellular(x, y float64) float64` (2D, Euclidean distance, `Distance` return type, the default cellular jitter), and `getSimplexFractal(x, y float64) float64` (2D, the default FBM fractal with `SetFractalOctaves(2)` and the defaults for everything else).
- Port source: `reference/utils/FastNoise.cs`. Port only what these two calls reach: the constructor defaults, seed handling, the hash/grad tables, `SingleCellular` (2D), `SingleSimplex` (2D), the FBM fractal loop, and the lookup tables they use. Keep the file's `double`/`float` types exactly. FastNoise is Jordan Peck's MIT-licensed library; keep its copyright line in a comment at the top of `fastnoise.go`.
- The generator setup mirrors the C# constructor (≈lines 214–222 of `WorldGenerator.cs`): `new FastNoise(seed)`, `Cellular`, `Euclidean`, `Distance`, `SetFractalOctaves(2)`, then `SetSeed(0)`. The seed ends up 0.

- [ ] **Step 1: Write the failing test** — `internal/worldgen/fastnoise_test.go`

```go
package worldgen

import "testing"

func TestFastNoiseDeterministicAndBounded(t *testing.T) {
	a, b := newFastNoise(0), newFastNoise(0)
	for i := 0; i < 1000; i++ {
		x, y := float64(i)*1.37-500, float64(i)*0.91+200
		c1, c2 := a.getCellular(x, y), b.getCellular(x, y)
		s := a.getSimplexFractal(x, y)
		if c1 != c2 {
			t.Fatal("cellular noise must be deterministic")
		}
		if c1 < -1.5 || c1 > 1.5 || s < -1.5 || s > 1.5 {
			t.Fatalf("noise out of range at %v,%v: cellular %v simplex %v", x, y, c1, s)
		}
	}
	if a.getCellular(10.5, 3.25) == newFastNoise(1).getCellular(10.5, 3.25) {
		t.Error("different seeds should give different cellular noise")
	}
}
```

The real correctness check for FastNoise is the Ashlands part of the Task 4 height oracle. Task 4 reports the AshLands median error separately.

- [ ] **Step 2: Run to verify it fails**, then port, then run `go test ./internal/worldgen/ -run FastNoise -v`. Expected: PASS.

- [ ] **Step 3: Commit** (`feat(worldgen): FastNoise subset (cellular, simplex fractal) for Ashlands`, with the trailer).

---

### Task 4: Per-biome heights and the height oracle

**Files:**
- Create: `internal/worldgen/heights.go`
- Modify: `internal/worldgen/oracle_test.go` (add the height oracle)

**Interfaces:**
- Produces:
  - `(g *Generator) Height(wx, wy float32) float32`: the port of `GetHeight(float,float)` → `GetBiomeHeight(biome, wx, wy, out mask)` with `preGeneration=false`. It returns world metres, where the water line is 30.
  - `(g *Generator) addRivers(wx, wy, h float32) float32`. In this task it returns `h` unchanged, with a comment saying Task 5 replaces it.
  - `(g *Generator) pregenerationHeight(wx, wy float32, riverPreGen bool) float32`: `GetPregenerationHeight`, which Task 5 needs.
- Port sources in `reference/valheim/WorldGenerator.cs`:
  - `GetBiomeHeight` (≈1018–1071);
  - `GetMarshHeight`, `GetMeadowsHeight`, `GetForestHeight`, `GetMistlandsHeight`, `GetPlainsHeight`, `GetAshlandsHeightPregenerate`, `GetAshlandsHeight` with `cheap=false`, `GetEdgeHeight`, `GetOceanHeight`, `BaseHeightTilt`, `GetSnowMountainHeight` with `menu=false`, `GetDeepNorthHeightPregenerate`, `GetDeepNorthHeight`, `CreateAshlandsGap`, `CreateDeepNorthGap`, `DeepNorthWaveFade`, `GetHeightMultiplier` (≈1073–1455).

  Skip `m_world.m_menu` branches (not a menu world) and the `mask` colour outputs (compute nothing for them, but keep any side effects on the height). `GetBiomeSector` in `GetBiomeHeight` is computed but unused; omit it.

- [ ] **Step 1: Add the failing height oracle** — append to `internal/worldgen/oracle_test.go` (and add `"math"`, `"sort"` to its imports)

```go
func heightOracle(t *testing.T, sub, name string, maxMedian float64) {
	w, veg := goldenWorld(t, sub, name)
	if w.Meta.GenVersion < 2 {
		t.Skipf("%s uses worldGenVersion %d; height oracle runs on current-generation worlds", name, w.Meta.GenVersion)
	}
	g := New(w.Meta.Seed, w.Meta.GenVersion)
	perBiome := map[Biome][]float64{}
	var all []float64
	for _, z := range veg {
		d := math.Abs(float64(g.Height(z.Pos[0], z.Pos[2]) - z.Pos[1]))
		b := g.Biome(z.Pos[0], z.Pos[2])
		perBiome[b] = append(perBiome[b], d)
		all = append(all, d)
	}
	med := func(v []float64) float64 { sort.Float64s(v); return v[len(v)/2] }
	p90 := func(v []float64) float64 { sort.Float64s(v); return v[len(v)*9/10] }
	for b, v := range perBiome {
		t.Logf("%-12s n=%-6d median=%.3f m p90=%.3f m", b, len(v), med(v), p90(v))
	}
	m := med(all)
	t.Logf("%s: n=%d median=%.3f m p90=%.3f m", name, len(all), m, p90(all))
	if len(all) < 10000 || m > maxMedian {
		t.Fatalf("height oracle failed: median %.3f m > %.3f m", m, maxMedian)
	}
}

// Before rivers exist (Task 4) most vegetation is far from any river, so the
// median is already tight; Task 5 tightens the bound to the spec's 1 m.
func TestHeightOracleMuleVikings(t *testing.T) { heightOracle(t, "chunked", "MuleVikings", 2.0) }
```

`New` doesn't exist until Task 5. For this task, add `func New(seed, genVersion int32) *Generator { return NewBase(seed, genVersion) }` to `generator.go` with the comment `// New gains river pre-generation in Task 5.`

- [ ] **Step 2: Run to verify it fails** (`undefined: (*Generator).Height`). Then port the height functions.

- [ ] **Step 3: Run the oracle**

Run: `go test ./internal/worldgen/ -run HeightOracle -v`
Expected: PASS, with the per-biome table in the log. Record the table in the report. If one biome's median is far above the rest (more than 5 m), that biome's port is wrong; diff it against the reference before continuing.

- [ ] **Step 4: Commit** (`feat(worldgen): per-biome terrain heights ported, height oracle`, with the trailer).

---

### Task 5: Lakes, rivers, streams, and the final height oracle

**Files:**
- Create: `internal/worldgen/rivers.go`
- Modify: `internal/worldgen/generator.go` (`New` runs `pregenerate`), `internal/worldgen/heights.go` (the real `addRivers`), `internal/worldgen/oracle_test.go` (tighten the bound)

**Interfaces:**
- Produces:
  - `New(seed, genVersion int32) *Generator`: `NewBase` followed by `pregenerate()`, which runs `FindLakes`, `PlaceRivers`, `PlaceStreams(false)` and `PlaceStreams(true)`, then renders the river grids as the C# does.
  - `(g *Generator) Lakes() [][2]float32`
  - `(g *Generator) Rivers() []River` and `(g *Generator) Streams() []River`, where `River{P0, P1, Center [2]float32; WidthMin, WidthMax, Curve float32}` follows the C# `River` fields (check the reference class for the exact field list).
- Port sources in `reference/valheim/WorldGenerator.cs` (≈259–700):
  - `Pregenerate`, `FindLakes`, `MergePoints`, `FindClosest`, `PlaceStreams`, `FindStreamEndPoint`, `FindStreamStartPoint`, `PlaceRivers`, `FindClosestRiverEnd`, `FindRandomRiverEnd`, `HaveRiver` (both overloads), `IsRiverAllowed`, `RenderRivers`, `AddRiverPoint` (both), `InsideRiverGrid`, `GetRiverGrid`, `GetRiverWeight`, `GetWeight`, and `AddRivers` (≈937–957);
  - the nested `River`/`RiverPoint` types and any constants they use.
- **RNG discipline.** Each C# block that does `state = Random.state; InitState(seed); …; Random.state = state` becomes a fresh `NewURandom(seed)` used for just that block. The global Unity RNG must not leak between blocks. Every `Random.Range` call in those blocks draws from that block's `URandom`, in source order.
- **Iteration order.** Any C# `Dictionary`/`List` iteration that affects results must be reproduced in the same order. C# `List` keeps insertion order. For a C# `Dictionary` enumerated without removals, insertion order is also the enumeration order, so use a Go slice of keys alongside the map. Never range over a Go map where order matters.
- `New` on a real seed does the full pre-generation. Budget: under 30 s on one core. Report the measured time.

- [ ] **Step 1: Tighten the oracle first (it fails now)**

In `oracle_test.go`, change the MuleVikings call to `heightOracle(t, "chunked", "MuleVikings", 1.0)`. The spec's target is a median under 1 m. Add:

```go
func TestRiversPlaced(t *testing.T) {
	g := New(names.StableHash("FjordSeed"), 2)
	if len(g.Lakes()) == 0 || len(g.Rivers()) == 0 || len(g.Streams()) == 0 {
		t.Fatalf("lakes=%d rivers=%d streams=%d, want all > 0", len(g.Lakes()), len(g.Rivers()), len(g.Streams()))
	}
	t.Logf("FjordSeed: %d lakes, %d rivers, %d streams", len(g.Lakes()), len(g.Rivers()), len(g.Streams()))
}
```

Run: `go test ./internal/worldgen/ -run 'HeightOracle|RiversPlaced' -v`
Expected: FAIL (rivers not yet ported, or the median above 1.0 near rivers).

- [ ] **Step 2: Port the river code**, following the RNG and ordering rules above. Replace the Task 4 `addRivers` stub with the port.

- [ ] **Step 3: Run the tests**

Run: `go test ./internal/worldgen/ -v -run 'HeightOracle|RiversPlaced|BiomeOracle'`
Expected: PASS. Record the per-biome table, the lake/river/stream counts, and `New`'s wall time (add a `testing.B` benchmark `BenchmarkNewFjordSeed` and run `go test -bench NewFjordSeed -run '^$' ./internal/worldgen/`) in the report.

- [ ] **Step 4: Commit** (`feat(worldgen): lakes, rivers and streams; height oracle at spec target`, with the trailer).

---

### Task 5b: Heightmap blending across biome borders (added 2026-09-29 after Task 4)

**Why:** Task 4 found that the game does not use `GetHeight` directly for terrain. `HeightmapBuilder.Build` blends the four corner biomes of each 64 m zone heightmap with smoothstep weights. Without the blend, Mountain vegetation is off by about 3 m (median) near borders. With it, every biome is within about 0.02 m. The rendered map has to match the game's terrain.

**Files:**
- Create: `internal/worldgen/terrain.go`
- Modify: `internal/worldgen/oracle_test.go`

**Interfaces:**
- Produces:
  - `(g *Generator) TerrainHeight(wx, wy float32) float32`: the blended terrain height at a world point.
  - `type TerrainView struct{ g *Generator }` with `(v TerrainView) Biome(wx, wy float32) Biome` (= `g.Biome`) and `(v TerrainView) Height(wx, wy float32) float32` (= `g.TerrainHeight`).
  - `(g *Generator) TerrainView() TerrainView`. This satisfies `tiles.Source` (Task 6), and the CLI (Task 7) renders `g.TerrainView()`.
- Port source: `reference/valheim/HeightmapBuilder.cs` `Build`, the non-`m_distantLod` path (≈148–210):
  - Corner biomes via `GetBiome` at the heightmap's four corners.
  - When all four are equal, the height is `GetBiomeHeight(biome)`.
  - Otherwise it is the bilinear `DUtils.Lerp` of the four corner-biome heights, with weights `t2 = DUtils.SmoothStep(0,1,l/width)` along x and `t = DUtils.SmoothStep(0,1,k/width)` along z.
  - Keep the C# casts.
- Heightmap geometry:
  - A zone heightmap is centred on the zone centre, where zone `= Mathf.FloorToInt((p + 32) / 64)` per axis and centre `= zone * 64`.
  - Its corner is `centre - width*scale/2`.
  - The zone terrain uses `width 64`, `scale 1`. Those values come from the zone prefab, not from code, and the Task 4 implementer's scratch check confirmed them. The report at `.superpowers/sdd/2026-09-29-plan2-worldgen-and-tiles/task-4-report.md` explains its blending experiment; read it first.
  - For an arbitrary point, use the continuous fractions `l = (wx - cornerX)/scale` and `k = (wy - cornerZ)/scale`. The game evaluates the same formula at integer vertices and interpolates the mesh between them, so sampling the formula directly is what the map needs.
- Skip the colour mask and the `m_distantLod` smoothing branch.

- [ ] **Step 1: Add the failing oracle** to `oracle_test.go`. Add `terrainOracle(t, sub, name string, maxMedian, maxBiomeMedian float64)`, which works like `heightOracle` but compares `g.TerrainHeight`. It fails if the overall median exceeds `maxMedian` or any biome with n ≥ 500 exceeds `maxBiomeMedian`. Add `TestTerrainOracleMuleVikings` calling `terrainOracle(t, "chunked", "MuleVikings", 0.1, 0.5)`. The Task 4 scratch check measured about 0.02 m for every biome.
- [ ] **Step 2: Run it to verify it fails** (`undefined: (*Generator).TerrainHeight`), then implement `terrain.go`.
- [ ] **Step 3: Run** `go test ./internal/worldgen/ -run 'TerrainOracle|HeightOracle|BiomeOracle' -v`. Expected: PASS. Record the per-biome table and add a `BenchmarkTerrainHeight` (report ns/op).
- [ ] **Step 4: Commit** (`feat(worldgen): blend corner-biome heights like HeightmapBuilder`, with the trailer).

---

### Task 6: `tiles`, rendering the tile pyramid

**Files:**
- Create: `internal/tiles/tiles.go`, `internal/tiles/tiles_test.go`

**Interfaces:**
- Consumes: any value implementing `tiles.Source`. The CLI passes `g.TerrainView()` (Task 5b), so the map shows blended terrain.
- Produces:
  ```go
  type Source interface {
      Biome(wx, wy float32) worldgen.Biome
      Height(wx, wy float32) float32
  }
  type Options struct {
      Dir     string // output root; tiles at {Dir}/{z}/{x}/{y}.png
      Workers int    // 0 => runtime.NumCPU()
  }
  const (TileSize = 256; MaxZoom = 5; WorldRadius = 10500.0; WaterLevel = 30.0)
  func Render(ctx context.Context, src Source, opt Options, progress func(done, total int)) error
  func Complete(dir string) bool // true once the "complete" marker exists
  ```
- Behaviour:
  - z5 has `32×32` tiles. Pixel `(px,py)` of tile `(tx,ty)` samples world `x = -R + (tx*256+px+0.5)*m`, `z = R - (ty*256+py+0.5)*m`, where `m = 2R/8192`. North is up.
  - Colour: `biomeColor[biome]` shaded by a hillshade from the height gradient (light from the north-west). Water, meaning `Height < WaterLevel`, gets an ocean or lake colour that darkens with depth. Anything outside `WorldRadius` is transparent.
  - Each z5 tile samples a 1-px border so the hillshade is seamless at tile edges.
  - z4..z0: each tile is the 2×2 box-downsample of its four children, read back from disk.
  - Resumable: an existing, decodable tile file is skipped. Writes go to a temp file and are then renamed. `progress(done, 1024)` counts z5 tiles, including skipped ones, so the UI can show "389 of 1,024 tiles". When everything is written, write `{Dir}/complete`.
  - Honour `ctx` cancellation between tiles.
- Palette (sRGB) — Organic-friendly and muted, which Plan 4 may tune:
  - Meadows `#A3B25C`, Swamp `#786246`, Mountain `#E2E6EC`, BlackForest `#3E5834`, Plains `#D6BE6E`, AshLands `#963C28`, DeepNorth `#C8D7E6`, Mistlands `#6E6478`, Ocean `#2E5478`, and lake/river water `#3F6E91`.

- [ ] **Step 1: Write the failing tests** — `internal/tiles/tiles_test.go`

```go
package tiles

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

// flat: meadows island of radius 3000 m at 60 m, ocean elsewhere.
type flat struct{}

func (flat) Biome(x, z float32) worldgen.Biome {
	if x*x+z*z < 3000*3000 {
		return worldgen.Meadows
	}
	return worldgen.Ocean
}
func (flat) Height(x, z float32) float32 {
	if x*x+z*z < 3000*3000 {
		return 60
	}
	return 10
}

func TestRenderPyramid(t *testing.T) {
	dir := t.TempDir()
	var last, total int
	err := Render(context.Background(), flat{}, Options{Dir: dir, Workers: 4}, func(d, n int) { last, total = d, n })
	if err != nil {
		t.Fatal(err)
	}
	if total != 1024 || last != 1024 || !Complete(dir) {
		t.Fatalf("progress %d/%d complete=%v", last, total, Complete(dir))
	}
	for z, n := 0, 1; z <= MaxZoom; z, n = z+1, n*2 {
		for _, xy := range [][2]int{{0, 0}, {n - 1, n - 1}, {n / 2, n / 2}} {
			f, err := os.Open(filepath.Join(dir, itoa(z), itoa(xy[0]), itoa(xy[1])+".png"))
			if err != nil {
				t.Fatalf("z%d tile %v missing: %v", z, xy, err)
			}
			img, err := png.Decode(f)
			f.Close()
			if err != nil || img.Bounds().Dx() != TileSize || img.Bounds().Dy() != TileSize {
				t.Fatalf("z%d tile %v bad: %v", z, xy, err)
			}
		}
	}
	// Centre of the world is land (meadows green), a corner is outside the world (transparent).
	c := decodeAt(t, dir, 0, 0, 0, 128, 128)
	if c.A == 0 || c.G < c.B {
		t.Fatalf("world centre pixel %v should be opaque green land", c)
	}
	if corner := decodeAt(t, dir, 0, 0, 0, 0, 0); corner.A != 0 {
		t.Fatalf("outside-world pixel %v should be transparent", corner)
	}
}

func TestRenderResumesAndCancels(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	err := Render(ctx, flat{}, Options{Dir: dir, Workers: 1}, func(d, _ int) {
		n = d
		if d == 10 {
			cancel()
		}
	})
	if err == nil || Complete(dir) {
		t.Fatalf("cancelled render: err=%v complete=%v", err, Complete(dir))
	}
	var first int
	if err := Render(context.Background(), flat{}, Options{Dir: dir, Workers: 2}, func(d, _ int) {
		if first == 0 {
			first = d
		}
	}); err != nil || !Complete(dir) {
		t.Fatalf("resume: err=%v complete=%v", err, Complete(dir))
	}
	if first < n {
		t.Fatalf("resume restarted progress at %d; %d tiles already existed", first, n)
	}
}
```

Add small helpers to the test file: `itoa` (`strconv.Itoa`) and `decodeAt(t, dir, z, x, y, px, py) color.NRGBA`, which opens, decodes and converts the pixel with `color.NRGBAModel`. Resume semantics: the first progress callback after a resume already counts the skipped tiles, so `first >= n`.

- [ ] **Step 2: Run to verify it fails.** Implement `tiles.go` to the behaviour above. Workers pull z5 tile coordinates from a channel. Existing tiles are counted before the workers start and reported in the first progress call. Then build z4..z0.

- [ ] **Step 3: Run the tests**

Run: `gofmt -w . && go vet ./... && go test ./internal/tiles/ -v`
Expected: PASS.

- [ ] **Step 4: Commit** (`feat(tiles): resumable z0-z5 tile pyramid renderer with progress`, with the trailer).

---

### Task 7: The dev CLI `farsight-render`, and a real render

**Files:**
- Create: `cmd/farsight-render/main.go`

**Interfaces:**
- Consumes: `worldgen.New`, `names.StableHash`, `tiles.Render`.
- Produces: `farsight-render -seed NAME [-gen 2] -out DIR [-preview preview.png]`. It prints progress to stderr every 64 tiles and the total time. With `-preview`, it writes a 2048×2048 PNG stitched from the z3 tiles.

- [ ] **Step 1: Implement** `cmd/farsight-render/main.go`: parse the flags, `g := worldgen.New(names.StableHash(seed), gen)` (log the pre-generation time), call `tiles.Render(ctx, g.TerrainView(), …)` with a progress printer, then stitch the preview from `{out}/3/{x}/{y}.png` (8×8 tiles) using `image/draw`.

- [ ] **Step 2: Render FjordSeed**

Run: `go run ./cmd/farsight-render -seed FjordSeed -out /tmp/farsight-tiles -preview /tmp/farsight-preview.png`
Expected: pre-generation time, progress to 1024/1024, the total time, and the preview written. Record both times in the commit body and the report. Open `/tmp/farsight-preview.png` and check it by eye: a round world, a central Meadows start area, rivers and lakes visible as blue lines and blobs, snowy mountains, the Ashlands band in the south, and the Deep North band in the north. The controller will review the preview image too.

- [ ] **Step 3: Commit** (`feat(cli): farsight-render dev tool`, with the timings in the body and the trailer).

---

## After this plan

- Plan 4 calls `worldgen.New(seed, genVersion)` and `tiles.Render` when a server's `(seed, genVersion)` has no complete tile set, and serves `/tiles/{seed}-{gen}/{z}/{x}/{y}.png`. It shows the "Charting the world" state from `progress`.
- The fog mask stays client-side, built from the snapshot's `exploredZones`; tiles are never fogged.
