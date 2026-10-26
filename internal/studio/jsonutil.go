package studio

import (
	"encoding/json"
	"strings"
)

// jsonEscape makes a Go error string safe inside a JSON literal.
func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return strings.Trim(string(b), `"`)
}
