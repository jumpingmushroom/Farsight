package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/logwatch"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	})
	return s
}

func ms(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

// --- Open ---

func TestOpenEmptyPathGivesIsolatedInMemoryDB(t *testing.T) {
	a := newTestStore(t)
	b := newTestStore(t)

	ctx := context.Background()
	if _, err := a.PutSnapshot(ctx, "srv", "save1", ms("2026-01-01T00:00:00Z"), ms("2026-01-01T00:00:01Z"), []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := b.LatestSnapshot(ctx, "srv"); err != nil || ok {
		t.Fatalf("second Open(\"\") saw data from the first: ok=%v err=%v", ok, err)
	}
}

func TestOpenEmptyPathUsableConcurrently(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := logwatch.Event{ID: itoa(i), Type: logwatch.EvPlayerJoin, At: ms("2026-01-01T00:00:00Z")}
			if _, err := s.InsertEventIfNew(ctx, nil, "srv", e); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	evs, err := s.RecentActivity(ctx, "srv", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != n {
		t.Fatalf("RecentActivity returned %d events, want %d", len(evs), n)
	}
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{digits[i%10]}, b...)
		i /= 10
	}
	return string(b)
}

func TestOpenPathPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "farsight.db")
	ctx := context.Background()

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s1.PutSnapshot(ctx, "srv", "save1", ms("2026-01-01T00:00:00Z"), ms("2026-01-01T00:00:01Z"), []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	blob, savedAt, ok, err := s2.LatestSnapshot(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || string(blob) != "payload" || !savedAt.Equal(ms("2026-01-01T00:00:00Z")) {
		t.Fatalf("after reopen: blob=%q savedAt=%v ok=%v", blob, savedAt, ok)
	}
}

// --- snapshots ---

func TestPutSnapshotIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	inserted, err := s.PutSnapshot(ctx, "srv", "save1", ms("2026-01-01T00:00:00Z"), ms("2026-01-01T00:00:01Z"), []byte("v1"))
	if err != nil || !inserted {
		t.Fatalf("first put: inserted=%v err=%v", inserted, err)
	}
	inserted, err = s.PutSnapshot(ctx, "srv", "save1", ms("2026-01-01T00:00:00Z"), ms("2026-01-01T00:00:01Z"), []byte("v1"))
	if err != nil || inserted {
		t.Fatalf("second put: inserted=%v err=%v, want false", inserted, err)
	}

	blob, _, ok, err := s.LatestSnapshot(ctx, "srv")
	if err != nil || !ok || string(blob) != "v1" {
		t.Fatalf("blob=%q ok=%v err=%v", blob, ok, err)
	}
}

func TestLatestSnapshotReturnsNewestAndGzipRoundTrips(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.PutSnapshot(ctx, "srv", "old", ms("2026-01-01T00:00:00Z"), ms("2026-01-01T00:00:01Z"), []byte("old-blob")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutSnapshot(ctx, "srv", "new", ms("2026-01-02T00:00:00Z"), ms("2026-01-02T00:00:01Z"), []byte("new-blob")); err != nil {
		t.Fatal(err)
	}

	blob, savedAt, ok, err := s.LatestSnapshot(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || string(blob) != "new-blob" || !savedAt.Equal(ms("2026-01-02T00:00:00Z")) {
		t.Fatalf("blob=%q savedAt=%v ok=%v", blob, savedAt, ok)
	}

	// The blob must actually be stored gzip-compressed, not just returned
	// decompressed by coincidence.
	var raw []byte
	if err := s.db.QueryRowContext(ctx, `SELECT blob FROM snapshots WHERE server_id = ? AND save_id = ?`, "srv", "new").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("stored blob is not gzip: %v", err)
	}
	var out bytes.Buffer
	if _, err := out.ReadFrom(gr); err != nil {
		t.Fatal(err)
	}
	if out.String() != "new-blob" {
		t.Fatalf("decompressed raw blob = %q", out.String())
	}
}

func TestLatestSnapshotNotFound(t *testing.T) {
	s := newTestStore(t)
	_, _, ok, err := s.LatestSnapshot(context.Background(), "nope")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v, want ok=false", ok, err)
	}
}

func TestLatestSnapshotIDMatchesLatestSnapshot(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, ok, err := s.LatestSnapshotID(ctx, "srv"); err != nil || ok {
		t.Fatalf("empty: ok=%v err=%v", ok, err)
	}
	for _, p := range []struct{ id, at string }{
		{"a", "2026-01-01T00:00:00Z"}, {"c", "2026-01-03T00:00:00Z"}, {"b", "2026-01-02T00:00:00Z"},
		{"d", "2026-01-03T00:00:00Z"}, // ties "c" on saved_at: save_id breaks the tie
	} {
		if _, err := s.PutSnapshot(ctx, "srv", p.id, ms(p.at), ms(p.at), []byte(p.id)); err != nil {
			t.Fatal(err)
		}
	}
	id, ok, err := s.LatestSnapshotID(ctx, "srv")
	blob, _, _, _ := s.LatestSnapshot(ctx, "srv")
	if err != nil || !ok || id != "d" || string(blob) != "d" {
		t.Fatalf("id=%q blob=%q ok=%v err=%v, want d and d", id, blob, ok, err)
	}
}

func TestPruneSnapshotsNoWorldDiffRowPrunesNothing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Server "a" has no world_diff row (the backfill hasn't run), so even
	// a 30-day-old snapshot must survive a 14-day prune.
	if _, err := s.PutSnapshot(ctx, "a", "s1", ms("2026-01-01T00:00:00Z"), ms("2026-01-01T00:00:01Z"), []byte("s1")); err != nil {
		t.Fatal(err)
	}

	n, err := s.PruneSnapshots(ctx, ms("2026-01-31T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("pruned %d rows, want 0 (no world_diff row for server a)", n)
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM snapshots WHERE server_id = 'a'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("snapshots remaining for a = %d, want 1", count)
	}
}

func TestPruneSnapshotsKeepsFromWorldDiffKeyOnwardRegardlessOfAge(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// "before": older than the cutoff and before world_diff's key, so it
	// must be pruned.
	if _, err := s.PutSnapshot(ctx, "a", "before", ms("2026-01-01T00:00:00Z"), ms("2026-01-01T00:00:01Z"), []byte("before")); err != nil {
		t.Fatal(err)
	}
	// "key": world_diff's own snapshot, the predecessor of the next
	// diff, older than the cutoff too but must be kept.
	if _, err := s.PutSnapshot(ctx, "a", "key", ms("2026-01-02T00:00:00Z"), ms("2026-01-02T00:00:01Z"), []byte("key")); err != nil {
		t.Fatal(err)
	}
	// "after": newer than world_diff's key, must also be kept.
	if _, err := s.PutSnapshot(ctx, "a", "after", ms("2026-01-03T00:00:00Z"), ms("2026-01-03T00:00:01Z"), []byte("after")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutWorldDiffState(ctx, nil, "a", SnapshotKey{SaveID: "key", SavedAt: ms("2026-01-02T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}

	n, err := s.PruneSnapshots(ctx, ms("2030-01-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pruned %d rows, want 1 (only \"before\")", n)
	}

	var remaining []string
	rows, err := s.db.QueryContext(ctx, `SELECT save_id FROM snapshots WHERE server_id = 'a' ORDER BY save_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		remaining = append(remaining, id)
	}
	if len(remaining) != 2 || remaining[0] != "after" || remaining[1] != "key" {
		t.Fatalf("remaining snapshots = %v, want [after, key]", remaining)
	}
}

// --- events ---

func TestEventsTableIsWithoutRowid(t *testing.T) {
	s := newTestStore(t)
	var sqlText string
	err := s.db.QueryRowContext(context.Background(), `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'events'`).Scan(&sqlText)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFold(sqlText, "WITHOUT ROWID") {
		t.Fatalf("events table schema = %q, want WITHOUT ROWID", sqlText)
	}
}

func containsFold(haystack, needle string) bool {
	return bytesContainsFold([]byte(haystack), []byte(needle))
}

func bytesContainsFold(haystack, needle []byte) bool {
	up := func(b []byte) []byte {
		out := make([]byte, len(b))
		for i, c := range b {
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
			out[i] = c
		}
		return out
	}
	return bytes.Contains(up(haystack), up(needle))
}

func TestInsertEventIfNewDedupesPerServer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	e := logwatch.Event{ID: "e1", Type: logwatch.EvPlayerJoin, At: ms("2026-01-01T00:00:00Z"), Name: "Alice"}

	inserted, err := s.InsertEventIfNew(ctx, nil, "srv-a", e)
	if err != nil || !inserted {
		t.Fatalf("first insert: inserted=%v err=%v", inserted, err)
	}
	inserted, err = s.InsertEventIfNew(ctx, nil, "srv-a", e)
	if err != nil || inserted {
		t.Fatalf("second insert (same server): inserted=%v err=%v, want false", inserted, err)
	}
	inserted, err = s.InsertEventIfNew(ctx, nil, "srv-b", e)
	if err != nil || !inserted {
		t.Fatalf("insert on another server: inserted=%v err=%v, want true", inserted, err)
	}
}

func TestRecentActivityExcludesHeartbeatAndPlayersNowOrdersDesc(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	events := []logwatch.Event{
		{ID: "1", Type: logwatch.EvHeartbeat, At: ms("2026-01-01T00:00:01Z")},
		{ID: "2", Type: logwatch.EvPlayersNow, At: ms("2026-01-01T00:00:02Z")},
		{ID: "3", Type: logwatch.EvPlayerJoin, At: ms("2026-01-01T00:00:03Z"), Name: "Alice"},
		{ID: "4", Type: logwatch.EvPlayerLeave, At: ms("2026-01-01T00:00:05Z"), Name: "Alice"},
		{ID: "5", Type: logwatch.EvWorldSaved, At: ms("2026-01-01T00:00:04Z")},
	}
	for _, e := range events {
		if _, err := s.InsertEventIfNew(ctx, nil, "srv", e); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.RecentActivity(ctx, "srv", 100)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{"4", "5", "3"}
	if len(got) != len(wantIDs) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(wantIDs), got)
	}
	for i, id := range wantIDs {
		if got[i].ID != id {
			t.Fatalf("event %d = %q, want %q (full: %+v)", i, got[i].ID, id, got)
		}
	}
	if got[0].Name != "Alice" {
		t.Fatalf("round-tripped event lost fields: %+v", got[0])
	}
}

func TestSaveTimesOrdersDesc(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i, at := range []string{"2026-01-01T00:00:01Z", "2026-01-01T00:00:03Z", "2026-01-01T00:00:02Z"} {
		e := logwatch.Event{ID: itoa(i), Type: logwatch.EvWorldSaved, At: ms(at)}
		if _, err := s.InsertEventIfNew(ctx, nil, "srv", e); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.SaveTimes(ctx, "srv", 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Time{ms("2026-01-01T00:00:03Z"), ms("2026-01-01T00:00:02Z"), ms("2026-01-01T00:00:01Z")}
	if len(got) != len(want) {
		t.Fatalf("got %d save times, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("save time %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestPruneEventsDeletesOnlyOldHeartbeatsAndPlayersNow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, e := range []logwatch.Event{
		{ID: "old-hb", Type: logwatch.EvHeartbeat, At: ms("2026-01-01T00:00:00Z")},
		{ID: "old-now", Type: logwatch.EvPlayersNow, At: ms("2026-01-01T00:00:00Z")},
		{ID: "old-join", Type: logwatch.EvPlayerJoin, At: ms("2026-01-01T00:00:00Z")},
		{ID: "new-hb", Type: logwatch.EvHeartbeat, At: ms("2026-01-03T00:00:00Z")},
		{ID: "new-join", Type: logwatch.EvPlayerJoin, At: ms("2026-01-03T00:00:00Z")},
	} {
		if _, err := s.InsertEventIfNew(ctx, nil, "srv", e); err != nil {
			t.Fatal(err)
		}
	}

	n, err := s.PruneEvents(ctx, ms("2026-01-02T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("pruned %d, want 2 (the old heartbeat and players_now)", n)
	}

	got, err := s.RecentActivity(ctx, "srv", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "new-join" || got[1].ID != "old-join" {
		t.Fatalf("remaining activity = %+v, want new-join then old-join", got)
	}
}

// --- sessions ---

func TestOpenCloseSessionByExactKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	since := ms("2026-01-01T00:00:00Z")

	if err := s.OpenSession(ctx, nil, Session{ServerID: "srv", Name: "Alice", Platform: "Steam", PlatformID: "p1", Since: since}); err != nil {
		t.Fatal(err)
	}

	online, err := s.Online(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 1 || online[0].Name != "Alice" {
		t.Fatalf("online = %+v", online)
	}

	until := ms("2026-01-01T01:00:00Z")
	found, err := s.CloseSession(ctx, nil, Session{ServerID: "srv", Name: "Alice", PlatformID: "p1", Since: since, Until: &until, Seconds: 3600, Reason: "left"})
	if err != nil || !found {
		t.Fatalf("close: found=%v err=%v", found, err)
	}

	online, err = s.Online(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 0 {
		t.Fatalf("online after close = %+v, want empty", online)
	}

	recent, err := s.Recent(ctx, "srv", since, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].Seconds != 3600 || recent[0].Reason != "left" || recent[0].Until == nil || !recent[0].Until.Equal(until) {
		t.Fatalf("recent = %+v", recent)
	}
}

func TestOpenSessionIgnoresDuplicateKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	since := ms("2026-01-01T00:00:00Z")
	sess := Session{ServerID: "srv", Name: "Alice", Platform: "Steam", PlatformID: "p1", Since: since}

	if err := s.OpenSession(ctx, nil, sess); err != nil {
		t.Fatal(err)
	}
	if err := s.OpenSession(ctx, nil, sess); err != nil {
		t.Fatal(err)
	}

	online, err := s.Online(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 1 {
		t.Fatalf("online = %+v, want exactly 1 row", online)
	}
}

func TestCloseSessionNoMatchReturnsFalse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	until := ms("2026-01-01T01:00:00Z")

	found, err := s.CloseSession(ctx, nil, Session{ServerID: "srv", Name: "Ghost", PlatformID: "p1", Since: ms("2026-01-01T00:00:00Z"), Until: &until})
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("found = true, want false for no matching open session")
	}
}

func TestCloseSessionNilUntilReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	since := ms("2026-01-01T00:00:00Z")
	sess := Session{ServerID: "srv", Name: "Alice", Platform: "Steam", PlatformID: "p1", Since: since}

	if err := s.OpenSession(ctx, nil, sess); err != nil {
		t.Fatal(err)
	}

	if _, err := s.CloseSession(ctx, nil, sess); err == nil {
		t.Fatal("CloseSession with nil Until: want error, got nil")
	}

	online, err := s.Online(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 1 {
		t.Fatalf("online after failed close = %+v, want the session still open", online)
	}
}

func TestInsertClosedSessionLeaveWithoutJoin(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	since := ms("2026-01-01T00:00:00Z")
	until := ms("2026-01-01T00:10:00Z")

	sess := Session{ServerID: "srv", Name: "Bob", Platform: "Steam", PlatformID: "p2", Since: since, Until: &until, Seconds: 600, Reason: "left"}
	if err := s.InsertClosedSession(ctx, nil, sess); err != nil {
		t.Fatal(err)
	}

	recent, err := s.Recent(ctx, "srv", since, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].Name != "Bob" || recent[0].Seconds != 600 {
		t.Fatalf("recent = %+v", recent)
	}

	online, err := s.Online(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 0 {
		t.Fatalf("online = %+v, want empty (session was inserted already closed)", online)
	}
}

func TestCloseAllOpenComputesAndClampsSeconds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Opened normally: until - since = 30s.
	if err := s.OpenSession(ctx, nil, Session{ServerID: "srv", Name: "Alice", Platform: "Steam", PlatformID: "p1", Since: ms("2026-01-01T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	// Opened with a since AFTER the until we'll close with, so seconds must clamp to 0.
	if err := s.OpenSession(ctx, nil, Session{ServerID: "srv", Name: "Bob", Platform: "Steam", PlatformID: "p2", Since: ms("2026-01-01T00:05:00Z")}); err != nil {
		t.Fatal(err)
	}

	until := ms("2026-01-01T00:00:30Z")
	n, err := s.CloseAllOpen(ctx, nil, "srv", until, "server_stopped")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("closed %d sessions, want 2", n)
	}

	recent, err := s.Recent(ctx, "srv", ms("2026-01-01T00:00:00Z"), 10)
	if err != nil {
		t.Fatal(err)
	}
	bySecond := map[string]int64{}
	for _, r := range recent {
		bySecond[r.Name] = r.Seconds
		if r.Reason != "server_stopped" {
			t.Fatalf("reason = %q, want server_stopped", r.Reason)
		}
	}
	if bySecond["Alice"] != 30 {
		t.Fatalf("Alice seconds = %d, want 30", bySecond["Alice"])
	}
	if bySecond["Bob"] != 0 {
		t.Fatalf("Bob seconds = %d, want 0 (clamped)", bySecond["Bob"])
	}

	online, err := s.Online(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 0 {
		t.Fatalf("online after CloseAllOpen = %+v, want empty", online)
	}
}

func TestOnlineOrdersSinceAsc(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.OpenSession(ctx, nil, Session{ServerID: "srv", Name: "Second", Platform: "Steam", PlatformID: "p2", Since: ms("2026-01-01T00:02:00Z")}); err != nil {
		t.Fatal(err)
	}
	if err := s.OpenSession(ctx, nil, Session{ServerID: "srv", Name: "First", Platform: "Steam", PlatformID: "p1", Since: ms("2026-01-01T00:01:00Z")}); err != nil {
		t.Fatal(err)
	}

	online, err := s.Online(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 2 || online[0].Name != "First" || online[1].Name != "Second" {
		t.Fatalf("online = %+v, want First then Second", online)
	}
}

func TestRecentFiltersAfterAndOrdersUntilDesc(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mk := func(name, platformID, since, until string) Session {
		u := ms(until)
		return Session{ServerID: "srv", Name: name, Platform: "Steam", PlatformID: platformID, Since: ms(since), Until: &u, Seconds: 1, Reason: "left"}
	}
	for _, sess := range []Session{
		mk("TooOld", "p1", "2026-01-01T00:00:00Z", "2026-01-01T00:01:00Z"),
		mk("Middle", "p2", "2026-01-02T00:00:00Z", "2026-01-02T00:01:00Z"),
		mk("Newest", "p3", "2026-01-03T00:00:00Z", "2026-01-03T00:01:00Z"),
	} {
		if err := s.InsertClosedSession(ctx, nil, sess); err != nil {
			t.Fatal(err)
		}
	}

	recent, err := s.Recent(ctx, "srv", ms("2026-01-01T12:00:00Z"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].Name != "Newest" || recent[1].Name != "Middle" {
		t.Fatalf("recent = %+v, want [Newest, Middle]", recent)
	}
}

func TestServersWithOpenSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.OpenSession(ctx, nil, Session{ServerID: "a", Name: "Alice", Platform: "Steam", PlatformID: "p1", Since: ms("2026-01-01T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	until := ms("2026-01-01T00:01:00Z")
	if err := s.InsertClosedSession(ctx, nil, Session{ServerID: "b", Name: "Bob", Platform: "Steam", PlatformID: "p2", Since: ms("2026-01-01T00:00:00Z"), Until: &until}); err != nil {
		t.Fatal(err)
	}

	servers, err := s.ServersWithOpenSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0] != "a" {
		t.Fatalf("servers = %+v, want [a]", servers)
	}
}

// --- live ---

func TestLiveRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, ok, err := s.GetLive(ctx, nil, "srv"); err != nil || ok {
		t.Fatalf("GetLive before PutLive: ok=%v err=%v", ok, err)
	}

	l := Live{
		ServerID:       "srv",
		Status:         "online",
		Version:        "0.220.5",
		JoinCode:       "123456",
		NetworkVersion: 210,
		Players:        3,
		LastHeartbeat:  ms("2026-01-01T00:00:10Z"),
		LastEventAt:    ms("2026-01-01T00:00:09Z"),
		UpSince:        ms("2026-01-01T00:00:00Z"),
		JoinCodeAt:     ms("2026-01-01T00:00:05Z"),
		StatusAt:       ms("2026-01-01T00:00:01Z"),
	}
	if err := s.PutLive(ctx, nil, l); err != nil {
		t.Fatal(err)
	}

	got, ok, err := s.GetLive(ctx, nil, "srv")
	if err != nil || !ok {
		t.Fatalf("GetLive after PutLive: ok=%v err=%v", ok, err)
	}
	if got != l {
		t.Fatalf("got %+v, want %+v", got, l)
	}

	// PutLive again with a zero time on one field must round-trip that
	// field back as the zero value (unknown), not epoch.
	l2 := l
	l2.LastHeartbeat = time.Time{}
	if err := s.PutLive(ctx, nil, l2); err != nil {
		t.Fatal(err)
	}
	got2, ok, err := s.GetLive(ctx, nil, "srv")
	if err != nil || !ok {
		t.Fatalf("GetLive after second PutLive: ok=%v err=%v", ok, err)
	}
	if !got2.LastHeartbeat.IsZero() {
		t.Fatalf("LastHeartbeat = %v, want zero", got2.LastHeartbeat)
	}
}

// --- Tx ---

func TestTxCommitsOnSuccessAndRollsBackOnError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.Tx(ctx, func(tx *sql.Tx) error {
		return s.OpenSession(ctx, tx, Session{ServerID: "srv", Name: "Alice", Platform: "Steam", PlatformID: "p1", Since: ms("2026-01-01T00:00:00Z")})
	}); err != nil {
		t.Fatal(err)
	}
	online, err := s.Online(ctx, "srv")
	if err != nil || len(online) != 1 {
		t.Fatalf("online = %+v err=%v, want 1 row committed", online, err)
	}

	sentinel := errors.New("boom")
	err = s.Tx(ctx, func(tx *sql.Tx) error {
		if err := s.OpenSession(ctx, tx, Session{ServerID: "srv", Name: "Bob", Platform: "Steam", PlatformID: "p2", Since: ms("2026-01-01T00:00:00Z")}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Tx err = %v, want sentinel", err)
	}
	online, err = s.Online(ctx, "srv")
	if err != nil || len(online) != 1 {
		t.Fatalf("online after rollback = %+v err=%v, want still 1 row (Bob rolled back)", online, err)
	}
}

func TestLatestEventOfType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, ok, err := s.LatestEventOfType(ctx, "srv", logwatch.EvTimeSkip); err != nil || ok {
		t.Fatalf("empty: ok=%v err=%v, want none", ok, err)
	}
	for _, e := range []logwatch.Event{
		{ID: "a", Type: logwatch.EvTimeSkip, At: ms("2026-01-01T00:00:01Z"), To: 100},
		{ID: "b", Type: logwatch.EvTimeSkip, At: ms("2026-01-01T00:00:03Z"), To: 300},
		{ID: "c", Type: logwatch.EvTimeSkip, At: ms("2026-01-01T00:00:02Z"), To: 200},
		{ID: "d", Type: logwatch.EvWorldSaved, At: ms("2026-01-01T00:00:09Z")},
	} {
		if _, err := s.InsertEventIfNew(ctx, nil, "srv", e); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.InsertEventIfNew(ctx, nil, "other", logwatch.Event{ID: "x", Type: logwatch.EvTimeSkip, At: ms("2026-01-01T00:00:05Z")}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LatestEventOfType(ctx, "srv", logwatch.EvTimeSkip)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	var e logwatch.Event
	if err := json.Unmarshal(got.Body, &e); err != nil {
		t.Fatal(err)
	}
	if got.ID != "b" || got.Type != logwatch.EvTimeSkip || !got.At.Equal(ms("2026-01-01T00:00:03Z")) || e.To != 300 {
		t.Fatalf("latest = %+v (to %v), want b", got, e.To)
	}
}
