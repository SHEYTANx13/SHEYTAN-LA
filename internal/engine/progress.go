package engine

import (
	"encoding/json"
	"fmt"
	"time"
)

// Progress events (work order section 5.2: "structured load and
// generation progress events"). Events are plain JSON-serializable
// structs delivered on a caller-provided callback; the callback runs on
// the engine goroutine, so it must not block.

// EventKind enumerates the progress event vocabulary.
type EventKind string

const (
	EventLoadStart     EventKind = "load_start"
	EventLoadProgress  EventKind = "load_progress"
	EventLoadDone      EventKind = "load_done"
	EventGenerateStart EventKind = "generate_start"
	EventGenerateToken EventKind = "generate_token"
	EventGenerateDone  EventKind = "generate_done"
	EventSessionClosed EventKind = "session_closed"
)

// Event is one structured progress record. Phase/Step/Total carry the
// machine-readable progress; Detail is human context; TimestampUtc is
// the emission time (the device-truth chain pairs events with times).
type Event struct {
	Kind         EventKind `json:"kind"`
	SessionID    string    `json:"session,omitempty"`
	Phase        string    `json:"phase,omitempty"` // e.g. "safetensors", "graph", "weights"
	Step         int       `json:"step,omitempty"`
	Total        int       `json:"total,omitempty"`
	TokenID      int32     `json:"token_id,omitempty"`
	Detail       string    `json:"detail,omitempty"`
	TimestampUtc string    `json:"timestamp_utc"`
}

func newEvent(kind EventKind) Event {
	return Event{Kind: kind, TimestampUtc: nowUTCString()}
}

// nowUTCString is the shared RFC3339Nano UTC timestamp formatter.
func nowUTCString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// EventSink receives progress events; nil means "drop events".
type EventSink func(Event)

// emitJSON renders an event as one JSON line (for CLI/server surfaces).
func (e Event) emitJSON() string {
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Sprintf(`{"kind":%q,"error":"marshal failed"}`, string(e.Kind))
	}
	return string(b)
}
