// Package webui holds the built web UI (ui/dist) for the HTTP server.
//
// The assets are embedded only when building with -tags embedui after
// `make ui`, which copies ui/dist here. Without the tag the server still
// runs its API, and pages answer with a hint to build the UI.
package webui

import "strings"

var contentTypes = []struct{ ext, typ string }{
	{".html", "text/html; charset=utf-8"},
	{".js", "application/javascript; charset=utf-8"},
	{".css", "text/css; charset=utf-8"},
	{".json", "application/json; charset=utf-8"},
	{".svg", "image/svg+xml"},
	{".png", "image/png"},
	{".ico", "image/x-icon"},
}

// ContentType is the type scripts/embed_ui.ts assigned to a path.
func ContentType(path string) string {
	for _, c := range contentTypes {
		if strings.HasSuffix(path, c.ext) {
			return c.typ
		}
	}
	return "application/octet-stream"
}
