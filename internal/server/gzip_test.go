package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Fix round 1: gzip;q=0 (in any spelling) means "not acceptable", and a
// zero-valued weight on some other token doesn't affect gzip.
func TestAcceptsGzipHonoursQValues(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"", false},
		{"gzip", true},
		{"GZIP", true},
		{"deflate, gzip", true},
		{"gzip;q=1", true},
		{"gzip; q=1.0", true},
		{"gzip;q=0.5", true},
		{"gzip;q=0", false},
		{"gzip;q=0.0", false},
		{"gzip;q=0.000", false},
		{"gzip; q=0", false},
		{"deflate, gzip;q=0", false},
		{"gzip;q=0, deflate", false},
		{"*;q=0", false}, // no gzip token at all
		{"identity;q=1", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		if c.header != "" {
			r.Header.Set("Accept-Encoding", c.header)
		}
		if got := acceptsGzip(r); got != c.want {
			t.Errorf("Accept-Encoding %q: acceptsGzip = %v, want %v", c.header, got, c.want)
		}
	}
}

// Fix round 1: an explicit http.Flusher on gzipResponseWriter flushes the
// gzip writer (while compressing) or the buffered small body (while still
// deciding), then the underlying Flusher.
func TestGzipResponseWriterFlush(t *testing.T) {
	// Small (under threshold): Flush forces the buffered body out
	// uncompressed immediately.
	rec := httptest.NewRecorder()
	gw := &gzipResponseWriter{ResponseWriter: rec}
	gw.Write([]byte("short"))
	gw.Flush()
	if rec.Body.String() != "short" {
		t.Fatalf("after Flush, body = %q, want %q", rec.Body.String(), "short")
	}
	if rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding = %q, want none", rec.Header().Get("Content-Encoding"))
	}
	if !rec.Flushed {
		t.Error("underlying ResponseWriter's Flush was not called")
	}
	gw.finish() // no more buffered bytes: a no-op, must not duplicate the body
	if rec.Body.String() != "short" {
		t.Fatalf("after finish, body = %q, want unchanged %q", rec.Body.String(), "short")
	}

	// Large (over threshold): Flush flushes the gzip writer without
	// error, and later writes still decode correctly once finished.
	rec2 := httptest.NewRecorder()
	gw2 := &gzipResponseWriter{ResponseWriter: rec2}
	big := bytes.Repeat([]byte("y"), 2000)
	gw2.Write(big)
	gw2.Flush()
	gw2.Write([]byte("more"))
	gw2.finish()
	if rec2.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", rec2.Header().Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(rec2.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	want := append(append([]byte{}, big...), []byte("more")...)
	if !bytes.Equal(got, want) {
		t.Errorf("decoded %d bytes, want %d bytes matching the writes", len(got), len(want))
	}
}

// Fix round 2: once Flush has forced a small, still-buffering response
// onto the wire as plain, the response must stay plain forever after —
// even if a later Write crosses gzipMinBytes. Switching to compression
// mid-stream at that point would write gzip bytes right after the
// already-sent plain ones, corrupting the response.
func TestGzipResponseWriterFlushCommitsPlainEncoding(t *testing.T) {
	rec := httptest.NewRecorder()
	gw := &gzipResponseWriter{ResponseWriter: rec}
	gw.Write([]byte("short"))
	gw.Flush()
	big := bytes.Repeat([]byte("z"), 2000)
	gw.Write(big)
	gw.finish()

	want := append([]byte("short"), big...)
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("body = %d bytes, want %d bytes matching the plain concatenation", rec.Body.Len(), len(want))
	}
	if rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding = %q, want none: Flush already committed a plain response", rec.Header().Get("Content-Encoding"))
	}
}

// Fix round 1: a panic before the 1 KiB threshold is crossed leaves
// nothing committed to the client, so gzipJSON lets it propagate
// unchanged and an outer recoverer writes its normal, clean 500.
func TestGzipJSONPanicBeforeThresholdPropagatesToRecoverer(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := recoverer(log, gzipJSON(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("short"))
		panic("boom")
	})))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Errorf("Content-Encoding = gzip, want none: the buffered body was discarded, not compressed")
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q did not decode as JSON: %v", rec.Body.String(), err)
	}
	if body["error"] != "internal error" {
		t.Errorf("body = %v, want the standard 500 error", body)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("short")) {
		t.Errorf("the discarded buffered body leaked into the response: %s", rec.Body.String())
	}
}

// Fix round 2: a panic after compression has started (over 1 KiB already
// written, headers already flushed as 200) is recovered by gzipJSON
// itself, never reaching recoverer: recoverer writing a plain-text 500 on
// top of an already-sent 200 would corrupt the compressed stream. gzipJSON
// still finishes (Closes) the gzip stream and returns its writer to the
// pool, and logs the panic itself — but design round 2 changed what
// happens next: rather than returning normally (which would let the
// response complete looking like a truncated, but ostensibly
// "successful", 200), it re-panics with http.ErrAbortHandler.
// recoverer already re-panics that value unchanged, so net/http aborts
// the connection: the client must see a network error or a read error,
// never a clean, fully-decodable 200 body.
func TestGzipJSONAbortsConnectionOnPanicAfterCompressionStarts(t *testing.T) {
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, nil))
	big := bytes.Repeat([]byte("a"), 2000)
	h := recoverer(log, gzipJSON(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(big) // crosses gzipMinBytes: compression has started
		panic("boom")
	})))
	srv := httptest.NewServer(h)
	defer srv.Close()

	res, err := http.Get(srv.URL)
	if err != nil {
		// The connection was aborted before a response was even fully
		// received: a network error is itself the expected failure signal.
	} else {
		defer res.Body.Close()
		if _, err := io.ReadAll(res.Body); err == nil {
			t.Fatal("expected the connection to abort (a network error or a body read error), got a complete response")
		}
	}

	if logged.Len() == 0 {
		t.Error("the panic was not logged anywhere (recoverer was bypassed and gzipJSON didn't log it)")
	}
}

// Fix round 1/2: repeated compress-then-panic cycles must not corrupt the
// shared gzWriterPool (each writer is Reset before reuse, regardless of
// whether the previous user Closed it cleanly or was cut short mid-panic).
// Since a post-commit panic now aborts (panics http.ErrAbortHandler)
// instead of returning normally, this drives that directly through
// ServeHTTP and recovers the expected abort itself, rather than going
// over a real connection: what's under test here is pool hygiene, not the
// network-level abort (covered by TestGzipJSONAbortsConnectionOnPanicAfterCompressionStarts).
func TestGzipJSONPanicAfterCompressionStartsPoolReuseIsSafe(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	big := bytes.Repeat([]byte("b"), 2000)
	panics := recoverer(log, gzipJSON(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(big)
		panic("boom")
	})))
	ok := recoverer(log, gzipJSON(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(big)
	})))

	callExpectingAbort := func(t *testing.T, h http.Handler, rec *httptest.ResponseRecorder, req *http.Request) {
		t.Helper()
		defer func() {
			p := recover()
			if p != http.ErrAbortHandler {
				t.Fatalf("recovered %v, want http.ErrAbortHandler", p)
			}
		}()
		h.ServeHTTP(rec, req)
		t.Fatal("expected a panic(http.ErrAbortHandler); the handler returned normally")
	}

	for i := 0; i < 20; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		callExpectingAbort(t, panics, rec, req)

		rec2 := httptest.NewRecorder()
		req2 := httptest.NewRequest("GET", "/", nil)
		req2.Header.Set("Accept-Encoding", "gzip")
		ok.ServeHTTP(rec2, req2)
		zr, err := gzip.NewReader(rec2.Body)
		if err != nil {
			t.Fatalf("iteration %d: gzip.NewReader: %v", i, err)
		}
		got, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("iteration %d: gunzip: %v", i, err)
		}
		if !bytes.Equal(got, big) {
			t.Fatalf("iteration %d: decoded %d bytes, want %d matching bytes", i, len(got), len(big))
		}
	}
}

// Fix round 2: if the handler itself panics with http.ErrAbortHandler
// (e.g. it detected a client disconnect and aborted deliberately),
// gzipJSON still finishes the gzip stream and returns the writer to the
// pool, but re-panics the value unchanged and does not log it as an
// error: it's a deliberate abort, not a bug.
func TestGzipJSONPassesThroughErrAbortHandlerWithoutLogging(t *testing.T) {
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, nil))
	big := bytes.Repeat([]byte("a"), 2000)
	h := recoverer(log, gzipJSON(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(big) // crosses gzipMinBytes: compression has started
		panic(http.ErrAbortHandler)
	})))
	srv := httptest.NewServer(h)
	defer srv.Close()

	res, err := http.Get(srv.URL)
	if err == nil {
		defer res.Body.Close()
		if _, err := io.ReadAll(res.Body); err == nil {
			t.Fatal("expected the connection to abort, got a complete response")
		}
	}

	if logged.Len() != 0 {
		t.Errorf("http.ErrAbortHandler was logged as a panic: %s", logged.String())
	}
}
