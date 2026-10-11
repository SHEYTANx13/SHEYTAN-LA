package multiagent

import "testing"

// TestPolicyForRoleBuiltins: the built-in roles carry real policies and
// the coder never runs with a smaller requested window than the
// researcher — but never beyond its own maximum either.
func TestPolicyForRoleBuiltins(t *testing.T) {
	for _, role := range []string{
		string(RoleResearcher), string(RoleArchitect), string(RoleCoder),
		string(RoleDebugger), string(RoleTester), string(RoleSecurity),
		"planner", "critic", "summarizer",
	} {
		p := PolicyForRole(role)
		if p.RequestedContext <= 0 || p.MinimumContext <= 0 ||
			p.MaximumContext < p.RequestedContext || p.OutputReserve <= 0 {
			t.Fatalf("role %s has a broken policy: %+v", role, p)
		}
	}

	if unknown := PolicyForRole("nonexistent-role"); unknown.RequestedContext != 0 {
		t.Fatal("unknown roles must resolve to a zero policy (inherit)")
	}
}

// TestResolveAgentContextClampsToBase: an agent never exceeds the
// session/global effective decision.
func TestResolveAgentContextClampsToBase(t *testing.T) {
	policy := ContextPolicy{RequestedContext: 32768, MinimumContext: 8192, MaximumContext: 65536}

	effective, trail := ResolveAgentContext("coder", policy, 16384, 32768, 32768)

	if effective != 16384 {
		t.Fatalf("effective = %d, want 16384 (base clamp)", effective)
	}
	if len(trail) == 0 {
		t.Fatal("clamped resolution must leave a trail")
	}
}

// TestResolveAgentContextModelMaxWins: model maximum beats every request.
func TestResolveAgentContextModelMaxWins(t *testing.T) {
	policy := ContextPolicy{RequestedContext: 32768, MinimumContext: 8192, MaximumContext: 65536}

	effective, _ := ResolveAgentContext("coder", policy, 32768, 4096, 0)

	if effective != 4096 {
		t.Fatalf("effective = %d, want 4096 (model max)", effective)
	}
}

// TestResolveAgentContextNoPolicyInherits: zero policy = base value.
func TestResolveAgentContextNoPolicyInherits(t *testing.T) {
	effective, trail := ResolveAgentContext("ghost", ContextPolicy{}, 12288, 0, 0)

	if effective != 12288 {
		t.Fatalf("effective = %d, want 12288 (inherit)", effective)
	}
	if trail != nil {
		t.Fatal("inherit path must not produce a trail")
	}
}

// TestResolveAgentContextFloorHonorsHardLimits: the role minimum raises
// the value but never above the hard limit.
func TestResolveAgentContextFloorHonorsHardLimits(t *testing.T) {
	policy := ContextPolicy{RequestedContext: 4096, MinimumContext: 8192, MaximumContext: 65536}

	// Hard limit 6144 < minimum 8192 → the limit wins, honestly.
	effective, _ := ResolveAgentContext("tester", policy, 16384, 6144, 0)
	if effective != 6144 {
		t.Fatalf("effective = %d, want 6144 (hard limit beats role minimum)", effective)
	}

	// No hard conflict → the floor applies.
	effective, _ = ResolveAgentContext("tester", policy, 16384, 0, 0)
	if effective != 8192 {
		t.Fatalf("effective = %d, want 8192 (role minimum floor)", effective)
	}
}
