package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/fog"
	"github.com/jumpingmushroom/farsight/internal/store"
)

var discardLog = slog.New(slog.DiscardHandler)

// Fix 1: a padded-but-valid explored encoding (one a broken or hostile
// agent could send: Decode only checks the first maskBytes+1 decompressed
// bytes, so an extra, empty gzip member still decodes) must never reach
// browsers as received. newWorldState re-encodes the decoded mask, so the
// stored enc is byte-identical to a canonical Encode of the same mask.
func TestNewWorldStateCanonicalizesTheExploredEncoding(t *testing.T) {
	m := explored.New()
	m.Set(5, 5)
	m.Set(1000, 1500)

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(m.Bits()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	// An extra, empty gzip member: it decompresses to zero extra bytes
	// (gzip.Reader concatenates multistream members by default), so
	// Decode still accepts it, but it pads the wire form.
	zw2 := gzip.NewWriter(&buf)
	if err := zw2.Close(); err != nil {
		t.Fatal(err)
	}
	padded := explored.Encoded{
		Source: explored.SourceTables,
		Cell:   explored.CellMetres,
		Size:   explored.Size,
		Bits:   base64.StdEncoding.EncodeToString(buf.Bytes()),
	}

	decoded, err := explored.Decode(padded)
	if err != nil {
		t.Fatalf("decode padded: %v", err)
	}
	canonical := explored.Encode(decoded, explored.SourceTables)
	if padded.Bits == canonical.Bits {
		t.Fatal("test input is not actually padded relative to the canonical encoding")
	}

	snap := &extract.Snapshot{ServerID: "alpha", SaveID: "s1", SavedAt: t0, Explored: &padded}
	w := newWorldState(snap, discardLog)
	if w.enc != canonical {
		t.Errorf("enc = %+v, want canonical %+v", w.enc, canonical)
	}
}

// Plan 8, I3: ws.snap.Locations is cleared once classified into ws.locs,
// since nothing else reads it and it's the bulk of a decoded snapshot
// (raw agent locations, filtered to the explored mask by the agent but
// still the biggest slice on the struct). The caller's own snapshot must
// be untouched: worldState copies rather than mutating it, since a
// snapshot is shared read-only.
func TestNewWorldStateClearsSnapLocations(t *testing.T) {
	snap := &extract.Snapshot{
		ServerID: "alpha", SaveID: "s1", SavedAt: t0,
		Locations: []extract.Marker{{ID: "loc-1", Kind: "location", Type: "Eikthyrnir"}},
	}
	w := newWorldState(snap, discardLog)
	if w.snap.Locations != nil {
		t.Errorf("ws.snap.Locations = %+v, want nil", w.snap.Locations)
	}
	if len(w.locs) != 1 || w.locs[0].Kind != "boss_altar" {
		t.Errorf("ws.locs = %+v, want the classified Eikthyr altar", w.locs)
	}
	if len(snap.Locations) != 1 {
		t.Errorf("caller's snapshot was mutated: Locations = %+v", snap.Locations)
	}
}

// cardBosses rebuilds the boss list from GlobalKeys rather than
// trusting the stored Bosses, so the central app doesn't depend on the
// agent having been updated to drop the invented 8th boss, Writhan
// (updating the agent restarts the game server). A snapshot stored before
// GlobalKeys existed has no "globalKeys" key at all, so it decodes to a
// nil slice; that's the only case this falls back to the stored Bosses,
// filtering out any defeated_writhan entry.
func TestCardBosses(t *testing.T) {
	t.Run("rebuilds from GlobalKeys, ignoring stale stored Bosses", func(t *testing.T) {
		snap := &extract.Snapshot{
			GlobalKeys: []string{"activebosses 0", "defeated_eikthyr", "killedtroll", "defeated_gdking", "defeated_writhan", "defeated_bonemass"},
			Bosses: []extract.Boss{
				{Key: "defeated_eikthyr", Name: "Eikthyr", Defeated: true},
				{Key: "defeated_writhan", Name: "Writhan", Defeated: true},
			},
		}
		bosses := cardBosses(snap)
		if len(bosses) != 8 {
			t.Fatalf("len = %d, want 8: %+v", len(bosses), bosses)
		}
		defeated := map[string]bool{}
		n := 0
		for _, b := range bosses {
			if b.Key == "defeated_writhan" {
				t.Fatalf("writhan present: %+v", bosses)
			}
			if b.Defeated {
				n++
				defeated[b.Name] = true
			}
		}
		if n != 3 || !defeated["Eikthyr"] || !defeated["The Elder"] || !defeated["Bonemass"] {
			t.Fatalf("defeated = %+v, want exactly Eikthyr, The Elder, Bonemass", defeated)
		}
	})

	t.Run("falls back to stored Bosses, filtering writhan, when GlobalKeys is absent", func(t *testing.T) {
		snap := &extract.Snapshot{
			Bosses: []extract.Boss{
				{Key: "defeated_eikthyr", Name: "Eikthyr", Defeated: true},
				{Key: "defeated_gdking", Name: "The Elder", Defeated: false},
				{Key: "defeated_writhan", Name: "Writhan", Defeated: true},
			},
		}
		bosses := cardBosses(snap)
		if len(bosses) != 2 || bosses[0].Key != "defeated_eikthyr" || !bosses[0].Defeated || bosses[1].Defeated {
			t.Fatalf("bosses = %+v, want eikthyr(true), gdking(false), writhan filtered", bosses)
		}
	})
}

// newTestWorldCache is a worldCache over a fresh in-memory store, for
// tests that drive it directly rather than through the HTTP handlers.
func newTestWorldCache(t *testing.T) (*worldCache, *store.Store) {
	t.Helper()
	st, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return newWorldCache(st, discardLog), st
}

// putTestSnapshot stores a testSnapshot for serverID (testSnapshot always
// stamps ServerID "alpha"; callers for another id must override it in
// mutate), optionally mutated before marshalling.
func putTestSnapshot(t *testing.T, st *store.Store, serverID, saveID string, savedAt time.Time, mutate func(*extract.Snapshot)) {
	t.Helper()
	snap := testSnapshot(saveID, savedAt)
	snap.ServerID = serverID
	if mutate != nil {
		mutate(&snap)
	}
	blob, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutSnapshot(context.Background(), serverID, saveID, savedAt, snap.ReadAt, blob); err != nil {
		t.Fatal(err)
	}
}

// Fix 2: the field and class map depend only on the mask, so a later
// snapshot whose fog key hasn't changed must reuse the previous state's,
// not pay the rebuild again. A changed mask gets its own.
func TestWorldCacheReusesFogDataWhenTheFogKeyIsUnchanged(t *testing.T) {
	c, st := newTestWorldCache(t)
	ctx := context.Background()
	putTestSnapshot(t, st, "alpha", "s1", at(-2*time.Minute), nil)

	ws1, ok, err := c.get(ctx, "alpha")
	if err != nil || !ok {
		t.Fatalf("get 1: ok=%v err=%v", ok, err)
	}
	field1, classes1 := ws1.fogData()

	// A later save, same exploration: the fog key is unchanged.
	putTestSnapshot(t, st, "alpha", "s2", at(-time.Minute), nil)
	ws2, ok, err := c.get(ctx, "alpha")
	if err != nil || !ok {
		t.Fatalf("get 2: ok=%v err=%v", ok, err)
	}
	if ws2.fogKey != ws1.fogKey {
		t.Fatalf("fog key changed without new exploration: %q -> %q", ws1.fogKey, ws2.fogKey)
	}
	field2, classes2 := ws2.fogData()
	if field2 != field1 || classes2 != classes1 {
		t.Errorf("fog data rebuilt for an unchanged fog key: field %p -> %p, classes %p -> %p",
			field1, field2, classes1, classes2)
	}

	// More exploration: a new fog key, new fog data.
	putTestSnapshot(t, st, "alpha", "s3", at(0), func(s *extract.Snapshot) {
		for x := int16(0); x < 40; x++ {
			s.ExploredZones = append(s.ExploredZones, [2]int16{x, 40})
		}
	})
	ws3, ok, err := c.get(ctx, "alpha")
	if err != nil || !ok {
		t.Fatalf("get 3: ok=%v err=%v", ok, err)
	}
	if ws3.fogKey == ws2.fogKey {
		t.Fatal("fog key unchanged despite new exploration")
	}
	field3, _ := ws3.fogData()
	if field3 == field2 {
		t.Error("fog data shared across different fog keys")
	}
}

// Fix 3: many goroutines call get for the same and different servers
// while new snapshots are ingested. Every returned state must be
// complete and its fog key must match the mask it carries. Run under
// go test -race (CI does; there is no race detector in this sandbox).
func TestWorldCacheGetIsConsistentUnderConcurrentIngestAndReads(t *testing.T) {
	c, st := newTestWorldCache(t)
	ctx := context.Background()
	ids := []string{"alpha", "beta"}
	for _, id := range ids {
		putTestSnapshot(t, st, id, "s0", at(-time.Hour), nil)
	}

	const readers = 8
	const saves = 20
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := ids[n%len(ids)]
			for {
				select {
				case <-stop:
					return
				default:
				}
				ws, ok, err := c.get(ctx, id)
				if err != nil {
					t.Errorf("get(%s): %v", id, err)
					return
				}
				if !ok {
					t.Errorf("get(%s): not found", id)
					return
				}
				if ws.snap == nil || ws.mask == nil || ws.snap.ServerID != id {
					t.Errorf("get(%s): incomplete or mismatched state %+v", id, ws)
					return
				}
				if want := fog.Key(ws.enc.Source, ws.mask); ws.fogKey != want {
					t.Errorf("get(%s): fogKey = %q, want %q for its own mask", id, ws.fogKey, want)
					return
				}
			}
		}(i)
	}

	for i := 0; i < saves; i++ {
		id := ids[i%len(ids)]
		putTestSnapshot(t, st, id, fmt.Sprintf("s%d", i+1), at(time.Duration(i)*time.Second), nil)
	}
	close(stop)
	wg.Wait()
}
