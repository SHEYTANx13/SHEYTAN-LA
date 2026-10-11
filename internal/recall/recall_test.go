package recall

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
	"path/filepath"
)

func TestIndexSearchDedup(t *testing.T) {
	dir := t.TempDir()
	e := New(dir)

	if err := e.IndexTurn("s1", "Go", "how to read a file in go", "use os.ReadFile", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.IndexTurn("s1", "Go", "how to read a file in go", "use os.ReadFile", nil); err != nil {
		t.Fatal(err)
	}
	if got := e.Count(); got != 1 {
		t.Fatalf("dedup failed: %d capsules", got)
	}

	hits := e.Search("read file go", 2)
	if len(hits) != 1 || hits[0].SessionID != "s1" {
		t.Fatalf("search miss: %+v", hits)
	}
}

func TestBackfillOnce(t *testing.T) {
	dir := t.TempDir()
	store := sessions.New(filepath.Join(dir, "sessions"))
	s := store.Create()
	s.Title = "recipes"
	s.Messages = []llm.Message{
		{Role: "user", Content: "best pizza dough"},
		{Role: "assistant", Content: "flour water yeast salt, 24h cold rise"},
	}
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}

	e := New(dir)
	if err := e.Backfill(store); err != nil {
		t.Fatal(err)
	}
	if got := e.Count(); got != 1 {
		t.Fatalf("backfill count = %d, want 1", got)
	}
	// Second run is a no-op.
	if err := e.Backfill(store); err != nil {
		t.Fatal(err)
	}
	if got := e.Count(); got != 1 {
		t.Fatalf("backfill ran twice: %d", got)
	}
}

func TestRelevantBlockBounded(t *testing.T) {
	dir := t.TempDir()
	e := New(dir)
	long := strings.Repeat("deep learning gradient descent optimizer adam ", 30)
	if err := e.IndexTurn("s1", "ml", "explain "+long, strings.Repeat("answer ", 100), nil); err != nil {
		t.Fatal(err)
	}
	block := e.RelevantBlock("gradient descent", 1, 120) // tiny budget
	if block != "" && len(block) > 120*8 {               // generous byte ceiling
		t.Errorf("block not bounded: %d bytes", len(block))
	}
	// With a sane budget the capsule appears.
	block = e.RelevantBlock("gradient descent", 1, 600)
	if !strings.Contains(block, "RELEVANT PAST CONTEXT") {
		t.Errorf("block missing header: %q", block)
	}
}

func TestTokenize(t *testing.T) {
	terms := Tokenize("The Quick brown fox, and the lazy DOG!")
	want := []string{"quick", "brown", "fox", "lazy", "dog"}
	if len(terms) != len(want) {
		t.Fatalf("tokenize = %v, want %v", terms, want)
	}
	for i := range want {
		if terms[i] != want[i] {
			t.Errorf("term %d = %s, want %s", i, terms[i], want[i])
		}
	}
}

// --- BM25 recall and feedback steering ---

// searchReference is the pre-Phase-3 scoring path (per-capsule tf map),
// kept here as the behavioral reference.
func (e *Engine) searchReference(query string, k int) []Capsule {
	if k <= 0 {
		k = 4
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.loadLocked()
	e.loadFeedbackLocked()

	now := time.Now()
	qTerms := Tokenize(query)

	N := len(e.capsules)
	if N == 0 {
		return nil
	}

	avgLen := 0.0
	df := map[string]int{}
	for i := range e.capsules {
		terms := Tokenize(e.capsules[i].Query + " " + e.capsules[i].Answer + " " + e.capsules[i].Title)
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

	type scored struct {
		capsule Capsule
		score   float64
	}
	var hits []scored
	for _, c := range e.capsules {
		var score float64
		if len(qTerms) > 0 {
			terms := Tokenize(c.Query + " " + c.Answer + " " + c.Title)
			tf := make(map[string]int, len(terms))
			for _, t := range terms {
				tf[t]++
			}
			dl := float64(len(tf))
			for _, t := range qTerms {
				f, ok := tf[t]
				if !ok {
					continue
				}
				idf := idf(float64(df[t]), float64(N))
				norm := 1.0 - bm25B + bm25B*(dl/avgLen)
				score += idf * (float64(f) * (bm25K1 + 1)) / (float64(f) + bm25K1*norm)
			}
		} else {
			score = 1
		}
		if score <= 0 {
			continue
		}
		ageDays := now.Sub(c.TS).Hours() / 24
		boost := 1.0 + 0.5/(1.0+ageDays/recencyBoostDays)
		if fb := e.feedback[c.ID]; fb > 0 {
			boost *= 1.25
		} else if fb < 0 {
			boost *= 0.6
		}
		hits = append(hits, scored{capsule: c, score: score * boost})
	}
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

func TestSearchMatchesReferenceScoring(t *testing.T) {
	dir := t.TempDir()
	e := New(dir)
	e.loaded = true // inject directly for determinism

	queries := []string{
		"deploy pipeline failure",
		"rust borrow checker",
		"kubernetes context timeout",
		"unicode normalization bug",
		"", // empty query → recency ranking
	}

	seed := []struct{ q, a, title string }{
		{"how to fix the deploy pipeline failure", "rerun the deploy pipeline after the timeout cleared", "deploy"},
		{"rust borrow checker complains about the closure", "the borrow checker was right; restructure the closure", "rust"},
		{"kubernetes context timeout on large clusters", "raised the kubernetes client timeout to 30s", "k8s"},
		{"unicode normalization broke the search index", "applied NFC unicode normalization before indexing", "search"},
		{"deploy pipeline failed again", "the deploy pipeline timeout was a disk space issue", "deploy"},
		{"why does the borrow checker dislike this", "borrow checker dislikes the shared mutable borrow", "rust"},
	}

	for i, s := range seed {
		c := Capsule{
			ID:        fmt.Sprintf("seed%02d", i),
			SessionID: "sess",
			TS:        time.Now().UTC().Add(-time.Duration(i) * time.Hour),
			Title:     s.title,
			Query:     s.q,
			Answer:    s.a,
		}
		e.capsules = append(e.capsules, c)
	}

	for _, q := range queries {
		got := e.Search(q, 3)
		want := e.searchReference(q, 3)

		if len(got) != len(want) {
			t.Fatalf("query %q: got %d hits, reference %d", q, len(got), len(want))
		}
		for i := range got {
			if got[i].ID != want[i].ID {
				t.Fatalf("query %q: hit %d is %s, reference says %s", q, i, got[i].ID, want[i].ID)
			}
		}
	}
}

func TestClearResetsDistinctCache(t *testing.T) {
	dir := t.TempDir()
	e := New(dir)

	if err := e.IndexTurn("s1", "t", "query about pipelines", "answer about pipelines", nil); err != nil {
		t.Fatalf("index: %v", err)
	}

	if got := e.Count(); got != 1 {
		t.Fatalf("count %d", got)
	}

	_ = e.Search("pipelines", 3) // populate terms + distinct caches

	if err := e.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}

	if got := e.Count(); got != 0 {
		t.Fatalf("count after clear: %d", got)
	}

	// Re-indexing and searching after clear must work (caches reset).
	if err := e.IndexTurn("s2", "t", "fresh query", "fresh answer", nil); err != nil {
		t.Fatalf("re-index: %v", err)
	}
	hits := e.Search("fresh", 3)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit after clear+reindex, got %d", len(hits))
	}
}

func TestDistinctCountMatchesTfMapLen(t *testing.T) {
	// distinctFor must equal len(tf-map) — the BM25 document length.
	dir := t.TempDir()
	e := New(dir)
	e.loaded = true

	e.capsules = append(e.capsules,
		Capsule{ID: "a", Query: "alpha alpha beta gamma", Answer: "beta delta", Title: ""},
		Capsule{ID: "b", Query: "single", Answer: "", Title: ""},
	)

	if got, want := e.distinctFor(0), 4; got != want { // alpha, beta, gamma, delta
		t.Fatalf("distinct(0) = %d, want %d", got, want)
	}
	if got, want := e.distinctFor(1), 1; got != want {
		t.Fatalf("distinct(1) = %d, want %d", got, want)
	}
}

func BenchmarkRecallSearch5000(b *testing.B) {
	dir := b.TempDir()
	e := New(filepath.Join(dir, "recall"))
	e.loaded = true

	now := time.Now().UTC()
	for i := 0; i < 5000; i++ {
		e.capsules = append(e.capsules, Capsule{
			ID:        fmt.Sprintf("bench%05d", i),
			SessionID: "bench",
			TS:        now.Add(-time.Duration(i) * time.Minute),
			Title:     "bench",
			Query:     fmt.Sprintf("how do I configure the pipeline stage %d with retries", i%100),
			Answer:    "the pipeline stage was configured with retries and a bounded timeout",
			Tools:     []string{"shell"},
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hits := e.Search("pipeline retries timeout", 4)
		if len(hits) == 0 {
			b.Fatal("no hits")
		}
	}
}
