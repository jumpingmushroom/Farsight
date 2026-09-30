package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Final review: transactions begin IMMEDIATE, taking the write lock up
// front, so a read-then-write transaction can't lose a race to another
// committer with SQLITE_BUSY_SNAPSHOT (which busy_timeout doesn't retry).
func TestTxBeginsImmediate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "farsight.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// A second, independent connection that fails at once on a lock.
	other, err := sql.Open("sqlite", "file:"+path+"?_busy_timeout=0")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	ctx := context.Background()
	err = s.Tx(ctx, func(tx *sql.Tx) error {
		// The transaction has run no statement yet; only an IMMEDIATE
		// BEGIN holds the write lock at this point.
		_, err := other.ExecContext(ctx, `INSERT INTO live (server_id) VALUES ('other')`)
		if err == nil {
			return errors.New("another connection wrote while a store transaction was open: BEGIN was not IMMEDIATE")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Concurrent read-then-write transactions and plain writes on an on-disk
// store never fail.
func TestConcurrentTxAndWritesDoNotError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "farsight.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	const n = 150
	errs := make(chan error, 3*n)
	var wg sync.WaitGroup
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
				err := s.Tx(ctx, func(tx *sql.Tx) error {
					l, _, err := s.GetLive(ctx, tx, "srv")
					if err != nil {
						return err
					}
					l.ServerID = "srv"
					l.Players++
					return s.PutLive(ctx, tx, l)
				})
				if err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			if _, err := s.PutSnapshot(ctx, "srv", itoa(i), now, now, []byte(`{}`)); err != nil {
				errs <- err
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	l, _, err := s.GetLive(ctx, nil, "srv")
	if err != nil {
		t.Fatal(err)
	}
	if l.Players != 2*n {
		t.Fatalf("players = %d, want %d (lost updates)", l.Players, 2*n)
	}
}

// Final review: the on-disk DSN is built with escaping, so a path with
// spaces, '#', '%' or '?' opens exactly that file.
func TestOpenPathWithSpecialCharacters(t *testing.T) {
	for _, name := range []string{"with space", "hash#frag", "per%20cent", "q?mark=1&x"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "farsight.db")
			roundTrip(t, path)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("database not at %q: %v", path, err)
			}
		})
	}
}

// A relative path is relative to the working directory.
func TestOpenRelativePath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	roundTrip(t, filepath.Join("sub dir", "farsight.db"))
	if _, err := os.Stat(filepath.Join(dir, "sub dir", "farsight.db")); err != nil {
		t.Fatal(err)
	}
}

// roundTrip opens path (creating its parent), writes a live row, reopens
// and reads it back.
func roundTrip(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open %q: %v", path, err)
	}
	if err := s.PutLive(ctx, nil, Live{ServerID: "srv", Players: 7}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen %q: %v", path, err)
	}
	defer s.Close()
	l, ok, err := s.GetLive(ctx, nil, "srv")
	if err != nil || !ok || l.Players != 7 {
		t.Fatalf("reopen %q: live = %+v ok=%v err=%v", path, l, ok, err)
	}
}
