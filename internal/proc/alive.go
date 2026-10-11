package proc

// Process liveness + start identity (v1.3.6, spec §16): a per-install
// engine owner must be verifiable before its lease can be trusted or
// stolen. A PID alone is NOT an identity — operating systems recycle
// PIDs, so the verification triple is
//
//	alive + executable path + process start time.
//
// This file carries the shared contract; alive_unix.go and
// alive_windows.go carry the per-OS implementations. No shell parsing
// is involved anywhere (spec §18).

import (
	"errors"
	"time"
)

// ErrProcessDead is returned when the PID exists in the lease record but
// no live process answers behind it.
var ErrProcessDead = errors.New("process is not alive")

// ProcessAlive reports whether a live process answers behind pid.
// A recycled PID still reports true — callers MUST pair this with
// exe-path and start-time checks to bind the PID to an identity.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	return processAliveImpl(pid)
}

// ProcessStartTime returns when the process behind pid was started.
// The zero time means the start time could not be proven — callers
// must treat "unprovable" as NOT matching any recorded start time.
func ProcessStartTime(pid int) (time.Time, error) {
	if pid <= 0 {
		return time.Time{}, ErrProcessNotFound
	}

	return processStartTimeImpl(pid)
}

// SameStartIdentity binds a PID to a specific process incarnation:
// the recorded start time (when one was recorded) must match the live
// process start time. When either side is unprovable the check is
// deliberately conservative: identity is NOT proven.
func SameStartIdentity(pid int, recorded time.Time) bool {
	if recorded.IsZero() {
		// No baseline was recorded — cannot prove identity.
		return false
	}

	live, err := ProcessStartTime(pid)
	if err != nil || live.IsZero() {
		return false
	}

	// Jiffy/tick granularity: allow a small skew window.
	delta := live.Sub(recorded)
	if delta < 0 {
		delta = -delta
	}

	return delta <= 2*time.Second
}
