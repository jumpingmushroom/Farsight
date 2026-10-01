// Package extract turns a stream of save ZDOs into an atlas Snapshot.
package extract

import (
	"time"

	"github.com/jumpingmushroom/farsight/internal/explored"
)

type Snapshot struct {
	ServerID string `json:"serverId"`
	SaveID   string `json:"saveId"`
	// SavedAt is the game's own save time (save.World.SavedAt); ReadAt is
	// when this process extracted the snapshot from that save.
	SavedAt       time.Time  `json:"savedAt"`
	ReadAt        time.Time  `json:"readAt"`
	Format        string     `json:"format"`
	WorldVersion  int32      `json:"worldVersion"`
	World         WorldInfo  `json:"world"`
	GlobalKeys    []string   `json:"globalKeys"`
	Bosses        []Boss     `json:"bosses"`
	ExploredZones [][2]int16 `json:"exploredZones"`
	// Explored is the 12 m explored mask (cartography tables, or shrunk
	// zones, plus 100 m around built pieces). Nil in snapshots from agents
	// that predate it; ExploredZones stays for central apps that predate it.
	Explored  *explored.Encoded `json:"explored,omitempty"`
	Locations []Marker          `json:"locations"`
	Markers   []Marker          `json:"markers"`
	Bases     []Base            `json:"bases"`
	Players   []Player          `json:"players"`
	Stats     Stats             `json:"stats"`
}

type WorldInfo struct {
	Name       string            `json:"name"`
	SeedName   string            `json:"seedName"`
	Seed       int32             `json:"seed"`
	GenVersion int32             `json:"genVersion"`
	NetTime    float64           `json:"netTime"`
	Day        int               `json:"day"`
	Modifiers  map[string]string `json:"modifiers"`
	Flags      []string          `json:"flags"`
}

type Boss struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Defeated bool   `json:"defeated"`
}

type Marker struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	X       float32 `json:"x"`
	Y       float32 `json:"y"`
	Z       float32 `json:"z"`
	Label   string  `json:"label,omitempty"`
	Owner   string  `json:"owner,omitempty"`
	Species string  `json:"species,omitempty"`
	Type    string  `json:"type,omitempty"`
	Pair    string  `json:"pair,omitempty"`
}

type Base struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	X        float32   `json:"x"`
	Z        float32   `json:"z"`
	Radius   float32   `json:"radius"`
	Pieces   int       `json:"pieces"`
	Builders []Builder `json:"builders"`
}

type Builder struct {
	ID     int64  `json:"id"`
	Name   string `json:"name,omitempty"`
	Pieces int    `json:"pieces"`
}

type Player struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Stats struct {
	ZDOs           int `json:"zdos"`
	Pieces         int `json:"pieces"`
	UnknownPrefabs int `json:"unknownPrefabs"`
}
