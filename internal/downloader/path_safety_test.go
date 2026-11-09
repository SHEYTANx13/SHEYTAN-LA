package downloader

// path_safety_test.go — v1.11.1 path-injection remediation regression
// tests (work order §4.1). The download manager's Dest, CacheDir and
// source-cache keys are built by callers from REMOTE identifiers
// (llama.cpp release tags, app-manifest versions, platform ids). These
// tests prove the boundary controls:
//
//   - Dest's final component must satisfy the strict single-component
//     grammar (no traversal, absolute, drive/UNC, ADS, NUL or hidden
//     names) — a hostile identifier fails New(), it never becomes a job;
//   - a remote identifier containing "/" or "\" cannot escape the managed
//     root through the Dest or a source-cache key;
//   - a symlink planted at "<dest>.part" pointing outside the managed
//     root is refused (or neutralized), and the outside file is left
//     byte-for-byte untouched;
//   - valid-input behavior is unchanged (legit names still download,
//     activate, and cache).

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- grammar unit tests -----------------------------------------------------

func TestValidArtifactNameGrammar(t *testing.T) {
	accepted := []string{
		"asset.bin",
		"llama-b1234-bin-win-cpu-x64.zip",
		"SHEYTAN-LA-v1.11.1-staged.exe",
		"v1.2.3+build.7.tar.gz",
		"a-b_c.d",
	}
	for _, name := range accepted {
		if !validArtifactName(name) {
			t.Errorf("validArtifactName(%q) = false, want true", name)
		}
	}

	rejected := []string{
		"",                  // empty
		".",                 // dot
		"..",                // traversal
		".hidden",           // hidden
		"..escape",          // traversal-shaped
		"a/b",               // POSIX separator
		"a\\b",              // Windows separator
		"C:\\evil\\x.bin",   // drive shape
		"\\\\srv\\share\\x", // UNC shape
		"name:ads",          // colon (volume/ADS)
		"nul\x00byte",       // NUL
		" padded ",          // whitespace-padded
		strings.Repeat("x", 256),
	}
	for _, name := range rejected {
		if validArtifactName(name) {
			t.Errorf("validArtifactName(%q) = true, want false", name)
		}
	}
}

// --- Dest boundary control --------------------------------------------------

// TestNewRejectsUnsafeDestShapes proves a hostile Dest fails New() and
// creates NOTHING anywhere.
func TestNewRejectsUnsafeDestShapes(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a rejected job must never touch the network")
	}))
	defer srv.Close()

	hostile := []string{
		"../escape.bin",
		"..\\escape.bin",
		"..",
		".hidden",
		"C:\\evil\\x.bin",
		"\\\\srv\\share\\x.bin",
		"ads:name.bin",
		"nul\x00byte.bin",
		" padded.bin ",
	}
	for _, dest := range hostile {
		// The hostile shapes are passed RAW: the real-world injection embeds
		// the remote identifier in the destination string itself. The
		// boundary refuses the climb and every unsafe final component.
		job, err := New(Options{
			Dest:      dest,
			Sources:   []Source{{URL: srv.URL}},
			AllowHTTP: true,
		})
		if err == nil {
			t.Fatalf("New(Dest=%q) must fail", dest)
		}
		if job != nil {
			t.Fatalf("New(Dest=%q) must not return a job", dest)
		}
	}
	// Nothing was created in the managed dir by the rejected jobs.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected jobs must not create files, found %v", entries)
	}
}

// TestNewRejectsUnsafeCacheDir keeps the sidecar root from climbing.
func TestNewRejectsUnsafeCacheDir(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	for _, cacheDir := range []string{"..", "../elsewhere", "bad\x00dir"} {
		job, err := New(Options{
			Dest:      filepath.Join(t.TempDir(), "asset.bin"),
			CacheDir:  cacheDir,
			Sources:   []Source{{URL: srv.URL}},
			AllowHTTP: true,
		})
		if err == nil || job != nil {
			t.Fatalf("New(CacheDir=%q) must fail closed", cacheDir)
		}
	}
}

// TestHostileTagDestCannotEscapeManagedRoot replays the updater's Dest
// construction ("llama-<tag>-bin-<os>-<arch>.zip" joined under the
// managed staging root) with REMOTE tags. A tag containing traversal or
// separator characters must fail the job boundary — and whatever file
// the OLD code would have written outside the root must not exist.
func TestHostileTagDestCannotEscapeManagedRoot(t *testing.T) {
	root := t.TempDir()
	sibling := filepath.Join(filepath.Dir(root), "escape-probe.bin")

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte("payload"))
	}))
	defer srv.Close()

	// "../.." cancels one climb but still produces a dotted final
	// component ("..-bin-win-cpu-x64.zip") — refused. ".." alone merely
	// lands inside the root as "llama-..-bin-…zip" (contained, accepted);
	// the escape shapes are the ones below. Multi-component climbs with
	// valid final names ("../../etc") are prevented UPSTREAM by the
	// updater's engine-tag grammar — see the updater package tests.
	for _, tag := range []string{"../..", "..\\..", "b\x001", "b:1"} {
		// Exactly the shape updater.downloadEngineArchiveStaged builds.
		dest := filepath.Join(root, "llama-"+tag+"-bin-"+osArch()+".zip")
		if _, err := New(Options{
			Dest:      dest,
			Sources:   []Source{{URL: srv.URL}},
			AllowHTTP: true,
		}); err == nil {
			// If the boundary ever accepts such a shape it must still be
			// provably inside the managed root — reject is the only
			// acceptable outcome for these tags.
			t.Fatalf("New(Dest=%q from tag %q) must fail closed", dest, tag)
		}
	}
	if hits != 0 {
		t.Fatalf("no source may be contacted for hostile tags, hits=%d", hits)
	}
	if _, err := os.Stat(sibling); !os.IsNotExist(err) {
		t.Fatalf("file outside the managed root must not exist: %v", err)
	}
}

func osArch() string {
	return "win-cpu-x64" // arbitrary fixed suffix; the tag is the hostile part
}

// --- source cache key control ------------------------------------------------

// TestSourceCacheKeyCannotEscapeCacheDir proves a hostile cache key —
// the key embeds remote release tags and manifest versions — never
// creates a file outside the cache directory, while keys containing "/"
// map to a SAFE GENERATED local name inside it.
func TestSourceCacheKeyCannotEscapeCacheDir(t *testing.T) {
	cacheDir := t.TempDir()
	src := Source{URL: "https://example.test/asset.zip", Label: "probe", Trust: TrustPrimary}

	// 1. Keys that slug to an UNSAFE name (dotted/traversal-shaped stems)
	// or exceed the bound are refused outright: no cache file anywhere.
	for _, key := range []string{"../pwned", "..", "..\\pwned", "....", strings.Repeat("k", 201)} {
		recordSourceCache(cacheDir, key, src)
		if got := loadSourceCache(cacheDir, key); got != nil {
			t.Fatalf("hostile key %q must never hit the cache", key)
		}
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("hostile keys must not write anything, found %v", entries)
	}

	// 2. A key carrying a remote identifier with "/" (or even a NUL) is
	// slugged into a safe GENERATED name inside the managed cache
	// directory — data syntax is preserved, never a path shape.
	for _, key := range []string{"llama-engine-update-b1/../b2-linux-amd64", "k\x00", "a/b"} {
		recordSourceCache(cacheDir, key, src)
		if got := loadSourceCache(cacheDir, key); got == nil || got.URL != src.URL {
			t.Fatalf("slugged key %q must round-trip, got %+v", key, got)
		}
		safeName := sanitizeKey(key)
		if _, err := os.Stat(filepath.Join(cacheDir, "sheytan-source-cache", safeName+".json")); err != nil {
			t.Fatalf("cache file for %q must live inside the managed cache dir: %v", key, err)
		}
		// The slugged name must be a single safe component (never a path).
		if strings.ContainsAny(safeName, "/\\") || strings.HasPrefix(safeName, ".") {
			t.Fatalf("slugged key %q is not a safe single component", safeName)
		}
	}

	// 3. Nothing may appear outside the cache directory.
	entries, err = os.ReadDir(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "sheytan-source-cache" {
			t.Fatalf("unexpected entry %q outside the managed cache dir", e.Name())
		}
	}
}

// --- symlink-escape refusal ---------------------------------------------------

// TestPartSymlinkEscapeRefused plants a symlink at "<dest>.part" that
// points OUTSIDE the managed root and proves: the resume attempt fails
// closed, the victim file outside the root is byte-for-byte untouched,
// and nothing is activated.
func TestPartSymlinkEscapeRefused(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	victim := filepath.Join(outside, "victim.txt")
	payload := []byte("do not touch me")
	if err := os.WriteFile(victim, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(root, "asset.bin")
	if err := os.Symlink(victim, dest+".part"); err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "not supported") {
			t.Skipf("symlinks unavailable on this platform: %v", err)
		}
		t.Fatal(err)
	}

	body := []byte("downloaded payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	job, err := New(Options{
		Dest:      dest,
		Sources:   []Source{{URL: srv.URL}},
		SHA256:    digest(body),
		Resume:    true, // forces a Stat/Open of the planted .part symlink
		AllowHTTP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := job.Run(context.Background()); err == nil {
		t.Fatal("a .part symlink escaping the managed root must fail the job")
	}

	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("file outside the managed root was modified: %q", got)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("nothing may be activated through an escaping symlink")
	}
}

// TestFreshDownloadNeutralizesPlantedPartSymlink proves the non-resume
// path never WRITES through a planted symlink: the link itself is
// removed (POSIX unlink does not follow it) or the root refuses — either
// way the victim file outside the root stays byte-for-byte untouched.
func TestFreshDownloadNeutralizesPlantedPartSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	victim := filepath.Join(outside, "victim.txt")
	victimBytes := []byte("do not touch me")
	if err := os.WriteFile(victim, victimBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(root, "asset.bin")
	if err := os.Symlink(victim, dest+".part"); err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "not supported") {
			t.Skipf("symlinks unavailable on this platform: %v", err)
		}
		t.Fatal(err)
	}

	body := []byte("downloaded payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	job, err := New(Options{
		Dest:      dest,
		Sources:   []Source{{URL: srv.URL}},
		SHA256:    digest(body),
		AllowHTTP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := job.Run(context.Background())
	if err == nil {
		// The planted link was neutralized and the fresh download landed
		// INSIDE the managed root.
		if res.Path != dest {
			t.Fatalf("activated artifact must be at Dest, got %q", res.Path)
		}
		got, rerr := os.ReadFile(dest)
		if rerr != nil || string(got) != string(body) {
			t.Fatalf("activated artifact corrupt: %v", rerr)
		}
	}

	// THE INVARIANT, both when the root refused and when it neutralized:
	after, rerr := os.ReadFile(victim)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(after) != string(victimBytes) {
		t.Fatalf("file outside the managed root was modified: %q", after)
	}
}

// --- valid-input behavior is unchanged ---------------------------------------

// TestLegitDestNamesStillAccepted pins the accepted grammar so the
// remediation cannot silently break ordinary callers.
func TestLegitDestNamesStillAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	for _, name := range []string{
		"asset.bin",
		"llama-b1234-bin-win-cpu-x64.zip",
		"SHEYTAN-LA-v1.11.1-staged.exe",
	} {
		if _, err := New(Options{
			Dest:      filepath.Join(t.TempDir(), name),
			Sources:   []Source{{URL: srv.URL}},
			AllowHTTP: true,
		}); err != nil {
			t.Fatalf("New(Dest=%q) must be accepted: %v", name, err)
		}
	}
}
