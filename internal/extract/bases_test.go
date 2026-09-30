package extract

import "testing"

func grid(x0, z0 float32, n int, creator int64) []piece {
	out := make([]piece, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, piece{X: x0 + float32(i%10)*2, Z: z0 + float32(i/10)*2, Creator: creator})
	}
	return out
}

func TestClusterBases(t *testing.T) {
	var ps []piece
	ps = append(ps, grid(0, 0, 60, 1)...)       // base A: 60 pieces by 1
	ps = append(ps, grid(40, 0, 20, 2)...)      // 20 pieces 22 m east of A's edge: joins A (gap <= 32 m)
	ps = append(ps, grid(1000, 1000, 45, 3)...) // base B: 45 pieces by 3
	ps = append(ps, grid(-3000, 0, 10, 4)...)   // too small: dropped
	bases := clusterBases(ps, map[int64]string{1: "Thorgerdr"})
	if len(bases) != 2 {
		t.Fatalf("got %d bases, want 2: %+v", len(bases), bases)
	}
	a, b := bases[0], bases[1]
	if a.Pieces != 80 || a.ID != "base-1" || a.Name != "Thorgerdr's base" || len(a.Builders) != 2 || a.Builders[0].ID != 1 || a.Builders[0].Pieces != 60 {
		t.Fatalf("base A = %+v", a)
	}
	if b.Pieces != 45 || b.Name != "Base" || b.ID != "base-2" {
		t.Fatalf("base B = %+v", b)
	}
	if a.Radius <= 0 || a.X < 0 || a.X > 60 {
		t.Fatalf("base A geometry = %+v", a)
	}
}
