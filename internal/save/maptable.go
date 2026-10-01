package save

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

// A cartography table (piece_cartographytable) keeps the shared map its
// players recorded in its byte array "data": a gzip-compressed ZPackage of
// int32 version (2 or 3), int32 n (MapCells), then n bytes, one bool per
// minimap cell, then pins (ignored here).
var (
	MapTablePrefab = names.StableHash("piece_cartographytable")
	MapDataKey     = names.StableHash("data")
)

// MapCells is the number of cells in the game's 2048×2048 minimap grid.
const MapCells = 2048 * 2048

// DecodeMapData decodes a table's "data" byte array into its MapCells
// explored flags (non-zero = explored), row-major from the south-west
// corner. Only the header and the cells are decompressed: the pins after
// them are never read, which also bounds memory to MapCells bytes.
func DecodeMapData(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("save: map data: %w", err)
	}
	defer zr.Close()
	head := make([]byte, 8)
	if _, err := io.ReadFull(zr, head); err != nil {
		return nil, fmt.Errorf("save: map data header: %w", err)
	}
	r := zpkg.NewReader(head)
	version, n := r.I32(), r.I32()
	if version != 2 && version != 3 {
		return nil, fmt.Errorf("save: map data version %d", version)
	}
	if n != MapCells {
		return nil, fmt.Errorf("save: map data has %d cells, want %d", n, MapCells)
	}
	flags := make([]byte, MapCells)
	if _, err := io.ReadFull(zr, flags); err != nil {
		return nil, fmt.Errorf("save: map data cells: %w", err)
	}
	return flags, nil
}
