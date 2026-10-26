//go:build !headless

// Runtime — the ONE UI-side presentation state holder. It mirrors nothing
// the core does not publish: sessions, streams and guards only. Every
// mutation boundary carries its contract (ported from the retired React
// store): generation-guarded list refreshes, zero-session send, cumulative
// stream folding, settlement reload of the authoritative transcript.
package fyneui

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Runtime is safe for concurrent use. UI updates are marshalled to the
// Fyne main loop by the views (fyne.Do); Runtime itself is plain Go.
type Runtime struct {
	mu       sync.Mutex
	client   *CoreClient
	mode     string // current conversation space: chat | agent
	sessions []Session
	activeID string

	listGuard  SessionListGuard
	ctxGen     uint64 // context refresh generation (stale context reads)
	acc        StreamAccumulator
	phase      RunPhaseMachine
	runActive  string // runId when a run is live in this process
	busy       bool
	stopCh     chan struct{}
	onSettled  func() // UI callback (marshalled via fyne.Do)
	onStream   func() // UI callback for stream updates
	onSessions func() // UI callback for list changes
	subMu      sync.Mutex
	subCancel  context.CancelFunc
}

// NewRuntime builds the runtime over a client.
func NewRuntime(c *CoreClient) *Runtime {
	return &Runtime{client: c, mode: ModeChat}
}

// SetCallbacks installs UI-notification hooks (invoked from Runtime's
// goroutines; the view wraps them in fyne.Do).
func (r *Runtime) SetCallbacks(onStream, onSettled, onSessions func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onStream, r.onSettled, r.onSessions = onStream, onSettled, onSessions
}

func (r *Runtime) notify(fn *func()) {
	if fn != nil {
		if f := *fn; f != nil {
			f()
		}
	}
}

// Mode returns the current conversation space.
func (r *Runtime) Mode() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mode
}

// SetMode switches conversation spaces. Contract: switching modes never
// merges or silently switches conversations — the active id is cleared to
// the mode's own space selection (first session of that mode).
func (r *Runtime) SetMode(m string) {
	r.mu.Lock()
	m = NormalizeMode(m)
	r.mode = m
	r.activeID = ""
	// Keep per-mode lists; the UI re-selects from FilterSessions.
	r.mu.Unlock()
}

// Sessions returns the session list filtered for the current mode.
func (r *Runtime) Sessions() []Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return FilterSessions(r.sessions, r.mode)
}

// AllSessions returns every known session (both modes).
func (r *Runtime) AllSessions() []Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Session, len(r.sessions))
	copy(out, r.sessions)
	return out
}

// ActiveID returns the active session id ("" when none).
func (r *Runtime) ActiveID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.activeID
}

// ActiveSession returns the active session snapshot.
func (r *Runtime) ActiveSession() (Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		if s.ID == r.activeID {
			return s, true
		}
	}
	return Session{}, false
}

// Busy reports whether a run is live.
func (r *Runtime) Busy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.busy
}

// currentModel reports the model of the active session ("" when none) —
// presentation truth only; the engine's model lives in the core.
func (r *Runtime) currentModel() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		if s.ID == r.activeID {
			return s.Model
		}
	}
	return ""
}

// Stream returns the live accumulator snapshot.
func (r *Runtime) Stream() StreamAccumulator {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.acc
}

// Phase returns the observed run phase.
func (r *Runtime) Phase() RunPhase {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.phase.Phase
}

// RefreshSessions lists sessions through the generation guard. A stale
// response (ticket no longer current) is discarded — it can never
// resurrect a deleted session or overwrite a newer list.
func (r *Runtime) RefreshSessions(ctx context.Context) error {
	r.mu.Lock()
	ticket := r.listGuard.Ticket()
	mode := r.mode
	r.mu.Unlock()

	got, err := r.client.Sessions(ctx, "")
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.listGuard.IsCurrent(ticket) {
		return nil // superseded — discard silently, the newer state wins
	}
	r.sessions = got
	SortSessionsByUpdated(r.sessions)
	_ = mode
	if r.onSessions != nil {
		go r.onSessions()
	}
	return nil
}

// SelectSession activates a session and reloads its authoritative
// transcript. The reload is generation-guarded against rapid switching.
func (r *Runtime) SelectSession(ctx context.Context, id string) (Session, error) {
	r.mu.Lock()
	ticket := r.ctxGen
	ticket++
	r.ctxGen = ticket
	r.activeID = id
	r.mu.Unlock()

	s, err := r.client.OpenSession(ctx, id)
	if err != nil {
		return Session{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctxGen != ticket || r.activeID != id {
		return Session{}, nil // stale reload — a newer selection wins
	}
	for i, cur := range r.sessions {
		if cur.ID == s.ID {
			r.sessions[i] = s
			break
		}
	}
	return s, nil
}

// CreateSession creates a session in the current mode and makes it active.
func (r *Runtime) CreateSession(ctx context.Context, name string) (Session, error) {
	s, err := r.client.CreateSession(ctx, r.Mode())
	if err != nil {
		return Session{}, err
	}
	r.mu.Lock()
	r.listGuard.Invalidate() // invalidate any in-flight list before mutation
	r.sessions = append(r.sessions, s)
	r.activeID = s.ID
	_ = name
	if r.onSessions != nil {
		go r.onSessions()
	}
	r.mu.Unlock()
	return s, nil
}

// DeleteSession removes a session. Contract: the deletion invalidates
// in-flight lists FIRST; if the deleted session was active, the active id
// clears (zero-session state is usable — send creates a new session).
func (r *Runtime) DeleteSession(ctx context.Context, id string) error {
	r.mu.Lock()
	r.listGuard.Invalidate()
	r.mu.Unlock()

	if err := r.client.DeleteSession(ctx, id); err != nil {
		return err
	}

	r.mu.Lock()
	kept := r.sessions[:0]
	for _, s := range r.sessions {
		if s.ID != id {
			kept = append(kept, s)
		}
	}
	r.sessions = kept
	if r.activeID == id {
		r.activeID = ""
	}
	if r.onSessions != nil {
		go r.onSessions()
	}
	r.mu.Unlock()
	return nil
}

// RenameSession renames via the core PUT path.
func (r *Runtime) RenameSession(ctx context.Context, id, title string) error {
	s, err := r.client.RenameSession(ctx, id, title)
	if err != nil {
		return err
	}
	r.mu.Lock()
	for i, cur := range r.sessions {
		if cur.ID == s.ID {
			r.sessions[i] = s
			break
		}
	}
	if r.onSessions != nil {
		go r.onSessions()
	}
	r.mu.Unlock()
	return nil
}

// Send posts a user turn. ZERO-SESSION CONTRACT: with no active session it
// creates one in the current mode and completes the SAME send; a creation
// failure returns a typed error instead of wedging the composer. The run
// response returns immediately; content arrives over the activity stream.
func (r *Runtime) Send(ctx context.Context, text string, netSearch bool, thinking string) (RunResponse, error) {
	r.mu.Lock()
	id := r.activeID
	mode := r.mode
	r.mu.Unlock()

	if id == "" {
		s, err := r.CreateSession(ctx, "")
		if err != nil {
			return RunResponse{}, fmt.Errorf("no active session: %w", err)
		}
		id = s.ID
		_ = mode
	}

	r.mu.Lock()
	r.busy = true
	r.acc.Reset()
	r.phase.Reset()
	r.phase.Phase = PhasePreparing
	r.mu.Unlock()
	if r.onStream != nil {
		go r.onStream()
	}

	resp, err := r.client.Run(ctx, RunRequest{
		SessionID: id,
		Message:   text,
		NetSearch: netSearch,
		Thinking:  NormalizeThinking(thinking),
	})
	if err != nil {
		r.mu.Lock()
		r.busy = false
		r.phase.Phase = PhaseError
		r.mu.Unlock()
		if r.onStream != nil {
			go r.onStream()
		}
		return RunResponse{}, err
	}
	r.mu.Lock()
	r.runActive = resp.RunID
	r.mu.Unlock()
	return resp, nil
}

// Regenerate re-runs the last user turn.
func (r *Runtime) Regenerate(ctx context.Context) (RunResponse, error) {
	r.mu.Lock()
	id := r.activeID
	r.mu.Unlock()
	if id == "" {
		return RunResponse{}, fmt.Errorf("no active session")
	}
	r.mu.Lock()
	r.busy = true
	r.acc.Reset()
	r.phase.Reset()
	r.mu.Unlock()
	resp, err := r.client.Run(ctx, RunRequest{SessionID: id, Regenerate: true})
	if err != nil {
		r.mu.Lock()
		r.busy = false
		r.mu.Unlock()
		return resp, err
	}
	r.mu.Lock()
	r.runActive = resp.RunID
	r.mu.Unlock()
	return resp, nil
}

// Stop aborts the active run. Partial output survives (the accumulator
// keeps the last cumulative captions; the server persists what it has and
// the settlement reload restores the authoritative transcript).
func (r *Runtime) Stop(ctx context.Context) error {
	r.mu.Lock()
	id := r.activeID
	r.mu.Unlock()
	if id == "" {
		return nil
	}
	return r.client.Abort(ctx, id)
}

// Pause pauses the live run.
func (r *Runtime) Pause(ctx context.Context) (int, error) {
	r.mu.Lock()
	id, runID := r.activeID, r.runActive
	r.mu.Unlock()
	rev, err := r.client.PauseRun(ctx, id, runID)
	if err == nil {
		r.mu.Lock()
		r.phase.Pause()
		r.mu.Unlock()
		if r.onStream != nil {
			go r.onStream()
		}
	}
	return rev, err
}

// Resume resumes a paused run.
func (r *Runtime) Resume(ctx context.Context) error {
	r.mu.Lock()
	id, runID, phase := r.activeID, r.runActive, r.phase.Phase
	r.mu.Unlock()
	if phase != PhasePaused {
		return fmt.Errorf("no paused run")
	}
	if err := r.client.ResumeRun(ctx, id, runID, 0); err != nil {
		return err
	}
	return nil
}

// ResumeAt resumes with an explicit revision (CAS).
func (r *Runtime) ResumeAt(ctx context.Context, revision int) error {
	r.mu.Lock()
	id, runID := r.activeID, r.runActive
	r.mu.Unlock()
	err := r.client.ResumeRun(ctx, id, runID, revision)
	if err == nil {
		r.mu.Lock()
		r.phase.Phase = PhaseStreaming
		r.mu.Unlock()
		if r.onStream != nil {
			go r.onStream()
		}
	}
	return err
}

// AttachActivity subscribes to /ws/activity for the active session and
// pumps events into the accumulator until ctx is cancelled. Events whose
// seq belongs to a superseded attach are ignored by the server replay
// contract; here we fold what arrives. Never blocks generation.
func (r *Runtime) AttachActivity(ctx context.Context, sessionID string) error {
	r.subMu.Lock()
	if r.subCancel != nil {
		r.subCancel()
	}
	sctx, cancel := context.WithCancel(ctx)
	r.subCancel = cancel
	r.subMu.Unlock()

	return r.pumpActivity(sctx, sessionID)
}

// DetachActivity cancels the current subscription (navigation/shutdown).
func (r *Runtime) DetachActivity() {
	r.subMu.Lock()
	if r.subCancel != nil {
		r.subCancel()
		r.subCancel = nil
	}
	r.subMu.Unlock()
}

// pumpActivity dials the WS, waits for the attached frame, folds events.
func (r *Runtime) pumpActivity(ctx context.Context, sessionID string) error {
	conn, _, err := r.client.wsdial.DialContext(ctx, "ws://core/ws/activity?sessionId="+sessionID, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	done := make(chan struct{})
	go func() { // abort on ctx cancel
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var ev StreamEvent
		if err := json.Unmarshal(payload, &ev); err != nil {
			continue // malformed frame: skip, never fabricate
		}
		terminal := false
		r.mu.Lock()
		r.acc.Fold(ev)
		r.phase.Observe(canonicalKind(ev.Type))
		if k := canonicalKind(ev.Type); k.Terminal() {
			terminal = true
			r.busy = false
		}
		onStream := r.onStream
		onSettled := r.onSettled
		r.mu.Unlock()

		if onStream != nil {
			go onStream()
		}
		if terminal {
			// SETTLEMENT: reload the authoritative transcript — the live
			// bubble is replaced by the persisted server message.
			outcome := "done"
			switch canonicalKind(ev.Type) {
			case StreamAborted:
				outcome = "aborted"
			case StreamError:
				outcome = "error"
			}
			if s, err := r.client.OpenSession(context.Background(), sessionID); err == nil {
				r.mu.Lock()
				for i, cur := range r.sessions {
					if cur.ID == s.ID {
						r.sessions[i] = s
						break
					}
				}
				r.mu.Unlock()
			}
			if onSettled != nil {
				go onSettled()
				_ = outcome
			}
		}
	}
}

// Shutdown cancels subscriptions and detaches callbacks (window close).
func (r *Runtime) Shutdown() {
	r.DetachActivity()
	r.mu.Lock()
	r.onStream, r.onSettled, r.onSessions = nil, nil, nil
	r.mu.Unlock()
}
