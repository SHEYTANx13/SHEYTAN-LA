// failureclass_test.go — v1.7.1: native failure taxonomy + the shared
// exhaustion contract mapping (no C++ host required — pure Go).
package engine

import (
        "context"
        "errors"
        "fmt"
        "strings"
        "testing"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/recovery"
)

func TestNativeContextExhaustedMapsToSharedCondition(t *testing.T) {
        nativeErr := &ContextExhaustedError{Detail: "prompt 8400 tokens exceeds the model context window 4096"}

        if !IsContextExhausted(nativeErr) {
                t.Fatal("native typed exhaustion not detected by the native predicate")
        }
        if !recovery.IsContextExhausted(nativeErr) {
                t.Fatal("native typed exhaustion not detected by the SHARED predicate (Unwrap contract broken)")
        }
        if !errors.Is(nativeErr, recovery.ErrContextExhausted) {
                t.Fatal("errors.Is against the shared sentinel failed")
        }
}

func TestClassifyFailureKind(t *testing.T) {
        cases := []struct {
                detail string
                want   FailureKind
        }{
                {"prompt exceeds the model context window", FailureContextExhausted},
                {"failed to allocate 2 GiB: out of memory", FailureInsufficientResources},
                {"tensor type Q8_0 unsupported tensor for this op", FailureUnsupportedTensor},
                {"unsupported architecture deepseek3", FailureUnsupportedModel},
                {"not a gguf file (bad magic)", FailureInvalidModel},
                {"engine is not running (state stopped)", FailureBackendUnavailable},
                {"handshake failed: protocol version mismatch", FailureBackendUnavailable},
                {"something entirely unexpected", FailureRuntime},
        }
        for _, c := range cases {
                if got := ClassifyFailureKind(c.detail); got != c.want {
                        t.Errorf("ClassifyFailureKind(%q) = %s, want %s", c.detail, got, c.want)
                }
        }
}

func TestNormalizeErrorTypedAndExhaustionPreserved(t *testing.T) {
        // Raw host text becomes a typed Failure with the detail preserved.
        raw := errors.New("native engine load: out of memory while mapping weights")
        typed := NormalizeError(raw)
        f, ok := typed.(*Failure)
        if !ok {
                t.Fatalf("NormalizeError returned %T, want *Failure", typed)
        }
        if f.Kind != FailureInsufficientResources || f.Detail == "" {
                t.Fatalf("typed failure wrong: %+v", f)
        }

        // Already-typed errors pass through untouched.
        if NormalizeError(f) != error(f) {
                t.Fatal("typed error must pass through unchanged")
        }

        // Context exhaustion keeps the shared detection contract.
        ex := &ContextExhaustedError{Detail: "exceeds the model context window"}
        norm := NormalizeError(ex)
        if !recovery.IsContextExhausted(norm) {
                t.Fatal("normalized exhaustion lost the shared condition")
        }
        if NormalizeError(nil) != nil {
                t.Fatal("nil must pass through")
        }
}

func TestFailureUnwrapOnlyForExhaustion(t *testing.T) {
        if errors.Unwrap(&Failure{Kind: FailureContextExhausted}) != recovery.ErrContextExhausted {
                t.Fatal("exhaustion Failure must unwrap to the shared sentinel")
        }
        if errors.Unwrap(&Failure{Kind: FailureRuntime}) != nil {
                t.Fatal("non-exhaustion Failure must not unwrap to the shared sentinel")
        }
}

// TestNormalizeErrorPreservesContextSentinel — v1.7.5: the pause contract
// classifies a generation stop with errors.Is(err, context.Canceled). The
// native boundary previously ERASED the chain (a cause-less *Failure), so a
// pause on the NATIVE backend was misclassified as a regular failure and
// the run settled in error instead of PAUSED. The chain must survive.
func TestNormalizeErrorPreservesContextSentinel(t *testing.T) {
        wrapped := fmt.Errorf("native generation: %w", fmt.Errorf("generate: %w", context.Canceled))

        normalized := NormalizeError(wrapped)

        if !errors.Is(normalized, context.Canceled) {
                t.Fatalf("NormalizeError erased context.Canceled: %v (%T)", normalized, normalized)
        }

        // The deadline sentinel is equally preserved.
        deadline := NormalizeError(fmt.Errorf("native generation: %w", context.DeadlineExceeded))
        if !errors.Is(deadline, context.DeadlineExceeded) {
                t.Fatalf("NormalizeError erased context.DeadlineExceeded: %v", deadline)
        }

        // The public text is unchanged by the cause preservation.
        if got := normalized.Error(); !strings.Contains(got, "native engine") {
                t.Fatalf("unexpected normalized text: %q", got)
        }

        // A nil cause keeps the historical Unwrap contract.
        if errors.Unwrap(&Failure{Kind: FailureRuntime}) != nil {
                t.Fatal("cause-less Failure must not unwrap")
        }
}
