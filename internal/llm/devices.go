// devices.go — v1.2.6 real engine device enumeration + runtime offload
// evidence.
//
// The v1.2.5 GPU posture equated "ggml-vulkan.dll exists" with "GPU
// inference". This file is the fix's measurement side:
//
//   - EnumerateEngineDevices asks the INSTALLED ENGINE what devices it can
//     actually drive (llama.cpp --list-devices), parses the answer, and
//     caches it per engine binary (path + mtime key — no per-launch spawn).
//   - ParseOffloadLine + LlamaServer.ObserveEngineLine capture the runtime
//     log evidence ("offloaded 33/33 layers to GPU") every boot produces —
//     the strongest possible proof that GPU offload is REAL.
package llm

import (
        "context"
        "fmt"
        "os"
        "strconv"
        "strings"
        "sync"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/accelerator"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
)

// enumerateTimeout bounds one --list-devices invocation.
const enumerateTimeout = 20 * time.Second

// deviceCache is the per-binary enumeration cache.
type deviceCacheEntry struct {
        key       string
        at        time.Time
        devices   []accelerator.Device
        supported bool
        err       error
}

var (
        deviceMu    sync.Mutex
        deviceCache = map[string]deviceCacheEntry{}
)

// deviceCacheTTL bounds how long one enumeration stays fresh (the engine
// binary is keyed by mtime, so a binary swap invalidates immediately).
const deviceCacheTTL = 24 * time.Hour

func deviceCacheKey(binPath string) string {
        if fi, err := os.Stat(binPath); err == nil {
                return fmt.Sprintf("%s|%d|%d", binPath, fi.Size(), fi.ModTime().Unix())
        }

        return binPath
}

// enumerateEngineDevices is the swappable enumeration authority behind
// the v1.6.2 runtime backend verification (tests inject deterministic
// failures; production always runs the real engine probe).
var enumerateEngineDevices = EnumerateEngineDevices

// EnumerateEngineDevices runs the installed engine's own device listing
// (llama.cpp --list-devices). supported=false means this build does not
// implement the flag (older releases) — callers fall back to the documented
// weaker evidence with the reason recorded, never a claim.
func EnumerateEngineDevices(binPath string) (devices []accelerator.Device, supported bool, err error) {
        if binPath == "" {
                return nil, false, fmt.Errorf("no engine binary provisioned")
        }

        key := deviceCacheKey(binPath)

        deviceMu.Lock()

        if entry, ok := deviceCache[key]; ok && time.Since(entry.at) < deviceCacheTTL {
                devices, supported, err := entry.devices, entry.supported, entry.err
                deviceMu.Unlock()
                return devices, supported, err
        }

        deviceMu.Unlock()

        ctx, cancel := context.WithTimeout(context.Background(), enumerateTimeout)
        defer cancel()

        cmd := proc.CommandContext(ctx, binPath, "--list-devices")
        proc.Hide(cmd)

        out, execErr := cmd.CombinedOutput()

        devices, supported, parseErr := ParseEngineDeviceOutput(string(out))

        deviceMu.Lock()
        deviceCache[key] = deviceCacheEntry{
                key:       key,
                at:        time.Now(),
                devices:   devices,
                supported: supported,
                err:       parseErr,
        }
        deviceMu.Unlock()

        // A build that errors on --list-devices does not support enumeration —
        // that is a MEASURED "unsupported" verdict, not a failure to know.
        if execErr != nil {
                if isUnknownArgumentError(string(out)) {
                        return nil, false, nil
                }

                return devices, supported, fmt.Errorf("engine device enumeration: %w (output: %s)", execErr, truncateOut(string(out), 200))
        }

        return devices, supported, parseErr
}

// ParseEngineDeviceOutput parses the --list-devices output. Pure function —
// unit-tested with fixtures on every platform.
//
// Expected shape (llama.cpp):
//
//      Available devices:
//        Vulkan0: Intel(R) Arc(TM) A770M Graphics (256 MiB)
//        CUDA0: NVIDIA GeForce RTX 3090 (24576 MiB)
func ParseEngineDeviceOutput(out string) (devices []accelerator.Device, supported bool, err error) {
        text := strings.TrimSpace(out)

        if text == "" {
                return nil, false, fmt.Errorf("empty enumeration output")
        }

        // Unsupported builds reject the flag with an argument error.
        if isUnknownArgumentError(text) {
                return nil, false, nil
        }

        for _, rawLine := range strings.Split(text, "\n") {
                line := strings.TrimSpace(rawLine)

                // Device lines look like "Vulkan0: Name (N MiB)".
                idx := strings.Index(line, ":")
                if idx <= 0 || idx > 32 {
                        continue
                }

                backend := strings.TrimSpace(line[:idx])
                if !strings.HasPrefix(backend, "Vulkan") && !strings.HasPrefix(backend, "CUDA") &&
                        !strings.HasPrefix(backend, "Metal") && !strings.HasPrefix(backend, "SYCL") &&
                        !strings.HasPrefix(backend, "ROC") && !strings.HasPrefix(backend, "OpenCL") {
                        continue
                }

                rest := strings.TrimSpace(line[idx+1:])

                name := rest
                totalMB := 0

                if open := strings.LastIndex(rest, "("); open > 0 {
                        name = strings.TrimSpace(rest[:open])

                        closing := strings.TrimSpace(rest[open+1:])
                        closing = strings.TrimSuffix(closing, ")")

                        if strings.HasSuffix(closing, " MiB") {
                                if mb, convErr := strconv.Atoi(strings.TrimSpace(
                                        strings.TrimSuffix(closing, " MiB"),
                                )); convErr == nil {
                                        totalMB = mb
                                }
                        }
                }

                if name == "" {
                        continue
                }

                devices = append(devices, accelerator.Device{
                        Backend: backend,
                        Name:    name,
                        TotalMB: totalMB,
                        Source:  "engine-enumeration",
                })
        }

        return devices, true, nil
}

// isUnknownArgumentError recognizes the engines' unknown-argument rejections.
func isUnknownArgumentError(out string) bool {
        lower := strings.ToLower(out)

        return strings.Contains(lower, "unknown argument") ||
                strings.Contains(lower, "unrecognized argument") ||
                strings.Contains(lower, "invalid argument: \"--list-devices\"") ||
                strings.Contains(lower, "error: unknown flag")
}

func truncateOut(s string, n int) string {
        s = strings.TrimSpace(s)

        if len(s) > n {
                return s[:n] + "…"
        }

        return s
}

// ParseOffloadLine extracts the runtime offload evidence from ONE engine
// log line. llama.cpp prints e.g.:
//
//      offloaded 33/33 layers to GPU
//      llm_load_tensors: offloaded 33/33 layers to GPU
//
// The function returns the normalized evidence text and whether the line
// carried any.
func ParseOffloadLine(line string) (string, bool) {
        lower := strings.ToLower(line)

        if !strings.Contains(lower, "offloaded") {
                return "", false
        }

        if !strings.Contains(lower, "layers to") {
                return "", false
        }

        trimmed := strings.TrimSpace(line)

        // Strip the common prefix (e.g. "llm_load_tensors: ").
        if idx := strings.Index(trimmed, "offloaded"); idx > 0 {
                trimmed = trimmed[idx:]
        }

        // Only count GPU offload as GPU evidence (CPU offload lines exist for
        // hybrid splits — the GPU must appear in the same line).
        if !strings.Contains(lower, "gpu") {
                return "", false
        }

        return trimmed, true
}

// ObserveEngineLine feeds ONE engine log line to the runtime-evidence
// collector on the server. Called from the stdout/stderr line writers; it
// must never block or fail the stream.
func (s *LlamaServer) ObserveEngineLine(line string) {
        if evidence, ok := ParseOffloadLine(line); ok {
                s.mu.Lock()
                s.offloadEvidence = evidence
                s.mu.Unlock()
        }
}

// OffloadEvidence returns the measured runtime offload line ("" when none
// was observed yet — an honest unknown, never a guess).
func (s *LlamaServer) OffloadEvidence() string {
        s.mu.Lock()
        defer s.mu.Unlock()

        return s.offloadEvidence
}
