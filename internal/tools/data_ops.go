package tools

// v1.8.7 — aggregate / join / quality / export actions. These live in
// their own file to keep the v1.0.9 TURBINE analysis file focused on the
// statistics core. Same authority (DataTool), same dataset model, same
// parse-once numeric cache.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// ---------------------------------------------------------------------------
// aggregate — multiple aggregations in one deterministic call
// ---------------------------------------------------------------------------

// aggregateCompute is the engine behind aggregate and analyze's groupby
// section. Aggregation specs use the "<agg>:<column>" grammar:
//
//	count            group row count (no column)
//	sum:col mean:col min:col max:col median:col std:col
//	quantile:col     uses the q parameter (default 0.5)
//	avg:col          alias of mean:col
//
// Bare aggregation names ("sum") combined with `columns` expand to one
// spec per column. Groups are returned sorted ascending by group key —
// the result is a pure function of the dataset.
func (d *dataset) aggregateCompute(p *dataParams) (rows [][]string, cols []string, types []colType, err error) {
	// Resolve the grouping columns (byList wins over by).
	var groupIdx []string
	var groupCols []int
	byList := p.ByList
	if len(byList) == 0 && p.By != "" {
		byList = []string{p.By}
	}

	for _, g := range byList {
		ci, cerr := d.colIndex(g)
		if cerr != nil {
			return nil, nil, nil, cerr
		}
		groupCols = append(groupCols, ci)
		groupIdx = append(groupIdx, d.Columns[ci])
	}

	// Expand the aggregation specs.
	type aggSpec struct {
		kind string
		col  int // -1 for count
		name string
	}
	var specs []aggSpec

	expand := func(spec string) error {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			return nil
		}

		kind, colName := spec, ""
		if i := strings.IndexByte(spec, ':'); i >= 0 {
			kind, colName = spec[:i], spec[i+1:]
		}

		kind = strings.ToLower(kind)
		if kind == "" {
			return fmt.Errorf("empty aggregation spec")
		}

		if kind == "count" && colName == "" {
			specs = append(specs, aggSpec{kind: "count", col: -1, name: "count"})
			return nil
		}

		if colName == "" {
			// Bare column aggregation without a column → invalid unless
			// the caller supplied `columns` (expanded by the caller).
			return fmt.Errorf("aggregation %q needs a column (\"%s:<column>\")", kind, kind)
		}

		ci, cerr := d.colIndex(colName)
		if cerr != nil {
			return cerr
		}

		if kind != "count" && d.Types[ci] != typeNumber {
			return fmt.Errorf("column %q is not numeric (agg %s needs numbers)", colName, kind)
		}

		display := colName
		switch kind {
		case "avg":
			kind = "mean"
		case "median":
			kind = "quantile"
		}
		specs = append(specs, aggSpec{kind: kind, col: ci, name: kind + "_" + display})

		return nil
	}

	hasBare := false
	for _, a := range p.Aggs {
		trimmed := strings.TrimSpace(a)
		if trimmed == "" {
			continue
		}
		if !strings.Contains(trimmed, ":") && !strings.EqualFold(trimmed, "count") {
			hasBare = true
			continue
		}
		if err := expand(trimmed); err != nil {
			return nil, nil, nil, err
		}
	}

	// Bare names + `columns` shorthand: apply every bare agg to every
	// listed column.
	if hasBare && len(p.Columns) > 0 {
		var bare []string
		for _, a := range p.Aggs {
			t := strings.TrimSpace(a)
			if t != "" && !strings.Contains(t, ":") && !strings.EqualFold(t, "count") {
				bare = append(bare, t)
			}
		}
		for _, a := range p.Aggs {
			if strings.EqualFold(strings.TrimSpace(a), "count") {
				if err := expand("count"); err != nil {
					return nil, nil, nil, err
				}
			}
		}
		for _, b := range bare {
			for _, c := range p.Columns {
				if err := expand(b + ":" + c); err != nil {
					return nil, nil, nil, err
				}
			}
		}
	} else {
		for _, a := range p.Aggs {
			if strings.EqualFold(strings.TrimSpace(a), "count") {
				if err := expand("count"); err != nil {
					return nil, nil, nil, err
				}
			}
		}
	}

	if len(specs) == 0 {
		if err := expand("count"); err != nil {
			return nil, nil, nil, err
		}
	}

	// Group and collect per-spec values in one pass.
	type group struct {
		key  string
		keys []string
		vals [][]float64 // one slice per spec with col >= 0
		n    int
	}
	groups := map[string]*group{}
	var order []string

	for r := range d.Rows {
		keys := make([]string, len(groupCols))
		for gi, ci := range groupCols {
			keys[gi] = strings.TrimSpace(d.Rows[r][ci])
		}
		key := strings.Join(keys, "\x1f")

		g := groups[key]
		if g == nil {
			g = &group{key: key, keys: keys, vals: make([][]float64, len(specs))}
			groups[key] = g
			order = append(order, key)
		}
		g.n++
		for si, sp := range specs {
			if sp.col < 0 {
				continue
			}
			if v := parseNumber(d.Rows[r][sp.col]); !math.IsNaN(v) {
				g.vals[si] = append(g.vals[si], v)
			}
		}
	}

	// Deterministic group order: ascending by composite key.
	sort.Strings(order)

	q := p.Q
	if q <= 0 || q >= 1 {
		q = 0.5
	}

	cols = append(cols, groupIdx...)
	types = append(types, make([]colType, len(groupIdx))...)
	for i := range types[:len(groupIdx)] {
		types[i] = typeString
	}
	for _, sp := range specs {
		cols = append(cols, sp.name)
		types = append(types, typeNumber)
	}

	rows = make([][]string, 0, len(order))
	for _, key := range order {
		g := groups[key]
		row := make([]string, 0, len(cols))
		row = append(row, g.keys...)

		for si, sp := range specs {
			vals := g.vals[si]
			switch sp.kind {
			case "count":
				if sp.col < 0 {
					row = append(row, strconv.Itoa(g.n))
				} else {
					row = append(row, strconv.Itoa(len(vals)))
				}
			default:
				row = append(row, fmtNum(aggValue(sp.kind, vals, q)))
			}
		}

		rows = append(rows, row)
	}

	return rows, cols, types, nil
}

// aggValue computes one aggregation over present values (sorted copy for
// the order-dependent kinds).
func aggValue(kind string, vals []float64, q float64) float64 {
	if len(vals) == 0 {
		return math.NaN()
	}

	switch kind {
	case "sum":
		s := 0.0
		for _, v := range vals {
			s += v
		}
		return s
	case "mean":
		s := 0.0
		for _, v := range vals {
			s += v
		}
		return s / float64(len(vals))
	case "min":
		m := vals[0]
		for _, v := range vals {
			if v < m {
				m = v
			}
		}
		return m
	case "max":
		m := vals[0]
		for _, v := range vals {
			if v > m {
				m = v
			}
		}
		return m
	case "median":
		q = 0.5
		fallthrough
	case "quantile":
		sorted := append([]float64(nil), vals...)
		sort.Float64s(sorted)
		return quantile(sorted, q)
	case "std":
		if len(vals) < 2 {
			return math.NaN()
		}
		s := 0.0
		for _, v := range vals {
			s += v
		}
		mean := s / float64(len(vals))
		varSS := 0.0
		for _, v := range vals {
			dv := v - mean
			varSS += dv * dv
		}
		return math.Sqrt(varSS / float64(len(vals)-1))
	}

	return math.NaN()
}

func (t *DataTool) actionAggregate(ctx context.Context, p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}

	if err := ctxErr(ctx); err != nil {
		return "", err
	}

	if len(p.ByList) == 0 && p.By == "" {
		return "", fmt.Errorf("'by' or 'byList' is required (the grouping column(s))")
	}

	rows, cols, types, err := d.aggregateCompute(p)
	if err != nil {
		return "", err
	}

	res := &dataset{Columns: cols, Types: types, Rows: rows}

	// Optional materialization (bounded preview + artifact path).
	artifact := ""
	if p.Format != "" {
		artifact, err = materializeDataset(res, p.Format, p.Name, "aggregate-result")
		if err != nil {
			return "", err
		}
	}

	mode := outMode(p)
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}

	var b strings.Builder
	if mode == "json" {
		out, jerr := jsonResult(map[string]any{
			"file":      filepath.Base(d.Path),
			"meta":      datasetMeta(d),
			"by":        p.ByList,
			"groups":    len(rows),
			"columns":   cols,
			"preview":   rowsPreview(res, limit),
			"artifact":  artifact,
			"truncated": len(rows) > limit,
		})
		if jerr != nil {
			return "", jerr
		}
		return out, nil
	}

	fmt.Fprintf(&b, "Aggregate — %s — %s (%d groups)\n%s\n\n", filepath.Base(d.Path),
		strings.Join(p.ByList, ", "), len(rows), datasetMeta(d))
	writeTable(&b, res, 0, limit, nil)
	if len(rows) > limit {
		fmt.Fprintf(&b, "… %d more groups\n", len(rows)-limit)
	}
	if artifact != "" {
		fmt.Fprintf(&b, "\nFull result written to: %s\n", artifact)
	}

	return b.String(), nil
}

// ---------------------------------------------------------------------------
// join — deterministic local joins between two datasets
// ---------------------------------------------------------------------------

func (t *DataTool) actionJoin(ctx context.Context, p *dataParams) (string, error) {
	if p.Path2 == "" {
		return "", fmt.Errorf("'path2' is required (the right-hand dataset)")
	}

	how := strings.ToLower(strings.TrimSpace(p.How))
	if how == "" {
		how = "inner"
	}
	switch how {
	case "inner", "left", "right", "full":
	default:
		return "", fmt.Errorf("unknown join type %q (inner|left|right|full)", how)
	}

	left, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	right, err := t.load(p.Path2)
	if err != nil {
		return "", err
	}

	if err := ctxErr(ctx); err != nil {
		return "", err
	}

	// Resolve keys (explicit join keys are REQUIRED — no implicit
	// column-position or name guessing).
	leftKeys, rightKeys := p.LeftKeys, p.RightKeys
	if len(leftKeys) == 0 && p.Key != "" {
		leftKeys = []string{p.Key}
		rightKeys = []string{p.Key2}
		if p.Key2 == "" {
			rightKeys = []string{p.Key}
		}
	}
	if len(leftKeys) == 0 || len(rightKeys) == 0 {
		return "", fmt.Errorf("explicit join keys are required (key/key2, or leftKeys/rightKeys for composite keys)")
	}
	if len(leftKeys) != len(rightKeys) {
		return "", fmt.Errorf("leftKeys and rightKeys must have the same length (%d vs %d)", len(leftKeys), len(rightKeys))
	}

	lIdx := make([]int, len(leftKeys))
	for i, k := range leftKeys {
		ci, cerr := left.colIndex(k)
		if cerr != nil {
			return "", fmt.Errorf("left dataset: %w", cerr)
		}
		lIdx[i] = ci
	}
	rIdx := make([]int, len(rightKeys))
	for i, k := range rightKeys {
		ci, cerr := right.colIndex(k)
		if cerr != nil {
			return "", fmt.Errorf("right dataset: %w", cerr)
		}
		rIdx[i] = ci
	}

	// Output columns: all left columns + all right columns except the
	// duplicated join keys (unless an explicit projection was requested).
	var outCols []string
	var lProj []int
	var rProj []int

	if len(p.Columns) > 0 {
		for _, c := range p.Columns {
			if ci, err := left.colIndex(c); err == nil {
				lProj = append(lProj, ci)
				outCols = append(outCols, left.Columns[ci])
				continue
			}
			ci, err := right.colIndex(c)
			if err != nil {
				return "", fmt.Errorf("projection column %q not found in either dataset", c)
			}
			rProj = append(rProj, ci)
			outCols = append(outCols, right.Columns[ci])
		}
	} else {
		for i := range left.Columns {
			lProj = append(lProj, i)
			outCols = append(outCols, left.Columns[i])
		}
		rKeySet := map[int]bool{}
		for _, ci := range rIdx {
			rKeySet[ci] = true
		}
		for i := range right.Columns {
			if !rKeySet[i] {
				rProj = append(rProj, i)
				outCols = append(outCols, right.Columns[i])
			}
		}
	}

	if err := ctxErr(ctx); err != nil {
		return "", err
	}

	// Hash the right dataset on its key tuple.
	keyOf := func(d *dataset, idx []int, row int) string {
		parts := make([]string, len(idx))
		for i, ci := range idx {
			parts[i] = strings.TrimSpace(d.Rows[row][ci])
		}
		return strings.Join(parts, "\x1f")
	}

	rightByKey := map[string][]int{}
	for r := range right.Rows {
		rightByKey[keyOf(right, rIdx, r)] = append(rightByKey[keyOf(right, rIdx, r)], r)
		if r%65536 == 0 {
			if err := ctxErr(ctx); err != nil {
				return "", err
			}
		}
	}

	// Join. Left rows in file order; per key the right matches in file
	// order; unmatched right rows appended in file order (full/right).
	rightMatched := make([]bool, len(right.Rows))
	rowCount := 0
	matchedPairs := 0
	emptyL := make([]string, len(lProj))
	emptyR := make([]string, len(rProj))

	buildRow := func(lrow []string, rrow []string) []string {
		row := make([]string, 0, len(outCols))
		row = append(row, lrow...)
		row = append(row, rrow...)
		return row
	}

	var outRows [][]string
	appendRow := func(row []string) error {
		rowCount++
		if rowCount > joinRowCap {
			return fmt.Errorf("join result exceeds the %d row cap — aggregate or filter the inputs first", joinRowCap)
		}
		outRows = append(outRows, row)
		return nil
	}

	for l := range left.Rows {
		if l%65536 == 0 {
			if err := ctxErr(ctx); err != nil {
				return "", err
			}
		}

		key := keyOf(left, lIdx, l)
		matches := rightByKey[key]

		lcells := make([]string, len(lProj))
		for j, ci := range lProj {
			lcells[j] = left.Rows[l][ci]
		}

		if len(matches) == 0 {
			if how == "left" || how == "full" {
				if err := appendRow(buildRow(lcells, emptyR)); err != nil {
					return "", err
				}
			}
			continue
		}

		for _, r := range matches {
			rightMatched[r] = true
			rcells := make([]string, len(rProj))
			for j, ci := range rProj {
				rcells[j] = right.Rows[r][ci]
			}
			if err := appendRow(buildRow(lcells, rcells)); err != nil {
				return "", err
			}
		}
		matchedPairs += len(matches)
	}

	if how == "right" || how == "full" {
		for r := range right.Rows {
			if rightMatched[r] {
				continue
			}
			if r%65536 == 0 {
				if err := ctxErr(ctx); err != nil {
					return "", err
				}
			}
			rcells := make([]string, len(rProj))
			for j, ci := range rProj {
				rcells[j] = right.Rows[r][ci]
			}
			if err := appendRow(buildRow(emptyL, rcells)); err != nil {
				return "", err
			}
		}
	}

	res := &dataset{
		Columns: outCols,
		Types:   inferTypes(outCols, outRows),
		Rows:    outRows,
		Path:    "",
	}

	// Optional materialization.
	artifact := ""
	if p.Format != "" {
		var err error
		artifact, err = materializeDataset(res, p.Format, p.Name, "join-result")
		if err != nil {
			return "", err
		}
	}

	mode := outMode(p)
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}

	var b strings.Builder
	if mode == "json" {
		out, jerr := jsonResult(map[string]any{
			"left":         filepath.Base(left.Path),
			"right":        filepath.Base(right.Path),
			"how":          how,
			"keys":         leftKeys,
			"meta":         datasetMeta(left) + "; " + datasetMeta(right),
			"rows":         len(outRows),
			"matchedPairs": matchedPairs,
			"columns":      outCols,
			"preview":      rowsPreview(res, limit),
			"artifact":     artifact,
		})
		if jerr != nil {
			return "", jerr
		}
		return out, nil
	}

	fmt.Fprintf(&b, "Join %s ⨝ %s (%s on %s) — %d rows\n%s\n\n",
		filepath.Base(left.Path), filepath.Base(right.Path), how,
		strings.Join(leftKeys, "+"), len(outRows), datasetMeta(left))
	writeTable(&b, res, 0, limit, nil)
	if len(outRows) > limit {
		fmt.Fprintf(&b, "… %d more rows\n", len(outRows)-limit)
	}
	if artifact != "" {
		fmt.Fprintf(&b, "\nFull result written to: %s\n", artifact)
	}

	return b.String(), nil
}

// inferTypes re-runs the dataset type inference for a derived dataset.
func inferTypes(cols []string, rows [][]string) []colType {
	d := buildDataset(cols, rows)
	return d.Types
}

// jsonResult marshals a compact machine-readable result.
func jsonResult(v any) (string, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}

	return string(out), nil
}

// ---------------------------------------------------------------------------
// quality — compact data-quality diagnostics
// ---------------------------------------------------------------------------

func (t *DataTool) actionQuality(ctx context.Context, p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}

	if err := ctxErr(ctx); err != nil {
		return "", err
	}

	type colReport struct {
		Column     string `json:"column"`
		Type       string `json:"type"`
		Missing    int    `json:"missing"`
		MissingPct string `json:"missingPct"`
		Distinct   int    `json:"distinct"`
		Constant   bool   `json:"constant"`
		HighCard   bool   `json:"highCardinality"`
		InvalidNum int    `json:"invalidNumeric"`
		MixedType  bool   `json:"mixedType"`
		Outliers   int    `json:"outliers"`
	}

	var cols []colReport
	issues := 0

	for i := range d.Columns {
		cr := colReport{
			Column:   d.Columns[i],
			Type:     string(d.Types[i]),
			Missing:  d.missingCount(i),
			Distinct: d.distinctCount(i),
		}

		if len(d.Rows) > 0 {
			cr.MissingPct = fmtNum(float64(cr.Missing)/float64(len(d.Rows))*100) + "%"
		}

		cr.Constant = len(d.Rows) > 1 && cr.Distinct <= 1 && cr.Missing < len(d.Rows)
		cr.HighCard = d.Types[i] != typeNumber && len(d.Rows) > 20 &&
			float64(cr.Distinct) > 0.9*float64(len(d.Rows))

		// Invalid numeric: present cells in a numeric column that parse
		// to NaN or ±Inf (parse-once cache; Inf passes ParseFloat but
		// poisons statistics).
		if d.Types[i] == typeNumber {
			for _, v := range d.numericColumn(i) {
				if math.IsInf(v, 0) {
					cr.InvalidNum++
				}
			}
		}

		// Mixed type: a string column where some non-missing values parse
		// as numbers and some do not (schema/type inconsistency evidence).
		if d.Types[i] == typeString {
			numLike, text := 0, 0
			for r := range d.Rows {
				v := strings.TrimSpace(d.Rows[r][i])
				if isMissing(v) {
					continue
				}
				if !math.IsNaN(parseNumber(v)) {
					numLike++
				} else {
					text++
				}
			}
			cr.MixedType = numLike > 0 && text > 0
		}

		if d.Types[i] == typeNumber {
			if _, _, n := d.iqrFences(i); n > 0 {
				cr.Outliers = n
			}
		}

		if cr.Constant || cr.HighCard || cr.InvalidNum > 0 || cr.MixedType || cr.Missing > 0 {
			issues++
		}

		cols = append(cols, cr)
	}

	dups := d.duplicateRowCount()

	qualityJSON := map[string]any{
		"file":          filepath.Base(d.Path),
		"meta":          datasetMeta(d),
		"rows":          len(d.Rows),
		"columnCount":   len(d.Columns),
		"duplicateRows": dups,
		"issueColumns":  issues,
		"columns":       cols,
	}

	mode := outMode(p)
	if mode == "json" {
		out, jerr := jsonResult(qualityJSON)
		if jerr != nil {
			return "", jerr
		}

		return out, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Data quality — %s (%d rows × %d cols)\n%s\n\n", filepath.Base(d.Path), len(d.Rows), len(d.Columns), datasetMeta(d))

	if mode == "table" {
		fmt.Fprintf(&b, "%-18s %6s %7s %8s %5s %5s %6s %6s %6s %7s\n",
			"column", "type", "missing", "distinct", "const", "hiCard", "badNum", "mixed", "outlier", "issue")
		for _, c := range cols {
			issue := ""
			if c.Constant || c.HighCard || c.InvalidNum > 0 || c.MixedType || c.Missing > 0 {
				issue = "yes"
			}
			fmt.Fprintf(&b, "%-18s %6s %7d %8d %5s %5s %6d %6s %6d %7s\n",
				clipStr(c.Column, 18), c.Type, c.Missing, c.Distinct,
				yesNo(c.Constant), yesNo(c.HighCard), c.InvalidNum,
				yesNo(c.MixedType), c.Outliers, issue)
		}
	} else {
		fmt.Fprintf(&b, "duplicate rows: %d\n", dups)
		worst := 0
		for _, c := range cols {
			if c.Constant || c.HighCard || c.InvalidNum > 0 || c.MixedType || c.Missing > 0 {
				worst++
				var probs []string
				if c.Missing > 0 {
					probs = append(probs, c.MissingPct+" missing")
				}
				if c.Constant {
					probs = append(probs, "constant")
				}
				if c.HighCard {
					probs = append(probs, "high cardinality")
				}
				if c.InvalidNum > 0 {
					probs = append(probs, strconv.Itoa(c.InvalidNum)+" invalid numeric")
				}
				if c.MixedType {
					probs = append(probs, "mixed numeric/text")
				}
				fmt.Fprintf(&b, "  %-20s %s\n", clipStr(c.Column, 20), strings.Join(probs, ", "))
			}
		}
		if worst == 0 {
			b.WriteString("  no column-level issues detected\n")
		}
	}

	if dups > 0 {
		fmt.Fprintf(&b, "\n%d duplicate row(s) — run dedupe before aggregating.\n", dups)
	} else {
		b.WriteString("\nNo duplicate rows.\n")
	}

	return b.String(), nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "-"
}

// ---------------------------------------------------------------------------
// export — materialize analysis results as artifacts
// ---------------------------------------------------------------------------

// actionExport materializes a dataset (optionally filtered, projected,
// sorted and limited) as a CSV/TSV/JSON artifact under the workspace
// authority. The model gets a path + counts instead of pasted rows.
func (t *DataTool) actionExport(ctx context.Context, p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}

	if err := ctxErr(ctx); err != nil {
		return "", err
	}

	rows := d.Rows

	// Optional filter (same condition engine as filter/query).
	if p.Column != "" && p.Op != "" {
		ci, cerr := d.colIndex(p.Column)
		if cerr != nil {
			return "", cerr
		}
		rows = matchRows(d, ci, p.Op, p.Value)
	}

	// Optional sort.
	if p.Column != "" && (p.Desc || p.Limit > 0) && p.Op == "" {
		ci, cerr := d.colIndex(p.Column)
		if cerr != nil {
			return "", cerr
		}
		idx := make([]int, len(rows))
		for i := range idx {
			idx[i] = i
		}
		numeric := d.Types[ci] == typeNumber
		sort.SliceStable(idx, func(a, b int) bool {
			ra, rb := idx[a], idx[b]
			if numeric {
				fa, fb := parseNumber(rows[ra][ci]), parseNumber(rows[rb][ci])
				if !math.IsNaN(fa) && !math.IsNaN(fb) && fa != fb {
					if p.Desc {
						return fa > fb
					}
					return fa < fb
				}
			}
			sa, sb := rows[ra][ci], rows[rb][ci]
			if sa != sb {
				if p.Desc {
					return sa > sb
				}
				return sa < sb
			}
			return false
		})
		sorted := make([][]string, len(idx))
		for i, r := range idx {
			sorted[i] = rows[r]
		}
		rows = sorted
	}

	// Optional projection.
	var types []colType
	if len(p.Columns) > 0 {
		var proj [][]string
		var colIdxs []int
		var cols []string
		for _, c := range p.Columns {
			ci, cerr := d.colIndex(c)
			if cerr != nil {
				return "", cerr
			}
			colIdxs = append(colIdxs, ci)
			cols = append(cols, d.Columns[ci])
			types = append(types, d.Types[ci])
		}
		proj = make([][]string, len(rows))
		for r := range rows {
			line := make([]string, len(colIdxs))
			for j, ci := range colIdxs {
				line[j] = rows[r][ci]
			}
			proj[r] = line
		}
		rows = proj
		d = &dataset{Columns: cols, Types: types, Rows: proj, Path: d.Path}
	}

	// Optional row limit.
	// v1.8.8 correctness repair: the limit previously MUTATED d.Rows on
	// the dataset returned by load() — which is the LRU-cached pointer
	// whenever no projection ran — so a limited export permanently
	// truncated the cached dataset for every subsequent action. The
	// limited view is now a separate dataset; the cache is untouched.
	outDataset := d
	if p.Limit > 0 && p.Limit < len(rows) {
		rows = rows[:p.Limit]
		if types == nil {
			types = d.Types
		}
		outDataset = &dataset{Columns: d.Columns, Types: types, Rows: rows, Path: d.Path}
	}

	format := p.Format
	if format == "" {
		return "", fmt.Errorf("'format' is required (csv|tsv|json)")
	}

	name := p.Name
	if name == "" {
		ext := filepath.Ext(d.Path)
		name = strings.TrimSuffix(filepath.Base(d.Path), ext) + "-export"
	}

	artifact, err := materializeDataset(outDataset, format, name, "export-result")
	if err != nil {
		return "", err
	}

	size := int64(0)
	if fi, ferr := os.Stat(artifact); ferr == nil {
		size = fi.Size()
	}

	logging.Default().Info("dataAnalysis", "export: %s (%d rows, %d bytes)", artifact, len(rows), size)

	if outMode(p) == "json" {
		out, jerr := jsonResult(map[string]any{
			"artifact": artifact,
			"format":   strings.ToLower(strings.TrimPrefix(format, ".")),
			"rows":     len(rows),
			"columns":  len(outDataset.Columns),
			"bytes":    size,
		})
		if jerr != nil {
			return "", jerr
		}

		return out, nil
	}

	return fmt.Sprintf("Exported %d rows × %d cols → %s (%d bytes)\n%s",
		len(rows), len(outDataset.Columns), artifact, size, datasetMeta(outDataset)), nil
}
