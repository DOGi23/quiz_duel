package web

import "embed"

//go:embed index.html app.js style.css hackathon.css vendor brand
var Files embed.FS
