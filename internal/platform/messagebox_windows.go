//go:build windows

package platform

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

// MessageBox shows a blocking, top-most application modal with the given
// title and text. It is the USER's error channel: the production core is
// built -H=windowsgui, owns no console and no window, so a fatal boot
// failure was printed to a detached stdout and the launch "closed
// instantly" with zero on-screen words (run #36: every installed launch).
// The caller's text names the failing step, the error and the app.log
// path; the box makes that story visible to the operator's eyes, not
// only to the log.
//
// Syscall-only: user32!MessageBoxW through LazySystemDLL/LazyProc — the
// exact surface identity_windows.go already uses (no cgo, no new
// dependency). Best-effort observability: a failure to show the box is
// returned and the caller ignores it — the named-step ERROR line in
// app.log remains the primary catcher. MB_TOPMOST|MB_SETFOREGROUND so a
// launch-from-shortcut failure cannot hide behind the taskbar; the user
// clicks OK, the process exits with its original code (exit codes and
// control flow are UNCHANGED — visibility only).
func MessageBox(title, text string) error {
	if err := procMessageBoxW.Find(); err != nil {
		return fmt.Errorf("MessageBoxW unavailable: %w", err)
	}

	const (
		mbIconError     = 0x00000010
		mbSetForeground = 0x00010000
		mbTopmost       = 0x00040000
	)

	titlePtr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return fmt.Errorf("MessageBox title: %w", err)
	}
	textPtr, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return fmt.Errorf("MessageBox text: %w", err)
	}

	ret, _, callErr := procMessageBoxW.Call(
		0, // hWnd: no owner — the core owns no window by design
		uintptr(unsafe.Pointer(textPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(mbIconError|mbSetForeground|mbTopmost),
	)
	if ret == 0 {
		return fmt.Errorf("MessageBoxW failed: %v", callErr)
	}
	return nil
}
