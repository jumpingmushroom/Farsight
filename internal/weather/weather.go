// Package weather is the game's deterministic weather schedule per biome:
// EnvMan.UpdateEnvironment's weighted draw (worldgen.URandom) over each
// biome's non-override environment list, keyed by a 666 s "weather
// period". See docs/superpowers/specs/2026-10-05-time-and-weather-design.md.
//
// Not modelled (spec 2026-10-05): the Dark Meadows alt biome, Ashlands
// and Deep North override entries (both local to where a player stands),
// and raid/boss/dungeon weather.
package weather

import (
	"math"

	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

const (
	// PeriodSec is the game's weather period length: EnvMan's
	// m_environmentDuration.
	PeriodSec = 666
	// DaySec is the game's day length: EnvMan's m_dayLengthSec.
	DaySec = 1800
)

// Period is the weather period containing netTime (ZNet seconds).
func Period(netTime float64) int64 {
	return int64(math.Floor(math.Floor(netTime) / PeriodSec))
}

type entry struct {
	env    string
	weight float32
}

// biomeEntries are each biome's non-override environments, weights as
// float32 exactly as the source JSON, in the game's load order (EnvMan's
// base biome list, then LocationList appends). Ashlands/Deep North
// override entries (e.g. Ocean's Ashlands_SeaStorm) are omitted, as is
// the Dark Meadows alt biome's block/add list. Extracted from the game's
// main scene EnvMan + LocationLists, 2026-10-05.
var biomeEntries = map[worldgen.Biome][]entry{
	worldgen.Meadows:     {{"Clear", 5}, {"Rain", 0.2}, {"Misty", 0.2}, {"ThunderStorm", 0.2}, {"LightRain", 0.2}},
	worldgen.BlackForest: {{"DeepForest Mist", 2}, {"Rain", 0.1}, {"Misty", 0.1}, {"ThunderStorm", 0.1}},
	worldgen.Swamp:       {{"SwampRain", 1}},
	worldgen.Mountain:    {{"SnowStorm", 1}, {"Snow", 5}},
	worldgen.Plains:      {{"Heath clear", 2}, {"Misty", 0.4}, {"LightRain", 0.4}},
	worldgen.Ocean:       {{"Rain", 0.1}, {"LightRain", 0.1}, {"Misty", 0.1}, {"Clear", 1}, {"ThunderStorm", 0.1}},
	worldgen.Mistlands:   {{"Mistlands_clear", 1.5}, {"Mistlands_rain", 0.1}, {"Mistlands_thunder", 0.1}},
	worldgen.AshLands:    {{"Ashlands_ashrain", 1.5}, {"Ashlands_misty", 0.1}, {"Ashlands_CinderRain", 0.2}, {"Ashlands_storm", 0.05}},
	worldgen.DeepNorth:   {{"Twilight_SnowStorm", 0.5}, {"Twilight_Snow", 1}, {"Twilight_Clear", 1}},
}

// EnvAt is the internal environment name the game draws for b at period,
// mirroring EnvMan.UpdateEnvironment's SelectWeightedEnvironment over the
// biome's non-override entries (float32 running sum, first entry whose
// sum is at least the draw). "" for biomes with no entries (e.g. 0, the
// zero Biome).
func EnvAt(period int64, b worldgen.Biome) string {
	es := biomeEntries[b]
	if len(es) == 0 {
		return ""
	}
	var total float32
	for _, e := range es {
		total += e.weight
	}
	draw := worldgen.NewURandom(int32(period)).RangeFloat(0, total)
	var sum float32
	for _, e := range es {
		sum += e.weight
		if sum >= draw {
			return e.env
		}
	}
	return es[len(es)-1].env
}

// At is EnvAt's display name (see names.go); "" for biomes with no
// entries.
func At(period int64, b worldgen.Biome) string {
	env := EnvAt(period, b)
	if env == "" {
		return ""
	}
	return displayName[env]
}
