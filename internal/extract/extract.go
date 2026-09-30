package extract

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save"
)

var (
	kTag       = names.StableHash("tag")
	kOwner     = names.StableHash("owner")
	kOwnerName = names.StableHash("ownerName")
	kTamed     = names.StableHash("tamed")
	kTamedName = names.StableHash("TamedName")
	kText      = names.StableHash("text")
	kCreator   = names.StableHash("creator")
)

type Extractor struct {
	markers []Marker
	counts  map[string]int
	pieces  []piece
	players map[int64]string
	unknown map[int32]bool
}

func New() *Extractor {
	return &Extractor{counts: map[string]int{}, players: map[int64]string{}, unknown: map[int32]bool{}}
}

func (e *Extractor) mark(kind string, z *save.ZDO) *Marker {
	e.counts[kind]++
	e.markers = append(e.markers, Marker{
		ID: fmt.Sprintf("%s-%d", kind, e.counts[kind]), Kind: kind,
		X: z.Pos[0], Y: z.Pos[1], Z: z.Pos[2],
	})
	return &e.markers[len(e.markers)-1]
}

func (e *Extractor) owner(z *save.ZDO) string {
	name := z.Strings[kOwnerName]
	if id := z.Longs[kOwner]; id != 0 && name != "" {
		e.players[id] = name
	}
	return name
}

// Add inspects one ZDO. It copies what it keeps, so z may be reused.
func (e *Extractor) Add(z *save.ZDO) {
	name, ok := names.Lookup(z.Prefab)
	if !ok {
		e.unknown[z.Prefab] = true
	}
	if id := z.Longs[kCreator]; id != 0 {
		e.pieces = append(e.pieces, piece{X: z.Pos[0], Z: z.Pos[2], Creator: id})
	}
	switch {
	case strings.HasPrefix(name, "portal"):
		e.mark("portal", z).Label = z.Strings[kTag]
	case name == "bed" || strings.HasPrefix(name, "piece_bed"):
		e.mark("bed", z).Owner = e.owner(z)
	case name == "Player_tombstone":
		e.mark("tombstone", z).Owner = e.owner(z)
	case name == "sign":
		e.mark("sign", z).Label = z.Strings[kText]
	case name != "Skeleton_Friendly" && (z.Strings[kTamedName] != "" || z.Ints[kTamed] == 1):
		// Skeleton_Friendly is a temporary Raise Skeleton summon, not a
		// player-kept tame; everything else in this branch (e.g. Hen) is.
		m := e.mark("tame", z)
		m.Species, m.Label = name, z.Strings[kTamedName]
	}
}

// Finish builds the Snapshot for world w. readAt is when this process
// extracted the snapshot (Snapshot.ReadAt); the snapshot's SavedAt is the
// game's own save time, w.SavedAt.
func (e *Extractor) Finish(w *save.World, serverID string, readAt time.Time) *Snapshot {
	s := &Snapshot{
		ServerID: serverID, SaveID: w.SaveID, SavedAt: w.SavedAt.UTC(), ReadAt: readAt, Format: string(w.Format),
		WorldVersion: w.Version,
		GlobalKeys:   w.GlobalKeys, ExploredZones: w.Zones, Markers: e.markers,
		World: worldInfo(w),
		Stats: Stats{ZDOs: w.ZDOCount, Pieces: len(e.pieces), UnknownPrefabs: len(e.unknown)},
	}
	pairPortals(s.Markers)
	for i, l := range w.Locations {
		name := names.Name(l.Hash)
		kind, label := "", ""
		if v, ok := bossAltars[name]; ok {
			kind, label = "boss_altar", v
		} else if v, ok := traders[name]; ok {
			kind, label = "trader", v
		} else if v, ok := dungeons[name]; ok {
			kind, label = "dungeon", v
		} else {
			continue
		}
		s.Locations = append(s.Locations, Marker{
			ID: fmt.Sprintf("loc-%d", i+1), Kind: kind, Type: name, Label: label,
			X: l.Pos[0], Y: l.Pos[1], Z: l.Pos[2],
		})
	}
	keys := map[string]bool{}
	for _, k := range w.GlobalKeys {
		keys[k] = true
	}
	for _, b := range bossKeys {
		s.Bosses = append(s.Bosses, Boss{Key: b.Key, Name: b.Name, Defeated: keys[b.Key]})
	}
	for id, name := range e.players {
		s.Players = append(s.Players, Player{ID: id, Name: name})
	}
	sort.Slice(s.Players, func(i, j int) bool { return s.Players[i].Name < s.Players[j].Name })
	s.Bases = clusterBases(e.pieces, e.players)
	nonNilSlices(s)
	return s
}

// nonNilSlices ensures every slice field that the JSON contract promises is
// never null (an empty world/save has none of these) encodes as [] instead.
func nonNilSlices(s *Snapshot) {
	if s.GlobalKeys == nil {
		s.GlobalKeys = []string{}
	}
	if s.Bosses == nil {
		s.Bosses = []Boss{}
	}
	if s.ExploredZones == nil {
		s.ExploredZones = [][2]int16{}
	}
	if s.Locations == nil {
		s.Locations = []Marker{}
	}
	if s.Markers == nil {
		s.Markers = []Marker{}
	}
	if s.Bases == nil {
		s.Bases = []Base{}
	}
	if s.Players == nil {
		s.Players = []Player{}
	}
	if s.World.Flags == nil {
		s.World.Flags = []string{}
	}
	for i := range s.Bases {
		if s.Bases[i].Builders == nil {
			s.Bases[i].Builders = []Builder{}
		}
	}
}

func pairPortals(ms []Marker) {
	byTag := map[string][]int{}
	for i, m := range ms {
		if m.Kind == "portal" {
			byTag[m.Label] = append(byTag[m.Label], i)
		}
	}
	for _, idx := range byTag {
		if len(idx) == 2 {
			ms[idx[0]].Pair, ms[idx[1]].Pair = ms[idx[1]].ID, ms[idx[0]].ID
		}
	}
}

func worldInfo(w *save.World) WorldInfo {
	wi := WorldInfo{
		Name: w.Meta.Name, SeedName: w.Meta.SeedName, Seed: w.Meta.Seed, GenVersion: w.Meta.GenVersion,
		NetTime: w.NetTime, Day: int(w.NetTime / 1800), Modifiers: map[string]string{},
	}
	seenFlag := map[string]bool{}
	for _, k := range w.Meta.StartingKeys {
		if rest, ok := strings.CutPrefix(k, "preset "); ok {
			for _, part := range strings.Split(rest, ":") {
				if a, b, ok := strings.Cut(part, "_"); ok {
					wi.Modifiers[a] = b
				}
			}
			continue
		}
		if !seenFlag[k] {
			seenFlag[k] = true
			wi.Flags = append(wi.Flags, k)
		}
	}
	return wi
}
