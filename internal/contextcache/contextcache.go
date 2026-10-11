// Package contextcache is SHEYTAN's content-aware cache for expensive
// context processing: file extraction, normalization, chunking, chunk
// metadata, retrieval results, and deterministic context transformations.
//
// Design rules (v1.1.3):
//
//   - Keys are composed by callers from CONTENT identity — never from
//     paths alone. The same path with different bytes must miss; the same
//     bytes under different paths must hit.
//   - Every key embeds a processing version and the relevant configuration
//     fingerprint, so upgrading the chunker or changing a budget naturally
//     invalidates old entries (version invalidation).
//   - Growth is bounded by both entry count and total byte size; eviction
//     is least-recently-used. Hit/miss/eviction counters expose behavior.
//   - The cache is safe for concurrent use.
//
// The cache stores derived, reproducible data only. Nothing here is the
// source of truth: dropping the whole cache must never lose information,
// only recomputation time.
package contextcache

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"time"
)

// Version is the global processing version baked into every key the
// helpers build. Bump it when the processing pipeline changes shape so
// stale entries can never be served to new code.
//
// v4 (v1.1.5 Phase 3): chunk derivation moved to the shared chunking
// engine (chunk metadata + byte-range addressing changed shape) and the
// cache gained single-flight coalescing plus an oversized-entry guard.
const Version = 4

// entry is one cached value.
type entry struct {
	key       string
	value     any
	bytes     int64
	expiresAt time.Time // zero = no expiry
}

// Cache is a bounded LRU cache with hit/miss statistics.
type Cache struct {
	mu sync.Mutex

	maxEntries    int
	maxBytes      int64
	maxEntryBytes int64 // 0 = unlimited (bound is maxBytes)

	ll    *list.List               // front = most recent
	items map[string]*list.Element // key -> element holding *entry

	hits      uint64
	misses    uint64
	evictions uint64
	inserts   uint64 // successful Put of a NEW key (updates don't count)
	oversized uint64 // Put rejected: single entry above maxEntryBytes
	coalesced uint64 // GetOrCompute waiters that joined an in-flight compute

	bytes int64

	// inflight tracks duplicate-computation coalescing. The map holds a
	// *inflightCall only while a compute is running OUTSIDE mu — waiters
	// block on the call, never on the cache mutex, so expensive work never
	// serializes behind the lock.
	inflight map[string]*inflightCall
}

// inflightCall is one in-flight GetOrCompute computation. value/panicked
// are written before wg.Done() and read only after wg.Wait(), which gives
// joiners a happens-before guarantee without touching the cache mutex.
type inflightCall struct {
	wg       sync.WaitGroup
	value    any
	panicked any // propagated panic value, if the compute panicked
}

// runCompute runs compute(), converting a panic into a shared value so the
// in-flight slot can always be released and joiners never deadlock.
func runCompute[T any](compute func() T) (value T, panicked any) {
	defer func() {
		if r := recover(); r != nil {
			var zero T
			value, panicked = zero, r
		}
	}()
	return compute(), nil
}

// Option configures a Cache at construction time.
type Option func(*Cache)

// WithMaxEntries caps the number of live entries (default 512).
func WithMaxEntries(n int) Option {
	return func(c *Cache) {
		if n > 0 {
			c.maxEntries = n
		}
	}
}

// WithMaxBytes caps the total size of cached values (default 256 MiB).
func WithMaxBytes(n int64) Option {
	return func(c *Cache) {
		if n > 0 {
			c.maxBytes = n
		}
	}
}

// WithMaxEntryBytes rejects any single Put whose tracked size exceeds n.
// Without it a value larger than maxBytes would still be stored (the
// eviction loop keeps at least one entry), letting one oversized value pin
// the cache above its byte bound forever. Default: 0 = unlimited (the
// total-bytes bound applies).
func WithMaxEntryBytes(n int64) Option {
	return func(c *Cache) {
		if n > 0 {
			c.maxEntryBytes = n
		}
	}
}

// New returns a Cache with the given options.
func New(opts ...Option) *Cache {
	c := &Cache{
		maxEntries: 512,
		maxBytes:   256 << 20,
		ll:         list.New(),
		items:      make(map[string]*list.Element),
		inflight:   make(map[string]*inflightCall),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Key composes a stable cache key from ordered parts. Parts are joined
// with a separator that cannot appear in hex hashes; callers should pass
// hashes/versions/typed values, never raw user text.
func Key(parts ...string) string {
	return strings.Join(parts, "\x1f")
}

// ContentHash returns the hex SHA-256 of data — the canonical content
// identity used in keys. Same bytes → same hash, whatever the path.
func ContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ConfigFingerprint hashes a compact configuration description into the
// key space so a budget/parameter change invalidates derived entries.
func ConfigFingerprint(parts ...string) string {
	h := fnv.New64a()

	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))
}

// Get returns the cached value for key and reports whether it hit.
// Expired entries are treated as misses and dropped.
func (c *Cache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		c.misses++
		return nil, false
	}

	e := el.Value.(*entry)

	if !e.expiresAt.IsZero() && time.Now().After(e.expiresAt) {
		c.removeElement(el)
		c.misses++
		return nil, false
	}

	c.ll.MoveToFront(el)
	c.hits++
	return e.value, true
}

// Put stores value under key with an optional TTL (ttl <= 0 = no expiry).
// The size hint lets the bounds track heterogeneous values; callers that
// cannot estimate cheaply may pass 0 (then only the entry count applies).
// Returns false when the entry was REJECTED — today that only happens when
// the tracked size exceeds the max-entry bound. Callers may ignore the
// result; rejection is also visible in Stats.OversizedRejected.
func (c *Cache) Put(key string, value any, sizeHint int64, ttl time.Duration) bool {
	if sizeHint < 0 {
		sizeHint = 0
	}

	var expiresAt time.Time

	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Oversized-entry guard: a single entry above the per-entry bound is
	// refused outright instead of being stored and then permanently pinning
	// the cache above its byte bound (eviction always keeps one entry).
	if c.maxEntryBytes > 0 && sizeHint > c.maxEntryBytes {
		c.oversized++
		return false
	}

	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry)
		c.bytes -= e.bytes
		e.value = value
		e.bytes = sizeHint
		e.expiresAt = expiresAt
		c.bytes += sizeHint
		c.ll.MoveToFront(el)
		c.evictLocked()
		return true
	}

	e := &entry{
		key:       key,
		value:     value,
		bytes:     sizeHint,
		expiresAt: expiresAt,
	}

	c.items[key] = c.ll.PushFront(e)
	c.bytes += sizeHint
	c.inserts++
	c.evictLocked()
	return true
}

// GetOrCompute returns the cached value for key, computing it exactly once
// even when several goroutines race on the same key (single-flight
// coalescing). The FIRST caller runs compute OUTSIDE the cache mutex —
// expensive work never blocks other cache users — and the result is stored
// and shared with concurrent callers of the same key. Coalesced joins are
// counted in Stats.Coalesced.
//
// sizeHint measures the computed value for the byte bound (may be nil = 0).
// compute must be deterministic, must not call back into the same cache key
// (it runs without the lock, so re-entrance on OTHER keys is safe), and its
// result must be reproducible from inputs alone — the cache is never a
// source of truth.
//
// The T-typed form keeps call sites allocation-light: callers no longer
// need a separate Get type assertion round-trip.
func GetOrCompute[T any](
	c *Cache,
	key string,
	ttl time.Duration,
	sizeHint func(T) int64,
	compute func() T,
) (T, bool) {
	// Fast path: hit under one short lock.
	if v, ok := c.Get(key); ok {
		if tv, ok := v.(T); ok {
			return tv, true
		}
		// Wrong type means a different pipeline generation used the key —
		// treat as a miss and recompute (version invalidation handles the
		// common case; this is belt-and-braces).
		c.Invalidate(key)
	}

	// Claim the in-flight slot for this key.
	c.mu.Lock()

	if call, ok := c.inflight[key]; ok {
		// Someone is already computing: join them instead of duplicating
		// the work. The WaitGroup guarantees the result is visible.
		c.coalesced++
		c.mu.Unlock()

		call.wg.Wait()

		if call.panicked != nil {
			panic(call.panicked) // propagate the compute failure to joiners
		}

		if tv, ok := call.value.(T); ok {
			return tv, true
		}

		var zero T
		return zero, false
	}

	call := &inflightCall{}
	call.wg.Add(1)
	c.inflight[key] = call
	c.mu.Unlock()

	// Compute with NO lock held. Every path below releases the in-flight
	// slot before returning so joiners can never deadlock.
	value, panicked := runCompute(compute)

	var size int64
	if panicked == nil && sizeHint != nil {
		size = sizeHint(value)
		if size < 0 {
			size = 0
		}
	}

	c.mu.Lock()
	if panicked == nil {
		c.putLocked(key, value, size, ttl)
	}
	delete(c.inflight, key)
	c.mu.Unlock()

	if panicked != nil {
		call.panicked = panicked
		call.wg.Done()
		panic(panicked) // owner re-panics like a normal panic
	}

	call.value = value
	call.wg.Done()

	return value, true
}

// putLocked is Put without locking; caller holds mu.
func (c *Cache) putLocked(key string, value any, sizeHint int64, ttl time.Duration) bool {
	if sizeHint < 0 {
		sizeHint = 0
	}

	var expiresAt time.Time

	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	if c.maxEntryBytes > 0 && sizeHint > c.maxEntryBytes {
		c.oversized++
		return false
	}

	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry)
		c.bytes -= e.bytes
		e.value = value
		e.bytes = sizeHint
		e.expiresAt = expiresAt
		c.bytes += sizeHint
		c.ll.MoveToFront(el)
		c.evictLocked()
		return true
	}

	e := &entry{
		key:       key,
		value:     value,
		bytes:     sizeHint,
		expiresAt: expiresAt,
	}

	c.items[key] = c.ll.PushFront(e)
	c.bytes += sizeHint
	c.inserts++
	c.evictLocked()
	return true
}

// Invalidate drops one key. Returns whether it existed.
func (c *Cache) Invalidate(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		return false
	}

	c.removeElement(el)
	return true
}

// InvalidatePrefix drops every key that starts with the given prefix —
// e.g. all chunks derived from one source, or everything produced by an
// old processing version. Returns the number of dropped entries.
func (c *Cache) InvalidatePrefix(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	var doomed []*list.Element

	for key, el := range c.items {
		if strings.HasPrefix(key, prefix) {
			doomed = append(doomed, el)
		}
	}

	for _, el := range doomed {
		c.removeElement(el)
	}

	return len(doomed)
}

// Clear drops every entry (corruption recovery: callers that suspect bad
// derived data simply wipe the cache — all contents are reproducible).
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ll.Init()
	c.items = make(map[string]*list.Element)
	c.bytes = 0
}

// Stats is a point-in-time snapshot of cache behavior.
type Stats struct {
	Entries   int     `json:"entries"`
	Bytes     int64   `json:"bytes"`
	MaxBytes  int64   `json:"maxBytes"`
	Hits      uint64  `json:"hits"`
	Misses    uint64  `json:"misses"`
	Evictions uint64  `json:"evictions"`
	Inserts   uint64  `json:"inserts"`
	Coalesced uint64  `json:"coalesced"` // GetOrCompute duplicate-compute joins
	Oversized uint64  `json:"oversizedRejected"`
	HitRatio  float64 `json:"hitRatio"`
}

// Stats returns the counters and current occupancy.
func (c *Cache) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()

	s := Stats{
		Entries:   len(c.items),
		Bytes:     c.bytes,
		MaxBytes:  c.maxBytes,
		Hits:      c.hits,
		Misses:    c.misses,
		Evictions: c.evictions,
		Inserts:   c.inserts,
		Coalesced: c.coalesced,
		Oversized: c.oversized,
	}

	if total := s.Hits + s.Misses; total > 0 {
		s.HitRatio = float64(s.Hits) / float64(total)
	}

	return s
}

// Keys returns the live keys sorted lexicographically (inspection aid;
// used by tests and the debug endpoint).
func (c *Cache) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]string, 0, len(c.items))
	for k := range c.items {
		out = append(out, k)
	}

	sort.Strings(out)
	return out
}

// removeElement drops one element; caller holds mu.
func (c *Cache) removeElement(el *list.Element) {
	e := el.Value.(*entry)
	c.bytes -= e.bytes
	_ = c.ll.Remove(el)
	delete(c.items, e.key)
}

// evictLocked enforces both bounds; caller holds mu.
func (c *Cache) evictLocked() {
	for len(c.items) > c.maxEntries {
		c.evictOldestLocked()
	}

	for c.bytes > c.maxBytes && len(c.items) > 1 {
		c.evictOldestLocked()
	}
}

func (c *Cache) evictOldestLocked() {
	el := c.ll.Back()
	if el == nil {
		return
	}

	c.removeElement(el)
	c.evictions++
}

// TrimIdle sheds the cold tail of the cache: evicts least-recently-used
// entries until at most keepEntries remain (the byte bound is inherently
// re-checked since each eviction removes its tracked bytes). Returns the
// estimated bytes freed. Called by the runtime memory manager during
// coordinated cleanup; the cache's own bounds stay in force at all times,
// so trimming is always optional pressure relief, never correctness.
func (c *Cache) TrimIdle(keepEntries int) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if keepEntries < 0 {
		keepEntries = 0
	}
	if len(c.items) <= keepEntries {
		return 0
	}

	var freed int64
	for len(c.items) > keepEntries {
		el := c.ll.Back()
		if el == nil {
			break
		}
		e := el.Value.(*entry)
		freed += e.bytes
		c.removeElement(el)
		c.evictions++
	}
	return freed
}

// String renders a compact human-readable summary for logs.
func (s Stats) String() string {
	return fmt.Sprintf(
		"entries=%d bytes=%d hits=%d misses=%d evictions=%d inserts=%d coalesced=%d oversized=%d hitRatio=%.2f",
		s.Entries, s.Bytes, s.Hits, s.Misses, s.Evictions, s.Inserts,
		s.Coalesced, s.Oversized, s.HitRatio,
	)
}
