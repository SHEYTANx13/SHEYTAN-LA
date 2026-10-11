package skills

// markdown_test.go — v1.7.0 SKILL.md progressive-disclosure contract:
// valid/invalid markdown, metadata discovery, trigger matching,
// progressive loading, malformed frontmatter, path traversal, task-scoped
// lifecycle, global promotion, unverified rejection.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validSkillMD = `---
name: Deploy Verify
description: Verify a deployment end to end before reporting success
version: 2
triggers: deploy, release
taskTypes: coding, ops
tools: shell, fetch
references: references/checklist.md
---

Run the verification procedure:

1. Probe /health until 200.
2. Check the release marker.
3. Report evidence.
`

func writeSkill(t *testing.T, dir, id, content string) {
	t.Helper()
	pkg := filepath.Join(dir, id)
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseValidSkillMD(t *testing.T) {
	md, err := ParseSKILLMD([]byte(validSkillMD))
	if err != nil {
		t.Fatal(err)
	}

	if md.Meta.Name != "Deploy Verify" || md.Meta.Version != 2 {
		t.Fatalf("meta: %+v", md.Meta)
	}
	if len(md.Meta.Triggers) != 2 || md.Meta.Triggers[0] != "deploy" {
		t.Fatalf("triggers: %v", md.Meta.Triggers)
	}
	if len(md.Meta.Tools) != 2 || md.Meta.Tools[1] != "fetch" {
		t.Fatalf("tools: %v", md.Meta.Tools)
	}
	if len(md.Meta.References) != 1 || md.Meta.References[0] != "references/checklist.md" {
		t.Fatalf("references: %v", md.Meta.References)
	}
	if !strings.Contains(md.Body, "Probe /health") {
		t.Fatalf("body: %q", md.Body)
	}
}

func TestParseMalformedFrontmatter(t *testing.T) {
	cases := map[string]string{
		"no frontmatter":  "just a body, no --- block",
		"never closed":    "---\nname: X\ndescription: Y\nbody without close",
		"missing name":    "---\ndescription: Y\n---\nbody",
		"missing description": "---\nname: X\n---\nbody",
		"bad version":     "---\nname: X\ndescription: Y\nversion: abc\n---\nbody",
		"escaping ref":    "---\nname: X\ndescription: Y\nreferences: ../../etc/passwd\n---\nbody",
	}

	for what, content := range cases {
		if _, err := ParseSKILLMD([]byte(content)); err == nil {
			t.Errorf("%s: expected rejection, got none", what)
		}
	}
}

func TestDiscoverMetadataOnlyAndProgressiveBody(t *testing.T) {
	dataDir := t.TempDir()
	global := filepath.Join(dataDir, "skills")
	writeSkill(t, global, "deploy-verify", validSkillMD)
	writeSkill(t, global, "broken-skill", "---\nname: Broken\n") // no description, no close... invalid

	m := NewMarkdownStore(dataDir, "")

	found, err := m.Discover()
	if err != nil {
		t.Fatal(err)
	}

	// The broken package is skipped honestly; exactly one survives.
	if len(found) != 1 || found[0].Meta.Name != "Deploy Verify" {
		t.Fatalf("discovered: %+v (err=%v)", found, err)
	}

	// Progressive: discovery does NOT load bodies.
	if found[0].Body != "" {
		t.Fatal("discovery must not load the body (progressive disclosure)")
	}

	// Step 3: the body loads on demand.
	body, err := m.LoadBody("deploy-verify")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "release marker") {
		t.Fatalf("loaded body: %q", body)
	}
}

func TestTriggerMatchingAndScopePrecedence(t *testing.T) {
	dataDir := t.TempDir()
	global := filepath.Join(dataDir, "skills")
	workspace := filepath.Join(dataDir, "ws", "skills")
	taskDir := filepath.Join(dataDir, "task-skills", "task-1")

	writeSkill(t, global, "deploy-verify", strings.Replace(validSkillMD, "version: 2", "version: 1", 1))
	writeSkill(t, workspace, "deploy-verify", validSkillMD)
	writeSkill(t, taskDir, "deploy-verify", strings.Replace(validSkillMD, "version: 2", "version: 9", 1))
	writeSkill(t, global, "unrelated-skill", strings.Replace(validSkillMD, "triggers: deploy, release", "triggers: gardening", 1))

	m := NewMarkdownStore(dataDir, workspace)

	matched := m.Match("please deploy the service", "", 5)
	if len(matched) != 1 {
		t.Fatalf("matched: %+v", matched)
	}

	// Task scope shadows workspace shadows global; the TASK copy (v9) wins.
	if matched[0].Scope != "task" || matched[0].Meta.Version != 9 {
		t.Fatalf("scope precedence failed: %+v", matched[0])
	}

	// Task-type restriction filters.
	if got := m.Match("deploy now", "research", 5); len(got) != 0 {
		t.Fatalf("task-type filter failed: %+v", got)
	}
	if got := m.Match("deploy now", "coding", 5); len(got) != 1 {
		t.Fatalf("task-type match failed: %+v", got)
	}

	// Limit honored.
	if got := m.Match("deploy", "", 0); len(got) != 1 {
		t.Fatalf("unrelated skill must not match: %+v", got)
	}
}

func TestReferenceLoadingAndTraversal(t *testing.T) {
	dataDir := t.TempDir()
	global := filepath.Join(dataDir, "skills")

	writeSkill(t, global, "deploy-verify", validSkillMD)
	refs := filepath.Join(global, "deploy-verify", "references")
	if err := os.MkdirAll(refs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refs, "checklist.md"), []byte("# checklist\n- health"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMarkdownStore(dataDir, "")
	if _, err := m.Discover(); err != nil {
		t.Fatal(err)
	}

	data, err := m.LoadReference("deploy-verify", "references/checklist.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "health") {
		t.Fatalf("reference: %q", data)
	}

	// Undeclared or escaping references are refused.
	if _, err := m.LoadReference("deploy-verify", "references/other.md"); err == nil {
		t.Fatal("undeclared reference must be refused")
	}
	if _, err := m.LoadReference("deploy-verify", "../../secret"); err == nil {
		t.Fatal("escaping reference must be refused")
	}
}

func TestTaskScopedLifecycleAndPromotion(t *testing.T) {
	dataDir := t.TempDir()
	m := NewMarkdownStore(dataDir, "")

	md := &MarkdownSkill{
		Meta: MarkdownMeta{
			Name:        "Tmp Probe",
			Description: "A temporary task skill",
			Version:     1,
			Triggers:    []string{"probe"},
		},
		Scope:  "task",
		TaskID: "task-42",
		Body:   "Do the probe procedure.",
	}

	if err := m.Create("tmp-probe", md, "task"); err != nil {
		t.Fatal(err)
	}

	// Usable immediately after validation.
	matched := m.Match("run the probe", "", 5)
	if len(matched) != 1 || matched[0].Scope != "task" {
		t.Fatalf("task skill not usable after create: %+v", matched)
	}

	// Global creation through the creation path is refused (no automatic
	// promotion to reusable global skills).
	if err := m.Create("direct-global", md, "global"); err == nil {
		t.Fatal("direct global creation must be refused")
	}

	// Promotion WITHOUT verified evidence is refused (VERIFIED rule).
	if err := m.PromoteTaskSkill("tmp-probe", Evidence{Verification: "claimed"}); err == nil {
		t.Fatal("unverified promotion must be refused")
	}
	if err := m.PromoteTaskSkill("tmp-probe", Evidence{Verification: "partially_verified"}); err == nil {
		t.Fatal("partially verified promotion must be refused")
	}

	// Verified promotion moves the package to the global scope.
	if err := m.PromoteTaskSkill("tmp-probe", Evidence{
		Verification: "verified",
		RunIDs:       []string{"run-1"},
		Checks:       []string{"probe /health = 200"},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(m.GlobalDir, "tmp-probe", "SKILL.md")); err != nil {
		t.Fatalf("promoted package missing: %v", err)
	}

	// Task-scoped skills are cleaned up with the task.
	if err := m.DeleteTaskScope("task-42"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.TaskDir, "task-42")); !os.IsNotExist(err) {
		t.Fatal("task scope survived deletion")
	}
}

func TestCreateValidationRejections(t *testing.T) {
	m := NewMarkdownStore(t.TempDir(), "")

	good := &MarkdownSkill{
		Meta:    MarkdownMeta{Name: "Good", Description: "d", Triggers: []string{"go"}},
		Body:    "steps",
		TaskID:  "task-1",
	}

	// Path-unsafe ids.
	for _, id := range []string{"../escape", "UPPER", "a", "with space", "slash/x"} {
		if err := m.Create(id, good, "task"); err == nil {
			t.Errorf("id %q must be rejected", id)
		}
	}

	// Missing triggers.
	noTrigger := &MarkdownSkill{Meta: MarkdownMeta{Name: "NT", Description: "d"}, Body: "b", TaskID: "task-1"}
	if err := m.Create("no-trigger", noTrigger, "task"); err == nil {
		t.Error("skill without triggers must be rejected")
	}

	// Missing body.
	noBody := &MarkdownSkill{Meta: MarkdownMeta{Name: "NB", Description: "d", Triggers: []string{"x"}}, TaskID: "task-1"}
	if err := m.Create("no-body", noBody, "task"); err == nil {
		t.Error("skill without a body must be rejected")
	}
}

func TestToSkillConvergesToSkillAuthority(t *testing.T) {
	md, err := ParseSKILLMD([]byte(validSkillMD))
	if err != nil {
		t.Fatal(err)
	}

	sk := md.ToSkill()
	if sk.Identity.Name != "Deploy Verify" || sk.Identity.Version != 2 {
		t.Fatalf("identity: %+v", sk.Identity)
	}
	if len(sk.Trigger.Keywords) != 2 || sk.Trigger.Keywords[0] != "deploy" {
		t.Fatalf("trigger: %+v", sk.Trigger)
	}
	if len(sk.Steps) < 2 {
		t.Fatalf("steps from body paragraphs: %+v", sk.Steps)
	}
}
