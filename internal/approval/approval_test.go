package approval

import (
	"strings"
	"testing"
)

func TestClassifyRiskMatrix(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"memory", nil, RiskReadOnly},
		{"repo_search", nil, RiskReadOnly},
		{"dataanalysis", nil, RiskReadOnly},
		{"research", nil, RiskExternalNetwork},
		{"fetch", map[string]any{"url": "https://x"}, RiskExternalNetwork},
		{"browser", nil, RiskExternalNetwork},
		{"shell", map[string]any{"command": "ls -la"}, RiskWorkspaceWrite},
		{"shell", map[string]any{"command": "rm -rf /usr/share"}, RiskDestructive},
		{"shell", map[string]any{"command": "sudo apt install x"}, RiskPrivileged},
		{"files", map[string]any{"action": "read"}, RiskReadOnly},
		{"files", map[string]any{"action": "write"}, RiskWorkspaceWrite},
		{"files", map[string]any{"action": "delete"}, RiskDestructive},
		{"git", map[string]any{"action": "push"}, RiskDestructive},
		{"git", map[string]any{"action": "status"}, RiskReadOnly},
		{"totally_unknown_tool", nil, RiskWorkspaceWrite}, // conservative default
	}
	for _, c := range cases {
		got := ClassifyRisk(c.tool, c.args)
		if got != c.want {
			t.Errorf("ClassifyRisk(%q) = %q, want %q", c.tool, got, c.want)
		}
	}
}

func TestRequiresAskDefaults(t *testing.T) {
	// read-only NEVER asks, under any policy.
	for _, pol := range []string{PolicyAuto, PolicyAskRisky, PolicyAskAll} {
		if RequiresAsk(pol, RiskReadOnly) {
			t.Fatalf("read-only must never ask (policy %s)", pol)
		}
	}
	if RequiresAsk(PolicyAuto, RiskDestructive) {
		t.Fatalf("auto must never ask")
	}
	if !RequiresAsk(PolicyAskRisky, RiskDestructive) ||
		!RequiresAsk(PolicyAskRisky, RiskPrivileged) ||
		!RequiresAsk(PolicyAskRisky, RiskExternalNetwork) {
		t.Fatalf("ask-risky must ask for external-network/destructive/privileged")
	}
	if RequiresAsk(PolicyAskRisky, RiskWorkspaceWrite) {
		t.Fatalf("ask-risky must NOT ask for workspace writes")
	}
	if !RequiresAsk(PolicyAskAll, RiskWorkspaceWrite) {
		t.Fatalf("ask-all must ask even for workspace writes")
	}
	if RequiresAsk("unknown-garbage", RiskWorkspaceWrite) {
		t.Fatalf("unknown policy must normalize to the safe default (workspace writes stay allowed, risky asks)")
	}
}

func TestCallKeyBindsExactIdentity(t *testing.T) {
	a := CallKey("shell", map[string]any{"command": "make test"})
	b := CallKey("shell", map[string]any{"command": "make test"})
	if a != b {
		t.Fatalf("identical calls must share one identity")
	}
	c := CallKey("shell", map[string]any{"command": "make clean"})
	if a == c {
		t.Fatalf("different args must produce different identities")
	}
	// Argument ORDER is normalized: the same call in different key order
	// is the SAME identity (spec: bind to the exact normalized call).
	d := CallKey("files", map[string]any{"action": "write", "path": "x"})
	e := CallKey("files", map[string]any{"path": "x", "action": "write"})
	if d != e {
		t.Fatalf("argument order must normalize to one identity")
	}
	if !strings.Contains(a, "workspace-write") {
		t.Fatalf("the identity must carry the risk class")
	}
}

func TestLedgerExactCallBinding(t *testing.T) {
	l := NewLedger(8)
	key := CallKey("fetch", map[string]any{"url": "https://example.com"})
	other := CallKey("fetch", map[string]any{"url": "https://other.com"})

	l.Approve(key)
	if !l.Approved(key) {
		t.Fatalf("approved key must read approved")
	}
	if l.Approved(other) {
		t.Fatalf("a DIFFERENT call must not inherit the approval (exact-call binding)")
	}
	l.Reject(other)
	if !l.Rejected(other) || l.Rejected(key) {
		t.Fatalf("rejection must bind to its exact key only")
	}
}
