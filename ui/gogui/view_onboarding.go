//go:build !headless

// view_onboarding.go — the first-run onboarding wizard (area 11, NEW in
// this migration — the Fyne app had only setBootError). When the boot
// handshake fails or the engine state reports selection-required, the
// shell offers this wizard card: it explains the engine availability
// contract (the core downloads/imports nothing by itself), shows the
// current connection state honestly, and routes the operator to Models
// (selection) or Downloads (engine acquisition) — real navigation, real
// client wiring, no mock progress.
package goguiui

import (
	"sync"

	"github.com/go-gui-org/go-gui/gui"
)

// onboardingViewState is the first-run wizard's state.
type onboardingViewState struct {
	st *shellState

	mu     sync.Mutex
	step   int // 0 welcome, 1 engine availability, 2 connect
	err    error
	engine string
}

func onboardingState(s *shellState) *onboardingViewState {
	return s.view("onboarding", func() any { return &onboardingViewState{st: s} }).(*onboardingViewState)
}

// bootErrorView is the boot-failure surface the shell renders instead of
// the dock when the handshake failed: the wizard card with Retry.
func (s *shellState) bootErrorView() gui.View {
	_, _, bootErr, _ := s.connectionSnapshot()
	onboardingState(s) // register the wizard state for future steps

	steps := []gui.View{
		// Step 1 — welcome + what the product is.
		card("Welcome to "+s.ap.title, "",
			wrappedText("SHEYTAN-LA runs a local AI core on this machine. The UI is a client: durable state, permissions and model truth live in the core, and every action you see calls a real core operation."),
		),
		// Step 2 — engine availability (the honest contract).
		card("Engine availability", "",
			wrappedText("No model ships with the app. The engine downloads from the official llama.cpp release source (Downloads), and models are imported as .gguf files or selected from what you already have (Models). Nothing downloads without you asking."),
			gui.Row(gui.ContainerCfg{
				Content: []gui.View{
					gui.Button(gui.ButtonCfg{
						ID: absID("onboarding-goto-models"), Label: "Open Models",
						OnClick: func(gui.EventCtx) { s.showSub("models") },
					}),
					gui.Button(gui.ButtonCfg{
						ID: absID("onboarding-goto-downloads"), Label: "Open Downloads",
						OnClick: func(gui.EventCtx) { s.show("downloads") },
					}),
				},
			}),
		),
		// Step 3 — connection state + retry.
		card("Connection", "",
			func() []gui.View {
				out := []gui.View{}
				if bootErr != nil {
					out = append(out, ErrorSurface("connection", bootErr, nil))
				} else {
					out = append(out, wrappedText("Connected to the core."))
				}
				out = append(out, gui.Button(gui.ButtonCfg{
					ID:      absID("onboarding-retry"),
					Label:   "Retry connection",
					Variant: gui.ButtonPrimary,
					OnClick: func(gui.EventCtx) { s.ap.retryBootstrap() },
				}))
				return out
			}()...,
		),
	}

	return gui.Column(gui.ContainerCfg{
		ID:         absID("view-onboarding"),
		Sizing:     gui.FillFill,
		Content:    append([]gui.View{PageHeader("Get started", "First-run setup — three steps, all optional after connect")}, steps...),
		Scrollable: true,
	})
}

// buildOnboardingView is the wizard as a standalone dock panel (also the
// boot-error body).
func buildOnboardingView(s *shellState) gui.View {
	onboardingState(s)
	return s.bootErrorView()
}
