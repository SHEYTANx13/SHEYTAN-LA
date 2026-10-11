// governor.go — v1.8.0: the Runtime Governor's API surface.
//
// GET /api/governor — the ONE system-health / runtime-policy read model,
// COMPOSED from the existing authorities (no second state anywhere):
//
//	governor     — resource state, envelope, self-model (the Governor's
//	               own measured policy facts, always read live)
//	hardware     — the existing sysinfo authority (ProbeFast)
//	engine       — the existing engine lifecycle owner's Metrics
//	capabilities — the existing construction-time toolset facts
//
// Every block carries its own provenance and names its unknowns. The
// handler NEVER invents a value: an authority that cannot answer reports
// "measured: false" (or is omitted), never a placeholder number.
package api

import (
	"net/http"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
)

// handleGovernor serves the runtime-governor / system-health surface.
func (s *Server) handleGovernor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	out := map[string]any{
		"appName":    config.AppName,
		"appVersion": config.AppVersion,
	}

	// --- the Governor's own policy block (always live) -----------------
	if gov := s.stack.Governor(); gov != nil {
		out["governor"] = map[string]any{
			"available": true,
			"state":     gov.State(),
			"envelope":  gov.Envelope(),
			"selfModel": gov.SelfModel(),
		}
	} else {
		out["governor"] = map[string]any{
			"available": false,
			"reason":    "the runtime governor is not wired in this process (tests, partial stacks)",
		}
	}

	// --- hardware: the EXISTING sysinfo authority ----------------------
	out["hardware"] = governorHardwareBlock()

	// --- engine health: the EXISTING lifecycle owner's metrics ---------
	out["engine"] = s.governorEngineBlock(r)

	// --- capabilities: existing construction-time facts ----------------
	out["capabilities"] = s.governorCapabilitiesBlock()

	writeJSON(w, out)
}

// governorHardwareBlock reads the existing sysinfo authority and shapes
// the compact hardware facts with provenance. Nothing is probed here:
// the fast snapshot is served, deep facts only when the background probe
// already landed (the same posture as /api/sysinfo).
func governorHardwareBlock() map[string]any {
	info := sysinfo.ProbeFast()
	if info == nil {
		return map[string]any{"measured": false}
	}

	block := map[string]any{
		"measured":     true,
		"os":           info.OS,
		"osBuild":      info.OSBuild,
		"osDisplay":    info.OSDisplay,
		"deepProbedAt": info.DeepProbedAt,
	}

	if info.CPU.Name != "" {
		cpu := map[string]any{"name": info.CPU.Name}
		if info.CPU.LogicalCores > 0 {
			cpu["logicalCores"] = info.CPU.LogicalCores
		}
		block["cpu"] = cpu
	} else {
		block["cpu"] = map[string]any{"measured": false}
	}

	if info.RAM.TotalBytes > 0 {
		block["ram"] = map[string]any{
			"totalBytes":     info.RAM.TotalBytes,
			"availableBytes": info.RAM.Available,
			"measured":       info.RAM.Available > 0,
		}
	} else {
		block["ram"] = map[string]any{"measured": false}
	}

	// GPU facts are a LIST (multi-GPU hosts exist); every entry is
	// DETECTION evidence — never execution evidence.
	gpus := make([]map[string]any, 0, len(info.GPU))
	for _, g := range info.GPU {
		if g.Name == "" {
			continue
		}
		gpus = append(gpus, map[string]any{
			"name":      g.Name,
			"vendor":    g.Vendor,
			"vramBytes": g.VRAMBytes,
			"driverVer": g.DriverVer,
			"measured":  true,
			"note":      "detection only — execution evidence is the engine's device enumeration",
		})
	}
	if len(gpus) > 0 {
		block["gpus"] = gpus
	} else {
		block["gpus"] = []map[string]any{}
	}

	// NPU: detection is presence/identity evidence ONLY (the NPUInfo
	// contract) — never implied execution.
	if info.NPU != nil && info.NPU.Name != "" {
		block["npu"] = map[string]any{
			"name":     info.NPU.Name,
			"vendor":   info.NPU.Vendor,
			"detected": true,
			"note":     "detection only — NPU execution evidence is a separate, verified surface",
		}
	}

	return block
}

// governorEngineBlock reads the engine lifecycle owner's published
// metrics — read-only, no engine is started or probed by this handler.
func (s *Server) governorEngineBlock(r *http.Request) map[string]any {
	eng := s.stack.Engine()
	if eng == nil {
		return map[string]any{"running": false}
	}

	m, err := eng.Metrics(r.Context())
	if err != nil {
		return map[string]any{
			"running": false,
			"unknown": true,
			"reason":  "engine metrics unavailable: " + err.Error(),
		}
	}

	block := map[string]any{
		"backend":     m.Backend,
		"engineState": m.EngineState,
		"running":     m.Pid != 0,
	}

	if m.Pid != 0 {
		block["pid"] = m.Pid
	}

	if m.Model != "" {
		block["model"] = m.Model
	}

	if m.ProcessRSSBytes > 0 {
		block["processRssBytes"] = m.ProcessRSSBytes
	} else {
		block["processRssMeasured"] = false
	}

	if m.UptimeSeconds > 0 {
		block["uptimeSeconds"] = m.UptimeSeconds
	}

	if m.Restarts > 0 {
		block["restarts"] = m.Restarts
	}

	return block
}

// governorCapabilitiesBlock reports the EXISTING toolset facts (what this
// process actually constructed) — never aspirational feature lists.
func (s *Server) governorCapabilitiesBlock() map[string]any {
	return map[string]any{
		"browser":      s.stack.Browser != nil,
		"sandbox":      s.stack.Sandbox != nil,
		"nativeEngine": s.stack.Native != nil,
		"repoIndex":    s.stack.RepoIndex != nil,
		"research":     s.stack.Research != nil,
		"skills":       s.stack.Skills != nil,
		"lab":          s.stack.Lab != nil,
		"recall":       s.stack.Recall != nil,
		"multiAgent":   s.stack.Multi != nil,
	}
}
