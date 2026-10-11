package agent

// registry_snapshot_test.go — v1.7.2 P0 crash regression.
//
// THE SUPPLIED WINDOWS CRASH (v1.7.2, local provider, one-word chat):
// the log ends at `task classified...`; the next expected line is
// `tier selected...`. Between those two markers RunDetailed executes:
//
//	task classified (log)
//	resolveEffectiveContext
//	RAMInfo
//	len(o.tools) + for name := range o.Tools()   <-- THE CRASH WINDOW
//	toolsets.SelectForTask
//	specs.BuildSpecs
//	taskclassify.SelectTier
//	tier selected (log)
//
// The registry mutators that run CONCURRENTLY with a user chat in the
// real desktop stack:
//
//   - internal/api/customtools.go: HTTP handlers call Register/Unregister
//     (custom tools are first-class registry citizens, v1.6.0);
//   - internal/runtime/automation.go taskRunner: RegisterInto registers
//     task-scoped tools and `defer unregister()` removes them — while a
//     user chat may be mid-run;
//   - internal/native/engine/runtime.go: four unregister call sites.
//
// When any of those writes lands while RunDetailed iterates the map
// returned by the old `Tools()` (which handed out the LIVE internal map),
// Go throws
//
//	fatal error: concurrent map iteration and map write
//
// which is NOT a recoverable panic: the whole desktop process dies
// instantly with no terminal error event — exactly the supplied
// "application then crashed when the user chatted" signature.
//
// This file reproduces that window deterministically and pins the fix:
// Tools() must return an immutable snapshot; no caller may retain the
// internal map.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// mutatorTool is a tiny custom-shaped tool like the runtime ones.
type mutatorTool struct {
	name string
}

func (m *mutatorTool) Name() string        { return m.name }
func (m *mutatorTool) Description() string { return "mutator stress tool" }
func (m *mutatorTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (m *mutatorTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return "ok", nil
}

// TestToolsSnapshotIsImmutableAtCrashWindow reproduces the v1.7.2 Windows
// chat crash window: concurrent registry mutation while RunDetailed walks
// the tool surface between classification and tier selection.
//
// Run with -race this test FAILS on the defective build (DATA RACE on the
// tools map); without -race the same interleaving is the process-fatal
// `concurrent map iteration and map write`. With the snapshot fix it is
// clean under both.
func TestToolsSnapshotIsImmutableAtCrashWindow(t *testing.T) {
	// A chat-ish engine: enough turns to keep the run alive while the
	// mutator goroutines hammer the registry from "HTTP handler" and
	// "task runner" style goroutines.
	engine, turns := newFakeEngine(t, func(turn int, _ map[string]any) string {
		time.Sleep(5 * time.Millisecond) // widen the crash window
		return sseChunk(fmt.Sprintf("reply-%d.", turn)) + sseDone
	})

	cfg := remoteConfig(t, engine.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	// Seed the registry the way the runtime does.
	for i := 0; i < 8; i++ {
		orch.Register(&mutatorTool{name: fmt.Sprintf("seed-%d", i)})
	}

	var wg sync.WaitGroup

	// Mutator A — the custom-tools HTTP handler pattern: repeated
	// register/unregister of first-class custom tools.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 400; i++ {
			name := fmt.Sprintf("custom-%d", i%12)
			orch.Register(&mutatorTool{name: name})
			orch.Unregister(name)
		}
	}()

	// Mutator B — the task-runner pattern: register a scope, run, tear it
	// down (defer unregister) — concurrently with the user chat.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			name := fmt.Sprintf("task-%d", i%4)
			orch.Register(&mutatorTool{name: name})
			// "task body" — the chat run happens here.
			orch.Unregister(name)
		}
	}()

	// The user's one-word chat through the REAL RunDetailed path
	// (classification → tool surface walk → tier selection).
	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "hi"}},
		func(Activity) {},
	)

	wg.Wait()

	if err != nil {
		t.Fatalf("RunDetailed must survive concurrent registry mutation: %v", err)
	}
	if res.Text == "" {
		t.Fatal("assistant reply must be non-empty")
	}
	if *turns == 0 {
		t.Fatal("the engine must have been reached at least once")
	}
}

// TestToolsSnapshotIsACopy proves the snapshot contract directly: the
// returned map is a private copy, so a later mutation of the registry
// never writes into a map a caller holds, and the snapshot's contents
// are stable.
func TestToolsSnapshotIsACopy(t *testing.T) {
	engine, _ := newFakeEngine(t, func(turn int, _ map[string]any) string {
		return sseChunk("ok.") + sseDone
	})

	cfg := remoteConfig(t, engine.URL)
	orch := New(config.NewSource(cfg), llm.NewClient(config.NewSource(cfg)))

	orch.Register(&mutatorTool{name: "one"})
	orch.Register(&mutatorTool{name: "two"})

	snap := orch.Tools()

	// Mutate AFTER the snapshot: the snapshot must not change, and no
	// concurrent-iteration hazard may exist for callers holding it.
	orch.Register(&mutatorTool{name: "three"})
	orch.Unregister("one")

	if len(snap) != 2 {
		t.Fatalf("snapshot must be isolated from later mutations: want 2, got %d", len(snap))
	}
	if _, ok := snap["one"]; !ok {
		t.Fatal("snapshot captured at read time must still contain the tool that was later unregistered")
	}
	if _, ok := snap["three"]; ok {
		t.Fatal("snapshot must not observe tools registered after the snapshot was taken")
	}

	live := orch.Tools()
	if _, ok := live["one"]; ok {
		t.Fatal("the live registry must reflect the unregister")
	}
	if _, ok := live["three"]; !ok {
		t.Fatal("the live registry must reflect the register")
	}
}

// TestRegistryStressConcurrentSnapshots is the broad stress the repair
// contract requires: many concurrent snapshot readers while registering
// and unregistering (including custom tools), with deterministic
// validation of snapshot integrity — no fatal map race, no torn state.
func TestRegistryStressConcurrentSnapshots(t *testing.T) {
	engine, _ := newFakeEngine(t, func(turn int, _ map[string]any) string {
		return sseChunk("ok.") + sseDone
	})

	cfg := remoteConfig(t, engine.URL)
	orch := New(config.NewSource(cfg), llm.NewClient(config.NewSource(cfg)))

	for i := 0; i < 16; i++ {
		orch.Register(&mutatorTool{name: fmt.Sprintf("base-%d", i)})
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Readers: hammer Tools() and validate every snapshot is internally
	// consistent (names map to non-nil tools; count is sane).
	for r := 0; r < 6; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}

				snap := orch.Tools()
				for name, tool := range snap {
					if tool == nil {
						t.Errorf("snapshot entry %q must never be nil", name)
						return
					}
					if tool.Name() != name {
						t.Errorf("snapshot key %q does not match tool name %q", name, tool.Name())
						return
					}
				}
			}
		}()
	}

	// Writers: register + unregister scopes concurrently, like the
	// custom-tools API and the task runner do.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				name := fmt.Sprintf("w%d-%d", w, i%10)
				orch.Register(&mutatorTool{name: name})
				orch.Unregister(name)
			}
		}(w)
	}

	// Drain writers deterministically, then release readers.
	time.Sleep(50 * time.Millisecond)
	go func() {
		// writers finish on their own; poll until base set stabilizes
		for {
			snap := orch.Tools()
			if len(snap) >= 16 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Give writers a bounded window, then stop readers.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := orch.Tools()
		if len(snap) >= 16 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	close(stop)
	wg.Wait()

	// Final state: base tools intact, no writer residue.
	final := orch.Tools()
	for i := 0; i < 16; i++ {
		name := fmt.Sprintf("base-%d", i)
		if _, ok := final[name]; !ok {
			t.Fatalf("base tool %q must survive the stress", name)
		}
	}
	for i := 0; i < 10; i++ {
		for w := 0; w < 4; w++ {
			name := fmt.Sprintf("w%d-%d", w, i)
			if _, ok := final[name]; ok {
				t.Fatalf("writer tool %q must be unregistered after the stress", name)
			}
		}
	}
}
