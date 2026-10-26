package main

// Regression tests for the run #54 version-resolution repair: the
// workflow resolves APP_VERSION through this helper, so the output shape,
// the fail-closed behavior on malformed values, and the flag contract are
// load-bearing for both OS build jobs. The shipped authority
// (internal/config.AppVersion) is a const; malformed inputs are exercised
// through the validateVersion/run seam.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// TestPlainOutputMatchesAuthority pins the exact --plain output: the bare
// semantic version, one line, no "v" prefix, no padding, no decoration.
func TestPlainOutputMatchesAuthority(t *testing.T) {
	var out, errW bytes.Buffer
	if code := run(config.AppVersion, []string{"--plain"}, &out, &errW); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errW.String())
	}
	got := out.String()
	if got != config.AppVersion+"\n" {
		t.Fatalf("--plain output = %q, want %q exactly", got, config.AppVersion+"\n")
	}
}

// TestJSONOutputIsMachineReadable pins the --json contract: valid JSON
// with app + version fields, one line, parseable without post-processing.
func TestJSONOutputIsMachineReadable(t *testing.T) {
	var out, errW bytes.Buffer
	if code := run(config.AppVersion, []string{"--json"}, &out, &errW); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errW.String())
	}
	var decoded map[string]string
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("--json output is not valid JSON: %v (output %q)", err, out.String())
	}
	if decoded["version"] != config.AppVersion {
		t.Fatalf("json version = %q, want the Go authority %q", decoded["version"], config.AppVersion)
	}
	if decoded["app"] != config.AppName {
		t.Fatalf("json app = %q, want %q", decoded["app"], config.AppName)
	}
}

// TestMalformedVersionsFailClosed — no fallback, no guessable default:
// every malformed shape must exit 2 and print NOTHING on stdout.
func TestMalformedVersionsFailClosed(t *testing.T) {
	for _, bad := range []string{
		"",
		"   ",
		"1.11",
		"1.11.0.1",
		"v1.11.0", // the helper resolves the authority verbatim — no v-prefix normalization on output paths
		"01.11.0", // leading zeros are not a release shape
		"1.11.0-rc1",
		"1.11.0+build.5",
		"latest",
		"1.eleven.0",
		"-1.11.0",
	} {
		var out, errW bytes.Buffer
		if code := run(bad, []string{"--plain"}, &out, &errW); code != 2 {
			t.Errorf("version %q: exit = %d, want 2", bad, code)
		}
		if out.Len() != 0 {
			t.Errorf("version %q: wrote %q to stdout on the failure path — must print nothing", bad, out.String())
		}
		if !strings.Contains(errW.String(), "fail closed") {
			t.Errorf("version %q: stderr %q must explain the fail-closed refusal", bad, errW.String())
		}
	}
}

// TestValidateAcceptsOnlyThreeNumericComponents — the regex is the shape
// gate; keep it strict against drift.
func TestValidateAcceptsOnlyThreeNumericComponents(t *testing.T) {
	for _, good := range []string{"1.11.0", "0.1.0", "10.20.30", "1.0.0"} {
		if err := validateVersion(good); err != nil {
			t.Errorf("validateVersion(%q) = %v, want nil", good, err)
		}
	}
}

// TestMutuallyExclusiveFlags — asking for both output shapes is a caller
// bug; fail closed rather than picking one silently.
func TestMutuallyExclusiveFlags(t *testing.T) {
	var out, errW bytes.Buffer
	if code := run(config.AppVersion, []string{"--plain", "--json"}, &out, &errW); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if out.Len() != 0 {
		t.Fatalf("wrote %q to stdout on the failure path", out.String())
	}
}

// TestNoFlagsIsUsageError — an unflagged invocation must not print a
// version in an ambiguous shape that a shell parser might then guess at.
func TestNoFlagsIsUsageError(t *testing.T) {
	var out, errW bytes.Buffer
	if code := run(config.AppVersion, nil, &out, &errW); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if out.Len() != 0 {
		t.Fatalf("wrote %q to stdout without an explicit output flag", out.String())
	}
}
