package localipc

import (
	"encoding/json"
	"net/http"
)

// writeJSONIpc writes one bounded JSON response.
func writeJSONIpc(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

// writeIpcError writes one bounded structured error response. Error
// responses never echo request bodies or secrets.
func writeIpcError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":    message,
		"status":   status,
		"protocol": ProtocolName,
	})
}
