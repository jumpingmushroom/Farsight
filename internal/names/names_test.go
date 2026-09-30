package names

import "testing"

func TestStableHash(t *testing.T) {
	cases := map[string]int32{
		"tag":              696029674,
		"portal_wood":      -661882940,
		"Player_tombstone": -1558312669,
		"FjordSeed":        -1032944128,
		"creator":          881008290,
		"tagauthor":        -1565613777,
		"":                 371857150, // 5381 + 5381*1566083941 wrapped to int32
	}
	for s, want := range cases {
		if got := StableHash(s); got != want {
			t.Errorf("StableHash(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestTableResolvesKnownPrefabs(t *testing.T) {
	if Count() < 6000 {
		t.Fatalf("name table has %d entries, want > 6000 (regenerate names.txt)", Count())
	}
	for _, s := range []string{"portal_wood", "Player_tombstone", "bed", "Eikthyrnir", "DN_Bossroom", "Crypt2", "Wolf", "_ZoneCtrl"} {
		if got := Name(StableHash(s)); got != s {
			t.Errorf("Name(StableHash(%q)) = %q", s, got)
		}
	}
	if _, ok := Lookup(12345); ok {
		t.Error("Lookup of unknown hash must report false")
	}
}
