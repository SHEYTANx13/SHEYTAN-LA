package toolsets

// v1.8.7 — regression guard: dataAnalysis must stay the selected tool
// for data tasks through the dynamic-toolset machinery. The v1.8.7
// data-analysis upgrade extended the SAME tool (one data authority, no
// duplicate managers); these tests pin the selection contract.

import (
	"slices"
	"testing"
)

func TestDataAnalysisSelectedForDataTaskSignatures(t *testing.T) {
	available := []string{
		"shell", "files", "codeExec", "git", "diff", "memory",
		"json", "dataAnalysis", "archive", "fetch", "browser",
	}

	tasks := []string{
		"analyze the sales csv and tell me the total revenue",
		"profile this dataset and plot a chart of revenue by region",
		"join orders.csv with customers.csv and compute statistics",
		"check the data quality of my exported json file",
		"aggregate the quarterly data and export a csv summary",
	}

	for _, task := range tasks {
		selected := SelectForTask(available, task, 8)
		if !slices.Contains(selected, "dataAnalysis") {
			t.Fatalf("task %q must select dataAnalysis, got %v", task, selected)
		}
	}
}

func TestDataAnalysisIsGroupedAndSignalBacked(t *testing.T) {
	groups, ok := ToolGroups["dataAnalysis"]
	if !ok {
		t.Fatal("dataAnalysis must be mapped in ToolGroups")
	}

	if !slices.Contains(groups, GroupData) {
		t.Fatalf("dataAnalysis must belong to the %q group, got %v", GroupData, groups)
	}

	dataSignals := false
	for _, sig := range TaskSignals {
		if sig.Group == GroupData {
			dataSignals = true
		}
	}
	if !dataSignals {
		t.Fatal("TaskSignals must map data keywords to the data group")
	}
}
