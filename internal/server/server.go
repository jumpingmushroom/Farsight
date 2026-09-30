// Package server is the farsight central backend's HTTP layer: agent
// ingest (snapshots and log events), visitor unlock, the per-server card
// and map-data API, and world map tile serving.
package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jumpingmushroom/farsight/internal/auth"
	"github.com/jumpingmushroom/farsight/internal/config"
	"github.com/jumpingmushroom/farsight/internal/live"
	"github.com/jumpingmushroom/farsight/internal/store"
	"github.com/jumpingmushroom/farsight/internal/tileset"
)

// Deps are everything the HTTP handlers need.
type Deps struct {
	Config  *config.Config
	Store   *store.Store
	Applier *live.Applier
	Tiles   *tileset.Manager
	Codec   auth.Codec
	Limiter *auth.Limiter // unlock attempts, per client IP
	// IngestLimiter charges ingest requests whose token needs a bcrypt
	// compare (cache misses and unknown server ids), per client IP; nil
	// means a default of 10 per minute, burst 10.
	IngestLimiter *auth.Limiter
	Now           func() time.Time
	Log           *slog.Logger
	UI            http.Handler // Plan 4b static UI; nil => 404 for "/"
	// SplitIngest, when true, means ingest is served on a separate
	// in-cluster listener (NewIngest): the public handler (New) returns
	// the shared JSON 404 for every /ingest/ path instead of serving it.
	SplitIngest bool
}

// maxCookieLen is the longest cookie value we will try to decode; anything
// longer is treated as no cookie at all.
const maxCookieLen = 4096

type server struct {
	Deps
	tokens *tokenCache
	// dummyHash is compared against for unlock attempts on an unknown
	// server, so the response time doesn't reveal which ids exist. It is
	// generated once, on first use, at the configured hashes' cost.
	dummyHash func() []byte
	// dummyTokenHash does the same for ingest requests naming an unknown
	// server, at the configured agent token hashes' cost.
	dummyTokenHash func() []byte
}

// newServer fills in Deps defaults and builds the handler state.
func newServer(d Deps) *server {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if d.IngestLimiter == nil {
		d.IngestLimiter = auth.NewLimiter(10, 10, d.Now)
	}
	return &server{
		Deps:           d,
		tokens:         newTokenCache(),
		dummyHash:      newDummyHash(dummyCost(d.Config)),
		dummyTokenHash: newDummyHash(dummyTokenCost(d.Config)),
	}
}

// healthz is the liveness/readiness probe, identical on every listener.
func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, "ok\n")
}

// notFoundHandler is the shared "unknown route" handler, used for every
// prefix that isn't otherwise registered.
func notFoundHandler(w http.ResponseWriter, r *http.Request) { notFound(w) }

// NewHandlers builds the public and ingest HTTP handlers from one shared
// server state, so the ingest limiter, token cache and bcrypt semaphore
// aren't duplicated between them. New and NewIngest are thin wrappers
// around it for callers that only need one side.
func NewHandlers(d Deps) (public, ingest http.Handler) {
	s := newServer(d)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)

	if d.SplitIngest {
		// Ingest lives on the separate in-cluster listener; every
		// /ingest/ path here is the shared JSON 404, same as an unknown
		// API or tiles route.
		mux.HandleFunc("/ingest/", notFoundHandler)
	} else {
		mux.HandleFunc("POST /ingest/{server}/snapshot", s.ingestSnapshot)
		mux.HandleFunc("POST /ingest/{server}/events", s.ingestEvents)
	}

	mux.HandleFunc("POST /api/unlock", s.unlock)
	mux.HandleFunc("GET /api/servers", s.listServers)
	mux.Handle("GET /api/servers/{id}", gzipJSON(s.Log, http.HandlerFunc(s.card)))
	mux.Handle("GET /api/servers/{id}/snapshot", gzipJSON(s.Log, http.HandlerFunc(s.snapshot)))

	mux.HandleFunc("GET /tiles/{id}/{key}/{z}/{x}/{y}", s.tile)

	// Anything else under the API prefixes is a JSON 404, never the UI.
	// "/ingest/" is already registered above when SplitIngest is true.
	prefixes := []string{"/api/", "/tiles/"}
	if !d.SplitIngest {
		prefixes = append(prefixes, "/ingest/")
	}
	for _, p := range prefixes {
		mux.HandleFunc(p, notFoundHandler)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if s.UI != nil {
			s.UI.ServeHTTP(w, r)
			return
		}
		notFound(w)
	})

	imux := http.NewServeMux()
	imux.HandleFunc("GET /healthz", s.healthz)
	imux.HandleFunc("POST /ingest/{server}/snapshot", s.ingestSnapshot)
	imux.HandleFunc("POST /ingest/{server}/events", s.ingestEvents)
	imux.HandleFunc("/", notFoundHandler)

	return recoverer(s.Log, mux), recoverer(s.Log, noForwardedFor(imux))
}

// noForwardedFor drops X-Forwarded-For before the ingest handler sees it.
// TrustProxy describes the public listener's ingress; nothing proxies the
// in-cluster ingest listener, so there the header is client-supplied and
// clientIP must fall back to RemoteAddr.
func noForwardedFor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Values("X-Forwarded-For")) > 0 {
			r = r.Clone(r.Context())
			r.Header.Del("X-Forwarded-For")
		}
		next.ServeHTTP(w, r)
	})
}

// New returns the farsight public HTTP handler.
func New(d Deps) http.Handler {
	public, _ := NewHandlers(d)
	return public
}

// NewIngest returns the in-cluster ingest-only HTTP handler: only the
// ingest POST routes and /healthz; everything else is a JSON 404.
func NewIngest(d Deps) http.Handler {
	_, ingest := NewHandlers(d)
	return ingest
}

// recoverer turns a handler panic into a 500, logging the panic value
// (never the request body).
func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			p := recover()
			if p == nil {
				return
			}
			if p == http.ErrAbortHandler {
				panic(p)
			}
			log.Error("server: handler panic", "method", r.Method, "path", r.URL.Path, "panic", p)
			writeError(w, http.StatusInternalServerError, "internal error")
		}()
		next.ServeHTTP(w, r)
	})
}

// lookup finds a configured server by id.
func (s *server) lookup(id string) (*config.Server, bool) {
	for i := range s.Config.Servers {
		if s.Config.Servers[i].ID == id {
			return &s.Config.Servers[i], true
		}
	}
	return nil, false
}

// unlockedIDs returns the server ids carried by the request's valid
// unlock cookie (nil if none).
func (s *server) unlockedIDs(r *http.Request) []string {
	c, err := r.Cookie(auth.CookieName)
	if err != nil || len(c.Value) > maxCookieLen {
		return nil
	}
	ids, ok := s.Codec.Decode(c.Value)
	if !ok {
		return nil
	}
	return ids
}

// unlocked reports whether id is a configured server that the request's
// cookie has unlocked, returning its config.
func (s *server) unlocked(r *http.Request, id string) (*config.Server, bool) {
	srv, ok := s.lookup(id)
	if !ok {
		return nil, false
	}
	for _, u := range s.unlockedIDs(r) {
		if u == id {
			return srv, true
		}
	}
	return nil, false
}

// clientIP is the address the unlock and ingest limiters key on: with
// TrustProxy, the rightmost non-empty X-Forwarded-For entry (the address
// the single trusted proxy saw; every earlier entry is client-supplied),
// otherwise, or if there is none, RemoteAddr's host. The split ingest
// listener strips the header first (noForwardedFor), so it always keys on
// RemoteAddr.
func (s *server) clientIP(r *http.Request) string {
	if s.Config.TrustProxy {
		// Header.Values, not Get: a proxy may append its own line rather
		// than extend the client's, and that last line is the one to trust.
		vals := r.Header.Values("X-Forwarded-For")
		for i := len(vals) - 1; i >= 0; i-- {
			hops := strings.Split(vals[i], ",")
			for j := len(hops) - 1; j >= 0; j-- {
				if hop := strings.TrimSpace(hops[j]); hop != "" {
					return hop
				}
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// notFound is the single 404 body used for unknown routes, unknown server
// ids and locked servers alike.
func notFound(w http.ResponseWriter) { writeError(w, http.StatusNotFound, "not found") }

// rfc3339 formats t as an RFC 3339 UTC string.
func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// optTime is rfc3339(t), or "" (omitted) for the zero time.
func optTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return rfc3339(t)
}
