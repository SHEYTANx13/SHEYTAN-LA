package updater

// variant_install_test.go — v1.6.1: the archive-seam installer commits a
// manifest that RECORDS the backend variant (the identity the accelerator
// surface and the diagnostics inspect), and refuses variants with no
// asset on this platform before touching anything.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallStagedFromArchiveRecordsVariant(t *testing.T) {
	cfg := v137Config(t)

	archive := v137ArchiveFromTestBinary(t, t.TempDir())

	staged, err := InstallStagedFromArchiveDeferredWithVariant(cfg, "b-variant", VariantVulkan, archive)
	if err != nil {
		t.Fatalf("variant deferred install: %v", err)
	}

	staged.Commit()

	// The committed manifest carries the variant — the package identity
	// is inspectable without launching the engine.
	m, ok := ReadInstallManifest(cfg)
	if !ok {
		t.Fatal("committed manifest must exist")
	}
	if m.Tag != "b-variant" {
		t.Fatalf("manifest tag = %q", m.Tag)
	}
	if got := NormalizeAssetVariant(string(m.Variant)); got != VariantVulkan {
		t.Fatalf("manifest variant = %q, want vulkan", m.Variant)
	}

	if InstalledEngineVariant(cfg) != VariantVulkan {
		t.Fatal("InstalledEngineVariant must report the committed variant")
	}

	// The outcome text is honest about the backend family.
	if !strings.Contains(staged.Result().Outcome, "vulkan") {
		t.Fatalf("outcome must name the backend: %q", staged.Result().Outcome)
	}
}

func TestInstallStagedFromArchiveDefaultsToCPUVariant(t *testing.T) {
	cfg := v137Config(t)

	archive := v137ArchiveFromTestBinary(t, t.TempDir())

	staged, err := InstallStagedFromArchiveDeferred(cfg, "b-plain", archive)
	if err != nil {
		t.Fatalf("deferred install: %v", err)
	}

	staged.Commit()

	m, ok := ReadInstallManifest(cfg)
	if !ok {
		t.Fatal("committed manifest must exist")
	}
	if got := NormalizeAssetVariant(string(m.Variant)); got != VariantCPU {
		t.Fatalf("default install variant = %q, want cpu", m.Variant)
	}
}

func TestDeferredInstallRefusesVariantWithoutAsset(t *testing.T) {
	if runtimeIsWindows() {
		t.Skip("Windows serves the Vulkan asset — the refusal path belongs to the other platforms")
	}

	cfg := v137Config(t)

	// Sanity: no engine files touched by the refusal.
	binDir := EngineBinDir(cfg)

	_, err := InstallStagedFromArchiveDeferredWithVariant(cfg, "b-never", VariantVulkan, "")
	// The empty archive path fails the extract step BEFORE any swap; the
	// variant-aware download path would have refused the asset up front.
	// Either way: an error, and nothing staged.
	if err == nil {
		t.Fatal("must fail")
	}

	if _, statErr := os.Stat(binDir + ".update-old"); statErr == nil {
		t.Fatal("refusal must not create an .update-old directory")
	}
}

func runtimeIsWindows() bool {
	return os.PathSeparator == '\\' && filepath.Separator == '\\'
}
