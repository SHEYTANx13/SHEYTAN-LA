package llm

// v1.3.6 (spec §4/§7): first-class engine diagnostics. Every startup
// attempt records an EngineFailureReport when it dies: exit code,
// decoded Windows loader class, binary identity, stream tails and the
// lifecycle context of the attempt. EngineDiagnostics() aggregates the
// full state for /api/engine and the UI.

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// AttemptContext describes WHO initiated the failed attempt.
type AttemptContext string

const (
	AttemptFirstLaunch AttemptContext = "first-launch"
	AttemptRestart     AttemptContext = "restart"
	AttemptWatchdog    AttemptContext = "watchdog"
	AttemptUpdater     AttemptContext = "updater"
	AttemptRepair      AttemptContext = "repair"
)

// EngineFailureReport is the recorded evidence of one failed startup
// attempt (spec §4 minimum capture list).
type EngineFailureReport struct {
	// Phase names the lifecycle phase that failed
	// ("preflight" | "launch" | "readiness").
	Phase string `json:"phase"`

	// AttemptID increments per launch attempt this process lifetime.
	AttemptID uint64 `json:"attemptId"`

	// Generation is the lifecycle episode token at failure time.
	Generation uint64 `json:"generation"`

	// Context names what initiated the attempt.
	Context AttemptContext `json:"context"`

	// Executable evidence.
	ExePath   string `json:"exePath"`
	ExeSHA256 string `json:"exeSha256,omitempty"`
	ExeSize   int64  `json:"exeSize,omitempty"`
	ExeMtime  int64  `json:"exeMtimeUnix,omitempty"`

	// Version evidence: what bookkeeping recorded vs what was probed.
	ExpectedTag string `json:"expectedTag,omitempty"`
	ProbedVer   string `json:"probedVersion,omitempty"`

	// Process/exit evidence.
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	PID         int    `json:"pid,omitempty"`
	ExitCode    int    `json:"exitCode,omitempty"`
	ExitCodeHex string `json:"exitCodeHex,omitempty"`

	// Decoded Windows class (empty when not a loader failure).
	FailureClass string `json:"failureClass,omitempty"`
	ClassSummary string `json:"classSummary,omitempty"`

	// Dependency closure evidence (Windows PE).
	MissingDeps []string `json:"missingDeps,omitempty"`

	// Output tails.
	StdoutTail string `json:"stdoutTail,omitempty"`
	StderrTail string `json:"stderrTail,omitempty"`

	// Launch facts.
	ModelPath   string    `json:"modelPath,omitempty"`
	CompatLevel int       `json:"compatLevel,omitempty"`
	At          time.Time `json:"at"`
}

// EngineDiagnostics is the /api/engine diagnostics block (spec §7 API
// surface): the real state, identity, failure classification and the
// recent engine output — everything the UI needs to show the truth
// instead of a spinner.
type EngineDiagnostics struct {
	State        string `json:"state"`
	Detail       string `json:"detail,omitempty"`
	PID          int    `json:"pid,omitempty"`
	BinaryPath   string `json:"binaryPath,omitempty"`
	BinaryTag    string `json:"binaryTag,omitempty"`
	ProbedVer    string `json:"probedVersion,omitempty"`
	RestartCount int    `json:"restartCount,omitempty"`
	LastExitCode int    `json:"lastExitCode,omitempty"`

	Failure *EngineFailureReport `json:"failure,omitempty"`

	LastPreflight *EnginePreflight `json:"lastPreflight,omitempty"`

	RecentLogs   []string `json:"recentLogs,omitempty"`
	RecentErrors []string `json:"recentErrors,omitempty"`
}

var (
	goOS           = runtime.GOOS
	goArch         = runtime.GOARCH
	attemptCounter atomic.Uint64
)

func nextAttemptID() uint64 {
	return attemptCounter.Add(1)
}

// exitCodeHex renders a process exit code in the canonical 0x…… form
// used across engine diagnostics (spec §19: record exit code AND hex).
func exitCodeHex(code int) string {
	if code == 0 {
		return ""
	}

	if code < 0 {
		// Go renders wait status negatives; normalize to unsigned NTSTATUS-style.
		return fmt.Sprintf("0x%08X", uint32(uint(code)))
	}

	return fmt.Sprintf("0x%08X", uint32(code))
}

// engineLeaseDir resolves the cross-process lease directory for a
// config (<DataDir>/run — single derivation shared with the updater).
func engineLeaseDir(cfg *config.Config) string {
	return filepath.Join(cfg.DataDir, "run")
}

// recordFailureReport stores one failure report (nil-safe on the server).
func (s *LlamaServer) recordFailureReportLocked(rep *EngineFailureReport) {
	if rep == nil {
		return
	}

	s.lastFailure = rep
}

// recordFailureReport builds a report from the current preflight evidence
// and a loader classification, then stores it.
func (s *LlamaServer) recordFailureReport(pf *EnginePreflight, lf loaderFailure, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rep := &EngineFailureReport{
		Phase:        "preflight",
		AttemptID:    nextAttemptID(),
		Context:      AttemptFirstLaunch,
		OS:           goOS,
		Arch:         goArch,
		FailureClass: string(lf.Kind),
		ClassSummary: lf.Summary + (func() string {
			if detail != "" {
				return " — " + detail
			}
			return ""
		})(),
		At: time.Now().UTC(),
	}

	if pf != nil {
		rep.ExePath = pf.Path
		rep.ExeSHA256 = pf.Identity.SHA256
		rep.ExeSize = pf.Identity.Size
		rep.ExeMtime = pf.Identity.ModTime
		rep.ProbedVer = pf.ProbedVer
		rep.MissingDeps = pf.Deps.Missing
		rep.StderrTail = pf.ProbeOut
	}

	s.lastFailure = rep
}
