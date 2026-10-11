// provision_v137_test.go — v1.3.7 engine-provisioning order regressions.
//
// The v1.3.6 ensureBinary order put the OFFLINE gate before the local
// discovery tier, so a machine with a perfectly usable local llama-server
// (PATH, exe dir, known dirs, or the persisted discovery cache) was refused
// at boot whenever the network was down — the discovery tier's entire
// reason to exist. The v1.3.7 order is the documented one:
//
//	valid managed engine
//	→ valid locally discovered/importable engine   (works OFFLINE)
//	→ network download
//	→ genuine failure
package llm

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
)

// stageEngineFixture copies the test binary as a structurally valid
// llama-server candidate (real host-arch executable, exec bit, --version
// answered by TestMain) — the same fixture pattern as the discovery and
// lifecycle suites.
func stageEngineFixture(t *testing.T, dir, name string) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary: %v", err)
	}

	dst := filepath.Join(dir, name)

	in, err := os.Open(exe)
	if err != nil {
		t.Fatalf("open test binary: %v", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}

	return dst
}

func engineBinaryName() string {
	if os.PathSeparator == '\\' {
		return "llama-server.exe"
	}

	return "llama-server"
}

func fileSHA(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// TestEnsureBinaryUsesDiscoveredEngineWhileOffline pins THE v1.3.7
// provisioning-order fix: a valid local engine in a Tier-1 location is
// imported and used while the machine is OFFLINE — never rejected merely
// because the network is unavailable.
func TestEnsureBinaryUsesDiscoveredEngineWhileOffline(t *testing.T) {
	root := t.TempDir()

	// A valid engine candidate in a directory that is the ONLY PATH entry.
	extDir := filepath.Join(root, "external")
	fixture := stageEngineFixture(t, extDir, engineBinaryName())
	fixtureSHA := fileSHA(t, fixture)

	// The managed location is EMPTY — only the local tier can provision.
	cfg := config.Default()
	cfg.DataDir = filepath.Join(root, "data")
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.LlamaBinPath = "" // the managed default; no binary there

	// Deterministic discovery: replace PATH entirely so no other engine
	// candidate can exist anywhere the Tier-1 pass looks.
	t.Setenv("PATH", extDir)

	// Force OFFLINE deterministically through the netcheck test seam —
	// this is exactly the state the v1.3.6 order failed on.
	netcheck.SetProbe(func() bool { return false })
	t.Cleanup(func() { netcheck.SetProbe(nil) })

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	bin, err := srv.ensureBinary(cfg)
	if err != nil {
		t.Fatalf("a usable local engine must be provisioned while OFFLINE (v1.3.7 order): %v", err)
	}

	want := expectedEngineBinPath(cfg)
	if filepath.Clean(bin) != filepath.Clean(want) {
		t.Fatalf("ensureBinary = %q, want the managed path %q", bin, want)
	}

	// The imported binary is byte-identical to the discovered candidate —
	// no download happened (offline anyway) and nothing else was imported.
	if got := fileSHA(t, bin); got != fixtureSHA {
		t.Fatalf("imported engine is not the discovered candidate (sha %s vs %s)", got, fixtureSHA)
	}
}

// TestEnsureBinaryFailsHonestlyOfflineWithoutLocalEngine pins the other
// side of the order: with NO managed binary, NO discoverable candidate and
// no network, provisioning fails with the honest OFFLINE error (never a
// misleading download failure, never a silent success).
func TestEnsureBinaryFailsHonestlyOfflineWithoutLocalEngine(t *testing.T) {
	root := t.TempDir()

	cfg := config.Default()
	cfg.DataDir = filepath.Join(root, "data")
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.LlamaBinPath = ""

	// No discoverable engine anywhere: PATH points at an empty directory.
	emptyDir := filepath.Join(root, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	t.Setenv("PATH", emptyDir)

	netcheck.SetProbe(func() bool { return false })
	t.Cleanup(func() { netcheck.SetProbe(nil) })

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	_, err := srv.ensureBinary(cfg)
	if err == nil {
		t.Fatal("provisioning must fail: no managed binary, no local candidate, no network")
	}

	for _, want := range []string{"OFFLINE", "llama-server"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("offline failure must mention %q, got: %v", want, err)
		}
	}
}
