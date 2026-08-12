//go:build embedded_ui

package webui

import (
	"embed"
	"io/fs"
)

//go:embed dist
var assets embed.FS

func assetsFS() fs.FS {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return dist
}
