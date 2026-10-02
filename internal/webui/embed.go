//go:build embedui

package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Get returns the asset at an absolute URL path like "/index.html".
func Get(path string) ([]byte, bool) {
	b, err := fs.ReadFile(dist, "dist"+path)
	if err != nil {
		return nil, false
	}
	return b, true
}

// Embedded reports whether the UI is built in.
const Embedded = true
