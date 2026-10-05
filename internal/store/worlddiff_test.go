package store

import (
	"context"
	"testing"
)

func TestWorldDiffStateRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, ok, err := s.WorldDiffState(ctx, nil, "srv"); err != nil || ok {
		t.Fatalf("fresh state: ok=%v err=%v", ok, err)
	}
	for _, k := range []SnapshotKey{
		{SaveID: "chunked:1", SavedAt: ms("2026-10-01T10:00:00Z")},
		{SaveID: "chunked:2", SavedAt: ms("2026-10-01T10:20:00Z")},
	} {
		if err := s.PutWorldDiffState(ctx, nil, "srv", k); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := s.WorldDiffState(ctx, nil, "srv")
	if err != nil || !ok || got.SaveID != "chunked:2" || !got.SavedAt.Equal(ms("2026-10-01T10:20:00Z")) {
		t.Fatalf("state = %+v ok=%v err=%v", got, ok, err)
	}
}

func TestSnapshotsAfterAndSnapshot(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	put := func(id, at, body string) {
		t.Helper()
		if _, err := s.PutSnapshot(ctx, "srv", id, ms(at), ms(at), []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	put("b", "2026-10-01T10:00:00Z", "B")
	put("a", "2026-10-01T10:00:00Z", "A") // same save time: ordered by id
	put("c", "2026-10-01T11:00:00Z", "C")
	if _, err := s.PutSnapshot(ctx, "other", "x", ms("2026-10-01T09:00:00Z"), ms("2026-10-01T09:00:00Z"), []byte("X")); err != nil {
		t.Fatal(err)
	}

	all, err := s.SnapshotsAfter(ctx, "srv", SnapshotKey{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].SaveID != "a" || all[1].SaveID != "b" || all[2].SaveID != "c" {
		t.Fatalf("all = %+v, want a, b, c", all)
	}
	rest, err := s.SnapshotsAfter(ctx, "srv", all[0])
	if err != nil || len(rest) != 2 || rest[0].SaveID != "b" {
		t.Fatalf("after a = %+v err=%v, want b, c", rest, err)
	}
	none, err := s.SnapshotsAfter(ctx, "srv", all[2])
	if err != nil || len(none) != 0 {
		t.Fatalf("after c = %+v err=%v", none, err)
	}

	blob, ok, err := s.Snapshot(ctx, "srv", "b")
	if err != nil || !ok || string(blob) != "B" {
		t.Fatalf("snapshot b = %q ok=%v err=%v", blob, ok, err)
	}
	if _, ok, err := s.Snapshot(ctx, "srv", "gone"); err != nil || ok {
		t.Fatalf("missing snapshot: ok=%v err=%v", ok, err)
	}
}

func TestTombstonesDistinctByIDOldestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, tb := range []Tombstone{
		{ID: "t2", Owner: "Bjorn", X: 5, Z: 6, FirstSeen: ms("2026-10-02T10:00:00Z")},
		{ID: "t1", Owner: "Bjorn", X: 1, Z: 2, FirstSeen: ms("2026-10-01T10:00:00Z")},
		{ID: "t3", Owner: "Astrid", X: 9, Z: 9, FirstSeen: ms("2026-10-01T10:00:00Z")},
	} {
		if ok, err := s.InsertTombstone(ctx, nil, "srv", tb); err != nil || !ok {
			t.Fatalf("insert %s: ok=%v err=%v", tb.ID, ok, err)
		}
	}
	if ok, err := s.InsertTombstone(ctx, nil, "srv", Tombstone{ID: "t1", Owner: "Bjorn", FirstSeen: ms("2026-10-05T10:00:00Z")}); err != nil || ok {
		t.Fatalf("duplicate insert: ok=%v err=%v", ok, err)
	}
	got, err := s.Tombstones(ctx, "srv", "Bjorn")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "t1" || got[0].X != 1 || !got[0].FirstSeen.Equal(ms("2026-10-01T10:00:00Z")) || got[1].ID != "t2" {
		t.Fatalf("Bjorn's tombstones = %+v", got)
	}
}
