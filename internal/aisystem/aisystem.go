// Package aisystem implements SHEYTAN's AI System authority (v1.9.0).
//
// An AI System is a FIRST-CLASS, USER-OWNED configuration object: a named,
// revisioned bundle of instructions, model policy, reasoning/effort policy,
// allowed tool surface, approval policy, skills policy, memory/knowledge
// policy, compaction policy and verification policy. Exactly ONE system is
// ACTIVE at a time; the active system's snapshot is frozen into every run
// (systemId + systemRevision) so a mid-run edit can never mutate a run's
// bound configuration.
//
// Design contract (the one-authority rule):
//
//   - This package OWNS the AI System object store. It does NOT duplicate
//     the model manager, tool registry, scheduler, memory, Governor, skills
//     or session storage — it COMPOSES them at run start through the
//     existing orchestrator seams (ToolPolicy constrain, thinking ladder,
//     briefing injection, model selection).
//   - Persistence follows the house store pattern: one JSON document per
//     system under <DataDir>/ai-systems/, atomic temp+rename writes,
//     bounded, deterministic ordering, reload-safe, corruption-tolerant
//     (an unreadable document is skipped and reported, never crash-looped).
//   - Fresh installs get exactly ONE valid default system whose behavior
//     is byte-identical to the pre-v1.9 runtime (empty instructions, no
//     overrides). Existing config is migrated non-destructively: nothing
//     outside the ai-systems directory is touched.
//   - "The model proposes. The tools execute. The laboratory verifies."
//     An AI System constrains what the model may PROPOSE; it can never
//     widen permissions beyond the existing security authorities.
package aisystem

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

// ReservedID is the system id of the always-present default system. Its
// behavior is the pre-v1.9 runtime behavior exactly (no instructions, no
// overrides). It can never be deleted.
const ReservedID = "default"

// Approval policies (the one risk vocabulary lives in the approval gate;
// here it is only WHICH calls pause).
const (
	ApprovalAuto     = "auto"      // never pause for approvals
	ApprovalAskRisky = "ask-risky" // pause for external-network/destructive/privileged (default)
	ApprovalAskAll   = "ask-all"   // pause for every tool call
)

// Reasoning ladder (the EXISTING v1.8.5 numeric ladder — no second
// mechanism): low=0, mid=1024, high=4096, ultra=engine default.
const (
	ReasoningLow    = "low"
	ReasoningMid    = "mid"
	ReasoningHigh   = "high"
	ReasoningUltra  = "ultra"
	ReasoningUnspec = "" // runtime default (composer's own tier posture)
)

// Verification policies.
const (
	VerificationStandard = "standard"
	VerificationStrict   = "strict"
)

// Bounds. Every persisted document is bounded; the store is bounded.
const (
	MaxSystems                     = 200
	MaxNameLen                     = 200
	MaxInstructionsLen             = 64 * 1024
	MaxAllowedTools                = 64
	MaxKnowledgeRefs               = 64
	MaxEnabledSkills               = 256
	maxDocumentBytes               = 256 * 1024
	filePerm           os.FileMode = 0o600
	dirPerm            os.FileMode = 0o700
)

// System is one persisted AI System object.
type System struct {
	SystemID     string `json:"systemId"`
	Name         string `json:"name"`
	Revision     int    `json:"revision"`
	Instructions string `json:"instructions"`

	// Model override ("": use the runtime's selected model — never a
	// hard-coded model identity).
	Model string `json:"model,omitempty"`

	// Reasoning/effort preference on the EXISTING ladder.
	Reasoning string `json:"reasoning,omitempty"`

	// AllowedTools is the system's tool surface. Empty = every tool the
	// runtime policy already offers. Non-empty = the run's tool surface is
	// constrained to this set (server-side, never prompt-side). It can
	// never widen anything.
	AllowedTools []string `json:"allowedTools,omitempty"`

	// ApprovalPolicy: which tool calls pause for human approval.
	ApprovalPolicy string `json:"approvalPolicy"`

	// EnabledSkills: the skills surface. Empty = every installed skill is
	// discoverable; non-empty = only the listed skill ids activate.
	EnabledSkills []string `json:"enabledSkills,omitempty"`

	// KnowledgeRefs: cross-mode history references / repository scope the
	// system prefers (advisory input to the existing retrieval seam).
	KnowledgeRefs []string `json:"knowledgeRefs,omitempty"`

	// MemoryPolicy: "" or "default" (existing memory behavior), "off",
	// "read-only".
	MemoryPolicy string `json:"memoryPolicy,omitempty"`

	// CompactionPolicy: "" or "default", "conservative", "aggressive" —
	// a preference input to the existing context/continuum authorities.
	CompactionPolicy string `json:"compactionPolicy,omitempty"`

	// RuntimeProfile: advisory resource posture hint consumed by the
	// Governor-owned envelope (never a bypass).
	RuntimeProfile string `json:"runtimeProfile,omitempty"`

	// VerificationPolicy: standard | strict.
	VerificationPolicy string `json:"verificationPolicy"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Snapshot is the FROZEN per-run binding. Every run resolves its system
// once, at start; later edits to the System object never mutate an
// in-flight run's snapshot.
type Snapshot struct {
	SystemID           string   `json:"systemId"`
	SystemRevision     int      `json:"systemRevision"`
	Name               string   `json:"name"`
	Instructions       string   `json:"instructions"`
	Model              string   `json:"model,omitempty"`
	Reasoning          string   `json:"reasoning,omitempty"`
	AllowedTools       []string `json:"allowedTools,omitempty"`
	ApprovalPolicy     string   `json:"approvalPolicy,omitempty"`
	EnabledSkills      []string `json:"enabledSkills,omitempty"`
	VerificationPolicy string   `json:"verificationPolicy,omitempty"`
}

// Snapshot returns the frozen value copy of the system (defensive: the
// slices are copied so later mutation of the stored document can never
// leak into an in-flight run).
func (s System) Snapshot() Snapshot {
	snap := Snapshot{
		SystemID:           s.SystemID,
		SystemRevision:     s.Revision,
		Name:               s.Name,
		Instructions:       s.Instructions,
		Model:              s.Model,
		Reasoning:          s.Reasoning,
		ApprovalPolicy:     s.ApprovalPolicy,
		VerificationPolicy: s.VerificationPolicy,
	}
	if len(s.AllowedTools) > 0 {
		snap.AllowedTools = append([]string(nil), s.AllowedTools...)
	}
	if len(s.EnabledSkills) > 0 {
		snap.EnabledSkills = append([]string(nil), s.EnabledSkills...)
	}
	return snap
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	ErrNotFound = errors.New("ai system not found")
	ErrReserved = errors.New("the default AI System cannot be deleted or replaced")
	ErrInvalid  = errors.New("invalid AI System")
	ErrTooMany  = errors.New("the AI System store is full")
)

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

// Store is the ONE AI System store. It is safe for concurrent use.
type Store struct {
	mu     sync.Mutex
	dir    string
	active string // "" until first resolution
}

// Open loads (or initializes) the store under <DataDir>/ai-systems.
// A fresh or empty store receives the default system, which becomes
// active — the exact pre-v1.9 runtime behavior.
func Open(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "ai-systems")
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("ai systems dir: %w", err)
	}

	s := &Store{dir: dir}
	if err := s.ensureDefault(); err != nil {
		return nil, err
	}
	if err := s.loadActiveOrMigrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Dir returns the persistence directory (diagnostics only).
func (s *Store) Dir() string { return s.dir }

func (s *Store) systemPath(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *Store) activePath() string {
	return filepath.Join(s.dir, "active.json")
}

// writeAtomic writes a document via unique-temp + rename (the house
// pattern — never a partial document on disk).
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-aisystem-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func newID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// Deterministic fallback (time-based); crypto/rand failure is
		// practically impossible, but the store never returns a collision.
		return fmt.Sprintf("sys-%x", time.Now().UnixNano())
	}
	return "sys-" + hex.EncodeToString(buf)
}

// DefaultSystem builds the always-present default system: byte-identical
// runtime behavior to the pre-v1.9 product.
func DefaultSystem() System {
	now := time.Now().UTC()
	return System{
		SystemID:           ReservedID,
		Name:               "Default",
		Revision:           1,
		Instructions:       "",
		ApprovalPolicy:     ApprovalAskRisky,
		VerificationPolicy: VerificationStandard,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

// ensureDefault creates the default system when missing (idempotent).
func (s *Store) ensureDefault() error {
	if _, err := os.Stat(s.systemPath(ReservedID)); err == nil {
		return nil
	}
	def := DefaultSystem()
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.systemPath(ReservedID), data)
}

// loadActiveOrMigrate resolves the active pointer. Migration rule for
// existing users: a missing/unreadable/dangling pointer resolves to the
// default system (never a destructive change, never a second source).
func (s *Store) loadActiveOrMigrate() error {
	data, err := os.ReadFile(s.activePath())
	if err == nil && len(data) > 0 {
		var ptr struct {
			SystemID string `json:"systemId"`
		}
		if json.Unmarshal(data, &ptr) == nil && ptr.SystemID != "" {
			if _, err := s.read(ptr.SystemID); err == nil {
				s.active = ptr.SystemID
				return nil
			}
		}
	}
	// First boot / repair: default active, persisted.
	s.active = ReservedID
	return s.persistActive(ReservedID)
}

func (s *Store) persistActive(id string) error {
	data, err := json.MarshalIndent(struct {
		SystemID string `json:"systemId"`
	}{SystemID: id}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.activePath(), data)
}

// read loads one system document from disk.
func (s *Store) read(id string) (System, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return System{}, ErrNotFound
	}
	data, err := os.ReadFile(s.systemPath(id))
	if err != nil {
		return System{}, ErrNotFound
	}
	if len(data) > maxDocumentBytes {
		return System{}, ErrInvalid
	}
	var sys System
	if err := json.Unmarshal(data, &sys); err != nil {
		return System{}, ErrInvalid
	}
	if sys.SystemID != id {
		return System{}, ErrInvalid
	}
	return sys, nil
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

// List returns every system deterministically ordered (CreatedAt, then
// SystemID). Unreadable documents are skipped (corruption tolerance) —
// the store never crash-loops on one bad file.
func (s *Store) List() ([]System, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked()
}

func (s *Store) listLocked() ([]System, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := make([]System, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == "active.json" {
			continue
		}
		if len(out) >= MaxSystems {
			break
		}
		sys, err := s.read(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue // corruption-tolerant listing
		}
		out = append(out, sys)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].SystemID < out[j].SystemID
	})
	return out, nil
}

// Get returns one system.
func (s *Store) Get(id string) (System, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(id)
}

// Validate checks one system's fields; every persisted/imported document
// passes through it.
func Validate(sys *System) error {
	sys.Name = strings.TrimSpace(sys.Name)
	if sys.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if len(sys.Name) > MaxNameLen {
		return fmt.Errorf("%w: name exceeds %d chars", ErrInvalid, MaxNameLen)
	}
	if len(sys.Instructions) > MaxInstructionsLen {
		return fmt.Errorf("%w: instructions exceed %d bytes", ErrInvalid, MaxInstructionsLen)
	}
	if len(sys.AllowedTools) > MaxAllowedTools {
		return fmt.Errorf("%w: too many allowed tools", ErrInvalid)
	}
	if len(sys.KnowledgeRefs) > MaxKnowledgeRefs {
		return fmt.Errorf("%w: too many knowledge refs", ErrInvalid)
	}
	if len(sys.EnabledSkills) > MaxEnabledSkills {
		return fmt.Errorf("%w: too many enabled skills", ErrInvalid)
	}
	sys.ApprovalPolicy = NormalizeApproval(sys.ApprovalPolicy)
	sys.Reasoning = NormalizeReasoning(sys.Reasoning)
	sys.VerificationPolicy = strings.TrimSpace(sys.VerificationPolicy)
	if sys.VerificationPolicy == "" {
		sys.VerificationPolicy = VerificationStandard
	}
	if sys.VerificationPolicy != VerificationStandard && sys.VerificationPolicy != VerificationStrict {
		return fmt.Errorf("%w: unknown verification policy %q", ErrInvalid, sys.VerificationPolicy)
	}
	switch sys.MemoryPolicy {
	case "", "default", "off", "read-only":
	default:
		return fmt.Errorf("%w: unknown memory policy %q", ErrInvalid, sys.MemoryPolicy)
	}
	switch sys.CompactionPolicy {
	case "", "default", "conservative", "aggressive":
	default:
		return fmt.Errorf("%w: unknown compaction policy %q", ErrInvalid, sys.CompactionPolicy)
	}
	cleaned := make([]string, 0, len(sys.AllowedTools))
	for _, t := range sys.AllowedTools {
		t = strings.TrimSpace(t)
		if t != "" {
			cleaned = append(cleaned, t)
		}
	}
	sys.AllowedTools = cleaned
	return nil
}

// NormalizeApproval maps unknown approval vocabularies to the safe default.
func NormalizeApproval(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ApprovalAuto:
		return ApprovalAuto
	case ApprovalAskAll:
		return ApprovalAskAll
	default:
		return ApprovalAskRisky
	}
}

// NormalizeReasoning maps unknown effort values to the empty (runtime
// default) value — never a fabricated budget.
func NormalizeReasoning(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ReasoningLow:
		return ReasoningLow
	case ReasoningMid:
		return ReasoningMid
	case ReasoningHigh:
		return ReasoningHigh
	case ReasoningUltra:
		return ReasoningUltra
	default:
		return ReasoningUnspec
	}
}

// Create persists a NEW system (revision 1).
func (s *Store) Create(sys System) (System, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, _ := s.listLocked()
	if len(existing) >= MaxSystems {
		return System{}, ErrTooMany
	}

	sys.SystemID = newID()
	sys.Revision = 1
	now := time.Now().UTC()
	sys.CreatedAt = now
	sys.UpdatedAt = now
	if err := Validate(&sys); err != nil {
		return System{}, err
	}
	if err := s.write(sys); err != nil {
		return System{}, err
	}
	return sys, nil
}

func (s *Store) write(sys System) error {
	data, err := json.MarshalIndent(sys, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.systemPath(sys.SystemID), data)
}

// Update applies a mutation to one system and increments its revision.
func (s *Store) Update(id string, mutate func(*System) error) (System, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sys, err := s.read(id)
	if err != nil {
		return System{}, err
	}
	if err := mutate(&sys); err != nil {
		return System{}, err
	}
	sys.SystemID = id // identity is immutable
	if err := Validate(&sys); err != nil {
		return System{}, err
	}
	sys.Revision++
	sys.UpdatedAt = time.Now().UTC()
	if err := s.write(sys); err != nil {
		return System{}, err
	}
	return sys, nil
}

// Delete removes a non-default system. Deleting the ACTIVE system moves
// activation to the default system (correct active-system handling).
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == ReservedID {
		return ErrReserved
	}
	if _, err := s.read(id); err != nil {
		return err
	}
	if err := os.Remove(s.systemPath(id)); err != nil {
		return err
	}
	if s.active == id {
		s.active = ReservedID
		return s.persistActive(ReservedID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Activation
// ---------------------------------------------------------------------------

// Select activates a system. The NEXT run binds the new system's snapshot;
// in-flight runs keep their frozen snapshot (value semantics).
func (s *Store) Select(id string) (System, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sys, err := s.read(id)
	if err != nil {
		return System{}, err
	}
	s.active = id
	if err := s.persistActive(id); err != nil {
		return System{}, err
	}
	return sys, nil
}

// Active returns the currently active system.
func (s *Store) Active() (System, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(s.activeIDLocked())
}

func (s *Store) activeIDLocked() string {
	if s.active == "" {
		return ReservedID
	}
	return s.active
}

// ActiveSnapshot returns the frozen snapshot of the active system.
func (s *Store) ActiveSnapshot() (Snapshot, error) {
	sys, err := s.Active()
	if err != nil {
		return Snapshot{}, err
	}
	return sys.Snapshot(), nil
}

// SnapshotOf freezes one system by id (explicit per-run selection).
func (s *Store) SnapshotOf(id string) (Snapshot, error) {
	sys, err := s.Get(id)
	if err != nil {
		return Snapshot{}, err
	}
	return sys.Snapshot(), nil
}

// ---------------------------------------------------------------------------
// Clone / Export / Import
// ---------------------------------------------------------------------------

// Clone copies one system into a new one named "<Name> (copy)".
func (s *Store) Clone(id string) (System, error) {
	src, err := s.Get(id)
	if err != nil {
		return System{}, err
	}
	src.Name = strings.TrimSpace(src.Name + " (copy)")
	src.SystemID = ""
	src.Revision = 0
	return s.Create(src)
}

// Export serializes one system (portable JSON document).
func (s *Store) Export(id string) ([]byte, error) {
	sys, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(sys, "", "  ")
}

// Import persists an exported document as a NEW system (a fresh identity
// and revision 1 — import never overwrites an existing system).
func (s *Store) Import(data []byte) (System, error) {
	if len(data) > maxDocumentBytes {
		return System{}, ErrInvalid
	}
	var sys System
	if err := json.Unmarshal(data, &sys); err != nil {
		return System{}, fmt.Errorf("%w: unreadable document", ErrInvalid)
	}
	sys.SystemID = ""
	sys.Revision = 0
	return s.Create(sys)
}

// ensure filePerm is used (kept for future direct-file tooling).
var _ = filePerm
