package server

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jumpingmushroom/farsight/internal/explored"
	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
)

// Ingest body limits. The compressed cap applies to the request body as
// sent; the decompressed caps bound what gzip may expand it to.
// maxCompressed caps the request body as sent (a var so tests can lower it).
var maxCompressed int64 = 32 << 20

const (
	maxSnapshotDecompr = 128 << 20
	maxEventsDecompr   = 16 << 20
	tokenCacheTTL      = 10 * time.Minute
	msgTooLarge        = "request body too large"
)

// errTooLarge is returned by limitReader once its limit is exceeded.
var errTooLarge = errors.New("decompressed body too large")

// tokenKey identifies one verified (server, token) pair without keeping
// the token itself.
type tokenKey struct {
	server string
	sum    [sha256.Size]byte
}

// tokenCache remembers recently verified agent tokens so each request
// doesn't pay a bcrypt compare.
type tokenCache struct {
	mu      sync.Mutex
	entries map[tokenKey]time.Time // expiry
}

func newTokenCache() *tokenCache { return &tokenCache{entries: make(map[tokenKey]time.Time)} }

func (c *tokenCache) valid(k tokenKey, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	exp, ok := c.entries[k]
	return ok && now.Before(exp)
}

// add records k as verified until now+TTL, first dropping expired
// entries so the map stays bounded by the number of live (server, token)
// pairs.
func (c *tokenCache) add(k tokenKey, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, exp := range c.entries {
		if !now.Before(exp) {
			delete(c.entries, key)
		}
	}
	c.entries[k] = now.Add(tokenCacheTTL)
}

// authIngest checks the path's server and bearer token, writing the error
// response and returning ok=false on failure. A missing token, an unknown
// server and a wrong token all get the same 401, so ingest can't be used
// to probe which server ids exist. Anything that needs a bcrypt compare
// (a token-cache miss, including every unknown server) is first charged
// to the per-IP IngestLimiter; a successful compare refunds the charge.
func (s *server) authIngest(w http.ResponseWriter, r *http.Request) (id string, ok bool) {
	id = r.PathValue("server")
	token, hasBearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !hasBearer || token == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return "", false
	}
	srv, found := s.lookup(id)
	key := tokenKey{server: id, sum: sha256.Sum256([]byte(token))}
	now := s.Now()
	if found && s.tokens.valid(key, now) {
		return id, true
	}

	ip := s.clientIP(r)
	if !s.IngestLimiter.Allow(ip) {
		writeError(w, http.StatusTooManyRequests, "too many attempts")
		return "", false
	}
	hash := s.dummyTokenHash()
	if found {
		hash = []byte(srv.AgentTokenHash)
	}
	match, err := compareHash(r.Context(), hash, []byte(token))
	if err != nil {
		// Cancelled while queued for a compare: nothing was checked.
		s.IngestLimiter.Refund(ip)
		writeError(w, http.StatusServiceUnavailable, "busy")
		return "", false
	}
	if !found || !match {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return "", false
	}
	s.IngestLimiter.Refund(ip)
	s.tokens.add(key, now)
	return id, true
}

// limitReader reads at most n bytes from r, failing with errTooLarge if
// there is more.
type limitReader struct {
	r io.Reader
	n int64
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		// Probe for one more byte to tell "exactly n" from "more than n".
		var one [1]byte
		k, err := l.r.Read(one[:])
		if k > 0 {
			return 0, errTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	k, err := l.r.Read(p)
	l.n -= int64(k)
	return k, err
}

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	k, err := c.r.Read(p)
	c.n += int64(k)
	return k, err
}

// decodeBody decodes the gzip JSON request body into v, enforcing the
// size limits, and returns the number of compressed bytes read. It writes
// the error response and returns ok=false on failure.
func decodeBody(w http.ResponseWriter, r *http.Request, maxDecompressed int64, v any) (compressed int64, ok bool) {
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Content-Encoding")), "gzip") {
		writeError(w, http.StatusBadRequest, "Content-Encoding: gzip required")
		return 0, false
	}
	body := &countingReader{r: http.MaxBytesReader(w, r.Body, maxCompressed)}
	zr, err := gzip.NewReader(body)
	if err == nil {
		defer zr.Close()
		dec := json.NewDecoder(&limitReader{r: zr, n: maxDecompressed})
		err = dec.Decode(v)
		if err == nil {
			// Reject trailing garbage after the JSON value.
			if _, terr := dec.Token(); terr != io.EOF {
				err = errors.New("trailing data")
				if terr != nil {
					err = terr
				}
			}
		}
	}
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) || errors.Is(err, errTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, msgTooLarge)
		} else {
			writeError(w, http.StatusBadRequest, "malformed body")
		}
		return body.n, false
	}
	return body.n, true
}

// storedSaveID is the id a snapshot is stored (and deduplicated) under.
// A chunked save's id is only the game's save counter ("chunked:N"),
// which restarts lower when a world backup is restored, so it gets the
// save time appended: otherwise every new save up to the old counter
// would be dropped as a duplicate of a pre-restore one. Legacy ids
// already carry the file's mtime. A retried POST of the same save keeps
// the same SavedAt, so it still dedupes.
func storedSaveID(saveID string, savedAt time.Time) string {
	if strings.HasPrefix(saveID, "chunked:") && strings.Count(saveID, ":") == 1 {
		return saveID + ":" + strconv.FormatInt(savedAt.UnixMilli(), 10)
	}
	return saveID
}

// maxSnapshotFuture is how far past now a snapshot's SavedAt (the save
// file's mtime, by the agent host's clock) may be before it is stored as
// now instead. Left alone, a future save time stays the latest save for
// good, moves the world diff past every correct save that follows and
// stalls the world clock estimate.
const maxSnapshotFuture = 10 * time.Minute

func (s *server) ingestSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authIngest(w, r)
	if !ok {
		return
	}
	var snap extract.Snapshot
	n, ok := decodeBody(w, r, maxSnapshotDecompr, &snap)
	if !ok {
		return
	}
	if snap.ServerID != id || snap.SaveID == "" || snap.SavedAt.IsZero() {
		writeError(w, http.StatusBadRequest, "invalid snapshot")
		return
	}
	if snap.Explored != nil {
		if _, err := explored.Decode(*snap.Explored); err != nil {
			writeError(w, http.StatusBadRequest, "invalid snapshot")
			return
		}
	}
	// The id comes from the save time as sent, before any clamp: a retry
	// of a clamped save would clamp to a later now, and must still dedupe.
	snap.SaveID = storedSaveID(snap.SaveID, snap.SavedAt)
	if now := s.Now(); snap.SavedAt.After(now.Add(maxSnapshotFuture)) {
		s.Log.Warn("server: snapshot saved in the future, stored as received now",
			"server", id, "saveId", snap.SaveID, "savedAt", snap.SavedAt, "offset", snap.SavedAt.Sub(now).Round(time.Second))
		snap.SavedAt = now
	}
	blob, err := json.Marshal(snap)
	if err != nil {
		s.Log.Error("server: encode snapshot", "server", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	stored, err := s.Store.PutSnapshot(r.Context(), id, snap.SaveID, snap.SavedAt, snap.ReadAt, blob)
	if err != nil {
		s.Log.Error("server: store snapshot", "server", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Idempotent and cheap, so run it even for a duplicate: it restarts a
	// render that failed or was lost to a restart.
	s.Tiles.Ensure(snap.World.Seed, snap.World.GenVersion)
	// Diff the new save against the previous one into world events, in the
	// background: a slow backfill must never hold up the response. Also
	// idempotent; a failure is logged, not the agent's problem: the next
	// save (or a restart's backfill) catches up.
	s.World.CatchUpAsync(id)
	s.Log.Info("ingest", "server", id, "kind", "snapshot", "stored", stored, "saveId", snap.SaveID, "bytes", n)
	writeJSON(w, http.StatusOK, map[string]bool{"stored": stored})
}

func (s *server) ingestEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authIngest(w, r)
	if !ok {
		return
	}
	var body struct {
		Events []logwatch.Event `json:"events"`
	}
	n, ok := decodeBody(w, r, maxEventsDecompr, &body)
	if !ok {
		return
	}
	applied, skipped, err := s.Applier.Apply(r.Context(), id, body.Events)
	if err != nil {
		s.Log.Error("server: apply events", "server", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.Log.Info("ingest", "server", id, "kind", "events", "applied", applied, "skipped", skipped, "bytes", n)
	writeJSON(w, http.StatusOK, map[string]int{"applied": applied, "skipped": skipped})
}
