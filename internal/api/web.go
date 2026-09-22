package api

import "embed"

//go:embed web/index.html web/app.js web/styles.css
var webFiles embed.FS

var (
	webIndex = mustWebFile("web/index.html")
	webJS    = mustWebFile("web/app.js")
	webCSS   = mustWebFile("web/styles.css")
)

func mustWebFile(name string) []byte {
	data, err := webFiles.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return data
}
