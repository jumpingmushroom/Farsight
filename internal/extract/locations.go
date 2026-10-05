package extract

// ClassifyLocations applies locationTable to a snapshot's locations: kind,
// label and group from each entry's prefab (Type), so snapshots from
// agents that classified locations themselves get the same answer.
// Unmapped prefabs are dropped, and so are the unplaced candidates of
// unique sites. The result is a new, never-nil slice in input order.
func ClassifyLocations(raw []Marker) []Marker {
	// /8: most of a save's locations are unmapped (dropped below) or
	// unplaced candidates of a unique site, so the kept set is a small
	// fraction of raw; this just avoids a few reallocations, not a bound.
	out := make([]Marker, 0, len(raw)/8)
	for _, m := range raw {
		e, ok := locationTable[m.Type]
		if !ok || (e.Unique && m.Unplaced) {
			continue
		}
		m.Kind, m.Label, m.Group = e.Kind, e.Label, e.Group
		out = append(out, m)
	}
	return out
}
