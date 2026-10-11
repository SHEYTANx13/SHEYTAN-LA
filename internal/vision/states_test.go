package vision

import (
	"os"
	"path/filepath"
	"testing"
)

// clipGGUFBytes returns the bytes of a minimal clip-carrying GGUF.
func clipGGUFBytes(t *testing.T) []byte {
	src := clipGGUF(t, []string{"clip.has_vision_encoder", "clip.projector_type", "clip.vision.image_size"})
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// clipGGUFPathBytes writes a minimal clip-carrying GGUF at the exact path
// the evaluation under test expects.
func writeClipGGUF(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, clipGGUFBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestEvaluateModel_StateMachine exercises every pre-boot state path with
// real files — the UI contract is that these states are evidence-backed.
func TestEvaluateModel_StateMachine(t *testing.T) {
	dir := t.TempDir()

	// A vision-capable model with its matching projector.
	visionModel := filepath.Join(dir, "gemma-3-4b-it-Q4_K_M.gguf")
	projector := filepath.Join(dir, "mmproj-gemma-3-4b-it-BF16.gguf")
	writeClipGGUF(t, projector)
	if err := os.WriteFile(visionModel, []byte("not a real gguf"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Projector found: pairing evidence beats everything.
	ev := EvaluateModel(dir, visionModel, "", "llama")
	if ev.State != StateFound || ev.Projector == "" {
		t.Fatalf("state=%s projector=%q, want found+path", ev.State, ev.Projector)
	}

	// A projector-less vision-capable model with NO mmproj present at all.
	emptyDir := t.TempDir()
	ev = EvaluateModel(emptyDir, visionModel, "", "gemma3")
	if ev.State != StateSupported {
		t.Fatalf("state=%s, want supported (vision arch, no projector files)", ev.State)
	}

	// mmproj files exist but none pair → projector-missing.
	other := filepath.Join(emptyDir, "mmproj-qwen2-vl.gguf")
	if err := os.WriteFile(other, clipGGUFBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	ev = EvaluateModel(emptyDir, visionModel, "", "gemma3")
	if ev.State != StateMissing {
		t.Fatalf("state=%s, want projector-missing (other mmproj present, none pairs)", ev.State)
	}

	// Text-only architecture, no projector anywhere.
	// Text-only architecture with only an UNRELATED mmproj present: the
	// honest verdict is unsupported — a qwen projector can never serve a
	// gemma model, and the architecture has no vision encoder anyway.
	ev = EvaluateModel(emptyDir, visionModel, "", "llama")
	if ev.State != StateUnsupported {
		t.Fatalf("state=%s, want unsupported (text arch, unrelated mmproj only)", ev.State)
	}

	ev = EvaluateModel(t.TempDir(), visionModel, "", "llama")
	if ev.State != StateUnsupported {
		t.Fatalf("state=%s, want unsupported (text arch, zero mmproj)", ev.State)
	}

	// Explicit override, existing and missing.
	ev = EvaluateModel(dir, visionModel, projector, "")
	if ev.State != StateFound || ev.Projector != projector {
		t.Fatalf("override existing: state=%s projector=%q", ev.State, ev.Projector)
	}
	ev = EvaluateModel(dir, visionModel, "mmproj-nope.gguf", "")
	if ev.State != StateFailed {
		t.Fatalf("override missing: state=%s, want failed (never a silent substitute)", ev.State)
	}
}

func TestVisionCapableArch(t *testing.T) {
	for _, a := range []string{"gemma3", "llava", "mllama", "qwen2vl", "qwen2p5vl", "minicpmv", "pixtral", "Mistral3"} {
		if !VisionCapableArch(a) {
			t.Errorf("arch %s should be vision-capable", a)
		}
	}
	for _, a := range []string{"llama", "qwen2", "mistral", "phi3", ""} {
		if VisionCapableArch(a) {
			t.Errorf("arch %q must NOT be vision-capable (no fabricated support)", a)
		}
	}
}

func TestState_KnownAndHealthy(t *testing.T) {
	if !StateReady.Healthy() || !StateVerified.Healthy() {
		t.Fatal("ready/verified must be healthy")
	}
	if StateDegraded.Healthy() {
		t.Fatal("degraded must not read healthy")
	}
	for _, s := range []State{StateUnsupported, StateSupported, StateMissing, StateFound, StateVerified, StateLoading, StateReady, StateDegraded, StateFailed} {
		if !s.Known() {
			t.Fatalf("state %s must be known", s)
		}
	}
	if State("vision-yes").Known() {
		t.Fatal("invented states must be rejected")
	}
}

func TestInspectProjector(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mmproj-x.gguf")
	if err := os.WriteFile(p, clipGGUFBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	info, ok := InspectProjector(p)
	if !ok || info.SizeBytes == 0 || info.Format != "gguf" {
		t.Fatalf("info=%+v ok=%v", info, ok)
	}
	if _, ok := InspectProjector(filepath.Join(dir, "nope.gguf")); ok {
		t.Fatal("missing projector must report not-ok")
	}
}
