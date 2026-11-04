package studio

import (
	"encoding/json"
	"net/http"
	"strings"
)

// jsonEscape makes a Go error string safe inside a JSON literal.
func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return strings.Trim(string(b), `"`)
}

// writeViewerJSONError emits a JSON error object via encoding/json —
// hand-concatenated JSON documents are exactly how quoting bugs happen
// (CodeQL go/unsafe-quoting): every dynamic string must go through the
// structured serializer, never through string building.
func writeViewerJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	data, _ := json.Marshal(map[string]string{"error": msg})
	_, _ = w.Write(data)
}
