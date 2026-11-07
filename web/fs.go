package web

import (
	"embed"
	"io/fs"
	"os"
)

//go:embed index.html login.html app.js styles.css lemontube.svg
var embedded embed.FS

// Assets returns live files in debug mode and embedded assets otherwise.
func Assets() fs.FS {
	if os.Getenv("DEBUG") == "1" {
		return os.DirFS("web")
	}
	return embedded
}
