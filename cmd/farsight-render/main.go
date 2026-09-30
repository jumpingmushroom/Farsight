// Command farsight-render is a dev CLI that generates a Valheim world by
// seed name and renders its z0-z5 PNG tile pyramid, optionally stitching a
// 2048x2048 preview PNG from the z3 tiles.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/tiles"
	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

// previewZoom and previewTilesPerAxis pick the zoom level the preview is
// stitched from: z3 is an 8x8 grid of 256px tiles, i.e. 2048x2048 overall.
const (
	previewZoom         = 3
	previewTilesPerAxis = 1 << previewZoom // 8
	previewSize         = previewTilesPerAxis * tiles.TileSize
	progressEveryNTiles = 64
)

func main() {
	seed := flag.String("seed", "", "world seed name (required)")
	gen := flag.Int("gen", 2, "world generator version")
	out := flag.String("out", "", "tile output root directory (required); the tile set itself is written to {out}/{seedHash}-{gen}-r{tiles.RenderVersion}")
	preview := flag.String("preview", "", "if set, write a stitched preview PNG here")
	flag.Parse()

	if *seed == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *seed, int32(*gen), *out, *preview); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		var ue usageError
		if errors.As(err, &ue) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// usageError marks a bad-invocation error (e.g. an out-of-range -gen) that
// should exit 2, like flag.Parse's own usage failures, rather than 1 for a
// runtime failure such as a render error.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

func run(ctx context.Context, seed string, gen int32, out, preview string) error {
	seedHash := names.StableHash(seed)

	pregenStart := time.Now()
	g, err := worldgen.NewChecked(seedHash, gen)
	if err != nil {
		return usageError{err}
	}
	pregenElapsed := time.Since(pregenStart)
	fmt.Fprintf(os.Stderr, "pre-generation: seed=%q hash=%d gen=%d in %s\n", seed, seedHash, gen, pregenElapsed.Round(time.Millisecond))

	// out is the tile root; the actual tile set lives one level below it,
	// keyed by (seed, genVersion, tiles.RenderVersion) so a renderer change
	// or a different world never resumes into a stale or mismatched set.
	dir := tiles.SetDir(out, seedHash, gen)

	renderStart := time.Now()
	lastPrinted := 0
	progress := func(done, total int) {
		if done-lastPrinted >= progressEveryNTiles || done == total {
			fmt.Fprintf(os.Stderr, "tiles: %d/%d\n", done, total)
			lastPrinted = done
		}
	}

	if err := tiles.Render(ctx, g.TerrainView(), tiles.Options{Dir: dir}, progress); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	renderElapsed := time.Since(renderStart)
	fmt.Fprintf(os.Stderr, "render: total %s (pre-generation %s + tiles %s) at %s\n",
		(pregenElapsed + renderElapsed).Round(time.Millisecond), pregenElapsed.Round(time.Millisecond), renderElapsed.Round(time.Millisecond), dir)

	if preview != "" {
		if err := stitchPreview(dir, preview); err != nil {
			return fmt.Errorf("preview: %w", err)
		}
		fmt.Fprintf(os.Stderr, "preview: wrote %s\n", preview)
	}
	return nil
}

// stitchPreview assembles a previewSize x previewSize PNG from the
// previewTilesPerAxis x previewTilesPerAxis z{previewZoom} tiles at
// {dir}/{previewZoom}/{x}/{y}.png and writes it to outPath.
func stitchPreview(dir, outPath string) error {
	img := image.NewNRGBA(image.Rect(0, 0, previewSize, previewSize))
	for ty := 0; ty < previewTilesPerAxis; ty++ {
		for tx := 0; tx < previewTilesPerAxis; tx++ {
			tile, err := readTilePNG(dir, previewZoom, tx, ty)
			if err != nil {
				return fmt.Errorf("read tile z%d/%d/%d: %w", previewZoom, tx, ty, err)
			}
			dstRect := image.Rect(tx*tiles.TileSize, ty*tiles.TileSize, (tx+1)*tiles.TileSize, (ty+1)*tiles.TileSize)
			draw.Draw(img, dstRect, tile, image.Point{}, draw.Src)
		}
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func readTilePNG(dir string, z, x, y int) (image.Image, error) {
	path := filepath.Join(dir, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".png")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}
