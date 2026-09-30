package worldgen

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
)

// terrainDigestPoints is the fixed sample set for TestTerrainDigest: a
// 128x128 grid covering the whole playable world, plus a small table of
// zone-edge and zone-corner points that stress TerrainHeight's corner-biome
// blend (terrain.go's zoneAxis, which floors (p+32)/64 -- zone boundaries
// fall at p = ..., -96, -32, 32, 96, ...).
func terrainDigestPoints() [][2]float32 {
	const n = 128
	const r float32 = 10500
	const step = 2 * r / n

	pts := make([][2]float32, 0, n*n+16+4+16)
	for iy := 0; iy < n; iy++ {
		wz := -r + (float32(iy)+0.5)*step
		for ix := 0; ix < n; ix++ {
			wx := -r + (float32(ix)+0.5)*step
			pts = append(pts, [2]float32{wx, wz})
		}
	}

	// 0.01 m on either side of every zone edge on the x axis (z held at 0)
	// and on the z axis (x held at 0): the point closest to where
	// zoneAxis's floor can flip the enclosing zone, and so the corner
	// biomes TerrainHeight blends.
	for _, x0 := range []float32{-96, -32, 32, 96} {
		pts = append(pts, [2]float32{x0 - 0.01, 0}, [2]float32{x0 + 0.01, 0})
	}
	for _, z0 := range []float32{-32, 32} {
		pts = append(pts, [2]float32{0, z0 - 0.01}, [2]float32{0, z0 + 0.01})
	}

	// Exact zone corners (e.g. (32, 32), (-32, 96)), where all four of
	// TerrainHeight's corner biomes can be distinct at once.
	for _, x0 := range []float32{-96, -32, 32, 96} {
		for _, z0 := range []float32{-96, -32, 32, 96} {
			pts = append(pts, [2]float32{x0, z0})
		}
	}
	return pts
}

// terrainDigest hashes Biome, math.Float32bits(Height) and
// math.Float32bits(TerrainHeight) at every terrainDigestPoints() point, in
// order, into a single SHA-256.
func terrainDigest(g *Generator) string {
	h := sha256.New()
	var buf [4]byte
	for _, p := range terrainDigestPoints() {
		binary.LittleEndian.PutUint16(buf[:2], uint16(g.Biome(p[0], p[1])))
		h.Write(buf[:2])
		binary.LittleEndian.PutUint32(buf[:], math.Float32bits(g.Height(p[0], p[1])))
		h.Write(buf[:])
		binary.LittleEndian.PutUint32(buf[:], math.Float32bits(g.TerrainHeight(p[0], p[1])))
		h.Write(buf[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestTerrainDigest pins a SHA-256 digest of Biome/Height/TerrainHeight over
// terrainDigestPoints(), computed with no golden save data -- unlike the
// oracle tests (oracle_test.go), which skip when testdata-golden/ is
// absent, this always runs. It guards against two things the oracles
// (loose m-scale tolerances against real placements) can't catch:
//
//   - Cross-platform FMA drift: float32 arithmetic can fuse multiply-add
//     differently across architectures/GOAMD64 levels, changing the low
//     bits of Height/TerrainHeight without moving an oracle's median at
//     all. This repo is built amd64 with the default GOAMD64=v1 (Plan 2's
//     Task 4 ruling); this test is the tripwire if that ever changes.
//   - Silent refactors: a change that shifts a value by a fraction of a
//     millimetre passes every oracle's tolerance but flips this digest.
//
// The constants below were pinned from this package's HEAD at the time
// this test was added (the oracle tests already prove that HEAD matches
// the game). If a legitimate change moves a digest, recompute it from this
// test's t.Logf output and re-pin.
func TestTerrainDigest(t *testing.T) {
	cases := []struct {
		name       string
		seed       int32
		genVersion int32
		want       string
	}{
		{"FjordSeed-gen2", names.StableHash("FjordSeed"), 2, "35bf1181b2a2198350beb1614b4a554e33441125c9760c8d1e26acba2913c393"},
		{"Qm4RtX8vLc-gen1", names.StableHash("Qm4RtX8vLc"), 1, "b517b1af0e7176a756e169fef6afe317ac42d7116276f837aff2b14b7da550d7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := New(c.seed, c.genVersion)
			got := terrainDigest(g)
			t.Logf("%s: digest %s", c.name, got)
			if got != c.want {
				t.Fatalf("digest mismatch: got %s, want %s (re-pin if this is a legitimate change)", got, c.want)
			}
		})
	}
}
