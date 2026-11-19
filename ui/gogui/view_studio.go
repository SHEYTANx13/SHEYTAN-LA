//go:build !headless

// view_studio.go — the optional 3D Studio entry point (ported from
// ui/fyne/view_studio.go).
//
// This view is a CONTROL SURFACE only: it starts/stops the core-owned
// loopback viewer and reports its honest state. The rendering happens in
// the user's browser (the viewer URL opens the default browser through
// the native platform OpenURI); the go-gui window never embeds a web
// runtime. Everything the page needs (Three.js r182, MIT) is embedded in
// the Go binary and served offline over 127.0.0.1.
package goguiui

import (
	"net/url"
	"sync"

	"github.com/go-gui-org/go-gui/gui"
)

// studioViewState is the Studio control surface's state.
type studioViewState struct {
	st *shellState

	mu      sync.Mutex
	status  string
	detail  string
	disable string // why the actions are unavailable ("" = available)
	running bool
	loaded  bool
}

func studioState(s *shellState) *studioViewState {
	return s.view("studio", func() any { return &studioViewState{st: s} }).(*studioViewState)
}

func buildStudioView(s *shellState) gui.View {
	v := studioState(s)
	v.refresh()

	v.mu.Lock()
	status, detail, disable := v.status, v.detail, v.disable
	v.mu.Unlock()

	openEnabled := disable == "" && !v.isRunning()
	stopEnabled := disable == "" && v.isRunning()

	actions := []gui.View{
		gui.Button(gui.ButtonCfg{
			ID:       absID("studio-open"),
			Label:    "Open Studio",
			Disabled: !openEnabled,
			OnClick:  func(gui.EventCtx) { v.open() },
		}),
		gui.Button(gui.ButtonCfg{
			ID:       absID("studio-stop"),
			Label:    "Stop",
			Disabled: !stopEnabled,
			OnClick:  func(gui.EventCtx) { v.stopViewer() },
		}),
		gui.Button(gui.ButtonCfg{
			ID:       absID("studio-copy"),
			Label:    "Copy URL",
			Disabled: !stopEnabled,
			OnClick:  func(gui.EventCtx) { v.copyURL() },
		}),
	}

	return gui.Column(gui.ContainerCfg{
		ID:     absID("view-studio"),
		Sizing: gui.FillFill,
		Content: []gui.View{
			PageHeader("3D Studio", "Read-only system-topology viewer (core-owned)"),
			card("About", "",
				wrappedText("A read-only 3D map of your live system topology — host hardware, inference engine, governor, sessions and tool registry — rendered with a locally embedded three.js release. The viewer cannot execute tools or change any setting."),
			),
			card("Controls", "", append(actions,
				gui.Text(gui.TextCfg{Text: "Open Studio launches the read-only viewer in your web browser.", TextStyle: gui.CurrentTheme().TextStyleCaption}),
			)...),
			card("State", "", func() []gui.View {
				out := []gui.View{}
				if status != "" {
					out = append(out, wrappedText(status))
				} else {
					out = append(out, LoadingSurface("studio state"))
				}
				if detail != "" {
					out = append(out, codeText(detail))
				}
				if disable != "" {
					out = append(out, wrappedText(disable))
				}
				return out
			}()...),
		},
		Scrollable: true,
	})
}

func (v *studioViewState) isRunning() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.running
}

func (v *studioViewState) refresh() {
	ap := v.st.ap
	go func() {
		st, err := ap.Client().StudioState(ap.Ctx())
		uiDo(func() {
			v.mu.Lock()
			defer v.mu.Unlock()
			v.loaded = true
			if err != nil {
				v.status = "The core did not answer the studio state request."
				v.detail = err.Error()
				v.disable = "Actions unavailable until the core answers."
				return
			}
			if !st.Enabled {
				v.status = "The 3D Studio is disabled in Settings."
				v.detail = st.Reason
				v.disable = "Enable it under Settings → General → “3D Studio”, then come back here."
				return
			}
			v.disable = ""
			v.running = st.Running
			if st.Running {
				v.status = "Viewer running — the topology renders in your browser at " + st.URL
				v.detail = "renderer: three.js " + st.Version + " (" + st.License + ") · served offline from the application binary · requests: " + itoa64(st.Requests)
			} else {
				v.status = "Viewer stopped. Opening the studio starts a read-only local viewer (127.0.0.1, ephemeral port) and your default browser."
				v.detail = "renderer: three.js " + st.Version + " (" + st.License + ") · embedded · no network fetch"
			}
		})
	}()
}

func (v *studioViewState) open() {
	ap := v.st.ap
	uiDo(func() {
		v.mu.Lock()
		v.status = "Starting the viewer…"
		v.mu.Unlock()
	})
	go func() {
		launch, err := ap.Client().StudioStart(ap.Ctx())
		uiDo(func() {
			if err != nil {
				v.mu.Lock()
				v.status = "The viewer could not start."
				v.detail = err.Error()
				v.mu.Unlock()
				v.refresh()
				return
			}
			// The ONLY consumer of the tokened URL: the user's default
			// browser. Never logged, never stored in UI state.
			if u, perr := url.Parse(launch.URL); perr == nil {
				_ = openURI(u.String())
			}
			v.refresh()
		})
	}()
}

func (v *studioViewState) stopViewer() {
	ap := v.st.ap
	go func() {
		err := ap.Client().StudioStop(ap.Ctx())
		uiDo(func() {
			if err != nil {
				v.mu.Lock()
				v.detail = err.Error()
				v.mu.Unlock()
			}
			v.refresh()
		})
	}()
}

// copyURL copies the honest notice (the tokened URL is returned ONCE per
// launch by design and never stored — the clipboard copy says so instead
// of pretending we still hold the token).
func (v *studioViewState) copyURL() {
	ap := v.st.ap
	go func() {
		st, err := ap.Client().StudioState(ap.Ctx())
		uiDo(func() {
			if err != nil || !st.Running || st.URL == "" {
				return
			}
			w, _ := uiDoWindow.Load().(*gui.Window)
			if w != nil {
				w.SetClipboard(st.URL + "  (open the studio from this app for the tokened link)")
			}
		})
	}()
}

// itoa64 formats an int64 without importing strconv for one call site.
func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
