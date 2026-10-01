// Command farsight-seed is a dev and e2e tool that seeds a local farsight
// with demo data. It can write a world's tile set straight into a data
// directory (a fast fake one, or the real render), and post a fixture
// snapshot and events to the ingest API as an agent would.
//
// Typical use, with the tiles written before `farsight serve` starts so its
// tile manager finds a complete set:
//
//	farsight-seed -fake-tiles DATA -snapshot web/tests/fixtures/snapshot.json
//	farsight serve -config ...
//	FARSIGHT_SEED_TOKEN=... farsight-seed -server demo -shift -explored \
//	    -snapshot web/tests/fixtures/snapshot.json -events web/tests/fixtures/events.json
//
// The agent token is only ever read from the environment (-token-env names
// the variable), so it never shows in `ps`.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/ingest"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
	"github.com/jumpingmushroom/farsight/internal/tiles"
	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

// config holds the parsed flags.
type config struct {
	URL       string
	Server    string
	TokenEnv  string
	Snapshot  string
	Events    string
	Shift     bool
	Explored  bool
	FakeTiles string
	RealTiles string
}

// usageError marks a bad invocation (exit status 2).
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func main() {
	cfg, err := parseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, os.Getenv, time.Now().UTC(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "farsight-seed:", err)
		var ue usageError
		if errors.As(err, &ue) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func parseFlags(args []string, stderr io.Writer) (config, error) {
	var c config
	fs := flag.NewFlagSet("farsight-seed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&c.URL, "url", "http://127.0.0.1:8080", "farsight base URL")
	fs.StringVar(&c.Server, "server", "", "server id to post to (required to post a snapshot or events)")
	fs.StringVar(&c.TokenEnv, "token-env", "FARSIGHT_SEED_TOKEN", "name of the env var holding the agent token")
	fs.StringVar(&c.Snapshot, "snapshot", "", "snapshot JSON file (an extract.Snapshot); also gives the world seed for -fake-tiles/-real-tiles")
	fs.StringVar(&c.Events, "events", "", `events JSON file ({"events": [...]}, the ingest body)`)
	fs.BoolVar(&c.Shift, "shift", false, "shift event and snapshot times so the newest event is now - 1 min")
	fs.BoolVar(&c.Explored, "explored", false, "give a snapshot without an explored mask one rasterised from its exploredZones, as a current agent sends")
	fs.StringVar(&c.FakeTiles, "fake-tiles", "", "DATADIR: write a complete flat-colour tile set for the snapshot's world under DATADIR/tiles")
	fs.StringVar(&c.RealTiles, "real-tiles", "", "DATADIR: render the real tile set for the snapshot's world under DATADIR/tiles")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	if fs.NArg() > 0 {
		err := fmt.Errorf("unexpected arguments: %v", fs.Args())
		fmt.Fprintln(stderr, err)
		fs.Usage()
		return c, err
	}
	return c, nil
}

// run does the work: tiles first (they need no server), then the posts.
// now is the reference for -shift.
func run(ctx context.Context, c config, getenv func(string) string, now time.Time, out io.Writer) error {
	tilesWanted := c.FakeTiles != "" || c.RealTiles != ""
	posting := c.Server != ""
	if !tilesWanted && !posting {
		return usageError{"nothing to do: give -server (to post) and/or -fake-tiles/-real-tiles"}
	}
	if tilesWanted && c.Snapshot == "" {
		return usageError{"-fake-tiles and -real-tiles need -snapshot for the world seed"}
	}
	if posting && c.Snapshot == "" && c.Events == "" {
		return usageError{"-server needs -snapshot and/or -events"}
	}

	var snap *extract.Snapshot
	if c.Snapshot != "" {
		s, err := readSnapshot(c.Snapshot)
		if err != nil {
			return err
		}
		snap = s
	}
	var evs []logwatch.Event
	if c.Events != "" {
		e, err := readEvents(c.Events)
		if err != nil {
			return err
		}
		evs = e
	}

	if c.FakeTiles != "" {
		dir, err := writeFakeTiles(c.FakeTiles, snap.World.Seed, snap.World.GenVersion)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "fake tiles:", dir)
	}
	if c.RealTiles != "" {
		dir, err := renderRealTiles(ctx, c.RealTiles, snap.World.Seed, snap.World.GenVersion, out)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "real tiles:", dir)
	}
	if !posting {
		return nil
	}

	token := getenv(c.TokenEnv)
	if token == "" {
		return usageError{fmt.Sprintf("the agent token env var %s is empty", c.TokenEnv)}
	}
	if c.Shift {
		d := shiftTimes(evs, snap, now)
		fmt.Fprintf(out, "shifted times by %s\n", d.Round(time.Second))
	}
	if c.Explored && snap != nil && snap.Explored == nil {
		enc := explored.Encode(explored.FromZones(snap.ExploredZones), explored.SourceZones)
		snap.Explored = &enc
	}
	cl := ingest.New(c.URL, c.Server, token)
	if snap != nil {
		snap.ServerID = c.Server
		if err := cl.Post(ctx, "snapshot", snap); err != nil {
			return fmt.Errorf("post snapshot: %w", err)
		}
		fmt.Fprintf(out, "posted snapshot %s (saved %s)\n", snap.SaveID, snap.SavedAt.Format(time.RFC3339))
	}
	if evs != nil {
		if err := cl.Post(ctx, "events", map[string]any{"events": evs}); err != nil {
			return fmt.Errorf("post events: %w", err)
		}
		fmt.Fprintf(out, "posted %d events\n", len(evs))
	}
	return nil
}

func decodeStrict(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func readSnapshot(path string) (*extract.Snapshot, error) {
	var s extract.Snapshot
	if err := decodeStrict(path, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func readEvents(path string) ([]logwatch.Event, error) {
	var body struct {
		Events []logwatch.Event `json:"events"`
	}
	if err := decodeStrict(path, &body); err != nil {
		return nil, err
	}
	return body.Events, nil
}

// shiftTimes moves every event time (at, since) and the snapshot's
// savedAt/readAt by one delta, chosen so the newest event lands at
// now - 1 min; relative gaps are preserved. With no events, the snapshot
// is placed as if its save were 5 min before such a newest event. It
// returns the delta.
func shiftTimes(evs []logwatch.Event, snap *extract.Snapshot, now time.Time) time.Duration {
	var newest time.Time
	for _, e := range evs {
		if e.At.After(newest) {
			newest = e.At
		}
	}
	if newest.IsZero() {
		if snap == nil {
			return 0
		}
		newest = snap.SavedAt.Add(5 * time.Minute)
	}
	d := now.Add(-time.Minute).Sub(newest)
	for i := range evs {
		evs[i].At = evs[i].At.Add(d)
		if evs[i].Since != nil {
			s := evs[i].Since.Add(d)
			evs[i].Since = &s
		}
	}
	if snap != nil {
		snap.SavedAt = snap.SavedAt.Add(d)
		snap.ReadAt = snap.ReadAt.Add(d)
	}
	return d
}

// fakeColors are the flat tile colours by quadrant: NW, NE, SW, SE.
var fakeColors = [4]color.NRGBA{
	{0xA3, 0xB2, 0x5C, 0xFF}, // meadows green
	{0xD6, 0xBE, 0x6E, 0xFF}, // plains gold
	{0x3E, 0x58, 0x34, 0xFF}, // black forest
	{0x2E, 0x54, 0x78, 0xFF}, // ocean
}

// writeFakeTiles writes a complete z0..z5 pyramid of flat-colour 256 px
// PNGs (the colour picked by the tile centre's quadrant) plus the
// completion marker into the tile-set directory for (seed, gen) under
// dataDir/tiles, and returns that directory.
func writeFakeTiles(dataDir string, seed, gen int32) (string, error) {
	dir := tiles.SetDir(filepath.Join(dataDir, "tiles"), seed, gen)
	var encoded [4][]byte
	for i, c := range fakeColors {
		img := image.NewNRGBA(image.Rect(0, 0, tiles.TileSize, tiles.TileSize))
		for p := 0; p < len(img.Pix); p += 4 {
			img.Pix[p], img.Pix[p+1], img.Pix[p+2], img.Pix[p+3] = c.R, c.G, c.B, c.A
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return "", err
		}
		encoded[i] = buf.Bytes()
	}
	for z := 0; z <= tiles.MaxZoom; z++ {
		n := 1 << z
		for x := 0; x < n; x++ {
			col := filepath.Join(dir, strconv.Itoa(z), strconv.Itoa(x))
			if err := os.MkdirAll(col, 0o755); err != nil {
				return "", err
			}
			for y := 0; y < n; y++ {
				q := 0
				if 2*x+1 > n { // centre east of the middle
					q++
				}
				if 2*y+1 > n { // centre south of the middle
					q += 2
				}
				if err := os.WriteFile(filepath.Join(col, strconv.Itoa(y)+".png"), encoded[q], 0o644); err != nil {
					return "", err
				}
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "complete"), []byte(strconv.Itoa(tiles.RenderVersion)), 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

// renderRealTiles renders the true tile set for (seed, gen) under
// dataDir/tiles, leaving one CPU free.
func renderRealTiles(ctx context.Context, dataDir string, seed, gen int32, out io.Writer) (string, error) {
	g, err := worldgen.NewChecked(seed, gen)
	if err != nil {
		return "", err
	}
	dir := tiles.SetDir(filepath.Join(dataDir, "tiles"), seed, gen)
	workers := max(1, runtime.GOMAXPROCS(0)-1)
	start := time.Now()
	last := -1
	progress := func(done, total int) {
		if pct := done * 10 / total; pct != last {
			last = pct
			fmt.Fprintf(out, "rendering %d/%d tiles\n", done, total)
		}
	}
	if err := tiles.Render(ctx, g.TerrainView(), tiles.Options{Dir: dir, Workers: workers}, progress); err != nil {
		return "", err
	}
	fmt.Fprintf(out, "rendered in %s\n", time.Since(start).Round(time.Millisecond))
	return dir, nil
}
