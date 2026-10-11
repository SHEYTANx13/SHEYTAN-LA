//go:build !windows

// Per-invocation process-tree ownership for custom command tools
// (v1.6.0 repair, spec §4).
//
// The Windows implementation uses a Job Object so timeout/cancellation
// terminates the complete descendant tree and releases the inherited
// stdout/stderr pipes immediately. On non-Windows platforms the
// existing exec.CommandContext semantics (direct-process kill, which
// the passing Linux/macOS tests rely on) are preserved unchanged: the
// tracker is a no-op.
package customtools

import "os"

type processTree struct{}

func newProcessTree() *processTree { return &processTree{} }

func (t *processTree) attach(*os.Process) {}
func (t *processTree) terminate()         {}
func (t *processTree) close()             {}
