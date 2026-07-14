// Package webui embeds the local search and content reader interface.
package webui

import "embed"

// Assets contains the build-free local web interface.
//
//go:embed index.html app.css app.js
var Assets embed.FS
