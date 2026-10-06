package store

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/logwatch"
)

func TestMigrationTwoUpgradesAVersionOneDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "farsight.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Populate v1 data that must survive the upgrade untouched.
	if _, err := s.InsertEventIfNew(ctx, nil, "srv", logwatch.Event{ID: "e1", Type: logwatch.EvPlayerJoin, At: ms("2026-10-01T00:00:00Z"), Name: "Astrid"}); err != nil {
		t.Fatal(err)
	}
	until := ms("2026-10-01T01:00:00Z")
	if err := s.InsertClosedSession(ctx, nil, Session{ServerID: "srv", Name: "Astrid", Platform: "Steam", PlatformID: "1", Since: ms("2026-10-01T00:00:00Z"), Until: &until, Seconds: 3600, Reason: "left"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutSnapshot(ctx, "srv", "save1", ms("2026-10-01T00:00:00Z"), ms("2026-10-01T00:00:01Z"), []byte("v1-snapshot")); err != nil {
		t.Fatal(err)
	}

	// Roll the file back to schema version 1.
	for _, q := range []string{`DROP TABLE world_diff`, `DROP TABLE tombstones`, `DROP INDEX idx_sessions_server_platform`, `UPDATE schema_version SET v = 1`} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var v int
	if err := s.db.QueryRowContext(ctx, `SELECT v FROM schema_version`).Scan(&v); err != nil || v != len(migrations) {
		t.Fatalf("schema version = %d (err %v), want %d", v, err, len(migrations))
	}
	if _, ok, err := s.WorldDiffState(ctx, nil, "srv"); err != nil || ok {
		t.Fatalf("world_diff after upgrade: ok=%v err=%v", ok, err)
	}
	if _, err := s.InsertTombstone(ctx, nil, "srv", Tombstone{ID: "t", Owner: "A", FirstSeen: ms("2026-10-01T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}

	// The v1 rows seeded before the rollback must have survived the
	// upgrade untouched.
	acts, err := s.RecentActivity(ctx, "srv", 10)
	if err != nil || len(acts) != 1 || acts[0].ID != "e1" || acts[0].Name != "Astrid" {
		t.Fatalf("event after upgrade = %+v err=%v, want e1/Astrid to survive", acts, err)
	}
	sessions, err := s.PlayerSessions(ctx, "srv", "1")
	if err != nil || len(sessions) != 1 || sessions[0].Seconds != 3600 || sessions[0].Reason != "left" {
		t.Fatalf("session after upgrade = %+v err=%v, want the closed v1 session to survive", sessions, err)
	}
	blob, _, ok, err := s.LatestSnapshot(ctx, "srv")
	if err != nil || !ok || string(blob) != "v1-snapshot" {
		t.Fatalf("snapshot after upgrade = %q ok=%v err=%v, want v1-snapshot to survive", blob, ok, err)
	}
}

func TestEventsBetweenWindowOrderAndNoise(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, e := range []logwatch.Event{
		{ID: "before", Type: logwatch.EvPlayerJoin, At: ms("2026-10-01T23:59:59Z")},
		{ID: "a", Type: logwatch.EvPlayerJoin, At: ms("2026-10-02T00:00:00Z")},
		{ID: "b", Type: logwatch.EvWorldSaved, At: ms("2026-10-02T12:00:00Z")},
		{ID: "c", Type: logwatch.EvPlayerLeave, At: ms("2026-10-02T12:00:00Z")},
		{ID: "hb", Type: logwatch.EvHeartbeat, At: ms("2026-10-02T13:00:00Z")},
		{ID: "at-until", Type: logwatch.EvPlayerJoin, At: ms("2026-10-03T00:00:00Z")},
	} {
		if _, err := s.InsertEventIfNew(ctx, nil, "srv", e); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.InsertRawEvent(ctx, nil, "srv", StoredEvent{ID: "w", Type: "world_boss", At: ms("2026-10-02T06:00:00Z"), Body: []byte(`{"boss":"Moder"}`)}); err != nil {
		t.Fatal(err)
	}
	got, err := s.EventsBetween(ctx, "srv", ms("2026-10-02T00:00:00Z"), ms("2026-10-03T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range got {
		ids = append(ids, e.ID)
	}
	want := []string{"c", "b", "w", "a"} // newest first, ties by id descending
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
	if string(got[2].Body) != `{"boss":"Moder"}` || got[2].Type != "world_boss" {
		t.Fatalf("raw event = %+v", got[2])
	}

	first, ok, err := s.EarliestEvent(ctx, "srv")
	if err != nil || !ok || !first.Equal(ms("2026-10-01T23:59:59Z")) {
		t.Fatalf("earliest = %v ok=%v err=%v", first, ok, err)
	}
	if _, ok, err := s.EarliestEvent(ctx, "other"); err != nil || ok {
		t.Fatalf("earliest on an empty server: ok=%v err=%v", ok, err)
	}
}

func TestPlayerSessionsAndOverlapping(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	closed := func(name, id, since, until string) Session {
		u := ms(until)
		return Session{ServerID: "srv", Name: name, Platform: "Steam", PlatformID: id, Since: ms(since), Until: &u, Reason: "left"}
	}
	for _, sess := range []Session{
		closed("Astrid", "1", "2026-10-01T10:00:00Z", "2026-10-01T11:00:00Z"),
		closed("Astrid", "1", "2026-10-01T23:00:00Z", "2026-10-02T01:00:00Z"), // across midnight
		closed("Bjorn", "2", "2026-10-02T08:00:00Z", "2026-10-02T09:00:00Z"),
		closed("Bjorn", "2", "2026-10-03T08:00:00Z", "2026-10-03T09:00:00Z"),
	} {
		if err := s.InsertClosedSession(ctx, nil, sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.OpenSession(ctx, nil, Session{ServerID: "srv", Name: "Astrid", Platform: "Steam", PlatformID: "1", Since: ms("2026-10-02T20:00:00Z")}); err != nil {
		t.Fatal(err)
	}

	astrid, err := s.PlayerSessions(ctx, "srv", "1")
	if err != nil || len(astrid) != 3 || astrid[2].Until != nil {
		t.Fatalf("Astrid's sessions = %+v err=%v", astrid, err)
	}

	day, err := s.SessionsOverlapping(ctx, "srv", ms("2026-10-02T00:00:00Z"), ms("2026-10-03T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if len(day) != 3 {
		t.Fatalf("2 Oct sessions = %+v, want the one across midnight, Bjorn's and the open one", day)
	}
	if !day[0].Since.Equal(ms("2026-10-01T23:00:00Z")) || day[1].Name != "Bjorn" || day[2].Until != nil {
		t.Fatalf("2 Oct sessions = %+v", day)
	}
}

// Each card poll asks for the sessions since the last save: the query
// must range-scan the (server_id, until) index from the window's start
// (plus the open sessions), not read the server's whole session history.
func TestSessionsOverlappingUsesTheIndex(t *testing.T) {
	s := newTestStore(t)
	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+sessionsOverlappingSQL, "srv", int64(1), "srv", int64(0), int64(1))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var searches []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		t.Logf("plan: %s", detail)
		if strings.HasPrefix(detail, "SCAN") || strings.HasPrefix(detail, "SEARCH") {
			searches = append(searches, detail)
		}
	}
	want := []string{
		"SEARCH sessions USING INDEX idx_sessions_server_until (server_id=? AND until=?)",
		"SEARCH sessions USING INDEX idx_sessions_server_until (server_id=? AND until>?)",
	}
	if !slices.Equal(searches, want) {
		t.Fatalf("searches = %q, want %q", searches, want)
	}
}

func TestPlayersNewestNameOnlineFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := func(v string) *time.Time { t := ms(v); return &t }
	for _, sess := range []Session{
		{ServerID: "srv", Name: "OldName", Platform: "Steam", PlatformID: "1", Since: ms("2026-10-01T10:00:00Z"), Until: u("2026-10-01T11:00:00Z")},
		{ServerID: "srv", Name: "Astrid", Platform: "Steam", PlatformID: "1", Since: ms("2026-10-02T10:00:00Z"), Until: u("2026-10-02T11:00:00Z")},
		{ServerID: "srv", Name: "Ulf", Platform: "Steam", PlatformID: "4", Since: ms("2026-10-03T10:00:00Z"), Until: u("2026-10-03T11:00:00Z")},
		{ServerID: "srv", Name: "Nobody", Platform: "", PlatformID: "", Since: ms("2026-10-03T10:00:00Z"), Until: u("2026-10-03T11:00:00Z")},
	} {
		if err := s.InsertClosedSession(ctx, nil, sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.OpenSession(ctx, nil, Session{ServerID: "srv", Name: "Bjorn", Platform: "Xbox", PlatformID: "2", Since: ms("2026-10-01T09:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	ps, err := s.Players(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 3 {
		t.Fatalf("players = %+v, want 3 (no empty platform ID)", ps)
	}
	if ps[0].Name != "Bjorn" || !ps[0].Online || ps[0].Platform != "Xbox" {
		t.Fatalf("first = %+v, want Bjorn online", ps[0])
	}
	if ps[1].Name != "Ulf" || ps[2].Name != "Astrid" || !ps[2].LastSeen.Equal(ms("2026-10-02T11:00:00Z")) ||
		len(ps[2].Names) != 2 || ps[2].Names[0] != "OldName" {
		t.Fatalf("players = %+v, want Ulf then Astrid (renamed from OldName)", ps)
	}
}

func TestEventsOfTypeOldestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, e := range []logwatch.Event{
		{ID: "d2", Type: logwatch.EvPlayerDeath, At: ms("2026-10-02T12:00:00Z"), Name: "B"},
		{ID: "j", Type: logwatch.EvPlayerJoin, At: ms("2026-10-02T11:00:00Z"), Name: "A"},
		{ID: "d1", Type: logwatch.EvPlayerDeath, At: ms("2026-10-02T10:00:00Z"), Name: "A", PlatformID: "1"},
	} {
		if _, err := s.InsertEventIfNew(ctx, nil, "srv", e); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.InsertEventIfNew(ctx, nil, "other", logwatch.Event{ID: "d3", Type: logwatch.EvPlayerDeath, At: ms("2026-10-02T10:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	got, err := s.EventsOfType(ctx, "srv", logwatch.EvPlayerDeath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "d1" || got[1].ID != "d2" || !got[0].At.Equal(ms("2026-10-02T10:00:00Z")) || !strings.Contains(string(got[0].Body), `"platformId":"1"`) {
		t.Fatalf("deaths = %+v", got)
	}
}

// A profile asks for every death on the server: the query must search
// the (server_id, type, at) index, not scan the server's whole history.
func TestEventsOfTypeUsesTheIndex(t *testing.T) {
	s := newTestStore(t)
	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+eventsOfTypeSQL, "srv", "player_death")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	want := []string{"SEARCH events USING INDEX idx_events_server_type_at (server_id=? AND type=?)"}
	if !slices.Equal(plan, want) {
		t.Fatalf("plan = %q, want %q", plan, want)
	}
}
