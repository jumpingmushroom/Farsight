package server

import (
	"time"

	"github.com/jumpingmushroom/farsight/internal/store"
)

// span is a session's [since, end): an open session runs until now. A
// session that (through a clock step) ends before it starts is empty.
func span(s store.Session, now time.Time) (time.Time, time.Time) {
	end := now
	if s.Until != nil {
		end = *s.Until
	}
	if end.Before(s.Since) {
		end = s.Since
	}
	return s.Since, end
}

// overlapSeconds is how many whole seconds [a0, a1) and [b0, b1) share.
func overlapSeconds(a0, a1, b0, b1 time.Time) int64 {
	lo, hi := a0, a1
	if b0.After(lo) {
		lo = b0
	}
	if b1.Before(hi) {
		hi = b1
	}
	if !hi.After(lo) {
		return 0
	}
	return int64(hi.Sub(lo) / time.Second)
}

// localDays returns the starts of the n local days (in loc) that end with
// the day containing now, oldest first, and the end of that day. Days are
// calendar days: one with a DST change is 23 or 25 hours long.
func localDays(now time.Time, loc *time.Location, n int) (starts []time.Time, end time.Time) {
	t := now.In(loc)
	today := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	for i := n - 1; i >= 0; i-- {
		starts = append(starts, today.AddDate(0, 0, -i))
	}
	return starts, today.AddDate(0, 0, 1)
}

// dayTotals is the time sessions spent online in each of the n local days
// ending today (oldest first): a session across midnight is split between
// the two days, and an open one counts up to now.
func dayTotals(sessions []store.Session, now time.Time, loc *time.Location, n int) []int64 {
	starts, end := localDays(now, loc, n)
	out := make([]int64, n)
	for _, s := range sessions {
		a, b := span(s, now)
		for i, d0 := range starts {
			d1 := end
			if i+1 < n {
				d1 = starts[i+1]
			}
			out[i] += overlapSeconds(a, b, d0, d1)
		}
	}
	return out
}

// totalSeconds is the time sessions spent online, an open one up to now.
func totalSeconds(sessions []store.Session, now time.Time) int64 {
	var n int64
	for _, s := range sessions {
		a, b := span(s, now)
		n += int64(b.Sub(a) / time.Second)
	}
	return n
}
