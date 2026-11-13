// Package startup records monotonic application startup-phase timings with
// stable, sanitized fields only.
//
// Work order for run 43008361835 §F2/§5.1: the bootstrap path had no
// first-frame/phase telemetry, so a red or slow boot left no reproducible
// timing record. This package provides one process-wide recorder that
//
//   - measures named phases with a MONOTONIC clock (time.Since on a single
//     process-start reference — never wall-clock differences),
//   - stores bounded, in-memory records (no data root required — a pristine
//     smoke copy with no app-data directory still produces evidence),
//   - emits a sanitized summary to stderr (stable fields only: phase,
//     duration_ms, success, error_class, platform, build_version,
//     cold_or_warm) and, in the controlled diagnostic mode
//     (SHEYTAN_STARTUP_DIAGNOSTICS=1), the full JSON records to stdout.
//
// Never recorded: prompts, model contents, user paths, environment values,
// secrets, tokens, session IDs or raw request bodies. Error details are
// reduced to a small error_class token.
package startup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Phase is a stable, machine-readable bootstrap phase identifier. The set is
// closed: phases map 1:1 onto the v1.11.1 desktop launch sequence
// (cmd → config/migrations/logging → desktop core/IPC → Fyne app/shell →
// first interactive UI → core handshake → session refresh → background
// readiness), plus the shutdown phase recorded at close.
type Phase string

const (
	PhaseProcessEntry    Phase = "process_entry"
	PhaseConfigLoad      Phase = "config_load"
	PhaseDataMigrations  Phase = "data_migrations"
	PhaseLoggingInit     Phase = "logging_init"
	PhaseCoreInit        Phase = "core_init"
	PhaseCoreSetup       Phase = "core_setup"
	PhaseIPCKey          Phase = "ipc_key"
	PhaseIPCServe        Phase = "ipc_serve"
	PhaseFyneApp         Phase = "fyne_app"
	PhaseShellBuild      Phase = "shell_build"
	PhaseFirstView       Phase = "first_view"
	PhaseWindowReady     Phase = "window_ready"
	PhaseCoreHandshake   Phase = "core_handshake"
	PhaseSessionsRefresh Phase = "sessions_refresh"
	PhaseBackgroundReady Phase = "background_ready"
	// PhaseShutdownUI measures the UI-side coordinator teardown only; the
	// full process shutdown (UI close → IPC stop → core close) is recorded
	// as PhaseShutdown by internal/desktop.
	PhaseShutdownUI Phase = "shutdown_ui"
	PhaseShutdown   Phase = "shutdown"
)

// maxRecords bounds the ring: one record per phase is expected; the bound
// exists so a runaway caller cannot grow the recorder without limit.
const maxRecords = 64

// Record is one completed phase measurement. Fields are stable and safe to
// print: no paths, no user content, no error text.
type Record struct {
	Phase      Phase  `json:"phase"`
	DurationMS int64  `json:"duration_ms"`
	Success    bool   `json:"success"`
	ErrorClass string `json:"error_class,omitempty"`
}

// Recorder is safe for concurrent use.
type Recorder struct {
	mu       sync.Mutex
	t0       time.Time
	records  []Record
	begins   map[Phase]time.Time
	platform string
	version  string
	warm     bool
	diag     atomic.Bool
}

var (
	defaultOnce sync.Once
	defaultRec  *Recorder
)

// Default returns the process-wide recorder. Its reference time is captured
// at the first call (package-init time at the earliest, main entry at the
// latest) — the closest Go-observable point to process entry. The build
// version is filled in by the boot path (SetVersion) as soon as it is known.
func Default() *Recorder {
	defaultOnce.Do(func() {
		defaultRec = New("")
		defaultRec.RecordPhase(PhaseProcessEntry, 0, nil)
	})
	return defaultRec
}

// New builds a recorder over the given build version string.
func New(version string) *Recorder {
	return &Recorder{
		t0:       time.Now(),
		begins:   map[Phase]time.Time{},
		platform: runtime.GOOS + "/" + runtime.GOARCH,
		version:  version,
	}
}

// SetVersion records the build version (the one authority, internal/config
// AppVersion, resolved by the boot path).
func (r *Recorder) SetVersion(v string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.version = v
}

// SetWarm records whether this launch was warm (the canonical data root
// already existed at process entry — a returning user) or cold (pristine
// data root; first launch). It is computed by the caller BEFORE config.Load
// can create anything, and only the boolean is stored — never the path.
func (r *Recorder) SetWarm(warm bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.warm = warm
}

// Warm reports the recorded cold/warm classification.
func (r *Recorder) Warm() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.warm
}

// SetDiagnostic enables the controlled diagnostic mode (stdout JSON records
// at emission). It never changes application behavior beyond the additional
// stdout output.
func (r *Recorder) SetDiagnostic(on bool) { r.diag.Store(on) }

// StartPhase marks the monotonic begin of a phase span.
func (r *Recorder) StartPhase(p Phase) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.begins == nil {
		r.begins = map[Phase]time.Time{}
	}
	r.begins[p] = time.Now()
}

// EndPhase completes the most recent StartPhase span for p and records it.
// A nil err means success; any error is reduced to an error_class token.
func (r *Recorder) EndPhase(p Phase, err error) {
	r.mu.Lock()
	start, ok := r.begins[p]
	if ok {
		delete(r.begins, p)
	}
	r.mu.Unlock()
	if !ok {
		// No open span: record an instantaneous marker so the phase still
		// appears in the summary (the caller lost the begin mark).
		r.RecordPhase(p, 0, err)
		return
	}
	r.RecordPhase(p, time.Since(start), err)
}

// RecordPhase appends one completed phase measurement.
func (r *Recorder) RecordPhase(p Phase, d time.Duration, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.records) >= maxRecords {
		copy(r.records, r.records[1:])
		r.records = r.records[:maxRecords-1]
	}
	rec := Record{Phase: p, DurationMS: d.Milliseconds(), Success: err == nil}
	if err != nil {
		rec.ErrorClass = ErrorClass(err)
	}
	r.records = append(r.records, rec)
}

// RecordElapsed records a phase measured from the process-start reference
// (time to first interactive UI, time to background readiness) instead of a
// begin/end span.
func (r *Recorder) RecordElapsed(p Phase, err error) {
	r.mu.Lock()
	elapsed := time.Since(r.t0)
	r.mu.Unlock()
	r.RecordPhase(p, elapsed, err)
}

// Records returns a copy of the completed records in order.
func (r *Recorder) Records() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Record, len(r.records))
	copy(out, r.records)
	return out
}

// Summary renders the sanitized multi-line summary. One line per record,
// stable fields only — safe for stderr on every platform, with or without a
// data root.
func (r *Recorder) Summary() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	version := r.version
	if version == "" {
		version = "unknown"
	}
	cw := "cold"
	if r.warm {
		cw = "warm"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "startup phases: platform=%s build_version=%s cold_or_warm=%s", r.platform, version, cw)
	for _, rec := range r.records {
		line := fmt.Sprintf("startup phase: phase=%s duration_ms=%d success=%t", rec.Phase, rec.DurationMS, rec.Success)
		if rec.ErrorClass != "" {
			line += " error_class=" + rec.ErrorClass
		}
		b.WriteString("\n" + line)
	}
	return b.String()
}

// JSON renders the full machine-readable records (diagnostic mode).
func (r *Recorder) JSON() ([]byte, error) {
	doc := struct {
		Platform     string   `json:"platform"`
		BuildVersion string   `json:"build_version"`
		ColdOrWarm   string   `json:"cold_or_warm"`
		Phases       []Record `json:"phases"`
	}{Platform: r.platform, BuildVersion: r.version, Phases: r.Records()}
	doc.ColdOrWarm = "cold"
	if r.Warm() {
		doc.ColdOrWarm = "warm"
	}
	return json.MarshalIndent(doc, "", "  ")
}

// Emit writes the summary to w (stderr in production). In diagnostic mode it
// additionally writes the full JSON records. Writing is best-effort: an
// unwritable sink never fails the application.
func (r *Recorder) Emit(w io.Writer) {
	if w == nil {
		return
	}
	_, _ = io.WriteString(w, r.Summary()+"\n")
	if r.diag.Load() {
		if data, err := r.JSON(); err == nil {
			_, _ = io.WriteString(w, string(data)+"\n")
		}
	}
}

// DiagnosticsEnabled reports whether the controlled diagnostic mode is on
// (env SHEYTAN_STARTUP_DIAGNOSTICS=1 — the documented test/CI switch; it
// adds stdout output only and never changes application behavior).
func DiagnosticsEnabled() bool {
	return os.Getenv("SHEYTAN_STARTUP_DIAGNOSTICS") == "1"
}

// ErrorClass reduces an error to a small, stable, sanitized token. Error
// text itself never reaches the record. Context cancellation classes and
// concrete connection-failure classes win over the generic net.Error
// fallback (context.DeadlineExceeded itself implements net.Error, and a
// wrapped OpError carries its cause in the string).
func ErrorClass(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "connection refused"):
		return "connection_refused"
	case strings.Contains(s, "no such host"):
		return "dns"
	case strings.Contains(s, "broken pipe"), strings.Contains(s, "connection reset"):
		return "connection_reset"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "timeout"
		}
		return "network"
	}
	if errors.Is(err, os.ErrNotExist) {
		return "not_found"
	}
	if errors.Is(err, os.ErrPermission) {
		return "permission"
	}
	return "other"
}
