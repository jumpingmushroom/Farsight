package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func do(t *testing.T, h http.Handler, method, target string) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec.Result()
}

func body(t *testing.T, res *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func checkSecurityHeaders(t *testing.T, res *http.Response) {
	t.Helper()
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"X-Frame-Options":        "DENY",
		// frame-ancestors is ignored in a <meta> CSP, so it is a header.
		"Content-Security-Policy": "frame-ancestors 'none'",
	} {
		if got := res.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestPlaceholderHandler(t *testing.T) {
	if Built {
		t.Skip("built with -tags webui")
	}
	res := do(t, Handler(), http.MethodGet, "/")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if b := body(t, res); !strings.Contains(b, "Farsight UI not built") {
		t.Errorf("body = %q, want it to mention Farsight UI not built", b)
	}
	checkSecurityHeaders(t, res)

	// Any path gets the placeholder.
	res = do(t, Handler(), http.MethodGet, "/some/deep/link")
	if res.StatusCode != http.StatusOK {
		t.Errorf("deep link status = %d, want 200", res.StatusCode)
	}
	checkSecurityHeaders(t, res)
}

const indexHTML = "<!doctype html><html><body><div>farsight index</div></body></html>"

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte(indexHTML)},
		"favicon.png":          {Data: []byte("\x89PNG\r\n\x1a\nfake")},
		"_app/immutable/x.js":  {Data: []byte("console.log(1)")},
		"_app/version.json":    {Data: []byte(`{"version":"1"}`)},
		"_app/immutable/dir/a": {Data: []byte("a")},
	}
}

func TestNewHandler(t *testing.T) {
	h := newHandler(testFS())

	tests := []struct {
		name, method, path string
		status             int
		cache              string // "" = don't check
		ctype              string // prefix; "" = don't check
		bodyHas            string
	}{
		{"root", "GET", "/", 200, "no-cache", "text/html", "farsight index"},
		{"index direct", "GET", "/index.html", 200, "no-cache", "text/html", "farsight index"},
		{"immutable js", "GET", "/_app/immutable/x.js", 200, "public, max-age=31536000, immutable", "text/javascript", "console.log(1)"},
		{"favicon", "GET", "/favicon.png", 200, "no-cache", "image/png", ""},
		{"version json", "GET", "/_app/version.json", 200, "no-cache", "application/json", ""},
		{"deep link", "GET", "/some/deep/link", 200, "no-cache", "text/html", "farsight index"},
		{"directory", "GET", "/_app/immutable/dir", 200, "no-cache", "text/html", "farsight index"},
		{"missing with ext", "GET", "/missing.js", 404, "", "", ""},
		{"head", "HEAD", "/", 200, "no-cache", "text/html", ""},
		{"head asset", "HEAD", "/_app/immutable/x.js", 200, "public, max-age=31536000, immutable", "text/javascript", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := do(t, h, tc.method, tc.path)
			checkSecurityHeaders(t, res)
			if res.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", res.StatusCode, tc.status)
			}
			if tc.cache != "" {
				if got := res.Header.Get("Cache-Control"); got != tc.cache {
					t.Errorf("Cache-Control = %q, want %q", got, tc.cache)
				}
			}
			if tc.ctype != "" {
				if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, tc.ctype) {
					t.Errorf("Content-Type = %q, want prefix %q", got, tc.ctype)
				}
			}
			b := body(t, res)
			if tc.method == "HEAD" && b != "" {
				t.Errorf("HEAD body = %q, want empty", b)
			}
			if tc.bodyHas != "" && !strings.Contains(b, tc.bodyHas) {
				t.Errorf("body = %q, want it to contain %q", b, tc.bodyHas)
			}
		})
	}
}

func TestNewHandlerTraversal(t *testing.T) {
	h := newHandler(testFS())
	// Set URL.Path directly: httptest.NewRequest would reject or clean these.
	for _, p := range []string{"/../etc/passwd", "../../etc/passwd", "/_app/../../etc/passwd", "/_app/immutable/../../../etc/passwd.js"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = p
		h.ServeHTTP(rec, req)
		res := rec.Result()
		b := body(t, res)
		if strings.Contains(b, "root:") {
			t.Fatalf("%s escaped the fs: %q", p, b)
		}
		if res.StatusCode != 404 && !(res.StatusCode == 200 && strings.Contains(b, "farsight index")) {
			t.Errorf("%s: status %d body %q, want 404 or index", p, res.StatusCode, b)
		}
	}
}

func TestNewHandlerMethods(t *testing.T) {
	h := newHandler(testFS())
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		res := do(t, h, m, "/")
		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s status = %d, want 405", m, res.StatusCode)
		}
		if a := res.Header.Get("Allow"); a != "GET, HEAD" {
			t.Errorf("%s Allow = %q", m, a)
		}
		checkSecurityHeaders(t, res)
	}
}
