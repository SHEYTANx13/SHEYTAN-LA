//go:build darwin && !ios && !cgo

package proc

// CGO_ENABLED=0 darwin fallback (fail closed): the libproc CGo bridge is
// not linked in this build, so process identity is the typed UNSUPPORTED
// answer — never a guess, never a partial syscall transcription. This
// keeps CGO-free darwin builds (type-checks, code-signing-agnostic
// tooling) compilable while the identity contract stays honest
// (run 44306343338 repair §4.7). The production darwin binaries link cgo,
// so the REAL implementation in identity_darwin.go is what ships.

import (
	"fmt"
	"runtime"
	"time"
)

// darwinIdentityNative reports that this build does NOT link the libproc
// bridge — the native-walk tests must skip with an explicit reason
// instead of asserting behavior this build cannot provide.
const darwinIdentityNative = false

// listeningProcessImpl: port ownership cannot be proven without the
// native bridge — typed UNSUPPORTED (fail closed).
func listeningProcessImpl(port int) (int, string, bool, error) {
	return 0, "", false, fmt.Errorf("%w: darwin identity requires the cgo libproc bridge (CGO_ENABLED=0 build) on %s/%s",
		ErrPortOwnerUnsupported, runtime.GOOS, runtime.GOARCH)
}

// processExePathImpl: image-path identity is not linked in this build —
// typed UNSUPPORTED.
func processExePathImpl(pid int) (string, error) {
	return "", fmt.Errorf("%w (pid %d): darwin identity requires the cgo libproc bridge (CGO_ENABLED=0 build)",
		ErrProcessMetadataUnsupported, pid)
}

// processStartTimeImpl: start identity is not linked in this build —
// typed UNSUPPORTED.
func processStartTimeImpl(pid int) (time.Time, error) {
	return time.Time{}, fmt.Errorf("%w (pid %d): darwin identity requires the cgo libproc bridge (CGO_ENABLED=0 build)",
		ErrProcessMetadataUnsupported, pid)
}
