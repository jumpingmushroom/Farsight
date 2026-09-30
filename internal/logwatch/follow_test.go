package logwatch

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

// TestFollowerCapsPartialLine: a partial line may not grow the carried
// buffer past maxPartialLine. Once it would, the partial line is thrown
// away up to the next newline, one Warn is logged, and following lines
// are read normally.
func TestFollowerCapsPartialLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.log")
	var buf bytes.Buffer
	fw := &follower{
		resolve: func(string) (string, error) { return path, nil },
		role:    "server",
		log:     slog.New(slog.NewTextHandler(&buf, nil)),
	}
	read := func() []string {
		t.Helper()
		lines, err := fw.readNew()
		if err != nil {
			t.Fatal(err)
		}
		return lines
	}

	appendLine(t, path, "first\n"+strings.Repeat("x", maxPartialLine/2))
	if got := read(); len(got) != 1 || got[0] != "first\n" {
		t.Fatalf("lines = %q", got)
	}
	appendLine(t, path, strings.Repeat("x", maxPartialLine/2+1)) // now over the cap
	if got := read(); len(got) != 0 {
		t.Fatalf("lines = %d, want none", len(got))
	}
	if len(fw.buf) != 0 {
		t.Fatalf("buffer holds %d bytes after exceeding the cap, want 0", len(fw.buf))
	}
	appendLine(t, path, strings.Repeat("y", 1000)) // still the same overlong line
	if got := read(); len(got) != 0 || len(fw.buf) != 0 {
		t.Fatalf("lines = %d, buf = %d: the rest of an overlong line must be discarded", len(got), len(fw.buf))
	}
	appendLine(t, path, "tail of the long line\nnext\n")
	if got := read(); len(got) != 1 || got[0] != "next\n" {
		t.Fatalf("lines = %q, want just the line after the overlong one", got)
	}

	// A second overlong line is discarded too, without a second warning.
	appendLine(t, path, strings.Repeat("z", maxPartialLine+1))
	read()
	appendLine(t, path, "end\nafter\n")
	if got := read(); len(got) != 1 || got[0] != "after\n" {
		t.Fatalf("lines = %q", got)
	}
	if n := strings.Count(buf.String(), "level=WARN"); n != 1 {
		t.Fatalf("logged %d warnings, want 1: %s", n, buf.String())
	}
}
