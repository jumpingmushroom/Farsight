package logwatch

import (
	"testing"
	"time"
)

// On 2026-10-25 Europe/Oslo falls back from CEST (UTC+2) to CET (UTC+1)
// at 03:00 CEST, so the local wall-clock hour 02:00–02:59 happens twice:
// first as 00:00–00:59 UTC (CEST), then as 01:00–01:59 UTC (CET).

func TestClockDisambiguatesFallBackHour(t *testing.T) {
	c := newClock(oslo(t))
	const layout = "2006-01-02 15:04:05"
	for _, tc := range []struct{ wall, want string }{
		{"2026-10-25 01:50:00", "2026-10-24T23:50:00Z"}, // unambiguous, CEST
		{"2026-10-25 02:00:00", "2026-10-25T00:00:00Z"}, // first pass: CEST
		{"2026-10-25 02:59:59", "2026-10-25T00:59:59Z"},
		{"2026-10-25 02:00:00", "2026-10-25T01:00:00Z"}, // second pass: CET
		{"2026-10-25 02:10:00", "2026-10-25T01:10:00Z"},
		{"2026-10-25 02:59:59", "2026-10-25T01:59:59Z"},
		{"2026-10-25 03:00:00", "2026-10-25T02:00:00Z"}, // unambiguous, CET
	} {
		got, ok := c.at(layout, tc.wall)
		if !ok || !got.Equal(utc(tc.want)) || got.Location() != time.UTC {
			t.Fatalf("at(%s) = %v, %v; want %s", tc.wall, got, ok, tc.want)
		}
	}
}

// A fresh clock (the stateless ParseServerLine/ParseSupervisorLine path)
// has no history, so an ambiguous time resolves to the earlier instant.
func TestStatelessParseTakesEarlierInstant(t *testing.T) {
	r := one(t, ParseServerLine("10/25/2026 02:40:00: Game server connected", oslo(t)))
	if !r.At.Equal(utc("2026-10-25T00:40:00Z")) {
		t.Fatalf("server At = %v, want 00:40Z (CEST)", r.At)
	}
	r = one(t, ParseSupervisorLine("2026-10-25 02:40:00,000 INFO spawned: 'valheim-server' with pid 1", oslo(t)))
	if !r.At.Equal(utc("2026-10-25T00:40:00Z")) {
		t.Fatalf("supervisor At = %v, want 00:40Z (CEST)", r.At)
	}
}

func feedStream(t *testing.T, lines ...string) []Event {
	t.Helper()
	c := newClock(oslo(t))
	s := NewSessionizer()
	var out []Event
	for _, l := range lines {
		for _, r := range c.serverLine(l) {
			out = append(out, s.Feed(r)...)
		}
	}
	return out
}

func TestDSTFallBackSessionFirstPass(t *testing.T) {
	evs := feedStream(t,
		"10/25/2026 01:49:00: PlayFab socket with remote ID playfab/X received local Platform ID Steam_1",
		"10/25/2026 01:50:00: Got character ZDOID from A : 11:1",
		"10/25/2026 02:40:00: Destroying abandoned non persistent zdo 11:5 owner 11",
	)
	l := ofType(evs, EvPlayerLeave)
	if len(l) != 1 || l[0].Seconds != 3000 || !l[0].At.Equal(utc("2026-10-25T00:40:00Z")) {
		t.Fatalf("leaves = %+v, want one of 3000 s ending 00:40Z", l)
	}
}

func TestDSTFallBackSessionAcrossRepeat(t *testing.T) {
	evs := feedStream(t,
		"10/25/2026 02:29:00: PlayFab socket with remote ID playfab/X received local Platform ID Steam_1",
		"10/25/2026 02:30:00: Got character ZDOID from A : 11:1",                     // CEST
		"10/25/2026 02:50:00:  Connections 1 ZDOS:34937  sent:0 recv:85",             // CEST
		"10/25/2026 02:10:00: Destroying abandoned non persistent zdo 11:5 owner 11", // CET
	)
	j, l := ofType(evs, EvPlayerJoin), ofType(evs, EvPlayerLeave)
	if len(j) != 1 || !j[0].At.Equal(utc("2026-10-25T00:30:00Z")) {
		t.Fatalf("joins = %+v", j)
	}
	if len(l) != 1 || l[0].Seconds != 2400 || !l[0].At.Equal(utc("2026-10-25T01:10:00Z")) {
		t.Fatalf("leaves = %+v, want one of 2400 s ending 01:10Z", l)
	}
}

func TestLeaveSecondsClampedAtZero(t *testing.T) {
	s := NewSessionizer()
	at := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	s.Feed(Raw{Kind: RawIdentity, At: at, Platform: "Steam", PlatformID: "1"})
	s.Feed(Raw{Kind: RawSpawn, At: at, Name: "A", UID: 11})
	l := ofType(s.Feed(Raw{Kind: RawDestroy, At: at.Add(-time.Minute), UID: 11}), EvPlayerLeave)
	if len(l) != 1 || l[0].Seconds != 0 {
		t.Fatalf("leave = %+v, want seconds clamped to 0", l)
	}
}
