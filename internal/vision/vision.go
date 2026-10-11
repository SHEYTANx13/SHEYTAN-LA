// Package vision implements SHEYTAN's multimodal layer (v1.0.6): multimodal
// projector (mmproj) discovery and pairing with the selected chat model, plus
// image encoding into the OpenAI-style data-URL wire format that llama.cpp's
// mtmd runtime consumes.
//
// The package is deliberately a LEAF: it depends on nothing internal, so the
// llm engine, the tools, and the UI can all import it without cycles.
package vision

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"golang.org/x/image/bmp"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/anthonynsimon/bild/transform"
	"golang.org/x/image/webp"
)

// MaxSide is the largest image dimension sent to the model. Gemma-class
// vision encoders process at most ~1536-2048 px per side; anything bigger is
// downscaled once here so a 4K screenshot never becomes a multi-megabyte
// base64 blob repeated on every agent iteration.
const MaxSide = 2048

// FallbackSide is used when an image still exceeds the byte budget after the
// first downscale (pathological case: a noise-heavy PNG).
const FallbackSide = 1280

// EncodedBudget is the soft cap on one image's base64 payload.
const EncodedBudget = 6 << 20 // 6 MB

// MaxImagesPerMessage caps how many images ride on a single message — every
// image is re-tokenized by the vision encoder, so an unbounded gallery would
// stall the turn for minutes.
const MaxImagesPerMessage = 4

// --- mmproj detection -------------------------------------------------------

// IsMMProj reports whether path is a multimodal projector GGUF. The filename
// convention ("mmproj-*.gguf") is the primary signal — it is what every major
// publisher (google/gemma, Qwen, llama.cpp converters) ships — with a GGUF
// metadata scan as the fallback for unconventionally named files.
func IsMMProj(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	if strings.Contains(name, "mmproj") {
		return strings.HasSuffix(name, ".gguf")
	}
	if !strings.HasSuffix(name, ".gguf") {
		return false
	}
	return ggufHasClipKeys(path)
}

// ListProjectors returns every mmproj file directly inside dir (sorted by
// name). Sub-directories are not scanned — the models folder is flat by
// convention.
func ListProjectors(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if IsMMProj(p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// FindProjector pairs a projector with the chat model file. Scoring is fuzzy
// token overlap between the two file names ("gemma-4-E2B-it-Q4_K_M" vs
// "mmproj-gemma-4-E2B-it-BF16" share gemma/4/e2b/it) so quantization and
// precision suffixes never break the pair. A projector only pairs when the
// family token matches; when several pair, the largest overlap wins.
//
// An explicit override always short-circuits detection: when override is a
// non-empty file name or path that exists, it is returned as-is (the user
// knows better than the heuristic). A missing explicit override returns "" —
// never a silent substitute.
func FindProjector(dir, modelFile, override string) string {
	if override != "" {
		if filepath.IsAbs(override) {
			if _, err := os.Stat(override); err == nil {
				return override
			}
			return ""
		}
		p := filepath.Join(dir, override)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	}
	projectors := ListProjectors(dir)
	if len(projectors) == 0 {
		return ""
	}
	modelName := strings.ToLower(strings.TrimSuffix(filepath.Base(modelFile), ".gguf"))
	modelTokens := tokenSet(modelName)

	bestScore, bestPath := 0, ""
	for _, p := range projectors {
		pName := strings.ToLower(strings.TrimSuffix(filepath.Base(p), ".gguf"))
		if !familyMatches(modelName, pName) {
			continue
		}
		pTokens := tokenSet(pName)
		score := 0
		for tok := range modelTokens {
			if pTokens[tok] {
				score++
			}
		}
		if score < 1 {
			continue // family matched but zero overlap should not happen; safety
		}
		// A lone family token (score 1) is a weak pair — accept it only when
		// nothing better exists, so "gemma-3-4b" does not hijack a
		// "gemma-4-E2B" model when the matching projector is present.
		if score > bestScore { // ties keep the first (sorted) — deterministic
			bestScore, bestPath = score, p
		}
	}
	if bestScore == 0 {
		return ""
	}
	return bestPath
}

// familyMatches reports whether the two names share their family token
// (the first token of the name: gemma↔gemma, qwen↔qwen, ...).
func familyMatches(a, b string) bool {
	fa, fb := familyFromName(a), familyFromName(b)
	return fa != "" && fa == fb
}

// familyFromName returns the first token of a model/projector file name —
// by POSITION, not alphabetically ("gemma-4-E2B…" → "gemma").
func familyFromName(name string) string {
	for _, f := range strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == ' ' || r == '+'
	}) {
		if f != "" && f != "mmproj" && f != "gguf" {
			return strings.ToLower(f)
		}
	}
	return ""
}

func tokenSet(name string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == ' ' || r == '+'
	}) {
		if f != "" && f != "gguf" && f != "mmproj" && f != "it" && f != "instruct" {
			out[f] = true
		}
	}
	return out
}

// --- GGUF clip-key scan (fallback detector) ---------------------------------

// ggufHasClipKeys reads the GGUF metadata header and reports whether the file
// carries vision-projector metadata (keys prefixed "clip."). Values are
// SKIPPED by size, never materialized — the scan is O(header) with a strict
// byte bound so a corrupt or hostile file cannot stall the app.
func ggufHasClipKeys(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	r := io.LimitReader(f, 8<<20)

	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil || string(magic[:]) != "GGUF" {
		return false
	}
	ver, err := readU32(r)
	if err != nil || ver < 1 || ver > 4 {
		return false
	}
	if _, err := readU64(r); err != nil { // tensor count
		return false
	}
	kvCount, err := readU64(r)
	if err != nil || kvCount > 4096 {
		return false
	}
	clipKeys := 0
	for i := uint64(0); i < kvCount; i++ {
		key, err := readStr(r)
		if err != nil {
			return false
		}
		vtype, err := readU32(r)
		if err != nil {
			return false
		}
		if err := skipValue(r, int(vtype)); err != nil {
			return false
		}
		if strings.HasPrefix(key, "clip.") {
			clipKeys++
		}
	}
	return clipKeys >= 3
}

func readU32(r io.Reader) (uint32, error) {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24, nil
}

func readU64(r io.Reader) (uint64, error) {
	var b [8]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	var v uint64
	for i := 0; i < 8; i++ {
		v |= uint64(b[i]) << (8 * i)
	}
	return v, nil
}

func readStr(r io.Reader) (string, error) {
	n, err := readU64(r)
	if err != nil || n > 1<<20 {
		return "", fmt.Errorf("bad string length")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// gguf type ids (GGUF spec)
const (
	tUint8   = 0
	tInt8    = 1
	tUint16  = 2
	tInt16   = 3
	tUint32  = 4
	tInt32   = 5
	tFloat32 = 6
	tBool    = 7
	tString  = 8
	tArray   = 9
	tUint64  = 10
	tInt64   = 11
	tFloat64 = 12
)

func skipValue(r io.Reader, t int) error {
	skip := func(n int64) error {
		_, err := io.CopyN(io.Discard, r, n)
		return err
	}
	switch t {
	case tUint8, tInt8, tBool:
		return skip(1)
	case tUint16, tInt16:
		return skip(2)
	case tUint32, tInt32, tFloat32:
		return skip(4)
	case tUint64, tInt64, tFloat64:
		return skip(8)
	case tString:
		n, err := readU64(r)
		if err != nil {
			return err
		}
		if n > 64<<20 {
			return fmt.Errorf("oversized string")
		}
		return skip(int64(n))
	case tArray:
		et, err := readU32(r)
		if err != nil {
			return err
		}
		n, err := readU64(r)
		if err != nil || n > 1<<20 {
			return fmt.Errorf("bad array")
		}
		// fixed-size element types can be skipped in one shot
		switch int(et) {
		case tUint8, tInt8, tBool:
			return skip(int64(n))
		case tUint16, tInt16:
			return skip(2 * int64(n))
		case tUint32, tInt32, tFloat32:
			return skip(4 * int64(n))
		case tUint64, tInt64, tFloat64:
			return skip(8 * int64(n))
		}
		// nested strings/arrays: walk element by element
		for i := uint64(0); i < n; i++ {
			if err := skipValue(r, int(et)); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown gguf type %d", t)
	}
}

// --- image encoding -----------------------------------------------------------

// IsImageFile reports whether the path looks like a raster image the vision
// encoder accepts.
func IsImageFile(path string) bool {
	switch strings.ToLower(filepathExt(path)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
		return true
	}
	return false
}

func filepathExt(p string) string {
	name := strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}

// EncodeImage reads the image at path, downscales it to MaxSide when needed,
// and returns an OpenAI-style inline data URL (data:image/<mime>;base64,…).
// PNG sources stay PNG (screenshots keep their text crisp); everything else
// re-encodes as JPEG quality 88 — the sweet spot for vision token counts.
func EncodeImage(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	img, format, err := decodeImage(data)
	if err != nil {
		return "", fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}

	url, err := encodeAs(img, format == "png")
	if err != nil {
		return "", err
	}
	// Still too heavy (a 2048px dense PNG can exceed the budget): downscale
	// harder and fall back to JPEG.
	if len(url) > EncodedBudget {
		small := scaleDown(img, FallbackSide)
		url2, err := encodeAs(small, false)
		if err != nil {
			return "", err
		}
		url = url2
	}
	return url, nil
}

func decodeImage(data []byte) (image.Image, string, error) {
	r := bytes.NewReader(data)
	if img, err := png.Decode(r); err == nil {
		return img, "png", nil
	}
	r.Reset(data)
	if img, err := jpeg.Decode(r); err == nil {
		return img, "jpeg", nil
	}
	r.Reset(data)
	if img, err := webp.Decode(r); err == nil {
		return img, "webp", nil
	}
	r.Reset(data)
	if img, err := gif.Decode(r); err == nil {
		return img, "gif", nil
	}
	r.Reset(data)
	// v1.1.4: .bmp files were accepted by IsImageFile but had no decoder —
	// a bmp attachment always failed at encode time with "unsupported
	// image format".
	if img, err := bmp.Decode(r); err == nil {
		return img, "bmp", nil
	}
	return nil, "", fmt.Errorf("unsupported image format")
}

func encodeAs(img image.Image, asPNG bool) (string, error) {
	// Downscale first when oversized.
	img = scaleDown(img, MaxSide)
	var buf bytes.Buffer
	mime := "image/jpeg"
	if asPNG {
		mime = "image/png"
		if err := png.Encode(&buf, img); err != nil {
			return "", err
		}
	} else {
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88}); err != nil {
			return "", err
		}
	}
	// A PNG that exploded past the budget as-is: fall back to JPEG bytes.
	if asPNG && buf.Len() > EncodedBudget {
		buf.Reset()
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88}); err != nil {
			return "", err
		}
		mime = "image/jpeg"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func scaleDown(img image.Image, maxSide int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxSide && h <= maxSide {
		return img
	}
	scale := float64(maxSide) / float64(w)
	if float64(h)*scale > float64(maxSide) {
		scale = float64(maxSide) / float64(h)
	}
	nw, nh := max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
	return transform.Resize(img, nw, nh, transform.Linear)
}
