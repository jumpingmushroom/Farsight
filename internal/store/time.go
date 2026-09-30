package store

import (
	"database/sql"
	"time"
)

// millis converts t to Unix milliseconds UTC for storage.
func millis(t time.Time) int64 {
	return t.UTC().UnixMilli()
}

// fromMillis converts stored Unix milliseconds back to a UTC time.Time.
func fromMillis(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}

// nullMillis converts t to a nullable Unix-milliseconds value for
// storage: the zero time.Time maps to NULL (unknown).
func nullMillis(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return millis(t)
}

// fromNullMillis converts a nullable stored Unix-milliseconds column back
// to a time.Time: NULL maps to the zero time.Time (unknown).
func fromNullMillis(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return fromMillis(v.Int64)
}
