package llm

// gguf.go — pure-Go GGUF header parser (v1.0.4).
//
// The model manager shows REAL model cards instead of bare filenames: the
// architecture, parameter count, quantization, training context length,
// and estimated memory footprint all live in the GGUF metadata block that
// starts at byte 0 of every GGUF file. Parsing needs no external library
// and only the first kilobytes-to-megabytes of the file (array values are
// SKIPPED, never materialized — tokenizer vocab arrays never enter RAM).
//
// Format reference: https://github.com/ggml-org/ggml/blob/master/docs/gguf.md

import (
        "bufio"
        "encoding/binary"
        "fmt"
        "io"
        "math"
        "os"
        "strings"
)

// gguf magic.
const ggufMagic = "GGUF"

// ggufMetadataReadLimit bounds how much of a GGUF file the header parser
// will read (v1.8.1: 32 MiB, up from 8 MiB — see ReadModelCard).
const ggufMetadataReadLimit = 32 << 20

// GGUF metadata value types.
const (
        ggufTypeUint8   = 0
        ggufTypeInt8    = 1
        ggufTypeUint16  = 2
        ggufTypeInt16   = 3
        ggufTypeUint32  = 4
        ggufTypeInt32   = 5
        ggufTypeFloat32 = 6
        ggufTypeBool    = 7
        ggufTypeString  = 8
        ggufTypeArray   = 9
        ggufTypeUint64  = 10
        ggufTypeInt64   = 11
        ggufTypeFloat64 = 12
)

// ModelCard is the parsed identity of one local .gguf file.
type ModelCard struct {
        Path          string
        File          string // base name
        SizeBytes     int64  // on-disk size
        Arch          string // llama, qwen2, gemma3, ...
        Name          string // general.name ("Qwen2.5 7B Instruct")
        Quant         string // Q4_K_M, Q8_0, F16, ...
        ParamsCount   uint64 // total parameters
        ContextLength int    // trained context length
        Layers        int    // transformer blocks
        EmbeddingLen  int    // hidden size
}

// BitsPerWeight returns average bits per parameter (model size vs params).
func (c *ModelCard) BitsPerWeight() float64 {
        if c == nil || c.ParamsCount == 0 || c.SizeBytes == 0 {
                return 0
        }
        return float64(c.SizeBytes) * 8 / float64(c.ParamsCount)
}

// FormatParameterCount renders "7.6B" / "472M" / "724K" from a raw
// parameter count (0 renders as "—"). Shared by the llama model cards
// and the native engine model info so both surfaces format identically.
func FormatParameterCount(n uint64) string {
        if n == 0 {
                return "—"
        }

        f := float64(n)
        switch {
        case f >= 1e9:
                return fmt.Sprintf("%.1fB", f/1e9)
        case f >= 1e6:
                return fmt.Sprintf("%dM", int64(f/1e6))
        default:
                return fmt.Sprintf("%dK", int64(f/1e3))
        }
}

// FormatParams renders "7.6B" / "472M" from ParamsCount.
func (c *ModelCard) FormatParams() string {
        if c == nil {
                return "—"
        }
        return FormatParameterCount(c.ParamsCount)
}

// Meta renders the compact card meta line: "7.6B · Q4_K_M · 32K ctx · 4.4 GB".
func (c *ModelCard) Meta() string {
        if c == nil {
                return ""
        }
        parts := []string{}
        if p := c.FormatParams(); p != "—" {
                parts = append(parts, p)
        }
        if c.Quant != "" {
                parts = append(parts, c.Quant)
        }
        if c.ContextLength > 0 {
                parts = append(parts, FormatCtx(c.ContextLength))
        }
        parts = append(parts, FormatBytes(c.SizeBytes))
        return strings.Join(parts, " · ")
}

// FormatCtx renders context lengths: "32K ctx", "128K ctx", "4K ctx".
func FormatCtx(n int) string {
        switch {
        case n >= 1024 && n%1024 == 0:
                return fmt.Sprintf("%dK ctx", n/1024)
        case n >= 1000:
                return fmt.Sprintf("%.0fK ctx", float64(n)/1000)
        default:
                return fmt.Sprintf("%d ctx", n)
        }
}

// FormatBytes renders a human size: "4.4 GB".
func FormatBytes(b int64) string {
        switch {
        case b >= 1<<30:
                return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
        case b >= 1<<20:
                return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
        case b >= 1<<10:
                return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
        default:
                return fmt.Sprintf("%d B", b)
        }
}

// gguf quantization names by llama.cpp file_type value.
var ggufQuantNames = map[uint32]string{
        0: "F32", 1: "F16", 2: "Q4_0", 3: "Q4_1", 4: "Q4_1", 5: "Q4_B", 6: "Q4_B",
        7: "Q5_0", 8: "Q5_1", 9: "Q8_0", 10: "Q2_K", 11: "Q3_K_S", 12: "Q3_K_M",
        13: "Q3_K_L", 14: "Q4_K_S", 15: "Q4_K_M", 16: "Q5_K_S", 17: "Q5_K_M",
        18: "Q6_K", 19: "TQ1_0", 20: "TQ2_0", 21: "IQ2_XXS", 22: "IQ2_XS",
        23: "Q2_K_S", 24: "IQ3_XS", 25: "IQ3_S", 26: "IQ3_M", 27: "IQ2_S",
        28: "IQ2_M", 29: "IQ4_XS", 30: "BF16", 31: "IQ1_S", 32: "IQ1_M",
        36: "IQ4_NL", 38: "MXFP4",
}

func quantName(ft uint32) string {
        if n, ok := ggufQuantNames[ft]; ok {
                return n
        }
        return fmt.Sprintf("type-%d", ft)
}

// ReadModelCard parses the GGUF header of `path`. It never reads more than
// a few MB (large array values are skipped on disk) and returns an error
// for anything that is not a valid GGUF v2/v3 file — the caller falls back
// to a plain filename card.
func ReadModelCard(path string) (*ModelCard, error) {
        f, err := os.Open(path)
        if err != nil {
                return nil, err
        }
        defer f.Close()

        fi, err := f.Stat()
        if err != nil {
                return nil, err
        }
        // v1.1.5 Phase 3: the metadata read is BUFFERED. Skipping a tokenizer
        // array used to issue one unbuffered CopyN syscall per vocab string
        // (up to ~150k syscalls for a single header); a 64 KiB bufio window
        // reduces that to a handful of reads. Parsing semantics unchanged.
        //
        // v1.8.1 P0 REPAIR: the read bound is 32 MiB (was 8 MiB). Gemma-class
        // models ship 262,144-entry tokenizer arrays (tokens + scores +
        // token_type ≈ 7–11 MiB of metadata alone) — real Gemma E2B/E4B files
        // crossed the old 8 MiB bound mid-array, ReadModelCard failed with
        // EOF, ResolveModelCapabilities returned nil, and the FIRST ordinary
        // chat turn panicked in resolveEffectiveContext (the v1.8.0 Windows
        // `hi` crash). 32 MiB covers the real model class with 4x headroom
        // while remaining a hard bound against hostile files; the array-
        // element and per-string sanity bounds above are unchanged, so the
        // worst-case skipped volume stays bounded.
        r := bufio.NewReaderSize(io.LimitReader(f, ggufMetadataReadLimit), 64<<10)

        // magic
        var magic [4]byte
        if _, err := io.ReadFull(r, magic[:]); err != nil {
                return nil, fmt.Errorf("read magic: %w", err)
        }
        if string(magic[:]) != ggufMagic {
                return nil, fmt.Errorf("not a GGUF file (magic %q)", string(magic[:]))
        }
        ver, err := readGGUFUint32(r)
        if err != nil {
                return nil, err
        }
        if ver < 1 || ver > 3 {
                return nil, fmt.Errorf("unsupported GGUF version %d", ver)
        }
        if _, err := readGGUFUint64(r); err != nil { // tensor count — unused
                return nil, err
        }
        kvCount, err := readGGUFUint64(r)
        if err != nil {
                return nil, err
        }
        if kvCount > 4096 { // sanity bound — hostile/corrupt header
                return nil, fmt.Errorf("implausible kv count %d", kvCount)
        }

        card := &ModelCard{Path: path, File: baseName(path), SizeBytes: fi.Size()}
        kvs := map[string]any{}
        for i := uint64(0); i < kvCount; i++ {
                key, err := readGGUFString(r)
                if err != nil {
                        return nil, err
                }
                vtype, err := readGGUFUint32(r)
                if err != nil {
                        return nil, err
                }
                val, err := readGGUFValue(r, int(vtype))
                if err != nil {
                        return nil, fmt.Errorf("key %q: %w", key, err)
                }
                kvs[key] = val
        }

        if v, ok := kvs["general.architecture"].(string); ok {
                card.Arch = v
        }
        if v, ok := kvs["general.name"].(string); ok {
                card.Name = v
        }
        if v, ok := kvs["general.parameter_count"].(uint64); ok {
                card.ParamsCount = v
        }
        if v, ok := kvs["general.file_type"].(uint32); ok {
                card.Quant = quantName(v)
        }
        // per-architecture fields: llama.context_length, qwen2.context_length…
        //
        // v1.2.5 repair: the lookups previously matched ONLY uint32/uint64.
        // GGUF writers in the wild also emit these fields as SIGNED integers
        // (int32/int64 — both are legal GGUF types and both appear in real
        // model files); a signed value silently failed the type assertion and
        // left ContextLength at 0 — the "modelMax=0" defect. Every integer
        // type is now accepted (readGGUFValue already normalizes the narrow
        // types to uint32/int32).
        if card.Arch != "" {
                card.ContextLength = ggufInt(kvs[card.Arch+".context_length"])
                card.Layers = ggufInt(kvs[card.Arch+".block_count"])
                card.EmbeddingLen = ggufInt(kvs[card.Arch+".embedding_length"])
        }
        // Missing parameter count: derive from bits-per-weight heuristics is
        // unreliable — leave 0 (the card shows the file size regardless).
        return card, nil
}

// ggufInt converts a parsed GGUF scalar into a Go int for the per-arch
// numeric fields (context_length, block_count, embedding_length). Accepts
// every integer representation (uint32/uint64/int32 — the narrow types are
// normalized at parse time) and rejects negatives/overflow, which leave the
// field at 0 = unknown. A non-integer (string, float, array) also stays 0.
func ggufInt(v any) int {
        switch n := v.(type) {
        case uint32:
                return int(n) // safe: GGUF lengths fit 32 bits
        case uint64:
                if n > uint64(^uint(0)>>1) {
                        return 0
                }
                return int(n)
        case int32:
                if n < 0 {
                        return 0
                }
                return int(n)
        }

        return 0
}

// readGGUFValue reads (or skips) one typed value.
func readGGUFValue(r io.Reader, t int) (any, error) {
        switch t {
        case ggufTypeUint8:
                var b [1]byte
                if _, err := io.ReadFull(r, b[:]); err != nil {
                        return nil, err
                }
                return uint32(b[0]), nil
        case ggufTypeInt8:
                var b [1]byte
                if _, err := io.ReadFull(r, b[:]); err != nil {
                        return nil, err
                }
                return int32(int8(b[0])), nil
        case ggufTypeUint16:
                var b [2]byte
                if _, err := io.ReadFull(r, b[:]); err != nil {
                        return nil, err
                }
                return uint32(binary.LittleEndian.Uint16(b[:])), nil
        case ggufTypeInt16:
                var b [2]byte
                if _, err := io.ReadFull(r, b[:]); err != nil {
                        return nil, err
                }
                return int32(int16(binary.LittleEndian.Uint16(b[:]))), nil
        case ggufTypeUint32:
                return readGGUFUint32(r)
        case ggufTypeInt32:
                v, err := readGGUFUint32(r)
                return int32(v), err
        case ggufTypeFloat32:
                raw, err := readGGUFUint32(r)
                if err != nil {
                        return nil, err
                }
                return math.Float32frombits(raw), nil
        case ggufTypeBool:
                var b [1]byte
                if _, err := io.ReadFull(r, b[:]); err != nil {
                        return nil, err
                }
                return b[0] != 0, nil
        case ggufTypeString:
                return readGGUFString(r)
        case ggufTypeUint64, ggufTypeInt64:
                return readGGUFUint64(r)
        case ggufTypeFloat64:
                raw, err := readGGUFUint64(r)
                if err != nil {
                        return nil, err
                }
                return math.Float64frombits(raw), nil
        case ggufTypeArray:
                // element type + count, then the elements are SKIPPED on disk
                et, err := readGGUFUint32(r)
                if err != nil {
                        return nil, err
                }
                count, err := readGGUFUint64(r)
                if err != nil {
                        return nil, err
                }
                if err := skipGGUFArray(r, int(et), count); err != nil {
                        return nil, err
                }
                return nil, nil // arrays are skipped (tokenizer data etc.)
        default:
                return nil, fmt.Errorf("unknown value type %d", t)
        }
}

// skipGGUFArray advances the reader past `count` elements of type `et`
// without materializing them.
func skipGGUFArray(r io.Reader, et int, count uint64) error {
        if count > 100<<20 { // hostile bound: 100M elements is not a real model
                return fmt.Errorf("implausible array length %d", count)
        }
        if et == ggufTypeString {
                for i := uint64(0); i < count; i++ {
                        n, err := readGGUFUint64(r) // string length
                        if err != nil {
                                return err
                        }
                        if n > 1<<24 {
                                return fmt.Errorf("implausible string length %d", n)
                        }
                        if _, err := io.CopyN(io.Discard, r, int64(n)); err != nil {
                                return err
                        }
                }
                return nil
        }
        sz, ok := ggufTypeSize(et)
        if !ok {
                return fmt.Errorf("array of unknown type %d", et)
        }
        if _, err := io.CopyN(io.Discard, r, int64(sz)*int64(count)); err != nil {
                return err
        }
        return nil
}

// ggufTypeSize is the byte size of a fixed-width GGUF value type.
func ggufTypeSize(t int) (int, bool) {
        switch t {
        case ggufTypeUint8, ggufTypeInt8, ggufTypeBool:
                return 1, true
        case ggufTypeUint16, ggufTypeInt16:
                return 2, true
        case ggufTypeUint32, ggufTypeInt32, ggufTypeFloat32:
                return 4, true
        case ggufTypeUint64, ggufTypeInt64, ggufTypeFloat64:
                return 8, true
        }
        return 0, false
}

func readGGUFUint32(r io.Reader) (uint32, error) {
        var b [4]byte
        if _, err := io.ReadFull(r, b[:]); err != nil {
                return 0, err
        }
        return binary.LittleEndian.Uint32(b[:]), nil
}

func readGGUFUint64(r io.Reader) (uint64, error) {
        var b [8]byte
        if _, err := io.ReadFull(r, b[:]); err != nil {
                return 0, err
        }
        return binary.LittleEndian.Uint64(b[:]), nil
}

func readGGUFString(r io.Reader) (string, error) {
        n, err := readGGUFUint64(r)
        if err != nil {
                return "", err
        }
        if n > 1<<24 { // 16MB key bound — hostile/corrupt header
                return "", fmt.Errorf("implausible string length %d", n)
        }
        buf := make([]byte, n)
        if _, err := io.ReadFull(r, buf); err != nil {
                return "", err
        }
        return string(buf), nil
}

func baseName(p string) string {
        if i := strings.LastIndexByte(p, '/'); i >= 0 {
                p = p[i+1:]
        }
        if i := strings.LastIndexByte(p, '\\'); i >= 0 {
                p = p[i+1:]
        }
        return p
}
