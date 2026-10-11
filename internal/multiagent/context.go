// Per-agent context policies (1.1.6 §5).
//
// Every specialist/role carries an optional context policy so the
// multi-agent loop stops giving every agent the largest available
// window. A policy is a REQUEST plus hard bounds:
//
//	requestedContext — the window this role wants
//	minimumContext   — below this the role's prompts genuinely break
//	maximumContext   — this role never needs more than this
//	outputReserve    — tokens held back for the role's reply
//
// Resolution priority: agent policy → session/global effective value →
// model capability → engine window → safe effective value. The resolved
// value is LOGGED (one line per consult) so operators can see the
// decision instead of guessing.
package multiagent

import (
	"fmt"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// ContextPolicy is one role's context request plus its hard bounds.
type ContextPolicy struct {
	RequestedContext int `json:"requestedContext"`
	MinimumContext   int `json:"minimumContext"`
	MaximumContext   int `json:"maximumContext"`
	OutputReserve    int `json:"outputReserve"`
}

// roleContextPolicies are the built-in per-role policies. Internal roles
// (planner/critic/summarizer) get policies too — they are agents like the
// specialists, only less visible.
var roleContextPolicies = map[string]ContextPolicy{
	// Specialists (specialists.go).
	string(RoleResearcher): {RequestedContext: 8192, MinimumContext: 2048, MaximumContext: 16384, OutputReserve: 512},
	string(RoleArchitect):  {RequestedContext: 16384, MinimumContext: 4096, MaximumContext: 32768, OutputReserve: 768},
	string(RoleCoder):      {RequestedContext: 32768, MinimumContext: 8192, MaximumContext: 65536, OutputReserve: 1024},
	string(RoleDebugger):   {RequestedContext: 16384, MinimumContext: 4096, MaximumContext: 32768, OutputReserve: 768},
	string(RoleTester):     {RequestedContext: 8192, MinimumContext: 4096, MaximumContext: 16384, OutputReserve: 512},
	string(RoleSecurity):   {RequestedContext: 12288, MinimumContext: 4096, MaximumContext: 24576, OutputReserve: 512},
	// Internal loop roles.
	"planner":    {RequestedContext: 16384, MinimumContext: 4096, MaximumContext: 32768, OutputReserve: 512},
	"critic":     {RequestedContext: 12288, MinimumContext: 2048, MaximumContext: 16384, OutputReserve: 256},
	"summarizer": {RequestedContext: 8192, MinimumContext: 2048, MaximumContext: 16384, OutputReserve: 1024},
}

// PolicyForRole returns the policy for a role (zero policy when unknown —
// the caller then falls back to the global effective context).
func PolicyForRole(role string) ContextPolicy {
	if p, ok := roleContextPolicies[role]; ok {
		return p
	}
	return ContextPolicy{}
}

// ResolveAgentContext resolves one role's policy against the real limits.
//
// Semantics:
//   - no policy  → the base decision passes through untouched;
//   - the role request starts at requestedContext, clamped into
//     [0, maximumContext];
//   - it never exceeds the base (session/global) decision, the model
//     maximum or the engine window — whichever is smallest (an agent has
//     no right to a window the machine cannot serve);
//   - if that lands BELOW the role's minimumContext, the value is raised
//     toward the minimum but never past the hard limits above; when even
//     the minimum is impossible, the hard-limited value stands and the
//     trail says so.
//
// base is the already-resolved effective context for the current
// session/global decision; modelMax/engineMax are 0 when unknown.
func ResolveAgentContext(role string, policy ContextPolicy, base, modelMax, engineMax int) (effective int, trail []string) {
	if policy.RequestedContext <= 0 {
		// No policy: inherit the base decision unchanged.
		return base, nil
	}

	requested := policy.RequestedContext
	trail = append(trail, fmt.Sprintf("role %s policy requests %d", role, requested))

	if policy.MaximumContext > 0 && requested > policy.MaximumContext {
		requested = policy.MaximumContext
		trail = append(trail, fmt.Sprintf("clamped to role maximum %d", policy.MaximumContext))
	}

	// Hard ceilings: the base (session/global) decision is itself
	// model/engine-clamped, so all three bound the agent request.
	hardLimit := 0
	if base > 0 && requested > base {
		requested = base
		trail = append(trail, fmt.Sprintf("clamped to session/global effective %d", base))
	}
	if modelMax > 0 && requested > modelMax {
		requested = modelMax
		trail = append(trail, fmt.Sprintf("clamped to model maximum %d", modelMax))
	}
	if engineMax > 0 && requested > engineMax {
		requested = engineMax
		trail = append(trail, fmt.Sprintf("clamped to engine window %d", engineMax))
	}
	if modelMax > 0 {
		hardLimit = modelMax
	}
	if engineMax > 0 && (hardLimit == 0 || engineMax < hardLimit) {
		hardLimit = engineMax
	}

	if policy.MinimumContext > 0 && requested < policy.MinimumContext {
		floor := policy.MinimumContext
		if hardLimit > 0 && floor > hardLimit {
			floor = hardLimit
			trail = append(trail, fmt.Sprintf(
				"role minimum %d cannot be honored — hard limit %d wins",
				policy.MinimumContext, hardLimit))
		}
		if floor > requested {
			requested = floor
			trail = append(trail, fmt.Sprintf("floored at role minimum %d", floor))
		}
	}
	if requested < 1024 {
		requested = 1024
		trail = append(trail, "floored at the absolute minimum 1024")
	}

	return requested, trail
}

// logAgentContext emits the one-line resolution record for a consult.
func logAgentContext(role string, requested, effective, base, modelMax, engineMax int) {
	logging.Default().Info(
		"multiagent",
		"agent context: role=%s requested=%d effective=%d base=%d modelMax=%d engineMax=%d",
		role, requested, effective, base, modelMax, engineMax,
	)
}

// resolveRoleContext resolves one role's context for an imminent call:
// base is the session/global effective window, and the model/engine
// limits come from ModelLimitsFn when wired. The resolved value is
// logged and the request should carry it as NumCtx (local endpoints).
func (m *MultiAgent) resolveRoleContext(role string, policy ContextPolicy, base int) (effective int) {
	modelMax, engineMax := 0, 0
	if m.ModelLimitsFn != nil {
		modelMax, engineMax = m.ModelLimitsFn()
	}

	effective, _ = ResolveAgentContext(role, policy, base, modelMax, engineMax)
	logAgentContext(role, policy.RequestedContext, effective, base, modelMax, engineMax)
	return effective
}
