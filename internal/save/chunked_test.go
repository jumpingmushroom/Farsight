package save

import (
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata/chunked", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadFWL2Fixture(t *testing.T) {
	ver, m, err := readFWL(fixture(t, "main.fwl2"))
	if err != nil {
		t.Fatal(err)
	}
	if ver != 41 || m.Name != "MuleVikings" || m.SeedName != "FjordSeed" || m.Seed != -1032944128 || m.UID != 1234567890 || m.GenVersion != 2 {
		t.Fatalf("meta = %d %+v", ver, m)
	}
	if len(m.StartingKeys) != 14 || m.StartingKeys[0] != "teleportall" {
		t.Fatalf("starting keys = %q", m.StartingKeys)
	}
}

func TestReadChunkIndexFixture(t *testing.T) {
	total, refs, err := readChunkIndex(fixture(t, "main.chunks"))
	if err != nil {
		t.Fatal(err)
	}
	if total != 476157 || len(refs) != 37 {
		t.Fatalf("total=%d refs=%d", total, len(refs))
	}
	var sum int32
	for _, r := range refs {
		sum += r.ZDOs
	}
	if sum != total {
		t.Fatalf("sum of chunk counts %d != total %d", sum, total)
	}
	if got := (chunkRef{Chunk: 0x1e20, Size: 1, Version: 317}).fileName(); got != "1e_20__1_317.chunk" {
		t.Fatalf("fileName = %q", got)
	}
}

func TestReadDB2Fixture(t *testing.T) {
	nt, zs, err := readDB2(fixture(t, "main.db2"))
	if err != nil {
		t.Fatal(err)
	}
	if nt != 501766.85744524375 || len(zs.Zones) != 5233 || len(zs.Locations) != 12298 {
		t.Fatalf("netTime=%v zones=%d locs=%d", nt, len(zs.Zones), len(zs.Locations))
	}
	if zs.Zones[0] != [2]int16{-2, -5} {
		t.Fatalf("first zone %v", zs.Zones[0])
	}
	want := []string{"activebosses 0", "defeated_eikthyr", "killedtroll", "defeated_gdking", "defeated_writhan"}
	if len(zs.GlobalKeys) != len(want) {
		t.Fatalf("keys %q", zs.GlobalKeys)
	}
	for i := range want {
		if zs.GlobalKeys[i] != want[i] {
			t.Fatalf("keys %q", zs.GlobalKeys)
		}
	}
	unknown := 0
	for _, l := range zs.Locations {
		if _, ok := names.Lookup(l.Hash); !ok {
			unknown++
		}
	}
	if unknown != 0 {
		t.Fatalf("%d locations with unknown names", unknown)
	}
}

func TestReadChunkedSynthetic(t *testing.T) {
	dir := t.TempDir()
	zdos := []savetest.ZDO{
		{Pos: [3]float32{1, 2, 3}, Prefab: "portal_wood", Strings: map[string]string{"tag": "home"}},
		{Pos: [3]float32{4, 5, 6}, Prefab: "portal_wood", Strings: map[string]string{"tag": "home"}},
	}
	savetest.WriteChunkedWorld(t, dir, "Test", 7, "abc", zdos, [][2]int16{{0, 0}, {1, 0}},
		[]string{"defeated_eikthyr"}, []savetest.Location{{Name: "Eikthyrnir", Pos: [3]float32{100, 30, 200}}})
	n, ok, err := latestChunkedSave(filepath.Join(dir, "Test"))
	if err != nil || !ok || n != 7 {
		t.Fatalf("latest = %d %v %v", n, ok, err)
	}
	var got []ZDO
	w, err := readChunked(filepath.Join(dir, "Test"), func(z *ZDO) { got = append(got, *z) })
	if err != nil {
		t.Fatal(err)
	}
	if w.Format != FormatChunked || w.SaveID != "chunked:7" || w.ZDOCount != 2 || len(got) != 2 ||
		w.Meta.SeedName != "abc" || len(w.Zones) != 2 || w.GlobalKeys[0] != "defeated_eikthyr" ||
		names.Name(w.Locations[0].Hash) != "Eikthyrnir" {
		t.Fatalf("world = %+v", w)
	}
}

func TestReadChunkedMissingChunkIsSaveChanged(t *testing.T) {
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, dir, "Test", 1, "abc",
		[]savetest.ZDO{{Prefab: "bed"}}, nil, nil, nil)
	matches, _ := filepath.Glob(filepath.Join(dir, "Test", "*.chunk"))
	for _, m := range matches {
		os.Remove(m)
	}
	if _, err := readChunked(filepath.Join(dir, "Test"), func(*ZDO) {}); err != ErrSaveChanged {
		t.Fatalf("err = %v, want ErrSaveChanged", err)
	}
}

func TestReadChunkIndexRejectsBadCounts(t *testing.T) {
	var idx zpkg.Writer
	idx.I16(41)
	idx.I32(0)
	idx.I32(-1) // negative ref count
	if _, _, err := readChunkIndex(idx.Bytes()); err == nil {
		t.Fatal("want error on negative ref count")
	}

	var idx2 zpkg.Writer
	idx2.I16(41)
	idx2.I32(0)
	idx2.I32(0x7fffffff) // huge ref count, far exceeding remaining bytes
	if _, _, err := readChunkIndex(idx2.Bytes()); err == nil {
		t.Fatal("want error on implausibly large ref count")
	}
}

func TestReadDB2RejectsBadCounts(t *testing.T) {
	dbWith := func(payload []byte) []byte {
		var gz bytes.Buffer
		gw := gzip.NewWriter(&gz)
		gw.Write(payload)
		gw.Close()
		var db2 zpkg.Writer
		db2.I32(41)
		db2.F64(0)
		db2.ByteArray(gz.Bytes())
		return db2.Bytes()
	}

	var neg zpkg.Writer
	neg.I32(-1) // negative zone count
	if _, _, err := readDB2(dbWith(neg.Bytes())); err == nil {
		t.Fatal("want error on negative zone count")
	}

	var hugeZone zpkg.Writer
	hugeZone.I32(0x7fffffff) // huge zone count
	if _, _, err := readDB2(dbWith(hugeZone.Bytes())); err == nil {
		t.Fatal("want error on implausibly large zone count")
	}

	var hugeLoc zpkg.Writer
	hugeLoc.I32(0)  // zones
	hugeLoc.I32(32) // location version
	hugeLoc.I32(0)  // global keys
	hugeLoc.Bool(true)
	hugeLoc.I32(0x7fffffff) // huge location count
	if _, _, err := readDB2(dbWith(hugeLoc.Bytes())); err == nil {
		t.Fatal("want error on implausibly large location count")
	}
}

func TestReadDB2LimitsGzipDecompression(t *testing.T) {
	// A payload whose decompressed size exceeds the 64 MiB cap must error,
	// not allocate the full amount.
	big := bytes.Repeat([]byte{0}, 65<<20)
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	gw.Write(big)
	gw.Close()
	var db2 zpkg.Writer
	db2.I32(41)
	db2.F64(0)
	db2.ByteArray(gz.Bytes())
	if _, _, err := readDB2(db2.Bytes()); err == nil {
		t.Fatal("want error when decompressed db2 payload exceeds cap")
	}
}

func TestReadChunkedSavedAtIsOkFileMTime(t *testing.T) {
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, dir, "Test", 9, "abc", []savetest.ZDO{{Prefab: "bed"}}, nil, nil, nil)
	okPath := filepath.Join(dir, "Test", "_main.9.ok")
	fi, err := os.Stat(okPath)
	if err != nil {
		t.Fatal(err)
	}
	w, err := readChunked(filepath.Join(dir, "Test"), func(*ZDO) {})
	if err != nil {
		t.Fatal(err)
	}
	if !w.SavedAt.Equal(fi.ModTime()) {
		t.Fatalf("SavedAt = %v, want .ok mtime %v", w.SavedAt, fi.ModTime())
	}
}

func TestReadChunkedRejectsUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, dir, "Test", 1, "abc", []savetest.ZDO{{Prefab: "bed"}}, nil, nil, nil)
	// Overwrite the .fwl2 with the same content but world version 42.
	fwlPath := filepath.Join(dir, "Test", "_main.1.fwl2")
	b, err := os.ReadFile(fwlPath)
	if err != nil {
		t.Fatal(err)
	}
	_, meta, err := readFWL(b)
	if err != nil {
		t.Fatal(err)
	}
	var p zpkg.Writer
	p.I32(42)
	p.Str(meta.Name)
	p.Str(meta.SeedName)
	p.I32(meta.Seed)
	p.I64(meta.UID)
	p.I32(meta.GenVersion) // world gen version
	p.Bool(true)           // needsDB
	p.I32(int32(len(meta.StartingKeys)))
	for _, k := range meta.StartingKeys {
		p.Str(k)
	}
	var out zpkg.Writer
	out.ByteArray(p.Bytes())
	if err := os.WriteFile(fwlPath, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = readChunked(filepath.Join(dir, "Test"), func(*ZDO) {})
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("err = %v, want ErrUnsupportedVersion via errors.Is", err)
	}
}
