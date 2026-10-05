package extract

// NearPhrase is m's "near …" wording (e.g. "a sunken crypt", "Haldor",
// "the Forge of Potential"), looked up by m.Type (the raw prefab name),
// which every location carries whether or not m has been classified yet.
// "" for a prefab not in the table.
func NearPhrase(m Marker) string {
	return locationTable[m.Type].Near
}

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
