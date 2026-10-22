//go:build !headless

// ui_stdio.go — the UI child's stdout/stderr flow through this adapter
// into the process log manager, tagged "ui". v1.10.14 (run #36 class):
// the old wiring pointed the child's streams at the core's os.Stdout/
// os.Stderr — correct on a console, completely lost under the
// -H=windowsgui production build, where the Qt host's warnings (QML
// errors, plugin failures) vanished with the console. Every line the UI
// writes now reaches app.log — stdout at Info, stderr at Warn — the
// warning catcher the operator's log was missing. errors-only economy
// (D1 class): one line per UI line, no dumps, no echo of the core's own
// channels.

package desktop

import (
	"bytes"
	"strings"
	"sync"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// maxPendingLine bounds the pending partial line: a chatty UI stream
// that never newline-terminates must not grow this buffer without limit.
// The over-long remainder is emitted once (level-accurate, content
// preserved) instead of being held forever.
const maxPendingLine = 8 * 1024

// uiLogWriter adapts ONE child-process output stream into the log
// manager. Line-buffered: a Write flushes every complete line it ends
// with and keeps the trailing partial line for the next Write; Flush
// emits whatever remains when the child exits.
type uiLogWriter struct {
	mu   sync.Mutex
	pend []byte
	warn bool // stderr stream → Warn level; stdout → Info
}

func newUILogWriter(warn bool) *uiLogWriter {
	return &uiLogWriter{warn: warn}
}

func (w *uiLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pend = append(w.pend, p...)
	for {
		i := bytes.IndexByte(w.pend, '\n')
		if i < 0 {
			break
		}
		w.emit(string(w.pend[:i]))
		w.pend = w.pend[i+1:]
	}
	if len(w.pend) > maxPendingLine {
		w.emit(string(w.pend))
		w.pend = w.pend[:0]
	}
	return len(p), nil
}

// Flush emits a final unterminated partial line (the child's last output
// without a trailing newline). Safe to call twice; a no-op when nothing
// is pending.
func (w *uiLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pend) > 0 {
		w.emit(string(w.pend))
		w.pend = w.pend[:0]
	}
}

func (w *uiLogWriter) emit(line string) {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	if w.warn {
		logging.Default().Warn("ui", "%s", line)
	} else {
		logging.Default().Info("ui", "%s", line)
	}
}
