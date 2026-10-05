// Package worldevents turns the differences between two consecutive world
// saves into timeline events (a new tombstone, portal, tame or base, a
// base that grew, a boss defeated) and keeps the event log in step with
// the stored snapshots: Diff is the pure comparison, Deriver replays
// stored snapshots through it.
package worldevents

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"
)

// Event types, as stored in the events table's type column.
const (
	TypeTombstone    = "world_tombstone"
	TypePortal       = "world_portal"
	TypePortalPaired = "world_portal_paired"
	TypeTame         = "world_tame"
	TypeBaseNew      = "world_base_new"
	TypeBaseGrew     = "world_base_grew"
	TypeBoss         = "world_boss"
)

const (
	// GrowthMin is the smallest growth, in pieces, reported for a base.
	GrowthMin = 25
	// MatchRadius is how close (metres) a tombstone or portal must be to
	// one with the same owner or tag in the previous save to be the same
	// object. Neither moves; the slack absorbs float noise.
	MatchRadius = 4
	// NearRadius is how close (metres) a known location must be to be
	// named in an event's place ("near a sunken crypt").
	NearRadius = 300
	// baseLink is the slack (metres) beyond a previous base's radius
	// within which a base's centre is taken to be the same base.
	baseLink = 32
)

// Pos is a world position (x east, z north), in metres.
type Pos struct {
	X float32 `json:"x"`
	Z float32 `json:"z"`
}

// Event is one world-save event. At is the save's time, never the moment
// it happened. Only the fields its type uses are set.
type Event struct {
	ID     string    `json:"id"`
	Type   string    `json:"type"`
	At     time.Time `json:"at"`
	SaveID string    `json:"saveId"`
	Pos    *Pos      `json:"pos,omitempty"`

	Owner    string   `json:"owner,omitempty"`    // tombstone owner; portal creator (name)
	Namer    string   `json:"namer,omitempty"`    // tame namer (platform user ID, "Steam_…")
	Builders []string `json:"builders,omitempty"` // base builders (names)

	Tag     string `json:"tag,omitempty"`     // portal tag
	Paired  bool   `json:"paired,omitempty"`  // world_portal: paired in the save it appeared in
	Name    string `json:"name,omitempty"`    // tame or base name
	Species string `json:"species,omitempty"` // tame species (prefab name)
	Pieces  int    `json:"pieces,omitempty"`  // base pieces now
	Grew    int    `json:"grew,omitempty"`    // world_base_grew: pieces added
	Boss    string `json:"boss,omitempty"`    // world_boss: the boss's name

	Biome string `json:"biome,omitempty"` // display name: "Swamp", "Black Forest"…
	Near  string `json:"near,omitempty"`  // a known location within NearRadius: "a sunken crypt"
}

// eventID is the deterministic id of the typ event for the object named by
// key in save saveID, so replaying a save pair writes the same ids.
func eventID(typ, saveID string, key ...any) string {
	parts := []string{typ, saveID}
	for _, k := range key {
		parts = append(parts, fmt.Sprint(k))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "w" + hex.EncodeToString(sum[:8])
}

// round is a coordinate rounded to whole metres, for ids.
func round(v float32) int { return int(math.Round(float64(v))) }

func dist(ax, az, bx, bz float32) float64 {
	return math.Hypot(float64(ax-bx), float64(az-bz))
}
