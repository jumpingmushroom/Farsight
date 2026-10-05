package extract

// Location prefabs that become atlas markers, from the spike's location list.
var bossAltars = map[string]string{
	"Eikthyrnir":                    "Eikthyr",
	"GDKing":                        "The Elder",
	"Bonemass":                      "Bonemass",
	"Dragonqueen":                   "Moder",
	"GoblinKing":                    "Yagluth",
	"Mistlands_DvergrBossEntrance1": "The Queen",
	"FaderLocation":                 "Fader",
}

var traders = map[string]string{
	"Vendor_BlackForest": "Haldor",
	"Hildir_camp":        "Hildir",
	"BogWitch_Camp":      "Bog Witch",
}

var dungeons = map[string]string{
	"Crypt2":                "Burial chambers",
	"Crypt3":                "Burial chambers",
	"Crypt4":                "Burial chambers",
	"SunkenCrypt4":          "Sunken crypt",
	"TrollCave02":           "Troll cave",
	"MountainCave02":        "Frost cave",
	"BearCave":              "Bear cave",
	"Hildir_cave":           "Howling cavern",
	"Hildir_crypt":          "Smouldering tomb",
	"Hildir_plainsfortress": "Sealed tower",
	"GoblinCamp2":           "Fuling village",
}

// Boss progression, in game order, keyed by the global key set on defeat.
// Valheim has exactly seven bosses; defeated_writhan is not one of
// them, it's the progress key the game sets when the Writhan, an ordinary
// Swamp creature, is killed (like killedtroll).
var bossKeys = []struct{ Key, Name string }{
	{"defeated_eikthyr", "Eikthyr"},
	{"defeated_gdking", "The Elder"},
	{"defeated_bonemass", "Bonemass"},
	{"defeated_dragon", "Moder"},
	{"defeated_goblinking", "Yagluth"},
	{"defeated_queen", "The Queen"},
	{"defeated_fader", "Fader"},
}

// BossesFromKeys returns the seven bosses in game order, with Defeated set
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
