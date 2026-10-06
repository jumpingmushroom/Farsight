package logwatch

import (
	"testing"
	"time"
)

func oslo(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func one(t *testing.T, got []Raw) Raw {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("got %d raws, want 1: %+v", len(got), got)
	}
	return got[0]
}

func TestParseServerLines(t *testing.T) {
	loc := oslo(t)
	at := time.Date(2026, 9, 15, 7, 39, 37, 0, time.UTC) // 09:39:37 CEST

	r := one(t, ParseServerLine("09/15/2026 09:39:37: Got character ZDOID from Thorgerdr : 100004242:1", loc))
	if r.Kind != RawSpawn || r.Name != "Thorgerdr" || r.UID != 100004242 || !r.At.Equal(at) {
		t.Fatalf("spawn = %+v", r)
	}
	r = one(t, ParseServerLine("09/15/2026 09:39:37: Got character ZDOID from John Doe : 100004242:1", loc))
	if r.Kind != RawSpawn || r.Name != "John Doe" || r.UID != 100004242 {
		t.Fatalf("spawn with spaces in name = %+v", r)
	}
	if got := ParseServerLine("09/15/2026 20:52:53: Got character ZDOID from Thorvaldsson : 0:0", loc); len(got) != 0 {
		t.Fatalf("death line (uid 0) must be ignored: %+v", got)
	}
	r = one(t, ParseServerLine("09/15/2026 09:39:17: PlayFab socket with remote ID playfab/F0000000000000A2 received local Platform ID Steam_76561190000000007", loc))
	if r.Kind != RawIdentity || r.Platform != "Steam" || r.PlatformID != "76561190000000007" {
		t.Fatalf("identity = %+v", r)
	}
	r = one(t, ParseServerLine("09/15/2026 20:54:10: PlayFab socket with remote ID playfab/F0000000000000A1 received local Platform ID PlayStation_1000000000000000001", loc))
	if r.Platform != "PlayStation" || r.PlatformID != "1000000000000000001" {
		t.Fatalf("playstation identity = %+v", r)
	}
	r = one(t, ParseServerLine("09/15/2026 09:39:17: PlayFab socket with remote ID playfab/F0000000000000A2 received local Platform ID Steam_76561190000000007\r", loc))
	if r.Kind != RawIdentity || r.Platform != "Steam" || r.PlatformID != "76561190000000007" {
		t.Fatalf("identity with CRLF = %+v", r)
	}
	r = one(t, ParseServerLine("09/15/2026 10:54:16: Destroying abandoned non persistent zdo 100004242:12183 owner 100004242", loc))
	if r.Kind != RawDestroy || r.UID != 100004242 {
		t.Fatalf("destroy = %+v", r)
	}
	r = one(t, ParseServerLine("09/15/2026 23:28:56: Destroying abandoned non persistent zdo -100004256:44 owner -100004256", loc))
	if r.UID != -100004256 {
		t.Fatalf("negative uid = %+v", r)
	}
	got := ParseServerLine(`09/15/2026 10:54:16: Player connection lost server "Mulevikings" that has join code 114544, now 0 player(s)`, loc)
	if len(got) != 2 || got[0].Kind != RawJoinCode || got[0].Code != "114544" || got[1].Kind != RawPlayers || got[1].Players != 0 {
		t.Fatalf("connection lost = %+v", got)
	}
	got = ParseServerLine(`09/28/2026 08:40:10: Session "Mulevikings" with join code 110100 and IP 203.0.113.10:2456 is active with 3 player(s)`, loc)
	if len(got) != 2 || got[0].Code != "110100" || got[1].Players != 3 {
		t.Fatalf("session active = %+v", got)
	}
	if got := ParseServerLine(`09/28/2026 08:40:01: New session server "Mulevikings" that has join code , now 0 player(s)`, loc); len(got) != 0 {
		t.Fatalf("empty-code new-session line must be ignored: %+v", got)
	}
	r = one(t, ParseServerLine("09/28/2026 08:39:48: Valheim version: l-1.0.16 (network version 40)", loc))
	if r.Kind != RawBoot || r.Version != "l-1.0.16" || r.NetVersion != 40 {
		t.Fatalf("boot = %+v", r)
	}
	if r := one(t, ParseServerLine("09/28/2026 08:40:00: Game server connected", loc)); r.Kind != RawReady {
		t.Fatalf("ready = %+v", r)
	}
	if r := one(t, ParseServerLine("09/28/2026 08:40:00: Game server connected\r", loc)); r.Kind != RawReady {
		t.Fatalf("ready with CRLF = %+v", r)
	}
	if got := ParseServerLine("09/28/2026 08:40:00: Game server connected failed", loc); len(got) != 0 {
		t.Fatalf("'connected failed' is not ready: %+v", got)
	}
	for _, l := range []string{
		"09/28/2026 09:09:48: World save (5/5) done. Total time [53ms]",
		"09/18/2026 11:05:52: World saved ( 5183.458ms )",
	} {
		if r := one(t, ParseServerLine(l, loc)); r.Kind != RawSaved {
			t.Fatalf("%q = %+v", l, r)
		}
	}
	if r := one(t, ParseServerLine("09/09/2026 15:43:52:  Connections 1 ZDOS:34937  sent:0 recv:85", loc)); r.Kind != RawPlayers || r.Players != 1 {
		t.Fatalf("connections = %+v", r)
	}
	if r := one(t, ParseServerLine("09/09/2026 15:34:01: Got connection SteamID 76561190000000007", loc)); r.Kind != RawSteamConnect || r.SteamID != "76561190000000007" {
		t.Fatalf("steam connect = %+v", r)
	}
	if r := one(t, ParseServerLine("09/09/2026 15:51:31: Closing socket 76561190000000007", loc)); r.Kind != RawSteamClose || r.SteamID != "76561190000000007" {
		t.Fatalf("steam close = %+v", r)
	}
	for _, l := range []string{"ZPlayFabSocket::Dispose. State: CLOSED", "", "garbage", "09/15/2026 20:37:04: RPC_Disconnect"} {
		if got := ParseServerLine(l, loc); len(got) != 0 {
			t.Fatalf("%q should parse to nothing: %+v", l, got)
		}
	}
}

func TestParseSupervisorLines(t *testing.T) {
	loc := oslo(t)
	r := one(t, ParseSupervisorLine("2026-09-29 05:10:13,203 INFO stopped: valheim-server (exit status 0)", loc))
	if r.Kind != RawStopped || !r.At.Equal(time.Date(2026, 9, 29, 3, 10, 13, 0, time.UTC)) {
		t.Fatalf("stopped = %+v", r)
	}
	if r := one(t, ParseSupervisorLine("2026-09-29 05:10:13,205 INFO spawned: 'valheim-server' with pid 317320", loc)); r.Kind != RawStarting {
		t.Fatalf("spawned = %+v", r)
	}
	if r := one(t, ParseSupervisorLine("2026-09-29 06:00:00,000 INFO exited: valheim-server (exit status 1; not expected)", loc)); r.Kind != RawStopped {
		t.Fatalf("exited = %+v", r)
	}
	for _, l := range []string{
		"2026-09-29 05:10:05,191 INFO waiting for valheim-server to stop",
		"2026-09-29 05:10:13,205 INFO spawned: 'crond' with pid 12",
		"2026-09-29 05:10:23,225 INFO success: valheim-server entered RUNNING state, process has stayed up for > than 10 seconds (startsecs)",
	} {
		if got := ParseSupervisorLine(l, loc); len(got) != 0 {
			t.Fatalf("%q should parse to nothing: %+v", l, got)
		}
	}
}

func TestParseRandomEventSet(t *testing.T) {
	loc := oslo(t)
	r := one(t, ParseServerLine("10/03/2026 21:14:05: Random event set:army_theelder", loc))
	if r.Kind != RawRaid || r.Raid != "army_theelder" || !r.At.Equal(time.Date(2026, 10, 3, 19, 14, 5, 0, time.UTC)) {
		t.Fatalf("raid = %+v", r)
	}
	r = one(t, ParseServerLine("10/03/2026 21:14:05: Random event set: foresttrolls\r", loc))
	if r.Raid != "foresttrolls" {
		t.Fatalf("raid with a space and CRLF = %+v", r)
	}
	if got := ParseServerLine("10/03/2026 21:14:05: Random event set:", loc); len(got) != 0 {
		t.Fatalf("a nameless event line must be ignored: %+v", got)
	}
}

func TestParseTimeSkip(t *testing.T) {
	loc := oslo(t)
	r := one(t, ParseServerLine("09/29/2026 00:52:14: Time 487653.364537966, day:270    nextm:488070.000010729  skipspeed:34.7196227302775", loc))
	if r.Kind != RawTimeSkip || r.To != 488070.000010729 || !r.At.Equal(time.Date(2026, 9, 28, 22, 52, 14, 0, time.UTC)) {
		t.Fatalf("time skip = %+v", r)
	}
}
