package api

// engine_variant.go — v1.6.1: the backend-variant provisioning surface.
//
//   POST /api/engine/provision        body: {"variant": "vulkan" | "cpu"}
//   GET  /api/engine/provision        the installed variant + platform support
//
// Vulkan must be REAL, not cosmetic: the endpoint drives the engine-owned
// transactional provisioning (stop → staged install of the variant
// package → restart → verify → commit / rollback). An explicit VULKAN
// request that cannot be provisioned fails LOUDLY — it never silently
// installs the CPU package. GPU/Vulkan claims in the accelerator surface
// remain evidence-gated exactly as before (executionVerified only with
// real engine/backend evidence).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// handleEngineProvision drives backend-variant provisioning through the
// engine's transactional UpdateEngineVariantNow.
func (s *Server) handleEngineProvision(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.src.Load()

		// v1.7.2: the AUTO GPU candidate state rides the same surface —
		// the UI renders authoritative facts (installed variant, platform
		// support, last bounded AUTO probe outcome); it never computes
		// compatibility itself.
		payload := map[string]any{
			"installedVariant":  string(updater.InstalledEngineVariant(cfg)),
			"supportedVariants": variantStrings(updater.SupportedVariants()),
			"platform":          goPlatform(),
		}
		if st := s.llama.AutoGPUProbeSnapshot(); st != nil {
			payload["autoProbe"] = st
		}

		writeJSON(w, payload)

	case http.MethodPost:
		var body struct {
			Variant string `json:"variant"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("decode provision body: %w", err))
			return
		}

		// v1.6.2 STRICT VARIANT PARSING: an explicit request with an
		// empty or unknown variant is a DETERMINISTIC 400 — it can
		// never be normalized to CPU provisioning (the v1.6.1
		// defect: {"variant":"banana"} silently installed CPU).
		// The lenient legacy reader stays reserved for manifest
		// reads inside the updater.
		variant, perr := updater.ParseAssetVariant(body.Variant)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, perr)
			return
		}

		// An explicit request for an unsupported variant is a 400 with the
		// actionable reason — NOT a silent CPU install.
		if !updater.VariantSupported(variant) {
			writeErr(w, http.StatusBadRequest, fmt.Errorf(
				"engine variant %q cannot be provisioned on %s — upstream llama.cpp publishes no prebuilt package for this platform; keep the CPU engine or point llamaBinPath at a self-built %s llama-server",
				variant, goPlatform(), variant,
			))
			return
		}

		// Model changes and generation must not race the package swap.
		if s.anyRunActive() {
			writeErr(w, http.StatusConflict, fmt.Errorf(
				"a run is active — engine variant provisioning is locked while a generation is in flight",
			))
			return
		}

		if s.calibrating.Load() {
			writeErr(w, http.StatusConflict, fmt.Errorf(
				"an automatic calibration is in progress — retry when it settles",
			))
			return
		}

		cfg := s.src.Load()

		if cfg.IsRemote() {
			writeErr(w, http.StatusBadRequest, fmt.Errorf(
				"remote provider active — engine variant provisioning is a local-engine concept",
			))
			return
		}

		outcome, err := s.llama.UpdateEngineVariantNow(r.Context(), variant, nil)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}

		writeJSON(w, map[string]any{
			"ok":               true,
			"variant":          string(variant),
			"installedVariant": string(updater.InstalledEngineVariant(s.src.Load())),
			"outcome":          outcome,
		})

	default:
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
	}
}

func variantStrings(vs []updater.AssetVariant) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, string(v))
	}
	return out
}

func goPlatform() string { return runtime.GOOS + "/" + runtime.GOARCH }
