//go:build ios

package proc

// iOS contained identity provider (run 44547031698 §5.3): an iOS
// application runs inside the Apple sandbox — process-wide enumeration
// and foreign-process port-ownership inspection are neither permitted nor
// meaningful to the app runtime. The provider reports the typed
// UNSUPPORTED/contained answers so the identity contract stays fail-closed
// on the iOS target: adoption never proceeds on unprovable identity, and
// nothing here conflates the macOS runner host with the iOS target binary.
//
// The macOS-host test jobs exercise the REAL macOS walk
// (identity_darwin.go, build constraint darwin && !ios); this file is the
// GOOS=ios compilation of the same contract.

import (
	"fmt"
	"runtime"
	"time"
)

// darwinIdentityNative reports that the GOOS=ios build does not link the
// macOS desktop walk — the native-walk tests are darwin-desktop scoped.
const darwinIdentityNative = false

// listeningProcessImpl: the iOS sandbox prevents process-wide port
// ownership inspection — typed UNSUPPORTED (fail closed).
func listeningProcessImpl(port int) (int, string, bool, error) {
	return 0, "", false, fmt.Errorf("%w: the iOS sandbox does not permit process-wide port ownership inspection (contained runtime) on %s/%s",
		ErrPortOwnerUnsupported, runtime.GOOS, runtime.GOARCH)
}

// processExePathImpl: foreign image-path identity is not inspectable
// inside the iOS sandbox — typed UNSUPPORTED.
func processExePathImpl(pid int) (string, error) {
	return "", fmt.Errorf("%w (pid %d): the iOS sandbox does not permit foreign process path inspection (contained runtime)",
		ErrProcessMetadataUnsupported, pid)
}

// processStartTimeImpl: foreign start identity is not inspectable inside
// the iOS sandbox — typed UNSUPPORTED.
func processStartTimeImpl(pid int) (time.Time, error) {
	return time.Time{}, fmt.Errorf("%w (pid %d): the iOS sandbox does not permit foreign process start identity inspection (contained runtime)",
		ErrProcessMetadataUnsupported, pid)
}
