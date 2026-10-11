package api

// models_import.go — v1.6.1: the first-class local GGUF import surface.
//
//   POST /api/models/import
//        body: {"path": "/abs/model.gguf"}
//        Validates the GGUF header, streams the file into the managed
//        models directory (atomic placement, duplicate-safe, source never
//        modified) and returns the import result. Selection is driven
//        immediately afterwards by the SAME frontend flow that calls the
//        existing POST /api/models/select — one selection state machine,
//        no duplicate implementation here.
//
//   POST /api/models/import/pick
//        Opens the platform-native multi-select GGUF file dialog
//        (comdlg32 on Windows) and returns the chosen absolute paths.
//        Non-Windows hosts answer 501 with an honest message — the
//        import endpoint itself stays usable with a typed path.

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/native"
)

// modelsImportRequestBody is the POST /api/models/import body.
type modelsImportRequestBody struct {
	// Path is the source path of the GGUF file to import (absolute, or
	// resolvable from the server's working directory).
	Path string `json:"path"`
}

// modelsImportResponse is the successful import result.
type modelsImportResponse struct {
	OK bool `json:"ok"`
	// Name is the final file name inside the managed models directory
	// (the id the selection API accepts).
	Name string `json:"name"`
	// Path is the absolute path of the imported copy.
	Path string `json:"path"`
	// SizeBytes is the verified size of the imported file.
	SizeBytes int64 `json:"sizeBytes"`
	// Duplicate is true when an identical file (same size + SHA-256)
	// already existed and the copy was skipped.
	Duplicate bool `json:"duplicate"`
	// RenamedFrom carries the original name when a collision forced a
	// fresh one.
	RenamedFrom string `json:"renamedFrom,omitempty"`
	// Architecture / Quantization / ContextLength / ParameterInfo are the
	// parsed GGUF header facts (validated before the copy started).
	Architecture  string `json:"architecture,omitempty"`
	Quantization  string `json:"quantization,omitempty"`
	ContextLength int    `json:"contextLength,omitempty"`
	ParameterInfo string `json:"parameterInfo,omitempty"`
}

// handleModelsImport imports one local GGUF into the managed models
// directory. The response carries the model id the EXISTING selection
// API (/api/models/select) accepts, so the UI can select it in the same
// user action.
func (s *Server) handleModelsImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	var body modelsImportRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("decode import body: %w", err))
		return
	}

	cfg := s.src.Load()

	if cfg.IsRemote() {
		writeErr(w, http.StatusBadRequest, fmt.Errorf(
			"remote provider active — model import is a local-engine concept",
		))
		return
	}

	result, err := llm.ImportModel(cfg.ModelsDir, body.Path, nil)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	resp := modelsImportResponse{
		OK:           true,
		Name:         result.Name,
		Path:         result.Path,
		SizeBytes:    result.SizeBytes,
		Duplicate:    result.Duplicate,
		RenamedFrom:  result.RenamedFrom,
	}

	if result.Card != nil {
		resp.Architecture = result.Card.Arch
		resp.Quantization = result.Card.Quant
		resp.ContextLength = result.Card.ContextLength
		resp.ParameterInfo = result.Card.FormatParams()
	}

	writeJSON(w, resp)
}

// handleModelsImportPick opens the native GGUF file picker.
func (s *Server) handleModelsImportPick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	cfg := s.src.Load()

	result := native.PickFiles(
		"Import GGUF models",
		[]native.FileFilter{
			{Label: "GGUF model files", Pattern: "*.gguf"},
			{Label: "All files", Pattern: "*.*"},
		},
		cfg.ModelsDir,
	)

	if result.Err != nil {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf(
			"native file picker unavailable: %v — type or paste the model path into the import field instead",
			result.Err,
		))
		return
	}

	if result.Canceled || len(result.Paths) == 0 {
		writeJSON(w, map[string]any{"canceled": true, "paths": []string{}})
		return
	}

	writeJSON(w, map[string]any{"canceled": false, "paths": result.Paths})
}
