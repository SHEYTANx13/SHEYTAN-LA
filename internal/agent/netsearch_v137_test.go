package agent

// v1.3.7 regression: the explicit `netSearch` request intent is wired
// end-to-end on the SERVER side. The v1.3.6 UI removal of the Research
// workspace made Net Search the replacement surface; these tests pin the
// backend half of the chain that no Go test covered:
//
//	POST /api/run {netSearch:true}
//	→ WithNetSearch(true)
//	→ runOptions.netSearch + ToolPolicy.NetSearch
//	→ ToolPolicy.allows("research") == true under ANY mode
//	→ the offered-surface guarantee appends the research tool
//
// plus the boundary: the intent authorizes EXACTLY the research tool —
// never any other tool.

import (
	"context"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

func TestWithNetSearchSetsPolicyIntent(t *testing.T) {
	var ro runOptions

	WithNetSearch(true)(&ro)

	if !ro.netSearch {
		t.Fatal("WithNetSearch(true) must set runOptions.netSearch — the telemetry/log surface")
	}

	if !ro.toolPolicy.NetSearch {
		t.Fatal("WithNetSearch(true) must set ToolPolicy.NetSearch — the enforcement surface")
	}

	// The off state clears both surfaces.
	var roOff runOptions

	WithNetSearch(false)(&roOff)

	if roOff.netSearch || roOff.toolPolicy.NetSearch {
		t.Fatal("WithNetSearch(false) must clear the intent on both surfaces")
	}
}

func TestNetSearchPolicyAuthorizesResearchToolUnderEveryMode(t *testing.T) {
	for _, mode := range []string{ToolPolicyAuto, ToolPolicyManual} {
		p := ToolPolicy{Mode: mode, NetSearch: true}

		if !p.allows("research") {
			t.Fatalf("an explicit Net Search request must authorize the research tool under mode %v", mode)
		}

		// Case-insensitive match — the tool registry is canonical-lower.
		if !p.allows("Research") {
			t.Fatalf("the research authorization must match case-insensitively (mode %v)", mode)
		}
	}

	// Without the intent, manual mode keeps its gate.
	manual := ToolPolicy{Mode: ToolPolicyManual, NetSearch: false}

	if manual.allows("research") {
		t.Fatal("manual mode without Net Search must NOT blanket-authorize the research tool")
	}
}

func TestNetSearchIntentNeverExtendsToOtherTools(t *testing.T) {
	// Manual mode, nothing allowed, Net Search on: the intent authorizes
	// EXACTLY research — the boundary the v1.3.6 contract demands.
	p := ToolPolicy{Mode: ToolPolicyManual, NetSearch: true}

	for _, other := range []string{"shell", "webSearch", "browser", "fileWrite", "git"} {
		if p.allows(other) {
			t.Fatalf("the Net Search intent must never authorize %q — it extends to exactly the research tool", other)
		}
	}
}

func TestNetSearchOfferedSurfaceAppendsResearch(t *testing.T) {
	// End-to-end over the REAL composer path: manual mode offers only
	// "echo"; an explicit Net Search request must add the research tool
	// to the offered surface (and authorize executing it), while the
	// out-of-policy shell tool stays refused.
	seen := &engineSeen{}
	refused := ""

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)

		if turn == 1 {
			// The model reaches for a tool OUTSIDE the manual selection
			// (but not research) — the boundary must hold.
			return sseToolCall("call-1", "shell", `{"command":"ls"}`) + sseDone
		}

		return sseChunk("net-searched.") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	orch.Register(&fakeTool{name: "echo"})
	orch.Register(&fakeTool{name: "research"})
	orch.Register(&fakeTool{name: "shell"})

	if _, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "search the web for the answer"}},
		func(a Activity) {
			if a.Type == "tool_end" && strings.Contains(a.Caption, "shell") {
				if d, ok := a.Detail.(string); ok {
					refused = d
				}
			}
		},
		WithToolPolicy("manual", []string{"echo"}),
		WithNetSearch(true),
	); err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	offered := map[string]bool{}
	for _, name := range seen.tools {
		offered[name] = true
	}

	if !offered["echo"] {
		t.Fatalf("the manual selection must stay offered, got %v", seen.tools)
	}

	if !offered["research"] {
		t.Fatalf("an explicit Net Search request must guarantee the research tool in the offered surface, got %v", seen.tools)
	}

	if offered["shell"] {
		t.Fatalf("the Net Search intent must NOT leak other tools into the offered surface, got %v", seen.tools)
	}

	if !strings.Contains(refused, "manual tool selection") {
		t.Fatalf("the out-of-policy shell call must stay refused, got %q", refused)
	}
}
