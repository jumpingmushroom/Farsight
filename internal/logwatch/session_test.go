package logwatch

import (
	"bufio"
	"os"
	"testing"
	"time"
)

func feedFile(t *testing.T, path string) []Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	loc := oslo(t)
	s := NewSessionizer()
	var out []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		for _, r := range ParseServerLine(sc.Text(), loc) {
			out = append(out, s.Feed(r)...)
		}
	}
	return out
}

func ofType(evs []Event, typ string) []Event {
	var out []Event
	for _, e := range evs {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func utc(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }

func TestCrossplayFixtureSessions(t *testing.T) {
	evs := feedFile(t, "testdata/valheim-crossplay.log")
	joins, leaves := ofType(evs, EvPlayerJoin), ofType(evs, EvPlayerLeave)
	if len(joins) != 2 || len(leaves) != 2 {
		t.Fatalf("joins=%+v leaves=%+v", joins, leaves)
	}
	j0, l0, j1, l1 := joins[0], leaves[0], joins[1], leaves[1]
	if j0.Name != "Thorgerdr" || j0.Platform != "Steam" || j0.PlatformID != "76561190000000007" || !j0.At.Equal(utc("2026-09-15T07:39:37Z")) {
		t.Fatalf("join 0 = %+v", j0)
	}
	if l0.Name != "Thorgerdr" || l0.Seconds != 4479 || l0.Reason != "left" || !l0.At.Equal(utc("2026-09-15T08:54:16Z")) {
		t.Fatalf("leave 0 = %+v", l0)
	}
	if j1.Name != "Thorvaldsson" || j1.PlatformID != "76561190000000003" || !j1.At.Equal(utc("2026-09-15T18:51:41Z")) {
		t.Fatalf("join 1 = %+v", j1)
	}
	if l1.Name != "Thorvaldsson" || l1.Seconds != 125 {
		t.Fatalf("leave 1 = %+v", l1)
	}
	codes := map[string]bool{}
	for _, e := range ofType(evs, EvJoinCode) {
		codes[e.Code] = true
	}
	if !codes["114544"] || !codes["113433"] || len(codes) != 2 {
		t.Fatalf("join codes = %v", codes)
	}
	pn := ofType(evs, EvPlayersNow)
	if len(pn) == 0 || *pn[len(pn)-1].Players != 0 {
		t.Fatalf("last players_now = %+v", pn[len(pn)-1])
	}
	for _, e := range evs {
		if e.ID == "" || len(e.ID) != 16 {
			t.Fatalf("event without a 16-hex id: %+v", e)
		}
	}
}

func TestSteamFixtureSessionAndBoot(t *testing.T) {
	evs := feedFile(t, "testdata/valheim-1.0.log")
	joins, leaves := ofType(evs, EvPlayerJoin), ofType(evs, EvPlayerLeave)
	if len(joins) != 1 || joins[0].Name != "Orm" || joins[0].Platform != "Steam" || joins[0].PlatformID != "76561190000000007" {
		t.Fatalf("joins = %+v", joins)
	}
	if len(leaves) != 1 || leaves[0].Seconds != 1011 || leaves[0].Reason != "left" {
		t.Fatalf("leaves = %+v", leaves)
	}
	boots := ofType(evs, EvServerBoot)
	if len(boots) != 1 || boots[0].Version != "l-1.0.7" || boots[0].NetworkVersion != 39 {
		t.Fatalf("boots = %+v", boots)
	}
	if len(ofType(evs, EvWorldSaved)) == 0 {
		t.Fatal("world_saved missing")
	}
}

func TestStopClosesOpenSessionsAndRespawnIsIgnored(t *testing.T) {
	s := NewSessionizer()
	at := func(sec int) time.Time { return time.Date(2026, 9, 29, 10, 0, sec, 0, time.UTC) }
	var evs []Event
	evs = append(evs, s.Feed(Raw{Kind: RawIdentity, At: at(0), Platform: "Steam", PlatformID: "1"})...)
	evs = append(evs, s.Feed(Raw{Kind: RawSpawn, At: at(5), Name: "A", UID: 11})...)
	evs = append(evs, s.Feed(Raw{Kind: RawSpawn, At: at(9), Name: "A", UID: 11})...) // respawn
	evs = append(evs, s.Feed(Raw{Kind: RawIdentity, At: at(10), Platform: "Xbox", PlatformID: "2"})...)
	evs = append(evs, s.Feed(Raw{Kind: RawSpawn, At: at(12), Name: "B", UID: 22})...)
	evs = append(evs, s.Feed(Raw{Kind: RawStopped, At: at(30)})...)
	evs = append(evs, s.Feed(Raw{Kind: RawDestroy, At: at(31), UID: 11})...) // after stop: nothing
	if j := ofType(evs, EvPlayerJoin); len(j) != 2 || j[1].Platform != "Xbox" {
		t.Fatalf("joins = %+v", j)
	}
	l := ofType(evs, EvPlayerLeave)
	if len(l) != 2 || l[0].Name != "A" || l[0].Reason != "server_stopped" || l[0].Seconds != 25 || l[1].Name != "B" || l[1].Seconds != 18 {
		t.Fatalf("leaves = %+v", l)
	}
	if last := evs[len(evs)-1]; last.Type != EvServerStopped {
		t.Fatalf("last event = %+v, want server_stopped", last)
	}
}

func TestEventIDDeterministic(t *testing.T) {
	a := feedFile(t, "testdata/valheim-crossplay.log")
	b := feedFile(t, "testdata/valheim-crossplay.log")
	if len(a) != len(b) {
		t.Fatal("replay length differs")
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("event %d id differs across replays", i)
		}
	}
}

func TestRaidEvent(t *testing.T) {
	at := time.Date(2026, 10, 3, 19, 14, 5, 0, time.UTC)
	s := NewSessionizer()
	evs := s.Feed(Raw{Kind: RawRaid, At: at, Raid: "army_theelder"})
	if len(evs) != 1 {
		t.Fatalf("events = %+v", evs)
	}
	e := evs[0]
	if e.Type != EvRaid || e.Raid != "army_theelder" || !e.At.Equal(at) || e.ID == "" {
		t.Fatalf("raid event = %+v", e)
	}
	if again := s.Feed(Raw{Kind: RawRaid, At: at, Raid: "army_theelder"}); again[0].ID != e.ID {
		t.Fatalf("raid IDs differ on replay: %s vs %s", again[0].ID, e.ID)
	}
}
