package server

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/jumpingmushroom/farsight/internal/fog"
	"github.com/jumpingmushroom/farsight/internal/tiles"
	"github.com/jumpingmushroom/farsight/internal/tileset"
)

// fogTile serves GET /tiles/{id}/{key}/{fog}/{z}/{x}/{y}.png (z0–z6): the
// server's terrain with its fog of war drawn in. Every failure is the same
// 404 as a locked server, including a key that isn't the current complete
// tile set and a fog key that isn't the current one. A Clear tile at z0–z5
// is the terrain file itself; every other tile is composed, PNG-encoded
// and kept in the tile cache.
func (s *server) fogTile(w http.ResponseWriter, r *http.Request) {
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
	z, okZ := parseCoord(r.PathValue("z"), fog.MaxZoom+1)
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

	ws, ok, err := s.worlds.get(r.Context(), id)
	if err != nil {
		s.internalError(w, "tile", id, err)
		return
	}
	if !ok {
		notFound(w)
		return
	}
	seed, gen := ws.snap.World.Seed, ws.snap.World.GenVersion
	key := r.PathValue("key")
	if key != s.Tiles.Key(seed, gen) || s.Tiles.Status(seed, gen).State != tileset.StateComplete ||
		r.PathValue("fog") != ws.fogKey {
		notFound(w)
		return
	}
	field, classes := ws.fogData()
	class := classes.At(z, x, y)
	dir := s.Tiles.Dir(seed, gen)

	if class == fog.Clear && z <= tiles.MaxZoom {
		// Built only from parsed integers: no request string reaches the path.
		p := terrainPath(dir, z, x, y)
		if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
			notFound(w)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(w, r, p)
		return
	}
	b, err := s.fogTiles.get(tileCacheKey{server: id, tiles: key, fog: ws.fogKey, z: z, x: x, y: y},
		func() ([]byte, error) { return composeTile(dir, field, class, z, x, y) })
	if errors.Is(err, fs.ErrNotExist) {
		notFound(w)
		return
	}
	if err != nil {
		s.internalError(w, "tile", id, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(b)
}

// composeTile draws tile (z, x, y) of class c from the tile set in dir:
// the texture alone for Fog (no terrain read), the terrain for Clear (only
// z6 gets here: its terrain is upscaled), terrain blended with fog for
// Edge. PNG at BestSpeed, like the terrain tiles.
func composeTile(dir string, f *fog.Field, c fog.Class, z, x, y int) ([]byte, error) {
	var img *image.NRGBA
	if c == fog.Fog {
		img = fog.FogTile(z, x, y)
	} else {
		t, err := readTerrain(dir, z, x, y)
		if err != nil {
			return nil, err
		}
		if c == fog.Edge {
			fog.Blend(t, f, z, x, y)
		}
		img = t
	}
	var buf bytes.Buffer
	if err := pngEncoder.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pngEncoder reuses its compressor state across tiles.
var pngEncoder = png.Encoder{CompressionLevel: png.BestSpeed, BufferPool: &pngBuffers{}}

type pngBuffers struct{ p sync.Pool }

func (b *pngBuffers) Get() *png.EncoderBuffer {
	eb, _ := b.p.Get().(*png.EncoderBuffer)
	return eb
}

func (b *pngBuffers) Put(eb *png.EncoderBuffer) { b.p.Put(eb) }

// readTerrain decodes terrain tile (z, x, y). z6 has no file: it is its z5
// parent's quadrant upscaled 2×.
func readTerrain(dir string, z, x, y int) (*image.NRGBA, error) {
	if z > tiles.MaxZoom {
		parent, err := readTerrain(dir, tiles.MaxZoom, x>>1, y>>1)
		if err != nil {
			return nil, err
		}
		return fog.Upscale(parent, x&1, y&1), nil
	}
	f, err := os.Open(terrainPath(dir, z, x, y))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	return toNRGBA(img)
}

// toNRGBA converts a decoded terrain tile to a TileSize² NRGBA at the
// origin. An opaque tile decodes as *image.RGBA, which at alpha 255 has
// the same bytes as NRGBA.
func toNRGBA(src image.Image) (*image.NRGBA, error) {
	b := src.Bounds()
	if b.Dx() != fog.TileSize || b.Dy() != fog.TileSize {
		return nil, fmt.Errorf("server: terrain tile is %dx%d", b.Dx(), b.Dy())
	}
	if n, ok := src.(*image.NRGBA); ok && b.Min == (image.Point{}) {
		return n, nil
	}
	out := image.NewNRGBA(image.Rect(0, 0, fog.TileSize, fog.TileSize))
	if r, ok := src.(*image.RGBA); ok && b.Min == (image.Point{}) && r.Opaque() {
		for y := 0; y < fog.TileSize; y++ {
			copy(out.Pix[y*out.Stride:(y+1)*out.Stride], r.Pix[y*r.Stride:])
		}
		return out, nil
	}
	draw.Draw(out, out.Rect, src, b.Min, draw.Src)
	return out, nil
}

func terrainPath(dir string, z, x, y int) string {
	return filepath.Join(dir, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".png")
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
