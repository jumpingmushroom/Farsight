// Package fog draws the atlas's fog of war into map tiles: a signed
// distance field over the explored mask, a smooth alpha band just inside
// the explored edge, and the parchment texture the browser used to paint
// (spec 2026-10-01-fog-tiles-design.md).
package fog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/jumpingmushroom/farsight/internal/explored"
)

// StyleVersion identifies the fog's look and maths: the texture, the alpha
// curve, the edge width and the field. Bump it whenever a change would
// alter a composed tile's pixels. It is part of the fog key, so browsers
// and the tile cache then fetch every fog tile afresh.
const StyleVersion = 1

const (
	// MaxZoom is the highest zoom fog tiles are served at (terrain has
	// z0–z5; z6 terrain is its z5 parent upscaled).
	MaxZoom = 6
	// TileSize is a tile's side in pixels.
	TileSize = 256

	worldRadius = explored.WorldRadius
	worldSpan   = 2 * worldRadius
	half        = explored.Size / 2
	cellMetres  = float64(explored.CellMetres)

	maxEdge    = 57.6 // metres: the widest soft band
	edgePixels = 24   // the band's width in pixels, until maxEdge caps it
)

// Key is the fog key for a mask: the first 16 hex digits of SHA-256 over
// its source, grid size, bitset and StyleVersion. It changes only when
// exploration (or the fog style) changes, not on every save.
func Key(source string, m *explored.Mask) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00", source, explored.Size)
	h.Write(m.Bits())
	fmt.Fprintf(h, "\x00%d", StyleVersion)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// MetresPerPixel is a tile pixel's side at zoom z: the 21 km world square
// spans TileSize·2^z pixels.
func MetresPerPixel(z int) float64 { return worldSpan / TileSize / float64(int(1)<<z) }

// EdgeWidth is the soft band's width at zoom z: 24 pixels, at most 57.6 m.
func EdgeWidth(z int) float64 { return min(maxEdge, edgePixels*MetresPerPixel(z)) }

// Alpha is the fog's opacity at signed distance d (metres, positive inside
// the explored area) for band width w: 1 − smoothstep(0, w, d). It is
// exactly 1 at and beyond the explored edge (d <= 0) and 0 from w inwards.
func Alpha(d, w float64) float64 {
	t := d / w
	if t <= 0 {
		return 1
	}
	if t >= 1 {
		return 0
	}
	return 1 - t*t*(3-2*t)
}

// pixelCell is the grid position (in cells, cell centres on integers)
// under the centre of global pixel (gx, gy) at zoom z.
func pixelCell(z, gx, gy int) (u, v float64) {
	mpp := MetresPerPixel(z)
	wx := -worldRadius + (float64(gx)+0.5)*mpp
	wz := worldRadius - (float64(gy)+0.5)*mpp
	return wx/cellMetres + half, wz/cellMetres + half
}
