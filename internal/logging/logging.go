// Package logging is SHEYTAN's log catcher. It captures every meaningful
// runtime event into rotating files under <dataDir>/logs so bugs can be
// diagnosed and the data reused to drive future updates:
//
//	logs/app.log        human-readable app events (rotated, 5 files × 2 MB)
//	logs/tools.jsonl    one structured record per tool call (args/result/duration)
//	logs/llm.jsonl      one structured record per LLM call (latency/tokens/finish)
//	logs/crashes/       one file per recovered panic, with stack trace
//	logs/screenshots/   browser-tool screenshots
//
// A package-level default manager makes it reachable from anywhere
// (orchestrator, tools, GUI, CLI) without plumbing.
package logging

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	appMaxBytes    = 2 * 1024 * 1024 // rotate app.log at 2 MB
	appKeepFiles   = 5               // keep app.log + app-1..app-4
	jsonlMaxBytes  = 10 * 1024 * 1024
	jsonlKeepFiles = 3
	recentLinesCap = 512

	// v1.7.4: the bootstrap sink keeps at most this many pre-canonical
	// lines in memory (bounded ring, same discipline as recentLinesCap)
	// before they are replayed into the canonical manager.
	bootstrapBufferCap = 256

	// v1.11.1 sink-health contract: a failed or nil app sink must never
	// silently discard a log event. The event is preserved in a bounded
	// emergency ring (distinct from the UI ring: it is the evidence of
	// what was logged while the sink was broken), and the failure is
	// surfaced on stderr — deduplicated by failure key and rate-limited
	// per window so a broken sink cannot flood the console.
	emergencyLinesCap = 256
	sinkReportWindow  = 30 * time.Second
	sinkReportBudget  = 16 // max distinct stderr reports per window
)

// bootstrapEntry is one pre-canonical log line held by the bootstrap
// sink (v1.7.4). Level and category are stored structurally so
// ReplayInto can re-emit the line through the canonical manager's real
// Info/Warn/Error pipeline instead of parsing formatted text back apart.
type bootstrapEntry struct {
	Level    string
	Category string
	Message  string
}

// Manager owns all log sinks for one data directory.
type Manager struct {
	dir string

	// v1.7.4: bootstrap mode — every line goes to stderr plus a bounded
	// in-memory buffer and NO file is created or opened. A distinct
	// field (not an overloaded dir=="") keeps the true no-op manager
	// disabled while the bootstrap manager stays enabled.
	bootstrap bool

	mu    sync.Mutex
	appF  *os.File
	appN  int64
	toolF *os.File
	toolN int64
	llmF  *os.File
	llmN  int64

	recent []string // ring of recent app log lines for the UI

	buf []bootstrapEntry // v1.7.4: bounded pre-canonical ring (bootstrap mode only)

	// v1.11.1 sink-health state (observability repair): Enabled() says
	// the manager is willing to write; these fields say whether the FILE
	// sinks actually accepted the last event. A broken sink is reported,
	// never hidden behind a healthy-looking manager.
	sinkErr        error
	sinkErrAt      time.Time
	emergency      []string // bounded ring of lines preserved while the app sink was broken
	lastReportKey  string
	lastReportAt   time.Time
	reportWindowAt time.Time
	reportBudget   int
	fallbackReason string // non-empty when the canonical dir failed and the fallback is active
}

// ToolCallRecord is one structured tool-call entry (tools.jsonl).
type ToolCallRecord struct {
	TS         time.Time `json:"ts"`
	Tool       string    `json:"tool"`
	Args       string    `json:"args,omitempty"`
	Result     string    `json:"result,omitempty"`
	Error      string    `json:"error,omitempty"`
	DurationMs int64     `json:"durationMs"`
	Session    string    `json:"session,omitempty"`
}

// LLMCallRecord is one structured LLM-call entry (llm.jsonl).
type LLMCallRecord struct {
	TS              time.Time `json:"ts"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	PromptMsgs      int       `json:"promptMsgs"`
	PromptChars     int       `json:"promptChars"`
	CompletionChars int       `json:"completionChars"`
	ToolCalls       int       `json:"toolCalls"`
	FinishReason    string    `json:"finishReason,omitempty"`
	DurationMs      int64     `json:"durationMs"`
	Error           string    `json:"error,omitempty"`
}

// --- package-level default ---

var (
	defMu sync.RWMutex
	def   *Manager
)

// SetDefault installs the process-wide manager (call once at boot).
func SetDefault(m *Manager) {
	defMu.Lock()
	def = m
	defMu.Unlock()
}

// Default returns the process-wide manager. If none was installed it
// returns a disabled no-op manager — never nil, never panics.
func Default() *Manager {
	defMu.RLock()
	m := def
	defMu.RUnlock()
	if m != nil {
		return m
	}
	return noop
}

// noop is a Manager whose directory is empty; every write is skipped.
var noop = &Manager{}

// NewBootstrap returns the pre-logger manager (v1.7.4). Every line goes
// to stderr AND a bounded in-memory buffer; no file is ever created or
// opened — not even the canonical sink files.
//
// Boot needs this because the runtime-data migrations must be able to
// create or rename the very sink files (app.log, tools.jsonl, llm.jsonl)
// a canonical manager would hold open for the whole process lifetime —
// on Windows a rename onto an open path fails with "Access is denied",
// which made the legacy-root migration fail on every v1.7.3 boot.
// Install it with SetDefault BEFORE the migrations run, then ReplayInto
// the canonical manager once logging.New has opened the real sinks.
func NewBootstrap() *Manager {
	return &Manager{bootstrap: true}
}

// New creates (or opens) the log directory and its sinks.
func New(dir string) (*Manager, error) {
	if dir == "" {
		return noop, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	m := &Manager{dir: dir}
	if err := m.openSinks(); err != nil {
		return nil, err
	}

	// v1.1.4: crash reports accumulated without bound — every panic added
	// one forever. Keep the most recent 20.
	m.pruneCrashes(20)

	return m, nil
}

// pruneCrashes keeps only the newest n crash-*.log files.
func (m *Manager) pruneCrashes(keep int) {
	crashes, err := filepath.Glob(filepath.Join(m.dir, "crashes", "crash-*.log"))
	if err != nil || len(crashes) <= keep {
		return
	}

	sort.Slice(crashes, func(i, j int) bool { return crashes[i] > crashes[j] })

	for _, p := range crashes[keep:] {
		_ = os.Remove(p)
	}
}

func (m *Manager) openSinks() error {
	if m.dir == "" {
		return nil
	}
	var closeErrs []error
	appF, appN, err := openAppend(m.appPath())
	if err != nil {
		return err
	}
	toolF, toolN, err := openAppend(filepath.Join(m.dir, "tools.jsonl"))
	if err != nil {
		// v1.11.1: close EVERY handle already opened before returning — a
		// half-open Manager would leak descriptors and (worse) pretend a
		// partial sink set is a healthy logger. The original open error is
		// preserved; close failures are joined onto it.
		closeErrs = append(closeErrs, appF.Close())
		return errors.Join(append([]error{fmt.Errorf("open tools.jsonl: %w", err)}, closeErrs...)...)
	}
	llmF, llmN, err := openAppend(filepath.Join(m.dir, "llm.jsonl"))
	if err != nil {
		closeErrs = append(closeErrs, appF.Close(), toolF.Close())
		return errors.Join(append([]error{fmt.Errorf("open llm.jsonl: %w", err)}, closeErrs...)...)
	}
	m.appF, m.appN = appF, appN
	m.toolF, m.toolN = toolF, toolN
	m.llmF, m.llmN = llmF, llmN
	return nil
}

func openAppend(path string) (*os.File, int64, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		// v1.11.1 (CodeQL go/unhandled-writable-file-close #241): the
		// Stat-failure path closes the writable handle and reports both
		// errors — the close failure is never silently dropped.
		return nil, 0, errors.Join(err, f.Close())
	}
	return f, st.Size(), nil
}

func (m *Manager) appPath() string { return filepath.Join(m.dir, "app.log") }

// Dir returns the log directory ("" for the no-op manager).
func (m *Manager) Dir() string { return m.dir }

// Enabled reports whether this manager actually writes. The bootstrap
// manager writes to stderr + memory even though it owns no directory;
// only the true no-op manager (no directory, not bootstrap) is disabled.
func (m *Manager) Enabled() bool { return m.dir != "" || m.bootstrap }

// SinkHealth is the observable health of the log catcher's file sinks —
// what the Logs API and the UI surface report. Enabled() means the
// manager is willing to write; SinksReady says the file sinks actually
// accepted the last event. A fallback is never presented as a healthy
// canonical sink: FallbackReason stays non-empty for the process
// lifetime.
type SinkHealth struct {
	Dir            string    `json:"dir"`
	SinksReady     bool      `json:"sinksReady"`
	LastErr        string    `json:"lastErr,omitempty"`
	LastErrAt      time.Time `json:"lastErrAt,omitempty"`
	FallbackReason string    `json:"fallbackReason,omitempty"`
	EmergencyLines int       `json:"emergencyLines"`
}

// Health snapshots the observable sink state. The no-op manager reports
// Dir="" and SinksReady=false; the bootstrap manager reports its stderr/
// memory posture.
func (m *Manager) Health() SinkHealth {
	if m == nil {
		return SinkHealth{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	h := SinkHealth{
		Dir:            m.dir,
		SinksReady:     m.appF != nil && m.toolF != nil && m.llmF != nil,
		FallbackReason: m.fallbackReason,
		EmergencyLines: len(m.emergency),
	}
	if m.bootstrap {
		h.Dir = "(bootstrap: stderr + bounded memory)"
	}
	if m.sinkErr != nil {
		h.LastErr = m.sinkErr.Error()
		h.LastErrAt = m.sinkErrAt
	}
	return h
}

// Emergency returns up to n lines preserved while the app sink was broken
// (bounded ring — the evidence of what was logged during the failure).
func (m *Manager) Emergency(n int) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n <= 0 || n > len(m.emergency) {
		n = len(m.emergency)
	}
	out := make([]string, n)
	copy(out, m.emergency[len(m.emergency)-n:])
	return out
}

// FallbackLogDir returns the documented per-user fallback log location
// used ONLY when the configured logs directory cannot be opened: the
// user's cache directory (per-user and private on every supported
// platform) plus the product name.
func FallbackLogDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("user cache dir: %w", err)
	}
	return filepath.Join(base, "SHEYTAN-LA", "logs"), nil
}

// NewWithFallback opens the canonical sinks at dir. When that fails, the
// primary failure is recorded FIRST (stderr, before anything else), then
// a per-user private fallback directory is used. The returned Manager
// reports the ACTIVE directory via Dir()/Health() and the reason via
// Health().FallbackReason — the fallback is never hidden and never
// presented as the canonical sink. When even the fallback cannot be
// opened, the error names both paths; the caller keeps the bootstrap
// logger (stderr + bounded memory — the last-resort sink).
func NewWithFallback(dir string) (*Manager, error) {
	m, err := New(dir)
	if err == nil {
		return m, nil
	}
	primaryErr := err

	fbDir, fbErr := FallbackLogDir()
	if fbErr != nil {
		return nil, fmt.Errorf("primary log dir %q: %v; fallback unavailable: %w", dir, primaryErr, fbErr)
	}
	m, err = New(fbDir)
	if err != nil {
		return nil, fmt.Errorf("primary log dir %q: %v; fallback log dir %q: %w", dir, primaryErr, fbDir, err)
	}

	m.mu.Lock()
	m.fallbackReason = fmt.Sprintf("primary log dir %q unavailable: %v (fallback active: %s)", dir, primaryErr, fbDir)
	m.mu.Unlock()

	// The primary failure is the fallback sink's FIRST records: the
	// evidence travels with the logs an operator will actually read.
	fmt.Fprintf(os.Stderr, "sheytan-log: primary log directory unavailable: %v — using fallback: %s\n", primaryErr, fbDir)
	m.Error("logging", "primary log directory unavailable — using the per-user fallback: %v", primaryErr)
	return m, nil
}

// --- app.log ---

func (m *Manager) log(level, category, format string, args ...interface{}) {
	if !m.Enabled() {
		return
	}

	message := fmt.Sprintf(format, args...)

	// v1.8.2: the human-facing redaction rule — opaque run/session
	// identity tokens never reach a rendered log line. Applied here, at
	// the ONE point where a record becomes rendered text, so every sink
	// (app.log, the UI recent ring, stderr, crash reports) inherits the
	// rule. Internal identity (API objects, run state, journals, storage
	// keys) is untouched — see redact.go.
	message = RedactIdentityTokens(message)

	// v1.3.0: a WARN/ERROR with no actionable context is a defect in the
	// CALLER, not just noise — the v1.2.9 runtime log carried lines like
	// "WARN  [updater]" with nothing after them, which is undiscoverable
	// later. Instead of emitting the blank line, attach the emitting code
	// location so the record stays actionable. (Call sites are ALSO fixed
	// to always carry a message — this guard is defense in depth, not a
	// license to log empty warnings.)
	if (level == "WARN" || level == "ERROR") && strings.TrimSpace(message) == "" {
		if _, file, line, ok := runtime.Caller(2); ok {
			message = fmt.Sprintf("(no detail provided) raised at %s:%d — fix the call site to include context",
				filepath.Base(file), line)
		} else {
			message = "(no detail provided) — fix the call site to include context"
		}
	}

	line := fmt.Sprintf("%s %-5s [%s] %s",
		time.Now().Format("2006-01-02 15:04:05.000"), level, category,
		message)
	m.mu.Lock()
	defer m.mu.Unlock()

	// v1.7.4: bootstrap sink — stderr + bounded memory buffer, no files.
	if m.bootstrap {
		fmt.Fprintln(os.Stderr, line)
		if len(m.buf) >= bootstrapBufferCap {
			m.buf = m.buf[1:]
		}
		m.buf = append(m.buf, bootstrapEntry{Level: level, Category: category, Message: message})
		return
	}

	m.appendLineLocked(line)
}

// appendLineLocked persists one formatted line through the app sink.
// Callers hold m.mu.
//
// v1.11.1 sink-health contract: the UI ring stays live even when the app
// sink is broken (the Logs surface must reflect reality, not emptiness),
// and a line that could not reach app.log is preserved in the bounded
// emergency ring with the failure surfaced — never silently discarded.
func (m *Manager) appendLineLocked(line string) {
	// recent ring for the UI — first, unconditionally.
	if len(m.recent) >= recentLinesCap {
		m.recent = m.recent[1:]
	}
	m.recent = append(m.recent, line)

	if m.appF == nil {
		m.sinkFailureLocked(line, errors.New("app.log sink unavailable (open or rotation failed)"))
		return
	}
	if _, err := m.appF.WriteString(line + "\n"); err != nil {
		m.sinkFailureLocked(line, fmt.Errorf("app.log write failed: %w", err))
		return
	}
	m.appN += int64(len(line) + 1)
	if m.appN >= appMaxBytes {
		m.rotateAppLocked()
	}
}

// sinkFailureLocked records a log event that could not reach app.log:
// the line is preserved in the bounded emergency ring, the observable
// health state is updated, and the failure is surfaced on stderr —
// deduplicated by failure message and rate-limited per window so a
// broken sink cannot flood the console. It NEVER logs through the
// Manager itself: a logger reporting its own failure through itself is
// the recursion the v1.11.1 repair explicitly forbids. Callers hold m.mu.
func (m *Manager) sinkFailureLocked(line string, failure error) {
	m.sinkErr = failure
	m.sinkErrAt = time.Now()
	if len(m.emergency) >= emergencyLinesCap {
		m.emergency = m.emergency[1:]
	}
	m.emergency = append(m.emergency, line)
	m.reportSinkFaultLocked(failure)
}

// sinkFaultLocked records a sink fault where NO log line was lost
// (rotation bookkeeping, jsonl write failures — the app.log line itself
// was already accepted): observable health + rate-limited stderr, no
// emergency append. Callers hold m.mu.
func (m *Manager) sinkFaultLocked(failure error) {
	m.sinkErr = failure
	m.sinkErrAt = time.Now()
	m.reportSinkFaultLocked(failure)
}

// reportSinkFaultLocked is the ONE stderr channel for sink faults:
// deduplicated by message key, rate-limited to sinkReportBudget distinct
// reports per sinkReportWindow. Callers hold m.mu.
func (m *Manager) reportSinkFaultLocked(failure error) {
	if failure == nil {
		return
	}
	key := failure.Error()
	now := time.Now()
	if now.Sub(m.reportWindowAt) >= sinkReportWindow {
		m.reportWindowAt = now
		m.reportBudget = sinkReportBudget
	}
	report := false
	if key != m.lastReportKey {
		report = true
	} else if now.Sub(m.lastReportAt) >= sinkReportWindow {
		report = true
	}
	if !report || m.reportBudget <= 0 {
		return
	}
	m.reportBudget--
	m.lastReportKey = key
	m.lastReportAt = now
	fmt.Fprintf(os.Stderr, "sheytan-log: %v — log events continue in the in-memory ring (emergency lines: %d)\n", failure, len(m.emergency))
}

func (m *Manager) rotateAppLocked() {
	// v1.11.1 (CodeQL go/unhandled-writable-file-close #253): a failing
	// close before rotation is surfaced on stderr — the Manager has no
	// error-return channel to its callers (Write is best-effort by
	// contract), and stderr is the ONE sink that survives a broken file
	// logger (the bootstrap path uses the same channel).
	if cerr := m.appF.Close(); cerr != nil {
		fmt.Fprintf(os.Stderr, "sheytan-log: closing app.log before rotation failed: %v\n", cerr)
	}
	// v1.11.1 observability repair: rotation is bookkeeping — no log line
	// is lost by a rename failure (app.log keeps receiving writes) — but a
	// failed rotation must be REPORTED, not ignored: the observable health
	// state and stderr carry it.
	var rotateErrs []error
	// shift app-3.log→app-4.log ... app.log→app-1.log
	for i := appKeepFiles - 1; i >= 1; i-- {
		src := m.rotatedPath(i)
		dst := m.rotatedPath(i + 1)
		if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
			rotateErrs = append(rotateErrs, fmt.Errorf("shift %s: %w", filepath.Base(src), err))
		}
	}
	if err := os.Rename(m.appPath(), m.rotatedPath(1)); err != nil {
		rotateErrs = append(rotateErrs, fmt.Errorf("rotate app.log: %w", err))
	}
	if len(rotateErrs) > 0 {
		m.sinkFaultLocked(errors.Join(rotateErrs...))
	}
	f, n, err := openAppend(m.appPath())
	if err != nil {
		// The app sink is now unavailable: report it and let every
		// subsequent line flow through the emergency path — never a nil
		// sink that silently drops everything.
		m.appF = nil
		m.sinkFaultLocked(fmt.Errorf("reopen app.log after rotation failed: %w", err))
		return
	}
	m.appF, m.appN = f, n
}

// rotatedPath returns <dir>/app-<n>.log for rotated generations.
func (m *Manager) rotatedPath(n int) string {
	return filepath.Join(m.dir, fmt.Sprintf("app-%d.log", n))
}

// Debug/Info/Warn/Error log to app.log with a category.
func (m *Manager) Debug(category, format string, args ...interface{}) {
	m.log("DEBUG", category, format, args...)
}
func (m *Manager) Info(category, format string, args ...interface{}) {
	m.log("INFO", category, format, args...)
}

// SessionBanner writes ONE unambiguous separator line at process start
// (v1.2.2). app.log persists across versions and boots — historical
// entries (e.g. a v0.8.0 startup from months ago) previously sat above
// current runtime lines with nothing to tell them apart. The banner:
//
//	2026-09-15 10:12:33.001 INFO  [session] ==== SHEYTAN-LA v1.2.2 session start (pid 4212) ====
//
// flows through the same ring/rotation pipeline, so the in-app Log
// Viewer shows it too — everything ABOVE a session banner is verifiably
// historical, everything below belongs to the current run.
func (m *Manager) SessionBanner(appName, version string) {
	m.log(
		"INFO",
		"session",
		"==== %s v%s session start (pid %d) ====",
		appName,
		version,
		os.Getpid(),
	)
}
func (m *Manager) Warn(category, format string, args ...interface{}) {
	m.log("WARN", category, format, args...)
}
func (m *Manager) Error(category, format string, args ...interface{}) {
	m.log("ERROR", category, format, args...)
}

// ReplayInto re-emits every line the bootstrap sink buffered through dst
// (v1.7.4). Level, category and message are preserved; timestamps are
// re-stamped by dst's normal pipeline at replay time. The buffer is
// drained, so a second call writes nothing — the replay is exactly once.
// A nil dst (e.g. the canonical manager failed to open) or dst == m is
// ignored; the lines remain on stderr either way.
func (m *Manager) ReplayInto(dst *Manager) {
	if m == nil || dst == nil || dst == m {
		return
	}

	m.mu.Lock()
	buf := m.buf
	m.buf = nil
	m.mu.Unlock()

	for _, e := range buf {
		switch e.Level {
		case "WARN":
			dst.Warn(e.Category, "%s", e.Message)
		case "ERROR":
			dst.Error(e.Category, "%s", e.Message)
		case "DEBUG":
			dst.Debug(e.Category, "%s", e.Message)
		default:
			dst.Info(e.Category, "%s", e.Message)
		}
	}
}

// Recent returns up to n most recent app log lines (for the UI Logs tab).
func (m *Manager) Recent(n int) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n <= 0 || n > len(m.recent) {
		n = len(m.recent)
	}
	out := make([]string, n)
	copy(out, m.recent[len(m.recent)-n:])
	return out
}

// LogEntry is one parsed app-log line for the in-app Log Viewer (v1.1.7).
type LogEntry struct {
	Index    int    `json:"index"`
	Time     string `json:"time"`
	Level    string `json:"level"`
	Category string `json:"category"`
	Message  string `json:"message"`
	Raw      string `json:"raw"`
}

// RecentParsed returns up to n recent app log lines parsed into entries and
// REDACTED before display (the viewer is a UI surface; secrets typed into
// tool arguments or configs must never render verbatim). Index is the
// entry's position in the bounded ring so the UI can tail incrementally.
//
// The parser is deliberately tolerant: any line that does not match the
// canonical "time LEVEL [category] message" layout is still returned, with
// the whole line in Message — the viewer never drops content it cannot
// parse.
func (m *Manager) RecentParsed(n int) []LogEntry {
	lines := m.Recent(n)

	out := make([]LogEntry, 0, len(lines))
	for i, line := range lines {
		entry := LogEntry{Index: i, Raw: line, Level: "INFO"}
		rest := line

		// time (2006-01-02 15:04:05.000)
		if len(rest) >= 23 && rest[4] == '-' && rest[10] == ' ' && rest[13] == ':' {
			entry.Time = rest[:23]
			rest = strings.TrimSpace(rest[23:])
		}

		// level token
		if sp := strings.IndexByte(rest, ' '); sp > 0 {
			tok := rest[:sp]
			switch tok {
			case "DEBUG", "INFO", "WARN", "ERROR":
				entry.Level = tok
				rest = strings.TrimSpace(rest[sp:])
			}
		}

		// [category]
		if strings.HasPrefix(rest, "[") {
			if end := strings.IndexByte(rest, ']'); end > 0 {
				entry.Category = rest[1:end]
				rest = strings.TrimSpace(rest[end+1:])
			}
		}

		entry.Message = redact(rest)
		entry.Raw = redact(line)

		out = append(out, entry)
	}

	return out
}

// --- structured sinks ---

// ToolCall appends one record to tools.jsonl.
func (m *Manager) ToolCall(rec ToolCallRecord) {
	if !m.Enabled() {
		return
	}
	rec.Args = clip(rec.Args, 4096)
	rec.Result = clip(rec.Result, 4096)
	rec.Error = clip(rec.Error, 1024)
	m.appendJSONL(&m.toolF, &m.toolN, filepath.Join(m.dir, "tools.jsonl"), jsonlMaxBytes, jsonlKeepFiles, rec)
}

// LLMCall appends one record to llm.jsonl.
func (m *Manager) LLMCall(rec LLMCallRecord) {
	if !m.Enabled() {
		return
	}
	rec.Error = clip(rec.Error, 1024)
	m.appendJSONL(&m.llmF, &m.llmN, filepath.Join(m.dir, "llm.jsonl"), jsonlMaxBytes, jsonlKeepFiles, rec)
}

func (m *Manager) appendJSONL(f **os.File, n *int64, path string, max int64, keep int, rec any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if *f == nil {
		// v1.11.1: a nil structured sink reports honestly — the record is
		// preserved as an emergency line (redaction applies at render),
		// never silently dropped.
		m.sinkFailureLocked(
			fmt.Sprintf("structured record not persisted (%s unavailable): %s", filepath.Base(path), summarizeJSONL(rec)),
			fmt.Errorf("%s sink unavailable", filepath.Base(path)))
		return
	}
	data, err := json.Marshal(rec)
	if err != nil {
		m.sinkFaultLocked(fmt.Errorf("encode %s record: %w", filepath.Base(path), err))
		return
	}
	if _, err := (*f).Write(append(data, '\n')); err != nil {
		// v1.11.1: the write failure is observable (health + rate-limited
		// stderr + emergency evidence) — a failing disk must never look
		// like a healthy structured sink.
		m.sinkFailureLocked(
			fmt.Sprintf("structured record lost (%s write failed): %s", filepath.Base(path), summarizeJSONL(rec)),
			fmt.Errorf("%s write failed: %w", filepath.Base(path), err))
		return
	} else {
		*n += int64(len(data) + 1)
	}
	if *n >= max {
		// v1.11.1 (CodeQL go/unhandled-writable-file-close #254): the
		// pre-rotation close failure is surfaced on stderr (same
		// propagation rationale as rotateAppLocked).
		if cerr := (*f).Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "sheytan-log: closing %s before rotation failed: %v\n", path, cerr)
		}
		var rotateErrs []error
		for i := keep - 1; i >= 1; i-- {
			src := fmt.Sprintf("%s.%d", path, i)
			dst := fmt.Sprintf("%s.%d", path, i+1)
			if rerr := os.Rename(src, dst); rerr != nil && !os.IsNotExist(rerr) {
				rotateErrs = append(rotateErrs, fmt.Errorf("shift %s: %w", filepath.Base(src), rerr))
			}
		}
		if rerr := os.Rename(path, path+".1"); rerr != nil {
			rotateErrs = append(rotateErrs, fmt.Errorf("rotate %s: %w", filepath.Base(path), rerr))
		}
		if len(rotateErrs) > 0 {
			m.sinkFaultLocked(errors.Join(rotateErrs...))
		}
		nf, nn, err := openAppend(path)
		if err != nil {
			*f = nil
			m.sinkFaultLocked(fmt.Errorf("reopen %s after rotation failed: %w", filepath.Base(path), err))
			return
		}
		*f, *n = nf, nn
	}
}

// summarizeJSONL names a structured record WITHOUT its payloads (tool
// arguments and LLM traffic are data, not diagnostics).
func summarizeJSONL(rec any) string {
	switch r := rec.(type) {
	case ToolCallRecord:
		return fmt.Sprintf("tool=%s session=%s durationMs=%d err=%t", r.Tool, r.Session, r.DurationMs, r.Error != "")
	case LLMCallRecord:
		return fmt.Sprintf("provider=%s model=%s durationMs=%d err=%t", r.Provider, r.Model, r.DurationMs, r.Error != "")
	default:
		return fmt.Sprintf("%T", rec)
	}
}

// --- crash catcher ---

// crashVersion is set by the boot path so crash files record the app version.
var crashVersion = "unknown"

// SetVersion records the current app version for crash reports.
func SetVersion(v string) { crashVersion = v }

// Crash writes a recovered panic (with stack) to logs/crashes/crash-<ts>.log
// and also records it in app.log. Returns the crash file path.
func (m *Manager) Crash(r interface{}, stack []byte) string {
	// v1.7.4: the bootstrap sink owns no directory — a crash report
	// must never be written to a relative fallback path.
	if !m.Enabled() || m.bootstrap {
		return ""
	}
	dir := filepath.Join(m.dir, "crashes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, "crash-"+time.Now().Format("20060102-150405")+".log")
	// v1.8.2: the panic VALUE is human-facing report text — the identity
	// redaction applies to it (and to the log line below, which the sink
	// redacts anyway). The stack trace is machine diagnostics and is
	// written verbatim.
	panicText := RedactIdentityTokens(fmt.Sprint(r))
	body := fmt.Sprintf("time:    %s\nversion: %s\npanic:   %s\n\nstack:\n%s\n",
		time.Now().Format(time.RFC3339), crashVersion, panicText, string(stack))
	_ = os.WriteFile(path, []byte(body), 0o644)
	m.log("ERROR", "crash", "recovered panic: %s (details: %s)", panicText, path)
	return path
}

// --- diagnostics export ---

// Stats aggregates tools.jsonl + llm.jsonl into counts useful for updates.
type Stats struct {
	GeneratedAt   time.Time          `json:"generatedAt"`
	ToolCalls     int                `json:"toolCalls"`
	ToolErrors    int                `json:"toolErrors"`
	AvgDurationMs map[string]float64 `json:"avgDurationMs"`
	CallsPerTool  map[string]int     `json:"callsPerTool"`
	ErrorsPerTool map[string]int     `json:"errorsPerTool"`
	LLMCalls      int                `json:"llmCalls"`
	LLMErrors     int                `json:"llmErrors"`
	AvgLLMLatency float64            `json:"avgLLMLatencyMs"`
	ProviderMix   map[string]int     `json:"providerMix"`
}

// ComputeStats scans the structured logs and returns aggregated stats.
func (m *Manager) ComputeStats() Stats {
	st := Stats{
		GeneratedAt:   time.Now(),
		AvgDurationMs: map[string]float64{},
		CallsPerTool:  map[string]int{},
		ErrorsPerTool: map[string]int{},
		ProviderMix:   map[string]int{},
	}
	if !m.Enabled() {
		return st
	}
	toolDur := map[string]int64{}
	if f, err := os.Open(filepath.Join(m.dir, "tools.jsonl")); err == nil {
		dec := json.NewDecoder(f)
		for {
			var rec ToolCallRecord
			if err := dec.Decode(&rec); err != nil {
				break
			}
			st.ToolCalls++
			st.CallsPerTool[rec.Tool]++
			toolDur[rec.Tool] += rec.DurationMs
			if rec.Error != "" {
				st.ToolErrors++
				st.ErrorsPerTool[rec.Tool]++
			}
		}
		f.Close()
	}
	for tool, n := range st.CallsPerTool {
		if d, ok := toolDur[tool]; ok && n > 0 {
			st.AvgDurationMs[tool] = float64(d) / float64(n)
		}
	}
	var llmDur int64
	if f, err := os.Open(filepath.Join(m.dir, "llm.jsonl")); err == nil {
		dec := json.NewDecoder(f)
		for {
			var rec LLMCallRecord
			if err := dec.Decode(&rec); err != nil {
				break
			}
			st.LLMCalls++
			llmDur += rec.DurationMs
			st.ProviderMix[rec.Provider]++
			if rec.Error != "" {
				st.LLMErrors++
			}
		}
		f.Close()
	}
	if st.LLMCalls > 0 {
		st.AvgLLMLatency = float64(llmDur) / float64(st.LLMCalls)
	}
	return st
}

// Diagnostics bundles logs + stats + config (redacted) into a zip the user
// can attach to a bug report or update request.
// extraFiles maps name→path of additional files to include.
func (m *Manager) Diagnostics(zipPath string, configPath string, extraFiles map[string]string) (string, error) {
	if !m.Enabled() {
		return "", fmt.Errorf("logging disabled")
	}
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zw := zip.NewWriter(f)

	// 1. stats.json
	if statsData, err := json.MarshalIndent(m.ComputeStats(), "", "  "); err == nil {
		if w, err := zw.Create("stats.json"); err == nil {
			_, _ = w.Write(statsData)
		}
	}

	// 2. redacted config
	if configPath != "" {
		if data, err := os.ReadFile(configPath); err == nil {
			if w, err := zw.Create("config.redacted.json"); err == nil {
				_, _ = w.Write([]byte(redact(string(data))))
			}
		}
	}

	// 3. app logs (current + rotated)
	files := []string{m.appPath()}
	for i := 1; i <= appKeepFiles-1; i++ {
		files = append(files, m.rotatedPath(i))
	}
	for _, p := range files {
		if data, err := os.ReadFile(p); err == nil && len(data) > 0 {
			if w, err := zw.Create(filepath.Base(p)); err == nil {
				_, _ = w.Write(data)
			}
		}
	}

	// 4. structured logs — REDACTED (v1.1.4: tool/LLM arguments can contain
	// secrets the user or the model typed; shipping them raw in a diagnostics
	// zip was a real leak vector).
	for _, p := range []string{
		filepath.Join(m.dir, "tools.jsonl"),
		filepath.Join(m.dir, "llm.jsonl"),
	} {
		if data, err := os.ReadFile(p); err == nil && len(data) > 0 {
			if w, err := zw.Create(filepath.Base(p)); err == nil {
				_, _ = w.Write([]byte(redact(string(data))))
			}
		}
	}

	// 5. crashes (most recent 10)
	if crashes, err := filepath.Glob(filepath.Join(m.dir, "crashes", "crash-*.log")); err == nil {
		sort.Slice(crashes, func(i, j int) bool { return crashes[i] > crashes[j] })
		if len(crashes) > 10 {
			crashes = crashes[:10]
		}
		for _, p := range crashes {
			if data, err := os.ReadFile(p); err == nil {
				if w, err := zw.Create("crashes/" + filepath.Base(p)); err == nil {
					_, _ = w.Write([]byte(redact(string(data))))
				}
			}
		}
	}

	// 6. extra files (sysinfo etc.)
	for name, p := range extraFiles {
		if data, err := os.ReadFile(p); err == nil {
			if w, err := zw.Create(name); err == nil {
				_, _ = w.Write(data)
			}
		}
	}

	if err := zw.Close(); err != nil {
		return "", err
	}
	return zipPath, nil
}

// redact strips anything that looks like an API key or token.
//
// v1.1.4: handles the three shapes that actually occur in this app —
// plain "key: value" lines, JSON members ("key":"value"), and env-style
// "key=value" — instead of only the first.
//
// v1.1.7: the in-app Log Viewer renders arbitrary runtime lines, so the
// scrub now also covers inline occurrences: a secret-looking keyword
// followed by a quoted value (e.g. `fetch with apiKey "sk-…"`) and
// bearer / OpenAI-style tokens anywhere in the line. The Log Viewer is a
// UI surface — a secret typed mid-sentence must never render verbatim.
func redact(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if !lineLooksSecret(l) {
			lines[i] = l
			continue
		}

		switch {
		case redactJSONLine(&lines[i], l):
		default:
			if idx := strings.Index(l, ":"); idx > 0 {
				lines[i] = l[:idx+1] + " \"[REDACTED]\""
			} else if idx := strings.Index(l, "="); idx > 0 {
				lines[i] = l[:idx+1] + "[REDACTED]"
			}
		}
	}

	out := strings.Join(lines, "\n")

	// Inline passes (applied after the line pass; both are cheap and
	// idempotent).
	out = inlineSecretQuoted.ReplaceAllString(out, "${1} \"[REDACTED]\"")
	out = inlineBearer.ReplaceAllString(out, "Bearer [REDACTED]")
	out = inlineSKToken.ReplaceAllString(out, "[REDACTED]")

	return out
}

// inlineSecretQuoted matches `keyword "value"` / `keyword=value` /
// `keyword: value` where keyword looks secret-bearing.
var inlineSecretQuoted = regexp.MustCompile(
	`(?i)\b(api[_-]?key|apikey|token|password|secret|authorization)\b(\s*[:=]\s*|\s+)["']([^"']{4,})["']`)

// inlineBearer matches RFC 6750 bearer credentials.
var inlineBearer = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-]{8,}`)

// inlineSKToken matches OpenAI-style key prefixes wherever they appear.
var inlineSKToken = regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{12,}`)

func lineLooksSecret(l string) bool {
	low := strings.ToLower(l)
	return strings.Contains(low, "apikey") || strings.Contains(low, "api_key") ||
		strings.Contains(low, "token") || strings.Contains(low, "authorization") ||
		strings.Contains(low, "password") || strings.Contains(low, "secret")
}

// redactJSONLine rewrites every secret-looking JSON member value in the
// line; reports whether it changed anything.
func redactJSONLine(dst *string, l string) bool {
	if !strings.Contains(l, "{\")") && !strings.Contains(l, "{\"") {
		return false
	}

	// Cheap scanner over "key":"value" pairs on a single JSONL line.
	changed := false
	var out strings.Builder
	i := 0
	for i < len(l) {
		// find next key string
		rest := l[i:]
		q1 := strings.IndexByte(rest, '"')
		if q1 < 0 {
			out.WriteString(rest)
			break
		}
		q2 := strings.IndexByte(rest[q1+1:], '"')
		if q2 < 0 {
			out.WriteString(rest)
			break
		}
		key := rest[q1+1 : q1+1+q2]
		afterKey := rest[q1+1+q2+1:]

		if strings.HasPrefix(afterKey, ":") {
			// value must be a JSON string
			v1 := strings.IndexByte(afterKey[1:], '"')
			if v1 == 0 {
				v2 := strings.IndexByte(afterKey[2:], '"')
				if v2 >= 0 {
					value := afterKey[2 : 2+v2]
					if isSecretKey(strings.ToLower(key)) && value != "" {
						out.WriteString(rest[:q1+1+q2+1])
						out.WriteString(":\"[REDACTED]\"")
						i += q1 + 1 + q2 + 1 + 2 + v2 + 1
						changed = true
						continue
					}
					out.WriteString(rest[:q1+1+q2+1])
					out.WriteString(":")
					out.WriteString(value)
					i += q1 + 1 + q2 + 1 + 2 + v2 + 1
					continue
				}
			}
		}
		// not a string member — copy the key token and advance past it
		out.WriteString(rest[:q1+1+q2+1])
		i += q1 + 1 + q2 + 1
	}

	if changed {
		*dst = out.String()
	}
	return changed
}

func isSecretKey(key string) bool {
	return strings.Contains(key, "apikey") || strings.Contains(key, "api_key") ||
		strings.Contains(key, "token") || strings.Contains(key, "authorization") ||
		strings.Contains(key, "password") || strings.Contains(key, "secret") ||
		key == "key" // bare generic key field, exactly
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("…[+%d chars clipped]", len(s)-n)
}

// Close flushes and closes every sink, recording the close OUTCOME
// first (while the sink is still writable) and attempting EVERY sink even
// when one close fails — all failures are returned joined. It is safe to
// call more than once.
func (m *Manager) Close() error {
	if m == nil || m == noop {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// v1.11.1: the close-outcome record is written BEFORE the handles
	// close, so app.log carries the lifecycle evidence; a close failure is
	// additionally surfaced on stderr and in the observable health state.
	if !m.bootstrap && m.appF != nil {
		m.appendLineLocked(fmt.Sprintf("%s INFO  [logging] log sinks closing (dir=%s)",
			time.Now().Format("2006-01-02 15:04:05.000"), m.dir))
	}

	var errs []error
	closeSink := func(f **os.File, name string) {
		if *f == nil {
			return
		}
		if err := (*f).Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", name, err))
		}
		*f = nil
	}

	closeSink(&m.appF, "app.log")
	closeSink(&m.toolF, "tools.jsonl")
	closeSink(&m.llmF, "llm.jsonl")

	joined := errors.Join(errs...)
	if joined != nil {
		fmt.Fprintf(os.Stderr, "sheytan-log: sink close failures: %v\n", joined)
		m.sinkErr = joined
		m.sinkErrAt = time.Now()
	}
	return joined
}
