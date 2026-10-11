// toolcache.go — v1.2.5 tool I/O efficiency.
//
// Two caches remove measured waste from the tool path:
//
//   - specCache: every tool's serialized JSON schema and token estimate
//     is marshaled ONCE per registry generation, not once per turn
//     (v1.2.4 marshaled all ~20 specs on every RunDetailed — twice,
//     once to measure and once to build the request).
//
//   - resultCache: successful results of DETERMINISTIC tool calls are
//     reused when the model repeats an identical call — bounded by
//     bytes and entries, LRU-evicted, and honest about what it stores.
//     Non-deterministic tools (shell, codeExec, git, browser, screenshot,
//     memory) are never cached: their results depend on the world's state.
//
// Neither cache ever fabricates data: a miss simply re-executes.
package agent

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// specCacheEntry is one cached serialized tool spec. gen is the registry
// generation the entry was built from: a cache entry is only ever SERVED to
// a reader whose generation matches, so an entry a superseded reader writes
// back after a Register/Invalidate can never leak the replaced tool's
// metadata into a later build (v1.7.6 — the last name-keyed staleness
// window).
type specCacheEntry struct {
	json   string
	tokens int
	gen    uint64
}

// specCache memoizes tool-schema serialization per registry generation.
type specCache struct {
	mu      sync.RWMutex
	entries map[string]specCacheEntry
}

func newSpecCache() *specCache {
	return &specCache{entries: map[string]specCacheEntry{}}
}

// Invalidate drops every cached spec (called on Register so a tool whose
// description changed is re-serialized exactly once).
func (c *specCache) Invalidate() {
	c.mu.Lock()
	c.entries = map[string]specCacheEntry{}
	c.mu.Unlock()
}

// Spec returns the serialized spec and token estimate for one tool,
// marshaling at most once PER REGISTRY GENERATION. The caller passes the
// generation its tool references came from (Orchestrator.ToolsGeneration /
// ToolsAt). A hit is served only when the cached entry carries the SAME
// generation: metadata built inside one operation is therefore stable for
// that operation's lifetime even while the registry churns underneath.
func (c *specCache) Spec(t Tool, gen uint64) (string, int) {
	name := t.Name()

	c.mu.RLock()
	e, ok := c.entries[name]
	c.mu.RUnlock()

	if ok && e.gen == gen {
		return e.json, e.tokens
	}

	spec := llm.ToolSpec{}
	spec.Type = "function"
	spec.Function.Name = t.Name()
	spec.Function.Description = t.Description()
	spec.Function.Parameters = t.Parameters()

	data, err := json.Marshal(spec)
	if err != nil {
		data = []byte("{}")
	}

	entry := specCacheEntry{
		json:   string(data),
		tokens: chunking.EstimateTokens(string(data)),
		gen:    gen,
	}

	c.mu.Lock()
	c.entries[name] = entry
	c.mu.Unlock()

	return entry.json, entry.tokens
}

// BuildSpecsAssembles turns a tool-name selection into (specs, tokens)
// using the cache. Callers that need the raw JSON can use Spec directly.
// gen must be the registry generation the tools were resolved from.
func (c *specCache) BuildSpecs(tools []Tool, gen uint64) ([]llm.ToolSpec, int) {
	specs := make([]llm.ToolSpec, 0, len(tools))
	total := 0

	for _, t := range tools {
		data, tokens := c.Spec(t, gen)

		var spec llm.ToolSpec
		if err := json.Unmarshal([]byte(data), &spec); err != nil {
			// fall back to direct assembly — never drop a tool
			spec = llm.ToolSpec{}
			spec.Type = "function"
			spec.Function.Name = t.Name()
			spec.Function.Description = t.Description()
			spec.Function.Parameters = t.Parameters()
		}

		specs = append(specs, spec)
		total += tokens
	}

	return specs, total
}

// resultCache is a bounded LRU of deterministic tool results.
type resultCache struct {
	mu       sync.Mutex
	entries  map[string]string
	order    []string // LRU: least-recent first
	bytes    int
	maxBytes int
	maxEntry int

	hits   int
	misses int
}

func newResultCache(maxBytes, maxEntries int) *resultCache {
	if maxBytes <= 0 {
		maxBytes = 1 << 20 // 1 MiB default
	}

	if maxEntries <= 0 {
		maxEntries = 64
	}

	return &resultCache{
		entries:  map[string]string{},
		order:    make([]string, 0, maxEntries),
		maxBytes: maxBytes,
		maxEntry: maxEntries,
	}
}

// cacheableTools lists tools whose identical successful calls are safe to
// reuse within a session. Everything else (shell, codeExec, git, browser,
// screenshot, memory, lab, research, pipeline, scheduler, …) is skipped —
// its results depend on mutable world state.
var cacheableTools = map[string]bool{
	"files": true, // read/list/stat actions (mutations bypass the cache)
	"diff":  true,
	"json":  true,
}

// cacheableCall reports whether one call may use the result cache.
func cacheableCall(tool, args string) bool {
	if !cacheableTools[tool] {
		return false
	}

	if tool == "files" {
		// Only read-shaped actions are deterministic. Anything that
		// writes, moves or deletes is NEVER served from cache.
		lower := lowerASCII(args)
		for _, act := range []string{`"write"`, `"move"`, `"delete"`, `"mkdir"`, `"remove"`, `"rename"`, `"copy"`} {
			if containsLower(lower, act) {
				return false
			}
		}
	}

	return true
}

func lowerASCII(s string) string {
	out := []byte(s)
	for i, b := range out {
		if b >= 'A' && b <= 'Z' {
			out[i] = b + 32
		}
	}
	return string(out)
}

// normalizeArgsForCache produces the cache key argument form: whitespace
// collapsed, case preserved (JSON argument semantics).
func normalizeArgsForCache(args string) string {
	fields := strings.Fields(args)
	return strings.Join(fields, " ")
}

// idempotentNetworkTool reports whether one failed call may be retried
// once: only stateless network reads whose re-execution cannot duplicate
// side effects.
func idempotentNetworkTool(name string) bool {
	switch name {
	case "fetch", "webSearch", "research":
		return true
	default:
		return false
	}
}

func containsLower(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Get returns a cached result when present (and promotes it).
func (c *resultCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	v, ok := c.entries[key]
	if !ok {
		c.misses++
		return "", false
	}

	c.hits++
	c.promote(key)

	return v, true
}

// Put stores a successful result under the byte/entry bounds.
func (c *resultCache) Put(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(value) > c.maxBytes {
		return // a single result larger than the whole cache is not stored
	}

	// Replace or append.
	if _, ok := c.entries[key]; ok {
		c.bytes -= len(c.entries[key])
		c.entries[key] = value
		c.bytes += len(value)
		c.promote(key)
		c.evictLocked()
		return
	}

	c.entries[key] = value
	c.order = append(c.order, key)
	c.bytes += len(value)
	c.evictLocked()
}

func (c *resultCache) promote(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			return
		}
	}
}

func (c *resultCache) evictLocked() {
	for (len(c.order) > c.maxEntry || c.bytes > c.maxBytes) && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		c.bytes -= len(c.entries[oldest])
		delete(c.entries, oldest)
	}

	if c.bytes < 0 {
		c.bytes = 0
	}
}

// Stats reports the measured cache counters.
func (c *resultCache) Stats() (hits, misses, entries, bytes int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.hits, c.misses, len(c.entries), c.bytes
}

// globalResultCache is the orchestrator-level result cache (bounded).
var globalResultCache = newResultCache(1<<20, 64)

// ToolResultCacheStats exposes the measured cache counters for /api/perf.
func ToolResultCacheStats() (hits, misses, entries, bytes int) {
	return globalResultCache.Stats()
}
