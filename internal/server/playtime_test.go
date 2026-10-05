package server

import (
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/store"
)

func oslo(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func sess(since, until string) store.Session {
	s := store.Session{Since: mustTime(since)}
	if until != "" {
		u := mustTime(until)
		s.Until = &u
	}
	return s
}

func mustTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		panic(err)
	}
	return t
}

func TestDayTotalsSplitAtLocalMidnightAndCountTheOpenSession(t *testing.T) {
	loc := oslo(t)
	now := mustTime("2026-10-05T12:00:00+02:00") // Monday noon in Oslo
	sessions := []store.Session{
		sess("2026-09-28T10:00:00+02:00", "2026-09-28T11:00:00+02:00"), // before the window
		sess("2026-09-29T23:00:00+02:00", "2026-09-30T01:30:00+02:00"), // 1 h on 29 Sep, 1.5 h on 30 Sep
		sess("2026-10-03T22:00:00Z", "2026-10-04T00:00:00Z"),           // 00:00–02:00 Oslo on 4 Oct
		sess("2026-10-05T10:00:00+02:00", ""),                          // open: 2 h so far today
	}
	got := dayTotals(sessions, now, loc, 7)
	want := []int64{3600, 5400, 0, 0, 0, 7200, 7200} // 29 Sep, 30 Sep, 1–3 Oct, 4 Oct, 5 Oct (today)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("day totals = %v, want %v", got, want)
		}
	}
	if total := totalSeconds(sessions, now); total != 3600+9000+7200+7200 {
		t.Fatalf("total = %d", total)
	}
}

func TestLocalDaysAcrossTheDSTChange(t *testing.T) {
	loc := oslo(t)
	// 25 Oct 2026: clocks go back in Oslo, so that day is 25 hours long.
	starts, end := localDays(mustTime("2026-10-26T09:00:00+01:00"), loc, 2)
	if starts[0].Format(time.RFC3339) != "2026-10-25T00:00:00+02:00" || starts[1].Format(time.RFC3339) != "2026-10-26T00:00:00+01:00" ||
		end.Format(time.RFC3339) != "2026-10-27T00:00:00+01:00" {
		t.Fatalf("starts %v end %v", starts, end)
	}
	whole := sess("2026-10-25T00:00:00+02:00", "2026-10-26T00:00:00+01:00")
	if got := dayTotals([]store.Session{whole}, mustTime("2026-10-26T09:00:00+01:00"), loc, 2); got[0] != 25*3600 || got[1] != 0 {
		t.Fatalf("DST day = %v, want 25 h then 0", got)
	}
}

func TestSpanClampsBackwardsSessions(t *testing.T) {
	s := sess("2026-10-05T10:00:00Z", "2026-10-05T09:00:00Z")
	if total := totalSeconds([]store.Session{s}, mustTime("2026-10-05T12:00:00Z")); total != 0 {
		t.Fatalf("backwards session counted %d s", total)
	}
}
