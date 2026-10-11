package llm

// v1.3.6 (spec §3): IDENTITY-PROVEN ADOPTION. The old adoptExisting()
// treated any process answering /health with HTTP 200 as "our engine".
// A foreign llama.cpp, a previous SHEYTAN instance, or a stale
// installation could be silently adopted — the suspected source of the
// observed b10642 → b11090 → b10642 version flap.
//
// Adoption now requires PROOF:
//
//  1. the endpoint answers /health (necessary, not sufficient);
//  2. the OS attributes the listening port to a PID (real APIs —
//     GetExtendedTcpTable on Windows, /proc/net/tcp on Linux; never
//     cmd.exe/PowerShell/tasklist parsing);
//  3. that PID's executable path matches the EXPECTED managed engine
//     path for this installation.
//
// When the port is occupied but identity cannot be proven, adoption is
// REFUSED, the process is never killed automatically, and the diagnostic
// carries the observed PID + executable so the user can act.

import (
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/engcheck"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// installedEngineTagFunc is the indirection used by diagnostics; tests
// may stub it.
var installedEngineTagFunc = updater.InstalledEngineTag

// expectedEngineBinPath is THE single engine binary path resolution for
// the llama.cpp server (spec §14/§40): cfg.LlamaBinPath when explicitly
// configured, else <DataDir>/bin/<llamaBinaryName()>. All consumers
// (adoption proof, provisioning, diagnostics) resolve through here so
// the derivation cannot drift between call sites.
func expectedEngineBinPath(cfg *config.Config) string {
	if cfg.LlamaBinPath != "" {
		return cfg.LlamaBinPath
	}

	return filepath.Join(cfg.DataDir, "bin", llamaBinaryName())
}

// adoptExisting reports whether the port is already served by OUR OWN
// managed engine — proven via process identity, not just a health 200.
//
// Return semantics:
//   - true:  identity proven; the caller may treat the engine as ready.
//   - false: not adoptable (nothing on the port, foreign process, or
//     unprovable identity). Evidence is logged either way.
func (s *LlamaServer) adoptExisting(cfg *config.Config) bool {
	url := fmt.Sprintf(
		"http://%s:%d/health",
		cfg.LlamaHost,
		cfg.LlamaPort,
	)

	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return false
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	// The endpoint answers. Now PROVE the process behind it.
	pid, exe, ok, perr := proc.ListeningProcess(cfg.LlamaPort)

	if !ok {
		// Nothing attributable is listening (the health answer may have
		// raced a dying process) — refuse.
		s.logf("adoption refused: /health answered but no listening PID could be attributed on port %d (err=%v)", cfg.LlamaPort, perr)

		return false
	}

	if perr != nil || exe == "" {
		logging.Default().Warn("engine",
			"engine port %d is occupied by pid %d whose executable identity could not be proven (%v) — adoption refused; close that program or change LlamaPort",
			cfg.LlamaPort, pid, perr)

		s.logf("adoption refused: listener pid %d on port %d could not be identified — foreign or protected process", pid, cfg.LlamaPort)

		return false
	}

	expected := expectedEngineBinPath(cfg)

	if !proc.SameExecutable(exe, expected) {
		logging.Default().Warn("engine",
			"engine port %d is occupied by a FOREIGN process: pid %d (%s) — expected the managed engine at %s. Not adopting, not killing; change LlamaPort or close that program",
			cfg.LlamaPort, pid, exe, expected)

		s.logf("adoption refused: pid %d serves %s — not the managed engine (%s)", pid, exe, expected)

		s.mu.Lock()
		s.recordFailureReportLocked(&EngineFailureReport{
			Phase:     "adoption",
			AttemptID: nextAttemptID(),
			Context:   AttemptFirstLaunch,
			OS:        goOS,
			Arch:      goArch,
			PID:       pid,
			ClassSummary: fmt.Sprintf(
				"port %d held by foreign process pid %d (%s); managed engine expected at %s",
				cfg.LlamaPort, pid, exe, expected,
			),
			At: time.Now().UTC(),
		})
		s.mu.Unlock()

		return false
	}

	// v1.3.6 (spec §17): ENGINE IDENTITY — path equality alone cannot
	// distinguish the recorded installed build from an engine swapped in
	// place at the same path. When an install manifest exists, the
	// on-disk binary must hash-match the recorded SHA-256, and (when
	// provable) the process must have STARTED after that install was
	// committed. A stale pre-update engine is refused, never killed.
	if !s.proveEngineIdentity(cfg, expected, pid) {
		return false
	}

	logging.Default().Info("engine",
		"adopting managed engine on port %d: pid %d serves %s (identity proven)",
		cfg.LlamaPort, pid, exe)

	s.logf("adopting managed engine: pid %d serves %s", pid, exe)

	return true
}

// engineDiagnostics aggregates the diagnostic surface (spec §7).
func (s *LlamaServer) EngineDiagnostics(cfg *config.Config) EngineDiagnostics {
	s.mu.Lock()

	diag := EngineDiagnostics{
		State:        s.state,
		Detail:       s.detail,
		PID:          s.currentPIDLocked(),
		RestartCount: s.restarts,
	}

	if s.lastFailure != nil {
		rep := *s.lastFailure
		diag.Failure = &rep
		diag.LastExitCode = rep.ExitCode
	}

	s.mu.Unlock()

	if pf := s.enginePreflightSnapshot(); pf != nil {
		diag.LastPreflight = pf
		diag.BinaryPath = pf.Path
		diag.ProbedVer = pf.ProbedVer
	}

	if cfg != nil {
		diag.BinaryTag = updaterInstalledTag(cfg)
	}

	diag.RecentLogs = s.Logs()
	diag.RecentErrors = s.ErrorLogs()

	return diag
}

// updaterInstalledTag is a tiny indirection so diagnostics.go does not
// need the full updater import graph in tests.
func updaterInstalledTag(cfg *config.Config) string {
	return installedEngineTagFunc(cfg)
}

// currentPIDLocked reads the live PID (caller holds mu).
func (s *LlamaServer) currentPIDLocked() int {
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}

	return 0
}

// ErrorLogs returns the captured engine stderr ring.
func (s *LlamaServer) ErrorLogs() []string {
	return s.errRing.lines()
}

// proveEngineIdentity verifies the RUNNING engine matches the recorded
// installed build (spec §17: "installation identity = expected, engine
// identity = expected"):
//
//  1. when an install manifest exists, the on-disk binary must hash to
//     the recorded SHA-256;
//  2. when the manifest's install time AND the process start time are
//     both provable, the process must have started at/after the install
//     commit (a process from BEFORE the last install is a stale engine).
//
// Honest degradation (spec §20): a missing manifest or an unprovable
// start time is logged explicitly — never faked — and adoption then
// rests on the proven path + health + PID evidence.
func (s *LlamaServer) proveEngineIdentity(cfg *config.Config, exePath string, pid int) bool {
	manifest, ok := updater.ReadInstallManifest(cfg)
	if !ok {
		s.logf("engine identity check unavailable: no install manifest for %s — adoption rests on path/health/PID proof", exePath)

		return true
	}

	ident, err := engcheck.IdentityOf(exePath)
	if err != nil {
		s.logf("engine identity check unavailable (%v) — adoption rests on path/health/PID proof", err)

		return true
	}

	if manifest.SHA256 != "" && ident.SHA256 != manifest.SHA256 {
		logging.Default().Warn("engine",
			"adoption refused: the binary at %s (sha256 %.12s…) does not match the recorded install manifest (sha256 %.12s…) — a different build replaced or shadowed the managed engine",
			exePath, ident.SHA256, manifest.SHA256)

		s.logf("adoption refused: engine identity mismatch at %s", exePath)

		return false
	}

	installedAt, terr := time.Parse(time.RFC3339, manifest.InstalledAt)
	startedAt, perr := proc.ProcessStartTime(pid)

	if terr == nil && perr == nil && !startedAt.IsZero() {
		if startedAt.Add(-2 * time.Second).Before(installedAt) {
			logging.Default().Warn("engine",
				"adoption refused: the engine process (pid %d) was started %s, BEFORE the recorded install at %s — stale engine from a previous package",
				pid, startedAt.Format(time.RFC3339), installedAt.Format(time.RFC3339))

			s.logf("adoption refused: stale engine process predates the recorded install")

			return false
		}
	} else {
		s.logf("engine start-time vs install-time check unavailable (install=%v ok=%v, process=%v ok=%v) — identity rests on the SHA-256 match",
			manifest.InstalledAt, terr == nil, startedAt.IsZero(), perr == nil)
	}

	return true
}

// ExpectedEngineBinPath is the exported single engine-binary resolution
// (delegates to expectedEngineBinPath). v1.5.0: the selection/calibration
// flow uses it to decide whether the llama engine can be driven through
// restart cycles — an engine that cannot be re-launched locally (e.g. an
// adopted instance with no managed binary) must not be benchmarked.
func ExpectedEngineBinPath(cfg *config.Config) string {
	return expectedEngineBinPath(cfg)
}
