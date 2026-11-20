//go:build darwin

package proc

import "path/filepath"

// localPortOf decodes insi_lport. The kernel stores the 16-bit port in
// NETWORK byte order (the convention every public PROC_PIDFDSOCKETINFO
// consumer applies via ntohs); both darwin architectures are
// little-endian, so the byte-swapped low half is the host port. Pure Go —
// the decoding convention is pinned by TestLocalPortOfNetworkOrderConvention
// in every darwin build variant (native bridge, CGO-free, iOS).
func localPortOf(raw int32) int {
	v := uint32(raw)

	if v > 0xFFFF {
		// A well-formed entry never sets the upper half; treat it as
		// unusable evidence rather than guessing.
		return 0
	}

	return int(v>>8&0xFF | v<<8&0xFF00)
}

// sameExecutableImpl on darwin resolves symlinks and compares paths
// (darwin is case-sensitive, unlike the Windows implementation). Pure Go —
// shared by the native libproc bridge, the CGO_ENABLED=0 fallback and the
// iOS contained provider (GOOS=ios satisfies the darwin constraint).
func sameExecutableImpl(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}

	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}

	return a == b
}
