// Package tools — v1.8.7 data-analysis authority expansion.
//
// This file upgrades the existing dataAnalysis tool (the ONE data tool —
// no second manager/authority) with the deterministic high-value
// operations that move computation out of the model and into local
// execution, so the model receives compact results instead of raw
// datasets:
//
//	analyze    — one call, complete compact dataset analysis
//	aggregate  — multiple aggregations + multiple grouping columns in one call
//	join       — deterministic local joins between two datasets
//	quality    — compact data-quality diagnostics
//	export     — materialize results as CSV/TSV/JSON artifacts
//
// Output philosophy (v1.8.7, "reduce model token load"):
//
//	what was measured → compact structured result → key findings →
//	artifact path when more detail exists
//
// Every new action supports three output modes — compact (default),
// table and json — and stamps compact provenance metadata (backend,
// bytes, rows, cols) so a result is reproducible and auditable.
//
// Backend decision (v1.8.7): the heavy-data backend evaluation kept the
// pure-Go in-process engine. DuckDB's Go client is cgo with a statically
// linked bundled engine — it would add a C toolchain requirement and
// tens of megabytes to every packaged target (Windows x64 and Linux
// x64) for workload sizes the 256 MB pure-Go fast path already covers.
// Parquet and SQL are therefore NOT added; the pure-Go `query`, join
// and aggregate actions remain the relational authority. Large files
// keep the honest 256 MB input bound (no fake streaming claim): split,
// filter or aggregate with the existing actions instead.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// joinRowCap bounds a materialized join result (bounded memory — a
// pathological many-to-many key must not be able to grow the output
// without limit). Real analytical joins land far below this; past the
// cap the action fails with guidance instead of silently truncating.
const joinRowCap = 2_000_000

// errContextCanceled is the uniform early-exit error for cancelled scans.
func ctxErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cancelled: %w", err)
	}

	return nil
}

// outMode normalizes the requested output mode: compact (default),
// table or json.
func outMode(p *dataParams) string {
	switch strings.ToLower(strings.TrimSpace(p.Mode)) {
	case "table":
		return "table"
	case "json", "raw":
		return "json"
	default:
		return "compact"
	}
}

// datasetMeta returns the compact provenance line stamped into every
// v1.8.7 analysis result: real measured values only (file bytes, rows,
// columns, backend identity).
func datasetMeta(d *dataset) string {
	bytes := int64(0)
	if fi, err := os.Stat(d.Path); err == nil {
		bytes = fi.Size()
	}

	return fmt.Sprintf("backend=in-process pure-go; bytes=%d; rows=%d; cols=%d", bytes, len(d.Rows), len(d.Columns))
}

// materializeDataset writes a result dataset as a CSV/TSV/JSON artifact
// next to the source (or under the base dir for joins). Shared by
// aggregate/join/export. Returns the absolute artifact path.
func materializeDataset(d *dataset, format, name, fallbackBase string) (string, error) {
	format = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), "."))
	switch format {
	case "csv", "tsv", "json":
	default:
		return "", fmt.Errorf("unsupported format %q (csv|tsv|json)", format)
	}

	if name == "" {
		name = fallbackBase + "." + format
	}

	if !strings.HasSuffix(name, "."+format) {
		name += "." + format
	}

	dir := ""
	if d.Path != "" {
		dir = filepath.Dir(d.Path)
	} else {
		dir = BaseDir()
	}

	out := filepath.Join(dir, sanitizeName(name))

	var data []byte
	var err error

	switch format {
	case "json":
		data, err = datasetToJSON(d)
	default: // csv / tsv
		delims := ","
		if format == "tsv" {
			delims = "\t"
		}

		var b strings.Builder
		b.WriteString(strings.Join(d.Columns, delims) + "\n")
		for r := range d.Rows {
			cells := make([]string, len(d.Columns))
			for i, v := range d.Rows[r] {
				if strings.ContainsAny(v, "\""+delims+"\n") {
					cells[i] = "\"" + strings.ReplaceAll(v, "\"", "\"\"") + "\""
				} else {
					cells[i] = v
				}
			}
			b.WriteString(strings.Join(cells, delims) + "\n")
		}
		data = []byte(b.String())
	}
	if err != nil {
		return "", err
	}

	if werr := os.WriteFile(out, data, 0o644); werr != nil {
		return "", werr
	}

	return out, nil
}

// datasetToJSON renders a dataset as a JSON array of row objects
// (numeric columns typed as numbers, missing cells as null).
func datasetToJSON(d *dataset) ([]byte, error) {
	objs := make([]map[string]any, len(d.Rows))
	for r := range d.Rows {
		o := make(map[string]any, len(d.Columns))
		for i, c := range d.Columns {
			v := d.Rows[r][i]
			if isMissing(strings.TrimSpace(v)) {
				o[c] = nil
				continue
			}
			if d.Types != nil && d.Types[i] == typeNumber {
				if f := parseNumber(v); !math.IsNaN(f) {
					o[c] = f
					continue
				}
			}
			o[c] = v
		}
		objs[r] = o
	}

	return json.MarshalIndent(objs, "", "  ")
}

// ---------------------------------------------------------------------------
// analyze — one call, complete compact dataset analysis
// ---------------------------------------------------------------------------

// analyzeSections is the section registry for the analyze action.
var analyzeSections = map[string]bool{
	"schema": true, "missing": true, "stats": true, "categorical": true,
	"correlations": true, "outliers": true, "groupby": true,
	"sample": true, "findings": true, "all": true,
}

// defaultAnalyzeSections is the compact default: enough for the model to
// answer most questions in one call without raw-table flooding.
const defaultAnalyzeSections = "schema,missing,stats,categorical,outliers,findings"

// actionAnalyze runs the configured analysis sections in one deterministic
// pass and returns a compact, model-oriented report.
func (t *DataTool) actionAnalyze(ctx context.Context, p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}

	if err := ctxErr(ctx); err != nil {
		return "", err
	}

	sections := strings.ToLower(strings.TrimSpace(p.Sections))
	if sections == "" {
		sections = defaultAnalyzeSections
	}

	for _, s := range strings.Split(sections, ",") {
		if !analyzeSections[strings.TrimSpace(s)] {
			return "", fmt.Errorf("unknown section %q (schema|missing|stats|categorical|correlations|outliers|groupby|sample|findings|all)", strings.TrimSpace(s))
		}
	}

	want := func(name string) bool {
		for _, s := range strings.Split(sections, ",") {
			s = strings.TrimSpace(s)
			if s == "all" || s == name {
				return true
			}
		}

		return false
	}

	mode := outMode(p)

	rep := newAnalyzeReport(d, mode)

	if want("schema") {
		rep.schemaSection()
	}
	if want("missing") {
		rep.missingSection()
	}
	if want("stats") {
		rep.statsSection()
	}
	if want("categorical") {
		rep.categoricalSection()
	}
	if want("correlations") {
		rep.correlationsSection()
	}
	if want("outliers") {
		rep.outliersSection()
	}
	if want("groupby") && p.By != "" {
		if err := rep.groupBySection(p); err != nil {
			return "", err
		}
	}
	if want("sample") {
		rep.sampleSection(p)
	}
	if want("findings") {
		rep.findingsSection()
	}

	if err := ctxErr(ctx); err != nil {
		return "", err
	}

	return rep.render()
}

// analyzeReport accumulates one analyze run. Sections append compact
// blocks; json mode collects the same measurements as structured rows.
type analyzeReport struct {
	d    *dataset
	mode string

	lines  []string
	fields []map[string]any
}

func newAnalyzeReport(d *dataset, mode string) *analyzeReport {
	rep := &analyzeReport{d: d, mode: mode}

	if mode == "json" {
		rep.fields = append(rep.fields, map[string]any{
			"section": "meta",
			"file":    filepath.Base(d.Path),
			"meta":    datasetMeta(d),
		})

		return rep
	}

	rep.lines = append(rep.lines,
		fmt.Sprintf("Dataset %s — %d rows × %d columns", filepath.Base(d.Path), len(d.Rows), len(d.Columns)),
		datasetMeta(d),
	)

	return rep
}

func (rep *analyzeReport) schemaSection() {
	d := rep.d
	if rep.mode == "json" {
		for i, c := range d.Columns {
			rep.fields = append(rep.fields, map[string]any{
				"section": "schema", "column": c, "type": string(d.Types[i]),
				"distinct": d.distinctCount(i),
			})
		}

		return
	}

	rep.lines = append(rep.lines, "", "Schema (type / missing / distinct):")
	for i, c := range d.Columns {
		missing := d.missingCount(i)
		pct := 0.0
		if len(d.Rows) > 0 {
			pct = float64(missing) / float64(len(d.Rows)) * 100
		}
		rep.lines = append(rep.lines, fmt.Sprintf("  %-20s %-7s %5.1f%% missing %6d distinct",
			clipStr(c, 20), d.Types[i], pct, d.distinctCount(i)))
	}
}

func (rep *analyzeReport) missingSection() {
	d := rep.d
	var cols []int
	for i := range d.Columns {
		if d.missingCount(i) > 0 {
			cols = append(cols, i)
		}
	}

	if len(cols) == 0 {
		if rep.mode == "json" {
			rep.fields = append(rep.fields, map[string]any{"section": "missing", "columns": []string{}})
		} else {
			rep.lines = append(rep.lines, "", "Missingness: none — the dataset is complete.")
		}

		return
	}

	if rep.mode == "json" {
		for _, i := range cols {
			rep.fields = append(rep.fields, map[string]any{
				"section": "missing", "column": d.Columns[i],
				"missing": d.missingCount(i),
			})
		}

		return
	}

	rep.lines = append(rep.lines, "", "Missingness (only affected columns):")
	for _, i := range cols {
		pct := float64(d.missingCount(i)) / float64(len(d.Rows)) * 100
		rep.lines = append(rep.lines, fmt.Sprintf("  %-20s %6d (%.1f%%)", clipStr(d.Columns[i], 20), d.missingCount(i), pct))
	}
}

func (rep *analyzeReport) statsSection() {
	d := rep.d
	nums := d.numericCols()
	if len(nums) == 0 {
		if rep.mode == "json" {
			rep.fields = append(rep.fields, map[string]any{"section": "stats", "note": "no numeric columns"})
		}

		return
	}

	if rep.mode == "json" {
		for _, i := range nums {
			st := computeStats(d, i)
			rep.fields = append(rep.fields, map[string]any{
				"section": "stats", "column": d.Columns[i],
				"count": st.Count, "missing": st.Missing,
				"mean": st.Mean, "std": st.Std, "min": st.Min,
				"q1": st.Q1, "median": st.Median, "q3": st.Q3, "max": st.Max,
				"sum": st.Sum,
			})
		}

		return
	}

	rep.lines = append(rep.lines, "", "Numeric summary (mean / median / std / min…max):")
	for _, i := range nums {
		st := computeStats(d, i)
		rep.lines = append(rep.lines, fmt.Sprintf("  %-20s %s / %s / %s / %s…%s (sum %s)",
			clipStr(d.Columns[i], 20), fmtNum(st.Mean), fmtNum(st.Median),
			fmtNum(st.Std), fmtNum(st.Min), fmtNum(st.Max), fmtNum(st.Sum)))
	}
}

func (rep *analyzeReport) categoricalSection() {
	d := rep.d
	found := false
	for i := range d.Columns {
		if d.Types[i] == typeNumber {
			continue
		}

		counts := d.valueCounts(i)
		if len(counts) == 0 || len(counts) > 50 {
			continue // high-cardinality or empty — not a compact category
		}

		found = true
		top := 3
		if len(counts) < top {
			top = len(counts)
		}

		parts := make([]string, 0, top)
		for k := 0; k < top; k++ {
			share := float64(counts[k].n) / float64(len(d.Rows)) * 100
			parts = append(parts, fmt.Sprintf("%q %.0f%%", clipStr(counts[k].v, 18), share))
		}

		if rep.mode == "json" {
			rep.fields = append(rep.fields, map[string]any{
				"section": "categorical", "column": d.Columns[i],
				"distinct": len(counts), "top": parts,
			})
		} else {
			rep.lines = append(rep.lines, fmt.Sprintf("  %-20s %d distinct — top: %s",
				clipStr(d.Columns[i], 20), len(counts), strings.Join(parts, ", ")))
		}
	}

	if !found && rep.mode != "json" {
		rep.lines = append(rep.lines, "", "Categorical: no low-cardinality string columns.")
	}
}

func (rep *analyzeReport) correlationsSection() {
	d := rep.d
	cols := d.numericCols()
	if len(cols) < 2 {
		// v1.8.8 honesty: a REQUESTED section must answer, not silently
		// vanish — fewer than two numeric columns is a real finding.
		if rep.mode == "json" {
			rep.fields = append(rep.fields, map[string]any{"section": "correlations", "pairs": []string{}})
		} else {
			rep.lines = append(rep.lines, "", "Correlations: fewer than two numeric columns — nothing to correlate.")
		}
		return
	}

	type pair struct {
		a, b int
		r    float64
	}

	var strong []pair
	for i := 0; i < len(cols); i++ {
		for j := i + 1; j < len(cols); j++ {
			xs, ys := d.pairwiseComplete(cols[i], cols[j])
			r := pearson(xs, ys)
			if !math.IsNaN(r) && math.Abs(r) >= 0.6 {
				strong = append(strong, pair{cols[i], cols[j], r})
			}
		}
	}

	if len(strong) == 0 {
		if rep.mode == "json" {
			rep.fields = append(rep.fields, map[string]any{"section": "correlations", "pairs": []string{}})
		} else {
			rep.lines = append(rep.lines, "", "Correlations: no pair with |r| ≥ 0.6.")
		}

		return
	}

	// v1.8.8 determinism: equal |r| pairs get an explicit tie-breaker
	// (ascending column indexes) instead of relying on sort.Slice
	// internals — the reported ordering is a pure function of the input.
	sort.SliceStable(strong, func(a, b int) bool {
		ra, rb := math.Abs(strong[a].r), math.Abs(strong[b].r)
		if ra != rb {
			return ra > rb
		}
		if strong[a].a != strong[b].a {
			return strong[a].a < strong[b].a
		}
		return strong[a].b < strong[b].b
	})

	if rep.mode == "json" {
		for _, pr := range strong {
			rep.fields = append(rep.fields, map[string]any{
				"section": "correlations",
				"pair":    d.Columns[pr.a] + ":" + d.Columns[pr.b],
				"r":       pr.r,
			})
		}

		return
	}

	rep.lines = append(rep.lines, "", "Correlations (|r| ≥ 0.6):")
	for _, pr := range strong {
		rep.lines = append(rep.lines, fmt.Sprintf("  %s ↔ %s r=%s",
			d.Columns[pr.a], d.Columns[pr.b], fmtNum(pr.r)))
	}
}

func (rep *analyzeReport) outliersSection() {
	d := rep.d
	nums := d.numericCols()
	anyOut := false

	for _, i := range nums {
		lo, hi, n := d.iqrFences(i)
		if n == 0 {
			continue
		}

		anyOut = true
		if rep.mode == "json" {
			rep.fields = append(rep.fields, map[string]any{
				"section": "outliers", "column": d.Columns[i],
				"count": n, "fenceLow": lo, "fenceHigh": hi,
			})
		} else {
			rep.lines = append(rep.lines, fmt.Sprintf("  %-20s %d outlier(s) beyond IQR fences [%s, %s]",
				clipStr(d.Columns[i], 20), n, fmtNum(lo), fmtNum(hi)))
		}
	}

	if !anyOut && rep.mode != "json" {
		rep.lines = append(rep.lines, "", "Outliers: none beyond the IQR fences.")
	}
}

func (rep *analyzeReport) groupBySection(p *dataParams) error {
	aggReq := &dataParams{
		Path:    p.Path,
		By:      p.By,
		Column:  p.Column,
		Agg:     p.Agg,
		Mode:    rep.mode,
		Limit:   p.Limit,
		Columns: p.Columns,
		Aggs:    p.Aggs,
	}

	rows, cols, types, err := rep.d.aggregateCompute(aggReq)
	if err != nil {
		return err
	}

	res := &dataset{Columns: cols, Types: types, Rows: rows}

	if rep.mode == "json" {
		rep.fields = append(rep.fields, map[string]any{
			"section": "groupby", "by": p.By, "groups": len(rows),
			"preview": rowsPreview(res, 10),
		})

		return nil
	}

	rep.lines = append(rep.lines, "", fmt.Sprintf("Group by %q (%d groups, top %d):", p.By, len(rows), minInt(len(rows), 10)))
	var b strings.Builder
	writeTable(&b, res, 0, minInt(len(rows), 10), nil)
	rep.lines = append(rep.lines, b.String())

	return nil
}

func (rep *analyzeReport) sampleSection(p *dataParams) {
	d := rep.d
	limit := p.Limit
	if limit <= 0 {
		limit = 3
	}

	if rep.mode == "json" {
		rep.fields = append(rep.fields, map[string]any{
			"section": "sample", "rows": rowsPreview(d, minInt(limit, 10)),
		})

		return
	}

	rep.lines = append(rep.lines, "", "First rows:")
	var b strings.Builder
	writeTable(&b, d, 0, minInt(limit, 10), nil)
	rep.lines = append(rep.lines, b.String())
}

func (rep *analyzeReport) findingsSection() {
	d := rep.d
	var findings []string

	// Duplicate rows (deterministic map + first-seen scan).
	if dups := d.duplicateRowCount(); dups > 0 {
		findings = append(findings, fmt.Sprintf("%d duplicate row(s) — dedupe before aggregation", dups))
	}

	// Missingness extremes.
	for i := range d.Columns {
		missing := d.missingCount(i)
		if len(d.Rows) > 0 && float64(missing)/float64(len(d.Rows)) >= 0.3 {
			findings = append(findings, fmt.Sprintf("%q is %.0f%% missing", d.Columns[i],
				float64(missing)/float64(len(d.Rows))*100))
		}
	}

	// Constant columns.
	for i := range d.Columns {
		if len(d.Rows) > 1 && d.distinctCount(i) <= 1 && d.missingCount(i) < len(d.Rows) {
			findings = append(findings, fmt.Sprintf("%q is constant — carries no signal", d.Columns[i]))
		}
	}

	// Dominant categories.
	for i := range d.Columns {
		if d.Types[i] == typeNumber {
			continue
		}

		counts := d.valueCounts(i)
		if len(counts) == 0 || len(counts) > 10 {
			continue
		}

		share := float64(counts[0].n) / float64(len(d.Rows))
		if share >= 0.6 {
			findings = append(findings, fmt.Sprintf("%q is dominated by %q (%.0f%%)",
				d.Columns[i], clipStr(counts[0].v, 24), share*100))
		}
	}

	// Strongest correlation.
	if pair, r, ok := d.strongestCorrelation(0.6); ok {
		findings = append(findings, fmt.Sprintf("%s and %s correlate strongly (r=%s)",
			d.Columns[pair[0]], d.Columns[pair[1]], fmtNum(r)))
	}

	// Outlier totals.
	for _, i := range d.numericCols() {
		if _, _, n := d.iqrFences(i); n > 0 {
			findings = append(findings, fmt.Sprintf("%q has %d outlier(s) beyond the IQR fences", d.Columns[i], n))
		}
	}

	if len(findings) == 0 {
		findings = append(findings, "no notable issues — balanced schema, no missing values, no outliers")
	}

	if rep.mode == "json" {
		rep.fields = append(rep.fields, map[string]any{"section": "findings", "findings": findings})

		return
	}

	rep.lines = append(rep.lines, "", "Key findings:")
	for _, f := range findings {
		rep.lines = append(rep.lines, "  • "+f)
	}
}

func (rep *analyzeReport) render() (string, error) {
	if rep.mode == "json" {
		out, err := json.MarshalIndent(rep.fields, "", "  ")
		if err != nil {
			return "", err
		}

		return string(out), nil
	}

	return strings.Join(rep.lines, "\n") + "\n", nil
}

// --- dataset measurement helpers (shared by analyze/quality) ---

func (d *dataset) missingCount(col int) int {
	n := 0
	for r := range d.Rows {
		if isMissing(strings.TrimSpace(d.Rows[r][col])) {
			n++
		}
	}

	return n
}

func (d *dataset) distinctCount(col int) int {
	seen := make(map[string]struct{}, 64)
	for r := range d.Rows {
		v := strings.TrimSpace(d.Rows[r][col])
		if isMissing(v) {
			continue
		}
		seen[v] = struct{}{}
	}

	return len(seen)
}

type valCount struct {
	v string
	n int
}

// valueCounts returns non-missing value frequencies of a column,
// descending by count, ties in first-seen order (deterministic).
func (d *dataset) valueCounts(col int) []valCount {
	order := []string{}
	counts := map[string]int{}
	firstSeen := map[string]int{}
	for r := range d.Rows {
		v := strings.TrimSpace(d.Rows[r][col])
		if isMissing(v) {
			continue
		}
		if _, ok := counts[v]; !ok {
			order = append(order, v)
			firstSeen[v] = len(order) - 1
		}
		counts[v]++
	}

	out := make([]valCount, 0, len(counts))
	for _, v := range order {
		out = append(out, valCount{v: v, n: counts[v]})
	}

	sort.SliceStable(out, func(a, b int) bool {
		if out[a].n != out[b].n {
			return out[a].n > out[b].n
		}
		return firstSeen[out[a].v] < firstSeen[out[b].v]
	})

	return out
}

// pairwiseComplete returns both columns' parsed values restricted to rows
// where BOTH are present (in row order — deterministic).
func (d *dataset) pairwiseComplete(a, b int) ([]float64, []float64) {
	xs := d.numericColumn(a)
	ys := d.numericColumn(b)
	xa := make([]float64, 0, len(xs))
	ya := make([]float64, 0, len(ys))
	for r := range xs {
		x, y := xs[r], ys[r]
		if !math.IsNaN(x) && !math.IsNaN(y) {
			xa = append(xa, x)
			ya = append(ya, y)
		}
	}

	return xa, ya
}

// strongestCorrelation finds the strongest |r| pair at or above minAbsR.
func (d *dataset) strongestCorrelation(minAbsR float64) ([2]int, float64, bool) {
	cols := d.numericCols()
	best := [2]int{-1, -1}
	bestR := math.NaN()
	found := false

	for i := 0; i < len(cols); i++ {
		for j := i + 1; j < len(cols); j++ {
			xs, ys := d.pairwiseComplete(cols[i], cols[j])
			r := pearson(xs, ys)
			if math.IsNaN(r) {
				continue
			}
			if !found || math.Abs(r) > math.Abs(bestR) {
				if math.Abs(r) >= minAbsR {
					best, bestR, found = [2]int{cols[i], cols[j]}, r, true
				}
			}
		}
	}

	return best, bestR, found
}

// iqrFences computes the IQR outlier fences of a numeric column and the
// number of present values outside them.
func (d *dataset) iqrFences(col int) (lo, hi float64, count int) {
	vals := d.numericColumn(col)
	present := make([]float64, 0, len(vals))
	for _, v := range vals {
		if !math.IsNaN(v) {
			present = append(present, v)
		}
	}

	if len(present) < 4 {
		return 0, 0, 0
	}

	sorted := append([]float64(nil), present...)
	sort.Float64s(sorted)
	q1 := quantile(sorted, 0.25)
	q3 := quantile(sorted, 0.75)
	iqr := q3 - q1
	if iqr == 0 {
		// Constant column: anything different is an outlier by fence
		// arithmetic, but a constant column carries no outliers.
		for _, v := range sorted {
			if v != q1 {
				count++
			}
		}

		return q1, q3, count
	}

	lo, hi = q1-1.5*iqr, q3+1.5*iqr
	for _, v := range sorted {
		if v < lo || v > hi {
			count++
		}
	}

	return lo, hi, count
}

// duplicateRowCount counts rows (beyond the first occurrence) whose full
// cell tuple was already seen — deterministic, order-independent.
func (d *dataset) duplicateRowCount() int {
	seen := make(map[string]struct{}, len(d.Rows))
	dups := 0
	for r := range d.Rows {
		key := strings.Join(d.Rows[r], "\x1f")
		if _, ok := seen[key]; ok {
			dups++
			continue
		}
		seen[key] = struct{}{}
	}

	return dups
}

func rowsPreview(d *dataset, limit int) []map[string]any {
	if limit > len(d.Rows) {
		limit = len(d.Rows)
	}

	out := make([]map[string]any, 0, limit)
	for r := 0; r < limit; r++ {
		o := map[string]any{}
		for i, c := range d.Columns {
			if d.Types != nil && d.Types[i] == typeNumber {
				if f := parseNumber(d.Rows[r][i]); !math.IsNaN(f) {
					o[c] = f
					continue
				}
			}
			o[c] = d.Rows[r][i]
		}
		out = append(out, o)
	}

	return out
}
