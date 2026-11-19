//go:build darwin

package proc

import (
	"errors"
	"fmt"
	"time"
)

// processStartTimeImpl on darwin: PROC_PIDTBSDINFO (proc_pidinfo) carries
// pbi_start_tvsec/pbi_start_tvusec — the absolute epoch start time of the
// process incarnation (run 44457547289 §1.3.a item 2: PID-reuse defense
// needs the real start identity, not a name). The answer is typed:
//   - a vanished PID is ErrProcessNotFound;
//   - an uninspectable PID is a typed denied error — callers see "unknown",
//     never a fabricated timestamp (run 44306343338 repair §4.7).
func processStartTimeImpl(pid int) (time.Time, error) {
	if pid <= 0 {
		return time.Time{}, ErrProcessNotFound
	}

	var pbi procBsdInfo

	buf := structBytes(&pbi)

	n, err := procInfoPIDInfo(int32(pid), procPIDTBSDInfo, 0, buf)
	if err != nil {
		if errors.Is(err, errProcAccessDenied) {
			return time.Time{}, fmt.Errorf("%w (pid %d): %v", ErrProcessMetadataUnsupported, pid, err)
		}

		if errors.Is(err, errProcGone) {
			return time.Time{}, ErrProcessNotFound
		}

		return time.Time{}, fmt.Errorf("darwin start-time probe for pid %d failed: %w", pid, err)
	}

	if n != len(buf) {
		return time.Time{}, fmt.Errorf("darwin start-time probe for pid %d returned %d of %d bytes", pid, n, len(buf))
	}

	return time.Unix(int64(pbi.StartTVSec), int64(pbi.StartTVUsec)*1_000).UTC(), nil
}
