package save

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

func readFWL(b []byte) (int32, Meta, error) {
	outer := zpkg.NewReader(b)
	r := zpkg.NewReader(outer.Bytes(int(outer.I32())))
	if outer.Err() != nil {
		return 0, Meta{}, outer.Err()
	}
	var m Meta
	ver := r.I32()
	m.Name = r.Str()
	m.SeedName = r.Str()
	m.Seed = r.I32()
	m.UID = r.I64()
	if ver >= 26 {
		m.GenVersion = r.I32()
	}
	if ver >= 30 {
		r.Bool() // needsDB
	}
	if ver >= VersionGlobalKeys {
		n := int(r.I32())
		for i := 0; i < n && r.Err() == nil; i++ {
			m.StartingKeys = append(m.StartingKeys, r.Str())
		}
	}
	return ver, m, r.Err()
}

type chunkRef struct {
	Chunk   uint16
	Size    uint8
	Version uint32
	ZDOs    int32
}

func (c chunkRef) fileName() string {
	return fmt.Sprintf("%02x_%02x__%d_%d.chunk", c.Chunk>>8, c.Chunk&0xFF, c.Size, c.Version)
}

// chunkRefRecordSize is the smallest on-disk size of one chunkRef record
// (U16 + U8 + U32 + I32), used to sanity-check an untrusted count before it
// sizes an allocation.
const chunkRefRecordSize = 2 + 1 + 4 + 4

func readChunkIndex(b []byte) (int32, []chunkRef, error) {
	r := zpkg.NewReader(b)
	r.I16()
	total := r.I32()
	n, err := checkCount(r.I32(), r.Len(), chunkRefRecordSize)
	if err != nil {
		return 0, nil, fmt.Errorf("chunk index: %w", err)
	}
	refs := make([]chunkRef, 0, n)
	for i := 0; i < n && r.Err() == nil; i++ {
		refs = append(refs, chunkRef{Chunk: r.U16(), Size: r.U8(), Version: r.U32(), ZDOs: r.I32()})
	}
	return total, refs, r.Err()
}

func readChunkFile(b []byte, opt ReadOptions, fn func(*ZDO)) (int, error) {
	r := zpkg.NewReader(b)
	ver := int32(r.I16())
	if ver < VersionChunkedSave || ver > VersionDeepNorth {
		return 0, fmt.Errorf("%w: chunk version %d", ErrUnsupportedVersion, ver)
	}
	n := int(r.I32())
	var z ZDO
	for i := 0; i < n; i++ {
		if err := decodeZDO(r, ver, &z, opt.KeepBytes); err != nil {
			return i, fmt.Errorf("zdo %d: %w", i, err)
		}
		fn(&z)
	}
	if r.Len() != 0 {
		return n, fmt.Errorf("save: %d trailing bytes in chunk", r.Len())
	}
	return n, nil
}

type zoneState struct {
	Zones      [][2]int16
	GlobalKeys []string
	Locations  []Location
}

// maxDB2Uncompressed bounds the decompressed size of a .db2 zone/location
// payload: a corrupt or hostile gzip stream must not be allowed to inflate
// to an unbounded size in memory.
const maxDB2Uncompressed = 64 << 20 // 64 MiB

// zoneRecordSize and locationRecordSize are the smallest on-disk sizes of
// one zone (Vec2s) and one location (I32 hash + Vec3 pos + bool) record.
const (
	zoneRecordSize     = 2 + 2
	locationRecordSize = 4 + 4 + 4 + 4 + 1
)

func readDB2(b []byte) (float64, zoneState, error) {
	outer := zpkg.NewReader(b)
	outer.I32() // version
	netTime := outer.F64()
	gz := outer.Bytes(int(outer.I32()))
	if outer.Err() != nil {
		return 0, zoneState{}, outer.Err()
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return 0, zoneState{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(zr, maxDB2Uncompressed+1))
	if err != nil {
		return 0, zoneState{}, err
	}
	if len(raw) > maxDB2Uncompressed {
		return 0, zoneState{}, fmt.Errorf("save: db2 payload exceeds %d bytes", maxDB2Uncompressed)
	}
	r := zpkg.NewReader(raw)
	var zs zoneState
	nz, err := checkCount(r.I32(), r.Len(), zoneRecordSize)
	if err != nil {
		return 0, zoneState{}, fmt.Errorf("db2 zones: %w", err)
	}
	zs.Zones = make([][2]int16, 0, nz)
	for i := 0; i < nz && r.Err() == nil; i++ {
		zs.Zones = append(zs.Zones, r.Vec2s())
	}
	r.I32() // location version
	nk := int(r.I32())
	for i := 0; i < nk && r.Err() == nil; i++ {
		zs.GlobalKeys = append(zs.GlobalKeys, r.Str())
	}
	r.Bool() // locations generated
	nl, err := checkCount(r.I32(), r.Len(), locationRecordSize)
	if err != nil {
		return 0, zoneState{}, fmt.Errorf("db2 locations: %w", err)
	}
	zs.Locations = make([]Location, 0, nl)
	for i := 0; i < nl && r.Err() == nil; i++ {
		zs.Locations = append(zs.Locations, Location{Hash: r.I32(), Pos: r.Vec3(), Placed: r.Bool()})
	}
	return netTime, zs, r.Err()
}

var okFile = regexp.MustCompile(`^_main\.(\d+)\.ok$`)

func latestChunkedSave(dir string) (uint64, bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	var best uint64
	found := false
	for _, e := range entries {
		if m := okFile.FindStringSubmatch(e.Name()); m != nil {
			n, _ := strconv.ParseUint(m[1], 10, 64)
			if !found || n > best {
				best, found = n, true
			}
		}
	}
	return best, found, nil
}

// hasIncompleteChunkedSave reports whether dir contains any chunked-save
// file (_main.*) without a single _main.*.ok existing anywhere in dir,
// meaning a chunked save is currently being written. LatestSave uses this
// to avoid ever falling back to a stale legacy .db while a chunked save is
// mid-write.
func hasIncompleteChunkedSave(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	anyMain, anyOK := false, false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "_main.") {
			anyMain = true
			if okFile.MatchString(e.Name()) {
				anyOK = true
			}
		}
	}
	return anyMain && !anyOK, nil
}

// readFile maps a vanished file to ErrSaveChanged: Valheim deletes the
// previous save's files once a newer save is complete.
func readFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrSaveChanged
	}
	return b, err
}

func readChunked(dir string, opt ReadOptions, fn func(*ZDO)) (*World, error) {
	n, ok, err := latestChunkedSave(dir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNoSave
	}
	base := filepath.Join(dir, fmt.Sprintf("_main.%d", n))
	fwl, err := readFile(base + ".fwl2")
	if err != nil {
		return nil, err
	}
	ver, meta, err := readFWL(fwl)
	if err != nil {
		return nil, fmt.Errorf("fwl2: %w", err)
	}
	if ver < VersionChunkedSave || ver > VersionDeepNorth {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, ver)
	}
	idx, err := readFile(base + ".chunks")
	if err != nil {
		return nil, err
	}
	total, refs, err := readChunkIndex(idx)
	if err != nil {
		return nil, fmt.Errorf("chunks index: %w", err)
	}
	w := &World{Format: FormatChunked, Version: ver, SaveID: fmt.Sprintf("chunked:%d", n), Meta: meta}
	for _, ref := range refs {
		b, err := readFile(filepath.Join(dir, ref.fileName()))
		if err != nil {
			return nil, err
		}
		got, err := readChunkFile(b, opt, fn)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ref.fileName(), err)
		}
		if got != int(ref.ZDOs) {
			return nil, fmt.Errorf("%s: %d zdos, index says %d", ref.fileName(), got, ref.ZDOs)
		}
		w.ZDOCount += got
	}
	if w.ZDOCount != int(total) {
		return nil, fmt.Errorf("save: read %d zdos, index says %d", w.ZDOCount, total)
	}
	db2, err := readFile(base + ".db2")
	if err != nil {
		return nil, err
	}
	nt, zs, err := readDB2(db2)
	if err != nil {
		return nil, fmt.Errorf("db2: %w", err)
	}
	w.NetTime, w.Zones, w.GlobalKeys, w.Locations = nt, zs.Zones, zs.GlobalKeys, zs.Locations
	okInfo, err := os.Stat(base + ".ok")
	if err != nil {
		return nil, ErrSaveChanged
	}
	w.SavedAt = okInfo.ModTime()
	return w, nil
}
