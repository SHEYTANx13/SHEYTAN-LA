// Package tools — json: structured-data tool for JSON and JSONL files.
//
// The dataAnalysis engine speaks tabular (CSV-shaped) data; JSON with
// nesting needs its own surface. The json tool gives the agent:
//
//   - query:  dot/bracket path extraction (a.b[0].c) with [*] wildcards
//     over arrays, returning compact JSON values
//   - where:  JSONL row filtering (field equals / contains / compares),
//     bounded output, optional write-out of the matched rows
//   - stats:  object count, key frequency, value types per key, depth
//   - keys:   recursive key-path inventory (bounded)
//   - pretty: compact <-> indented conversion (chunk-written output)
//   - flatten: nested objects -> dot-keyed flat objects
//
// Every action is size-capped (256 MB) and stream/line-oriented where the
// input allows it — a 100 MB JSONL never materializes as one blob.
package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// JSONTool is the JSON/JSONL structured-data tool.
type JSONTool struct{}

// Name implements the agent tool interface.
func (JSONTool) Name() string { return "json" }

// Description implements the agent tool interface.
func (JSONTool) Description() string {
	return "Query and transform JSON/JSONL files. " +
		`action=query extracts a path like "a.b[0].c" (supports [*] wildcards over arrays); ` +
		`action=where filters JSONL rows by field (op: eq|ne|contains|gt|lt, optional format:"jsonl" write-out of matches); ` +
		"action=stats profiles objects (count, key frequency, value types); action=keys lists recursive key paths; " +
		"action=pretty converts compact<->indented; action=flatten converts nested objects to dot-keyed flat ones. " +
		"Pair with files/dataAnalysis: read raw JSON, filter/extract, then tabulate with dataAnalysis."
}

// Parameters implements the agent tool interface.
func (JSONTool) Parameters() any {
	return struct {
		Action string `json:"action"`
		Path   string `json:"path"`
		Query  string `json:"query,omitempty"`
		Field  string `json:"field,omitempty"`
		Value  string `json:"value,omitempty"`
		Op     string `json:"op,omitempty"`
		Limit  int    `json:"limit,omitempty"`
		Format string `json:"format,omitempty"`
		Dest   string `json:"dest,omitempty"`
	}{}
}

var _ = context.Background // context kept for interface parity

// jsonMaxOutBounds caps every textual result before it enters the model
// context (matches the files tool discipline).
const jsonMaxOut = 32 * 1024

// Run implements the agent tool interface.
func (t JSONTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Action string `json:"action"`
		Path   string `json:"path"`
		Query  string `json:"query"`
		Field  string `json:"field"`
		Value  string `json:"value"`
		Op     string `json:"op"`
		Limit  int    `json:"limit"`
		Format string `json:"format"`
		Dest   string `json:"dest"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	switch strings.ToLower(p.Action) {
	case "query":
		return t.query(p.Path, p.Query, p.Limit)
	case "where":
		return t.where(p.Path, p.Field, p.Op, p.Value, p.Limit, p.Format, p.Dest)
	case "stats":
		return t.stats(p.Path)
	case "keys":
		return t.keys(p.Path, p.Limit)
	case "pretty":
		return t.pretty(p.Path, p.Dest)
	case "flatten":
		return t.flatten(p.Path, p.Dest)
	default:
		return "", fmt.Errorf("unknown action %q (query|where|stats|keys|pretty|flatten)", p.Action)
	}
}

// jsonLoadAny loads a JSON array/object or a JSONL stream (line-by-line —
// big JSONL never becomes one in-memory blob per line beyond the line
// itself).
func jsonLoadAny(path string) (any, int, error) {
	abs := ResolvePath(path)
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, 0, fmt.Errorf("file not found: %s", abs)
	}
	if fi.Size() > 256<<20 {
		return nil, 0, fmt.Errorf("file too large (>%d MB)", 256)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, 0, err
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			// Fall back to JSONL interpretation (a stream of objects is a
			// valid file that merely lacks the wrapping array).
			if objs, lerr := jsonlDecode(trimmed); lerr == nil {
				return objs, len(objs), nil
			}
			return nil, 0, fmt.Errorf("parse JSON: %w", err)
		}
		n := 1
		if arr, ok := v.([]any); ok {
			n = len(arr)
		}
		return v, n, nil
	}
	objs, err := jsonlDecode(trimmed)
	if err != nil {
		return nil, 0, err
	}
	return objs, len(objs), nil
}

func jsonlDecode(text string) ([]any, error) {
	var objs []any
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		ln := strings.TrimSpace(sc.Text())
		if ln == "" {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(ln), &v); err != nil {
			return nil, fmt.Errorf("parse JSONL line: %w", err)
		}
		objs = append(objs, v)
	}
	if len(objs) == 0 {
		return nil, fmt.Errorf("no JSON objects found")
	}
	return objs, nil
}

// --- query ----------------------------------------------------------------

// jsonPathEval evaluates a dot/bracket path with [*] wildcard support.
// Returns the matched values (0 = not found).
func jsonPathEval(v any, path string) []any {
	if path == "" || path == "." {
		return []any{v}
	}
	segs := splitJSONPath(path)
	cur := []any{v}
	for _, seg := range segs {
		var next []any
		for _, item := range cur {
			switch seg.kind {
			case segKey:
				m, ok := item.(map[string]any)
				if ok {
					if val, exists := m[seg.key]; exists {
						next = append(next, val)
					}
				}
			case segIndex:
				arr, ok := item.([]any)
				if ok && seg.index >= 0 && seg.index < len(arr) {
					next = append(next, arr[seg.index])
				}
			case segWildcard:
				arr, ok := item.([]any)
				if ok {
					next = append(next, arr...)
				}
			}
		}
		if len(next) == 0 {
			return nil
		}
		cur = next
	}
	return cur
}

type pathSeg struct {
	kind  int // 0 key, 1 index, 2 wildcard
	key   string
	index int
}

const (
	segKey = iota
	segIndex
	segWildcard
)

// splitJSONPath parses "a.b[0][*].c" into segments.
func splitJSONPath(path string) []pathSeg {
	var segs []pathSeg
	i := 0
	for i < len(path) {
		switch path[i] {
		case '.':
			i++
		case '[':
			end := strings.IndexByte(path[i:], ']')
			if end < 0 {
				return segs
			}
			inner := path[i+1 : i+end]
			i += end + 1
			if inner == "*" {
				segs = append(segs, pathSeg{kind: segWildcard})
			} else if n, err := strconv.Atoi(inner); err == nil {
				segs = append(segs, pathSeg{kind: segIndex, index: n})
			}
		default:
			j := i
			for j < len(path) && path[j] != '.' && path[j] != '[' {
				j++
			}
			segs = append(segs, pathSeg{kind: segKey, key: path[i:j]})
			i = j
		}
	}
	return segs
}

func (t JSONTool) query(path, q string, limit int) (string, error) {
	if q == "" {
		return "", fmt.Errorf("query path is required (e.g. \"a.b[0].c\" or \"items[*].name\")")
	}
	v, _, err := jsonLoadAny(path)
	if err != nil {
		return "", err
	}
	hits := jsonPathEval(v, q)
	if len(hits) == 0 {
		return "no match for path " + q, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d match(es) for %s:\n", len(hits), q)
	for i, h := range hits {
		if i >= limit {
			fmt.Fprintf(&b, "… %d more (raise limit)\n", len(hits)-limit)
			break
		}
		line, err := json.Marshal(h)
		if err != nil {
			continue
		}
		if len(line) > 1024 {
			line = append(line[:1024], "…"...)
		}
		fmt.Fprintf(&b, "[%d] %s\n", i, line)
		if b.Len() > jsonMaxOut {
			b.WriteString("… output truncated\n")
			break
		}
	}
	return b.String(), nil
}

// --- where ------------------------------------------------------------------

func (t JSONTool) where(path, field, op, value string, limit int, format, dest string) (string, error) {
	if field == "" {
		return "", fmt.Errorf("field is required")
	}
	if op == "" {
		op = "eq"
	}
	abs := ResolvePath(path)
	f, err := os.Open(abs)
	if err != nil {
		return "", fmt.Errorf("file not found: %s", abs)
	}
	defer f.Close()
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	numVal, numErr := strconv.ParseFloat(value, 64)

	var b strings.Builder
	var matches []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		ln := strings.TrimSpace(sc.Text())
		if ln == "" {
			continue
		}
		var obj map[string]any
		if json.Unmarshal([]byte(ln), &obj) != nil {
			continue
		}
		if !jsonWhereMatch(obj, field, op, value, numVal, numErr == nil) {
			continue
		}
		matches = append(matches, ln)
		if len(matches) > 5000 {
			break // hard cap regardless of limit
		}
	}
	// JSON array input: treat each element as a row too.
	if len(matches) == 0 {
		if v, _, err := jsonLoadAny(path); err == nil {
			if arr, ok := v.([]any); ok {
				for _, item := range arr {
					obj, ok := item.(map[string]any)
					if !ok {
						continue
					}
					if jsonWhereMatch(obj, field, op, value, numVal, numErr == nil) {
						if line, merr := json.Marshal(obj); merr == nil {
							matches = append(matches, string(line))
						}
					}
				}
			}
		}
	}
	fmt.Fprintf(&b, "%d row(s) where %s %s %q\n", len(matches), field, op, value)
	for i, m := range matches {
		if i >= limit {
			fmt.Fprintf(&b, "… %d more (raise limit)\n", len(matches)-limit)
			break
		}
		if len(m) > 1024 {
			m = m[:1024] + "…"
		}
		fmt.Fprintf(&b, "[%d] %s\n", i, m)
		if b.Len() > jsonMaxOut {
			b.WriteString("… output truncated\n")
			break
		}
	}
	if dest != "" && len(matches) > 0 {
		dst := ResolvePath(dest)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return b.String(), err
		}
		var out []byte
		if strings.EqualFold(format, "json") {
			arr := make([]any, 0, len(matches))
			for _, m := range matches {
				var v any
				if json.Unmarshal([]byte(m), &v) == nil {
					arr = append(arr, v)
				}
			}
			data, _ := json.Marshal(arr)
			out = data
		} else {
			for _, m := range matches {
				out = append(out, m...)
				out = append(out, '\n')
			}
		}
		if err := os.WriteFile(dst, out, 0o644); err != nil {
			return b.String(), err
		}
		fmt.Fprintf(&b, "matched rows written to %s\n", dest)
	}
	return b.String(), nil
}

func jsonWhereMatch(obj map[string]any, field, op, value string, numVal float64, numOK bool) bool {
	raw, ok := obj[field]
	if !ok {
		return false
	}
	switch strings.ToLower(op) {
	case "eq", "==":
		return jsonScalarString(raw) == value
	case "ne", "!=":
		return jsonScalarString(raw) != value
	case "contains":
		return strings.Contains(strings.ToLower(jsonScalarString(raw)), strings.ToLower(value))
	case "gt", ">", "lt", "<", "gte", ">=", "lte", "<=":
		if !numOK {
			return false
		}
		f, ok := jsonScalarFloat(raw)
		if !ok {
			return false
		}
		switch op {
		case "gt", ">":
			return f > numVal
		case "lt", "<":
			return f < numVal
		case "gte", ">=":
			return f >= numVal
		default:
			return f <= numVal
		}
	default:
		return false
	}
}

func jsonScalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return ""
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func jsonScalarFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// --- stats / keys -------------------------------------------------------------

func (t JSONTool) stats(path string) (string, error) {
	v, n, err := jsonLoadAny(path)
	if err != nil {
		return "", err
	}
	rows := normalizeJSONRows(v)
	keyFreq := map[string]int{}
	keyTypes := map[string]map[string]int{}
	depth := jsonDepth(v)
	for _, row := range rows {
		for k, val := range row {
			keyFreq[k]++
			tname := jsonTypeName(val)
			if keyTypes[k] == nil {
				keyTypes[k] = map[string]int{}
			}
			keyTypes[k][tname]++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "objects: %d | depth: %d\n", n, depth)
	if len(rows) > 0 {
		fmt.Fprintf(&b, "keys (frequency, types):\n")
		keys := make([]string, 0, len(keyFreq))
		for k := range keyFreq {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keyFreq[keys[i]] > keyFreq[keys[j]] })
		for i, k := range keys {
			if i >= 30 {
				fmt.Fprintf(&b, "… %d more keys\n", len(keys)-30)
				break
			}
			types := keyTypes[k]
			var tparts []string
			for tn, c := range types {
				tparts = append(tparts, fmt.Sprintf("%s×%d", tn, c))
			}
			sort.Strings(tparts)
			fmt.Fprintf(&b, "  %-24s %5d  (%s)\n", k, keyFreq[k], strings.Join(tparts, ", "))
		}
	}
	return b.String(), nil
}

func normalizeJSONRows(v any) []map[string]any {
	var rows []map[string]any
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				rows = append(rows, m)
			}
		}
	case map[string]any:
		rows = append(rows, x)
	}
	return rows
}

func jsonTypeName(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	case nil:
		return "null"
	case []any:
		return "array"
	default:
		return "object"
	}
}

func jsonDepth(v any) int {
	switch x := v.(type) {
	case map[string]any:
		max := 0
		for _, val := range x {
			if d := jsonDepth(val); d > max {
				max = d
			}
		}
		return 1 + max
	case []any:
		max := 0
		for _, val := range x {
			if d := jsonDepth(val); d > max {
				max = d
			}
		}
		return 1 + max
	default:
		return 0
	}
}

func (t JSONTool) keys(path string, limit int) (string, error) {
	v, _, err := jsonLoadAny(path)
	if err != nil {
		return "", err
	}
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	set := map[string]bool{}
	var walk func(node any, prefix string)
	walk = func(node any, prefix string) {
		switch x := node.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				if len(set) < 2000 {
					set[p] = true
				}
				walk(x[k], p)
			}
		case []any:
			if len(x) > 0 {
				walk(x[0], prefix+"[0]")
			}
		}
	}
	walk(v, "")
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	fmt.Fprintf(&b, "%d unique key path(s)\n", len(paths))
	for i, p := range paths {
		if i >= limit {
			fmt.Fprintf(&b, "… %d more (raise limit)\n", len(paths)-limit)
			break
		}
		fmt.Fprintf(&b, "  %s\n", p)
	}
	return b.String(), nil
}

// --- pretty / flatten -----------------------------------------------------------

func (t JSONTool) pretty(path, dest string) (string, error) {
	abs := ResolvePath(path)
	raw, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("file not found: %s", abs)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("parse JSON: %w", err)
	}
	indented, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	out := abs
	if dest != "" {
		out = ResolvePath(dest)
	}
	if err := os.WriteFile(out, append(indented, '\n'), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("pretty-printed %d bytes -> %s (compact source was %d bytes)", len(indented), filepath.Base(out), len(raw)), nil
}

func (t JSONTool) flatten(path, dest string) (string, error) {
	v, _, err := jsonLoadAny(path)
	if err != nil {
		return "", err
	}
	rows := normalizeJSONRows(v)
	flat := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		f := map[string]any{}
		flattenInto(row, "", f)
		flat = append(flat, f)
	}
	var out []byte
	if len(flat) == 1 {
		out, err = json.MarshalIndent(flat[0], "", "  ")
	} else {
		out, err = json.MarshalIndent(flat, "", "  ")
	}
	if err != nil {
		return "", err
	}
	dst := ResolvePath(dest)
	if dest == "" {
		dst = ResolvePath(strings.TrimSuffix(filepath.Base(ResolvePath(path)), filepath.Ext(path)) + ".flat.json")
	}
	if err := os.WriteFile(dst, append(out, '\n'), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("flattened %d object(s), %d byte(s) -> %s", len(flat), len(out), filepath.Base(dst)), nil
}

func flattenInto(m map[string]any, prefix string, out map[string]any) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok && len(sub) > 0 {
			flattenInto(sub, key, out)
			continue
		}
		out[key] = v
	}
}
