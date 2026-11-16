// go-gui POC — v1.11.1 UI-engine evaluation slice (POC_ONLY).
//
// This is the isolated, reversible Go-Gui proof-of-concept required by the
// v1.11.1 work order (docs/architecture/UI-ENGINE-DECISION.md): a nested
// module that the production module does NOT depend on, exercising the
// reference slice against the pinned candidate:
//
//	github.com/go-gui-org/go-gui v0.96.0
//	  tag refs/tags/v0.96.0 @ 6f2d4e16e58b25fc2bec3d642e6552433a4e7ae4
//	  sum h1:VIHHfFXiMJ43Ru9L16GLqbR10k3y2TJuhKowSS62agI=
//	  Go 1.26.0, MIT license (module proxy metadata, verified 2026-11-16)
//
// The slice: responsive window + navigation sidebar, chat surface with a
// scrolling message list (long content, Persian/Arabic mixed-direction
// text with embedded LTR URLs/code), a composer with keyboard focus, a
// resizable inspector split pane, and a table artifact view. Headless
// app-level tests (type, click, render) plus a software-rendered
// screenshot live in main_test.go.
//
// Status: POC_ONLY. NOT PRODUCTION_READY. The production renderer for
// v1.11.1 remains Fyne (ui/fyne). Promotion requires the full platform
// gate in UI-ENGINE-DECISION.md (accessibility trees, IME/CJK, native
// dialogs, physical-device matrix) which this POC does not attempt.
package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"github.com/go-gui-org/go-gui/gui"
	"github.com/go-gui-org/go-gui/gui/backend"
	"github.com/go-gui-org/go-gui/gui/backend/soft"
)

const (
	// These three interactive IDs are ABSOLUTE (they contain the gui
	// IDSep ":"): inside the Splitter's pane scope a plain leaf would be
	// joined onto the splitter's auto-generated ID, but an absolute leaf
	// is its own whole identity (gui/id_resolve.go) — so the composer,
	// send button and inspector input stay addressable for SetFocus and
	// the test driver no matter how the panes scope them.
	composerID       = "poc:composer"
	sendID           = "poc:send"
	inspectorInputID = "poc:inspector"

	navChatID  = "nav-chat"
	navTableID = "nav-table"
	messagesID = "poc-messages"
	windowW    = 1024
	windowH    = 700
)

type chatMessage struct {
	ID      int
	Role    string // "user" | "assistant"
	Content string
}

type pocState struct {
	Nav           string // "chat" | "table"
	Draft         string
	Messages      []chatMessage
	NextID        int
	InspectorNote string
}

func newPOCState() *pocState {
	return &pocState{
		Nav:    "chat",
		NextID: 4,
		Messages: []chatMessage{
			{
				ID:   1,
				Role: "user",
				// Mixed-direction slice: Persian RTL text with embedded
				// LTR URL and code, the exact hard case for bidi layout.
				Content: "سلام! این یک پیام فارسی است — mixed with LTR " +
					"https://example.com/sheytan-la/path and code " +
					"`fmt.Println(\"salam\")` — end of the mixed message.",
			},
			{
				ID:   2,
				Role: "assistant",
				Content: "چیدمان راست‌به‌چپ و متن دوجهته در این نمونه بررسی می‌شود. " +
					"The quick brown fox jumps over the lazy dog. 0123456789.",
			},
			{
				ID:   3,
				Role: "user",
				// Long-content slice: forces the message list to scroll.
				Content: "Long content: " + strings.Repeat(
					"The quick brown fox jumps over the lazy dog. ", 24),
			},
		},
		InspectorNote: "inspector — resize me",
	}
}

func main() {
	screenshot := flag.String("screenshot", "", "render a screenshot to this path and exit")
	flag.Parse()

	gui.SetTheme(gui.ThemeLight.WithPadding(false))

	w := gui.NewWindow(gui.WindowCfg{
		State:  newPOCState(),
		Title:  "SHEYTAN-LA Go-Gui POC (POC_ONLY)",
		Width:  windowW,
		Height: windowH,
		OnInit: func(w *gui.Window) {
			w.SetView(mainView)
			w.SetFocus(composerID)
		},
	})

	if *screenshot != "" {
		if err := soft.RenderToPNG(w, 2, *screenshot); err != nil {
			log.Fatalf("screenshot: %v", err)
		}
		os.Exit(0)
	}
	backend.Run(w)
}

func mainView(w *gui.Window) gui.View {
	app := gui.State[pocState](w)
	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFill,
		Spacing: gui.SpacingMedium,
		Content: []gui.View{
			navView(w),
			mainSurfaceView(w, app),
		},
	})
}

func navView(w *gui.Window) gui.View {
	app := gui.State[pocState](w)
	chatVariant, tableVariant := gui.ButtonSecondary, gui.ButtonSecondary
	if app.Nav == "chat" {
		chatVariant = gui.ButtonPrimary
	}
	if app.Nav == "table" {
		tableVariant = gui.ButtonPrimary
	}
	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FitFill,
		Spacing: gui.SpacingMedium,
		Padding: gui.PaddingMedium,
		Content: []gui.View{
			gui.Text(gui.TextCfg{Text: "SHEYTAN-LA POC", TextStyle: gui.CurrentTheme().TextStyleTitle}),
			gui.Button(gui.ButtonCfg{
				ID:      navChatID,
				Label:   "Chat",
				Variant: chatVariant,
				OnClick: func(gui.EventCtx) { setNav(w, "chat") },
			}),
			gui.Button(gui.ButtonCfg{
				ID:      navTableID,
				Label:   "Table",
				Variant: tableVariant,
				OnClick: func(gui.EventCtx) { setNav(w, "table") },
			}),
		},
	})
}

func setNav(w *gui.Window, nav string) {
	app := gui.State[pocState](w)
	app.Nav = nav
}

func mainSurfaceView(w *gui.Window, app *pocState) gui.View {
	first := chatSurfaceView(w)
	if app.Nav == "table" {
		first = tableView(w)
	}
	return gui.Splitter(gui.SplitterCfg{
		First: gui.SplitterPaneCfg{
			Content: []gui.View{first},
			MinSize: 420,
		},
		Second: gui.SplitterPaneCfg{
			Content: []gui.View{inspectorView(w)},
			MinSize: 260,
		},
	})
}

func chatSurfaceView(w *gui.Window) gui.View {
	app := gui.State[pocState](w)
	theme := gui.CurrentTheme()

	msgs := make([]gui.View, 0, len(app.Messages))
	for _, m := range app.Messages {
		style := theme.TextStyleBody
		if m.Role == "assistant" {
			style = theme.TextStyleBodyLarge
		}
		msgs = append(msgs, gui.Text(gui.TextCfg{
			ID:        msgID(m.ID),
			Text:      m.Role + ": " + m.Content,
			TextStyle: style,
			Focusable: true,
		}))
	}

	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FillFill,
		Spacing: gui.SpacingMedium,
		Content: []gui.View{
			// Scrolling message list: long content scrolls inside the
			// pane, the composer stays fixed below it.
			gui.Column(gui.ContainerCfg{
				ID:            messagesID,
				Sizing:        gui.FillFill,
				Scrollable:    true,
				ScrollbarCfgY: &gui.ScrollbarCfg{Overflow: gui.ScrollbarAuto},
				Spacing:       gui.SpacingSmall,
				Padding:       gui.PaddingSmall,
				Content:       msgs,
			}),
			composerView(w),
		},
	})
}

func msgID(id int) string {
	return "poc-msg-" + itoa(id)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func composerView(w *gui.Window) gui.View {
	app := gui.State[pocState](w)
	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Spacing: gui.SpacingMedium,
		Content: []gui.View{
			gui.Input(gui.InputCfg{
				ID:    composerID,
				Text:  app.Draft,
				Label: "Message",
				OnTextChanged: func(s string, _ gui.EventCtx) {
					gui.State[pocState](w).Draft = s
				},
			}),
			gui.Button(gui.ButtonCfg{
				ID:      sendID,
				Label:   "Send",
				OnClick: func(gui.EventCtx) { sendDraft(w) },
			}),
		},
	})
}

func sendDraft(w *gui.Window) {
	app := gui.State[pocState](w)
	draft := strings.TrimSpace(app.Draft)
	if draft == "" {
		return
	}
	app.Messages = append(app.Messages, chatMessage{
		ID:      app.NextID,
		Role:    "user",
		Content: draft,
	})
	app.NextID++
	app.Draft = ""
}

func tableView(w *gui.Window) gui.View {
	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FillFill,
		Padding: gui.PaddingMedium,
		Content: []gui.View{
			gui.Text(gui.TextCfg{
				Text:      "Artifacts (table slice)",
				TextStyle: gui.CurrentTheme().TextStyleTitle,
			}),
			gui.Table(gui.TableCfgFromData([][]string{
				{"Artifact", "Type", "Size", "State"},
				{"chat-export.md", "markdown", "12.4 KB", "restorable"},
				{"tiny-llama.gguf", "model", "5.7 MB", "external"},
				{"run-summary.json", "json", "3.1 KB", "append-only"},
				{"screenshot.png", "image", "88 KB", "recoverable"},
			})),
		},
	})
}

func inspectorView(w *gui.Window) gui.View {
	app := gui.State[pocState](w)
	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FitFill,
		Padding: gui.PaddingMedium,
		Spacing: gui.SpacingMedium,
		Content: []gui.View{
			gui.Text(gui.TextCfg{
				Text:      "Inspector",
				TextStyle: gui.CurrentTheme().TextStyleTitle,
			}),
			gui.Text(gui.TextCfg{
				Text: "The split handle resizes this pane; the chat pane keeps a " +
					"usable minimum. POC slice only — no production behavior.",
				TextStyle: gui.CurrentTheme().TextStyleBody,
			}),
			gui.Input(gui.InputCfg{
				ID:    inspectorInputID,
				Text:  app.InspectorNote,
				Label: "Note",
				OnTextChanged: func(s string, _ gui.EventCtx) {
					gui.State[pocState](w).InspectorNote = s
				},
			}),
		},
	})
}
