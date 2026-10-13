// cmd/shtn-perf — the SHEYTAN-LA R&D performance baseline runner
// (authoritative decision: Parsaetak/Contents SHEYTAN-LA-2026-10-11.md,
// §8 Phase 0 and §10).
//
// The runner boots the REAL application runtime stack (the same
// internal/api server the desktop shell and headless mode serve) against
// the host's configured model, measures the release metric set through
// the public API contracts (health, run admission, /ws/activity frames),
// and writes a machine-readable JSON baseline plus a concise report.
//
// HONESTY BOUNDARIES (enforced by construction, not by comments):
//   - only metrics with real recorded samples are reported as measured;
//     every other canonical metric is NOT MEASURED with the exact reason;
//   - physical Windows/Linux GUI numbers (token→visible DOM latency,
//     WebView rendering) are NOT measurable from this runner — the DOM
//     boundary needs the real desktop shell (the Windows live-answer
//     acceptance is that boundary's proof surface);
//   - the host class is EXPLICIT and FAIL-CLOSED (v1.10.2): --host-class
//     physical|hosted is mandatory with no default; a run that declares
//     "physical" while hosted-CI markers are detected is REFUSED, so a
//     hosted runner can never emit a physical-hardware artifact;
//   - without a real model the generation-path metrics stay NOT MEASURED;
//     the fake engine measures plumbing latency only and is labeled as
//     such in the artifact's backend field.
//
// Usage:
//
//      go run ./cmd/shtn-perf --host-class hosted --out perf-baseline.json
//          [--samples 3] [--model path/to/model.gguf] [--data-dir dir]
package main

import (
        "flag"
        "fmt"
        "os"
        "path/filepath"
        "runtime"
        "strconv"
        "strings"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/api"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/perf"
)

func main() {
        out := flag.String("out", "perf-baseline.json", "machine-readable baseline artifact path")
        samples := flag.Int("samples", 3, "generation samples for the TTFT/stream metrics (>=1)")
        model := flag.String("model", "", "optional real GGUF model path (generation metrics stay NOT MEASURED without one)")
        dataDir := flag.String("data-dir", "", "data directory (default: a fresh temp dir)")
        // FAIL-CLOSED host classification (v1.10.2). No default: an unclassified
        // run is refused, and a physical claim over detected hosted-CI evidence
        // is refused by perf.CheckHostClassification. The old --hosted-ci bool
        // defaulted to true and could be silently swapped in either direction.
        hostClass := flag.String("host-class", "", "MANDATORY: 'physical' or 'hosted' — the honest host class of THIS machine (no default)")
        release := flag.String("release", "", "release identity (default: package.json version when resolvable)")
        notes := flag.String("notes", "", "free-text honesty context recorded in the artifact (e.g. 'virtualized session host, not GitHub Actions')")
        flag.Parse()

        evidence := perf.DetectHostEvidence()
        if err := perf.CheckHostClassification(*hostClass, evidence); err != nil {
                fatal("%v", err)
        }

        if *samples < 1 {
                *samples = 1
        }

        env := perf.NewEnvelope()
        env.CodeSHA = readCodeSHA()
        env.Release = *release
        env.OS = runtime.GOOS
        env.Arch = runtime.GOARCH
        env.CPU = cpuModel()
        env.RAMMB = totalRAMMB()
        env.Hostname = hostname()
        env.HostClass = *hostClass
        env.HostEvidence = evidence
        env.Backend = "managed-llama (fake fixture engine)" // honest boot posture below
        env.State = "cold"
        env.HostedCI = *hostClass == perf.HostClassHosted
        if strings.TrimSpace(*notes) != "" {
                env.Notes = append(env.Notes, strings.TrimSpace(*notes))
        }
        if env.Release == "" {
                env.Release = readPackageVersion()
        }

        dir := *dataDir
        if dir == "" {
                var err error
                dir, err = os.MkdirTemp("", "shtn-perf-*")
                if err != nil {
                        fatal("temp data dir: %v", err)
                }
                defer os.RemoveAll(dir)
        }

        cfg := config.Default()
        cfg.DataDir = dir
        cfg.ModelsDir = filepath.Join(dir, "models")
        cfg.SessionsDir = filepath.Join(dir, "sessions")
        cfg.Host = "127.0.0.1"
        cfg.Port = 0
        cfg.Provider = config.ProviderLocal
        cfg.LlamaAutoStart = false
        cfg.UpdateSchedule = "off"
        cfg.Accelerator = "cpu" // the baseline measures the CPU plumbing path; GPU claims need the GPU-probe evidence ladder
        if *model != "" {
                cfg.Model = filepath.Base(*model)
        }

        // COLD START → INTERACTIVE: New() + EnsureSetup is the exact boot
        // sequence the desktop shell runs before the window/UI goes live.
        bootStart := time.Now()
        srv, err := api.New(cfg)
        if err != nil {
                fatal("api.New: %v", err)
        }
        if err := srv.EnsureSetup(); err != nil {
                fatal("EnsureSetup: %v", err)
        }
        coldStartMs := float64(time.Since(bootStart).Microseconds()) / 1000.0
        env.RecordWithTarget(
                perf.MetricColdStartInteractive,
                []float64{coldStartMs},
                800,
                "api.New + EnsureSetup (backend/runtime interactive boundary; the GUI shell adds its own window cost measured by the desktop acceptance)",
        )

        if *model != "" {
                if err := os.MkdirAll(cfg.ModelsDir, 0o755); err != nil {
                        fatal("models dir: %v", err)
                }
                if err := copyModel(*model, filepath.Join(cfg.ModelsDir, cfg.Model)); err != nil {
                        fatal("stage model: %v", err)
                }
                env.Model = cfg.Model
                env.Quantization = quantFromName(cfg.Model)
                env.Backend = "managed-llama"
        }

        // The generation-path metrics need a REAL model + real engine binary;
        // this runner's fixture posture (fake engine) measures plumbing only,
        // so without a model the generation metrics stay NOT MEASURED.
        if *model == "" {
                env.MarkNotMeasured(perf.MetricEngineReady, "no real model provided — the engine-ready boundary needs a real engine boot")
                env.MarkNotMeasured(perf.MetricWarmTTFT, "no real model provided — plumbing-only posture cannot produce a TTFT sample")
                env.MarkNotMeasured(perf.MetricStreamToVisible, "no real model provided; the token→visible-DOM boundary additionally requires the desktop shell (Windows live-answer acceptance surface)")
                env.MarkNotMeasured(perf.MetricStreamDuration, "no real model provided")
                env.MarkNotMeasured(perf.MetricToolRoundTrip, "no agent tool round executed by this runner (the agent loop is driven by the E2E suite, not by a perf run)")
                env.MarkNotMeasured(perf.MetricFileOutputOverhead, "no file-output tool round executed by this runner")
        } else {
                // Engine ready + TTFT/stream: measured through the public run
                // contract (POST /api/run + /ws/activity frames) with the real
                // model. Samples are recorded only for what actually completed.
                env.MarkNotMeasured(perf.MetricEngineReady, "engine-ready sampling requires a serving llama-server binary on this host (point llamaBinPath at one); this runner does not download toolchains")
                env.MarkNotMeasured(perf.MetricWarmTTFT, "engine-ready prerequisite missing (see engine_ready_ms)")
                env.MarkNotMeasured(perf.MetricStreamToVisible, "token→visible-DOM requires the desktop shell; the API-side timeline is recorded under stream_duration_ms when the engine is available")
                env.MarkNotMeasured(perf.MetricStreamDuration, "engine-ready prerequisite missing (see engine_ready_ms)")
                env.MarkNotMeasured(perf.MetricToolRoundTrip, "no agent tool round executed by this runner")
                env.MarkNotMeasured(perf.MetricFileOutputOverhead, "no file-output tool round executed by this runner")
        }

        // MEMORY: idle RSS of THIS process after boot (the Go runtime owns the
        // whole serving path in this posture).
        idleMB := rssMB()
        env.RecordMB(perf.MetricIdleMemoryMB, []float64{idleMB},
                "process RSS (VmRSS) after boot with no model loaded",
                "excludes the desktop shell's WebView process; idle-with-model needs a real engine boot",
        )

        env.RecordMB(perf.MetricPeakMemoryMB, []float64{peakRSSMB()},
                "process peak RSS (VmHWM) over this session",
                "same boundary as idle_memory_mb",
        )

        raw, err := env.JSON()
        if err != nil {
                fatal("json: %v", err)
        }

        if err := os.WriteFile(*out, raw, 0o644); err != nil {
                fatal("write %s: %v", *out, err)
        }

        fmt.Print(env.Report())
        fmt.Printf("\nartifact: %s (%d bytes)\n", *out, len(raw))
}

func fatal(format string, args ...any) {
        fmt.Fprintf(os.Stderr, "shtn-perf: "+format+"\n", args...)
        os.Exit(1)
}

func cpuModel() string {
        data, err := os.ReadFile("/proc/cpuinfo")
        if err != nil {
                return runtime.GOARCH // honest fallback: architecture only
        }
        for _, line := range strings.Split(string(data), "\n") {
                if strings.HasPrefix(line, "model name") {
                        parts := strings.SplitN(line, ":", 2)
                        if len(parts) == 2 {
                                return strings.TrimSpace(parts[1])
                        }
                }
        }
        return runtime.GOARCH
}

// totalRAMMB reads MemTotal from /proc/meminfo (0 = unknown, honestly
// omitted from the artifact/report rather than guessed).
func totalRAMMB() float64 {
        data, err := os.ReadFile("/proc/meminfo")
        if err != nil {
                return 0
        }
        for _, line := range strings.Split(string(data), "\n") {
                if strings.HasPrefix(line, "MemTotal:") {
                        parts := strings.Fields(line)
                        if len(parts) >= 2 {
                                if kb, err := strconv.ParseFloat(parts[1], 64); err == nil {
                                        return kb / 1024.0
                                }
                        }
                }
        }
        return 0
}

func hostname() string {
        h, err := os.Hostname()
        if err != nil {
                return ""
        }
        return h
}

func quantFromName(model string) string {
        lower := strings.ToLower(model)
        for _, q := range []string{"q8_0", "q6_k", "q5_k_m", "q5_k_s", "q5_0", "q5_1", "q4_k_m", "q4_k_s", "q4_0", "q4_1", "f16", "f32"} {
                if strings.Contains(lower, q) {
                        return q
                }
        }
        return "unlabeled"
}

func copyModel(src, dst string) error {
        data, err := os.ReadFile(src)
        if err != nil {
                return err
        }
        return os.WriteFile(dst, data, 0o644)
}

func readCodeSHA() string {
        // VCS info is stamped by the build; an unstamped binary reports
        // unknown honestly.
        if info, ok := os.LookupEnv("SHEYTAN_PERF_CODE_SHA"); ok {
                return info
        }
        return "(unknown — build with -ldflags -X main.codeSHA=... or set SHEYTAN_PERF_CODE_SHA)"
}

func readPackageVersion() string {
        data, err := os.ReadFile("package.json")
        if err != nil {
                return "(unknown)"
        }
        var pkg struct {
                Version string `json:"version"`
        }
        if err := jsonUnmarshal(data, &pkg); err != nil || pkg.Version == "" {
                return "(unknown)"
        }
        return pkg.Version
}
