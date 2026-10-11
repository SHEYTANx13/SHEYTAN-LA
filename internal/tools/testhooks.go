// Package tools — cross-package test surface.
//
// The release stress suite lives in package cmd (cross-package by design:
// it tests the app exactly like an external consumer). A few of its v1.0.9
// scenarios assert parser-level internals, so this file exports the
// narrowest possible read-only shims. Production code never calls these.
package tools

import (
	"math"
	"sync/atomic"
)

// SplitCSVLineTest exposes csvFields to the stress suite (parity checks
// against the v1.0.8 parser semantics on hostile inputs).
func SplitCSVLineTest(line string) []string { return csvFields(line, ',') }

// SplitLinesTest exposes splitLinesAny to the stress suite (CRLF handling,
// quoted newlines).
func SplitLinesTest(s string) []string { return splitLinesAny(s) }

// ParseNumberTest exposes parseNumber to the stress suite (fast-path /
// cleanup-path parity).
func ParseNumberTest(s string) float64 { return parseNumber(s) }

// NaNTest returns a NaN value for comparisons in the stress suite.
func NaNTest() float64 { return math.NaN() }

// IsNaNTest reports whether v is NaN (mirrors math.IsNaN for symmetry with
// NaNTest).
func IsNaNTest(v float64) bool { return math.IsNaN(v) }

// LoadTest loads a dataset by relative path (stress + unit suites).
func (t *DataTool) LoadTest(rel string) (*dataset, error) { return t.load(rel) }

// RowsTest returns the row count of a dataset (nil-safe).
func (d *dataset) RowsTest() int {
	if d == nil {
		return 0
	}
	return len(d.Rows)
}

// NumericColumnTest exposes the parse-once numeric column cache.
func (d *dataset) NumericColumnTest(col int) []float64 { return d.numericColumn(col) }

// fetchAllowPrivateTest (v1.1.4: now an atomic) relaxes the fetch tool's
// SSRF guard so the release stress suite (package cmd) can exercise
// HTML→text extraction against a loopback httptest server. It is false in
// production; only the stress suite flips it, and it restores the previous
// value when done. The plain bool previously raced concurrent toggles.
var fetchAllowPrivateTest atomic.Bool

// SetFetchPrivateDestinationsAllowedForTest toggles the fetch SSRF guard's
// public-destination requirement. Test-only — never call from production
// code paths.
func SetFetchPrivateDestinationsAllowedForTest(v bool) bool {
	return fetchAllowPrivateTest.Swap(v)
}

// fetchPrivateAllowed reports the test-only relaxation state.
func fetchPrivateAllowed() bool {
	return fetchAllowPrivateTest.Load()
}
