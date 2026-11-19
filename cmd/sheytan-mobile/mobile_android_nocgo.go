//go:build android && !cgo

// mobile_android_nocgo.go — the CGO_ENABLED=0 Android type-check path.
// The real device backend REQUIRES cgo (GLES3/EGL), so a pure-Go Android
// binary cannot present a UI: func main reports the typed limitation
// loudly instead of pretending. gomobile bind (cgo on) replaces this
// file's build inclusion with mobile_android.go + the shared main above
// via the `android && cgo` guard.
//
// This file exists so `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build`
// (the sandbox verification, no NDK) has a compilable package whose
// non-cgo parts (headless core client wiring, platform adapters) still
// type-check on the android OS build.
package main

import (
	"errors"
	"fmt"
	"os"
)

// errAndroidNeedsCgo is the honest refusal: no GL surface without cgo.
var errAndroidNeedsCgo = errors.New(
	"sheytan-mobile: the go-gui Android backend requires cgo (NDK build via gomobile bind); " +
		"this CGO_ENABLED=0 build cannot present a UI")

func main() {
	fmt.Fprintln(os.Stderr, errAndroidNeedsCgo)
	os.Exit(3)
}
