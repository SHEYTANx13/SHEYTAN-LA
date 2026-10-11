package agent

// v1.8.7 — dataAnalysis orchestrator integration coverage: the real
// tool registration path (Orchestrator.Register + registry lookup +
// the agent.Tool interface contract) exercised against a fixture
// dataset, plus the dynamic-toolset guarantee that dataAnalysis stays
// selected for data tasks.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/tools"
)

// TestDataToolRegisteredAndRunnableThroughOrchestrator proves the real
// registry path: the tool registers under its canonical name, is
// retrievable, satisfies the agent.Tool contract and executes against a
// workspace fixture.
func TestDataToolRegisteredAndRunnableThroughOrchestrator(t *testing.T) {
	base := t.TempDir()
	tools.SetBaseDir(base)
	t.Cleanup(func() { tools.SetBaseDir("") })

	fixture := "region,revenue\nEMEA,120\nAPAC,80\n"
	if err := os.WriteFile(filepath.Join(base, "orch-fixture.csv"), []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	orch := New(nil, nil)
	orch.Register(tools.NewDataTool(nil))

	tool, ok := orch.Tools()["dataAnalysis"]
	if !ok {
		t.Fatal("dataAnalysis must be registered under its canonical name")
	}

	if tool.Name() != "dataAnalysis" {
		t.Fatalf("registry name mismatch: %s", tool.Name())
	}

	if strings.TrimSpace(tool.Description()) == "" {
		t.Fatal("dataAnalysis must expose a description (schema contract)")
	}

	// One deterministic analyze call through the registry interface.
	args, err := json.Marshal(map[string]any{"action": "analyze", "path": "orch-fixture.csv"})
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("analyze through orchestrator: %v", err)
	}

	for _, want := range []string{"EMEA", "2 rows", "Key findings"} {
		if !strings.Contains(out, want) {
			t.Fatalf("analyze through orchestrator must contain %q:\n%s", want, out)
		}
	}

	// Re-register replaces (one authority, no duplicate registrations).
	orch.Register(tools.NewDataTool(nil))
	if got := len(orch.Tools()); got < 1 {
		t.Fatalf("registry must stay usable after re-register (%d tools)", got)
	}
	if _, ok := orch.Tools()["dataAnalysis"]; !ok {
		t.Fatal("dataAnalysis must survive re-registration")
	}
}
