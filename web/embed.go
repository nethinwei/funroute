package webui

import "embed"

// Files contains both the reusable designer library and the MVP shell.
//
//go:embed index.html app.js styles.css funroute-core.js funroute-designer.js funroute-semantic.js funroute-source.js funroute-contract.js funroute-examples.js funroute-designer.css
var Files embed.FS
