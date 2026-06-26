// Package static embeds the CivicSync landing page assets into the binary.
package static

import "embed"

// StaticFiles holds the embedded web/static directory contents (index.html and styles.css).
//
//go:embed index.html styles.css
var StaticFiles embed.FS
