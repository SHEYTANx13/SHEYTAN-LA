package llm

// v1.3.6 (spec §4/§5): Windows process-exit codes caused by loader /
// runtime failures become FIRST-CLASS diagnostics instead of the generic
// "llama.cpp exited during startup: exit status 0xc0000139".
//
// The table is pure data — testable on every platform. The decoded class
// drives the recovery policy: loader failures are deterministic binary
// failures, so the compatibility ladder must NOT retry them (spec §5).
//
// Evidence discipline: a classified message says what the OS reported
// and what to check — it never claims a specific DLL is defective
// without proof.

import (
	"fmt"
	"strings"
)

// LoaderFailureKind classifies a deterministic process-loader failure.
type LoaderFailureKind string

const (
	LoaderNone              LoaderFailureKind = ""
	LoaderDLLNotFound       LoaderFailureKind = "dll-not-found"       // 0xC0000135
	LoaderEntryPointMissing LoaderFailureKind = "entry-point-missing" // 0xC0000139
	LoaderBadImage          LoaderFailureKind = "bad-image-format"    // 0xC000007B / corrupt
	LoaderDLLInitFailed     LoaderFailureKind = "dll-init-failed"     // 0xC0000142
	LoaderAccessViolation   LoaderFailureKind = "access-violation"    // 0xC0000005
	LoaderIllegalInstruct   LoaderFailureKind = "illegal-instruction" // 0xC000001D
	LoaderStackOverrun      LoaderFailureKind = "stack-buffer-overrun"
	LoaderAccessDenied      LoaderFailureKind = "access-denied"  // 5
	LoaderModuleNotFound    LoaderFailureKind = "mod-not-found"  // 126
	LoaderProcNotFound      LoaderFailureKind = "proc-not-found" // 127
	LoaderInvalidExe        LoaderFailureKind = "invalid-exe"    // 193 / 216
)

// loaderFailure describes one decoded Windows exit condition.
type loaderFailure struct {
	Kind    LoaderFailureKind
	Code    uint32
	Summary string
	Advice  string
}

// loaderFailureTable maps Windows exit codes (NTSTATUS and the Win32
// error levels processes propagate as exit codes) onto classifications.
var loaderFailureTable = map[uint32]loaderFailure{
	0xC0000135: {
		Kind:    LoaderDLLNotFound,
		Summary: "a required DLL could not be found by the Windows loader (STATUS_DLL_NOT_FOUND)",
		Advice: "check the engine directory for the missing runtime DLL reported in the diagnostic, " +
			"and install the Microsoft Visual C++ Redistributable (64-bit) from https://aka.ms/vs/17/release/vc_redist.x64.exe",
	},
	0xC0000139: {
		Kind:    LoaderEntryPointMissing,
		Summary: "a DLL was found but a required entry point is missing — the DLL beside the binary does not match the build (STATUS_ENTRYPOINT_NOT_FOUND)",
		Advice: "the engine package's DLL set is incompatible with the executable (mixed llama.cpp builds or a partial update). " +
			"Use Repair / Rediscover so the validated package is re-imported as one unit",
	},
	0xC000007B: {
		Kind:    LoaderBadImage,
		Summary: "the image format is invalid — wrong architecture or a corrupt file (STATUS_INVALID_IMAGE_FORMAT)",
		Advice:  "verify the engine binary matches this machine's architecture; re-download or re-import the engine package",
	},
	0xC0000142: {
		Kind:    LoaderDLLInitFailed,
		Summary: "a DLL's initialization routine failed (STATUS_DLL_INIT_FAILED)",
		Advice:  "check the engine's runtime DLL set beside the executable and the Windows event log for the failing module",
	},
	0xC0000005: {
		Kind:    LoaderAccessViolation,
		Summary: "the process hit an access violation (STATUS_ACCESS_VIOLATION)",
		Advice:  "if it reproduces on every start, the binary or one of its runtime libraries is incompatible with this machine",
	},
	0xC000001D: {
		Kind:    LoaderIllegalInstruct,
		Summary: "the process executed an illegal instruction — the binary targets a CPU feature this machine lacks (STATUS_ILLEGAL_INSTRUCTION)",
		Advice:  "use an engine build compiled for this machine's CPU (e.g. without AVX-512 on older CPUs)",
	},
	0xC0000409: {
		Kind:    LoaderStackOverrun,
		Summary: "the process aborted on a stack buffer overrun check",
		Advice:  "if it reproduces on every start, the binary is incompatible with this machine",
	},
	0x00000005: {
		Kind:    LoaderAccessDenied,
		Summary: "access denied while starting the executable (Win32 ERROR_ACCESS_DENIED)",
		Advice:  "check file permissions and antivirus quarantine of the engine binary",
	},
	0x0000007E: {
		Kind:    LoaderModuleNotFound,
		Summary: "a required module was not found (Win32 ERROR_MOD_NOT_FOUND)",
		Advice:  "check the engine directory for the missing runtime DLL reported in the diagnostic",
	},
	0x0000007F: {
		Kind:    LoaderProcNotFound,
		Summary: "a required procedure was not found in a module (Win32 ERROR_PROC_NOT_FOUND)",
		Advice:  "the DLL beside the binary does not match the build — re-import the validated engine package",
	},
	0x000000C1: {
		Kind:    LoaderBadImage,
		Summary: "the program is not a valid Win32 application (ERROR_BAD_EXE_FORMAT)",
		Advice:  "the engine binary has the wrong architecture for this machine",
	},
}

// classifyLoaderExit decodes a process exit code into a loader failure.
// ok is false for ordinary exits (including exit code 0 and normal
// llama.cpp argument errors) — those keep the existing behavior.
func classifyLoaderExit(exitCode int) (loaderFailure, bool) {
	if exitCode == 0 {
		return loaderFailure{}, false
	}

	// exec.ExitError reports negative values for codes > 0x7FFFFFFF/2
	// (e.g. 0xC0000135 arrives as -1073741515).
	code := uint32(int32(exitCode))

	lf, ok := loaderFailureTable[code]
	if ok {
		lf.Code = code
	}

	return lf, ok
}

// loaderClassFromText classifies loader failures from the binary's own
// stderr evidence when the exit code is not classifiable (Unix truncates
// exit codes to 8 bits, so a real NTSTATUS never arrives as a code).
// The text is the loader's own report — first-class evidence (spec §4).
func loaderClassFromText(text string) (loaderFailure, bool) {
	l := strings.ToLower(text)

	switch {
	case strings.Contains(l, "status_entrypoint_not_found") || strings.Contains(l, "0xc0000139"):
		lf := loaderFailureTable[0xC0000139]
		lf.Code = 0xC0000139
		return lf, true
	case strings.Contains(l, "status_dll_not_found") || strings.Contains(l, "0xc0000135"):
		lf := loaderFailureTable[0xC0000135]
		lf.Code = 0xC0000135
		return lf, true
	case strings.Contains(l, "status_dll_init_failed") || strings.Contains(l, "0xc0000142"):
		lf := loaderFailureTable[0xC0000142]
		lf.Code = 0xC0000142
		return lf, true
	default:
		return loaderFailure{}, false
	}
}

// loaderFailureError renders the first-class diagnostic for one loader
// failure. detail may carry the dependency evidence (missing imports,
// file identity); it is appended verbatim when non-empty.
func loaderFailureError(lf loaderFailure, detail string) error {
	msg := fmt.Sprintf(
		"llama.cpp failed to load: exit code 0x%08X — %s.",
		lf.Code, lf.Summary,
	)

	if lf.Advice != "" {
		msg += " " + lf.Advice + "."
	}

	if detail != "" {
		msg += "\n\n" + detail
	}

	return fmt.Errorf("%s", msg)
}
