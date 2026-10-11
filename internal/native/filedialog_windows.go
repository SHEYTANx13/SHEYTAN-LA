//go:build windows

package native

// filedialog_windows.go — comdlg32!GetOpenFileNameW via raw syscall.
//
// Layout notes: the OPENFILENAMEW structure is reproduced field-for-field
// with Windows x64 ABI alignment (pointers 8-aligned; explicit pad words
// where the ABI inserts them). lStructSize is taken from unsafe.Sizeof of
// the Go struct — exactly what the OS expects (160 bytes on amd64).
//
// Multi-select: with OFN_ALLOWMULTISELECT the lpstrFile buffer receives
// "<dir>\0<name1>\0<name2>\0…\0\0". Single select (and the degenerate
// one-file multiselect case) receives just the full path.

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

const (
	ofnReadOnly        = 0x0000_0001
	ofnFileMustExist   = 0x0000_1000
	ofnAllowMultiSel   = 0x0000_0200
	ofnNoChangeDir     = 0x0000_0008
	ofnExplorer        = 0x0008_0000
	ofnPathMustExist   = 0x0000_0800
	ofnHideReadOnly    = 0x0000_0004
	ofnDontAddToRecent = 0x0200_0000
)

// openFilename mirrors the Win32 OPENFILENAMEW layout (x64).
type openFilename struct {
	lStructSize       uint32
	_                 uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	_                 uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	_                 uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	_                 uint32
	nFileOffset       uint16
	nFileExtension    uint16
	_                 uint32
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

var (
	comdlg32        = syscall.NewLazyDLL("comdlg32.dll")
	procGetOpenName = comdlg32.NewProc("GetOpenFileNameW")
	user32          = syscall.NewLazyDLL("user32.dll")
	procForeground  = user32.NewProc("GetForegroundWindow")

	dialogMu sync.Mutex // one native dialog at a time, process-wide
)

// pickFilesImpl opens the Win32 common file dialog (multi-select).
func pickFilesImpl(title string, filters []FileFilter, initialDir string) PickFilesResult {
	// The common dialog is modal + re-entrant-hostile: serialize instances.
	dialogMu.Lock()
	defer dialogMu.Unlock()

	// lpstrFile buffer: 64 KB of UTF-16 — ample for multi-select of many
	// long paths (MAX_PATH * ~1000 files).
	const bufChars = 32768
	fileBuf := make([]uint16, bufChars)

	filterUTF16 := utf16Ptr(BuildFilterString(filters))
	titleUTF16, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		titleUTF16 = nil
	}
	var dirUTF16 *uint16
	if initialDir != "" {
		dirUTF16, _ = syscall.UTF16PtrFromString(initialDir)
	}

	ofn := openFilename{
		lStructSize:     uint32(unsafe.Sizeof(openFilename{})),
		hwndOwner:       foregroundWindow(),
		lpstrFilter:     filterUTF16,
		nFilterIndex:    1, // first filter entry preselected
		lpstrFile:       &fileBuf[0],
		nMaxFile:        uint32(bufChars),
		lpstrTitle:      titleUTF16,
		lpstrInitialDir: dirUTF16,
		flags: ofnExplorer | ofnFileMustExist | ofnPathMustExist |
			ofnAllowMultiSel | ofnHideReadOnly | ofnNoChangeDir | ofnReadOnly,
	}

	// GetOpenFileNameW returns FALSE on cancel AND on failure —
	// CommDlgExtendedError() distinguishes them (0 = user cancelled).
	ret, _, _ := procGetOpenName.Call(uintptr(unsafe.Pointer(&ofn)))
	if ret == 0 {
		if code := commDlgError(); code != 0 {
			return PickFilesResult{Err: fmt.Errorf("GetOpenFileNameW failed (code %d)", code)}
		}
		return PickFilesResult{Canceled: true}
	}

	paths := ParseMultiSelBuf(fileBuf)
	if len(paths) == 0 {
		return PickFilesResult{Canceled: true}
	}
	return PickFilesResult{Paths: paths}
}

// utf16Ptr converts a Go string to a NUL-terminated UTF-16 buffer pointer.
func utf16Ptr(s string) *uint16 {
	u, err := syscall.UTF16FromString(s)
	if err != nil || len(u) == 0 {
		return nil
	}
	return &u[0]
}

// foregroundWindow returns the caller's active top-level window so the
// dialog is owned (and modal) to it; 0 when unavailable — the dialog then
// still opens as a floating top-level window.
func foregroundWindow() uintptr {
	h, _, _ := procForeground.Call()
	return h
}

// commDlgError reads CommDlgExtendedError (0 = no error / user cancel).
func commDlgError() uintptr {
	proc := comdlg32.NewProc("CommDlgExtendedError")
	v, _, _ := proc.Call()
	return v
}
