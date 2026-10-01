package save

import (
	"bytes"
	"compress/gzip"
	"path/filepath"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

func TestDecodeMapDataVersions(t *testing.T) {
	for _, ver := range []int32{2, 3} {
		flags, err := DecodeMapData(savetest.MapData(ver, 0, 5, MapCells-1))
		if err != nil {
			t.Fatalf("v%d: %v", ver, err)
		}
		if len(flags) != MapCells {
			t.Fatalf("v%d: %d flags, want %d", ver, len(flags), MapCells)
		}
		n := 0
		for _, f := range flags {
			if f != 0 {
				n++
			}
		}
		if n != 3 || flags[0] != 1 || flags[5] != 1 || flags[MapCells-1] != 1 {
			t.Fatalf("v%d: %d explored, flags[0,5,last] = %d %d %d", ver, n, flags[0], flags[5], flags[MapCells-1])
		}
	}
}

// mapBlob gzips a hand-built header and cells.
func mapBlob(version, n int32, cells []byte) []byte {
	var p zpkg.Writer
	p.I32(version)
	p.I32(n)
	p.Raw(cells)
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	gw.Write(p.Bytes())
	gw.Close()
	return gz.Bytes()
}

func TestDecodeMapDataRejectsBadInput(t *testing.T) {
	good := savetest.MapData(3, 1)
	corrupt := bytes.Clone(good)
	for i := 10; i < len(corrupt); i++ {
		corrupt[i] = 0xFF // BFINAL=1, BTYPE=11: a reserved (invalid) deflate block
	}
	cases := map[string][]byte{
		"version 1":   mapBlob(1, MapCells, make([]byte, MapCells)),
		"wrong n":     mapBlob(3, 100, make([]byte, 100)),
		"short cells": mapBlob(3, MapCells, make([]byte, 1000)),
		"truncated":   good[:len(good)/2],
		"not gzip":    []byte("not a gzip stream"),
		"corrupt":     corrupt,
		"empty":       nil,
	}
	for name, b := range cases {
		if _, err := DecodeMapData(b); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestReadWithKeepsBytesOnlyForChosenPrefabs(t *testing.T) {
	blob := savetest.MapData(3, 42)
	zdos := []savetest.ZDO{
		{Prefab: "piece_cartographytable", ByteArrays: map[string][]byte{"data": blob}},
		{Prefab: "sign", Strings: map[string]string{"text": "hi"}, ByteArrays: map[string][]byte{"data": {1, 2, 3}}},
	}
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, filepath.Join(dir, "c"), "W", 1, "x", zdos, nil, nil, nil)
	savetest.WriteLegacyWorld(t, filepath.Join(dir, "l"), "W", "x", zdos, nil, nil, nil)
	keep := ReadOptions{KeepBytes: func(p int32) bool { return p == MapTablePrefab }}
	for _, sub := range []string{"c", "l"} {
		var tables, signs int
		_, err := ReadWith(filepath.Join(dir, sub), "W", keep, func(z *ZDO) {
			switch z.Prefab {
			case MapTablePrefab:
				tables++
				if !bytes.Equal(z.ByteArrays[MapDataKey], blob) {
					t.Errorf("%s: table data not kept intact (%d bytes)", sub, len(z.ByteArrays[MapDataKey]))
				}
			default:
				signs++
				if z.ByteArrays != nil || z.Strings[names.StableHash("text")] != "hi" {
					t.Errorf("%s: sign kept byte arrays %v, strings %v", sub, z.ByteArrays, z.Strings)
				}
			}
		})
		if err != nil || tables != 1 || signs != 1 {
			t.Fatalf("%s: err=%v tables=%d signs=%d", sub, err, tables, signs)
		}
		_, err = Read(filepath.Join(dir, sub), "W", func(z *ZDO) {
			if z.ByteArrays != nil {
				t.Errorf("%s: Read kept byte arrays for prefab %d", sub, z.Prefab)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
