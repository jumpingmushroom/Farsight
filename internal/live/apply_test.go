package live

import (
	"context"
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
	if recent[0].Reason != "server_lost" {
		t.Fatalf("reason = %q, want server_lost", recent[0].Reason)
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
