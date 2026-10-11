package agent

// spec_cache_v176_test.go — v1.7.6: the LAST name-keyed staleness window in
// the spec cache is closed by generation-checked entries.
//
// The v1.7.5 state closed the container race (Tools() snapshot) and the
// content race (customtools deep copies). One theoretical window remained:
// a reader that captured a pre-replacement Tool from a snapshot could
// re-populate the name-keyed cache with the OLD tool's schema AFTER a
// concurrent Register+Invalidate, and a later reader could then be served
// the superseded metadata under the same tool name.
//
// The v1.7.6 fix: every spec cache entry carries the registry generation it
// was built from; a hit is served only when the reader's generation matches
// (specCache.Spec). Register/Unregister bump the generation under toolsMu;
// ToolsAt() returns the snapshot+generation pair atomically.
//
// These tests are DETERMINISTIC: the superseded interleaving is driven
// exactly (write-back after invalidate, read by a later generation) with no
// goroutines and no sleeps, plus a registry-level proof through the real
// Register path. TestToolMetadataRaceDuringDefinitionChurn exercises the
// same cache concurrently under -race.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// descTool is a tool double whose description identifies the version.
type descTool struct {
	name string
	desc string
}

func (d *descTool) Name() string        { return d.name }
func (d *descTool) Description() string { return d.desc }
func (d *descTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (d *descTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return "ok", nil
}

// TestSpecCacheNeverServesSupersededGeneration drives the exact
// superseded-reader interleaving through the cache: build at gen 1 →
// Invalidate (Register) → the stale gen-1 reader writes its entry back →
// a gen-2 reader for the replaced tool must NEVER receive the gen-1 schema.
func TestSpecCacheNeverServesSupersededGeneration(t *testing.T) {
	c := newSpecCache()

	v1 := &descTool{name: "echo", desc: "SCHEMA-VERSION-1"}
	const gen1 = uint64(1)

	s1, tokens1 := c.Spec(v1, gen1)
	if !strings.Contains(s1, "SCHEMA-VERSION-1") {
		t.Fatalf("first build returned wrong schema: %s", s1)
	}
	if tokens1 <= 0 {
		t.Fatalf("token estimate = %d, want > 0", tokens1)
	}

	// The registry moves on: Register → Invalidate + generation bump (gen 2).
	c.Invalidate()

	// The superseded reader (still holding its gen-1 tool reference)
	// re-populates the cache AFTER the invalidate — the exact write-back a
	// concurrent re-register produces. At its OWN generation this is
	// correct behavior for that reader's operation.
	s1again, _ := c.Spec(v1, gen1)
	if !strings.Contains(s1again, "SCHEMA-VERSION-1") {
		t.Fatalf("same-generation rebuild lost: %s", s1again)
	}

	// A gen-2 reader for the REPLACED tool (same name, new definition) must
	// not be served the gen-1 entry recorded under that name.
	v2 := &descTool{name: "echo", desc: "SCHEMA-VERSION-2"}
	s2, _ := c.Spec(v2, 2)
	if !strings.Contains(s2, "SCHEMA-VERSION-2") || strings.Contains(s2, "SCHEMA-VERSION-1") {
		t.Fatalf("superseded generation served to a later reader: %s", s2)
	}

	// The gen-2 entry is now memoized: a repeat hit returns v2.
	s2again, _ := c.Spec(v2, 2)
	if !strings.Contains(s2again, "SCHEMA-VERSION-2") {
		t.Fatalf("gen-2 memoized hit corrupted: %s", s2again)
	}
}

// TestRegistryGenerationBumpsOnRegisterUnregister proves the generation the
// spec builds stamp comes from the REAL registry mutations — not a parallel
// counter that could drift from Register/Unregister.
func TestRegistryGenerationBumpsOnRegisterUnregister(t *testing.T) {
	o := New(nil, nil)

	gen0 := o.ToolsGeneration()

	o.Register(&descTool{name: "probe", desc: "d1"})
	gen1 := o.ToolsGeneration()

	o.Unregister("probe")
	gen2 := o.ToolsGeneration()

	if !(gen0 < gen1 && gen1 < gen2) {
		t.Fatalf("generation must strictly increase per mutation: %d -> %d -> %d", gen0, gen1, gen2)
	}
}

// TestToolsAtSnapshotMatchesGeneration proves the atomic pair: the snapshot
// returned by ToolsAt is exactly the registry state at the returned
// generation, and Register after the snapshot advances the generation.
func TestToolsAtSnapshotMatchesGeneration(t *testing.T) {
	o := New(nil, nil)
	o.Register(&descTool{name: "probe", desc: "d1"})

	snap, gen := o.ToolsAt()

	if _, ok := snap["probe"]; !ok {
		t.Fatal("snapshot missing registered tool")
	}

	o.Register(&descTool{name: "probe", desc: "d2"})

	if o.ToolsGeneration() == gen {
		t.Fatal("Register after ToolsAt must advance the generation (the stale-reader window)")
	}

	// The stale reader (old snap/gen) rebuilding its spec must not poison
	// the new generation's view: build at the OLD gen, then read at the NEW.
	data, _ := o.specs.Spec(&descTool{name: "probe", desc: "d1"}, gen)
	if !strings.Contains(data, "d1") {
		t.Fatalf("stale reader lost its own generation's schema: %s", data)
	}

	fresh, _ := o.specs.Spec(&descTool{name: "probe", desc: "d2"}, o.ToolsGeneration())
	if !strings.Contains(fresh, "d2") || strings.Contains(fresh, "d1") {
		t.Fatalf("current generation served superseded metadata: %s", fresh)
	}
}
