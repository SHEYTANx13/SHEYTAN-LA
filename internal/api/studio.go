// studio.go — the v1.11.0 optional 3D Studio surface.
//
// The studio is a READ-ONLY visualization service owned by the ONE core
// authority (this Server). The UI (or CLI) starts and stops it through
// these endpoints; the browser-side viewer talks only to the studio's own
// loopback server, which serves the vendored Three.js assets and ONE
// schema'd topology DTO assembled from the same read models the governor
// surface uses. No commands, no writes, no tool execution.
package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/logging"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/studio"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/sysinfo"
)

// handleStudioState reports the honest viewer lifecycle.
func (s *Server) handleStudioState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}
	if !s.src.Load().StudioEnabled {
		writeJSON(w, map[string]any{
			"enabled": false,
			"running": false,
			"reason":  "the 3D Studio is disabled in Settings (studioEnabled)",
		})
		return
	}
	if s.studioViewer == nil {
		writeJSON(w, map[string]any{"enabled": true, "running": false})
		return
	}
	st := s.studioViewer.State()
	writeJSON(w, map[string]any{
		"enabled":   true,
		"running":   st.Running,
		"url":       st.URL,
		"port":      st.Port,
		"startedAt": st.Started,
		"requests":  st.Requests,
		"version":   studio.ThreeJSVersion,
		"license":   studio.ThreeJSLicense,
	})
}

// handleStudioStart launches the loopback viewer and returns the ONE
// launch URL with the per-launch fragment token. The token never reaches
// any log: it travels in the response body to the requesting LOCAL client
// only.
func (s *Server) handleStudioStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}
	if !s.src.Load().StudioEnabled {
		writeErr(w, http.StatusForbidden, errors.New(
			"the 3D Studio is disabled in Settings — enable it under Settings → General (studioEnabled)"))
		return
	}
	if s.studioViewer == nil {
		s.studioViewer = studio.NewViewer(s)
	}
	url, err := s.studioViewer.Start()
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	st := s.studioViewer.State()
	logging.Default().Info("studio", "3D Studio viewer started on %s (port %d)", st.URL, st.Port)
	writeJSON(w, map[string]any{
		"url":     url, // includes #t= fragment — the ONLY place the token travels
		"port":    st.Port,
		"version": studio.ThreeJSVersion,
	})
}

// handleStudioStop shuts the viewer down. Stopping a stopped viewer is
// a clean no-op (honest idempotence, not an error).
func (s *Server) handleStudioStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}
	if s.studioViewer == nil {
		writeJSON(w, map[string]any{"running": false})
		return
	}
	if err := s.studioViewer.Stop(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	logging.Default().Info("studio", "3D Studio viewer stopped")
	writeJSON(w, map[string]any{"running": false})
}

// StudioTopology implements studio.ViewSource over the EXISTING read
// models — the same authorities the governor surface composes. It never
// starts or probes an engine; the engine block is read from the lifecycle
// owner's published metrics only. Unknowns are named, never invented.
func (s *Server) StudioTopology(ctx context.Context) (studio.Topology, error) {
	cfg := s.src.Load()
	nodes := make([]studio.Node, 0, 12)
	edges := make([]studio.Edge, 0, 12)

	// Host block: the EXISTING sysinfo authority (ProbeFast — the same
	// read the governor hardware block serves). Probed once, reused for
	// every hardware node below.
	host := studio.Node{ID: "host", Kind: "host", Label: config.AppName + " host", Status: "ok"}
	info := sysinfo.ProbeFast()
	if info != nil {
		if info.OSDisplay != "" {
			host.Detail = info.OSDisplay
		} else if info.OS != "" {
			host.Detail = info.OS
		}
	} else {
		host.Status = "unknown"
		host.Detail = "hardware probe unavailable"
	}
	nodes = append(nodes, host)

	if info != nil {
		if info.CPU.Name != "" {
			cpu := studio.Node{
				ID: "cpu", Kind: "cpu", Label: info.CPU.Name, Status: "ok",
			}
			if info.CPU.LogicalCores > 0 {
				cpu.Metrics = map[string]string{"logical cores": fmt.Sprintf("%d", info.CPU.LogicalCores)}
			}
			nodes = append(nodes, cpu)
			edges = append(edges, studio.Edge{From: "host", To: "cpu", Relation: "contains"})
		}
		if info.RAM.TotalBytes > 0 {
			ram := studio.Node{
				ID: "ram", Kind: "ram", Label: "System memory", Status: "ok",
				Metrics: map[string]string{
					"total":     humanBytes(info.RAM.TotalBytes),
					"available": humanBytes(info.RAM.Available),
				},
			}
			if info.RAM.Available <= 0 {
				ram.Metrics["available"] = "not measured"
			}
			nodes = append(nodes, ram)
			edges = append(edges, studio.Edge{From: "host", To: "ram", Relation: "contains"})
		}
		for i, g := range info.GPU {
			if g.Name == "" {
				continue
			}
			gpu := studio.Node{
				ID: fmt.Sprintf("gpu-%d", i), Kind: "gpu", Label: g.Name, Status: "ok",
				Detail: "detection evidence — execution is the engine's device enumeration",
			}
			if g.VRAMBytes > 0 {
				gpu.Metrics = map[string]string{"vram": humanBytes(g.VRAMBytes)}
			}
			nodes = append(nodes, gpu)
			edges = append(edges, studio.Edge{From: "host", To: gpu.ID, Relation: "contains"})
		}
	}

	// Engine block: the EXISTING lifecycle owner's metrics (never a probe).
	eng := studio.Node{ID: "engine", Kind: "engine", Label: "Inference engine", Status: "offline"}
	if s.stack != nil && s.stack.Engine() != nil {
		if m, err := s.stack.Engine().Metrics(ctx); err == nil {
			eng.Status = "ok"
			eng.Metrics = map[string]string{}
			if m.Backend != "" {
				eng.Metrics["backend"] = m.Backend
			}
			if m.EngineState != "" {
				eng.Metrics["state"] = m.EngineState
			}
			if m.Pid != 0 {
				eng.Metrics["pid"] = fmt.Sprintf("%d", m.Pid)
			} else {
				eng.Status = "offline"
			}
			if m.Model != "" {
				eng.Metrics["model"] = m.Model
			}
			if m.UptimeSeconds > 0 {
				eng.Metrics["uptime"] = (time.Duration(m.UptimeSeconds) * time.Second).String()
			}
			if len(eng.Metrics) == 0 {
				eng.Metrics = nil
			}
		} else {
			eng.Status = "unknown"
			eng.Detail = "engine metrics unavailable"
		}
	} else {
		eng.Detail = "no engine wired in this process"
	}
	nodes = append(nodes, eng)
	edges = append(edges, studio.Edge{From: "host", To: "engine", Relation: "contains"})

	// Governor block: the runtime posture, read live.
	if s.stack != nil && s.stack.Governor() != nil {
		gov := studio.Node{ID: "governor", Kind: "governor", Label: "Runtime governor", Status: "ok"}
		if rs := s.stack.Governor().State(); rs.At.After(time.Time{}) {
			gov.Detail = fmt.Sprintf("measured %s", rs.At.UTC().Format(time.RFC3339))
			if rs.RAMTotalBytes > 0 {
				gov.Metrics = map[string]string{
					"process RSS": humanBytes(uint64(rs.ProcRSSBytes)),
				}
				if rs.ProcRSSBytes <= 0 {
					gov.Metrics["process RSS"] = "not measured"
				}
			} else {
				gov.Detail = "no resource measurement yet"
			}
		} else {
			gov.Detail = "no resource measurement yet"
		}
		nodes = append(nodes, gov)
		edges = append(edges, studio.Edge{From: "governor", To: "engine", Relation: "observes"})
		edges = append(edges, studio.Edge{From: "governor", To: "host", Relation: "observes"})
	}

	// Sessions: real counts from the ONE session store.
	if s.store != nil {
		if list, err := s.store.List(); err == nil {
			chat, agent := 0, 0
			for _, st := range list {
				if st == nil {
					continue
				}
				switch st.Mode {
				case "chat":
					chat++
				default:
					agent++
				}
			}
			sessions := studio.Node{
				ID: "sessions", Kind: "sessions", Label: "Sessions", Status: "ok",
				Metrics: map[string]string{"chat": fmt.Sprintf("%d", chat), "agent": fmt.Sprintf("%d", agent)},
			}
			nodes = append(nodes, sessions)
			edges = append(edges, studio.Edge{From: "host", To: "sessions", Relation: "contains"})
		}
	}

	// Tools: the immutable registry snapshot (construction-time facts).
	if s.orch != nil {
		snapshot := s.orch.Tools()
		names := make([]string, 0, len(snapshot))
		for name := range snapshot {
			names = append(names, name)
		}
		sort.Strings(names)
		tools := studio.Node{
			ID: "tools", Kind: "tools", Label: "Tool registry", Status: "ok",
			Metrics: map[string]string{"registered": fmt.Sprintf("%d", len(names))},
		}
		nodes = append(nodes, tools)
		edges = append(edges, studio.Edge{From: "host", To: "tools", Relation: "contains"})
	}

	// Model identity from the ONE config authority (selection, not runtime
	// evidence — the engine block above carries the loaded model if any).
	if cfg.Model != "" {
		model := studio.Node{
			ID: "model", Kind: "model", Label: cfg.Model, Status: "unknown",
			Detail: "configured selection — loaded state is the engine's evidence",
		}
		nodes = append(nodes, model)
		edges = append(edges, studio.Edge{From: "engine", To: "model", Relation: "serves"})
	}

	return studio.Topology{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		AppVersion:  config.AppVersion,
		Source:      "governor read model (sysinfo + engine metrics + session store + tool registry)",
		Nodes:       nodes,
		Edges:       edges,
	}, nil
}

// humanBytes renders byte counts for the topology metrics map.
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
