//go:build !embedui

package webui

// Get returns the asset at an absolute URL path. Without the embedui build
// tag there are none.
func Get(path string) ([]byte, bool) { return nil, false }

// Embedded reports whether the UI is built in.
const Embedded = false
