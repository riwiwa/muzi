package main

import (
	"embed"
	"io/fs"
)

// Page templates and static files are built into the binary so muzi runs from any directory.
// static/uploads is left out on purpose: uploads are written at runtime and served from disk
// (storage.uploads_dir).

//go:embed templates/*.gohtml
var embeddedTemplates embed.FS

//go:embed static/app.js static/import.js static/style.css static/assets
var embeddedStatic embed.FS

func assetFS() (templates, static fs.FS) {
	templates, err := fs.Sub(embeddedTemplates, "templates")
	if err != nil {
		panic(err)
	}
	static, err = fs.Sub(embeddedStatic, "static")
	if err != nil {
		panic(err)
	}
	return templates, static
}
