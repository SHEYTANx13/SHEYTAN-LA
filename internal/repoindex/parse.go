package repoindex

// parse.go — language detection and bounded symbol/import extraction.
//
// Parsing philosophy: language-aware analysis where it is RELIABLE and
// already practical (line-oriented, regex-assisted extraction for the
// repository's important languages), conservative fallback everywhere
// else. No heavyweight parser dependency is added merely for feature
// appearance: the index needs WHERE things are, not a compiler-grade
// AST. Every extractor is line-based, byte-bounded and cap-bounded.

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ParseResult is the extracted content metadata of one file.
type ParseResult struct {
	Package string
	Symbols []Symbol
	Imports []string
	Binary  bool
}

// extension -> language map. Bounded, curated set; everything else is
// "other" (content is still indexed by path/size, just not parsed).
var languageByExt = map[string]string{
	".go":   "go",
	".ts":   "typescript",
	".tsx":  "typescript",
	".mts":  "typescript",
	".cts":  "typescript",
	".js":   "javascript",
	".jsx":  "javascript",
	".mjs":  "javascript",
	".cjs":  "javascript",
	".json": "json",
	".c":    "c",
	".h":    "c",
	".cc":   "cpp",
	".cpp":  "cpp",
	".cxx":  "cpp",
	".hpp":  "cpp",
	".hh":   "cpp",
	".hxx":  "cpp",
	".md":   "markdown",
	".yml":  "yaml",
	".yaml": "yaml",
	".toml": "toml",
	".sh":   "shell",
	".bat":  "shell",
	".ps1":  "shell",
	".css":  "css",
	".html": "html",
	".sql":  "sql",
	".py":   "python",
	".rs":   "rust",
	".java": "java",
}

// DetectLanguage maps a file path to its language ("" = unknown).
func DetectLanguage(relPath string) string {
	ext := strings.ToLower(filepath.Ext(relPath))
	if lang, ok := languageByExt[ext]; ok {
		return lang
	}

	// Special file names.
	base := strings.ToLower(filepath.Base(relPath))
	switch base {
	case "makefile", "dockerfile", "gnumakefile":
		return "make"
	case "go.mod":
		return "gomod"
	case ".gitignore", ".gitattributes", ".editorconfig", ".npmrc", ".env":
		return "config"
	}
	return ""
}

// DetectRole classifies a file's role in the repository.
func DetectRole(relPath, language string) string {
	base := filepath.Base(relPath)
	lower := strings.ToLower(relPath)

	// Tests: deterministic naming conventions.
	if language == "go" && strings.HasSuffix(base, "_test.go") {
		return "test"
	}
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"} {
		if strings.HasSuffix(base, ".test"+ext) || strings.HasSuffix(base, ".spec"+ext) {
			return "test"
		}
	}
	if strings.Contains(lower, "/__tests__/") || strings.HasPrefix(lower, "__tests__/") ||
		strings.Contains(lower, "/testdata/") || strings.HasPrefix(lower, "testdata/") {
		return "test"
	}

	switch language {
	case "json", "yaml", "toml", "config", "make", "gomod":
		return "config"
	case "markdown":
		return "doc"
	case "css", "html":
		return "asset"
	}

	// Source files under test trees are test fixtures.
	if strings.HasPrefix(lower, "test/") || strings.Contains(lower, "/test/") ||
		strings.HasPrefix(lower, "tests/") || strings.Contains(lower, "/tests/") {
		return "test"
	}
	return "source"
}

// isBinarySnip reports whether a prefix looks binary (NUL byte or a
// dominated share of control characters) — the same conservative sniff
// the rest of the application uses.
func isBinarySnip(b []byte) bool {
	n := len(b)
	if n == 0 {
		return false
	}
	limit := 8000
	if n < limit {
		limit = n
	}
	control := 0
	for i := 0; i < limit; i++ {
		c := b[i]
		if c == 0 {
			return true
		}
		if c < 32 && c != '\n' && c != '\r' && c != '\t' {
			control++
		}
	}
	return control*100 > limit*5
}

// ParseFile extracts package/symbols/imports from one file within the
// per-file bounds. Failures are silent-by-design at this layer: a file
// that cannot be parsed is indexed without symbols (the record still
// exists; nothing is invented).
func ParseFile(root, rel, language string) ParseResult {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ParseResult{}
	}
	defer f.Close()

	// Binary sniff on the first read chunk.
	sniff := make([]byte, 8000)
	n, _ := f.Read(sniff)
	if isBinarySnip(sniff[:n]) {
		return ParseResult{Binary: true}
	}
	if _, err := f.Seek(0, 0); err != nil {
		return ParseResult{}
	}

	// Bounded line scan: never read beyond maxParseFileBytes.
	reader := bufio.NewReader(f)
	var lines []string
	total := 0
	for total < maxParseFileBytes {
		line, rerr := reader.ReadString('\n')
		total += len(line)
		lines = append(lines, line)
		if rerr != nil {
			break
		}
		if len(lines) >= 24000 {
			break
		}
	}

	switch language {
	case "go":
		return parseGo(lines)
	case "gomod":
		return parseGoMod(lines)
	case "typescript", "javascript":
		return parseTSJS(lines)
	case "c", "cpp":
		return parseC(lines)
	case "json":
		return parseJSONKeys(lines)
	default:
		return ParseResult{}
	}
}

// ---------------------------------------------------------------------------
// go.mod (module path extraction for dependency resolution)
// ---------------------------------------------------------------------------

var reGoModule = regexp.MustCompile(`^module\s+"?([A-Za-z0-9_./-]+)"?`)

func parseGoMod(lines []string) ParseResult {
	out := ParseResult{}
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r\n"))
		if m := reGoModule.FindStringSubmatch(line); m != nil {
			out.Package = m[1]
			break
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Go
// ---------------------------------------------------------------------------

var (
	reGoPackage = regexp.MustCompile(`^package\s+([A-Za-z_][A-Za-z0-9_]*)`)
	reGoFunc    = regexp.MustCompile(`^func\s+(?:\(([^)]*)\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`)
	reGoType    = regexp.MustCompile(`^type\s+([A-Za-z_][A-Za-z0-9_]*)\s+(?:struct|interface|[A-Za-z_\*\[\]\d]+)`)
	reGoStruct  = regexp.MustCompile(`^type\s+([A-Za-z_][A-Za-z0-9_]*)\s+struct`)
	reGoIface   = regexp.MustCompile(`^type\s+([A-Za-z_][A-Za-z0-9_]*)\s+interface`)
	reGoConst   = regexp.MustCompile(`^(?:const|var)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	reGoImport1 = regexp.MustCompile(`^import\s+(?:[A-Za-z_][A-Za-z0-9_]*\s+)?"([^"]+)"`)
	reGoImportB = regexp.MustCompile(`^\s*(?:[A-Za-z_][A-Za-z0-9_]*\s+)?"([^"]+)"`)
)

func parseGo(lines []string) ParseResult {
	out := ParseResult{}
	inImportBlock := false

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r\n")
		trimmed := strings.TrimSpace(line)

		// Package.
		if out.Package == "" {
			if m := reGoPackage.FindStringSubmatch(trimmed); m != nil {
				out.Package = m[1]
			}
		}

		// Imports (single + block form; alias form included).
		if inImportBlock {
			if strings.HasPrefix(trimmed, ")") {
				inImportBlock = false
			} else if m := reGoImportB.FindStringSubmatch(trimmed); m != nil && len(out.Imports) < maxImportsPerFile {
				out.Imports = append(out.Imports, m[1])
			}
		} else if strings.HasPrefix(trimmed, "import") {
			if strings.Contains(trimmed, "(") {
				inImportBlock = true
			} else if m := reGoImport1.FindStringSubmatch(trimmed); m != nil && len(out.Imports) < maxImportsPerFile {
				out.Imports = append(out.Imports, m[1])
			}
		}

		// Symbols (cap-bounded).
		if len(out.Symbols) >= maxSymbolsPerFile {
			continue
		}
		lineNo := i + 1
		if m := reGoFunc.FindStringSubmatch(trimmed); m != nil {
			kind := "func"
			if m[1] != "" {
				kind = "method"
			}
			out.Symbols = append(out.Symbols, Symbol{Name: m[2], Kind: kind, Line: lineNo})
			continue
		}
		if m := reGoStruct.FindStringSubmatch(trimmed); m != nil {
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "struct", Line: lineNo})
			continue
		}
		if m := reGoIface.FindStringSubmatch(trimmed); m != nil {
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "interface", Line: lineNo})
			continue
		}
		if m := reGoType.FindStringSubmatch(trimmed); m != nil {
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "type", Line: lineNo})
			continue
		}
		if m := reGoConst.FindStringSubmatch(trimmed); m != nil {
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "const", Line: lineNo})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// TypeScript / JavaScript
// ---------------------------------------------------------------------------

var (
	reTSImport     = regexp.MustCompile(`^\s*import\s+(?:[^'"]*?\s+from\s+)?["']([^"']+)["']`)
	reTSExportFrom = regexp.MustCompile(`^\s*export\s+(?:[^'"]*?\s+from\s+)?["']([^"']+)["']`)
	reTSRequire    = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.])require\(\s*["']([^"']+)["']\s*\)`)
	reTSDynImport  = regexp.MustCompile(`import\(\s*["']([^"']+)["']\s*\)`)
	reTSFunc       = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][A-Za-z0-9_$]*)`)
	reTSClass      = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	reTSInterface  = regexp.MustCompile(`^\s*(?:export\s+)?interface\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	reTSType       = regexp.MustCompile(`^\s*(?:export\s+)?type\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*[={|]`)
	reTSConst      = regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	reTSEnum       = regexp.MustCompile(`^\s*(?:export\s+)?(?:const\s+)?enum\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
)

func parseTSJS(lines []string) ParseResult {
	out := ParseResult{}

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r\n")
		trimmed := strings.TrimSpace(line)

		// Imports: static import/export-from, require(), dynamic import().
		if len(out.Imports) < maxImportsPerFile {
			if m := reTSImport.FindStringSubmatch(line); m != nil {
				out.Imports = append(out.Imports, m[1])
			} else if m := reTSExportFrom.FindStringSubmatch(line); m != nil {
				out.Imports = append(out.Imports, m[1])
			} else if m := reTSRequire.FindStringSubmatch(line); m != nil {
				out.Imports = append(out.Imports, m[1])
			} else if m := reTSDynImport.FindStringSubmatch(line); m != nil {
				out.Imports = append(out.Imports, m[1])
			}
		}

		// Symbols (cap-bounded).
		if len(out.Symbols) >= maxSymbolsPerFile {
			continue
		}
		lineNo := i + 1
		if m := reTSFunc.FindStringSubmatch(line); m != nil {
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "function", Line: lineNo})
			continue
		}
		if m := reTSClass.FindStringSubmatch(line); m != nil {
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "class", Line: lineNo})
			continue
		}
		if m := reTSInterface.FindStringSubmatch(line); m != nil {
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "interface", Line: lineNo})
			continue
		}
		if m := reTSEnum.FindStringSubmatch(line); m != nil {
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "enum", Line: lineNo})
			continue
		}
		if m := reTSType.FindStringSubmatch(line); m != nil {
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "type", Line: lineNo})
			continue
		}
		if m := reTSConst.FindStringSubmatch(line); m != nil {
			kind := "var"
			if strings.HasPrefix(trimmed, "export ") || strings.HasPrefix(trimmed, "const ") {
				kind = "const"
			}
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: kind, Line: lineNo})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// C / C++
// ---------------------------------------------------------------------------

var (
	reCIncludeLocal = regexp.MustCompile(`^\s*#\s*include\s+"([^"]+)"`)
	reCIncludeSys   = regexp.MustCompile(`^\s*#\s*include\s+<([^>]+)>`)
	reCFunc         = regexp.MustCompile(`^\s*[A-Za-z_][A-Za-z0-9_ \t\*]*[\s\*]([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	reCStruct       = regexp.MustCompile(`^\s*(?:typedef\s+)?(?:class|struct|enum|union)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	reCNamespace    = regexp.MustCompile(`^\s*namespace\s+([A-Za-z_][A-Za-z0-9_]*)`)
	reCDefine       = regexp.MustCompile(`^\s*#\s*define\s+([A-Za-z_][A-Za-z0-9_]*)`)
	reCTypedef      = regexp.MustCompile(`^\s*typedef\s+.*\s([A-Za-z_][A-Za-z0-9_]*)\s*(?:\[[^\]]*\])?\s*;`)
	reCMethodName   = regexp.MustCompile(`^\s*[A-Za-z_][A-Za-z0-9_ \t\*&]*\b([A-Za-z_][A-Za-z0-9_]*)::([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
)

func parseC(lines []string) ParseResult {
	out := ParseResult{}

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r\n")
		trimmed := strings.TrimSpace(line)

		// Includes: quoted = local (resolvable dependency evidence);
		// angled = system (recorded as raw import only).
		if len(out.Imports) < maxImportsPerFile {
			if m := reCIncludeLocal.FindStringSubmatch(line); m != nil {
				out.Imports = append(out.Imports, m[1])
			} else if m := reCIncludeSys.FindStringSubmatch(line); m != nil {
				out.Imports = append(out.Imports, "<"+m[1]+">")
			}
		}

		// Symbols (cap-bounded).
		if len(out.Symbols) >= maxSymbolsPerFile {
			continue
		}
		lineNo := i + 1
		if m := reCStruct.FindStringSubmatch(line); m != nil {
			kind := "struct"
			switch {
			case strings.HasPrefix(trimmed, "class"):
				kind = "class"
			case strings.HasPrefix(trimmed, "enum"):
				kind = "enum"
			case strings.HasPrefix(trimmed, "union"):
				kind = "union"
			}
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: kind, Line: lineNo})
			continue
		}
		if m := reCNamespace.FindStringSubmatch(line); m != nil {
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "namespace", Line: lineNo})
			continue
		}
		if m := reCDefine.FindStringSubmatch(line); m != nil {
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "define", Line: lineNo})
			continue
		}
		if m := reCTypedef.FindStringSubmatch(line); m != nil {
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "type", Line: lineNo})
			continue
		}
		if m := reCMethodName.FindStringSubmatch(line); m != nil {
			// C++ qualified definition (Type::method) — the method
			// name is the last capture.
			out.Symbols = append(out.Symbols, Symbol{Name: m[2], Kind: "method", Line: lineNo})
			continue
		}
		if m := reCFunc.FindStringSubmatch(line); m != nil {
			name := m[1]
			// Filter control-flow keywords that leak through the
			// loose function pattern (if/for/while/switch...).
			switch name {
			case "if", "for", "while", "switch", "return", "catch", "sizeof", "else", "do", "case", "goto":
				continue
			}
			out.Symbols = append(out.Symbols, Symbol{Name: name, Kind: "func", Line: lineNo})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// JSON (configuration): top-level keys become bounded "key" symbols so
// config lookups ("where is modelsDir configured?") work through the
// same symbol search.
// ---------------------------------------------------------------------------

var reJSONKey = regexp.MustCompile(`^\s{0,4}"((?:[^"\\]|\\.)*)"\s*:`)

func parseJSONKeys(lines []string) ParseResult {
	out := ParseResult{}

	depth := 0
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r\n")
		trimmed := strings.TrimSpace(line)

		// Track brace depth conservatively (string contents may
		// contain braces — acceptable for a bounded heuristic).
		opens := strings.Count(trimmed, "{") + strings.Count(trimmed, "[")
		closes := strings.Count(trimmed, "}") + strings.Count(trimmed, "]")

		if depth <= 1 && len(out.Symbols) < maxSymbolsPerFile {
			if m := reJSONKey.FindStringSubmatch(line); m != nil {
				// m[1] is the already-unquoted key body; escape
				// sequences inside keys are rare and harmless.
				out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "key", Line: i + 1})
			}
		}

		depth += opens - closes
		if depth < 0 {
			depth = 0
		}
	}
	return out
}
