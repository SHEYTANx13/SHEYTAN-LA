//go:build !headless

// view_webws.go — the Web Workspace control view (v1.11.0 hybrid UI).
//
// CONTROL SURFACE ONLY, exactly like the 3D Studio view: it starts and
// stops the core-owned loopback host, publishes the current answer's
// typed blocks into the ONE presentation registry, and reports honest
// state. The rendering happens in the user's browser; the Fyne window
// never embeds a web runtime (documented decision — see
// docs/3dstudio.md for the same reasoning). Every button issues a typed
// operation through the authenticated local IPC channel; nothing here
// bypasses the core's authorities.
package fyneui

import (
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// buildWebWorkspaceView assembles the Workspace control view.
func buildWebWorkspaceView(ap *App) fyne.CanvasObject {
	status := widget.NewLabel("Checking the Web Workspace state…")
	status.Wrapping = fyne.TextWrapWord

	detail := widget.NewLabel("")
	detail.Wrapping = fyne.TextWrapWord
	detail.TextStyle = fyne.TextStyle{Monospace: true}

	openBtn := widget.NewButton("Open Workspace (browser)", nil)
	stopBtn := widget.NewButton("Stop host", nil)
	publishBtn := widget.NewButton("Publish current answer", nil)
	disabledNote := widget.NewLabel("")

	openBtn.Importance = widget.HighImportance
	publishBtn.Importance = widget.MediumImportance

	refresh := func() {
		go func() {
			st, err := ap.client.WorkspaceState(ap.Ctx())
			uiDo(func() {
				if err != nil {
					status.Text = "The core did not answer the workspace state request."
					detail.Text = err.Error()
					openBtn.Disable()
					stopBtn.Disable()
					status.Refresh()
					detail.Refresh()
					return
				}
				if !st.Enabled {
					status.Text = "The Web Workspace is disabled in Settings."
					detail.Text = st.Reason
					disabledNote.Text = "Enable it under Settings → General → “Web Workspace”, then come back here."
					openBtn.Disable()
					stopBtn.Disable()
					publishBtn.Disable()
					status.Refresh()
					detail.Refresh()
					disabledNote.Refresh()
					return
				}
				disabledNote.Text = ""
				disabledNote.Refresh()
				if st.Running {
					status.Text = "Workspace host running — the typed blocks render in your browser at " + st.URL
					detail.Text = "blocks published: " + itoa64(int64(st.Stats.Published)) +
						" · live: " + itoa64(int64(st.Stats.Live)) +
						" · evicted: " + itoa64(int64(st.Stats.Evicted)) +
						" · requests: " + itoa64(st.Requests)
					openBtn.Enable()
					stopBtn.Enable()
					publishBtn.Enable()
				} else {
					status.Text = "Host stopped. Opening the Workspace starts a read-only local renderer (127.0.0.1, ephemeral port) and your default browser."
					detail.Text = "no host process · nothing renders until content is published through the core"
					openBtn.Enable()
					stopBtn.Disable()
					publishBtn.Enable()
				}
				status.Refresh()
				detail.Refresh()
			})
		}()
	}

	openBtn.OnTapped = func() {
		openBtn.Disable()
		status.Text = "Starting the workspace host…"
		status.Refresh()
		go func() {
			launch, err := ap.client.WorkspaceStart(ap.Ctx())
			uiDo(func() {
				if err != nil {
					status.Text = "The workspace host could not start."
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
			err := ap.client.WorkspaceStop(ap.Ctx())
			uiDo(func() {
				if err != nil {
					detail.Text = err.Error()
					detail.Refresh()
				}
				refresh()
			})
		}()
	}

	publishBtn.OnTapped = func() {
		publishBtn.Disable()
		publishBtn.SetText("Publishing…")
		go func() {
			n := ap.publishCurrentAnswer()
			uiDo(func() {
				publishBtn.SetText("Publish current answer")
				publishBtn.Enable()
				if n <= 0 {
					detail.Text = "Nothing publishable in the current answer (no finished response with content)."
					detail.Refresh()
				} else {
					detail.Text = itoa64(int64(n)) + " typed block(s) published into the shared registry — open the Workspace to render them."
					detail.Refresh()
				}
				refresh()
			})
		}()
	}

	refresh()
	ap.shell.register(nil, nil, nil) // no stream/session wiring: the workspace is independent of runs

	return container.NewVBox(
		widget.NewLabelWithStyle("Web Workspace", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("An on-demand rich-content renderer: Markdown, code, diffs, tables and chart specs from your runs, rendered by your browser with the same security boundary as the 3D Studio (loopback-only, per-launch token, strict CSP, offline assets, zero commands). The native window remains the authority — this surface renders typed blocks the core published, nothing else."),
		container.NewHBox(openBtn, stopBtn, publishBtn),
		status,
		detail,
		disabledNote,
	)
}

// publishCurrentAnswer extracts typed blocks from the settled answer of
// the active chat view and publishes them into the ONE registry. It is
// the vertical-slice producer of the hybrid surface: real run output →
// typed envelope → both renderers. Returns the number of stored blocks
// (0 when there is nothing to publish — an honest no-op, never a fake
// success).
func (ap *App) publishCurrentAnswer() int {
	answer := ap.rt.Stream().Response
	if strings.TrimSpace(answer) == "" {
		return 0
	}
	runID := ap.rt.Stream().RunID
	sessionID := ap.rt.ActiveID()
	blocks := extractBlocksForPublish(answer)
	published := 0
	for _, b := range blocks {
		stored, err := ap.client.WorkspacePublish(ap.Ctx(), b.kind, b.title, b.payload, b.language, runID, sessionID, "chat-answer")
		if err != nil {
			return published
		}
		_ = stored
		published++
	}
	return published
}

// publishBlock is the typed unit handed to the publisher.
type publishBlock struct {
	kind     string
	title    string
	payload  string
	language string
}

// extractBlocksForPublish splits a finished answer into typed publish
// units (fenced code → code blocks; prose → markdown). Deterministic,
// local, no model steering — mirrors internal/presentation.ExtractBlocks
// semantics for the UI side (the core re-validates and degrades).
func extractBlocksForPublish(answer string) []publishBlock {
	var out []publishBlock
	var prose strings.Builder
	lines := strings.Split(answer, "\n")
	flushProse := func() {
		p := strings.TrimSpace(prose.String())
		prose.Reset()
		if p == "" {
			return
		}
		out = append(out, publishBlock{kind: "markdown", title: "Answer", payload: p})
	}
	inCode := false
	lang := ""
	var body strings.Builder
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if !inCode && strings.HasPrefix(t, "```") {
			flushProse()
			inCode = true
			lang = strings.TrimSpace(strings.TrimPrefix(t, "```"))
			body.Reset()
			continue
		}
		if inCode && strings.HasPrefix(t, "```") && strings.TrimSpace(strings.TrimPrefix(t, "```")) == "" {
			out = append(out, publishBlock{kind: "code", title: codeTitle(lang), payload: body.String(), language: lang})
			inCode = false
			lang = ""
			continue
		}
		if inCode {
			body.WriteString(line)
			body.WriteString("\n")
			continue
		}
		prose.WriteString(line)
		prose.WriteString("\n")
	}
	if inCode {
		// Unterminated fence: publish the body honestly as code.
		out = append(out, publishBlock{kind: "code", title: codeTitle(lang), payload: body.String(), language: lang})
	}
	flushProse()
	return out
}

func codeTitle(lang string) string {
	if lang == "" {
		return "Code"
	}
	return "Code (" + lang + ")"
}
