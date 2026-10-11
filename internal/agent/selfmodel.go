package agent

// selfmodel.go — v1.8.2 RUNTIME SELF-MODEL (capability questions).
//
// When the user asks what the runtime itself has — its tools,
// capabilities, access, or the model it is running — the model must
// answer from a compact, CURRENT description of the real runtime. This
// file is the ONE formatter that composes that description from the
// EXISTING authoritative sources; it owns no registry, no capability
// database and no model facts of its own:
//
//      tool catalog      — the orchestrator's tool registry snapshot (the
//                          same registry /api/tools serves), described via
//                          ShortDescription() when available with a bounded
//                          safe fallback from Description();
//      model facts       — the per-run ModelCapabilities card already
//                          resolved for the context decision (no re-parse);
//      backend/engine    — config provider kind + the installed engine tag
//                          from the updater bookkeeping + streaming /
//                          cancellation / research / Lab / memory facts the
//                          runtime actually has;
//      hardware          — the sysinfo fast snapshot (OS/CPU/RAM measured,
//                          GPU/NPU labeled DETECTION-only, never execution
//                          evidence);
//      memory            — which memory systems exist and what THIS turn
//                          actually injected (the plan's memory evidence).
//
// The block is bounded (selfModelMaxBytes) so a large registry or a long
// description list can never blow the context budget. Capability intent
// is deliberately CHEAP: no web research, no recall, no repo indexing —
// every input above is already in memory for the current turn.

import (
        "fmt"
        "sort"
        "strings"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

const (
        // selfModelMaxBytes bounds the composed block. It must fit the FAST
        // tier's optional-section budget comfortably; the capability answer
        // is a compact catalog, not a manual.
        selfModelMaxBytes = 6 * 1024

        // selfModelToolDescCap bounds ONE tool description line (Short
        // descriptions are naturally short; a Description() fallback is
        // truncated to its first sentence within this bound).
        selfModelToolDescCap = 140

        // selfModelMaxTools caps how many tool lines render before the block
        // notes the remaining count. Every registered tool is still counted
        // truthfully in the summary line.
        selfModelMaxTools = 48
)

// SelfModelInput carries the per-run facts the builder formats. Every
// field comes from an existing authority — the builder derives nothing.
type SelfModelInput struct {
        Cfg *config.Config

        // ToolSnapshot is the orchestrator registry snapshot (ToolsAt) — the
        // ONE tool registry.
        ToolSnapshot map[string]Tool

        // EnabledNames lists config-enabled tools (sorted, as produced by
        // the tier window).
        EnabledNames []string

        // OfferedNames lists the tools actually offered to the model for
        // THIS request (tier + policy selection — the composer surface).
        OfferedNames []string

        // Caps is the model capability card resolved once for THIS run (the
        // same object the context decision used — never a re-parse). Nil when
        // the card was unreadable (remote provider, missing file).
        Caps *llm.ModelCapabilities

        // MemoryNote is the measured memory-evidence line for this turn
        // (composed from the plan's MemoryEvidence — "" when none yet).
        MemoryNote string
}

// shortToolDescription renders the concise description for one tool:
// ShortDescription() when the tool implements it, otherwise the first
// sentence of Description() bounded to selfModelToolDescCap. Shared by
// the self-model and any other concise-description consumer so the two
// surfaces cannot drift.
func shortToolDescription(t Tool) string {
        if sd, ok := t.(interface{ ShortDescription() string }); ok {
                if s := strings.TrimSpace(sd.ShortDescription()); s != "" {
                        return truncateToolDesc(s)
                }
        }

        desc := strings.TrimSpace(t.Description())
        if desc == "" {
                return "(no description)"
        }

        // First sentence of the operational description — never the full
        // JSON-schema prose.
        if i := strings.IndexAny(desc, ".\n"); i > 0 {
                desc = desc[:i]
        }

        return truncateToolDesc(desc)
}

func truncateToolDesc(s string) string {
        s = strings.TrimSpace(s)
        if len(s) <= selfModelToolDescCap {
                return s
        }
        return s[:selfModelToolDescCap-1] + "…"
}

// BuildSelfModel composes the bounded runtime self-model block. The
// output is plain text the model reads as authoritative runtime facts.
func BuildSelfModel(in SelfModelInput) string {
        if in.Cfg == nil {
                return ""
        }

        var b strings.Builder

        b.WriteString("RUNTIME SELF-MODEL (measured facts about this application right now — answer capability questions from this block, never invent tools or claim hardware execution that is not evidenced here):\n")

        // --- TOOLS -----------------------------------------------------------
        offered := make(map[string]bool, len(in.OfferedNames))
        for _, n := range in.OfferedNames {
                offered[n] = true
        }
        enabled := make(map[string]bool, len(in.EnabledNames))
        for _, n := range in.EnabledNames {
                enabled[n] = true
        }

        registered := make([]string, 0, len(in.ToolSnapshot))
        for name := range in.ToolSnapshot {
                registered = append(registered, name)
        }
        sort.Strings(registered)

        offeredList := make([]string, 0, len(in.OfferedNames))
        notOfferedEnabled := make([]string, 0)
        disabled := make([]string, 0)

        for _, name := range registered {
                switch {
                case enabled[name] && offered[name]:
                        offeredList = append(offeredList, name)
                case enabled[name]:
                        notOfferedEnabled = append(notOfferedEnabled, name)
                default:
                        disabled = append(disabled, name)
                }
        }

        fmt.Fprintf(&b, "\nTOOLS — %d registered, %d enabled, %d offered for this request:\n",
                len(registered), len(enabled), len(offeredList))

        shown := 0
        for _, name := range offeredList {
                if shown >= selfModelMaxTools {
                        fmt.Fprintf(&b, "  … and %d more offered tools (all listed in the tool surface above this note)\n", len(offeredList)-shown)
                        break
                }
                t := in.ToolSnapshot[name]
                fmt.Fprintf(&b, "  %s — %s\n", name, shortToolDescription(t))
                shown++
        }

        if len(notOfferedEnabled) > 0 {
                fmt.Fprintf(&b, "  Enabled but not offered this request (tier/tool selection narrowed the surface): %s\n",
                        strings.Join(notOfferedEnabled, ", "))
        }

        if len(disabled) > 0 {
                fmt.Fprintf(&b, "  Registered but currently DISABLED in settings (NOT callable): %s\n",
                        strings.Join(disabled, ", "))
        }

        // --- MODEL -----------------------------------------------------------
        b.WriteString("\nMODEL:\n")
        if caps := in.Caps; caps != nil {
                if caps.Name != "" {
                        fmt.Fprintf(&b, "  name: %s\n", caps.Name)
                }
                fmt.Fprintf(&b, "  file: %s\n", caps.FileName)
                if caps.Arch != "" {
                        fmt.Fprintf(&b, "  architecture: %s\n", caps.Arch)
                }
                if caps.Quant != "" {
                        fmt.Fprintf(&b, "  quantization: %s\n", caps.Quant)
                }
                if caps.ParamsText != "" {
                        fmt.Fprintf(&b, "  parameters: %s\n", caps.ParamsText)
                }
                if caps.ContextLength > 0 {
                        fmt.Fprintf(&b, "  training context limit: %d tokens\n", caps.ContextLength)
                } else {
                        b.WriteString("  training context limit: unknown\n")
                }
                if caps.TokenizerFamily != "" && caps.TokenizerFamily != "unknown" {
                        fmt.Fprintf(&b, "  tokenizer family: %s\n", caps.TokenizerFamily)
                }
                fmt.Fprintf(&b, "  chat template: %s\n", yesNo(caps.ChatTemplate))
                if caps.Multimodal {
                        b.WriteString("  vision: available (vision projector paired)\n")
                } else {
                        b.WriteString("  vision: not available for this model\n")
                }
                if caps.NativeBackend {
                        b.WriteString("  native engine: this architecture is natively executable\n")
                } else if caps.NativeReason != "" {
                        fmt.Fprintf(&b, "  native engine: %s\n", caps.NativeReason)
                }
        } else {
                b.WriteString("  model card not readable this turn — model facts limited to what the user configured\n")
                if m := strings.TrimSpace(in.Cfg.Model); m != "" {
                        fmt.Fprintf(&b, "  configured model: %s\n", m)
                }
        }

        // --- BACKEND / ENGINE ------------------------------------------------
        b.WriteString("\nBACKEND / ENGINE:\n")
        switch {
        case in.Cfg.IsRemote():
                b.WriteString("  provider: remote endpoint (no local engine)\n")
        case in.Cfg.NativeBackendEnabled():
                b.WriteString("  provider: SHEYTAN native C++ engine\n")
        default:
                b.WriteString("  provider: local llama.cpp server (managed subprocess)\n")
                if tag := strings.TrimSpace(updater.InstalledEngineTag(in.Cfg)); tag != "" {
                        fmt.Fprintf(&b, "  installed engine build: %s\n", tag)
                }
        }
        b.WriteString("  streaming: supported (incremental deltas)\n")
        b.WriteString("  cancellation: supported (a Stop request ends generation at the next boundary)\n")
        if n := netcheck.Note(); strings.Contains(n, "offline") {
                b.WriteString("  web research: unavailable (runtime is offline)\n")
        } else {
                b.WriteString("  web research: available through the research tool when enabled\n")
        }
        fmt.Fprintf(&b, "  coding lab: %s\n", enabledDisabled(in.Cfg.LabEnabled))
        b.WriteString("  rolling session summary: built into every turn from this session's transcript\n")
        b.WriteString("  targeted recall (past exchanges): ")
        if in.Cfg.RecallEnabled {
                b.WriteString("available\n")
        } else {
                b.WriteString("disabled in settings\n")
        }
        fmt.Fprintf(&b, "  continuum chapter rollover: %s\n", enabledDisabled(in.Cfg.ContinuumEnabled))

        // --- HARDWARE --------------------------------------------------------
        b.WriteString("\nHARDWARE (measured detection — detection is not execution evidence):\n")
        info := sysinfo.ProbeFast()
        if info != nil {
                os := info.OSDisplay
                if os == "" {
                        os = info.OS
                }
                fmt.Fprintf(&b, "  os: %s (%s)\n", orUnknown(os), orUnknown(info.Arch))
                if info.CPU.Name != "" {
                        fmt.Fprintf(&b, "  cpu: %s (%d cores)\n", info.CPU.Name, info.CPU.LogicalCores)
                } else {
                        fmt.Fprintf(&b, "  cpu: %d logical cores\n", info.CPU.LogicalCores)
                }
                if info.RAM.TotalBytes > 0 {
                        fmt.Fprintf(&b, "  ram: %s total\n", humanBytes(info.RAM.TotalBytes))
                }
                if len(info.GPU) > 0 {
                        names := make([]string, 0, len(info.GPU))
                        for _, g := range info.GPU {
                                if g.Name != "" {
                                        names = append(names, g.Name)
                                }
                        }
                        if len(names) > 0 {
                                fmt.Fprintf(&b, "  gpu (detected, not necessarily used for inference): %s\n", strings.Join(names, ", "))
                        } else {
                                b.WriteString("  gpu (detected, not necessarily used for inference): present\n")
                        }
                } else {
                        b.WriteString("  gpu: none detected\n")
                }
                if info.NPU != nil {
                        b.WriteString("  npu (detected, not necessarily used for inference): present\n")
                }
        } else {
                b.WriteString("  hardware facts not measured yet\n")
        }

        // --- MEMORY ----------------------------------------------------------
        b.WriteString("\nMEMORY:\n")
        b.WriteString("  systems: rolling session summary · targeted recall of past exchanges · cross-mode history references")
        if in.Cfg.ContinuumEnabled {
                b.WriteString(" · continuum chapter rollover")
        }
        b.WriteString("\n")
        if in.MemoryNote != "" {
                fmt.Fprintf(&b, "  active this turn: %s\n", in.MemoryNote)
        } else {
                b.WriteString("  active this turn: (reported with the context summary when the turn completes assembly)\n")
        }

        out := b.String()
        if len(out) > selfModelMaxBytes {
                out = out[:selfModelMaxBytes] + "\n…[self-model truncated]"
        }

        return out
}

// modelCapsForSelfModel was folded into SelfModelInput.Caps (v1.8.2):
// the orchestrator passes the already-resolved card; the builder never
// re-parses or re-resolves model facts on its own.

func yesNo(v bool) string {
        if v {
                return "yes"
        }
        return "no"
}

func enabledDisabled(v bool) string {
        if v {
                return "enabled"
        }
        return "disabled"
}

func orUnknown(s string) string {
        if strings.TrimSpace(s) == "" {
                return "unknown"
        }
        return s
}

func humanBytes(n uint64) string {
        const kib = 1024
        switch {
        case n >= kib*kib*kib:
                return fmt.Sprintf("%.1f GiB", float64(n)/(kib*kib*kib))
        case n >= kib*kib:
                return fmt.Sprintf("%.1f MiB", float64(n)/(kib*kib))
        case n >= kib:
                return fmt.Sprintf("%.1f KiB", float64(n)/kib)
        default:
                return fmt.Sprintf("%d B", n)
        }
}
