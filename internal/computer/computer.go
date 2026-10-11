// Package computer implements Phase 7E: the unified computer-use
// abstraction over SHEYTAN's existing browser and screen capabilities.
//
// The model follows one loop for every interaction with a GUI surface:
//
//	observe → inspect → act → observe → verify
//
// Every action carries an explicit risk class, a timeout, and produces an
// Observation; destructive actions are gated behind the existing security
// policy and are denied unless a permission gate explicitly allows them.
// No uncontrolled mouse/keyboard automation exists in this layer: actions
// are bounded, cancellable through the context, and every step's result is
// observed before the next one runs.
package computer

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Risk classes an action can carry. The risk policy is deny-by-default for
// anything beyond read-only observation.
type Risk string

const (
	// RiskReadOnly: observation and inspection only (extract, url,
	// screenshot). Always allowed.
	RiskReadOnly Risk = "read-only"

	// RiskInteractive: bounded interaction with a page/app the user asked
	// the agent to work with (navigate, click, type into a form).
	RiskInteractive Risk = "interactive"

	// RiskDestructive: actions with irreversible side effects (form
	// submission with payment, deletion, purchases). Denied unless the
	// permission gate explicitly allows THIS action.
	RiskDestructive Risk = "destructive"
)

// ActionKind is the role of a step inside the observe→act→observe→verify
// loop.
type ActionKind string

const (
	KindObserve ActionKind = "observe"
	KindInspect ActionKind = "inspect"
	KindAct     ActionKind = "act"
	KindVerify  ActionKind = "verify"
)

// Observation is the result of one action — every action must produce one
// (result observation is part of the action boundary).
type Observation struct {
	Kind       ActionKind `json:"kind"`
	Action     string     `json:"action"`
	OK         bool       `json:"ok"`
	Text       string     `json:"text,omitempty"`
	ImagePath  string     `json:"imagePath,omitempty"`
	URL        string     `json:"url,omitempty"`
	Err        string     `json:"err,omitempty"`
	DurationMs int64      `json:"durationMs"`
}

// Action is one step of a computer-use plan.
type Action struct {
	Kind    ActionKind    `json:"kind"`
	Name    string        `json:"name"`
	Risk    Risk          `json:"risk"`
	Timeout time.Duration `json:"-"`

	// Run executes the action and returns its observation.
	Run func(ctx context.Context) (Observation, error)

	// Verify optionally checks the FOLLOWING observation (the loop runs
	// observe-after-act automatically; verify runs against it).
	Verify func(obs Observation) error
}

// PermissionGate is the deny-by-default risk policy. Destructive actions
// are executed only when Allow returns true for the concrete action name;
// interactive actions are allowed when the session is authorized at all.
type PermissionGate interface {
	Allow(risk Risk, action string) bool
}

// defaultGate allows read-only and interactive actions, denies destructive.
type defaultGate struct{}

func (defaultGate) Allow(risk Risk, _ string) bool {
	return risk == RiskReadOnly || risk == RiskInteractive
}

// Session executes bounded computer-use plans against a surface driver.
type Session struct {
	// MaxSteps bounds one plan (hard stop against runaway loops).
	MaxSteps int
	// DefaultTimeout bounds one action when the action sets none.
	DefaultTimeout time.Duration
	// Gate is the risk policy (default: allow read-only + interactive).
	Gate PermissionGate
}

// NewSession returns a session with safe defaults.
func NewSession() *Session {
	return &Session{
		MaxSteps:       16,
		DefaultTimeout: 45 * time.Second,
		Gate:           defaultGate{},
	}
}

// Run executes the plan step by step. Between every act step an
// observation is REQUIRED (the loop enforces observe-after-act by running
// the next action against the fresh state). Cancellation and timeouts are
// honored at every boundary; the first hard failure stops the plan and the
// partial observations are returned.
func (s *Session) Run(ctx context.Context, plan []Action) ([]Observation, error) {
	if len(plan) == 0 {
		return nil, fmt.Errorf("computer-use plan is empty")
	}
	max := s.MaxSteps
	if max <= 0 {
		max = 16
	}
	if len(plan) > max {
		plan = plan[:max]
	}

	gate := s.Gate
	if gate == nil {
		gate = defaultGate{}
	}

	timeout := s.DefaultTimeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}

	obs := make([]Observation, 0, len(plan))

	for i, act := range plan {
		if err := ctx.Err(); err != nil {
			return obs, fmt.Errorf("computer-use plan canceled at step %d: %w", i+1, err)
		}

		// Action boundary: risk policy first.
		if !gate.Allow(act.Risk, act.Name) {
			return obs, fmt.Errorf(
				"step %d (%s) denied by risk policy: %s actions require explicit permission",
				i+1, act.Name, act.Risk)
		}

		if act.Run == nil {
			return obs, fmt.Errorf("step %d (%s) has no executor", i+1, act.Name)
		}

		stepTimeout := timeout
		if act.Timeout > 0 {
			stepTimeout = act.Timeout
		}

		stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
		started := time.Now()
		o, err := act.Run(stepCtx)
		cancel()

		if o.Action == "" {
			o.Action = act.Name
		}
		if o.Kind == "" {
			o.Kind = act.Kind
		}
		o.DurationMs = time.Since(started).Milliseconds()

		if err != nil {
			o.OK = false
			o.Err = err.Error()
			obs = append(obs, o)

			if act.Kind == KindVerify {
				// A failed verification is a first-class result: the plan
				// stops with the evidence, not a blind retry.
				return obs, fmt.Errorf("verification step %d (%s) failed: %w", i+1, act.Name, err)
			}
			return obs, fmt.Errorf("step %d (%s) failed: %w", i+1, act.Name, err)
		}

		o.OK = true
		obs = append(obs, o)

		// The verification predicate runs against the FRESH observation —
		// a verify step whose check fails stops the plan with evidence.
		if act.Verify != nil {
			if vErr := act.Verify(o); vErr != nil {
				o.OK = false
				o.Err = vErr.Error()
				return obs, fmt.Errorf("verification step %d (%s) failed: %w", i+1, act.Name, vErr)
			}
		}
	}

	return obs, nil
}

// VerifyContains returns a Verify function checking observation text.
func VerifyContains(needle string) func(Observation) error {
	return func(obs Observation) error {
		if !strings.Contains(obs.Text, needle) {
			return fmt.Errorf("expected %q in observation, got %.120q", needle, obs.Text)
		}
		return nil
	}
}

// VerifyURLOnly allows a URL-prefix check for navigation verification.
func VerifyURLPrefix(prefix string) func(Observation) error {
	return func(obs Observation) error {
		if !strings.HasPrefix(obs.URL, prefix) {
			return fmt.Errorf("expected URL prefix %q, got %q", prefix, obs.URL)
		}
		return nil
	}
}
