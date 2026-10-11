//go:build !windows

// Package screen captures the display — the v1.0.6 vision input. The Windows
// build uses a pure-syscall GDI chain; every other platform (dev/test hosts)
// gets an explicit "unsupported" stub so callers can degrade gracefully.
package screen

import (
	"errors"
	"image"
)

// ErrUnsupported is returned on non-Windows platforms.
var ErrUnsupported = errors.New("screen capture is only supported on Windows")

// Supported reports whether display capture works on this platform.
func Supported() bool { return false }

// PrimarySize returns (0,0) where capture is unsupported.
func PrimarySize() (int, int) { return 0, 0 }

// Capture is the display grab; unsupported off Windows.
func Capture(monitor int) (image.Image, error) { return nil, ErrUnsupported }

// CapturePNG is the PNG-encoded display grab; unsupported off Windows.
func CapturePNG(monitor int) ([]byte, error) { return nil, ErrUnsupported }
