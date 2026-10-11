package repoindex

// search.go — bounded hybrid repository search over the index.
//
// Supported match dimensions (combinable, all optional):
//
//      path      — substring match on the slash-relative path / basename
//      symbol    — exact, prefix and substring match on declared symbols
//      language  — filter (go, typescript, ...)
//      role      — filter (source, test, config, doc, asset)
//      text      — keyword match against path + symbol names + imports
//                  (bounded lexical matching over indexed metadata — the
//                  index never stores file content)
//      depsOf    — dependency expansion: files X depends on
//      usedBy    — reverse dependency expansion: files that depend on X
//      testsOf   — deterministic test/source relationship lookup
//      task      — free task text; keywords are extracted and scored like
//                  text, with source/test role weighting
//
// Ranking is DETERMINISTIC: score desc, then path asc. Every result
// carries an Evidence string that says exactly WHY it matched and
// whether the evidence is structural (import/test facts) or inferred
// (keyword/path relevance).

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Score weights — fixed, documented, deterministic. No embeddings: the
// roadmap explicitly defers them until measured evidence justifies the
// dependency, and structural evidence already ranks precisely.
const (
	scoreSymbolExact    = 5.0
	scoreSymbolPrefix   = 3.0
	scoreSymbolSubstr   = 2.0
	scoreBaseNameExact  = 4.0
	scoreBaseNameSubstr = 2.5
	scorePathSubstr     = 1.5
	scoreKeywordHit     = 1.2
	scoreDepOutgoing    = 3.0
	scoreDepIncoming    = 3.0
	scoreTestLink       = 2.5
	scoreRoleMatch      = 0.8
	scoreLanguageMatch  = 0.5
	scoreRecentChange   = 0.4
	scoreGitModified    = 0.3
)

const (
	// maxResultsDefault / maxResults bound the result page.
	maxResultsDefault = 12
	maxResults        = 50

	// maxTaskKeywords bounds the keywords extracted from a task text.
	maxTaskKeywords = 12

	// maxEvidenceSymbols bounds the symbols listed in one result.
	maxEvidenceSymbols = 6
)

// Query is the hybrid search request. All fields optional; at least
// one dimension must be non-empty.
type Query struct {
	Text     string `json:"text,omitempty"`
	Symbol   string `json:"symbol,omitempty"`
	Path     string `json:"path,omitempty"`
	Language string `json:"language,omitempty"`
	Role     string `json:"role,omitempty"`
	Task     string `json:"task,omitempty"`
	DepsOf   string `json:"depsOf,omitempty"`
	UsedBy   string `json:"usedBy,omitempty"`
	TestsOf  string `json:"testsOf,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// Empty reports whether the query carries no dimension at all.
func (q Query) Empty() bool {
	return q.Text == "" && q.Symbol == "" && q.Path == "" && q.Language == "" &&
		q.Role == "" && q.Task == "" && q.DepsOf == "" && q.UsedBy == "" && q.TestsOf == ""
}

// Result is one bounded, evidence-carrying search hit.
type Result struct {
	Path     string   `json:"path"`
	Language string   `json:"language,omitempty"`
	Role     string   `json:"role,omitempty"`
	Score    float64  `json:"score"`
	Evidence string   `json:"evidence"`
	Symbols  []string `json:"symbols,omitempty"`
	Line     int      `json:"line,omitempty"`
	GitState string   `json:"gitState,omitempty"`
	Recent   bool     `json:"recent,omitempty"`
}

// SearchReport is the bounded search response.
type SearchReport struct {
	Root       string   `json:"root"`
	TotalHits  int      `json:"totalHits"`
	Returned   int      `json:"returned"`
	Truncated  bool     `json:"truncated"`
	Results    []Result `json:"results"`
	IndexAge   string   `json:"indexAge,omitempty"`
	DurationMs int64    `json:"durationMs"`
}

// Search runs the hybrid query against the index for root. The index
// is refreshed first when older than stalenessWindow (bounded
// incremental update — search results are always against a fresh-ish
// index, never a full rescan per request).
func (s *Store) Search(ctx context.Context, root string, q Query) (SearchReport, error) {
	started := time.Now()

	if q.Empty() {
		return SearchReport{}, fmt.Errorf("repoindex: empty query (provide text, symbol, path, language, depsOf, usedBy, testsOf or task)")
	}

	// Bounded freshness: refresh when older than the window (or when
	// another root is requested). Failures keep the stale index —
	// search never hard-fails on refresh problems.
	if st := s.Status(root); st.State == "empty" || st.State == "stale" || st.StaleRoot {
		upCtx, cancel := context.WithTimeout(ctx, searchTimeBudget)
		_, _ = s.Update(upCtx, root)
		cancel()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.loadLocked(root)
	if idx == nil {
		return SearchReport{}, fmt.Errorf("repoindex: no index for %s (update failed)", root)
	}

	limit := q.Limit
	if limit <= 0 {
		limit = maxResultsDefault
	}
	if limit > maxResults {
		limit = maxResults
	}

	results := executeQuery(idx, q, limit)

	report := SearchReport{
		Root:       root,
		TotalHits:  len(results),
		Returned:   len(results),
		Results:    results,
		IndexAge:   idx.UpdatedAt.UTC().Format(time.RFC3339),
		DurationMs: time.Since(started).Milliseconds(),
	}
	if report.TotalHits > limit {
		report.Returned = limit
		report.Truncated = true
		report.Results = results[:limit]
	}
	return report, nil
}

// executeQuery scores every record against the query dimensions.
// Deterministic: score desc, then path asc.
func executeQuery(idx *Index, q Query, limit int) []Result {
	if idx.byPath == nil {
		idx.rebuild()
	}

	taskKeywords := ExtractKeywords(q.Task)
	textKeywords := ExtractKeywords(q.Text)
	langLower := strings.ToLower(strings.TrimSpace(q.Language))
	roleLower := strings.ToLower(strings.TrimSpace(q.Role))

	// Dependency-dimension target records (resolved once, up front).
	depsOfPath := sNormalizePath(q.DepsOf)
	usedByPath := sNormalizePath(q.UsedBy)
	testsOfPath := sNormalizePath(q.TestsOf)

	depsOfRec := idx.record(depsOfPath)
	usedByRec := idx.record(usedByPath)
	testsOfRec := idx.record(testsOfPath)

	depsOfSet := map[string]bool{}
	usedBySet := map[string]bool{}
	testsOfSet := map[string]bool{}
	if depsOfRec != nil {
		for _, dep := range depsOfRec.Deps {
			depsOfSet[dep.Path] = true
		}
	}
	if usedByRec != nil {
		for _, importer := range importedByOf(idx, usedByRec.Path) {
			usedBySet[importer] = true
		}
	}
	if testsOfRec != nil {
		for _, t := range testsOfRec.Tests {
			testsOfSet[t] = true
		}
	}

	type scored struct {
		rec      *FileRecord
		score    float64
		evidence []string
		syms     []Symbol
	}

	var matches []scored

	for _, rec := range idx.Files {
		var ev []string
		var syms []Symbol
		score := 0.0

		// Filters (hard): language / role.
		if langLower != "" && rec.Language != langLower {
			continue
		}
		if roleLower != "" && rec.Role != roleLower {
			continue
		}

		// Dependency-dimension membership (hard membership, soft score).
		if depsOfPath != "" {
			if depsOfRec == nil {
				return nil // unresolvable target — honest empty result
			}
			if depsOfSet[rec.Path] {
				score += scoreDepOutgoing
				ev = append(ev, fmt.Sprintf("dependency of %s", depsOfRec.Path))
			}
		}
		if usedByPath != "" {
			if usedByRec == nil {
				return nil
			}
			if usedBySet[rec.Path] {
				score += scoreDepIncoming
				ev = append(ev, fmt.Sprintf("imported by %s", usedByRec.Path))
			}
		}
		if testsOfPath != "" {
			if testsOfRec == nil {
				return nil
			}
			if testsOfSet[rec.Path] {
				score += scoreTestLink
				if testsOfRec.Role == "test" {
					ev = append(ev, fmt.Sprintf("covers %s (test relationship)", testsOfRec.Path))
				} else {
					ev = append(ev, fmt.Sprintf("test file of %s", testsOfRec.Path))
				}
			}
		}

		// Path dimension (soft).
		if q.Path != "" {
			pathLower := strings.ToLower(rec.Path)
			needle := strings.ToLower(sNormalizePath(q.Path))
			baseLower := pathLower
			if slash := strings.LastIndex(pathLower, "/"); slash >= 0 {
				baseLower = pathLower[slash+1:]
			}
			switch {
			case baseLower == needle:
				score += scoreBaseNameExact
				ev = append(ev, "exact basename match")
			case strings.Contains(baseLower, needle):
				score += scoreBaseNameSubstr
				ev = append(ev, fmt.Sprintf("basename contains %q", q.Path))
			case strings.Contains(pathLower, needle):
				score += scorePathSubstr
				ev = append(ev, fmt.Sprintf("path contains %q", q.Path))
			}
		}

		// Symbol dimension (soft, strongest structural-adjacent signal).
		if q.Symbol != "" {
			symNeedle := strings.ToLower(q.Symbol)
			best := 0.0
			for _, sym := range rec.Symbols {
				nameLower := strings.ToLower(sym.Name)
				var s float64
				switch {
				case nameLower == symNeedle:
					s = scoreSymbolExact
				case strings.HasPrefix(nameLower, symNeedle):
					s = scoreSymbolPrefix
				case strings.Contains(nameLower, symNeedle):
					s = scoreSymbolSubstr
				}
				if s > best {
					best = s
					syms = []Symbol{sym}
				} else if s > 0 && s == best && len(syms) < maxEvidenceSymbols {
					syms = append(syms, sym)
				}
			}
			if best > 0 {
				score += best
				ev = append(ev, fmt.Sprintf("symbol match %q", q.Symbol))
			}
		}

		// Text/keyword dimension (soft; lexical over indexed metadata).
		// Weighted per keyword: a symbol hit outranks a path hit,
		// which outranks an import/package hit — evidence strength,
		// not just presence/absence.
		for _, kw := range textKeywords {
			if w := keywordWeight(rec, kw); w > 0 {
				score += w
				if len(ev) < 8 {
					ev = append(ev, fmt.Sprintf("keyword %q", kw))
				}
			}
		}

		// Task dimension (soft; the context-planner entry point).
		for _, kw := range taskKeywords {
			if w := keywordWeight(rec, kw); w > 0 {
				score += w
				if len(ev) < 8 {
					ev = append(ev, fmt.Sprintf("task keyword %q", kw))
				}
			}
		}

		// Role weighting for task-style queries: implementation words
		// prefer source, test words prefer tests (small, honest bias).
		if q.Task != "" {
			taskLower := strings.ToLower(q.Task)
			if strings.Contains(taskLower, "test") && rec.Role == "test" {
				score += scoreRoleMatch
			}
		}

		// Language match bonus when combined with other dimensions.
		if langLower != "" && score > 0 {
			score += scoreLanguageMatch
		}

		// Relevance metadata (recent change / git modification) —
		// applied ONLY to records that already matched a real
		// dimension, so an all-recency no-match index can never
		// surface every file for an unrelated query.
		if score <= 0 {
			continue
		}
		if rec.Recent {
			score += scoreRecentChange
		}
		if rec.GitState == "modified" {
			score += scoreGitModified
		}

		if score > 0 {
			matches = append(matches, scored{rec: rec, score: score, evidence: ev, syms: syms})
		}
	}

	// Deterministic order: score desc, path asc.
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].rec.Path < matches[j].rec.Path
	})

	out := make([]Result, 0, len(matches))
	for _, m := range matches {
		res := Result{
			Path:     m.rec.Path,
			Language: m.rec.Language,
			Role:     m.rec.Role,
			Score:    round2(m.score),
			Evidence: strings.Join(m.evidence, "; "),
			GitState: m.rec.GitState,
			Recent:   m.rec.Recent,
		}
		for _, sym := range m.syms {
			res.Symbols = append(res.Symbols, sym.Name)
		}
		if len(m.syms) > 0 {
			res.Line = m.syms[0].Line
		}
		if res.Evidence == "" {
			res.Evidence = "indexed (no stronger evidence)"
		}
		out = append(out, res)
	}
	return out
}

// keywordWeight scores one keyword's strongest evidence on the record:
// symbol name hits weigh most, then path, then import/package. 0 = no
// match. Deterministic and bounded — the index never stores file
// content, so every hit is over indexed metadata only.
func keywordWeight(rec *FileRecord, kw string) float64 {
	if kw == "" {
		return 0
	}

	for _, sym := range rec.Symbols {
		if strings.Contains(strings.ToLower(sym.Name), kw) {
			return scoreKeywordHit
		}
	}
	if strings.Contains(strings.ToLower(rec.Path), kw) {
		return scoreKeywordHit * 0.75
	}
	if rec.Package != "" && strings.Contains(strings.ToLower(rec.Package), kw) {
		return scoreKeywordHit * 0.5
	}
	for _, imp := range rec.Imports {
		if strings.Contains(strings.ToLower(imp), kw) {
			return scoreKeywordHit * 0.5
		}
	}
	return 0
}

// stopWords is the bounded task-keyword stop list.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "of": true,
	"to": true, "in": true, "on": true, "for": true, "with": true, "is": true,
	"are": true, "be": true, "it": true, "this": true, "that": true, "at": true,
	"by": true, "from": true, "as": true, "into": true, "about": true,
	"please": true, "can": true, "should": true, "would": true, "fix": true,
	"make": true, "use": true, "using": true, "add": true, "need": true,
	"want": true, "my": true, "our": true, "me": true, "i": true, "you": true,
}

// ExtractKeywords tokenizes free text into bounded lowercase keywords
// (letters/digits/underscore/dot/dash words, stopwords removed,
// shortest length 3, capped at maxTaskKeywords). Deterministic order:
// first occurrence.
func ExtractKeywords(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	var out []string
	seen := map[string]bool{}

	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		w := strings.ToLower(cur.String())
		cur.Reset()
		if len(w) < 3 || stopWords[w] || seen[w] {
			return
		}
		seen[w] = true
		out = append(out, w)
	}

	for _, r := range text {
		isWord := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-'
		if isWord {
			cur.WriteRune(r)
		} else {
			flush()
		}
		if len(out) >= maxTaskKeywords {
			return out
		}
	}
	flush()
	return out
}

// sNormalizePath normalizes a user-supplied path reference to the
// index's slash-relative form: absolute paths within root are
// relativized, backslashes become slashes, ".." segments are rejected
// (path safety: a query can never probe outside the workspace).
func sNormalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")

	// Absolute paths are relativized against the root by the caller
	// when needed (Search has no root here — records only ever match
	// relative forms, so absolute paths simply do not match, which is
	// the safe default).
	p = pathCleanSlashed(p)
	return p
}

// pathCleanSlashed is a slash-path cleaner that rejects traversal.
func pathCleanSlashed(p string) string {
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	p = strings.TrimPrefix(p, "/")
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return strings.TrimSuffix(p, "/")
}

// round2 rounds a score to two decimals (stable JSON output).
func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
