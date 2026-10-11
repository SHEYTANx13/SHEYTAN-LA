package toolsets

import "testing"

func TestSelectForTaskCodingTask(t *testing.T) {
	available := []string{"shell", "files", "codeExec", "webSearch", "git", "browser", "memory", "coding_lab", "diff"}
	got := SelectForTask(available, "fix the build error in main.go and verify with tests", 0)

	for _, want := range []string{"shell", "files", "memory", "coding_lab"} {
		found := false
		for _, name := range got {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("coding task should include %s, got %v", want, got)
		}
	}
	for _, unwanted := range []string{"browser", "webSearch"} {
		for _, name := range got {
			if name == unwanted {
				t.Fatalf("coding task should not include %s, got %v", unwanted, got)
			}
		}
	}
}

func TestSelectForTaskResearchTask(t *testing.T) {
	available := []string{"shell", "files", "webSearch", "research", "browser", "memory"}
	got := SelectForTask(available, "search the web for the latest llama.cpp release notes", 0)

	for _, want := range []string{"webSearch", "research", "memory"} {
		found := false
		for _, name := range got {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("research task should include %s, got %v", want, got)
		}
	}
}

func TestSelectForTaskMaxToolsBound(t *testing.T) {
	available := []string{"shell", "files", "codeExec", "git", "coding_lab", "diff", "memory", "json", "dataAnalysis", "archive"}
	got := SelectForTask(available, "implement and test and verify and document the parser", 4)
	if len(got) > 4 {
		t.Fatalf("max tools bound ignored: %v", got)
	}
	// Core tools survive the cut first.
	for _, core := range []string{"files", "shell"} {
		found := false
		for _, name := range got {
			if name == core {
				found = true
			}
		}
		if !found {
			t.Fatalf("core tool %s must survive the bound, got %v", core, got)
		}
	}
}

func TestSelectForTaskDeterministic(t *testing.T) {
	available := []string{"shell", "files", "git", "memory", "browser"}
	a := SelectForTask(available, "refactor the module", 0)
	b := SelectForTask(available, "refactor the module", 0)
	if len(a) != len(b) {
		t.Fatalf("nondeterministic selection: %v vs %v", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("nondeterministic order: %v vs %v", a, b)
		}
	}
}

func TestNamesForGroup(t *testing.T) {
	available := []string{"shell", "files", "webSearch", "research", "fetch"}
	got := NamesForGroup(available, GroupResearch)
	if len(got) != 3 {
		t.Fatalf("research group = %v, want webSearch+research+fetch", got)
	}
}
