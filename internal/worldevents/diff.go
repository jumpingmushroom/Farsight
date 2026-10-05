package worldevents

import (
	"strings"

	"github.com/jumpingmushroom/farsight/internal/extract"
)

// Diff returns the events between two consecutive saves of one world, in a
// stable order: tombstones, portals, tames, bases, bosses. prev nil (the
// first save seen) yields none: everything in it predates tracking. Place
// wording comes from geo, which describes cur's world.
func Diff(prev, cur *extract.Snapshot, geo Geo) []Event {
	if prev == nil {
		return nil
	}
	d := differ{prev: prev, cur: cur, geo: geo}
	d.tombstones()
	d.portals()
	d.tames()
	d.bases()
	d.bosses()
	return d.out
}

type differ struct {
	prev, cur *extract.Snapshot
	geo       Geo
	out       []Event
}

func (d *differ) add(e Event) {
	e.At, e.SaveID = d.cur.SavedAt, d.cur.SaveID
	d.out = append(d.out, e)
}

// place fills in the biome and nearby location for an event at (x, z).
func (d *differ) place(e *Event, x, z float32) {
	e.Pos = &Pos{X: x, Z: z}
	e.Biome = d.geo.Biome(x, z)
	e.Near = Near(d.cur.Locations, d.geo, x, z)
}

func ofKind(ms []extract.Marker, kind string) []extract.Marker {
	var out []extract.Marker
	for _, m := range ms {
		if m.Kind == kind {
			out = append(out, m)
		}
	}
	return out
}

// match returns the marker in prev that is the same object as m: same
// key, within MatchRadius.
func match(m extract.Marker, prev []extract.Marker, key func(extract.Marker) string) (extract.Marker, bool) {
	for _, p := range prev {
		if key(p) == key(m) && dist(p.X, p.Z, m.X, m.Z) <= MatchRadius {
			return p, true
		}
	}
	return extract.Marker{}, false
}

func ownerKey(m extract.Marker) string { return m.Owner }
func labelKey(m extract.Marker) string { return m.Label }

// NewTombstones returns cur's tombstones with no match (same owner, within
// MatchRadius) in prev; all of them when prev is nil.
func NewTombstones(prev, cur *extract.Snapshot) []extract.Marker {
	var before []extract.Marker
	if prev != nil {
		before = ofKind(prev.Markers, "tombstone")
	}
	var out []extract.Marker
	for _, m := range ofKind(cur.Markers, "tombstone") {
		if _, ok := match(m, before, ownerKey); !ok {
			out = append(out, m)
		}
	}
	return out
}

func (d *differ) tombstones() {
	for _, m := range NewTombstones(d.prev, d.cur) {
		e := Event{ID: eventID(TypeTombstone, d.cur.SaveID, m.Owner, round(m.X), round(m.Z)), Type: TypeTombstone, Owner: m.Owner}
		d.place(&e, m.X, m.Z)
		d.add(e)
	}
}

// portals reports new portals (matched by tag and position, not owner:
// snapshots from agents before Plan 7 have no owner), and old portals that
// became paired with another old portal. A portal paired with a new one is
// covered by the new one's event.
func (d *differ) portals() {
	before := ofKind(d.prev.Markers, "portal")
	now := ofKind(d.cur.Markers, "portal")
	isNew := map[string]bool{}
	for _, m := range now {
		if _, ok := match(m, before, labelKey); !ok {
			isNew[m.ID] = true
		}
	}
	for _, m := range now {
		if isNew[m.ID] {
			e := Event{ID: eventID(TypePortal, d.cur.SaveID, m.Label, round(m.X), round(m.Z)), Type: TypePortal,
				Tag: m.Label, Paired: m.Pair != "", Owner: m.Owner}
			d.place(&e, m.X, m.Z)
			d.add(e)
			continue
		}
		p, _ := match(m, before, labelKey)
		if p.Pair == "" && m.Pair != "" && !isNew[m.Pair] {
			e := Event{ID: eventID(TypePortalPaired, d.cur.SaveID, m.Label, round(m.X), round(m.Z)), Type: TypePortalPaired,
				Tag: m.Label, Owner: m.Owner}
			d.place(&e, m.X, m.Z)
			d.add(e)
		}
	}
}

// tames reports named tames. Tames walk about, so they are counted per
// (species, name) rather than matched by position; a key with more tames
// than before reports its extra ones (the last in save order). Unnamed
// tames (bred or freshly tamed animals) are not reported.
func (d *differ) tames() {
	key := func(m extract.Marker) string { return m.Species + "\x00" + m.Label }
	had := map[string]int{}
	for _, m := range ofKind(d.prev.Markers, "tame") {
		if m.Label != "" {
			had[key(m)]++
		}
	}
	byKey := map[string][]extract.Marker{}
	var order []string
	for _, m := range ofKind(d.cur.Markers, "tame") {
		if m.Label == "" {
			continue
		}
		k := key(m)
		if byKey[k] == nil {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], m)
	}
	for _, k := range order {
		ms := byKey[k]
		for i := had[k]; i < len(ms); i++ {
			m := ms[i]
			e := Event{ID: eventID(TypeTame, d.cur.SaveID, m.Species, m.Label, i), Type: TypeTame,
				Name: m.Label, Species: m.Species, Namer: m.Namer}
			d.place(&e, m.X, m.Z)
			d.add(e)
		}
	}
}

// bases matches each base to the previous base whose centre is nearest,
// among those within that base's radius plus baseLink: base ids are a
// size ranking, not stable across saves. Unmatched bases are new; matched
// ones that grew by GrowthMin or more pieces are reported.
func (d *differ) bases() {
	for _, b := range d.cur.Bases {
		var best *extract.Base
		bestD := 0.0
		for i := range d.prev.Bases {
			p := &d.prev.Bases[i]
			dd := dist(p.X, p.Z, b.X, b.Z)
			if dd <= float64(p.Radius)+baseLink && (best == nil || dd < bestD) {
				best, bestD = p, dd
			}
		}
		var builders []string
		for _, bl := range b.Builders {
			if bl.Name != "" {
				builders = append(builders, bl.Name)
			}
		}
		switch {
		case best == nil:
			e := Event{ID: eventID(TypeBaseNew, d.cur.SaveID, round(b.X), round(b.Z)), Type: TypeBaseNew,
				Name: b.Name, Pieces: b.Pieces, Builders: builders}
			d.place(&e, b.X, b.Z)
			d.add(e)
		case b.Pieces-best.Pieces >= GrowthMin:
			e := Event{ID: eventID(TypeBaseGrew, d.cur.SaveID, round(b.X), round(b.Z)), Type: TypeBaseGrew,
				Name: b.Name, Pieces: b.Pieces, Grew: b.Pieces - best.Pieces, Builders: builders}
			d.place(&e, b.X, b.Z)
			d.add(e)
		}
	}
}

// defeated is the set of bosses snap shows defeated: from its global keys
// (extract.BossesFromKeys), or, for a snapshot stored before global keys
// were sent, from its own boss list.
func defeated(snap *extract.Snapshot) map[string]bool {
	out := map[string]bool{}
	bosses := snap.Bosses
	if snap.GlobalKeys != nil {
		bosses = extract.BossesFromKeys(snap.GlobalKeys)
	}
	for _, b := range bosses {
		if b.Defeated {
			out[b.Key] = true
		}
	}
	return out
}

func (d *differ) bosses() {
	was := defeated(d.prev)
	for _, b := range extract.BossesFromKeys(d.cur.GlobalKeys) {
		if b.Defeated && !was[b.Key] {
			d.add(Event{ID: eventID(TypeBoss, d.cur.SaveID, b.Key), Type: TypeBoss, Boss: b.Name})
		}
	}
}

// Near names the known location (boss altar, trader or dungeon) nearest
// to (x, z) within NearRadius, in explored ground only, for "near …"
// wording: "a sunken crypt", "burial chambers", "Haldor", "Moder’s altar".
// "" if there is none.
func Near(locs []extract.Marker, geo Geo, x, z float32) string {
	var best *extract.Marker
	bestD := 0.0
	for i := range locs {
		l := &locs[i]
		dd := dist(l.X, l.Z, x, z)
		if dd > NearRadius || !geo.Explored(l.X, l.Z) {
			continue
		}
		if best == nil || dd < bestD {
			best, bestD = l, dd
		}
	}
	if best == nil {
		return ""
	}
	switch best.Kind {
	case "trader":
		return best.Label
	case "boss_altar":
		return best.Label + "’s altar"
	}
	name := strings.ToLower(best.Label)
	if strings.HasSuffix(name, "s") { // "burial chambers"
		return name
	}
	return "a " + name
}
