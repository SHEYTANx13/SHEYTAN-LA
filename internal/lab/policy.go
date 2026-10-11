package lab

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	ErrPolicyDenied       = errors.New("lab: command denied by policy")
	ErrNetworkDenied      = errors.New("lab: network access denied by policy")
	ErrDangerousCommand   = errors.New("lab: dangerous command denied by policy")
	ErrCommandTooLong     = errors.New("lab: command exceeds policy length limit")
	ErrInteractiveCommand = errors.New("lab: interactive command denied by policy")
	ErrWorkspaceEscape    = errors.New("lab: command may escape workspace boundary")
)

// Policy controls which commands the autonomous Coding Lab may execute.
//
// Policy is defense-in-depth. It is deliberately conservative and is intended
// to be combined with workspace isolation, filesystem permissions, and the
// process-group controls in internal/proc.
type Policy struct {
	AllowNetwork bool

	MaxCommandLength int

	AllowInteractive bool

	AllowDangerous bool

	AllowWorkspaceEscape bool
}

// DefaultPolicy returns the conservative Coding Lab policy.
func DefaultPolicy() Policy {
	return Policy{
		AllowNetwork:         false,
		MaxCommandLength:     16 * 1024,
		AllowInteractive:     false,
		AllowDangerous:       false,
		AllowWorkspaceEscape: false,
	}
}

// Evaluate checks whether a command is permitted.
func (p Policy) Evaluate(command string) error {
	command = strings.TrimSpace(command)

	if command == "" {
		return ErrCommandEmpty
	}

	if len(command) > p.maxCommandLength() {
		return ErrCommandTooLong
	}

	tokens, err := tokenizeCommand(command)
	if err != nil {
		return fmt.Errorf("%w: invalid command syntax: %v", ErrPolicyDenied, err)
	}

	if !p.AllowDangerous && containsDangerousTokens(tokens) {
		return fmt.Errorf(
			"%w: %s",
			ErrDangerousCommand,
			summarize(command),
		)
	}

	if !p.AllowNetwork && containsNetworkTokens(tokens) {
		return fmt.Errorf(
			"%w: %s",
			ErrNetworkDenied,
			summarize(command),
		)
	}

	if !p.AllowInteractive && containsInteractiveTokens(tokens) {
		return fmt.Errorf(
			"%w: %s",
			ErrInteractiveCommand,
			summarize(command),
		)
	}

	if !p.AllowWorkspaceEscape &&
		containsWorkspaceEscapeTokens(tokens) {
		return fmt.Errorf(
			"%w: %s",
			ErrWorkspaceEscape,
			summarize(command),
		)
	}

	return nil
}

// EvaluateForWorkspace performs policy validation with additional checks for
// an explicitly supplied working directory.
//
// Workspace.PathFor remains the authoritative filesystem boundary.
func (p Policy) EvaluateForWorkspace(
	command string,
	workingDir string,
) error {
	if err := p.Evaluate(command); err != nil {
		return err
	}

	workingDir = strings.TrimSpace(workingDir)

	if workingDir == "" {
		return nil
	}

	normalized := strings.ReplaceAll(
		workingDir,
		"\\",
		string(filepath.Separator),
	)

	cleaned := filepath.Clean(normalized)

	if cleaned == ".." ||
		strings.HasPrefix(
			cleaned,
			".."+string(filepath.Separator),
		) {
		return fmt.Errorf(
			"%w: working directory %q",
			ErrWorkspaceEscape,
			workingDir,
		)
	}

	return nil
}

func (p Policy) maxCommandLength() int {
	if p.MaxCommandLength <= 0 {
		return 16 * 1024
	}

	return p.MaxCommandLength
}

// shellToken is intentionally not a full shell AST. It is a conservative
// lexical representation sufficient for policy decisions.
//
// Separators are retained as tokens so commands chained with &&, ||, ;,
// pipes, subshells and redirections are evaluated command-by-command.
type shellToken struct {
	Value     string
	Separator bool
}

// tokenizeCommand lexes common POSIX/Windows shell syntax while preserving
// quoted words as single tokens.
//
// It rejects unterminated quotes and dangling escapes rather than trying to
// guess what the user intended.
func tokenizeCommand(command string) ([]shellToken, error) {
	var tokens []shellToken

	var word strings.Builder

	inSingle := false
	inDouble := false
	escaped := false

	flushWord := func() {
		if word.Len() == 0 {
			return
		}

		tokens = append(tokens, shellToken{
			Value: word.String(),
		})

		word.Reset()
	}

	appendSeparator := func(value string) {
		flushWord()

		tokens = append(tokens, shellToken{
			Value:     value,
			Separator: true,
		})
	}

	for i := 0; i < len(command); i++ {
		ch := command[i]

		if escaped {
			word.WriteByte(ch)
			escaped = false
			continue
		}

		if inSingle {
			if ch == '\'' {
				inSingle = false
			} else {
				word.WriteByte(ch)
			}

			continue
		}

		if inDouble {
			switch ch {
			case '"':
				inDouble = false

			case '\\':
				escaped = true

			default:
				word.WriteByte(ch)
			}

			continue
		}

		switch ch {
		case '\\':
			escaped = true

		case '\'':
			inSingle = true

		case '"':
			inDouble = true

		case ' ', '\t', '\r', '\n':
			flushWord()

		case '&':
			if i+1 < len(command) &&
				command[i+1] == '&' {
				appendSeparator("&&")
				i++
			} else {
				appendSeparator("&")
			}

		case '|':
			if i+1 < len(command) &&
				command[i+1] == '|' {
				appendSeparator("||")
				i++
			} else {
				appendSeparator("|")
			}

		case ';':
			appendSeparator(";")

		case '(':
			appendSeparator("(")

		case ')':
			appendSeparator(")")

		case '>':
			if i+1 < len(command) &&
				command[i+1] == '>' {
				appendSeparator(">>")
				i++
			} else {
				appendSeparator(">")
			}

		case '<':
			appendSeparator("<")

		default:
			word.WriteByte(ch)
		}
	}

	if escaped {
		return nil, errors.New("dangling escape")
	}

	if inSingle {
		return nil, errors.New("unterminated single quote")
	}

	if inDouble {
		return nil, errors.New("unterminated double quote")
	}

	flushWord()

	return tokens, nil
}

func normalizedToken(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'")
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "\\", "/")

	return value
}

func commandWords(tokens []shellToken) [][]string {
	var result [][]string
	var current []string

	flush := func() {
		if len(current) > 0 {
			result = append(result, current)
			current = nil
		}
	}

	for _, token := range tokens {
		if token.Separator {
			switch token.Value {
			case "&&", "||", ";", "|", "&", "(", ")":
				flush()

			case ">", ">>", "<":
				// Redirection changes where output/input goes but does not
				// start another command.
				continue
			}

			continue
		}

		value := normalizedToken(token.Value)

		if value != "" {
			current = append(current, value)
		}
	}

	flush()

	return result
}

func containsDangerousTokens(tokens []shellToken) bool {
	commands := commandWords(tokens)

	for _, words := range commands {
		if len(words) == 0 {
			continue
		}

		name := filepath.Base(words[0])

		switch name {
		case "shutdown",
			"shutdown.exe",
			"halt",
			"poweroff",
			"reboot",
			"bcdedit",
			"diskpart",
			"mkfs",
			"format",
			"format.com",
			"takeown",
			"cipher",
			"wevtutil",
			"reg",
			"reg.exe",
			"runas",
			"psexec",
			"sudo":
			return true
		}

		if name == "rm" &&
			hasFlag(words[1:], "-r", "-rf", "-fr", "--recursive", "--force") &&
			containsRootTarget(words[1:]) {
			return true
		}

		if name == "rmdir" ||
			name == "rd" ||
			name == "del" {
			if containsForceRecursiveFlag(words[1:]) {
				return true
			}
		}

		if name == "taskkill" ||
			name == "taskkill.exe" {
			if containsToken(words[1:], "/f") &&
				containsTokenPrefix(words[1:], "/im") {
				return true
			}
		}

		if name == "kill" &&
			containsToken(words[1:], "-9") &&
			containsToken(words[1:], "-1") {
			return true
		}

		if name == "killall" {
			return true
		}

		if name == "git" {
			if hasSubcommand(words, "push") ||
				hasSubcommand(words, "reset", "--hard") ||
				hasSubcommand(words, "clean", "-fd") ||
				hasSubcommand(words, "clean", "-xdf") {
				return true
			}
		}
	}

	return false
}

func containsNetworkTokens(tokens []shellToken) bool {
	commands := commandWords(tokens)

	for _, words := range commands {
		if len(words) == 0 {
			continue
		}

		name := filepath.Base(words[0])

		switch name {
		case "curl",
			"curl.exe",
			"wget",
			"wget.exe",
			"irm",
			"iwr",
			"bitsadmin",
			"certutil":
			return true

		case "invoke-webrequest",
			"invoke-restmethod":
			return true
		}

		if name == "git" &&
			(hasSubcommand(words, "clone") ||
				hasSubcommand(words, "fetch") ||
				hasSubcommand(words, "pull") ||
				hasSubcommand(words, "push") ||
				hasSubcommand(words, "remote", "add")) {
			return true
		}

		if name == "npm" &&
			hasSubcommand(words, "install") {
			return true
		}

		if name == "pnpm" &&
			(hasSubcommand(words, "install") ||
				hasSubcommand(words, "add") ||
				hasSubcommand(words, "update")) {
			return true
		}

		if name == "yarn" &&
			(hasSubcommand(words, "install") ||
				hasSubcommand(words, "add")) {
			return true
		}

		if name == "pip" ||
			name == "pip3" {
			if containsToken(words[1:], "install") {
				return true
			}
		}

		if name == "go" &&
			(hasSubcommand(words, "get") ||
				hasSubcommand(words, "install")) {
			return true
		}

		if name == "cargo" &&
			(hasSubcommand(words, "install") ||
				hasSubcommand(words, "add")) {
			return true
		}

		for _, arg := range words {
			if hasNetworkScheme(arg) {
				return true
			}
		}
	}

	return false
}

func containsInteractiveTokens(tokens []shellToken) bool {
	commands := commandWords(tokens)

	for _, words := range commands {
		if len(words) == 0 {
			continue
		}

		name := filepath.Base(words[0])

		switch name {
		case "vim",
			"vi",
			"nvim",
			"nano",
			"emacs",
			"notepad",
			"notepad.exe",
			"telnet",
			"ftp",
			"mysql",
			"psql":
			return true
		}

		if name == "git" {
			// git commit is deliberately allowed. The runner disconnects
			// stdin, making the autonomous form non-interactive. Commands
			// that actually require interactive patch/rebase UI remain blocked.
			if hasSubcommand(words, "add", "-p") ||
				hasSubcommand(words, "rebase", "-i") ||
				hasSubcommand(words, "rebase", "--interactive") {
				return true
			}
		}

		if name == "ssh" ||
			name == "telnet" {
			return true
		}

		if name == "read" {
			return true
		}

		if name == "cmd.exe" &&
			containsToken(words[1:], "/k") {
			return true
		}

		if (name == "powershell" ||
			name == "powershell.exe") &&
			containsToken(words[1:], "-interactive") {
			return true
		}
	}

	return false
}

func containsWorkspaceEscapeTokens(tokens []shellToken) bool {
	commands := commandWords(tokens)

	for _, words := range commands {
		for _, token := range words {
			value := normalizedToken(token)

			if value == "" {
				continue
			}

			if runtime.GOOS == "windows" &&
				isWindowsAbsolutePath(value) {
				return true
			}

			if strings.HasPrefix(value, "/") {
				return true
			}

			if strings.Contains(value, "../") ||
				strings.Contains(value, "/..") {
				return true
			}

			// v1.1.4: home-relative and env-expanded PATH tokens. The
			// lexical policy could not see through shell expansion, so
			// `cat $HOME/secret` and `~/x` passed the check while the real
			// shell resolved them OUTSIDE the workspace. HOME is now also
			// pinned to the workspace in the sanitized environment, so
			// legitimate uses keep working — but an explicit ~/ or $VAR/
			// path token is still rejected outright because its intent is
			// to reference the user profile.
			if isExpandedPathToken(value) {
				return true
			}
		}
	}

	return false
}

// isExpandedPathToken reports whether a token expands to a filesystem path
// outside the workspace: "~/..." or "$VAR/..." / "${VAR}/...". A bare
// "$PATH" or "$VAR" without a slash is left alone (echo $PATH is not an
// escape).
func isExpandedPathToken(value string) bool {
	if strings.HasPrefix(value, "~/") || value == "~" {
		return true
	}

	// windows %VAR% expansion — matched before the separator gate because
	// the tokenizer may swallow the separator after the closing %
	if strings.HasPrefix(value, "%") &&
		strings.Contains(value[1:], "%") {
		return true
	}

	if !strings.Contains(value, "/") && !strings.Contains(value, "\\") {
		return false
	}

	// any $-prefixed variable segment followed by a path separator
	if strings.HasPrefix(value, "$") {
		return true
	}
	if strings.HasPrefix(value, "${") {
		return true
	}
	if strings.HasPrefix(value, "%") {
		return true // %VAR%/path form
	}

	return false
}

func isWindowsAbsolutePath(value string) bool {
	if len(value) < 3 {
		return false
	}

	ch := value[0]

	if !((ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z')) {
		return false
	}

	return value[1] == ':' &&
		(value[2] == '/' || value[2] == '\\')
}

func hasNetworkScheme(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))

	for _, scheme := range []string{
		"http://",
		"https://",
		"ftp://",
		"ssh://",
		"git://",
	} {
		if strings.Contains(value, scheme) {
			return true
		}
	}

	return false
}

func hasFlag(
	words []string,
	flags ...string,
) bool {
	for _, word := range words {
		if containsToken(flags, word) {
			return true
		}
	}

	return false
}

func containsRootTarget(words []string) bool {
	for _, word := range words {
		switch normalizedToken(word) {
		case "/",
			"/*",
			".",
			"./",
			"c:/",
			"d:/",
			"e:/":
			return true
		}
	}

	return false
}

func containsForceRecursiveFlag(words []string) bool {
	return (containsToken(words, "/s") ||
		containsToken(words, "-r") ||
		containsToken(words, "--recursive")) &&
		(containsToken(words, "/q") ||
			containsToken(words, "-f") ||
			containsToken(words, "--force"))
}

func containsToken(words []string, target string) bool {
	target = normalizedToken(target)

	for _, word := range words {
		if normalizedToken(word) == target {
			return true
		}
	}

	return false
}

func containsTokenPrefix(words []string, prefix string) bool {
	prefix = normalizedToken(prefix)

	for _, word := range words {
		value := normalizedToken(word)

		if value == prefix ||
			strings.HasPrefix(value, prefix+"=") {
			return true
		}
	}

	return false
}

func hasSubcommand(
	words []string,
	values ...string,
) bool {
	if len(words) < 2 {
		return false
	}

	target := make([]string, 0, len(values))

	for _, value := range values {
		target = append(target, normalizedToken(value))
	}

	limit := len(words) - len(target)

	for i := 1; i <= limit; i++ {
		match := true

		for j := range target {
			if normalizedToken(words[i+j]) != target[j] {
				match = false
				break
			}
		}

		if match {
			return true
		}
	}

	return false
}

func summarize(command string) string {
	command = strings.TrimSpace(command)

	const max = 120

	if len(command) <= max {
		return command
	}

	return command[:max] + "..."
}
