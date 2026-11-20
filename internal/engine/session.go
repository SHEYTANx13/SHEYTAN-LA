package engine

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
)

// Session owns REQUEST-LOCAL state (work order section 5.1): the token
// history, the sampling configuration, the stream state and
// cancellation. A Session never owns model weights — the Model does —
// and desktop/CLI/server code all drive the SAME Session API (no second
// inference implementation anywhere).
//
// Concurrency: a Session serializes its own Generate calls; multiple
// Sessions over one Model are legal and bounded by the Engine's
// admission queue.
type Session struct {
	ID   string
	mod  *Model
	cfg  SamplingConfig
	sink EventSink

	mu        sync.Mutex
	history   []int32       // full token history (prompt + generated)
	generated []int32       // the generated suffix only
	closed    bool          // ErrSessionClosed once Close ran
	stop      StopCondition // optional stop condition
	rng       *rand.Rand    // deterministic per-session stream (seeded)
}

var sessionCounter atomic.Int64

// NewSession constructs a session over a loaded model. The sampling
// seed is explicit: the same seed + logits reproduce the same tokens
// (the reproducibility tests pin this).
func NewSession(m *Model, cfg SamplingConfig, sink EventSink) (*Session, error) {
	if m == nil {
		return nil, fmt.Errorf("%w: nil model", ErrEngineClosed)
	}
	if m.owner != nil && m.owner.isClosed() {
		return nil, ErrEngineClosed
	}
	if cfg.MaxNewTokens <= 0 {
		return nil, fmt.Errorf("%w: MaxNewTokens %d must be positive (bounded requests)", ErrBusy, cfg.MaxNewTokens)
	}
	if sink == nil {
		sink = func(Event) {}
	}
	return &Session{
		ID:   fmt.Sprintf("sess-%d", sessionCounter.Add(1)),
		mod:  m,
		cfg:  cfg,
		sink: sink,
		rng:  rand.New(rand.NewSource(cfg.Seed)),
	}, nil
}

// History returns the full token history (prompt + generated so far).
func (s *Session) History() []int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int32, len(s.history))
	copy(out, s.history)
	return out
}

// GeneratedTokens returns the generated suffix (a snapshot).
func (s *Session) GeneratedTokens() []int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int32, len(s.generated))
	copy(out, s.generated)
	return out
}

// Reset clears the request-local history (the honest, correct KV
// rollback of this slice: state is dropped wholesale, never reused
// across unrelated contexts — prefix-cache identity discipline, work
// order section 5.7).
func (s *Session) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = nil
	s.generated = nil
}

// Close releases the session state. Idempotent; later Generate calls
// answer ErrSessionClosed.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.history = nil
	s.generated = nil
	s.sink(Event{Kind: EventSessionClosed, SessionID: s.ID})
	return nil
}

// StreamToken is one streamed generation callback. Done marks the final
// call (after which the accumulated text is complete).
type StreamToken func(textSoFar string, token int32, done bool)

// Generate appends the prompt to the session history and generates up
// to cfg.MaxNewTokens tokens (or until a stop condition fires / the
// context is cancelled), streaming through cb when non-nil.
//
// The engine admission queue bounds concurrent Generate calls across
// the Engine (ErrBusy on overflow — bounded, observable backpressure).
// Every request cancels cleanly: ctx.Done() is checked before each
// forward pass.
func (s *Session) Generate(ctx context.Context, prompt string, cb StreamToken) (string, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", ErrSessionClosed
	}
	if s.mod.owner != nil && s.mod.owner.isClosed() {
		s.mu.Unlock()
		return "", ErrEngineClosed
	}
	histLen := len(s.history)
	s.mu.Unlock()

	tokens, err := s.mod.Tokenizer.Encode(prompt)
	if err != nil {
		return "", err
	}
	if len(tokens)+histLen > s.mod.Config.ContextLength {
		return "", fmt.Errorf("%w: prompt %d + history %d exceeds the context length %d",
			ErrContextLength, len(tokens), histLen, s.mod.Config.ContextLength)
	}

	// Bounded admission across the engine (admit BEFORE mutating the
	// session history so a rejected request leaves the session
	// untouched).
	if s.mod.owner != nil {
		if err := s.mod.owner.acquire(ctx); err != nil {
			return "", fmt.Errorf("%w: %v", ErrBusy, err)
		}
		defer s.mod.owner.release()
	}

	s.mu.Lock()
	s.history = append(s.history, tokens...)
	s.mu.Unlock()

	start := newEvent(EventGenerateStart)
	start.SessionID = s.ID
	start.Detail = s.mod.Provenance
	s.sink(start)

	// Forward pass over the FULL prefix (the fixture's bounded,
	// deterministic KV answer: recompute; never a fabricated cache hit).
	for step := 0; step < s.cfg.MaxNewTokens; step++ {
		if err := ctx.Err(); err != nil {
			done := newEvent(EventGenerateDone)
			done.SessionID = s.ID
			done.Detail = "cancelled"
			s.sink(done)
			return s.generatedText(), fmt.Errorf("%w: %v", ErrEngineClosed, err)
		}

		prefix := s.History()

		arch, err := s.architecture()
		if err != nil {
			return s.generatedText(), err
		}
		graph, tensors, err := arch.BuildGraph(s.mod, prefix)
		if err != nil {
			return s.generatedText(), err
		}
		plan, err := s.mod.Backend.Compile(graph)
		if err != nil {
			return s.generatedText(), fmt.Errorf("graph compile: %w", err)
		}
		if err := s.mod.Backend.Submit(plan, tensors); err != nil {
			return s.generatedText(), fmt.Errorf("graph execution: %w", err)
		}
		if err := s.mod.Backend.Synchronize(); err != nil {
			return s.generatedText(), err
		}

		// The LAST row of logits is the next-token distribution.
		logitsT := tensors["logits"]
		lf, err := logitsT.Floats()
		if err != nil {
			return s.generatedText(), err
		}
		lastRow := lf[(len(prefix)-1)*s.mod.Config.VocabSize : len(prefix)*s.mod.Config.VocabSize]

		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return "", ErrSessionClosed
		}
		id, serr := SampleToken(lastRow, s.cfg, s.rng)
		if serr != nil {
			s.mu.Unlock()
			return s.generatedTextLocked(), serr
		}
		s.history = append(s.history, id)
		s.generated = append(s.generated, id)
		s.mu.Unlock()

		ev := newEvent(EventGenerateToken)
		ev.SessionID = s.ID
		ev.Step = step + 1
		ev.TokenID = id
		s.sink(ev)

		if cb != nil {
			cb(s.generatedText(), id, false)
		}

		s.mu.Lock()
		stop := s.stop
		s.mu.Unlock()
		if stop != nil && stop(id) {
			break
		}
	}

	done := newEvent(EventGenerateDone)
	done.SessionID = s.ID
	done.Step = len(s.GeneratedTokens())
	s.sink(done)

	text := s.generatedText()
	if cb != nil {
		cb(text, 0, true)
	}
	return text, nil
}

// SetStopCondition installs a stop condition for subsequent Generate
// calls (nil clears it).
func (s *Session) SetStopCondition(sc StopCondition) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stop = sc
}

// architecture resolves the model's architecture adapter.
func (s *Session) architecture() (Architecture, error) {
	if s.mod.owner == nil {
		return TinyFixtureArchitecture{}, nil
	}
	return s.mod.owner.registry.Lookup(s.mod.Config.Architecture)
}

// generatedText decodes the generated suffix.
func (s *Session) generatedText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generatedTextLocked()
}

func (s *Session) generatedTextLocked() string {
	text, err := s.mod.Tokenizer.Decode(s.generated)
	if err != nil {
		return fmt.Sprintf("<decode error: %v>", err)
	}
	return text
}
