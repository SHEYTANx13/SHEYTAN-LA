package webworkspace

import (
	"context"
	"time"
)

// Spec describes ONE managed secondary workspace window. The spec is the
// full contract the adapter needs — the adapter owns no policy of its
// own and never widens the security boundary.
type Spec struct {
	// Title is the window title (the product identifier is prepended by
	// the adapter: "SHEYTAN-LA — <Title>").
	Title string

	// EntryURL is the workspace's opening document; it MUST pass the
	// policy's workspace decision (DecisionOpenInWorkspace).
	EntryURL string

	// Width/Height are the requested initial window size in device
	// independent pixels; zero selects the adapter default.
	Width, Height int

	// Policy is the enforced boundary; it is validated at Open time and
	// a policy without a CSP or allowlist refuses the window.
	Policy SecurityPolicy
}

// Provider is the typed capability surface the UI calls (the platform
// adapter layer; build-time selected, never runtime-guessed).
type Provider interface {
	// Available reports honestly whether this build can open a
	// workspace window (desktop + tagged adapter present).
	Available() bool

	// Mode names the integration shape this build ships.
	Mode() string

	// Open launches ONE managed secondary workspace window. It returns
	// after the window exists (not after it closes); concurrent windows
	// are the adapter's concern, multiple specs are supported.
	Open(ctx context.Context, spec Spec) error
}

// ProviderFunc adapts a pair of functions to the Provider interface.
type ProviderFunc struct {
	AvailableFn func() bool
	ModeFn      func() string
	OpenFn      func(ctx context.Context, spec Spec) error
}

func (f ProviderFunc) Available() bool { return f.AvailableFn() }
func (f ProviderFunc) Mode() string    { return f.ModeFn() }
func (f ProviderFunc) Open(ctx context.Context, spec Spec) error {
	return f.OpenFn(ctx, spec)
}

// UnsupportedProvider is the honest default: the capability is typed
// unavailable, the mode names what WOULD ship, and Open always refuses.
type UnsupportedProvider struct {
	// Reason is surfaced to the user (build tag missing, mobile
	// platform, headless host...).
	Reason string
}

func (UnsupportedProvider) Available() bool { return false }
func (UnsupportedProvider) Mode() string    { return ShippedMode }

func (u UnsupportedProvider) Open(context.Context, Spec) error {
	if u.Reason != "" {
		return ErrUnsupported
	}

	return ErrUnsupported
}

// ValidateSpec runs the full fail-closed preflight the adapter performs
// before creating a window: policy validity + entry-URL workspace
// decision.
func ValidateSpec(spec Spec, now time.Time) error {
	_ = now // reserved for future time-scoped tokens; no fake expiry here

	if err := spec.Policy.Validate(); err != nil {
		return err
	}

	decision, err := spec.Policy.CheckURL(spec.EntryURL)
	if err != nil {
		return err
	}

	if decision != DecisionOpenInWorkspace {
		return ErrPolicyViolation
	}

	return nil
}
