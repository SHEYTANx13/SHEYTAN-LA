//go:build windows

package platform

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32                       = windows.NewLazySystemDLL("shell32.dll")
	procSetCurrentProcessExplicit = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
)

// setAppUserModelID calls SetCurrentProcessExplicitAppUserModelID with the
// UTF-16 app id. This is the documented Windows entry point for the
// process-level AUMID; it must run before the first window is created.
func setAppUserModelID(id string) error {
	if id == "" {
		return fmt.Errorf("AppUserModelID must not be empty")
	}

	p16, err := syscall.UTF16PtrFromString(id)
	if err != nil {
		return fmt.Errorf("AppUserModelID %q: %w", id, err)
	}

	// HRESULT-call via raw proc: the API is a simple COM-free shell entry
	// point (one in-parameter, no object identity).
	ret, _, _ := procSetCurrentProcessExplicit.Call(uintptr(unsafe.Pointer(p16)))
	if ret != 0 {
		return fmt.Errorf("SetCurrentProcessExplicitAppUserModelID(%s) failed: hresult=0x%x", id, ret)
	}
	return nil
}
