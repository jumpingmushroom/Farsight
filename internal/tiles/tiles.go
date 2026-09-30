// Package tiles renders a resumable z0-z5 PNG slippy-map tile pyramid from
// any Source (biome + height sampler), most commonly worldgen.TerrainView.
package tiles

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

// Source is a read-only, concurrency-safe biome/height sampler. The CLI
// passes g.TerrainView() (worldgen Task 5b) so tiles show blended terrain.
type Source interface {
	Biome(wx, wy float32) worldgen.Biome
	Height(wx, wy float32) float32
}

// Options configures Render.
type Options struct {
	Dir     string // output root; tiles at {Dir}/{z}/{x}/{y}.png
	Workers int    // 0 => runtime.GOMAXPROCS(0)
}

const (
	TileSize    = 256
	MaxZoom     = 5
	WorldRadius = 10500.0
	WaterLevel  = 30.0

	tilesPerAxis = 1 << MaxZoom            // 32
	worldPixels  = tilesPerAxis * TileSize // 8192
	metresPerPx  = float32(2 * WorldRadius / worldPixels)

	completeMarker = "complete"

	// totalTiles is every tile in the z0..z5 pyramid: 1024 + 256 + 64 +
	// 16 + 4 + 1.
	totalTiles = (1<<(2*(MaxZoom+1)) - 1) / 3
)

// RenderVersion identifies this package's tile-drawing logic (colours,
// hillshade, water shading, downsampling -- anything renderZ5Tile or
// downsampleTile does). Bump it whenever a change would make an existing
// tile set's pixels stop matching what Render would draw today; every
// world then needs a fresh directory (see SetDir) and its old tile set is
// safe to garbage-collect once the new one is complete.
const RenderVersion = 1

// SetDir returns the tile-set directory for a world under root: one
// directory per (seed, genVersion, RenderVersion) triple, so a renderer
// change or a different genVersion never reads or resumes into a stale or
// mismatched tile set.
func SetDir(root string, seed int32, genVersion int32) string {
	return filepath.Join(root, fmt.Sprintf("%d-%d-r%d", seed, genVersion, RenderVersion))
}

// biomeColor is the sRGB colour for each biome, used both for dry land and,
// for Ocean, reused as the water tint (see waterColor).
var biomeColor = map[worldgen.Biome]color.NRGBA{
	worldgen.Meadows:     hexColor(0xA3B25C),
	worldgen.Swamp:       hexColor(0x786246),
	worldgen.Mountain:    hexColor(0xE2E6EC),
	worldgen.BlackForest: hexColor(0x3E5834),
	worldgen.Plains:      hexColor(0xD6BE6E),
	worldgen.AshLands:    hexColor(0x963C28),
	worldgen.DeepNorth:   hexColor(0xC8D7E6),
	worldgen.Ocean:       hexColor(0x2E5478),
	worldgen.Mistlands:   hexColor(0x6E6478),
}

// lakeWater is used for any water (Height < WaterLevel) in a non-Ocean
// biome: lakes, rivers and streams cutting through land biomes.
var lakeWater = hexColor(0x3F6E91)

var defaultColor = hexColor(0x808080)

func hexColor(c uint32) color.NRGBA {
	return color.NRGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 255}
}

func biomeBaseColor(b worldgen.Biome) color.NRGBA {
	if c, ok := biomeColor[b]; ok {
		return c
	}
	return defaultColor
}

// Render (re)builds the tile pyramid at opt.Dir, sampling src. It is
// resumable: an existing, decodable tile is skipped. progress reports the
// number of tiles done (including skipped ones) out of the whole z0..z5
// pyramid (totalTiles, 1365): z5 tiles as the workers finish them, with
// the count of any pre-existing z5 tiles folded into the first call, then
// each z4..z0 tile as the downsampling pass reaches it. done equals total
// exactly once, in the last call, made after the completion marker is
// written, so 100% means the tile set is complete. progress is called
// with a lock held, so it must be cheap and non-blocking (e.g. update a
// counter or send on a buffered channel; don't do I/O or block on another
// goroutine from inside it). ctx cancellation is honoured between tiles;
// on cancellation Render returns a non-nil error and does not write the
// completion marker.
func Render(ctx context.Context, src Source, opt Options, progress func(done, total int)) error {
	if opt.Workers <= 0 {
		opt.Workers = runtime.GOMAXPROCS(0)
	}
	dir := opt.Dir
	total := totalTiles

	if Complete(dir) {
		if progress != nil {
			progress(total, total)
		}
		return nil
	}

	// A process killed mid-write leaves its in-progress tile as a
	// .tmp-*.png sibling (writeTilePNG's os.CreateTemp pattern) that was
	// never renamed into place. Sweep those before doing anything else so a
	// resumed run doesn't accumulate garbage across restarts.
	if err := sweepStaleTemp(dir); err != nil {
		return fmt.Errorf("sweep stale temp files: %w", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	type coord struct{ x, y int }
	var pending []coord
	existing := 0
	for ty := 0; ty < tilesPerAxis; ty++ {
		for tx := 0; tx < tilesPerAxis; tx++ {
			if tileExists(dir, MaxZoom, tx, ty) {
				existing++
			} else {
				pending = append(pending, coord{tx, ty})
			}
		}
	}

	done := existing
	if len(pending) == 0 {
		// Fully rendered at z5 already (e.g. a crash right before z0-z4 or
		// the completion marker); still report where we stand.
		if progress != nil {
			progress(existing, total)
		}
	} else {
		var progressMu sync.Mutex
		report := func() {
			// Increment and the progress() call itself must be one
			// critical section: if either happened outside the lock, two
			// workers could still deliver counts out of order (e.g. 1009
			// reported before 1008) even though the increments themselves
			// were serialized.
			progressMu.Lock()
			done++
			if progress != nil {
				progress(done, total)
			}
			progressMu.Unlock()
		}

		// workCtx is cancelled either by the caller's ctx or by the first
		// write error, so a failure stops the pool promptly instead of
		// draining the remaining queue.
		workCtx, cancelWork := context.WithCancel(ctx)
		defer cancelWork()

		jobs := make(chan coord)
		errCh := make(chan error, 1)
		var wg sync.WaitGroup
		for i := 0; i < opt.Workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				wsrc := workerSource(src)
				for c := range jobs {
					if workCtx.Err() != nil {
						return
					}
					img := renderZ5Tile(wsrc, c.x, c.y)
					if err := writeTilePNG(dir, MaxZoom, c.x, c.y, img); err != nil {
						select {
						case errCh <- err:
						default:
						}
						cancelWork()
						return
					}
					report()
				}
			}()
		}

	feed:
		for _, c := range pending {
			select {
			case jobs <- c:
			case <-workCtx.Done():
				break feed
			}
		}
		close(jobs)
		wg.Wait()

		select {
		case err := <-errCh:
			return err
		default:
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	for z := MaxZoom - 1; z >= 0; z-- {
		nz := 1 << uint(z)
		for ty := 0; ty < nz; ty++ {
			for tx := 0; tx < nz; tx++ {
				if err := ctx.Err(); err != nil {
					return err
				}
				if !tileExists(dir, z, tx, ty) {
					img, err := downsampleTile(dir, z, tx, ty)
					if err != nil {
						return err
					}
					if err := writeTilePNG(dir, z, tx, ty, img); err != nil {
						return err
					}
				}
				// The pool is done, so no lock is needed here. The
				// final tile's report waits for the completion marker.
				if done++; progress != nil && done < total {
					progress(done, total)
				}
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, completeMarker), []byte(strconv.Itoa(RenderVersion)), 0o644); err != nil {
		return err
	}
	if progress != nil {
		progress(total, total)
	}
	return nil
}

// workerSource returns the Source one render worker should sample: its own
// *worldgen.Sampler (a per-goroutine corner-biome cache, bit-identical to
// TerrainView) when src offers one, otherwise src itself.
func workerSource(src Source) Source {
	if ss, ok := src.(interface{ NewSampler() *worldgen.Sampler }); ok {
		return ss.NewSampler()
	}
	return src
}

// Complete reports whether dir holds a fully rendered pyramid from the
// current RenderVersion. A marker left by a different RenderVersion (e.g.
// an old tile set found by a renderer that has since changed its drawing
// logic) reads as not complete, so Render re-renders it from scratch rather
// than trusting stale pixels.
func Complete(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, completeMarker))
	if err != nil {
		return false
	}
	return string(b) == strconv.Itoa(RenderVersion)
}

// renderZ5Tile samples a (TileSize+2)^2 height grid (1-px border, so
// hillshade is seamless across tile edges) once, then reuses it for every
// pixel's colour, hillshade and water shading -- one Height/Biome call per
// grid/pixel position, no more.
func renderZ5Tile(src Source, tx, ty int) *image.NRGBA {
	const n = TileSize
	const g = n + 2

	heights := make([]float32, g*g)
	for gy := 0; gy < g; gy++ {
		py := gy - 1
		wz := WorldRadius - (float32(ty*n+py)+0.5)*metresPerPx
		row := gy * g
		for gx := 0; gx < g; gx++ {
			px := gx - 1
			wx := -WorldRadius + (float32(tx*n+px)+0.5)*metresPerPx
			heights[row+gx] = src.Height(wx, wz)
		}
	}

	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for py := 0; py < n; py++ {
		wz := WorldRadius - (float32(ty*n+py)+0.5)*metresPerPx
		gy := py + 1
		for px := 0; px < n; px++ {
			wx := -WorldRadius + (float32(tx*n+px)+0.5)*metresPerPx
			gx := px + 1
			idx := gy*g + gx

			var col color.NRGBA
			if wx*wx+wz*wz > WorldRadius*WorldRadius {
				col = color.NRGBA{}
			} else {
				h := heights[idx]
				hL := heights[idx-1]
				hR := heights[idx+1]
				hN := heights[idx-g] // py-1 => larger z => north
				hS := heights[idx+g] // py+1 => smaller z => south

				dhdx := (hR - hL) / (2 * metresPerPx)
				dhdz := (hN - hS) / (2 * metresPerPx)
				shade := hillshade(dhdx, dhdz)

				biome := src.Biome(wx, wz)
				if h < WaterLevel {
					col = waterColor(biome, WaterLevel-h)
				} else {
					col = shadeColor(biomeBaseColor(biome), shade)
				}
			}
			img.SetNRGBA(px, py, col)
		}
	}
	return img
}

// hillshade returns a brightness in [0.4, 1.0] for a surface with the given
// (dh/dx east, dh/dz north) gradient, lit from the north-west and above.
func hillshade(dhdx, dhdz float32) float64 {
	nx, ny, nz := float64(-dhdx), 1.0, float64(-dhdz)
	length := math.Sqrt(nx*nx + ny*ny + nz*nz)
	nx, ny, nz = nx/length, ny/length, nz/length

	const inv = 0.5773502691896258 // 1/sqrt(3)
	lx, ly, lz := -inv, inv, inv   // west, up, north
	dot := nx*lx + ny*ly + nz*lz
	if dot < 0 {
		dot = 0
	}
	return 0.4 + 0.6*dot
}

func shadeColor(base color.NRGBA, brightness float64) color.NRGBA {
	return color.NRGBA{
		R: clampByte(float64(base.R) * brightness),
		G: clampByte(float64(base.G) * brightness),
		B: clampByte(float64(base.B) * brightness),
		A: 255,
	}
}

// waterColor darkens the ocean/lake tint with depth (WaterLevel - height),
// saturating at maxDepth.
func waterColor(biome worldgen.Biome, depth float32) color.NRGBA {
	base := lakeWater
	if biome == worldgen.Ocean {
		base = biomeColor[worldgen.Ocean]
	}
	const maxDepth = 50.0
	f := float64(depth) / maxDepth
	if f < 0 {
		f = 0
	} else if f > 1 {
		f = 1
	}
	darken := 1.0 - 0.4*f
	return color.NRGBA{
		R: clampByte(float64(base.R) * darken),
		G: clampByte(float64(base.G) * darken),
		B: clampByte(float64(base.B) * darken),
		A: 255,
	}
}

func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// downsampleTile builds zoom z tile (tx,ty) as the 2x2 box-downsample of its
// four z+1 children, read back from disk. Averaging is alpha-weighted
// (premultiplied) so pixels outside WorldRadius (transparent) don't darken
// the coastline they border.
func downsampleTile(dir string, z, tx, ty int) (*image.NRGBA, error) {
	out := image.NewNRGBA(image.Rect(0, 0, TileSize, TileSize))
	children := [2][2]struct{ cx, cy int }{
		{{2 * tx, 2 * ty}, {2*tx + 1, 2 * ty}},
		{{2 * tx, 2*ty + 1}, {2*tx + 1, 2*ty + 1}},
	}
	for qy := 0; qy < 2; qy++ {
		for qx := 0; qx < 2; qx++ {
			cc := children[qy][qx]
			child, err := readTilePNG(dir, z+1, cc.cx, cc.cy)
			if err != nil {
				return nil, fmt.Errorf("read child z%d/%d/%d: %w", z+1, cc.cx, cc.cy, err)
			}
			for ly := 0; ly < TileSize/2; ly++ {
				for lx := 0; lx < TileSize/2; lx++ {
					p00 := child.NRGBAAt(2*lx, 2*ly)
					p10 := child.NRGBAAt(2*lx+1, 2*ly)
					p01 := child.NRGBAAt(2*lx, 2*ly+1)
					p11 := child.NRGBAAt(2*lx+1, 2*ly+1)
					out.SetNRGBA(qx*TileSize/2+lx, qy*TileSize/2+ly, premultipliedAvg(p00, p10, p01, p11))
				}
			}
		}
	}
	return out, nil
}

func premultipliedAvg(pixels ...color.NRGBA) color.NRGBA {
	var sumR, sumG, sumB, sumA float64
	for _, p := range pixels {
		a := float64(p.A) / 255
		sumR += float64(p.R) * a
		sumG += float64(p.G) * a
		sumB += float64(p.B) * a
		sumA += float64(p.A)
	}
	n := float64(len(pixels))
	avgA := sumA / n
	if avgA <= 0 {
		return color.NRGBA{}
	}
	// sumR/n is the average premultiplied red; unpremultiply by avgA/255.
	unpre := 255 / avgA
	return color.NRGBA{
		R: clampByte(sumR / n * unpre),
		G: clampByte(sumG / n * unpre),
		B: clampByte(sumB / n * unpre),
		A: clampByte(avgA),
	}
}

// sweepStaleTemp removes any leftover writeTilePNG temp file (".tmp-*.png",
// from os.CreateTemp) anywhere under dir. A killed process can leave one
// behind mid-write; the real tile it was headed for was never renamed into
// place, so tileExists still treats that tile as missing and Render
// re-renders it -- this just clears the orphaned temp file rather than
// leaving it on disk forever. dir not existing yet is not an error.
func sweepStaleTemp(dir string) error {
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), ".tmp-") && strings.HasSuffix(d.Name(), ".png") {
			return os.Remove(path)
		}
		return nil
	})
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

func tilePath(dir string, z, x, y int) string {
	return filepath.Join(dir, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".png")
}

func tileExists(dir string, z, x, y int) bool {
	f, err := os.Open(tilePath(dir, z, x, y))
	if err != nil {
		return false
	}
	defer f.Close()
	_, err = png.Decode(f)
	return err == nil
}

func readTilePNG(dir string, z, x, y int) (*image.NRGBA, error) {
	f, err := os.Open(tilePath(dir, z, x, y))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	if n, ok := img.(*image.NRGBA); ok {
		return n, nil
	}
	b := img.Bounds()
	out := image.NewNRGBA(b)
	draw.Draw(out, b, img, b.Min, draw.Src)
	return out, nil
}

func writeTilePNG(dir string, z, x, y int, img image.Image) error {
	path := tilePath(dir, z, x, y)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.png")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(tmp, img); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	// os.CreateTemp files are mode 0600; these PNGs are served by another
	// process, so make them world-readable before the rename exposes them.
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
