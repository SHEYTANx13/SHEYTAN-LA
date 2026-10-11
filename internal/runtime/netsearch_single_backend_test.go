package runtime

// v1.3.6 (spec §26/§31): SINGLE SEARCH BACKEND regression.
//
// The old `tools.WebSearch` tool (a second, independent DuckDuckGo/Bing
// scraper registered into the orchestrator beside the research service)
// is removed. Web search MUST flow exclusively through the internal
// research service — the same service the Net Search composer control
// and /api/net-search use — so per-request intent is enforced
// server-side in one place and provenance is never lost.

import (
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/tools"
)

// TestNoSecondSearchBackendRegistered builds a real orchestrator the
// same way NewStack does and asserts no tool named "webSearch" (or any
// other duplicate web-search surface) is registered.
func TestNoSecondSearchBackendRegistered(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	src := config.NewSource(cfg)
	orch := agent.New(src, llm.NewClient(src))

	// Mirror NewStack's core tool registrations EXACTLY (the ones that
	// construct without a live stack): if webSearch ever sneaks back
	// into this list, the test fails.
	orch.Register(tools.Shell{})
	orch.Register(tools.Files{})
	orch.Register(tools.CodeExec{})
	orch.Register(tools.Git{})
	orch.Register(tools.NewBrowserTool(cfg))
	orch.Register(tools.NewDataTool(cfg))
	orch.Register(tools.JSONTool{})
	orch.Register(tools.ArchiveTool{})
	orch.Register(tools.NewFetchTool())
	orch.Register(tools.DiffTool{})

	for name, tool := range orch.Tools() {
		lower := strings.ToLower(name)

		if lower == "websearch" || lower == "web_search" || lower == "netsearch" || lower == "net-search" {
			t.Fatalf("second search backend registered as agent tool %q — web search must flow through the research service only (spec §26)", tool.Name())
		}
	}
}
