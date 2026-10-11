// orchestrator_tiers.go — v1.2.5 adaptive-turn machinery.
//
// This file implements the tier-driven composition the orchestrator's
// RunDetailed drives:
//
//	turnComposer  — holds the tier-scoped composition state and applies
//	                tier upgrades (escalation) mid-run without rebuilding
//	                the conversation from scratch;
//	escalationWatch — turns REAL run evidence (tool results, refused
//	                calls, verification outcome) into tier-escalation
//	                decisions with named reasons;
//	hardware snapshot — a TTL-cached measured RAM read feeding the tier
//	                decision (never a guess).
//
// The v1.2.4 behaviour — compose EVERYTHING, then degrade under overflow —
// is preserved as the degradation ladder safety net. What changes is the
// STARTING POINT: the first request now carries only P0 + the P1 the task
// signals actually need, and everything else arrives when evidence
// demands it.
package agent

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/aicontext"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/skills"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/taskclassify"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/toolsets"
)

// ThinkingControl values (composer → backend request).
//
// v1.8.5: the surface is the four-level REASONING DEPTH ladder
// low / mid / high / ultra. Each level carries a REAL numeric thinking
// token budget applied to the llama.cpp serving backend through the
// verified request-level `reasoning_budget_tokens` parameter (present in
// BOTH managed engine builds — b10642 and b11205 — verified against the
// actual server sources; a budget of 0 ends thinking immediately, a
// positive value caps it, -1 leaves the engine default/unrestricted).
// The engine applies the budget only when the model's chat template
// exposes a thinking section — a non-thinking model never fabricates
// reasoning; the level is simply inert there.
//
// Legacy v1.2.5 values (auto / fast / thinking) still arrive from older
// clients and persisted settings; they map onto the ladder and are
// normalized away at the boundary (never stored, never re-emitted).
const (
	ThinkingLow   = "low"
	ThinkingMid   = "mid"
	ThinkingHigh  = "high"
	ThinkingUltra = "ultra"

	// Legacy values (accepted at the wire boundary, normalized to the
	// ladder above; kept as constants so the migration is explicit).
	ThinkingAuto     = "auto"
	ThinkingFast     = "fast"
	ThinkingThinking = "thinking"
)

// Numeric reasoning budgets per level (thinking tokens).
//
//	low  →   0: thinking disabled ("0 for immediate end" — the engine's
//	             own documented semantics; latency first)
//	mid  → 1024: a bounded default — real thinking, predictably short
//	high → 4096: deeper reasoning for complex work
//	ultra→   -1: the engine default — unrestricted thinking
//
// These are REQUEST budgets on the llama.cpp OAI-compatible endpoint
// (verified in both managed builds' server sources), not prompt-side
// suggestions.
func reasoningBudgetTokens(control string) int {
	switch control {
	case ThinkingLow:
		return 0
	case ThinkingMid:
		return 1024
	case ThinkingHigh:
		return 4096
	case ThinkingUltra:
		return -1
	}
	return -1
}

// applyReasoningBudget stamps the level's numeric thinking-token budget
// onto ONE generation request. Local llama.cpp serving only — remote
// providers never receive the field (the same gating BuildChatRequest
// applies to TopK/NumCtx/MinP/RepeatLastN), and the native C++ path has
// no reasoning-budget control (its GenerationRequest carries no thinking
// channel — the level is documented-inert there rather than faked).
// ro.thinking is expected to be NORMALIZED (WithThinkingMode guarantees
// this); unknown values degrade to no budget, never to a fabricated one.
func applyReasoningBudget(cfg *config.Config, thinking string, req *llm.ChatRequest) {
	if cfg == nil || req == nil || cfg.IsRemote() {
		return
	}

	level := NormalizeThinkingControl(thinking)

	// -1 (ultra) means "engine default / unrestricted": the wire contract
	// treats -1 identically to omitting the field (the server falls back
	// to its own configured budget), so send NOTHING for it — the honest
	// encoding of "no client-side cap".
	budget := reasoningBudgetTokens(level)
	if budget < 0 {
		req.ReasoningBudget = nil
		return
	}

	req.ReasoningBudget = &budget
}

// NormalizeThinkingControl validates a thinking control string onto the
// v1.8.5 four-level ladder, migrating the legacy v1.2.5 vocabulary.
func NormalizeThinkingControl(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "fast", "low":
		return ThinkingLow
	case "thinking", "deep", "high":
		return ThinkingHigh
	case "ultra", "max":
		return ThinkingUltra
	default:
		// "auto", "", unknown → the balanced default.
		return ThinkingMid
	}
}

// ToolPolicyMode values.
const (
	ToolPolicyAuto   = "auto"
	ToolPolicyManual = "manual"
)

// ToolPolicy is the per-request manual tool control. In MANUAL mode only
// the listed tools may be offered AND executed — the orchestrator never
// silently re-enables a disabled tool, even when escalating context tiers.
//
// NetSearch (v1.3.6, spec §24) is the composer's EXPLICIT per-request
// Net Search intent: it authorizes the research tool for this request
// regardless of policy mode (the user asked for it directly), without
// widening any other tool's availability.
type ToolPolicy struct {
	Mode      string
	Allowed   []string
	NetSearch bool

	// AISystemConstrain (v1.9.0) is the ACTIVE AI System's allowed tool
	// surface. Non-empty, it restricts the offered AND executable tool
	// set in BOTH policy modes — server-side, never prompt-side. It can
	// never widen anything: it only removes. The Net Search intent
	// (below) is likewise subject to it: a system whose surface excludes
	// research never offers research, whatever the request intent.
	AISystemConstrain []string
}

// NormalizeToolPolicyMode validates a policy mode string.
func NormalizeToolPolicyMode(s string) string {
	if strings.EqualFold(strings.TrimSpace(s), ToolPolicyManual) {
		return ToolPolicyManual
	}

	return ToolPolicyAuto
}

// allows reports whether a tool may run under the policy.
func (p ToolPolicy) allows(name string) bool {
	// v1.9.0: the AI System surface binds FIRST — no intent, no policy
	// mode, no escalation can re-enable a tool the active system
	// excludes. Execution stays inside the user-owned surface.
	if len(p.AISystemConstrain) > 0 {
		ok := false
		for _, a := range p.AISystemConstrain {
			if strings.EqualFold(a, name) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}

	// v1.3.6: an explicit Net Search request authorizes exactly the
	// research tool — server-side enforcement of the user's intent,
	// never inferred from message text, never extended to other tools.
	if p.NetSearch && strings.EqualFold(name, "research") {
		return true
	}

	if p.Mode != ToolPolicyManual {
		return true
	}

	for _, a := range p.Allowed {
		if strings.EqualFold(a, name) {
			return true
		}
	}

	return false
}

// ---------------------------------------------------------------------------
// hardware snapshot (TTL-cached measured values)
// ---------------------------------------------------------------------------

// hwSnapshot is a TTL cache around hardware.Collect so tier selection
// gets MEASURED memory figures without re-probing the OS every turn.
type hwSnapshot struct {
	mu       sync.Mutex
	profile  atomic.Pointer[hardware.Profile]
	fetched  time.Time
	interval time.Duration
}

var hwCache = &hwSnapshot{interval: 60 * time.Second}

// RAMInfo returns measured total/available system RAM in MB (0/0 when the
// probe yields nothing). Cached for one minute; probes never run on the
// per-token path.
func RAMInfo() (totalMB, availMB int) {
	hwCache.mu.Lock()
	now := time.Now()
	prof := hwCache.profile.Load()

	if prof == nil || now.Sub(hwCache.fetched) > hwCache.interval {
		hwCache.mu.Unlock()

		p := hardware.Collect(&config.Config{})
		hwCache.profile.Store(&p)

		hwCache.mu.Lock()
		hwCache.fetched = now
		prof = &p
	}

	hwCache.mu.Unlock()

	if prof == nil {
		return 0, 0
	}

	totalMB = int(prof.RAM.TotalBytes >> 20)
	if prof.RAM.AvailableBytes > 0 {
		availMB = int(prof.RAM.AvailableBytes >> 20)
	} else if prof.RAM.FreeBytes > 0 {
		availMB = int(prof.RAM.FreeBytes >> 20)
	}

	return totalMB, availMB
}

// ---------------------------------------------------------------------------
// turnComposer — tier-scoped composition state + upgrades
// ---------------------------------------------------------------------------

// turnComposer owns the tier-scoped composition of ONE agent turn.
type turnComposer struct {
	orch      *Orchestrator
	cfg       *config.Config
	effCtx    llm.EffectiveContext
	safety    int
	policy    ToolPolicy
	thinkMode string

	// skillsAllow (v1.9.0) is the AI System's enabled-skills surface
	// (nil/empty = every installed skill remains discoverable).
	skillsAllow []string

	tier string
	spec taskclassify.TierSpec

	// Enabled tool names (config-gated), sorted.
	enabled []string

	// allNames is the offered surface (tier + policy selected).
	allNames []string

	// toolSpecs/toolTokens mirror allNames.
	toolSpecs []llm.ToolSpec
	toolTok   int

	// composed optional blocks
	card      string
	repoBlk   string
	skillBlk  string
	recallBlk string
	cardOn    bool
	repoOn    bool
	skillsOn  bool
	recallOn  bool

	// recallRetrievalMs is the measured recall-retrieval latency of the
	// composition (0 when the tier skipped recall entirely).
	recallRetrievalMs int64

	// briefing state
	usedCompact bool

	// escalated counts applied upgrades (bounded by maxEscalations).
	escalated int

	escalations []taskclassify.Escalation
}

const maxEscalations = 2

// newTurnComposer builds the composer at the STARTING tier and performs
// the tier-scoped composition immediately — FAST never pays for recall
// retrieval, project cards or skill matching at all.
func (o *Orchestrator) newTurnComposer(
	cfg *config.Config,
	effCtx llm.EffectiveContext,
	safety int,
	policy ToolPolicy,
	thinkMode string,
	skillsAllow []string,
	tier string,
	task string,
	recaller Recaller,
	cardProvider func() string,
	repoEvidence func(task string) string,
) *turnComposer {
	spec := taskclassify.Spec(tier)

	c := &turnComposer{
		orch:        o,
		cfg:         cfg,
		effCtx:      effCtx,
		safety:      safety,
		policy:      policy,
		thinkMode:   thinkMode,
		skillsAllow: skillsAllow,
		tier:        tier,
		spec:        spec,
	}

	// Enabled tools.
	for name := range o.Tools() {
		if cfg.ToolEnabled(name) {
			c.enabled = append(c.enabled, name)
		}
	}
	sortStrings(c.enabled)

	// Tier- and policy-scoped tool selection — from the START, not only
	// under overflow. Manual policy intersects; auto uses task signals.
	// v1.2.6: a ZERO-tool selection is a VALID outcome (pure conversation:
	// no capability signal → no schemas). The old `len(picked) > 0` guard
	// fell back to EVERY enabled tool — the exact over-injection the
	// contract prohibits.
	selected := c.enabled
	if policy.Mode == ToolPolicyManual {
		manual := make([]string, 0, len(policy.Allowed))
		for _, a := range c.Allowed() {
			for _, e := range c.enabled {
				if strings.EqualFold(a, e) {
					manual = append(manual, e)
					break
				}
			}
		}

		if len(manual) > 0 {
			selected = manual
		}
	} else {
		selected = toolsets.SelectForTask(c.enabled, task, spec.MaxTools)
	}

	// v1.3.6 (spec §23/§24): Net Search guarantee — when the user
	// explicitly enabled Net Search for this request, the research tool
	// is part of the offered surface regardless of task signals, in
	// BOTH policy modes. The global per-tool enable gate still applies
	// (research disabled in settings stays disabled).
	if policy.NetSearch {
		found := false

		for _, s := range selected {
			if strings.EqualFold(s, "research") {
				found = true
				break
			}
		}

		if !found {
			for _, e := range c.enabled {
				if strings.EqualFold(e, "research") {
					selected = append(selected, e)
					break
				}
			}
		}
	}

	// v1.9.0: the AI System surface intersects the tier/policy selection
	// — the OFFERED schema surface matches what allows() will execute.
	if len(policy.AISystemConstrain) > 0 {
		constrained := make([]string, 0, len(selected))
		for _, name := range selected {
			for _, a := range policy.AISystemConstrain {
				if strings.EqualFold(a, name) {
					constrained = append(constrained, name)
					break
				}
			}
		}
		// A zero-tool selection is valid (the v1.2.6 rule): a system
		// whose surface intersects nothing runs as pure conversation.
		selected = constrained
	}

	c.setTools(selected, task)

	// Optional composition — gated by the tier BEFORE any I/O happens.
	if spec.IncludeProjectCard && cardProvider != nil {
		c.card = cardProvider()
		c.cardOn = c.card != ""
	}

	// v1.3.4 (ROADMAP v1.4 slice 1): repository evidence for the task,
	// ranked by the persistent repoindex. Bounded retrieval, tier-gated.
	if spec.IncludeRepoEvidence && task != "" && repoEvidence != nil {
		c.repoBlk = repoEvidence(task)
		c.repoOn = c.repoBlk != ""
	}

	if spec.IncludeSkills && o.skillSource != nil && task != "" {
		if matched := filterSkillsBySystem(o.skillSource.MatchTask(task, 2), skillsAllow); len(matched) > 0 {
			c.skillBlk = renderSkills(matched, 400)
			c.skillsOn = c.skillBlk != ""

			for i := range matched {
				o.skillSource.RecordUse(matched[i].Identity.ID)
			}
		}
	}

	if spec.IncludeRecall && recaller != nil && cfg.RecallEnabled && task != "" {
		started := time.Now()
		c.recallBlk = recaller.RelevantBlock(task, cfg.EffectiveRecallTopK(), recallBlockTokenBudget)
		c.recallOn = c.recallBlk != ""
		c.recallRetrievalMs = time.Since(started).Milliseconds()
	}

	return c
}

// dropOptional clears every composed optional block (degradation ladder
// step 3 — the plan decided none of them travel this turn).
func (c *turnComposer) dropOptional() {
	c.card = ""
	c.repoBlk = ""
	c.skillBlk = ""
	c.recallBlk = ""
	c.cardOn = false
	c.repoOn = false
	c.skillsOn = false
	c.recallOn = false
}

// Allowed returns the manual-allowed list (normalized, deduplicated).
func (c *turnComposer) Allowed() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(c.policy.Allowed))

	for _, a := range c.policy.Allowed {
		a = strings.TrimSpace(a)
		if a == "" || seen[strings.ToLower(a)] {
			continue
		}

		seen[strings.ToLower(a)] = true
		out = append(out, a)
	}

	return out
}

// setTools stores the offered surface, bounded by the tier's tool-token
// budget (drop non-core tools last-in-first until it fits). The spec build
// runs against the CURRENT registry generation (v1.7.6): a tool replaced
// mid-turn re-serializes instead of serving a superseded schema.
func (c *turnComposer) setTools(names []string, task string) {
	tools := make([]Tool, 0, len(names))
	gen := c.orch.ToolsGeneration()

	for _, n := range names {
		if t, ok := c.orch.tool(n); ok {
			tools = append(tools, t)
		}
	}

	specs, tokens := c.orch.specs.BuildSpecs(tools, gen)

	// MAX (budget 0) never trims; a manual policy never trims below the
	// user's explicit selection — the budget only trims AUTO selections.
	if c.spec.ToolTokenBudget > 0 && c.policy.Mode != ToolPolicyManual && tokens > c.spec.ToolTokenBudget {
		for len(tools) > 1 && tokens > c.spec.ToolTokenBudget {
			// drop the least-important tool: non-core first, reverse
			// alphabetical within the same class (stable + explainable)
			idx := leastImportantToolIndex(tools)

			_, t := c.orch.specs.Spec(tools[idx], gen)
			tokens -= t

			tools = append(tools[:idx], tools[idx+1:]...)
		}

		specs, tokens = c.orch.specs.BuildSpecs(tools, gen)
	}

	c.toolSpecs = specs
	c.toolTok = tokens

	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name())
	}

	c.allNames = out
}

// leastImportantToolIndex picks the tool to drop under budget pressure:
// core tools (files/shell/memory) are kept longest; among the rest the
// reverse-alphabetical last is dropped first (deterministic).
func leastImportantToolIndex(tools []Tool) int {
	core := func(name string) bool {
		for _, c := range toolsets.CoreTools {
			if strings.EqualFold(c, name) {
				return true
			}
		}

		return false
	}

	best := -1

	for i, t := range tools {
		if core(t.Name()) {
			continue
		}

		if best == -1 {
			best = i
			continue
		}

		if t.Name() > tools[best].Name() {
			best = i
		}
	}

	if best >= 0 {
		return best
	}

	return len(tools) - 1 // all core: drop the last
}

// Briefing renders the system briefing for the current tier.
func (c *turnComposer) Briefing() (string, bool) {
	if c.spec.CompactBriefing {
		return aicontext.CompactSystemMessage(c.cfg, c.allNames), true
	}

	return aicontext.SystemMessageWithTools(c.cfg, c.allNames), false
}

// OptionalTokens is the composed optional-block token estimate.
func (c *turnComposer) OptionalTokens(staged int) int {
	return staged +
		tokensOrZero(c.card) +
		tokensOrZero(c.repoBlk) +
		tokensOrZero(c.skillBlk) +
		tokensOrZero(c.recallBlk)
}

func tokensOrZero(s string) int {
	if s == "" {
		return 0
	}

	return chunking.EstimateTokens(s)
}

// Injectables returns the composed optional blocks in priority order
// (card → repo evidence → skills → recall) with their injected flags.
func (c *turnComposer) Injectables() (card, repo, skills, recall string, cardOn, repoOn, skillsOn, recallOn bool) {
	return c.card, c.repoBlk, c.skillBlk, c.recallBlk, c.cardOn, c.repoOn, c.skillsOn, c.recallOn
}

// tierSpec returns the active tier spec (v1.8.2 memory-evidence seam:
// the injection site reads IncludeRecall to distinguish "recall did not
// apply to this turn" from "recall ran and found nothing").
func (c *turnComposer) tierSpec() taskclassify.TierSpec {
	return c.spec
}

// Escalate applies ONE tier upgrade (evidence-driven). It returns the
// enrichment that must be injected into the live conversation.
type upgrade struct {
	tier       string
	card       string
	repo       string
	skills     string
	recall     string
	fullBrief  string // non-empty → swap the compact briefing for the full one
	newNames   []string
	newSpecs   []llm.ToolSpec
	newToolTok int
	escalation taskclassify.Escalation
}

// Escalate upgrades the composer's tier, composing the blocks the new
// tier newly allows. Manual tool policy is NEVER widened by escalation.
// refusedTool (v1.2.6) re-enters the offered surface when the escalation
// evidence was a tool-surface refusal (auto policy only).
func (c *turnComposer) Escalate(reason taskclassify.EscalationReason, task, refusedTool string) (*upgrade, bool) {
	if c.escalated >= maxEscalations {
		return nil, false
	}

	next, ok := taskclassify.EscalateFrom(c.tier)
	if !ok {
		return nil, false
	}

	from := c.tier
	before := c.totalComposedTokens()

	c.tier = next
	c.spec = taskclassify.Spec(next)
	c.escalated++

	up := &upgrade{tier: next}

	// Briefing upgrade: compact → full.
	if c.usedCompact && !c.spec.CompactBriefing {
		up.fullBrief = aicontext.SystemMessageWithTools(c.cfg, c.allNames)
		c.usedCompact = false
	}

	// Newly-allowed optional blocks (compose lazily — exactly now).
	if c.spec.IncludeProjectCard && !c.cardOn && c.orch.projectCardProvider() != nil {
		c.card = c.orch.projectCardProvider()()
		c.cardOn = c.card != ""
		up.card = c.card
	}

	if c.spec.IncludeRepoEvidence && !c.repoOn && task != "" && c.orch.repoEvidenceProvider() != nil {
		c.repoBlk = c.orch.repoEvidenceProvider()(task)
		c.repoOn = c.repoBlk != ""
		up.repo = c.repoBlk
	}

	if c.spec.IncludeSkills && !c.skillsOn && c.orch.skillSource != nil && task != "" {
		if matched := filterSkillsBySystem(c.orch.skillSource.MatchTask(task, 2), c.skillsAllow); len(matched) > 0 {
			c.skillBlk = renderSkills(matched, 400)
			c.skillsOn = c.skillBlk != ""
			up.skills = c.skillBlk

			for i := range matched {
				c.orch.skillSource.RecordUse(matched[i].Identity.ID)
			}
		}
	}

	if c.spec.IncludeRecall && !c.recallOn && c.orch.recallProvider() != nil && c.cfg.RecallEnabled && task != "" {
		c.recallBlk = c.orch.recallProvider().RelevantBlock(task, c.cfg.EffectiveRecallTopK(), recallBlockTokenBudget)
		c.recallOn = c.recallBlk != ""
		up.recall = c.recallBlk
	}

	// Tool surface widening (auto policy only — manual never re-enables).
	// v1.2.6: the refused tool re-enters the surface FIRST (it is the
	// exact capability the run proved it needs), then the new tier's
	// task selection applies on top — union, deduplicated, bounded by
	// the new tier's MaxTools/token budget inside setTools.
	if c.policy.Mode != ToolPolicyManual {
		names := c.allNames

		if refusedTool != "" {
			found := false

			for _, n := range names {
				if strings.EqualFold(n, refusedTool) {
					found = true
					break
				}
			}

			if !found {
				for _, e := range c.enabled {
					if strings.EqualFold(e, refusedTool) {
						names = append([]string{e}, names...)
						break
					}
				}
			}
		}

		if picked := toolsets.SelectForTask(c.enabled, task, c.spec.MaxTools); len(picked) > 0 {
			seen := map[string]bool{}
			merged := make([]string, 0, len(picked)+len(names))

			for _, n := range names {
				if n == "" || seen[strings.ToLower(n)] {
					continue
				}
				seen[strings.ToLower(n)] = true
				merged = append(merged, n)
			}

			for _, n := range picked {
				if n == "" || seen[strings.ToLower(n)] {
					continue
				}
				seen[strings.ToLower(n)] = true
				merged = append(merged, n)
			}

			names = merged
		}

		if len(names) > len(c.allNames) {
			c.setTools(names, task)
		} else if c.spec.ToolTokenBudget > 0 && c.toolTok > c.spec.ToolTokenBudget {
			c.setTools(c.allNames, task)
		}

		up.newNames = c.allNames
		up.newSpecs = c.toolSpecs
		up.newToolTok = c.toolTok
	}

	up.escalation = taskclassify.Escalation{
		From:         from,
		To:           next,
		Reason:       reason,
		TokensBefore: before,
		TokensAfter:  c.totalComposedTokens(),
	}

	c.escalations = append(c.escalations, up.escalation)

	return up, true
}

// totalComposedTokens is the rough composed-context size (for the
// escalation log line).
func (c *turnComposer) totalComposedTokens() int {
	total := c.toolTok

	if c.cardOn {
		total += tokensOrZero(c.card)
	}

	if c.skillsOn {
		total += tokensOrZero(c.skillBlk)
	}

	if c.recallOn {
		total += tokensOrZero(c.recallBlk)
	}

	return total
}

// Escalations returns the recorded tier moves.
func (c *turnComposer) Escalations() []taskclassify.Escalation {
	return c.escalations
}

// Tier reports the current tier.
func (c *turnComposer) Tier() string {
	return c.tier
}

// HistoryShare reports the current tier's history share for the plan.
func (c *turnComposer) HistoryShare() float64 {
	return c.spec.HistoryShare
}

// ThinkingEnabled resolves the per-request thinking control against the
// global config: "low" disables the nudge for THIS request (latency
// first); "high"/"ultra" enable it even when the global toggle is off.
// v1.8.5: the nudge is the PROMPT-side posture only — the REAL per-level
// backend control is the numeric reasoning budget applied to the request
// (orchestrator.go's applyReasoningBudget); legacy values are normalized
// before they reach here.
func (c *turnComposer) ThinkingEnabled() bool {
	switch c.thinkMode {
	case ThinkingLow, ThinkingFast:
		return false
	case ThinkingHigh, ThinkingUltra, ThinkingThinking:
		return true
	default:
		return c.cfg.ThinkingMode
	}
}

// ---------------------------------------------------------------------------
// escalation evidence — REAL observations only
// ---------------------------------------------------------------------------

// escalationWatch accumulates run evidence and decides tier upgrades.
type escalationWatch struct {
	mu      sync.Mutex
	pending taskclassify.EscalationReason
	has     bool

	// refusedTool (v1.2.6) is the tool the model tried to call while it
	// was NOT part of the offered surface. The escalation re-offers
	// exactly this tool — without it, a pure-conversation task
	// (zero-signal → zero tools) could never recover from its own
	// refusal.
	refusedTool string
}

// note records one escalation signal; the FIRST signal of a turn wins
// (the escalation applies the full ladder step, which usually resolves
// the later signals too).
func (w *escalationWatch) note(reason taskclassify.EscalationReason) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.has {
		w.pending = reason
		w.has = true
	}
}

// take returns and clears the pending reason (plus the refused tool,
// when the evidence was a tool-surface refusal).
func (w *escalationWatch) take() (taskclassify.EscalationReason, string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	r := w.pending
	tool := w.refusedTool
	ok := w.has
	w.has = false
	w.pending = ""
	w.refusedTool = ""

	return r, tool, ok
}

// observeToolResult derives escalation evidence from one real tool
// outcome. Patterns are deliberately conservative — each one is a signal
// the CONTEXT was insufficient, not a generic failure.
func (w *escalationWatch) observeToolResult(tool, result string, failed bool) {
	if failed {
		return
	}

	lower := strings.ToLower(result)

	switch tool {
	case "files", "diff", "codeExec":
		// The model reached for a file/context that was never in the
		// prompt — the project card (repo map + conventions) is the cure.
		if containsAnyOf(lower, "file not found", "no such file", "does not exist", "cannot find") {
			w.note(taskclassify.ReasonMissingFileContext)
		}
	}

	// Project-wide asks: the answer references the repository itself.
	if containsAnyOf(lower, "the codebase", "the whole project", "the repository structure", "all files") {
		w.note(taskclassify.ReasonRepositoryDependency)
	}
}

// observeRefusal records the orchestrator's own tool-surface refusal —
// the model tried a tool that was not offered this turn. The refused
// tool's name is kept so the escalation can offer exactly what was
// missing (v1.2.6).
func (w *escalationWatch) observeRefusal(tool string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.has {
		w.pending = taskclassify.ReasonMissingToolContext
		w.has = true
		w.refusedTool = tool
	} else if w.refusedTool == "" {
		w.refusedTool = tool
	}
}

// observeVerification records a failed objective verification with a
// recoverable shape (build/test evidence the deeper tiers can repair).
func (w *escalationWatch) observeVerification(outcome string) {
	if strings.Contains(outcome, "failed") {
		w.note(taskclassify.ReasonVerificationFailure)
	}
}

// observeAttachmentTruncation records that staged attachment content was
// dropped under the tier's budget.
func (w *escalationWatch) observeAttachmentTruncation() {
	w.note(taskclassify.ReasonLargeAttachment)
}

// observeVisionPayload records mid-run vision content (a tool produced
// images the tier did not plan for).
func (w *escalationWatch) observeVisionPayload() {
	w.note(taskclassify.ReasonVisionRequirement)
}

func containsAnyOf(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}

	return false
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// visionPayloadTokens estimates the image payload riding the request.
// Per-image cost is the documented mmproj planning figure (an ESTIMATE
// used only for tier selection — the plan's reported tokens remain
// measured text tokens).
const perImageTokenEstimate = 800

func visionPayloadTokens(messages []llm.Message) int {
	n := 0

	for i := range messages {
		n += len(messages[i].Images)
	}

	return n * perImageTokenEstimate
}

// countStagedBlocks measures the attachment blocks already injected as
// system messages by the API layer (identified by their stable header).
const stagedAttachmentHeader = "[staged attachments relevant to this turn]"

func countStagedBlocks(messages []llm.Message) (blocks int, tokens int) {
	for i := range messages {
		if messages[i].Role == "system" && strings.Contains(messages[i].Content, stagedAttachmentHeader) {
			blocks++
			tokens += chunking.EstimateTokens(messages[i].Content)
		}
	}

	return blocks, tokens
}

// countImageMessages reports whether images ride the request and how
// many attachments the conversation carries.
func requestFacts(messages []llm.Message) (hasImages bool, imageCount, attachmentBlocks int) {
	for i := range messages {
		if len(messages[i].Images) > 0 {
			hasImages = true
			imageCount += len(messages[i].Images)
		}

		if messages[i].Attachments != nil {
			attachmentBlocks += len(messages[i].Attachments)
		}
	}

	return hasImages, imageCount, attachmentBlocks
}

// renderSkills delegates to the skills package (kept local to avoid an
// import cycle in the composer file).
func renderSkills(matched []skills.Skill, budget int) string {
	return skills.RenderBlock(matched, budget)
}

// The orchestrator accessor shims below keep the composer decoupled from
// the orchestrator's private fields while reading under the right locks.

func (o *Orchestrator) projectCardProvider() func() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.projectCard
}

func (o *Orchestrator) repoEvidenceProvider() func(task string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.repoEvidence
}

func (o *Orchestrator) recallProvider() Recaller {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.recaller
}


// filterSkillsBySystem (v1.9.0) narrows matched skills to the AI System's
// enabled-skills surface. An empty allow list keeps every match (the
// pre-v1.9 behavior); a non-empty list is the user-owned surface — skills
// outside it never inflate the context.
func filterSkillsBySystem(matched []skills.Skill, allow []string) []skills.Skill {
	if len(allow) == 0 {
		return matched
	}
	out := make([]skills.Skill, 0, len(matched))
	for _, m := range matched {
		id := m.Identity.ID
		for _, a := range allow {
			if strings.EqualFold(strings.TrimSpace(a), id) {
				out = append(out, m)
				break
			}
		}
	}
	return out
}
