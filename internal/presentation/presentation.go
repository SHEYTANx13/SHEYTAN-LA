// Package presentation — the v1.11.0 typed presentation contract.
//
// ONE Go-owned authority for rich content blocks shared by the two
// rendering surfaces (the native Fyne shell and the on-demand Web
// Workspace). The contract is deliberately small and versioned:
//
//   - A Block is an IMMUTABLE typed envelope: stable content ID, kind,
//     bounded safe payload (or artifact reference), provenance metadata
//     and capabilities. One ID maps to one logical artifact forever.
//   - Model-generated content is NEVER trusted HTML: there is no HTML
//     kind. Renderers treat every payload as text and build DOM (or
//     widgets) safely; unknown kinds degrade to a raw-data state, never
//     a blank or crashed session.
//   - Renderer selection (Select) is a DETERMINISTIC pure function of
//     the kind — the model cannot direct where its output renders.
//
// Both surfaces read the same Registry (served by the core); neither
// owns a second store. This package is pure Go with zero dependencies
// outside the standard library so it can be used by the API server and
// the UI client without pulling in a GUI stack.
package presentation

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// EnvelopeVersion is the version of the serialized envelope contract.
// Renderers reject envelopes whose version they do not understand and
// degrade to the raw-data state instead of guessing.
const EnvelopeVersion = 1

// Kind — the closed set of block kinds. The set is intentionally small:
// every kind must have an owner in both renderers (render, or the
// documented degradation path).
const (
	KindText      = "text"      // short status/prose; native shell renders natively
	KindStatus    = "status"    // lifecycle/status line (preparing, done, …)
	KindMarkdown  = "markdown"  // long-form prose rendered by the Workspace (safe subset)
	KindCode      = "code"      // source code with a language hint
	KindDiff      = "diff"      // unified diff
	KindTable     = "table"     // tabular data (header row in meta)
	KindChartSpec = "chartspec" // deterministic chart spec (kind + bounded series), not model HTML
	KindArtifact  = "artifact"  // reference to an artifact produced by a tool
	KindScene3D   = "scene3d"   // pointer at the read-only 3D Studio topology
)

// Capabilities a renderer may declare/require. Selection uses these, not
// model steering. CapMath and CapCitations are RESERVED names: math
// typesetting and citation rendering are PLANNED capabilities and are
// honestly degraded to text today — they are never claimed as shipped.
const (
	CapStreaming = "streaming"
	CapMath      = "math"
	CapCitations = "citations"
)

// Bounds — every field is bounded so a hostile or runaway producer can
// never balloon either renderer. Exceeding a bound is a typed error at
// publish time (fail closed), never a silent truncation.
const (
	MaxPayloadBytes = 256 << 10 // 256 KiB per block payload
	MaxTitleBytes   = 200
	MaxMetaEntries  = 16
	MaxMetaValueLen = 500
	MaxIDLen        = 64
	MaxBlocks       = 200 // registry bound; eviction is oldest-first and reported
)

// Block is one immutable typed content unit.
type Block struct {
	ID        string            `json:"id"`
	Version   int               `json:"version"` // envelope version of THIS block
	Kind      string            `json:"kind"`
	Title     string            `json:"title,omitempty"`
	Payload   string            `json:"payload,omitempty"`
	Language  string            `json:"language,omitempty"` // code/diff language hint
	RunID     string            `json:"runId,omitempty"`
	SessionID string            `json:"sessionId,omitempty"`
	Source    string            `json:"source"` // provenance: who produced this (required)
	CreatedAt string            `json:"createdAt"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// Validate checks the block against the contract. It returns a typed
// error naming the violated bound. Blocks that arrive with an UNKNOWN
// kind are not rejected by the registry (a newer producer must not be
// bricked by an older renderer) — they are DEGRADED by Degraded into
// a raw-data block the current renderers can always show.
func (b Block) Validate() error {
	if strings.TrimSpace(b.Source) == "" {
		return fmt.Errorf("presentation: block %q has no provenance source", b.ID)
	}
	if len(b.ID) > MaxIDLen {
		return fmt.Errorf("presentation: id exceeds %d bytes", MaxIDLen)
	}
	if len(b.Title) > MaxTitleBytes {
		return fmt.Errorf("presentation: title exceeds %d bytes", MaxTitleBytes)
	}
	if len(b.Payload) > MaxPayloadBytes {
		return fmt.Errorf("presentation: payload exceeds %d bytes (kind %s)", MaxPayloadBytes, b.Kind)
	}
	if len(b.Meta) > MaxMetaEntries {
		return fmt.Errorf("presentation: meta exceeds %d entries", MaxMetaEntries)
	}
	for k, v := range b.Meta {
		if len(k) > MaxMetaValueLen || len(v) > MaxMetaValueLen {
			return fmt.Errorf("presentation: meta entry %q exceeds %d bytes", k, MaxMetaValueLen)
		}
	}
	return nil
}

// IsKnownKind reports whether the kind is part of THIS envelope version's
// closed set. Unknown kinds from a future producer degrade safely.
func IsKnownKind(kind string) bool {
	switch kind {
	case KindText, KindStatus, KindMarkdown, KindCode, KindDiff,
		KindTable, KindChartSpec, KindArtifact, KindScene3D:
		return true
	}
	return false
}

// Degraded maps a block of unknown kind onto the safe raw-data state:
// the payload is preserved verbatim as an artifact-typed block and the
// original kind is recorded in meta so nothing is silently dropped.
func Degraded(b Block) Block {
	if IsKnownKind(b.Kind) {
		return b
	}
	out := b
	out.Meta = ensureMeta(out.Meta)
	out.Meta["originalKind"] = b.Kind
	out.Meta["degraded"] = "unknown kind rendered as raw data"
	out.Kind = KindArtifact
	return out
}

func ensureMeta(m map[string]string) map[string]string {
	if m == nil {
		m = make(map[string]string, 2)
	}
	return m
}

// Sanitize strips NUL bytes (the one control character that corrupts
// both renderers' text pipelines) and nothing else. Everything else
// passes through byte-identical: renderers own escaping.
func Sanitize(payload string) string {
	if !strings.ContainsRune(payload, 0) {
		return payload
	}
	return strings.ReplaceAll(payload, "\x00", "")
}

// Select is the DETERMINISTIC renderer policy: which surface renders a
// block. It is a pure function of the kind — the model cannot steer it.
//
// Native shell (Fyne): short text and status lines — instant, low-cost,
// no browser round-trip.
// Web Workspace: kinds that benefit from browser-grade rendering (long
// markdown, code, diffs, tables, chart specs, artifact previews, 3D).
// Unknown kinds route to the Workspace's raw-data state (the richest
// degradation surface); the native shell never crashes on them because
// it never receives them via this policy.
func Select(kind string) string {
	switch kind {
	case KindText, KindStatus:
		return "native"
	default:
		// markdown, code, diff, table, chartspec, artifact, scene3d and
		// every unknown kind → the Workspace (or its raw-data state).
		return "workspace"
	}
}

// Registry — the ONE store of published blocks. Thread-safe, bounded
// (MaxBlocks, oldest-first eviction reported in Stats), IDs immutable.
type Registry struct {
	mu      sync.Mutex
	blocks  []Block
	byID    map[string]int
	seq     int
	evicted int
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]int)}
}

// Publish validates, sanitizes, degrades (if needed), assigns a stable
// ID and stores the block. The stored block is returned; its ID is the
// identity both surfaces use forever. Publish never mutates an existing
// entry: re-rendering content is a NEW publish with a NEW id.
func (r *Registry) Publish(b Block) (Block, error) {
	b.ID = ""
	return r.publish(b)
}

// PublishWithID stores a block under a caller-provided stable ID (used
// when identity must be shared across a surface boundary). A duplicate
// ID is a typed error — identity is never silently remapped.
func (r *Registry) PublishWithID(b Block, id string) (Block, error) {
	if strings.TrimSpace(id) == "" {
		return Block{}, fmt.Errorf("presentation: empty block id")
	}
	if len(id) > MaxIDLen {
		return Block{}, fmt.Errorf("presentation: id exceeds %d bytes", MaxIDLen)
	}
	b.ID = id
	return r.publish(b)
}

func (r *Registry) publish(b Block) (Block, error) {
	b.Payload = Sanitize(b.Payload)
	b.Kind = strings.ToLower(strings.TrimSpace(b.Kind))
	if b.Kind == "" {
		b.Kind = KindText
	}
	b.Version = EnvelopeVersion
	b.Source = strings.TrimSpace(b.Source)
	if err := b.Validate(); err != nil {
		return Block{}, err
	}
	b = Degraded(b)
	if b.CreatedAt == "" {
		b.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if b.ID == "" {
		r.seq++
		id, err := newID(r.seq)
		if err != nil {
			return Block{}, err
		}
		b.ID = id
	} else if _, dup := r.byID[b.ID]; dup {
		return Block{}, fmt.Errorf("presentation: block id %q already exists", b.ID)
	}
	if len(r.blocks) >= MaxBlocks {
		// Bounded eviction: oldest first, counted (never silent).
		oldest := r.blocks[0]
		delete(r.byID, oldest.ID)
		r.blocks = r.blocks[1:]
		r.evicted++
	}
	r.blocks = append(r.blocks, b)
	r.byID[b.ID] = len(r.blocks) - 1
	return b, nil
}

// newID derives a stable, collision-resistant content ID:
// monotonic sequence + 8 random bytes (envelope-local identity only —
// it never claims to be a global UUID).
func newID(seq int) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("presentation: id entropy: %w", err)
	}
	return fmt.Sprintf("b-%06d-%s", seq, hex.EncodeToString(buf)), nil
}

// Get returns the block with the exact ID.
func (r *Registry) Get(id string) (Block, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, ok := r.byID[id]
	if !ok {
		return Block{}, false
	}
	return r.blocks[i], true
}

// List returns up to limit newest blocks (newest first, stable order).
func (r *Registry) List(limit int) []Block {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit > len(r.blocks) {
		limit = len(r.blocks)
	}
	out := make([]Block, 0, limit)
	for i := len(r.blocks) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, r.blocks[i])
	}
	return out
}

// Stats reports honest registry facts for diagnostics.
type Stats struct {
	Published int `json:"published"`
	Live      int `json:"live"`
	Evicted   int `json:"evicted"`
}

func (r *Registry) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Stats{Published: r.evicted + len(r.blocks), Live: len(r.blocks), Evicted: r.evicted}
}

// ExtractBlocks deterministically splits a finished assistant answer
// into typed blocks: fenced code blocks become kindCode (with the fence
// language when present) and the prose between fences becomes
// kindMarkdown. This is the v1.11.0 vertical-slice producer — plain
// text in, typed blocks out, no model steering, byte-deterministic for
// the same input. An answer with no fences yields exactly one markdown
// block (when prose exists). Empty input yields no blocks.
func ExtractBlocks(answer, runID, sessionID, source string) []Block {
	if strings.TrimSpace(answer) == "" {
		return nil
	}
	var out []Block
	var prose strings.Builder
	lines := strings.Split(answer, "\n")
	i := 0
	flushProse := func() {
		p := strings.TrimSpace(prose.String())
		prose.Reset()
		if p == "" {
			return
		}
		out = append(out, Block{
			Kind: KindMarkdown, Payload: p, RunID: runID, SessionID: sessionID, Source: source,
			Title: "Answer",
		})
	}
	for i < len(lines) {
		line := lines[i]
		if fence, ok := codeFence(line); ok {
			flushProse()
			var body strings.Builder
			i++
			for i < len(lines) && !codeFenceEnd(lines[i]) {
				body.WriteString(lines[i])
				body.WriteString("\n")
				i++
			}
			payload := body.String()
			out = append(out, Block{
				Kind: KindCode, Language: fence, Payload: payload, RunID: runID,
				SessionID: sessionID, Source: source, Title: "Code" + fenceTitle(fence),
			})
			i++ // skip the closing fence line (or EOF)
			continue
		}
		prose.WriteString(line)
		prose.WriteString("\n")
		i++
	}
	flushProse()
	return out
}

// codeFence reports whether the line opens a fenced block and returns
// the fence language ("" when bare). Only ``` at line start (after
// optional whitespace) opens a fence — the common Markdown subset the
// extraction contract owns.
func codeFence(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "```") {
		return "", false
	}
	lang := strings.TrimSpace(strings.TrimPrefix(t, "```"))
	return lang, true
}

// codeFenceEnd reports whether the line CLOSES a fence: a ``` line with
// nothing but backticks after trimming. A fence-opening line (with a
// language) is not a closer.
func codeFenceEnd(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "```") {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(t, "```")) == ""
}

func fenceTitle(lang string) string {
	if lang == "" {
		return ""
	}
	return " (" + lang + ")"
}
