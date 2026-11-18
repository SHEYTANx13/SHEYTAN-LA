//go:build darwin

package proc

import (
	"fmt"
	"time"
)

// processStartTimeImpl on darwin: no start-time probe is linked in this
// build (the Linux /proc/stat path does not exist on macOS). The typed
// unsupported result keeps the operation fail-closed — callers surface
// "unknown", never a fabricated timestamp (run 44306343338 repair §4.7).
func processStartTimeImpl(pid int) (time.Time, error) {
	return time.Time{}, fmt.Errorf("%w (pid %d): darwin start-time probe requires the libproc/sysctl path, which this build does not link", ErrProcessMetadataUnsupported, pid)
}
