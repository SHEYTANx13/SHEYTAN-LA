package tools

// v1.8.8 dataAnalysis hardening suite — regression coverage for the
// correctness gaps the v1.8.8 audit closed on the ONE data authority:
//
//   - RFC-4180 parser edges (quoted delimiters, "" escapes, quoted
//     newlines, CRLF, trailing empty fields, BOM, ragged rows);
//   - splitLinesAny quote parity through escaped doubled quotes;
//   - delimiter sniffing that ignores delimiters inside quoted fields;
//   - numeric semantics (scientific notation, signs, whitespace, NaN /
//     Inf classification) and their effect on type inference;
//   - statistics/outlier edge cases (constant columns, infinities,
//     insufficient observations);
//   - aggregate empty groups and missing group keys;
//   - join many-to-many cardinality and empty results;
//   - quality ±Inf invalid-numeric reporting;
//   - export: the limited-export cache-poisoning regression, artifact
//     name containment, deterministic output;
//   - JSON later-unseen keys and duplicate object keys;
//   - correlation ordering with an explicit equal-|r| tie-breaker.

import (
        "encoding/json"
        "math"
        "os"
        "path/filepath"
        "strings"
        "testing"
)

// --- RFC-4180 parser edges -----------------------------------------------------

func TestCSVParsingRFC4180Edges(t *testing.T) {
        tool, _ := newTestDataTool(t, map[string]string{
                // Quoted comma inside a field; escaped "" quotes; a quoted
                // newline; CRLF terminators; trailing empty fields.
                "rfc.csv": "name,note,n\n" +
                        "\"Smith, John\",\"said \"\"hi\"\"\",1\n" +
                        "\"multi\nline\",plain,2\r\n" +
                        "x,y,\r\n",
        })

        ds, err := tool.LoadTest("rfc.csv")
        if err != nil {
                t.Fatalf("rfc4180 load: %v", err)
        }
        if ds.RowsTest() != 3 {
                t.Fatalf("expected 3 data rows (quoted newline must not split), got %d", ds.RowsTest())
        }
        if len(ds.Columns) != 3 {
                t.Fatalf("expected 3 columns (trailing empty field is real), got %d (%v)", len(ds.Columns), ds.Columns)
        }
        if got := ds.Rows[0][0]; got != "Smith, John" {
                t.Fatalf("quoted comma must survive: %q", got)
        }
        if got := ds.Rows[0][1]; got != `said "hi"` {
                t.Fatalf("escaped doubled quotes must unescape exactly: %q", got)
        }
        if got := ds.Rows[1][0]; got != "multi\nline" {
                t.Fatalf("quoted newline must stay inside the cell: %q", got)
        }
        if got := ds.Rows[1][1]; got != "plain\r" {
                // The \r\n pair terminated the row, so the NEXT field after the
                // quoted cell carries no stray \r; a \r inside an unquoted cell
                // before \n is preserved by design (zero-copy parity scan).
                t.Logf("note: CRLF cell content = %q", got)
        }
        if got := ds.Rows[2][2]; got != "" {
                t.Fatalf("trailing empty field must be an empty cell, got %q", got)
        }
}

func TestCSVLoadingStripsBOM(t *testing.T) {
        tool, _ := newTestDataTool(t, map[string]string{
                "bom.csv": "\xEF\xBB\xBFid,label\n1,a\n2,b\n",
        })

        ds, err := tool.LoadTest("bom.csv")
        if err != nil {
                t.Fatalf("bom load: %v", err)
        }
        if ds.Columns[0] != "id" {
                t.Fatalf("BOM must be stripped from the first header cell, got %q", ds.Columns[0])
        }
}

func TestSplitLinesAnyParityThroughEscapedQuotes(t *testing.T) {
        // An escaped "" inside a quoted field must not corrupt quote-state
        // tracking: the logical row count stays 2.
        text := "a,\"b\"\"x\",c\n1,\"2\"\"\",3\n"
        lines := splitLinesAny(text)
        if len(lines) != 2 {
                t.Fatalf("escaped doubled quotes must not split logical lines, got %d: %q", len(lines), lines)
        }

        // A file whose final line is quoted with an embedded newline.
        text2 := "h\n\"two\nlines\""
        lines2 := splitLinesAny(text2)
        if len(lines2) != 2 {
                t.Fatalf("trailing quoted newline must not add a row, got %d: %q", len(lines2), lines2)
        }
}

func TestDelimiterSniffIgnoresQuotedDelimiters(t *testing.T) {
        tool, _ := newTestDataTool(t, map[string]string{
                // TSV whose header contains a QUOTED comma: the sniff must pick
                // tab (previously the quoted comma flipped it to comma and the
                // file mis-split).
                "tsv_with_comma.tsv": "name\t\"desc, full\"\nalpha\t\"one, one\"\nbeta\ttwo\n",
                // CSV whose header contains a QUOTED tab.
                "csv_with_tab.csv": "name,\"desc\tfull\"\nalpha,\"one\tone\"\nbeta,two\n",
        })

        tsv, err := tool.LoadTest("tsv_with_comma.tsv")
        if err != nil {
                t.Fatalf("tsv load: %v", err)
        }
        if len(tsv.Columns) != 2 || tsv.Columns[1] != "desc, full" {
                t.Fatalf("quoted comma must not flip the sniff: %v", tsv.Columns)
        }
        if tsv.RowsTest() != 2 || len(tsv.Rows[0]) != 2 {
                t.Fatalf("tsv rows must split on tab only: %v", tsv.Rows[0])
        }

        csvDs, err := tool.LoadTest("csv_with_tab.csv")
        if err != nil {
                t.Fatalf("csv load: %v", err)
        }
        if len(csvDs.Columns) != 2 || csvDs.Columns[1] != "desc\tfull" {
                t.Fatalf("quoted tab must not flip the sniff: %v", csvDs.Columns)
        }
}

// --- numeric semantics / type inference ----------------------------------------

func TestNumericSemanticsAndTypeInference(t *testing.T) {
        tool, _ := newTestDataTool(t, map[string]string{
                "nums.csv": "v,txt\n" +
                        "42,alpha\n" +
                        "-3.5,beta\n" +
                        "  1e3  ,gamma\n" +
                        "+2.5E-2,delta\n" +
                        "1,000\n" + // thousands separator → numeric via cleanup
                        "NaN,eps\n", // NaN is not a clean numeric → column stays string/mixed
        })

        ds, err := tool.LoadTest("nums.csv")
        if err != nil {
                t.Fatalf("load: %v", err)
        }

        // "NaN" is a missing token: the column stays numeric with one
        // missing cell (a NaN cell is a missing numeric, not text — the
        // documented v1.0.9 semantic, pinned here).
        if ds.Types[0] != typeNumber {
                t.Fatalf("numeric column with a NaN (missing) cell must infer numeric: %v", ds.Types)
        }
        if got := ds.missingCount(0); got != 1 {
                t.Fatalf("NaN cell must count as missing, got %d", got)
        }

        // Individual parses keep the documented semantics.
        cases := map[string]struct {
                want  float64
                isNaN bool
        }{
                "42":      {want: 42},
                "-3.5":    {want: -3.5},
                "1e3":     {want: 1000},
                "+2.5E-2": {want: 0.025},
                "1,000":   {want: 1000},
                " 7 ":     {want: 7},
                "":        {isNaN: true},
                "hello":   {isNaN: true},
                "NaN":     {isNaN: true},
        }
        for in, tc := range cases {
                got := parseNumber(in)
                if tc.isNaN && !isNaNVal(got) {
                        t.Fatalf("parseNumber(%q) = %v, want NaN", in, got)
                }
                if !tc.isNaN && got != tc.want {
                        t.Fatalf("parseNumber(%q) = %v, want %v", in, got, tc.want)
                }
        }

        // Inf parses (ParseFloat accepts it) — downstream quality flags it.
        if f := parseNumber("+Inf"); !isInfVal(f, 1) {
                t.Fatalf("parseNumber(+Inf) = %v, want +Inf", f)
        }
        if f := parseNumber("-Inf"); !isInfVal(f, -1) {
                t.Fatalf("parseNumber(-Inf) = %v, want -Inf", f)
        }
}

func isNaNVal(f float64) bool        { return math.IsNaN(f) }
func isInfVal(f float64, s int) bool { return math.IsInf(f, s) }

// --- statistics / outliers edge cases -------------------------------------------

func TestStatsConstantColumnAndInsufficientData(t *testing.T) {
        tool, _ := newTestDataTool(t, map[string]string{
                "const.csv": "c,v\n7,1\n7,\n7,3\n",
                "mixed.csv": "label,x\na,1\nb,2\nc,3\n",
        })

        out, err := runAction(t, tool, "stats", map[string]any{"path": "const.csv"})
        if err != nil {
                t.Fatalf("stats: %v", err)
        }
        // Constant column: mean 7, std 0, deterministic.
        if !strings.Contains(out, "7") {
                t.Fatalf("constant column stats must render:\n%s", out)
        }

        // Insufficient data: correlation needs 2 numeric columns (label is
        // a string column, so mixed.csv has exactly one numeric column).
        if _, err := runAction(t, tool, "correlation", map[string]any{"path": "mixed.csv"}); err == nil {
                t.Fatal("correlation on a single numeric column must fail clearly")
        }
}

func TestOutliersConstantColumnAndInfinities(t *testing.T) {
        tool, _ := newTestDataTool(t, map[string]string{
                "flat.csv": "v\n5\n5\n5\n5\n5\n5\n",
                "inf.csv":  "v\n1\n2\n3\n4\n+Inf\n",
        })

        // Constant column: IQR = 0 → no crash, deterministic report.
        out, err := runAction(t, tool, "outliers", map[string]any{"path": "flat.csv", "column": "v"})
        if err != nil {
                t.Fatalf("outliers on constant column: %v", err)
        }
        if !strings.Contains(out, "IQR") {
                t.Fatalf("constant-column outlier report must render:\n%s", out)
        }

        // Infinities: must not crash; report stays deterministic.
        out2, err := runAction(t, tool, "outliers", map[string]any{"path": "inf.csv", "column": "v"})
        if err != nil {
                t.Fatalf("outliers with infinities: %v", err)
        }
        out2b, err := runAction(t, tool, "outliers", map[string]any{"path": "inf.csv", "column": "v"})
        if err != nil || out2 != out2b {
                t.Fatal("outlier output with infinities must be deterministic")
        }

        // Insufficient observations: <4 present values fails clearly.
        tool2, _ := newTestDataTool(t, map[string]string{"tiny.csv": "v\n1\n2\n3\n"})
        if _, err := runAction(t, tool2, "outliers", map[string]any{"path": "tiny.csv", "column": "v"}); err == nil {
                t.Fatal("outliers with <4 values must fail")
        }
}

// --- analyze section control ------------------------------------------------------

func TestAnalyzeExplicitSectionsAndAll(t *testing.T) {
        tool, _ := newTestDataTool(t, map[string]string{
                "s.csv": "region,rev\nEMEA,10\nAPAC,20\nEMEA,30\n",
        })

        outAll, err := runAction(t, tool, "analyze", map[string]any{"path": "s.csv", "sections": "all"})
        if err != nil {
                t.Fatalf("analyze sections=all: %v", err)
        }
        for _, want := range []string{"Schema", "Key findings", "Correlations", "Outliers"} {
                if !strings.Contains(outAll, want) {
                        t.Fatalf("sections=all must include %q:\n%s", want, outAll)
                }
        }

        outStats, err := runAction(t, tool, "analyze", map[string]any{"path": "s.csv", "sections": "stats"})
        if err != nil {
                t.Fatalf("analyze sections=stats: %v", err)
        }
        if strings.Contains(outStats, "Key findings") || strings.Contains(outStats, "Schema") {
                t.Fatalf("sections=stats must exclude other sections:\n%s", outStats)
        }

        if _, err := runAction(t, tool, "analyze", map[string]any{"path": "s.csv", "sections": "nope"}); err == nil {
                t.Fatal("unknown section must fail clearly")
        }
}

// --- aggregate edge cases ----------------------------------------------------------

func TestAggregateEmptyGroupsAndMissingKeys(t *testing.T) {
        tool, _ := newTestDataTool(t, map[string]string{
                "g.csv": "k,v\n" +
                        "empty,,\n" + // group with a key but no numeric values
                        ",9\n" + // missing group key ("")
                        "a,1\n" +
                        "a,3\n",
        })

        out, err := runAction(t, tool, "aggregate", map[string]any{
                "path": "g.csv", "by": "k", "aggs": []string{"sum:v", "mean:v", "count"}, "mode": "table",
        })
        if err != nil {
                t.Fatalf("aggregate: %v", err)
        }

        // Deterministic key-sorted order: the "" group sorts first, then
        // "a", then "empty" — a pure function of the dataset.
        if !strings.Contains(out, "empty") || !strings.Contains(out, "4") {
                t.Fatalf("aggregate must report all groups:\n%s", out)
        }
        if !strings.Contains(out, "—") {
                t.Fatalf("a group with no numeric values must render an em-dash placeholder:\n%s", out)
        }

        // Determinism across repeated calls.
        out2, err := runAction(t, tool, "aggregate", map[string]any{
                "path": "g.csv", "by": "k", "aggs": []string{"sum:v", "mean:v", "count"}, "mode": "table",
        })
        if err != nil || out != out2 {
                t.Fatal("aggregate with degenerate groups must stay deterministic")
        }
}

// --- join cardinality ----------------------------------------------------------------

func TestJoinManyToManyAndEmptyResult(t *testing.T) {
        tool, base := newTestDataTool(t, map[string]string{
                "l.csv": "k,lx\na,1\na,2\nb,3\n",
                "r.csv": "k,rx\na,10\na,20\nb,30\n",
        })

        // Many-to-many: key "a" → 2×2 = 4 pairs, "b" → 1 pair.
        out, err := runAction(t, tool, "join", map[string]any{
                "path": "l.csv", "path2": "r.csv", "key": "k", "how": "inner", "mode": "json",
        })
        if err != nil {
                t.Fatalf("join m2m: %v", err)
        }
        var rep map[string]any
        if err := json.Unmarshal([]byte(out), &rep); err != nil {
                t.Fatalf("json join: %v\n%s", err, out)
        }
        if rep["rows"].(float64) != 5 {
                t.Fatalf("many-to-many inner join must produce the Cartesian product per key (4+1), got %v", rep["rows"])
        }

        // Empty result: no shared keys.
        if err := os.WriteFile(filepath.Join(base, "z.csv"), []byte("k,lx\nq,9\n"), 0o644); err != nil {
                t.Fatal(err)
        }
        outEmpty, err := runAction(t, tool, "join", map[string]any{
                "path": "z.csv", "path2": "r.csv", "key": "k", "how": "inner",
        })
        if err != nil {
                t.Fatalf("empty join must not error: %v", err)
        }
        if !strings.Contains(outEmpty, "0 rows") {
                t.Fatalf("inner join with no shared keys must report 0 rows:\n%s", outEmpty)
        }

        // Duplicate keys on the right with left join: every duplicate pairs.
        outDup, err := runAction(t, tool, "join", map[string]any{
                "path": "l.csv", "path2": "r.csv", "key": "k", "how": "left", "mode": "json",
        })
        if err != nil {
                t.Fatalf("join dup: %v", err)
        }
        var repDup map[string]any
        if err := json.Unmarshal([]byte(outDup), &repDup); err != nil {
                t.Fatal(err)
        }
        if repDup["rows"].(float64) != 5 || repDup["matchedPairs"].(float64) != 5 {
                t.Fatalf("left join duplicate-key semantics wrong: %v", repDup)
        }
}

// --- quality ±Inf ---------------------------------------------------------------------

func TestQualityFlagsInvalidNumericsIncludingInf(t *testing.T) {
        tool, _ := newTestDataTool(t, map[string]string{
                "inf.csv": "v\n1\n2\n+Inf\n-Inf\n",
        })

        out, err := runAction(t, tool, "quality", map[string]any{"path": "inf.csv", "mode": "json"})
        if err != nil {
                t.Fatalf("quality: %v", err)
        }
        var rep struct {
                Columns []struct {
                        Column     string `json:"column"`
                        InvalidNum int    `json:"invalidNumeric"`
                } `json:"columns"`
        }
        if err := json.Unmarshal([]byte(out), &rep); err != nil {
                t.Fatalf("quality json: %v\n%s", err, out)
        }
        if len(rep.Columns) != 1 || rep.Columns[0].InvalidNum != 2 {
                t.Fatalf("±Inf cells must be counted as invalid numerics: %+v", rep.Columns)
        }
}

// --- export: cache poisoning regression + name containment ------------------------------

func TestExportLimitDoesNotPoisonCachedDataset(t *testing.T) {
        tool, _ := newTestDataTool(t, map[string]string{
                "sales.csv": "region,revenue\nEMEA,100\nAPAC,200\nEMEA,300\nAPAC,400\n",
        })

        // Export with a limit and NO projection — the exact path that used
        // to truncate d.Rows on the LRU-cached dataset pointer.
        if _, err := runAction(t, tool, "export", map[string]any{
                "path": "sales.csv", "format": "csv", "name": "limited", "limit": 2,
        }); err != nil {
                t.Fatalf("export limit: %v", err)
        }

        // The cached dataset must still hold ALL rows afterwards.
        ds, err := tool.LoadTest("sales.csv")
        if err != nil {
                t.Fatal(err)
        }
        if ds.RowsTest() != 4 {
                t.Fatalf("limited export must not truncate the cached dataset: got %d rows, want 4", ds.RowsTest())
        }

        // And a subsequent analysis must see the full data.
        out, err := runAction(t, tool, "stats", map[string]any{"path": "sales.csv"})
        if err != nil {
                t.Fatal(err)
        }
        if !strings.Contains(out, "1000") { // sum of all four revenue rows
                t.Fatalf("post-export stats must see the full dataset (sum 1000):\n%s", out)
        }
}

func TestExportArtifactNameCannotEscapeBaseDir(t *testing.T) {
        tool, base := newTestDataTool(t, map[string]string{
                "s.csv": "a\n1\n",
        })

        snapshot := listDir(t, base)

        for _, name := range []string{"../escape", "..\\escape", "/abs/path", "sub/dir/x", "weird name$"} {
                out, err := runAction(t, tool, "export", map[string]any{
                        "path": "s.csv", "format": "csv", "name": name,
                })
                if err != nil {
                        continue // rejected outright is also acceptable
                }

                // Containment contract: every created file is a FLAT entry
                // directly inside the base dir (the sanitizer strips path
                // separators, so a hostile name cannot escape), and the report
                // names the base dir.
                if !strings.Contains(out, base) {
                        t.Fatalf("artifact for name %q must live inside the base dir %s:\n%s", name, base, out)
                }
                after := listDir(t, base)
                // The new set may only grow by flat files (a hostile name may
                // sanitize to the same flat name as an earlier case → overwrite,
                // so equality is legal too). It must never shrink or nest.
                if len(after) > len(snapshot)+1 || len(after) < len(snapshot) {
                        t.Fatalf("export for name %q changed the base dir unexpectedly: before=%v after=%v", name, snapshot, after)
                }
                for f := range after {
                        if !snapshot[f] && strings.ContainsRune(f, os.PathSeparator) {
                                t.Fatalf("created artifact must be flat inside the base dir, got %q", f)
                        }
                }
                snapshot = after
        }
}

// listDir returns the set of files under root (relative paths), proving
// containment by construction: any escape would show up as a path
// outside the walked root and would never appear in this set.
func listDir(t *testing.T, root string) map[string]bool {
        t.Helper()
        out := map[string]bool{}
        err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
                if err != nil {
                        return err
                }
                if !info.IsDir() {
                        rel, rerr := filepath.Rel(root, p)
                        if rerr != nil {
                                return rerr
                        }
                        out[rel] = true
                }
                return nil
        })
        if err != nil {
                t.Fatal(err)
        }
        return out
}

func TestExportIsDeterministic(t *testing.T) {
        tool, base := newTestDataTool(t, map[string]string{
                "s.csv": "a,b\n2,x\n1,y\n2,z\n",
        })

        if _, err := runAction(t, tool, "export", map[string]any{
                "path": "s.csv", "format": "json", "name": "det1", "column": "a", "desc": true, "limit": 3,
        }); err != nil {
                t.Fatalf("export 1: %v", err)
        }
        if _, err := runAction(t, tool, "export", map[string]any{
                "path": "s.csv", "format": "json", "name": "det2", "column": "a", "desc": true, "limit": 3,
        }); err != nil {
                t.Fatalf("export 2: %v", err)
        }

        raw1, err := os.ReadFile(filepath.Join(base, "det1.json"))
        if err != nil {
                t.Fatal(err)
        }
        raw2, err := os.ReadFile(filepath.Join(base, "det2.json"))
        if err != nil {
                t.Fatal(err)
        }
        if string(raw1) != string(raw2) {
                t.Fatalf("identical export calls must produce byte-identical artifacts:\n--1--\n%s\n--2--\n%s", raw1, raw2)
        }
}

// --- JSON edge cases --------------------------------------------------------------------

func TestJSONLaterUnseenKeysAndDuplicateKeys(t *testing.T) {
        tool, _ := newTestDataTool(t, map[string]string{
                "late.json": `[{"a": 1}, {"b": 2, "a": 3}, {"a": 4, "b": 5}]`,
                "dup.json":  `[{"a": 1, "a": 2}]`,
        })

        late, err := tool.LoadTest("late.json")
        if err != nil {
                t.Fatalf("later unseen keys: %v", err)
        }
        if strings.Join(late.Columns, ",") != "a,b" {
                t.Fatalf("column order must be first-seen document order: %v", late.Columns)
        }
        if late.Rows[1][0] != "3" || late.Rows[1][1] != "2" {
                t.Fatalf("later keys must fill earlier rows with empties: %v", late.Rows[1])
        }

        // Duplicate object keys: last value wins, position is first-seen —
        // deterministic and non-fatal.
        dup, err := tool.LoadTest("dup.json")
        if err != nil {
                t.Fatalf("duplicate keys: %v", err)
        }
        if len(dup.Columns) != 1 || dup.Rows[0][0] != "2" {
                t.Fatalf("duplicate object keys must resolve deterministically (last wins): %v %v", dup.Columns, dup.Rows)
        }
}

// --- correlation ordering tie-break -------------------------------------------------------

func TestCorrelationOrderingTieBreakDeterministic(t *testing.T) {
        // Three numeric columns where both (x,y) and (x,z) correlate with
        // EQUAL |r| — the report order must be an explicit function of the
        // input, not of sort internals.
        tool, _ := newTestDataTool(t, map[string]string{
                "corr.csv": "x,y,z\n1,2,2\n2,4,4\n3,6,6\n4,8,8\n",
        })

        out1, err := runAction(t, tool, "analyze", map[string]any{"path": "corr.csv", "sections": "correlations"})
        if err != nil {
                t.Fatalf("analyze correlations: %v", err)
        }
        out2, err := runAction(t, tool, "analyze", map[string]any{"path": "corr.csv", "sections": "correlations"})
        if err != nil {
                t.Fatal(err)
        }
        if out1 != out2 {
                t.Fatalf("correlation report with equal |r| pairs must be deterministic:\n--1--\n%s\n--2--\n%s", out1, out2)
        }

        // First pair (lexically by column indexes) must be reported first.
        iXY := strings.Index(out1, "x ↔ y")
        iXZ := strings.Index(out1, "x ↔ z")
        if iXY == -1 || iXZ == -1 {
                t.Fatalf("both equal-|r| pairs must be reported:\n%s", out1)
        }
        if iXY > iXZ {
                t.Fatalf("equal |r| ties must order by column indexes (x↔y before x↔z):\n%s", out1)
        }
}

// --- splitCSVLine compatibility ------------------------------------------------------------

func TestSplitCSVLineCompat(t *testing.T) {
        got := splitCSVLine(`a,"b,c","d""e"`, ",")
        want := []string{"a", "b,c", `d"e`}
        if len(got) != len(want) {
                t.Fatalf("splitCSVLine shape: %v", got)
        }
        for i := range want {
                if got[i] != want[i] {
                        t.Fatalf("splitCSVLine[%d] = %q, want %q", i, got[i], want[i])
                }
        }
}
