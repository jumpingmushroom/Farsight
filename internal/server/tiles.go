package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jumpingmushroom/farsight/internal/tiles"
	"github.com/jumpingmushroom/farsight/internal/tileset"
)

// tile serves /tiles/{id}/{key}/{z}/{x}/{y}.png from the server's current,
// complete tile set. Every failure is the same 404 as a locked server.
func (s *server) tile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.unlocked(r, id); !ok {
		notFound(w)
		return
	}
	yStr, ok := strings.CutSuffix(r.PathValue("y"), ".png")
	if !ok {
		notFound(w)
		return
	}
	z, okZ := parseCoord(r.PathValue("z"), tiles.MaxZoom+1)
	if !okZ {
		notFound(w)
		return
	}
	x, okX := parseCoord(r.PathValue("x"), 1<<z)
	y, okY := parseCoord(yStr, 1<<z)
	if !okX || !okY {
		notFound(w)
		return
	}

	snap, ok, err := s.latestSnapshot(r, id)
	if err != nil {
		s.internalError(w, "tile", id, err)
		return
	}
	if !ok {
		notFound(w)
		return
	}
	seed, gen := snap.World.Seed, snap.World.GenVersion
	if r.PathValue("key") != s.Tiles.Key(seed, gen) || s.Tiles.Status(seed, gen).State != tileset.StateComplete {
		notFound(w)
		return
	}

	// Built only from parsed integers: no request string reaches the path.
	p := filepath.Join(s.Tiles.Dir(seed, gen), strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".png")
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		notFound(w)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, p)
}

// parseCoord parses a plain non-negative decimal integer below limit.
func parseCoord(s string, limit int) (int, bool) {
	if s == "" || len(s) > 3 || strings.TrimLeft(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n >= limit {
		return 0, false
	}
	return n, true
}
