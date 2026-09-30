// Package savetest writes synthetic Valheim saves for tests.
package savetest

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

// ZDO is a test-friendly ZDO: prefab and keys are names, hashed on encode.
type ZDO struct {
	Pos     [3]float32
	Prefab  string
	Floats  map[string]float32
	Ints    map[string]int32
	Longs   map[string]int64
	Strings map[string]string
}

// EncodeZDO writes z in the record layout of the given world version
// (legacy 31-39 or chunked 40-41). Positions are always full Vec3 and
// rotation is never written.
func EncodeZDO(w *zpkg.Writer, version int32, z ZDO) {
	var flags uint16
	if len(z.Floats) > 0 {
		flags |= 0x02
	}
	if len(z.Ints) > 0 {
		flags |= 0x10
	}
	if len(z.Longs) > 0 {
		flags |= 0x20
	}
	if len(z.Strings) > 0 {
		flags |= 0x40
	}
	w.U16(flags)
	if version < 40 {
		w.Vec2s([2]int16{0, 0})
	}
	w.Vec3(z.Pos)
	w.I32(names.StableHash(z.Prefab))
	count := func(n int) {
		if version < 33 {
			w.U8(uint8(n))
		} else {
			w.NumItems(n)
		}
	}
	if len(z.Floats) > 0 {
		count(len(z.Floats))
		for k, v := range z.Floats {
			w.I32(names.StableHash(k))
			w.F32(v)
		}
	}
	if len(z.Ints) > 0 {
		count(len(z.Ints))
		for k, v := range z.Ints {
			w.I32(names.StableHash(k))
			w.I32(v)
		}
	}
	if len(z.Longs) > 0 {
		count(len(z.Longs))
		for k, v := range z.Longs {
			w.I32(names.StableHash(k))
			w.I64(v)
		}
	}
	if len(z.Strings) > 0 {
		count(len(z.Strings))
		for k, v := range z.Strings {
			w.I32(names.StableHash(k))
			w.Str(v)
		}
	}
}

type Location struct {
	Name string
	Pos  [3]float32
}

func writeFile(t testing.TB, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func fwl(version int32, name, seedName string) []byte {
	var p zpkg.Writer
	p.I32(version)
	p.Str(name)
	p.Str(seedName)
	p.I32(names.StableHash(seedName))
	p.I64(1)
	p.I32(2)     // world gen version
	p.Bool(true) // needsDB
	p.I32(1)
	p.Str("preset combat_default:deathpenalty_default:resources_default:raids_default:portals_casual")
	if version >= 41 {
		p.I32(0) // empty player roster
	}
	var out zpkg.Writer
	out.ByteArray(p.Bytes())
	return out.Bytes()
}

// WriteChunkedWorld writes a complete 1.0 chunked save <worldsDir>/<worldName>/_main.<n>.*
// with every ZDO in one chunk (1e_1e, size 1, version 1).
func WriteChunkedWorld(t testing.TB, worldsDir, worldName string, n int, seedName string,
	zdos []ZDO, zones [][2]int16, keys []string, locs []Location) {
	t.Helper()
	dir := filepath.Join(worldsDir, worldName)
	base := filepath.Join(dir, fmt.Sprintf("_main.%d", n))

	var chunk zpkg.Writer
	chunk.I16(41)
	chunk.I32(int32(len(zdos)))
	for _, z := range zdos {
		EncodeZDO(&chunk, 41, z)
	}
	writeFile(t, filepath.Join(dir, "1e_1e__1_1.chunk"), chunk.Bytes())

	var idx zpkg.Writer
	idx.I16(41)
	idx.I32(int32(len(zdos)))
	idx.I32(1)
	idx.U16(0x1e1e)
	idx.U8(1)
	idx.U32(1)
	idx.I32(int32(len(zdos)))
	writeFile(t, base+".chunks", idx.Bytes())

	var zp zpkg.Writer
	zp.I32(int32(len(zones)))
	for _, z := range zones {
		zp.Vec2s(z)
	}
	zp.I32(32)
	zp.I32(int32(len(keys)))
	for _, k := range keys {
		zp.Str(k)
	}
	zp.Bool(true)
	zp.I32(int32(len(locs)))
	for _, l := range locs {
		zp.I32(names.StableHash(l.Name))
		zp.Vec3(l.Pos)
		zp.Bool(true)
	}
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	gw.Write(zp.Bytes())
	gw.Close()
	var db2 zpkg.Writer
	db2.I32(41)
	db2.F64(1800 * 10.5)
	db2.ByteArray(gz.Bytes())
	writeFile(t, base+".db2", db2.Bytes())

	writeFile(t, base+".fwl2", fwl(41, worldName, seedName))
	var ok zpkg.Writer
	ok.I32(41)
	writeFile(t, base+".ok", ok.Bytes())
}

// WriteLegacyWorld writes <worldsDir>/<worldName>.db and .fwl at version 37.
func WriteLegacyWorld(t testing.TB, worldsDir, worldName, seedName string,
	zdos []ZDO, zones [][2]int16, keys []string, locs []Location) {
	t.Helper()
	var db zpkg.Writer
	db.I32(37)
	db.F64(1800 * 42.25)
	db.I64(0)
	db.U32(0)
	db.I32(int32(len(zdos)))
	for _, z := range zdos {
		EncodeZDO(&db, 37, z)
	}
	db.I32(int32(len(zones)))
	for _, z := range zones {
		db.I32(int32(z[0]))
		db.I32(int32(z[1]))
	}
	db.I32(0)  // pgw
	db.I32(32) // location version
	db.I32(int32(len(keys)))
	for _, k := range keys {
		db.Str(k)
	}
	db.Bool(true)
	db.I32(int32(len(locs)))
	for _, l := range locs {
		db.Str(l.Name)
		db.Vec3(l.Pos)
		db.Bool(true)
	}
	db.I32(0) // random events: ignored by the reader
	writeFile(t, filepath.Join(worldsDir, worldName+".db"), db.Bytes())
	writeFile(t, filepath.Join(worldsDir, worldName+".fwl"), fwl(37, worldName, seedName))
}
