package startup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecordPhasesAndSummaryFields(t *testing.T) {
	r := New("1.11.1")
	r.SetWarm(true)
	r.StartPhase(PhaseConfigLoad)
	time.Sleep(2 * time.Millisecond)
	r.EndPhase(PhaseConfigLoad, nil)
	r.RecordPhase(PhaseCoreInit, 5*time.Millisecond, nil)
	r.StartPhase(PhaseCoreHandshake)
	r.EndPhase(PhaseCoreHandshake, fmt.Errorf("dial tcp: connection refused"))

	got := r.Summary()
	for _, want := range []string{
		"platform=",
		"build_version=1.11.1",
		"cold_or_warm=warm",
		"phase=config_load duration_ms=",
		"success=true",
		"phase=core_handshake",
		"success=false",
		"error_class=connection_refused",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q\nsummary:\n%s", want, got)
		}
	}
	// Sanitization: the error text itself must never appear.
	if strings.Contains(got, "dial tcp") {
		t.Error("summary leaked raw error text")
	}
	if strings.Contains(got, "connection refused") && strings.Contains(got, "error_class=connection refused") {
		t.Error("summary leaked error message content")
	}
}

func TestRecordsBounded(t *testing.T) {
	r := New("1.11.1")
	for i := 0; i < maxRecords+20; i++ {
		r.RecordPhase(Phase("p"), time.Duration(i)*time.Millisecond, nil)
	}
	if n := len(r.Records()); n != maxRecords {
		t.Fatalf("records bounded to %d, got %d", maxRecords, n)
	}
}

func TestEndPhaseWithoutStartRecordsMarker(t *testing.T) {
	r := New("1.11.1")
	r.EndPhase(PhaseIPCKey, nil)
	recs := r.Records()
	if len(recs) != 1 || recs[0].Phase != PhaseIPCKey || recs[0].DurationMS != 0 {
		t.Fatalf("expected instantaneous marker, got %+v", recs)
	}
}

func TestErrorClassMapping(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{context.Canceled, "canceled"},
		{context.DeadlineExceeded, "deadline"},
		{os.ErrNotExist, "not_found"},
		{os.ErrPermission, "permission"},
		{fmt.Errorf("wrap: %w", &net.OpError{Op: "dial", Err: errors.New("connection refused")}), "connection_refused"},
		{fmt.Errorf("lookup core.example: no such host"), "dns"},
		{fmt.Errorf("write: broken pipe"), "connection_reset"},
		{&timeoutErr{}, "timeout"},
		{&netOpError{}, "network"},
		{fmt.Errorf("something unexpected"), "other"},
	}
	for _, c := range cases {
		if got := ErrorClass(c.err); got != c.want {
			t.Errorf("ErrorClass(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

type netOpError struct{}

func (netOpError) Error() string   { return "network problem" }
func (netOpError) Timeout() bool   { return false }
func (netOpError) Temporary() bool { return false }

func TestJSONShape(t *testing.T) {
	r := New("1.11.1")
	r.SetWarm(false)
	r.RecordPhase(PhaseShellBuild, 12*time.Millisecond, nil)
	data, err := r.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var doc struct {
		Platform     string `json:"platform"`
		BuildVersion string `json:"build_version"`
		ColdOrWarm   string `json:"cold_or_warm"`
		Phases       []struct {
			Phase      string `json:"phase"`
			DurationMS int64  `json:"duration_ms"`
			Success    bool   `json:"success"`
			ErrorClass string `json:"error_class"`
		} `json:"phases"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v\njson:\n%s", err, data)
	}
	if doc.BuildVersion != "1.11.1" || doc.ColdOrWarm != "cold" || len(doc.Phases) != 1 {
		t.Fatalf("unexpected doc: %+v", doc)
	}
	if doc.Phases[0].Phase != "shell_build" || doc.Phases[0].DurationMS != 12 || !doc.Phases[0].Success {
		t.Fatalf("unexpected phase record: %+v", doc.Phases[0])
	}
}

func TestEmitDiagnosticMode(t *testing.T) {
	r := New("1.11.1")
	r.RecordPhase(PhaseWindowReady, 33*time.Millisecond, nil)
	r.SetDiagnostic(true)
	var buf bytes.Buffer
	r.Emit(&buf)
	out := buf.String()
	if !strings.Contains(out, "phase=window_ready") {
		t.Errorf("emit missing summary line:\n%s", out)
	}
	if !strings.Contains(out, "\"phase\": \"window_ready\"") {
		t.Errorf("diagnostic mode missing JSON records:\n%s", out)
	}
}

func TestConcurrentUse(t *testing.T) {
	r := New("1.11.1")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := Phase(fmt.Sprintf("p%d", i%4))
			r.StartPhase(p)
			r.EndPhase(p, nil)
			_ = r.Summary()
			_ = r.Warm()
		}(i)
	}
	wg.Wait()
	if len(r.Records()) == 0 {
		t.Fatal("no records after concurrent use")
	}
}
