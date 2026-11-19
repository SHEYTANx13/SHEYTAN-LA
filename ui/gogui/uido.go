//go:build !headless

// uido.go — the ONE widget-state mutation marshalling seam, ported from
// ui/fyne/ui_marshal.go.
//
// Production: go-gui's documented cross-goroutine mutation path is
// Window.QueueCommand — the callback runs on the UI thread at the next
// frame update. Every background-worker→UI mutation in this package goes
// through uiDo so the same seam covers both toolkits.
//
// Tests: go-gui's test driver settles queued commands only when a Test*
// action runs, so background-goroutine mutations would not be visible to
// assertions at deterministic points. Tests enable the test marshal
// (SetTestUIMarshal); uiDo then executes INLINE under a process-wide
// mutex — every mutation is serialized in every environment, mirroring
// ui/fyne's contract exactly.
package goguiui

import (
	"sync"
	"sync/atomic"

	"github.com/go-gui-org/go-gui/gui"
)

var (
	testMarshal atomic.Bool
	uiTestMu    sync.Mutex

	// uiDoWindow is the window the running app marshals onto. App.Run
	// stores it before any background worker starts; it is never
	// cleared (a closed window's QueueCommand is a no-op via its ctx).
	uiDoWindow atomic.Value // *gui.Window
)

// SetTestUIMarshal switches uiDo to inline mutex serialization (test
// harness only). It returns a restore func for t.Cleanup.
func SetTestUIMarshal() func() {
	testMarshal.Store(true)
	return func() { testMarshal.Store(false) }
}

// uiDo marshals fn onto the UI loop (production) or runs it inline under
// the test lock (tests). In production fn runs on the frame thread; it
// must mutate app state / call gui.SetTheme, never block on IO.
func uiDo(fn func()) {
	if testMarshal.Load() {
		uiTestMu.Lock()
		defer uiTestMu.Unlock()
		fn()
		return
	}
	// The window may not be armed yet (pre-Run calls): fall back to a
	// package-level window registry entry once Run owns one. uiDoWindow
	// is set by App.Run before any worker starts.
	if w := uiDoWindow.Load(); w != nil {
		w.(*gui.Window).QueueCommand(func(*gui.Window) { fn() })
		return
	}
	// No window yet (tests without SetTestUIMarshal, detached probes):
	// run inline rather than drop the mutation silently.
	fn()
}

// uiDoLocked runs fn under the test serialization lock only (test-side
// helper for direct state operations; production callers use uiDo).
func uiDoLocked(fn func()) {
	if testMarshal.Load() {
		uiTestMu.Lock()
		defer uiTestMu.Unlock()
	}
	fn()
}
