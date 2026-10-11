//go:build windows

// Package screen captures the display — the v1.0.6 vision input. On Windows
// this is a pure-syscall GDI chain (GetDC → CreateCompatibleDC → BitBlt →
// GetDIBits): no CGO, no external binary, nothing flashing on screen, and it
// cross-compiles from any host.
package screen

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	gdi32             = windows.NewLazySystemDLL("gdi32.dll")
	pGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	pGetDC            = user32.NewProc("GetDC")
	pReleaseDC        = user32.NewProc("ReleaseDC")
	pCreateCompatDC   = gdi32.NewProc("CreateCompatibleDC")
	pDeleteDC         = gdi32.NewProc("DeleteDC")
	pCreateCompatBmp  = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject     = gdi32.NewProc("SelectObject")
	pDeleteObject     = gdi32.NewProc("DeleteObject")
	pBitBlt           = gdi32.NewProc("BitBlt")
	pGetDIBits        = gdi32.NewProc("GetDIBits")
)

const (
	smCXScreen = 0
	smCYScreen = 1
	srccopy    = 0x00CC0020
	dibRGB     = 0
)

type bitmapInfoHeader struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

// ErrCapture is returned when the GDI chain fails (locked session, headless
// service context, ...).
var ErrCapture = errors.New("screen capture failed")

// Supported reports whether display capture works on this platform.
func Supported() bool { return true }

// PrimarySize returns the primary monitor dimensions in pixels.
func PrimarySize() (int, int) {
	w, _, _ := pGetSystemMetrics.Call(uintptr(smCXScreen))
	h, _, _ := pGetSystemMetrics.Call(uintptr(smCYScreen))
	return int(int32(w)), int(int32(h))
}

// Capture grabs the primary display (monitor must be 0 in this release) and
// returns it as an RGBA image.
func Capture(monitor int) (image.Image, error) {
	if monitor != 0 {
		return nil, fmt.Errorf("only the primary display (monitor 0) is supported, got %d", monitor)
	}
	w, h := PrimarySize()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("%w: screen metrics %dx%d", ErrCapture, w, h)
	}

	screenDC, _, _ := pGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("%w: GetDC returned NULL", ErrCapture)
	}
	defer pReleaseDC.Call(0, screenDC)

	memDC, _, _ := pCreateCompatDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("%w: CreateCompatibleDC failed", ErrCapture)
	}
	defer pDeleteDC.Call(memDC)

	hBmp, _, _ := pCreateCompatBmp.Call(screenDC, uintptr(w), uintptr(h))
	if hBmp == 0 {
		return nil, fmt.Errorf("%w: CreateCompatibleBitmap failed", ErrCapture)
	}
	defer pDeleteObject.Call(hBmp)

	old, _, _ := pSelectObject.Call(memDC, hBmp)
	defer pSelectObject.Call(memDC, old)

	ok, _, _ := pBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), screenDC, 0, 0, uintptr(srccopy))
	if ok == 0 {
		return nil, fmt.Errorf("%w: BitBlt failed", ErrCapture)
	}

	// GetDIBits requires the bitmap to NOT be selected into a DC.
	pSelectObject.Call(memDC, old)

	// 32bpp top-down BGRA.
	bmi := bitmapInfoHeader{
		BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:       int32(w),
		BiHeight:      -int32(h), // negative = top-down rows
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: 0, // BI_RGB
	}
	buf := make([]byte, w*h*4)
	ret, _, _ := pGetDIBits.Call(
		screenDC, hBmp, 0, uintptr(h),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bmi)), dibRGB,
	)
	if ret == 0 {
		return nil, fmt.Errorf("%w: GetDIBits returned 0", ErrCapture)
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// GDI gives BGRA; RGBA image wants R,G,B,A order.
	for y := 0; y < h; y++ {
		row := buf[y*w*4 : (y+1)*w*4]
		pix := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for x := 0; x < w; x++ {
			b := row[x*4]
			g := row[x*4+1]
			r := row[x*4+2]
			a := row[x*4+3]
			pix[x*4] = r
			pix[x*4+1] = g
			pix[x*4+2] = b
			pix[x*4+3] = a
		}
	}
	return img, nil
}

// CapturePNG captures the primary display and encodes it as PNG bytes.
func CapturePNG(monitor int) ([]byte, error) {
	img, err := Capture(monitor)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
