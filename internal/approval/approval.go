// Package approval implements SHEYTAN's ONE tool-risk classification and
// approval vocabulary (v1.9.0, spec §11).
//
// Every model-facing tool call carries one of five risk classes. The
// class is derived DETERMINISTICALLY (tool identity + normalized
// arguments) — never from model self-reporting. Consumers:
//
//   - the orchestrator's approval seam (agent.RegisterApprovalGate) —
//     a gate decides whether a call may execute under the ACTIVE AI
//     System's approval policy; a denied call never executes and the
//     denial is evidence;
//   - the Goal engine's approval parking — a pending approval with the
//     exact normalized call identity persists durably (survives reload),
//     and only the exact approved call resumes.
//
// This package owns CLASSIFICATION and DECISION POLICY only. It never
// executes anything and never widens permissions: a gate can only DENY,
// never grant beyond the existing tool-policy/permission authorities.
package approval

import (
	"strings"
	"sync"
)

// Risk classes (the one authoritative vocabulary).
const (
	RiskReadOnly        = "read-only"
	RiskWorkspaceWrite  = "workspace-write"
	RiskExternalNetwork = "external-network"
	RiskDestructive     = "destructive"
	RiskPrivileged      = "privileged/host-level"
)

// Policies (mirrors aisystem.Approval*; kept dependency-free so BOTH the
// orchestrator and the goal engine can import without cycles).
const (
	PolicyAuto     = "auto"      // nothing asks
	PolicyAskRisky = "ask-risky" // external-network/destructive/privileged ask
	PolicyAskAll   = "ask-all"   // every call asks
)

// ClassifyRisk derives the risk class of one tool call from its identity
// and normalized arguments. Unknown tools classify conservatively as
// workspace-write (never silently read-only).
func ClassifyRisk(toolName string, args map[string]any) string {
	name := strings.ToLower(strings.TrimSpace(toolName))
	switch name {
	case "memory", "recall", "repo_search", "dataanalysis", "data_analysis",
		"json", "diff", "screenshot", "files.info", "sysinfo", "histref":
		return RiskReadOnly
	case "research", "fetch", "net_search", "browser":
		return RiskExternalNetwork
	case "mcp_tool", "mcp":
		// External tool execution is network-class by default; the MCP
		// allow-list may narrow it, never widen it.
		return RiskExternalNetwork
	case "codeexec", "lab", "coding_lab", "pipeline":
		return RiskWorkspaceWrite
	}

	switch name {
	case "shell":
		return classifyShell(args)
	case "files":
		return classifyFiles(args)
	case "git":
		return classifyGit(args)
	}
	return RiskWorkspaceWrite
}

// destructive shell vocabulary — exact-token prefix checks on the
// command string (deterministic; no model involvement).
var destructiveFragments = []string{
	"rm -rf", "rm -fr", "rmdir /s", "del /f", "del /s", "rd /s",
	"format ", "mkfs", "dd if=", "> /dev/", "shred", "wipefs",
	":(){ :|:&", "chmod -r 000", "taskkill /f", "kill -9 1",
}

var privilegedFragments = []string{
	"sudo ", "sudo\t", "doas ", "su ", "runas ", "pkexec ",
	"systemctl ", "launchctl ", "reg add", "regedit", "sc create",
}

func classifyShell(args map[string]any) string {
	cmd := strings.ToLower(argsString(args, "command"))
	if cmd == "" {
		cmd = strings.ToLower(argsString(args, "cmd"))
	}
	for _, frag := range privilegedFragments {
		if strings.Contains(cmd, frag) {
			return RiskPrivileged
		}
	}
	for _, frag := range destructiveFragments {
		if strings.Contains(cmd, frag) {
			return RiskDestructive
		}
	}
	return RiskWorkspaceWrite
}

func classifyFiles(args map[string]any) string {
	action := strings.ToLower(argsString(args, "action"))
	switch action {
	case "read", "list", "tree", "search", "info":
		return RiskReadOnly
	case "delete", "replace":
		return RiskDestructive
	default:
		return RiskWorkspaceWrite
	}
}

func classifyGit(args map[string]any) string {
	action := strings.ToLower(argsString(args, "action"))
	switch action {
	case "status", "log", "diff", "show", "branch":
		return RiskReadOnly
	case "push", "reset", "rebase", "clean":
		return RiskDestructive
	default:
		return RiskWorkspaceWrite
	}
}

func argsString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// RequiresAsk reports whether one call must pause for approval under the
// given policy. read-only never asks; ask-all asks for everything;
// ask-risky asks exactly for external-network, destructive and
// privileged calls.
func RequiresAsk(policy string, risk string) bool {
	switch NormalizePolicy(policy) {
	case PolicyAuto:
		return false
	case PolicyAskAll:
		return risk != RiskReadOnly
	default: // ask-risky
		switch risk {
		case RiskExternalNetwork, RiskDestructive, RiskPrivileged:
			return true
		}
		return false
	}
}

// NormalizePolicy maps unknown vocabularies to the safe default.
func NormalizePolicy(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case PolicyAuto:
		return PolicyAuto
	case PolicyAskAll:
		return PolicyAskAll
	default:
		return PolicyAskRisky
	}
}

// ---------------------------------------------------------------------------
// Decision cache — the EXACT approved call identity (spec §11: resume
// executes only the exact approved call; reject stale/malformed
// approvals by binding the decision to the normalized call identity).
// ---------------------------------------------------------------------------

// CallKey builds the normalized identity of one tool call: the exact
// (tool, risk, canonical-args) triple. An approval decision is valid
// ONLY for this key.
func CallKey(toolName string, args map[string]any) string {
	canonical := canonicalArgs(args)
	return toolName + "|" + ClassifyRisk(toolName, args) + "|" + canonical
}

func canonicalArgs(args map[string]any) string {
	if len(args) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(strings.TrimSpace(argsString(args, k)))
		b.WriteString(";")
	}
	return b.String()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Ledger is the durable decision ledger for one run/goal: exact approved
// call keys, exact rejected keys. Concurrency-safe, bounded.
type Ledger struct {
	mu         sync.Mutex
	approved   map[string]int
	rejected   map[string]int
	maxEntries int
}

// NewLedger creates a bounded decision ledger.
func NewLedger(maxEntries int) *Ledger {
	if maxEntries <= 0 {
		maxEntries = 64
	}
	return &Ledger{
		approved:   map[string]int{},
		rejected:   map[string]int{},
		maxEntries: maxEntries,
	}
}

// Approve records an approval for the EXACT call key.
func (l *Ledger) Approve(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bound()
	l.approved[key]++
}

// Reject records a rejection for the EXACT call key.
func (l *Ledger) Reject(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bound()
	l.rejected[key]++
}

// Approved reports whether the exact key was approved.
func (l *Ledger) Approved(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.approved[key] > 0
}

// Rejected reports whether the exact key was rejected.
func (l *Ledger) Rejected(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rejected[key] > 0
}

func (l *Ledger) bound() {
	if len(l.approved) > l.maxEntries {
		for k := range l.approved {
			delete(l.approved, k)
			break
		}
	}
	if len(l.rejected) > l.maxEntries {
		for k := range l.rejected {
			delete(l.rejected, k)
			break
		}
	}
}
