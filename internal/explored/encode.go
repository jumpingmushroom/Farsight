package explored

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Encoded is a mask as it travels in snapshots and the snapshot API:
// the bitset (Mask.Bits) gzip'd and then base64'd (standard alphabet,
// padded).
type Encoded struct {
	Source string `json:"source"` // SourceTables or SourceZones
	Cell   int    `json:"cell"`   // CellMetres
	Size   int    `json:"size"`   // Size
	Bits   string `json:"bits"`
}

// Encode packs m, recording where it came from.
func Encode(m *Mask, source string) Encoded {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(m.bits) // a bytes.Buffer never fails
	zw.Close()
	return Encoded{Source: source, Cell: CellMetres, Size: Size, Bits: base64.StdEncoding.EncodeToString(buf.Bytes())}
}

// Decode unpacks e, rejecting anything but this grid's exact encoding.
func Decode(e Encoded) (*Mask, error) {
	if e.Source != SourceTables && e.Source != SourceZones {
		return nil, fmt.Errorf("explored: unknown source %q", e.Source)
	}
	if e.Cell != CellMetres || e.Size != Size {
		return nil, fmt.Errorf("explored: grid %d×%d at %d m, want %d×%d at %d m", e.Size, e.Size, e.Cell, Size, Size, CellMetres)
	}
	gz, err := base64.StdEncoding.DecodeString(e.Bits)
	if err != nil {
		return nil, fmt.Errorf("explored: bits: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("explored: bits: %w", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(io.LimitReader(zr, maskBytes+1))
	if err != nil {
		return nil, fmt.Errorf("explored: bits: %w", err)
	}
	if len(raw) != maskBytes {
		return nil, errors.New("explored: bits are not a 2048×2048 bitset")
	}
	return &Mask{bits: raw}, nil
}
