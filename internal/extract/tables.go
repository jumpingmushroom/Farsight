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
	"DN_Bossroom":                   "Writhan",
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
var bossKeys = []struct{ Key, Name string }{
	{"defeated_eikthyr", "Eikthyr"},
	{"defeated_gdking", "The Elder"},
	{"defeated_bonemass", "Bonemass"},
	{"defeated_dragon", "Moder"},
	{"defeated_goblinking", "Yagluth"},
	{"defeated_queen", "The Queen"},
	{"defeated_fader", "Fader"},
	{"defeated_writhan", "Writhan"},
}
