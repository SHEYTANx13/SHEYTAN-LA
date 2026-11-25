module github.com/SHEYTANx13/SHEYTAN-LA

go 1.26.0

// One formatter everywhere (agent, CI, contributors): GOTOOLCHAIN=auto now
// converges on this exact Go release, whose gofmt is also the one the
// preflight gate runs. Keep "go" as the minimum language version.
toolchain go1.27.4

require (
	github.com/anthonynsimon/bild v0.17.1
	github.com/chromedp/cdproto v0.160.8
	github.com/chromedp/chromedp v0.24.0
	github.com/go-gui-org/go-gui v0.99.1
	github.com/gogpu/gogpu v0.54.1
	github.com/gorilla/websocket v1.5.3
	github.com/tc-hib/winres v0.3.1
	golang.org/x/image v0.48.0
	golang.org/x/mobile v0.0.0-20261117202452-47699113848a
	golang.org/x/net v0.63.0
	golang.org/x/sys v0.50.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/coder/websocket v1.8.14 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	golang.org/x/exp/shiny v0.0.0-20261113113407-e3970619a1a4 // indirect
	golang.org/x/term v0.48.0 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
)

require (
	github.com/alecthomas/chroma/v2 v2.27.0 // indirect
	github.com/dlclark/regexp2/v2 v2.13.2 // indirect
	github.com/ebitengine/purego v0.11.3 // indirect
	github.com/go-gui-org/go-glyph v1.29.2 // indirect
	github.com/go-json-experiment/json v0.0.0-20260623181947-01eb4420fa68 // indirect
	github.com/go-pdf/fpdf v0.9.0 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/go-webgpu/goffi v0.6.8 // indirect
	github.com/go-webgpu/webgpu v0.5.5 // indirect
	github.com/godbus/dbus/v5 v5.2.4 // indirect
	github.com/gogpu/gpucontext v0.31.3 // indirect
	github.com/gogpu/gputypes v0.8.2 // indirect
	github.com/gogpu/naga v0.21.4 // indirect
	github.com/gogpu/wgpu v0.34.9 // indirect
	github.com/jezek/xgb v1.3.1 // indirect
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	github.com/tdewolff/parse/v2 v2.25.0 // indirect
	github.com/wailsapp/wails/v3 v3.1.0
	github.com/yuin/goldmark v1.8.7 // indirect
	github.com/yuin/goldmark-emoji v1.0.6 // indirect
	golang.org/x/text v0.44.0 // indirect
)
