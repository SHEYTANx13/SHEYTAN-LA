package aisystem

// modelstudio_v1111_test.go — run #41415240891 Model Studio (§5.2).
//
// Pins the per-AI-System model-assignment contract:
//
//   - the FROZEN per-run snapshot carries the model override, the runtime
//     profile and the context/memory policy preferences (a mid-run edit
//     can never mutate a running request — the binding is complete);
//   - the model override validates as a bare model name (inherit is the
//     empty string and is DISTINCT from a missing/unreadable model);
//   - the runtime profile reuses the ONE recommendation task vocabulary
//     (no second preset list exists anywhere).
//
// Backward compatibility is pinned too: existing documents without the
// new fields load with the safe "inherit" defaults.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/recommendation"
)

func TestSnapshotFreezesModelStudioBinding(t *testing.T) {
	sys := System{
		SystemID:         "s1",
		Name:             "Coder",
		Revision:         7,
		Model:            "qwen3-coder.gguf",
		RuntimeProfile:   "coding",
		CompactionPolicy: "aggressive",
		MemoryPolicy:     "read-only",
	}

	snap := sys.Snapshot()

	if snap.Model != "qwen3-coder.gguf" {
		t.Errorf("snapshot model = %q, want the frozen override", snap.Model)
	}
	if snap.RuntimeProfile != "coding" {
		t.Errorf("snapshot runtimeProfile = %q, want the frozen profile", snap.RuntimeProfile)
	}
	if snap.CompactionPolicy != "aggressive" || snap.MemoryPolicy != "read-only" {
		t.Errorf("snapshot policies = %q/%q, want the frozen values", snap.CompactionPolicy, snap.MemoryPolicy)
	}

	// The freeze is defensive: editing the source system afterwards must
	// not mutate the snapshot.
	sys.Model = "other.gguf"
	sys.RuntimeProfile = "chat"
	if snap.Model != "qwen3-coder.gguf" || snap.RuntimeProfile != "coding" {
		t.Fatal("snapshot leaked a reference into the source system — the binding is not frozen")
	}
}

func TestSnapshotInheritStateIsEmpty(t *testing.T) {
	// The default/inherit state: no model override, no profile override.
	snap := System{Name: "Plain", Revision: 1}.Snapshot()
	if snap.Model != "" || snap.RuntimeProfile != "" {
		t.Fatalf("inherit state must be the EMPTY value, got model=%q profile=%q", snap.Model, snap.RuntimeProfile)
	}
	// Empty values marshal as absent (omitempty) — old consumers and old
	// documents round-trip unchanged.
	b := string(mustJSON(t, snap))
	if strings.Contains(b, "runtimeProfile") || strings.Contains(b, `"model"`) {
		t.Fatalf("inherit snapshot must omit the optional fields, got %s", b)
	}
}

func TestValidateModelOverride(t *testing.T) {
	cases := []struct {
		model string
		ok    bool
	}{
		{"", true},                       // inherit — the default state
		{"qwen3-coder.gguf", true},       // a bare local model name
		{"  qwen3-coder.gguf ", true},    // trimmed to the bare name
		{"../escape.gguf", false},        // path traversal
		{"dir/model.gguf", false},        // a path, not a model name
		{"model with space.gguf", false}, // whitespace is never a model name
	}

	for _, c := range cases {
		sys := System{Name: "S", Model: c.model}
		err := Validate(&sys)
		if c.ok && err != nil {
			t.Errorf("model %q: unexpected error %v", c.model, err)
		}
		if !c.ok && err == nil {
			t.Errorf("model %q: must be rejected", c.model)
		}
		if c.ok && c.model != "" && sys.Model != strings.TrimSpace(c.model) {
			t.Errorf("model %q must be trimmed, got %q", c.model, sys.Model)
		}
	}
}

func TestValidateRuntimeProfileUsesOneVocabulary(t *testing.T) {
	// Inherit.
	if err := Validate(&System{Name: "S"}); err != nil {
		t.Fatalf("empty runtime profile must validate: %v", err)
	}

	// Every supported task profile id must pass — the ONE vocabulary.
	for _, task := range recommendation.Tasks() {
		id := string(task.ID)
		sys := System{Name: "S", RuntimeProfile: id}
		if err := Validate(&sys); err != nil {
			t.Errorf("runtime profile %q (a supported task) must validate: %v", id, err)
		}
	}

	// Anything else is rejected — no second preset list, no silent map.
	for _, bad := range []string{"fastest-safe", "deep-work", "balanced", "low-memory", "manual", "turbo"} {
		sys := System{Name: "S", RuntimeProfile: bad}
		if err := Validate(&sys); err == nil {
			t.Errorf("runtime profile %q must be rejected — it is not a recommendation.Task id", bad)
		}
	}
}

func TestLegacyDocumentWithoutModelStudioFieldsLoads(t *testing.T) {
	// A pre-Model-Studio document: no model override, no profile, no
	// snapshot fields. It must load with safe inherit defaults.
	legacy := `{"systemId":"legacy","name":"Legacy","revision":3,
                "instructions":"","approvalPolicy":"ask-risky","verificationPolicy":"standard"}`

	var sys System
	if err := json.Unmarshal([]byte(legacy), &sys); err != nil {
		t.Fatalf("legacy document must load: %v", err)
	}
	if sys.Model != "" || sys.RuntimeProfile != "" || sys.CompactionPolicy != "" || sys.MemoryPolicy != "" {
		t.Fatalf("legacy document must inherit (empty) Model Studio fields, got %+v", sys)
	}
	if err := Validate(&sys); err != nil {
		t.Fatalf("legacy document must validate unchanged: %v", err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	return data
}
