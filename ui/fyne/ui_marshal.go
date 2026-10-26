//go:build !headless

// ui_marshal.go — the ONE widget-mutation marshalling seam.
//
// Production: fyne.Do serializes fn onto the Fyne event loop — the only
// thread-safe widget-update mechanism.
//
// Tests: the Fyne test driver runs fyne.Do callbacks INLINE on the
// calling goroutine (test/driver.go DoFromGoroutine), which would race
// the test goroutine's own widget access. Tests enable the test marshal
// (SetTestUIMarshal); uiDo then serializes through a process-wide mutex,
// and tests wrap their direct widget operations in uiDo as well — every
// mutation is serialized, in every environment.
package fyneui

import (
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
)

var (
	testMarshal atomic.Bool
	uiTestMu    sync.Mutex
)

// SetTestUIMarshal switches uiDo to mutex serialization (test harness
// only). It returns a restore func for t.Cleanup.
func SetTestUIMarshal() func() {
	testMarshal.Store(true)
	return func() { testMarshal.Store(false) }
}

// uiDo marshals fn onto the UI loop (production) or serializes it inline
// under the test lock (tests).
func uiDo(fn func()) {
	if testMarshal.Load() {
		uiTestMu.Lock()
		defer uiTestMu.Unlock()
		fn()
		return
	}
	fyne.Do(fn)
}

// uiDoLocked runs fn under the test serialization lock only (test-side
// helper for direct widget operations; production callers use uiDo).
func uiDoLocked(fn func()) {
	if testMarshal.Load() {
		uiTestMu.Lock()
		defer uiTestMu.Unlock()
	}
	fn()
}
