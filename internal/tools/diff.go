// Package tools — diff: line-level file comparison (Myers O(ND)).
//
// Gives the agent a native "what changed" answer — for code edits it just
// made (verify), for data exports between days, or for the classic
// "compare my two config files". Output is a bounded unified-style block
// with -/+ lines; a similarity summary reports how close the files are
// when the edit distance explodes (identical/different-region fallback
// keeps pathological inputs cheap).
package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// DiffTool compares files line by line.
type DiffTool struct{}

// Name implements the agent tool interface.
func (DiffTool) Name() string { return "diff" }

// Description implements the agent tool interface.
func (DiffTool) Description() string {
	return "Compare two files line by line (Myers diff) and return the changed regions (- removed, + added). " +
		`{"path":"a.txt","path2":"b.txt"} — context:N shows N unchanged lines around each change (default 2). ` +
		"Use it to verify an edit you just made, or to spot what changed between two versions of a file."
}

// Parameters implements the agent tool interface.
func (DiffTool) Parameters() any {
	return struct {
		Path    string `json:"path"`
		Path2   string `json:"path2"`
		Context int    `json:"context,omitempty"`
	}{}
}

const (
	diffMaxLines = 60000 // per file
	diffMaxD     = 20000 // edit-distance cap before fallback
	diffMaxOut   = 300   // output hunk line cap
)

// Run implements the agent tool interface.
func (t DiffTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path    string `json:"path"`
		Path2   string `json:"path2"`
		Context int    `json:"context"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	if p.Path == "" || p.Path2 == "" {
		return "", fmt.Errorf("path and path2 are required")
	}
	a, err := readLinesCapped(ResolvePath(p.Path))
	if err != nil {
		return "", err
	}
	b, err := readLinesCapped(ResolvePath(p.Path2))
	if err != nil {
		return "", err
	}
	if p.Context <= 0 {
		p.Context = 2
	}
	return diffReport(a, b, p.Context), nil
}

func readLinesCapped(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > diffMaxLines {
			return nil, fmt.Errorf("file too large for line diff (>%d lines) — chunk it with the files tool first", diffMaxLines)
		}
	}
	return lines, sc.Err()
}

// diffOp is one edit-script entry: keep/delete/add.
type diffOp struct {
	kind byte // '=' '-' '+'
	line string
	aIdx int // index into a (for '-' and '=')
	bIdx int // index into b (for '+' and '=')
}

// diffReport renders the bounded diff of two line slices.
func diffReport(a, b []string, ctxLines int) string {
	ops := myersDiff(a, b)
	if ops == nil {
		// Edit distance cap hit — fall back to a block-level summary.
		return fallbackDiff(a, b)
	}
	var (
		out            strings.Builder
		hunkLines      int
		inHunk         bool
		ctxLeft        int
		same, del, add int
	)
	flush := func() { inHunk = false }
	for _, op := range ops {
		switch op.kind {
		case '=':
			same++
			if inHunk {
				if ctxLeft > 0 {
					fmt.Fprintf(&out, "  %s\n", op.line)
					hunkLines++
					ctxLeft--
				} else {
					flush()
				}
			}
		case '-':
			del++
			if !inHunk {
				out.WriteString("  ── changed region ──\n")
				inHunk = true
				ctxLeft = ctxLines
			}
			fmt.Fprintf(&out, "- %s\n", op.line)
			hunkLines++
		case '+':
			add++
			if !inHunk {
				out.WriteString("  ── changed region ──\n")
				inHunk = true
				ctxLeft = ctxLines
			}
			fmt.Fprintf(&out, "+ %s\n", op.line)
			hunkLines++
		}
		if hunkLines >= diffMaxOut {
			out.WriteString("… diff output truncated\n")
			break
		}
	}
	total := same + del + add
	fmt.Fprintf(&out, "\n%d line(s): %d unchanged, %d removed, %d added", total, same, del, add)
	if total > 0 {
		fmt.Fprintf(&out, " (%.1f%% similar)", 100*float64(same)/float64(total))
	}
	out.WriteString("\n")
	return out.String()
}

// fallbackDiff summarizes wildly different files without the O(ND) walk.
func fallbackDiff(a, b []string) string {
	setA := map[string]int{}
	for _, l := range a {
		setA[l]++
	}
	shared := 0
	for _, l := range b {
		if setA[l] > 0 {
			shared++
			setA[l]--
		}
	}
	total := len(a) + len(b)
	if total == 0 {
		total = 1
	}
	return fmt.Sprintf("files differ beyond the diff cap (edit distance > %d)\nfirst file: %d lines | second file: %d lines | shared lines: %d (%.1f%% overlap)\nuse files read with offsetLine/maxLines to compare regions manually",
		diffMaxD, len(a), len(b), shared, 200*float64(shared)/float64(total))
}

// myersDiff computes the edit script with Myers' greedy algorithm
// (bounded by diffMaxD; returns nil when the cap is exceeded).
func myersDiff(a, b []string) []diffOp {
	n, m := len(a), len(b)
	maxD := n + m
	if maxD > diffMaxD {
		maxD = diffMaxD
	}
	// Trim common prefix/suffix first — the classic optimization that
	// keeps "one line changed in a big file" at D=1.
	prefix := 0
	for prefix < n && prefix < m && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < n-prefix && suffix < m-prefix && a[n-1-suffix] == b[m-1-suffix] {
		suffix++
	}
	coreA := a[prefix : n-suffix]
	coreB := b[prefix : m-suffix]
	cn, cm := len(coreA), len(coreB)

	var coreOps []diffOp
	if cn == 0 && cm == 0 {
		coreOps = nil
	} else if cn == 0 {
		for j, l := range coreB {
			coreOps = append(coreOps, diffOp{kind: '+', line: l, bIdx: j})
		}
	} else if cm == 0 {
		for i, l := range coreA {
			coreOps = append(coreOps, diffOp{kind: '-', line: l, aIdx: i})
		}
	} else {
		var ok bool
		coreOps, ok = myersCore(coreA, coreB)
		if !ok {
			return nil
		}
	}

	var ops []diffOp
	for i := 0; i < prefix; i++ {
		ops = append(ops, diffOp{kind: '=', line: a[i], aIdx: i, bIdx: i})
	}
	ops = append(ops, coreOps...)
	for i := 0; i < suffix; i++ {
		ai := n - suffix + i
		bi := m - suffix + i
		ops = append(ops, diffOp{kind: '=', line: a[ai], aIdx: ai, bIdx: bi})
	}
	return ops
}

// myersCore runs the greedy LCS walk on the already-trimmed cores.
// trace[d] holds the V array AFTER iteration d (the backtrack contract).
func myersCore(a, b []string) ([]diffOp, bool) {
	n, m := len(a), len(b)
	max := n + m
	if max > diffMaxD {
		max = diffMaxD
	}
	v := make([]int, 2*max+1) // shift by max
	offset := max
	trace := make([][]int, 0, max+1)

	for d := 0; d <= max; d++ {
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				// Snapshot AFTER this iteration — the backtrack needs the
				// final state as trace[d].
				vc := make([]int, len(v))
				copy(vc, v)
				trace = append(trace, vc)
				return backtrack(trace, a, b, d, offset), true
			}
		}
		vc := make([]int, len(v))
		copy(vc, v)
		trace = append(trace, vc)
	}
	return nil, false // cap exceeded
}

// backtrack reconstructs the edit script from the V trace.
func backtrack(trace [][]int, a, b []string, d, offset int) []diffOp {
	var ops []diffOp
	x, y := len(a), len(b)
	for i := d; i > 0; i-- {
		v := trace[i]
		k := x - y
		var prevK int
		if k == -i || (k != i && v[offset+k-1] < v[offset+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := trace[i-1][offset+prevK]
		prevY := prevX - prevK

		for x > prevX && y > prevY {
			ops = append([]diffOp{{kind: '=', line: a[x-1], aIdx: x - 1, bIdx: y - 1}}, ops...)
			x--
			y--
		}
		if x > prevX {
			ops = append([]diffOp{{kind: '-', line: a[x-1], aIdx: x - 1}}, ops...)
			x--
		} else if y > prevY {
			ops = append([]diffOp{{kind: '+', line: b[y-1], bIdx: y - 1}}, ops...)
			y--
		}
	}
	for x > 0 && y > 0 {
		ops = append([]diffOp{{kind: '=', line: a[x-1], aIdx: x - 1, bIdx: y - 1}}, ops...)
		x--
		y--
	}
	return ops
}
