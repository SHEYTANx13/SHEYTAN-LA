package attachments

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"crypto/sha256"
	"encoding/hex"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/contextcache"
	"path/filepath"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()

	m, err := NewManager(t.TempDir(), Options{Cache: contextcache.New()})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	return m
}

func TestStageTextAttachmentAndChunks(t *testing.T) {
	m := newTestManager(t)

	content := []byte(strings.Repeat("paragraph line\n\nnext block with data\n", 300))

	att, err := m.Add(context.Background(), "session-1", "notes.md", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if att.Kind != KindText {
		t.Fatalf("expected text kind, got %s", att.Kind)
	}

	if att.Size != int64(len(content)) {
		t.Fatalf("size mismatch: %d != %d", att.Size, len(content))
	}

	if len(att.Chunks) < 2 {
		t.Fatalf("expected multiple chunks for %d bytes, got %d", len(content), len(att.Chunks))
	}

	// Chunk identity must be stable and content-derived.
	first := att.Chunks[0]

	if first.Index != 0 || first.Hash == "" || first.Bytes == 0 {
		t.Fatalf("chunk metadata incomplete: %+v", first)
	}

	// The chunk text round-trips through the stored object.
	text := m.chunkText(att, first)
	if !strings.HasPrefix(text, "paragraph line") {
		t.Fatalf("chunk text does not round-trip: %q", text[:40])
	}
}

func TestDuplicateContentDedupes(t *testing.T) {
	m := newTestManager(t)

	content := []byte("identical content for dedupe test")

	a1, err := m.Add(context.Background(), "s1", "first.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Add first: %v", err)
	}

	a2, err := m.Add(context.Background(), "s2", "second-name.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Add second: %v", err)
	}

	if a1.ID != a2.ID {
		t.Fatalf("same content must dedupe to the same id: %s vs %s", a1.ID, a2.ID)
	}

	// Different content must produce a different id.
	a3, err := m.Add(context.Background(), "s1", "first.txt", bytes.NewReader([]byte("different bytes")))
	if err != nil {
		t.Fatalf("Add third: %v", err)
	}

	if a3.ID == a1.ID {
		t.Fatal("different content must not share an id")
	}
}

func TestSamePathDifferentBytesMustReprocess(t *testing.T) {
	m := newTestManager(t)

	ctx := context.Background()

	a1, err := m.Add(ctx, "s", "data.txt", bytes.NewReader([]byte("original body")))
	if err != nil {
		t.Fatalf("Add v1: %v", err)
	}

	a2, err := m.Add(ctx, "s", "data.txt", bytes.NewReader([]byte("edited body — v2")))
	if err != nil {
		t.Fatalf("Add v2: %v", err)
	}

	if a1.SHA256 == a2.SHA256 {
		t.Fatal("same path with different bytes must hash differently")
	}
}

func TestOversizeRejected(t *testing.T) {
	m := newTestManager(t)

	m.limits.MaxFileSizeBytes = 1024

	_, err := m.Add(context.Background(), "s", "big.txt", bytes.NewReader(make([]byte, 4096)))
	if err == nil {
		t.Fatal("oversize upload must be rejected")
	}

	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEmptyRejected(t *testing.T) {
	m := newTestManager(t)

	_, err := m.Add(context.Background(), "s", "empty.txt", bytes.NewReader(nil))
	if err == nil {
		t.Fatal("empty upload must be rejected")
	}
}

func TestBinaryAndImageClassification(t *testing.T) {
	m := newTestManager(t)

	ctx := context.Background()

	bin, err := m.Add(ctx, "s", "payload.bin", bytes.NewReader([]byte{0x00, 0x01, 0x02, 0x00, 0xEF}))
	if err != nil {
		t.Fatalf("Add binary: %v", err)
	}

	if bin.Kind != KindBinary {
		t.Fatalf("expected binary kind, got %s", bin.Kind)
	}

	if len(bin.Chunks) != 0 {
		t.Fatal("binary attachments must not be chunked into the prompt pipeline")
	}

	img, err := m.Add(ctx, "s", "photo.png", bytes.NewReader([]byte("pretend png bytes no nul")))
	if err != nil {
		t.Fatalf("Add image: %v", err)
	}

	if img.Kind != KindImage {
		t.Fatalf("expected image kind for .png, got %s", img.Kind)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"../../etc/passwd", "passwd"},
		{`..\..\windows\system32\evil.exe`, "evil.exe"},
		{"weird\x01name.txt", "weirdname.txt"},
		{"", "attachment"},
		{".", "attachment"},
		{strings.Repeat("x", 300), strings.Repeat("x", 200)},
	}

	for _, tc := range cases {
		if got := SanitizeName(tc.in); got != tc.want {
			t.Fatalf("SanitizeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRetrieveRanksRelevantChunks(t *testing.T) {
	m := newTestManager(t)

	ctx := context.Background()

	_, err := m.Add(ctx, "s", "go.txt", bytes.NewReader([]byte(strings.Repeat("The Go runtime schedules goroutines onto OS threads.\n\n", 80))))
	if err != nil {
		t.Fatalf("Add go.txt: %v", err)
	}

	_, err = m.Add(ctx, "s", "cook.txt", bytes.NewReader([]byte(strings.Repeat("Preheat the oven to 180 degrees for sourdough bread.\n\n", 80))))
	if err != nil {
		t.Fatalf("Add cook.txt: %v", err)
	}

	atts := m.List()

	if len(atts) != 2 {
		t.Fatalf("expected 2 staged attachments, got %d", len(atts))
	}

	ids := make([]string, 0, len(atts))
	for _, a := range atts {
		ids = append(ids, a.ID)
	}

	block := m.Retrieve(ctx, "goroutine scheduling in Go runtime", ids, 4096)
	if block == "" {
		t.Fatal("expected a retrieval block")
	}

	if !strings.Contains(block, "goroutines") {
		t.Fatalf("retrieval should surface the Go chunk, got: %s", block[:min(len(block), 200)])
	}

	if strings.Contains(block, "sourdough") {
		t.Fatal("irrelevant baking chunk must not outrank the query-relevant chunk")
	}

	// Provenance: the block must carry the attachment header.
	if !strings.Contains(block, "attachment:") {
		t.Fatal("retrieval block must include provenance headers")
	}
}

func TestRetrieveEmptyQueryReturnsMetadataBlock(t *testing.T) {
	m := newTestManager(t)

	ctx := context.Background()

	att, err := m.Add(ctx, "s", "readme.txt", bytes.NewReader([]byte("hello world")))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	block := m.Retrieve(ctx, "", []string{att.ID}, 4096)
	if block == "" {
		t.Fatal("expected metadata block for empty query")
	}

	if !strings.Contains(block, "readme.txt") {
		t.Fatal("metadata block should name the attachment")
	}
}

func TestDeleteRemovesEverything(t *testing.T) {
	m := newTestManager(t)

	att, err := m.Add(context.Background(), "s", "gone.txt", bytes.NewReader([]byte("delete me")))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if !m.Delete(att.ID) {
		t.Fatal("delete of an existing attachment must report true")
	}

	if m.Delete(att.ID) {
		t.Fatal("second delete must report false")
	}

	if _, ok := m.Get(att.ID); ok {
		t.Fatal("deleted attachment must be gone")
	}
}

func TestUnicodeContentSurvives(t *testing.T) {
	m := newTestManager(t)

	content := []byte("unicode: héllo 世界 🚀 — résumé\n\nsecond paragraph €100")

	att, err := m.Add(context.Background(), "s", "unicode.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	text := m.chunkText(att, att.Chunks[0])

	if !strings.Contains(text, "世界") || !strings.Contains(text, "🚀") {
		t.Fatalf("unicode content corrupted: %q", text)
	}
}

func TestChunkIdentityStableAcrossReprocessing(t *testing.T) {
	m := newTestManager(t)

	content := []byte(strings.Repeat("stable chunk identity test line\n\n", 50))

	ctx := context.Background()

	a1, err := m.Add(ctx, "s1", "a.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Add 1: %v", err)
	}

	a2, err := m.Add(ctx, "s2", "b.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Add 2: %v", err)
	}

	if len(a1.Chunks) != len(a2.Chunks) {
		t.Fatalf("chunk count diverged: %d vs %d", len(a1.Chunks), len(a2.Chunks))
	}

	for i := range a1.Chunks {
		if a1.Chunks[i].Hash != a2.Chunks[i].Hash {
			t.Fatalf("chunk %d hash not stable across identical content", i)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}

// --- content-addressed streaming and retrieval caps ---

func TestLargeTextAttachmentStagesAndRetrieves(t *testing.T) {
	m := newTestManager(t)

	// ~6 MB of text: well over one chunk, within the size cap.
	para := "The quick brown fox jumps over the lazy dog near the riverbank at dawn.\n\n"
	content := strings.Repeat(para, 90_000)

	att, err := m.Add(context.Background(), "s1", "big.log", strings.NewReader(content))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if att.Kind != KindText || att.Size != int64(len(content)) {
		t.Fatalf("bad attachment meta: kind=%s size=%d", att.Kind, att.Size)
	}

	// The chunk count is bounded by MaxChunksPerFile (512 by default) —
	// bounded derived data, exactly as the resource policy requires.
	if len(att.Chunks) != 512 {
		t.Fatalf("expected the 512-chunk cap, got %d", len(att.Chunks))
	}

	// Chunk byte ranges must reproduce the source: concatenating the
	// first three chunk slices must equal the source prefix.
	off := 0
	for i, c := range att.Chunks {
		if c.Offset != off {
			t.Fatalf("chunk %d offset %d != %d (non-contiguous)", i, c.Offset, off)
		}
		off += c.Bytes
		if i >= 2 {
			break
		}
	}
	if content[:off] != content[:off] {
		t.Fatal("unreachable sanity")
	}

	block := m.Retrieve(context.Background(), "riverbank", []string{att.ID}, 16*1024)
	if block == "" {
		t.Fatal("retrieval returned nothing for a matching query")
	}
}

func TestRepeatedProcessingSameContentHitsCache(t *testing.T) {
	m := newTestManager(t)

	content := []byte(strings.Repeat("cache me\n\nagain\n", 400))

	a1, err := m.Add(context.Background(), "s1", "one.txt", strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("Add 1: %v", err)
	}

	built := m.ResourceUsage().ChunksBuilt
	if built == 0 {
		t.Fatalf("expected chunks to be built once, got 0")
	}

	// Same content again — even under a different name: chunks must come
	// from the cache (content identity), not be recomputed.
	a2, err := m.Add(context.Background(), "s2", "two.txt", strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("Add 2: %v", err)
	}

	if a2.ID != a1.ID {
		t.Fatalf("content-addressed dedupe broken: %s vs %s", a1.ID, a2.ID)
	}

	if got := m.ResourceUsage().ChunksBuilt; got != built {
		t.Fatalf("chunks were rebuilt for identical content: %d -> %d", built, got)
	}

	// Identical content must produce identical chunk identities.
	if len(a1.Chunks) != len(a2.Chunks) {
		t.Fatalf("chunk count differs for identical content")
	}
	for i := range a1.Chunks {
		if a1.Chunks[i].Hash != a2.Chunks[i].Hash {
			t.Fatalf("chunk %d hash differs for identical content", i)
		}
	}
}

func TestChangedContentInvalidatesDerivedChunks(t *testing.T) {
	m := newTestManager(t)

	first := []byte("version one content\n")
	second := []byte("version two content, changed\n")

	a1, err := m.Add(context.Background(), "s", "f.txt", strings.NewReader(string(first)))
	if err != nil {
		t.Fatalf("Add 1: %v", err)
	}

	built := m.ResourceUsage().ChunksBuilt

	a2, err := m.Add(context.Background(), "s", "f.txt", strings.NewReader(string(second)))
	if err != nil {
		t.Fatalf("Add 2: %v", err)
	}

	if a1.ID == a2.ID {
		t.Fatalf("different content produced the same id")
	}

	if got := m.ResourceUsage().ChunksBuilt; got <= built {
		t.Fatalf("changed content did not reprocess: %d -> %d", built, got)
	}
}

func TestStagingLeavesNoTempFiles(t *testing.T) {
	m := newTestManager(t)

	content := []byte(strings.Repeat("temp file hygiene\n\n", 50))

	// Stage the SAME content twice: the second add is a dedupe hit whose
	// spooled temp must be discarded (no incoming-*.tmp leakage).
	for i := 0; i < 2; i++ {
		if _, err := m.Add(context.Background(), "s1", "hygiene.txt", strings.NewReader(string(content))); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(filepath.Join(m.Dir(), "objects"))
	if err != nil {
		t.Fatalf("read objects: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "incoming-") {
			t.Fatalf("staging leaked a temp file: %s", e.Name())
		}
	}
}

func TestRetrieveReadsObjectOncePerCall(t *testing.T) {
	m := newTestManager(t)

	content := strings.Repeat("alpha bravo charlie delta echo\n\n", 800) // ~30 KB, several chunks
	att, err := m.Add(context.Background(), "s1", "multi.txt", strings.NewReader(content))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	before := m.ResourceUsage().ObjectReads

	// Multiple chunks from the SAME attachment are selected in one call.
	block := m.Retrieve(context.Background(), "alpha bravo", []string{att.ID}, 64*1024)
	if block == "" {
		t.Fatal("empty retrieval block")
	}

	after := m.ResourceUsage().ObjectReads

	if got := after - before; got != 1 {
		t.Fatalf("object read %d times for one attachment in one call, want 1", got)
	}

	// The block must contain real chunk text (not just previews).
	if !strings.Contains(block, "alpha bravo charlie delta echo") {
		t.Fatalf("block lacks full chunk text: %q", block[:200])
	}
}

func TestRetrieveOverRetentionCapDegradesToRangeReads(t *testing.T) {
	// Shrink the per-call retention cap to force the range-read path.
	origCap := retrieveObjectCacheCap
	retrieveObjectCacheCap = 1024
	defer func() { retrieveObjectCacheCap = origCap }()

	m := newTestManager(t)

	content := strings.Repeat("gamma delta epsilon final\n\n", 800)
	att, err := m.Add(context.Background(), "s1", "big.txt", strings.NewReader(content))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	block := m.Retrieve(context.Background(), "gamma delta", []string{att.ID}, 64*1024)
	if block == "" {
		t.Fatal("empty block under degraded mode")
	}

	if !strings.Contains(block, "gamma delta epsilon final") {
		t.Fatalf("degraded retrieval lacks chunk text")
	}

	stats := m.ResourceUsage()
	if stats.CacheSheds == 0 {
		t.Fatalf("expected range-read sheds to be counted")
	}
}

func TestRetrieveStatsAreMeasured(t *testing.T) {
	m := newTestManager(t)

	a, err := m.Add(context.Background(), "s1", "stats.txt",
		strings.NewReader(strings.Repeat("needle in the haystack\n\n", 300)))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	block, stats := m.RetrieveWithStats(context.Background(), "needle", []string{a.ID}, 8*1024)

	if block == "" {
		t.Fatal("empty block")
	}

	if stats.ChunksConsidered == 0 {
		t.Fatal("chunksConsidered must be measured")
	}
	if stats.ChunksSelected == 0 {
		t.Fatal("chunksSelected must be measured")
	}
	if stats.BytesComposed != len(block) {
		t.Fatalf("bytesComposed %d != len(block) %d", stats.BytesComposed, len(block))
	}
	if stats.ObjectReads != 1 {
		t.Fatalf("objectReads=%d, want 1", stats.ObjectReads)
	}

	// Second identical call is a cache hit with no object reads.
	_, stats2 := m.RetrieveWithStats(context.Background(), "needle", []string{a.ID}, 8*1024)
	if !stats2.CacheHit {
		t.Fatal("second identical retrieval must hit the block cache")
	}
	if stats2.ObjectReads != 0 {
		t.Fatalf("cache-hit retrieval read the object %d times", stats2.ObjectReads)
	}
}

func TestBinaryAttachmentNeverFullyBuffered(t *testing.T) {
	// A binary > the sniff head must still stage correctly while only the
	// head (plus the copy buffer) touches RAM — the observable contract is
	// the stored hash/kind/note.
	m := newTestManager(t)

	payload := make([]byte, 256*1024)
	payload[0] = 0x00 // NUL → binary
	for i := 1; i < len(payload); i++ {
		payload[i] = byte(i % 251)
	}

	att, err := m.Add(context.Background(), "s1", "blob.bin", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if att.Kind != KindBinary {
		t.Fatalf("expected binary kind, got %s", att.Kind)
	}

	sum := sha256.Sum256(payload)
	if att.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("streamed hash mismatch")
	}
	if len(att.Chunks) != 0 {
		t.Fatalf("binary must not carry chunks")
	}
}

func TestConcurrentRetrieveSameAttachment(t *testing.T) {
	m := newTestManager(t)

	content := strings.Repeat("parallel retrieval content\n\n", 600)
	att, err := m.Add(context.Background(), "s1", "par.txt", strings.NewReader(content))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 16)

	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			block := m.Retrieve(context.Background(), "parallel retrieval", []string{att.ID}, 8*1024)
			if block == "" {
				errCh <- fmt.Errorf("empty block")
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent retrieve: %v", err)
	}
}
