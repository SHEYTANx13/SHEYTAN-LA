// Package tools — dataAnalysis: a pure-Go data-analysis engine for the agent.
//
// Loads CSV/TSV/JSON datasets with type inference, then profiles, computes
// descriptive statistics, correlations, regressions, group-by aggregations,
// filtering, sorting, histograms, format conversion, and renders
// publication-quality fire-themed SVG charts. No external dependencies —
// everything is stdlib.
//
// v1.0.9 (TURBINE) parsing engine rewrite — "parse once, analyze many":
//   - csvFields: a zero-copy RFC-4180 field scanner. Unquoted and cleanly
//     quoted fields are subslices of the source line (no per-cell
//     strings.Builder, no per-cell heap churn); only fields that actually
//     contain escaped quotes materialize a small buffer. The old parser
//     appended every cell byte-by-byte through a strings.Builder, which made
//     loading a 100 MB CSV allocate hundreds of millions of bytes.
//   - splitLinesAny: quote-aware line splitting that treats \n and \r\n as
//     terminators DURING the scan (the old path did a full-file
//     strings.ReplaceAll \r\n → \n copy first — a second complete copy of
//     the dataset on every load).
//   - parseNumber: allocation-free fast path (ASCII, no thousand separators)
//     that skips straight to strconv.ParseFloat.
//   - numericColumn: every numeric column is parsed AT MOST ONCE per dataset
//     generation and cached; stats/correlation/regression/groupby/histogram
//     previously re-parsed the same cells on every single action.
//   - dataset cache: a bounded LRU (recency-ordered, byte-budgeted) instead
//     of the old wipe-everything-at-16-entries map, so chained analysis on a
//     hot dataset never loses its parse.
//   - records presized from a newline census: append-growth reallocation of
//     the row matrix (a multi-megabyte copy cascade on big files) is gone.
//
// Interoperability: dataset paths resolve through ResolvePath (app folder),
// so a CSV written with the `files` tool is immediately analyzable with the
// same relative path.
package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

// --- dataset model ---

type colType string

const (
	typeNumber colType = "number"
	typeString colType = "string"
	typeBool   colType = "bool"
)

var missingTokens = map[string]bool{
	"": true, "-": true, "NA": true, "N/A": true, "na": true,
	"null": true, "NULL": true, "Null": true, "NaN": true, "nan": true,
	"none": true, "None": true,
}

type dataset struct {
	Path    string // absolute path the data was loaded from
	Columns []string
	Types   []colType
	Rows    [][]string // raw cells, aligned with Columns

	// v1.0.9 numeric cache: parse-once columns (missing cells come back as
	// NaN). Guarded by a mutex because datasets are shared through the cache
	// while the orchestrator may run tools concurrently.
	numMu      sync.Mutex
	numCache   map[int][]float64
	generation int64 // bumped by writers (dedupe/sample) to drop stale caches
}

// numeric extracts the float value of a cell (NaN when missing/non-numeric).
func (d *dataset) numeric(row int, col int) float64 {
	return parseNumber(d.Rows[row][col])
}

// numericColumn returns the parsed values of a numeric column (missing /
// non-numeric cells are NaN), parsed once per dataset and cached. This is
// the hot input of every statistical action — v1.0.9 turns repeated
// full-column string parsing into one slice lookup.
func (d *dataset) numericColumn(col int) []float64 {
	d.numMu.Lock()
	defer d.numMu.Unlock()
	if d.numCache == nil {
		d.numCache = map[int][]float64{}
	}
	if v, ok := d.numCache[col]; ok {
		return v
	}
	vals := make([]float64, len(d.Rows))
	for r := range d.Rows {
		vals[r] = parseNumber(d.Rows[r][col])
	}
	d.numCache[col] = vals
	return vals
}

// invalidateNumCache drops cached numeric columns after Rows is replaced
// in place (dedupe / fill style operations on the same dataset object).
func (d *dataset) invalidateNumCache() {
	d.numMu.Lock()
	d.numCache = nil
	d.generation++
	d.numMu.Unlock()
}

// parseNumber converts a cell to float64 (NaN when missing/non-numeric).
// v1.0.9 fast path: clean ASCII numbers (the overwhelming majority of
// numeric cells) go straight to ParseFloat with zero allocations. Only
// cells that carry thousand separators or surrounding whitespace take the
// normalization path.
func parseNumber(s string) float64 {
	if s == "" {
		return math.NaN()
	}
	// Fast path: first byte already looks numeric; skip TrimSpace when the
	// string is already trimmed, and skip comma-stripping when there is no
	// comma. ParseFloat rejects leading spaces itself.
	if needsNumberCleanup(s) {
		s = strings.TrimSpace(s)
		if s == "" {
			return math.NaN()
		}
		if strings.IndexByte(s, ',') >= 0 {
			s = strings.ReplaceAll(s, ",", "")
			if s == "" {
				return math.NaN()
			}
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN()
	}
	return f
}

// needsNumberCleanup reports whether a raw cell needs trimming or
// comma-stripping before ParseFloat. Byte checks only — no allocation.
func needsNumberCleanup(s string) bool {
	c := s[0]
	if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == ',' || c == '+' {
		// leading separators/spaces need trimming (',' cannot START a number
		// but a thousands-separator form like ",123" is broken anyway);
		// '+' is accepted by ParseFloat directly but costs nothing to allow.
		return c != '+' || strings.IndexByte(s, ',') >= 0
	}
	if strings.IndexByte(s, ',') >= 0 {
		return true
	}
	// trailing space check: last byte
	switch s[len(s)-1] {
	case ' ', '\t', '\r', '\n':
		return true
	}
	return false
}

func isMissing(s string) bool { return missingTokens[strings.TrimSpace(s)] }

func (d *dataset) colIndex(name string) (int, error) {
	for i, c := range d.Columns {
		if strings.EqualFold(c, name) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("unknown column %q (available: %s)", name, strings.Join(d.Columns, ", "))
}

// numericCols returns the indexes of all numeric columns.
func (d *dataset) numericCols() []int {
	var out []int
	for i, t := range d.Types {
		if t == typeNumber {
			out = append(out, i)
		}
	}
	return out
}

// --- dataset cache (v1.0.9: bounded LRU keyed by path+mtime+size) ---
//
// Chained analysis calls (profile → stats → groupby → chart on one file)
// hit the cache every time; the recency order keeps the hottest datasets
// resident while cold ones fall out. The old map was wiped wholesale when
// it grew past 16 entries, which evicted the file the user was actively
// working on the moment a second dataset appeared.

type cacheKey struct {
	path  string
	mtime int64
	size  int64
}

const (
	dsMaxEntries = 16
	dsMaxBytes   = 192 << 20 // ~192 MB of retained cells across all entries
)

var (
	dsMu    sync.Mutex
	dsCache = map[cacheKey]*lruEntry{}
	dsOrder []cacheKey // front = oldest, back = most recent
	dsBytes int64
)

type lruEntry struct {
	ds    *dataset
	bytes int64
}

// dsSize approximates the memory footprint of a dataset (cells + row
// headers + column vectors).
func dsSize(d *dataset) int64 {
	var n int64
	for _, row := range d.Rows {
		n += int64(cap(row)) * 16
		for _, cell := range row {
			n += int64(len(cell))
		}
	}
	n += int64(len(d.Columns)) * 64
	return n
}

func dsTouch(key cacheKey) {
	for i, k := range dsOrder {
		if k == key {
			copy(dsOrder[i:], dsOrder[i+1:])
			dsOrder[len(dsOrder)-1] = key
			return
		}
	}
	dsOrder = append(dsOrder, key)
}

func dsEvictLocked() {
	for len(dsOrder) > 0 && (len(dsOrder) > dsMaxEntries || dsBytes > dsMaxBytes) {
		old := dsOrder[0]
		dsOrder = dsOrder[1:]
		if e, ok := dsCache[old]; ok {
			dsBytes -= e.bytes
			delete(dsCache, old)
		}
	}
	if dsBytes < 0 {
		dsBytes = 0
	}
}

// maxDatasetBytes is the honest in-process input bound (pure-Go
// backend — see data_insights.go for the v1.8.7 backend decision).
// Package-level so the bound is testable without materializing a
// 256 MB fixture.
var maxDatasetBytes int64 = 256 << 20

// checkDatasetSize enforces the in-process input bound. There is no
// streaming claim: files past the bound are rejected with guidance, and
// heavy relational work stays on the pure-Go fast path (filter first,
// then aggregate).
// v1.8.8 honesty repair: the previous text suggested "filter it in
// chunks" — chunked processing the engine does NOT implement. The
// message now states exactly what is true: the whole file must fit in
// memory; oversized inputs must be split or pre-filtered outside this
// tool.
func checkDatasetSize(size int64) error {
	if size > maxDatasetBytes {
		return fmt.Errorf("dataset too large (%d MB) for the in-process engine (honest bound: %d MB, whole file in memory) — split the file or pre-filter it outside dataAnalysis; there is no streaming or chunked mode", size>>20, maxDatasetBytes>>20)
	}

	return nil
}

func (t *DataTool) load(path string) (*dataset, error) {
	abs := ResolvePath(path)
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("dataset not found: %s (relative paths resolve against the app folder)", abs)
	}
	if err := checkDatasetSize(fi.Size()); err != nil {
		return nil, err
	}
	key := cacheKey{path: abs, mtime: fi.ModTime().UnixNano(), size: fi.Size()}
	dsMu.Lock()
	if e, ok := dsCache[key]; ok {
		dsTouch(key)
		dsMu.Unlock()
		return e.ds, nil
	}
	dsMu.Unlock()

	ext := strings.ToLower(filepath.Ext(abs))
	var ds *dataset
	switch ext {
	case ".json", ".jsonl":
		ds, err = loadJSON(abs)
	default: // .csv, .tsv, .txt → delimiter sniffing
		ds, err = loadDelimited(abs)
	}
	if err != nil {
		return nil, err
	}
	ds.Path = abs

	dsMu.Lock()
	// Replace-by-key first (same file re-parsed), then admit with LRU bookkeeping.
	if e, ok := dsCache[key]; ok {
		dsBytes -= e.bytes
	}
	sz := dsSize(ds)
	dsCache[key] = &lruEntry{ds: ds, bytes: sz}
	dsBytes += sz
	dsTouch(key)
	dsEvictLocked()
	dsMu.Unlock()
	return ds, nil
}

// --- loaders ---

// countUnquoted counts occurrences of target outside double-quoted
// spans (quote parity scan — same contract as splitLinesAny).
func countUnquoted(s string, target byte) int {
	n := 0
	inQ := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQ = !inQ
		case target:
			if !inQ {
				n++
			}
		}
	}

	return n
}

func loadDelimited(path string) (*dataset, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("file is empty")
	}
	// v1.0.9: BOM strip on the byte level (no whole-file string conversion
	// before we know the file is even usable).
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		raw = raw[3:]
	}
	text := unsafeString(raw)

	lines := splitLinesAny(text)
	if len(lines) == 0 {
		return nil, fmt.Errorf("file is empty")
	}

	// Delimiter sniff on the header line (comma vs tab).
	// v1.8.8: count only delimiters OUTSIDE quoted fields — a quoted
	// comma in a TSV header (or a quoted tab in a CSV header) previously
	// flipped the sniff and mis-split the whole file.
	delim := byte('\t')
	if countUnquoted(lines[0], ',') >= countUnquoted(lines[0], '\t') {
		delim = ','
	}

	// v1.0.9: presize the row matrix from a newline census — a 1M-row file
	// no longer grows its [][]string through ~30 doubling copies.
	records := make([][]string, 0, estimateRecords(text))
	for _, ln := range lines {
		if ln == "" || isBlankASCII(ln) {
			continue
		}
		records = append(records, csvFields(ln, delim))
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("need a header row plus at least one data row")
	}
	return buildDataset(records[0], records[1:]), nil
}

// decodeJSONObject decodes one JSON object preserving the DOCUMENT order
// of its keys. encoding/json map iteration is randomized, so deriving the
// column order from the decoded map directly made every JSON analysis
// result order-nondeterministic between runs (v1.8.7 determinism audit
// finding). Returns the value map plus the first-seen key order.
func decodeJSONObject(dec *json.Decoder) (map[string]any, []string, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, nil, fmt.Errorf("expected a JSON object, got %v", tok)
	}

	obj := map[string]any{}
	var order []string

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, nil, fmt.Errorf("object key is not a string")
		}

		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}

		if _, seen := obj[key]; !seen {
			order = append(order, key)
		}
		obj[key] = v
	}

	// Consume the closing '}'.
	if _, err := dec.Token(); err != nil {
		return nil, nil, err
	}

	return obj, order, nil
}

func loadJSON(path string) (*dataset, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Support both a JSON array and JSONL. Both paths decode objects in
	// DOCUMENT order so the column order is deterministic.
	trimmed := strings.TrimSpace(string(raw))

	var objs []map[string]any
	var orders [][]string

	if strings.HasPrefix(trimmed, "[") {
		dec := json.NewDecoder(strings.NewReader(trimmed))
		if t, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("parse JSON array: %w", err)
		} else if d, ok := t.(json.Delim); !ok || d != '[' {
			return nil, fmt.Errorf("parse JSON array: not an array")
		}
		for dec.More() {
			obj, order, err := decodeJSONObject(dec)
			if err != nil {
				return nil, fmt.Errorf("parse JSON array: %w", err)
			}
			objs = append(objs, obj)
			orders = append(orders, order)
		}
	} else {
		// v1.0.9: JSONL parses through the same quote-aware splitter; each
		// line is decoded straight from its subslice (no intermediate
		// []string of the whole file beyond the split itself).
		for _, ln := range splitLinesAny(trimmed) {
			ln = strings.TrimSpace(ln)
			if ln == "" {
				continue
			}
			obj, order, err := decodeJSONObject(json.NewDecoder(strings.NewReader(ln)))
			if err != nil {
				return nil, fmt.Errorf("parse JSONL line: %w", err)
			}
			objs = append(objs, obj)
			orders = append(orders, order)
		}
	}
	if len(objs) == 0 {
		return nil, fmt.Errorf("no records in JSON")
	}

	// Column order: first-seen document order across all objects.
	colSet := map[string]bool{}
	var cols []string
	for _, order := range orders {
		for _, k := range order {
			if !colSet[k] {
				colSet[k] = true
				cols = append(cols, k)
			}
		}
	}
	rows := make([][]string, 0, len(objs))
	for _, o := range objs {
		row := make([]string, len(cols))
		for i, c := range cols {
			v, ok := o[c]
			if !ok || v == nil {
				row[i] = ""
				continue
			}
			switch x := v.(type) {
			case string:
				row[i] = x
			case float64:
				row[i] = strconv.FormatFloat(x, 'g', -1, 64)
			case bool:
				row[i] = strconv.FormatBool(x)
			default:
				b, _ := json.Marshal(x)
				row[i] = string(b)
			}
		}
		rows = append(rows, row)
	}
	return buildDataset(cols, rows), nil
}

// buildDataset infers column types from the values.
func buildDataset(header []string, rows [][]string) *dataset {
	cols := make([]string, len(header))
	for i, h := range header {
		h = strings.TrimSpace(h)
		if h == "" {
			h = fmt.Sprintf("col%d", i+1)
		}
		cols[i] = h
	}
	// Pad/truncate rows to header width.
	for r := range rows {
		if len(rows[r]) < len(cols) {
			rows[r] = append(rows[r], make([]string, len(cols)-len(rows[r]))...)
		} else if len(rows[r]) > len(cols) {
			rows[r] = rows[r][:len(cols)]
		}
	}
	types := make([]colType, len(cols))
	for c := range cols {
		num, boolN, total := 0, 0, 0
		for r := range rows {
			v := strings.TrimSpace(rows[r][c])
			if isMissing(v) {
				continue
			}
			total++
			if !math.IsNaN(parseNumber(v)) {
				num++
			}
			switch strings.ToLower(v) {
			case "true", "false", "yes", "no":
				boolN++
			}
		}
		switch {
		case total > 0 && num == total:
			types[c] = typeNumber
		case total > 0 && boolN == total:
			types[c] = typeBool
		default:
			types[c] = typeString
		}
	}
	return &dataset{Columns: cols, Types: types, Rows: rows}
}

// --- CSV parsing (v1.0.9 zero-copy engine) ---

// csvFields scans one logical line into fields, RFC-4180-compatible with
// the v1.0.8 parser (quote parity, "" escapes, quotes dropped from cells).
//
// Performance contract: a field is a SUBSLICE of `line` unless it actually
// needed an escape unescaped or had structural quotes in its middle — only
// those rare fields pay for a small buffer. The v1.0.8 parser routed every
// byte of every cell through a strings.Builder; on a 1M-row dataset that
// was ~100M WriteByte calls and the single largest profile entry in the
// whole app.
func csvFields(line string, delim byte) []string {
	out := make([]string, 0, 8)
	n := len(line)
	i := 0
	for {
		// Parse one field starting at i.
		field, next := scanField(line, i, delim, n)
		out = append(out, field)
		if next > n { // sentinel: consumed the tail
			break
		}
		i = next
		if i >= n {
			// Line ended exactly on a delimiter → trailing empty field.
			out = append(out, "")
			break
		}
	}
	return out
}

// scanField parses one field beginning at `from`. Returns the field and the
// index just past its delimiter, or next > len(line) when the line tail was
// consumed.
func scanField(line string, from int, delim byte, n int) (string, int) {
	i := from
	// Fast path 1: unquoted run with no quotes at all.
	litStart := i
	inQ := false
	var buf strings.Builder
	usedBuf := false

	flush := func(end int) {
		if usedBuf {
			if end > litStart {
				buf.WriteString(line[litStart:end])
			}
		}
	}

	for i < n {
		c := line[i]
		switch {
		case c == '"':
			if inQ && i+1 < n && line[i+1] == '"' {
				// Escaped quote: materialize everything before it plus '"'.
				if !usedBuf {
					buf.WriteString(line[litStart:i])
					usedBuf = true
				} else if i > litStart {
					buf.WriteString(line[litStart:i])
				}
				buf.WriteByte('"')
				i += 2
				litStart = i
				continue
			}
			// Structural quote (parity toggle): the quote character itself
			// disappears. Any pending literal run before it must be preserved
			// (flushed into the buffer once buffering started), and the zero-copy
			// boundary shifts past it when nothing is buffered yet.
			if i > litStart {
				buf.WriteString(line[litStart:i])
				usedBuf = true
			}
			litStart = i + 1
			inQ = !inQ
			i++
		case c == delim && !inQ:
			if usedBuf {
				flush(i)
				return buf.String(), i + 1
			}
			return line[litStart:i], i + 1
		default:
			i++
		}
	}
	// Line tail: flush remaining literal run.
	if usedBuf {
		if n > litStart {
			buf.WriteString(line[litStart:n])
		}
		return buf.String(), n + 1
	}
	return line[litStart:n], n + 1
}

// splitCSVLine keeps the v1.0.8 name/behavior for existing callers/tests,
// now delegating to the zero-copy engine.
func splitCSVLine(line, delim string) []string {
	return csvFields(line, delim[0])
}

// splitLinesAny splits text into logical lines on \n or \r\n, honoring
// quote parity (a newline inside quotes never terminates a line). Returns
// SUBSLICES of s — the function never copies file data; a trailing \r is
// trimmed only where a \r\n pair actually terminated the line.
func splitLinesAny(s string) []string {
	out := make([]string, 0, 1+strings.Count(s, "\n"))
	start := 0
	inQ := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQ = !inQ
		case '\n':
			if inQ {
				continue
			}
			end := i
			if end > start && s[end-1] == '\r' {
				end--
			}
			out = append(out, s[start:end])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// splitLines keeps the v1.0.8 name for existing callers.
func splitLines(s string) []string { return splitLinesAny(s) }

// isBlankASCII reports whether a line is only spaces/tabs.
func isBlankASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return false
		}
	}
	return true
}

// estimateRecords estimates the row count from a newline census so the row
// matrix is allocated once at its final size.
func estimateRecords(text string) int {
	n := strings.Count(text, "\n") + 1
	if n < 16 {
		n = 16
	}
	return n
}

// unsafeString wraps a []byte as a string without copying (the byte slice
// comes straight from os.ReadFile and is never mutated afterwards; it stays
// reachable through the resulting subslices for the dataset's lifetime).
// The same pattern the standard library uses in strings.Builder.String.
func unsafeString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(&b[0], len(b))
}
