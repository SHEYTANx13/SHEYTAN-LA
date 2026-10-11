// Package native — low-level Windows system access.
//
// THE ATTACHMENT-CRASH FIX (v1.0.8): the app previously opened attachments
// through a toolkit file dialog that walks the filesystem in its own
// goroutines to list folders. On real Windows machines that walker can
// panic when it meets special locations (network drives, OneDrive virtual
// folders, empty card readers, indexed junctions) — and an uncaught panic
// in ANY goroutine terminates the whole process. That is exactly the
// "app closes when I click attach" bug reported against v1.0.7.
//
// This package replaces the walker with the OS's own dialog:
// comdlg32!GetOpenFileNameW, invoked through raw syscalls — the classic
// lower-language move. Zero Go-side filesystem walking, instant open on
// every machine, native Explorer chrome, real multi-select, and it cannot
// panic because there is no interpreted layer between us and the OS.
//
// Everything here is pure syscall — no CGO, no cgo-gcc dependency at build
// time. These files must stay pure so they also work in CGO_ENABLED=0 test
// and cross builds.
package native

import (
	"strings"
	"unicode/utf16"
)

// FileFilter is one entry in the picker's type dropdown: a human label and
// the semicolon-joined extension pattern (e.g. "*.txt;*.md").
type FileFilter struct {
	Label   string
	Pattern string
}

// PickFilesResult reports what the user chose.
type PickFilesResult struct {
	// Paths holds the selected absolute paths (empty when cancelled).
	Paths []string
	// Canceled is true when the user dismissed the dialog.
	Canceled bool
	// Err is set when the dialog could not be opened at all (caller should
	// fall back to another picker).
	Err error
}

// PickFiles opens the platform-native multi-select file picker.
// It BLOCKS until the dialog closes — call it from a worker goroutine,
// never from the UI thread (the modal loop must pump, and on Windows the
// UI goroutine must keep serving our own window).
//
// initialDir may be "" (the OS remembers the last folder, which is the
// friendlier behavior across runs).
func PickFiles(title string, filters []FileFilter, initialDir string) PickFilesResult {
	return pickFilesImpl(title, filters, initialDir)
}

// BuildFilterString renders the filter list as the double-null-terminated
// sequence GetOpenFileNameW expects: "Label\0Pattern\0…\0\0". Pure logic —
// shared by every platform and unit-testable everywhere.
func BuildFilterString(filters []FileFilter) string {
	var b strings.Builder
	for _, f := range filters {
		b.WriteString(f.Label)
		b.WriteByte(0)
		b.WriteString(f.Pattern)
		b.WriteByte(0)
	}
	b.WriteByte(0) // terminating extra NUL
	return b.String()
}

// ParseMultiSelBuf decodes the lpstrFile buffer after a successful
// GetOpenFileNameW call. Single selection yields one full path; multi
// selection yields "<dir>\0<name1>\0<name2>\0…\0\0" which becomes
// dir-joined paths. Empty / all-NUL buffers yield nil (caller treats as
// cancel). Pure logic — unit-testable on every platform, hostile-input
// safe (truncated buffers, missing terminators).
func ParseMultiSelBuf(buf []uint16) []string {
	segs := splitNulls(buf)
	switch len(segs) {
	case 0:
		return nil
	case 1:
		return []string{segs[0]}
	default:
		dir := segs[0]
		out := make([]string, 0, len(segs)-1)
		for _, name := range segs[1:] {
			out = append(out, dir+"\\"+name)
		}
		return out
	}
}

// splitNulls extracts the NUL-separated strings, stopping at the double
// NUL terminator.
func splitNulls(buf []uint16) []string {
	var out []string
	start := -1
	for i, c := range buf {
		if c == 0 {
			if start >= 0 {
				out = append(out, utf16ToString(buf[start:i]))
				start = -1
				// double-NUL terminator?
				if i+1 < len(buf) && buf[i+1] == 0 {
					break
				}
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	// Drop trailing empty strings (buffer zero padding).
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func utf16ToString(u []uint16) string {
	return string(utf16.Decode(u))
}
