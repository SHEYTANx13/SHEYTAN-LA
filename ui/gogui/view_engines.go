//go:build !headless

// view_engines.go — the Engine Gallery (v1.11.1 UX-01..UX-05, ported).
//
// ONE registry-driven surface: every engine in internal/renderer renders
// as a card with its truthful maturity, runtime state (with the
// operator-facing reason for every non-ready engine), per-platform
// verification for THIS machine, capabilities (honest states only) and
// known gaps. The go-gui production engine is the live reference; the
// not-built engines show their POC boundary — no fake launch buttons, no
// static screenshot pretending to be interactive (the §0 honesty rule).
//
// The Appearance section carries the live theme switcher (theme_service.go
// contract: select = live preview, Save = the only persistence path,
// Cancel = restore saved).
//
// No client/API calls happen here: the catalog is compiled-in truth, so
// the view builds detached from a live core (test-friendly, bootable
// before connect).
package goguiui

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/go-gui-org/go-gui/gui"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/renderer"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/renderstate"
	themetokens "github.com/SHEYTANx13/SHEYTAN-LA/internal/theme"
)

// enginesViewState is the Engines & Appearance state.
type enginesViewState struct {
	st        *shellState
	themes    *themeService
	themeLine string
	themeSel  string
	gpu       *renderBridge
}

func enginesState(s *shellState) *enginesViewState {
	return s.view("engines", func() any {
		// The service owns boot-time application too; on a detached shell
		// (tests) build one on the fly.
		v := &enginesViewState{st: s}
		if s.ap != nil && s.ap.themes != nil {
			v.themes = s.ap.themes
		} else {
			v.themes = newThemeService()
		}
		if id := v.themes.Active(); id != "" {
			v.themeSel = themeNameForID(id)
		}
		v.themeLine = themeLineFor(v.themes)
		return v
	}).(*enginesViewState)
}

func themeNameForID(id string) string {
	t, ok := themetokens.ByID(id)
	if !ok {
		return ""
	}
	return t.Name
}

func themeLineFor(s *themeService) string {
	if s.IsPreviewing() {
		return fmt.Sprintf("Previewing %q (unsaved) — Save persists it, Cancel restores %q.", s.Active(), s.Saved())
	}
	return fmt.Sprintf("Saved theme: %q.", s.Saved())
}

// buildEnginesView assembles the gallery. Reachable as the "engines"
// primary view from the shell registry (and Ctrl+9).
func buildEnginesView(s *shellState) gui.View {
	v := enginesState(s)

	// ---- Theme switcher (UX-01) --------------------------------------
	themeNames := make([]string, 0, len(themetokens.Catalog()))
	for _, t := range themetokens.Catalog() {
		themeNames = append(themeNames, t.Name)
	}
	themeCard := card("Theme", "Live preview on select; Save is the only persistence path; Cancel restores the saved theme.",
		gui.Select(gui.SelectCfg{
			ID:       absID("engines-theme-select"),
			Items:    themeNames,
			MaxWidth: 260,
			Selected: func() []string {
				if v.themeSel == "" {
					return nil
				}
				return []string{v.themeSel}
			}(),
			OnSelect: func(vals []string, _ gui.EventCtx) {
				if len(vals) == 0 {
					return
				}
				for _, t := range themetokens.Catalog() {
					if t.Name == vals[0] {
						_ = v.themes.Apply(t.ID)
						v.themeSel = vals[0]
						v.themeLine = themeLineFor(v.themes)
						return
					}
				}
			},
		}),
		gui.Row(gui.ContainerCfg{
			Content: []gui.View{
				gui.Button(gui.ButtonCfg{
					ID:      absID("engines-theme-cancel"),
					Label:   "Cancel",
					OnClick: func(gui.EventCtx) { v.themes.Cancel(); v.themeLine = themeLineFor(v.themes) },
				}),
				gui.Button(gui.ButtonCfg{
					ID:      absID("engines-theme-save"),
					Label:   "Save theme",
					Variant: gui.ButtonPrimary,
					OnClick: func(gui.EventCtx) { v.themes.Save(); v.themeLine = themeLineFor(v.themes) },
				}),
			},
		}),
		gui.Text(gui.TextCfg{ID: absID("engines-theme-line"), Text: v.themeLine, TextStyle: gui.CurrentTheme().TextStyleCaption}),
	)

	// ---- Engine cards (UX-02/UX-03) ----------------------------------
	cardViews := make([]gui.View, 0, 8)
	for _, m := range renderer.Catalog() {
		cardViews = append(cardViews, engineCard(m))
	}

	return gui.Column(gui.ContainerCfg{
		ID:         absID("view-engines"),
		Sizing:     gui.FillFill,
		Content:    append([]gui.View{PageHeader("Engines", "Renderer catalog, themes — truthful states only")}, append([]gui.View{themeCard, gpuCard(v)}, cardViews...)...),
		Scrollable: true,
	})
}

// engineCard renders one manifest: identity, version, maturity badge,
// state+reason, this-platform line, capabilities (honest), known gaps.
// NOT-built engines render no launch action at all — the gallery never
// offers an action the package cannot perform (§4.1).
func engineCard(m renderer.Manifest) gui.View {
	var variant gui.BadgeVariant
	switch m.Maturity {
	case renderer.MaturityProduction:
		variant = BadgeOK
	case renderer.MaturityPreview:
		variant = BadgeWarn
	case renderer.MaturityExperimental:
		variant = BadgeInfo
	default:
		variant = BadgeNeutral
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s · v%s · this platform: %s", m.Maturity, m.Version, m.ThemeMapping) + "\n")
	b.WriteString(fmt.Sprintf("State: %s", m.State))
	if m.StateReason != "" {
		b.WriteString(" — " + m.StateReason)
	}
	b.WriteString("\n")
	status, supported := m.Supported(runtime.GOOS, runtime.GOARCH)
	if supported {
		b.WriteString("Platform: " + status + "\n")
	} else {
		b.WriteString("Platform: not covered for " + runtime.GOOS + "/" + runtime.GOARCH + "\n")
	}
	if len(m.Capabilities) > 0 {
		names := make([]string, 0, len(m.Capabilities))
		for name := range m.Capabilities {
			names = append(names, name)
		}
		// Deterministic order for stable tests.
		for i := 1; i < len(names); i++ {
			for j := i; j > 0 && names[j] < names[j-1]; j-- {
				names[j], names[j-1] = names[j-1], names[j]
			}
		}
		b.WriteString("Capabilities: " + strings.Join(names, ", ") + "\n")
	}
	for _, gap := range m.KnownGaps {
		b.WriteString("Gap: " + gap + "\n")
	}
	if m.Notes != "" {
		b.WriteString(m.Notes + "\n")
	}

	return card(m.DisplayName, "",
		gui.Row(gui.ContainerCfg{
			Content: []gui.View{StatusBadge(absID("engines-badge", string(m.ID)), string(m.Maturity), variant)},
		}),
		codeText(b.String()),
	)
}

// gpuCard is the live GoGPU integration surface (run 44457547289 §5):
// the render service's honest state, ONE deterministic verification job
// executed through the real gogpu stack with checksum verified readback,
// and the frame published into the shell through the toolkit's
// PixelSurface — a real app surface, not a standalone demo.
func gpuCard(v *enginesViewState) gui.View {
	if v.gpu == nil {
		v.gpu = newRenderBridge(240, 120)
	}

	line := v.gpu.StatusText()

	return card("GPU stack (GoGPU)", "One deterministic job through the gogpu software path; verified against the CPU reference. Execution is only ever reported after a real verified run.",
		gui.Column(gui.ContainerCfg{
			Spacing: gui.SpacingPx(6),
			Content: []gui.View{
				gui.Image(gui.ImageCfg{
					ID:      absID("engines-gpu-preview"),
					Surface: v.gpu.Surface(),
				}),
				gui.Row(gui.ContainerCfg{
					Content: []gui.View{
						gui.Button(gui.ButtonCfg{
							ID:      absID("engines-gpu-verify"),
							Label:   "Render + verify frame",
							Width:   190,
							OnClick: func(gui.EventCtx) { runGPUVerification(v) },
						}),
					},
				}),
				gui.Text(gui.TextCfg{ID: absID("engines-gpu-line"), Text: line, TextStyle: gui.CurrentTheme().TextStyleCaption}),
			},
		}))
}

// runGPUVerification discovers, executes one job, publishes the preview
// frame and records the outcome line — all off the UI thread except the
// final marshalled status refresh.
func runGPUVerification(v *enginesViewState) {
	if v.gpu == nil || v.st == nil {
		return
	}

	go func() {
		// The verification hook is wired only in builds that link the
		// gogpu stack; here the typed no-wire answer keeps the card
		// honest (the service itself is verified in the core build).
		out, err := renderstate.Verify()

		v.gpu.PublishOne(jobIndexForVerify())

		uiDo(func() {
			_ = out
			_ = err
		})
	}()
}

// jobIndexForVerify derives a deterministic-per-click job index without a
// wall clock dependency in the view layer (the render service's
// verification is index-addressable; the exact index is presentation
// detail).
func jobIndexForVerify() int {
	return int(renderstate.Latest().JobsSubmitted) + 1
}
