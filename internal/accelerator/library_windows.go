// library_windows.go — the measured Windows library-load probe (v1.2.6).
//go:build windows

package accelerator

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// loadable reports whether the OS loader can actually map the library.
func loadable(path string) bool {
	handle, err := windows.LoadLibrary(path)
	if err != nil {
		return false
	}

	_ = windows.FreeLibrary(handle)

	return true
}

// findLibraryOnPath resolves a library name through the Windows DLL search
// order (LoadLibrary semantics — the strongest honest probe: the OS loader
// itself performs the resolution, including any PATH entries).
func findLibraryOnPath(name string) (string, error) {
	handle, err := windows.LoadLibrary(name)
	if err != nil {
		return "", fmt.Errorf("LoadLibrary(%s): %w", name, err)
	}

	defer windows.FreeLibrary(handle)

	buf := make([]uint16, windows.MAX_PATH)

	n, err := windows.GetModuleFileName(handle, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		// The library IS loadable — the path is just unknown. The load is
		// the measured fact that matters.
		return name, nil
	}

	return windows.UTF16ToString(buf[:n]), nil
}
