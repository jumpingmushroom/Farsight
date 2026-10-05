package extract

import "testing"

func TestBossesFromKeysHasKallLast(t *testing.T) {
	b := BossesFromKeys([]string{"defeated_frozenking", "defeated_writhan"})
	if len(b) != 8 {
		t.Fatalf("len = %d, want 8", len(b))
	}
	last := b[len(b)-1]
	if last.Key != "defeated_frozenking" || last.Name != "Kall Fimbulbringer" || !last.Defeated {
		t.Fatalf("last = %+v, want Kall Fimbulbringer defeated", last)
	}
	for _, x := range b[:7] {
		if x.Defeated {
			t.Fatalf("%s defeated by defeated_writhan", x.Name)
		}
	}
}
