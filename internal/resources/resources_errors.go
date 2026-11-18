package resources

import "errors"

// ErrProcRAMUnsupported is the typed platform limitation: no supported
// process-RAM probe exists for the requested PID in this build
// configuration (work order §4.6: missing values must never be silently
// represented as zero or fabricated — callers render "unknown").
var ErrProcRAMUnsupported = errors.New("process RAM probe unsupported for this pid/platform")
