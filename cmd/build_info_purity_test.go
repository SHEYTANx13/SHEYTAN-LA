package cmd

// build_info_purity_test.go — run 44205355718 repair (master work order
// §0.2 root cause C).
//
// The Linux x64 lane's compiled-version gate used to grep the HUMAN
// `version` banner for `v[0-9]+.[0-9]+.[0-9]+` and compare the result
// with APP_VERSION=1.11.1. The ALPHA identity change made that scrape
// return <none> and the gate went red even though the binary was fine:
// a workflow-contract bug, not a build failure. The repair separates the
// two concerns for good:
//
//   - `version`    — the HUMAN identity: ALPHA branding, never the
//     numeric build identifier (pinned by version_purity_test.go);
//   - `build-info` — the MACHINE identity: one compact JSON line whose
//     product_stage and build_id fields CI asserts INDEPENDENTLY.
//
// These tests pin the build-info contract directly:
//
//  1. the early dispatch handles `build-info` and exits 0;
//  2. the output is valid JSON with the EXACT field set and values:
//     app = config.AppName, product_stage = brand.Stage (the public
//     identity), build_id = config.AppVersion (the internal build
//     identifier), plus the go/os/arch facts;
//  3. the invocation creates ZERO filesystem entries anywhere it could
//     possibly write — the same SHEYTAN_DATA_DIR probe contract as the
//     version purity tests (run #41415240891 Failure A): the probe must
//     stay read-only and leave its isolated working directory empty;
//  4. the machine identity stays machine-only: build-info carries the
//     numeric build identifier, while the human `version` banner must
//     never leak it (that direction is pinned by the version purity
//     suite; here the separation is asserted from the build-info side).

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/brand"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
)

// captureEarlyDispatch runs one early-dispatch spelling with stdout
// captured and every writable runtime path pointed at the given probe
// directory. If the identity path regains boot side effects, the residue
// lands in the probe dir and the empty-dir assertion names it.
func captureEarlyDispatch(t *testing.T, probe, arg string) (int, string) {
	t.Helper()
	t.Setenv("SHEYTAN_DATA_DIR", probe)

	oldArgs := os.Args
	os.Args = []string{"sheytan-local-agent", arg}
	t.Cleanup(func() { os.Args = oldArgs })

	var out bytes.Buffer
	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	code := RunWithDefaultFn(nil)
	_ = w.Close()
	os.Stdout = stdout
	_, _ = out.ReadFrom(r)
	_ = r.Close()

	return code, out.String()
}

// probeDirMustBeEmpty fails the test when the probe directory gained
// entries — the read-only identity-probe contract.
func probeDirMustBeEmpty(t *testing.T, probe string) {
	t.Helper()
	entries, err := os.ReadDir(probe)
	if err != nil {
		t.Fatalf("read probe dir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("identity dispatch created filesystem residue %v — the version/build-info paths must stay read-only (run #41415240891 Failure A; run 44205355718 repair)", names)
	}
}

// TestBuildInfoDispatchIsReadOnlyAndExact pins the machine-readable
// build-identity contract: exit 0, exact JSON field set, the public
// stage and the internal build identifier asserted independently, and a
// read-only filesystem contract.
func TestBuildInfoDispatchIsReadOnlyAndExact(t *testing.T) {
	probe := t.TempDir()
	code, text := captureEarlyDispatch(t, probe, "build-info")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	var fields map[string]string
	if err := json.Unmarshal([]byte(text), &fields); err != nil {
		t.Fatalf("build-info output is not valid JSON: %v (output %q)", err, text)
	}

	wantKeys := []string{"app", "arch", "build_id", "go", "os", "product_stage"} // sorted order
	var gotKeys []string
	for k := range fields {
		gotKeys = append(gotKeys, k)
	}
	sort.Strings(gotKeys)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("build-info field set = %v, want exactly %v — the JSON contract is load-bearing for CI", gotKeys, wantKeys)
	}

	// Field 1 (independent): the PUBLIC product stage is the brand
	// authority — never a numeric release version.
	if fields["product_stage"] != brand.Stage {
		t.Errorf("product_stage = %q, want brand.Stage %q — the public identity must come from the ONE brand authority", fields["product_stage"], brand.Stage)
	}
	if fields["product_stage"] == config.AppVersion {
		t.Errorf("product_stage leaked the numeric build identifier %q as the public stage — the two identities must stay separate", config.AppVersion)
	}

	// Field 2 (independent): the INTERNAL build identifier matches the
	// config authority exactly (machine-readable reproducibility only).
	if fields["build_id"] != config.AppVersion {
		t.Errorf("build_id = %q, want config.AppVersion %q — the machine identity must come from the ONE version authority", fields["build_id"], config.AppVersion)
	}

	// The remaining facts stay truthful and populated.
	for _, k := range []string{"app", "go", "os", "arch"} {
		if fields[k] == "" {
			t.Errorf("build-info field %q is empty — every fact line must be populated", k)
		}
	}
	if fields["app"] != config.AppName {
		t.Errorf("app = %q, want config.AppName %q", fields["app"], config.AppName)
	}

	probeDirMustBeEmpty(t, probe)
}

// TestBuildInfoIsHandledByTheEarlyDispatch pins that build-info never
// reaches the full boot path: the early dispatch reports handled for it
// (it is a read-only identity query, same contract as `version`), while
// ordinary commands still fall through.
func TestBuildInfoIsHandledByTheEarlyDispatch(t *testing.T) {
	if _, handled := earlyVersionDispatch([]string{"build-info"}); !handled {
		t.Fatal("earlyVersionDispatch did not handle build-info — the machine identity probe must be answered before any boot work")
	}
	for _, other := range []string{"ask", "serve", "doctor", "nonsense"} {
		if _, handled := earlyVersionDispatch([]string{other}); handled {
			t.Errorf("earlyVersionDispatch handled %q — only the identity queries (version spellings, build-info) may be handled early", other)
		}
	}
}
