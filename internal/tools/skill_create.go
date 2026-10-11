// skill_create.go — v1.7.0: the agent-facing VALIDATED skill creation
// tool (Markdown SKILL.md packages, task-scoped by default).
//
// Rules (the v1.7.0 contract):
//   - path-safe skill id (refused otherwise);
//   - frontmatter schema validated (name/description/triggers required);
//   - bounded file size;
//   - no filesystem escape (the store enforces the root);
//   - NO automatic promotion to globally reusable skills — task skills
//     may be used immediately after validation, promotion goes through
//     the existing VERIFIED-learning rule.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/skills"
)

// MarkdownSkills is the store seam the runtime installs.
var MarkdownSkills *skills.MarkdownStore

// SkillCreate is the registry tool.
type SkillCreate struct{}

func (SkillCreate) Name() string { return "skill_create" }

func (SkillCreate) ShortDescription() string {
	return "Create a validated task-scoped skill (SKILL.md) from a verified procedure"
}

func (SkillCreate) Description() string {
	return `Create a TASK-SCOPED skill package (SKILL.md) so a procedure proven in this task can be reused later in the same task.

The skill needs: a name, a one-line description, trigger keywords (when the skill applies) and the procedure body in Markdown (steps, verification, failure modes). It is validated (path-safe id, schema, bounded size) and usable IMMEDIATELY inside the task.

It is NOT promoted to the global reusable library automatically — promotion requires verified objective evidence (the VERIFIED-learning rule).`
}

func (SkillCreate) Parameters() any {
	return struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Triggers    []string `json:"triggers"`
		TaskTypes   []string `json:"taskTypes,omitempty"`
		Tools       []string `json:"tools,omitempty"`
		Body        string   `json:"body"`
	}{}
}

func (SkillCreate) Run(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Triggers    []string `json:"triggers"`
		TaskTypes   []string `json:"taskTypes"`
		Tools       []string `json:"tools"`
		Body        string   `json:"body"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("skill_create arguments: %w", err)
	}

	if MarkdownSkills == nil {
		return "", fmt.Errorf("skill store unavailable")
	}

	id := strings.ToLower(strings.TrimSpace(p.ID))
	name := strings.TrimSpace(p.Name)

	if len(p.Body) > skills.MaxSkillMDBytes {
		return "", fmt.Errorf("skill body too large (max %d bytes)", skills.MaxSkillMDBytes)
	}
	if len(p.Triggers) == 0 {
		return "", fmt.Errorf("at least one trigger keyword is required")
	}

	scope := "task"
	taskID := ""
	if tc := CurrentTaskContext(); tc != nil {
		taskID = tc.TaskID
	} else {
		// Outside a task run there is no task scope to own the skill —
		// creating one is refused (global promotion is a separate,
		// verified path).
		return "", fmt.Errorf("skill_create is task-scoped — no task context is installed (use it inside a task run)")
	}

	md := &skills.MarkdownSkill{
		Meta: skills.MarkdownMeta{
			Name:        name,
			Description: strings.TrimSpace(p.Description),
			Version:     1,
			Triggers:    p.Triggers,
			TaskTypes:   p.TaskTypes,
			Tools:       p.Tools,
		},
		Scope:  scope,
		TaskID: taskID,
		Body:   p.Body,
	}

	if err := MarkdownSkills.Create(id, md, scope); err != nil {
		return "", err
	}

	return fmt.Sprintf("skill %q created in the task scope (id=%s) — usable immediately; promotion to the global library requires verified evidence", name, id), nil
}
