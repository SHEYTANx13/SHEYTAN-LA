//go:build !headless

// view_studio.go — the optional 3D Studio entry point (v1.11.0).
//
// This view is a CONTROL SURFACE only: it starts/stops the core-owned
// loopback viewer and reports its honest state. The rendering itself
// happens in the user's browser (the viewer URL opens the default
// browser); the Fyne window never embeds a web runtime — the embedded
// webview adapter was NOT verified on both desktop targets in this
// timebox, so the browser-hosted viewer is the shipped, honest slice.
// Everything the page needs (Three.js r182, MIT) is embedded in the Go
// binary and served offline over 127.0.0.1.
package fyneui

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// buildStudioView assembles the 3D Studio control view.
func buildStudioView(ap *App) fyne.CanvasObject {
	status := widget.NewLabel("Checking the 3D Studio state…")
	status.Wrapping = fyne.TextWrapWord

	detail := widget.NewLabel("")
	detail.Wrapping = fyne.TextWrapWord
	detail.TextStyle = fyne.TextStyle{Monospace: true}

	openBtn := widget.NewButton("Open 3D Studio (browser)", nil)
	stopBtn := widget.NewButton("Stop viewer", nil)
	copyBtn := widget.NewButton("Copy viewer URL", nil)
	disabledNote := widget.NewLabel("")

	openBtn.Importance = widget.HighImportance
	stopBtn.Importance = widget.MediumImportance

	refresh := func() {
		go func() {
			st, err := ap.client.StudioState(ap.Ctx())
			uiDo(func() {
				if err != nil {
					status.Text = "The core did not answer the studio state request."
					detail.Text = err.Error()
					openBtn.Disable()
					stopBtn.Disable()
					copyBtn.Disable()
					status.Refresh()
					detail.Refresh()
					return
				}
				if !st.Enabled {
					status.Text = "The 3D Studio is disabled in Settings."
					detail.Text = st.Reason
					disabledNote.Text = "Enable it under Settings → General → “3D Studio”, then come back here."
					openBtn.Disable()
					stopBtn.Disable()
					copyBtn.Disable()
					status.Refresh()
					detail.Refresh()
					disabledNote.Refresh()
					return
				}
				disabledNote.Text = ""
				disabledNote.Refresh()
				if st.Running {
					status.Text = "Viewer running — the topology renders in your browser at " + st.URL
					detail.Text = "renderer: three.js " + st.Version + " (" + st.License + ") · served offline from the application binary · requests: " + itoa64(st.Requests)
					openBtn.Enable()
					stopBtn.Enable()
					copyBtn.Enable()
				} else {
					status.Text = "Viewer stopped. Opening the studio starts a read-only local viewer (127.0.0.1, ephemeral port) and your default browser."
					detail.Text = "renderer: three.js " + st.Version + " (" + st.License + ") · embedded · no network fetch"
					openBtn.Enable()
					stopBtn.Disable()
					copyBtn.Disable()
				}
				status.Refresh()
				detail.Refresh()
			})
		}()
	}

	openBtn.OnTapped = func() {
		openBtn.Disable()
		status.Text = "Starting the viewer…"
		status.Refresh()
		go func() {
			launch, err := ap.client.StudioStart(ap.Ctx())
			uiDo(func() {
				if err != nil {
					status.Text = "The viewer could not start."
					detail.Text = err.Error()
					detail.Refresh()
					status.Refresh()
					refresh()
					return
				}
				// The ONLY consumer of the tokened URL: the user's default
				// browser. Never logged, never stored in UI state.
				if u, perr := url.Parse(launch.URL); perr == nil {
					ap.a.OpenURL(u)
				}
				refresh()
			})
		}()
	}

	stopBtn.OnTapped = func() {
		stopBtn.Disable()
		go func() {
			err := ap.client.StudioStop(ap.Ctx())
			uiDo(func() {
				if err != nil {
					detail.Text = err.Error()
					detail.Refresh()
				}
				refresh()
			})
		}()
	}

	copyBtn.OnTapped = func() {
		// Off the UI thread: the state call is a bounded core request and
		// must never block the event loop (threading contract).
		go func() {
			st, err := ap.client.StudioState(ap.Ctx())
			uiDo(func() {
				if err != nil || !st.Running || st.URL == "" {
					return
				}
				// The tokened URL is returned ONCE per launch by design and
				// never stored — the clipboard copy honestly says so instead
				// of pretending we still hold the token.
				ap.w.Clipboard().SetContent(st.URL + "  (open the studio from this app for the tokened link)")
			})
		}()
	}

	refresh()
	ap.shell.register(nil, nil, nil) // no stream/session wiring: the studio is independent of runs

	return container.NewVBox(
		widget.NewLabelWithStyle("3D Studio", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("A read-only 3D map of your live system topology — host hardware, inference engine, governor, sessions and tool registry — rendered with a locally embedded three.js release. The viewer cannot execute tools or change any setting."),
		container.NewHBox(openBtn, stopBtn, copyBtn),
		status,
		detail,
		disabledNote,
	)
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
