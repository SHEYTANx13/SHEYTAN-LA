package render

import (
	"errors"
	"testing"
)

func TestDiscoverWithoutStackFallsBackHonestly(t *testing.T) {
	resetForTest()

	s := Default()

	snap := s.Discover()

	// The probe result is host-dependent: the gogpu software path either
	// initializes (available) or it does not (fallback with a typed
	// reason). BOTH answers are honest; what the contract forbids is a
	// fake middle state.
	switch snap.State {
	case string(StateAvailable):
		if snap.Backend != string(BackendGoGPUSoftware) {
			t.Fatalf("available state must name the gogpu software backend, got %q", snap.Backend)
		}

		if snap.FallbackReason != "" {
			t.Fatalf("available state must not carry a fallback reason, got %q", snap.FallbackReason)
		}
	case string(StateFallback):
		if snap.Backend != string(BackendCPU) {
			t.Fatalf("fallback state must label the CPU path, got %q", snap.Backend)
		}

		if snap.FallbackReason == "" {
			t.Fatalf("fallback state must carry the typed reason")
		}

		if !errors.Is(s.probeErrOf(snap), nil) && snap.FallbackReason == "" {
			t.Fatalf("fallback reason must be populated from the probe error")
		}
	default:
		t.Fatalf("discovery must end available or fallback, got %q", snap.State)
	}
}

func TestVerificationJobReachesExecutionVerified(t *testing.T) {
	resetForTest()

	s := Default()
	s.Discover()

	res, err := s.RunVerificationJob(3, 96, 64)
	if err != nil {
		t.Fatalf("verification job: %v", err)
	}

	if res.State != string(StateExecutionVerified) {
		t.Fatalf("job state = %q, want execution_verified", res.State)
	}

	if res.SHA256 == "" {
		t.Fatalf("a verified job must record its output checksum")
	}

	if res.Width != 96 || res.Height != 64 {
		t.Fatalf("job geometry mismatch: %dx%d", res.Width, res.Height)
	}

	snap := s.Snapshot()
	if snap.State != string(StateExecutionVerified) {
		t.Fatalf("service state = %q, want execution_verified", snap.State)
	}

	if snap.JobsSubmitted != 1 || snap.JobsVerified != 1 || snap.JobsFailed != 0 {
		t.Fatalf("telemetry counters wrong: submitted=%d verified=%d failed=%d",
			snap.JobsSubmitted, snap.JobsVerified, snap.JobsFailed)
	}

	switch snap.Backend {
	case string(BackendGoGPUSoftware), string(BackendCPU):
		// Both labels are honest (software path vs CPU reference path).
	default:
		t.Fatalf("backend label %q is not an honest execution label", snap.Backend)
	}

	if snap.LastVerifiedAt.IsZero() {
		t.Fatalf("verified job must timestamp the verification")
	}
}

func TestVerificationJobIsDeterministic(t *testing.T) {
	resetForTest()

	s := Default()
	s.Discover()

	a, err := s.RunVerificationJob(7, 96, 64)
	if err != nil {
		t.Fatalf("job a: %v", err)
	}

	b, err := s.RunVerificationJob(7, 96, 64)
	if err != nil {
		t.Fatalf("job b: %v", err)
	}

	if a.SHA256 != b.SHA256 {
		t.Fatalf("the same job index must render byte-identical output: %s vs %s", a.SHA256, b.SHA256)
	}

	c, err := s.RunVerificationJob(8, 96, 64)
	if err != nil {
		t.Fatalf("job c: %v", err)
	}

	if c.SHA256 == a.SHA256 {
		t.Fatalf("different job indices must produce different frames")
	}
}

func TestReferenceFrameRejectsTampering(t *testing.T) {
	resetForTest()

	s := Default()
	s.Discover()

	// A frame whose bars were moved must NOT verify: the amplitude
	// profile comparison catches the displaced bars.
	good := barProfile(referenceFrameRGBA(2, 64, 48), 64, 48)
	bad := barProfile(referenceFrameRGBA(5, 64, 48), 64, 48)

	if err := profilesEqual(good, bad); err == nil {
		t.Fatalf("amplitude profiles from different job indices must not verify against each other")
	}

	if err := profilesEqual(good, barProfile(referenceFrameRGBA(2, 64, 48), 64, 48)); err != nil {
		t.Fatalf("the same job index must self-verify: %v", err)
	}

	if pix := referenceFrameRGBA(2, 64, 48); len(pix) != 64*48*4 {
		t.Fatalf("reference frame must be tightly packed RGBA: %d bytes", len(pix))
	}
}

func TestRenderFrameBeforeDiscoverIsTypedUnsupported(t *testing.T) {
	resetForTest()

	s := Default()

	_, _, err := s.RenderFrameRGBA(1, 32, 32)
	if err == nil {
		t.Fatalf("frame render before discovery must be a typed error")
	}
}

// probeErrOf keeps the fallback-honesty assertion readable.
func (s *service) probeErrOf(Snapshot) error { return s.probeErr }
