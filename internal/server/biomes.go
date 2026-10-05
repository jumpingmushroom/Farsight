package server

import (
	"net/http"

	"github.com/jumpingmushroom/farsight/internal/tileset"
)

// biomes serves GET /tiles/{id}/{key}/biomes: the world's base-biome grid
// (internal/biomegrid), gzip'd, for the cursor readout. key must be the
// server's current tile-set key, which names the seed and generator, so
// the response never changes and is cached for good.
func (s *server) biomes(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.unlocked(r, id); !ok {
		notFound(w)
		return
	}
	ws, ok, err := s.worlds.get(r.Context(), id)
	if err != nil {
		s.internalError(w, "biomes", id, err)
		return
	}
	if !ok {
		notFound(w)
		return
	}
	seed, gen := ws.snap.World.Seed, ws.snap.World.GenVersion
	if r.PathValue("key") != s.Tiles.Key(seed, gen) || tileset.Refused(gen) {
		notFound(w)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(s.biomeGrids.Get(seed, gen))
}
