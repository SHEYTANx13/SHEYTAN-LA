// Package attachments implements SHEYTAN's real attachment pipeline
// (v1.1.3): select → validate → safe staging → inspect → type detection
// → extract → chunk → cache → associate with a session/message → retrieve
// relevant content → send a bounded representation to the model.
//
// Security model:
//
//   - Uploads are staged inside a dedicated directory under the app's
//     private data dir. Stored files are content-addressed by SHA-256 and
//     written 0600 with O_EXCL semantics; symlink following is rejected.
//   - Original filenames are sanitized to a display name only — they never
//     touch the filesystem.
//   - Nothing is ever executed. Text-ish content is extracted and chunked;
//     images ride the vision pipeline as plain paths; binaries become a
//     bounded metadata note so the model can decide what to do with the
//     files tool.
//   - Hard limits: file size, per-request byte cap, file count, processing
//     time, chunk count, and total in-memory buffering are all bounded and
//     enforced.
//
// Processing results (normalized text, chunk metadata, retrieval blocks)
// are cached in the content-aware contextcache keyed by content hash +
// processing version + configuration fingerprint. Same content under a
// different name must hit; same name with different bytes must miss.
package attachments

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/contextcache"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/humanize"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
)

// Hard limits (all enforced; overridable via Options for tests).
const (
	DefaultMaxFileSizeBytes  = 64 << 20 // 64 MiB per file
	DefaultMaxFilesPerAttach = 16       // files per upload request
	DefaultProcessTimeout    = 30 * time.Second
	DefaultChunkSizeBytes    = 4096 // chunk target size for text
	DefaultMaxChunksPerFile  = 512
	DefaultMaxTotalBytes     = 128 << 20 // staging dir soft cap
	DefaultRetrievalBudget   = 16 * 1024 // bytes for one retrieval block
)

// retrieveObjectCacheCap bounds how many object bytes ONE retrieval call
// may retain for chunk-text reuse. When the accumulated objects exceed the
// cap the source degrades to per-chunk byte-range reads (correct, more
// syscalls) instead of pinning unbounded buffers — the Phase 3 resource
// policy: shed retention, never correctness. A var so tests can shrink it;
// production code never writes it.
var retrieveObjectCacheCap = int64(32 << 20)

// sniffHeadBytes is the amount of content kept in RAM during staging for
// classification (looksBinary reads 8 KiB, isUTF8ish 16 KiB — 16 KiB covers
// both).
const sniffHeadBytes = 16 * 1024

// Kind classifies a staged attachment.
type Kind string

const (
	KindText   Kind = "text"
	KindImage  Kind = "image"
	KindBinary Kind = "binary"
)

// Limits is the bound set applied to staging and processing.
type Limits struct {
	MaxFileSizeBytes  int64
	MaxFilesPerAttach int
	ProcessTimeout    time.Duration
	ChunkSizeBytes    int
	MaxChunksPerFile  int
	MaxTotalBytes     int64
	RetrievalBudget   int
}

// DefaultLimits returns the production bound set.
func DefaultLimits() Limits {
	return Limits{
		MaxFileSizeBytes:  DefaultMaxFileSizeBytes,
		MaxFilesPerAttach: DefaultMaxFilesPerAttach,
		ProcessTimeout:    DefaultProcessTimeout,
		ChunkSizeBytes:    DefaultChunkSizeBytes,
		MaxChunksPerFile:  DefaultMaxChunksPerFile,
		MaxTotalBytes:     DefaultMaxTotalBytes,
		RetrievalBudget:   DefaultRetrievalBudget,
	}
}

// Chunk is one stored chunk of an attachment with stable identity and
// provenance. Hashes are content hashes: the same content processed with
// the same configuration yields identical chunk identities.
type Chunk struct {
	ID      string `json:"id"` // <attID>:<index>:<hash8>
	AttID   string `json:"attId"`
	Index   int    `json:"index"`
	Hash    string `json:"hash"`   // sha256 of chunk text
	Offset  int    `json:"offset"` // byte offset in normalized text
	Bytes   int    `json:"bytes"`
	Tokens  int    `json:"tokens"`  // estimated
	Preview string `json:"preview"` // first ~120 chars
}

// Attachment is the metadata record of one staged file.
type Attachment struct {
	ID        string    `json:"id"` // content-addressed: "a" + sha256[:24]
	Name      string    `json:"name"`
	Kind      Kind      `json:"kind"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"createdAt"`
	Chunks    []Chunk   `json:"chunks,omitempty"`
	Note      string    `json:"note,omitempty"`

	// SessionIDs records which sessions reference this attachment
	// (provenance, for listing/cleanup decisions).
	SessionIDs []string `json:"sessionIds,omitempty"`
}

// Manager owns the staged attachment store.
type Manager struct {
	mu      sync.Mutex
	dir     string // <dataDir>/attachments
	cache   *contextcache.Cache
	limits  Limits
	metas   map[string]*Attachment // id -> metadata (loaded lazily)
	loaded  bool
	version string // processing fingerprint for cache keys

	// res is the Phase 3 resource accounting: measured bytes flowing
	// through the manager. Atomic — hot paths never take mu to count.
	res resourceCounters
}

// resourceCounters tracks measured data movement. Everything here is a
// real measurement (bytes actually read/written), never an estimate.
type resourceCounters struct {
	staged      atomic.Uint64 // files staged
	stagedBytes atomic.Uint64 // bytes written to the object store
	objectReads atomic.Uint64 // object file opens during retrieval
	readBytes   atomic.Uint64 // object bytes read during retrieval
	chunksBuilt atomic.Uint64 // chunks derived (cache misses only)
	cacheSheds  atomic.Uint64 // times the per-call object cache shed entries
}

// ResourceStats is a point-in-time snapshot of measured data movement.
type ResourceStats struct {
	FilesStaged uint64 `json:"filesStaged"`
	StagedBytes uint64 `json:"stagedBytes"`
	ObjectReads uint64 `json:"objectReads"`
	ReadBytes   uint64 `json:"readBytes"`
	ChunksBuilt uint64 `json:"chunksBuilt"`
	CacheSheds  uint64 `json:"cacheSheds"`
}

// Options configures a Manager.
type Options struct {
	Limits Limits
	Cache  *contextcache.Cache
}

// NewManager opens (and creates) the attachment store under dir.
func NewManager(dir string, opts Options) (*Manager, error) {
	if dir == "" {
		return nil, fmt.Errorf("attachments: empty directory")
	}

	limits := opts.Limits
	if limits.MaxFileSizeBytes <= 0 {
		limits = DefaultLimits()
	}

	m := &Manager{
		dir:    dir,
		cache:  opts.Cache,
		limits: limits,
		metas:  make(map[string]*Attachment),
	}

	if m.cache == nil {
		m.cache = contextcache.New()
	}

	m.version = contextcache.ConfigFingerprint(
		fmt.Sprintf("v%d", contextcache.Version),
		fmt.Sprintf("chunk=%d", limits.ChunkSizeBytes),
		fmt.Sprintf("maxchunks=%d", limits.MaxChunksPerFile),
	)

	for _, sub := range []string{"objects", "meta"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("attachments: create %s: %w", sub, err)
		}
	}

	return m, nil
}

// Dir returns the store root.
func (m *Manager) Dir() string { return m.dir }

// Limits returns the active bound set.
func (m *Manager) Limits() Limits { return m.limits }

// ResourceUsage returns the measured data-movement counters of this
// manager (diagnostics: real counts only, no estimates).
func (m *Manager) ResourceUsage() ResourceStats {
	return ResourceStats{
		FilesStaged: m.res.staged.Load(),
		StagedBytes: m.res.stagedBytes.Load(),
		ObjectReads: m.res.objectReads.Load(),
		ReadBytes:   m.res.readBytes.Load(),
		ChunksBuilt: m.res.chunksBuilt.Load(),
		CacheSheds:  m.res.cacheSheds.Load(),
	}
}

// objectPath is the content-addressed object location.
func (m *Manager) objectPath(id string) string {
	return filepath.Join(m.dir, "objects", id)
}

func (m *Manager) metaPath(id string) string {
	return filepath.Join(m.dir, "meta", id+".json")
}

// Add stages one uploaded file: it streams, bounds, hashes, stores, sniffs,
// and processes (chunks) the content. The returned Attachment is safe to
// persist and display. `sessionID` records provenance (may be empty).
//
// v1.1.5 Phase 3: staging is STREAMING — content is spooled to a temp
// file while hashed on the fly, so only a bounded sniff head (16 KiB) and
// one copy buffer ever sit in RAM, whatever the upload size. Previously the
// whole file (up to 64 MiB) was buffered just to hash and classify it.
// Binary and image attachments are never fully buffered anymore; text
// attachments are read back from the object store once for chunking (the
// page cache makes that cheap) so peak memory stays bounded.
func (m *Manager) Add(
	ctx context.Context,
	sessionID string,
	displayName string,
	r io.Reader,
) (*Attachment, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	name := SanitizeName(displayName)

	// Stream to a temp object while hashing. Oversize/empty checks happen
	// during the copy — an oversized upload never touches the object store.
	tmpPath, size, sum, head, err := m.spool(name, r)
	if err != nil {
		return nil, err
	}
	// From here on the temp file is ours to rename or discard.
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	sha := hex.EncodeToString(sum[:])
	id := "a" + sha[:24]

	// Content-addressed store: identical content under any name dedupes.
	obj := m.objectPath(id)

	if err := commitObject(obj, tmpPath); err != nil {
		return nil, err
	}
	tmpPath = "" // renamed into place — nothing left to clean up

	kind := classify(name, head)

	att := &Attachment{
		ID:        id,
		Name:      name,
		Kind:      kind,
		Size:      size,
		SHA256:    sha,
		CreatedAt: time.Now().UTC(),
	}

	if sessionID != "" {
		att.SessionIDs = []string{sessionID}
	}

	m.res.staged.Add(1)
	m.res.stagedBytes.Add(uint64(size))

	// Process (extract + chunk) with a bounded timeout. Text only — images
	// and binaries carry no chunks by design. Text content is read back
	// from the stored object in one pass.
	if kind == KindText {
		pctx, cancel := context.WithTimeout(ctx, m.limits.ProcessTimeout)
		defer cancel()

		data, rerr := os.ReadFile(obj)
		if rerr != nil {
			att.Note = "processing incomplete: " + rerr.Error()
		} else {
			chunks, perr := m.processText(pctx, att, data)
			if perr != nil {
				att.Note = "processing incomplete: " + perr.Error()
			}
			att.Chunks = chunks
		}
	} else if kind == KindImage {
		att.Note = "image attachment — delivered to the vision pipeline when the engine supports it"
	} else {
		att.Note = fmt.Sprintf(
			"binary attachment (%s) — not inlined; the agent can inspect it with its file tools at %s",
			humanize.Bytes(att.Size),
			obj,
		)
	}

	if err := m.saveMeta(att); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.metas[id] = att
	m.mu.Unlock()

	return att, nil
}

// spool streams r into a temp file under the object store while hashing it
// and capturing the sniff head. It enforces the per-file size cap during
// the copy, so oversized input is rejected without ever buffering it (and
// without writing it to the final object location). The caller owns the
// returned temp file (rename or remove).
func (m *Manager) spool(name string, r io.Reader) (tmpPath string, size int64, sum []byte, head []byte, err error) {
	token, terr := spoolToken()
	if terr != nil {
		return "", 0, nil, nil, fmt.Errorf("attachments: spool token: %w", terr)
	}

	tmpPath = filepath.Join(m.dir, "objects", "incoming-"+token)

	f, ferr := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if ferr != nil {
		return "", 0, nil, nil, fmt.Errorf("attachments: stage: %w", ferr)
	}
	defer func() {
		f.Close()
		if err != nil {
			_ = os.Remove(tmpPath)
			tmpPath = ""
		}
	}()

	hasher := sha256.New()
	headBuf := make([]byte, 0, sniffHeadBytes)

	// boundedHead captures the first sniffHeadBytes as the stream passes.
	boundedHead := &headCapture{dst: &headBuf, cap: sniffHeadBytes}
	limited := io.LimitReader(r, m.limits.MaxFileSizeBytes+1)
	sink := io.MultiWriter(f, hasher, boundedHead)

	buf := copyBufferPool.Get().(*[]byte)
	n, cerr := io.CopyBuffer(sink, limited, *buf)
	copyBufferPool.Put(buf)

	if cerr != nil {
		err = fmt.Errorf("attachments: read: %w", cerr)
		return "", 0, nil, nil, err
	}

	size = n

	if size > m.limits.MaxFileSizeBytes {
		err = fmt.Errorf(
			"attachments: %s exceeds the %s per-file limit",
			name,
			humanize.Bytes(m.limits.MaxFileSizeBytes),
		)
		return "", 0, nil, nil, err
	}

	if size == 0 {
		err = fmt.Errorf("attachments: %s is empty", name)
		return "", 0, nil, nil, err
	}

	return tmpPath, size, hasher.Sum(nil), headBuf, nil
}

// headCapture retains the first `cap` bytes of a stream (no-op afterwards).
type headCapture struct {
	dst *[]byte
	cap int
}

func (h *headCapture) Write(p []byte) (int, error) {
	if len(*h.dst) < h.cap {
		room := h.cap - len(*h.dst)
		if room > len(p) {
			room = len(p)
		}
		*h.dst = append(*h.dst, p[:room]...)
	}
	return len(p), nil
}

var copyBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 128*1024)
		return &b
	},
}

// spoolToken produces a unique-enough token for temp file names.
func spoolToken() (string, error) {
	var rnd [8]byte
	if _, err := crand.Read(rnd[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(rnd[:]), nil
}

// commitObject moves a spooled temp object into its content-addressed
// location. An existing identical object is a dedupe hit: the temp file is
// discarded HERE (the caller clears its cleanup reference). Symlinks at
// the target are refused (path safety unchanged).
func commitObject(obj, tmp string) error {
	if fi, err := os.Lstat(obj); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("attachments: refusing symlink at %s", obj)
		}
		// Same content already stored — dedupe hit; discard the spool.
		_ = os.Remove(tmp)
		return nil
	}

	if err := os.Rename(tmp, obj); err != nil {
		return fmt.Errorf("attachments: commit: %w", err)
	}

	return nil
}

// processText normalizes and chunks text content, caching the result.
//
// v1.1.5 Phase 3: chunk derivation runs through the shared chunking
// engine (chunking.ChunkText) with full metadata, and the cache lookup is
// single-flight — concurrent processing of the same content computes once.
func (m *Manager) processText(
	ctx context.Context,
	att *Attachment,
	data []byte,
) ([]Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	key := contextcache.Key(
		"attachments:chunks",
		att.SHA256,
		m.version,
	)

	chunks, _ := contextcache.GetOrCompute(
		m.cache,
		key,
		0,
		func(chunks []Chunk) int64 {
			return int64(len(chunks)) * int64(chunkMetaSize)
		},
		func() []Chunk {
			text := NormalizeText(data)

			// Historical floors preserved: chunkSize >= 256, maxChunks >= 1.
			chunkSize := m.limits.ChunkSizeBytes
			if chunkSize < 256 {
				chunkSize = 256
			}
			maxChunks := m.limits.MaxChunksPerFile
			if maxChunks < 1 {
				maxChunks = 1
			}

			derived := chunking.ChunkText(
				att.SHA256,
				text,
				chunking.ChunkerConfig{
					MaxBytes:    chunkSize,
					MaxChunks:   maxChunks,
					WithPreview: true,
				},
			)

			out := make([]Chunk, 0, len(derived))

			for i, dc := range derived {
				// Wire identity unchanged: <attID>:<index>:<hash8> — the same
				// format every stored meta file uses.
				out = append(out, Chunk{
					ID:      fmt.Sprintf("%s:%d:%s", att.ID, i, dc.Hash[:8]),
					AttID:   att.ID,
					Index:   i,
					Hash:    dc.Hash,
					Offset:  int(dc.Offset),
					Bytes:   dc.Bytes,
					Tokens:  dc.Tokens,
					Preview: dc.Preview,
				})
			}

			m.res.chunksBuilt.Add(uint64(len(out)))

			return out
		},
	)

	return chunks, nil
}

// chunkMetaSize is a rough per-chunk overhead estimate for cache sizing.
const chunkMetaSize = 160

// Get returns the metadata for one attachment.
func (m *Manager) Get(id string) (*Attachment, bool) {
	m.mu.Lock()
	att, ok := m.metas[id]
	m.mu.Unlock()

	if ok {
		return att, true
	}

	att, err := m.loadMeta(id)
	if err != nil {
		return nil, false
	}

	m.mu.Lock()
	m.metas[id] = att
	m.mu.Unlock()

	return att, true
}

// List returns every stored attachment, newest first.
func (m *Manager) List() []*Attachment {
	m.loadAll()

	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]*Attachment, 0, len(m.metas))

	for _, att := range m.metas {
		out = append(out, att)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})

	return out
}

// Delete removes an attachment: object, metadata, cached derivatives.
// Returns whether the id existed.
func (m *Manager) Delete(id string) bool {
	existed := false

	m.mu.Lock()
	if _, ok := m.metas[id]; ok {
		delete(m.metas, id)
		existed = true
	}
	m.mu.Unlock()

	if err := os.Remove(m.metaPath(id)); err == nil {
		existed = true
	}

	if err := os.Remove(m.objectPath(id)); err == nil {
		existed = true
	}

	m.cache.Invalidate(
		contextcache.Key("attachments:chunks", shaOfID(id), m.version),
	)

	return existed
}

// Associate records that a session references an attachment.
func (m *Manager) Associate(id, sessionID string) {
	if sessionID == "" {
		return
	}

	att, ok := m.Get(id)
	if !ok {
		return
	}

	for _, s := range att.SessionIDs {
		if s == sessionID {
			return
		}
	}

	att.SessionIDs = append(att.SessionIDs, sessionID)
	_ = m.saveMeta(att)
}

// StagePath returns the on-disk path of the stored object (for the vision
// pipeline and for the model's file tools). Returns "" when unknown.
func (m *Manager) StagePath(id string) string {
	att, ok := m.Get(id)
	if !ok {
		return ""
	}

	return m.objectPath(att.ID)
}

// RetrievalStats carries the measured outcomes of one retrieval call —
// every field is a real count, never an estimate (Phase 3 instrumentation).
type RetrievalStats struct {
	AttachmentsConsidered int    `json:"attachmentsConsidered"`
	ChunksConsidered      int    `json:"chunksConsidered"`
	ChunksSelected        int    `json:"chunksSelected"`
	BytesComposed         int    `json:"bytesComposed"`
	ObjectReads           int    `json:"objectReads"`
	ObjectBytesRead       int64  `json:"objectBytesRead"`
	CacheSheds            uint64 `json:"cacheSheds"`
	CacheHit              bool   `json:"cacheHit"`
}

// Retrieve composes a bounded, provenance-tagged block of the most
// relevant chunks across the given attachments for one query. Images are
// skipped (they ride the vision pipeline); binaries contribute their note.
// Results are cached content-aware; an empty result returns "".
func (m *Manager) Retrieve(
	ctx context.Context,
	query string,
	ids []string,
	budgetBytes int,
) string {
	block, _ := m.RetrieveWithStats(ctx, query, ids, budgetBytes)
	return block
}

// RetrieveWithStats is Retrieve plus the measured RetrievalStats.
//
// v1.1.5 Phase 3: object text is read from disk AT MOST ONCE per
// attachment per call and reused for every selected chunk (previously the
// whole object was re-read per selected chunk — N chunks meant N full
// reads). Objects larger than the per-call retention cap degrade to
// per-chunk byte-range reads instead of being pinned in memory.
func (m *Manager) RetrieveWithStats(
	ctx context.Context,
	query string,
	ids []string,
	budgetBytes int,
) (string, RetrievalStats) {
	stats := RetrievalStats{}

	if budgetBytes <= 0 {
		budgetBytes = m.limits.RetrievalBudget
	}

	type scored struct {
		att   *Attachment
		chunk Chunk
		score float64
	}

	terms := tokenize(query)
	var outAttachments []*Attachment

	for _, id := range ids {
		if att, ok := m.Get(id); ok {
			outAttachments = append(outAttachments, att)
		}
	}

	stats.AttachmentsConsidered = len(outAttachments)

	if len(outAttachments) == 0 {
		return "", stats
	}

	// Deterministic cache key: sorted ids + query hash + version + budget.
	sortedIDs := make([]string, len(ids))
	copy(sortedIDs, ids)
	sort.Strings(sortedIDs)

	qHash := contextcache.ContentHash([]byte(strings.ToLower(query)))

	key := contextcache.Key(
		"attachments:retrieve",
		strings.Join(sortedIDs, ","),
		qHash,
		m.version,
		fmt.Sprintf("b%d", budgetBytes),
	)

	if cached, ok := m.cache.Get(key); ok {
		if block, ok := cached.(string); ok {
			stats.CacheHit = true
			stats.BytesComposed = len(block)
			return block, stats
		}
	}

	var candidates []scored

	for _, att := range outAttachments {
		switch att.Kind {
		case KindImage:
			continue
		case KindBinary:
			continue
		}

		for _, ch := range att.Chunks {
			if ctx.Err() != nil {
				break
			}

			stats.ChunksConsidered++

			preview := ch.Preview
			score := scoreChunk(terms, preview, ch)

			if score > 0 {
				candidates = append(candidates, scored{att: att, chunk: ch, score: score})
			}
		}
	}

	// Highest score first; ties broken by attachment then index for
	// determinism.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}

		if candidates[i].att.ID != candidates[j].att.ID {
			return candidates[i].att.ID < candidates[j].att.ID
		}

		return candidates[i].chunk.Index < candidates[j].chunk.Index
	})

	// Per-call object source: each attachment's object is read at most
	// once, bounded by the retention cap (larger objects fall back to
	// per-chunk byte-range reads).
	src := &objectSource{m: m, cap: retrieveObjectCacheCap, bytes: map[string][]byte{}}
	defer src.release()

	var b strings.Builder
	used := 0

	writeAttHeader := func(att *Attachment) {
		fmt.Fprintf(
			&b,
			"----- attachment: %s (%s, %s) -----\n",
			att.Name,
			att.Kind,
			humanize.Bytes(att.Size),
		)
	}

	written := make(map[string]bool)

	for _, c := range candidates {
		if ctx.Err() != nil {
			break
		}

		full := src.chunkText(c.att, c.chunk)
		if full == "" {
			continue
		}

		need := len(full) + len(c.att.Name) + 80
		if used+need > budgetBytes {
			// Try a truncated tail of this chunk if we still have room.
			remain := budgetBytes - used

			if remain > len(full) {
				remain = len(full)
			}

			if remain > 256 {
				full = full[:remain]
				need = remain
			} else {
				break
			}
		}

		if !written[c.att.ID] {
			writeAttHeader(c.att)
			written[c.att.ID] = true
		}

		fmt.Fprintf(&b, "[chunk %d · %s]\n%s\n\n", c.chunk.Index, c.chunk.Hash[:8], full)
		used += need
		stats.ChunksSelected++
	}

	// If nothing scored, still surface a compact metadata block so the
	// model knows attachments exist (bounded to two files' headers).
	if b.Len() == 0 {
		n := 0

		for _, att := range outAttachments {
			if att.Kind == KindImage {
				continue
			}

			writeAttHeader(att)
			att2 := m.objectPath(att.ID)
			fmt.Fprintf(&b, "full text available at %s\n\n", att2)
			n++
			used += 200

			if n >= 2 || used >= budgetBytes {
				break
			}
		}

		if b.Len() == 0 {
			return "", stats
		}
	}

	block := strings.TrimSpace(b.String())
	stats.BytesComposed = len(block)
	stats.ObjectReads = src.reads
	stats.ObjectBytesRead = src.bytesRead
	stats.CacheSheds = src.sheds

	// Fold the per-call degradation counts into the manager's resource
	// accounting (measured counters only).
	m.res.cacheSheds.Add(src.sheds)

	m.cache.Put(key, block, int64(len(block)), time.Minute)

	return block, stats
}

// objectSource serves chunk text for one retrieval call. Each object is
// read at most once (whole-file, reused for every chunk of that
// attachment) while the retained bytes stay under cap; over the cap it
// degrades to exact byte-range reads — correct, just more syscalls.
type objectSource struct {
	m         *Manager
	cap       int64
	bytes     map[string][]byte
	retained  int64
	reads     int
	bytesRead int64
	sheds     uint64
}

// chunkText returns the exact byte range of one chunk. Falls back to the
// preview when the object vanished or the stored range no longer exists
// (the cache entry is invalidated so stale previews stop circulating) —
// the same fallback semantics the pre-Phase-3 path had.
func (o *objectSource) chunkText(att *Attachment, ch Chunk) string {
	if data, ok := o.bytes[att.ID]; ok {
		return sliceChunk(data, ch, ch.Preview)
	}

	if att.Size <= o.cap-o.retained {
		data, err := o.m.readObject(att.ID)
		o.reads++
		if err != nil {
			o.m.cache.Invalidate(
				contextcache.Key("attachments:chunks", att.SHA256, o.m.version),
			)
			return ch.Preview
		}
		o.bytesRead += int64(len(data))

		// Retain only while it fits; large objects stream per chunk below.
		if int64(len(data)) <= o.cap-o.retained {
			o.bytes[att.ID] = data
			o.retained += int64(len(data))
		}

		return sliceChunk(data, ch, ch.Preview)
	}

	// Over the retention cap: read exactly the chunk's byte range.
	text, ok := o.m.readObjectRange(att.ID, ch.Offset, ch.Bytes)
	o.reads++
	o.bytesRead += int64(len(text))
	if !ok || text == "" {
		o.m.cache.Invalidate(
			contextcache.Key("attachments:chunks", att.SHA256, o.m.version),
		)
		o.sheds++
		return ch.Preview
	}

	o.sheds++
	return text
}

// release drops the retained object buffers (called at the end of the
// retrieval call — nothing large outlives the request).
func (o *objectSource) release() {
	for k := range o.bytes {
		delete(o.bytes, k)
	}
	o.retained = 0
}

// sliceChunk extracts one chunk range from a loaded object; out-of-range
// offsets return the provided fallback (the chunk preview).
func sliceChunk(data []byte, ch Chunk, fallback string) string {
	if ch.Offset >= len(data) {
		return fallback
	}

	end := ch.Offset + ch.Bytes
	if end > len(data) {
		end = len(data)
	}

	return string(data[ch.Offset:end])
}

// readObject reads the stored object (counted).
func (m *Manager) readObject(id string) ([]byte, error) {
	m.res.objectReads.Add(1)

	data, err := os.ReadFile(m.objectPath(id))
	if err == nil {
		m.res.readBytes.Add(uint64(len(data)))
	}

	return data, err
}

// readObjectRange reads exactly [offset, offset+length) from the stored
// object (counted). ok=false when the object is missing.
func (m *Manager) readObjectRange(id string, offset, length int) (string, bool) {
	f, err := os.Open(m.objectPath(id))
	if err != nil {
		return "", false
	}
	defer f.Close()

	if offset < 0 || length <= 0 {
		return "", false
	}

	buf := make([]byte, length)
	n, err := io.ReadFull(io.NewSectionReader(f, int64(offset), int64(length)), buf)
	m.res.objectReads.Add(1)

	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", false
	}

	m.res.readBytes.Add(uint64(n))
	return string(buf[:n]), true
}

// chunkText reads the exact byte range of one chunk from the stored
// object (single-chunk convenience path: a range read — the whole object
// is NOT loaded for one chunk). Falls back to the preview when the object
// vanished (the cache entry is invalidated so stale previews stop
// circulating).
func (m *Manager) chunkText(att *Attachment, ch Chunk) string {
	text, ok := m.readObjectRange(att.ID, ch.Offset, ch.Bytes)
	if !ok {
		m.cache.Invalidate(
			contextcache.Key("attachments:chunks", att.SHA256, m.version),
		)
		return ch.Preview
	}

	return text
}

// --- persistence ----------------------------------------------------------

func (m *Manager) saveMeta(att *Attachment) error {
	data, err := json.Marshal(att)
	if err != nil {
		return err
	}

	return writeAtomic(m.metaPath(att.ID), data)
}

func (m *Manager) loadMeta(id string) (*Attachment, error) {
	data, err := os.ReadFile(m.metaPath(id))
	if err != nil {
		return nil, err
	}

	var att Attachment
	if err := json.Unmarshal(data, &att); err != nil {
		return nil, fmt.Errorf("attachments: corrupt meta %s: %w", id, err)
	}

	return &att, nil
}

func (m *Manager) loadAll() {
	m.mu.Lock()
	loaded := m.loaded
	m.loaded = true
	m.mu.Unlock()

	if loaded {
		return
	}

	entries, err := os.ReadDir(filepath.Join(m.dir, "meta"))
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		id := strings.TrimSuffix(e.Name(), ".json")

		if att, err := m.loadMeta(id); err == nil {
			m.mu.Lock()
			m.metas[id] = att
			m.mu.Unlock()
		}
	}
}

// --- pure helpers ---------------------------------------------------------

// SanitizeName reduces an uploaded filename to a safe display name: the
// base name only (both separator styles), control characters stripped,
// length capped.
func SanitizeName(name string) string {
	name = strings.TrimSpace(name)

	// Normalize Windows-style separators first so "..\dir\file.exe" and
	// "../dir/file.exe" both reduce to their base name.
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))

	name = strings.Map(func(r rune) rune {
		switch {
		case r < 32 || r == 127:
			return -1
		case r == '/' || r == '\\':
			return '_'
		default:
			return r
		}
	}, name)

	name = strings.TrimSpace(name)

	const maxName = 200
	if utf8.RuneCountInString(name) > maxName {
		runes := []rune(name)
		name = string(runes[:maxName])
	}

	if name == "" || name == "." || name == ".." {
		name = "attachment"
	}

	return name
}

// classify decides text/image/binary from the name extension and a
// content sniff (a renamed .exe must not pass as text).
func classify(name string, data []byte) Kind {
	if vision.IsImageFile(name) {
		return KindImage
	}

	if looksBinary(data) {
		return KindBinary
	}

	if chunking.IsKnownTextExt(name) || isUTF8ish(data) {
		return KindText
	}

	return KindBinary
}

// looksBinary reports whether the head of the content has binary
// signatures (NUL runs).
func looksBinary(data []byte) bool {
	head := data
	if len(head) > 8192 {
		head = head[:8192]
	}

	for _, b := range head {
		if b == 0 {
			return true
		}
	}

	return false
}

// isUTF8ish tolerates mostly-valid UTF-8 (log files with a stray byte).
func isUTF8ish(data []byte) bool {
	head := data
	if len(head) > 16384 {
		head = head[:16384]
	}

	if utf8.Valid(head) {
		return true
	}

	bad, total := 0, 0

	for _, r := range string(head) {
		total++
		if r == utf8.RuneError {
			bad++
		}
	}

	return total > 0 && bad*10 < total
}

// NormalizeText converts staged bytes into prompt-ready text: invalid
// UTF-8 replaced, BOM dropped, CRLF normalized, trailing newlines trimmed.
//
// v1.1.5 Phase 3: the clean case (valid UTF-8, no BOM, no CR) returns the
// ORIGINAL bytes without a single full-content copy — previously every
// staged text paid up to three copies (ToValidUTF8 + two ReplaceAll) even
// when nothing needed replacing.
func NormalizeText(data []byte) string {
	b := data

	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}

	if utf8.Valid(b) && bytes.IndexByte(b, '\r') < 0 {
		return strings.TrimRight(string(b), "\n")
	}

	s := strings.ToValidUTF8(string(b), "\uFFFD")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	return strings.TrimRight(s, "\n")
}

// tokenize lowercases and splits a query into overlap terms.
func tokenize(q string) []string {
	fields := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127)
	})

	out := make([]string, 0, len(fields))

	for _, f := range fields {
		if len(f) >= 2 {
			out = append(out, f)
		}
	}

	return out
}

// scoreChunk ranks one chunk against the query terms using the preview
// text plus term frequency. Term overlap in a 120-char preview is a cheap
// first-pass relevance signal; the full chunk text is not scanned here to
// keep retrieval O(chunks) with no I/O (chunk text is only read for
// selected chunks). Lexical overlap stays deterministic and offline-safe.
func scoreChunk(terms []string, preview string, ch Chunk) float64 {
	if len(terms) == 0 {
		return 0
	}

	hay := strings.ToLower(preview)

	score := 0.0

	for _, t := range terms {
		if strings.Contains(hay, t) {
			score += float64(len(t))
		}
	}

	if score == 0 {
		return 0
	}

	// Earlier chunks carry headers/titles — slight boost.
	score += float64(maxChunksBoost-ch.Index) * 0.01

	// Larger chunks carry more evidence.
	score += float64(ch.Bytes) / 4096.0

	return score
}

const maxChunksBoost = 16

func shaOfID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}

	return string([]rune(s)[:n]) + "…"
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}
