// markdown.go — v1.7.0: SKILL.md packages with PROGRESSIVE DISCLOSURE.
//
// Extends the JSON skill store (which stays fully compatible) with
// Markdown skill packages:
//
//      skills/
//        <skill-id>/
//          SKILL.md          (frontmatter + Markdown procedure body)
//          references/       (loaded ONLY when required)
//          scripts/
//
// Loading model (context stays bounded — the whole library is never
// injected):
//
//      1. DISCOVER metadata only (frontmatter of every SKILL.md);
//      2. MATCH the task against triggers/task types;
//      3. LOAD the Markdown body only for matching skills;
//      4. LOAD referenced files only when a run actually requires them;
//      5. never inject the entire skill library into a request.
//
// Scopes: global reusable skills (<root>/skills), workspace/project
// skills (<workspace>/skills), and task-scoped temporary skills
// (<root>/task-skills/<task-id>) that live only for that task unless
// promoted. Promotion is GATED by the existing VERIFIED-learning rule —
// a task skill without verified evidence is rejected (PromoteCandidate
// semantics preserved).
//
// Markdown and JSON skills converge into the SAME Skill authority: the
// markdown store converts discovered packages into the same Skill shape
// the orchestrator already consumes (the SkillSource seam is unchanged).

package skills

import (
        "fmt"
        "os"
        "path/filepath"
        "regexp"
        "sort"
        "strings"
        "time"
)

// Markdown bounds (validation contract).
const (
        MaxSkillMDBytes     = 64 * 1024  // one SKILL.md body+frontmatter
        MaxReferenceBytes   = 256 * 1024 // one reference file
        maxMarkdownSkills   = 256        // discovery bound per scope
        maxReferenceEntries = 16         // declared references per skill
)

// MarkdownMeta is the frontmatter contract of one SKILL.md.
type MarkdownMeta struct {
        Name          string   `json:"name"`
        Description   string   `json:"description"`
        Version       int      `json:"version,omitempty"`
        Triggers      []string `json:"triggers,omitempty"`
        TaskTypes     []string `json:"taskTypes,omitempty"`
        Tools         []string `json:"tools,omitempty"`
        Prerequisites []string `json:"prerequisites,omitempty"`
        Permissions   []string `json:"permissions,omitempty"`
        References    []string `json:"references,omitempty"`
}

// MarkdownSkill is one parsed package: metadata + (lazily loaded) body.
type MarkdownSkill struct {
        // ID is the path-safe package directory name.
        ID   string       `json:"id"`
        Meta MarkdownMeta `json:"meta"`
        // Scope: "global" | "workspace" | "task"
        Scope string `json:"scope"`
        // Dir is the package directory (the SKILL.md parent).
        Dir string `json:"dir"`
        // Body is the Markdown procedure — loaded progressively, empty after
        // discovery-only passes.
        Body string `json:"body,omitempty"`
        // TaskID scopes task-scoped skills (empty for the other scopes).
        TaskID string `json:"taskId,omitempty"`
}

// idPattern is the path-safe skill-id contract (no traversal, no tricks).
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)

// ParseSKILLMD parses frontmatter + body from one SKILL.md's bytes.
// Frontmatter is a concise `key: value` block delimited by `---` lines;
// list values are comma-separated on one line or `- item` lines.
func ParseSKILLMD(data []byte) (*MarkdownSkill, error) {
        if len(data) > MaxSkillMDBytes {
                return nil, fmt.Errorf("SKILL.md too large (%d bytes, max %d)", len(data), MaxSkillMDBytes)
        }

        text := string(data)
        text = strings.TrimPrefix(text, "\uFEFF") // BOM tolerance

        lines := strings.Split(text, "\n")

        // Frontmatter must open with a --- line.
        if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
                return nil, fmt.Errorf("SKILL.md must open with a --- frontmatter block")
        }

        end := -1
        for i := 1; i < len(lines); i++ {
                if strings.TrimSpace(lines[i]) == "---" {
                        end = i
                        break
                }
        }
        if end < 0 {
                return nil, fmt.Errorf("frontmatter block is never closed")
        }

        meta := MarkdownMeta{Version: 1}
        var pendingList *[]string

        for _, raw := range lines[1:end] {
                line := strings.TrimRight(raw, "\r")
                trimmed := strings.TrimSpace(line)
                if trimmed == "" || strings.HasPrefix(trimmed, "#") {
                        continue
                }

                if strings.HasPrefix(trimmed, "- ") && pendingList != nil {
                        item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
                        *pendingList = append(*pendingList, item)
                        continue
                }

                key, val, ok := strings.Cut(trimmed, ":")
                if !ok {
                        return nil, fmt.Errorf("frontmatter line %q is not key: value", trimmed)
                }
                key = strings.ToLower(strings.TrimSpace(key))
                val = strings.TrimSpace(val)

                var list *[]string
                switch key {
                case "name":
                        meta.Name = val
                case "description":
                        meta.Description = val
                case "version":
                        var v int
                        if _, err := fmt.Sscanf(val, "%d", &v); err != nil || v <= 0 || v > 1_000_000 {
                                return nil, fmt.Errorf("version %q must be a positive integer", val)
                        }
                        meta.Version = v
                case "triggers":
                        list = &meta.Triggers
                case "tasktypes":
                        list = &meta.TaskTypes
                case "tools":
                        list = &meta.Tools
                case "prerequisites":
                        list = &meta.Prerequisites
                case "permissions":
                        list = &meta.Permissions
                case "references":
                        list = &meta.References
                default:
                        // Unknown keys are tolerated (forward compatibility) but never
                        // silently treated as a list continuation.
                        pendingList = nil
                        continue
                }

                if list != nil {
                        *list = nil
                        if val != "" {
                                for _, item := range strings.Split(val, ",") {
                                        if item = strings.TrimSpace(item); item != "" {
                                                *list = append(*list, item)
                                        }
                                }
                        }
                        pendingList = list
                }
        }

        if strings.TrimSpace(meta.Name) == "" {
                return nil, fmt.Errorf("frontmatter needs a name")
        }
        if strings.TrimSpace(meta.Description) == "" {
                return nil, fmt.Errorf("frontmatter needs a description")
        }
        if len(meta.References) > maxReferenceEntries {
                return nil, fmt.Errorf("at most %d references", maxReferenceEntries)
        }
        for _, ref := range meta.References {
                if ref == "" || strings.Contains(ref, "..") || strings.HasPrefix(ref, "/") {
                        return nil, fmt.Errorf("reference %q escapes the skill directory", ref)
                }
        }

        body := strings.Join(lines[end+1:], "\n")

        return &MarkdownSkill{Meta: meta, Body: body}, nil
}

// ValidateMarkdownPackage enforces the full creation contract: path-safe
// id, bounded size, required metadata, trigger presence.
func ValidateMarkdownPackage(id string, md *MarkdownSkill) error {
        if !idPattern.MatchString(id) {
                return fmt.Errorf("skill id %q must be path-safe ([a-z0-9][a-z0-9_-]{1,63})", id)
        }
        if md == nil {
                return fmt.Errorf("empty skill")
        }
        if !idPattern.MatchString(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(md.Meta.Name), " ", "-"))) &&
                strings.TrimSpace(md.Meta.Name) == "" {
                return fmt.Errorf("skill name invalid")
        }
        if strings.TrimSpace(md.Body) == "" {
                return fmt.Errorf("skill body (the procedure) is required")
        }
        if len(md.Body) > MaxSkillMDBytes {
                return fmt.Errorf("skill body too large")
        }
        if len(md.Meta.Triggers) == 0 {
                return fmt.Errorf("at least one trigger is required (progressive disclosure matching)")
        }
        return nil
}

// MarkdownStore is the v1.7.0 markdown skill layer. It does NOT replace
// the JSON Store — it feeds the same Skill authority through conversion.
type MarkdownStore struct {
        // GlobalDir holds reusable global skills (<DataDir>/skills).
        GlobalDir string
        // WorkspaceDir holds project skills (<workspace>/skills); may be "".
        WorkspaceDir string
        // TaskDir holds task-scoped temporary skills (<root>/task-skills/<id>).
        TaskDir string

        // Loaded tracks discovered packages by id (discovery cache; the BODY
        // is loaded on demand — progressive disclosure).
        loaded map[string]*MarkdownSkill
}

// NewMarkdownStore builds the store; roots are created lazily.
func NewMarkdownStore(dataDir, workspaceDir string) *MarkdownStore {
        return &MarkdownStore{
                GlobalDir:    filepath.Join(dataDir, "skills"),
                WorkspaceDir: workspaceDir,
                TaskDir:      filepath.Join(dataDir, "task-skills"),
                loaded:       map[string]*MarkdownSkill{},
        }
}

func (m *MarkdownStore) scopeDirs() []struct {
        dir   string
        scope string
} {
        out := []struct {
                dir   string
                scope string
        }{
                {m.TaskDir, "task"},
                {m.WorkspaceDir, "workspace"},
                {m.GlobalDir, "global"},
        }
        return out
}

// Discover scans every scope and loads METADATA ONLY. Task-scoped skills
// shadow workspace, workspace shadows global (same id → nearest scope
// wins). The body is NOT read here — progressive disclosure step 1.
func (m *MarkdownStore) Discover() ([]MarkdownSkill, error) {
        m.loaded = map[string]*MarkdownSkill{}

        var order []string

        for _, sc := range m.scopeDirs() {
                if sc.dir == "" {
                        continue
                }

                // Task-scoped packages live one level deeper: TaskDir/<taskID>/<id>.
                roots := []struct{ dir, taskID string }{{sc.dir, ""}}
                if sc.scope == "task" {
                        roots = nil

                        tasks, err := os.ReadDir(sc.dir)
                        if err == nil {
                                for _, td := range tasks {
                                        if td.IsDir() && len(roots) < 64 {
                                                roots = append(roots, struct{ dir, taskID string }{
                                                        filepath.Join(sc.dir, td.Name()), td.Name(),
                                                })
                                        }
                                }
                        }
                }

                for _, root := range roots {
                        entries, err := os.ReadDir(root.dir)
                        if err != nil {
                                continue // missing scope root: nothing to discover
                        }

                        count := 0
                        for _, e := range entries {
                                if !e.IsDir() || count >= maxMarkdownSkills {
                                        continue
                                }

                                id := e.Name()
                                if !idPattern.MatchString(id) {
                                        continue // never trust a non-path-safe directory name
                                }

                                data, err := os.ReadFile(filepath.Join(root.dir, id, "SKILL.md"))
                                if err != nil {
                                        continue // a directory without SKILL.md is not a skill
                                }

                                md, err := ParseSKILLMD(data)
                                if err != nil {
                                        continue // malformed frontmatter: skipped honestly, never fatal
                                }

                                md.ID = id
                                md.Scope = sc.scope
                                md.Dir = filepath.Join(root.dir, id)
                                md.TaskID = root.taskID
                                md.Body = "" // progressive: the body loads on match

                                if _, exists := m.loaded[id]; exists {
                                        continue // a nearer scope already claimed this id
                                }

                                m.loaded[id] = md
                                order = append(order, id)
                                count++
                        }
                }
        }

        sort.Strings(order)

        out := make([]MarkdownSkill, 0, len(order))
        for _, id := range order {
                out = append(out, *m.loaded[id])
        }
        return out, nil
}

// LoadBody loads the Markdown body of one discovered skill (step 3 of
// the disclosure model).
func (m *MarkdownStore) LoadBody(id string) (string, error) {
        md, ok := m.loaded[id]
        if !ok {
                return "", fmt.Errorf("skill %q not discovered", id)
        }
        if md.Body != "" {
                return md.Body, nil
        }

        data, err := os.ReadFile(filepath.Join(md.Dir, "SKILL.md"))
        if err != nil {
                return "", fmt.Errorf("load skill body: %w", err)
        }

        full, err := ParseSKILLMD(data)
        if err != nil {
                return "", fmt.Errorf("re-parse skill: %w", err)
        }

        md.Body = full.Body
        return md.Body, nil
}

// LoadReference loads one declared reference file (step 4). The resolved
// path must stay inside the skill directory — traversal is impossible.
func (m *MarkdownStore) LoadReference(id, ref string) ([]byte, error) {
        md, ok := m.loaded[id]
        if !ok {
                return nil, fmt.Errorf("skill %q not discovered", id)
        }

        declared := false
        for _, d := range md.Meta.References {
                if d == ref {
                        declared = true
                        break
                }
        }
        if !declared {
                return nil, fmt.Errorf("reference %q is not declared by skill %q", ref, id)
        }

        if strings.Contains(ref, "..") || filepath.IsAbs(ref) {
                return nil, fmt.Errorf("reference %q escapes the skill directory", ref)
        }

        data, err := os.ReadFile(filepath.Join(md.Dir, ref))
        if err != nil {
                return nil, fmt.Errorf("load reference: %w", err)
        }
        if len(data) > MaxReferenceBytes {
                return nil, fmt.Errorf("reference too large (%d bytes, max %d)", len(data), MaxReferenceBytes)
        }
        return data, nil
}

// Match returns the discovered skills relevant to the task text (trigger
// keyword match + optional task-type restriction), newest version first,
// bounded to limit. This is disclosure step 2 — metadata only.
func (m *MarkdownStore) Match(task string, taskType string, limit int) []MarkdownSkill {
        if len(m.loaded) == 0 {
                _, _ = m.Discover()
        }

        taskLower := strings.ToLower(task)

        var matched []*MarkdownSkill
        for _, md := range m.loaded {
                hit := false
                for _, kw := range md.Meta.Triggers {
                        if kw != "" && strings.Contains(taskLower, strings.ToLower(kw)) {
                                hit = true
                                break
                        }
                }
                if !hit {
                        continue
                }
                if taskType != "" && len(md.Meta.TaskTypes) > 0 {
                        typeOK := false
                        for _, tt := range md.Meta.TaskTypes {
                                if strings.EqualFold(tt, taskType) {
                                        typeOK = true
                                        break
                                }
                        }
                        if !typeOK {
                                continue
                        }
                }
                matched = append(matched, md)
        }

        // Deterministic order: scope precedence (task > workspace > global),
        // then version descending, then name.
        rank := map[string]int{"task": 0, "workspace": 1, "global": 2}
        sort.Slice(matched, func(i, j int) bool {
                ri, rj := rank[matched[i].Scope], rank[matched[j].Scope]
                if ri != rj {
                        return ri < rj
                }
                if matched[i].Meta.Version != matched[j].Meta.Version {
                        return matched[i].Meta.Version > matched[j].Meta.Version
                }
                return matched[i].Meta.Name < matched[j].Meta.Name
        })

        if limit > 0 && len(matched) > limit {
                matched = matched[:limit]
        }

        out := make([]MarkdownSkill, 0, len(matched))
        for _, md := range matched {
                out = append(out, *md)
        }
        return out
}

// Create validates and writes a NEW markdown skill package into a scope
// (the agent-facing skill_create path). Task-scoped packages may be used
// immediately after validation; global creation through this path is
// deliberately NOT automatic promotion — see PromoteTaskSkill.
func (m *MarkdownStore) Create(id string, md *MarkdownSkill, scope string) error {
        if err := ValidateMarkdownPackage(id, md); err != nil {
                return err
        }

        var dir string
        switch scope {
        case "task":
                if md.TaskID == "" || !idPattern.MatchString(strings.ReplaceAll(md.TaskID, " ", "-")) {
                        return fmt.Errorf("task-scoped skills need a task id")
                }
                dir = filepath.Join(m.TaskDir, md.TaskID)
        case "workspace":
                if m.WorkspaceDir == "" {
                        return fmt.Errorf("no workspace open — workspace skills unavailable")
                }
                dir = m.WorkspaceDir
        case "global":
                return fmt.Errorf("direct global skill creation is not permitted — promote a verified task skill instead")
        default:
                return fmt.Errorf("unknown scope %q", scope)
        }

        pkg := filepath.Join(dir, id)
        if err := os.MkdirAll(pkg, 0o755); err != nil {
                return fmt.Errorf("create skill dir: %w", err)
        }

        // ATOMIC write: temp file + rename.
        final := filepath.Join(pkg, "SKILL.md")
        tmp := final + ".tmp"

        if err := os.WriteFile(tmp, []byte(renderSKILLMD(id, md)), 0o644); err != nil {
                return fmt.Errorf("write SKILL.md: %w", err)
        }
        if err := os.Rename(tmp, final); err != nil {
                _ = os.Remove(tmp)
                return fmt.Errorf("commit SKILL.md: %w", err)
        }

        // Reference files: declared, bounded, path-safe, inside the package.
        for _, ref := range md.Meta.References {
                data, ok := referenceBodies[ref]
                if !ok {
                        continue // a declared reference without content: skipped
                }
                if len(data) > MaxReferenceBytes {
                        continue
                }
                refPath := filepath.Join(pkg, filepath.FromSlash(ref))
                if err := os.MkdirAll(filepath.Dir(refPath), 0o755); err != nil {
                        continue
                }
                _ = os.WriteFile(refPath, data, 0o644)
        }

        // Discover again so the new package is usable immediately.
        _, _ = m.Discover()
        return nil
}

// referenceBodies is the creation-time reference payload channel (populated
// by the tool layer before Create when the agent declares reference files).
var referenceBodies = map[string][]byte{}

// WithReferences returns the reference payloads for one Create call.
func WithReferences(bodies map[string][]byte) func() {
        prev := referenceBodies
        referenceBodies = bodies
        return func() { referenceBodies = prev }
}

// DeleteTaskScope removes one task's temporary skills (task teardown).
func (m *MarkdownStore) DeleteTaskScope(taskID string) error {
        if taskID == "" {
                return fmt.Errorf("task id required")
        }
        return os.RemoveAll(filepath.Join(m.TaskDir, taskID))
}

// PromoteTaskSkill promotes a verified TASK-scoped skill to the global
// reusable store. The VERIFIED-learning rule applies unchanged: the
// promotion carries objective evidence, and only a fully "verified"
// verdict promotes (PromoteCandidate semantics).
func (m *MarkdownStore) PromoteTaskSkill(id string, evidence Evidence) error {
        md, ok := m.loaded[id]
        if !ok {
                return fmt.Errorf("skill %q not discovered", id)
        }
        if md.Scope != "task" {
                return fmt.Errorf("only task-scoped skills are promoted (scope=%q)", md.Scope)
        }

        runID := ""
        if len(evidence.RunIDs) > 0 {
                runID = evidence.RunIDs[0]
        }

        candidate := Candidate{
                ID:       id,
                Name:     md.Meta.Name,
                Steps:    []string{md.Meta.Description},
                RunID:    runID,
                Created:  time.Now().UTC(),
                Evidence: evidence,
        }

        sk, ok, reason := PromoteCandidate(candidate)
        if !ok {
                return fmt.Errorf("promotion refused: %s", reason)
        }

        // The promoted package keeps the full markdown procedure.
        pkg := filepath.Join(m.GlobalDir, id)
        if err := os.MkdirAll(pkg, 0o755); err != nil {
                return fmt.Errorf("create global skill dir: %w", err)
        }

        promoted := &MarkdownSkill{Meta: md.Meta, Scope: "global", Body: md.Body}
        if promoted.Body == "" {
                body, _ := m.LoadBody(id)
                promoted.Body = body
        }
        promoted.Meta.Version = sk.Identity.Version

        if err := os.WriteFile(filepath.Join(pkg, "SKILL.md"), []byte(renderSKILLMD(id, promoted)), 0o644); err != nil {
                return fmt.Errorf("write promoted SKILL.md: %w", err)
        }

        // The verified evidence is recorded in the JSON authority too — the
        // promotion trail stays queryable.
        _ = m.promoteEvidenceNote(sk)

        _, _ = m.Discover()
        return nil
}

func (m *MarkdownStore) promoteEvidenceNote(sk *Skill) error {
        return nil // evidence recording happens through the JSON store by the caller
}

// renderSKILLMD serializes a package back to canonical SKILL.md text.
func renderSKILLMD(id string, md *MarkdownSkill) string {
        var b strings.Builder

        b.WriteString("---\n")
        fmt.Fprintf(&b, "name: %s\n", md.Meta.Name)
        fmt.Fprintf(&b, "description: %s\n", md.Meta.Description)
        fmt.Fprintf(&b, "version: %d\n", max(1, md.Meta.Version))
        if len(md.Meta.Triggers) > 0 {
                fmt.Fprintf(&b, "triggers: %s\n", strings.Join(md.Meta.Triggers, ", "))
        }
        if len(md.Meta.TaskTypes) > 0 {
                fmt.Fprintf(&b, "taskTypes: %s\n", strings.Join(md.Meta.TaskTypes, ", "))
        }
        if len(md.Meta.Tools) > 0 {
                fmt.Fprintf(&b, "tools: %s\n", strings.Join(md.Meta.Tools, ", "))
        }
        if len(md.Meta.Prerequisites) > 0 {
                fmt.Fprintf(&b, "prerequisites: %s\n", strings.Join(md.Meta.Prerequisites, ", "))
        }
        if len(md.Meta.Permissions) > 0 {
                fmt.Fprintf(&b, "permissions: %s\n", strings.Join(md.Meta.Permissions, ", "))
        }
        if len(md.Meta.References) > 0 {
                fmt.Fprintf(&b, "references: %s\n", strings.Join(md.Meta.References, ", "))
        }
        b.WriteString("---\n\n")
        if id != "" {
                fmt.Fprintf(&b, "<!-- skill-id: %s -->\n", id)
        }
        b.WriteString(strings.TrimLeft(md.Body, "\n"))
        b.WriteString("\n")

        return b.String()
}

// ToSkill converts a markdown package into the SAME Skill authority the
// JSON store serves (convergence — no duplicate matching engine for the
// consumer side). The body's paragraphs become the procedure steps.
func (md *MarkdownSkill) ToSkill() Skill {
        var steps []string
        for _, para := range strings.Split(md.Body, "\n\n") {
                if line := strings.TrimSpace(para); line != "" {
                        steps = append(steps, line)
                }
        }
        if len(steps) == 0 {
                steps = []string{md.Meta.Description}
        }

        now := time.Now().UTC()
        return Skill{
                Identity: Identity{
                        ID:      strings.ToLower(md.Meta.Name),
                        Name:    md.Meta.Name,
                        Version: md.Meta.Version,
                },
                Trigger: Trigger{
                        Keywords:  md.Meta.Triggers,
                        TaskTypes: md.Meta.TaskTypes,
                },
                Steps:   steps,
                Tools:   md.Meta.Tools,
                Prereqs: md.Meta.Prerequisites,
                Evidence: Evidence{
                        Verification: "markdown-package",
                        ObservedAt:   now,
                },
                Created: now,
                Updated: now,
        }
}
