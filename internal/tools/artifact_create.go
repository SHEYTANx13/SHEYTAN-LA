// artifact_create.go — v1.7.0: the agent-facing VALIDATED artifact
// creation operation.
//
// Contract (mirrors internal/artifacts): task-scoped, path-safe,
// bounded, ATOMIC, registered in the artifact tracker, linked to the
// current task/run, and never silently written outside the task's
// artifact root. Markdown, plain text, code, JSON, CSV, HTML, SVG,
// images (already-encoded), archives (already-packed bytes) and reports
// are all first-class — the kind is derived from the filename, honestly.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/artifacts"
)

// ArtifactRegistry is the registry seam the runtime installs (the ONE
// artifacts.TaskRegistry).
var ArtifactRegistry *artifacts.TaskRegistry

// ArtifactCreate is the registry tool.
type ArtifactCreate struct{}

func (ArtifactCreate) Name() string { return "artifact_create" }

func (ArtifactCreate) ShortDescription() string {
	return "Create a versioned task artifact (report, markdown, code, data)"
}

func (ArtifactCreate) Description() string {
	return `Create a durable ARTIFACT inside the current task's artifact space and register it with full provenance (task, run, source tool, version, hash).

Use it whenever a task produces a deliverable the user should keep: a markdown report, a code file, a JSON/CSV dataset, an SVG chart, an HTML page, notes. Markdown is first-class: it renders in the artifact viewer with code blocks and tables.

The artifact is written ATOMICALLY, path-safely and BOUNDED (max 8 MiB). Re-creating the same filename in the same task creates a NEW VERSION — history is preserved, nothing is silently overwritten.

Available ONLY inside a task run (scheduled or manual task); chat without a task context refuses it.`
}

func (ArtifactCreate) Parameters() any {
	return struct {
		Filename string `json:"filename"`
		Content  string `json:"content"`
		Title    string `json:"title,omitempty"`
	}{}
}

func (ArtifactCreate) Run(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Filename string `json:"filename"`
		Content  string `json:"content"`
		Title    string `json:"title"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("artifact_create arguments: %w", err)
	}

	if ArtifactRegistry == nil {
		return "", fmt.Errorf("artifact registry unavailable")
	}

	tc := CurrentTaskContext()
	if tc == nil {
		return "", fmt.Errorf("artifact_create is task-scoped — no task context is installed (use it inside a task run)")
	}

	meta, err := ArtifactRegistry.Create(artifacts.CreateRequest{
		TaskID:   tc.TaskID,
		RunID:    tc.RunID,
		Source:   "artifact_create",
		Title:    strings.TrimSpace(p.Title),
		Filename: p.Filename,
		Content:  []byte(p.Content),
	})
	if err != nil {
		return "", err
	}

	out := map[string]any{
		"id":      meta.ID,
		"path":    meta.Path,
		"kind":    meta.Kind,
		"version": meta.Version,
		"size":    meta.Size,
		"hash":    meta.Hash,
	}

	b, err := json.Marshal(out)
	if err != nil {
		return meta.Path, nil
	}
	return string(b), nil
}
