//go:build !windows

package native

// filedialog_other.go — non-Windows stub: the native Win32 picker does not
// exist here, so callers fall back to the toolkit dialog (test/CI builds,
// Linux dev boxes). This keeps `go test ./...` green on any host.

import "errors"

// ErrUnavailable reports that no native picker exists on this platform.
var ErrUnavailable = errors.New("native file picker unavailable on this platform")

func pickFilesImpl(string, []FileFilter, string) PickFilesResult {
	return PickFilesResult{Err: ErrUnavailable}
}
