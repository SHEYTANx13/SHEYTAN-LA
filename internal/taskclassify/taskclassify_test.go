package taskclassify

import (
	"strings"
	"testing"
)

func TestClassifySimpleChat(t *testing.T) {
	p := Classify("hi", ClassifyOptions{})
	if p.Kind != KindChat {
		t.Fatalf("hi: kind = %s, want chat", p.Kind)
	}

	if p.Complexity > 21 {
		t.Fatalf("hi: complexity %d unexpectedly high", p.Complexity)
	}

	if p.Signals.NeedsFiles || p.Signals.NeedsResearch || p.Signals.NeedsVision {
		t.Fatalf("hi: no capability signals expected, got %+v", p.Signals)
	}
}

func TestClassifyQuestion(t *testing.T) {
	p := Classify("What is the capital of France?", ClassifyOptions{})
	if p.Kind != KindQuestion {
		t.Fatalf("question: kind = %s, want question", p.Kind)
	}
}

func TestClassifyCoding(t *testing.T) {
	p := Classify("fix the bug in main.go — the build fails with a nil pointer", ClassifyOptions{})
	if p.Kind != KindCoding {
		t.Fatalf("coding: kind = %s, want coding", p.Kind)
	}

	if !p.Signals.NeedsFiles {
		t.Fatalf("coding: NeedsFiles expected")
	}
}

func TestClassifyResearch(t *testing.T) {
	p := Classify("search the web for the latest news about llama.cpp releases and compare sources", ClassifyOptions{})
	if p.Kind != KindResearch {
		t.Fatalf("research: kind = %s, want research", p.Kind)
	}

	if !p.Signals.NeedsResearch {
		t.Fatalf("research: NeedsResearch expected")
	}
}

func TestClassifyVisionFromImages(t *testing.T) {
	p := Classify("what do you think?", ClassifyOptions{HasImages: true})
	if p.Kind != KindVision {
		t.Fatalf("vision: kind = %s, want vision", p.Kind)
	}

	if !p.Signals.NeedsVision {
		t.Fatalf("vision: NeedsVision expected")
	}
}

func TestClassifyArchitecture(t *testing.T) {
	p := Classify("analyze the codebase and propose an architecture refactor plan for this project", ClassifyOptions{})
	if p.Kind != KindArchitecture {
		t.Fatalf("architecture: kind = %s, want architecture", p.Kind)
	}
}

func TestClassifyDepthControl(t *testing.T) {
	base := Classify("explain quantum tunneling carefully", ClassifyOptions{})
	fast := Classify("explain quantum tunneling carefully", ClassifyOptions{UserDepth: "fast"})
	deep := Classify("explain quantum tunneling carefully", ClassifyOptions{UserDepth: "thinking"})

	if fast.Complexity >= base.Complexity {
		t.Fatalf("fast control should lower complexity: fast=%d base=%d", fast.Complexity, base.Complexity)
	}

	// v1.2.6: "thinking" no longer inflates complexity — reasoning depth is
	// NOT context size. The v1.2.5 +20 complexity bump (plus the forced
	// STANDARD tier floor) is what turned a one-line question into a
	// ~13K-token prompt.
	if deep.Complexity != base.Complexity {
		t.Fatalf("thinking control must not change complexity: deep=%d base=%d", deep.Complexity, base.Complexity)
	}
}

func TestClassifyComplexityClamped(t *testing.T) {
	p := Classify("design the architecture, fix the bug, build, test, verify, research the web, analyze the codebase, migrate the repository, plan a roadmap, review everything, deep dive step by step, run the command, debug the crash, refactor the function, implement the method, validate coverage, assert the traceback, compile the stack trace warning, npm install docker cargo make script, search latest news look up find online, remember my preference, chart plot dataset statistics csv", ClassifyOptions{AttachmentCount: 5})
	if p.Complexity != 100 {
		t.Fatalf("complexity = %d, want clamped 100", p.Complexity)
	}
}

func TestSelectTierSimpleChatIsFast(t *testing.T) {
	p := Classify("hi", ClassifyOptions{})
	tier := SelectTier(p, Resources{EffectiveContext: 8192, OutputReserve: 1024})
	if tier != TierFast {
		t.Fatalf("simple chat tier = %s, want FAST", tier)
	}
}

func TestSelectTierCodingIsStandardOrHigher(t *testing.T) {
	p := Classify("fix the bug in main.go", ClassifyOptions{})
	tier := SelectTier(p, Resources{EffectiveContext: 8192, OutputReserve: 1024})
	if TierRank(tier) < TierRank(TierStandard) {
		t.Fatalf("coding tier = %s, want >= STANDARD", tier)
	}
}

func TestSelectTierArchitectureIsDeepOrMax(t *testing.T) {
	p := Classify("analyze the codebase and propose an architecture plan for this project", ClassifyOptions{})
	tier := SelectTier(p, Resources{EffectiveContext: 16384, OutputReserve: 2048})
	if TierRank(tier) < TierRank(TierDeep) {
		t.Fatalf("architecture tier = %s, want >= DEEP", tier)
	}
}

func TestSelectTierVisionNeverFast(t *testing.T) {
	p := Classify("what do you see?", ClassifyOptions{HasImages: true})
	tier := SelectTier(p, Resources{EffectiveContext: 8192, OutputReserve: 1024, VisionPayloadTokens: 1200})
	if TierRank(tier) < TierRank(TierStandard) {
		t.Fatalf("vision tier = %s, want >= STANDARD", tier)
	}
}

func TestSelectTierFastControlRespected(t *testing.T) {
	p := Classify("explain this code file carefully", ClassifyOptions{})
	tier := SelectTier(p, Resources{EffectiveContext: 8192, OutputReserve: 1024, UserDepth: "fast"})
	if tier != TierFast {
		t.Fatalf("fast control: tier = %s, want FAST", tier)
	}
}

func TestSelectTierFastControlCannotDropBelowVisionFloor(t *testing.T) {
	p := Classify("what do you see?", ClassifyOptions{HasImages: true})
	tier := SelectTier(p, Resources{EffectiveContext: 8192, OutputReserve: 1024, VisionPayloadTokens: 1500, UserDepth: "fast"})
	if TierRank(tier) < TierRank(TierStandard) {
		t.Fatalf("fast control with vision: tier = %s, want >= STANDARD", tier)
	}
}

func TestSelectTierThinkingControlKeepsClassifiedTier(t *testing.T) {
	// v1.2.6: "thinking" enables REASONING (the nudge), not context size.
	// A trivial chat stays on FAST even with thinking on; depth arrives
	// through the task's own signals and the evidence-driven escalation
	// ladder. (v1.2.5 floored thinking at STANDARD — the over-injection.)
	p := Classify("hi", ClassifyOptions{})
	tier := SelectTier(p, Resources{EffectiveContext: 8192, OutputReserve: 1024, UserDepth: "thinking"})
	if tier != TierFast {
		t.Fatalf("thinking control on a trivial chat: tier = %s, want FAST (reasoning ≠ bigger context)", tier)
	}

	// A genuinely deep task carries its own signals and lands deeper on its
	// own merits — with or without the thinking control.
	deep := Classify("analyze the codebase and design the architecture for this project", ClassifyOptions{})
	deepTier := SelectTier(deep, Resources{EffectiveContext: 8192, OutputReserve: 1024, UserDepth: "thinking"})
	if TierRank(deepTier) < TierRank(TierDeep) {
		t.Fatalf("deep task with thinking: tier = %s, want >= DEEP (task signals must drive depth)", deepTier)
	}
}

func TestSelectTierSmallContextCapped(t *testing.T) {
	p := Classify("analyze the whole codebase and design the architecture", ClassifyOptions{})
	tier := SelectTier(p, Resources{EffectiveContext: 4096, OutputReserve: 512})
	if TierRank(tier) > TierRank(TierStandard) {
		t.Fatalf("small ctx: tier = %s, want <= STANDARD", tier)
	}
}

func TestSelectTierLowRAMCapsDeep(t *testing.T) {
	p := Classify("analyze the codebase and design the architecture plan", ClassifyOptions{})
	tier := SelectTier(p, Resources{EffectiveContext: 16384, OutputReserve: 2048, SystemRAMMB: 2048})
	if TierRank(tier) > TierRank(TierStandard) {
		t.Fatalf("low RAM: tier = %s, want <= STANDARD", tier)
	}
}

func TestSelectTierLongHistoryEscalates(t *testing.T) {
	p := Classify("what did we discuss earlier?", ClassifyOptions{HistoryMessages: 40})
	res := Resources{
		EffectiveContext: 8192,
		OutputReserve:    1024,
		HistoryTokens:    4200, // cannot fit FAST's 30% share
	}
	tier := SelectTier(p, res)
	if TierRank(tier) < TierRank(TierStandard) {
		t.Fatalf("long history: tier = %s, want >= STANDARD", tier)
	}
}

func TestEscalateLadder(t *testing.T) {
	if s, ok := EscalateFrom(TierFast); !ok || s != TierStandard {
		t.Fatalf("FAST → %s (ok=%v), want STANDARD", s, ok)
	}

	if s, ok := EscalateFrom(TierStandard); !ok || s != TierDeep {
		t.Fatalf("STANDARD → %s (ok=%v), want DEEP", s, ok)
	}

	if s, ok := EscalateFrom(TierDeep); !ok || s != TierMax {
		t.Fatalf("DEEP → %s (ok=%v), want MAX", s, ok)
	}

	if _, ok := EscalateFrom(TierMax); ok {
		t.Fatalf("MAX must not escalate further")
	}
}

func TestEscalationDescribe(t *testing.T) {
	e := Escalation{From: TierFast, To: TierStandard, Reason: ReasonMissingFileContext, TokensBefore: 3800, TokensAfter: 7200}
	got := e.Describe()
	want := "FAST 3.8K → STANDARD 7.2K reason=MissingFileContext"
	if got != want {
		t.Fatalf("Describe = %q, want %q", got, want)
	}
}

func TestSpecDefaults(t *testing.T) {
	s := Spec("nonsense")
	if s.Name != TierStandard {
		t.Fatalf("unknown tier spec = %s, want STANDARD", s.Name)
	}

	fast := Spec(TierFast)
	if !fast.CompactBriefing || fast.IncludeRecall || fast.IncludeProjectCard || fast.IncludeSkills {
		t.Fatalf("FAST policy wrong: %+v", fast)
	}

	if fast.MaxTools != 6 || fast.ToolTokenBudget != 700 {
		t.Fatalf("FAST budgets wrong: %+v", fast)
	}

	max := Spec(TierMax)
	if max.ToolTokenBudget != 0 || max.MaxTools != 0 {
		t.Fatalf("MAX must be uncapped: %+v", max)
	}
}

func TestReasonVocabularyStable(t *testing.T) {
	want := []string{
		"MissingFileContext", "MissingHistory", "MissingToolContext",
		"RepositoryDependency", "VerificationFailure", "LargeAttachment",
		"VisionRequirement", "UserRequestedDepth",
	}

	got := AllReasonStrings()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("reason vocabulary changed: %v", got)
	}
}

func TestClassifyDeterministic(t *testing.T) {
	opts := ClassifyOptions{HasImages: false, HistoryMessages: 3}
	a := Classify("fix the bug and run the tests", opts)
	b := Classify("fix the bug and run the tests", opts)

	if a.Kind != b.Kind || a.Complexity != b.Complexity || a.Reason != b.Reason {
		t.Fatalf("classification not deterministic: %+v vs %+v", a, b)
	}
}
