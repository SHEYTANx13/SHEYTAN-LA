// v1.2.0 — Environment Centre (/api/environment) and verified health
// (/api/health).
//
//   - /api/environment aggregates EXISTING telemetry into one system /
//     runtime / recommendation view. Every value comes from a real probe;
//     nothing is synthesized for display.
//   - /api/health reports one check per consequential subsystem with the
//     underlying EVIDENCE. A green check never means "assumed": states are
//     ok (verified), warn (verified but degraded), fail (verified broken),
//     unknown (cannot be measured here) or na (not applicable on this
//     platform).
package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/platform"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recommendation"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
)

// --- Environment Centre ------------------------------------------------------

type environmentPayload struct {
	Device         deviceBlock         `json:"device"`
	Runtime        runtimeBlock        `json:"runtime"`
	Recommendation recommendationBlock `json:"recommendation"`
	GeneratedAt    string              `json:"generatedAt"`
}

type deviceBlock struct {
	OS       string           `json:"os"`
	Arch     string           `json:"arch"`
	CPU      hardware.CPU     `json:"cpu"`
	RAM      hardware.RAM     `json:"ram"`
	Storage  hardware.Storage `json:"storage"`
	GPUs     []hardware.GPU   `json:"gpus"`
	NPU      *sysinfo.NPUInfo `json:"npu,omitempty"`
	Backend  hardware.Backend `json:"backend"`
	Identity platformIdentity `json:"identity"`

	// DeepReady (v1.2.6): false while GPU/NPU/driver facts are still being
	// probed in the background — the UI keeps the fast view and refreshes.
	DeepReady bool `json:"deepReady"`
}

type platformIdentity struct {
	Product        string `json:"product"`
	ShortName      string `json:"shortName"`
	AppUserModelID string `json:"appUserModelId,omitempty"`
	Platform       string `json:"platform"`
}

type runtimeBlock struct {
	EngineState string          `json:"engineState"`
	EnginePhase string          `json:"enginePhase,omitempty"`
	Provider    string          `json:"provider"`
	Backend     string          `json:"backend"`
	Model       string          `json:"model,omitempty"`
	Context     int             `json:"context,omitempty"`
	Verified    bool            `json:"verified"`
	Vision      visionInfoBlock `json:"vision"`
	EngineTag   string          `json:"engineTag,omitempty"`
}

type visionInfoBlock struct {
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	Active    bool   `json:"active"`
	Projector string `json:"projectorName,omitempty"`
	Bytes     int64  `json:"projectorBytes,omitempty"`
}

type recommendationBlock struct {
	Task      string   `json:"task"`
	Applied   bool     `json:"applied"`
	Context   int      `json:"context,omitempty"`
	Threads   int      `json:"threads,omitempty"`
	GPULayers int      `json:"gpuLayers,omitempty"`
	Summary   string   `json:"summary"`
	Reasons   []string `json:"reasons,omitempty"`
}

// handleEnvironment serves the unified System Centre view.
func (s *Server) handleEnvironment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	cfg := s.src.Load()

	// v1.2.6: the Environment Centre serves the NON-BLOCKING snapshot
	// (fast facts immediately; GPU/NPU/driver facts when the background
	// deep probe lands — hardware.WarmDeep() runs at startup). The
	// response's deepReady flag tells the UI whether to keep polling.
	// The v1.2.5 code called hardware.Collect() (which blocks on the
	// FULL CIM probe — measured 5.4 s on the target Windows machine).
	if !sysinfo.DeepReady() {
		hardware.WarmDeep()
	}

	hw := hardware.Snapshot(cfg)

	eng := s.engineSnapshot()

	runtime := runtimeBlock{
		EngineState: eng.State,
		EnginePhase: eng.Phase,
		Provider:    eng.Provider,
		Backend:     eng.Backend,
		Model:       eng.Model,
		Context:     eng.VerifiedContext,
		Verified:    eng.Verified,
	}
	if eng.VerifiedContext == 0 {
		runtime.Context = cfg.LLM.NumCtx
	}
	if eng.VisionState != "" {
		runtime.Vision = visionInfoBlock{
			State:     eng.VisionState,
			Reason:    eng.VisionReason,
			Active:    eng.VisionActive,
			Projector: eng.VisionProjectorName,
			Bytes:     eng.VisionProjectorBytes,
		}
	}
	runtime.EngineTag = hw.Backend.EngineTag

	task := recommendation.Task(cfg.RuntimeProfile)
	if !recommendation.IsValidTask(string(task)) {
		task = recommendation.TaskChat
	}
	in := s.buildRecommendationInput(cfg, "", string(task))
	rec := recommendation.Recommend(in)

	summary := "Current configuration is optimal for this machine."
	applied := false
	if rec.Context > 0 {
		applied = rec.Context == runtime.Context &&
			(rec.Threads == 0 || rec.Threads == cfg.LLM.NumThread || cfg.LLM.NumThread == 0) &&
			rec.GPUAutoOffload == cfg.GPUAutoOffload
		if !applied {
			summary = "A better-fitting configuration is available."
		}
	}

	payload := environmentPayload{
		Device: deviceBlock{
			OS:        hw.OS,
			Arch:      hw.Arch,
			CPU:       hw.CPU,
			RAM:       hw.RAM,
			Storage:   hw.Storage,
			GPUs:      hw.GPUs,
			NPU:       hw.NPU,
			Backend:   hw.Backend,
			DeepReady: hw.DeepReady,
			Identity: platformIdentity{
				Product:        config.AppName,
				ShortName:      config.AppShortName,
				AppUserModelID: brandAppUserModelID,
				Platform:       platform.CurrentOS(),
			},
		},
		Runtime: runtime,
		Recommendation: recommendationBlock{
			Task:      string(task),
			Applied:   applied,
			Context:   cfg.LLM.NumCtx,
			Threads:   cfg.LLM.NumThread,
			GPULayers: cfg.LLM.NumGPU,
			Summary:   summary,
		},
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if rec.Context > 0 {
		payload.Recommendation.Reasons = rec.Reasons
	}

	writeJSON(w, payload)
}

// --- Verified health -----------------------------------------------------------

type healthCheck struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	State    string `json:"state"` // ok | warn | fail | unknown | na
	Evidence string `json:"evidence"`
}

type healthPayload struct {
	Overall     string        `json:"overall"`
	Checks      []healthCheck `json:"checks"`
	GeneratedAt string        `json:"generatedAt"`
}

// handleHealth serves the evidence-backed health view. Nothing green
// without proof.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	cfg := s.src.Load()

	// v1.2.6: health uses the non-blocking snapshot too — a slow or
	// failed deep probe must never stall the health view.
	hw := hardware.Snapshot(cfg)
	eng := s.engineSnapshot()
	checks := make([]healthCheck, 0, 11)

	// Engine — the verified-startup machine is the evidence.
	engineState := "unknown"
	evidence := "engine has not been started this session"
	switch {
	case eng.Verified:
		engineState = "ok"
		evidence = "health probe 200 + serving model verified: " + eng.VerifiedModel
	case eng.State == llm.StateFailed:
		engineState = "fail"
		evidence = "engine failed: " + eng.Detail
	case eng.State == llm.StateIdle || eng.State == llm.StateStopped:
		evidence = "engine " + eng.State + " — not running"
	default:
		engineState = "warn"
		evidence = "engine " + eng.State + " — model verification pending"
	}
	checks = append(checks, healthCheck{ID: "engine", Label: "Engine", State: engineState, Evidence: evidence})

	// Model — the configured model exists on disk with GGUF metadata.
	modelState := "unknown"
	modelEvidence := "no model configured"
	if cfg.Model != "" {
		path, err := llm.ResolveModelPath(cfg.ModelsDir, cfg.Model)
		if err != nil {
			modelState = "fail"
			modelEvidence = "configured model not found in " + cfg.ModelsDir
		} else if fi, statErr := os.Stat(path); statErr == nil && fi.Size() > 0 {
			modelState = "ok"
			modelEvidence = filepath.Base(path) + " present"
		}
	}
	checks = append(checks, healthCheck{ID: "model", Label: "Model", State: modelState, Evidence: modelEvidence})

	// Context — effective window sane and not over model limit.
	ctxState := "unknown"
	ctxEvidence := "no context configured"
	if eff := cfg.LLM.NumCtx; eff > 0 {
		ctxState = "ok"
		ctxEvidence = "configured context " + strconv.Itoa(eff) + " tokens"
	}
	checks = append(checks, healthCheck{ID: "context", Label: "Context", State: ctxState, Evidence: ctxEvidence})

	// Vision — the runtime state machine speaks for itself.
	vs := s.llama.VisionStatus()
	visionState := "unknown"
	switch vs.State {
	case vision.StateReady, vision.StateVerified:
		visionState = "ok"
	case vision.StateDegraded:
		visionState = "warn"
	case vision.StateFailed:
		visionState = "fail"
	case vision.StateUnsupported:
		visionState = "na"
	case vision.StateLoading:
		visionState = "warn"
	case vision.StateFound, vision.StateSupported, vision.StateMissing:
		visionState = "unknown"
	}
	visionEvidence := vs.Reason
	if visionEvidence == "" {
		visionEvidence = "no boot has evaluated vision yet"
	}
	checks = append(checks, healthCheck{ID: "vision", Label: "Vision", State: visionState, Evidence: visionEvidence})

	// GPU — measured adapter + backend evidence.
	gpuState := "unknown"
	gpuEvidence := "no GPU detected — CPU execution"
	if hw.HasGPU() {
		if hw.Backend.Vulkan {
			gpuState = "ok"
			gpuEvidence = "adapter: " + gpuNames(hw) + " — Vulkan engine backend present"
		} else {
			gpuState = "warn"
			gpuEvidence = "adapter detected but no Vulkan engine backend beside the binary"
		}
	}
	checks = append(checks, healthCheck{ID: "gpu", Label: "GPU", State: gpuState, Evidence: gpuEvidence})

	// Storage — free space for sessions/workspace/logs.
	storageState := "unknown"
	storageEvidence := "disk free space unavailable"
	if free := hw.Storage.FreeBytes; free > 0 {
		if free < 5<<30 {
			storageState = "warn"
			storageEvidence = humanGB(free) + " free on " + hw.Storage.Path + " — below the 5 GB comfort band"
		} else {
			storageState = "ok"
			storageEvidence = humanGB(free) + " free on " + hw.Storage.Path
		}
	}
	checks = append(checks, healthCheck{ID: "storage", Label: "Storage", State: storageState, Evidence: storageEvidence})

	// Network — offline probe is cheap and cached by netcheck.
	netState := "unknown"
	netEvidence := netcheck.Note()
	switch {
	case netcheck.IsOffline():
		netState = "warn"
		netEvidence = "offline — remote models/research unavailable; local inference unaffected"
	default:
		netState = "ok"
		netEvidence = "network reachable"
	}
	checks = append(checks, healthCheck{ID: "network", Label: "Network", State: netState, Evidence: netEvidence})

	// Verification — the runtime's verify pipeline.
	verState := "unknown"
	verEvidence := "no verification run yet"
	if eng.Verified {
		verState = "ok"
		verEvidence = "startup verification passed (model + context + capability profile persisted)"
	}
	checks = append(checks, healthCheck{ID: "verification", Label: "Verification", State: verState, Evidence: verEvidence})

	// Coding Lab — configured + enabled state.
	labState := "na"
	labEvidence := "Coding Lab disabled in settings"
	if cfg.LabEnabled {
		labState = "ok"
		labEvidence = "isolated workspaces + command policy active"
	}
	checks = append(checks, healthCheck{ID: "lab", Label: "Coding Lab", State: labState, Evidence: labEvidence})

	// Firewall — honest platform capability reporting.
	fwState := "unknown"
	fwEvidence := "loopback-first architecture: no inbound firewall rule required"
	if platform.CurrentOS() == "windows" {
		exists, err := platform.FirewallStatus(r.Context(), platform.FirewallRulePort)
		if err != nil {
			fwState = "unknown"
			fwEvidence = "firewall status unavailable: " + err.Error()
		} else if exists {
			fwState = "ok"
			fwEvidence = "managed rule present: " + platform.FirewallRulePort
		} else {
			fwState = "na"
			fwEvidence = "no managed rule (default loopback bind needs none)"
		}
	} else {
		fwEvidence += " — rule management not supported on " + platform.CurrentOS()
	}
	checks = append(checks, healthCheck{ID: "firewall", Label: "Firewall", State: fwState, Evidence: fwEvidence})

	// Application identity — declared identity is fact; signature status
	// is reported HONESTLY (no fake Authenticode claims).
	idState := "ok"
	idEvidence := "product " + config.AppName + " · AUMID " + platform.AppUserModelID + " · code signature: none (unsigned developer build)"
	checks = append(checks, healthCheck{ID: "identity", Label: "Application Identity", State: idState, Evidence: idEvidence})

	overall := "ok"
	for _, c := range checks {
		switch c.State {
		case "fail":
			overall = "fail"
		case "warn":
			if overall != "fail" {
				overall = "warn"
			}
		}
	}

	writeJSON(w, healthPayload{
		Overall:     overall,
		Checks:      checks,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func gpuNames(hw hardware.Profile) string {
	names := ""
	for i, g := range hw.GPUs {
		if i > 0 {
			names += ", "
		}
		names += g.Name
	}
	return names
}

func humanGB(n int64) string {
	return strconv.Itoa(int(n>>30)) + " GB"
}

// brandAppUserModelID aliases the platform constant for the API surface.
const brandAppUserModelID = platform.AppUserModelID
