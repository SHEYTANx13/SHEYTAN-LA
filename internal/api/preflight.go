// preflight.go — v1.7.1: the /api/preflight endpoint.
//
// The UI reads the ONE authoritative pre-run compatibility report from
// here (GET /api/preflight, optional ?context=<tokens> to evaluate a
// candidate window). The frontend never recalculates compatibility — it
// renders this report's verdict, reasons, numbers and recommended
// action. When the severity is incompatible the report IS the refusal
// text the run gate would surface (RefusalMessage).
package api

import (
	"net/http"
	"strconv"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/preflight"
)

func (s *Server) handlePreflight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	requested := 0
	if v := r.URL.Query().Get("context"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			requested = n
		}
	}

	report := s.stack.PreflightReport(requested)

	// For an incompatible verdict, carry the exact refusal text the
	// run-start gate would show — one source of truth for UI + gate.
	resp := map[string]any{
		"model":             report.Model,
		"backend":           report.Backend,
		"device":            report.Device,
		"compatible":        report.Compatible,
		"severity":          report.Severity,
		"reasons":           report.Reasons,
		"requirements":      report.Requirements,
		"available":         report.Available,
		"safetyMarginPct":   report.SafetyMarginPct,
		"recommendedAction": report.RecommendedAction,
		"unknown":           report.Unknown,
	}
	if msg := preflight.RefusalMessage(report); msg != "" {
		resp["refusalMessage"] = msg
	}

	writeJSON(w, resp)
}
