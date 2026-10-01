package save

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
)

func TestReadLegacySynthetic(t *testing.T) {
	dir := t.TempDir()
	zdos := []savetest.ZDO{
		{Pos: [3]float32{-2182.46, 33.2, 1996.27}, Prefab: "Player_tombstone",
			Longs: map[string]int64{"owner": 100000077}, Strings: map[string]string{"ownerName": "Thordis"}},
		{Pos: [3]float32{5, 6, 7}, Prefab: "bed"},
	}
	savetest.WriteLegacyWorld(t, dir, "Mulennials", "Qm4RtX8vLcL", zdos,
		[][2]int16{{-3, 4}}, []string{"defeated_bonemass"},
		[]savetest.Location{{Name: "Bonemass", Pos: [3]float32{1, 2, 3}}})

	db := filepath.Join(dir, "Mulennials.db")
	var got []ZDO
	w, err := readLegacy(db, filepath.Join(dir, "Mulennials.fwl"), ReadOptions{}, func(z *ZDO) { got = append(got, *z) })
	if err != nil {
		t.Fatal(err)
	}
	if w.Format != FormatLegacy || w.Version != 37 || w.ZDOCount != 2 || len(got) != 2 {
		t.Fatalf("world = %+v", w)
	}
	if got[0].Strings[names.StableHash("ownerName")] != "Thordis" || got[0].Pos[0] != -2182.46 {
		t.Fatalf("zdo = %+v", got[0])
	}
	if w.Meta.SeedName != "Qm4RtX8vLcL" || w.Zones[0] != [2]int16{-3, 4} ||
		w.GlobalKeys[0] != "defeated_bonemass" || names.Name(w.Locations[0].Hash) != "Bonemass" {
		t.Fatalf("world = %+v", w)
	}
	if !strings.HasPrefix(w.SaveID, "legacy:") {
		t.Fatalf("SaveID = %q", w.SaveID)
	}
}

func TestLegacyRejectsOldVersion(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Old.db"), []byte{30, 0, 0, 0}, 0o644)
	savetest.WriteLegacyWorld(t, dir, "Tmp", "x", nil, nil, nil, nil) // just for a valid .fwl
	_, err := readLegacy(filepath.Join(dir, "Old.db"), filepath.Join(dir, "Tmp.fwl"), ReadOptions{}, func(*ZDO) {})
	if err == nil || !strings.Contains(err.Error(), ErrUnsupportedVersion.Error()) {
		t.Fatalf("err = %v, want unsupported version", err)
	}
}

func TestLegacySaveIDInProgress(t *testing.T) {
	dir := t.TempDir()
	savetest.WriteLegacyWorld(t, dir, "W", "x", nil, nil, nil, nil)
	db := filepath.Join(dir, "W.db")
	if _, _, err := legacySaveID(db); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(db+".new", []byte("partial"), 0o644)
	if _, _, err := legacySaveID(db); err != ErrSaveInProgress {
		t.Fatalf("err = %v, want ErrSaveInProgress", err)
	}
}

func TestReadLegacySavedAtIsDBFileMTime(t *testing.T) {
	dir := t.TempDir()
	savetest.WriteLegacyWorld(t, dir, "W", "x", []savetest.ZDO{{Prefab: "bed"}}, nil, nil, nil)
	dbPath := filepath.Join(dir, "W.db")
	fi, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	w, err := readLegacy(dbPath, filepath.Join(dir, "W.fwl"), ReadOptions{}, func(*ZDO) {})
	if err != nil {
		t.Fatal(err)
	}
	if !w.SavedAt.Equal(fi.ModTime()) {
		t.Fatalf("SavedAt = %v, want .db mtime %v", w.SavedAt, fi.ModTime())
	}
}
