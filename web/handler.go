// Package web serves the Farsight web UI: a SvelteKit static build that is
// embedded into the binary with -tags webui, or a "not built" placeholder.
package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// setSecurityHeaders sets the headers every UI response carries. The main
// CSP is a <meta> tag emitted by SvelteKit (kit.csp) with script hashes;
// browsers ignore frame-ancestors there, so it is sent as a header policy
// of its own (both policies apply). X-Frame-Options covers old browsers.
func setSecurityHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "frame-ancestors 'none'")
}

// newHandler serves a SvelteKit static build from fsys: real files as-is,
// extension-less paths fall back to index.html (SPA), immutable assets are
// cached for a year, everything else must revalidate.
func newHandler(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		setSecurityHeaders(h)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if st, err := fs.Stat(fsys, p); err != nil || st.IsDir() {
			if path.Ext(p) != "" {
				http.NotFound(w, r)
				return
			}
			p = "index.html"
		}
		if strings.HasPrefix(p, "_app/immutable/") {
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "no-cache")
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + p
		r2.URL.RawPath = ""
		if p == "index.html" {
			r2.URL.Path = "/" // FileServer redirects /index.html → /
		}
		files.ServeHTTP(w, r2)
	})
}
