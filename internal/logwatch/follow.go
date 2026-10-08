package logwatch

import (
	"bufio"
	"io"
	"log/slog"
	"os"
)

// maxPartialLine caps the bytes of a not-yet-terminated line a follower
// carries across polls. A line that grows past it is discarded up to its
// terminating newline, so a runaway writer can't grow memory unbounded.
const maxPartialLine = 1 << 20

// follower tails one logical log role (the server's stdout log or
// supervisord.log) across log rotation and game restarts. resolve is
// called on every readNew with the path currently open (empty if none),
// and returns the path that currently holds this role's content; it may
// return a different path than last time (a new random suffix after
// rotation or a pod restart).
type follower struct {
	resolve func(current string) (string, error)

	f      *os.File
	fi     os.FileInfo // identity of the currently open file, for os.SameFile
	offset int64
	buf    []byte // bytes of a not-yet-terminated line, carried across polls

	// rotated: the first file this follower opens is read after its
	// newest rotated backup (<path>.1), so a replay after an agent
	// restart still sees what was logged before supervisord's last
	// rotation. opened records that the first open has happened.
	rotated bool
	opened  bool

	role       string       // "server" or "supervisor", for log lines
	log        *slog.Logger // may be nil
	discarding bool         // dropping an overlong line until its newline
	warned     bool         // the overlong-line warning has been logged
}

// readNew returns the complete lines appended since the last call. A
// trailing partial line (no '\n' yet) is buffered and prefixed onto the
// next complete line once it arrives. If the resolved file's identity
// differs from the one currently open (rotation, or a fresh path after a
// restart), or the file has shrunk below the tracked offset (truncation),
// readNew drains the old handle to EOF first — so nothing written just
// before a rename is lost — then reopens the new path from offset 0.
func (fw *follower) readNew() ([]string, error) {
	current := ""
	if fw.f != nil {
		current = fw.f.Name()
	}
	path, err := fw.resolve(current)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if fw.f == nil || !os.SameFile(fw.fi, fi) {
		var lines []string
		var carry []byte
		discarding := false
		if fw.f != nil {
			drained, _ := fw.drain() // best effort; errors here don't block the switch
			lines = append(lines, drained...)
			fw.f.Close()
		} else if fw.rotated && !fw.opened {
			// The backup's unterminated tail (rotation can split a line)
			// carries into the current file.
			lines, carry, discarding = fw.readBackup(path + ".1")
		}
		nf, err := os.Open(path)
		if err != nil {
			fw.f, fw.fi = nil, nil
			return lines, err
		}
		fw.opened = true
		fw.f, fw.fi, fw.offset, fw.buf, fw.discarding = nf, fi, 0, carry, discarding
		more, err := fw.drain()
		return append(lines, more...), err
	}

	if fi.Size() < fw.offset {
		fw.f.Close()
		nf, err := os.Open(path)
		if err != nil {
			fw.f, fw.fi = nil, nil
			return nil, err
		}
		fw.f, fw.fi, fw.offset, fw.buf, fw.discarding = nf, fi, 0, nil, false
	}

	return fw.drain()
}

// readBackup reads every line of a rotated backup at path, returning
// them with its unterminated tail (and whether that tail is an overlong
// line being discarded). A missing or unreadable backup reads as empty.
func (fw *follower) readBackup(path string) (lines []string, tail []byte, discarding bool) {
	b := &follower{resolve: func(string) (string, error) { return path, nil }, role: fw.role, log: fw.log}
	defer b.close()
	lines, err := b.readNew()
	if err != nil && !os.IsNotExist(err) && fw.log != nil {
		fw.log.Warn("reading rotated log file", "role", fw.role, "path", path, "err", err)
	}
	return lines, b.buf, b.discarding
}

// close releases the currently open handle, if any. It is safe to call
// more than once and on a follower that never opened a file.
func (fw *follower) close() {
	if fw.f != nil {
		fw.f.Close()
		fw.f, fw.fi = nil, nil
	}
}

// drain reads from the current offset to EOF of the currently open file,
// returning complete lines and buffering any trailing partial line.
func (fw *follower) drain() ([]string, error) {
	if _, err := fw.f.Seek(fw.offset, io.SeekStart); err != nil {
		return nil, err
	}
	r := bufio.NewReader(fw.f)
	var lines []string
	for {
		chunk, err := r.ReadBytes('\n')
		if len(chunk) > 0 {
			fw.offset += int64(len(chunk))
			switch {
			case fw.discarding:
				// The rest of an overlong line: drop it through its newline.
				fw.discarding = chunk[len(chunk)-1] != '\n'
			case chunk[len(chunk)-1] == '\n':
				if len(fw.buf) > 0 {
					fw.buf = append(fw.buf, chunk...)
					lines = append(lines, string(fw.buf))
					fw.buf = nil
				} else {
					lines = append(lines, string(chunk))
				}
			case len(fw.buf)+len(chunk) > maxPartialLine:
				fw.buf, fw.discarding = nil, true
				if !fw.warned && fw.log != nil {
					fw.log.Warn("discarding a log line longer than the partial-line cap", "role", fw.role, "cap", maxPartialLine)
				}
				fw.warned = true
			default:
				fw.buf = append(fw.buf, chunk...)
			}
		}
		if err != nil {
			if err == io.EOF {
				return lines, nil
			}
			return lines, err
		}
	}
}
