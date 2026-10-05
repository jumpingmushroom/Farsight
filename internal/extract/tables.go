package extract

// locationEntry is how one location prefab shows on the atlas.
type locationEntry struct {
	Kind, Label, Group string
	// Unique sites (one per world) keep up to ~10 planned candidates until
	// a player loads one; only the placed one is real.
	Unique bool
	// Near is the entry's "near …" phrase (worldevents.Near), e.g. "a
	// sunken crypt", "Haldor", "the Forge of Potential", "Mörkhalla",
	// "burial chambers": proper nouns and boss altars carry no article,
	// plurals carry none either, everything else gets "a"/"an". Never
	// empty, never "a "+vowel or a double article: TestNearWording checks
	// every entry.
	Near string
}

// locationTable maps location prefabs to markers (spec 2026-10-05,
// cursor biome and locations). Prefabs not listed are not shown.
var locationTable = map[string]locationEntry{
	"Eikthyrnir":                    {"boss_altar", "Eikthyr", "landmarks", false, "Eikthyr’s altar"},
	"GDKing":                        {"boss_altar", "The Elder", "landmarks", false, "The Elder’s altar"},
	"Bonemass":                      {"boss_altar", "Bonemass", "landmarks", false, "Bonemass’s altar"},
	"Dragonqueen":                   {"boss_altar", "Moder", "landmarks", false, "Moder’s altar"},
	"GoblinKing":                    {"boss_altar", "Yagluth", "landmarks", false, "Yagluth’s altar"},
	"Mistlands_DvergrBossEntrance1": {"boss_altar", "The Queen", "landmarks", false, "The Queen’s altar"},
	"FaderLocation":                 {"boss_altar", "Fader", "landmarks", false, "Fader’s altar"},
	"DN_Bossroom":                   {"boss_altar", "Kall Fimbulbringer", "landmarks", false, "Kall Fimbulbringer’s altar"},

	"Vendor_BlackForest": {"trader", "Haldor", "landmarks", true, "Haldor"},
	"Hildir_camp":        {"trader", "Hildir", "landmarks", true, "Hildir"},
	"BogWitch_Camp":      {"trader", "Bog Witch", "landmarks", true, "Bog Witch"},

	"AncientUpgradeStation": {"landmark", "Forge of Potential", "landmarks", true, "the Forge of Potential"},
	"StartTemple":           {"landmark", "Sacrificial stones", "landmarks", false, "sacrificial stones"},
	"PlaceofMystery1":       {"landmark", "Mysterious location", "landmarks", false, "a mysterious location"},
	"PlaceofMystery2":       {"landmark", "Mysterious location", "landmarks", false, "a mysterious location"},
	"PlaceofMystery3":       {"landmark", "Mysterious location", "landmarks", false, "a mysterious location"},
	"Hildir_cave":           {"dungeon", "Howling cavern", "landmarks", false, "a howling cavern"},
	"Hildir_crypt":          {"dungeon", "Smouldering tomb", "landmarks", false, "a smouldering tomb"},
	"Hildir_plainsfortress": {"dungeon", "Sealed tower", "landmarks", false, "a sealed tower"},
	"CharredFortress":       {"landmark", "Charred fortress", "landmarks", false, "a charred fortress"},
	"NorthMemorialPlace":    {"landmark", "Memorial site", "landmarks", false, "a memorial site"},

	"Crypt2":                        {"dungeon", "Burial chambers", "dungeons", false, "burial chambers"},
	"Crypt3":                        {"dungeon", "Burial chambers", "dungeons", false, "burial chambers"},
	"Crypt4":                        {"dungeon", "Burial chambers", "dungeons", false, "burial chambers"},
	"SunkenCrypt4":                  {"dungeon", "Sunken crypt", "dungeons", false, "a sunken crypt"},
	"TrollCave02":                   {"dungeon", "Troll cave", "dungeons", false, "a troll cave"},
	"MountainCave02":                {"dungeon", "Frost cave", "dungeons", false, "a frost cave"},
	"Mistlands_DvergrTownEntrance1": {"dungeon", "Infested mine", "dungeons", false, "an infested mine"},
	"Mistlands_DvergrTownEntrance2": {"dungeon", "Infested mine", "dungeons", false, "an infested mine"},
	"MorkBorg":                      {"dungeon", "Mörkhalla", "dungeons", false, "Mörkhalla"},
	"TheHole01":                     {"dungeon", "Winding tunnels", "dungeons", false, "winding tunnels"},

	"GoblinCamp2":  {"landmark", "Fuling village", "minor", false, "a fuling village"},
	"BearCave":     {"landmark", "Bear cave", "minor", false, "a bear cave"},
	"NorthVillage": {"landmark", "Abandoned village", "minor", false, "an abandoned village"},
	"MorgenHole1":  {"landmark", "Putrid hole", "minor", false, "a putrid hole"},
	"MorgenHole2":  {"landmark", "Putrid hole", "minor", false, "a putrid hole"},
	"MorgenHole3":  {"landmark", "Putrid hole", "minor", false, "a putrid hole"},
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
