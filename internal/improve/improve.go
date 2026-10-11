// Package improve implements Phase 11: SHEYTAN's self-improvement loop
// architecture.
//
// The cycle:
//
//	Understand → Compile → Predict → Plan → Act → Observe → Verify →
//	Compare prediction vs outcome → Diagnose → Update active tactics →
//	Validate → Store only validated learning
//
// The central rule: the model's own statement of success is never
// evidence. A tactic becomes persistent ONLY when the objective
// verification layer (the laboratory) confirms the predicted outcome.
// This package records predictions, compares them with measured results,
// and promotes tactics exclusively on verified evidence — everything else
// stays a bounded, quarantined candidate.
package improve

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Tactic is one active, learned approach ("when X, do Y, expecting Z").
type Tactic struct {
	ID         string     `json:"id"`
	Condition  string     `json:"condition"`  // when this situation applies
	Action     string     `json:"action"`     // do this
	Prediction string     `json:"prediction"` // expecting this measurable outcome
	State      State      `json:"state"`
	Uses       int        `json:"uses"`
	Successes  int        `json:"successes"`
	Created    time.Time  `json:"created"`
	Updated    time.Time  `json:"updated"`
	Evidence   []Evidence `json:"evidence"`
}

// State is the tactic lifecycle state.
type State string

const (
	// StateCandidate: not yet validated; never injected into prompts.
	StateCandidate State = "candidate"
	// StateActive: validated by the lab; may guide planning.
	StateActive State = "active"
	// StateRetired: contradicted by later evidence.
	StateRetired State = "retired"
)

// Evidence is one measured validation point.
type Evidence struct {
	At           time.Time `json:"at"`
	RunID        string    `json:"runId,omitempty"`
	Predicted    string    `json:"predicted"`
	Observed     string    `json:"observed"`
	Matched      bool      `json:"matched"`
	Verification string    `json:"verification"` // objective verdict, never model prose
}

// Attempt is one prediction→outcome comparison from a run.
type Attempt struct {
	RunID        string    `json:"runId,omitempty"`
	TacticID     string    `json:"tacticId,omitempty"`
	Predicted    string    `json:"predicted"`
	Observed     string    `json:"observed"`
	Matched      bool      `json:"matched"`
	Verification string    `json:"verification"` // e.g. "verified" from the EvidenceCollector
	At           time.Time `json:"at"`
}

// Store persists tactics (bounded JSONL).
type Store struct {
	mu         sync.Mutex
	path       string
	maxTactics int
}

// NewStore creates the tactic store under dir.
func NewStore(dir string) *Store {
	return &Store{
		path:       filepath.Join(dir, "tactics.jsonl"),
		maxTactics: 128,
	}
}

// Load reads all tactics.
func (s *Store) Load() ([]Tactic, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

// loadLocked reads all tactics. It MUST be called with s.mu already held.
// Callers that already hold the lock must use this instead of Load(), which
// re-acquires the mutex. sync.Mutex is not reentrant: Load() under a held
// lock deadlocks (the 1.1.6 CI hang in Propose/RecordAttempt).
func (s *Store) loadLocked() ([]Tactic, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Tactic
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var t Tactic
		if json.Unmarshal([]byte(line), &t) == nil {
			out = append(out, t)
		}
	}
	return out, nil
}

// saveLocked rewrites the store, bounded.
func (s *Store) saveLocked(tactics []Tactic) error {
	if len(tactics) > s.maxTactics {
		tactics = append([]Tactic(nil), tactics[len(tactics)-s.maxTactics:]...)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	for _, t := range tactics {
		line, err := json.Marshal(t)
		if err != nil {
			continue
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Propose records a candidate tactic. Candidates NEVER guide planning —
// they wait for validation.
func (s *Store) Propose(t Tactic) error {
	if strings.TrimSpace(t.Condition) == "" ||
		strings.TrimSpace(t.Action) == "" ||
		strings.TrimSpace(t.Prediction) == "" {
		return fmt.Errorf("tactic needs condition, action and prediction")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.loadLocked()
	if err != nil {
		return fmt.Errorf("read tactic store: %w", err)
	}
	for i := range existing {
		if existing[i].ID == t.ID {
			return fmt.Errorf("tactic %q already exists", t.ID)
		}
	}

	t.State = StateCandidate
	t.Created = time.Now().UTC()
	t.Updated = t.Created

	if err := s.saveLocked(append(existing, t)); err != nil {
		return fmt.Errorf("persist tactic: %w", err)
	}
	return nil
}

// RecordAttempt applies one prediction→outcome comparison to a tactic and
// returns whether the tactic moved state. The comparison uses the OBJECTIVE
// verification verdict: attempts whose verification is not "verified" can
// neither confirm nor advance a tactic.
func (s *Store) RecordAttempt(att Attempt) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if att.At.IsZero() {
		att.At = time.Now().UTC()
	}

	existing, err := s.loadLocked()
	if err != nil {
		return "", fmt.Errorf("read tactic store: %w", err)
	}
	for i := range existing {
		if existing[i].ID != att.TacticID {
			continue
		}

		t := &existing[i]
		t.Uses++
		t.Updated = att.At
		t.Evidence = append(t.Evidence, Evidence{
			At:           att.At,
			RunID:        att.RunID,
			Predicted:    att.Predicted,
			Observed:     att.Observed,
			Matched:      att.Matched,
			Verification: att.Verification,
		})
		// Bound the evidence trail.
		if len(t.Evidence) > 16 {
			t.Evidence = t.Evidence[len(t.Evidence)-16:]
		}

		objectivelyVerified := strings.EqualFold(att.Verification, "verified")

		switch {
		case objectivelyVerified && att.Matched:
			t.Successes++
			if t.State != StateActive && t.Successes >= 2 {
				// Two independently verified predictions promote.
				t.State = StateActive
			}
		case objectivelyVerified && !att.Matched:
			// A verified contradiction retires the tactic.
			t.State = StateRetired
		default:
			// Unverified attempts change nothing — by design.
		}

		if err := s.saveLocked(existing); err != nil {
			return "", fmt.Errorf("persist tactic: %w", err)
		}
		return t.State, nil
	}

	return "", fmt.Errorf("tactic %q not found", att.TacticID)
}

// Active returns the tactics allowed to guide planning (active state only).
func (s *Store) Active() []Tactic {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, _ := s.loadLocked()
	var out []Tactic
	for _, t := range all {
		if t.State == StateActive {
			out = append(out, t)
		}
	}
	return out
}

// RenderActive renders active tactics as a bounded system block fragment
// (used by the orchestrator integration; empty when nothing is active).
func RenderActive(tactics []Tactic, maxChars int) string {
	if len(tactics) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## VALIDATED TACTICS (objectively verified on this machine)\n")
	for _, t := range tactics {
		one := fmt.Sprintf("- When %s → %s (expected: %s; verified %d/%d)\n",
			t.Condition, t.Action, t.Prediction, t.Successes, t.Uses)
		if b.Len()+len(one) > maxChars {
			break
		}
		b.WriteString(one)
	}
	return b.String()
}
