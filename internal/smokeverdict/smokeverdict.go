package smokeverdict

// smokeverdict.go — v1.11.1 (run 43995689377) P0 fix: the split smoke
// truth contract.
//
// THE FALSE GREEN this package kills: the Windows smoke asserted the
// desktop SHELL (window, named-pipe IPC handshake, first core read,
// responsiveness soak, graceful close, shutdown records) and printed a
// generic
//
//      SMOKE OK: window booted, IPC handshake succeeded, …
//
// while the SAME app's data/logs/app.log recorded that managed-engine
// maintenance FAILED ("stage engine swap: rename of data\bin to
// data\bin.update-old returned Access is denied"), validation of the
// previous engine failed (llama-server.exe absent), maintenance entered
// BLOCKED with update-failed-engine-unusable and prewarm was REFUSED.
// A shell-only green is NOT a working AI desktop app.
//
// The verdict separates the two claims — and a third:
//
//      SHELL SMOKE OK; ENGINE NOT READY  → the run-43995689377 class:
//        the Windows AI test-ready gate must be RED with exactly this label.
//      SMOKE OK: shell + managed engine  → both gates green.
//      INFERENCE: NOT RUN                → no model was selected/executed;
//        engine health is never inference proof (separate claim, separate
//        gate, only run with a permitted deterministic test model).
//
// This package is the CONTRACT AUTHORITY: the GitHub workflow's
// PowerShell engine-ready gate mirrors these rules; the fixture tests
// here pin the exact observed false-green log shape so the two cannot
// drift silently.
//
// Pure functions only — no I/O, no clocks: deterministic on every OS.

import "strings"

// Shell evidence records (the app.log strings the smoke asserts).
//
// TWO shell eras carry the same three-stage contract (handshake, first
// core read, shutdown):
//   - the named-pipe era: the shell client dialed the windows named pipe
//     (ui/gogui bootstrap — the Android mobile shell today);
//   - the web era (run 45634345922 flip): the desktop shell is the
//     same-origin web shell — it owns NO client-side named-pipe dial, so
//     the desktop's handshake-equivalent is the REAL first core read the
//     web host performs through the exact served handler before the
//     window boots (internal/desktop, run 46050871166 repair). Both
//     transport tokens are accepted; each era's record set is internally
//     consistent and a mixed log still fails the stage it lies about.
const (
	ShellHandshakeRecord = "core handshake successful"
	ShellPipeTransport   = "transport=windows-named-pipe"
	ShellWebTransport    = "transport=same-origin-http"
	ShellFirstReadRecord = "first session refresh succeeded"
	ShellWebFirstRead    = "first core read succeeded"
	ShellIPCStopped      = "ipc stopped"
	ShellCoreClosed      = "server closed"
)

// Managed-engine maintenance records (internal/api/maintenance.go and
// internal/updater logging — the observed vocabulary).
const (
	EngineCurrentRecord    = "startup gate: engine is current"
	EngineVerifiedRecord   = "engine verified, prewarm released"
	EngineCommittedRecord  = "committed (sha256"
	EngineSwapFailedRecord = "stage engine swap"
	EngineUnusableRecord   = "update-failed-engine-unusable"
	EnginePrewarmBlocked   = "prewarm BLOCKED"
	EngineUpdateFailed     = "update to"
	EngineFailedValidOld   = "keeping the last-known-good engine"
	EngineDeferredRecord   = "startup gate: scheduled engine updates are off"
	EngineOfflineRecord    = "update check skipped (offline)"

	// EngineUpstreamUnavailableRecord: upstream llama.cpp publishes no
	// prebuilt engine asset for this platform (observed live on
	// linux/amd64) — an upstream availability limit, never a candidate
	// defect. The platform is honestly UI-ONLY-TEST, never TEST-READY,
	// while upstream lacks the asset.
	EngineUpstreamUnavailableRecord = "no recent release ships a prebuilt asset"
)

// EngineState is the managed-engine outcome class.
type EngineState string

const (
	// EngineStateInstalled: maintenance succeeded (fresh install or
	// update committed, or already current).
	EngineStateInstalled EngineState = "installed"

	// EngineStateUpdateFailed: maintenance failed AND the installed
	// engine is unusable (BLOCKED, prewarm refused) — the
	// run-43995689377 class. NOT engine-ready.
	EngineStateUpdateFailed EngineState = "update-failed"

	// EngineStateFailedValidOld: the update failed but the previous
	// engine still validates (last-known-good continues to serve).
	EngineStateFailedValidOld EngineState = "update-failed-engine-valid"

	// EngineStateDeferred: no maintenance this boot (schedule off /
	// offline / not due) — engine presence is proven by files, not logs.
	EngineStateDeferred EngineState = "deferred"

	// EngineStateUpstreamUnavailable: upstream ships no prebuilt engine
	// asset for this platform — the engine cannot be installed from
	// upstream at all. NOT engine-ready (never TEST-READY on this
	// platform while upstream lacks the asset), and NOT a candidate
	// defect: the failure belongs to upstream availability.
	EngineStateUpstreamUnavailable EngineState = "upstream-unavailable"

	// EngineStateUnknown: no maintenance record observed.
	EngineStateUnknown EngineState = "unknown"
)

// Verdict is the split smoke truth. ShellOK and EngineReady are
// INDEPENDENT claims; TestReadyOverall is their conjunction — the gate
// the Windows AI test-ready artifact must pass.
type Verdict struct {
	ShellOK     bool        `json:"shellOk"`
	EngineReady bool        `json:"engineReady"`
	EngineState EngineState `json:"engineState"`
	Reasons     []string    `json:"reasons"`
}

// TestReadyOverall reports whether the full AI test-ready contract holds
// (shell + managed engine). Inference is a SEPARATE, stricter gate and is
// never implied by this value.
func (v Verdict) TestReadyOverall() bool {
	return v.ShellOK && v.EngineReady
}

// Summary renders the one-line verdict the gate prints. The exact string
// "SHELL SMOKE OK; ENGINE NOT READY" is the run-43995689377 class label.
func (v Verdict) Summary() string {
	switch {
	case v.ShellOK && v.EngineReady:
		return "SMOKE OK: shell + managed engine"
	case v.ShellOK && !v.EngineReady:
		return "SHELL SMOKE OK; ENGINE NOT READY"
	default:
		return "SMOKE FAILED"
	}
}

// InferenceLabel states the inference claim honestly: without a real
// model execution it is NOT RUN — engine-binary health is never
// inference proof.
func InferenceLabel(modelSelected bool) string {
	if modelSelected {
		return "INFERENCE: model selected — execution proof required"
	}
	return "INFERENCE: NOT RUN (no model selected — engine health is not inference)"
}

// FromAppLog derives the verdict from the persisted app.log text.
//
// Rules (mirrored by the workflow's engine-ready gate — fixture tests
// here pin them):
//
//   - ShellOK requires the handshake record naming the named-pipe
//     transport, the first core read, and the shutdown records (ipc
//     stopped + server closed);
//   - EngineReady requires a maintenance SUCCESS record (current /
//     verified+released / committed) or the failed-but-valid last-known
//     good continuation, AND the absence of every failure record class
//     (swap failure, engine-unusable BLOCKED, prewarm refused).
func FromAppLog(logText string) Verdict {
	v := Verdict{EngineState: EngineStateUnknown}

	has := func(pattern string) bool {
		return strings.Contains(logText, pattern)
	}

	// --- shell evidence ---------------------------------------------------
	// The handshake record must name ONE shell-era transport and the
	// first-read record must belong to the SAME era — a log mixing eras
	// (a hand-edited or half-upgraded shell) still fails: the named-pipe
	// first-read record proves nothing about the web transport and vice
	// versa. The binding is per-record (line-level), not document-level:
	// the transport named BY the handshake line must be the transport the
	// first-read line belongs to. The shutdown records close the contract.
	handshakeEra := ""
	firstReadEra := ""

	for _, line := range strings.Split(logText, "\n") {
		if strings.Contains(line, ShellHandshakeRecord) {
			switch {
			case strings.Contains(line, ShellPipeTransport):
				handshakeEra = "pipe"
			case strings.Contains(line, ShellWebTransport):
				handshakeEra = "web"
			}
		}

		switch {
		case strings.Contains(line, ShellFirstReadRecord):
			firstReadEra = "pipe"
		case strings.Contains(line, ShellWebFirstRead):
			firstReadEra = "web"
		}
	}

	v.ShellOK = handshakeEra != "" && handshakeEra == firstReadEra &&
		has(ShellIPCStopped) && has(ShellCoreClosed)

	if !v.ShellOK {
		v.Reasons = append(v.Reasons, "shell evidence incomplete (handshake/first-read/shutdown records)")
	}

	// --- managed-engine evidence -------------------------------------------
	failed := false

	switch {
	case has(EngineUnusableRecord), has(EnginePrewarmBlocked), has(EngineSwapFailedRecord):
		failed = true
		v.EngineState = EngineStateUpdateFailed
		v.Reasons = append(v.Reasons,
			"engine maintenance failed and the installed engine is unusable (BLOCKED / prewarm refused / engine-swap failure)")

	case has(EngineCurrentRecord), has(EngineVerifiedRecord), has(EngineCommittedRecord):
		v.EngineState = EngineStateInstalled

	case has(EngineFailedValidOld):
		v.EngineState = EngineStateFailedValidOld

	case has(EngineDeferredRecord), has(EngineOfflineRecord):
		v.EngineState = EngineStateDeferred

	case has(EngineUpstreamUnavailableRecord):
		v.EngineState = EngineStateUpstreamUnavailable
		v.Reasons = append(v.Reasons,
			"upstream publishes no prebuilt engine asset for this platform — the engine cannot be installed from upstream (an upstream availability limit, not a candidate defect; the platform is UI-ONLY-TEST, never TEST-READY, while upstream lacks the asset)")

	case has(EngineUpdateFailed):
		// An update failure without an explicit swap/BLOCKED record and
		// without the valid-old continuation is treated as unproven —
		// never as ready.
		failed = true
		v.EngineState = EngineStateUpdateFailed
		v.Reasons = append(v.Reasons, "engine update failure recorded without a usable-engine continuation")
	}

	v.EngineReady = v.EngineState == EngineStateInstalled || v.EngineState == EngineStateFailedValidOld

	if !v.EngineReady && !failed {
		switch v.EngineState {
		case EngineStateDeferred:
			v.Reasons = append(v.Reasons, "engine maintenance deferred this boot (schedule off / offline / not due) — engine presence unproven by logs")
		case EngineStateUpstreamUnavailable:
			// reason already recorded at classification
		default:
			v.Reasons = append(v.Reasons, "no managed-engine success record observed")
		}
	}

	return v
}
