package presentation

import (
	"strings"
	"sync"
	"testing"
)

func TestEnvelopeVersionIsOne(t *testing.T) {
	if EnvelopeVersion != 1 {
		t.Fatalf("envelope version drifted: %d", EnvelopeVersion)
	}
}

func TestPublishAssignsStableUniqueIDs(t *testing.T) {
	r := NewRegistry()
	b1, err := r.Publish(Block{Kind: KindMarkdown, Payload: "# hi", Source: "test"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	b2, err := r.Publish(Block{Kind: KindMarkdown, Payload: "# hi", Source: "test"})
	if err != nil {
		t.Fatalf("publish 2: %v", err)
	}
	if b1.ID == "" || b2.ID == "" {
		t.Fatal("publish returned empty ids")
	}
	if b1.ID == b2.ID {
		t.Fatalf("duplicate id %q for two publishes", b1.ID)
	}
	got, ok := r.Get(b1.ID)
	if !ok {
		t.Fatalf("Get(%q) missing", b1.ID)
	}
	if got.Version != EnvelopeVersion {
		t.Fatalf("block version = %d, want %d", got.Version, EnvelopeVersion)
	}
	if got.Payload != "# hi" {
		t.Fatalf("payload mutated: %q", got.Payload)
	}
}

func TestPublishRejectsMissingProvenance(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Publish(Block{Kind: KindText, Payload: "x"}); err == nil {
		t.Fatal("publish without source must fail closed")
	}
}

func TestPublishRejectsOversizedPayload(t *testing.T) {
	r := NewRegistry()
	big := strings.Repeat("a", MaxPayloadBytes+1)
	if _, err := r.Publish(Block{Kind: KindText, Payload: big, Source: "test"}); err == nil {
		t.Fatal("oversized payload must fail closed, never truncate silently")
	}
}

func TestUnknownKindDegradesToRawDataState(t *testing.T) {
	r := NewRegistry()
	b, err := r.Publish(Block{Kind: "hologram", Payload: "future content", Source: "test"})
	if err != nil {
		t.Fatalf("unknown kind must publish, not reject: %v", err)
	}
	if b.Kind != KindArtifact {
		t.Fatalf("degraded kind = %q, want %q", b.Kind, KindArtifact)
	}
	if b.Meta["originalKind"] != "hologram" {
		t.Fatalf("original kind not preserved: %v", b.Meta)
	}
	if b.Payload != "future content" {
		t.Fatal("degradation lost the payload")
	}
	// Known kinds are never degraded.
	k, err := r.Publish(Block{Kind: KindCode, Payload: "x", Source: "test"})
	if err != nil || k.Kind != KindCode {
		t.Fatalf("known kind degraded: %v %q", err, k.Kind)
	}
}

func TestSanitizeStripsNulOnly(t *testing.T) {
	in := "ab\x00cd"
	if got := Sanitize(in); got != "abcd" {
		t.Fatalf("sanitize = %q", got)
	}
	if got := Sanitize("tab\tnewline\n"); got != "tab\tnewline\n" {
		t.Fatal("sanitize altered non-NUL bytes")
	}
}

func TestSelectIsDeterministicAndModelProof(t *testing.T) {
	cases := map[string]string{
		KindText:      "native",
		KindStatus:    "native",
		KindMarkdown:  "workspace",
		KindCode:      "workspace",
		KindDiff:      "workspace",
		KindTable:     "workspace",
		KindChartSpec: "workspace",
		KindArtifact:  "workspace",
		KindScene3D:   "workspace",
		"hologram":    "workspace", // unknown → the richest degradation surface
		"":            "workspace",
	}
	for kind, want := range cases {
		for i := 0; i < 5; i++ {
			if got := Select(kind); got != want {
				t.Fatalf("Select(%q) = %q, want %q (must be deterministic)", kind, got, want)
			}
		}
	}
}

func TestRegistryBoundEvictsOldestAndReports(t *testing.T) {
	r := NewRegistry()
	var firstID string
	for i := 0; i < MaxBlocks; i++ {
		b, err := r.Publish(Block{Kind: KindText, Payload: "x", Source: "test"})
		if err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
		if i == 0 {
			firstID = b.ID
		}
	}
	if _, ok := r.Get(firstID); !ok {
		t.Fatal("registry below bound lost the oldest block")
	}
	// One more → the oldest is evicted, counted, and never silent.
	b, err := r.Publish(Block{Kind: KindText, Payload: "y", Source: "test"})
	if err != nil {
		t.Fatalf("publish over bound: %v", err)
	}
	if _, ok := r.Get(firstID); ok {
		t.Fatal("over-bound registry kept the evicted block")
	}
	if _, ok := r.Get(b.ID); !ok {
		t.Fatal("newest block missing after eviction")
	}
	st := r.Stats()
	if st.Evicted != 1 || st.Live != MaxBlocks {
		t.Fatalf("stats = %+v, want evicted=1 live=%d", st, MaxBlocks)
	}
}

func TestPublishWithIDDuplicateIsTypedError(t *testing.T) {
	r := NewRegistry()
	if _, err := r.PublishWithID(Block{Kind: KindText, Payload: "a", Source: "s"}, "fixed-id"); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if _, err := r.PublishWithID(Block{Kind: KindText, Payload: "b", Source: "s"}, "fixed-id"); err == nil {
		t.Fatal("duplicate id must be a typed error, never a silent remap")
	}
	// The original entry is intact (one ID = one logical artifact).
	got, _ := r.Get("fixed-id")
	if got.Payload != "a" {
		t.Fatalf("duplicate publish mutated the original: %q", got.Payload)
	}
}

func TestConcurrentPublishIsRaceFreeAndBounded(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, _ = r.Publish(Block{Kind: KindText, Payload: "x", Source: "concurrent"})
			}
		}()
	}
	wg.Wait()
	if st := r.Stats(); st.Live > MaxBlocks {
		t.Fatalf("registry exceeded its bound: %+v", st)
	}
}

func TestExtractBlocksSplitsFencesDeterministically(t *testing.T) {
	answer := "Here is prose.\n\n```go\nfmt.Println(1)\nfmt.Println(2)\n```\n\nAnd a tail.\n"
	got := ExtractBlocks(answer, "run-1", "sess-1", "test")
	if len(got) != 3 {
		t.Fatalf("blocks = %d, want 3 (prose, code, prose): %+v", len(got), got)
	}
	if got[0].Kind != KindMarkdown || got[0].Payload != "Here is prose." {
		t.Fatalf("block0 = %+v", got[0])
	}
	if got[1].Kind != KindCode || got[1].Language != "go" {
		t.Fatalf("block1 = %+v", got[1])
	}
	if got[1].Payload != "fmt.Println(1)\nfmt.Println(2)\n" {
		t.Fatalf("code payload = %q", got[1].Payload)
	}
	if got[2].Kind != KindMarkdown || got[2].Payload != "And a tail." {
		t.Fatalf("block2 = %+v", got[2])
	}
	for _, b := range got {
		if b.RunID != "run-1" || b.SessionID != "sess-1" || b.Source != "test" {
			t.Fatalf("provenance lost: %+v", b)
		}
	}
	// Determinism: same input → identical payload/kind sequence.
	again := ExtractBlocks(answer, "run-1", "sess-1", "test")
	for i := range got {
		if got[i].Kind != again[i].Kind || got[i].Payload != again[i].Payload {
			t.Fatalf("extraction not deterministic at %d", i)
		}
	}
}

func TestExtractBlocksEdgeCases(t *testing.T) {
	if got := ExtractBlocks("", "r", "s", "t"); got != nil {
		t.Fatalf("empty answer must yield no blocks, got %+v", got)
	}
	if got := ExtractBlocks("plain prose only", "r", "s", "t"); len(got) != 1 || got[0].Kind != KindMarkdown {
		t.Fatalf("prose-only answer = %+v", got)
	}
	// Unterminated fence: the rest of the answer is the code body —
	// nothing is dropped.
	got := ExtractBlocks("before\n```\ncode body", "r", "s", "t")
	if len(got) != 2 || got[1].Kind != KindCode || got[1].Payload != "code body\n" {
		t.Fatalf("unterminated fence = %+v", got)
	}
	// Bare fence without language.
	got = ExtractBlocks("```\nx\n```", "r", "s", "t")
	if len(got) != 1 || got[0].Kind != KindCode || got[0].Language != "" {
		t.Fatalf("bare fence = %+v", got)
	}
}

func TestListNewestFirst(t *testing.T) {
	r := NewRegistry()
	var ids []string
	for i := 0; i < 3; i++ {
		b, err := r.Publish(Block{Kind: KindText, Payload: string(rune('a' + i)), Source: "s"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, b.ID)
	}
	got := r.List(2)
	if len(got) != 2 || got[0].ID != ids[2] || got[1].ID != ids[1] {
		t.Fatalf("list order = %+v", got)
	}
	all := r.List(0)
	if len(all) != 3 || all[0].ID != ids[2] {
		t.Fatalf("list(all) = %+v", all)
	}
}
