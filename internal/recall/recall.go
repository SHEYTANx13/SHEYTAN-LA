// Package recall is SHEYTAN's persistent memory over past conversations
// (v1.0.2). The problem it solves: a user chats for weeks; sessions pile up
// on disk; re-feeding old conversations into every prompt would blow both
// the context window and RAM. Instead recall keeps a tiny "digest capsule"
// per exchange (a few hundred bytes) in an append-only index and, on every
// new user turn, retrieves only the most relevant capsules via BM25 and
// injects them as ONE bounded block. Full session files are never loaded for
// retrieval — only when the user (or the agent, via the files tool) opens
// them explicitly.
//
// RAM discipline:
//
//   - the in-memory index holds capsules only (~300 B each; 10,000 exchanges
//     ≈ 3 MB — months of heavy use)
//   - the index is loaded once and appended to in place; no per-turn re-read
//   - no session file is ever opened by the retrieval path
//
// Scoring: BM25 (k1=1.2, b=0.75) over capsule text with a mild recency
// boost, so equally-relevant hits favor what the user did lately.
package recall

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
)

// Capsule is the persistent digest of one completed exchange (user turn +
// assistant answer). It is deliberately small: everything here must stay
// prompt-injectable in bulk.
type Capsule struct {
	ID        string    `json:"id"`        // stable hash (dedup key)
	SessionID string    `json:"sessionId"` // points into sessions/<id>.json
	TS        time.Time `json:"ts"`
	Title     string    `json:"title,omitempty"` // session title for context
	Query     string    `json:"query"`           // user intent (≤ queryLimit chars)
	Answer    string    `json:"answer"`          // assistant conclusion (≤ answerLimit chars)
	Tools     []string  `json:"tools,omitempty"` // tools the agent used
}

const (
	queryLimit  = 160
	answerLimit = 340

	// bm25 parameters
	bm25K1 = 1.2
	bm25B  = 0.75

	// recencyBoostDays is the half-life-ish scale of the recency boost.
	recencyBoostDays = 14.0

	// BackfillMaxSessions bounds the one-time historical import.
	BackfillMaxSessions = 200
)

// Engine is the recall index. Safe for concurrent use.
type Engine struct {
	dir  string // <DataDir>/recall
	path string // <DataDir>/recall/index.jsonl

	// v1.0.6 feedback sidecar: the user's 👍/👎 verdicts on past answers.
	// Kept separate from the append-only capsule index so a verdict can
	// arrive AFTER the capsule was written (it always does) without
	// rewriting index lines. fbPath is <DataDir>/recall/feedback.jsonl.
	fbPath   string
	feedback map[string]int
	fbLoaded bool

	mu       sync.Mutex
	capsules []Capsule
	loaded   bool

	// v1.0.10 corpus cache: BM25 needs document frequencies, average
	// length and per-capsule term frequencies. Before v1.0.10 every
	// Search() re-tokenized EVERY capsule and rebuilt every tf map — the
	// whole corpus, once per user turn. Now term slices are computed once
	// per capsule (lazily, cached in the parallel slice) and the corpus
	// aggregates are cached until an IndexTurn/Clear changes them.
	terms        [][]string // parallel to capsules (nil = not tokenized yet)
	distinct     []int      // parallel to capsules (-1 = not computed yet)
	statsOK      bool       // df/avgLen/N caches valid
	cachedN      int
	cachedAvgLen float64
	cachedDf     map[string]int
}

// New creates (or opens) the recall engine under dataDir.
func New(dataDir string) *Engine {
	dir := filepath.Join(dataDir, "recall")
	_ = os.MkdirAll(dir, 0o755)
	return &Engine{
		dir:      dir,
		path:     filepath.Join(dir, "index.jsonl"),
		fbPath:   filepath.Join(dir, "feedback.jsonl"),
		feedback: map[string]int{},
	}
}

// Dir returns the recall directory (UI/metadata use).
func (e *Engine) Dir() string { return e.dir }

// maxCapsules bounds the in-memory + on-disk index (v1.1.4: the index was
// append-only forever — recall scoring is O(corpus) and old exchanges lose
// value over time). Retention keeps the newest capsules.
const maxCapsules = 5000

// loadLocked reads the whole index into memory exactly once (append-only
// format: subsequent writes only append + mirror in memory).
//
// v1.1.4: when the index exceeds maxCapsules it is compacted to the newest
// maxCapsules lines — unbounded growth made both startup loads and every
// search slower forever.
func (e *Engine) loadLocked() {
	if e.loaded {
		return
	}
	e.loaded = true
	f, err := os.Open(e.path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for sc.Scan() {
		var c Capsule
		if json.Unmarshal(sc.Bytes(), &c) == nil && c.ID != "" {
			e.capsules = append(e.capsules, c)
		}
	}

	if len(e.capsules) > maxCapsules {
		e.capsules = e.capsules[len(e.capsules)-maxCapsules:]
		e.rewriteIndexLocked()
	}
}

// rewriteIndexLocked rewrites index.jsonl from the in-memory capsules
// (atomic tmp+rename).
func (e *Engine) rewriteIndexLocked() {
	tmp := e.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)

	for _, c := range e.capsules {
		if err := enc.Encode(c); err != nil {
			f.Close()
			_ = os.Remove(tmp)
			return
		}
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return
	}

	_ = os.Rename(tmp, e.path)
}

// loadFeedbackLocked reads the feedback sidecar exactly once (later verdicts
// only append + mirror in memory — same discipline as the capsule index).
func (e *Engine) loadFeedbackLocked() {
	if e.fbLoaded {
		return
	}
	e.fbLoaded = true
	if e.feedback == nil {
		e.feedback = map[string]int{}
	}
	f, err := os.Open(e.fbPath)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for sc.Scan() {
		var fb struct {
			ID       string `json:"id"`
			Feedback int    `json:"feedback"`
		}
		if json.Unmarshal(sc.Bytes(), &fb) == nil && fb.ID != "" && fb.Feedback != 0 {
			e.feedback[fb.ID] = normalizeFeedback(fb.Feedback)
		}
	}
}

// normalizeFeedback clamps a verdict to -1/0/+1.
func normalizeFeedback(fb int) int {
	if fb > 0 {
		return 1
	}
	if fb < 0 {
		return -1
	}
	return 0
}

// SetFeedback records the user's verdict (+1 like / -1 dislike / 0 clear)
// for the capsule identified by id. Unknown ids are stored anyway — a
// verdict may land before the capsule finishes indexing (it is the same
// deterministic hash either way).
func (e *Engine) SetFeedback(id string, fb int) error {
	if id == "" {
		return nil
	}
	fb = normalizeFeedback(fb)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.loadFeedbackLocked()
	if e.feedback[id] == fb {
		return nil // already recorded — no duplicate lines
	}
	rec := struct {
		TS       time.Time `json:"ts"`
		ID       string    `json:"id"`
		Feedback int       `json:"feedback"`
	}{TS: time.Now().UTC(), ID: id, Feedback: fb}
	f, err := os.OpenFile(e.fbPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	e.feedback[id] = fb
	return nil
}

// FeedbackFor returns the recorded verdict for a capsule id (0 = none).
func (e *Engine) FeedbackFor(id string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.loadFeedbackLocked()
	return e.feedback[id]
}

// FeedbackStats returns (likes, dislikes) across all recorded verdicts.
func (e *Engine) FeedbackStats() (int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.loadFeedbackLocked()
	var likes, dislikes int
	for _, fb := range e.feedback {
		if fb > 0 {
			likes++
		} else if fb < 0 {
			dislikes++
		}
	}
	return likes, dislikes
}

// CapsuleID returns the deterministic capsule id for a session+query pair —
// the same hash IndexTurn dedupes on, so the UI can tag a verdict onto a
// message long after the exchange was indexed.
func CapsuleID(sessionID, query string) string {
	return capsuleID(sessionID, strings.TrimSpace(clipRunes(query, queryLimit)))
}

// IndexTurn records one completed exchange. Re-indexing the same
// session+query is a no-op (dedup by content hash) so callers never need to
// track what was already stored.
func (e *Engine) IndexTurn(sessionID, title, query, answer string, tools []string) error {
	query = strings.TrimSpace(clipRunes(query, queryLimit))
	answer = strings.TrimSpace(clipRunes(answer, answerLimit))
	if query == "" && answer == "" {
		return nil
	}
	id := capsuleID(sessionID, query)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.loadLocked()
	for _, c := range e.capsules {
		if c.ID == id {
			return nil // already indexed
		}
	}
	c := Capsule{
		ID:        id,
		SessionID: sessionID,
		TS:        time.Now().UTC(),
		Title:     clipRunes(title, 80),
		Query:     query,
		Answer:    answer,
		Tools:     tools,
	}
	f, err := os.OpenFile(e.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(c); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	e.capsules = append(e.capsules, c)
	e.invalidateStatsLocked()
	return nil
}

// Count returns the number of indexed exchanges.
func (e *Engine) Count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.loadLocked()
	return len(e.capsules)
}

// termsFor returns the cached tokenization of capsule i (computing it
// once on first use). The distinct-term count is computed in the same
// pass and cached — BM25's document length is the DISTINCT count, and
// Phase 3 scoring counts query-term occurrences by linear scan over the
// cached terms (zero per-query allocations) instead of rebuilding a tf
// map per capsule per query.
func (e *Engine) termsFor(i int) []string {
	if i < len(e.terms) && e.terms[i] != nil {
		return e.terms[i]
	}
	c := e.capsules[i]
	t := Tokenize(c.Query + " " + c.Answer + " " + c.Title)
	for len(e.terms) < len(e.capsules) {
		e.terms = append(e.terms, nil)
	}
	e.terms[i] = t
	for len(e.distinct) < len(e.capsules) {
		e.distinct = append(e.distinct, -1)
	}
	if i < len(e.distinct) && e.distinct[i] < 0 {
		seen := make(map[string]struct{}, len(t))
		for _, term := range t {
			seen[term] = struct{}{}
		}
		e.distinct[i] = len(seen)
	}
	return t
}

// distinctFor returns the cached distinct-term count of capsule i.
func (e *Engine) distinctFor(i int) int {
	_ = e.termsFor(i) // ensures the parallel distinct cache is filled
	if i < len(e.distinct) {
		return e.distinct[i]
	}
	return 0
}

// countTerm returns how many times term occurs in the cached term slice
// (allocation-free; slices are short — capsule digests are a few hundred
// bytes — so the linear scan beats a per-query map rebuild).
func countTerm(terms []string, term string) int {
	n := 0
	for _, t := range terms {
		if t == term {
			n++
		}
	}
	return n
}

// invalidateStatsLocked drops the corpus aggregates after an index change.
func (e *Engine) invalidateStatsLocked() {
	e.statsOK = false
	e.cachedDf = nil
}

// statsLocked returns (N, avgLen, df) with caching. avgLen counts RAW
// terms (duplicates included) — matching the pre-cache semantics exactly.
func (e *Engine) statsLocked() (int, float64, map[string]int) {
	N := len(e.capsules)
	if e.statsOK && e.cachedN == N {
		return N, e.cachedAvgLen, e.cachedDf
	}
	avgLen := 0.0
	df := map[string]int{}
	for i := range e.capsules {
		terms := e.termsFor(i)
		tf := make(map[string]int, len(terms))
		for _, t := range terms {
			tf[t]++
		}
		avgLen += float64(len(terms))
		for t := range tf {
			df[t]++
		}
	}
	if N > 0 {
		avgLen /= float64(N)
	}
	if avgLen == 0 {
		avgLen = 1
	}
	e.statsOK = true
	e.cachedN = N
	e.cachedAvgLen = avgLen
	e.cachedDf = df
	return N, avgLen, df
}

// Search returns the k most relevant capsules for query (BM25 + recency
// boost). Empty query returns the k most recent.
func (e *Engine) Search(query string, k int) []Capsule {
	if k <= 0 {
		k = 4
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.loadLocked()
	e.loadFeedbackLocked()

	now := time.Now()
	qTerms := Tokenize(query)

	// Corpus statistics over capsules (v1.0.10: cached across searches).
	N, avgLen, df := e.statsLocked()
	if N == 0 {
		return nil
	}

	type scored struct {
		capsule Capsule
		score   float64
	}
	var hits []scored
	for i, c := range e.capsules {
		var score float64
		if len(qTerms) > 0 {
			// v1.1.5 Phase 3: count occurrences directly against the
			// cached term slice — no tf map per capsule per query. The
			// scoring math is unchanged (f values, distinct-count dl and
			// raw-length avgLen are all identical to the pre-Phase-3 path).
			terms := e.termsFor(i)
			dl := float64(e.distinctFor(i))
			for _, t := range qTerms {
				f := countTerm(terms, t)
				if f == 0 {
					continue
				}
				idf := idf(float64(df[t]), float64(N))
				norm := 1.0 - bm25B + bm25B*(dl/avgLen)
				score += idf * (float64(f) * (bm25K1 + 1)) / (float64(f) + bm25K1*norm)
			}
		} else {
			score = 1 // empty query: recency-only ranking
		}
		if score <= 0 {
			continue
		}
		// Recency boost: up to +50% for capsules from the last two weeks,
		// decaying afterwards. Recall should prefer fresh context without
		// ever hiding an older exact hit (BM25 dominates on real matches).
		ageDays := now.Sub(c.TS).Hours() / 24
		boost := 1.0 + 0.5/(1.0+ageDays/recencyBoostDays)
		// v1.0.6 feedback steering: answers the user LIKED rank
		// higher (+25%), answers they DISLIKED sink (-40%) — the
		// recall engine learns the user's taste from the 👍/👎
		// buttons without ever growing the prompt.
		if fb := e.feedback[c.ID]; fb > 0 {
			boost *= 1.25
		} else if fb < 0 {
			boost *= 0.6
		}
		hits = append(hits, scored{capsule: c, score: score * boost})
	}
	// Partial selection sort for top-k (k is tiny; full sort is pointless).
	if len(hits) > k {
		for i := 0; i < k; i++ {
			best := i
			for j := i + 1; j < len(hits); j++ {
				if hits[j].score > hits[best].score {
					best = j
				}
			}
			hits[i], hits[best] = hits[best], hits[i]
		}
		hits = hits[:k]
	}
	out := make([]Capsule, len(hits))
	for i, h := range hits {
		out[i] = h.capsule
	}
	return out
}

func idf(df, n float64) float64 {
	// BM25+ style: never negative.
	v := (n - df + 0.5) / (df + 0.5)
	if v < 0 {
		v = 0.0001
	}
	return 1 + math.Log2(v+1.0)
}

// RelevantBlock formats the top-k capsules for `query` into a compact
// prompt block bounded by maxTokens. Empty string when nothing relevant.
func (e *Engine) RelevantBlock(query string, k, maxTokens int) string {
	if k <= 0 {
		k = 4
	}
	hits := e.Search(query, k)
	if len(hits) == 0 {
		return ""
	}
	if maxTokens <= 0 {
		maxTokens = 600
	}
	var b strings.Builder
	b.WriteString("## RELEVANT PAST CONTEXT (auto-recalled from earlier conversations — most relevant first)\n\n")
	used := chunking.EstimateTokens(b.String())
	for i, c := range hits {
		entry := formatCapsule(c, i+1)
		cost := chunking.EstimateTokens(entry)
		if used+cost > maxTokens {
			break
		}
		b.WriteString(entry)
		used += cost
	}
	out := b.String()
	if strings.Count(out, "\n") <= 2 {
		return "" // nothing actually fit
	}
	return out
}

func formatCapsule(c Capsule, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%d] %s", n, c.TS.Format("2006-01-02"))
	if c.Title != "" {
		fmt.Fprintf(&b, " · %s", c.Title)
	}
	b.WriteString("\n")
	if c.Query != "" {
		fmt.Fprintf(&b, "    user asked: %s\n", c.Query)
	}
	if c.Answer != "" {
		fmt.Fprintf(&b, "    outcome: %s\n", c.Answer)
	}
	if len(c.Tools) > 0 {
		fmt.Fprintf(&b, "    tools used: %s\n", strings.Join(c.Tools, ", "))
	}
	b.WriteString("\n")
	return b.String()
}

// Backfill imports existing sessions into the index ONCE (guarded by a
// marker file) so recall works immediately for users upgrading from earlier
// versions. It is meant to run on a background goroutine at startup.
func (e *Engine) Backfill(store *sessions.Store) error {
	marker := filepath.Join(e.dir, "backfilled")
	if _, err := os.Stat(marker); err == nil {
		return nil // already done
	}
	list, err := store.ListFull()
	if err != nil {
		return err
	}
	if len(list) > BackfillMaxSessions {
		list = list[:BackfillMaxSessions]
	}
	for _, sess := range list {
		var query, answer string
		var tools []string
		for _, m := range sess.Messages {
			switch m.Role {
			case "user":
				if query != "" && answer != "" {
					// flush the previous pair before starting a new one
					_ = e.IndexTurn(sess.ID, sess.Title, query, answer, tools)
					answer = ""
					tools = nil
				}
				query = m.Content
			case "assistant":
				if m.Content != "" {
					answer = m.Content
				}
			case "tool":
				// tool messages carry the tool name
				if m.Name != "" {
					tools = append(tools, m.Name)
				}
			}
		}
		if query != "" && answer != "" {
			_ = e.IndexTurn(sess.ID, sess.Title, query, answer, tools)
		}
	}
	return os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644)
}

// Clear wipes the index and the backfill marker (memory view "clear").
func (e *Engine) Clear() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.capsules = nil
	e.terms = nil
	e.distinct = nil
	e.invalidateStatsLocked()
	e.loaded = true
	_ = os.Remove(filepath.Join(e.dir, "backfilled"))
	return os.WriteFile(e.path, []byte{}, 0o644)
}

// Stats describes the index for the UI.
type Stats struct {
	Capsules       int       `json:"capsules"`
	Oldest         time.Time `json:"oldest"`
	Newest         time.Time `json:"newest"`
	IndexSizeBytes int64     `json:"indexSizeBytes"`
}

// Stats returns index statistics.
func (e *Engine) Stats() Stats {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.loadLocked()
	st := Stats{Capsules: len(e.capsules)}
	if fi, err := os.Stat(e.path); err == nil {
		st.IndexSizeBytes = fi.Size()
	}
	for _, c := range e.capsules {
		if st.Oldest.IsZero() || c.TS.Before(st.Oldest) {
			st.Oldest = c.TS
		}
		if c.TS.After(st.Newest) {
			st.Newest = c.TS
		}
	}
	return st
}

// Tokenize lowercases and splits text into alphanumeric terms, dropping
// stopwords and 1-2 char fragments. ASCII-fast with UTF-8 passthrough.
func Tokenize(s string) []string {
	s = strings.ToLower(s)
	terms := strings.FieldsFunc(s, func(r rune) bool {
		return !unicodeLetterDigit(r)
	})
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		if len(t) < 3 || stopwords[t] {
			continue
		}
		out = append(out, t)
	}
	return out
}

func unicodeLetterDigit(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= 0x80:
		return r > 0 && r != ' ' // treat non-ASCII runes as term chars
	}
	return false
}

// stopwords is a compact English stopword list (search-only; generation is
// unaffected). Kept local to keep the tokenizer dependency-free.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true,
	"not": true, "you": true, "all": true, "can": true, "her": true,
	"was": true, "one": true, "our": true, "out": true, "day": true,
	"get": true, "has": true, "him": true, "his": true, "how": true,
	"its": true, "new": true, "now": true, "old": true, "see": true,
	"two": true, "way": true, "who": true, "did": true, "yes": true,
	"with": true, "this": true, "that": true, "what": true, "when": true,
	"where": true, "from": true, "have": true, "they": true, "will": true,
	"your": true, "into": true, "than": true, "then": true, "them": true,
	"there": true, "these": true, "those": true, "about": true, "after": true,
	"again": true, "been": true, "before": true, "being": true, "could": true,
	"does": true, "done": true, "each": true, "just": true, "like": true,
	"make": true, "many": true, "more": true, "most": true, "much": true,
	"must": true, "need": true, "over": true, "some": true, "such": true,
	"should": true, "would": true, "please": true, "tell": true, "want": true,
}

// capsuleID builds the dedup key: session + normalized query.
func capsuleID(sessionID, query string) string {
	h := sha1.Sum([]byte(sessionID + "\x1f" + strings.ToLower(strings.TrimSpace(query))))
	return hex.EncodeToString(h[:])[:16]
}

// clipRunes truncates s to at most n runes on a word boundary.
func clipRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	out := string(r[:n])
	// back off to the last space so words are not cut mid-way
	if i := strings.LastIndexByte(out, ' '); i > n/2 {
		out = out[:i]
	}
	return out + "…"
}

// LatestSessionID is a helper for callers that only need the most recent
// session pointer (uses the store's meta index — no full loads).
func LatestSessionID(store *sessions.Store) string {
	list, err := store.List()
	if err != nil || len(list) == 0 {
		return ""
	}
	return list[0].ID
}
