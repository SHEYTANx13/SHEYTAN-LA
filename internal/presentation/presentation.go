// Package presentation is the ONE versioned presentation contract between
// the Go runtime (the only block authority) and the rendering surfaces
// (the Fyne native shell and the on-demand Web Workspace).
//
// Architecture position (v1.11.0 hybrid UI):
//
//	Go runtime authorities (runs, sessions, tools, artifacts)
//	      ↓  issues typed Blocks (this package)
//	presentation.Store (bounded, Go-owned)
//	      ↓  serves snapshots
//	Fyne native shell (always renders first, never depends on the web)
//	Web Workspace   (on-demand rich rendering, internal/workspace)
//	3D Studio       (read-only topology, internal/studio — unchanged)
//
// Invariants:
//   - One Block ID maps to ONE logical artifact (upsert semantics).
//   - Payloads are bounded and kind-validated; invalid content is refused
//     with a typed error — the native shell keeps showing the full native
//     rendering, so nothing user-visible is lost when a block is refused.
//   - Unknown kinds never crash a session: they are accepted into the
//     store flagged with a Degrade record and rendered as safe raw data.
//   - Surface selection is deterministic and capability/kind-driven —
//     never model-directed, never per-token re-decided.
//   - Model-generated content is DATA, never trusted HTML. Renderers must
//     treat every payload byte as text (DOM textContent on the web side).
package presentation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Version is the envelope version. A Block with V != Version is refused:
// silent reinterpretation of an older envelope is exactly the class of
// drift this contract exists to prevent.
const Version = 1

// Kind is the typed content kind of a Block.
type Kind string

const (
	KindText     Kind = "text"
	KindStatus   Kind = "status"
	KindMarkdown Kind = "markdown"
	KindCode     Kind = "code"
	KindDiff     Kind = "diff"
	KindTable    Kind = "table"
	KindChart    Kind = "chart-spec"
	KindArtifact Kind = "artifact"
	Kind3D       Kind = "3d"
)

// Surface is a rendering surface of the hybrid shell.
type Surface string

const (
	SurfaceNative    Surface = "native"    // Fyne shell (always available)
	SurfaceWorkspace Surface = "workspace" // on-demand Web Workspace
	SurfaceStudio    Surface = "studio"    // read-only 3D Studio
)

// Payload bounds. They keep a single block from pinning unbounded memory
// in the store or flooding the browser channel; larger content belongs in
// the artifact authority, referenced by Path.
const (
	MaxTextPayload = 512 << 10 // 512 KiB for text-like kinds
	MaxJSONPayload = 256 << 10 // 256 KiB for structured kinds
	MaxMetaPayload = 4 << 10   // 4 KiB for reference-only kinds
	MaxIDLen       = 128
	MaxTitleLen    = 200
	MaxSourceLen   = 128
	MaxTableCells  = 5000 // total cells bound
	MaxTableRowLen = 200  // columns per row
	MaxTableRows   = 1000
)

// Typed validation errors (errors.Is-able).
var (
	ErrEnvelopeVersion = errors.New("presentation: unsupported envelope version")
	ErrID              = errors.New("presentation: invalid block id")
	ErrKind            = errors.New("presentation: unknown kind")
	ErrPayload         = errors.New("presentation: invalid payload")
	ErrTitle           = errors.New("presentation: invalid title")
	ErrSource          = errors.New("presentation: invalid source")
	ErrArtifact        = errors.New("presentation: artifact reference required")
)

// Degrade carries the typed reason a block will NOT receive its native
// rich rendering in the Workspace and instead degrades to the safe
// generic/raw-data state. Unknown kinds degrade; they never fail the run.
type Degrade struct {
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// Typed degrade reasons.
const (
	DegradeUnknownKind   = "kind-unknown"
	DegradeOversize      = "payload-oversize"
	DegradeMalformedJSON = "payload-malformed-json"
)

// Block is the versioned presentation envelope. It is a value object:
// producers construct it, Validate canonicalizes it, the Store keeps it.
type Block struct {
	V         int       `json:"v"`
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	Title     string    `json:"title,omitempty"`
	RunID     string    `json:"runId,omitempty"`
	SessionID string    `json:"sessionId,omitempty"`
	Source    string    `json:"source,omitempty"` // provenance: the producing authority
	CreatedAt time.Time `json:"createdAt,omitempty"`

	// Encoding: "utf8" for text payloads, "json" for structured payloads.
	// It is set by Validate — producers never hand-annotate it.
	Encoding string `json:"encoding,omitempty"`

	// Payload is the bounded content. Text kinds carry raw UTF-8 bytes;
	// structured kinds carry JSON. Never executable markup on the web.
	Payload []byte `json:"payload,omitempty"`

	// Path references content living in the artifact authority instead of
	// inlining it (required for KindArtifact, optional elsewhere).
	Path string `json:"path,omitempty"`

	// Capabilities lists optional rendering affordances the payload was
	// produced for (e.g. "math", "mermaid"). Advisory: surfaces must
	// render safely without honoring any of them.
	Capabilities []string `json:"capabilities,omitempty"`

	// Degrade is set by Validate for unknown kinds. A degraded block
	// renders as safe raw data, never as a blank or crashed session.
	Degrade *Degrade `json:"degrade,omitempty"`
}

// KnownKind reports whether k is one of the contract kinds.
func KnownKind(k Kind) bool {
	switch k {
	case KindText, KindStatus, KindMarkdown, KindCode, KindDiff,
		KindTable, KindChart, KindArtifact, Kind3D:
		return true
	default:
		return false
	}
}

// SelectSurface is the deterministic renderer policy. The Fyne shell
// ALWAYS renders first regardless of the result — this function only
// decides which surface is preferred for the rich rendering, and it is a
// pure function of the kind: no model output, no per-token state, no
// hidden preference can change it.
func SelectSurface(k Kind) Surface {
	if !KnownKind(k) {
		return SurfaceNative // unknown kinds stay in the safe native shell
	}
	switch k {
	case KindMarkdown, KindCode, KindDiff, KindTable, KindChart:
		return SurfaceWorkspace
	case Kind3D:
		return SurfaceStudio
	default:
		return SurfaceNative
	}
}

func validID(id string) bool {
	if id == "" || len(id) > MaxIDLen {
		return false
	}
	for _, r := range id {
		if r < 0x21 || r > 0x7e {
			return false // printable ASCII, no spaces — URL-path safe
		}
	}
	return true
}

// Validate checks and canonicalizes b in place: it stamps V, Encoding and
// CreatedAt, and (for unknown kinds) sets the Degrade record. It returns
// a typed error for content the contract refuses; refusing is safe
// because the native shell renders content independently of blocks.
func (b *Block) Validate() error {
	b.V = Version

	if !validID(b.ID) {
		return fmt.Errorf("%w: %q (must be 1..%d printable ASCII, no spaces)", ErrID, b.ID, MaxIDLen)
	}
	if len(b.Title) > MaxTitleLen {
		return fmt.Errorf("%w: %d bytes > %d", ErrTitle, len(b.Title), MaxTitleLen)
	}
	if len(b.Source) > MaxSourceLen {
		return fmt.Errorf("%w: %d bytes > %d", ErrSource, len(b.Source), MaxSourceLen)
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now().UTC()
	}

	switch b.Kind {
	case KindText, KindStatus, KindMarkdown, KindCode, KindDiff:
		if len(b.Payload) > MaxTextPayload {
			return fmt.Errorf("%w: %d bytes > %d for kind %q", ErrPayload, len(b.Payload), MaxTextPayload, b.Kind)
		}
		if !utf8.Valid(b.Payload) && len(b.Payload) > 0 {
			return fmt.Errorf("%w: kind %q requires valid UTF-8", ErrPayload, b.Kind)
		}
		b.Encoding = "utf8"
	case KindTable, KindChart:
		if len(b.Payload) > MaxJSONPayload {
			return fmt.Errorf("%w: %d bytes > %d for kind %q", ErrPayload, len(b.Payload), MaxJSONPayload, b.Kind)
		}
		if err := validateStructured(b.Kind, b.Payload); err != nil {
			return err
		}
		b.Encoding = "json"
	case KindArtifact:
		if strings.TrimSpace(b.Path) == "" {
			return ErrArtifact
		}
		if len(b.Payload) > MaxMetaPayload {
			return fmt.Errorf("%w: artifact blocks carry metadata only (%d > %d bytes) — reference the content via Path", ErrPayload, len(b.Payload), MaxMetaPayload)
		}
		b.Encoding = "utf8"
	case Kind3D:
		// The Studio owns 3D rendering; a block only announces the view.
		if len(b.Payload) > MaxMetaPayload {
			return fmt.Errorf("%w: %d bytes > %d for kind %q", ErrPayload, len(b.Payload), MaxMetaPayload, b.Kind)
		}
		b.Encoding = "utf8"
	default:
		// Unknown kind: accept as bounded raw data with an explicit Degrade
		// record. This is the contract's safe-degradation path — a future
		// producer kind must never blank or crash an existing session.
		if len(b.Payload) > MaxJSONPayload {
			return fmt.Errorf("%w: unknown-kind raw payload %d > %d bytes", ErrPayload, len(b.Payload), MaxJSONPayload)
		}
		if !utf8.Valid(b.Payload) && len(b.Payload) > 0 {
			return fmt.Errorf("%w: raw payload must be valid UTF-8", ErrPayload)
		}
		b.Encoding = "utf8"
		if b.Degrade == nil {
			b.Degrade = &Degrade{
				Reason: DegradeUnknownKind,
				Detail: fmt.Sprintf("kind %q is not part of envelope v%d; rendering as raw data", string(b.Kind), Version),
			}
		}
	}
	return nil
}

// validateStructured enforces the shape of the JSON kinds:
//   - table: {"columns":[...], "rows":[[...]]} within the cell/row bounds
//   - chart-spec: any JSON object (schema owned by the producing tool),
//     bounded by MaxJSONPayload
func validateStructured(k Kind, p []byte) error {
	if !json.Valid(p) {
		return fmt.Errorf("%w: kind %q payload is not valid JSON", ErrPayload, k)
	}
	if k != KindTable {
		return nil
	}
	var t struct {
		Columns []string          `json:"columns"`
		Rows    []json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(p, &t); err != nil {
		return fmt.Errorf("%w: table payload: %v", ErrPayload, err)
	}
	if len(t.Columns) > MaxTableRowLen {
		return fmt.Errorf("%w: table has %d columns > %d", ErrPayload, len(t.Columns), MaxTableRowLen)
	}
	if len(t.Rows) > MaxTableRows {
		return fmt.Errorf("%w: table has %d rows > %d", ErrPayload, len(t.Rows), MaxTableRows)
	}
	cells := 0
	for i, r := range t.Rows {
		var row []any
		if err := json.Unmarshal(r, &row); err != nil {
			return fmt.Errorf("%w: table row %d is not an array", ErrPayload, i)
		}
		cells += len(row)
		if cells > MaxTableCells {
			return fmt.Errorf("%w: table exceeds %d total cells", ErrPayload, MaxTableCells)
		}
	}
	return nil
}

// Meta is the bounded block descriptor the store's List returns and the
// Workspace index serves. It never carries payload bytes.
type Meta struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	Title     string    `json:"title,omitempty"`
	RunID     string    `json:"runId,omitempty"`
	SessionID string    `json:"sessionId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	Bytes     int       `json:"bytes"`
	Degrade   *Degrade  `json:"degrade,omitempty"`
}

// MetaOf projects the bounded descriptor of a block.
func MetaOf(b Block) Meta {
	return Meta{
		ID:        b.ID,
		Kind:      b.Kind,
		Title:     b.Title,
		RunID:     b.RunID,
		SessionID: b.SessionID,
		CreatedAt: b.CreatedAt,
		Bytes:     len(b.Payload),
		Degrade:   b.Degrade,
	}
}
