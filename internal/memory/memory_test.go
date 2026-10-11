package memory

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStoreAppendAndAll(t *testing.T) {
	tmp := t.TempDir() + "/mem.jsonl"
	m := New(tmp)

	if err := m.Append([]string{"a", "b"}, "hello world", "test"); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if err := m.Append([]string{"c"}, "second entry", "test"); err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	all, err := m.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	if len(all) != 2 {
		t.Errorf("expected 2 entries, got %d", len(all))
	}

	if all[0].Content != "hello world" {
		t.Errorf(
			"entry 0 content: got %q",
			all[0].Content,
		)
	}

	if all[0].Class != ClassM7 {
		t.Errorf(
			"Append default class: got %q, want %q",
			all[0].Class,
			ClassM7,
		)
	}

	if all[0].Trust != TrustProvisional {
		t.Errorf(
			"Append default trust: got %q, want %q",
			all[0].Trust,
			TrustProvisional,
		)
	}
}

func TestStoreSearch(t *testing.T) {
	tmp := t.TempDir() + "/mem.jsonl"
	m := New(tmp)

	_ = m.Append([]string{"fruit"}, "apple is red", "test")
	_ = m.Append([]string{"fruit"}, "banana is yellow", "test")
	_ = m.Append([]string{"vehicle"}, "car has wheels", "test")

	hits, err := m.Search("fruit", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(hits) != 2 {
		t.Errorf(
			"expected 2 fruit hits, got %d",
			len(hits),
		)
	}

	hits, _ = m.Search("yellow", 10)
	if len(hits) != 1 {
		t.Errorf(
			"expected 1 yellow hit, got %d",
			len(hits),
		)
	}

	hits, _ = m.Search("zzz", 10)
	if len(hits) != 0 {
		t.Errorf(
			"expected 0 hits for zzz, got %d",
			len(hits),
		)
	}
}

func TestStoreCorruptJSONLIsSkipped(t *testing.T) {
	tmp := t.TempDir() + "/mem.jsonl"

	_ = os.WriteFile(
		tmp,
		[]byte(strings.Join([]string{
			"garbage line",
			`{"id":"1","tags":["a"],"content":"good","createdAt":"2026-01-01T00:00:00Z"}`,
			"more garbage",
			`{"id":"2","tags":["b"],"content":"also good","createdAt":"2026-01-02T00:00:00Z"}`,
		}, "\n")),
		0o644,
	)

	m := New(tmp)

	all, err := m.All()
	if err != nil {
		t.Errorf(
			"All() should not fail on corrupt lines: %v",
			err,
		)
	}

	if len(all) != 2 {
		t.Errorf(
			"expected 2 valid entries, got %d",
			len(all),
		)
	}
}

func TestStoreDeleteByID(t *testing.T) {
	tmp := t.TempDir() + "/mem.jsonl"
	m := New(tmp)

	_ = m.Append([]string{"a"}, "first", "test")
	_ = m.Append([]string{"b"}, "second", "test")

	all, _ := m.All()

	if len(all) != 2 {
		t.Fatalf(
			"expected 2 entries before delete, got %d",
			len(all),
		)
	}

	if all[0].ID == all[1].ID {
		t.Fatalf(
			"two Appends produced the SAME id %q — delete would wipe both",
			all[0].ID,
		)
	}

	if err := m.DeleteByID(all[0].ID); err != nil {
		t.Fatalf("DeleteByID: %v", err)
	}

	all, _ = m.All()

	if len(all) != 1 {
		t.Errorf(
			"expected 1 entry after delete, got %d",
			len(all),
		)
	}

	if all[0].Content != "second" {
		t.Errorf(
			"wrong entry left after delete: got %q",
			all[0].Content,
		)
	}
}

// TestUniqueIDNeverCollides pins the v1.0.11 fix: on Windows the clock can
// return the same value for consecutive time.Now() calls, and the old
// timestamp-only ID scheme made rapid Appends share IDs (then DeleteByID
// removed them all at once). 200 rapid appends must yield 200 distinct IDs.
func TestUniqueIDNeverCollides(t *testing.T) {
	tmp := t.TempDir() + "/mem.jsonl"
	m := New(tmp)

	const n = 200

	for i := 0; i < n; i++ {
		if err := m.Append(
			nil,
			"rapid entry",
			"test",
		); err != nil {
			t.Fatalf(
				"Append %d: %v",
				i,
				err,
			)
		}
	}

	all, err := m.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	if len(all) != n {
		t.Fatalf(
			"expected %d entries, got %d",
			n,
			len(all),
		)
	}

	seen := make(map[string]bool, n)

	for _, e := range all {
		if seen[e.ID] {
			t.Fatalf(
				"duplicate ID %q after %d rapid appends",
				e.ID,
				n,
			)
		}

		seen[e.ID] = true
	}

	before := len(all)

	if err := m.DeleteByID(all[0].ID); err != nil {
		t.Fatalf(
			"DeleteByID: %v",
			err,
		)
	}

	if after := m.Count(); after != before-1 {
		t.Fatalf(
			"delete removed %d entries, want exactly 1",
			before-after,
		)
	}
}

func TestStoreClear(t *testing.T) {
	tmp := t.TempDir() + "/mem.jsonl"
	m := New(tmp)

	_ = m.Append([]string{"a"}, "first", "test")
	_ = m.Append([]string{"b"}, "second", "test")

	if m.Count() != 2 {
		t.Errorf(
			"expected 2 before clear, got %d",
			m.Count(),
		)
	}

	if err := m.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	if m.Count() != 0 {
		t.Errorf(
			"expected 0 after clear, got %d",
			m.Count(),
		)
	}
}

func TestNormalizeEntryTrustedUserFactRemainsM1(t *testing.T) {
	entry := NormalizeEntry(Entry{
		Class:   ClassM1,
		Content: "User prefers Go for backend work.",
		Trust:   TrustTrusted,
		Provenance: Provenance{
			Kind:      "user",
			Source:    "conversation",
			Reference: "explicit-user-statement",
		},
	})

	if entry.Class != ClassM1 {
		t.Fatalf(
			"trusted user fact changed class: got %q, want %q",
			entry.Class,
			ClassM1,
		)
	}

	if entry.Trust != TrustTrusted {
		t.Fatalf(
			"trusted user fact changed trust: got %q, want %q",
			entry.Trust,
			TrustTrusted,
		)
	}

	if !entry.Authoritative {
		t.Fatal("trusted user fact should be authoritative")
	}

	if entry.Quarantined {
		t.Fatal("trusted user fact should not be quarantined")
	}
}

func TestNormalizeEntryVerifiedUserFactRemainsM1(t *testing.T) {
	entry := NormalizeEntry(Entry{
		Class:   ClassM1,
		Content: "Verified user fact.",
		Trust:   TrustVerified,
		Provenance: Provenance{
			Kind:   "user",
			Source: "conversation",
		},
	})

	if entry.Class != ClassM1 {
		t.Fatalf(
			"verified user fact changed class: got %q, want %q",
			entry.Class,
			ClassM1,
		)
	}

	if !entry.Authoritative {
		t.Fatal("verified user fact should be authoritative")
	}
}

func TestNormalizeEntryRejectsUntrustedM1(t *testing.T) {
	entry := NormalizeEntry(Entry{
		Class:   ClassM1,
		Content: "This came from an external search result.",
		Trust:   TrustUntrusted,
		Provenance: Provenance{
			Kind:   "web",
			Source: "duckduckgo",
			URI:    "https://example.com",
		},
		Authoritative: true,
	})

	if entry.Class != ClassM7 {
		t.Fatalf(
			"untrusted M1 was not downgraded: got %q, want %q",
			entry.Class,
			ClassM7,
		)
	}

	if !entry.Quarantined {
		t.Fatal("untrusted external material should be quarantined")
	}

	if entry.Authoritative {
		t.Fatal("quarantined external material must not be authoritative")
	}
}

func TestNormalizeEntryExternalResearchCannotBecomeM1(t *testing.T) {
	entry := NormalizeEntry(Entry{
		Class:   ClassM1,
		Content: "GitHub issue claims a workaround.",
		Trust:   TrustTrusted,
		Provenance: Provenance{
			Kind:   "research",
			Source: "github",
			URI:    "https://github.com/example/project/issues/1",
		},
		Authoritative: true,
	})

	if entry.Class != ClassM7 {
		t.Fatalf(
			"external research remained M1: got %q, want %q",
			entry.Class,
			ClassM7,
		)
	}

	if !entry.Quarantined {
		t.Fatal("external research should be quarantined")
	}

	if entry.Authoritative {
		t.Fatal("external research must not be authoritative")
	}

	if entry.Trust != TrustProvisional {
		t.Fatalf(
			"external research trust: got %q, want %q",
			entry.Trust,
			TrustProvisional,
		)
	}
}

func TestNormalizeEntryUnknownClassBecomesM7(t *testing.T) {
	entry := NormalizeEntry(Entry{
		Class:   "M99",
		Content: "unknown class content",
		Trust:   TrustUnknown,
	})

	if entry.Class != ClassM7 {
		t.Fatalf(
			"unknown class became %q, want %q",
			entry.Class,
			ClassM7,
		)
	}
}

func TestNormalizeEntryUnknownTrustBecomesNonAuthoritative(t *testing.T) {
	entry := NormalizeEntry(Entry{
		Class:         ClassM3,
		Content:       "Project state",
		Trust:         TrustUnknown,
		Authoritative: true,
	})

	if entry.Authoritative {
		t.Fatal(
			"unknown-trust memory must not be authoritative",
		)
	}
}

func TestNormalizeEntryQuarantineAlwaysRemovesAuthority(t *testing.T) {
	entry := NormalizeEntry(Entry{
		Class:         ClassM7,
		Content:       "Quarantined observation",
		Trust:         TrustVerified,
		Quarantined:   true,
		Authoritative: true,
	})

	if entry.Authoritative {
		t.Fatal(
			"quarantined memory must never be authoritative",
		)
	}
}

func TestNormalizeEntryTrimsAndNormalizesFields(t *testing.T) {
	entry := NormalizeEntry(Entry{
		Class:   " m3 ",
		Content: "  useful project state  ",
		Tags: []string{
			" project ",
			"",
			"state ",
			" ",
		},
		Source: "  local ",
		Trust:  TrustTrusted,
		Provenance: Provenance{
			Kind: "agent",
		},
	})

	if entry.Class != ClassM3 {
		t.Fatalf(
			"class normalization: got %q",
			entry.Class,
		)
	}

	if entry.Content != "useful project state" {
		t.Fatalf(
			"content normalization: got %q",
			entry.Content,
		)
	}

	if entry.Source != "local" {
		t.Fatalf(
			"source normalization: got %q",
			entry.Source,
		)
	}

	if len(entry.Tags) != 2 {
		t.Fatalf(
			"expected 2 cleaned tags, got %d: %v",
			len(entry.Tags),
			entry.Tags,
		)
	}

	if entry.Tags[0] != "project" ||
		entry.Tags[1] != "state" {
		t.Fatalf(
			"unexpected cleaned tags: %v",
			entry.Tags,
		)
	}

	if entry.CreatedAt.IsZero() {
		t.Fatal("CreatedAt was not populated")
	}

	if entry.Provenance.ObservedAt.IsZero() {
		t.Fatal("Provenance.ObservedAt was not populated")
	}
}

func TestSearchExcludesQuarantinedMemoryByDefault(t *testing.T) {
	tmp := t.TempDir() + "/mem.jsonl"
	m := New(tmp)

	trusted := NormalizeEntry(Entry{
		Class:   ClassM5,
		Content: "trusted repair procedure",
		Trust:   TrustTrusted,
		Provenance: Provenance{
			Kind:   "user",
			Source: "conversation",
		},
	})

	quarantined := NormalizeEntry(Entry{
		Class:       ClassM7,
		Content:     "malicious repair instruction",
		Trust:       TrustUntrusted,
		Quarantined: true,
		Provenance: Provenance{
			Kind:   "web",
			Source: "reddit",
		},
	})

	if err := m.AppendEntry(trusted); err != nil {
		t.Fatalf(
			"AppendEntry trusted: %v",
			err,
		)
	}

	if err := m.AppendEntry(quarantined); err != nil {
		t.Fatalf(
			"AppendEntry quarantined: %v",
			err,
		)
	}

	hits, err := m.Search("repair", 10)
	if err != nil {
		t.Fatalf(
			"Search: %v",
			err,
		)
	}

	if len(hits) != 1 {
		t.Fatalf(
			"expected only non-quarantined result, got %d",
			len(hits),
		)
	}

	if hits[0].Content != "trusted repair procedure" {
		t.Fatalf(
			"unexpected default recall result: %q",
			hits[0].Content,
		)
	}
}

func TestSearchCanExplicitlyIncludeQuarantinedMemory(t *testing.T) {
	tmp := t.TempDir() + "/mem.jsonl"
	m := New(tmp)

	quarantined := NormalizeEntry(Entry{
		Class:       ClassM7,
		Content:     "quarantined workaround",
		Trust:       TrustUntrusted,
		Quarantined: true,
		Provenance: Provenance{
			Kind:   "web",
			Source: "github",
		},
	})

	if err := m.AppendEntry(quarantined); err != nil {
		t.Fatalf(
			"AppendEntry: %v",
			err,
		)
	}

	hits, err := m.SearchWithOptions(
		"workaround",
		10,
		true,
	)

	if err != nil {
		t.Fatalf(
			"SearchWithOptions: %v",
			err,
		)
	}

	if len(hits) != 1 {
		t.Fatalf(
			"expected quarantined result when explicitly requested, got %d",
			len(hits),
		)
	}

	if !hits[0].Quarantined {
		t.Fatal("explicit quarantined recall lost quarantine marker")
	}

	if hits[0].Authoritative {
		t.Fatal("explicit quarantined recall became authoritative")
	}
}

func TestSearchRankingPrefersTrustedMemory(t *testing.T) {
	tmp := t.TempDir() + "/mem.jsonl"
	m := New(tmp)

	provisional := NormalizeEntry(Entry{
		Class:   ClassM5,
		Content: "repair procedure",
		Trust:   TrustProvisional,
		Provenance: Provenance{
			Kind: "agent",
		},
	})

	verified := NormalizeEntry(Entry{
		Class:   ClassM5,
		Content: "repair procedure",
		Trust:   TrustVerified,
		Provenance: Provenance{
			Kind: "user",
		},
	})

	// Append in the reverse order so the test proves trust contributes to
	// ranking rather than relying on timestamp ordering.
	if err := m.AppendEntry(provisional); err != nil {
		t.Fatalf(
			"AppendEntry provisional: %v",
			err,
		)
	}

	time.Sleep(2 * time.Millisecond)

	if err := m.AppendEntry(verified); err != nil {
		t.Fatalf(
			"AppendEntry verified: %v",
			err,
		)
	}

	hits, err := m.Search("repair", 10)
	if err != nil {
		t.Fatalf(
			"Search: %v",
			err,
		)
	}

	if len(hits) != 2 {
		t.Fatalf(
			"expected 2 results, got %d",
			len(hits),
		)
	}

	if hits[0].Trust != TrustVerified {
		t.Fatalf(
			"expected verified memory to rank first, got %q",
			hits[0].Trust,
		)
	}
}

func TestMemoryPersistsTrustAndProvenance(t *testing.T) {
	tmp := t.TempDir() + "/mem.jsonl"
	m := New(tmp)

	original := Entry{
		ID:      "persisted-1",
		Class:   ClassM4,
		Tags:    []string{"decision"},
		Content: "Use the sandbox before promotion.",
		Trust:   TrustTrusted,
		Provenance: Provenance{
			Kind:        "user",
			Source:      "conversation",
			URI:         "chat://local/1",
			Reference:   "decision-42",
			ObservedAt:  time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC),
			CollectedBy: "agent",
		},
	}

	if err := m.AppendEntry(original); err != nil {
		t.Fatalf(
			"AppendEntry: %v",
			err,
		)
	}

	all, err := m.All()
	if err != nil {
		t.Fatalf(
			"All: %v",
			err,
		)
	}

	if len(all) != 1 {
		t.Fatalf(
			"expected 1 entry, got %d",
			len(all),
		)
	}

	got := all[0]

	if got.ID != original.ID {
		t.Fatalf(
			"ID: got %q, want %q",
			got.ID,
			original.ID,
		)
	}

	if got.Class != ClassM4 {
		t.Fatalf(
			"Class: got %q, want %q",
			got.Class,
			ClassM4,
		)
	}

	if got.Trust != TrustTrusted {
		t.Fatalf(
			"Trust: got %q, want %q",
			got.Trust,
			TrustTrusted,
		)
	}

	if !got.Authoritative {
		t.Fatal("trusted persisted memory lost authority")
	}

	if got.Provenance.Kind != "user" {
		t.Fatalf(
			"Provenance.Kind: got %q",
			got.Provenance.Kind,
		)
	}

	if got.Provenance.Source != "conversation" {
		t.Fatalf(
			"Provenance.Source: got %q",
			got.Provenance.Source,
		)
	}

	if got.Provenance.URI != "chat://local/1" {
		t.Fatalf(
			"Provenance.URI: got %q",
			got.Provenance.URI,
		)
	}

	if got.Provenance.Reference != "decision-42" {
		t.Fatalf(
			"Provenance.Reference: got %q",
			got.Provenance.Reference,
		)
	}

	if got.Provenance.CollectedBy != "agent" {
		t.Fatalf(
			"Provenance.CollectedBy: got %q",
			got.Provenance.CollectedBy,
		)
	}

	if got.Provenance.ObservedAt.IsZero() {
		t.Fatal("persisted provenance timestamp is missing")
	}
}

// --- memory persistence contracts ---

func TestAppendDoesNotForceFullReparses(t *testing.T) {
	path := t.TempDir() + "/mem.jsonl"
	s := New(path)

	// Create the store, then warm the cache: the first Search parses the
	// file once. (A search against a NOT-YET-EXISTING file correctly stays
	// cold — there is nothing to cache yet.)
	if err := s.Append([]string{"seed"}, "seed entry for warming", "agent"); err != nil {
		t.Fatalf("seed append: %v", err)
	}
	if _, err := s.Search("warmup", 5); err != nil {
		t.Fatalf("warmup search: %v", err)
	}

	parses0, inc0 := s.ParseStats()

	for i := 0; i < 50; i++ {
		if err := s.Append([]string{"tag"}, strings.Repeat("entry number ", 20)+string(rune('a'+i%26)), "agent"); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Every append must have been folded in incrementally — no full parse
	// may have happened after the warmup.
	parses1, inc1 := s.ParseStats()
	if parses1 != parses0 {
		t.Fatalf("appends triggered full re-parses: %d -> %d", parses0, parses1)
	}
	if inc1-inc0 != 50 {
		t.Fatalf("expected 50 incremental appends, got %d", inc1-inc0)
	}

	// And the data must actually be searchable.
	hits, err := s.Search("entry number", 100)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 50 {
		t.Fatalf("expected 50 hits after incremental appends, got %d", len(hits))
	}
}

func TestSearchCorrectAfterClearAndReappend(t *testing.T) {
	path := t.TempDir() + "/mem.jsonl"
	s := New(path)

	_ = s.Append([]string{"old"}, "pre-clear memory about keyboards", "agent")
	if err := s.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}

	hits, err := s.Search("keyboards", 10)
	if err != nil {
		t.Fatalf("post-clear search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("cleared store returned hits: %d", len(hits))
	}

	if err := s.Append([]string{"new"}, "post-clear memory about keyboards", "agent"); err != nil {
		t.Fatalf("re-append: %v", err)
	}

	hits, err = s.Search("keyboards", 10)
	if err != nil {
		t.Fatalf("post-reappend search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected exactly 1 hit after clear+reappend, got %d", len(hits))
	}
	if hits[0].Content != "post-clear memory about keyboards" {
		t.Fatalf("wrong hit: %q", hits[0].Content)
	}
}

func TestDeleteByIDUpdatesCacheIncrementally(t *testing.T) {
	path := t.TempDir() + "/mem.jsonl"
	s := New(path)

	var ids []string
	for i := 0; i < 10; i++ {
		e := Entry{Tags: []string{"t"}, Content: "delete target number", Class: ClassM7, Trust: TrustProvisional}
		e = NormalizeEntry(e)
		if err := s.AppendEntry(e); err != nil {
			t.Fatalf("append: %v", err)
		}
		ids = append(ids, e.ID)
	}

	// Warm the parsed cache — a cold cache parses lazily on first need
	// (correct); the incremental guarantee is about NOT re-parsing on
	// every write afterwards.
	if _, err := s.Search("delete target", 100); err != nil {
		t.Fatalf("warmup search: %v", err)
	}

	parses0, _ := s.ParseStats()

	if err := s.DeleteByID(ids[3]); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteByID(ids[7]); err != nil {
		t.Fatalf("delete: %v", err)
	}

	parses1, _ := s.ParseStats()
	if parses1 != parses0 {
		t.Fatalf("delete forced a full re-parse on next read: %d -> %d", parses0, parses1)
	}

	if got := s.Count(); got != 8 {
		t.Fatalf("expected 8 entries after deletes, got %d", got)
	}

	hits, err := s.Search("delete target", 100)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 8 {
		t.Fatalf("deleted entries still searchable: %d", len(hits))
	}
}

func TestExternalWriterStillDetected(t *testing.T) {
	// The cache key remains stat-based, so a store written by ANOTHER
	// Store instance (or process) must still be picked up — the
	// incremental path must never hide external writes.
	path := t.TempDir() + "/mem.jsonl"
	s1 := New(path)
	s2 := New(path)

	_ = s1.Append([]string{"a"}, "first writer content", "agent")
	_ = s2.Append([]string{"b"}, "second writer content", "agent")

	hits, err := s1.Search("second writer", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("external write not visible: %d hits", len(hits))
	}
}

func TestSearchDoesNotMutateCachedEntries(t *testing.T) {
	path := t.TempDir() + "/mem.jsonl"
	s := New(path)

	_ = s.Append([]string{"score"}, "scored content alpha", "agent")
	_ = s.Append([]string{"score"}, "scored content beta", "agent")

	// Score is search-only and must not leak into the cached entries.
	if _, err := s.Search("alpha", 10); err != nil {
		t.Fatalf("search: %v", err)
	}

	all, err := s.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	for _, e := range all {
		if e.Score != 0 {
			t.Fatalf("cached entry carried a search score: %+v", e)
		}
	}
}

func TestConcurrentSearchAppendRace(t *testing.T) {
	path := t.TempDir() + "/mem.jsonl"
	s := New(path)

	_ = s.Append([]string{"seed"}, "seed entry", "agent")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = s.Append([]string{"c"}, "concurrent content", "agent")
				_, _ = s.Search("concurrent", 5)
			}
		}(i)
	}
	wg.Wait()

	if got := s.Count(); got != 81 {
		t.Fatalf("expected 81 entries after concurrent appends, got %d", got)
	}
}

func BenchmarkMemorySearchLargeStore(b *testing.B) {
	path := b.TempDir() + "/mem.jsonl"
	s := New(path)

	for i := 0; i < 5000; i++ {
		_ = s.Append([]string{"bench"}, "benchmark entry with some searchable content about pipelines "+string(rune('a'+i%26)), "agent")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Search("searchable content", 10); err != nil {
			b.Fatal(err)
		}
	}
}
