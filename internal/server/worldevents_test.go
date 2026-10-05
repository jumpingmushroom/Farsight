package server

import (
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
)

// Ingesting a save diffs it against the previous one into world events;
// re-posting a save writes nothing new.
func TestIngestDerivesWorldEvents(t *testing.T) {
	e := newEnv(t)
	if err := e.post("alpha", "alpha-token", "snapshot", testSnapshot("s1", at(-30*time.Minute))); err != nil {
		t.Fatal(err)
	}
	s2 := testSnapshot("s2", at(-10*time.Minute))
	s2.Markers = append(s2.Markers, extract.Marker{ID: "tombstone-1", Kind: "tombstone", Owner: "Alice", X: 5, Z: 5})
	for i := 0; i < 2; i++ {
		if err := e.post("alpha", "alpha-token", "snapshot", s2); err != nil {
			t.Fatal(err)
		}
	}
	evs, err := e.store.EventsBetween(t.Context(), "alpha", at(-time.Hour), at(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != worldevents.TypeTombstone || !evs[0].At.Equal(s2.SavedAt) {
		t.Fatalf("events = %+v", evs)
	}
	tombs, err := e.store.Tombstones(t.Context(), "alpha", "Alice")
	if err != nil || len(tombs) != 1 {
		t.Fatalf("Alice's tombstones = %+v err=%v", tombs, err)
	}
}
