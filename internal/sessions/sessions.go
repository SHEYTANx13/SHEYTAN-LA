// Package sessions persists chat sessions.
//
// Layout: one compact JSON file per session (<dir>/<id>.json) plus a
// meta-index (<dir>/index.json) of stubs. The sidebar/List() path only ever
// touches stubs (id, title, timestamps, message count, chapter chain) —
// full histories are loaded exactly once for the session the user opens.
// All writes are atomic (tmp file + rename) and every mutator is
// mutex-serialized, so concurrent AppendMessage calls can never lose a
// message. The index self-heals: orphan session files on disk are folded
// back into the index on load, and index entries whose file vanished are
// dropped.
//
// v1.0.10 rewrite notes (vs the v1.0.9 reconstruction):
//   - saveLocked amortizes the index rewrite: the index is rewritten only
//     when stub metadata actually CHANGED (title/model/preset/thread/
//     chapter/count), not on every message append of an unchanged header.
//   - the activity feed trim reuses the backing array instead of copying
//     per append once the cap is reached.
//   - reconcileLocked (the self-healing pass) rebuilds stubs from orphan
//     files in one directory scan instead of re-reading every file.
package sessions

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// Context carries per-session user settings: the system prompt override,
// the files staged to every turn, the staged attachment ids, the
// max-iterations override, and the per-session context-window policy.
type Context struct {
	SystemPrompt  string   `json:"systemPrompt,omitempty"`
	AttachedFiles []string `json:"attachedFiles,omitempty"`
	AttachmentIDs []string `json:"attachmentIds,omitempty"`
	MaxIterations int      `json:"maxIterations,omitempty"`

	// ContextTokens is the per-session context-window policy in tokens
	// (1.1.6). 0 = inherit the global configured context. The policy
	// travels with the session: Chat A = 8K, Chat B = 32K, Chat C = 16K
	// are independent, and switching sessions restores each chat's own
	// policy. It is a CEILING request, never a raise past the model's
	// real capability — llm.ResolveSessionContext clamps it to the model
	// maximum, the engine's verified window, and the resource-safe value.
	ContextTokens int `json:"contextTokens,omitempty"`

	// HistoryRefs (v1.2.8) pins the cross-mode history sessions the user
	// explicitly attached to this conversation. A reference is DATA, not
	// a copy: the source sessions are retrieved per turn (relevant
	// portions only, provenance-labeled) and are never merged into this
	// transcript. The list is bounded (MaxHistoryRefs) and travels with
	// the session like the staged attachment ids.
	HistoryRefs []HistoryRef `json:"historyRefs,omitempty"`
}

// MaxHistoryRefs bounds the per-session attached history references.
const MaxHistoryRefs = 4

// HistoryRef is one attached cross-mode history reference (stable
// reference to ANOTHER session — never a transcript copy).
type HistoryRef struct {
	SessionID string `json:"sessionId"`
	Mode      string `json:"mode,omitempty"` // metadata hint; the store's own mode is authoritative
	// SummaryVersion pins which summary revision the picker previewed.
	SummaryVersion int `json:"summaryVersion,omitempty"`
	// Ranges optionally restricts retrieval to explicit message index
	// ranges [from,to) — the explicit "read more" control.
	Ranges []HistoryRefRange `json:"ranges,omitempty"`
}

// HistoryRefRange is one half-open message index range [From, To).
type HistoryRefRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// ActivityEntry is one event in a session's persisted activity feed
// (tool calls, plan milestones, errors…). Streaming deltas are never
// persisted — the API layer filters them before calling AppendActivity.
type ActivityEntry struct {
	Type      string    `json:"type"`
	Caption   string    `json:"caption,omitempty"`
	Timestamp time.Time `json:"ts,omitempty"`
}

// maxActivities bounds the per-session activity sidecar. The feed is a
// debugging aid, not a transcript — old entries are dropped when the
// sidecar outgrows the byte cap.
const (
	maxActivities = 200
	maxActivityKB = 256 // sidecar rotation threshold
)

// v1.2.8 session mode identity. A session belongs to exactly ONE
// conversation space: Chat and Agent are two independent histories backed
// by the same runtime, engine and tools — not two views of one transcript.
// The mode is FIXED at creation and never mutated by a UI mode switch
// (switching modes switches the visible session space, it never re-labels
// or merges conversations).
const (
	ModeChat  = "chat"
	ModeAgent = "agent"
)

// DefaultMode is the deterministic mode for sessions that predate the
// mode field (every pre-v1.2.8 session was created through the unified
// agent workspace) and for creates that carry no explicit mode.
const DefaultMode = ModeAgent

// NormalizeMode maps a requested mode onto the two known modes. Empty
// strings resolve to DefaultMode; unknown values ALSO resolve to
// DefaultMode rather than inventing a third space.
func NormalizeMode(mode string) string {
	switch mode {
	case ModeChat:
		return ModeChat
	case ModeAgent:
		return ModeAgent
	default:
		return DefaultMode
	}
}

// activitiesSidecar is the append-only activity log for one session:
// <sessions dir>/<id>.activities.jsonl. v1.0.10 moved the feed OUT of the
// session JSON — the API layer appends an entry per milestone tool event,
// and rewriting a 500-message session file (≈100 KB) per event was the
// single most expensive write in serve mode. One append now costs one
// small write, O(1) in session size.

// Session is one persisted conversation.
type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	Model     string    `json:"model,omitempty"`
	Preset    string    `json:"preset,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`

	// Mode (v1.2.8) is the session's conversation space: "chat" or
	// "agent". Fixed at creation. Legacy files (mode omitted) migrate
	// deterministically to DefaultMode on load — never duplicated, only
	// labeled. omitempty keeps every pre-v1.2.8 byte-identical until its
	// next natural save.
	Mode string `json:"mode,omitempty"`

	ThreadID   string          `json:"threadId,omitempty"` // stable across chapter rollovers
	ParentID   string          `json:"parentId,omitempty"` // previous chapter
	Chapter    int             `json:"chapter,omitempty"`  // 0 = original session
	Context    Context         `json:"context,omitempty"`
	Messages   []llm.Message   `json:"messages,omitempty"`
	Activities []ActivityEntry `json:"activities,omitempty"`

	// MsgCount is the persisted message count so meta-index stubs
	// (Messages == nil) can still answer MessageCount().
	MsgCount int `json:"msgCount,omitempty"`
}

// MessageCount returns the number of messages, on stubs and full sessions
// alike.
func (s *Session) MessageCount() int {
	if s == nil {
		return 0
	}
	if s.Messages != nil {
		return len(s.Messages)
	}
	return s.MsgCount
}

// stubOf projects a session onto its meta-index stub.
func stubOf(sess *Session) *Session {
	return &Session{
		ID:        sess.ID,
		Title:     sess.Title,
		Model:     sess.Model,
		Preset:    sess.Preset,
		CreatedAt: sess.CreatedAt,
		UpdatedAt: sess.UpdatedAt,
		Mode:      sess.Mode,
		ThreadID:  sess.ThreadID,
		ParentID:  sess.ParentID,
		Chapter:   sess.Chapter,
		MsgCount:  sess.MessageCount(),
	}
}

// sameStub reports whether two stubs carry identical metadata (the index
// rewrite is skipped when nothing visible changed).
func sameStub(a, b *Session) bool {
	return a.ID == b.ID && a.Title == b.Title && a.Model == b.Model &&
		a.Preset == b.Preset && a.ThreadID == b.ThreadID &&
		a.ParentID == b.ParentID && a.Chapter == b.Chapter &&
		a.Mode == b.Mode &&
		a.MsgCount == b.MsgCount && a.UpdatedAt.Equal(b.UpdatedAt)
}

// Store persists sessions under Dir.
type Store struct {
	mu      sync.Mutex
	dir     string
	idxPath string
	loaded  bool
	index   []*Session          // stubs, newest first
	pending map[string]*Session // created in-memory, not yet persisted

	// hot (v1.2.4): bounded LRU of fully-loaded sessions, validated by
	// (size, mtime) on every hit so external writers are still detected.
	// This removes the re-read + re-unmarshal of the whole session file
	// from the per-message append path and from repeated Get calls.
	hot map[string]*hotEntry
}

// hotCap bounds the loaded-session cache. Eight full sessions cover the
// active conversation plus the sessions a recall backfill / continuum pass
// touches most recently, with a bounded memory footprint (entries hold
// references to parsed message structs, not file bytes).
const hotCap = 8

// hotEntry is one cached full session plus the disk identity it was loaded
// from. Identity is re-validated on every hit; a mismatch (external edit,
// changed size) silently falls back to a disk reload.
type hotEntry struct {
	sess    *Session
	size    int64
	mod     time.Time
	touched time.Time // LRU stamp
	acts    []ActivityEntry
	actSize int64
	actMod  time.Time
}

// touchHotLocked refreshes the LRU stamp and evicts past the cap.
// Caller holds mu.
func (s *Store) touchHotLocked(id string, e *hotEntry) {
	if s.hot == nil {
		s.hot = map[string]*hotEntry{}
	}
	e.touched = time.Now()
	s.hot[id] = e
	for len(s.hot) > hotCap {
		oldestID := ""
		var oldest time.Time
		for id, e := range s.hot {
			if oldestID == "" || e.touched.Before(oldest) {
				oldestID, oldest = id, e.touched
			}
		}
		if oldestID == "" {
			break
		}
		delete(s.hot, oldestID)
	}
}

// hotGetLocked returns the cached session when the disk identity still
// matches. Caller holds mu.
func (s *Store) hotGetLocked(id string) (*Session, bool) {
	e, ok := s.hot[id]
	if !ok {
		return nil, false
	}
	fi, err := os.Stat(s.path(id))
	if err != nil || fi.Size() != e.size || !fi.ModTime().Equal(e.mod) {
		delete(s.hot, id) // stale (external write) — reload from disk
		return nil, false
	}
	e.touched = time.Now()
	return e.sess, true
}

// copySession returns a read-isolated copy: struct copy plus fresh Messages
// and Activities slice headers, so callers that mutate the result outside
// the store lock (the API run path does exactly that) can never tear the
// cached copy other readers see. Strings are immutable and nested tool-call
// slices are treated as read-only, matching the previous fresh-unmarshal
// semantics.
func copySession(sess *Session) *Session {
	if sess == nil {
		return nil
	}
	out := *sess
	if sess.Messages != nil {
		out.Messages = append([]llm.Message(nil), sess.Messages...)
	}
	if sess.Activities != nil {
		out.Activities = append([]ActivityEntry(nil), sess.Activities...)
	}
	return &out
}

// New returns a session store rooted at dir (created lazily on first
// write; the index is read lazily on first use).
func New(dir string) *Store {
	return &Store{
		dir:     dir,
		idxPath: filepath.Join(dir, "index.json"),
		pending: map[string]*Session{},
	}
}

// Dir returns the store's root directory.
func (s *Store) Dir() string { return s.dir }

// newSessionID returns a filesystem-safe, sortable, collision-resistant id.
func newSessionID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand never fails on supported platforms; degrade to
		// time-only rather than panic.
		return fmt.Sprintf("s%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("s%d%s", time.Now().UnixMilli(), hex.EncodeToString(b[:]))
}

// Create returns a fresh in-memory session in the DEFAULT mode. It is
// persisted by the first Save/Append*/Update* call (so "New chat" costs
// nothing until the user actually types).
func (s *Store) Create() *Session {
	return s.CreateInMode(DefaultMode)
}

// CreateInMode returns a fresh in-memory session fixed to the given
// conversation space (v1.2.8). Empty/unknown modes resolve to DefaultMode.
func (s *Store) CreateInMode(mode string) *Session {
	now := time.Now().UTC()
	sess := &Session{
		ID:        newSessionID(),
		Mode:      NormalizeMode(mode),
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.mu.Lock()
	s.pending[sess.ID] = sess
	s.mu.Unlock()
	return sess
}

// --- persistence primitives ---------------------------------------------

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".json") }

// writeAtomic writes data to path via tmp+rename so a crash can never
// leave a half-written session or index behind.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadIndexLocked reads index.json exactly once, then reconciles it with
// the directory (self-healing both directions).
func (s *Store) loadIndexLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	data, err := os.ReadFile(s.idxPath)
	if err == nil {
		var stubs []*Session
		if json.Unmarshal(data, &stubs) == nil {
			s.index = stubs
		}
	}
	// v1.2.8: legacy index stubs predate the mode field — label them
	// DefaultMode so mode filtering works before the first re-save.
	for _, st := range s.index {
		if st != nil && st.Mode == "" {
			st.Mode = DefaultMode
		}
	}
	s.reconcileLocked()
}

// reconcileLocked folds orphan session files into the index and drops
// index entries whose file disappeared.
func (s *Store) reconcileLocked() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return // no dir yet — nothing to reconcile
	}
	byID := make(map[string]bool, len(s.index))
	for _, st := range s.index {
		byID[st.ID] = true
	}
	changed := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || name == "index.json" {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if byID[id] {
			continue
		}
		// Orphan: rebuild the stub from the file itself.
		if sess, err := s.readLocked(id); err == nil {
			s.index = append(s.index, stubOf(sess))
			changed = true
		}
	}
	// Drop entries whose file is gone (deleted behind our back).
	kept := s.index[:0]
	for _, st := range s.index {
		if _, err := os.Stat(s.path(st.ID)); err == nil {
			kept = append(kept, st)
		} else {
			changed = true
		}
	}
	s.index = kept
	if changed {
		s.sortIndexLocked()
		_ = s.saveIndexLocked()
	}
}

// readLocked loads the full session with the given id from disk. Legacy
// activities embedded in old session files migrate to the sidecar once.
func (s *Store) readLocked(id string) (*Session, error) {
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("corrupt session %s: %w", id, err)
	}
	if sess.ID == "" {
		sess.ID = id
	}
	// v1.2.8 mode migration: a session file that predates the mode
	// field is labeled DefaultMode ("agent") IN MEMORY. The on-disk
	// bytes stay untouched until the session's next natural save —
	// migration is deterministic, lossless and never duplicates
	// conversation content.
	if sess.Mode == "" {
		sess.Mode = DefaultMode
	}
	// v1.0.10 migration: activities now live in the sidecar. Old files
	// carry them inline — move them out exactly once, then the session
	// JSON stays lean forever after.
	if len(sess.Activities) > 0 {
		legacy := sess.Activities
		if len(s.loadActivitiesLocked(id)) == 0 {
			for _, e := range legacy {
				line, mErr := json.Marshal(e)
				if mErr != nil {
					continue
				}
				f, oErr := os.OpenFile(s.activityPath(id), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
				if oErr != nil {
					break
				}
				_, _ = f.Write(append(line, '\n'))
				_ = f.Close()
			}
		}
		sess.Activities = nil
	}
	// v1.2.4: remember the parsed session in the bounded hot cache keyed
	// by the exact disk identity we just read.
	if fi, statErr := os.Stat(s.path(id)); statErr == nil {
		s.touchHotLocked(id, &hotEntry{sess: &sess, size: fi.Size(), mod: fi.ModTime()})
	}
	return &sess, nil
}

// saveIndexLocked rewrites index.json (compact, atomic).
func (s *Store) saveIndexLocked() error {
	data, err := json.Marshal(s.index)
	if err != nil {
		return err
	}
	return writeAtomic(s.idxPath, data)
}

// sortIndexLocked orders stubs newest-first (UpdatedAt desc, ties broken
// by CreatedAt desc so same-millisecond creates stay deterministic).
func (s *Store) sortIndexLocked() {
	sort.SliceStable(s.index, func(i, j int) bool {
		if s.index[i].UpdatedAt.Equal(s.index[j].UpdatedAt) {
			return s.index[i].CreatedAt.After(s.index[j].CreatedAt)
		}
		return s.index[i].UpdatedAt.After(s.index[j].UpdatedAt)
	})
}

// upsertIndexLocked refreshes (or inserts) the stub for sess. Returns
// whether the index content changed (skips the rewrite when it did not).
func (s *Store) upsertIndexLocked(sess *Session) bool {
	st := stubOf(sess)
	for i, e := range s.index {
		if e.ID == sess.ID {
			if sameStub(e, st) {
				return false
			}
			s.index[i] = st
			s.sortIndexLocked()
			return true
		}
	}
	s.index = append(s.index, st)
	s.sortIndexLocked()
	return true
}

// saveLocked persists the session file (compact JSON, atomic) and updates
// the meta-index. The index rewrite is skipped when the stub did not
// change — appending the 500th message to an unchanged header costs one
// session-file write, not two.
//
// v1.2.4: the file is streamed into the tmp file through a buffered
// json.Encoder instead of marshalling the whole session into one heap
// buffer first (halves the transient allocation on every append), and the
// hot cache entry is refreshed with the exact (size, mtime) identity of
// the freshly written file so the next fetch is a cache hit.
func (s *Store) saveLocked(sess *Session) error {
	if sess.ID == "" {
		return fmt.Errorf("session has no id")
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now().UTC()
	}
	if sess.UpdatedAt.IsZero() {
		sess.UpdatedAt = sess.CreatedAt
	}
	sess.MsgCount = len(sess.Messages)
	path := s.path(sess.ID)
	tmp := path + ".tmp"
	size, err := writeSessionFile(tmp, sess)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if fi, statErr := os.Stat(path); statErr == nil {
		size = fi.Size()
	}
	delete(s.pending, sess.ID)
	if e := s.hot[sess.ID]; e != nil && e.sess == sess {
		// Same object we already cache — refresh identity in place.
		e.size, e.mod = size, modTimeOf(path)
		e.touched = time.Now()
		e.acts = nil // sidecar may have rotated; cheap to re-derive
	} else {
		s.touchHotLocked(sess.ID, &hotEntry{sess: sess, size: size, mod: modTimeOf(path)})
	}
	if s.upsertIndexLocked(sess) {
		return s.saveIndexLocked()
	}
	return nil
}

// writeSessionFile streams sess into path as compact JSON via a buffered
// encoder and returns the written size. Streaming avoids materializing the
// full JSON document (≈2× the file size) in memory per save.
func writeSessionFile(path string, sess *Session) (int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	bw := bufio.NewWriterSize(f, 256<<10)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	err = enc.Encode(sess)
	if flushErr := bw.Flush(); err == nil {
		err = flushErr
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return 0, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func modTimeOf(path string) time.Time {
	if fi, err := os.Stat(path); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

// fetchLocked returns the live session for id: the in-memory copy first
// (pending), then the hot cache, then the file on disk, then a bare stub
// if only the index knows the id (resilient against a lost file).
func (s *Store) fetchLocked(id string) (*Session, error) {
	if id == "" {
		return nil, fmt.Errorf("empty session id")
	}
	if sess, ok := s.pending[id]; ok {
		return sess, nil
	}
	if sess, ok := s.hotGetLocked(id); ok {
		return sess, nil
	}
	if sess, err := s.readLocked(id); err == nil {
		return sess, nil
	}
	for _, st := range s.index {
		if st.ID == id {
			return st, nil
		}
	}
	return nil, fmt.Errorf("session %s not found", id)
}

// --- public API -----------------------------------------------------------

// Save persists a session (creating or replacing its file + index stub).
func (s *Store) Save(sess *Session) error {
	if sess == nil {
		return fmt.Errorf("nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	return s.saveLocked(sess)
}

// Get loads the full session with the given id (messages + activity
// feed). The returned session is a read-isolated copy — callers may mutate
// it freely without affecting the internal cache; persistence flows
// through Save/Append*.
func (s *Store) Get(id string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	sess, err := s.fetchLocked(id)
	if err != nil {
		return nil, err
	}
	out := copySession(sess)
	// Merge the activity sidecar for API completeness (bounded). Served
	// from the per-entry sidecar cache when the file identity is unchanged.
	if out.Activities == nil {
		out.Activities = s.loadActivitiesLocked(id)
	}
	return out, nil
}

// List returns meta-index stubs, newest first. Stub Messages is always
// nil — full histories load through Get/ListFull only.
func (s *Store) List() ([]*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	out := make([]*Session, 0, len(s.index)+len(s.pending))
	out = append(out, s.index...)
	// Fresh, not-yet-persisted sessions stay visible (a "New chat" must
	// survive a sidebar search refresh).
	extra := 0
	for _, p := range s.pending {
		if p != nil && !containsID(s.index, p.ID) {
			out = append(out, stubOf(p))
			extra++
		}
	}
	if extra > 0 {
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		})
	}
	return out, nil
}

func containsID(list []*Session, id string) bool {
	for _, s := range list {
		if s.ID == id {
			return true
		}
	}
	return false
}

// ListFull loads every session with its complete history, newest first.
func (s *Store) ListFull() ([]*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	ids := make([]string, 0, len(s.index)+len(s.pending))
	for _, st := range s.index {
		ids = append(ids, st.ID)
	}
	for id := range s.pending {
		if !containsID(s.index, id) {
			ids = append(ids, id)
		}
	}
	var out []*Session
	for _, id := range ids {
		if sess, err := s.fetchLocked(id); err == nil {
			out = append(out, sess)
		}
	}
	return out, nil
}

// Delete removes a session (file + index entry + pending copy + activity
// sidecar). Deleting an unknown id fails — callers rely on the second
// delete erroring.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()

	found := false
	if _, ok := s.pending[id]; ok {
		delete(s.pending, id)
		found = true
	}
	for i, st := range s.index {
		if st.ID == id {
			s.index = append(s.index[:i], s.index[i+1:]...)
			found = true
			break
		}
	}
	if err := os.Remove(s.path(id)); err == nil {
		found = true
	} else if !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(s.activityPath(id))
	_ = os.Remove(s.summaryPath(id))
	delete(s.hot, id)
	if !found {
		return fmt.Errorf("session %s not found", id)
	}
	return s.saveIndexLocked()
}

// AppendMessage appends one message to the session, persists it, and
// returns the updated session. Concurrent calls are serialized; every
// message survives.
func (s *Store) AppendMessage(id string, msg llm.Message) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	sess, err := s.fetchLocked(id)
	if err != nil {
		return nil, err
	}
	sess.Messages = append(sess.Messages, msg)
	sess.UpdatedAt = time.Now().UTC()
	if err := s.saveLocked(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// AppendActivity appends one activity-feed entry to the session's
// sidecar (bounded) — O(1) in session size, no session rewrite.
func (s *Store) AppendActivity(id string, entry ActivityEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	if _, err := s.fetchLocked(id); err != nil {
		return err // unknown session — same contract as before
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	path := s.activityPath(id)
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Refresh the sidecar cache identity (append-only write).
	if e := s.hot[id]; e != nil {
		if e.acts != nil {
			e.acts = append(e.acts, entry)
		}
		if fi, statErr := os.Stat(path); statErr == nil {
			e.actSize, e.actMod = fi.Size(), fi.ModTime()
		}
	}
	s.rotateActivitiesLocked(id)
	// Keep the index timestamp fresh so the session stays sorted by use.
	for _, st := range s.index {
		if st.ID == id {
			st.UpdatedAt = entry.Timestamp
			break
		}
	}
	return nil
}

// rotateActivitiesLocked trims the sidecar back under BOTH bounds (entry
// count and byte cap) when it outgrows either — keep the newest half — one
// read, one atomic rewrite, amortized over hundreds of appends.
func (s *Store) rotateActivitiesLocked(id string) {
	path := s.activityPath(id)

	fi, err := os.Stat(path)
	if err != nil {
		return
	}

	// The byte-cap check is free; the count check reads the (bounded) file.
	if fi.Size() <= maxActivityKB*1024 {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}

		lines := strings.Count(string(data), "\n")
		if lines <= maxActivities {
			return // within both bounds — nothing to do
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	keep := lines
	if len(lines) > maxActivities {
		keep = lines[len(lines)-maxActivities:]
	}

	out := strings.Join(keep, "\n")
	_ = writeAtomic(path, []byte(strings.TrimPrefix(out, "\n")))
}

// loadActivitiesLocked reads the sidecar (missing file = empty feed).
// Served from the hot entry's validated sidecar cache when unchanged.
func (s *Store) loadActivitiesLocked(id string) []ActivityEntry {
	if e := s.hot[id]; e != nil {
		if fi, err := os.Stat(s.activityPath(id)); err == nil {
			if e.acts != nil && fi.Size() == e.actSize && fi.ModTime().Equal(e.actMod) {
				return e.acts
			}
		} else if e.acts != nil && e.actSize == 0 {
			return nil // no sidecar, cache agrees
		}
	}
	data, err := os.ReadFile(s.activityPath(id))
	if err != nil {
		if e := s.hot[id]; e != nil {
			e.acts, e.actSize, e.actMod = nil, 0, time.Time{}
		}
		return nil
	}
	var out []ActivityEntry
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var e ActivityEntry
		if json.Unmarshal([]byte(ln), &e) == nil {
			out = append(out, e)
		}
	}
	if e := s.hot[id]; e != nil {
		if fi, statErr := os.Stat(s.activityPath(id)); statErr == nil {
			e.acts, e.actSize, e.actMod = out, fi.Size(), fi.ModTime()
		}
	}
	return out
}

func (s *Store) activityPath(id string) string { return filepath.Join(s.dir, id+".activities.jsonl") }

// TrimHot sheds the loaded-session cache (v1.2.4 coordinated cleanup):
// evicts entries until at most keep remain, keeping the MOST RECENTLY used
// ones. Returns an ESTIMATE of bytes reclaimed (the on-disk size identities
// of the dropped sessions). The hot cache is a pure read-through cache —
// dropping entries never loses data; the next Get reloads from disk.
// Registered with the runtime memory manager so idle/pressure cleanups can
// bound resident history while the active session stays warm.
func (s *Store) TrimHot(keep int) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	if keep < 0 {
		keep = 0
	}
	if len(s.hot) <= keep {
		return 0
	}

	type stamp struct {
		id string
		at time.Time
		//nolint:unused // mirrors hotEntry.size for the reclaim estimate
		size int64
	}
	entries := make([]stamp, 0, len(s.hot))
	for id, e := range s.hot {
		entries = append(entries, stamp{id: id, at: e.touched, size: e.size})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].at.After(entries[j].at) })

	var freed int64
	for _, e := range entries[keep:] {
		freed += e.size
		delete(s.hot, e.id)
	}
	return freed
}

// UpdateTitle sets the session title (and persists it).
func (s *Store) UpdateTitle(id, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	sess, err := s.fetchLocked(id)
	if err != nil {
		return err
	}
	sess.Title = title
	sess.UpdatedAt = time.Now().UTC()
	return s.saveLocked(sess)
}

// UpdateContext replaces the session context (system prompt, attached
// files, max iterations) and persists it.
func (s *Store) UpdateContext(id string, ctx Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	sess, err := s.fetchLocked(id)
	if err != nil {
		return err
	}
	sess.Context = ctx
	sess.UpdatedAt = time.Now().UTC()
	return s.saveLocked(sess)
}

// SaveMessagesKeepContext (v1.2.8.1) persists the session's transcript
// mutations (user message, regenerate trim, title) while preserving the
// CURRENT stored context: a whole-object Save built from a caller's stale
// copy would silently revert context mutations (history refs, attachment
// associations, token policy) that landed concurrently. The context is
// fetched and re-attached under the SAME store lock that performs the
// save — the store remains the single serialization point.
func (s *Store) SaveMessagesKeepContext(sess *Session) error {
	if sess == nil {
		return fmt.Errorf("session is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	cur, err := s.fetchLocked(sess.ID)
	if err != nil {
		return err
	}
	merged := copySession(sess)
	merged.Context = cur.Context
	merged.UpdatedAt = time.Now().UTC()
	return s.saveLocked(merged)
}

// UpdateContextFunc (v1.2.8.1) applies fn to the session context UNDER the
// store lock — the single serialization point for every read-modify-write
// on the session context (history refs, attachments, context-token
// policy). The previous API shape (Get a copy → mutate → UpdateContext
// whole-object) let two concurrent mutations silently overwrite each
// other; here the load, mutate and save are one atomic step, so
// concurrent updates merge instead of racing. fn reports whether it
// changed anything — a no-op call never bumps UpdatedAt and never
// rewrites the file.
func (s *Store) UpdateContextFunc(id string, fn func(c *Context) (changed bool, err error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	sess, err := s.fetchLocked(id)
	if err != nil {
		return err
	}
	changed, err := fn(&sess.Context)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	sess.UpdatedAt = time.Now().UTC()
	return s.saveLocked(sess)
}

// ModeOf (v1.2.8.1) returns the session's conversation mode from the
// in-memory index — NO transcript read. It is the server-side authority
// for cross-mode reference enforcement (the client-provided mode is never
// trusted). Unknown or vanished sessions return "" — callers decide
// whether that means "drop" or "skip quietly".
func (s *Store) ModeOf(id string) string {
	if id == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	return s.modeFromIndexLocked(id)
}

// SetModel records the model the session runs with.
func (s *Store) SetModel(id, model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadIndexLocked()
	sess, err := s.fetchLocked(id)
	if err != nil {
		return err
	}
	sess.Model = model
	sess.UpdatedAt = time.Now().UTC()
	return s.saveLocked(sess)
}

// EditLastUserMessage (v1.7.4) replaces the content of the LAST user
// message in place — the single authoritative transcript mutation for the
// Pause/Edit/Resume flow. The store lock serializes it against every other
// session mutation; the message is REPLACED, never duplicated, so a
// resumed run can never produce a double user turn.
func (s *Store) EditLastUserMessage(id, content string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.loadIndexLocked()

	sess, err := s.fetchLocked(id)
	if err != nil {
		return nil, err
	}

	for i := len(sess.Messages) - 1; i >= 0; i-- {
		if sess.Messages[i].Role == "user" {
			sess.Messages[i].Content = content
			sess.UpdatedAt = time.Now().UTC()
			return sess, s.saveLocked(sess)
		}
	}

	return nil, fmt.Errorf("session %s has no user message to edit", id)
}
