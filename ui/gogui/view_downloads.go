//go:build !headless

// view_downloads.go — the Downloads view (area 4): the engine download
// progress surface (core-owned /api/engine download envelope), the model
// import progress, and the engine-update download progress (the Models
// panel's install poll reads the same truth). Every progress line renders
// ONLY what the core reports — real phases, no fabricated progress.
package goguiui

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/go-gui-org/go-gui/gui"
)

// downloadsViewState is the Downloads surface's state.
type downloadsViewState struct {
	st *shellState

	mu        sync.Mutex
	engineDl  *DownloadProgress
	engineMsg string
	updDl     *DownloadProgress
	updLine   string
	importMsg string
	err       error
	loaded    bool
}

func downloadsState(s *shellState) *downloadsViewState {
	return s.view("downloads", func() any { return &downloadsViewState{st: s} }).(*downloadsViewState)
}

func buildDownloadsView(s *shellState) gui.View {
	v := downloadsState(s)
	v.ensureLoaded()

	return gui.Column(gui.ContainerCfg{
		ID:     absID("view-downloads"),
		Sizing: gui.FillFill,
		Content: []gui.View{
			PageHeader("Downloads", "Engine and model transfer progress — core-reported phases only"),
			v.errView(),
			gui.Column(gui.ContainerCfg{
				ID:         absID("downloads-scroll"),
				Sizing:     gui.FillFill,
				Scrollable: true,
				Spacing:    gui.SpacingPx(6),
				Content: []gui.View{
					v.engineDownloadCard(),
					v.updateDownloadCard(),
					v.importCard(),
				},
			}),
		},
	})
}

func (v *downloadsViewState) ensureLoaded() {
	v.mu.Lock()
	if v.loaded {
		v.mu.Unlock()
		return
	}
	v.loaded = true
	v.mu.Unlock()
	v.refresh()
}

func (v *downloadsViewState) errView() gui.View {
	v.mu.Lock()
	err := v.err
	v.mu.Unlock()
	if err == nil {
		return nil
	}
	return ErrorSurface("downloads", err, nil)
}

func (v *downloadsViewState) refresh() {
	ap := v.st.ap
	go func() {
		var engineDl, updDl *DownloadProgress
		var engineMsg, updLine, importMsg string
		var err error

		if e, eerr := ap.Client().Engine(ap.Ctx()); eerr != nil {
			err = eerr
		} else {
			if len(e.Download) > 0 && string(e.Download) != "null" {
				var dl struct {
					Phase    string  `json:"phase"`
					Bytes    int64   `json:"bytes"`
					Total    int64   `json:"total"`
					Fraction float64 `json:"fraction"`
					Error    string  `json:"error"`
				}
				if json.Unmarshal(e.Download, &dl) == nil {
					if dl.Error != "" {
						engineMsg = "Engine download failed: " + dl.Error
					} else if dl.Phase != "" {
						engineDl = &DownloadProgress{Phase: dl.Phase, Bytes: dl.Bytes, Total: dl.Total, Fraction: dl.Fraction}
					}
				}
			}
			// The engine-update transaction rides the same check surface.
			if u, uerr := ap.Client().EngineUpdateCheckNow(ap.Ctx()); uerr == nil && u != nil {
				updLine = fmt.Sprintf("Engine update: %s", u.Status)
				if u.Message != "" {
					updLine += " — " + u.Message
				}
				updDl = u.Progress
			}
		}

		uiDo(func() {
			v.mu.Lock()
			v.engineDl = engineDl
			v.updDl = updDl
			v.engineMsg = engineMsg
			v.updLine = updLine
			v.importMsg = importMsg
			v.err = err
			v.mu.Unlock()
		})
	}()
}

// progressRow renders one honest progress bar (fraction from the core).
func progressRow(id, label string, p *DownloadProgress) gui.View {
	if p == nil {
		return wrappedText(label + ": no transfer in progress")
	}
	pct := float32(p.Fraction)
	if pct == 0 && p.Total > 0 {
		pct = float32(p.Bytes) / float32(p.Total)
	}
	if pct <= 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Spacing: gui.SpacingPx(4),
		Content: []gui.View{
			gui.Text(gui.TextCfg{Text: fmt.Sprintf("%s — phase %s (%s / %s)", label, orDash(p.Phase), humanBytes(p.Bytes), humanBytes(p.Total)), TextStyle: gui.CurrentTheme().TextStyleCaption}),
			gui.ProgressBar(gui.ProgressBarCfg{ID: absID(id), Percent: pct, Sizing: gui.FillFit, TextShow: true}),
		},
	})
}

func (v *downloadsViewState) engineDownloadCard() gui.View {
	v.mu.Lock()
	dl := v.engineDl
	msg := v.engineMsg
	v.mu.Unlock()
	rows := []gui.View{progressRow("downloads-engine", "Engine download", dl)}
	if msg != "" {
		rows = append(rows, wrappedText(msg))
	}
	rows = append(rows, gui.Button(gui.ButtonCfg{
		ID: absID("downloads-engine-cancel"), Label: "Stop download",
		OnClick: func(gui.EventCtx) {
			go func() {
				if err := v.st.ap.Client().CancelEngineDownload(v.st.ap.Ctx()); err != nil {
					v.fail("cancel engine download", err)
				}
				v.refresh()
			}()
		},
	}))
	return card("Engine", "The core owns the transfer; this surface only mirrors it", rows...)
}

func (v *downloadsViewState) updateDownloadCard() gui.View {
	v.mu.Lock()
	dl := v.updDl
	line := v.updLine
	v.mu.Unlock()
	rows := []gui.View{}
	if line != "" {
		rows = append(rows, wrappedText(line))
	}
	rows = append(rows, progressRow("downloads-update", "Engine update download", dl))
	return card("Engine updates", "User-controlled install through the core's engine-update API", rows...)
}

func (v *downloadsViewState) importCard() gui.View {
	return card("Model import", "Imports run through the core's import API; progress appears in Models",
		wrappedText("Model imports are posted from the Models surface (Import). This card is the honest pointer to that flow — the core reports import progress through the Models payload, never fabricated here."),
	)
}

func (v *downloadsViewState) fail(what string, err error) {
	uiDo(func() {
		v.mu.Lock()
		v.err = fmt.Errorf("%s: %w", what, err)
		v.mu.Unlock()
	})
}

var _ = strings.TrimSpace // import guard
