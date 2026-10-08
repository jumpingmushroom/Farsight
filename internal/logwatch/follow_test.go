package logwatch

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readLines collects the lines one readNew hands over.
func readLines(fw *follower) ([]string, error) {
	var lines []string
	err := fw.readNew(func(l string) { lines = append(lines, l) })
	return lines, err
}

// TestFollowerCapsPartialLine: a partial line may not grow the carried
// buffer past maxLine. Once it would, the partial line is thrown
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
		lines, err := readLines(fw)
		if err != nil {
			t.Fatal(err)
		}
		return lines
	}

	appendLine(t, path, "first\n"+strings.Repeat("x", maxLine/2))
	if got := read(); len(got) != 1 || got[0] != "first\n" {
		t.Fatalf("lines = %q", got)
	}
	appendLine(t, path, strings.Repeat("x", maxLine/2+1)) // now over the cap
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
	appendLine(t, path, strings.Repeat("z", maxLine+1))
	read()
	appendLine(t, path, "end\nafter\n")
	if got := read(); len(got) != 1 || got[0] != "after\n" {
		t.Fatalf("lines = %q", got)
	}
	if n := strings.Count(buf.String(), "level=WARN"); n != 1 {
		t.Fatalf("logged %d warnings, want 1: %s", n, buf.String())
	}
}

// TestFollowerCapsCompleteLine: the cap holds for a line that arrives
// whole, newline and all, in one read: it is discarded like an overlong
// partial line, with one Warn, and the lines around it are kept.
func TestFollowerCapsCompleteLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.log")
	var buf bytes.Buffer
	fw := &follower{
		resolve: func(string) (string, error) { return path, nil },
		role:    "server",
		log:     slog.New(slog.NewTextHandler(&buf, nil)),
	}
	defer fw.close()
	appendLine(t, path, "first\n"+strings.Repeat("x", maxLine)+"\nnext\n")
	lines, err := readLines(fw)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "first\n" || lines[1] != "next\n" {
		t.Fatalf("got %d lines, want just first and next", len(lines))
	}
	if n := strings.Count(buf.String(), "level=WARN"); n != 1 {
		t.Fatalf("logged %d warnings, want 1: %s", n, buf.String())
	}

	// A line longer than one read fragment but under the cap is kept whole.
	long := strings.Repeat("l", 3*readChunk) + "\n"
	appendLine(t, path, long)
	if lines, err := readLines(fw); err != nil || len(lines) != 1 || lines[0] != long {
		t.Fatalf("got %d lines (err %v), want the long line intact", len(lines), err)
	}
}

// TestFollowerIdentityIsTheOpenedFile: a rotation between readNew's stat
// of the path and its open must not leave the follower holding one file
// while recording the other's identity, or the next poll sees a "new"
// file and reads the one it already has again from the start.
func TestFollowerIdentityIsTheOpenedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.log")
	appendLine(t, path, "old\n")
	rotate := true
	orig := openFile
	t.Cleanup(func() { openFile = orig })
	openFile = func(name string) (*os.File, error) {
		if rotate {
			rotate = false
			if err := os.Rename(path, path+".1"); err != nil {
				t.Fatal(err)
			}
			appendLine(t, path, "new\n")
		}
		return orig(name)
	}

	fw := &follower{resolve: func(string) (string, error) { return path, nil }}
	defer fw.close()
	var got []string
	for range 2 {
		lines, err := readLines(fw)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, lines...)
	}
	if len(got) != 1 || got[0] != "new\n" {
		t.Fatalf("lines = %q, want the opened file's line once", got)
	}
}
