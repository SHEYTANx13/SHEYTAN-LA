package mesh

import (
	"fmt"
	"strings"
)

// Phase 2a — dynamic dual-role operation (owner work order, 2026-11-23:
// "each device can be both server and user based on need ... users
// should be able to run their heavy model AI locally on their more
// advanced system by using their phone as interface").
//
// The static Config.Role stays the OWNER'S CEILING: it names what the
// device may ever be ("controller" = requester/interface side, "worker"
// = serving/provider side, "both"). Negotiation DYNAMIZES that ceiling
// per need: a workstation both serves remote inference AND submits its
// own; a compact device (the phone) narrows to the interface role and
// dispatches its heavy inference to the workstation through the peer
// seam (peer.go). Negotiation may only NARROW the configured ceiling —
// the owner's static decision is never widened behind their back.

// DeviceClass is the honest capability class a device reports about
// itself. Detection only: a reported class is an input to the role
// decision, never evidence that acceleration executes (DETECTED is not
// VERIFIED — the runtime distinction stays with the hardware gates).
type DeviceClass string

const (
	// ClassCompact: phone-class or otherwise constrained device.
	ClassCompact DeviceClass = "compact"
	// ClassWorkstation: desktop-class device with real compute headroom.
	ClassWorkstation DeviceClass = "workstation"
)

// DeviceCapability is the self-reported capability snapshot role
// negotiation consumes. Detection only: AcceleratorPresent means a
// GPU/NPU was DETECTED — never that acceleration executes (DETECTED is
// not EXECUTED; the runtime distinction stays with the hardware gates).
type DeviceCapability struct {
	// Class is the honest device class (compact = phone-class).
	Class DeviceClass
	// MemoryBytes is the total physical memory report (informational;
	// the provider floor below is the honest serving gate).
	MemoryBytes int64
	// AcceleratorPresent reports DETECTED GPU/NPU hardware.
	AcceleratorPresent bool
}

// RolePreference is the owner's need-driven intent for THIS negotiation.
type RolePreference string

const (
	// PrefAuto negotiates purely from the capability report.
	PrefAuto RolePreference = "auto"
	// PrefConsumeOnly forces the interface/requester role (the
	// phone-as-interface case: dispatch heavy inference elsewhere).
	PrefConsumeOnly RolePreference = "consume-only"
	// PrefServeOnly forces the serving/provider role.
	PrefServeOnly RolePreference = "serve-only"
)

// MinProviderMemoryBytes is the honest memory floor below which a
// device is never negotiated into the serving role: local-first AI
// inference needs real headroom, and a compact device pretending to be
// a provider would only thrash. Pure stdlib constant — no hardware
// probing happens here.
const MinProviderMemoryBytes int64 = 6 << 30 // 6 GiB

// roleCeiling reports whether a dynamic role stays inside the static
// configured ceiling: "controller" may only ever be a controller,
// "worker" only ever a worker, "both" may narrow to either.
func roleCeiling(dynamic, ceiling string) bool {
	switch ceiling {
	case "controller":
		return dynamic == "controller"
	case "worker":
		return dynamic == "worker"
	case "both":
		return dynamic == "controller" || dynamic == "worker" || dynamic == "both"
	default:
		return false
	}
}

// NegotiateRole decides the device's dynamic role NOW from its
// capability report and the owner's preference, narrowed by the static
// configured ceiling (cfg.Role). It is deterministic and side-effect
// free; the manager's NegotiateRole method applies the answer.
//
// Policy:
//   - consume-only → controller (the interface role — the owner wants
//     this device to submit, never serve);
//   - serve-only → worker;
//   - auto: a workstation-class device with the honest provider
//     headroom (>= MinProviderMemoryBytes) serves AND submits ("both");
//     a compact device narrows to controller (its heavy inference is
//     dispatched to a peer through the seam).
//
// Ceiling interaction: an EXPLICIT preference contradicting the
// configured ceiling is an owner-contract error (never silently
// widened). An auto proposal outside the ceiling NARROWS to the
// ceiling — the ceiling itself is the owner's explicit static decision
// (a workstation under a "controller" ceiling cannot serve; a compact
// device under a "worker" ceiling stays the dedicated server the owner
// configured).
func NegotiateRole(cap DeviceCapability, pref RolePreference, ceiling string) (string, error) {
	switch pref {
	case PrefAuto, PrefConsumeOnly, PrefServeOnly:
	default:
		return "", fmt.Errorf("%w: role preference %q is not auto/consume-only/serve-only", ErrInvalidConfig, pref)
	}

	switch cap.Class {
	case ClassCompact, ClassWorkstation:
	default:
		return "", fmt.Errorf("%w: device class %q is not compact/workstation", ErrInvalidConfig, cap.Class)
	}

	if cap.MemoryBytes < 0 {
		return "", fmt.Errorf("%w: negative memory report %d", ErrInvalidConfig, cap.MemoryBytes)
	}

	var role string
	switch pref {
	case PrefConsumeOnly:
		role = "controller"
	case PrefServeOnly:
		role = "worker"
	case PrefAuto:
		if cap.Class == ClassWorkstation && cap.MemoryBytes >= MinProviderMemoryBytes {
			role = "both"
		} else {
			// Compact device, or a workstation without the honest
			// provider headroom: the interface role. It still runs its
			// LOCAL jobs locally (the coordinator path is unchanged) —
			// the role decides what the device may SERVE, and a
			// controller serves nothing.
			role = "controller"
		}
	}

	if !roleCeiling(role, ceiling) {
		// PrefAuto has no explicit owner demand: the capability-based
		// proposal may only NARROW to the configured ceiling (a
		// workstation under a "controller" ceiling simply cannot serve).
		// An EXPLICIT preference that contradicts the ceiling is an
		// owner-contract error — never silently widened.
		if pref != PrefAuto {
			return "", fmt.Errorf("%w: negotiated role %q (preference %q) contradicts the configured ceiling %q — the static owner decision is never widened",
				ErrInvalidConfig, role, pref, ceiling)
		}
		if ceiling != "controller" && ceiling != "worker" && ceiling != "both" {
			return "", fmt.Errorf("%w: configured ceiling %q is not controller/worker/both", ErrInvalidConfig, ceiling)
		}
		role = ceiling
	}

	return role, nil
}

// NegotiateRole negotiates the device's dynamic role from the
// capability report and owner preference and applies it to the manager
// (reflected by Status().Role). It is refused while the mesh is
// disabled (ErrMeshDisabled) — a disabled mesh has no role to negotiate
// — and a negotiated answer outside the configured ceiling fails with
// ErrInvalidConfig without touching the configuration.
func (m *Manager) NegotiateRole(cap DeviceCapability, pref RolePreference) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.cfg.Enabled {
		return "", ErrMeshDisabled
	}

	role, err := NegotiateRole(cap, pref, m.cfg.Role)
	if err != nil {
		return "", err
	}

	m.cfg.Role = strings.ToLower(role)
	return m.cfg.Role, nil
}
