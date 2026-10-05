package worldevents

import (
	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

// Geo answers the place questions events need about one save's world.
type Geo interface {
	// Biome is the display name of the biome at (x, z): "Black Forest".
	Biome(x, z float32) string
	// Explored reports whether (x, z) is in explored ground.
	Explored(x, z float32) bool
}

// biomeNames are the biomes' display names, as the map legend shows them.
var biomeNames = map[worldgen.Biome]string{
	worldgen.Meadows:     "Meadows",
	worldgen.BlackForest: "Black Forest",
	worldgen.Swamp:       "Swamp",
	worldgen.Mountain:    "Mountains",
	worldgen.Plains:      "Plains",
	worldgen.Mistlands:   "Mistlands",
	worldgen.AshLands:    "Ashlands",
	worldgen.DeepNorth:   "Deep North",
	worldgen.Ocean:       "Ocean",
}

// BiomeName is b's display name ("" for none).
func BiomeName(b worldgen.Biome) string { return biomeNames[b] }

type snapGeo struct {
	gen  *worldgen.Generator
	mask *explored.Mask
}

// NewGeo is the Geo of snap's world, explored per mask. Biomes come from
// the world generator's base pass (worldgen.NewBase): GetBiome needs no
// rivers, so the lake and river pre-generation is skipped.
func NewGeo(snap *extract.Snapshot, mask *explored.Mask) Geo {
	return snapGeo{gen: worldgen.NewBase(snap.World.Seed, snap.World.GenVersion), mask: mask}
}

func (g snapGeo) Biome(x, z float32) string { return BiomeName(g.gen.Biome(x, z)) }

func (g snapGeo) Explored(x, z float32) bool { return g.mask.At(float64(x), float64(z)) }

// MaskOf is snap's explored mask: the agent's 12 m mask, or for agents
// that predate it the generated zones, as the central app's tiles use.
func MaskOf(snap *extract.Snapshot) *explored.Mask {
	if snap.Explored != nil {
		if m, err := explored.Decode(*snap.Explored); err == nil {
			return m
		}
	}
	return explored.FromZones(snap.ExploredZones)
}
