package logwatch

import (
	"bufio"
	"io"
	"log/slog"
	"os"
)

// maxLine caps the bytes of one log line, newline included, that a
// follower will hold: a line longer than that is discarded through its
// terminating newline, whether it arrives whole or grows across polls, so
// a runaway writer can't grow memory unbounded.
const maxLine = 1 << 20

// readChunk is the size of a follower's read buffer: lines are read in
// fragments of at most this many bytes and assembled in follower.buf, so
// the cap is checked before a line's bytes are held, not after.
const readChunk = 64 << 10

// openFile is a seam over os.Open, so tests can rotate a file between
// readNew's stat of a path and its open.
var openFile = os.Open

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
	buf    []byte        // bytes of a not-yet-terminated line, carried across polls
	r      *bufio.Reader // reused across drains, reset onto f at each

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

// readNew hands line each complete line appended since the last call, in
// order, as it is read: nothing is kept once line returns, so a replay of
// a large file holds one line at a time rather than the whole file. A
// trailing partial line (no '\n' yet) is buffered and prefixed onto the
// next complete line once it arrives. If the resolved file's identity
// differs from the one currently open (rotation, or a fresh path after a
// restart), or the file has shrunk below the tracked offset (truncation),
// readNew drains the old handle to EOF first — so nothing written just
// before a rename is lost — then reopens the new path from offset 0.
func (fw *follower) readNew(line func(string)) error {
	current := ""
	if fw.f != nil {
		current = fw.f.Name()
	}
	path, err := fw.resolve(current)
	if err != nil {
		return err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}

	if fw.f == nil || !os.SameFile(fw.fi, fi) {
		var carry []byte
		discarding := false
		if fw.f != nil {
			fw.drain(line) // best effort; errors here don't block the switch
			fw.f.Close()
		} else if fw.rotated && !fw.opened {
			// The backup's unterminated tail (rotation can split a line)
			// carries into the current file.
			carry, discarding = fw.readBackup(path+".1", line)
		}
		nf, nfi, err := open(path)
		if err != nil {
			fw.f, fw.fi = nil, nil
			return err
		}
		fw.opened = true
		fw.f, fw.fi, fw.offset, fw.buf, fw.discarding = nf, nfi, 0, carry, discarding
		return fw.drain(line)
	}

	if fi.Size() < fw.offset {
		fw.f.Close()
		nf, nfi, err := open(path)
		if err != nil {
			fw.f, fw.fi = nil, nil
			return err
		}
		fw.f, fw.fi, fw.offset, fw.buf, fw.discarding = nf, nfi, 0, nil, false
	}

	return fw.drain(line)
}

// open opens path and returns the handle with its own identity. The path
// may be rotated between readNew's stat and this open, so the identity
// must come from the handle, not the earlier stat: a mismatch would make
// the next poll treat the file already open as new and read it again.
func open(path string) (*os.File, os.FileInfo, error) {
	f, err := openFile(path)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, fi, nil
}

// readBackup hands line every line of a rotated backup at path, returning
// its unterminated tail (and whether that tail is an overlong line being
// discarded). A missing or unreadable backup reads as empty.
func (fw *follower) readBackup(path string, line func(string)) (tail []byte, discarding bool) {
	b := &follower{resolve: func(string) (string, error) { return path, nil }, role: fw.role, log: fw.log}
	defer b.close()
	err := b.readNew(line)
	if err != nil && !os.IsNotExist(err) && fw.log != nil {
		fw.log.Warn("reading rotated log file", "role", fw.role, "path", path, "err", err)
	}
	fw.warned = fw.warned || b.warned
	return b.buf, b.discarding
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
// handing line each complete line and buffering any trailing partial
// line. It reads in fragments of at most readChunk bytes, so a line is
// only ever held whole once it is known to fit under maxLine.
func (fw *follower) drain(line func(string)) error {
	if _, err := fw.f.Seek(fw.offset, io.SeekStart); err != nil {
		return err
	}
	if fw.r == nil {
		fw.r = bufio.NewReaderSize(fw.f, readChunk)
	} else {
		fw.r.Reset(fw.f)
	}
	for {
		frag, err := fw.r.ReadSlice('\n')
		if len(frag) > 0 {
			fw.offset += int64(len(frag))
			complete := frag[len(frag)-1] == '\n'
			switch {
			case fw.discarding:
				// The rest of an overlong line: drop it through its newline.
				fw.discarding = !complete
			case len(fw.buf)+len(frag) > maxLine:
				fw.buf, fw.discarding = nil, !complete
				if !fw.warned && fw.log != nil {
					fw.log.Warn("discarding a log line longer than the line cap", "role", fw.role, "cap", maxLine)
				}
				fw.warned = true
			case complete && len(fw.buf) > 0:
				fw.buf = append(fw.buf, frag...)
				line(string(fw.buf))
				fw.buf = nil
			case complete:
				line(string(frag))
			default:
				// frag is only valid until the next read: copy it.
				fw.buf = append(fw.buf, frag...)
			}
		}
		switch err {
		case nil, bufio.ErrBufferFull:
		case io.EOF:
			return nil
		default:
			return err
		}
	}
}
