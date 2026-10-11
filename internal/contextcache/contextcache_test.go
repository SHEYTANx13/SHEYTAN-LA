package contextcache

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"sync/atomic"
)

func TestHitMissAndStats(t *testing.T) {
	c := New()

	if _, ok := c.Get("missing"); ok {
		t.Fatal("expected miss for absent key")
	}

	c.Put("k1", "value1", 10, 0)

	v, ok := c.Get("k1")
	if !ok || v != "value1" {
		t.Fatalf("expected hit with value1, got ok=%v v=%v", ok, v)
	}

	stats := c.Stats()

	if stats.Hits != 1 || stats.Misses != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	if stats.HitRatio != 0.5 {
		t.Fatalf("expected hit ratio 0.5, got %v", stats.HitRatio)
	}
}

func TestSamePathDifferentContentMustMiss(t *testing.T) {
	// The cache contract: keys are content-derived. Two "files" at the same
	// path with different bytes must produce different keys.
	c := New()

	path := "/workspace/notes.md"

	keyV1 := Key("chunks", path, ContentHash([]byte("version one")))
	keyV2 := Key("chunks", path, ContentHash([]byte("version two — edited")))

	c.Put(keyV1, "old chunks", 10, 0)

	if _, ok := c.Get(keyV2); ok {
		t.Fatal("edited content must not be served from the old content's cache entry")
	}

	if v, ok := c.Get(keyV1); !ok || v != "old chunks" {
		t.Fatal("unchanged content should still hit")
	}
}

func TestLRUEvictionByEntries(t *testing.T) {
	c := New(WithMaxEntries(3), WithMaxBytes(1<<30))

	for i := 0; i < 5; i++ {
		c.Put(fmt.Sprintf("k%d", i), i, 1, 0)
	}

	stats := c.Stats()

	if stats.Entries != 3 {
		t.Fatalf("expected 3 entries after eviction, got %d", stats.Entries)
	}

	if stats.Evictions != 2 {
		t.Fatalf("expected 2 evictions, got %d", stats.Evictions)
	}

	// Oldest entries must be gone; newest must remain.
	if _, ok := c.Get("k0"); ok {
		t.Fatal("k0 should have been evicted (oldest)")
	}

	if _, ok := c.Get("k4"); !ok {
		t.Fatal("k4 should remain (newest)")
	}
}

func TestLRUEvictionByBytes(t *testing.T) {
	c := New(WithMaxEntries(100), WithMaxBytes(100))

	c.Put("big1", "x", 60, 0)
	c.Put("big2", "y", 60, 0)

	stats := c.Stats()

	if stats.Bytes > 100 {
		t.Fatalf("byte bound violated: %d > 100", stats.Bytes)
	}

	if stats.Entries > 1 {
		t.Fatalf("expected eviction down to <=1 entry, got %d", stats.Entries)
	}
}

func TestLRURecencyProtectsHotEntries(t *testing.T) {
	c := New(WithMaxEntries(2))

	c.Put("a", 1, 1, 0)
	c.Put("b", 2, 1, 0)

	// Touch "a" so it becomes most-recently-used.
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a should be present")
	}

	c.Put("c", 3, 1, 0)

	if _, ok := c.Get("a"); !ok {
		t.Fatal("recently-used 'a' must survive; 'b' should be evicted instead")
	}

	if _, ok := c.Get("b"); ok {
		t.Fatal("least-recently-used 'b' should have been evicted")
	}
}

func TestTTLExpiry(t *testing.T) {
	c := New()

	c.Put("ttl", "value", 1, 20*time.Millisecond)

	if _, ok := c.Get("ttl"); !ok {
		t.Fatal("fresh TTL entry should hit")
	}

	time.Sleep(40 * time.Millisecond)

	if _, ok := c.Get("ttl"); ok {
		t.Fatal("expired entry must miss")
	}
}

func TestInvalidatePrefix(t *testing.T) {
	c := New()

	c.Put(Key("attachments:chunks", "aaa", "v1"), 1, 1, 0)
	c.Put(Key("attachments:chunks", "bbb", "v1"), 2, 1, 0)
	c.Put(Key("attachments:retrieve", "aaa"), 3, 1, 0)

	dropped := c.InvalidatePrefix("attachments:chunks")

	if dropped != 2 {
		t.Fatalf("expected 2 dropped, got %d", dropped)
	}

	if _, ok := c.Get(Key("attachments:retrieve", "aaa")); !ok {
		t.Fatal("unrelated entry must survive prefix invalidation")
	}
}

func TestClearAndConfigFingerprint(t *testing.T) {
	c := New()

	c.Put("a", 1, 1, 0)
	c.Put("b", 2, 1, 0)

	c.Clear()

	if got := c.Stats().Entries; got != 0 {
		t.Fatalf("clear must empty the cache, got %d entries", got)
	}

	// Content-sensitive fingerprints: different config → different key.
	f1 := ConfigFingerprint("chunk=4096")
	f2 := ConfigFingerprint("chunk=8192")

	if f1 == f2 {
		t.Fatal("different configuration must produce different fingerprints")
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := New(WithMaxEntries(64))

	var wg sync.WaitGroup

	for worker := 0; worker < 8; worker++ {
		wg.Add(1)

		go func(seed int) {
			defer wg.Done()

			for i := 0; i < 500; i++ {
				key := fmt.Sprintf("k%d", (seed+i)%100)

				if i%3 == 0 {
					c.Put(key, i, 1, 0)
					continue
				}

				c.Get(key)

				if i%97 == 0 {
					c.InvalidatePrefix("k")
				}
			}
		}(worker)
	}

	wg.Wait()

	if got := c.Stats().Entries; got > 64 {
		t.Fatalf("bound violated under concurrency: %d entries", got)
	}
}

// --- content-keyed cache behavior ---

func TestGetOrComputeComputesOnce(t *testing.T) {
	c := New()

	calls := 0
	v, ok := GetOrCompute(c, "k", 0, func(s string) int64 { return int64(len(s)) }, func() string {
		calls++
		return "computed"
	})

	if !ok || v != "computed" || calls != 1 {
		t.Fatalf("first call: ok=%t v=%q calls=%d", ok, v, calls)
	}

	v, ok = GetOrCompute(c, "k", 0, func(s string) int64 { return int64(len(s)) }, func() string {
		calls++
		return "should-not-run"
	})

	if !ok || v != "computed" || calls != 1 {
		t.Fatalf("cached call recomputed: ok=%t v=%q calls=%d", ok, v, calls)
	}

	if c.Stats().Hits != 1 {
		t.Fatalf("expected 1 hit, got %d", c.Stats().Hits)
	}
}

func TestGetOrComputeCoalescesConcurrentSameKey(t *testing.T) {
	c := New()

	var calls atomic.Int64
	release := make(chan struct{})
	computeStarted := make(chan struct{})

	compute := func() string {
		if calls.Add(1) == 1 {
			close(computeStarted)
		}
		<-release
		return "shared"
	}

	const workers = 16
	var wg sync.WaitGroup
	results := make([]string, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			v, ok := GetOrCompute(c, "hot", 0, func(s string) int64 { return int64(len(s)) }, compute)
			if !ok {
				panic("coalesced call lost")
			}
			results[idx] = v
		}(i)
	}

	<-computeStarted
	// Give the other goroutines a moment to pile onto the in-flight call.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("compute ran %d times, want exactly 1", got)
	}

	for i, r := range results {
		if r != "shared" {
			t.Fatalf("worker %d got %q", i, r)
		}
	}

	if got := c.Stats().Coalesced; got == 0 {
		t.Fatalf("expected coalesced joins to be counted, got 0")
	}
}

func TestGetOrComputePropagatesPanic(t *testing.T) {
	c := New()

	boom := func() string {
		panic("boom")
	}

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("expected panic to propagate to the owner")
			}
		}()
		GetOrCompute(c, "p", 0, nil, boom)
	}()

	// The in-flight slot must be released: the next caller recomputes
	// instead of deadlocking/seeing a stale call.
	ran := false
	func() {
		defer func() {
			_ = recover()
		}()
		GetOrCompute(c, "p", 0, nil, boom)
		ran = true // unreachable when boom panics again — fine
	}()

	if ran {
		// boom panics every time; reaching here means the panic vanished.
		t.Fatalf("panic was swallowed on recompute")
	}

	// A healthy value can now be stored under the same key.
	v, ok := GetOrCompute(c, "p", 0, nil, func() string { return "ok" })
	if !ok || v != "ok" {
		t.Fatalf("recovery path broken: ok=%t v=%q", ok, v)
	}
}

func TestPutRejectsOversizedEntries(t *testing.T) {
	c := New(WithMaxBytes(1024), WithMaxEntryBytes(512))

	if !c.Put("small", "v", 100, 0) {
		t.Fatalf("small entry must be accepted")
	}

	if c.Put("huge", "v", 4096, 0) {
		t.Fatalf("entry above the max-entry bound must be rejected")
	}

	st := c.Stats()
	if st.Oversized != 1 {
		t.Fatalf("expected 1 oversized rejection, got %d", st.Oversized)
	}

	// The cache must still hold exactly the small entry — the rejection
	// cannot have evicted anything.
	if st.Entries != 1 || st.Bytes != 100 {
		t.Fatalf("cache state after rejection: %+v", st)
	}

	if _, ok := c.Get("small"); !ok {
		t.Fatalf("small entry vanished")
	}

	// GetOrCompute with an oversized result shares the value with the
	// caller but must not cache it.
	v, ok := GetOrCompute(c, "big", 0, func(s string) int64 { return int64(len(s)) }, func() string {
		return string(make([]byte, 4096))
	})
	if !ok || len(v) != 4096 {
		t.Fatalf("oversized GetOrCompute result lost")
	}
	if _, ok := c.Get("big"); ok {
		t.Fatalf("oversized GetOrCompute value must not be cached")
	}
}

func TestGetOrComputeDifferentKeysComputeConcurrently(t *testing.T) {
	c := New()

	release := make(chan struct{})
	started := make(chan struct{}, 2)

	compute := func() string {
		started <- struct{}{}
		<-release
		return "v"
	}

	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			GetOrCompute(c, "a", 0, nil, compute)
		}()
		break // only one pair needed
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		GetOrCompute(c, "b", 0, nil, compute)
	}()

	<-started
	<-started // both keys compute in parallel — no cross-key serialization
	close(release)
	wg.Wait()
}

func TestInsertsCountedExactly(t *testing.T) {
	c := New()

	c.Put("a", 1, 10, 0)
	c.Put("a", 2, 10, 0) // update, not an insert
	c.Put("b", 3, 20, 0)

	st := c.Stats()
	if st.Inserts != 2 || st.Bytes != 30 {
		t.Fatalf("accounting wrong: inserts=%d bytes=%d", st.Inserts, st.Bytes)
	}

	c.Invalidate("a")
	c.Put("a", 4, 10, 0) // re-insert after invalidation

	if st := c.Stats(); st.Inserts != 3 {
		t.Fatalf("re-insert not counted: inserts=%d", st.Inserts)
	}
}

func BenchmarkCacheGetOrComputeHit(b *testing.B) {
	c := New()
	GetOrCompute(c, "k", 0, nil, func() string { return "value" })

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := GetOrCompute(c, "k", 0, nil, func() string {
			b.Fatal("must not recompute")
			return ""
		}); !ok {
			b.Fatal("hit lost")
		}
	}
}
