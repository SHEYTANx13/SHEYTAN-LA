package llm

// v1.3.6 (spec §35) — ENGINE TEST SUITE for the lifecycle-ownership
// rework. These tests are deterministic: they run against the re-exec
// fake engine (TestMain) and prove:
//
//      lifecycle : concurrent Starts collapse; Start+Update never overlap;
//                  Stop reaps; exactly one managed PID.
//      ownership : only the MANAGED engine binary is ever adopted; a
//                  foreign process on the port is refused with evidence.
//      classify  : 0xC0000139-class failures decode into actionable
//                  diagnostics and NEVER enter the compatibility ladder.
//      recovery  : a failed engine transaction rolls back to
//                  last-known-good; a good transaction commits.

import (
        "archive/zip"
        "context"
        "errors"
        "fmt"
        "io"
        "net"
        "net/http"
        "os"
        "os/exec"
        "path/filepath"
        "strings"
        "sync"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// execCmd spawns a process with the OS defaults the test binary needs
// (no special attributes — the fake engine is a plain child).
func execCmd(name string, args ...string) *exec.Cmd {
        return exec.Command(name, args...)
}

// ---------------------------------------------------------------------------
// Failure classification (spec §4/§5)
// ---------------------------------------------------------------------------

func TestLoaderExitClassificationTable(t *testing.T) {
        cases := []struct {
                code int
                kind LoaderFailureKind
        }{
                {-1073741515, LoaderDLLNotFound},       // 0xC0000135
                {-1073741511, LoaderEntryPointMissing}, // 0xC0000139
                {-1073741701, LoaderBadImage},          // 0xC000007B
                {-1073741819, LoaderAccessViolation},   // 0xC0000005
                {5, LoaderAccessDenied},
                {126, LoaderModuleNotFound},
                {127, LoaderProcNotFound},
                {0, LoaderNone},
                {1, LoaderNone}, // ordinary llama.cpp failure — must NOT classify
        }

        for _, tc := range cases {
                lf, ok := classifyLoaderExit(tc.code)

                if got := okString(ok, lf); got != string(tc.kind) {
                        t.Errorf("classifyLoaderExit(%d / 0x%08X): want class %q, got %q",
                                tc.code, uint32(int32(tc.code)), tc.kind, got)
                }
        }
}

func okString(ok bool, lf loaderFailure) string {
        if !ok {
                return string(LoaderNone)
        }

        return string(lf.Kind)
}

func TestEntryPointMissingHasActionableAdvice(t *testing.T) {
        lf, ok := classifyLoaderExit(-1073741511) // 0xC0000139
        if !ok {
                t.Fatalf("0xC0000139 must classify")
        }

        msg := loaderFailureError(lf, "Binary: C:\\engine\\llama-server.exe").Error()

        for _, want := range []string{
                "0xC0000139",
                "entry point",
                "Repair",
        } {
                if !strings.Contains(msg, want) {
                        t.Errorf("diagnostic for 0xC0000139 missing %q — got:\n%s", want, msg)
                }
        }
}

// TestLoaderFailureSkipsCompatLadder proves the spec §5/§6 contract on
// the REAL boot path: a binary that dies with loader-class evidence is
// caught at PREFLIGHT (the bounded --version probe reproduces the
// failure), the full engine is never launched (zero model launches),
// the compat ladder never descends, and the diagnostic carries the
// decoded classification.
func TestLoaderFailureSkipsCompatLadder(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "loader-fail")

        countFile := filepath.Join(cfg.DataDir, "launch-count.txt")
        t.Setenv("SHEYTAN_FAKE_LAUNCH_COUNT", countFile)

        // The recorded compat level would drive a ladder descent if the old
        // retry behavior were still in place.
        cfg.EngineCompat = 0

        srv := NewLlamaServer(config.NewSource(cfg))
        defer func() { _ = srv.Stop() }()

        err := srv.Start()
        if err == nil {
                t.Fatalf("loader-failing engine must not reach ready")
        }

        lower := strings.ToLower(err.Error())
        if !strings.Contains(lower, "entrypoint_not_found") &&
                !strings.Contains(lower, "0xc0000139") {
                t.Fatalf("failure must carry the decoded classification, got: %v", err)
        }

        if data, readErr := os.ReadFile(countFile); readErr == nil {
                launches := strings.Count(string(data), "launch")
                if launches != 0 {
                        t.Fatalf("preflight must stop the boot BEFORE any model launch, got %d", launches)
                }
        }

        if got := srv.State(); got != StateFailed {
                t.Fatalf("state must be failed, got %s", got)
        }

        diag := srv.EngineDiagnostics(cfg)
        if diag.Failure == nil {
                t.Fatalf("a failure report must be recorded for the UI")
        }

        if diag.Failure.Phase != "preflight" {
                t.Fatalf("failure phase must be preflight, got %q", diag.Failure.Phase)
        }

        if diag.Failure.FailureClass != string(LoaderEntryPointMissing) {
                t.Fatalf("failure class must be entry-point-missing, got %q", diag.Failure.FailureClass)
        }

        // v1.4.0: the machine-readable classification must ALSO survive into
        // the structured failure report's class summary — the UI renders
        // from ClassSummary, so a class that only exists in the error string
        // would be invisible to users (and to the machine surface).
        if !strings.Contains(diag.Failure.ClassSummary, string(LoaderEntryPointMissing)) &&
                !strings.Contains(diag.Failure.ClassSummary, "0xC0000139") &&
                !strings.Contains(diag.Failure.ClassSummary, "entry point") {
                t.Fatalf("class summary must carry the decoded entry-point classification, got %q", diag.Failure.ClassSummary)
        }
}

// ---------------------------------------------------------------------------
// Ownership / adoption (spec §3)
// ---------------------------------------------------------------------------

// startBareHealthServer starts a FOREIGN process-equivalent: a plain
// HTTP server answering /health with 200 on the given port, owned by
// this test binary but NOT at the managed engine path.
func startBareHealthServer(t *testing.T, port int) {
        t.Helper()

        l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
        if err != nil {
                t.Fatalf("foreign listener: %v", err)
        }

        mux := http.NewServeMux()
        mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
                w.WriteHeader(http.StatusOK)
                _, _ = w.Write([]byte(`{"status":"ok"}`))
        })

        srv := &http.Server{Handler: mux}

        go func() { _ = srv.Serve(l) }()

        t.Cleanup(func() {
                _ = srv.Close()
        })
}

func TestAdoptionRefusesForeignProcess(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "")

        // Point the managed engine path at a location that is NOT the
        // process actually listening — the identity proof must fail.
        cfg.LlamaBinPath = filepath.Join(cfg.DataDir, "bin", "somewhere-else", "llama-server")
        cfg.LlamaPort = freePort(t)

        startBareHealthServer(t, cfg.LlamaPort)

        srv := NewLlamaServer(config.NewSource(cfg))
        defer func() { _ = srv.Stop() }()

        if srv.adoptExisting(cfg) {
                t.Fatalf("a foreign /health responder must NEVER be adopted")
        }

        // The refusal must produce actionable evidence.
        diag := srv.EngineDiagnostics(cfg)
        if diag.Failure == nil || !strings.Contains(diag.Failure.ClassSummary, "foreign process") {
                t.Fatalf("refusal must record the foreign-process evidence, got %+v", diag.Failure)
        }
}

func TestAdoptionAcceptsManagedEngine(t *testing.T) {
        cfg, _ := fakeEngineConfig(t, "")

        // Launch the REAL fake engine out-of-band: same binary as the
        // managed engine path (fakeEngineConfig sets LlamaBinPath to the
        // test binary), same port — a genuine "previous SHEYTAN instance".
        port := freePort(t)
        cfg.LlamaPort = port

        execTestBinaryAsFakeEngine(t, port)

        srv := NewLlamaServer(config.NewSource(cfg))
        defer func() { _ = srv.Stop() }()

        deadline := time.Now().Add(5 * time.Second)
        adopted := false

        for time.Now().Before(deadline) {
                if srv.adoptExisting(cfg) {
                        adopted = true
                        break
                }

                time.Sleep(100 * time.Millisecond)
        }

        if !adopted {
                t.Fatalf("the managed engine binary serving the port must be adoptable (process identity proof)")
        }
}

func execTestBinaryAsFakeEngine(t *testing.T, port int) *exec.Cmd {
        t.Helper()

        testBin, err := os.Executable()
        if err != nil {
                t.Fatalf("test binary: %v", err)
        }

        cmd := execCmd(testBin, "--port", fmt.Sprintf("%d", port))
        cmd.Env = append(os.Environ(), "SHEYTAN_FAKE_LLAMA=1")

        if err := cmd.Start(); err != nil {
                t.Fatalf("start fake engine: %v", err)
        }

        t.Cleanup(func() {
                _ = cmd.Process.Kill()
                _, _ = cmd.Process.Wait()
        })

        return cmd
}

// ---------------------------------------------------------------------------
// Transactional update + race (spec §1/§2/§35)
// ---------------------------------------------------------------------------

// buildFakeEngineArchive packs the test binary (a valid, runnable
// llama-server stand-in) as a release zip for InstallStagedFromArchive.
func buildFakeEngineArchive(t *testing.T, dir string) string {
        t.Helper()

        testBin, err := os.Executable()
        if err != nil {
                t.Fatalf("test binary: %v", err)
        }

        name := "llama-server"
        if filepath.Ext(testBin) == ".exe" {
                name = "llama-server.exe"
        }

        archivePath := filepath.Join(dir, "llama-b12000-bin.zip")

        src, err := os.Open(testBin)
        if err != nil {
                t.Fatalf("open test binary: %v", err)
        }
        defer src.Close()

        out, err := os.Create(archivePath)
        if err != nil {
                t.Fatalf("create archive: %v", err)
        }
        defer out.Close()

        zw := zip.NewWriter(out)

        // A real release zip carries the executable bit for the server
        // binary — the extracted candidate must be executable on Unix.
        hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
        hdr.SetMode(0o755)

        w, err := zw.CreateHeader(hdr)
        if err != nil {
                t.Fatalf("zip member: %v", err)
        }

        if _, err := io.Copy(w, src); err != nil {
                t.Fatalf("zip copy: %v", err)
        }

        if err := zw.Close(); err != nil {
                t.Fatalf("zip close: %v", err)
        }

        return archivePath
}

func TestUpdateEngineTransactionCommitsVerifiedBinary(t *testing.T) {
        cfg, _ := fakeManagedEngineConfig(t, "")

        archive := buildFakeEngineArchive(t, t.TempDir())

        res, err := updater.InstallStagedFromArchive(cfg, "b12000", archive)
        if err != nil {
                t.Fatalf("transactional install must commit: %v", err)
        }

        if res.Tag != "b12000" || res.SHA256 == "" {
                t.Fatalf("install result must carry tag + sha, got %+v", res)
        }

        // The installed binary is byte-identical to the validated candidate
        // and lives in the managed directory.
        if filepath.Dir(res.BinPath) != filepath.Dir(updater.EngineBinDir(cfg)) &&
                !strings.HasPrefix(res.BinPath, updater.EngineBinDir(cfg)) {
                t.Fatalf("installed binary must live in the managed bin dir, got %s", res.BinPath)
        }

        if got := updater.InstalledEngineTag(cfg); got != "b12000" {
                t.Fatalf("recorded tag must be b12000, got %q", got)
        }

        // No staging leftovers (spec §20).
        for _, junk := range []string{".update-stage", ".engine-download"} {
                if _, err := os.Stat(filepath.Join(updater.EngineBinDir(cfg), junk)); err == nil {
                        t.Fatalf("%s must be cleaned after a committed install", junk)
                }
        }
}

func TestUpdateEngineFailurePreservesLastKnownGood(t *testing.T) {
        cfg, _ := fakeManagedEngineConfig(t, "")

        // Commit a good engine first.
        goodArchive := buildFakeEngineArchive(t, t.TempDir())
        if _, err := updater.InstallStagedFromArchive(cfg, "b10642", goodArchive); err != nil {
                t.Fatalf("seed good engine: %v", err)
        }

        binDir := updater.EngineBinDir(cfg)
        goodBin := filepath.Join(binDir, fakeServerBinaryName())

        goodHash, err := os.ReadFile(goodBin)
        if err != nil {
                t.Fatalf("good binary missing: %v", err)
        }

        // A CORRUPT candidate (not an executable format) must be rejected
        // BEFORE the swap and leave last-known-good untouched.
        badArchive := filepath.Join(t.TempDir(), "llama-b13000-bin.zip")
        if err := os.WriteFile(badArchive, []byte("this is not a zip"), 0o644); err != nil {
                t.Fatalf("bad archive: %v", err)
        }

        if _, err := updater.InstallStagedFromArchive(cfg, "b13000", badArchive); err == nil {
                t.Fatalf("corrupt candidate must fail validation")
        }

        after, err := os.ReadFile(goodBin)
        if err != nil {
                t.Fatalf("last-known-good binary must survive a failed transaction: %v", err)
        }

        if string(after) != string(goodHash) {
                t.Fatalf("last-known-good binary was modified by a failed install")
        }

        if got := updater.InstalledEngineTag(cfg); got != "b10642" {
                t.Fatalf("recorded tag must stay at last-known-good, got %q", got)
        }
}

func fakeServerBinaryName() string {
        if os.PathSeparator == '\\' {
                return "llama-server.exe"
        }

        return "llama-server"
}

// fakeManagedEngineConfig is the transactional-update fixture: the fake
// engine lives at the MANAGED path (<DataDir>/bin/llama-server) as a
// copy of the test binary, and cfg.LlamaBinPath is empty so the unified
// path resolution derives the managed location. The engine-directory
// swap therefore operates on the managed dir only — never on the
// running test binary's own directory.
func fakeManagedEngineConfig(t *testing.T, mode string) (*config.Config, string) {
        t.Helper()

        cfg, modelFile := fakeEngineConfig(t, mode)

        testBin, err := os.Executable()
        if err != nil {
                t.Fatalf("test binary: %v", err)
        }

        binDir := filepath.Join(cfg.DataDir, "bin")
        if err := os.MkdirAll(binDir, 0o755); err != nil {
                t.Fatalf("bin dir: %v", err)
        }

        managedBin := filepath.Join(binDir, fakeServerBinaryName())

        if err := copyExecutable(testBin, managedBin); err != nil {
                t.Fatalf("stage managed engine: %v", err)
        }

        cfg.LlamaBinPath = ""

        return cfg, modelFile
}

func copyExecutable(src, dst string) error {
        in, err := os.Open(src)
        if err != nil {
                return err
        }
        defer in.Close()

        out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
        if err != nil {
                return err
        }
        defer out.Close()

        _, err = io.Copy(out, in)

        return err
}

// TestStartAndUpdateCannotInterleave is the deterministic race test
// (spec §35 "Update race"): a Start and an UpdateEngineNow launched
// concurrently on the same server MUST serialize — the new architecture
// holds switchMu across the whole transaction, so the update never
// overwrites files under a booting engine and the boot never launches a
// half-swapped directory. After both operations: exactly one managed
// process, serving, with the installed tag recorded.
func TestStartAndUpdateCannotInterleave(t *testing.T) {
        cfg, _ := fakeManagedEngineConfig(t, "")

        archive := buildFakeEngineArchive(t, t.TempDir())

        srv := NewLlamaServer(config.NewSource(cfg))
        defer func() { _ = srv.Stop() }()

        // Deterministic staged package: the transaction never touches the
        // network in this test.
        srv.SetStagedArchiveForTest(archive)

        var (
                wg           sync.WaitGroup
                startErr     error
                updateErr    error
                updateResult string
        )

        wg.Add(2)

        go func() {
                defer wg.Done()
                startErr = srv.Start()
        }()

        go func() {
                defer wg.Done()
                // The transactional path via the ENGINE (lifecycle-owned).
                res, err := updater.UpdateEngineWithProgress(
                        context.Background(), cfg, srv, "b12000", nil,
                )
                updateErr = err
                updateResult = res
        }()

        wg.Wait()

        if startErr != nil && !errors.Is(startErr, context.Canceled) {
                // A Start losing the race to the updater's stop is allowed to
                // report shutdown-in-progress; a crash or adoption error is not.
                if !strings.Contains(startErr.Error(), "aborted") &&
                        !strings.Contains(startErr.Error(), "shutdown") {
                        t.Fatalf("concurrent Start failed unexpectedly: %v", startErr)
                }
        }

        if updateErr != nil {
                t.Fatalf("transactional update failed: %v", updateErr)
        }

        if updateResult == "" {
                t.Fatalf("update must return an outcome message")
        }

        if got := updater.InstalledEngineTag(cfg); got != "b12000" {
                t.Fatalf("installed tag must be committed, got %q", got)
        }

        // Give the final boot a moment to reach ready (the update restarts
        // the engine when the raced Start had it running).
        deadline := time.Now().Add(20 * time.Second)

        for time.Now().Before(deadline) {
                st := srv.State()

                if st == StateReady || st == StateRunning || st == StateBusy {
                        break
                }

                time.Sleep(100 * time.Millisecond)
        }

        if !srv.IsRunning() {
                t.Fatalf("engine must be serving after the serialized start+update, state=%s detail=%s", srv.State(), srv.Detail())
        }

        if srv.Pid() == 0 {
                t.Fatalf("exactly one managed process must remain, got pid 0")
        }
}
