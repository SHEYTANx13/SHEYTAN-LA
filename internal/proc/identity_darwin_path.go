//go:build darwin

package proc

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

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
//
// Run 45522548507 repair: when an executable image is UNLINKED while the
// process runs (the actions/runner-images#14756 class on the macos-15
// runner images), filepath.EvalSymlinks fails on BOTH sides and the raw
// strings can differ only by the macOS /private symlink alias
// (proc_pidpath answers the canonical /private/var/... vnode path while
// os.Executable answers the /var/... exec path — the boundary the
// run 45086860202 repair documented). When both sides RESOLVE, the
// comparison stays exact: two successfully resolved but different paths
// are different files, and the alias fallback never runs.
func sameExecutableImpl(a, b string) bool {
	ra, aerr := filepath.EvalSymlinks(a)
	if aerr == nil {
		a = ra
	}

	rb, berr := filepath.EvalSymlinks(b)
	if berr == nil {
		b = rb
	}

	if a == b {
		return true
	}

	if aerr == nil && berr == nil {
		// Both paths resolved to real files and differ: different images.
		return false
	}

	// At least one image is unresolvable (the unlinked-image class): the
	// surviving raw strings are compared lexically, with the /private
	// symlink alias normalized in both directions.
	ca, cb := filepath.Clean(a), filepath.Clean(b)

	return ca == cb || ca == "/private"+cb || cb == "/private"+ca
}

// ---------------------------------------------------------------------------
// Bounded walk diagnostics (run 45424141601 repair).
//
// The darwin attribution family failed on every hosted macOS lane for six
// consecutive runs while every locally-runnable check stayed green: the
// walk's failure stage (kernel enumeration, per-record decode, or owner
// path resolution) was never NAMED in the evidence the suite emitted, so
// each repair session guessed. The contract "no failure without a specific
// explanation" therefore lives IN the walk now: every ErrPortOwnerUnknown
// answer carries this bounded, single-line, typed-class-only diagnostic —
// no pids beyond counts, no paths, no hostnames, no raw process names —
// so a hosted failure message identifies the broken stage exactly.
// Pure Go, shared by every darwin build variant; the native bridge fills
// it, the CGO-free and iOS builds never produce one (no walk).
// ---------------------------------------------------------------------------

// Typed walk states (fail-closed contract). Pure Go — carried here so
// every darwin build variant (native bridge, CGO_ENABLED=0 type-checks,
// iOS) sees the same classes; the native bridge in identity_darwin.go is
// their only PRODUCER (run 45424141601 repair: the walk diagnostic and
// its tests must compile in the CGO-free darwin type-check builds too).
var (
	// errProcGone: the candidate process exited during inspection.
	errProcGone = errors.New("process exited during inspection")

	// errProcAccessDenied: the candidate process cannot be inspected by
	// this (unprivileged, same-user) caller.
	errProcAccessDenied = errors.New("process inspection denied")

	// errProcTruncated: the kernel answer did not fit the buffer even
	// after growth retries — the walk refuses to run on partial data.
	errProcTruncated = errors.New("process descriptor list truncated")

	// errSocketKindUndocumented: a socket record's soi_kind is outside
	// every kind the installed SDK documents — the union layout is
	// unknown, so the walk refuses the record instead of interpreting
	// any union member as TCP data (run 44835425398 repair: fail closed,
	// never a guessed port).
	errSocketKindUndocumented = errors.New("socket record carries an undocumented kind")
)

// diagClassBound caps one error-class bucket map and diagTextBound caps
// one rendered class string — the diagnostic is contractually bounded.
const (
	diagClassBound = 4
	diagTextBound  = 64
)

// procWalkDiagnostic is the bounded failure evidence of ONE walk.
type procWalkDiagnostic struct {
	EnumerationError string         // "" when the pid enumeration succeeded; its typed class otherwise
	PIDs             int            // pids the kernel enumerated (0 when enumeration failed)
	SelfPresent      bool           // the walk's own pid was in the enumerated list
	CandidatesRead   int            // candidates whose fd list was read successfully
	SelfSocketFDs    int            // socket fds seen on the walk's own process
	AccessDenied     int            // candidates (or single descriptors) this caller may not inspect
	Gone             int            // candidates that exited mid-walk
	ListErrors       map[string]int // candidate fd-list error class -> count (bounded)
	RecordErrors     map[string]int // per-record decode error class -> count (bounded)
	PathErrors       map[string]int // owner path-resolution error class -> count (bounded)
}

// recordDiagError counts one typed error into a bounded class bucket.
// The map is capped at diagClassBound entries; beyond it the count is
// folded into the existing "other" bucket so the line can never grow.
func recordDiagError(m map[string]int, err error) {
	if err == nil {
		return
	}

	class := errorClassOf(err)
	if _, ok := m[class]; !ok && len(m) >= diagClassBound {
		class = "other"
	}

	m[class]++
}

// errorClassOf renders a SHORT typed class name for one error — the
// stable walk classes by identity, kernel errnos by their name, and any
// other error truncated to diagTextBound characters. It never carries a
// path, a hostname or a process name.
func errorClassOf(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errProcGone):
		return "gone"
	case errors.Is(err, errProcAccessDenied):
		return "access-denied"
	case errors.Is(err, errProcTruncated):
		return "truncated"
	case errors.Is(err, errSocketKindUndocumented):
		return "undocumented-socket-kind"
	case errors.Is(err, ErrProcessPathUnresolvable):
		return "path-unresolvable"
	case errors.Is(err, ErrProcessNotFound):
		return "not-found"
	case errors.Is(err, ErrPortOwnerUnsupported):
		return "unsupported"
	}

	var errno syscall.Errno
	if errors.As(err, &errno) {
		return "errno-" + errno.Error()
	}

	text := err.Error()
	if len(text) > diagTextBound {
		text = text[:diagTextBound]
	}

	return "error(" + text + ")"
}

// renderProcWalkDiagnostic renders the diagnostic as ONE bounded ASCII
// line (the consolidated-log collector normalizes separators; this line
// additionally never contains "===" or "---" runs). Bounded: the two
// largest possible renders carry diagClassBound class buckets each.
func renderProcWalkDiagnostic(d *procWalkDiagnostic) string {
	if d == nil {
		return ""
	}

	var b strings.Builder

	b.WriteString("walk-diag pids=")
	fmt.Fprintf(&b, "%d self=%t self_sockets=%d walked=%d denied=%d gone=%d",
		d.PIDs, d.SelfPresent, d.SelfSocketFDs, d.CandidatesRead, d.AccessDenied, d.Gone)

	if d.EnumerationError != "" {
		fmt.Fprintf(&b, " enum_err=%s", d.EnumerationError)
	}

	renderBuckets := func(label string, m map[string]int) {
		if len(m) == 0 {
			return
		}

		fmt.Fprintf(&b, " %s=[", label)

		first := true

		for _, k := range sortedClassKeys(m) {
			if !first {
				b.WriteByte(',')
			}

			first = false
			fmt.Fprintf(&b, "%s=%d", k, m[k])
		}

		b.WriteByte(']')
	}

	renderBuckets("list_err", d.ListErrors)
	renderBuckets("record_err", d.RecordErrors)
	renderBuckets("path_err", d.PathErrors)

	return b.String()
}

// sortedClassKeys returns the bucket keys in deterministic order (Go map
// iteration is randomized; the rendered line must be stable for the
// collector's byte-identical report contract).
func sortedClassKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
