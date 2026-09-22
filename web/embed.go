package webui

import "embed"

// Files contains both the reusable designer library and the MVP shell.
//
//go:embed index.html app.js styles.css funroute-core.js funroute-workspace.js funroute-display.js funroute-fields.js funroute-dnd.js funroute-palette.js funroute-reference.js funroute-designer.js funroute-contract.js funroute-examples.js funroute-examples.json funroute-designer.css
var Files embed.FS
