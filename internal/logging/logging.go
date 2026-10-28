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
	defaultMu sync.Mutex
	defaultM  *Manager
	noop      = &Manager{}
	version   string
)

// SetVersion records the app version string used in session banners.
func SetVersion(v string) { version = v }

// SetDefault installs the process-wide default manager.
func SetDefault(m *Manager) {
	defaultMu.Lock()
	defaultM = m
	defaultMu.Unlock()
}

// Default returns the process-wide manager (or a no-op if none is set).
func Default() *Manager {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		return noop
	}
	return defaultM
}

// New creates a Manager rooted at dir. Files are opened lazily.
func New(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Manager{dir: dir}, nil
}

// NewBootstrap returns a manager that writes only to stderr + a bounded
// in-memory ring (no files). Used before the data directory is ready.
func NewBootstrap() *Manager {
	return &Manager{bootstrap: true}
}

// Dir returns the log directory (empty for bootstrap/noop).
func (m *Manager) Dir() string {
	if m == nil {
		return ""
	}
	return m.dir
}

// Enabled reports whether this manager is a real (non-noop) sink.
func (m *Manager) Enabled() bool {
	return m != nil && m != noop
}

// Info logs an informational line.
func (m *Manager) Info(category, format string, args ...interface{}) {
	m.log("INFO", category, format, args...)
}

// Warn logs a warning line.
func (m *Manager) Warn(category, format string, args ...interface{}) {
	m.log("WARN", category, format, args...)
}

// Error logs an error line.
func (m *Manager) Error(category, format string, args ...interface{}) {
	m.log("ERROR", category, format, args...)
}

// SessionBanner writes ONE unambiguous separator line at process start.
func (m *Manager) SessionBanner(name, ver string) {
	m.Info("session", "==== %s v%s session start (pid %d) ====", name, ver, os.Getpid())
}

// log is the shared implementation for Info/Warn/Error/Debug.
func (m *Manager) log(level, category, format string, args ...interface{}) {
	if m == nil || m == noop {
		return
	}
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s %s  [%s] %s", time.Now().Format("2006-01-02 15:04:05.000"), level, category, msg)
	if m.bootstrap {
		fmt.Fprintln(os.Stderr, line)
		m.mu.Lock()
		if len(m.buf) >= bootstrapBufferCap {
			m.buf = m.buf[1:]
		}
		m.buf = append(m.buf, bootstrapEntry{Level: level, Category: category, Message: msg})
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeAppLocked(line)
	if len(m.recent) >= recentLinesCap {
		m.recent = m.recent[1:]
	}
	m.recent = append(m.recent, line)
}

func (m *Manager) writeAppLocked(line string) {
	if m.appF == nil {
		f, err := os.OpenFile(filepath.Join(m.dir, "app.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		m.appF = f
		st, _ := f.Stat()
		if st != nil {
			m.appN = st.Size()
		}
	}
	n, _ := fmt.Fprintln(m.appF, line)
	m.appN += int64(n)
	if m.appN >= appMaxBytes {
		m.rotateAppLocked()
	}
}

func (m *Manager) rotateAppLocked() {
	if m.appF != nil {
		m.appF.Close()
		m.appF = nil
	}
	for i := appKeepFiles - 1; i >= 1; i-- {
		src := m.rotatedPath(i)
		dst := m.rotatedPath(i + 1)
		_ = os.Rename(src, dst)
	}
	_ = os.Rename(filepath.Join(m.dir, "app.log"), m.rotatedPath(1))
	m.appN = 0
}

func (m *Manager) rotatedPath(n int) string {
	return filepath.Join(m.dir, fmt.Sprintf("app-%d.log", n))
}

// Debug is defined in debug_gate.go (gated by DeveloperDebug, default OFF).

// Close flushes and closes every sink.
// It is safe to call more than once.
func (m *Manager) Close() error {
	if m == nil || m == noop {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var firstErr error
	closeFile := func(f **os.File) {
		if *f == nil {
			return
		}
		if err := (*f).Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		*f = nil
	}
	closeFile(&m.appF)
	closeFile(&m.toolF)
	closeFile(&m.llmF)
	return firstErr
}
