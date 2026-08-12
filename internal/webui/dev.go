//go:build !embedded_ui

package webui

import (
	"io/fs"
	"testing/fstest"
)

func assetsFS() fs.FS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(`<!doctype html>
<html lang="zh-CN">
<head><meta charset="UTF-8"><title>模汇</title></head>
<body>后端已启动。请在 web 目录运行 pnpm dev 启动管理后台。</body>
</html>
`)},
	}
}
