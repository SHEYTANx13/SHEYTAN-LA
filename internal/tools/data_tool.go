// Package tools — DataTool: the agent-facing data-analysis tool.
//
//		{"action":"analyze",   "path":"sales.csv"}                 — ONE compact call: schema+missing+stats+top values+outliers+findings
//		{"action":"aggregate", "path":..,"by":"region","aggs":["count","sum:revenue","mean:revenue"]}
//		{"action":"join",      "path":"orders.csv","path2":"customers.csv","key":"customer_id","how":"left"}
//		{"action":"quality",   "path":"sales.csv"}                 — missing/duplicate/constant/invalid/mixed/high-cardinality report
//		{"action":"export",    "path":..,"format":"csv","columns":["a","b"],"limit":5000,"name":"result"}
//
//	     {"action":"analyze",   "path":"sales.csv"}                 — ONE compact call: schema+missing+stats+top values+outliers+findings
//	     {"action":"aggregate", "path":..,"by":"region","aggs":["count","sum:revenue","mean:revenue"]}
//	     {"action":"aggregate", "path":..,"byList":["region","product"],"aggs":["sum"],"columns":["revenue","units"]}
//	     {"action":"join",      "path":"orders.csv","path2":"customers.csv","key":"customer_id","how":"left"}
//	     {"action":"quality",   "path":"sales.csv"}                 — missing/duplicate/constant/invalid/mixed/high-cardinality report
//	     {"action":"export",    "path":..,"format":"csv","columns":["a","b"],"column":"x","op":">","value":"0","limit":5000,"name":"result"}
//	     {"action":"profile",    "path":"sales.csv"}
//	     {"action":"stats",      "path":"sales.csv"}
//	     {"action":"correlation","path":"sales.csv"}
//	     {"action":"groupby",    "path":"sales.csv","by":"region","column":"revenue","agg":"sum"}
//	     {"action":"filter",     "path":"sales.csv","column":"revenue","op":">","value":"1000"}
//	     {"action":"sort",       "path":"sales.csv","column":"revenue","desc":true,"limit":10}
//	     {"action":"query",      "path":"sales.csv","columns":["region","revenue"],"column":"revenue","op":">","value":"0","limit":20}
//	     {"action":"histogram",  "path":"sales.csv","column":"revenue","bins":12}
//	     {"action":"convert",    "path":"sales.csv","format":"json"}
//	     {"action":"chart",      "path":"sales.csv","chart":"bar","labelCol":"region","valueCol":"revenue","name":"revenue-by-region"}
//	     {"action":"chart",      "path":"sales.csv","chart":"scatter","column":"price","column2":"sales"}
//	     {"action":"missing",    "path":"sales.csv"}
package tools

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

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// DataTool is the data-analysis tool: loads CSV/TSV/JSON datasets and runs
// profiling, statistics, aggregation, filtering, conversion and charting.
type DataTool struct {
	cfg *config.Config
}

// NewDataTool constructs the data tool bound to the app config.
func NewDataTool(cfg *config.Config) *DataTool { return &DataTool{cfg: cfg} }

func (t *DataTool) Name() string { return "dataAnalysis" }

func (t *DataTool) Description() string {
	return `Analyze datasets (CSV/TSV/JSON) deterministically in-process. Prefer ONE analyze/aggregate call over many small ones — results are compact and model-oriented, never raw-row dumps.
Actions (flat JSON, one per call):
  analyze      {"path":"sales.csv"}                            — ONE call: schema, missing, numeric stats, top categories, outliers, key findings
               {"path":..,"sections":"schema,stats,correlations,groupby","by":"region","column":"revenue","agg":"sum"}
               sections: schema|missing|stats|categorical|correlations|outliers|groupby|sample|findings|all
  aggregate    {"path":..,"by":"region","aggs":["count","sum:revenue","mean:revenue","min:revenue","max:revenue","median:revenue","std:revenue","quantile:revenue"],"q":0.9}
               {"path":..,"byList":["region","product"],"aggs":["sum","mean"],"columns":["revenue","units"]}
               — many aggregations + many grouping columns in ONE call; groups sorted by key
  join         {"path":"orders.csv","path2":"customers.csv","key":"customer_id","key2":"id","how":"inner|left|right|full"}
               {"path":..,"path2":..,"leftKeys":["a","b"],"rightKeys":["x","y"]} — composite keys; add "format":"csv","name":"joined" to materialize
  quality      {"path":"sales.csv"}                            — missing, duplicate rows, constant columns, invalid numerics, mixed types, high cardinality, outliers
  export       {"path":..,"format":"csv|tsv|json","name":"result"}  — materialize rows as a file artifact (model gets the path, not the rows)
               {"path":..,"format":"csv","columns":["a","b"],"column":"x","op":">","value":"0","desc":true,"limit":5000}
  profile      {"path":"sales.csv"}                            — shape, column types, missing counts, first rows
  stats        {"path":"sales.csv"}                            — count/mean/std/min/q1/median/q3/max/sum per numeric column
  correlation  {"path":"sales.csv"}                            — Pearson correlation matrix of numeric columns
  groupby      {"path":..,"by":"region","column":"revenue","agg":"sum"}   — aggregate (count|sum|mean|min|max)
  filter       {"path":..,"column":"revenue","op":">","value":"1000"}     — rows where condition holds
               ops: = != > < >= <= contains startswith endswith in empty
  sort         {"path":..,"column":"revenue","desc":true,"limit":10}      — order rows by a column
  query        {"path":..,"columns":["region","revenue"],"column":"revenue","op":">","value":"0","desc":true,"limit":20}
                                                              — combined select+filter+sort (report generator)
  histogram    {"path":..,"column":"revenue","bins":12}       — value distribution
  missing      {"path":..}                                    — per-column missing-value report
  regression   {"path":..,"column":"price","column2":"sales","value":"42"} — least-squares y=slope·x+b with R²/RMSE; "value" predicts
  valueCounts  {"path":..,"column":"region","limit":15}        — frequency table of a column (top values + share)
  pivot        {"path":..,"by":"region","column2":"product","column":"revenue","agg":"sum"} — 2-D group-by grid (max 12 column keys)
  dedupe       {"path":..,"column":"id","format":"csv"}        — drop duplicate rows (key = 'column', or all columns); format→ also writes cleaned CSV
  sample       {"path":..,"op":"head|tail|random","limit":10}  — peek rows without loading the whole set into the reply
  outliers     {"path":..,"column":"revenue"}                 — IQR + z-score outlier fences and the actual outlier values
  movingavg    {"path":..,"column":"revenue","bins":7,"format":"csv"} — windowed moving average; format→ writes the full smoothed series
  convert      {"path":"sales.csv","format":"json"}           — csv↔json↔tsv conversion (writes next to source)
  chart        {"path":..,"chart":"bar|line|pie","labelCol":"region","valueCol":"revenue","name":"rev"}
               {"path":..,"chart":"scatter","column":"price","column2":"units"}   — scatter uses two numeric columns
                                                              — renders an SVG into <app>/charts/ and returns the path
Output modes (analyze/aggregate/join/quality/export): "mode":"compact" (default) | "table" | "json". Compact reports end with key findings; large results return row counts + artifact paths instead of raw rows.
Tips: relative paths resolve against the app folder (same as the files tool). Every result is deterministic — same dataset + same action = same result.
Chain: files write CSV → analyze → aggregate/join → export or chart → tell the user the artifact path.
Datasets load up to 256 MB in-process (pure Go, no external engine); numeric columns are parsed once and cached.`
}

func (t *DataTool) Parameters() any {
	return struct {
		Action    string   `json:"action"`
		Path      string   `json:"path"`
		Column    string   `json:"column,omitempty"`
		Column2   string   `json:"column2,omitempty"`
		Columns   []string `json:"columns,omitempty"`
		By        string   `json:"by,omitempty"`
		Agg       string   `json:"agg,omitempty"`
		Op        string   `json:"op,omitempty"`
		Value     string   `json:"value,omitempty"`
		Bins      int      `json:"bins,omitempty"`
		Chart     string   `json:"chart,omitempty"`
		LabelCol  string   `json:"labelCol,omitempty"`
		ValueCol  string   `json:"valueCol,omitempty"`
		Name      string   `json:"name,omitempty"`
		Format    string   `json:"format,omitempty"`
		Mode      string   `json:"mode,omitempty"`
		Sections  string   `json:"sections,omitempty"`
		Path2     string   `json:"path2,omitempty"`
		How       string   `json:"how,omitempty"`
		Key       string   `json:"key,omitempty"`
		Key2      string   `json:"key2,omitempty"`
		LeftKeys  []string `json:"leftKeys,omitempty"`
		RightKeys []string `json:"rightKeys,omitempty"`
		ByList    []string `json:"byList,omitempty"`
		Aggs      []string `json:"aggs,omitempty"`
		Q         float64  `json:"q,omitempty"`

		Limit int  `json:"limit,omitempty"`
		Desc  bool `json:"desc,omitempty"`
	}{}
}

// dataParams mirrors Parameters() with json.RawMessage-friendly fields.
type dataParams struct {
	Action    string   `json:"action"`
	Path      string   `json:"path"`
	Column    string   `json:"column"`
	Column2   string   `json:"column2"`
	Columns   []string `json:"columns"`
	By        string   `json:"by"`
	Agg       string   `json:"agg"`
	Op        string   `json:"op"`
	Value     string   `json:"value"`
	Bins      int      `json:"bins"`
	Chart     string   `json:"chart"`
	LabelCol  string   `json:"labelCol"`
	ValueCol  string   `json:"valueCol"`
	Name      string   `json:"name"`
	Format    string   `json:"format"`
	Mode      string   `json:"mode"`
	Sections  string   `json:"sections"`
	Path2     string   `json:"path2"`
	How       string   `json:"how"`
	Key       string   `json:"key"`
	Key2      string   `json:"key2"`
	LeftKeys  []string `json:"leftKeys"`
	RightKeys []string `json:"rightKeys"`
	ByList    []string `json:"byList"`
	Aggs      []string `json:"aggs"`
	Q         float64  `json:"q"`

	Limit int  `json:"limit"`
	Desc  bool `json:"desc"`
}

func (t *DataTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var p dataParams
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("bad args: %w — expected a FLAT object like {\"action\":\"profile\",\"path\":\"sales.csv\"}", err)
	}
	if p.Action == "" {
		return "", fmt.Errorf("action is required (analyze|aggregate|join|quality|export|profile|stats|correlation|groupby|filter|sort|query|histogram|missing|regression|valueCounts|pivot|dedupe|sample|outliers|movingavg|convert|chart)")
	}
	if p.Action != "chart" && p.Path == "" {
		return "", fmt.Errorf("path is required (CSV/TSV/JSON dataset; relative paths resolve against the app folder)")
	}

	switch strings.ToLower(p.Action) {
	case "analyze":
		return t.actionAnalyze(ctx, &p)
	case "aggregate":
		return t.actionAggregate(ctx, &p)
	case "join":
		return t.actionJoin(ctx, &p)
	case "quality":
		return t.actionQuality(ctx, &p)
	case "export":
		return t.actionExport(ctx, &p)
	case "profile":
		return t.actionProfile(&p)
	case "stats", "describe":
		return t.actionStats(&p)
	case "correlation", "corr":
		return t.actionCorrelation(&p)
	case "groupby":
		return t.actionGroupBy(&p)
	case "filter":
		return t.actionFilter(&p, nil)
	case "sort":
		return t.actionSort(&p)
	case "query", "select":
		return t.actionQuery(&p)
	case "histogram":
		return t.actionHistogram(&p)
	case "missing":
		return t.actionMissing(&p)
	case "regression", "linreg", "fit":
		return t.actionRegression(&p)
	case "valuecounts", "freq", "frequencies":
		return t.actionValueCounts(&p)
	case "pivot":
		return t.actionPivot(&p)
	case "dedupe", "deduplicate", "unique":
		return t.actionDedupe(&p)
	case "sample", "head", "tail":
		if p.Action == "head" && p.Op == "" {
			p.Op = "head"
		}
		if p.Action == "tail" && p.Op == "" {
			p.Op = "tail"
		}
		return t.actionSample(&p)
	case "outliers", "anomalies":
		return t.actionOutliers(&p)
	case "movingavg", "movingaverage", "smooth":
		return t.actionMovingAvg(&p)
	case "convert":
		return t.actionConvert(&p)
	case "chart", "plot":
		return t.actionChart(&p)
	default:
		return "", fmt.Errorf("unknown action %q (analyze|aggregate|join|quality|export|profile|stats|correlation|groupby|filter|sort|query|histogram|missing|regression|valueCounts|pivot|dedupe|sample|outliers|movingavg|convert|chart)", p.Action)
	}
}

// --- actions ---

func (t *DataTool) actionProfile(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Dataset: %s\n%d rows × %d columns\n\n", filepath.Base(d.Path), len(d.Rows), len(d.Columns))
	fmt.Fprintf(&b, "%-22s %-8s %8s %9s %s\n", "column", "type", "missing", "distinct", "example")
	for i, c := range d.Columns {
		missing, distinct := 0, map[string]bool{}
		var example string
		for r := range d.Rows {
			v := strings.TrimSpace(d.Rows[r][i])
			if isMissing(v) {
				missing++
				continue
			}
			distinct[v] = true
			if example == "" {
				example = clipStr(v, 24)
			}
		}
		fmt.Fprintf(&b, "%-22s %-8s %8d %9d %s\n", clipStr(c, 22), d.Types[i], missing, len(distinct), example)
	}
	b.WriteString("\nFirst rows:\n")
	writeTable(&b, d, 0, 5, nil)
	return b.String(), nil
}

func (t *DataTool) actionStats(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Descriptive statistics — %s (%d rows)\n\n", filepath.Base(d.Path), len(d.Rows))
	fmt.Fprintf(&b, "%-16s %7s %8s %10s %10s %10s %10s %10s %10s %10s %7s\n",
		"column", "count", "miss", "mean", "std", "min", "q1", "median", "q3", "max", "sum")
	for i, c := range d.Columns {
		if d.Types[i] != typeNumber {
			continue
		}
		st := computeStats(d, i)
		fmt.Fprintf(&b, "%-16s %7d %8d %10s %10s %10s %10s %10s %10s %10s %10s\n",
			clipStr(c, 16), st.Count, st.Missing,
			fmtNum(st.Mean), fmtNum(st.Std), fmtNum(st.Min), fmtNum(st.Q1),
			fmtNum(st.Median), fmtNum(st.Q3), fmtNum(st.Max), fmtNum(st.Sum))
	}
	if !strings.Contains(b.String(), "\n\n\n") && strings.Count(b.String(), "\n") <= 3 {
		b.WriteString("\n(no numeric columns — stats applies to numeric data)")
	}
	return b.String(), nil
}

func (t *DataTool) actionCorrelation(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	cols := d.numericCols()
	if len(cols) < 2 {
		return "", fmt.Errorf("need at least two numeric columns for correlation")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Pearson correlation — %s\n\n", filepath.Base(d.Path))
	header := "            "
	for _, c := range cols {
		header += fmt.Sprintf("%12s", clipStr(d.Columns[c], 12))
	}
	b.WriteString(header + "\n")
	// Pairwise-complete observations: a row contributes to a pair only when
	// BOTH values are present, so every pearson() call gets equal-length
	// vectors (per-column NaN dropping would desync them).
	// v1.0.9: both columns come from the parse-once numeric cache — the
	// matrix no longer re-parses rows·cols² cells per call.
	cached := make(map[int][]float64, len(cols))
	col := func(i int) []float64 {
		if v, ok := cached[i]; ok {
			return v
		}
		v := d.numericColumn(cols[i])
		cached[i] = v
		return v
	}
	pairVecs := func(i, j int) ([]float64, []float64) {
		xsAll, ysAll := col(i), col(j)
		xs := make([]float64, 0, len(xsAll))
		ys := make([]float64, 0, len(ysAll))
		for r := range xsAll {
			x, y := xsAll[r], ysAll[r]
			if !math.IsNaN(x) && !math.IsNaN(y) {
				xs = append(xs, x)
				ys = append(ys, y)
			}
		}
		return xs, ys
	}
	for i := range cols {
		line := fmt.Sprintf("%-12s", clipStr(d.Columns[cols[i]], 12))
		for j := range cols {
			xs, ys := pairVecs(i, j)
			line += fmt.Sprintf("%12s", fmtNum(pearson(xs, ys)))
		}
		b.WriteString(line + "\n")
	}
	return b.String(), nil
}

func (t *DataTool) actionGroupBy(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	if p.By == "" {
		return "", fmt.Errorf("'by' is required (the column to group on)")
	}
	byIdx, err := d.colIndex(p.By)
	if err != nil {
		return "", err
	}
	agg := strings.ToLower(p.Agg)
	if agg == "" {
		agg = "count"
	}

	var valIdx = -1
	if agg != "count" {
		if p.Column == "" {
			return "", fmt.Errorf("'column' is required for agg=%s", agg)
		}
		valIdx, err = d.colIndex(p.Column)
		if err != nil {
			return "", err
		}
		if d.Types[valIdx] != typeNumber {
			return "", fmt.Errorf("column %q is not numeric (agg %s needs numbers)", p.Column, agg)
		}
	}

	type group struct {
		key   string
		count int
		vals  []float64
	}
	groups := map[string]*group{}
	var order []string
	for r := range d.Rows {
		k := strings.TrimSpace(d.Rows[r][byIdx])
		g := groups[k]
		if g == nil {
			g = &group{key: k}
			groups[k] = g
			order = append(order, k)
		}
		g.count++
		if valIdx >= 0 {
			if v := parseNumber(d.Rows[r][valIdx]); !math.IsNaN(v) {
				g.vals = append(g.vals, v)
			}
		}
	}

	rows := make([][]string, 0, len(groups))
	for _, k := range order {
		g := groups[k]
		var val string
		switch agg {
		case "count":
			val = strconv.Itoa(g.count)
		case "sum":
			s := 0.0
			for _, v := range g.vals {
				s += v
			}
			val = fmtNum(s)
		case "mean", "avg":
			if len(g.vals) == 0 {
				val = "—"
			} else {
				s := 0.0
				for _, v := range g.vals {
					s += v
				}
				val = fmtNum(s / float64(len(g.vals)))
			}
		case "min":
			if len(g.vals) == 0 {
				val = "—"
			} else {
				m := g.vals[0]
				for _, v := range g.vals {
					if v < m {
						m = v
					}
				}
				val = fmtNum(m)
			}
		case "max":
			if len(g.vals) == 0 {
				val = "—"
			} else {
				m := g.vals[0]
				for _, v := range g.vals {
					if v > m {
						m = v
					}
				}
				val = fmtNum(m)
			}
		default:
			return "", fmt.Errorf("unknown agg %q (count|sum|mean|min|max)", agg)
		}
		rows = append(rows, []string{g.key, strconv.Itoa(g.count), val})
	}

	var b strings.Builder
	valName := p.Column
	if valName == "" {
		valName = "rows"
	}
	fmt.Fprintf(&b, "Group by %q — %s(%s) — %s (%d groups)\n\n", p.By, agg, valName, filepath.Base(d.Path), len(rows))
	res := &dataset{
		Columns: []string{p.By, "count", agg + "_" + valName},
		Types:   []colType{typeString, typeNumber, typeNumber},
		Rows:    rows,
	}
	sort.SliceStable(res.Rows, func(i, j int) bool {
		a, _ := strconv.ParseFloat(res.Rows[i][2], 64)
		bv, _ := strconv.ParseFloat(res.Rows[j][2], 64)
		return a > bv
	})
	writeTable(&b, res, 0, maxInt(len(rows), 30), nil)
	return b.String(), nil
}

func (t *DataTool) actionFilter(p *dataParams, ds *dataset) (string, error) {
	d := ds
	if d == nil {
		var err error
		d, err = t.load(p.Path)
		if err != nil {
			return "", err
		}
	}
	if p.Column == "" {
		return "", fmt.Errorf("'column' is required for filter")
	}
	ci, err := d.colIndex(p.Column)
	if err != nil {
		return "", err
	}
	if p.Op == "" {
		p.Op = "="
	}
	matched := matchRows(d, ci, p.Op, p.Value)
	res := &dataset{Columns: d.Columns, Types: d.Types, Rows: matched}
	var b strings.Builder
	fmt.Fprintf(&b, "Filter %s %s %q → %d of %d rows\n\n", p.Column, p.Op, p.Value, len(matched), len(d.Rows))
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}
	writeTable(&b, res, 0, limit, nil)
	return b.String(), nil
}

func (t *DataTool) actionSort(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	if p.Column == "" {
		return "", fmt.Errorf("'column' is required for sort")
	}
	ci, err := d.colIndex(p.Column)
	if err != nil {
		return "", err
	}
	idx := make([]int, len(d.Rows))
	for i := range idx {
		idx[i] = i
	}
	numeric := d.Types[ci] == typeNumber
	sort.SliceStable(idx, func(a, b int) bool {
		ra, rb := idx[a], idx[b]
		if numeric {
			fa, fb := parseNumber(d.Rows[ra][ci]), parseNumber(d.Rows[rb][ci])
			if !math.IsNaN(fa) && !math.IsNaN(fb) && fa != fb {
				if p.Desc {
					return fa > fb
				}
				return fa < fb
			}
		}
		sa, sb := d.Rows[ra][ci], d.Rows[rb][ci]
		if sa != sb {
			if p.Desc {
				return sa > sb
			}
			return sa < sb
		}
		return false
	})
	rows := make([][]string, len(idx))
	for i, r := range idx {
		rows[i] = d.Rows[r]
	}
	res := &dataset{Columns: d.Columns, Types: d.Types, Rows: rows}
	var b strings.Builder
	order := "asc"
	if p.Desc {
		order = "desc"
	}
	fmt.Fprintf(&b, "Sorted by %q (%s) — %s (%d rows)\n\n", p.Column, order, filepath.Base(d.Path), len(rows))
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}
	writeTable(&b, res, 0, limit, nil)
	return b.String(), nil
}

func (t *DataTool) actionQuery(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	rows := d.Rows
	var cond string
	if p.Column != "" && p.Op != "" {
		ci, cerr := d.colIndex(p.Column)
		if cerr != nil {
			return "", cerr
		}
		rows = matchRows(d, ci, p.Op, p.Value)
		cond = fmt.Sprintf(" where %s %s %q", p.Column, p.Op, p.Value)
	}
	// Column projection.
	var cols []string
	var colIdx []int
	if len(p.Columns) > 0 {
		for _, c := range p.Columns {
			ci, cerr := d.colIndex(c)
			if cerr != nil {
				return "", cerr
			}
			colIdx = append(colIdx, ci)
			cols = append(cols, d.Columns[ci])
		}
	} else {
		cols = d.Columns
		for i := range d.Columns {
			colIdx = append(colIdx, i)
		}
	}
	proj := make([][]string, len(rows))
	for r := range rows {
		proj[r] = make([]string, len(colIdx))
		for j, ci := range colIdx {
			proj[r][j] = rows[r][ci]
		}
	}
	res := &dataset{Columns: cols, Types: nil, Rows: proj}
	// Optional sort on the first projected column or p.Column.
	sortCol := p.Column
	if sortCol != "" {
		if p.Desc || p.Limit > 0 {
			// reuse sort logic via a temp dataset on projected data
			si := -1
			for j, c := range cols {
				if strings.EqualFold(c, sortCol) {
					si = j
					break
				}
			}
			if si >= 0 {
				sort.SliceStable(res.Rows, func(a, b int) bool {
					sa, sb := res.Rows[a][si], res.Rows[b][si]
					fa, fb := parseNumber(sa), parseNumber(sb)
					if !math.IsNaN(fa) && !math.IsNaN(fb) && fa != fb {
						if p.Desc {
							return fa > fb
						}
						return fa < fb
					}
					if sa != sb {
						if p.Desc {
							return sa > sb
						}
						return sa < sb
					}
					return false
				})
			}
		}
	}
	var b strings.Builder
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}
	fmt.Fprintf(&b, "Query — %s%s (%d rows, showing %d)\n\n", filepath.Base(d.Path), cond, len(res.Rows), minInt(limit, len(res.Rows)))
	writeTable(&b, res, 0, limit, nil)
	return b.String(), nil
}

func (t *DataTool) actionHistogram(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	if p.Column == "" {
		return "", fmt.Errorf("'column' is required for histogram")
	}
	ci, err := d.colIndex(p.Column)
	if err != nil {
		return "", err
	}
	var vals []float64
	for r := range d.Rows {
		if v := parseNumber(d.Rows[r][ci]); !math.IsNaN(v) {
			vals = append(vals, v)
		}
	}
	if len(vals) < 2 {
		return "", fmt.Errorf("not enough numeric values in column %q", p.Column)
	}
	bins := p.Bins
	if bins <= 0 {
		bins = 10
	}
	if bins > 50 {
		bins = 50
	}
	sort.Float64s(vals)
	lo, hi := vals[0], vals[len(vals)-1]
	if lo == hi {
		hi = lo + 1
	}
	width := (hi - lo) / float64(bins)
	counts := make([]int, bins)
	for _, v := range vals {
		k := int((v - lo) / width)
		if k >= bins {
			k = bins - 1
		}
		counts[k]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Histogram — %s.%s (%d values, range %s…%s, %d bins)\n\n",
		filepath.Base(d.Path), p.Column, len(vals), fmtNum(lo), fmtNum(hi), bins)
	maxC := 0
	for _, c := range counts {
		if c > maxC {
			maxC = c
		}
	}
	for i, c := range counts {
		from := lo + float64(i)*width
		to := from + width
		barLen := 0
		if maxC > 0 {
			barLen = c * 36 / maxC
		}
		fmt.Fprintf(&b, "%12s…%12s %6d %s\n", fmtNum(from), fmtNum(to), c, strings.Repeat("█", maxInt(barLen, 0)))
	}
	return b.String(), nil
}

func (t *DataTool) actionMissing(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Missing-value report — %s (%d rows)\n\n", filepath.Base(d.Path), len(d.Rows))
	totalMissing := 0
	for i, c := range d.Columns {
		missing := 0
		for r := range d.Rows {
			if isMissing(strings.TrimSpace(d.Rows[r][i])) {
				missing++
			}
		}
		totalMissing += missing
		pct := 0.0
		if len(d.Rows) > 0 {
			pct = float64(missing) / float64(len(d.Rows)) * 100
		}
		fmt.Fprintf(&b, "%-22s %8d missing (%.1f%%)\n", clipStr(c, 22), missing, pct)
	}
	if totalMissing == 0 {
		b.WriteString("\nNo missing values — the dataset is complete.\n")
	}
	return b.String(), nil
}

func (t *DataTool) actionConvert(p *dataParams) (string, error) {
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	format := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(p.Format), "."))
	switch format {
	case "":
		return "", fmt.Errorf("'format' is required (csv|tsv|json)")
	case "csv", "tsv", "json":
	default:
		return "", fmt.Errorf("unsupported format %q (csv|tsv|json)", format)
	}
	if format == filepath.Ext(d.Path)[1:] {
		return fmt.Sprintf("dataset is already %s: %s", format, d.Path), nil
	}
	name := p.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(d.Path), filepath.Ext(d.Path)) + "." + format
	}
	if !strings.HasSuffix(name, "."+format) {
		name += "." + format
	}
	out := filepath.Join(filepath.Dir(d.Path), sanitizeName(name))

	var data []byte
	switch format {
	case "json":
		objs := make([]map[string]any, len(d.Rows))
		for r := range d.Rows {
			o := map[string]any{}
			for i, c := range d.Columns {
				v := d.Rows[r][i]
				if d.Types[i] == typeNumber && !isMissing(v) {
					if f := parseNumber(v); !math.IsNaN(f) {
						o[c] = f
						continue
					}
				}
				o[c] = v
			}
			objs[r] = o
		}
		data, err = json.MarshalIndent(objs, "", "  ")
	case "csv", "tsv":
		var b strings.Builder
		delim := ","
		if format == "tsv" {
			delim = "\t"
		}
		b.WriteString(strings.Join(d.Columns, delim) + "\n")
		for r := range d.Rows {
			cells := make([]string, len(d.Columns))
			for i, v := range d.Rows[r] {
				if strings.ContainsAny(v, "\""+delim+"\n") {
					cells[i] = "\"" + strings.ReplaceAll(v, "\"", "\"\"") + "\""
				} else {
					cells[i] = v
				}
			}
			b.WriteString(strings.Join(cells, delim) + "\n")
		}
		data = []byte(b.String())
	}
	if err != nil {
		return "", err
	}
	if werr := os.WriteFile(out, data, 0o644); werr != nil {
		return "", werr
	}
	logging.Default().Info("dataAnalysis", "converted %s → %s (%d bytes)", d.Path, out, len(data))
	return fmt.Sprintf("Converted %d rows × %d cols → %s\n(%s → %s)", len(d.Rows), len(d.Columns), out, strings.TrimPrefix(filepath.Ext(d.Path), "."), format), nil
}

// --- filter engine ---

func matchRows(d *dataset, ci int, op, value string) [][]string {
	op = strings.ToLower(strings.TrimSpace(op))
	var out [][]string
	numeric := d.Types[ci] == typeNumber
	fv := parseNumber(value)
	inSet := map[string]bool{}
	for _, s := range strings.Split(value, ",") {
		inSet[strings.TrimSpace(strings.ToLower(s))] = true
	}
	for r := range d.Rows {
		cell := strings.TrimSpace(d.Rows[r][ci])
		ok := false
		switch op {
		case "=", "==", "equals", "eq":
			ok = strings.EqualFold(cell, value)
		case "!=", "<>", "ne":
			ok = !strings.EqualFold(cell, value)
		case ">", "gt":
			if numeric && !math.IsNaN(fv) {
				f := parseNumber(cell)
				ok = !math.IsNaN(f) && f > fv
			} else {
				ok = cell > value
			}
		case "<", "lt":
			if numeric && !math.IsNaN(fv) {
				f := parseNumber(cell)
				ok = !math.IsNaN(f) && f < fv
			} else {
				ok = cell < value
			}
		case ">=", "gte":
			if numeric && !math.IsNaN(fv) {
				f := parseNumber(cell)
				ok = !math.IsNaN(f) && f >= fv
			} else {
				ok = cell >= value
			}
		case "<=", "lte":
			if numeric && !math.IsNaN(fv) {
				f := parseNumber(cell)
				ok = !math.IsNaN(f) && f <= fv
			} else {
				ok = cell <= value
			}
		case "contains", "like":
			ok = strings.Contains(strings.ToLower(cell), strings.ToLower(value))
		case "startswith":
			ok = strings.HasPrefix(strings.ToLower(cell), strings.ToLower(value))
		case "endswith":
			ok = strings.HasSuffix(strings.ToLower(cell), strings.ToLower(value))
		case "in":
			ok = inSet[strings.ToLower(cell)]
		case "empty", "missing":
			ok = isMissing(cell)
		default:
			ok = strings.EqualFold(cell, value)
		}
		if ok {
			out = append(out, d.Rows[r])
		}
	}
	return out
}

// --- table rendering ---

// writeTable renders up to `limit` rows of the dataset as an aligned table.
// `only` selects column indexes (nil = all).
func writeTable(b *strings.Builder, d *dataset, from, limit int, only []int) {
	idx := only
	if idx == nil {
		idx = make([]int, len(d.Columns))
		for i := range d.Columns {
			idx[i] = i
		}
	}
	widths := make([]int, len(idx))
	for j, ci := range idx {
		w := len(clipStr(d.Columns[ci], 18))
		for r := from; r < len(d.Rows) && r < from+limit; r++ {
			if l := len(clipStr(d.Rows[r][ci], 24)); l > w {
				w = l
			}
		}
		if w > 24 {
			w = 24
		}
		widths[j] = w
	}
	for j, ci := range idx {
		b.WriteString(pad(clipStr(d.Columns[ci], 18), widths[j]) + "  ")
	}
	b.WriteString("\n" + strings.Repeat("─", 60) + "\n")
	end := from + limit
	if end > len(d.Rows) {
		end = len(d.Rows)
	}
	for r := from; r < end; r++ {
		for j, ci := range idx {
			b.WriteString(pad(clipStr(d.Rows[r][ci], 24), widths[j]) + "  ")
		}
		b.WriteString("\n")
	}
	if end < len(d.Rows) {
		fmt.Fprintf(b, "… %d more rows\n", len(d.Rows)-end)
	}
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == ' ':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}
