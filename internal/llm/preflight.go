package llm

// v1.3.6 (spec §6): ENGINE PRE-FLIGHT — before the first real model
// startup the candidate binary must pass:
//
//      locate → exists → architecture → dependency closure →
//      bounded identity/version probe → record exact binary identity
//
// If the preflight itself observes a loader failure (0xC0000139 and
// friends), the full engine is never started: the failure is fast,
// classified and actionable instead of burning the compatibility ladder
// on a binary that cannot load (spec §5).
//
// The preflight probe uses the same proc.Command runner as the real
// launch path so behavior cannot drift between the two.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/engcheck"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
)

// preflightProbeTimeout bounds the --version identity probe.
const preflightProbeTimeout = 15 * time.Second

// EnginePreflight is the recorded evidence of one preflight pass.
type EnginePreflight struct {
	Path       string                    `json:"path"`
	Identity   engcheck.Identity         `json:"identity"`
	Deps       engcheck.DependencyReport `json:"deps"`
	Probed     bool                      `json:"probed"`
	ProbedVer  string                    `json:"probedVersion,omitempty"`
	ProbeExit  int                       `json:"probeExit,omitempty"`
	ProbeOut   string                    `json:"probeOutput,omitempty"`
	At         time.Time                 `json:"at"`
	DurationMs int64                     `json:"durationMs"`

	// DepsUnavailable and ProbeUnavailable carry the v1.3.6 (spec §20)
	// honesty contract: when dependency inspection or the version probe
	// could not produce evidence, the REASON is surfaced here instead of
	// the report silently reading as "no deps" / "no version". Never
	// faked, never guessed.
	DepsUnavailable  string `json:"depsUnavailable,omitempty"`
	ProbeUnavailable string `json:"probeUnavailable,omitempty"`
}

// preflightFailure is a typed error: the candidate failed BEFORE any
// model launch. Loader-class failures carry the decoded Windows class.
type preflightFailure struct {
	Path     string
	Identity engcheck.Identity
	Deps     engcheck.DependencyReport
	Loader   LoaderFailureKind
	Detail   string
	err      error
}

func (e *preflightFailure) Error() string {
	msg := "engine preflight failed for " + e.Path

	if e.Detail != "" {
		msg += ": " + e.Detail
	}

	if e.err != nil && e.Detail != "" {
		msg += fmt.Sprintf(" (%v)", e.err)
	} else if e.err != nil {
		msg += ": " + e.err.Error()
	}

	return msg
}

func (e *preflightFailure) Unwrap() error { return e.err }

// isLoaderFailure reports whether err is a deterministic binary-loader
// failure (from preflight or from a classified process exit). Such
// failures must never enter the compatibility ladder (spec §5).
func isLoaderFailure(err error) bool {
	if err == nil {
		return false
	}

	var pf *preflightFailure
	if errors.As(err, &pf) {
		return pf.Loader != LoaderNone
	}

	var ef *exitFailure
	if errors.As(err, &ef) && ef != nil {
		if _, ok := classifyLoaderExit(ef.code); ok {
			return true
		}

		// Textual evidence fallback (v1.3.6): when the OS exit code is
		// truncated (Unix keeps 8 bits) the loader's own NTSTATUS text in
		// the captured stderr tail is still first-class evidence. This
		// keeps classification deterministic cross-platform.
		for _, line := range ef.tail {
			l := strings.ToLower(line)

			if strings.Contains(l, "status_entrypoint_not_found") ||
				strings.Contains(l, "status_dll_not_found") ||
				strings.Contains(l, "0xc0000139") ||
				strings.Contains(l, "0xc0000135") {
				return true
			}
		}
	}

	return false
}

// exitCodeOf extracts the process exit code from an exec error when the
// standard library exposes one.
func exitCodeOf(err error) *int {
	type exitCoder interface{ ExitCode() int }

	if ec, ok := err.(exitCoder); ok {
		code := ec.ExitCode()
		return &code
	}

	return nil
}

// preflightBinary runs the full static + probe gate on binPath. On
// success it returns the recorded identity evidence; on failure the
// returned *preflightFailure already carries every field the UI needs.
func (s *LlamaServer) preflightBinary(cfg *config.Config, binPath string) (*EnginePreflight, *preflightFailure) {
	started := time.Now()

	pf := &EnginePreflight{
		Path: binPath,
		At:   started,
	}

	fail := func(detail string, kind LoaderFailureKind, err error) *preflightFailure {
		return &preflightFailure{
			Path:     binPath,
			Identity: pf.Identity,
			Deps:     pf.Deps,
			Loader:   kind,
			Detail:   detail,
			err:      err,
		}
	}

	// 1) static gate: exists, non-empty, executable bit, recognizable
	// format, host architecture.
	ident, err := engcheck.StaticValidate(binPath)
	pf.Identity = ident

	if err != nil {
		kind := LoaderNone
		if errors.Is(err, engcheck.ErrArchMismatch) {
			kind = LoaderBadImage
		}

		return nil, fail(err.Error(), kind, err)
	}

	// 2) dependency closure (PE only; ELF/other = nothing to check).
	deps, err := engcheck.CheckDependencies(binPath)
	pf.Deps = deps

	if err != nil {
		pf.DepsUnavailable = fmt.Sprintf("dependency validation unavailable: %v", err)

		s.logf("preflight dependency inspection unavailable for %s: %v", filepath.Base(binPath), err)
	}

	if len(deps.Missing) > 0 {
		missing := strings.Join(deps.Missing, ", ")
		detail := fmt.Sprintf(
			"missing runtime DLLs beside the executable: %s — the engine package is incomplete (mixed or partial install)",
			missing,
		)

		return nil, fail(detail, LoaderDLLNotFound, nil)
	}

	// 3) bounded identity probe (--version): real version evidence for
	// the running binary (spec §34) — never trusted as configuration,
	// only recorded (spec §12).
	ctx, cancel := context.WithTimeout(context.Background(), preflightProbeTimeout)
	defer cancel()

	cmd := proc.CommandContext(ctx, binPath, "--version")
	out, err := cmd.CombinedOutput()
	pf.Probed = true
	pf.ProbeExit = 0
	pf.ProbeOut = compactLines(strings.Split(string(out), "\n"), 6)
	pf.DurationMs = time.Since(started).Milliseconds()

	if err != nil {
		code := 0
		if ee := exitCodeOf(err); ee != nil {
			code = *ee
		}

		pf.ProbeExit = code

		lf, loader := classifyLoaderExit(code)
		if !loader {
			// Exit codes are truncated to 8 bits on Unix — a real
			// NTSTATUS never arrives as a code there. The loader's own
			// stderr text is then the classifiable evidence.
			lf, loader = loaderClassFromText(string(out))
		}

		if loader {
			detail := loaderDependencyDetail(pf)

			s.logf("preflight probe of %s exited 0x%08X — %s",
				filepath.Base(binPath), lf.Code, lf.Summary)

			s.recordFailureReport(pf, lf, detail)

			pfail := fail(loaderFailureError(lf, detail).Error(), lf.Kind, nil)
			pfail.Loader = lf.Kind

			return nil, pfail
		}

		// A non-zero --version from a binary that DID load is honest
		// evidence the executable runs; llama.cpp returns 0 on
		// --version, but tolerate non-loader failures without blocking
		// the boot — the compat ladder owns argument-level behavior.
		pf.ProbeUnavailable = fmt.Sprintf("version probe unavailable: %v", err)

		s.logf("preflight --version probe failed (non-loader): %v", err)
	} else {
		if v := firstNonEmptyLine(string(out)); v != "" {
			pf.ProbedVer = strings.TrimSpace(v)
		}
	}

	pf.DurationMs = time.Since(started).Milliseconds()

	s.mu.Lock()
	s.lastPreflight = pf
	s.mu.Unlock()

	s.logf("engine preflight ok: %s (%s/%s, sha256 %.12s…)",
		filepath.Base(binPath), runtime.GOOS, pf.Identity.Arch, pf.Identity.SHA256)

	return pf, nil
}

// loaderDependencyDetail renders the evidence block appended to loader
// failure diagnostics: identity + dependency closure state.
func loaderDependencyDetail(pf *EnginePreflight) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Binary: %s\n", pf.Path)
	fmt.Fprintf(&b, "Identity: sha256 %s, %d bytes\n", pf.Identity.SHA256, pf.Identity.Size)

	if pf.Deps.Format == "pe" {
		if len(pf.Deps.Present) > 0 {
			fmt.Fprintf(&b, "Runtime DLLs present: %s\n", strings.Join(pf.Deps.Present, ", "))
		}

		if len(pf.Deps.Missing) > 0 {
			fmt.Fprintf(&b, "Runtime DLLs missing: %s\n", strings.Join(pf.Deps.Missing, ", "))
		} else if len(pf.Deps.Present) > 0 || !pf.Deps.SystemOnly {
			fmt.Fprintf(&b, "All non-system imports resolved beside the executable.\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}

	return ""
}

// enginePreflightSnapshot returns a copy of the last recorded preflight
// (nil when none has run yet).
func (s *LlamaServer) enginePreflightSnapshot() *EnginePreflight {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastPreflight == nil {
		return nil
	}

	cp := *s.lastPreflight

	return &cp
}
