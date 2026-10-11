// Package skills implements Phase 7B: a reusable local skills architecture
// with a verified-learning promotion rule.
//
// A skill is a persisted, reusable procedure: identity, trigger, steps,
// tools, prerequisites, verification, failure modes and evidence. Skills
// are load-on-demand — only skills whose trigger matches the current task
// are injected into the prompt, and each injection is token-budgeted, so a
// growing skill library never inflates every conversation.
//
// VERIFIED-LEARNING RULE (the core of the design): a successful execution
// does NOT automatically become a skill. An execution produces a CANDIDATE
// with its objective evidence; promotion to a persistent skill requires
// that the evidence contains a verified (not merely claimed) outcome, and
// the promoted skill records exactly that evidence. This is SHEYTAN's
// validated learning mechanism — the laboratory remains authoritative.
package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Skill is one persisted reusable procedure.
type Skill struct {
	Identity Identity  `json:"identity"`
	Trigger  Trigger   `json:"trigger"`
	Steps    []string  `json:"procedure"`
	Tools    []string  `json:"tools"`
	Prereqs  []string  `json:"prerequisites,omitempty"`
	Verify   []string  `json:"verification,omitempty"`
	Failures []string  `json:"failureModes,omitempty"`
	Evidence Evidence  `json:"evidence"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Uses     int       `json:"uses"`
	Disabled bool      `json:"disabled,omitempty"`
}

// Identity names and versions a skill.
type Identity struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// Trigger decides when a skill is relevant (keyword match on the task).
type Trigger struct {
	Keywords []string `json:"keywords"`
	// TaskTypes optionally restricts to task classes
	// (coding / research / verification...).
	TaskTypes []string `json:"taskTypes,omitempty"`
}

// Evidence is the objective provenance that justified the skill. A skill
// without verified evidence must not exist.
type Evidence struct {
	// Source run identifiers that produced the candidate.
	RunIDs []string `json:"runIds,omitempty"`
	// Verification is the objective verification verdict
	// ("verified", "partially_verified", ...).
	Verification string `json:"verification"`
	// Checks lists the concrete commands/probes that passed.
	Checks []string `json:"checks,omitempty"`
	// ObservedAt is when the evidence was captured.
	ObservedAt time.Time `json:"observedAt"`
}

// Candidate is an unpromoted execution trace awaiting verification.
type Candidate struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Steps     []string  `json:"steps"`
	Tools     []string  `json:"tools"`
	RunID     string    `json:"runId"`
	Created   time.Time `json:"created"`
	Evidence  Evidence  `json:"evidence"`
	Rejection string    `json:"rejection,omitempty"`
}

// Store is the JSONL-backed skill store (append-friendly, bounded).
type Store struct {
	mu        sync.Mutex
	path      string
	skills    []*Skill
	maxSkills int
}

// maxSkillNameID normalizes a skill name into a stable ID fragment.
var idSanitizer = regexp.MustCompile(`[^a-z0-9]+`)

// NewStore opens (or creates) the skill store under dir.
func NewStore(dir string) *Store {
	return &Store{
		path:      filepath.Join(dir, "skills.jsonl"),
		maxSkills: 256,
	}
}

// Load reads the store from disk; a missing file is an empty store.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.skills = nil
			return nil
		}
		return err
	}

	var skills []*Skill
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var sk Skill
		if err := json.Unmarshal([]byte(line), &sk); err == nil {
			skills = append(skills, &sk)
		}
	}
	s.skills = skills
	return nil
}

// saveLocked rewrites the file (bounded to maxSkills newest entries).
func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	// Bound: newest maxSkills entries survive a compaction.
	if len(s.skills) > s.maxSkills {
		s.skills = append([]*Skill(nil), s.skills[len(s.skills)-s.maxSkills:]...)
	}

	var b strings.Builder
	for _, sk := range s.skills {
		line, err := json.Marshal(sk)
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

// All returns a snapshot of loaded skills.
func (s *Store) All() []Skill {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Skill, 0, len(s.skills))
	for _, sk := range s.skills {
		out = append(out, *sk)
	}
	return out
}

// Count returns the number of persisted skills.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.skills)
}

// MatchTask returns skills whose trigger keywords intersect the task text,
// most recently updated first, bounded to limit. Empty when nothing
// matches — skills are load-on-demand, never injected wholesale.
func (s *Store) MatchTask(task string, limit int) []Skill {
	taskLower := strings.ToLower(task)

	s.mu.Lock()
	defer s.mu.Unlock()

	var matched []*Skill
	for _, sk := range s.skills {
		if sk.Disabled {
			continue
		}
		for _, kw := range sk.Trigger.Keywords {
			if kw != "" && strings.Contains(taskLower, strings.ToLower(kw)) {
				matched = append(matched, sk)
				break
			}
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].Updated.After(matched[j].Updated)
	})

	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}

	out := make([]Skill, 0, len(matched))
	for _, sk := range matched {
		out = append(out, *sk)
	}
	return out
}

// RenderBlock renders matched skills as a compact system block, bounded by
// maxTokens (token estimate ≈ chars/4). Returns "" when nothing fits.
func RenderBlock(matched []Skill, maxTokens int) string {
	if len(matched) == 0 || maxTokens <= 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## APPLICABLE SKILLS (verified procedures for this task)\n")

	budget := maxTokens * 4 // chars
	used := b.Len()

	for _, sk := range matched {
		var one strings.Builder
		fmt.Fprintf(&one, "\n### %s (v%d, verified %s)\n",
			sk.Identity.Name, sk.Identity.Version,
			sk.Evidence.Verification)
		for i, step := range sk.Steps {
			fmt.Fprintf(&one, "%d. %s\n", i+1, step)
		}
		if len(sk.Tools) > 0 {
			fmt.Fprintf(&one, "Tools: %s\n", strings.Join(sk.Tools, ", "))
		}
		if len(sk.Failures) > 0 {
			fmt.Fprintf(&one, "Watch for: %s\n", strings.Join(sk.Failures, "; "))
		}

		if used+one.Len() > budget {
			break
		}
		b.WriteString(one.String())
		used += one.Len()
	}

	if b.Len() <= len("## APPLICABLE SKILLS (verified procedures for this task)\n") {
		return ""
	}
	return b.String()
}

// PromoteCandidate promotes a candidate to a persistent skill ONLY when the
// evidence carries a verified objective outcome. Returns the promoted skill
// and true on success; returns (nil, false, reason) otherwise. The reason
// is always recorded on the candidate by the caller.
func PromoteCandidate(c Candidate) (*Skill, bool, string) {
	switch strings.ToLower(strings.TrimSpace(c.Evidence.Verification)) {
	case "verified":
		// objective verification passed — promotable
	case "partially_verified":
		return nil, false, "evidence is only partially_verified — a skill must be fully verified"
	case "":
		return nil, false, "no objective verification evidence attached"
	default:
		return nil, false, fmt.Sprintf("verification outcome %q is not a verified result", c.Evidence.Verification)
	}

	if len(c.Steps) == 0 {
		return nil, false, "candidate has no procedure steps"
	}

	now := time.Now().UTC()
	id := idSanitizer.ReplaceAllString(strings.ToLower(c.Name), "-")
	id = strings.Trim(id, "-")
	if id == "" {
		id = fmt.Sprintf("skill-%d", now.Unix())
	}

	sk := &Skill{
		Identity: Identity{ID: id, Name: c.Name, Version: 1},
		Trigger: Trigger{
			// Default trigger: the skill name and its nouns. Callers may
			// refine the trigger before promotion via PromoteWithTrigger.
			Keywords: triggerKeywordsFrom(c.Name),
		},
		Steps: append([]string(nil), c.Steps...),
		Tools: append([]string(nil), c.Tools...),
		Evidence: Evidence{
			RunIDs:       []string{c.RunID},
			Verification: c.Evidence.Verification,
			Checks:       append([]string(nil), c.Evidence.Checks...),
			ObservedAt:   now,
		},
		Created: now,
		Updated: now,
	}
	return sk, true, ""
}

// PromoteWithTrigger is PromoteCandidate with an explicit trigger.
func PromoteWithTrigger(c Candidate, keywords []string) (*Skill, bool, string) {
	sk, ok, reason := PromoteCandidate(c)
	if !ok {
		return nil, false, reason
	}
	if len(keywords) > 0 {
		sk.Trigger.Keywords = keywords
	}
	return sk, true, ""
}

// SaveSkill inserts or updates a skill (version-bumping same-ID entries).
func (s *Store) SaveSkill(sk *Skill) error {
	if sk == nil {
		return fmt.Errorf("nil skill")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.skills {
		if existing.Identity.ID == sk.Identity.ID {
			sk.Identity.Version = existing.Identity.Version + 1
			sk.Created = existing.Created
			break
		}
	}
	sk.Updated = time.Now().UTC()

	// Replace-or-append.
	replaced := false
	for i, existing := range s.skills {
		if existing.Identity.ID == sk.Identity.ID {
			s.skills[i] = sk
			replaced = true
			break
		}
	}
	if !replaced {
		s.skills = append(s.skills, sk)
	}

	return s.saveLocked()
}

// RecordUse increments the use counter of one skill (fire-and-forget).
func (s *Store) RecordUse(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sk := range s.skills {
		if sk.Identity.ID == id {
			sk.Uses++
			return
		}
	}
}

// triggerKeywordsFrom extracts lowercase words from a skill name as the
// default trigger set.
func triggerKeywordsFrom(name string) []string {
	words := idSanitizer.Split(strings.ToLower(name), -1)
	var out []string
	for _, w := range words {
		if len(w) >= 4 {
			out = append(out, w)
		}
	}
	if len(out) == 0 && name != "" {
		out = []string{strings.ToLower(name)}
	}
	return out
}
