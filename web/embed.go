package webui

import "embed"

// Files contains both the reusable designer library and the MVP shell.
//
//go:embed index.html app.js styles.css funroute-designer.js funroute-source.js funroute-designer.css
var Files embed.FS
