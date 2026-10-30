package workspace

import "embed"

// Assets are bundled at build time — offline by construction, no CDN, no
// remote JS, no telemetry. errAlreadyRunning mirrors the studio viewer.
//
//go:embed view/index.html view/workspace.js view/workspace.css
var viewFS embed.FS

var (
	pageHTML = mustRead("view/index.html")
	pageJS   = mustRead("view/workspace.js")
	pageCSS  = mustRead("view/workspace.css")
)

func mustRead(name string) string {
	b, err := viewFS.ReadFile(name)
	if err != nil {
		// Embedded at build time; a miss is a programming error, not a
		// runtime condition. Fail loudly at process start, not per request.
		panic("workspace: embedded asset missing: " + name + ": " + err.Error())
	}
	return string(b)
}
