package aicontext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func TestNormalizeToolNamesRemovesEmptyDuplicatesAndSorts(t *testing.T) {
	got := normalizeToolNames([]string{
		" shell ",
		"",
		"memory",
		"shell",
		"  ",
		"research",
		"memory",
	})

	want := []string{
		"memory",
		"research",
		"shell",
	}

	if len(got) != len(want) {
		t.Fatalf(
			"unexpected normalized tool count: got %d want %d (%v)",
			len(got),
			len(want),
			got,
		)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf(
				"unexpected normalized tool[%d]: got %q want %q",
				i,
				got[i],
				want[i],
			)
		}
	}
}

func TestNormalizeToolNamesFallsBackToCompatibilityList(t *testing.T) {
	got := normalizeToolNames(nil)

	if len(got) != len(defaultToolNames) {
		t.Fatalf(
			"unexpected fallback tool count: got %d want %d",
			len(got),
			len(defaultToolNames),
		)
	}

	for _, name := range defaultToolNames {
		found := false

		for _, actual := range got {
			if actual == name {
				found = true
				break
			}
		}

		if !found {
			t.Fatalf(
				"fallback list is missing %q: %v",
				name,
				got,
			)
		}
	}
}

func TestBriefingWithToolsUsesRegisteredToolNames(t *testing.T) {
	cfg := config.Default()

	briefing := BriefingWithTools(
		cfg,
		[]string{
			"research",
			"memory",
			"shell",
		},
	)

	if !strings.Contains(
		briefing,
		"- Tools available: memory, research, shell",
	) {
		t.Fatalf(
			"briefing does not contain normalized registered tool list:\n%s",
			briefing,
		)
	}

	if strings.Contains(
		briefing,
		"webSearch",
	) {
		t.Fatal(
			"briefing leaked compatibility tools when explicit registered tools were supplied",
		)
	}
}

func TestBriefingWithToolsRespectsEnabledToolList(t *testing.T) {
	cfg := config.Default()
	cfg.EnabledTools = []string{
		"research",
		"shell",
	}

	briefing := BriefingWithTools(
		cfg,
		[]string{
			"browser",
			"research",
			"shell",
			"memory",
		},
	)

	if !strings.Contains(
		briefing,
		"- Tools available: research, shell",
	) {
		t.Fatalf(
			"configured tool restriction was not applied:\n%s",
			briefing,
		)
	}

	if !strings.Contains(
		briefing,
		"- Tool selection: the user restricted the toolset",
	) {
		t.Fatal(
			"restricted-tool warning is missing",
		)
	}

	for _, forbidden := range []string{
		"browser",
		"memory",
	} {
		if strings.Contains(
			briefing,
			"- Tools available: "+forbidden,
		) {
			t.Fatalf(
				"forbidden tool %q leaked into tool list",
				forbidden,
			)
		}
	}
}

func TestSystemMessageWithToolsUsesDynamicRegistry(t *testing.T) {
	cfg := config.Default()

	message := SystemMessageWithTools(
		cfg,
		[]string{
			"research",
			"customTool",
		},
	)

	if !strings.Contains(
		message,
		HeaderSentinel,
	) {
		t.Fatal(
			"system message does not contain AI operating instructions",
		)
	}

	if !strings.Contains(
		message,
		"- Tools available: customTool, research",
	) {
		t.Fatalf(
			"system message did not use dynamic registered tools:\n%s",
			message,
		)
	}

	if strings.Contains(
		message,
		"- Tools available: files, shell",
	) {
		t.Fatal(
			"system message leaked compatibility tool list",
		)
	}
}

func TestBriefingWithToolsEmptyRegistryUsesCompatibilityFallback(t *testing.T) {
	cfg := config.Default()

	briefing := BriefingWithTools(cfg, nil)

	if !strings.Contains(
		briefing,
		"- Tools available:",
	) {
		t.Fatal("missing tool list")
	}

	for _, name := range defaultToolNames {
		if !strings.Contains(
			briefing,
			name,
		) {
			t.Fatalf(
				"compatibility fallback is missing %q",
				name,
			)
		}
	}
}

func TestEnsureFileInstallsEmbeddedContext(t *testing.T) {
	dataDir := t.TempDir()

	path, err := EnsureFile(dataDir)
	if err != nil {
		t.Fatalf(
			"EnsureFile returned error: %v",
			err,
		)
	}

	if path != filepath.Join(dataDir, FileName) {
		t.Fatalf(
			"unexpected path: %q",
			path,
		)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf(
			"ReadFile: %v",
			err,
		)
	}

	if string(data) != Embedded() {
		t.Fatal(
			"installed context does not match embedded context",
		)
	}
}

func TestEnsureFilePreservesUserAuthoredContextWithoutMarker(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, FileName)

	const custom = "# Custom AI Context\n\nUser-authored instructions.\n"

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf(
			"MkdirAll: %v",
			err,
		)
	}

	if err := os.WriteFile(
		path,
		[]byte(custom),
		0o644,
	); err != nil {
		t.Fatalf(
			"WriteFile: %v",
			err,
		)
	}

	gotPath, err := EnsureFile(dataDir)
	if err != nil {
		t.Fatalf(
			"EnsureFile returned error: %v",
			err,
		)
	}

	if gotPath != path {
		t.Fatalf(
			"unexpected path: %q",
			gotPath,
		)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf(
			"ReadFile: %v",
			err,
		)
	}

	if string(data) != custom {
		t.Fatalf(
			"user-authored context was overwritten: %q",
			string(data),
		)
	}
}

func TestFileVersionParsesMarker(t *testing.T) {
	body := "<!-- sheytan-context-version: 42 -->\n# Context\n"

	version, ok := fileVersion(body)

	if !ok {
		t.Fatal("expected version marker")
	}

	if version != 42 {
		t.Fatalf(
			"unexpected version: got %d want 42",
			version,
		)
	}
}

func TestFileVersionRejectsMissingOrInvalidMarker(t *testing.T) {
	tests := []string{
		"# no marker",
		"<!-- sheytan-context-version: -->",
		"<!-- sheytan-context-version: abc -->",
		"<!-- sheytan-context-version: 0 -->",
	}

	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			version, ok := fileVersion(body)

			if ok {
				t.Fatalf(
					"expected invalid marker, got version %d",
					version,
				)
			}
		})
	}
}
