package web

import "embed"

// Assets are embedded into the binary; no frontend runtime or CDN is needed.
//
//go:embed index.html app.css app.js
var Assets embed.FS
