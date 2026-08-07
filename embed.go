package main

import "embed"

//go:embed internal/templates static/dist
var embeddedAssets embed.FS
