package worldevents

import (
	"fmt"
	"sort"
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

// portalExplored reports whether the portal with the given id (in now) is
// explored; false if there is no such portal (a dangling pair reference).
// The map hides an unexplored marker and unpairs any portal whose partner
// is unexplored, so an event must never reveal that an unexplored portal
// exists or is a pair's other end.
func portalExplored(now map[string]extract.Marker, geo Geo, id string) bool {
	m, ok := now[id]
	return ok && geo.Explored(m.X, m.Z)
}

// pairSortKey orders a portal within its pair, deterministically and
// independently of save-to-save marker ids: by label, then position.
func pairSortKey(m extract.Marker) (string, int, int) { return m.Label, round(m.X), round(m.Z) }

func pairLess(a, b extract.Marker) bool {
	al, ax, az := pairSortKey(a)
	bl, bx, bz := pairSortKey(b)
	if al != bl {
		return al < bl
	}
	if ax != bx {
		return ax < bx
	}
	return az < bz
}

// portals reports new portals (matched by tag and position, not owner:
// snapshots from agents before Plan 7 have no owner), and old portals that
// became paired with another old portal. A portal paired with a new one is
// covered by the new one's event. A pairing is only ever reported, and a
// portal only ever reported paired, once both ends are explored: the map
// unpairs a portal whose partner is unexplored, so an event must not
// reveal that an unexplored partner exists. One world_portal_paired event
// is written per pair, not one per end, using a canonical (label,
// position) ordering of the two ends for a deterministic id and body.
func (d *differ) portals() {
	before := ofKind(d.prev.Markers, "portal")
	now := ofKind(d.cur.Markers, "portal")
	byID := make(map[string]extract.Marker, len(now))
	for _, m := range now {
		byID[m.ID] = m
	}
	isNew := map[string]bool{}
	for _, m := range now {
		if _, ok := match(m, before, labelKey); !ok {
			isNew[m.ID] = true
		}
	}
	reported := map[string]bool{} // canonical pair key already written, this call
	for _, m := range now {
		if isNew[m.ID] {
			paired := m.Pair != "" && portalExplored(byID, d.geo, m.Pair)
			e := Event{ID: eventID(TypePortal, d.cur.SaveID, m.Label, round(m.X), round(m.Z)), Type: TypePortal,
				Tag: m.Label, Paired: paired, Owner: m.Owner}
			d.place(&e, m.X, m.Z)
			d.add(e)
			continue
		}
		if m.Pair == "" || isNew[m.Pair] {
			continue
		}
		p, ok := match(m, before, labelKey)
		if !ok || p.Pair != "" {
			continue // already paired before (the other end reports it)
		}
		partner, ok := byID[m.Pair]
		if !ok || !d.geo.Explored(m.X, m.Z) || !d.geo.Explored(partner.X, partner.Z) {
			continue // dangling reference, or one end isn't explored yet
		}
		a, b := m, partner
		if pairLess(b, a) {
			a, b = b, a
		}
		al, ax, az := pairSortKey(a)
		bl, bx, bz := pairSortKey(b)
		key := fmt.Sprintf("%s,%d,%d|%s,%d,%d", al, ax, az, bl, bx, bz)
		if reported[key] {
			continue
		}
		reported[key] = true
		e := Event{ID: eventID(TypePortalPaired, d.cur.SaveID, al, ax, az, bl, bx, bz), Type: TypePortalPaired,
			Tag: a.Label, Owner: a.Owner}
		d.place(&e, a.X, a.Z)
		d.add(e)
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

// basePair is a candidate match between a previous and a current base,
// within the previous one's radius plus baseLink.
type basePair struct {
	pi, ci int
	dist   float64
}

// matchBases pairs each current base with at most one previous base (the
// globally closest valid pair first, then the next closest among what's
// left, and so on), each previous base used at most once: base ids are a
// size ranking, not stable across saves, and a one-to-one match keeps a
// base that merges two old ones, or splits into two, from inflating
// growth or mis-reporting a split half as grown. It returns, per current
// base index, the matched previous base's index (ok false if unmatched).
func matchBases(prev, cur []extract.Base) map[int]int {
	var candidates []basePair
	for pi := range prev {
		p := &prev[pi]
		for ci := range cur {
			c := &cur[ci]
			dd := dist(p.X, p.Z, c.X, c.Z)
			if dd <= float64(p.Radius)+baseLink {
				candidates = append(candidates, basePair{pi, ci, dd})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].dist < candidates[j].dist })
	usedPrev := map[int]bool{}
	matched := map[int]int{}
	for _, c := range candidates {
		if usedPrev[c.pi] {
			continue
		}
		if _, ok := matched[c.ci]; ok {
			continue
		}
		usedPrev[c.pi] = true
		matched[c.ci] = c.pi
	}
	return matched
}

// bases reports a current base with no matched previous base as new, and
// a matched one that grew by GrowthMin or more pieces as grown.
func (d *differ) bases() {
	matched := matchBases(d.prev.Bases, d.cur.Bases)
	for ci := range d.cur.Bases {
		b := &d.cur.Bases[ci]
		var builders []string
		for _, bl := range b.Builders {
			if bl.Name != "" {
				builders = append(builders, bl.Name)
			}
		}
		pi, ok := matched[ci]
		if !ok {
			e := Event{ID: eventID(TypeBaseNew, d.cur.SaveID, round(b.X), round(b.Z)), Type: TypeBaseNew,
				Name: b.Name, Pieces: b.Pieces, Builders: builders}
			d.place(&e, b.X, b.Z)
			d.add(e)
			continue
		}
		best := &d.prev.Bases[pi]
		if b.Pieces-best.Pieces >= GrowthMin {
			e := Event{ID: eventID(TypeBaseGrew, d.cur.SaveID, round(b.X), round(b.Z)), Type: TypeBaseGrew,
				Name: b.Name, Pieces: b.Pieces, Grew: b.Pieces - best.Pieces, Builders: builders}
			d.place(&e, b.X, b.Z)
			d.add(e)
		}
	}
}

// bossesOf returns the bosses snap shows, as cardBosses does: from its
// global keys (extract.BossesFromKeys), or, for a snapshot stored before
// global keys were sent, from its own boss list.
func bossesOf(snap *extract.Snapshot) []extract.Boss {
	if snap.GlobalKeys != nil {
		return extract.BossesFromKeys(snap.GlobalKeys)
	}
	return snap.Bosses
}

// defeated is the set of bosses snap shows defeated.
func defeated(snap *extract.Snapshot) map[string]bool {
	out := map[string]bool{}
	for _, b := range bossesOf(snap) {
		if b.Defeated {
			out[b.Key] = true
		}
	}
	return out
}

// bosses applies the same fallback to cur as to prev (bossesOf), so a
// boss defeated between two snapshots that both predate global keys is
// not lost: without it, only prev falls back to its own boss list, and
// cur's always-nil GlobalKeys would make BossesFromKeys(nil) report none
// defeated.
func (d *differ) bosses() {
	was := defeated(d.prev)
	for _, b := range bossesOf(d.cur) {
		if b.Defeated && !was[b.Key] {
			d.add(Event{ID: eventID(TypeBoss, d.cur.SaveID, b.Key), Type: TypeBoss, Boss: b.Name})
		}
	}
}

// Near names the known location (boss altar, trader or dungeon) nearest
// to (x, z) within NearRadius, in explored ground only, for "near …"
// wording: "a sunken crypt", "burial chambers", "Haldor", "Moder’s altar".
// "" if there is none, or if (x, z) itself isn't explored: an event in
// unexplored ground must not read as being near a landmark the map
// doesn't show there, even when that landmark is itself explored and
// within range. The caller falls back to the biome.
func Near(locs []extract.Marker, geo Geo, x, z float32) string {
	if !geo.Explored(x, z) {
		return ""
	}
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
