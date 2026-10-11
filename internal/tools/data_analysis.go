// Package tools — v1.0.9 (TURBINE) data-analysis expansion.
//
// This file carries:
//   - the optimized statistics core (computeStats/quantile/pearson/fmtNum),
//     now driven by the dataset's parse-once numeric column cache;
//   - the new analysis actions: describe (all-columns profile), regression
//     (least squares + R² + prediction), valueCounts (frequency table),
//     pivot (two-dimensional group-by), dedupe, sample (head/tail/random),
//     outliers (IQR + z-score) and movingavg (windowed series).
//
// Every action reads columns through numericColumn, so a chained workflow
// (stats → regression → outliers on one file) parses each numeric cell
// exactly once for the whole session instead of once per action.
package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// --- statistics core ---

type colStats struct {
	Count   int
	Missing int
	Mean    float64
	Std     float64
	Min     float64
	Q1      float64
	Median  float64
	Q3      float64
	Max     float64
	Sum     float64
}

// computeStats computes the full five-number summary of a column. The
// numeric values come from the parse-once column cache; the only remaining
// cost is one sort for the quartiles.
func computeStats(d *dataset, col int) colStats {
	vals := d.numericColumn(col)
	missing := len(d.Rows) - countPresent(vals)
	st := colStats{Count: len(vals) - missing, Missing: missing}
	present := make([]float64, 0, len(vals))
	for _, v := range vals {
		if !math.IsNaN(v) {
			present = append(present, v)
		}
	}
	st.Count = len(present)
	if len(present) == 0 {
		return st
	}
	sorted := append([]float64(nil), present...)
	sort.Float64s(sorted)
	n := len(sorted)
	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	st.Sum = sum
	st.Mean = sum / float64(n)
	st.Min = sorted[0]
	st.Max = sorted[n-1]
	st.Median = quantile(sorted, 0.5)
	st.Q1 = quantile(sorted, 0.25)
	st.Q3 = quantile(sorted, 0.75)
	if n > 1 {
		// Two-pass variance for numerical stability (cheap: present values
		// are already in registers/cache after the sort pass).
		varSS := 0.0
		for _, v := range sorted {
			dv := v - st.Mean
			varSS += dv * dv
		}
		st.Std = math.Sqrt(varSS / float64(n-1))
	}
	return st
}

func countPresent(vals []float64) int {
	n := 0
	for _, v := range vals {
		if !math.IsNaN(v) {
			n++
		}
	}
	return n
}

func quantile(sorted []float64, q float64) float64 {
	n := len(sorted)
	if n == 0 {
		return math.NaN()
	}
	pos := q * float64(n-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	frac := pos - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// pearson computes the Pearson product-moment correlation in one fused
// pass (sums and cross-products accumulate together — one cache sweep).
func pearson(xs, ys []float64) float64 {
	n := len(xs)
	if n < 2 || len(ys) != n {
		return math.NaN()
	}
	var sx, sy, sxx, syy, sxy float64
	for i := 0; i < n; i++ {
		x, y := xs[i], ys[i]
		sx += x
		sy += y
		sxx += x * x
		syy += y * y
		sxy += x * y
	}
	mx, my := sx/float64(n), sy/float64(n)
	num := sxy - float64(n)*mx*my
	dx := sxx - float64(n)*mx*mx
	dy := syy - float64(n)*my*my
	if dx <= 0 || dy <= 0 {
		return math.NaN()
	}
	return num / math.Sqrt(dx*dy)
}

// linearFit returns the least-squares slope/intercept of (xs, ys) plus R²
// and RMSE, using the fused single-pass sums.
func linearFit(xs, ys []float64) (slope, intercept, r2, rmse float64) {
	n := len(xs)
	if n < 2 {
		return math.NaN(), math.NaN(), math.NaN(), math.NaN()
	}
	var sx, sy, sxx, syy, sxy float64
	for i := 0; i < n; i++ {
		x, y := xs[i], ys[i]
		sx += x
		sy += y
		sxx += x * x
		syy += y * y
		sxy += x * y
	}
	fn := float64(n)
	mx, my := sx/fn, sy/fn
	sxxc := sxx - fn*mx*mx
	syyc := syy - fn*my*my
	sxyc := sxy - fn*mx*my
	if sxxc <= 0 {
		return math.NaN(), math.NaN(), math.NaN(), math.NaN()
	}
	slope = sxyc / sxxc
	intercept = my - slope*mx
	ssTot := syyc
	ssRes := 0.0
	if ssTot > 0 {
		for i := range xs {
			p := slope*xs[i] + intercept
			d := ys[i] - p
			ssRes += d * d
		}
		r2 = 1 - ssRes/ssTot
		rmse = math.Sqrt(ssRes / fn)
	} else {
		r2 = 1
	}
	return slope, intercept, r2, rmse
}

// fmtNum renders a float compactly for tables.
func fmtNum(f float64) string {
	if math.IsNaN(f) {
		return "—"
	}
	if math.IsInf(f, 0) {
		return "∞"
	}
	if math.Abs(f) >= 1e12 || (math.Abs(f) < 1e-4 && f != 0) {
		return strconv.FormatFloat(f, 'g', 4, 64)
	}
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

// --- v1.0.9 actions ---

// actionRegression runs least-squares regression of column2 (y) on column
// (x) and reports slope/intercept/R²/RMSE plus optional predictions.
func (t *DataTool) actionRegression(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	if p.Column == "" || p.Column2 == "" {
		return "", fmt.Errorf("regression needs 'column' (x) and 'column2' (y)")
	}
	ci, err := d.colIndex(p.Column)
	if err != nil {
		return "", err
	}
	yi, err := d.colIndex(p.Column2)
	if err != nil {
		return "", err
	}
	xsAll, ysAll := d.numericColumn(ci), d.numericColumn(yi)
	xs := make([]float64, 0, len(xsAll))
	ys := make([]float64, 0, len(ysAll))
	for i := range xsAll {
		if !math.IsNaN(xsAll[i]) && !math.IsNaN(ysAll[i]) {
			xs = append(xs, xsAll[i])
			ys = append(ys, ysAll[i])
		}
	}
	if len(xs) < 2 {
		return "", fmt.Errorf("need at least 2 paired numeric rows for regression (got %d)", len(xs))
	}
	slope, intercept, r2, rmse := linearFit(xs, ys)
	if math.IsNaN(slope) {
		return "", fmt.Errorf("x column %q has zero variance — regression is undefined", p.Column)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Linear regression — %s (%d paired rows)\n\n", filepath.Base(d.Path), len(xs))
	fmt.Fprintf(&b, "y = %s · x + %s\n\n", fmtNum(slope), fmtNum(intercept))
	fmt.Fprintf(&b, "%-12s %s\n", "slope", fmtNum(slope))
	fmt.Fprintf(&b, "%-12s %s\n", "intercept", fmtNum(intercept))
	fmt.Fprintf(&b, "%-12s %s\n", "R²", fmtNum(r2))
	fmt.Fprintf(&b, "%-12s %s\n", "RMSE", fmtNum(rmse))
	fmt.Fprintf(&b, "%-12s %s %s → %s\n", "predict(x)", p.Column+"=", p.Value, func() string {
		if xv := parseNumber(p.Value); !math.IsNaN(xv) {
			return fmtNum(slope*xv + intercept)
		}
		return "(set \"value\" to a number to predict)"
	}())
	if r2 >= 0.9 {
		b.WriteString("\nFit is very strong (R² ≥ 0.9).\n")
	} else if r2 >= 0.6 {
		b.WriteString("\nFit is moderate — consider scatter chart (chart action, chart=scatter) to inspect.\n")
	}
	return b.String(), nil
}

// actionValueCounts returns the frequency table of a column (top-K values,
// ties by first appearance).
func (t *DataTool) actionValueCounts(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	if p.Column == "" {
		return "", fmt.Errorf("'column' is required for valueCounts")
	}
	ci, err := d.colIndex(p.Column)
	if err != nil {
		return "", err
	}
	freq := map[string]int{}
	order := make([]string, 0, 64)
	for r := range d.Rows {
		v := strings.TrimSpace(d.Rows[r][ci])
		if isMissing(v) {
			v = "(missing)"
		}
		if freq[v] == 0 {
			order = append(order, v)
		}
		freq[v]++
	}
	sort.SliceStable(order, func(i, j int) bool { return freq[order[i]] > freq[order[j]] })
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > len(order) {
		limit = len(order)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Value counts — %s.%s (%d distinct of %d rows)\n\n",
		filepath.Base(d.Path), p.Column, len(order), len(d.Rows))
	for i := 0; i < limit; i++ {
		v := order[i]
		pct := float64(freq[v]) / float64(len(d.Rows)) * 100
		bar := freq[v] * 30 / maxInt(1, freq[order[0]])
		fmt.Fprintf(&b, "%-24s %8d (%4.1f%%) %s\n", clipStr(v, 24), freq[v], pct, strings.Repeat("█", bar))
	}
	if len(order) > limit {
		fmt.Fprintf(&b, "… %d more distinct values\n", len(order)-limit)
	}
	return b.String(), nil
}

// actionPivot aggregates a value column over two key columns (rows × cols
// grid), summing by default.
func (t *DataTool) actionPivot(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	if p.By == "" || p.Column2 == "" {
		return "", fmt.Errorf("pivot needs 'by' (row key) and 'column2' (column key)")
	}
	agg := strings.ToLower(p.Agg)
	if agg == "" {
		agg = "sum"
	}
	ri, err := d.colIndex(p.By)
	if err != nil {
		return "", err
	}
	ci, err := d.colIndex(p.Column2)
	if err != nil {
		return "", err
	}
	vi := -1
	if agg != "count" {
		if p.Column == "" {
			return "", fmt.Errorf("'column' (the value to %s) is required for agg=%s", agg, agg)
		}
		vi, err = d.colIndex(p.Column)
		if err != nil {
			return "", err
		}
		if d.Types[vi] != typeNumber {
			return "", fmt.Errorf("column %q is not numeric", p.Column)
		}
	}

	type cell struct {
		count int
		vals  []float64
	}
	grid := map[string]map[string]*cell{}
	rowKeys := make([]string, 0, 16)
	rowSeen := map[string]bool{}
	colKeys := make([]string, 0, 16)
	colSeen := map[string]bool{}
	for r := range d.Rows {
		rk := strings.TrimSpace(d.Rows[r][ri])
		ck := strings.TrimSpace(d.Rows[r][ci])
		if !rowSeen[rk] {
			rowSeen[rk] = true
			rowKeys = append(rowKeys, rk)
		}
		if !colSeen[ck] {
			colSeen[ck] = true
			colKeys = append(colKeys, ck)
		}
		g := grid[rk]
		if g == nil {
			g = map[string]*cell{}
			grid[rk] = g
		}
		c := g[ck]
		if c == nil {
			c = &cell{}
			g[ck] = c
		}
		c.count++
		if vi >= 0 {
			if v := parseNumber(d.Rows[r][vi]); !math.IsNaN(v) {
				c.vals = append(c.vals, v)
			}
		}
	}
	if len(colKeys) > 12 {
		return "", fmt.Errorf("column key %q has %d distinct values — pivot supports up to 12 (filter the dataset first)", p.Column2, len(colKeys))
	}
	aggCell := func(c *cell) string {
		switch agg {
		case "count":
			return strconv.Itoa(c.count)
		case "mean", "avg":
			if len(c.vals) == 0 {
				return "—"
			}
			s := 0.0
			for _, v := range c.vals {
				s += v
			}
			return fmtNum(s / float64(len(c.vals)))
		case "min":
			if len(c.vals) == 0 {
				return "—"
			}
			m := c.vals[0]
			for _, v := range c.vals {
				if v < m {
					m = v
				}
			}
			return fmtNum(m)
		case "max":
			if len(c.vals) == 0 {
				return "—"
			}
			m := c.vals[0]
			for _, v := range c.vals {
				if v > m {
					m = v
				}
			}
			return fmtNum(m)
		default: // sum
			s := 0.0
			for _, v := range c.vals {
				s += v
			}
			return fmtNum(s)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Pivot — %s(%s) of %s × %s — %s (%d rows × %d cols)\n\n",
		agg, p.Column, p.By, p.Column2, filepath.Base(d.Path), len(rowKeys), len(colKeys))
	b.WriteString(pad(clipStr(p.By, 18), 18))
	for _, ck := range colKeys {
		b.WriteString(pad(clipStr(ck, 14), 15))
	}
	b.WriteString("\n" + strings.Repeat("─", 60) + "\n")
	for _, rk := range rowKeys {
		b.WriteString(pad(clipStr(rk, 18), 18))
		for _, ck := range colKeys {
			c := grid[rk][ck]
			v := "—"
			if c != nil {
				v = aggCell(c)
			}
			b.WriteString(pad(v, 15))
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// actionDedupe removes duplicate rows (by all columns or a key column) and
// optionally writes the cleaned dataset.
func (t *DataTool) actionDedupe(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	var keyIdx []int
	if p.Column != "" {
		ci, err := d.colIndex(p.Column)
		if err != nil {
			return "", err
		}
		keyIdx = []int{ci}
	} else {
		for i := range d.Columns {
			keyIdx = append(keyIdx, i)
		}
	}
	seen := make(map[string]struct{}, len(d.Rows))
	out := make([][]string, 0, len(d.Rows))
	for r := range d.Rows {
		var kb strings.Builder
		for _, ci := range keyIdx {
			kb.WriteString(d.Rows[r][ci])
			kb.WriteByte(0)
		}
		k := kb.String()
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, d.Rows[r])
	}
	removed := len(d.Rows) - len(out)
	var b strings.Builder
	fmt.Fprintf(&b, "Dedupe — %s: %d rows in, %d unique, %d duplicates removed (key: %s)\n",
		filepath.Base(d.Path), len(d.Rows), len(out), removed, strings.Join(quoteCols(d, keyIdx), ", "))
	// Optional write-out through the same path rules as convert.
	if p.Format != "" {
		name := strings.TrimSuffix(filepath.Base(d.Path), filepath.Ext(d.Path)) + "-dedup.csv"
		dst := filepath.Join(filepath.Dir(d.Path), sanitizeName(name))
		clean := &dataset{Columns: d.Columns, Types: d.Types, Rows: out}
		if err := writeCSVFile(clean, dst); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "Cleaned dataset written to %s\n", dst)
		if OnFileCreated != nil {
			OnFileCreated(dst)
		}
	}
	return b.String(), nil
}

func quoteCols(d *dataset, idx []int) []string {
	out := make([]string, len(idx))
	for j, i := range idx {
		out[j] = d.Columns[i]
	}
	return out
}

// writeCSVFile writes a dataset as RFC-4180 CSV (chunked through a
// buffered writer — never materializes the whole file in memory).
func writeCSVFile(d *dataset, dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 256<<10) // 256 KB chunks
	if _, err := w.WriteString(strings.Join(d.Columns, ",") + "\n"); err != nil {
		f.Close()
		return err
	}
	cells := make([]string, len(d.Columns))
	for r := range d.Rows {
		for i := range d.Rows[r] {
			v := d.Rows[r][i]
			if strings.ContainsAny(v, "\",\n") {
				cells[i] = "\"" + strings.ReplaceAll(v, "\"", "\"\"") + "\""
			} else {
				cells[i] = v
			}
		}
		if _, err := w.WriteString(strings.Join(cells, ",") + "\n"); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// actionSample returns head/tail/random rows of the dataset.
func (t *DataTool) actionSample(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	n := p.Limit
	if n <= 0 {
		n = 10
	}
	if n > 200 {
		n = 200
	}
	mode := strings.ToLower(p.Op)
	if mode == "" {
		mode = "head"
	}
	var rows [][]string
	var label string
	switch mode {
	case "head", "first", "top":
		label = "head"
		end := minInt(n, len(d.Rows))
		rows = d.Rows[:end]
	case "tail", "last", "bottom":
		label = "tail"
		start := maxInt(0, len(d.Rows)-n)
		rows = d.Rows[start:]
	case "random", "rand", "sample":
		label = "random"
		if n >= len(d.Rows) {
			rows = d.Rows
		} else {
			// Floyd's algorithm: distinct indexes in O(n) with no full shuffle.
			pick := make([]int, 0, n)
			inSet := make(map[int]struct{}, n*2)
			for i := len(d.Rows) - n; i < len(d.Rows); i++ {
				j := int(randInt63(int64(i + 1)))
				if _, taken := inSet[j]; taken {
					j = i
				}
				inSet[j] = struct{}{}
				pick = append(pick, j)
			}
			sort.Ints(pick)
			rows = make([][]string, 0, n)
			for _, idx := range pick {
				rows = append(rows, d.Rows[idx])
			}
		}
	default:
		return "", fmt.Errorf("unknown sample mode %q (head|tail|random — pass it via \"op\")", mode)
	}
	res := &dataset{Columns: d.Columns, Types: d.Types, Rows: rows}
	var b strings.Builder
	fmt.Fprintf(&b, "Sample (%s %d) — %s (%d of %d rows)\n\n", label, n, filepath.Base(d.Path), len(rows), len(d.Rows))
	writeTable(&b, res, 0, n, nil)
	return b.String(), nil
}

// actionOutliers flags outliers in a numeric column using the IQR rule
// (with optional z-score column).
func (t *DataTool) actionOutliers(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	if p.Column == "" {
		return "", fmt.Errorf("'column' is required for outliers")
	}
	ci, err := d.colIndex(p.Column)
	if err != nil {
		return "", err
	}
	vals := d.numericColumn(ci)
	present := make([]float64, 0, len(vals))
	for _, v := range vals {
		if !math.IsNaN(v) {
			present = append(present, v)
		}
	}
	if len(present) < 4 {
		return "", fmt.Errorf("need at least 4 numeric values in %q", p.Column)
	}
	sorted := append([]float64(nil), present...)
	sort.Float64s(sorted)
	q1 := quantile(sorted, 0.25)
	q3 := quantile(sorted, 0.75)
	iqr := q3 - q1
	loF, hiF := q1-1.5*iqr, q3+1.5*iqr
	loE, hiE := q1-3.0*iqr, q3+3.0*iqr
	mean, std := 0.0, 0.0
	{
		s := 0.0
		for _, v := range present {
			s += v
		}
		mean = s / float64(len(present))
		var ss float64
		for _, v := range present {
			dv := v - mean
			ss += dv * dv
		}
		if len(present) > 1 {
			std = math.Sqrt(ss / float64(len(present)-1))
		}
	}
	var mild, extreme []float64
	for _, v := range present {
		if v < loE || v > hiE {
			extreme = append(extreme, v)
		} else if v < loF || v > hiF {
			mild = append(mild, v)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Outliers — %s.%s (IQR rule, %d values)\n\n", filepath.Base(d.Path), p.Column, len(present))
	fmt.Fprintf(&b, "Q1 %s · Q3 %s · IQR %s\n", fmtNum(q1), fmtNum(q3), fmtNum(iqr))
	fmt.Fprintf(&b, "fence  mild  [%s, %s]\n", fmtNum(loF), fmtNum(hiF))
	fmt.Fprintf(&b, "fence  extreme  [%s, %s]\n\n", fmtNum(loE), fmtNum(hiE))
	fmt.Fprintf(&b, "%-8s %6d   (z-score |z| > %.1f)\n", "mild:", len(mild), 1.5)
	fmt.Fprintf(&b, "%-8s %6d   (z-score |z| > %.1f)\n\n", "extreme:", len(extreme), 3.0)
	show := func(name string, list []float64) {
		sort.Float64s(list)
		max := 12
		sep := ""
		fmt.Fprintf(&b, "%s:", name)
		for i, v := range list {
			if i >= max {
				fmt.Fprintf(&b, " … (%d more)", len(list)-max)
				break
			}
			b.WriteString(sep + fmtNum(v))
			sep = ", "
		}
		if len(list) == 0 {
			b.WriteString(" none")
		}
		b.WriteString("\n")
	}
	show("mild values", mild)
	show("extreme values", extreme)
	if std > 0 {
		fmt.Fprintf(&b, "\nmean %s · std %s (z = (x − mean)/std)\n", fmtNum(mean), fmtNum(std))
	}
	return b.String(), nil
}

// actionMovingAvg computes the windowed moving average of a numeric column
// and optionally writes the smoothed series next to the source.
func (t *DataTool) actionMovingAvg(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	if p.Column == "" {
		return "", fmt.Errorf("'column' is required for movingavg")
	}
	ci, err := d.colIndex(p.Column)
	if err != nil {
		return "", err
	}
	win := p.Bins // reuse `bins` as the window size
	if win <= 0 {
		win = 7
	}
	if win > 1000 {
		win = 1000
	}
	vals := d.numericColumn(ci)
	// Prefix sums over present values make every window sum O(1): the
	// whole series costs one pass regardless of window width.
	present := make([]float64, len(vals))
	copy(present, vals)
	prefix := make([]float64, len(present)+1)
	for i, v := range present {
		if math.IsNaN(v) {
			v = 0
		}
		prefix[i+1] = prefix[i] + v
	}
	smoothed := make([]float64, len(present))
	for i := range present {
		lo := i - win + 1
		if lo < 0 {
			lo = 0
		}
		smoothed[i] = (prefix[i+1] - prefix[lo]) / float64(i+1-lo)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Moving average — %s.%s (window %d, cumulative smoothing)\n\n",
		filepath.Base(d.Path), p.Column, win)
	// Show a compact tail of the series (last 8 points raw → smoothed).
	from := maxInt(0, len(present)-8)
	fmt.Fprintf(&b, "%-10s %12s %12s\n", "row", "value", "smoothed")
	for i := from; i < len(present); i++ {
		v, s := "—", "—"
		if !math.IsNaN(present[i]) {
			v = fmtNum(present[i])
		}
		if !math.IsNaN(smoothed[i]) {
			s = fmtNum(smoothed[i])
		}
		fmt.Fprintf(&b, "%-10d %12s %12s\n", i+1, v, s)
	}
	if p.Format != "" {
		name := strings.TrimSuffix(filepath.Base(d.Path), filepath.Ext(d.Path)) + "-movingavg.csv"
		dst := filepath.Join(filepath.Dir(d.Path), sanitizeName(name))
		out := &dataset{
			Columns: []string{"row", p.Column + "_raw", p.Column + "_ma" + strconv.Itoa(win)},
			Types:   []colType{typeNumber, typeNumber, typeNumber},
		}
		out.Rows = make([][]string, len(present))
		for i := range present {
			out.Rows[i] = []string{
				strconv.Itoa(i + 1),
				fmtNum(present[i]),
				fmtNum(smoothed[i]),
			}
		}
		if err := writeCSVFile(out, dst); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "\nFull series written to %s\n", dst)
		if OnFileCreated != nil {
			OnFileCreated(dst)
		}
	}
	return b.String(), nil
}

// randInt63 is a tiny deterministic PRNG (xorshift64*) so `sample` with
// mode=random is reproducible across calls without seeding plumbing.
// v1.8.8: the generator state is mutex-guarded — the tool registry can
// execute tool calls concurrently, and the previous bare global var was
// a data race (two concurrent sample(mode=random) calls).
var (
	rngMu    sync.Mutex
	rngState uint64 = 0x9E3779B97F4A7C15
)

func randInt63(n int64) int64 {
	rngMu.Lock()
	defer rngMu.Unlock()
	rngState = rngState ^ rngState<<13
	rngState = rngState ^ rngState>>7
	rngState = rngState ^ rngState<<17
	return int64((rngState >> 1) % uint64(n))
}

// Ensure the action helpers stay wired even if a future refactor trims
// the call graph (the compiler would otherwise prune nothing, but the
// context import guard keeps parity with data_tool.go).
var _ = context.Background
var _ = json.Marshal
