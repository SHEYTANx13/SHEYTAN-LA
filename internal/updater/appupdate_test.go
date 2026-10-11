package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestCompareVersions_Ordering(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.0", "1.1.9", 1},
		{"1.2.0", "1.2.0", 0},
		{"1.2.1", "1.2.0", 1},
		{"1.10.0", "1.9.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.2", "1.2.0", 0},
		{"1.2.0", "1.2.0-beta", 1}, // prerelease sorts below the release, semver-style

		// v1.2.9 semver §11 cases (the single version grammar):
		{"1.3.0-alpha.1", "1.3.0-alpha.2", -1}, // numeric identifiers numerically
		{"1.3.0-alpha.2", "1.3.0-alpha.10", -1},
		{"1.3.0-alpha", "1.3.0-alpha.1", -1},  // shorter prerelease list sorts lower with equal prefix
		{"1.3.0-alpha.1", "1.3.0-beta", -1},   // alphanumeric lexically
		{"1.3.0-1", "1.3.0-alpha", -1},        // numeric identifier sorts below alphanumeric
		{"1.3.0-beta.2", "1.3.0-beta.11", -1}, // two-digit numeric comparison
		{"v1.3.0", "1.3.0", 0},                // v-prefix tolerated
		{"1.3.0+build.5", "1.3.0", 0},         // build metadata ignored (semver §10)
		{"1.3.0-beta+build.5", "1.3.0-beta", 0},
		{"not-a-version", "1.2.0", -1}, // malformed compares LOW, never newer
		{"", "1.2.0", -1},              // empty compares LOW
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		// Antisymmetry: reversed operands must give the mirrored result.
		if tc.want != 0 {
			rev := -tc.want
			if got := CompareVersions(tc.b, tc.a); got != rev {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d (antisymmetry)", tc.b, tc.a, got, rev)
			}
		}
	}
}

func TestPlatformID_Shape(t *testing.T) {
	id := PlatformID()
	if !strings.Contains(id, "-") {
		t.Fatalf("platform id %q must be <os>-<arch>", id)
	}
}

func TestCheckAppUpdate_UpToDateAndAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
                        "version": "1.3.0",
                        "channel": "stable",
                        "notes": "test",
                        "platforms": {
                                "windows-x64": {
                                        "url": "https://example.invalid/x.exe",
                                        "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
                                }
                        }
                }`))
	}))
	defer srv.Close()

	st, err := CheckAppUpdate(context.Background(), "1.2.0", srv.URL)
	if err != nil {
		t.Fatalf("CheckAppUpdate: %v", err)
	}
	if st.State != AppUpdateAvailable || st.Latest != "1.3.0" {
		t.Fatalf("state=%s latest=%s, want update-available/1.3.0", st.State, st.Latest)
	}

	st, err = CheckAppUpdate(context.Background(), "1.3.0", srv.URL)
	if err != nil {
		t.Fatalf("CheckAppUpdate equal: %v", err)
	}
	if st.State != AppUpToDate {
		t.Fatalf("state=%s, want up-to-date", st.State)
	}
}

func TestCheckAppUpdate_CheckFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	st, err := CheckAppUpdate(context.Background(), "1.2.0", srv.URL)
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
	if st.State != AppCheckFailed {
		t.Fatalf("state=%s, want check-failed", st.State)
	}
}

func TestStageAppUpdate_RefusesUnverifiedManifest(t *testing.T) {
	dir := t.TempDir()
	m := &AppManifest{
		Version: "1.3.0",
		Platforms: map[string]AppPlatformUpdate{
			PlatformID(): {URL: "https://example.invalid/x"},
		},
	}
	if _, _, err := StageAppUpdate(context.Background(), dir, m, PlatformID()); err == nil {
		t.Fatal("expected refusal when manifest has no sha256")
	}
	if _, _, err := StageAppUpdate(context.Background(), dir, m, "beos-powerpc"); err == nil {
		t.Fatal("expected error for unknown platform")
	}
}

func TestStageAppUpdate_DownloadsVerifiesAndStages(t *testing.T) {
	payload := []byte("SHEYTAN-LA fake installer payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			_, _ = w.Write([]byte("tampered"))
			return
		}
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	goodSHA := sha256Hex(payload)
	dir := t.TempDir()

	m := &AppManifest{
		Version: "1.3.0",
		Platforms: map[string]AppPlatformUpdate{
			PlatformID(): {URL: srv.URL + "/good", SHA256: goodSHA, SizeBytes: int64(len(payload)), Kind: "zip"},
		},
	}
	path, sha, err := StageAppUpdate(context.Background(), dir, m, PlatformID())
	if err != nil {
		t.Fatalf("StageAppUpdate: %v", err)
	}
	if sha != goodSHA {
		t.Fatalf("returned digest %s != %s", sha, goodSHA)
	}
	if !strings.HasPrefix(filepath.Base(path), "SHEYTAN-LA-v1.3.0-staged") {
		t.Fatalf("unexpected staged name %q", filepath.Base(path))
	}
	if err := StageIsValid(path, goodSHA); err != nil {
		t.Fatalf("StageIsValid: %v", err)
	}

	// Tampered artifact must be refused and leave no staged file.
	// (v1.2.3: a verified staged artifact at the destination is skipped,
	// so remove it first to exercise the actual download+verify path.)
	if err := os.RemoveAll(filepath.Join(dir, "updates")); err != nil {
		t.Fatal(err)
	}
	tampered := &AppManifest{
		Version: "1.3.0",
		Platforms: map[string]AppPlatformUpdate{
			PlatformID(): {URL: srv.URL + "/bad", SHA256: goodSHA, SizeBytes: int64(len(payload)), Kind: "zip"},
		},
	}
	if _, _, err := StageAppUpdate(context.Background(), dir, tampered, PlatformID()); err == nil {
		t.Fatal("expected verification refusal")
	} else if !strings.Contains(err.Error(), "sha256 mismatch") && !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("wrong error: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "updates", "staging"))
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "SHEYTAN-LA-v1.3.0-staged") {
			t.Fatalf("temp download leaked: %s", e.Name())
		}
	}
}
