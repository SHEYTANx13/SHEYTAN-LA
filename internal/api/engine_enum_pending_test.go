package api

// engine_enum_pending_test.go — run #124 slice: the engine device-
// enumeration failure disposition. A MISSING engine during the ACTIVE
// startup maintenance window is the expected pending state and must be
// reported as "engine install/update pending", never as a misleading
// engine-fault warning; after the gate releases (and for every other
// failure class) the existing warning applies. Nothing is suppressed —
// both dispositions log. Pure classification (no sleeps, no driver).

import (
	"errors"
	"fmt"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/llm"
)

func TestClassifyEngineEnumFailure(t *testing.T) {
	missing := fmt.Errorf("engine device enumeration: %w: /data/bin/llama-server: no such file", llm.ErrEngineBinaryMissing)
	other := errors.New("engine device enumeration: engine probe timed out")

	active := []MaintenancePhase{
		PhaseChecking,
		PhaseMaintenanceRequired,
		PhaseDownloading,
		PhaseVerifying,
		PhaseInstalling,
	}

	for _, phase := range active {
		if got := classifyEngineEnumFailure(missing, true, phase); got != enumReportPending {
			t.Fatalf("phase %s: missing engine must classify as pending, got %v", phase, got)
		}
	}

	terminal := []MaintenancePhase{
		PhaseReadyForPrewarm,
		PhaseDeferred,
		PhaseFailed,
		PhaseBlocked,
	}
	for _, phase := range terminal {
		if got := classifyEngineEnumFailure(missing, true, phase); got != enumWarnGeneric {
			t.Fatalf("phase %s: missing engine AFTER gate release is a genuine fault, got %v", phase, got)
		}
	}

	if got := classifyEngineEnumFailure(missing, false, PhaseChecking); got != enumWarnGeneric {
		t.Fatalf("no gate armed: missing engine must warn generically, got %v", got)
	}
	if got := classifyEngineEnumFailure(other, true, PhaseDownloading); got != enumWarnGeneric {
		t.Fatalf("non-missing failures must always warn generically, got %v", got)
	}
	if got := classifyEngineEnumFailure(nil, true, PhaseDownloading); got != enumWarnGeneric {
		t.Fatalf("nil error must never classify as pending, got %v", got)
	}
}
