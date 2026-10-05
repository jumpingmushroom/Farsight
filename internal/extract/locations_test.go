package extract

import "testing"

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
	// Every prefab in the spec's table, with its group.
	want := map[string]string{
		"Eikthyrnir": "landmarks", "GDKing": "landmarks", "Bonemass": "landmarks", "Dragonqueen": "landmarks",
		"GoblinKing": "landmarks", "Mistlands_DvergrBossEntrance1": "landmarks", "FaderLocation": "landmarks", "DN_Bossroom": "landmarks",
		"Vendor_BlackForest": "landmarks", "Hildir_camp": "landmarks", "BogWitch_Camp": "landmarks",
		"AncientUpgradeStation": "landmarks", "StartTemple": "landmarks",
		"PlaceofMystery1": "landmarks", "PlaceofMystery2": "landmarks", "PlaceofMystery3": "landmarks",
		"Hildir_cave": "landmarks", "Hildir_crypt": "landmarks", "Hildir_plainsfortress": "landmarks",
		"CharredFortress": "landmarks", "NorthMemorialPlace": "landmarks",
		"Crypt2": "dungeons", "Crypt3": "dungeons", "Crypt4": "dungeons", "SunkenCrypt4": "dungeons",
		"TrollCave02": "dungeons", "MountainCave02": "dungeons",
		"Mistlands_DvergrTownEntrance1": "dungeons", "Mistlands_DvergrTownEntrance2": "dungeons",
		"MorkBorg": "dungeons", "TheHole01": "dungeons",
		"GoblinCamp2": "minor", "BearCave": "minor", "NorthVillage": "minor",
		"MorgenHole1": "minor", "MorgenHole2": "minor", "MorgenHole3": "minor",
	}
	if len(locationTable) != len(want) {
		t.Errorf("table has %d entries, want %d", len(locationTable), len(want))
	}
	for prefab, group := range want {
		if e, ok := locationTable[prefab]; !ok || e.Group != group {
			t.Errorf("%s = %+v, want group %s", prefab, e, group)
		}
	}
}
