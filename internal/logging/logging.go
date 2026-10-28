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
