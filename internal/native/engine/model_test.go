package engine

// model_test.go — native model lifecycle tests (v1.1.5 Phase 2).
//
// Exercises the Go-side model state machine against the fake host
// (protocol-exact), covering the required matrix:
//
//      Load A → Load A again → Load B → Unload → Reload → failed load
//
// plus state resets across host lifecycle boundaries, bounded load
// timeouts and concurrent safe inspection (race-detector targets).

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func startFakeEngine(t *testing.T, mode string) *Engine {
	t.Helper()

	e := newFakeEngine(t, mode)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start fake engine: %v", err)
	}

	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = e.Stop(stopCtx)
	})

	return e
}

func writeTestModelFile(t *testing.T, name string, content []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEngineModelLoadInfoUnloadLifecycle(t *testing.T) {
	e := startFakeEngine(t, "")

	if state := e.NativeModelState(); state != ModelStateUnloaded {
		t.Fatalf("fresh engine model state = %q, want unloaded", state)
	}

	model := writeTestModelFile(t, "model-a.gguf", []byte("fake-gguf-bytes"))

	ctx := context.Background()

	if err := e.LoadModel(ctx, ModelSpec{Path: model}); err != nil {
		t.Fatalf("load model: %v", err)
	}

	if state := e.NativeModelState(); state != ModelStateLoaded {
		t.Fatalf("model state after load = %q, want loaded", state)
	}

	info, err := e.ModelInfo(ctx)
	if err != nil {
		t.Fatalf("model info: %v", err)
	}

	if !info.Loaded || info.State != ModelStateLoaded {
		t.Fatalf("model info after load: %+v", info)
	}
	if info.Model == nil {
		t.Fatal("model info after load carries no model card")
	}
	if info.Model.Path != model || info.Model.Architecture != "llama" ||
		info.Model.ContextLength != 256 || info.Model.VocabularySize != 96 ||
		info.Model.LayerCount != 2 || info.Model.TensorCount != 3 {
		t.Fatalf("unexpected model card: %+v", info.Model)
	}
	if info.Memory == nil || info.Memory.WeightsBytes == 0 || info.Memory.TotalBytes == 0 {
		t.Fatalf("unexpected memory plan: %+v", info.Memory)
	}
	if info.Memory.FitsInRAM != 1 {
		t.Fatalf("fake plan fits-in-ram = %d, want 1", info.Memory.FitsInRAM)
	}

	// Unload, then unload again (idempotent).
	if err := e.UnloadModel(ctx); err != nil {
		t.Fatalf("unload: %v", err)
	}
	if err := e.UnloadModel(ctx); err != nil {
		t.Fatalf("second unload must be a no-op: %v", err)
	}

	if state := e.NativeModelState(); state != ModelStateUnloaded {
		t.Fatalf("model state after unload = %q, want unloaded", state)
	}

	info, err = e.ModelInfo(ctx)
	if err != nil {
		t.Fatalf("model info after unload: %v", err)
	}
	if info.Loaded || info.State != ModelStateUnloaded || info.Model != nil {
		t.Fatalf("unexpected model info after unload: %+v", info)
	}
}

func TestEngineLoadModelValidation(t *testing.T) {
	e := startFakeEngine(t, "")

	ctx := context.Background()

	// Empty path.
	if err := e.LoadModel(ctx, ModelSpec{Path: ""}); err == nil {
		t.Fatal("empty path accepted")
	}
	if state := e.NativeModelState(); state != ModelStateUnloaded {
		t.Fatalf("empty-path rejection changed state to %q", state)
	}

	// Missing file: a real attempt → failed state with detail.
	if err := e.LoadModel(ctx, ModelSpec{Path: filepath.Join(t.TempDir(), "nope.gguf")}); err == nil {
		t.Fatal("missing file accepted")
	}
	if state := e.NativeModelState(); state != ModelStateFailed {
		t.Fatalf("missing-file state = %q, want failed", state)
	}

	// Directory.
	dir := t.TempDir()
	if err := e.LoadModel(ctx, ModelSpec{Path: dir}); err == nil {
		t.Fatal("directory accepted")
	}
	if state := e.NativeModelState(); state != ModelStateFailed {
		t.Fatalf("directory state = %q, want failed", state)
	}

	// Unload clears the failure.
	if err := e.UnloadModel(ctx); err != nil {
		t.Fatalf("unload after failure: %v", err)
	}
	if state := e.NativeModelState(); state != ModelStateUnloaded {
		t.Fatalf("state after unload = %q, want unloaded", state)
	}
}

func TestEngineModelLoadReplaceSemantics(t *testing.T) {
	e := startFakeEngine(t, "")

	modelA := writeTestModelFile(t, "model-a.gguf", []byte("A"))
	modelB := writeTestModelFile(t, "model-b.gguf", []byte("B"))

	ctx := context.Background()

	// Load A, then A again (same path).
	if err := e.LoadModel(ctx, ModelSpec{Path: modelA}); err != nil {
		t.Fatalf("load A: %v", err)
	}
	if err := e.LoadModel(ctx, ModelSpec{Path: modelA}); err != nil {
		t.Fatalf("load A again: %v", err)
	}

	info, err := e.ModelInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Loaded || info.Model == nil || info.Model.Path != modelA {
		t.Fatalf("after load A twice: %+v", info)
	}

	// Load B replaces A.
	if err := e.LoadModel(ctx, ModelSpec{Path: modelB}); err != nil {
		t.Fatalf("load B: %v", err)
	}

	info, err = e.ModelInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Loaded || info.Model == nil || info.Model.Path != modelB {
		t.Fatalf("after load B: %+v", info)
	}
}

func TestEngineModelLoadFailureAndRecovery(t *testing.T) {
	e := startFakeEngine(t, "loadfail")

	ctx := context.Background()
	model := writeTestModelFile(t, "broken.gguf", []byte("garbage"))

	if err := e.LoadModel(ctx, ModelSpec{Path: model}); err == nil {
		t.Fatal("loadfail host must reject the load")
	}

	if state := e.NativeModelState(); state != ModelStateFailed {
		t.Fatalf("state after failed load = %q, want failed", state)
	}

	info, err := e.ModelInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Loaded {
		t.Fatalf("failed load must not report loaded: %+v", info)
	}
	if info.Model == nil || info.Model.Error == "" {
		t.Fatalf("failed load must carry the failure detail: %+v", info)
	}

	// Recovery: unload clears the failure, a retry attempt is possible.
	if err := e.UnloadModel(ctx); err != nil {
		t.Fatalf("unload after failure: %v", err)
	}
	if state := e.NativeModelState(); state != ModelStateUnloaded {
		t.Fatalf("state after unload = %q, want unloaded", state)
	}
}

func TestEngineModelLoadBoundedByCallerContext(t *testing.T) {
	// loadslow: the fake host delays the load_model response by 3 s.
	e := startFakeEngine(t, "loadslow")

	model := writeTestModelFile(t, "model.gguf", []byte("fake-gguf-bytes"))

	// A caller that gives up bounds the load; the state walks to failed,
	// never stuck in loading.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := e.LoadModel(ctx, ModelSpec{Path: model}); err == nil {
		t.Fatal("load must respect the caller context")
	}

	if state := e.NativeModelState(); state != ModelStateFailed {
		t.Fatalf("state after aborted load = %q, want failed", state)
	}

	// The engine itself is still alive and healthy.
	healthCtx, healthCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer healthCancel()

	report, err := e.Health(healthCtx)
	if err != nil || !report.Alive {
		t.Fatalf("engine must stay healthy after a failed load: %+v (%v)", report, err)
	}
}

func TestEngineModelStateResetsAcrossLifecycle(t *testing.T) {
	e := startFakeEngine(t, "")

	model := writeTestModelFile(t, "model.gguf", []byte("fake-gguf-bytes"))

	ctx := context.Background()

	if err := e.LoadModel(ctx, ModelSpec{Path: model}); err != nil {
		t.Fatalf("load: %v", err)
	}

	// Deliberate stop + start: a fresh host maps nothing.
	if err := e.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if state := e.NativeModelState(); state != ModelStateUnloaded {
		t.Fatalf("model state after stop = %q, want unloaded", state)
	}

	if err := e.Start(ctx); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if state := e.NativeModelState(); state != ModelStateUnloaded {
		t.Fatalf("model state after restart = %q, want unloaded", state)
	}

	// And the reset state is the LIVE truth (round-trip).
	info, err := e.ModelInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Loaded || info.State != ModelStateUnloaded {
		t.Fatalf("live model info after restart: %+v", info)
	}
}

func TestEngineModelConcurrentInspection(t *testing.T) {
	e := startFakeEngine(t, "")

	modelA := writeTestModelFile(t, "model-a.gguf", []byte("A"))
	modelB := writeTestModelFile(t, "model-b.gguf", []byte("B"))

	ctx := context.Background()

	var wg sync.WaitGroup

	// Inspectors: concurrent ModelInfo + NativeModelState reads.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := 0; round < 30; round++ {
				_, _ = e.ModelInfo(ctx)
				_ = e.NativeModelState()
			}
		}()
	}

	// Churn: serialized loads/unloads (modelMu guarantees serialization).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for round := 0; round < 10; round++ {
			_ = e.LoadModel(ctx, ModelSpec{Path: modelA})
			_ = e.LoadModel(ctx, ModelSpec{Path: modelB})
			_ = e.UnloadModel(ctx)
		}
	}()

	wg.Wait()

	// Final state is a consistent, valid vocabulary value.
	switch state := e.NativeModelState(); state {
	case ModelStateUnloaded, ModelStateLoaded, ModelStateFailed:
		// valid terminal states for the churn above
	default:
		t.Fatalf("final model state %q is not part of the vocabulary", state)
	}
}
