//go:build !headless

// Package goguiui is the Go-Gui (github.com/go-gui-org/go-gui v0.97.0)
// product UI: the migration of the Fyne v2.8.2 host (ui/fyne) to the
// go-gui toolkit, preserving the ONE-authority contract (the UI is a
// client of the Go core over localipc) and the same wire DTOs.
//
// BUILD CONTRACT (identical to ui/fyne): every file carries
// //go:build !headless so `go build -tags headless ./...` excludes the
// whole package. The toolkit is pure Go at BUILD time — the Linux GL
// backend resolves EGL/X11 symbols at RUNTIME through
// github.com/ebitengine/purego and speaks X11 through github.com/jezek/xgb,
// so `CGO_ENABLED=0 go build -tags '!headless' ./ui/gogui/...` compiles in
// a sandbox with no X11/GL dev headers. Verified against v0.97.0.
//
// # VERIFIED v0.97.0 API NOTES (read from the module source, not guessed)
//
// Window + app boot:
//
//	w := gui.NewWindow(gui.WindowCfg{State: *T, Title, Width, Height,
//	        OnInit: func(w *gui.Window) { w.SetView(viewFn) }})
//	backend.Run(w) // gui/backend — per-OS native loop (X11+purego EGL on linux)
//
// Tests use gui.NewTestWindow(t, cfg) (+ t.Cleanup(WindowCleanup)) and the
// driver methods w.TestRender(view), w.TestClick(id), w.TestType(id, text),
// w.TestKey(id, key, mods), w.TestTab(dir), w.TestStep(dt). Test actions
// settle commands queued with Window.QueueCommand (up to 8 passes).
//
// Stateless view pipeline: each frame the view function returns a
// gui.View tree; there is no widget identity and no virtual DOM. The app
// stores ONE typed state value in the window (gui.State[T](w) recovers
// *T; WindowCfg.State seeds it) and every view derives from it. So —
// unlike Fyne — background workers mutate APP STATE (guarded by the
// app mutex), and uiDo only marshals the state mutation onto the UI
// thread + triggers the refresh; the next frame rebuilds the tree.
//
// Threading seam (uido.go): uiDo(fn) → w.QueueCommand(func(*Window){fn()})
// in production (the documented thread-safe mutation path; SetTheme from a
// goroutine is NOT safe — it must ride a queued command too). Tests call
// SetTestUIMarshal() to run uiDo inline under a process-wide mutex — the
// same contract ui/fyne/ui_marshal.go ships for Fyne.
//
// Widget set used here (all verified against v0.97.0 sources):
// gui.Column/Row(gui.ContainerCfg{Sizing, Spacing, Padding, Scrollable,
// ScrollbarCfgY, Content}), gui.Text(TextCfg{Text, TextStyle, Mode:
// gui.TextWrap, Sizing: gui.FillFit}), gui.Button(ButtonCfg{ID, Label,
// Variant: gui.ButtonPrimary/Secondary/Danger, OnClick}), gui.Input(
// InputCfg{ID, Label, Text, Placeholder, OnTextChanged, OnEnter,
// OnTextCommit, MaxLength}), gui.Switch/Toggle (SwitchCfg{Label, Selected,
// OnClick}), gui.Select(SelectCfg{ID, Items/Options, Placeholder, Selected,
// OnSelect}), gui.ProgressBar(ProgressBarCfg{Percent, TextShow,
// Indefinite}), gui.Badge(BadgeCfg{Label, Color, Dot}), gui.Separator(
// SeparatorCfg{}), gui.TabControl(TabControlCfg{Selected, OnSelect, Items}),
// gui.Splitter(SplitterCfg{First/Second: SplitterPaneCfg{Content, MinSize},
// OnChange(ratio, collapsed, ctx)}), gui.DockLayout(DockLayoutCfg{Root
// *gui.DockNode, Panels []DockPanelDef, OnPanelSelect, OnLayoutChange})
// with gui.DockSplit(id, dir, ratio, first, second) / gui.DockPanelGroup,
// gui.D.rsDockNodeSanitize after json.Unmarshal, gui.Listbox for list
// surfaces. Text style roles: theme.TextStyleTitle/Body/BodySmall/Caption/
// Code etc.; color roles: theme.ColorBackground/Panel/Interior/Border/
// ColorAccent/Success/Warning/Error/ColorTextSecondary.
//
// Layout sizing vocabulary: gui.FillFill/FillFit/FitFill/FitFit/FixedFixed
// via cfg.Sizing; container Scrollable:true + ScrollbarCfgY makes a
// scrolling column. IDs: a leaf ID is scoped by ancestor container IDs
// (IDSep ":" composes absolute IDs); the tests address widgets by their
// ABSOLUTE ids (ui:<view>:<name>) exactly like the POC.
//
// Theme: gui.ThemeMaker(gui.ThemeCfg{...}) builds a full theme from a
// config (colors, spacing ladder, radii, text ladder). gui.SetTheme(t)
// publishes the app default (frame-thread only — queue it from workers);
// gui.CurrentTheme() reads it during generation; w.Theme() inside views.
// This package maps internal/theme catalog tokens (six themes, DefaultID
// "hellish") into ThemeCfg roles — see theme.go.
//
// Native platform: window.NativePlatformBackend() returns the
// gui.NativePlatform the backend installed (nil in tests). The v0.97.0
// interface includes nativeKeepAwake.SetKeepAwake(on bool, app, reason
// string) error — gui counts Window.KeepAwake tokens across the app and
// calls SetKeepAwake only when the aggregate state changes. Our
// internal/platform typed adapters mirror this signature (PowerProvider)
// and the desktop real implementation forwards to the backend's platform.
//
// Clipboard: Window.SetClipboard(text)/Window.GetClipboard() with
// pluggable SetClipboardFn/SetClipboardGetFn hooks (tests may stub them;
// the GL backend binds the OS clipboard).
//
// Debug: GOGUI_DEBUG env gates gui.Debug diagnostics
// (DebugAll categories; GOGUI_DEBUG_REBUILDS adds rebuild logging).
//
// PixelSurface: gui.PixelSurface is an owned CPU-side pixel buffer
// (PixelFormatRGBA8 planes) for zero-copy texture upload — not needed by
// this migration (no embedded GL content), noted for the gogpu interop.
package goguiui
