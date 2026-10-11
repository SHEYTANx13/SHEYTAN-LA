// handoff.go — the durable, versioned recovery record (v1.7.1 §3.6).
//
// One exhaustion episode produces ONE handoff record, written ATOMICALLY
// (temp file + rename, fsync best-effort) under:
//
//	<DataDir>/recovery/<handoffId>.json
//
// The record is integrated with existing session/task state by identity
// (sessionId / threadId / taskId / runId), survives process restart, and
// is the single source a restart/continuation path reads to rebuild the
// recovery injection. Listing and loading are read-only; nothing here
// rewrites session files (the session store stays the session authority).
package recovery

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HandoffVersion is the schema version of the durable record.
const HandoffVersion = 1

// Handoff is the durable recovery record.
type Handoff struct {
	// HandoffVersion is the record schema version.
	HandoffVersion int `json:"handoffVersion"`

	HandoffID string `json:"handoffId"`
	SessionID string `json:"sessionId"`
	ThreadID  string `json:"threadId,omitempty"`
	TaskID    string `json:"taskId,omitempty"`
	RunID     string `json:"runId,omitempty"`

	// SourceAttempt is the 1-based attempt number that exhausted.
	SourceAttempt int `json:"sourceAttempt"`
	// RecoveryAttempt is the 1-based automatic recovery attempt this
	// record enables (1 = first and only automatic attempt).
	RecoveryAttempt int `json:"recoveryAttempt"`

	CreatedAt time.Time `json:"createdAt"`

	SourceBackend string     `json:"sourceBackend,omitempty"`
	SourceModel   string     `json:"sourceModel,omitempty"`
	Detected      Exhaustion `json:"detectedContextCondition"`

	// Summary is the whole-context recovery summary (hierarchical when a
	// summarizer was available, deterministic otherwise). SummarySource
	// records which one produced it: "hierarchical" | "fallback".
	Summary        string            `json:"summary"`
	SummarySource  string            `json:"summarySource,omitempty"`
	Mission        string            `json:"mission,omitempty"`
	Facts          []string          `json:"facts,omitempty"`
	Decisions      []string          `json:"decisions,omitempty"`
	OpenThreads    []string          `json:"openThreads,omitempty"`
	Artifacts      []string          `json:"artifacts,omitempty"`
	NextActions    []string          `json:"nextActions,omitempty"`
	CompletedWork  []string          `json:"completedWork,omitempty"`
	LastUserPrompt string            `json:"lastUserPrompt,omitempty"`
	Preferences    []string          `json:"preferences,omitempty"`
	Configuration  map[string]string `json:"configuration,omitempty"`

	// SnapshotTokens records the estimated size of the exhausted logical
	// context (observability, not injected).
	SnapshotTokens int `json:"snapshotTokens,omitempty"`
}

// NewHandoff builds the durable record from the frozen snapshot.
func NewHandoff(snap *Snapshot, summary, summarySource string) *Handoff {
	h := &Handoff{
		HandoffVersion:  HandoffVersion,
		HandoffID:       newHandoffID(),
		SessionID:       snap.SessionID,
		ThreadID:        snap.ThreadID,
		TaskID:          snap.TaskID,
		RunID:           snap.RunID,
		SourceAttempt:   snap.Attempt,
		RecoveryAttempt: 1,
		CreatedAt:       time.Now().UTC(),
		SourceBackend:   snap.SourceBackend,
		SourceModel:     snap.SourceModel,
		Detected:        snap.Detected,
		Summary:         summary,
		SummarySource:   summarySource,
		Mission:         snap.Mission,
		Facts:           snap.Facts,
		Decisions:       snap.Decisions,
		OpenThreads:     snap.OpenThreads,
		Artifacts:       snap.Artifacts,
		NextActions:     snap.NextActions,
		CompletedWork:   snap.CompletedWork(),
		Preferences:     snap.Preferences,
		Configuration:   snap.Configuration,
		SnapshotTokens:  snap.TokenCount(),
	}
	h.LastUserPrompt = lastUserContent(snap.Messages)
	return h
}

// newHandoffID produces a collision-resistant id:
// ho-<utc-compact>-<8 hex random>.
func newHandoffID() string {
	var rnd [4]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		// Time fallback — still unique enough for one process.
		return fmt.Sprintf("ho-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("ho-%s-%s",
		time.Now().UTC().Format("20060102T150405"),
		hex.EncodeToString(rnd[:]),
	)
}

// lastUserContent returns the last user message content (the continuation
// anchor — the same user turn is NOT re-appended during injection).
func lastUserContent(msgs []Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return clip(msgs[i].Content, 2000)
		}
	}
	return ""
}

// --- durable store -------------------------------------------------------------

// Store persists handoff records under one directory. Safe for concurrent
// use; every write is atomic (temp + rename in the same directory).
type Store struct {
	dir string
}

// NewStore returns the handoff store for dir (created lazily on Save).
func NewStore(dir string) *Store { return &Store{dir: dir} }

// Dir returns the store directory.
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

// Save writes the record atomically and durably (temp file in the same
// directory, rename over the target; fsync best-effort on the file).
func (s *Store) Save(h *Handoff) error {
	if s == nil || s.dir == "" {
		return fmt.Errorf("recovery store: no directory")
	}
	if h == nil {
		return fmt.Errorf("recovery store: nil handoff")
	}
	if h.HandoffID == "" {
		h.HandoffID = newHandoffID()
	}
	if h.CreatedAt.IsZero() {
		h.CreatedAt = time.Now().UTC()
	}

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("recovery store: %w", err)
	}

	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return fmt.Errorf("recovery store: %w", err)
	}

	final := s.path(h.HandoffID)
	tmp := final + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("recovery store: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("recovery store: %w", err)
	}
	_ = f.Sync()
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("recovery store: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("recovery store: %w", err)
	}
	return nil
}

// Get loads one record by id.
func (s *Store) Get(handoffID string) (*Handoff, error) {
	if s == nil || handoffID == "" {
		return nil, fmt.Errorf("recovery store: no id")
	}
	data, err := os.ReadFile(s.path(handoffID))
	if err != nil {
		return nil, fmt.Errorf("recovery store: %w", err)
	}
	var h Handoff
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, fmt.Errorf("recovery store: corrupt record: %w", err)
	}
	if h.HandoffVersion > HandoffVersion {
		return nil, fmt.Errorf("recovery store: unsupported version %d", h.HandoffVersion)
	}
	return &h, nil
}

// List returns all records for one session, oldest first. A nil sessionID
// lists every record. Unreadable/corrupt files are skipped (recovery must
// never hard-fail on a torn record when others exist).
func (s *Store) List(sessionID string) []*Handoff {
	if s == nil || s.dir == "" {
		return nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}

	var out []*Handoff
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") ||
			strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var h Handoff
		if json.Unmarshal(data, &h) != nil || h.HandoffID == "" {
			continue
		}
		if sessionID != "" && h.SessionID != sessionID {
			continue
		}
		out = append(out, &h)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// Latest returns the newest record for a session (nil when none).
func (s *Store) Latest(sessionID string) *Handoff {
	all := s.List(sessionID)
	if len(all) == 0 {
		return nil
	}
	return all[len(all)-1]
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}
