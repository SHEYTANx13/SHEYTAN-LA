// bounded command-output capture (v1.2.4).
//
// Shell/CodeExec/Git used exec.CombinedOutput(), which buffers the ENTIRE
// output of an agent-triggered command in RAM with no cap — the only place
// in the pipeline where a single tool call could allocate without bound
// (a runaway `yes`, a full build log, a binary mis-parse). This file
// replaces that path with a streaming, bounded capture: bytes are drained
// from the process as fast as they arrive (so the child never blocks on a
// full pipe), only the first toolOutputCap bytes are retained, and an
// explicit truncation marker with the TRUE total is appended so the model
// knows what it is not seeing. Aggregate counters make the saving
// measurable (see CaptureStats).
package tools

import (
	"bytes"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/humanize"
)

// toolOutputCap bounds how much combined stdout+stderr one tool invocation
// retains. 1 MiB is comfortably above anything the model can act on in one
// result (downstream layers clip to 4 KiB for logs and compact tool results
// in-loop) while capping worst-case memory per concurrent tool call.
const toolOutputCap int64 = 1 << 20

// captureCounters are process-wide telemetry for the bounded capture.
var captureCounters struct {
	produced atomic.Int64 // total bytes emitted by child processes
	retained atomic.Int64 // bytes actually kept in tool results
	caps     atomic.Int64 // number of truncated captures
}

// CaptureStats snapshots the bounded-capture telemetry.
type CaptureStats struct {
	BytesProduced int64 `json:"bytesProduced"`
	BytesRetained int64 `json:"bytesRetained"`
	Truncations   int64 `json:"truncations"`
}

func GetCaptureStats() CaptureStats {
	return CaptureStats{
		BytesProduced: captureCounters.produced.Load(),
		BytesRetained: captureCounters.retained.Load(),
		Truncations:   captureCounters.caps.Load(),
	}
}

// boundWriter is a concurrency-safe writer that counts everything written
// to it but retains at most cap bytes (the head of the stream). It is
// written concurrently by exec.Cmd's internal stdout and stderr copiers.
type boundWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	cap   int64
	total int64
}

func (w *boundWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.total += int64(len(p))
	if room := w.cap - int64(w.buf.Len()); room > 0 {
		if int64(len(p)) <= room {
			w.buf.Write(p)
		} else {
			w.buf.Write(p[:room])
		}
	}
	// Always report the full length consumed: dropped bytes are counted,
	// never blocked.
	return len(p), nil
}

// boundedCombinedOutput runs cmd with combined stdout+stderr streamed
// through a bounded buffer. The returned string holds the first
// toolOutputCap bytes plus, when output was cut, a marker stating the real
// total. The process runs to completion (or its context deadline) either
// way — no pipe deadlock, no unbounded allocation.
func boundedCombinedOutput(cmd *exec.Cmd, capBytes int64) (string, int64, error) {
	if capBytes <= 0 {
		capBytes = toolOutputCap
	}
	w := &boundWriter{cap: capBytes}
	cmd.Stdout = w
	cmd.Stderr = w
	err := cmd.Run()

	w.mu.Lock()
	head := w.buf.String()
	total := w.total
	w.mu.Unlock()

	captureCounters.produced.Add(total)
	captureCounters.retained.Add(int64(len(head)))

	out := head
	if total > int64(len(head)) {
		captureCounters.caps.Add(1)
		out += fmt.Sprintf(
			"\n[output truncated: showing first %s of %s total — narrow the query, page the output, or redirect it to a file and read it in windows]",
			humanize.Bytes(int64(len(head))), humanize.Bytes(total),
		)
	}
	return out, total, err
}
