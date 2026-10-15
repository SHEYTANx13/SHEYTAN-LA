package perf

// perf_test.go — v1.9.6 (R&D Phase 0): the measurement framework's own
// honest-contract pins, deterministic (no network, no engine, no timing
// sensitivity):
//
//   1. nearest-rank P50/P95 over a known sample set (exact expected
//      values, not recomputed by the same code);
//   2. every new envelope defines ALL canonical metrics as NOT MEASURED
//      with a reason — a baseline cannot silently drop a dimension;
//   3. Record stamps measured status + samples; MarkNotMeasured resets;
//   4. the target note records the GAP or a host-scoped meet — never a
//      product-wide achievement claim;
//   5. the JSON artifact round-trips with the schema version and the
//      envelope fields the R&D report requires (sha/release/host/model/
//      state/hostedCI);
//   6. the human report shows NOT MEASURED lines with reasons.

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSummarizeNearestRankExact(t *testing.T) {
	// 20 samples 1..20: nearest-rank P50 = ceil(0.5*20)=10th value = 10;
	// P95 = ceil(0.95*20)=19th value = 19.
	got := Summarize([]float64{20, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19})
	if got.N != 20 || got.Min != 1 || got.Max != 20 {
		t.Fatalf("extremes wrong: %+v", got)
	}
	if got.P50 != 10 {
		t.Fatalf("P50 = %v, want 10 (nearest rank)", got.P50)
	}
	if got.P95 != 19 {
		t.Fatalf("P95 = %v, want 19 (nearest rank)", got.P95)
	}
	if got.Mean != 10.5 {
		t.Fatalf("mean = %v, want 10.5", got.Mean)
	}
	if got.Unit != "ms" {
		t.Fatalf("unit = %q, want ms", got.Unit)
	}

	// Single sample: every percentile IS that sample.
	one := Summarize([]float64{7})
	if one.P50 != 7 || one.P95 != 7 || one.Min != 7 || one.Max != 7 {
		t.Fatalf("single-sample stats wrong: %+v", one)
	}

	// Empty: zero stats — the caller must mark NOT MEASURED.
	if empty := Summarize(nil); empty.N != 0 {
		t.Fatalf("empty stats must be n=0, got %+v", empty)
	}
}

func TestNewEnvelopeDefinesEveryCanonicalMetricNotMeasured(t *testing.T) {
	e := NewEnvelope()

	if e.Schema != SchemaVersion {
		t.Fatalf("schema = %q, want %q", e.Schema, SchemaVersion)
	}

	for _, name := range CanonicalMetrics() {
		m, ok := e.Metrics[name]
		if !ok {
			t.Fatalf("canonical metric %q missing from a fresh envelope", name)
		}
		if m.Status != StatusNotMeasured {
			t.Fatalf("metric %q = %q, want NOT MEASURED on a fresh envelope", name, m.Status)
		}
		if m.Reason == "" {
			t.Fatalf("metric %q has no NOT-MEASURED reason", name)
		}
		if m.Stats.N != 0 {
			t.Fatalf("metric %q carries samples on a fresh envelope", name)
		}
	}
}

func TestRecordAndMarkNotMeasuredRoundTrip(t *testing.T) {
	e := NewEnvelope()

	e.RecordWithTarget(MetricWarmTTFT, []float64{40, 42, 41}, 800, "api: POST /api/run accepted → first /ws/activity response frame")
	m := e.Metrics[MetricWarmTTFT]
	if m.Status != StatusMeasured || m.Stats.N != 3 || len(m.Samples) != 3 {
		t.Fatalf("recorded metric wrong: %+v", m)
	}
	if m.TargetMs == nil || *m.TargetMs != 800 {
		t.Fatalf("target missing: %+v", m)
	}
	// 41 < 800: the note must state the host-scoped meet, never a
	// product-wide claim.
	if !strings.Contains(m.TargetNote, "THIS host") {
		t.Fatalf("target note must be host-scoped: %q", m.TargetNote)
	}

	// A gap note must say GAP.
	e.RecordWithTarget(MetricColdStartInteractive, []float64{2000}, 800, "method")
	if !strings.Contains(e.Metrics[MetricColdStartInteractive].TargetNote, "gap recorded") {
		t.Fatalf("exceeded target must record the gap: %q", e.Metrics[MetricColdStartInteractive].TargetNote)
	}

	// MarkNotMeasured resets honestly.
	e.MarkNotMeasured(MetricWarmTTFT, "model removed mid-session")
	m2 := e.Metrics[MetricWarmTTFT]
	if m2.Status != StatusNotMeasured || m2.Stats.N != 0 || m2.Samples != nil || m2.Reason == "" {
		t.Fatalf("mark-not-measured did not reset honestly: %+v", m2)
	}
}

func TestEnvelopeJSONRoundTrip(t *testing.T) {
	e := NewEnvelope()
	e.CodeSHA = "070a05930f35ac09536ead74cc7cf2f376b8c2a4"
	e.Release = "1.9.6"
	e.OS = "windows"
	e.Arch = "amd64"
	e.CPU = "test-cpu"
	e.Backend = "fake-engine"
	e.State = "cold"
	e.HostedCI = true
	e.Record(MetricStreamDuration, []float64{5, 6}, "api: first response frame → done frame")

	raw, err := e.JSON()
	if err != nil {
		t.Fatalf("json: %v", err)
	}

	var back Envelope
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if back.Schema != SchemaVersion || back.Release != "1.9.6" || !back.HostedCI {
		t.Fatalf("envelope fields lost: %+v", back)
	}
	if back.Metrics[MetricStreamDuration].Status != StatusMeasured || back.Metrics[MetricStreamDuration].Stats.N != 2 {
		t.Fatalf("measured metric lost: %+v", back.Metrics[MetricStreamDuration])
	}
	if back.Metrics[MetricIdleMemoryMB].Status != StatusNotMeasured {
		t.Fatalf("unmeasured metric must stay NOT MEASURED: %+v", back.Metrics[MetricIdleMemoryMB])
	}
}

func TestReportShowsNotMeasuredWithReasons(t *testing.T) {
	e := NewEnvelope()
	e.Release = "1.9.6"
	e.MarkNotMeasured(MetricToolRoundTrip, "no agent tool round executed in this boot mode")

	report := e.Report()

	if !strings.Contains(report, "NOT MEASURED") {
		t.Fatal("the report must show NOT MEASURED dimensions")
	}
	if !strings.Contains(report, "no agent tool round executed") {
		t.Fatal("the report must show the reason")
	}
	if !strings.Contains(report, "1.9.6") {
		t.Fatal("the report must carry the release")
	}
}

// --- v1.10.2 fail-closed host classification -------------------------------

func TestValidateHostClassRejectsEmptyAndUnknown(t *testing.T) {
	if err := ValidateHostClass(""); err == nil {
		t.Fatal("empty host class must be refused (fail-closed: nothing is assumed)")
	}
	if err := ValidateHostClass("Hosted"); err == nil {
		t.Fatal("unknown class casing must be refused")
	}
	if err := ValidateHostClass("physical"); err != nil {
		t.Fatalf("physical must be accepted: %v", err)
	}
	if err := ValidateHostClass("hosted"); err != nil {
		t.Fatalf("hosted must be accepted: %v", err)
	}
}

func TestCheckHostClassificationRefusesPhysicalOverHostedEvidence(t *testing.T) {
	// The dangerous direction: a hosted runner declaring physical.
	err := CheckHostClassification(HostClassPhysical, []string{"GITHUB_ACTIONS"})
	if err == nil {
		t.Fatal("physical claim over hosted-CI evidence must be REFUSED")
	}
	if !strings.Contains(err.Error(), "fail-closed") {
		t.Fatalf("refusal must state the fail-closed contract, got: %v", err)
	}

	// Under-claiming is always safe.
	if err := CheckHostClassification(HostClassHosted, []string{"GITHUB_ACTIONS"}); err != nil {
		t.Fatalf("hosted over hosted evidence must be accepted: %v", err)
	}
	if err := CheckHostClassification(HostClassHosted, nil); err != nil {
		t.Fatalf("hosted without evidence must be accepted: %v", err)
	}

	// Physical without markers is accepted but the artifact still
	// carries the (empty) evidence list for the reader to judge.
	if err := CheckHostClassification(HostClassPhysical, nil); err != nil {
		t.Fatalf("physical without evidence must be accepted: %v", err)
	}
}

func TestDetectHostEvidenceFindsMarkers(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("CI", "1")

	found := DetectHostEvidence()
	has := func(name string) bool {
		for _, f := range found {
			if f == name {
				return true
			}
		}
		return false
	}
	if !has("GITHUB_ACTIONS") || !has("CI") {
		t.Fatalf("evidence = %v, want GITHUB_ACTIONS and CI present", found)
	}
	// v1.10.3 (R5): MEMBERSHIP only — never an exact count. Hosts may
	// legitimately export additional markers the detector knows
	// (TF_BUILD, JENKINS_URL, TRAVIS, CIRCLECI, BUILDKITE); the
	// v1.10.2 len(found)==2 assert failed spuriously on any such host.
	for _, foreign := range []string{"TF_BUILD", "JENKINS_URL", "TRAVIS", "CIRCLECI", "BUILDKITE"} {
		t.Setenv(foreign, "1")
	}
	found = DetectHostEvidence()
	// has() closes over the found variable, so it now inspects the
	// re-detected slice with the foreign markers present.
	if !has("GITHUB_ACTIONS") || !has("CI") {
		t.Fatalf("evidence = %v, want GITHUB_ACTIONS and CI still present alongside foreign markers", found)
	}
}

func TestEnvelopeCarriesHostClassification(t *testing.T) {
	env := NewEnvelope()
	env.HostClass = HostClassHosted
	env.HostEvidence = []string{"GITHUB_ACTIONS"}
	env.RAMMB = 8192
	env.Hostname = "runner-1"

	raw, err := env.JSON()
	if err != nil {
		t.Fatalf("json: %v", err)
	}

	var back Envelope
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.HostClass != HostClassHosted {
		t.Fatalf("hostClass = %q, want %q", back.HostClass, HostClassHosted)
	}
	if len(back.HostEvidence) != 1 || back.HostEvidence[0] != "GITHUB_ACTIONS" {
		t.Fatalf("hostEvidence = %v, want [GITHUB_ACTIONS]", back.HostEvidence)
	}
	if !back.HostedCI {
		t.Fatal("hostedCI must stay true for hosted artifacts (schema compat)")
	}
	if back.RAMMB != 8192 || back.Hostname != "runner-1" {
		t.Fatalf("ram/hostname lost in round-trip: %v %q", back.RAMMB, back.Hostname)
	}

	// Physical artifact: hostedCI false, evidence empty but present.
	env2 := NewEnvelope()
	env2.HostClass = HostClassPhysical
	raw2, _ := env2.JSON()
	var back2 Envelope
	_ = json.Unmarshal(raw2, &back2)
	if back2.HostedCI {
		t.Fatal("physical artifact must carry hostedCI=false")
	}
	if back2.HostClass != HostClassPhysical {
		t.Fatalf("hostClass = %q, want physical", back2.HostClass)
	}
}
