package webworkspace

import "embed"

// The Workspace page is fully offline: three embedded files, zero CDN,
// zero remote fonts, zero telemetry. The strict CSP would block remote
// loads anyway — the offline posture is the design, not a fallback.
//
//go:embed page.html app.js app.css
var files embed.FS

var (
	pageHTML = mustFile("page.html")
	appJS    = mustFile("app.js")
	appCSS   = mustFile("app.css")
)

func mustFile(name string) string {
	b, err := files.ReadFile(name)
	if err != nil {
		panic("webworkspace: embedded asset missing: " + name + ": " + err.Error())
	}
	return string(b)
}
