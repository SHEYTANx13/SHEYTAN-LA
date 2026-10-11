// tiers.go — the adaptive FAST/STANDARD/DEEP/MAX ladder (v1.2.5).
//
// A tier is a CONTEXT POLICY, not a fixed size: it says which prompt
// sections may be composed, how much of the usable window history may
// occupy, and how many tool-schema tokens the request may spend. The
// absolute token numbers still come from the machine's real effective
// context, so the same tier is honest on a 4K-window laptop and on a
// 128K workstation — no hard-coded one-size context.
package taskclassify

import (
        "fmt"
        "strings"
)

// Tier names — stable for logs, telemetry and the UI contract.
const (
        TierFast     = "FAST"
        TierStandard = "STANDARD"
        TierDeep     = "DEEP"
        TierMax      = "MAX"
)

// Tier order index for ladder math.
var tierOrder = map[string]int{
        TierFast:     0,
        TierStandard: 1,
        TierDeep:     2,
        TierMax:      3,
}

// TierSpec is the context policy of one tier.
type TierSpec struct {
        Name string

        // CompactBriefing selects the ~350-token compact system briefing
        // instead of the full ~7.4K-token form.
        CompactBriefing bool

        // HistoryShare is the fraction of the usable window history may
        // occupy (after fixed sections) — 0.30..0.85.
        HistoryShare float64

        // ToolTokenBudget caps serialized tool-schema tokens in the prompt.
        ToolTokenBudget int

        // IncludeRecall / IncludeProjectCard / IncludeSkills /
        // IncludeRepoEvidence gate the optional composition work — the tier
        // decides BEFORE anything is composed, so FAST never pays
        // recall-retrieval or repo-index I/O at all.
        IncludeRecall       bool
        IncludeProjectCard  bool
        IncludeSkills       bool
        IncludeRepoEvidence bool

        // MaxTools caps the count of exposed tool schemas.
        MaxTools int
}

// tierSpecs are the policies per tier.
var tierSpecs = map[string]TierSpec{
        TierFast: {
                Name:                TierFast,
                CompactBriefing:     true,
                HistoryShare:        0.30,
                ToolTokenBudget:     700,
                IncludeRecall:       false,
                IncludeProjectCard:  false,
                IncludeSkills:       false,
                IncludeRepoEvidence: false,
                MaxTools:            6,
        },
        TierStandard: {
                Name:                TierStandard,
                CompactBriefing:     false,
                HistoryShare:        0.50,
                ToolTokenBudget:     1400,
                IncludeRecall:       true,
                IncludeProjectCard:  true,
                IncludeSkills:       false,
                IncludeRepoEvidence: true,
                MaxTools:            12,
        },
        TierDeep: {
                Name:                TierDeep,
                CompactBriefing:     false,
                HistoryShare:        0.70,
                ToolTokenBudget:     2200,
                IncludeRecall:       true,
                IncludeProjectCard:  true,
                IncludeSkills:       true,
                IncludeRepoEvidence: true,
                MaxTools:            16,
        },
        TierMax: {
                Name:                TierMax,
                CompactBriefing:     false,
                HistoryShare:        0.85,
                ToolTokenBudget:     0, // 0 = no cap, expose everything enabled
                IncludeRecall:       true,
                IncludeProjectCard:  true,
                IncludeSkills:       true,
                IncludeRepoEvidence: true,
                MaxTools:            0, // 0 = no cap
        },
}

// Spec returns the TierSpec for a tier name (unknown → STANDARD).
func Spec(tier string) TierSpec {
        if s, ok := tierSpecs[tier]; ok {
                return s
        }

        return tierSpecs[TierStandard]
}

// TierRank returns the ladder position of a tier (unknown → STANDARD's).
func TierRank(tier string) int {
        if r, ok := tierOrder[tier]; ok {
                return r
        }

        return tierOrder[TierStandard]
}

// Resources are the measured machine/request facts the tier decision uses.
// Every field is a REAL measured or resolved value — never a guess.
type Resources struct {
        // EffectiveContext is the model-aware usable window
        // (min(configured, GGUF limit, engine limit, session policy)).
        EffectiveContext int

        // OutputReserve is the tokens held back for the reply.
        OutputReserve int

        // SystemRAMMB / AvailableRAMMB come from the hardware probe; 0 = unknown.
        SystemRAMMB    int
        AvailableRAMMB int

        // VisionPayloadTokens is the measured vision payload estimate riding
        // this request (image tokens), 0 when none.
        VisionPayloadTokens int

        // AttachmentTokens is the composed attachment-chunk estimate, 0 when none.
        AttachmentTokens int

        // ToolRequirement is the measured token cost of the task-relevant
        // tool schemas the classifier's signals imply (from the toolsets
        // selection), 0 when unknown.
        ToolRequirement int

        // HistoryTokens is the measured full-history token estimate for THIS
        // session (what windowing would have to carry).
        HistoryTokens int

        // UserDepth is the composer thinking control: "fast" | "auto" | "thinking".
        UserDepth string
}

// SelectTier chooses the starting tier for a profile under the measured
// resources. The ladder is intentionally conservative upward: small tasks
// start small; demanding tasks or payloads start where the evidence says.
func SelectTier(p Profile, res Resources) string {
        tier := TierFast

        switch {
        case p.Complexity >= 75:
                tier = TierMax
        case p.Complexity >= 50:
                tier = TierDeep
        case p.Complexity >= 22:
                tier = TierStandard
        default:
                tier = TierFast
        }

        // Vision payloads are token-heavy: never run them on the 30% history
        // share of FAST.
        if res.VisionPayloadTokens > 0 && TierRank(tier) < TierRank(TierStandard) {
                tier = TierStandard
        }

        // Large attachments similarly need room to be represented at all.
        if res.AttachmentTokens > 0 && res.AttachmentTokens >= res.effectiveUsable()/4 && TierRank(tier) < TierRank(TierStandard) {
                tier = TierStandard
        }

        // A long session whose history cannot fit the tier's share escalates
        // so windowing does not immediately strip the conversation the task
        // references.
        if res.HistoryTokens > 0 && res.EffectiveContext > 0 {
                need := res.HistoryTokens + res.OutputReserve + res.ToolRequirement + 600
                if need > int(float64(res.effectiveUsable())*specShare(tier)) && TierRank(tier) < TierRank(TierMax) {
                        tier = nextTier(tier)
                }
        }

        // Small effective windows: keep the request SMALL rather than starve
        // output — the compact briefing tier is the right posture.
        if res.EffectiveContext > 0 && res.EffectiveContext <= 4096 && TierRank(tier) > TierRank(TierStandard) {
                tier = TierStandard
        }

        // Memory pressure on constrained machines: avoid the deepest tiers —
        // bigger prompts mean bigger KV cache pressure. Unknown RAM (0) never
        // influences the decision.
        if res.SystemRAMMB > 0 && res.SystemRAMMB < 4096 && TierRank(tier) >= TierRank(TierDeep) {
                tier = TierStandard
        }

        // The user's explicit thinking control is the FINAL word.
        // v1.8.5: the four-level ladder (low/mid/high/ultra) with the
        // legacy values preserved at the same rungs.
        switch strings.ToLower(strings.TrimSpace(res.UserDepth)) {
        case "fast", "low":
                // Latency first — but never below what the payload needs.
                floor := TierFast
                if res.VisionPayloadTokens > 0 || res.AttachmentTokens >= res.effectiveUsable()/4 {
                        floor = TierStandard
                }

                if TierRank(tier) > TierRank(floor) {
                        tier = floor
                }
        case "thinking", "high", "ultra":
                // v1.2.6: reasoning depth is NOT context size. The deeper
                // levels enable the reasoning nudge
                // (composer.ThinkingEnabled) and keep the evidence-driven
                // escalation ceiling — they never force a deeper context
                // tier. A trivial "2+2" at high/ultra stays FAST (compact
                // briefing, minimal history, no irrelevant injection); a
                // genuinely deep task carries its own complexity signals
                // and lands deeper on its own merits. The LEVEL's real
                // depth comes from its numeric reasoning budget on the
                // generation request (v1.8.5), not from context inflation.
        }

        return tier
}

// EscalationReason is the evidence code that justifies moving UP a tier
// mid-run. Every code is derived from real run observations — the model's
// behaviour, tool results or verification outcome — never a heuristic
// guess about the future.
type EscalationReason string

const (
        ReasonMissingFileContext   EscalationReason = "MissingFileContext"   // tool reported the needed file/context absent from the prompt
        ReasonMissingHistory       EscalationReason = "MissingHistory"       // the answer referenced conversation facts not in the window
        ReasonMissingToolContext   EscalationReason = "MissingToolContext"   // a refused tool call showed the needed tool was not offered
        ReasonRepositoryDependency EscalationReason = "RepositoryDependency" // the task needs project-wide context not yet loaded
        ReasonVerificationFailure  EscalationReason = "VerificationFailure"  // objective verification failed with a recoverable cause
        ReasonLargeAttachment      EscalationReason = "LargeAttachment"      // attachment retrieval was truncated by the tier budget
        ReasonVisionRequirement    EscalationReason = "VisionRequirement"    // vision content arrived mid-run
        ReasonUserRequestedDepth   EscalationReason = "UserRequestedDepth"   // the user explicitly asked for more depth
)

// AllReasonStrings returns the reason vocabulary (stable order) for
// validation and tests.
func AllReasonStrings() []string {
        return []string{
                string(ReasonMissingFileContext),
                string(ReasonMissingHistory),
                string(ReasonMissingToolContext),
                string(ReasonRepositoryDependency),
                string(ReasonVerificationFailure),
                string(ReasonLargeAttachment),
                string(ReasonVisionRequirement),
                string(ReasonUserRequestedDepth),
        }
}

// Escalation is one recorded tier move.
type Escalation struct {
        From         string           `json:"from"`
        To           string           `json:"to"`
        Reason       EscalationReason `json:"reason"`
        TokensBefore int              `json:"tokensBefore,omitempty"`
        TokensAfter  int              `json:"tokensAfter,omitempty"`
}

// Describe renders the compact log line, e.g.
// "FAST 3.8K → STANDARD 7.2K reason=MissingFileContext".
func (e Escalation) Describe() string {
        if e.TokensAfter > 0 {
                return fmt.Sprintf("%s %s → %s %s reason=%s",
                        e.From, formatK(e.TokensBefore), e.To, formatK(e.TokensAfter), e.Reason)
        }

        return fmt.Sprintf("%s → %s reason=%s", e.From, e.To, e.Reason)
}

// nextTier moves one step up the ladder; MAX has no successor.
func nextTier(tier string) string {
        switch tier {
        case TierFast:
                return TierStandard
        case TierStandard:
                return TierDeep
        case TierDeep:
                return TierMax
        default:
                return TierMax
        }
}

// EscalateFrom returns the next tier from the current one; ok=false at MAX.
func EscalateFrom(tier string) (string, bool) {
        if tier == TierMax {
                return TierMax, false
        }

        return nextTier(tier), true
}

// specShare returns the tier's history share (helper for SelectTier).
func specShare(tier string) float64 {
        return Spec(tier).HistoryShare
}

// effectiveUsable is the window minus the output reserve (never below 1).
func (r Resources) effectiveUsable() int {
        u := r.EffectiveContext - r.OutputReserve
        if u < 1 {
                return 1
        }

        return u
}

// DescribeResources renders the resource inputs for the decision log.
func DescribeResources(res Resources) string {
        return fmt.Sprintf(
                "effectiveCtx=%d outputReserve=%d ramMB=%d availMB=%d visionTok=%d attachTok=%d toolReq=%d historyTok=%d depth=%s",
                res.EffectiveContext, res.OutputReserve, res.SystemRAMMB, res.AvailableRAMMB,
                res.VisionPayloadTokens, res.AttachmentTokens, res.ToolRequirement,
                res.HistoryTokens, res.UserDepth,
        )
}

func formatK(n int) string {
        if n >= 1000 {
                return fmt.Sprintf("%.1fK", float64(n)/1000)
        }

        return fmt.Sprintf("%d", n)
}
