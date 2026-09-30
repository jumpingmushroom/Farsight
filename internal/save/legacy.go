package save

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

// legacySaveID also returns the .db file's os.FileInfo (nil on error) so
// callers can read its mtime without a second stat.
func legacySaveID(dbPath string) (string, os.FileInfo, error) {
	if _, err := os.Stat(dbPath + ".new"); err == nil {
		return "", nil, ErrSaveInProgress
	}
	fi, err := os.Stat(dbPath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, ErrNoSave
	}
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("legacy:%d:%d", fi.ModTime().UnixNano(), fi.Size()), fi, nil
}

func readLegacy(dbPath, fwlPath string, fn func(*ZDO)) (*World, error) {
	id, fi, err := legacySaveID(dbPath)
	if err != nil {
		return nil, err
	}
	fwlBytes, err := readFile(fwlPath)
	if err != nil {
		return nil, err
	}
	_, meta, err := readFWL(fwlBytes)
	if err != nil {
		return nil, fmt.Errorf("fwl: %w", err)
	}
	b, err := readFile(dbPath)
	if err != nil {
		return nil, err
	}
	r := zpkg.NewReader(b)
	ver := r.I32()
	if ver < VersionNewSaveFormat || ver >= VersionChunkedSave {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, ver)
	}
	w := &World{Format: FormatLegacy, Version: ver, SaveID: id, Meta: meta, SavedAt: fi.ModTime()}
	w.NetTime = r.F64()
	r.I64() // session id
	r.U32() // next uid
	n := int(r.I32())
	var z ZDO
	for i := 0; i < n; i++ {
		if err := DecodeZDO(r, ver, &z); err != nil {
			return nil, fmt.Errorf("zdo %d: %w", i, err)
		}
		fn(&z)
	}
	w.ZDOCount = n

	nz := int(r.I32())
	for i := 0; i < nz && r.Err() == nil; i++ {
		x, y := r.I32(), r.I32()
		w.Zones = append(w.Zones, [2]int16{int16(x), int16(y)})
	}
	r.I32() // pgw
	r.I32() // location version
	nk := int(r.I32())
	for i := 0; i < nk && r.Err() == nil; i++ {
		w.GlobalKeys = append(w.GlobalKeys, r.Str())
	}
	r.Bool() // locations generated
	nl := int(r.I32())
	for i := 0; i < nl && r.Err() == nil; i++ {
		name := r.Str()
		w.Locations = append(w.Locations, Location{Hash: names.StableHash(name), Pos: r.Vec3(), Placed: r.Bool()})
	}
	if r.Err() != nil {
		return nil, fmt.Errorf("zone system: %w", r.Err())
	}
	if after, _, err := legacySaveID(dbPath); err != nil || after != id {
		return nil, ErrSaveChanged
	}
	return w, nil
}
