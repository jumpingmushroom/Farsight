// Package save reads Valheim world saves: the 1.0 chunked directory format
// (world version 40-41) and the legacy single-file .db (versions 31-39).
package save

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

const (
	VersionNewSaveFormat = 31
	VersionGlobalKeys    = 32
	VersionNumItems      = 33
	VersionChunkedSave   = 40
	VersionDeepNorth     = 41
)

var (
	ErrUnsupportedVersion = errors.New("save: unsupported world version")
	ErrSaveChanged        = errors.New("save: save changed while reading; retry")
	ErrNoSave             = errors.New("save: no world save found")
	ErrSaveInProgress     = errors.New("save: a save is being written")
)

type Format string

const (
	FormatChunked Format = "chunked"
	FormatLegacy  Format = "legacy"
)

type Meta struct {
	Name         string
	SeedName     string
	Seed         int32
	UID          int64
	GenVersion   int32
	StartingKeys []string
}

type Location struct {
	Hash   int32
	Pos    [3]float32
	Placed bool
}

type World struct {
	Format  Format
	Version int32
	SaveID  string
	Meta    Meta
	// SavedAt is the game's own save time: the chunked format's _main.N.ok
	// mtime, or the legacy format's .db mtime. It is not the time this
	// process read the file (see extract.Snapshot.ReadAt for that).
	SavedAt    time.Time
	NetTime    float64
	ZDOCount   int
	Zones      [][2]int16
	GlobalKeys []string
	Locations  []Location
}

// LatestSave reports the newest complete save's identity without reading it.
func LatestSave(worldsDir, worldName string) (string, Format, error) {
	dir := filepath.Join(worldsDir, worldName)
	n, ok, err := latestChunkedSave(dir)
	if err != nil {
		return "", "", err
	}
	if ok {
		return fmt.Sprintf("chunked:%d", n), FormatChunked, nil
	}
	// A chunked save directory with _main.* files but no .ok anywhere means
	// a chunked save is being written; never fall back to a stale legacy
	// .db in that case.
	inProgress, err := hasIncompleteChunkedSave(dir)
	if err != nil {
		return "", "", err
	}
	if inProgress {
		return "", "", ErrSaveInProgress
	}
	id, _, err := legacySaveID(filepath.Join(worldsDir, worldName+".db"))
	if err != nil {
		return "", "", err
	}
	return id, FormatLegacy, nil
}

// Read streams every ZDO of the newest save to fn and returns the world
// metadata. fn's argument is reused between calls.
func Read(worldsDir, worldName string, fn func(*ZDO)) (*World, error) {
	_, f, err := LatestSave(worldsDir, worldName)
	if err != nil {
		return nil, err
	}
	if f == FormatChunked {
		return readChunked(filepath.Join(worldsDir, worldName), fn)
	}
	return readLegacy(filepath.Join(worldsDir, worldName+".db"), filepath.Join(worldsDir, worldName+".fwl"), fn)
}
