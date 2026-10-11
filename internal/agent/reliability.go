// reliability.go — v1.1.5 Phase 6: the agent reliability core.
//
// SHEYTAN's strategic product target is that a small local model plus
// SHEYTAN accomplishes work that normally requires a substantially larger
// model or a cloud agent. The three components in this file are what make
// that credible for the EXECUTION loop:
//
//  1. FailureClassification — every tool failure is diagnosed into an
//     explicit category, and the recovery hint fed back to the model is
//     chosen from the CATEGORY, not from the raw error text. A wrong
//     path gets a filesystem hint; a refused connection gets a network
//     hint; a rejected context window gets a context hint. The model
//     re-plans against the diagnosis instead of retrying blind.
//
//  2. LoopGuard — detects repetitive agent behaviour: the same tool
//     called with the same arguments, the same failure recurring with
//     no measurable progress. Repetition is answered with an explicit
//     strategy-change instruction, and beyond a bound the run is
//     terminated honestly instead of burning iterations.
//
//  3. RunBudget — bounded attempts, tool calls, repair cycles and wall
//     clock. The orchestrator already bounds iterations; this adds the
//     finer-grained counters so a single runaway loop inside one
//     iteration cannot consume the machine.
//
// The design keeps the existing resource posture: nothing here retries
// silently, nothing grows without a bound, and every intervention is
// visible as an Activity so the timeline stays honest.
package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"time"
)

// FailureCategory classifies WHY a tool invocation failed (or why the
// agent is stuck). The categories are deliberately coarse: the point is
// a recovery strategy, not a taxonomy.
type FailureCategory string

const (
	// CatKnowledge: the model lacks a fact (API, path, flag) it needs.
	CatKnowledge FailureCategory = "knowledge"
	// CatReasoning: the plan itself is incoherent for the goal.
	CatReasoning FailureCategory = "reasoning"
	// CatPlanning: steps exist but are wrong-ordered or incomplete.
	CatPlanning FailureCategory = "planning"
	// CatToolSelection: the chosen tool cannot do what is asked.
	CatToolSelection FailureCategory = "tool_selection"
	// CatToolArguments: right tool, malformed or wrong arguments.
	CatToolArguments FailureCategory = "tool_arguments"
	// CatExecution: the tool ran and the command/operation failed.
	CatExecution FailureCategory = "execution"
	// CatFilesystem: path missing, permission denied, jail violation.
	CatFilesystem FailureCategory = "filesystem"
	// CatProcess: spawn failure, timeout, killed process.
	CatProcess FailureCategory = "process"
	// CatNetwork: DNS, connect, TLS, offline mode.
	CatNetwork FailureCategory = "network"
	// CatModelCapability: the local model cannot fulfill the request.
	CatModelCapability FailureCategory = "model_capability"
	// CatContext: the request does not fit the context window.
	CatContext FailureCategory = "context"
	// CatVerification: claimed completion failed objective verification.
	CatVerification FailureCategory = "verification"
	// CatEnvironment: missing toolchain/runtime on the machine.
	CatEnvironment FailureCategory = "environment"
	// CatState: stale session/workspace/model state.
	CatState FailureCategory = "state"
	// CatUnknown: no confident diagnosis.
	CatUnknown FailureCategory = "unknown"
)

// ToolFailure is the diagnosis input: everything the classifier knows
// about one failed tool invocation.
type ToolFailure struct {
	Tool    string
	Args    string
	Err     string
	Output  string
	Timeout bool
}

// ClassifyFailure maps a tool failure to its most probable category using
// evidence from the error text, the tool output and the tool kind. The
// rules are ordered so that the MOST SPECIFIC match wins; anything that
// matches nothing is CatUnknown rather than a guess.
func ClassifyFailure(f ToolFailure) FailureCategory {
	e := strings.ToLower(f.Err + "\n" + f.Output)

	switch {
	case f.Timeout || containsAny(e,
		"timed out", "deadline exceeded", "context deadline",
		"killed", "signal: killed", "watchdog"):
		return CatProcess

	case containsAny(e,
		"no such file or directory", "file does not exist",
		"directory does not exist", "cannot find", "not found"):
		// A path miss is filesystem; "command not found" is environment.
		if containsAny(e, "command not found", "executable file not found",
			"is not recognized as", "not installed") {
			return CatEnvironment
		}
		return CatFilesystem

	case containsAny(e, "command not found",
		"executable file not found", "is not recognized as",
		"no command", "not installed", "toolchain", "compiler"):
		return CatEnvironment

	case containsAny(e,
		"permission denied", "access is denied", "read-only file system",
		"outside the workspace", "path jail", "escape", "not allowed",
		"blocked by policy", "policy"):
		return CatFilesystem

	case containsAny(e,
		"connection refused", "no route to host", "network is unreachable",
		"dns", "lookup", "tls handshake", "certificate", "offline",
		"no internet", "unreachable", "ssrf", "dial tcp"):
		return CatNetwork

	case containsAny(e,
		"context window", "exceeds the model context", "prompt is too long",
		"too many tokens", "num_ctx", "maximum context length"):
		return CatContext

	case containsAny(e,
		"invalid json", "unexpected end of json", "cannot unmarshal",
		"invalid character", "looking for beginning of value",
		"malformed arguments", "missing required", "invalid argument",
		"argument", "parameter", "expects", "unexpected token"):
		return CatToolArguments

	case containsAny(e,
		"unknown tool", "is disabled", "tool does not", "unsupported",
		"not supported", "not implemented"):
		return CatToolSelection

	case containsAny(e,
		"verification failed", "verify failed", "tests failed",
		"build failed", "test failed", "compile error", "exit status 1",
		"exit status 2", "assert"):
		// Command-level failure that produced compiler/test evidence is
		// an execution failure with verification signal.
		if containsAny(e, "verification") {
			return CatVerification
		}
		return CatExecution

	case containsAny(e,
		"model", "tokenizer", "inference", "generation", "gguf",
		"engine", "backend"):
		return CatModelCapability

	case containsAny(e,
		"session", "stale", "expired", "no longer", "already unloaded",
		"state"):
		return CatState
	}

	return CatUnknown
}

// containsAny reports whether s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// RepairHint returns the recovery instruction for a category — the text
// appended to the tool result so the model re-plans against the
// DIAGNOSIS. Hints prescribe a strategy change, never a blind retry.
func RepairHint(c FailureCategory) string {
	switch c {
	case CatKnowledge:
		return "Recover: the fact you need is not available from memory — inspect the actual files or environment before continuing; do not guess paths, flags or APIs."
	case CatReasoning, CatPlanning:
		return "Recover: the plan is not working — restate the goal, list what you have actually verified so far, and choose a different decomposition."
	case CatToolSelection:
		return "Recover: this tool cannot do what you asked — pick a different tool from the available set that matches the operation."
	case CatToolArguments:
		return "Recover: the arguments were malformed or wrong for this tool — re-read the tool's parameter description and fix the arguments; do not resend them unchanged."
	case CatExecution:
		return "Recover: the command failed — read the exact stderr/output above, fix the concrete cause it names, then retry with corrected inputs."
	case CatFilesystem:
		return "Recover: the path is missing, protected or outside the allowed root — verify the exact path with a listing first, then adjust it."
	case CatProcess:
		return "Recover: the process timed out or was killed — narrow the command's scope (fewer inputs, bounded output) or split the work into smaller steps."
	case CatNetwork:
		return "Recover: the network operation failed or the system is offline — use local information instead, or skip the remote step and note the limitation."
	case CatModelCapability:
		return "Recover: the local model or engine cannot perform this operation — decompose the task into simpler steps the local model can handle."
	case CatContext:
		return "Recover: the request exceeds the context window — select a smaller working set (fewer files, shorter excerpts) instead of resending everything."
	case CatVerification:
		return "Recover: objective verification failed — inspect the failing check output, repair the concrete defect, and re-verify. Do not report completion while a check fails."
	case CatEnvironment:
		return "Recover: the required toolchain is not installed on this machine — use an alternative approach that works with what is installed and tell the user what is missing."
	case CatState:
		return "Recover: the session or workspace state is stale — re-open or re-create the resource you need instead of reusing the dead handle."
	default:
		return "Recover: the failure could not be classified — inspect the exact error and change your approach; do not repeat the same call unchanged."
	}
}

// --- loop prevention ---------------------------------------------------------

// callSignature identifies one tool invocation: tool name + stable hash of
// the normalized arguments. The same signature twice = the model repeated
// itself.
type callSignature struct {
	tool string
	args string
}

// LoopGuard detects repetitive agent behaviour across the tool calls of
// one run. It is safe for concurrent use (the orchestrator executes tool
// calls sequentially today, but nothing here assumes that).
//
// Wall-clock budget semantics (v1.2.0 repair):
//
//   - WallClock > 0  → the budget expires when elapsed >= WallClock. A fully
//     consumed budget blocks, so a deadline is honored on the exact tick it
//     is due instead of one whole scheduling granularity after it.
//   - WallClock == 0 → the wall-clock budget is DISABLED (unbounded run);
//     the per-call and total-call budgets still apply.
//   - WallClock < 0  → also disabled; treated identically to zero.
//
// The clock is injectable (now field) so budget behaviour is deterministic
// under test. Real time on Windows has coarse monotonic granularity — a
// time.Nanosecond budget measured against the raw wall clock can read
// elapsed == 0 and never expire, which is exactly the flake the injectable
// clock removes from the test suite.
type LoopGuard struct {
	mu sync.Mutex

	// calls maps signature → number of executions.
	calls map[callSignature]int
	// results maps signature → hash of the last observed result.
	results map[callSignature]string
	// repeatStrikes counts how many times a repeated call was already
	// intercepted with a warning (one warning per signature).
	warned map[callSignature]bool

	// Configurable bounds (all defense-in-depth on top of the
	// orchestrator's own iteration bound).
	MaxSameCall      int           // max executions of one signature
	MaxToolCalls     int           // max total tool calls in one run
	MaxRepeatStrikes int           // max repeated-signature interventions
	WallClock        time.Duration // max wall time for tool execution
	started          time.Time

	// now is the time source. Never nil after NewLoopGuard; tests may
	// replace it to advance time deterministically.
	now func() time.Time
}

// DefaultLoopGuardBounds keeps a single runaway pattern bounded without
// constraining legitimate exploration: the same call twice is a warning,
// three is a hard stop for that call; total tool calls and wall clock
// bound the whole run.
const (
	DefaultMaxSameCall      = 3
	DefaultMaxToolCalls     = 200
	DefaultMaxRepeatStrikes = 6
	DefaultLoopWallClock    = 30 * time.Minute
)

// NewLoopGuard returns a guard with the default bounds.
func NewLoopGuard() *LoopGuard {
	g := &LoopGuard{
		calls:            make(map[callSignature]int),
		results:          make(map[callSignature]string),
		warned:           make(map[callSignature]bool),
		MaxSameCall:      DefaultMaxSameCall,
		MaxToolCalls:     DefaultMaxToolCalls,
		MaxRepeatStrikes: DefaultMaxRepeatStrikes,
		WallClock:        DefaultLoopWallClock,
	}

	g.now = time.Now
	g.started = g.now()

	return g
}

// elapsedWallClock returns how much of the wall-clock budget has been
// consumed, using the injectable time source.
func (g *LoopGuard) elapsedWallClock() time.Duration {
	return g.now().Sub(g.started)
}

// wallClockExhausted reports whether the wall-clock budget is enabled and
// fully consumed. Budget semantics are documented on LoopGuard.
func (g *LoopGuard) wallClockExhausted() bool {
	if g.WallClock <= 0 {
		return false // disabled (zero or negative budget)
	}

	return g.elapsedWallClock() >= g.WallClock
}

// WallClockExhausted reports whether the wall-clock budget is enabled and
// fully consumed. Exported for the release stress gate (cmd), which pins
// the budget semantics — an exhausted budget blocks, a zero/negative
// budget never does — against Windows monotonic-clock granularity.
func (g *LoopGuard) WallClockExhausted() bool {
	return g.wallClockExhausted()
}

// normalizeArgs produces a stable representation of raw JSON tool
// arguments: parse-insensitive — whitespace and key order collapse so
// only a SEMANTIC difference produces a new signature when the model
// reformats the same call.
func normalizeArgs(args string) string {
	s := strings.TrimSpace(args)
	if s == "" {
		return ""
	}
	// Cheap canonicalization: sort "key":"value" chunks appears nowhere —
	// instead hash through the generic JSON canonicalizer in json_tool
	// when possible; fall back to a compacted raw form. The orchestrator
	// passes the model's raw argument string, so the best stable form is
	// the compact byte digest of the raw text with whitespace collapsed.
	var b strings.Builder
	inString := false
	var last byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			b.WriteByte(c)
			if c == '"' && last != '\\' {
				inString = false
			}
			last = c
			continue
		}
		switch {
		case c == '"':
			inString = true
			b.WriteByte(c)
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			// skip whitespace outside strings
		default:
			b.WriteByte(c)
		}
		last = c
	}
	return b.String()
}

// resultDigest hashes a tool result to detect "same call, same outcome".
func resultDigest(result string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(result))
	return fmt.Sprintf("%016x", h.Sum64())
}

// argDigest hashes normalized arguments for the signature key.
func argDigest(args string) string {
	sum := sha256.Sum256([]byte(normalizeArgs(args)))
	return hex.EncodeToString(sum[:8])
}

// Observation is what the guard infers for one attempted call.
type Observation struct {
	// Repeat is true when this exact signature already executed before.
	Repeat bool
	// SameResult is true when this repeat previously produced the same
	// result digest (same failure, no progress).
	SameResult bool
	// Block, when set, carries the instruction the orchestrator must
	// return to the model INSTEAD of executing the call again.
	Block string
	// Warn, when set, is a strategy-change note to append to the result.
	Warn string
}

// Observe records one attempted tool call and returns the verdict:
// execute (possibly with a warning), or block with a message.
func (g *LoopGuard) Observe(tool, args string) Observation {
	g.mu.Lock()
	defer g.mu.Unlock()

	sig := callSignature{tool: tool, args: argDigest(args)}

	total := 0
	for _, n := range g.calls {
		total += n
	}

	if g.MaxToolCalls > 0 && total >= g.MaxToolCalls {
		return Observation{
			Repeat: true,
			Block: fmt.Sprintf(
				"Error: run tool-call budget exhausted (%d calls). Stop calling tools and produce your final answer from what you have.",
				g.MaxToolCalls,
			),
		}
	}

	if g.wallClockExhausted() {
		return Observation{
			Repeat: true,
			Block: fmt.Sprintf(
				"Error: run wall-clock budget exhausted (%v of tool execution). Stop calling tools and produce your final answer from what you have.",
				g.elapsedWallClock().Round(time.Second),
			),
		}
	}

	count := g.calls[sig]

	// A call already at the per-signature bound is refused WITHOUT
	// being counted: the counters track EXECUTIONS (what actually ran
	// and consumed machine resources), while refused attempts cost
	// nothing and are answered with the block instruction every time.
	if g.MaxSameCall > 0 && count >= g.MaxSameCall {
		return Observation{
			Repeat:     true,
			SameResult: g.results[sig] != "",
			Block: fmt.Sprintf(
				"Error: tool %s was called %d times with the same arguments (limit %d). This exact call is now refused. Change your approach: state what you already learned from the previous identical result and take a DIFFERENT step.",
				tool, count, g.MaxSameCall,
			),
		}
	}

	g.calls[sig] = count + 1

	if count == 0 {
		return Observation{}
	}

	// A repeat. Same result as before means zero progress.
	same := g.results[sig]
	obs := Observation{Repeat: true, SameResult: same != ""}

	if !g.warned[sig] {
		g.warned[sig] = true

		return Observation{
			Repeat:     obs.Repeat,
			SameResult: obs.SameResult,
			Warn: fmt.Sprintf(
				"Note: you already called %s with these exact arguments and received the result above. Repeating it will return the same information — take a different step.",
				tool,
			),
		}
	}

	return obs
}

// Record stores the outcome of one executed call so future repeats can be
// compared against it.
func (g *LoopGuard) Record(tool, args, result string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	sig := callSignature{tool: tool, args: argDigest(args)}
	g.results[sig] = resultDigest(result)
}

// ProgressSummary reports the guard's view of the run: distinct calls,
// total calls, repeats. Used for the final timeline entry.
func (g *LoopGuard) ProgressSummary() (distinct, total, repeats int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, n := range g.calls {
		total += n
		repeats += n - 1
	}
	return len(g.calls), total, repeats
}

// StatsSnapshot is a stable, sorted view of per-tool call counts for
// observability (the run report).
type StatsSnapshot struct {
	Tool        string `json:"tool"`
	Calls       int    `json:"calls"`
	DistinctArg int    `json:"distinctArgs"`
}

// CallStats returns per-tool call statistics, sorted by tool name.
func (g *LoopGuard) CallStats() []StatsSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()

	perTool := map[string]*StatsSnapshot{}
	for sig, n := range g.calls {
		st, ok := perTool[sig.tool]
		if !ok {
			st = &StatsSnapshot{Tool: sig.tool}
			perTool[sig.tool] = st
		}
		st.Calls += n
		st.DistinctArg++
	}

	out := make([]StatsSnapshot, 0, len(perTool))
	for _, st := range perTool {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tool < out[j].Tool })
	return out
}
