package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// gzipMinBytes is the smallest response gzipJSON will compress; anything
// under it is sent as-is, since gzip's overhead isn't worth it for a small
// JSON body.
const gzipMinBytes = 1024

// gzWriterPool reuses gzip.Writer values across requests.
var gzWriterPool = sync.Pool{
	New: func() any { return gzip.NewWriter(nil) },
}

// gzipJSON wraps a handler so that, when the request's Accept-Encoding
// includes gzip, a response of at least gzipMinBytes is gzip-compressed.
// It always sets Vary: Accept-Encoding on the routes it wraps, whether or
// not the particular response ends up compressed, and it never compresses
// a response the handler already gave a Content-Encoding.
//
// If the wrapped handler panics, gzipJSON recovers from it itself rather
// than letting it reach an outer recoverer: once compression has started
// (or the handler committed a plain response, via its own Content-Encoding
// or an explicit Flush while still buffering), a 200 and part of the body
// have already reached the client, so a plain-text 500 written by
// recoverer on top of that would corrupt the stream. In that case gzipJSON
// still finishes (Closes) the gzip stream so it's a validly-terminated, if
// truncated, gzip member and returns the writer to the pool — but there is
// no way to send a clean error response at that point, so rather than
// returning normally (which would let the response complete looking like
// a truncated, but ostensibly "successful", 200), it logs the panic itself
// and re-panics with http.ErrAbortHandler: recoverer already re-panics
// that value unchanged, and net/http then aborts the connection without
// writing or logging anything further. If the handler's own panic value
// was already http.ErrAbortHandler (a deliberate abort, not a bug), that's
// passed through unchanged, without logging it as an error.
//
// If nothing was written yet (still buffering under gzipMinBytes), nothing
// has reached the client, so the panic is re-raised unchanged and an outer
// recoverer can still write a clean 500.
func gzipJSON(log *slog.Logger, next http.Handler) http.Handler {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if !acceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer func() {
			p := recover()
			if p == nil {
				gw.finish()
				return
			}
			if !gw.committed() {
				// Nothing reached the client yet: there's nothing for
				// gw to clean up, so let the panic propagate untouched.
				panic(p)
			}
			// Headers and part of a compressed (or committed-plain) body
			// already reached the client as a 200. Finish (Close) the
			// gzip stream and return the pooled writer regardless of why
			// the handler panicked.
			gw.finish()
			if p == http.ErrAbortHandler {
				// A deliberate abort, not a bug: pass it through as is.
				panic(p)
			}
			log.Error("server: handler panic mid-response", "method", r.Method, "path", r.URL.Path, "panic", p)
			// There is no way to send a clean error response now: abort
			// the connection outright instead of returning normally.
			panic(http.ErrAbortHandler)
		}()
		next.ServeHTTP(gw, r)
	})
}

// acceptsGzip reports whether the request's Accept-Encoding header lists
// gzip as acceptable: present as a token, and not explicitly disabled with
// a zero q-value ("gzip;q=0"), per RFC 9110 12.5.1.
func acceptsGzip(r *http.Request) bool {
	for _, v := range r.Header.Values("Accept-Encoding") {
		for _, tok := range strings.Split(v, ",") {
			name, params, hasParams := strings.Cut(tok, ";")
			if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
				continue
			}
			if hasParams && qIsZero(params) {
				continue
			}
			return true
		}
	}
	return false
}

// qIsZero reports whether params (everything after a token's first ";")
// carries an explicit q=0 (in any of its numeric spellings, e.g. "0",
// "0.0", "0.000"), which per RFC 9110 12.5.1 means "not acceptable".
func qIsZero(params string) bool {
	for _, p := range strings.Split(params, ";") {
		name, val, ok := strings.Cut(p, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "q") {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		return err == nil && f == 0
	}
	return false
}

// gzipResponseWriter buffers up to gzipMinBytes of the response before
// deciding whether to compress it: small bodies are flushed as-is, larger
// ones switch to a pooled gzip.Writer partway through.
type gzipResponseWriter struct {
	http.ResponseWriter
	buf           bytes.Buffer
	code          int
	headerWritten bool
	gz            *gzip.Writer // non-nil once compressing
	passthrough   bool         // the handler already set Content-Encoding
}

// committed reports whether anything (headers or body bytes) has already
// reached the real http.ResponseWriter: once compressing, or once the
// handler's own Content-Encoding sent us into passthrough, headers have
// necessarily been flushed too.
func (g *gzipResponseWriter) committed() bool {
	return g.gz != nil || g.passthrough
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if g.code == 0 {
		g.code = code
	}
}

func (g *gzipResponseWriter) Write(p []byte) (int, error) {
	switch {
	case g.gz != nil:
		return g.gz.Write(p)
	case g.passthrough:
		g.flushHeader()
		return g.ResponseWriter.Write(p)
	case g.ResponseWriter.Header().Get("Content-Encoding") != "":
		// The handler compressed (or otherwise encoded) the body itself;
		// never double-compress it.
		g.passthrough = true
		g.flushHeader()
		if g.buf.Len() > 0 {
			if _, err := g.ResponseWriter.Write(g.buf.Bytes()); err != nil {
				return 0, err
			}
			g.buf.Reset()
		}
		return g.ResponseWriter.Write(p)
	case g.buf.Len()+len(p) < gzipMinBytes:
		return g.buf.Write(p)
	default:
		if err := g.startCompressing(); err != nil {
			return 0, err
		}
		return g.gz.Write(p)
	}
}

// startCompressing switches to gzip once the buffered body has crossed
// gzipMinBytes: it sets the compression headers, drops any stale
// Content-Length (the compressed length isn't known up front), writes the
// status line, and flushes what's buffered so far through a pooled
// gzip.Writer.
func (g *gzipResponseWriter) startCompressing() error {
	h := g.ResponseWriter.Header()
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	g.flushHeader()

	gz := gzWriterPool.Get().(*gzip.Writer)
	gz.Reset(g.ResponseWriter)
	g.gz = gz
	if g.buf.Len() == 0 {
		return nil
	}
	_, err := gz.Write(g.buf.Bytes())
	g.buf.Reset()
	return err
}

func (g *gzipResponseWriter) flushHeader() {
	if g.headerWritten {
		return
	}
	g.headerWritten = true
	code := g.code
	if code == 0 {
		code = http.StatusOK
	}
	g.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher. While compressing, it flushes the gzip
// writer (its underlying flate blocks) so buffered-but-unwritten
// compressed bytes reach the client now; while still buffering under
// gzipMinBytes, an explicit Flush forces that buffer out uncompressed,
// since there is no later point at which to decide to compress it — and,
// because that commits a plain (uncompressed) response to the wire, it
// also marks the writer passthrough, so a later Write that crosses
// gzipMinBytes can't switch to compression mid-stream, which would write
// gzip bytes right after the already-sent plain ones. Either way, Flush
// then flushes the underlying ResponseWriter, if it can.
func (g *gzipResponseWriter) Flush() {
	switch {
	case g.gz != nil:
		g.gz.Flush()
	case g.passthrough:
		// Already written straight through; nothing buffered here.
	default:
		g.flushHeader()
		if g.buf.Len() > 0 {
			g.ResponseWriter.Write(g.buf.Bytes())
			g.buf.Reset()
		}
		g.passthrough = true
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// finish flushes whatever is left once the handler has returned: a small,
// never-compressed body, or the trailing gzip bytes and footer. It is
// idempotent-ish in the sense that only the branch matching the writer's
// current state runs; callers (gzipJSON) only ever call it once per
// request.
func (g *gzipResponseWriter) finish() {
	if g.gz != nil {
		g.gz.Close() // nothing more to do with a write error this late
		gzWriterPool.Put(g.gz)
		g.gz = nil
		return
	}
	if g.passthrough {
		return
	}
	g.flushHeader()
	if g.buf.Len() > 0 {
		g.ResponseWriter.Write(g.buf.Bytes())
	}
}
