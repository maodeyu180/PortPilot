package web

import "embed"

// Assets contains the production UI. Run npm run build in web/ before building Go.
//
//go:embed all:dist
var Assets embed.FS
