package extract

// locationEntry is how one location prefab shows on the atlas.
type locationEntry struct {
	Kind, Label, Group string
	// Unique sites (one per world) keep up to ~10 planned candidates until
	// a player loads one; only the placed one is real.
	Unique bool
}

// locationTable maps location prefabs to markers (spec 2026-10-05,
// cursor biome and locations). Prefabs not listed are not shown.
var locationTable = map[string]locationEntry{
	"Eikthyrnir":                    {"boss_altar", "Eikthyr", "landmarks", false},
	"GDKing":                        {"boss_altar", "The Elder", "landmarks", false},
	"Bonemass":                      {"boss_altar", "Bonemass", "landmarks", false},
	"Dragonqueen":                   {"boss_altar", "Moder", "landmarks", false},
	"GoblinKing":                    {"boss_altar", "Yagluth", "landmarks", false},
	"Mistlands_DvergrBossEntrance1": {"boss_altar", "The Queen", "landmarks", false},
	"FaderLocation":                 {"boss_altar", "Fader", "landmarks", false},
	"DN_Bossroom":                   {"boss_altar", "Kall Fimbulbringer", "landmarks", false},

	"Vendor_BlackForest": {"trader", "Haldor", "landmarks", true},
	"Hildir_camp":        {"trader", "Hildir", "landmarks", true},
	"BogWitch_Camp":      {"trader", "Bog Witch", "landmarks", true},

	"AncientUpgradeStation": {"landmark", "Forge of Potential", "landmarks", true},
	"StartTemple":           {"landmark", "Sacrificial stones", "landmarks", false},
	"PlaceofMystery1":       {"landmark", "Mysterious location", "landmarks", false},
	"PlaceofMystery2":       {"landmark", "Mysterious location", "landmarks", false},
	"PlaceofMystery3":       {"landmark", "Mysterious location", "landmarks", false},
	"Hildir_cave":           {"dungeon", "Howling cavern", "landmarks", false},
	"Hildir_crypt":          {"dungeon", "Smouldering tomb", "landmarks", false},
	"Hildir_plainsfortress": {"dungeon", "Sealed tower", "landmarks", false},
	"CharredFortress":       {"landmark", "Charred fortress", "landmarks", false},
	"NorthMemorialPlace":    {"landmark", "Memorial site", "landmarks", false},

	"Crypt2":                        {"dungeon", "Burial chambers", "dungeons", false},
	"Crypt3":                        {"dungeon", "Burial chambers", "dungeons", false},
	"Crypt4":                        {"dungeon", "Burial chambers", "dungeons", false},
	"SunkenCrypt4":                  {"dungeon", "Sunken crypt", "dungeons", false},
	"TrollCave02":                   {"dungeon", "Troll cave", "dungeons", false},
	"MountainCave02":                {"dungeon", "Frost cave", "dungeons", false},
	"Mistlands_DvergrTownEntrance1": {"dungeon", "Infested mine", "dungeons", false},
	"Mistlands_DvergrTownEntrance2": {"dungeon", "Infested mine", "dungeons", false},
	"MorkBorg":                      {"dungeon", "Mörkhalla", "dungeons", false},
	"TheHole01":                     {"dungeon", "Winding tunnels", "dungeons", false},

	"GoblinCamp2":  {"landmark", "Fuling village", "minor", false},
	"BearCave":     {"landmark", "Bear cave", "minor", false},
	"NorthVillage": {"landmark", "Abandoned village", "minor", false},
	"MorgenHole1":  {"landmark", "Putrid hole", "minor", false},
	"MorgenHole2":  {"landmark", "Putrid hole", "minor", false},
	"MorgenHole3":  {"landmark", "Putrid hole", "minor", false},
}

// Boss progression, in game order, keyed by the global key set on defeat.
// Valheim 1.0 has eight bosses, the last being Kall Fimbulbringer in the
// Deep North (prefab FrozenKing, key defeated_frozenking).
// defeated_writhan is not a boss key: it's the progress key the game sets
// when the Writhan, an ordinary Swamp creature, is killed (like
// killedtroll).
var bossKeys = []struct{ Key, Name string }{
	{"defeated_eikthyr", "Eikthyr"},
	{"defeated_gdking", "The Elder"},
	{"defeated_bonemass", "Bonemass"},
	{"defeated_dragon", "Moder"},
	{"defeated_goblinking", "Yagluth"},
	{"defeated_queen", "The Queen"},
	{"defeated_fader", "Fader"},
	{"defeated_frozenking", "Kall Fimbulbringer"},
}

// BossesFromKeys returns the eight bosses in game order, with Defeated set
// from whichever of globalKeys are present. It's the single source of
// truth for turning a save's (or a stored snapshot's) global keys into the
// boss list; the extractor and the central app's card-building code both
// use it, so a snapshot without updated Bosses data still gets the right
// answer.
func BossesFromKeys(globalKeys []string) []Boss {
	keys := map[string]bool{}
	for _, k := range globalKeys {
		keys[k] = true
	}
	bosses := make([]Boss, 0, len(bossKeys))
	for _, b := range bossKeys {
		bosses = append(bosses, Boss{Key: b.Key, Name: b.Name, Defeated: keys[b.Key]})
	}
	return bosses
}
