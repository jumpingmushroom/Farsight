//go:build !webui

package web

import (
	"io"
	"net/http"
)

// Built reports whether the UI is compiled into this binary.
const Built = false

const placeholderHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Farsight</title></head>
<body>
<p>Farsight UI not built. Build with <code>make web farsight-ui</code>.</p>
</body>
</html>
`

// Handler serves a placeholder page explaining how to build the UI.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		setSecurityHeaders(h)
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			io.WriteString(w, placeholderHTML)
		}
	})
}
