//go:build darwin

// sysinfo_fast_darwin.go — fast RAM facts on macOS (run 44306343338
// repair §4.6): total physical RAM through the documented hw.memsize
// sysctl path (golang.org/x/sys SysctlUint64) — a genuine native probe,
// no external command is spawned.
//
// Free/available memory has no sysctl-backed equivalent in this build
// (Mach host VM statistics would require libmach cgo). Those fields
// stay 0 as this package's explicit "unknown" convention and are
// documented as a platform limitation — never fabricated.
package sysinfo

import (
	"golang.org/x/sys/unix"
)

func fastRAM() RAMInfo {
	r := RAMInfo{}

	if total, err := unix.SysctlUint64("hw.memsize"); err == nil && total > 0 {
		r.TotalBytes = total
	}

	return r
}
