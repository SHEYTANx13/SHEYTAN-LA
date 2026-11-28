package smokeverdict

// smokeverdict_v1111_test.go — the run-43995689377 regression fixtures.
//
// Fixture 1 is the EXACT observed false green: every shell record the
// smoke asserts PASSED while the same app.log recorded the engine update
// failing at stage engine swap, the previous engine failing validation,
// maintenance BLOCKED with update-failed-engine-unusable and prewarm
// refused. The old gate printed SMOKE OK; the contract now requires
// SHELL SMOKE OK; ENGINE NOT READY and a RED overall verdict.
//
// Fixture 2 is the fully healthy app: both gates green.
//
// The remaining fixtures pin the state vocabulary (deferred, no
// maintenance, failed-but-valid-old-engine, inference labels).

import (
	"strings"
	"testing"
)

// run43995689377ShellBlock renders the shell records the smoke asserts
// (all of them present and successful — the shell genuinely passed).
func run43995689377ShellBlock() string {
	return strings.Join([]string{
		`2026-11-14T10:00:01Z INFO  startup session start (SHEYTAN-LA 1.11.1, windows/amd64)`,
		`2026-11-14T10:00:01Z INFO  paths log directory: C:\smoke\SHEYTAN-LA\data\logs`,
		`2026-11-14T10:00:02Z INFO  core core handshake successful (transport=windows-named-pipe)`,
		`2026-11-14T10:00:02Z INFO  core first session refresh succeeded`,
		`2026-11-14T10:00:43Z INFO  core ipc stopped`,
		`2026-11-14T10:00:43Z INFO  core server closed`,
		`2026-11-14T10:00:43Z INFO  logging log sinks closing`,
	}, "\n")
}

// run43995689377EngineFailureBlock renders the observed maintenance
// failure sequence (paraphrased from the run log's own vocabulary).
func run43995689377EngineFailureBlock() string {
	return strings.Join([]string{
		`2026-11-14T10:00:04Z INFO  maintenance startup gate: engine update required (b10642 → b12445) — performing transactional maintenance BEFORE any engine start`,
		`2026-11-14T10:00:05Z INFO  maintenance startup gate: no model selected — engine update installs files only; the model selection owns the first start`,
		`2026-11-14T10:00:41Z ERROR maintenance engine update failed: stage engine swap: rename of data\bin to data\bin.update-old returned Access is denied`,
		`2026-11-14T10:00:42Z ERROR maintenance startup gate: update to b12445 failed (…) AND the installed engine fails validation (data\bin\llama-server.exe does not exist) — prewarm BLOCKED`,
		`2026-11-14T10:00:42Z ERROR maintenance maintenance BLOCKED (update-failed-engine-unusable): prewarm refused`,
		`2026-11-14T10:00:43Z INFO  core ipc stopped`,
		`2026-11-14T10:00:43Z INFO  core server closed`,
		`2026-11-14T10:00:43Z INFO  logging log sinks closing`,
	}, "\n")
}

// run46050871166WebShellBlock renders the WEB-SHELL-era records (the
// run 45634345922 flip's packaged desktop): the shell has no client-side
// named-pipe dial, so the handshake-equivalent records come from the web
// host's real first core read through the served handler.
func run46050871166WebShellBlock() string {
	return strings.Join([]string{
		`2026-11-27T20:00:01Z INFO  startup session start (SHEYTAN-LA 1.11.1, windows/amd64)`,
		`2026-11-27T20:00:01Z INFO  paths log directory: C:\smoke\SHEYTAN-LA\data\logs`,
		`2026-11-27T20:00:01Z INFO  desktop SHEYTAN-LA v1.11.1-ALPHA (web UI host — the adaptive web shell)`,
		`2026-11-27T20:00:02Z INFO  ipc core handshake successful protocol=sheytan-ipc protocolVersion=1 coreVersion=1.11.1 transport=same-origin-http`,
		`2026-11-27T20:00:02Z INFO  ipc first core read succeeded (transport=same-origin-http)`,
		`2026-11-27T20:00:43Z INFO  desktop ipc stopped`,
		`2026-11-27T20:00:43Z INFO  desktop server closed`,
		`2026-11-27T20:00:43Z INFO  logging log sinks closing`,
	}, "\n")
}

// TestWebShellEraSmokeIsGreen pins the run 46050871166 repair: the
// packaged desktop shell is the same-origin web shell, so the smoke must
// accept the web-era record set exactly like the named-pipe era's — and
// a log that MIXES the eras must still fail (each record set proves only
// its own transport).
func TestWebShellEraSmokeIsGreen(t *testing.T) {
	v := FromAppLog(run46050871166WebShellBlock() + "\n" +
		`2026-11-27T20:00:04Z INFO  maintenance startup gate: engine is current (b12445)`)

	if !v.ShellOK {
		t.Fatalf("the web-era shell records genuinely prove the three stages — ShellOK must be true (reasons=%v)", v.Reasons)
	}
	if !v.EngineReady {
		t.Fatalf("the engine-current maintenance record makes the engine ready, got state %q", v.EngineState)
	}
	if !v.TestReadyOverall() {
		t.Fatal("the web-era healthy app must pass the overall gate")
	}
	if got := v.Summary(); got != "SMOKE OK: shell + managed engine" {
		t.Fatalf("summary = %q", got)
	}
}

// TestMixedEraLogStillFails: a handshake record naming one transport with
// a first-read record from the OTHER era proves nothing — the verdict
// stays red (a half-upgraded or hand-edited log is not shell evidence).
func TestMixedEraLogStillFails(t *testing.T) {
	mixed := strings.Join([]string{
		`2026-11-27T20:00:02Z INFO  ipc core handshake successful protocol=sheytan-ipc protocolVersion=1 coreVersion=1.11.1 transport=same-origin-http`,
		`2026-11-27T20:00:02Z INFO  ipc first session refresh succeeded (connected) transport=windows-named-pipe`,
		`2026-11-27T20:00:43Z INFO  desktop ipc stopped`,
		`2026-11-27T20:00:43Z INFO  desktop server closed`,
	}, "\n")

	v := FromAppLog(mixed)

	if v.ShellOK {
		t.Fatal("a mixed-era log must fail the shell contract (each record set proves only its own transport)")
	}
}

// TestRun43995689377FalseGreenIsNowRed is THE regression fixture: shell
// green + engine red must produce the labelled split verdict and a RED
// overall test-ready result.
func TestRun43995689377FalseGreenIsNowRed(t *testing.T) {
	v := FromAppLog(run43995689377ShellBlock() + "\n" + run43995689377EngineFailureBlock())

	if !v.ShellOK {
		t.Fatal("the shell evidence in this fixture genuinely passed — ShellOK must be true (the fixture mirrors the observed run)")
	}
	if v.EngineReady {
		t.Fatal("the managed engine FAILED (swap denied, engine unusable, BLOCKED, prewarm refused) — EngineReady must be false")
	}
	if v.EngineState != EngineStateUpdateFailed {
		t.Fatalf("engine state = %q, want %q", v.EngineState, EngineStateUpdateFailed)
	}
	if v.TestReadyOverall() {
		t.Fatal("the overall Windows AI test-ready verdict must be RED for the run-43995689377 class")
	}
	if got := v.Summary(); got != "SHELL SMOKE OK; ENGINE NOT READY" {
		t.Fatalf("summary = %q, want the exact run-43995689377 class label %q", got, "SHELL SMOKE OK; ENGINE NOT READY")
	}
	if len(v.Reasons) == 0 {
		t.Fatal("the engine failure reason must be carried for the gate's error message")
	}
}

// TestHealthyAppIsFullyGreen pins the fully healthy shape: shell records
// plus a maintenance success record (engine current / verified+released /
// committed).
func TestHealthyAppIsFullyGreen(t *testing.T) {
	for name, engineBlock := range map[string]string{
		"already current": `2026-11-14T10:00:04Z INFO  maintenance startup gate: engine is current (b12445)`,
		"updated+verified": strings.Join([]string{
			`2026-11-14T10:00:04Z INFO  maintenance startup gate: engine update required (b10642 → b12445) — performing transactional maintenance BEFORE any engine start`,
			`2026-11-14T10:00:35Z INFO  maintenance startup gate: engine b12445 installed and verified — engine verified, prewarm released`,
		}, "\n"),
	} {
		v := FromAppLog(run43995689377ShellBlock() + "\n" + engineBlock + "\n" +
			`2026-11-14T10:00:50Z INFO  core ipc stopped` + "\n" +
			`2026-11-14T10:00:50Z INFO  core server closed`)

		if !v.ShellOK || !v.EngineReady {
			t.Fatalf("%s: healthy app must be fully green (shell=%t engine=%t, reasons=%v)",
				name, v.ShellOK, v.EngineReady, v.Reasons)
		}
		if !v.TestReadyOverall() {
			t.Fatalf("%s: healthy app must pass the overall gate", name)
		}
		if got := v.Summary(); got != "SMOKE OK: shell + managed engine" {
			t.Fatalf("%s: summary = %q", name, got)
		}
	}
}

// TestShellOnlyWithoutMaintenanceRecords: a pristine boot with no
// maintenance records at all is NOT engine-ready (unknown ≠ installed).
func TestShellOnlyWithoutMaintenanceRecords(t *testing.T) {
	v := FromAppLog(run43995689377ShellBlock())

	if !v.ShellOK {
		t.Fatal("shell records present — ShellOK true")
	}
	if v.EngineReady {
		t.Fatal("no maintenance record exists — engine readiness is unproven, never green")
	}
	if v.EngineState != EngineStateUnknown {
		t.Fatalf("engine state = %q, want unknown", v.EngineState)
	}
}

// TestDeferredMaintenanceIsNotEngineReady: schedule off / offline are
// honest deferrals — the logs alone cannot prove the engine present.
func TestDeferredMaintenanceIsNotEngineReady(t *testing.T) {
	for name, line := range map[string]string{
		"updates off": `2026-11-14T10:00:04Z INFO  maintenance startup gate: scheduled engine updates are off — no maintenance this boot`,
		"offline":     `2026-11-14T10:00:04Z INFO  maintenance startup gate: release check failed: offline — update check skipped (offline)`,
	} {
		v := FromAppLog(run43995689377ShellBlock() + "\n" + line)
		if v.EngineReady {
			t.Fatalf("%s: a deferred maintenance boot is not engine-ready by log evidence alone", name)
		}
		if v.EngineState != EngineStateDeferred {
			t.Fatalf("%s: engine state = %q, want deferred", name, v.EngineState)
		}
	}
}

// TestFailedButValidOldEngineIsUsable: the update failed but the previous
// engine still validates — the app is usable, the engine gate is green
// with the recorded reason.
func TestFailedButValidOldEngineIsUsable(t *testing.T) {
	block := strings.Join([]string{
		`2026-11-14T10:00:04Z INFO  maintenance startup gate: engine update required (b10642 → b12445) — performing transactional maintenance BEFORE any engine start`,
		`2026-11-14T10:00:30Z WARN  maintenance startup gate: update to b12445 failed (…) — keeping the last-known-good engine and continuing startup with it`,
		`2026-11-14T10:00:31Z INFO  core ipc stopped`,
		`2026-11-14T10:00:31Z INFO  core server closed`,
	}, "\n")

	v := FromAppLog(run43995689377ShellBlock() + "\n" + block)

	if !v.ShellOK {
		t.Fatal("shell records present")
	}
	if !v.EngineReady {
		t.Fatalf("the last-known-good engine still validates — engine-ready with reason, got state %q reasons %v", v.EngineState, v.Reasons)
	}
	if v.EngineState != EngineStateFailedValidOld {
		t.Fatalf("engine state = %q, want %q", v.EngineState, EngineStateFailedValidOld)
	}
}

// TestInferenceIsNeverImpliedByEngineHealth pins the inference label
// contract: no model → NOT RUN; a selected model still requires real
// execution proof.
func TestInferenceIsNeverImpliedByEngineHealth(t *testing.T) {
	if got := InferenceLabel(false); !strings.Contains(got, "NOT RUN") {
		t.Fatalf("no model selected must render NOT RUN, got %q", got)
	}
	if got := InferenceLabel(true); strings.Contains(got, "NOT RUN") || !strings.Contains(got, "execution proof required") {
		t.Fatalf("a selected model requires execution proof, got %q", got)
	}
}

// TestShellFailureIsSmokeFailed: without the shell records nothing is
// green — the verdict never upgrades a failed shell.
func TestShellFailureIsSmokeFailed(t *testing.T) {
	v := FromAppLog("2026-11-14T10:00:04Z INFO  maintenance startup gate: engine is current (b12445)")

	if v.ShellOK {
		t.Fatal("no shell records — ShellOK must be false")
	}
	if v.TestReadyOverall() {
		t.Fatal("a failed shell can never be test-ready overall")
	}
	if got := v.Summary(); got != "SMOKE FAILED" {
		t.Fatalf("summary = %q, want SMOKE FAILED", got)
	}
}

// TestUpstreamUnavailableIsNotEngineReadyNotACandidateDefect pins the
// live-observed Linux shape (2026-11-17, local end-to-end app run):
// upstream llama.cpp ships no prebuilt engine asset for linux/amd64.
// The engine is NOT ready (Linux is UI-ONLY-TEST, never TEST-READY,
// while upstream lacks the asset) — and the state must be attributable
// to upstream availability, never mistaken for a candidate defect.
func TestUpstreamUnavailableIsNotEngineReadyNotACandidateDefect(t *testing.T) {
	block := strings.Join([]string{
		`2026-11-17T04:49:10Z WARN  maintenance startup gate: release check failed: no recent release ships a prebuilt asset for linux/amd64`,
		`2026-11-17T04:49:10Z INFO  gpu AUTO Vulkan candidate not considered this boot: no prebuilt Vulkan engine package for linux/amd64`,
		`2026-11-17T04:49:10Z INFO  engine no model selected — engine prewarm deferred; the Model Selector owns the next step`,
	}, "\n")

	v := FromAppLog(run43995689377ShellBlock() + "\n" + block)

	if !v.ShellOK {
		t.Fatal("the shell records in this fixture genuinely passed")
	}
	if v.EngineReady || v.TestReadyOverall() {
		t.Fatal("an upstream-unavailable platform is never engine-ready / TEST-READY")
	}
	if v.EngineState != EngineStateUpstreamUnavailable {
		t.Fatalf("engine state = %q, want %q", v.EngineState, EngineStateUpstreamUnavailable)
	}
	if got := v.Summary(); got != "SHELL SMOKE OK; ENGINE NOT READY" {
		t.Fatalf("summary = %q, want the split label", got)
	}
	reason := strings.Join(v.Reasons, "; ")
	if !strings.Contains(reason, "upstream") || !strings.Contains(reason, "not a candidate defect") {
		t.Fatalf("the reason must attribute the limit to upstream availability: %q", reason)
	}
}
