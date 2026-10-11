// automation.go — v1.7.0: CONNECTING THE FOUR SYSTEMS at the runtime
// layer.
//
// The real v1.7 flow, wired through the EXISTING authorities (no second
// scheduler, no parallel registries):
//
//	USER CREATES TASK (POST /api/automation/tasks)
//	        ↓
//	SCHEDULE / EVENT (durable claim Tick / NotifyEvent)
//	        ↓
//	AGENT RUN (the orchestrator — full reliability machinery)
//	        ↓
//	TASK-SCOPED SKILLS (linked JSON skills + task SKILL.md packages)
//	        ↓
//	TASK-SCOPED TOOLS (customtools TaskStore → the ONE registry)
//	        ↓
//	ARTIFACTS (artifact_create → the durable task/run registry)
//	        ↓
//	VERIFICATION (the run settles through the standard verification)
//	        ↓
//	CHRONOLOGICAL RUN HISTORY (reports.jsonl → the timeline API)
//	        ↓
//	OPTIONAL SKILL PROMOTION (VERIFIED rule, task scope → global)
//
// Real application-generated events (never synthetic): a succeeded clone
// IS a git_change; a failed Lab verification IS a test_failure; a file a
// tool writes IS a file_change; the boot IS startup.

package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/artifacts"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/customtools"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/lab"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/scheduler"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/skills"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/tools"
)

// wireAutomation installs the v1.7.0 automation stack onto a constructed
// Stack: task-scoped tool/artifact/skill authorities, the agent-facing
// task tools, and the genuine event emitters.
func wireAutomation(s *Stack) {
	if s == nil || s.Sched == nil || s.Orch == nil || s.Cfg == nil {
		return
	}

	taskTools, err := customtools.NewTaskStore(s.Cfg.DataDir)
	if err != nil {
		logging.Default().Warn("runtime", "task tools unavailable: %v", err)
	}
	s.TaskTools = taskTools

	artRegistry := artifacts.NewTaskRegistry(s.Cfg.DataDir)
	if err := artRegistry.Load(); err != nil {
		logging.Default().Warn("runtime", "artifact registry load: %v", err)
	}
	s.Artifacts = artRegistry

	mdSkills := skills.NewMarkdownStore(s.Cfg.DataDir, s.Cfg.WorkspaceDir()+"/skills")
	s.MarkdownSkills = mdSkills

	// The agent-facing task-scoped creation tools (self-gating: they
	// refuse to run outside a task context).
	tools.ArtifactRegistry = artRegistry
	tools.MarkdownSkills = mdSkills
	s.Orch.Register(tools.ArtifactCreate{})
	s.Orch.Register(tools.SkillCreate{})

	// REAL file_change emitter: a file a tool created IS a file change.
	tools.OnFileCreated = func(string) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			s.Sched.NotifyEvent(ctx, scheduler.EventFileChange)
		}()
	}

	// REAL test_failure emitter: a failed Lab verification IS a test
	// failure (the hook fires from the ONE settlement point — every
	// recorded verification, pass or fail).
	lab.OnVerificationSettled = func(taskID string, passed bool) {
		if passed {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			fired := s.Sched.NotifyEvent(ctx, scheduler.EventTestFailure)
			if len(fired) > 0 {
				logging.Default().Info("scheduler",
					"lab verification of task %s failed — %d automation task(s) triggered", taskID, len(fired))
			}
		}()
	}

	// REAL startup emitter: the application booted.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.Sched.NotifyEvent(ctx, scheduler.EventStartup)
	}()
}

// NotifyGitChange emits the genuine git_change event (called by the API
// when a clone genuinely succeeded and the workspace root changed).
func (s *Stack) NotifyGitChange() {
	if s == nil || s.Sched == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.Sched.NotifyEvent(ctx, scheduler.EventGitChange)
	}()
}

// TaskRunContextKey names the per-run task identity carried through the
// orchestrator call (for run scoping).
var TaskRunContextKey = &struct{ name string }{"task-run"}

// taskRunner replaces scheduleRunner: one scheduled task = one bounded
// agent run with the task context, task-scoped tools and linked skills
// installed for the run's lifetime.
func taskRunner(s *Stack, orch *agent.Orchestrator, runner scheduler.Runner) scheduler.Runner {
	return func(ctx context.Context, task scheduler.Task) (string, error) {
		if orch == nil {
			return "", fmt.Errorf("scheduler: orchestrator unavailable")
		}

		runID := fmt.Sprintf("run-%s-%d", task.ID, time.Now().UnixNano())

		// Task context for the agent-facing task tools.
		tools.SetTaskContext(&tools.TaskContext{
			TaskID:  task.ID,
			RunID:   runID,
			Trigger: string(task.Trigger),
		})
		defer tools.SetTaskContext(nil)

		// Task-scoped tools: registered into the ONE registry for the run,
		// unregistered at teardown (unapproved/disabled are never offered).
		if s.TaskTools != nil {
			unregister, registered := s.TaskTools.RegisterInto(
				customtools.RegistrarFuncs{
					Register:   func(t *customtools.Tool) { orch.Register(t) },
					Unregister: orch.Unregister,
				},
				task.ID,
			)
			defer unregister()
			if len(registered) > 0 {
				logging.Default().Info("scheduler",
					"task %s run %s: %d task-scoped tool(s) available", task.ID, runID, len(registered))
			}
		}

		prompt := task.Prompt

		// Task-scoped skills: linked JSON skills (the same Skill authority)
		// + task SKILL.md packages matched on the prompt, injected as a
		// bounded block (progressive disclosure — matched skills only).
		block := taskSkillBlock(s, task, prompt)
		if block != "" {
			prompt = block + "\n\n" + prompt
		}

		output, err := runner(ctx, task)

		// The run's report carries the artifact count (provenance echo).
		if s.Artifacts != nil {
			if n := len(s.Artifacts.ListRun(runID)); n > 0 {
				output = fmt.Sprintf("%s\n[%d task artifact(s) registered under run %s]", strings.TrimRight(output, "\n"), n, runID)
			}
		}

		return output, err
	}
}

// taskSkillBlock renders the bounded skill block for one task run.
func taskSkillBlock(s *Stack, task scheduler.Task, prompt string) string {
	var b strings.Builder

	// Linked JSON skills (same authority the orchestrator injects from).
	if s.Skills != nil && len(task.LinkedSkills) > 0 {
		all := s.Skills.All()
		var matched []skills.Skill
		for _, want := range task.LinkedSkills {
			for _, sk := range all {
				if strings.EqualFold(sk.Identity.ID, want) || strings.EqualFold(sk.Identity.Name, want) {
					matched = append(matched, sk)
					break
				}
			}
		}
		if len(matched) > 0 {
			if rendered := skills.RenderBlock(matched, 512); rendered != "" {
				b.WriteString(rendered)
			}
		}
	}

	// Task-scoped markdown SKILL.md packages (progressive disclosure:
	// match on metadata, load only matching bodies).
	if s.MarkdownSkills != nil {
		hits := s.MarkdownSkills.Match(prompt, strings.Join(task.TaskTypes, ","), 2)
		for _, hit := range hits {
			body, err := s.MarkdownSkills.LoadBody(hit.ID)
			if err != nil || strings.TrimSpace(body) == "" {
				continue
			}
			if b.Len() > 16*1024 {
				break // bounded injection — never unbounded
			}
			fmt.Fprintf(&b, "\n### TASK SKILL: %s (v%d, %s scope)\n%s\n",
				hit.Meta.Name, hit.Meta.Version, hit.Scope, body)
		}
	}

	if b.Len() == 0 {
		return ""
	}
	return "## TASK CONTEXT (scoped skills for this task — bounded)\n" + b.String()
}
