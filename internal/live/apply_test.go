package live

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/logwatch"
	"github.com/jumpingmushroom/farsight/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open("")
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

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// --- 1: join then leave closes the session with the agent's seconds ---

func TestApplyJoinThenLeaveClosesSessionWithAgentSeconds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := parseTime("2026-01-01T00:10:00Z")
	a := &Applier{Store: s, Now: fixedNow(now)}

	since := parseTime("2026-01-01T00:00:00Z")
	leaveAt := parseTime("2026-01-01T00:05:00Z")

	joinEv := logwatch.Event{ID: "e1", Type: logwatch.EvPlayerJoin, At: since, Name: "Alice", Platform: "Steam", PlatformID: "p1"}
	// The agent's Seconds (12345) deliberately does not equal leaveAt-since
	// (300s), so a pass proves the store's Seconds column holds the
	// agent's own value rather than something recomputed from timestamps.
	leaveEv := logwatch.Event{ID: "e2", Type: logwatch.EvPlayerLeave, At: leaveAt, Name: "Alice", Platform: "Steam", PlatformID: "p1", Since: &since, Seconds: 12345, Reason: "left"}

	applied, skipped, err := a.Apply(ctx, "srv", []logwatch.Event{joinEv, leaveEv})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 2 || skipped != 0 {
		t.Fatalf("applied=%d skipped=%d, want 2,0", applied, skipped)
	}

	online, err := s.Online(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 0 {
		t.Fatalf("online = %+v, want empty", online)
	}

	recent, err := s.Recent(ctx, "srv", since, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 {
		t.Fatalf("recent = %+v, want 1", recent)
	}
	if recent[0].Seconds != 12345 {
		t.Fatalf("seconds = %d, want agent's 12345", recent[0].Seconds)
	}
	if recent[0].Until == nil || !recent[0].Until.Equal(leaveAt) {
		t.Fatalf("until = %v, want %v", recent[0].Until, leaveAt)
	}
	if recent[0].Reason != "left" {
		t.Fatalf("reason = %q, want left", recent[0].Reason)
	}
}

// --- 2: applying the same batch twice ---

func TestApplySameBatchTwiceIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := parseTime("2026-01-01T00:10:00Z")
	a := &Applier{Store: s, Now: fixedNow(now)}

	p2 := 2
	evs := []logwatch.Event{
		{ID: "e1", Type: logwatch.EvHeartbeat, At: parseTime("2026-01-01T00:00:00Z")},
		{ID: "e2", Type: logwatch.EvPlayersNow, At: parseTime("2026-01-01T00:00:01Z"), Players: &p2},
	}

	applied, skipped, err := a.Apply(ctx, "srv", evs)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 2 || skipped != 0 {
		t.Fatalf("first pass applied=%d skipped=%d, want 2,0", applied, skipped)
	}

	applied2, skipped2, err := a.Apply(ctx, "srv", evs)
	if err != nil {
		t.Fatal(err)
	}
	if applied2 != 0 || skipped2 != 2 {
		t.Fatalf("second pass applied=%d skipped=%d, want 0,2", applied2, skipped2)
	}
}

// --- 3: leave without join inserts a closed session ---

func TestApplyLeaveWithoutJoinInsertsClosedSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := parseTime("2026-01-01T00:10:00Z")
	a := &Applier{Store: s, Now: fixedNow(now)}

	since := parseTime("2026-01-01T00:00:00Z")
	leaveAt := parseTime("2026-01-01T00:05:00Z")
	leaveEv := logwatch.Event{ID: "e1", Type: logwatch.EvPlayerLeave, At: leaveAt, Name: "Ghost", Platform: "Steam", PlatformID: "p1", Since: &since, Seconds: 300, Reason: "left"}

	applied, skipped, err := a.Apply(ctx, "srv", []logwatch.Event{leaveEv})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || skipped != 0 {
		t.Fatalf("applied=%d skipped=%d, want 1,0", applied, skipped)
	}

	recent, err := s.Recent(ctx, "srv", since, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].Name != "Ghost" {
		t.Fatalf("recent = %+v", recent)
	}
	if !recent[0].Since.Equal(since) {
		t.Fatalf("since = %v, want %v", recent[0].Since, since)
	}
	if recent[0].Until == nil || !recent[0].Until.Equal(leaveAt) {
		t.Fatalf("until = %v, want %v", recent[0].Until, leaveAt)
	}

	// A leave with no Since at all falls back to Since=Until=at.
	leaveAt2 := parseTime("2026-01-01T00:06:00Z")
	leaveEv2 := logwatch.Event{ID: "e2", Type: logwatch.EvPlayerLeave, At: leaveAt2, Name: "Ghost2", Platform: "Steam", PlatformID: "p2", Reason: "left"}
	if _, _, err := a.Apply(ctx, "srv", []logwatch.Event{leaveEv2}); err != nil {
		t.Fatal(err)
	}
	recent2, err := s.Recent(ctx, "srv", leaveAt2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent2) != 1 || recent2[0].Name != "Ghost2" {
		t.Fatalf("recent2 = %+v", recent2)
	}
	if !recent2[0].Since.Equal(leaveAt2) || recent2[0].Until == nil || !recent2[0].Until.Equal(leaveAt2) {
		t.Fatalf("since/until = %v/%v, want both %v", recent2[0].Since, recent2[0].Until, leaveAt2)
	}
}

// --- 4: server_boot after a heartbeat closes open sessions with
// until=heartbeat time, even when the boot comes shortly after ---

func TestApplyServerBootClosesSessionsAtLastHeartbeatNotBootTime(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := parseTime("2026-01-01T01:00:00Z")
	a := &Applier{Store: s, Now: fixedNow(now)}

	joinAt := parseTime("2026-01-01T00:00:00Z")
	heartbeatAt := parseTime("2026-01-01T00:05:00Z")
	bootAt := heartbeatAt.Add(30 * time.Second)

	evs := []logwatch.Event{
		{ID: "e1", Type: logwatch.EvPlayerJoin, At: joinAt, Name: "Alice", Platform: "Steam", PlatformID: "p1"},
		{ID: "e2", Type: logwatch.EvHeartbeat, At: heartbeatAt},
		{ID: "e3", Type: logwatch.EvServerBoot, At: bootAt, Version: "0.220.5", NetworkVersion: 210},
	}

	applied, skipped, err := a.Apply(ctx, "srv", evs)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 3 || skipped != 0 {
		t.Fatalf("applied=%d skipped=%d, want 3,0", applied, skipped)
	}

	recent, err := s.Recent(ctx, "srv", joinAt, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 {
		t.Fatalf("recent = %+v, want 1", recent)
	}
	if recent[0].Reason != "server_lost" {
		t.Fatalf("reason = %q, want server_lost", recent[0].Reason)
	}
	if recent[0].Until == nil || !recent[0].Until.Equal(heartbeatAt) {
		t.Fatalf("until = %v, want %v (last heartbeat, not boot time)", recent[0].Until, heartbeatAt)
	}

	live, ok, err := s.GetLive(ctx, nil, "srv")
	if err != nil || !ok {
		t.Fatalf("GetLive: ok=%v err=%v", ok, err)
	}
	if live.Status != "starting" {
		t.Fatalf("status = %q, want starting", live.Status)
	}
	if live.Version != "0.220.5" || live.NetworkVersion != 210 {
		t.Fatalf("version/networkVersion = %q/%d, want 0.220.5/210", live.Version, live.NetworkVersion)
	}
	if !live.UpSince.Equal(bootAt) {
		t.Fatalf("upSince = %v, want %v", live.UpSince, bootAt)
	}
}

// --- 5: server_stopped closes with until=stop.at ---

func TestApplyServerStoppedClosesWithStopTime(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := parseTime("2026-01-01T01:00:00Z")
	a := &Applier{Store: s, Now: fixedNow(now)}

	joinAt := parseTime("2026-01-01T00:00:00Z")
	stopAt := parseTime("2026-01-01T00:30:00Z")

	evs := []logwatch.Event{
		{ID: "e1", Type: logwatch.EvPlayerJoin, At: joinAt, Name: "Alice", Platform: "Steam", PlatformID: "p1"},
		{ID: "e2", Type: logwatch.EvServerStopped, At: stopAt},
	}

	applied, skipped, err := a.Apply(ctx, "srv", evs)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 2 || skipped != 0 {
		t.Fatalf("applied=%d skipped=%d, want 2,0", applied, skipped)
	}

	recent, err := s.Recent(ctx, "srv", joinAt, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].Reason != "server_stopped" {
		t.Fatalf("recent = %+v, want reason server_stopped", recent)
	}
	if recent[0].Until == nil || !recent[0].Until.Equal(stopAt) {
		t.Fatalf("until = %v, want %v", recent[0].Until, stopAt)
	}

	live, ok, err := s.GetLive(ctx, nil, "srv")
	if err != nil || !ok {
		t.Fatalf("GetLive: ok=%v err=%v", ok, err)
	}
	if live.Status != "restarting" {
		t.Fatalf("status = %q, want restarting", live.Status)
	}
}

// --- 6: invalid events are skipped while valid ones in the same batch
// still apply ---

func TestApplySkipsInvalidEventsButAppliesValidOnes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := parseTime("2026-01-01T00:10:00Z")
	a := &Applier{Store: s, Now: fixedNow(now)}

	validHeartbeat := logwatch.Event{ID: "e-valid", Type: logwatch.EvHeartbeat, At: now.Add(-1 * time.Minute)}
	emptyID := logwatch.Event{ID: "", Type: logwatch.EvHeartbeat, At: now}
	unknownType := logwatch.Event{ID: "e-unknown", Type: "not_a_real_type", At: now}
	zeroAt := logwatch.Event{ID: "e-zero", Type: logwatch.EvHeartbeat, At: time.Time{}}
	farFuture := logwatch.Event{ID: "e-future", Type: logwatch.EvHeartbeat, At: now.Add(11 * time.Minute)}

	evs := []logwatch.Event{emptyID, unknownType, zeroAt, farFuture, validHeartbeat}
	applied, skipped, err := a.Apply(ctx, "srv", evs)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || skipped != 4 {
		t.Fatalf("applied=%d skipped=%d, want 1,4", applied, skipped)
	}

	live, ok, err := s.GetLive(ctx, nil, "srv")
	if err != nil || !ok {
		t.Fatalf("GetLive: ok=%v err=%v", ok, err)
	}
	if !live.LastHeartbeat.Equal(validHeartbeat.At) {
		t.Fatalf("LastHeartbeat = %v, want %v", live.LastHeartbeat, validHeartbeat.At)
	}
}

// --- 7: SweepStale closes sessions only once the heartbeat is older
// than 3 min, using the last heartbeat as the end ---

func TestSweepStaleClosesOnlyAfterHeartbeatTimeout(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	heartbeatAt := parseTime("2026-01-01T00:00:00Z")

	a := &Applier{Store: s, Now: fixedNow(heartbeatAt)}
	joinAt := heartbeatAt.Add(-1 * time.Minute)
	evs := []logwatch.Event{
		{ID: "e1", Type: logwatch.EvPlayerJoin, At: joinAt, Name: "Alice", Platform: "Steam", PlatformID: "p1"},
		{ID: "e2", Type: logwatch.EvHeartbeat, At: heartbeatAt},
	}
	if _, _, err := a.Apply(ctx, "srv", evs); err != nil {
		t.Fatal(err)
	}

	// Not yet stale.
	a.Now = fixedNow(heartbeatAt.Add(HeartbeatTimeout - time.Second))
	closed, err := a.SweepStale(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if closed != 0 {
		t.Fatalf("closed = %d, want 0 (not yet stale)", closed)
	}
	online, err := s.Online(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 1 {
		t.Fatalf("online = %+v, want still open", online)
	}

	// Stale.
	a.Now = fixedNow(heartbeatAt.Add(HeartbeatTimeout + time.Second))
	closed, err = a.SweepStale(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}

	recent, err := s.Recent(ctx, "srv", joinAt, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 {
		t.Fatalf("recent = %+v, want 1", recent)
	}
	if recent[0].Reason != "heartbeat_lost" {
		t.Fatalf("reason = %q, want heartbeat_lost", recent[0].Reason)
	}
	if recent[0].Until == nil || !recent[0].Until.Equal(heartbeatAt) {
		t.Fatalf("until = %v, want last heartbeat %v", recent[0].Until, heartbeatAt)
	}
}

// --- 8: the Status table covers unknown, offline, restarting, starting
// and online ---

func TestStatus(t *testing.T) {
	now := parseTime("2026-01-01T00:10:00Z")
	recentHB := now.Add(-10 * time.Second)
	oldHB := now.Add(-4 * time.Minute)

	cases := []struct {
		name string
		live store.Live
		want string
	}{
		{"no-row", store.Live{}, "unknown"},
		{"offline", store.Live{LastHeartbeat: oldHB}, "offline"},
		{"restarting", store.Live{LastHeartbeat: recentHB, Status: "restarting"}, "restarting"},
		{"starting", store.Live{LastHeartbeat: recentHB, Status: "starting"}, "starting"},
		{"online-fallback", store.Live{LastHeartbeat: recentHB, Status: ""}, "online"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Status(c.live, now); got != c.want {
				t.Fatalf("Status = %q, want %q", got, c.want)
			}
		})
	}
}

// --- 9: players_now with 0 sets Players to 0 ---

func TestApplyPlayersNowZeroSetsPlayersToZero(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := parseTime("2026-01-01T00:10:00Z")
	a := &Applier{Store: s, Now: fixedNow(now)}

	p3, p0 := 3, 0

	if _, _, err := a.Apply(ctx, "srv", []logwatch.Event{
		{ID: "e1", Type: logwatch.EvPlayersNow, At: now.Add(-2 * time.Minute), Players: &p3},
	}); err != nil {
		t.Fatal(err)
	}
	live, ok, err := s.GetLive(ctx, nil, "srv")
	if err != nil || !ok {
		t.Fatalf("GetLive: ok=%v err=%v", ok, err)
	}
	if live.Players != 3 {
		t.Fatalf("players = %d, want 3", live.Players)
	}

	if _, _, err := a.Apply(ctx, "srv", []logwatch.Event{
		{ID: "e2", Type: logwatch.EvPlayersNow, At: now.Add(-1 * time.Minute), Players: &p0},
	}); err != nil {
		t.Fatal(err)
	}
	live2, ok, err := s.GetLive(ctx, nil, "srv")
	if err != nil || !ok {
		t.Fatalf("GetLive: ok=%v err=%v", ok, err)
	}
	if live2.Players != 0 {
		t.Fatalf("players = %d, want 0", live2.Players)
	}
}

// --- replaying events older than the retention window is a no-op ---

// A restarted agent replays its whole log since pod start. Once serve has
// pruned the oldest of those events, their ids no longer dedupe, so Apply
// must refuse them by age instead of re-applying weeks-old state.
func TestApplyReplayOfPrunedEventsLeavesLiveStateUnchanged(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := parseTime("2026-03-01T12:00:00Z")
	clock := now.Add(-20 * 24 * time.Hour)
	a := &Applier{Store: s, Now: func() time.Time { return clock }}

	old := now.Add(-20 * 24 * time.Hour)
	p3 := 3
	oldEvs := []logwatch.Event{
		{ID: "o1", Type: logwatch.EvServerBoot, At: old.Add(-time.Hour), Version: "0.100.0", NetworkVersion: 20},
		{ID: "o2", Type: logwatch.EvServerStarting, At: old.Add(-time.Hour)},
		{ID: "o3", Type: logwatch.EvServerReady, At: old.Add(-59 * time.Minute)},
		{ID: "o4", Type: logwatch.EvJoinCode, At: old.Add(-58 * time.Minute), Code: "OLD111"},
		{ID: "o5", Type: logwatch.EvPlayersNow, At: old.Add(-50 * time.Minute), Players: &p3},
	}
	// The events arrived live 20 days ago.
	if _, _, err := a.Apply(ctx, "srv", oldEvs); err != nil {
		t.Fatal(err)
	}

	clock = now
	p1 := 1
	curEvs := []logwatch.Event{
		{ID: "c1", Type: logwatch.EvServerBoot, At: now.Add(-time.Hour), Version: "0.219.14", NetworkVersion: 34},
		{ID: "c2", Type: logwatch.EvServerReady, At: now.Add(-59 * time.Minute)},
		{ID: "c3", Type: logwatch.EvJoinCode, At: now.Add(-58 * time.Minute), Code: "NEW222"},
		{ID: "c4", Type: logwatch.EvPlayerJoin, At: now.Add(-30 * time.Minute), Name: "Alice", Platform: "Steam", PlatformID: "111"},
		{ID: "c5", Type: logwatch.EvPlayersNow, At: now.Add(-30 * time.Minute), Players: &p1},
		{ID: "c6", Type: logwatch.EvHeartbeat, At: now.Add(-time.Minute)},
	}
	if _, _, err := a.Apply(ctx, "srv", curEvs); err != nil {
		t.Fatal(err)
	}

	// serve's hourly prune drops the 20-day-old events.
	if _, err := s.PruneEvents(ctx, now.Add(-EventRetention)); err != nil {
		t.Fatal(err)
	}

	before, _, err := s.GetLive(ctx, nil, "srv")
	if err != nil {
		t.Fatal(err)
	}
	onlineBefore, err := s.Online(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}

	// The restarted agent replays everything.
	applied, skipped, err := a.Apply(ctx, "srv", append(append([]logwatch.Event{}, oldEvs...), curEvs...))
	if err != nil {
		t.Fatal(err)
	}
	if applied != 0 || skipped != len(oldEvs)+len(curEvs) {
		t.Fatalf("replay applied=%d skipped=%d, want 0,%d", applied, skipped, len(oldEvs)+len(curEvs))
	}

	after, _, err := s.GetLive(ctx, nil, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("live state changed by replay:\nbefore %+v\nafter  %+v", before, after)
	}
	if after.JoinCode != "NEW222" || after.Version != "0.219.14" {
		t.Fatalf("live = %+v, want current join code and version", after)
	}
	onlineAfter, err := s.Online(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(onlineBefore) != 1 || len(onlineAfter) != 1 || onlineAfter[0] != onlineBefore[0] {
		t.Fatalf("online sessions changed by replay:\nbefore %+v\nafter  %+v", onlineBefore, onlineAfter)
	}
}

// A restart ends the old boot's crossplay session: its join code no
// longer works and nobody is on, until the new boot logs its own.
func TestApplyRestartClearsJoinCodeAndPlayers(t *testing.T) {
	for _, typ := range []string{logwatch.EvServerStopped, logwatch.EvServerStarting, logwatch.EvServerBoot} {
		t.Run(typ, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			at := parseTime("2026-01-01T10:00:00Z")
			a := &Applier{Store: s, Now: fixedNow(at.Add(time.Hour))}
			p2 := 2
			if _, _, err := a.Apply(ctx, "srv", []logwatch.Event{
				{ID: "c1", Type: logwatch.EvJoinCode, At: at, Code: "111111"},
				{ID: "p1", Type: logwatch.EvPlayersNow, At: at, Players: &p2},
				{ID: "r1", Type: typ, At: at.Add(10 * time.Minute)},
			}); err != nil {
				t.Fatal(err)
			}
			l, _, err := s.GetLive(ctx, nil, "srv")
			if err != nil {
				t.Fatal(err)
			}
			if l.Players != 0 || l.JoinCode != "" || !l.JoinCodeAt.IsZero() {
				t.Fatalf("live = %+v, want no players and no join code", l)
			}
		})
	}
}

// The supervisor's log (stopped, starting) and the server's are read by
// separate followers, so a restart may be applied after the new boot's
// join code: a code logged after the restart stays.
func TestApplyRestartKeepsANewerJoinCode(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	at := parseTime("2026-01-01T10:00:00Z")
	a := &Applier{Store: s, Now: fixedNow(at.Add(time.Hour))}
	if _, _, err := a.Apply(ctx, "srv", []logwatch.Event{
		{ID: "c1", Type: logwatch.EvJoinCode, At: at.Add(time.Minute), Code: "222222"},
		{ID: "s1", Type: logwatch.EvServerStarting, At: at},
	}); err != nil {
		t.Fatal(err)
	}
	l, _, err := s.GetLive(ctx, nil, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if l.JoinCode != "222222" {
		t.Fatalf("join code = %q, want the newer 222222 kept", l.JoinCode)
	}
}

func TestApplyStoresRaidEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := parseTime("2026-10-03T20:00:00Z")
	a := &Applier{Store: s, Now: fixedNow(now)}
	ev := logwatch.Event{ID: "r1", Type: logwatch.EvRaid, At: parseTime("2026-10-03T19:14:05Z"), Raid: "army_theelder"}
	applied, skipped, err := a.Apply(ctx, "srv", []logwatch.Event{ev})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || skipped != 0 {
		t.Fatalf("applied=%d skipped=%d, want 1,0", applied, skipped)
	}
	got, err := s.RecentActivity(ctx, "srv", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != logwatch.EvRaid || got[0].Raid != "army_theelder" {
		t.Fatalf("stored = %+v", got)
	}
}

func TestApplyStoresTimeSkipEventsWithoutChangingLiveState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := parseTime("2026-09-29T00:00:00Z")
	a := &Applier{Store: s, Now: fixedNow(now)}
	ev := logwatch.Event{ID: "ts1", Type: logwatch.EvTimeSkip, At: parseTime("2026-09-28T22:52:14Z"), To: 488070.000010729}
	applied, skipped, err := a.Apply(ctx, "srv", []logwatch.Event{ev})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || skipped != 0 {
		t.Fatalf("applied=%d skipped=%d, want 1,0", applied, skipped)
	}
	got, err := s.RecentActivity(ctx, "srv", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != logwatch.EvTimeSkip || got[0].To != 488070.000010729 {
		t.Fatalf("stored = %+v", got)
	}
	live, ok, err := s.GetLive(ctx, nil, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || live.Status != "" || !live.UpSince.IsZero() {
		t.Fatalf("live state changed by a time_skip event: %+v", live)
	}
}

func TestApplyStoresDeathEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := parseTime("2026-10-06T20:00:00Z")
	a := &Applier{Store: s, Now: fixedNow(now)}
	ev := logwatch.Event{ID: "d1", Type: logwatch.EvPlayerDeath, At: parseTime("2026-10-06T19:14:05Z"), Name: "Orm", Platform: "Steam", PlatformID: "765"}
	applied, skipped, err := a.Apply(ctx, "srv", []logwatch.Event{ev})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || skipped != 0 {
		t.Fatalf("applied=%d skipped=%d, want 1,0", applied, skipped)
	}
	got, err := s.RecentActivity(ctx, "srv", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != logwatch.EvPlayerDeath || got[0].PlatformID != "765" {
		t.Fatalf("stored = %+v", got)
	}
}

// --- a session swept as heartbeat_lost is repaired by what the agent
// sends once it is back ---

// sweptSession applies a join and a heartbeat for Alice, then sweeps the
// server stale, returning the applier and the join time.
func sweptSession(t *testing.T, s *store.Store) (*Applier, time.Time) {
	t.Helper()
	ctx := context.Background()
	heartbeatAt := parseTime("2026-01-01T00:00:00Z")
	joinAt := heartbeatAt.Add(-time.Minute)
	a := &Applier{Store: s, Now: fixedNow(heartbeatAt)}
	evs := []logwatch.Event{
		{ID: "j1", Type: logwatch.EvPlayerJoin, At: joinAt, Name: "Alice", Platform: "Steam", PlatformID: "p1"},
		{ID: "h1", Type: logwatch.EvHeartbeat, At: heartbeatAt},
	}
	if _, _, err := a.Apply(ctx, "srv", evs); err != nil {
		t.Fatal(err)
	}
	a.Now = fixedNow(heartbeatAt.Add(HeartbeatTimeout + time.Second))
	if n, err := a.SweepStale(ctx); err != nil || n != 1 {
		t.Fatalf("sweep closed %d, %v; want 1", n, err)
	}
	return a, joinAt
}

func TestApplyLeaveAfterSweepRecordsTheRealEnd(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, joinAt := sweptSession(t, s)

	leaveAt := joinAt.Add(20 * time.Minute)
	a.Now = fixedNow(leaveAt.Add(time.Minute))
	leave := logwatch.Event{ID: "l1", Type: logwatch.EvPlayerLeave, At: leaveAt, Name: "Alice", Platform: "Steam", PlatformID: "p1", Since: &joinAt, Seconds: 1200, Reason: "left"}
	if _, _, err := a.Apply(ctx, "srv", []logwatch.Event{leave}); err != nil {
		t.Fatal(err)
	}

	got, err := s.PlayerSessions(ctx, "srv", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("sessions = %+v, want 1", got)
	}
	if got[0].Until == nil || !got[0].Until.Equal(leaveAt) || got[0].Seconds != 1200 || got[0].Reason != "left" {
		t.Fatalf("session = %+v, want closed at %v with 1200s, reason left", got[0], leaveAt)
	}
}

func TestApplyReplayedJoinReopensASweptSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, joinAt := sweptSession(t, s)

	// The agent restarted and replays its log: the join is a duplicate.
	join := logwatch.Event{ID: "j1", Type: logwatch.EvPlayerJoin, At: joinAt, Name: "Alice", Platform: "Steam", PlatformID: "p1"}
	if _, _, err := a.Apply(ctx, "srv", []logwatch.Event{join}); err != nil {
		t.Fatal(err)
	}

	online, err := s.Online(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 1 || !online[0].Since.Equal(joinAt) {
		t.Fatalf("online = %+v, want Alice reopened", online)
	}
}

func TestApplyReplayedJoinLeavesASessionClosedByBootClosed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	at := parseTime("2026-01-01T00:00:00Z")
	a := &Applier{Store: s, Now: fixedNow(at.Add(time.Hour))}
	join := logwatch.Event{ID: "j1", Type: logwatch.EvPlayerJoin, At: at, Name: "Alice", Platform: "Steam", PlatformID: "p1"}
	evs := []logwatch.Event{
		join,
		{ID: "b1", Type: logwatch.EvServerBoot, At: at.Add(10 * time.Minute)},
	}
	if _, _, err := a.Apply(ctx, "srv", evs); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Apply(ctx, "srv", []logwatch.Event{join}); err != nil {
		t.Fatal(err)
	}
	online, err := s.Online(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 0 {
		t.Fatalf("online = %+v, want none (the boot closed it)", online)
	}
}

func TestSweepStaleWaitsAHeartbeatTimeoutAfterStart(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	heartbeatAt := parseTime("2026-01-01T00:00:00Z")
	a := &Applier{Store: s, Now: fixedNow(heartbeatAt)}
	evs := []logwatch.Event{
		{ID: "j1", Type: logwatch.EvPlayerJoin, At: heartbeatAt.Add(-time.Minute), Name: "Alice", Platform: "Steam", PlatformID: "p1"},
		{ID: "h1", Type: logwatch.EvHeartbeat, At: heartbeatAt},
	}
	if _, _, err := a.Apply(ctx, "srv", evs); err != nil {
		t.Fatal(err)
	}

	// The app was down for ten minutes and has just started: the agent
	// may not have got a heartbeat through yet.
	started := heartbeatAt.Add(10 * time.Minute)
	a.StartedAt = started
	a.Now = fixedNow(started.Add(HeartbeatTimeout - time.Second))
	if n, err := a.SweepStale(ctx); err != nil || n != 0 {
		t.Fatalf("sweep closed %d, %v; want 0 inside the start grace", n, err)
	}
	a.Now = fixedNow(started.Add(HeartbeatTimeout + time.Second))
	if n, err := a.SweepStale(ctx); err != nil || n != 1 {
		t.Fatalf("sweep closed %d, %v; want 1 after the start grace", n, err)
	}
}

// --- events dropped for their timestamp are warned about, at most once
// per server per invalidWarnInterval; duplicates are not ---

func TestApplyWarnsAboutEventsWithABadTimestamp(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := parseTime("2026-01-01T12:00:00Z")
	var logs bytes.Buffer
	a := &Applier{Store: s, Now: fixedNow(now), Log: slog.New(slog.NewTextHandler(&logs, nil))}

	// Logged in Oslo time but read as UTC: an hour in the future.
	future := logwatch.Event{ID: "f1", Type: logwatch.EvPlayerJoin, At: now.Add(time.Hour), Name: "Alice", PlatformID: "p1"}
	ok := logwatch.Event{ID: "ok1", Type: logwatch.EvHeartbeat, At: now}
	if _, _, err := a.Apply(ctx, "srv", []logwatch.Event{future, ok, ok}); err != nil {
		t.Fatal(err)
	}
	out := logs.String()
	if n := strings.Count(out, "level=WARN"); n != 1 {
		t.Fatalf("warnings = %d, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, "server=srv") || !strings.Contains(out, "FARSIGHT_LOG_TZ") || !strings.Contains(out, "invalid=1") {
		t.Fatalf("warning lacks server, count or TZ hint:\n%s", out)
	}

	// Another bad batch straight after is not warned about again.
	future.ID = "f2"
	if _, _, err := a.Apply(ctx, "srv", []logwatch.Event{future}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(logs.String(), "level=WARN"); n != 1 {
		t.Fatalf("warnings = %d after a second bad batch, want still 1", n)
	}

	// Once the interval has passed it is.
	a.Now = fixedNow(now.Add(invalidWarnInterval + time.Second))
	future.ID = "f3"
	if _, _, err := a.Apply(ctx, "srv", []logwatch.Event{future}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(logs.String(), "level=WARN"); n != 2 {
		t.Fatalf("warnings = %d after the interval, want 2", n)
	}
}

// --- a death flagged Intro (within logwatch.IntroWindow of joining) is a
// skipped Valkyrie intro on a character's first session, and kept for a
// character who has played before ---

// applyJoinAndDeath applies Alice's (p1) join at joinAt and her death a
// minute later, flagged intro or not, and returns how many deaths the
// server has stored.
func applyJoinAndDeath(t *testing.T, s *store.Store, a *Applier, joinAt time.Time, intro bool) int {
	t.Helper()
	ctx := context.Background()
	evs := []logwatch.Event{
		{ID: "j-intro", Type: logwatch.EvPlayerJoin, At: joinAt, Name: "Alice", Platform: "Steam", PlatformID: "p1"},
		{ID: "d-intro", Type: logwatch.EvPlayerDeath, At: joinAt.Add(time.Minute), Name: "Alice", Platform: "Steam", PlatformID: "p1", Intro: intro},
	}
	if _, _, err := a.Apply(ctx, "srv", evs); err != nil {
		t.Fatal(err)
	}
	got, err := s.EventsOfType(ctx, "srv", logwatch.EvPlayerDeath)
	if err != nil {
		t.Fatal(err)
	}
	return len(got)
}

// playedBefore applies a closed hour-long session two days before joinAt.
func playedBefore(t *testing.T, a *Applier, joinAt time.Time, name, platformID string) {
	t.Helper()
	since := joinAt.Add(-48 * time.Hour)
	if _, _, err := a.Apply(context.Background(), "srv", []logwatch.Event{
		{ID: "j0-" + name + platformID, Type: logwatch.EvPlayerJoin, At: since, Name: name, Platform: "Steam", PlatformID: platformID},
		{ID: "l0-" + name + platformID, Type: logwatch.EvPlayerLeave, At: since.Add(time.Hour), Name: name, Platform: "Steam", PlatformID: platformID, Since: &since, Seconds: 3600, Reason: "left"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyDropsAnIntroDeathOnAFirstSession(t *testing.T) {
	s := newTestStore(t)
	joinAt := parseTime("2026-01-03T10:00:00Z")
	a := &Applier{Store: s, Now: fixedNow(joinAt.Add(time.Hour))}
	if n := applyJoinAndDeath(t, s, a, joinAt, true); n != 0 {
		t.Fatalf("deaths = %d, want 0: a new character skipping the intro", n)
	}
}

func TestApplyKeepsAnIntroDeathOfAReturningCharacter(t *testing.T) {
	s := newTestStore(t)
	joinAt := parseTime("2026-01-03T10:00:00Z")
	a := &Applier{Store: s, Now: fixedNow(joinAt.Add(time.Hour))}
	playedBefore(t, a, joinAt, "Alice", "p1")
	if n := applyJoinAndDeath(t, s, a, joinAt, true); n != 1 {
		t.Fatalf("deaths = %d, want 1: Alice has played before", n)
	}
}

func TestApplyIntroDeathIgnoresOtherCharactersSessions(t *testing.T) {
	// The same player's other character, or another account's character
	// of the same name, says nothing about whether this one saw the intro.
	s := newTestStore(t)
	joinAt := parseTime("2026-01-03T10:00:00Z")
	a := &Applier{Store: s, Now: fixedNow(joinAt.Add(time.Hour))}
	playedBefore(t, a, joinAt, "Old Alice", "p1")
	playedBefore(t, a, joinAt, "Alice", "p2")
	if n := applyJoinAndDeath(t, s, a, joinAt, true); n != 0 {
		t.Fatalf("deaths = %d, want 0: no earlier session of this character", n)
	}
}

func TestApplyKeepsAnUnflaggedDeathOnAFirstSession(t *testing.T) {
	s := newTestStore(t)
	joinAt := parseTime("2026-01-03T10:00:00Z")
	a := &Applier{Store: s, Now: fixedNow(joinAt.Add(time.Hour))}
	if n := applyJoinAndDeath(t, s, a, joinAt, false); n != 1 {
		t.Fatalf("deaths = %d, want 1", n)
	}
}
