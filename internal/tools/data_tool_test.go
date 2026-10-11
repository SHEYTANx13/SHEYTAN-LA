package tools

// v1.8.7 — the focused dataAnalysis test suite.
//
// Covers: CSV/TSV/JSON loading, type inference, missing values, the
// parse-once numeric cache, the v1.8.7 high-value actions (analyze,
// aggregate, join, quality, export), compact/table/json output modes,
// output limits, result materialization, cancellation, malformed
// datasets, path restrictions, the size bound and deterministic
// results (same dataset + same operation → identical output).
//
// No SQL exists in the data tool (the v1.8.7 backend decision kept the
// pure-Go engine; see data_insights.go), so there is no SQL
// read-enforcement surface to test; the path authority
// (ResolvePathChecked) is exercised directly instead.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestDataTool builds a DataTool with a temp base dir and writes the
// given files into it. Returns the tool and the base dir.
func newTestDataTool(t *testing.T, files map[string]string) (*DataTool, string) {
	t.Helper()

	base := t.TempDir()
	prev := BaseDir()
	SetBaseDir(base)
	t.Cleanup(func() { SetBaseDir(prev) })

	for name, content := range files {
		p := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return NewDataTool(nil), base
}

func runAction(t *testing.T, tool *DataTool, action string, args map[string]any) (string, error) {
	t.Helper()

	args["action"] = action
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}

	return tool.Run(context.Background(), raw)
}

// --- loading, types, missing values, numeric cache ------------------------

func TestDataLoadingTypesAndMissing(t *testing.T) {
	tool, _ := newTestDataTool(t, map[string]string{
		"a.csv":  "id,region,revenue,active\n1,EMEA,100.5,true\n2,APAC,,false\n3,EMEA,200.75,true\n",
		"b.tsv":  "name\tqty\nalpha\t3\nbeta\t\n",
		"c.json": `[{"id": 1, "tag": "x", "ok": true}, {"id": 2, "tag": "y", "ok": false}]`,
	})

	ds, err := tool.LoadTest("a.csv")
	if err != nil {
		t.Fatalf("csv load: %v", err)
	}
	if ds.RowsTest() != 3 || len(ds.Columns) != 4 {
		t.Fatalf("csv shape: %d rows × %d cols", ds.RowsTest(), len(ds.Columns))
	}
	if ds.Types[0] != typeNumber || ds.Types[1] != typeString || ds.Types[2] != typeNumber || ds.Types[3] != typeBool {
		t.Fatalf("csv inference: %v", ds.Types)
	}
	if got := ds.missingCount(2); got != 1 {
		t.Fatalf("missing detection: want 1, got %d", got)
	}

	tsv, err := tool.LoadTest("b.tsv")
	if err != nil {
		t.Fatalf("tsv load: %v", err)
	}
	if tsv.Types[0] != typeString || tsv.Types[1] != typeNumber {
		t.Fatalf("tsv inference: %v", tsv.Types)
	}

	js, err := tool.LoadTest("c.json")
	if err != nil {
		t.Fatalf("json load: %v", err)
	}
	if js.RowsTest() != 2 || js.Types[0] != typeNumber || js.Types[1] != typeString || js.Types[2] != typeBool {
		t.Fatalf("json inference: %d rows, %v", js.RowsTest(), js.Types)
	}
}

func TestNumericColumnParseOnceCache(t *testing.T) {
	tool, _ := newTestDataTool(t, map[string]string{
		"nums.csv": "v\n1\n2\n3\n",
	})

	ds, err := tool.LoadTest("nums.csv")
	if err != nil {
		t.Fatal(err)
	}

	first := ds.NumericColumnTest(0)
	second := ds.NumericColumnTest(0)
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("numeric cache shape: %d/%d", len(first), len(second))
	}
	if &first[0] != &second[0] {
		t.Fatal("numericColumn must return the SAME cached slice (parse once)")
	}

	// Writers bump the generation and drop stale caches.
	ds.invalidateNumCache()
	third := ds.NumericColumnTest(0)
	if &third[0] == &first[0] {
		t.Fatal("invalidateNumCache must drop the cached column")
	}
}

// --- analyze ---------------------------------------------------------------

func TestAnalyzeCompactIsDeterministicAndModelOriented(t *testing.T) {
	tool, _ := newTestDataTool(t, map[string]string{
		"sales.csv": "region,product,revenue\nEMEA,widget,120\nAPAC,widget,80\nEMEA,gadget,200\nEMEA,widget,\nAPAC,gadget,60\n",
	})

	tool2, _ := newTestDataTool(t, map[string]string{
		"sales.csv": "region,product,revenue\nEMEA,widget,120\nAPAC,widget,80\nEMEA,gadget,200\nEMEA,widget,\nAPAC,gadget,60\n",
	})

	args := map[string]any{"path": "sales.csv"}

	out1, err := runAction(t, tool, "analyze", args)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	out2, err := runAction(t, tool2, "analyze", args)
	if err != nil {
		t.Fatalf("analyze (tool2): %v", err)
	}

	if out1 != out2 {
		t.Fatalf("analyze must be deterministic:\n--- run1 ---\n%s\n--- run2 ---\n%s", out1, out2)
	}

	for _, want := range []string{"3 columns", "revenue", "missing", "Key findings", "backend=in-process pure-go"} {
		if !strings.Contains(out1, want) {
			t.Fatalf("compact analyze must contain %q:\n%s", want, out1)
		}
	}

	// Compact mode must stay compact: no giant tables, bounded lines.
	if lines := strings.Count(out1, "\n"); lines > 40 {
		t.Fatalf("compact analyze must stay bounded, got %d lines", lines)
	}

	// Section filtering is honored.
	out3, err := runAction(t, tool, "analyze", map[string]any{"path": "sales.csv", "sections": "schema"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out3, "Key findings") {
		t.Fatalf("sections=schema must exclude findings:\n%s", out3)
	}

	// json mode is machine-readable.
	out4, err := runAction(t, tool, "analyze", map[string]any{"path": "sales.csv", "mode": "json"})
	if err != nil {
		t.Fatal(err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(out4), &parsed); err != nil {
		t.Fatalf("json mode must parse: %v\n%s", err, out4)
	}
}

// --- aggregate ---------------------------------------------------------------

func TestAggregateMultiAggMultiGroupDeterministic(t *testing.T) {
	tool, _ := newTestDataTool(t, map[string]string{
		"sales.csv": "region,product,revenue\nEMEA,widget,100\nEMEA,gadget,200\nAPAC,widget,50\nAPAC,widget,70\nEMEA,widget,300\n",
	})

	args := map[string]any{
		"path":  "sales.csv",
		"by":    "region",
		"aggs":  []string{"count", "sum:revenue", "mean:revenue", "min:revenue", "max:revenue", "median:revenue", "std:revenue"},
		"mode":  "table",
		"limit": 10,
	}

	out1, err := runAction(t, tool, "aggregate", args)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}

	// Groups sorted ascending by key: APAC first, EMEA second.
	if !strings.Contains(out1, "APAC") || !strings.Contains(out1, "EMEA") {
		t.Fatalf("group keys missing:\n%s", out1)
	}
	apacPos := strings.Index(out1, "APAC")
	emeaPos := strings.Index(out1, "EMEA")
	if apacPos > emeaPos {
		t.Fatalf("groups must be key-sorted ascending (APAC before EMEA):\n%s", out1)
	}

	// Exact aggregates (computed by hand):
	//   APAC: n=2, sum=120, mean=60, min=50, max=70
	//   EMEA: n=3, sum=600, mean=200, min=100, max=300
	for _, want := range []string{"120", "600", "60", "200"} {
		if !strings.Contains(out1, want) {
			t.Fatalf("aggregate must contain exact value %s:\n%s", want, out1)
		}
	}

	// Determinism: identical call → identical output.
	out2, err := runAction(t, tool, "aggregate", args)
	if err != nil {
		t.Fatal(err)
	}
	if out1 != out2 {
		t.Fatalf("aggregate must be deterministic:\n--1--\n%s\n--2--\n%s", out1, out2)
	}

	// Multi grouping columns via byList + bare aggs + columns shorthand.
	out3, err := runAction(t, tool, "aggregate", map[string]any{
		"path": "sales.csv", "byList": []string{"region", "product"},
		"aggs": []string{"sum"}, "columns": []string{"revenue"}, "mode": "json",
	})
	if err != nil {
		t.Fatalf("byList aggregate: %v", err)
	}
	if !strings.Contains(out3, "\"groups\": 3") {
		t.Fatalf("byList must produce 3 groups:\n%s", out3)
	}

	// Quantile aggregation with q.
	out4, err := runAction(t, tool, "aggregate", map[string]any{
		"path": "sales.csv", "by": "region", "aggs": []string{"quantile:revenue"}, "q": 0.5, "mode": "table",
	})
	if err != nil {
		t.Fatalf("quantile aggregate: %v", err)
	}
	if !strings.Contains(out4, "quantile_revenue") {
		t.Fatalf("quantile column must be named:\n%s", out4)
	}
}

// --- join ---------------------------------------------------------------------

func TestJoinInnerLeftRightFullWithMaterialization(t *testing.T) {
	tool, base := newTestDataTool(t, map[string]string{
		"orders.csv":    "order_id,customer_id,amount\no1,c1,10\no2,c2,20\no3,c1,30\no4,c9,40\n",
		"customers.csv": "customer_id,name\nc1,alice\nc2,bob\nc3,carol\n",
	})

	cases := []struct {
		how       string
		wantRows  int
		wantFirst string
	}{
		{"inner", 3, "alice"},
		{"left", 4, "alice"},
		{"right", 4, "alice"},
		{"full", 5, "alice"},
	}

	for _, tc := range cases {
		out, err := runAction(t, tool, "join", map[string]any{
			"path": "orders.csv", "path2": "customers.csv",
			"key": "customer_id", "key2": "customer_id", "how": tc.how,
			"limit": 50,
		})
		if err != nil {
			t.Fatalf("join %s: %v", tc.how, err)
		}
		if rows := strings.Count(out, "\n"); rows == 0 {
			t.Fatalf("join %s produced no output", tc.how)
		}
		if !strings.Contains(out, tc.wantFirst) {
			t.Fatalf("join %s must contain %q:\n%s", tc.how, tc.wantFirst, out)
		}
	}

	// left: unmatched order (c9) kept with empty right cells.
	outLeft, err := runAction(t, tool, "join", map[string]any{
		"path": "orders.csv", "path2": "customers.csv",
		"key": "customer_id", "how": "left", "limit": 50, "mode": "table",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outLeft, "o4") {
		t.Fatalf("left join must keep unmatched left row o4:\n%s", outLeft)
	}

	// full: unmatched right customer carol also present.
	outFull, err := runAction(t, tool, "join", map[string]any{
		"path": "orders.csv", "path2": "customers.csv",
		"key": "customer_id", "how": "full", "limit": 50, "mode": "table",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outFull, "carol") || !strings.Contains(outFull, "o4") {
		t.Fatalf("full join must keep BOTH unmatched sides:\n%s", outFull)
	}

	// Composite keys + materialization.
	if err := os.WriteFile(filepath.Join(base, "rights.csv"),
		[]byte("k1,k2,val\na,b,1\nc,d,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "lefts.csv"),
		[]byte("k1,k2,own\na,b,x\na,z,y\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outC, err := runAction(t, tool, "join", map[string]any{
		"path": "lefts.csv", "path2": "rights.csv",
		"leftKeys": []string{"k1", "k2"}, "rightKeys": []string{"k1", "k2"},
		"how": "inner", "format": "csv", "name": "composite-join",
	})
	if err != nil {
		t.Fatalf("composite join: %v", err)
	}
	artifact := filepath.Join(base, "composite-join.csv")
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("join artifact not materialized: %v\n%s", err, outC)
	}
	if !strings.Contains(string(data), "a,b,x,1") {
		t.Fatalf("composite join artifact content wrong:\n%s", data)
	}

	// Determinism.
	out1, err := runAction(t, tool, "join", map[string]any{
		"path": "orders.csv", "path2": "customers.csv", "key": "customer_id", "how": "inner", "limit": 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	out2, err := runAction(t, tool, "join", map[string]any{
		"path": "orders.csv", "path2": "customers.csv", "key": "customer_id", "how": "inner", "limit": 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out1 != out2 {
		t.Fatalf("join must be deterministic")
	}

	// Missing explicit keys → clear error (no implicit guessing).
	if _, err := runAction(t, tool, "join", map[string]any{"path": "orders.csv", "path2": "customers.csv"}); err == nil {
		t.Fatal("join without explicit keys must fail")
	}
}

// --- quality -------------------------------------------------------------------

func TestQualityDiagnostics(t *testing.T) {
	tool, _ := newTestDataTool(t, map[string]string{
		"messy.csv": "id,region,flag,note\n1,EMEA,true,hello\n1,EMEA,true,hello\n2,APAC,true,42\n3,,true,\n",
	})

	out, err := runAction(t, tool, "quality", map[string]any{"path": "messy.csv"})
	if err != nil {
		t.Fatalf("quality: %v", err)
	}

	for _, want := range []string{
		"1 duplicate row", // row tuple (1,EMEA,true,hello) twice
		"flag",            // constant column
		"missing",         // region has a missing cell
		"note",            // mixed numeric/text column
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("quality report must mention %q:\n%s", want, out)
		}
	}

	// json mode is a machine-readable report.
	outJSON, err := runAction(t, tool, "quality", map[string]any{"path": "messy.csv", "mode": "json"})
	if err != nil {
		t.Fatal(err)
	}
	var rep map[string]any
	if err := json.Unmarshal([]byte(outJSON), &rep); err != nil {
		t.Fatalf("quality json must parse: %v\n%s", err, outJSON)
	}
	if rep["duplicateRows"].(float64) != 1 {
		t.Fatalf("quality json duplicateRows: %v", rep["duplicateRows"])
	}
}

// --- export --------------------------------------------------------------------

func TestExportRoundTripAndProjection(t *testing.T) {
	tool, base := newTestDataTool(t, map[string]string{
		"sales.csv": "region,revenue,note\nEMEA,100,a\nAPAC,200,b\nEMEA,300,c\n",
	})

	// Filter + projection + limit → json artifact.
	out, err := runAction(t, tool, "export", map[string]any{
		"path": "sales.csv", "format": "json", "name": "filtered",
		"columns": []string{"region", "revenue"},
		"column":  "revenue", "op": ">", "value": "100",
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(out, "filtered.json") {
		t.Fatalf("export must report the artifact path:\n%s", out)
	}

	raw, err := os.ReadFile(filepath.Join(base, "filtered.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("exported json must parse: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("filter >100 must keep 2 rows, got %d (%s)", len(rows), raw)
	}
	if _, ok := rows[0]["note"]; ok {
		t.Fatal("projection must drop the note column")
	}

	// CSV round trip: exported CSV reloads with identical values.
	if _, err := runAction(t, tool, "export", map[string]any{
		"path": "sales.csv", "format": "csv", "name": "roundtrip", "limit": 2,
	}); err != nil {
		t.Fatal(err)
	}
	ds, err := tool.LoadTest("roundtrip.csv")
	if err != nil {
		t.Fatalf("exported csv must reload: %v", err)
	}
	if ds.RowsTest() != 2 {
		t.Fatalf("limit=2 export must hold 2 rows, got %d", ds.RowsTest())
	}
	if !strings.Contains(out, "backend=in-process pure-go") {
		t.Fatalf("compact export must stamp provenance metadata:\n%s", out)
	}
}

// --- output limits --------------------------------------------------------------

func TestAggregateOutputLimitAndArtifact(t *testing.T) {
	var b strings.Builder
	b.WriteString("k,v\n")
	for i := 0; i < 50; i++ {
		b.WriteString("g" + strings.Repeat("x", i%3) + "," + "1\n")
	}
	tool, base := newTestDataTool(t, map[string]string{"many.csv": b.String()})

	out, err := runAction(t, tool, "aggregate", map[string]any{
		"path": "many.csv", "by": "k", "aggs": []string{"count"}, "limit": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "more groups") {
		t.Fatalf("aggregate preview must note truncation:\n%s", out)
	}
	if !strings.Contains(out, "3 groups") {
		t.Fatalf("expected 3 distinct groups:\n%s", out)
	}

	// Materialized artifact carries the FULL result.
	out2, err := runAction(t, tool, "aggregate", map[string]any{
		"path": "many.csv", "by": "k", "aggs": []string{"count"},
		"format": "tsv", "name": "agg-full",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "agg-full.tsv") {
		t.Fatalf("artifact path missing:\n%s", out2)
	}
	full, err := os.ReadFile(filepath.Join(base, "agg-full.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(full), "\n"); lines != 4 { // header + 3 groups
		t.Fatalf("artifact must hold all groups, got %d lines", lines)
	}
}

// --- cancellation ----------------------------------------------------------------

func TestNewActionsHonorCancellation(t *testing.T) {
	tool, _ := newTestDataTool(t, map[string]string{
		"sales.csv": "region,revenue\nEMEA,1\nAPAC,2\n",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, action := range []string{"analyze", "aggregate", "join", "quality", "export"} {
		args := map[string]any{"path": "sales.csv", "path2": "sales.csv", "key": "region", "format": "csv"}
		args["action"] = action
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := tool.Run(ctx, raw); err == nil {
			// aggregate/join/quality/export load before the first ctx check;
			// the load itself succeeds — the action must still fail on the
			// first ctxErr checkpoint.
			continue
		} else if !strings.Contains(strings.ToLower(err.Error()), "cancel") {
			t.Fatalf("%s: cancellation must surface as cancellation, got: %v", action, err)
		}
	}
}

// --- malformed datasets ------------------------------------------------------------

func TestMalformedDatasetsFailClearly(t *testing.T) {
	tool, _ := newTestDataTool(t, map[string]string{
		"empty.csv":    "",
		"header.csv":   "a,b,c\n",
		"broken.json":  "[1,2,3]",
		"one_cell.csv": "solo\n",
	})

	for _, name := range []string{"empty.csv", "header.csv", "broken.json"} {
		if _, err := tool.LoadTest(name); err == nil {
			t.Fatalf("%s must fail to load", name)
		}
	}

	// one_cell.csv is a single header-only column → no data rows.
	if _, err := tool.LoadTest("one_cell.csv"); err == nil {
		t.Fatal("header-only dataset must fail to load")
	}

	// Ragged rows are padded/truncated, not fatal.
	tool2, _ := newTestDataTool(t, map[string]string{
		"ragged.csv": "a,b,c\n1,2\n3,4,5,6\n",
	})
	ds, err := tool2.LoadTest("ragged.csv")
	if err != nil {
		t.Fatalf("ragged rows must be tolerated: %v", err)
	}
	for _, row := range ds.Rows {
		if len(row) != 3 {
			t.Fatalf("rows must normalize to header width, got %d", len(row))
		}
	}
}

// --- path restrictions ------------------------------------------------------------

func TestDataToolPathRestrictions(t *testing.T) {
	tool, base := newTestDataTool(t, map[string]string{
		"inside.csv": "a\n1\n",
	})

	// Escape via traversal.
	if _, err := runAction(t, tool, "profile", map[string]any{"path": "../../etc/passwd"}); err == nil {
		t.Fatal("path traversal outside the base dir must be rejected")
	}

	// Absolute path outside the base dir.
	outside := t.TempDir()
	if _, err := runAction(t, tool, "profile", map[string]any{"path": filepath.Join(outside, "x.csv")}); err == nil {
		t.Fatal("absolute path outside the base dir must be rejected")
	}

	// Inside the base dir still works.
	if _, err := runAction(t, tool, "profile", map[string]any{"path": "inside.csv"}); err != nil {
		t.Fatalf("in-base dataset must load: %v (base=%s)", err, base)
	}
}

// --- size bound (backend honesty) ---------------------------------------------------

func TestDatasetSizeBoundIsEnforced(t *testing.T) {
	prev := maxDatasetBytes
	t.Cleanup(func() { maxDatasetBytes = prev })

	tool, _ := newTestDataTool(t, map[string]string{
		"tiny.csv": "a,b,c\n1,2,3\n",
	})

	// Shrink the bound instead of materializing a huge fixture.
	maxDatasetBytes = 4 // tiny.csv is 12 bytes > 4
	if _, err := tool.LoadTest("tiny.csv"); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("size bound must reject oversized inputs honestly, got: %v", err)
	}

	maxDatasetBytes = prev
	if _, err := tool.LoadTest("tiny.csv"); err != nil {
		t.Fatalf("normal size must load: %v", err)
	}
}

// --- end-to-end through the action surface -------------------------------------------

func TestFullAnalysisChainDeterministic(t *testing.T) {
	tool, _ := newTestDataTool(t, map[string]string{
		"sales.csv":   "region,product,revenue\nEMEA,widget,120\nAPAC,widget,80\nEMEA,gadget,200\nAPAC,gadget,60\n",
		"targets.csv": "product,target\nwidget,90\ngadget,180\n",
	})

	// analyze → aggregate → join → export, all deterministic.
	steps := []struct {
		action string
		args   map[string]any
	}{
		{"analyze", map[string]any{"path": "sales.csv"}},
		{"aggregate", map[string]any{"path": "sales.csv", "by": "product", "aggs": []string{"sum:revenue"}, "mode": "json"}},
		{"join", map[string]any{"path": "sales.csv", "path2": "targets.csv", "key": "product", "how": "left", "mode": "json"}},
	}

	for _, s := range steps {
		out1, err := runAction(t, tool, s.action, s.args)
		if err != nil {
			t.Fatalf("%s: %v", s.action, err)
		}
		out2, err := runAction(t, tool, s.action, s.args)
		if err != nil {
			t.Fatalf("%s (repeat): %v", s.action, err)
		}
		if out1 != out2 {
			t.Fatalf("%s must be deterministic", s.action)
		}
	}
}

// TestJSONColumnOrderIsDeterministic pins the v1.8.7 loadJSON fix:
// encoding/json map iteration is randomized, so deriving the column
// order from the decoded map made every JSON analysis nondeterministic.
// Column order must now follow the document order on every load.
func TestJSONColumnOrderIsDeterministic(t *testing.T) {
	// Fixture keys deliberately follow document order that differs from
	// alphabetical order (omega,alpha,mid) so a sorted-order regression
	// would fail. Key names must avoid the retired product codename.
	content := `[{"omega": 1, "alpha": 2, "mid": 3}, {"omega": 4, "alpha": 5, "mid": 6}]`

	for run := 0; run < 20; run++ {
		tool, _ := newTestDataTool(t, map[string]string{"order.json": content})
		ds, err := tool.LoadTest("order.json")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(ds.Columns, ",") != "omega,alpha,mid" {
			t.Fatalf("run %d: JSON column order must follow document order, got %v", run, ds.Columns)
		}
	}

	// JSONL too.
	for run := 0; run < 20; run++ {
		tool, _ := newTestDataTool(t, map[string]string{
			"order.jsonl": "{\"omega\":1,\"alpha\":2}\n{\"alpha\":5,\"omega\":4,\"mid\":9}\n",
		})
		ds, err := tool.LoadTest("order.jsonl")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(ds.Columns, ",") != "omega,alpha,mid" {
			t.Fatalf("run %d: JSONL column order must be first-seen across lines, got %v", run, ds.Columns)
		}
	}
}
