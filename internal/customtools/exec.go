// Custom tool executors (v1.6.0, spec §8/§9).
//
// Both executors enforce the same security contract:
//
//   - DISABLED tools are rejected before anything runs
//   - re-validation of the definition before every execution
//   - bounded timeout (context deadline + hard cap)
//   - bounded output (byte cap, honest truncation marker)
//   - cancellation (context + process kill for commands)
//   - controlled environment for command tools (no inherit of the
//     app's secrets; explicit, minimal PATH/HOME/TEMP only)
//   - no secret values in normal execution logs — header values are
//     logged as redacted
//   - clear failure reporting (the model sees the real error class)
package customtools

import (
        "bytes"
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "io"
        "net/http"
        "net/url"
        "os"
        "os/exec"
        "path/filepath"
        "strconv"
        "strings"
        "time"
)

// ErrDisabled is returned when a disabled tool is invoked.
var ErrDisabled = errors.New("custom tool is disabled")

// ErrInvalid is returned when the definition fails re-validation.
var ErrInvalid = errors.New("custom tool definition is invalid")

// ErrSecretLeak guards against model-visible descriptions ever
// containing header/secret material (defense in depth).
var ErrSecretLeak = errors.New("custom tool description contains secret material")

// Tool is the agent-registry adapter: one Definition wrapped as a
// first-class Tool (the SAME interface every built-in tool implements).
type Tool struct {
        def    *Definition
        client *http.Client
}

// NewTool wraps a Definition as a registry Tool. The definition is
// re-validated on EVERY Run — a hand-edited or corrupted file can never
// reach an executor.
//
// v1.7.4 crash-window hardening: the tool takes a DEEP copy of the
// definition (Params slice, per-param Enum/Default, HTTP config incl. the
// Headers map, Command config incl. Args). The previous shallow share
// aliased the store's cached pointers: any caller mutating a definition
// in place while the orchestrator's planning pass introspected
// Name/Description/Parameters could corrupt a concurrently iterated map
// (t.def.HTTP.Headers) — on Windows that is a fatal, unrecoverable
// process death exactly inside the `task classified → tier selected`
// window. The registry-facing tool object is now immutable for the whole
// run; store updates create new definitions, never mutate shared ones.
func NewTool(d *Definition) *Tool {
        return &Tool{def: deepCopyDefinition(d)}
}

// deepCopyDefinition clones a definition with NO shared mutable state
// (slices, maps, nested config pointers). A nil input yields nil so the
// existing nil-handling behavior is preserved.
func deepCopyDefinition(d *Definition) *Definition {
        if d == nil {
                return nil
        }

        out := *d

        if len(d.Params) > 0 {
                out.Params = make([]Param, len(d.Params))
                copy(out.Params, d.Params)

                for i := range out.Params {
                        if len(out.Params[i].Enum) > 0 {
                                out.Params[i].Enum = append([]string(nil), out.Params[i].Enum...)
                        }
                        if len(out.Params[i].Default) > 0 {
                                out.Params[i].Default = append(json.RawMessage(nil), out.Params[i].Default...)
                        }
                }
        }

        if d.HTTP != nil {
                h := *d.HTTP
                if d.HTTP.Headers != nil {
                        h.Headers = make(map[string]string, len(d.HTTP.Headers))
                        for k, v := range d.HTTP.Headers {
                                h.Headers[k] = v
                        }
                }
                out.HTTP = &h
        }

        if d.Command != nil {
                c := *d.Command
                if len(d.Command.Args) > 0 {
                        c.Args = append([]string(nil), d.Command.Args...)
                }
                out.Command = &c
        }

        return &out
}

// SetClientForTest injects an HTTP client (test-only seam: TLS test
// servers need the harness client to verify their self-signed certs).
func (t *Tool) SetClientForTest(c *http.Client) {
        t.client = c
}

// Name implements agent.Tool.
func (t *Tool) Name() string { return t.def.Name }

// ShortDescription implements the optional short-description surface
// /api/tools uses for the Options UI.
func (t *Tool) ShortDescription() string { return t.def.ShortDesc }

// Description implements agent.Tool — the FULL operational spec the
// model sees. It NEVER contains header values (secret or not): only
// header NAMES are referenced, and any attempt to smuggle a header
// value into the description is rejected at run time.
func (t *Tool) Description() string {
        var b strings.Builder

        b.WriteString(t.def.Description)

        b.WriteString("\n\nInput parameters:")

        for _, p := range t.def.Params {
                req := "optional"
                if p.Required {
                        req = "required"
                }

                desc := strings.TrimSpace(p.Description)
                if desc == "" {
                        desc = "(no description)"
                }

                enum := ""
                if len(p.Enum) > 0 {
                        enum = " one of: " + strings.Join(p.Enum, ", ")
                }

                fmt.Fprintf(&b, "\n- %s (%s, %s): %s%s", p.Name, p.Type, req, desc, enum)
        }

        fmt.Fprintf(&b, "\n\nExecution: %s", t.def.ExecType)

        if t.def.ExecType == ExecHTTP && t.def.HTTP != nil {
                fmt.Fprintf(&b, " %s %s", t.def.HTTP.Method, redactedURL(t.def.HTTP.URL))

                if len(t.def.HTTP.Headers) > 0 {
                        names := make([]string, 0, len(t.def.HTTP.Headers))
                        for k := range t.def.HTTP.Headers {
                                names = append(names, k)
                        }
                        // sorted: deterministic model-visible schema
                        for i := 0; i < len(names); i++ {
                                for j := i + 1; j < len(names); j++ {
                                        if names[j] < names[i] {
                                                names[i], names[j] = names[j], names[i]
                                        }
                                }
                        }
                        fmt.Fprintf(&b, " (headers: %s — values never shown)", strings.Join(names, ", "))
                }
        }

        fmt.Fprintf(&b, ". Timeout: %ds. Output capped at %d bytes.", t.def.TimeoutSec, t.def.OutputLimit)

        return b.String()
}

func redactedURL(raw string) string {
        u, err := url.Parse(raw)
        if err != nil {
                return "<invalid url>"
        }
        if u.User != nil {
                u.User = url.User("redacted")
        }
        return u.String()
}

// schemaFor converts the declared params into a JSON Schema object —
// the same contract built-in tools expose via struct tags.
func (t *Tool) Parameters() any {
        props := map[string]any{}
        var required []string

        for _, p := range t.def.Params {
                prop := map[string]any{"type": p.Type}

                if d := strings.TrimSpace(p.Description); d != "" {
                        prop["description"] = d
                }

                if len(p.Enum) > 0 {
                        prop["enum"] = p.Enum
                }

                if len(p.Default) > 0 {
                        var dv any
                        if err := json.Unmarshal(p.Default, &dv); err == nil {
                                prop["default"] = dv
                        }
                }

                props[p.Name] = prop

                if p.Required {
                        required = append(required, p.Name)
                }
        }

        schema := map[string]any{
                "type":       "object",
                "properties": props,
        }

        if len(required) > 0 {
                // sorted: deterministic schema bytes
                sorted := append([]string(nil), required...)
                for i := 0; i < len(sorted); i++ {
                        for j := i + 1; j < len(sorted); j++ {
                                if sorted[j] < sorted[i] {
                                        sorted[i], sorted[j] = sorted[j], sorted[i]
                                }
                        }
                }
                schema["required"] = sorted
        }

        return schema
}

// Run implements agent.Tool: validate → parse args → execute → bound
// the output. Every failure is reported with a clear, honest class.
func (t *Tool) Run(ctx context.Context, args json.RawMessage) (string, error) {
        if !t.def.Enabled {
                return "", fmt.Errorf("%w: %s", ErrDisabled, t.def.Name)
        }

        if vr := t.def.Validate(); !vr.OK {
                return "", fmt.Errorf("%w: %s: %s (%s)", ErrInvalid, t.def.Name, vr.Error, vr.Owner)
        }

        // Defense in depth: no header VALUE may ever appear in the
        // model-visible description.
        desc := t.Description()
        if t.def.HTTP != nil {
                for _, v := range t.def.HTTP.Headers {
                        if v != "" && len(v) >= 8 && strings.Contains(desc, v) {
                                return "", ErrSecretLeak
                        }
                }
        }

        parsed, err := t.parseArgs(args)
        if err != nil {
                return "", err
        }

        runCtx, cancel := context.WithTimeout(ctx, time.Duration(t.def.TimeoutSec)*time.Second)
        defer cancel()

        var out []byte

        switch t.def.ExecType {
        case ExecHTTP:
                out, err = t.runHTTP(runCtx, parsed)
        case ExecCommand:
                out, err = t.runCommand(runCtx, parsed)
        default:
                return "", fmt.Errorf("%w: unknown execution type %q", ErrInvalid, t.def.ExecType)
        }

        if err != nil {
                return "", fmt.Errorf("custom tool %s failed: %w", t.def.Name, err)
        }

        return boundOutput(out, t.def.OutputLimit), nil
}

// parseArgs validates the call arguments against the declared schema.
func (t *Tool) parseArgs(args json.RawMessage) (map[string]any, error) {
        parsed := map[string]any{}

        if len(strings.TrimSpace(string(args))) > 0 {
                if err := json.Unmarshal(args, &parsed); err != nil {
                        return nil, fmt.Errorf("arguments must be a JSON object: %w", err)
                }
        }

        for _, p := range t.def.Params {
                v, ok := parsed[p.Name]

                if !ok || v == nil {
                        if p.Required {
                                if len(p.Default) == 0 {
                                        return nil, fmt.Errorf("missing required parameter %q", p.Name)
                                }
                                var dv any
                                if err := json.Unmarshal(p.Default, &dv); err == nil {
                                        parsed[p.Name] = dv
                                }
                        }
                        continue
                }

                if err := checkType(p.Name, p.Type, v); err != nil {
                        return nil, err
                }
        }

        // Unknown extra keys are dropped (schema-honest, not an error): the
        // model sees exactly the declared surface.
        for k := range parsed {
                known := false
                for _, p := range t.def.Params {
                        if p.Name == k {
                                known = true
                                break
                        }
                }
                if !known {
                        delete(parsed, k)
                }
        }

        return parsed, nil
}

func checkType(name, want string, v any) error {
        switch want {
        case "string":
                if _, ok := v.(string); !ok {
                        return fmt.Errorf("parameter %q must be a string", name)
                }
        case "number":
                if _, ok := v.(float64); !ok {
                        return fmt.Errorf("parameter %q must be a number", name)
                }
        case "integer":
                f, ok := v.(float64)
                if !ok || f != float64(int64(f)) {
                        return fmt.Errorf("parameter %q must be an integer", name)
                }
        case "boolean":
                if _, ok := v.(bool); !ok {
                        return fmt.Errorf("parameter %q must be a boolean", name)
                }
        case "array":
                if _, ok := v.([]any); !ok {
                        return fmt.Errorf("parameter %q must be an array", name)
                }
        case "object":
                if _, ok := v.(map[string]any); !ok {
                        return fmt.Errorf("parameter %q must be an object", name)
                }
        default:
                return fmt.Errorf("parameter %q has unsupported type %q", name, want)
        }
        return nil
}

// runHTTP performs the bounded HTTPS request.
func (t *Tool) runHTTP(ctx context.Context, args map[string]any) ([]byte, error) {
        cfg := t.def.HTTP

        endpoint, err := substituteURL(cfg.URL, args)
        if err != nil {
                return nil, fmt.Errorf("url: %w", err)
        }

        // HTTPS-only enforcement INCLUDING the substituted result.
        if !strings.HasPrefix(strings.ToLower(endpoint), "https://") {
                return nil, fmt.Errorf("only https endpoints are allowed (got %s)", redactedURL(endpoint))
        }

        var body io.Reader

        if cfg.Method != "GET" && strings.TrimSpace(cfg.BodyTemplate) != "" {
                rendered, err := renderBodyTemplate(cfg.BodyTemplate, args)
                if err != nil {
                        return nil, fmt.Errorf("body template: %w", err)
                }
                body = bytes.NewReader(rendered)
        }

        req, err := http.NewRequestWithContext(ctx, cfg.Method, endpoint, body)
        if err != nil {
                return nil, fmt.Errorf("build request: %w", err)
        }

        for k, v := range cfg.Headers {
                req.Header.Set(k, v)
        }

        if body != nil && req.Header.Get("Content-Type") == "" {
                req.Header.Set("Content-Type", "application/json")
        }

        client := t.client
        if client == nil {
                client = &http.Client{Timeout: time.Duration(t.def.TimeoutSec) * time.Second}
        }

        resp, err := client.Do(req)
        if err != nil {
                return nil, fmt.Errorf("request: %w", err)
        }
        defer resp.Body.Close()

        // Bounded read: the output cap is enforced at the READER, not after
        // buffering an unbounded response.
        limited := io.LimitReader(resp.Body, int64(t.def.OutputLimit)+1)
        out, err := io.ReadAll(limited)
        if err != nil {
                return nil, fmt.Errorf("read response: %w", err)
        }

        if resp.StatusCode >= 400 {
                // The model sees the honest status + a bounded body excerpt.
                excerpt := string(boundOutput(out, 512))
                return nil, fmt.Errorf("endpoint returned HTTP %d: %s", resp.StatusCode, excerpt)
        }

        return out, nil
}

// substituteURL replaces $param occurrences in the URL with
// URL-encoded values (injection-safe).
func substituteURL(raw string, args map[string]any) (string, error) {
        if !strings.Contains(raw, "$") {
                return raw, nil
        }

        out := raw

        for name, v := range args {
                token := "$" + name
                if strings.Contains(out, token) {
                        enc := url.QueryEscape(fmt.Sprintf("%v", v))
                        out = strings.ReplaceAll(out, token, enc)
                }
        }

        // Any leftover $token that matches a DECLARED param but had no value
        // would produce a broken URL — reject honestly.
        if idx := strings.IndexByte(out, '$'); idx >= 0 {
                rest := out[idx:]
                if len(rest) > 1 && isParamToken(rest) {
                        return "", fmt.Errorf("unresolved parameter in url near %q", redactedURL(rest))
                }
        }

        return out, nil
}

func isParamToken(s string) bool {
        for i := 1; i < len(s); i++ {
                c := s[i]
                if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
                        return false
                }
        }
        return true
}

// renderBodyTemplate substitutes $param occurrences with JSON-encoded
// values inside a JSON template. Two forms are handled:
//
//      "$param"  → the full JSON encoding of the value (any type)
//       $param   → the JSON string form (safe in any position)
//
// The rendered body must remain valid JSON — verified before use.
func renderBodyTemplate(tpl string, args map[string]any) ([]byte, error) {
        out := tpl

        for name, v := range args {
                token := "$" + name

                if !strings.Contains(out, token) {
                        continue
                }

                enc, err := json.Marshal(v)
                if err != nil {
                        return nil, err
                }

                // Quoted form first: "$param" → the full JSON value.
                out = strings.ReplaceAll(out, `"`+token+`"`, string(enc))

                // Bare form: $param → a JSON string (or the value when scalar).
                if s, ok := v.(string); ok {
                        out = strings.ReplaceAll(out, token, strconv.Quote(s))
                } else {
                        out = strings.ReplaceAll(out, token, string(enc))
                }
        }

        // Verify the rendered body is still valid JSON.
        var probe any
        if err := json.Unmarshal([]byte(out), &probe); err != nil {
                return nil, fmt.Errorf("rendered body is not valid JSON: %w", err)
        }

        return []byte(out), nil
}

// runCommand performs the bounded local execution.
func (t *Tool) runCommand(ctx context.Context, args map[string]any) ([]byte, error) {
        cfg := t.def.Command

        cmdArgs := make([]string, 0, len(cfg.Args))
        for _, a := range cfg.Args {
                if strings.Contains(a, "$") {
                        replaced := a
                        for name, v := range args {
                                replaced = strings.ReplaceAll(replaced, "$"+name, fmt.Sprintf("%v", v))
                        }
                        cmdArgs = append(cmdArgs, replaced)
                        continue
                }
                cmdArgs = append(cmdArgs, a)
        }

        // Process-tree ownership (v1.6.0 repair, spec §4): the command runs
        // under a per-invocation process-tree tracker (a Windows Job Object;
        // a no-op elsewhere). Killing only the direct child — the default
        // exec.CommandContext behavior — leaves grandchildren alive, and a
        // live grandchild keeps the inherited stdout/stderr pipe write-end
        // open, so Wait blocks until the orphan exits on its own. Terminating
        // the tree by ownership makes both the pipes and the processes go
        // away together, promptly, with no timing heuristics.
        tree := newProcessTree()
        defer tree.close()

        cmd := exec.CommandContext(ctx, cfg.Executable, cmdArgs...)

        // Cancellation terminates the COMPLETE tree, not only the direct
        // child, then falls back to the standard direct-process kill (which
        // also reports the already-exited case as os.ErrProcessDone).
        cmd.Cancel = func() error {
                tree.terminate()
                return cmd.Process.Kill()
        }

        // Controlled environment (spec §8): the child NEVER inherits the
        // app's ambient environment (API keys, proxies, secrets). A minimal,
        // explicit env is provided instead.
        cmd.Env = minimalChildEnv()

        workDir := strings.TrimSpace(cfg.WorkingDir)
        if workDir == "" {
                // Private per-run scratch dir: the tool never writes wherever
                // the app happens to run from.
                tmp, err := os.MkdirTemp("", "sheytan-tool-*")
                if err != nil {
                        return nil, fmt.Errorf("scratch dir: %w", err)
                }
                defer os.RemoveAll(tmp)
                workDir = tmp
        }
        cmd.Dir = workDir

        // Bounded capture: combined output capped at the reader.
        var buf bytes.Buffer
        limited := &limitWriter{w: &buf, max: t.def.OutputLimit + 1}
        cmd.Stdout = limited
        cmd.Stderr = limited

        // Start → assign the process to its tree (descendants spawned after
        // the assignment inherit it automatically) → Wait. A process that
        // exits before the assignment is handled cleanly: attach is skipped
        // and Wait still reports the process's own exit status.
        runErr := cmd.Start()
        if runErr == nil {
                tree.attach(cmd.Process)
                runErr = cmd.Wait()
        }

        if runErr != nil {
                // Context state is checked FIRST: a timeout/cancel kills the
                // tree, which surfaces as a signal ExitError — the REAL cause
                // is the deadline, not the exit status.
                if ctx.Err() != nil {
                        if errors.Is(ctx.Err(), context.DeadlineExceeded) {
                                return nil, fmt.Errorf("timed out after %ds", t.def.TimeoutSec)
                        }
                        return nil, fmt.Errorf("canceled")
                }

                var exitErr *exec.ExitError
                if errors.As(runErr, &exitErr) {
                        // Honest failure with the bounded output excerpt.
                        return buf.Bytes(), fmt.Errorf("exit status %d: %s", exitErr.ExitCode(), boundOutput(buf.Bytes(), 512))
                }
                return nil, runErr
        }

        return buf.Bytes(), nil
}

// minimalChildEnv is the explicit, minimal environment for local
// command tools — inheritance-free.
func minimalChildEnv() []string {
        env := []string{}

        if p := os.Getenv("PATH"); p != "" {
                env = append(env, "PATH="+p)
        }
        if h, err := os.UserHomeDir(); err == nil && h != "" {
                env = append(env, "HOME="+h, "USERPROFILE="+h)
        }
        env = append(env,
                "TMPDIR="+os.TempDir(),
                "TMP="+os.TempDir(),
                "TEMP="+os.TempDir(),
                "LANG=C.UTF-8",
        )

        return env
}

type limitWriter struct {
        w   io.Writer
        max int
        n   int
}

func (lw *limitWriter) Write(p []byte) (int, error) {
        if lw.n >= lw.max {
                return len(p), nil // swallow beyond the cap
        }
        room := lw.max - lw.n
        if len(p) > room {
                n, _ := lw.w.Write(p[:room])
                lw.n += n
                return len(p), nil
        }
        n, err := lw.w.Write(p)
        lw.n += n
        return n, err
}

// boundOutput caps output bytes with an honest truncation marker.
func boundOutput(out []byte, limit int) string {
        if len(out) <= limit {
                return strings.TrimSpace(string(out))
        }
        return strings.TrimSpace(string(out[:limit])) + "\n…[output truncated at byte cap]"
}

// SandboxDir returns the per-tool working directory root (diagnostics).
func SandboxDir(dataDir string) string {
        return filepath.Join(dataDir, "custom-tools", "run")
}
