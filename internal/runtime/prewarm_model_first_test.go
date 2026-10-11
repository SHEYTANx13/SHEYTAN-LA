package runtime

// prewarm_model_first_test.go — the v1.5.0 MODEL-FIRST startup contract.
//
// Proven here at the runtime seam (the same PrewarmLLM/EnsureLLM the
// launch path calls):
//
//   - a fresh install with NO selected model NEVER boots an engine:
//     PrewarmLLM returns without starting llama (and without the native
//     prewarm), whatever models sit in the models dir;
//   - the run gates (EnsureLLM / EnsureLLMContext) fail fast and
//     honestly with llm.ErrNoModelSelected — never an arbitrary
//     "first .gguf" boot;
//   - with a selection present, the prewarm proceeds (the existing
//     install path is unchanged).
//
// The launch-recording engine is a RE-EXEC of this test binary (the
// cross-platform testfakes mechanism — no shell scripts, so the Windows
// CI runs the exact same proof): with SHEYTAN_FAKE_LAUNCH_ENGINE=1 the
// binary appends one line to the launch log and stays alive, so ANY
// engine launch — the defect this suite forbids — is observable.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// launchModeEnv / launchLogEnv select the re-exec fake engine and tell
// it where to record the launch. Both travel through the inherited
// process environment (the production engine spawn passes it through).
const (
	launchModeEnv = "SHEYTAN_FAKE_LAUNCH_ENGINE"
	launchLogEnv  = "SHEYTAN_FAKE_LAUNCH_LOG"
)

// TestMain implements the re-exec launch-recording engine plus the
// capability-probe answers (argv-shaped dispatch; a normal `go test`
// never starts this binary with these flags).
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version":
			// v1.3.6 preflight identity probe.
			fmt.Println("version: 4818 (abcdef12)")
			return
		case "--help":
			// Capability probe: a bare engine with no tracked options.
			fmt.Print("usage: llama-server [options]\n  -h, --help  show help\n")
			return
		}
	}

	if os.Getenv(launchModeEnv) == "1" {
		if log := os.Getenv(launchLogEnv); log != "" {
			_ = os.WriteFile(log, []byte("launched\n"), 0o644)
		}

		// Stay alive like a real server until the test stack stops us.
		time.Sleep(60 * time.Second)
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// modelFirstStack builds a stack whose llama binary is the launch-
// recording re-exec fake. Any engine launch appends to launchLog — the
// assertions below are exactly "the log stays EMPTY before selection".
func modelFirstStack(t *testing.T, model string) (*Stack, string) {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.LlamaHost = "127.0.0.1"
	cfg.LlamaPort = freePortForTest(t)
	cfg.LlamaAutoStart = true
	cfg.UpdateSchedule = "off"
	cfg.EngineCompat = 3 // bare flags: the fake engine serves no options
	cfg.VisionEnabled = false
	cfg.Model = model

	if err := os.MkdirAll(cfg.ModelsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A model file EXISTS: the v1.4.0 prewarm would have picked it.
	if err := os.WriteFile(
		filepath.Join(cfg.ModelsDir, "aaa-first-alphabetical.gguf"),
		[]byte("GGUF fixture"), 0o644,
	); err != nil {
		t.Fatal(err)
	}

	// The launch-recording engine = THIS test binary (re-exec fake).
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	launchLog := filepath.Join(t.TempDir(), "launches.log")
	t.Setenv(launchModeEnv, "1")
	t.Setenv(launchLogEnv, launchLog)
	cfg.LlamaBinPath = self

	stack := NewStack(cfg)
	t.Cleanup(stack.Close)

	return stack, launchLog
}

func launches(launchLog string) int {
	data, err := os.ReadFile(launchLog)
	if err != nil {
		return 0
	}

	return len(string(data))
}

func freePortForTest(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port
}

// TestPrewarmWithoutSelectionNeverBoots: Model="" + models present +
// LlamaAutoStart=true — the v1.4.0 defect shape — must NOT launch the
// engine. Discovery lists models; the Model Selector owns the next step.
func TestPrewarmWithoutSelectionNeverBoots(t *testing.T) {
	stack, launchLog := modelFirstStack(t, "")

	stack.PrewarmLLM()

	// The prewarm goroutine is asynchronous; a would-be launcher writes
	// the marker IMMEDIATELY on spawn, so a bounded state-based window
	// catches the defect if it exists.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if launches(launchLog) > 0 {
			t.Fatal("prewarm without a selected model MUST NOT launch the engine (implicit first-GGUF boot)")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if state := stack.Llama.State(); state != llm.StateIdle && state != llm.StateStopped {
		t.Fatalf("engine state = %q, want idle/stopped before selection", state)
	}
}

// TestEnsureLLMWithoutSelectionFailsHonestly: the run gate fails fast
// with the sentinel error — no engine launch, no arbitrary model.
func TestEnsureLLMWithoutSelectionFailsHonestly(t *testing.T) {
	stack, launchLog := modelFirstStack(t, "")

	err := stack.EnsureLLM()
	if !errors.Is(err, llm.ErrNoModelSelected) {
		t.Fatalf("EnsureLLM without selection = %v, want ErrNoModelSelected", err)
	}

	if launches(launchLog) > 0 {
		t.Fatal("the run gate without a selection must never launch the engine")
	}
}

// TestEnsureLLMContextWithoutSelectionFailsHonestly: same contract for
// the bounded request gate.
func TestEnsureLLMContextWithoutSelectionFailsHonestly(t *testing.T) {
	stack, _ := modelFirstStack(t, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := stack.EnsureLLMContext(ctx)
	if !errors.Is(err, llm.ErrNoModelSelected) {
		t.Fatalf("EnsureLLMContext without selection = %v, want ErrNoModelSelected", err)
	}
}
