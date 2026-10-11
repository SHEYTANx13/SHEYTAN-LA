// failureclass.go — v1.7.1: typed Native Engine failures normalized into
// the shared runtime categories (§5.5).
//
// The Go side of the native IPC returns errors as strings from the host
// ("native engine error: <detail>"). Raw C++ error text must not leak to
// the shared runtime: this file classifies it ONCE at the native boundary
// into explicit categories with stable names, so preflight, selection and
// recovery all react to the same taxonomy:
//
//      context exhausted      → recovery.ErrContextExhausted contract
//      unsupported model      → architecture not implemented in the engine
//      unsupported tensor     → quantization/tensor type not implemented
//      insufficient resources → memory/allocation failure
//      invalid model          → not a readable GGUF / corrupt metadata
//      backend unavailable    → engine not running / handshake/protocol broken
//      runtime failure        → everything else (honest default)
//
// The classification is conservative: text evidence is used only where
// the protocol has no structured field, and the detail text is always
// preserved verbatim.
package engine

import (
        "fmt"
        "strings"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/recovery"
)

// FailureKind is the shared runtime category of one native failure.
type FailureKind string

const (
        // FailureContextExhausted: the request cannot fit the model context.
        FailureContextExhausted FailureKind = "context_exhausted"
        // FailureUnsupportedModel: model architecture not implemented.
        FailureUnsupportedModel FailureKind = "unsupported_model"
        // FailureUnsupportedTensor: quantization/tensor type not implemented.
        FailureUnsupportedTensor FailureKind = "unsupported_tensor"
        // FailureInsufficientResources: memory or allocation failure.
        FailureInsufficientResources FailureKind = "insufficient_resources"
        // FailureInvalidModel: unreadable/corrupt GGUF or metadata.
        FailureInvalidModel FailureKind = "invalid_model"
        // FailureBackendUnavailable: engine not running or IPC broken.
        FailureBackendUnavailable FailureKind = "backend_unavailable"
        // FailureRuntime: an honest default for unclassified failures.
        FailureRuntime FailureKind = "runtime_failure"
)

// Failure is the typed native error. Context-exhausted failures also
// unwrap into the shared recovery condition.
//
// v1.7.5: the failure carries its CAUSE (when constructed from an existing
// error) and Unwrap navigates it. The previous cause-less conversion broke
// errors.Is at the boundary: the orchestrator's PAUSE contract classifies a
// generation stop with errors.Is(err, context.Canceled) — through
// NormalizeError the canceled chain was erased, so a pause on the NATIVE
// backend was misclassified as a regular failure and the run settled in
// error instead of PAUSED. The cause is additive: Kind/Detail semantics,
// JSON shape and Error() text are unchanged.
type Failure struct {
        Kind   FailureKind `json:"kind"`
        Detail string      `json:"detail"`

        // cause is the original error this Failure normalized (may be nil).
        cause error
}

// Error implements error.
func (f *Failure) Error() string {
        return fmt.Sprintf("native engine %s: %s", f.Kind, f.Detail)
}

// Unwrap maps context exhaustion into the shared runtime condition —
// recovery detection is identical for both backends — and exposes the
// preserved cause (v1.7.5) so sentinel classification (context.Canceled /
// context.DeadlineExceeded / wrapped typed errors) survives normalization.
func (f *Failure) Unwrap() error {
        if f.cause != nil {
                return f.cause
        }
        if f.Kind == FailureContextExhausted {
                return recovery.ErrContextExhausted
        }
        return nil
}

// ClassifyFailureKind maps raw native failure text to its category.
// Ordered most-specific first; the default is FailureRuntime.
func ClassifyFailureKind(detail string) FailureKind {
        d := strings.ToLower(detail)
        switch {
        case containsAnyNative(d,
                "exceeds the model context window",
                "exceeds the available context",
                "maximum context length",
                "context window",
                "prompt is too long",
                "too many tokens",
                "input is too long",
                "does not fit the context",
                "context length exceeded",
        ):
                return FailureContextExhausted

        case containsAnyNative(d,
                "out of memory", "oom", "bad_alloc", "allocation failed",
                "cannot allocate", "failed to allocate", "memory exhausted",
                "insufficient memory", "not enough memory",
        ):
                return FailureInsufficientResources

        case containsAnyNative(d,
                "unsupported tensor", "unknown tensor", "tensor type",
                "unsupported quantization", "unknown quantization",
                "quant type", "ggml type",
        ):
                return FailureUnsupportedTensor

        case containsAnyNative(d,
                "unsupported architecture", "unknown architecture",
                "architecture not", "unsupported model", "model type",
                "not implemented for this model",
        ):
                return FailureUnsupportedModel

        case containsAnyNative(d,
                "invalid gguf", "not a gguf", "corrupt", "failed to read",
                "failed to parse", "magic", "invalid model", "no such file",
                "cannot open", "does not exist", "metadata",
        ):
                return FailureInvalidModel

        case containsAnyNative(d,
                "not running", "not ready", "handshake", "protocol version",
                "abi version", "ipc", "no connection", "engine is not",
                "failed to start", "spawn",
        ):
                return FailureBackendUnavailable

        default:
                return FailureRuntime
        }
}

// NormalizeError wraps a raw native error into the typed *Failure. Nil
// passes through; an already-typed *Failure passes through untouched;
// an error whose text already carries the shared exhaustion condition is
// classified as exhausted. The detail is bounded.
func NormalizeError(err error) error {
        if err == nil {
                return nil
        }
        var f *Failure
        if ok := asFailure(err, &f); ok {
                return err
        }
        if recovery.IsContextExhausted(err) {
                return &Failure{Kind: FailureContextExhausted, Detail: clipFailure(err.Error()), cause: err}
        }
        // v1.7.5: the cause rides along (see the Failure doc) — sentinel
        // classification such as the pause path's errors.Is(err,
        // context.Canceled) must keep working through the native boundary.
        return &Failure{Kind: ClassifyFailureKind(err.Error()), Detail: clipFailure(err.Error()), cause: err}
}

// asFailure is a local errors.As for *Failure (no import of a second
// errors helper — keeps the boundary honest and small).
func asFailure(err error, target **Failure) bool {
        for err != nil {
                if f, ok := err.(*Failure); ok {
                        *target = f
                        return true
                }
                u, ok := err.(interface{ Unwrap() error })
                if !ok {
                        return false
                }
                err = u.Unwrap()
        }
        return false
}

// containsAnyNative reports whether s contains any of the phrases.
func containsAnyNative(s string, subs ...string) bool {
        for _, sub := range subs {
                if sub != "" && strings.Contains(s, sub) {
                        return true
                }
        }
        return false
}

// clipFailure bounds the preserved detail text.
func clipFailure(s string) string {
        const max = 500
        if len(s) <= max {
                return s
        }
        return s[:max] + "…"
}
