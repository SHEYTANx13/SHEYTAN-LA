//go:build !headless

// providers_gogui.go — the desktop real implementations over the go-gui
// window surface. The package stays toolkit-free for the INTERFACES; the
// go-gui-backed constructors live behind the !headless build tag exactly
// like the ui/gogui package, so `-tags headless` builds exclude them
// together with the UI.
package platform

import (
	"fmt"
	"os/exec"
	"runtime"
)

// GoguiClipboard adapts the window's clipboard functions (the shell
// wires Window.SetClipboard/GetClipboard through these pointers; tests
// inject stubs the same way).
type GoguiClipboard struct {
	Set func(text string)
	Get func() string
}

func (c GoguiClipboard) SetText(text string) error {
	if c.Set == nil {
		return fmt.Errorf("clipboard SetText: %w", ErrUnsupported)
	}
	c.Set(text)
	return nil
}

func (c GoguiClipboard) Text() (string, error) {
	if c.Get == nil {
		return "", fmt.Errorf("clipboard Text: %w", ErrUnsupported)
	}
	return c.Get(), nil
}

// GoguiPower adapts the toolkit's native platform SetKeepAwake — the
// v0.97.0 NativePlatform contract (gui counts window tokens and calls
// the platform only on aggregate state changes). A nil fn reports the
// typed unsupported error (headless tests).
type GoguiPower struct {
	// SetKeepAwakeFn mirrors nativeKeepAwake.SetKeepAwake.
	SetKeepAwakeFn func(on bool, app, reason string) error
}

func (p GoguiPower) SetKeepAwake(on bool, app, reason string) error {
	if p.SetKeepAwakeFn == nil {
		return fmt.Errorf("SetKeepAwake(%v, %q, %q): %w", on, app, reason, ErrUnsupported)
	}
	return p.SetKeepAwakeFn(on, app, reason)
}

// GoguiWebWorkspace opens URLs through the OS default handler. The
// toolkit's backends expose OpenURI on their native platform, but where
// no backend is attached the desktop fallback shells out to the
// platform opener (x dg-open / open / cmd start) — still an honest
// typed error when nothing is available.
type GoguiWebWorkspace struct {
	// OpenURIFn, when set, wins (the shell wires the native platform).
	OpenURIFn func(raw string) error
}

func (w GoguiWebWorkspace) OpenURL(raw string) error {
	if w.OpenURIFn != nil {
		return w.OpenURIFn(raw)
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", raw).Start()
	case "darwin":
		return exec.Command("open", raw).Start()
	case "linux", "freebsd", "openbsd", "netbsd":
		return exec.Command("xdg-open", raw).Start()
	default:
		return fmt.Errorf("OpenURL(%q): %w", raw, ErrUnsupported)
	}
}
