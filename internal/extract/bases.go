package extract

import (
	"fmt"
	"math"
	"sort"
)

type piece struct {
	X, Z    float32
	Creator int64
}

const (
	cellSize  = 16
	linkCells = 2 // occupied cells within this Chebyshev distance join (<= 32 m gap)
	minPieces = 40
)

type cell struct{ X, Z int32 }

func clusterBases(ps []piece, playerNames map[int64]string) []Base {
	cells := map[cell][]int{}
	for i, p := range ps {
		c := cell{int32(math.Floor(float64(p.X) / cellSize)), int32(math.Floor(float64(p.Z) / cellSize))}
		cells[c] = append(cells[c], i)
	}
	seen := map[cell]bool{}
	var bases []Base
	for start := range cells {
		if seen[start] {
			continue
		}
		seen[start] = true
		queue := []cell{start}
		var members []int
		for len(queue) > 0 {
			c := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			members = append(members, cells[c]...)
			for dx := int32(-linkCells); dx <= linkCells; dx++ {
				for dz := int32(-linkCells); dz <= linkCells; dz++ {
					n := cell{c.X + dx, c.Z + dz}
					if _, ok := cells[n]; ok && !seen[n] {
						seen[n] = true
						queue = append(queue, n)
					}
				}
			}
		}
		if len(members) >= minPieces {
			bases = append(bases, makeBase(ps, members, playerNames))
		}
	}
	sort.Slice(bases, func(i, j int) bool {
		if bases[i].Pieces != bases[j].Pieces {
			return bases[i].Pieces > bases[j].Pieces
		}
		return bases[i].X < bases[j].X
	})
	for i := range bases {
		bases[i].ID = fmt.Sprintf("base-%d", i+1)
	}
	return bases
}

func makeBase(ps []piece, members []int, playerNames map[int64]string) Base {
	var sx, sz float64
	counts := map[int64]int{}
	for _, i := range members {
		sx += float64(ps[i].X)
		sz += float64(ps[i].Z)
		counts[ps[i].Creator]++
	}
	cx, cz := sx/float64(len(members)), sz/float64(len(members))
	var r float64
	for _, i := range members {
		r = math.Max(r, math.Hypot(float64(ps[i].X)-cx, float64(ps[i].Z)-cz))
	}
	b := Base{X: float32(cx), Z: float32(cz), Radius: float32(r), Pieces: len(members)}
	for id, n := range counts {
		b.Builders = append(b.Builders, Builder{ID: id, Name: playerNames[id], Pieces: n})
	}
	sort.Slice(b.Builders, func(i, j int) bool {
		if b.Builders[i].Pieces != b.Builders[j].Pieces {
			return b.Builders[i].Pieces > b.Builders[j].Pieces
		}
		return b.Builders[i].ID < b.Builders[j].ID
	})
	b.Name = "Base"
	if top := b.Builders[0].Name; top != "" {
		b.Name = top + "'s base"
	}
	return b
}
