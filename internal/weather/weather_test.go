package weather

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/worldgen"
)

var biomeByKey = map[string]worldgen.Biome{
	"Meadows":     worldgen.Meadows,
	"BlackForest": worldgen.BlackForest,
	"Swamp":       worldgen.Swamp,
	"Mountain":    worldgen.Mountain,
	"Plains":      worldgen.Plains,
	"Ocean":       worldgen.Ocean,
	"Mistlands":   worldgen.Mistlands,
	"AshLands":    worldgen.AshLands,
	"DeepNorth":   worldgen.DeepNorth,
}

// TestEnvAtGolden checks EnvAt against testdata/golden.tsv, 1080 rows (120
// periods x 9 biomes) produced by the spike's weathercalc, which mirrors
// EnvMan.UpdateEnvironment bit for bit.
func TestEnvAtGolden(t *testing.T) {
	f, err := os.Open("testdata/golden.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 3 {
			t.Fatalf("malformed row %q", line)
		}
		period, err := strconv.ParseInt(cols[0], 10, 64)
		if err != nil {
			t.Fatalf("bad period %q: %v", cols[0], err)
		}
		b, ok := biomeByKey[cols[1]]
		if !ok {
			t.Fatalf("unknown biome key %q", cols[1])
		}
		want := cols[2]
		if got := EnvAt(period, b); got != want {
			t.Errorf("EnvAt(%d, %s) = %q, want %q", period, cols[1], got, want)
		}
		n++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("golden.tsv had no rows")
	}
}

func TestPeriod(t *testing.T) {
	cases := map[float64]int64{665.9: 0, 666: 1, 1331.99: 1}
	for netTime, want := range cases {
		if got := Period(netTime); got != want {
			t.Errorf("Period(%v) = %d, want %d", netTime, got, want)
		}
	}
}

// TestAtSwampIsAlwaysRain: Swamp's only entry is SwampRain, so the draw
// never matters.
func TestAtSwampIsAlwaysRain(t *testing.T) {
	for p := int64(0); p < 50; p++ {
		if got := At(p, worldgen.Swamp); got != "Rain" {
			t.Errorf("At(%d, Swamp) = %q, want Rain", p, got)
		}
	}
}

// TestEveryTableEnvHasADisplayName: every env name EnvAt can return must
// resolve to a non-empty display name, or At silently returns "".
func TestEveryTableEnvHasADisplayName(t *testing.T) {
	for b, es := range biomeEntries {
		for _, e := range es {
			if displayName[e.env] == "" {
				t.Errorf("biome %v env %q has no display name", b, e.env)
			}
		}
	}
}

func TestAtEmptyBiomeIsEmpty(t *testing.T) {
	if got := At(0, 0); got != "" {
		t.Errorf("At(0, 0) = %q, want \"\"", got)
	}
}
