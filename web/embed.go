//go:build webui

package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:build
var build embed.FS

// Built reports whether the UI is compiled into this binary.
const Built = true

// Handler serves the embedded web UI.
func Handler() http.Handler {
	sub, err := fs.Sub(build, "build")
	if err != nil {
		panic(err)
	}
	return newHandler(sub)
}
