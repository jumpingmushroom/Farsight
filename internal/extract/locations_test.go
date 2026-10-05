package extract

import (
	"strings"
	"testing"
	"unicode"
)

func TestClassifyLocations(t *testing.T) {
	raw := []Marker{
		{ID: "loc-1", Kind: "location", Type: "Eikthyrnir"},
		{ID: "loc-2", Kind: "location", Type: "Crypt3"},
		{ID: "loc-3", Kind: "location", Type: "Runestone_Meadows"},
		{ID: "loc-4", Kind: "location", Type: "BogWitch_Camp", Unplaced: true},
		{ID: "loc-5", Kind: "location", Type: "BogWitch_Camp"},
		{ID: "loc-6", Kind: "location", Type: "AncientUpgradeStation"},
		{ID: "loc-7", Kind: "location", Type: "MorgenHole2", Unplaced: true},
		{ID: "loc-8", Kind: "location", Type: "Hildir_crypt"},
		// An older agent's entry: already classified, no unplaced flag.
		{ID: "loc-9", Kind: "dungeon", Type: "SunkenCrypt4", Label: "Sunken crypt"},
	}
	got := ClassifyLocations(raw)
	want := []struct{ id, kind, label, group string }{
		{"loc-1", "boss_altar", "Eikthyr", "landmarks"},
		{"loc-2", "dungeon", "Burial chambers", "dungeons"},
		{"loc-5", "trader", "Bog Witch", "landmarks"},
		{"loc-6", "landmark", "Forge of Potential", "landmarks"},
		{"loc-7", "landmark", "Putrid hole", "minor"},
		{"loc-8", "dungeon", "Smouldering tomb", "landmarks"},
		{"loc-9", "dungeon", "Sunken crypt", "dungeons"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d: %+v", len(got), got)
	}
	for i, w := range want {
		g := got[i]
		if g.ID != w.id || g.Kind != w.kind || g.Label != w.label || g.Group != w.group {
			t.Errorf("%d = %+v, want %+v", i, g, w)
		}
	}
	if raw[0].Kind != "location" {
		t.Error("input was modified")
	}
	if ClassifyLocations(nil) == nil {
		t.Error("nil result")
	}
}

func TestLocationTableComplete(t *testing.T) {
	// Every prefab in the spec's table, with its kind and group. Only
	// Hildir's three caves/crypt/fortress and the dungeons group are kind
	// "dungeon" — everything else, including BearCave, is "landmark" (or
	// "boss_altar"/"trader").
	want := map[string]struct{ kind, group string }{
		"Eikthyrnir": {"boss_altar", "landmarks"}, "GDKing": {"boss_altar", "landmarks"},
		"Bonemass": {"boss_altar", "landmarks"}, "Dragonqueen": {"boss_altar", "landmarks"},
		"GoblinKing": {"boss_altar", "landmarks"}, "Mistlands_DvergrBossEntrance1": {"boss_altar", "landmarks"},
		"FaderLocation": {"boss_altar", "landmarks"}, "DN_Bossroom": {"boss_altar", "landmarks"},
		"Vendor_BlackForest": {"trader", "landmarks"}, "Hildir_camp": {"trader", "landmarks"},
		"BogWitch_Camp":         {"trader", "landmarks"},
		"AncientUpgradeStation": {"landmark", "landmarks"}, "StartTemple": {"landmark", "landmarks"},
		"PlaceofMystery1": {"landmark", "landmarks"}, "PlaceofMystery2": {"landmark", "landmarks"},
		"PlaceofMystery3": {"landmark", "landmarks"},
		"Hildir_cave":     {"dungeon", "landmarks"}, "Hildir_crypt": {"dungeon", "landmarks"},
		"Hildir_plainsfortress": {"dungeon", "landmarks"},
		"CharredFortress":       {"landmark", "landmarks"}, "NorthMemorialPlace": {"landmark", "landmarks"},
		"Crypt2": {"dungeon", "dungeons"}, "Crypt3": {"dungeon", "dungeons"}, "Crypt4": {"dungeon", "dungeons"},
		"SunkenCrypt4": {"dungeon", "dungeons"},
		"TrollCave02":  {"dungeon", "dungeons"}, "MountainCave02": {"dungeon", "dungeons"},
		"Mistlands_DvergrTownEntrance1": {"dungeon", "dungeons"}, "Mistlands_DvergrTownEntrance2": {"dungeon", "dungeons"},
		"MorkBorg": {"dungeon", "dungeons"}, "TheHole01": {"dungeon", "dungeons"},
		"GoblinCamp2": {"landmark", "minor"}, "BearCave": {"landmark", "minor"}, "NorthVillage": {"landmark", "minor"},
		"MorgenHole1": {"landmark", "minor"}, "MorgenHole2": {"landmark", "minor"}, "MorgenHole3": {"landmark", "minor"},
	}
	if len(locationTable) != len(want) {
		t.Errorf("table has %d entries, want %d", len(locationTable), len(want))
	}
	for prefab, w := range want {
		if e, ok := locationTable[prefab]; !ok || e.Kind != w.kind || e.Group != w.group {
			t.Errorf("%s = %+v, want kind %s group %s", prefab, e, w.kind, w.group)
		}
	}
}

// isVowel reports whether r starts a vowel sound for "a"/"an" (ASCII
// vowels only; none of the table's phrases start with anything else).
func isVowel(r rune) bool {
	switch unicode.ToLower(r) {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}

// TestNearPhraseComplete is the I2 fix: every table entry has a non-empty
// Near phrase (NearPhrase, used by worldevents.Near instead of a
// "a "+lower(Label) heuristic), it's never a double article ("a a…"), and
// an "a " phrase never precedes a vowel (that word should use "an").
// worldevents.TestNearWording checks specific wordings end to end; this
// checks the invariant holds for every entry, present and future.
func TestNearPhraseComplete(t *testing.T) {
	for prefab, e := range locationTable {
		near := e.Near
		if near == "" {
			t.Errorf("%s: empty Near", prefab)
			continue
		}
		if strings.HasPrefix(near, "a a") {
			t.Errorf("%s: Near = %q, doubled article", prefab, near)
		}
		if rest, ok := strings.CutPrefix(near, "a "); ok && len(rest) > 0 && isVowel(rune(rest[0])) {
			t.Errorf("%s: Near = %q, should be \"an\" before a vowel", prefab, near)
		}
		if NearPhrase(Marker{Type: prefab}) != near {
			t.Errorf("%s: NearPhrase = %q, want %q", prefab, NearPhrase(Marker{Type: prefab}), near)
		}
	}
	if NearPhrase(Marker{Type: "Runestone_Meadows"}) != "" {
		t.Error("NearPhrase for an unmapped prefab should be empty")
	}
}
