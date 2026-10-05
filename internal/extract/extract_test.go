package extract

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save"
)

func h(s string) int32 { return names.StableHash(s) }

func z(prefab string, pos [3]float32) *save.ZDO {
	return &save.ZDO{Prefab: h(prefab), Pos: pos}
}

func TestExtractMarkersPlayersBosses(t *testing.T) {
	e := New()
	p1 := z("portal_wood", [3]float32{1, 2, 3})
	p1.Strings = map[int32]string{h("tag"): "Trader"}
	p2 := z("portal_wood", [3]float32{4, 5, 6})
	p2.Strings = map[int32]string{h("tag"): "Trader"}
	p3 := z("portal_wood", [3]float32{7, 8, 9})
	p3.Strings = map[int32]string{h("tag"): "lonely"}
	bed := z("bed", [3]float32{10, 0, 10})
	bed.Longs = map[int32]int64{h("owner"): 1000000099}
	bed.Strings = map[int32]string{h("ownerName"): "Sigrun"}
	goblinBed := z("goblin_bed", [3]float32{0, 0, 0})
	wolf := z("Wolf", [3]float32{20, 0, 20})
	wolf.Ints = map[int32]int32{h("tamed"): 1}
	wolf.Strings = map[int32]string{h("TamedName"): "Skollr"}
	wildWolf := z("Wolf", [3]float32{0, 0, 0})
	tomb := z("Player_tombstone", [3]float32{-2182, 33, 1996})
	tomb.Strings = map[int32]string{h("ownerName"): "Thordis"}
	unknown := &save.ZDO{Prefab: 12345}
	for _, zz := range []*save.ZDO{p1, p2, p3, bed, goblinBed, wolf, wildWolf, tomb, unknown} {
		e.Add(zz)
	}
	w := &save.World{
		Format: save.FormatChunked, SaveID: "chunked:9", NetTime: 1800*278 + 5, ZDOCount: 9,
		Meta: save.Meta{Name: "MuleVikings", SeedName: "FjordSeed", Seed: -1032944128, GenVersion: 2,
			StartingKeys: []string{"teleportall", "preset combat_default:portals_casual", "teleportall"}},
		Zones:      [][2]int16{{0, 0}},
		GlobalKeys: []string{"defeated_eikthyr", "defeated_writhan"},
		Locations: []save.Location{
			{Hash: h("Eikthyrnir"), Pos: [3]float32{100, 30, 200}},
			{Hash: h("Crypt2"), Pos: [3]float32{1, 1, 1}},
			{Hash: h("Runestone_Meadows"), Pos: [3]float32{2, 2, 2}},
		},
	}
	s := e.Finish(w, "mulevikings", time.Unix(1700000000, 0).UTC())

	kinds := map[string][]Marker{}
	for _, m := range s.Markers {
		kinds[m.Kind] = append(kinds[m.Kind], m)
	}
	if len(kinds["portal"]) != 3 || len(kinds["bed"]) != 1 || len(kinds["tame"]) != 1 || len(kinds["tombstone"]) != 1 {
		t.Fatalf("markers = %+v", s.Markers)
	}
	pa, pb, pc := kinds["portal"][0], kinds["portal"][1], kinds["portal"][2]
	if pa.Pair != pb.ID || pb.Pair != pa.ID || pc.Pair != "" || pa.Label != "Trader" {
		t.Fatalf("portal pairing = %+v %+v %+v", pa, pb, pc)
	}
	if tm := kinds["tame"][0]; tm.Species != "Wolf" || tm.Label != "Skollr" {
		t.Fatalf("tame = %+v", tm)
	}
	if kinds["bed"][0].Owner != "Sigrun" || kinds["tombstone"][0].Owner != "Thordis" {
		t.Fatal("owners not extracted")
	}
	if len(s.Players) != 1 || s.Players[0].ID != 1000000099 || s.Players[0].Name != "Sigrun" {
		t.Fatalf("players = %+v", s.Players)
	}
	if len(s.Locations) != 2 || s.Locations[0].Kind != "boss_altar" || s.Locations[0].Label != "Eikthyr" || s.Locations[1].Kind != "dungeon" {
		t.Fatalf("locations = %+v", s.Locations)
	}
	if len(s.Bosses) != 7 || !s.Bosses[0].Defeated || s.Bosses[1].Defeated {
		t.Fatalf("bosses = %+v", s.Bosses)
	}
	for _, b := range s.Bosses {
		if b.Key == "defeated_writhan" {
			t.Fatalf("writhan should not be a boss: %+v", s.Bosses)
		}
	}
	if s.World.Day != 278 || s.World.Modifiers["combat"] != "default" || s.World.Modifiers["portals"] != "casual" ||
		len(s.World.Flags) != 1 || s.World.Flags[0] != "teleportall" {
		t.Fatalf("world = %+v", s.World)
	}
	if s.Stats.UnknownPrefabs != 1 || s.Stats.ZDOs != 9 || s.ServerID != "mulevikings" || s.SaveID != "chunked:9" || s.Format != "chunked" {
		t.Fatalf("snapshot header/stats = %+v", s)
	}
}

func TestAddCopiesReusedPointer(t *testing.T) {
	e := New()
	zz := z("portal_wood", [3]float32{1, 0, 1})
	zz.Strings = map[int32]string{h("tag"): "a"}
	e.Add(zz)
	*zz = save.ZDO{Prefab: h("portal_wood"), Pos: [3]float32{9, 0, 9}, Strings: map[int32]string{h("tag"): "b"}}
	e.Add(zz)
	s := e.Finish(&save.World{}, "x", time.Now())
	if s.Markers[0].Label != "a" || s.Markers[0].X != 1 {
		t.Fatalf("first marker was overwritten: %+v", s.Markers[0])
	}
}

func TestFinishSetsSavedAtReadAtWorldVersion(t *testing.T) {
	e := New()
	savedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.FixedZone("CET", 3600))
	readAt := time.Unix(1700000000, 0).UTC()
	w := &save.World{SaveID: "chunked:9", Format: save.FormatChunked, Version: 41, SavedAt: savedAt}
	s := e.Finish(w, "mulevikings", readAt)
	if !s.SavedAt.Equal(savedAt) || s.SavedAt.Location() != time.UTC {
		t.Fatalf("SavedAt = %v, want %v in UTC", s.SavedAt, savedAt)
	}
	if !s.ReadAt.Equal(readAt) {
		t.Fatalf("ReadAt = %v, want %v", s.ReadAt, readAt)
	}
	if s.WorldVersion != 41 {
		t.Fatalf("WorldVersion = %d, want 41", s.WorldVersion)
	}
}

func TestEmptyWorldSnapshotHasNoNullSlices(t *testing.T) {
	e := New()
	s := e.Finish(&save.World{}, "x", time.Now())
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"markers", "locations", "bases", "players", "bosses", "globalKeys", "exploredZones"} {
		v, ok := raw[field]
		if !ok {
			t.Fatalf("field %q missing from snapshot JSON", field)
		}
		if string(v) == "null" {
			t.Errorf("field %q is null, want []", field)
		}
	}
	var world map[string]json.RawMessage
	if err := json.Unmarshal(raw["world"], &world); err != nil {
		t.Fatal(err)
	}
	if string(world["flags"]) == "null" {
		t.Error(`world.flags is null, want []`)
	}
}

func TestTamesExcludeTemporarySummonsButKeepHen(t *testing.T) {
	e := New()
	skeleton := z("Skeleton_Friendly", [3]float32{1, 0, 1})
	skeleton.Ints = map[int32]int32{h("tamed"): 1}
	hen := z("Hen", [3]float32{2, 0, 2})
	hen.Ints = map[int32]int32{h("tamed"): 1}
	hen.Strings = map[int32]string{h("TamedName"): "Henrietta"}
	e.Add(skeleton)
	e.Add(hen)
	s := e.Finish(&save.World{}, "x", time.Now())
	var tames []Marker
	for _, m := range s.Markers {
		if m.Kind == "tame" {
			tames = append(tames, m)
		}
	}
	if len(tames) != 1 || tames[0].Species != "Hen" {
		t.Fatalf("tames = %+v, want only Hen (Skeleton_Friendly excluded)", tames)
	}
}
