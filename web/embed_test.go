//go:build webui

package web

import (
	"net/http"
	"strings"
	"testing"
)

// The embedded build carries the favicon at both paths a browser asks for:
// the SVG app.html links, and /favicon.ico, which browsers fetch on their
// own and which would otherwise 404.
func TestEmbeddedFavicon(t *testing.T) {
	// .ico's type comes from the system's MIME table (image/x-icon or
	// image/vnd.microsoft.icon) or, without one, from sniffing (x-icon).
	for path, ctype := range map[string]string{
		"/favicon.svg": "image/svg+xml",
		"/favicon.ico": "image/",
	} {
		res := do(t, Handler(), http.MethodGet, path)
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, res.StatusCode)
		}
		if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, ctype) {
			t.Errorf("%s: Content-Type = %q, want %s", path, ct, ctype)
		}
		checkSecurityHeaders(t, res)
	}
}
