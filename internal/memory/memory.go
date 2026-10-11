package memory

import (
	"bufio"
	"context"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Package memory is the agent's persistent memory layer.
//
// Memory is deliberately separated by class and provenance so that untrusted
// external material cannot silently become an authoritative user fact.
//
// M1 = user facts
// M2 = preferences
// M3 = project state
// M4 = decisions
// M5 = procedures / learned fixes
// M6 = conversation summaries
// M7 = observations / untrusted or provisional knowledge

const (
	ClassM1 = "M1"
	ClassM2 = "M2"
	ClassM3 = "M3"
	ClassM4 = "M4"
	ClassM5 = "M5"
	ClassM6 = "M6"
	ClassM7 = "M7"
)

type TrustLevel string

const (
	TrustUnknown     TrustLevel = "unknown"
	TrustUntrusted   TrustLevel = "untrusted"
	TrustProvisional TrustLevel = "provisional"
	TrustTrusted     TrustLevel = "trusted"
	TrustVerified    TrustLevel = "verified"
)

type Provenance struct {
	Kind        string    `json:"kind,omitempty"`
	Source      string    `json:"source,omitempty"`
	URI         string    `json:"uri,omitempty"`
	Reference   string    `json:"reference,omitempty"`
	ObservedAt  time.Time `json:"observedAt,omitempty"`
	CollectedBy string    `json:"collectedBy,omitempty"`
}

type Entry struct {
	ID            string     `json:"id"`
	Class         string     `json:"class,omitempty"`
	Tags          []string   `json:"tags"`
	Content       string     `json:"content"`
	Source        string     `json:"source,omitempty"` // legacy/session source
	CreatedAt     time.Time  `json:"createdAt"`
	Trust         TrustLevel `json:"trust,omitempty"`
	Provenance    Provenance `json:"provenance,omitempty"`
	Quarantined   bool       `json:"quarantined,omitempty"`
	Authoritative bool       `json:"authoritative,omitempty"`
	Score         float64    `json:"-"` // search-only
}

type Store struct {
	path string
	mu   sync.Mutex

	// v1.1.4: parsed-entry cache keyed by file (size, mtime). Every read
	// previously re-opened and re-parsed the whole JSONL file — the memory
	// tool calls Search on every use, so long stores made each tool call
	// O(file). Appends invalidate by bumping the observed stat.
	//
	// v1.1.5 Phase 3: the store is APPEND-AWARE. AppendEntry/DeleteByID/
	// Clear now update the parsed cache incrementally (and refresh the
	// observed stat), so the remember→recall cycle no longer re-parses the
	// whole file after every write. fullParses/incrementalAppends are
	// measured counters (ParseStats) proving which path served each read.
	cache             []Entry
	cacheKey          cacheKey
	fullParses        uint64
	incrementalAddes  uint64
	duplicatesSkipped uint64 // v1.2.4: exact-duplicate writes suppressed
}

// ParseStats reports the measured cache behavior of this store:
// how many times the whole JSONL file was re-parsed versus how many
// writes were folded into the cache incrementally.
func (s *Store) ParseStats() (fullParses, incrementalAppends uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fullParses, s.incrementalAddes
}

type cacheKey struct {
	size int64
	mod  time.Time
}

func New(path string) *Store {
	dir := filepath.Dir(path)

	if dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}

	return &Store{
		path: path,
	}
}

// NormalizeEntry applies the memory trust boundary.
//
// The key invariants are:
//
//   - M1 is reserved for explicit trusted/verified user facts.
//   - External web/research/GitHub/Reddit material can never remain in a
//     higher-trust memory class simply because the caller labeled it so.
//   - External material is downgraded to M7 and quarantined.
//   - Quarantined material is never authoritative.
//
// Such data can still be explicitly inspected, but ordinary recall excludes it.
func NormalizeEntry(e Entry) Entry {
	e.ID = strings.TrimSpace(e.ID)

	if e.ID == "" {
		e.ID = uniqueID()
	}

	e.Class = normalizeClass(e.Class)
	e.Trust = normalizeTrust(e.Trust)

	e.Content = strings.TrimSpace(e.Content)
	e.Source = strings.TrimSpace(e.Source)

	e.Provenance.Kind = strings.ToLower(
		strings.TrimSpace(e.Provenance.Kind),
	)
	e.Provenance.Source = strings.TrimSpace(
		e.Provenance.Source,
	)
	e.Provenance.URI = strings.TrimSpace(
		e.Provenance.URI,
	)
	e.Provenance.Reference = strings.TrimSpace(
		e.Provenance.Reference,
	)
	e.Provenance.CollectedBy = strings.TrimSpace(
		e.Provenance.CollectedBy,
	)

	for i := range e.Tags {
		e.Tags[i] = strings.TrimSpace(e.Tags[i])
	}

	var tags []string

	for _, tag := range e.Tags {
		if tag != "" {
			tags = append(tags, tag)
		}
	}

	e.Tags = tags

	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	} else {
		e.CreatedAt = e.CreatedAt.UTC()
	}

	if e.Provenance.ObservedAt.IsZero() {
		e.Provenance.ObservedAt = e.CreatedAt
	} else {
		e.Provenance.ObservedAt = e.Provenance.ObservedAt.UTC()
	}

	// External material is never allowed to retain a durable trusted class.
	// This check intentionally runs before the M1 rule so a caller cannot
	// bypass quarantine by labeling external material as M2-M6.
	if isExternalProvenance(e.Provenance) {
		e.Class = ClassM7
		e.Quarantined = true
		e.Authoritative = false
		e.Trust = TrustProvisional
	}

	// M1 is reserved for explicit trusted/verified user facts.
	if e.Class == ClassM1 {
		if !isTrustedUserFact(e) {
			e.Class = ClassM7
			e.Quarantined = true
			e.Authoritative = false

			if e.Trust == TrustTrusted ||
				e.Trust == TrustVerified {
				e.Trust = TrustProvisional
			}
		} else {
			e.Authoritative = true
		}
	}

	// Authority is granted exclusively to explicit trusted/verified user
	// facts regardless of memory class; everything else is non-authoritative.
	e.Authoritative = isTrustedUserFact(e)

	// Quarantine always wins over authority.
	if e.Quarantined {
		e.Authoritative = false
	}

	return e
}

func isTrustedUserFact(e Entry) bool {
	if strings.ToLower(
		strings.TrimSpace(e.Provenance.Kind),
	) != "user" {
		return false
	}

	return e.Trust == TrustTrusted ||
		e.Trust == TrustVerified
}

func isExternalProvenance(p Provenance) bool {
	switch strings.ToLower(strings.TrimSpace(p.Kind)) {
	case "web", "research", "github", "reddit":
		return true
	}

	switch strings.ToLower(strings.TrimSpace(p.Source)) {
	case "web", "research", "github", "reddit":
		return true
	}

	return false
}

func normalizeClass(class string) string {
	class = strings.ToUpper(strings.TrimSpace(class))

	switch class {
	case ClassM1, ClassM2, ClassM3, ClassM4,
		ClassM5, ClassM6, ClassM7:
		return class
	default:
		return ClassM7
	}
}

func normalizeTrust(trust TrustLevel) TrustLevel {
	switch TrustLevel(
		strings.ToLower(
			strings.TrimSpace(string(trust)),
		),
	) {
	case TrustUnknown:
		return TrustUnknown
	case TrustUntrusted:
		return TrustUntrusted
	case TrustProvisional:
		return TrustProvisional
	case TrustTrusted:
		return TrustTrusted
	case TrustVerified:
		return TrustVerified
	default:
		return TrustUnknown
	}
}

func (s *Store) Append(
	tags []string,
	content,
	source string,
) error {
	return s.AppendEntry(Entry{
		Tags:    tags,
		Content: content,
		Source:  source,
		Class:   ClassM7,
		Trust:   TrustProvisional,
		Provenance: Provenance{
			Kind:   "agent",
			Source: source,
		},
	})
}

func (s *Store) AppendEntry(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry = NormalizeEntry(entry)

	if entry.Content == "" {
		return fmt.Errorf("memory content is required")
	}

	f, err := os.OpenFile(
		s.path,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o644,
	)
	if err != nil {
		return err
	}

	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(entry); err != nil {
		return err
	}

	// Phase 3: fold the new entry into the parsed cache instead of letting
	// the next Search re-parse the whole file. Only valid when the cache is
	// already warm (never loaded → the next read parses everything once,
	// which includes this entry).
	if s.cache != nil {
		s.cache = append(s.cache, entry)
		s.incrementalAddes++
		s.refreshCacheKeyLocked()
	}

	return nil
}

// AppendEntryUnique appends one entry UNLESS an exact duplicate (same
// memory class, same normalized content, case-insensitive) already exists.
// v1.2.4: repeated identical facts used to grow memory.jsonl forever and,
// with it, the in-memory parsed cache; exact-duplicate suppression keeps
// the store lean without touching user data semantics — the FIRST copy of
// a fact is authoritative, later identical writes are counted and skipped.
// added=false signals a suppressed duplicate (never an error).
func (s *Store) AppendEntryUnique(entry Entry) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry = NormalizeEntry(entry)

	if entry.Content == "" {
		return false, fmt.Errorf("memory content is required")
	}

	// Make sure the dedup check covers the full history: a cold cache must
	// be warmed once before comparing (cachedLocked stat-validates).
	if _, ok := s.cachedLocked(); !ok {
		if _, err := s.allLocked(); err != nil {
			return false, err
		}
	}
	for i := range s.cache {
		if s.cache[i].Class == entry.Class && strings.EqualFold(s.cache[i].Content, entry.Content) {
			s.duplicatesSkipped++
			return false, nil
		}
	}

	f, err := os.OpenFile(
		s.path,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o644,
	)
	if err != nil {
		return false, err
	}

	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(entry); err != nil {
		return false, err
	}

	// Phase 3: fold the new entry into the parsed cache instead of letting
	// the next Search re-parse the whole file. Only valid when the cache is
	// already warm (never loaded → the next read parses everything once,
	// which includes this entry).
	if s.cache != nil {
		s.cache = append(s.cache, entry)
		s.incrementalAddes++
		s.refreshCacheKeyLocked()
	}

	return true, nil
}

// DuplicatesSkipped reports how many exact-duplicate memory writes were
// suppressed (v1.2.4 telemetry for bytes avoided by deduplication).
func (s *Store) DuplicatesSkipped() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.duplicatesSkipped
}

// refreshCacheKeyLocked re-observes the file stat after a write so the
// cache key matches the post-write file (caller holds mu).
func (s *Store) refreshCacheKeyLocked() {
	if fi, err := os.Stat(s.path); err == nil {
		s.cacheKey = cacheKey{size: fi.Size(), mod: fi.ModTime()}
	}
}

func (s *Store) All() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entries, ok := s.cachedLocked(); ok {
		out := make([]Entry, len(entries))
		copy(out, entries)
		return out, nil
	}

	return s.allLocked()
}

// cachedLocked returns the cached entries when the backing file is
// byte-identical to the cache key (same size + mtime).
func (s *Store) cachedLocked() ([]Entry, bool) {
	fi, err := os.Stat(s.path)
	if err != nil || fi.Size() != s.cacheKey.size || !fi.ModTime().Equal(s.cacheKey.mod) {
		return nil, false
	}
	return s.cache, true
}

func (s *Store) allLocked() ([]Entry, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.cache, s.cacheKey = nil, cacheKey{}
			return nil, nil
		}

		return nil, err
	}

	defer f.Close()

	var out []Entry

	sc := bufio.NewScanner(f)
	sc.Buffer(
		make([]byte, 0, 64*1024),
		4*1024*1024,
	)

	for sc.Scan() {
		var e Entry

		if err := json.Unmarshal(
			sc.Bytes(),
			&e,
		); err != nil {
			continue
		}

		e = NormalizeEntry(e)
		out = append(out, e)
	}

	if err := sc.Err(); err != nil {
		return nil, err
	}

	// An empty store still WARMS the cache: a non-nil (empty) slice marks
	// the cache as live so Phase 3 appends fold in instead of leaving the
	// store permanently cold.
	if out == nil {
		out = []Entry{}
	}

	// refresh the cache key from the post-read stat
	if fi, err := f.Stat(); err == nil {
		s.cache, s.cacheKey = out, cacheKey{size: fi.Size(), mod: fi.ModTime()}
		s.fullParses++
	}

	return out, nil
}

func (s *Store) Search(
	query string,
	limit int,
) ([]Entry, error) {
	return s.SearchWithOptions(
		query,
		limit,
		false,
	)
}

// SearchWithOptions searches memory while keeping quarantined entries hidden
// by default.
//
// Quarantined material can still be inspected explicitly, but it must never
// silently participate in ordinary recall.
func (s *Store) SearchWithOptions(
	query string,
	limit int,
	includeQuarantined bool,
) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 3: score the CACHED entries read-only instead of copying the
	// whole store per search. Scores live in a parallel slice so cached
	// entries are never mutated; only matching entries are copied out.
	entries, ok := s.cachedLocked()
	if !ok {
		var err error
		entries, err = s.allLocked()
		if err != nil {
			return nil, err
		}
	}

	q := strings.ToLower(
		strings.TrimSpace(query),
	)

	// Empty query: recency order (newest first), bounded by limit.
	if q == "" {
		recent := make([]Entry, 0, len(entries))

		for _, e := range entries {
			if !includeQuarantined && e.Quarantined {
				continue
			}

			recent = append(recent, e)
		}

		sort.Slice(
			recent,
			func(i, j int) bool {
				return recent[i].CreatedAt.After(
					recent[j].CreatedAt,
				)
			},
		)

		if limit > 0 &&
			len(recent) > limit {
			recent = recent[:limit]
		}

		return recent, nil
	}

	// scoreIdx/scoreVal are parallel to the filtered candidate set: the
	// cache slice itself is never written.
	type candidate struct {
		idx   int
		score float64
	}

	var candidates []candidate

	for i, e := range entries {
		if !includeQuarantined &&
			e.Quarantined {
			continue
		}

		score := 0.0
		matched := false

		for _, tag := range e.Tags {
			if strings.Contains(
				strings.ToLower(tag),
				q,
			) {
				score += 2.0
				matched = true
			}
		}

		if strings.Contains(
			strings.ToLower(e.Content),
			q,
		) {
			score += 1.0
			matched = true
		}

		if !matched {
			// Entries that do not match the query must never enter the
			// result set, regardless of their trust level.
			continue
		}

		// Trusted/verified memory is stronger recall material, but this
		// does not turn it into external authority for the rest of the agent.
		switch e.Trust {
		case TrustVerified:
			score += 0.50
		case TrustTrusted:
			score += 0.25
		case TrustProvisional:
			score += 0.05
		}

		candidates = append(candidates, candidate{idx: i, score: score})
	}

	hits := make([]Entry, 0, len(candidates))

	for _, c := range candidates {
		e := entries[c.idx]
		e.Score = c.score // copy — the cached entry keeps its zero Score
		hits = append(hits, e)
	}

	sort.SliceStable(
		hits,
		func(i, j int) bool {
			if hits[i].Score == hits[j].Score {
				return hits[i].CreatedAt.After(
					hits[j].CreatedAt,
				)
			}

			return hits[i].Score > hits[j].Score
		},
	)

	if limit > 0 &&
		len(hits) > limit {
		hits = hits[:limit]
	}

	return hits, nil
}

func (s *Store) DeleteByID(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 3: serve the rewrite from the warm parsed cache when possible —
	// the previous code re-parsed the whole file on every delete.
	entries, ok := s.cachedLocked()
	if !ok {
		var err error
		entries, err = s.allLocked()
		if err != nil {
			return err
		}
	}

	kept := make([]Entry, 0, len(entries))

	for _, e := range entries {
		if e.ID != id {
			kept = append(kept, e)
		}
	}

	tmp := s.path + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)

	for _, e := range kept {
		if err := enc.Encode(e); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)

			return err
		}
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}

	// Phase 3: the rewrite IS the new state — cache it instead of forcing
	// the next Search to re-parse the file. kept entries are already
	// normalized (they came from allLocked).
	if kept == nil {
		kept = []Entry{}
	}
	s.cache = kept
	s.incrementalAddes++
	s.refreshCacheKeyLocked()

	return nil
}

func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.WriteFile(
		s.path,
		[]byte{},
		0o644,
	); err != nil {
		return err
	}

	// Phase 3: the empty file is the new state — cache it.
	s.cache = []Entry{}
	s.refreshCacheKeyLocked()

	return nil
}

func (s *Store) Count() int {
	entries, _ := s.All()
	return len(entries)
}

// Tool wraps Store as an agent.Tool.
//
// Ordinary recall excludes quarantined/untrusted material.
// The result intentionally labels provenance and trust so recalled memory
// remains evidence rather than silently becoming authority.
type Tool struct {
	Store *Store

	// RecallSearch optionally searches past-conversation digests.
	RecallSearch func(query string, k int) []string
}

func (t Tool) Name() string {
	return "memory"
}

func (Tool) Description() string {
	return "Persistent memory with trust-aware M1-M7 classes. action=recall|remember|list|forget|clear|history"
}

// ShortDescription is the one-line UI label (v1.1.7).
func (Tool) ShortDescription() string {
	return "Store and retrieve approved knowledge."
}

func (Tool) Parameters() any {
	return struct {
		Action         string     `json:"action"`
		Query          string     `json:"query,omitempty"`
		Tags           []string   `json:"tags,omitempty"`
		Content        string     `json:"content,omitempty"`
		ID             string     `json:"id,omitempty"`
		Limit          int        `json:"limit,omitempty"`
		Class          string     `json:"class,omitempty"`
		Trust          TrustLevel `json:"trust,omitempty"`
		ProvenanceKind string     `json:"provenanceKind,omitempty"`
		ProvenanceSrc  string     `json:"provenanceSource,omitempty"`
		URI            string     `json:"uri,omitempty"`
		Reference      string     `json:"reference,omitempty"`
		Quarantine     bool       `json:"quarantine,omitempty"`
	}{}
}

func (t Tool) Run(
	ctx context.Context,
	args json.RawMessage,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var p struct {
		Action         string     `json:"action"`
		Query          string     `json:"query"`
		Tags           []string   `json:"tags"`
		Content        string     `json:"content"`
		ID             string     `json:"id"`
		Limit          int        `json:"limit"`
		Class          string     `json:"class"`
		Trust          TrustLevel `json:"trust"`
		ProvenanceKind string     `json:"provenanceKind"`
		ProvenanceSrc  string     `json:"provenanceSource"`
		URI            string     `json:"uri"`
		Reference      string     `json:"reference"`
		Quarantine     bool       `json:"quarantine"`
	}

	if err := json.Unmarshal(
		args,
		&p,
	); err != nil {
		var probe map[string]json.RawMessage

		if json.Unmarshal(
			args,
			&probe,
		) == nil {
			if rawTags, ok := probe["tags"]; ok &&
				len(rawTags) > 0 &&
				rawTags[0] == '"' {
				return "", fmt.Errorf(
					`bad args: "tags" must be a JSON ARRAY of strings, e.g. {"tags":["project","fact"]} — not a plain string`,
				)
			}
		}

		return "", fmt.Errorf(
			"bad args: %w",
			err,
		)
	}

	if p.Limit == 0 {
		p.Limit = 10
	}

	switch strings.ToLower(
		strings.TrimSpace(p.Action),
	) {
	case "recall", "search":
		hits, err := t.Store.Search(
			p.Query,
			p.Limit,
		)
		if err != nil {
			return "", err
		}

		if len(hits) == 0 {
			return "No matching memories found.", nil
		}

		var b strings.Builder

		for i, e := range hits {
			fmt.Fprintf(
				&b,
				"[%d] %s class=%s trust=%s authoritative=%t source=%s provenance=%s quarantined=%t\n    %s\n\n",
				i+1,
				e.ID,
				e.Class,
				e.Trust,
				e.Authoritative,
				e.Source,
				e.Provenance.Kind,
				e.Quarantined,
				truncate(e.Content, 240),
			)
		}

		return b.String(), nil

	case "history", "past":
		if t.RecallSearch == nil {
			return "Past-conversation search is not available in this build.", nil
		}

		if strings.TrimSpace(p.Query) == "" {
			return "", fmt.Errorf(
				"query is required for history search",
			)
		}

		lines := t.RecallSearch(
			p.Query,
			p.Limit,
		)

		if len(lines) == 0 {
			return "No matching past conversations found.", nil
		}

		return strings.Join(
			lines,
			"\n",
		), nil

	case "remember", "save", "add":
		if strings.TrimSpace(
			p.Content,
		) == "" {
			return "", fmt.Errorf(
				"content is required to remember",
			)
		}

		entry := Entry{
			Tags:    p.Tags,
			Content: p.Content,
			Class:   normalizeClass(p.Class),
			Trust:   normalizeTrust(p.Trust),
			Source:  "agent",
			Provenance: Provenance{
				Kind:        strings.TrimSpace(p.ProvenanceKind),
				Source:      strings.TrimSpace(p.ProvenanceSrc),
				URI:         strings.TrimSpace(p.URI),
				Reference:   strings.TrimSpace(p.Reference),
				CollectedBy: "SHEYTAN-Local-Agent",
			},
			Quarantined: p.Quarantine,
		}

		if entry.Class == "" {
			entry.Class = ClassM7
		}

		if entry.Trust == "" {
			entry.Trust = TrustProvisional
		}

		// Normalize before persistence and before reporting the result so
		// the returned ID/class/trust exactly match what was stored.
		entry = NormalizeEntry(entry)

		// v1.2.4: the remember path suppresses EXACT duplicates (same
		// class + same normalized content) so repeated model writes of
		// an already-known fact no longer grow memory.jsonl and the
		// parsed cache forever. Honest feedback: the model is told the
		// fact was already known.
		added, err := t.Store.AppendEntryUnique(entry)
		if err != nil {
			return "", err
		}
		if !added {
			return fmt.Sprintf(
				"already known — an identical %s memory exists (duplicate write skipped)",
				entry.Class,
			), nil
		}

		return fmt.Sprintf(
			"remembered as %s (%s, trust=%s, quarantined=%t)",
			entry.Class,
			entry.ID,
			entry.Trust,
			entry.Quarantined,
		), nil

	case "list":
		all, err := t.Store.All()
		if err != nil {
			return "", err
		}

		var b strings.Builder

		for _, e := range all {
			fmt.Fprintf(
				&b,
				"%s class=%s trust=%s authoritative=%t quarantined=%t tags=%v %s\n",
				e.ID,
				e.Class,
				e.Trust,
				e.Authoritative,
				e.Quarantined,
				e.Tags,
				truncate(e.Content, 100),
			)
		}

		return b.String(), nil

	case "forget", "delete":
		if strings.TrimSpace(p.ID) == "" {
			return "", fmt.Errorf(
				"id is required to forget",
			)
		}

		return fmt.Sprintf(
			"forgotten %s",
			p.ID,
		), t.Store.DeleteByID(p.ID)

	case "clear":
		return "cleared", t.Store.Clear()

	default:
		return "", fmt.Errorf(
			"unknown action %q",
			p.Action,
		)
	}
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}

	return s[:n] + "..."
}

var idSeq atomic.Uint64

func uniqueID() string {
	var rnd [4]byte

	if _, err := crand.Read(rnd[:]); err != nil {
		return time.Now().UTC().Format(
			"20060102150405",
		) +
			fmt.Sprintf(
				"-%09d",
				idSeq.Add(1),
			)
	}

	return time.Now().UTC().Format(
		"20060102150405",
	) +
		fmt.Sprintf(
			"-%06d-%08x",
			idSeq.Add(1)%1_000_000,
			rnd,
		)
}
