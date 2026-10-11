// Package customtools implements SHEYTAN's local custom tool system
// (v1.6.0, spec §8/§9/§17).
//
// A custom tool is a user-defined capability that becomes a FIRST-CLASS
// registered tool in the EXISTING agent tool registry — the same
// /api/tools surface, the same toolset selection semantics, the same
// model-visible schema contract as built-in tools. There is exactly ONE
// tool registry, ONE memory authority, ONE downloader; custom tools are
// data flowing through them, never a parallel architecture.
//
// Design contract:
//
//   - Definitions persist under <DataDir>/custom-tools (the existing
//     application data root — no new storage root).
//   - Every save is ATOMIC (temp file + rename).
//   - A valid tool definition has a valid object-root input schema with
//     only the supported basic types (string, number, integer, boolean,
//     array, object).
//   - Custom tools are DISABLED BY DEFAULT after creation; enabling is
//     an explicit user action.
//   - Execution types: HTTP (HTTPS-only, bounded) and local command
//     (explicit permission, bounded, cancellable, controlled env).
//   - Every execution carries: bounded timeout, bounded output,
//     cancellation, clear failure reporting, and never leaks secret
//     values into model-visible descriptions or normal execution logs.
//   - Name collisions with built-in tools are rejected.
package customtools

import (
        "crypto/rand"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
        "regexp"
        "sort"
        "strings"
        "sync"
        "time"
)

// ---------------------------------------------------------------------------
// Definition model
// ---------------------------------------------------------------------------

// Execution types (spec §8): only what is genuinely reliable.
const (
        ExecHTTP    = "http"
        ExecCommand = "command"
)

// Supported basic parameter types (spec §8). No fake schema capabilities.
var SupportedParamTypes = []string{"string", "number", "integer", "boolean", "array", "object"}

// Permission identifiers — explicit per-tool executable capabilities.
const (
        PermNetwork = "network" // HTTP tool: outbound network access
        PermLocal   = "local"   // command tool: local process execution
)

// Bounds (spec §8 security): every execution is bounded.
const (
        DefaultTimeoutSeconds = 30
        MaxTimeoutSeconds     = 300
        DefaultOutputLimit    = 32 * 1024 // 32 KiB
        MinOutputLimit        = 256
        MaxOutputLimitBytes   = 1024 * 1024 // 1 MiB

        // MaxDefinitionBytes bounds one persisted definition.
        MaxDefinitionBytes = 256 * 1024

        // maxHeaderBytes bounds the HTTP header block a definition may carry.
        maxHeaderBytes = 8 * 1024
)

// Param is one declared input parameter (spec §8: type, required,
// default, enum values, description).
type Param struct {
        Name        string          `json:"name"`
        Type        string          `json:"type"`
        Required    bool            `json:"required"`
        Description string          `json:"description,omitempty"`
        Default     json.RawMessage `json:"default,omitempty"`
        Enum        []string        `json:"enum,omitempty"`
}

// HTTPExec is the HTTP execution configuration.
type HTTPExec struct {
        Method string `json:"method"` // GET/POST/PUT/PATCH/DELETE
        URL    string `json:"url"`    // MUST be https://
        // Headers are static headers. Secret values live ONLY here — never
        // in model-visible descriptions, never in normal execution logs.
        Headers map[string]string `json:"headers,omitempty"`
        // BodyTemplate is a JSON template for POST/PUT/PATCH bodies; "$param"
        // occurrences are replaced by the JSON-encoded value of that
        // parameter.
        BodyTemplate string `json:"bodyTemplate,omitempty"`
}

// CommandExec is the local command/script execution configuration.
type CommandExec struct {
        // Executable is the program to run (resolved through PATH or an
        // absolute path).
        Executable string `json:"executable"`
        // Args are static arguments. "$param" occurrences are substituted
        // with the parameter value.
        Args []string `json:"args,omitempty"`
        // WorkingDir (optional; empty = a private per-run temp dir).
        WorkingDir string `json:"workingDir,omitempty"`
}

// Definition is one persisted custom tool (spec §8 minimum fields).
type Definition struct {
        ID          string       `json:"id"`
        Name        string       `json:"name"`
        ShortDesc   string       `json:"shortDescription"`
        Description string       `json:"description"`
        Params      []Param      `json:"inputParameters"`
        ExecType    string       `json:"executionType"`
        HTTP        *HTTPExec    `json:"http,omitempty"`
        Command     *CommandExec `json:"command,omitempty"`
        Permission  string       `json:"permission"` // network | local
        TimeoutSec  int          `json:"timeoutSeconds"`
        OutputLimit int          `json:"outputLimitBytes"`
        Enabled     bool         `json:"enabled"`
        CreatedAt   time.Time    `json:"createdAt"`
        UpdatedAt   time.Time    `json:"updatedAt"`
}

// ValidationResult reports WHY a definition was rejected.
type ValidationResult struct {
        OK    bool
        Owner string // which field failed
        Error string
}

var namePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{1,39}$`)

// ReservedNames are the built-in registry names a custom tool may never
// shadow (the registry is a single map — collisions would silently
// replace working machinery).
var ReservedNames = map[string]bool{
        "shell": true, "files": true, "codeExec": true, "git": true,
        "browser": true, "dataAnalysis": true, "json": true,
        "archive": true, "fetch": true, "diff": true, "screenshot": true,
        "linux": true, "coding_lab": true, "research": true,
        "memory": true, "pipeline": true, "repo_search": true,
        "webSearch": true,
}

var httpMethods = map[string]bool{
        "GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
}

// Validate enforces the full definition contract (spec §8/§9).
func (d *Definition) Validate() ValidationResult {
        if d == nil {
                return ValidationResult{Owner: "definition", Error: "empty definition"}
        }

        if !namePattern.MatchString(d.Name) {
                return ValidationResult{Owner: "name",
                        Error: "name must start with a letter and contain only letters, digits, - and _ (2-40 chars)"}
        }

        if ReservedNames[d.Name] {
                return ValidationResult{Owner: "name",
                        Error: fmt.Sprintf("%q is a built-in tool name — custom tools may not shadow it", d.Name)}
        }

        if strings.TrimSpace(d.ShortDesc) == "" {
                return ValidationResult{Owner: "shortDescription", Error: "short description required"}
        }

        if len(d.ShortDesc) > 160 {
                return ValidationResult{Owner: "shortDescription", Error: "short description must be at most 160 characters"}
        }

        if strings.TrimSpace(d.Description) == "" {
                return ValidationResult{Owner: "description", Error: "full description required"}
        }

        if len(d.Params) > 16 {
                return ValidationResult{Owner: "inputParameters", Error: "at most 16 input parameters"}
        }

        seen := map[string]bool{}

        for _, p := range d.Params {
                if !namePattern.MatchString(p.Name) {
                        return ValidationResult{Owner: "inputParameters",
                                Error: fmt.Sprintf("parameter name %q is invalid", p.Name)}
                }

                if seen[p.Name] {
                        return ValidationResult{Owner: "inputParameters",
                                Error: fmt.Sprintf("duplicate parameter %q", p.Name)}
                }
                seen[p.Name] = true

                if !supportedType(p.Type) {
                        return ValidationResult{Owner: "inputParameters",
                                Error: fmt.Sprintf("parameter %q has unsupported type %q (supported: %s)",
                                        p.Name, p.Type, strings.Join(SupportedParamTypes, ", "))}
                }

                if len(p.Enum) > 32 {
                        return ValidationResult{Owner: "inputParameters",
                                Error: fmt.Sprintf("parameter %q declares more than 32 enum values", p.Name)}
                }

                if len(p.Default) > 0 {
                        var probe any
                        if err := json.Unmarshal(p.Default, &probe); err != nil {
                                return ValidationResult{Owner: "inputParameters",
                                        Error: fmt.Sprintf("parameter %q default is not valid JSON", p.Name)}
                        }
                }
        }

        switch d.ExecType {
        case ExecHTTP:
                if d.HTTP == nil {
                        return ValidationResult{Owner: "http", Error: "HTTP execution requires its configuration"}
                }
                if !httpMethods[d.HTTP.Method] {
                        return ValidationResult{Owner: "http.method", Error: "method must be GET, POST, PUT, PATCH or DELETE"}
                }
                if !strings.HasPrefix(strings.ToLower(d.HTTP.URL), "https://") {
                        return ValidationResult{Owner: "http.url", Error: "only https:// URLs are allowed"}
                }
                if len(d.HTTP.URL) > 2048 {
                        return ValidationResult{Owner: "http.url", Error: "URL too long (max 2048)"}
                }
                if d.HTTP.Method != "GET" && d.HTTP.BodyTemplate != "" {
                        var probe any
                        if err := json.Unmarshal([]byte(d.HTTP.BodyTemplate), &probe); err != nil {
                                return ValidationResult{Owner: "http.bodyTemplate", Error: "body template must be valid JSON"}
                        }
                }
                if totalHeaderBytes(d.HTTP.Headers) > maxHeaderBytes {
                        return ValidationResult{Owner: "http.headers", Error: "header block too large"}
                }
                if d.Permission != PermNetwork {
                        return ValidationResult{Owner: "permission", Error: "HTTP tools require the explicit network permission"}
                }

        case ExecCommand:
                if d.Command == nil {
                        return ValidationResult{Owner: "command", Error: "command execution requires its configuration"}
                }
                if strings.TrimSpace(d.Command.Executable) == "" {
                        return ValidationResult{Owner: "command.executable", Error: "executable required"}
                }
                if len(d.Command.Executable) > 512 {
                        return ValidationResult{Owner: "command.executable", Error: "executable path too long"}
                }
                if len(d.Command.Args) > 32 {
                        return ValidationResult{Owner: "command.args", Error: "at most 32 arguments"}
                }
                if d.Permission != PermLocal {
                        return ValidationResult{Owner: "permission", Error: "command tools require the explicit local-execution permission"}
                }

        default:
                return ValidationResult{Owner: "executionType",
                        Error: fmt.Sprintf("execution type must be %q or %q", ExecHTTP, ExecCommand)}
        }

        if d.TimeoutSec <= 0 {
                return ValidationResult{Owner: "timeoutSeconds", Error: "timeout must be positive"}
        }
        if d.TimeoutSec > MaxTimeoutSeconds {
                return ValidationResult{Owner: "timeoutSeconds",
                        Error: fmt.Sprintf("timeout must be at most %d seconds", MaxTimeoutSeconds)}
        }

        if d.OutputLimit == 0 {
                return ValidationResult{Owner: "outputLimitBytes",
                        Error: "output size limit required (never unbounded)"}
        }
        if d.OutputLimit < MinOutputLimit {
                return ValidationResult{Owner: "outputLimitBytes",
                        Error: fmt.Sprintf("output limit must be at least %d bytes", MinOutputLimit)}
        }
        if d.OutputLimit > MaxOutputLimitBytes {
                return ValidationResult{Owner: "outputLimitBytes",
                        Error: fmt.Sprintf("output limit must be at most %d bytes", MaxOutputLimitBytes)}
        }

        return ValidationResult{OK: true}
}

func supportedType(t string) bool {
        for _, s := range SupportedParamTypes {
                if s == t {
                        return true
                }
        }
        return false
}

func totalHeaderBytes(h map[string]string) int {
        total := 0
        for k, v := range h {
                total += len(k) + len(v) + 4
        }
        return total
}

// ---------------------------------------------------------------------------
// Store — atomic persistence under <DataDir>/custom-tools
// ---------------------------------------------------------------------------

// Store loads and persists definitions under the existing application
// data root (spec §8: "<DataDir>/custom-tools", no unrelated root).
type Store struct {
        dir string

        mu    sync.Mutex
        cache map[string]*Definition
}

// NewStore opens (and lazily creates) the custom-tools directory.
func NewStore(dataDir string) (*Store, error) {
        dir := filepath.Join(dataDir, "custom-tools")

        if err := os.MkdirAll(dir, 0o755); err != nil {
                return nil, fmt.Errorf("custom tools dir: %w", err)
        }

        s := &Store{dir: dir, cache: map[string]*Definition{}}

        if err := s.reload(); err != nil {
                return nil, err
        }

        return s, nil
}

// Dir exposes the storage root (diagnostics only).
func (s *Store) Dir() string { return s.dir }

func (s *Store) reload() error {
        entries, err := os.ReadDir(s.dir)
        if err != nil {
                return fmt.Errorf("read custom tools dir: %w", err)
        }

        next := map[string]*Definition{}

        for _, e := range entries {
                if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
                        continue
                }

                path := filepath.Join(s.dir, e.Name())

                data, err := os.ReadFile(path)
                if err != nil {
                        continue // unreadable file: skipped honestly, never fatal
                }

                if len(data) > MaxDefinitionBytes {
                        continue
                }

                var def Definition
                if err := json.Unmarshal(data, &def); err != nil {
                        continue // corrupt file: skipped honestly
                }

                // Backward/forward normalization: defaults for legacy fields.
                normalize(&def)

                next[def.ID] = &def
        }

        s.cache = next
        return nil
}

// normalize applies non-destructive defaults to a loaded definition.
// Legacy/hand-edited files load without crashing; a definition that
// fails validation is never EXECUTED (the executor re-validates).
func normalize(d *Definition) {
        if d.TimeoutSec <= 0 {
                d.TimeoutSec = DefaultTimeoutSeconds
        }
        if d.TimeoutSec > MaxTimeoutSeconds {
                d.TimeoutSec = MaxTimeoutSeconds
        }
        if d.OutputLimit == 0 {
                d.OutputLimit = DefaultOutputLimit
        }
        if d.OutputLimit > MaxOutputLimitBytes {
                d.OutputLimit = MaxOutputLimitBytes
        }
        if d.ExecType == "" && d.HTTP != nil {
                d.ExecType = ExecHTTP
        }
        if d.ExecType == "" && d.Command != nil {
                d.ExecType = ExecCommand
        }
}

// List returns the definitions sorted deterministically (enabled first,
// then by name).
//
// v1.7.5: the returned definitions are DEEP copies. The previous shallow
// copy (`cp := *d`) shared Params, per-param Enum/Default, the HTTP config
// (incl. the Headers map) and the Command config (incl. Args) with the
// store's cache — exactly the aliasing shape that made a copied registry
// map unsafe while its values stayed mutable. A consumer marshaling a
// listed definition concurrently with any nested mutation raced; on
// Windows that class of concurrent map read/write is a fatal process
// death.
func (s *Store) List() []*Definition {
        s.mu.Lock()
        defer s.mu.Unlock()

        out := make([]*Definition, 0, len(s.cache))
        for _, d := range s.cache {
                out = append(out, deepCopyDefinition(d))
        }

        sort.Slice(out, func(i, j int) bool {
                if out[i].Enabled != out[j].Enabled {
                        return out[i].Enabled // enabled first
                }
                return out[i].Name < out[j].Name
        })

        return out
}

// Get returns one definition (DEEP copy — no shared mutable state with
// the cache, see the List note) by id.
func (s *Store) Get(id string) (*Definition, bool) {
        s.mu.Lock()
        defer s.mu.Unlock()

        d, ok := s.cache[id]
        if !ok {
                return nil, false
        }

        return deepCopyDefinition(d), true
}

// Save validates and persists one definition ATOMICALLY.
func (s *Store) Save(d *Definition) error {
        if d == nil {
                return fmt.Errorf("nil definition")
        }

        if vr := d.Validate(); !vr.OK {
                return fmt.Errorf("invalid tool (%s): %s", vr.Owner, vr.Error)
        }

        s.mu.Lock()
        defer s.mu.Unlock()

        if d.ID == "" {
                d.ID = newID()
        }

        d.UpdatedAt = time.Now().UTC()

        if d.CreatedAt.IsZero() {
                d.CreatedAt = d.UpdatedAt
        }

        data, err := json.MarshalIndent(d, "", "  ")
        if err != nil {
                return fmt.Errorf("marshal definition: %w", err)
        }

        if len(data) > MaxDefinitionBytes {
                return fmt.Errorf("definition too large (%d bytes, max %d)", len(data), MaxDefinitionBytes)
        }

        // ATOMIC save: temp file in the same directory + rename (spec §8).
        path := s.pathFor(d.ID)
        tmp := path + ".tmp"

        if err := os.WriteFile(tmp, data, 0o644); err != nil {
                return fmt.Errorf("write definition: %w", err)
        }

        if err := os.Rename(tmp, path); err != nil {
                _ = os.Remove(tmp)
                return fmt.Errorf("commit definition: %w", err)
        }

        // v1.7.5: the cache NEVER aliases the caller's nested state — the
        // cache entry is a deep copy, so a later mutation of the caller's
        // definition cannot corrupt what List/Get/NewTool serve.
        s.cache[d.ID] = deepCopyDefinition(d)

        return nil
}

// Delete removes a definition (missing id is a no-op).
func (s *Store) Delete(id string) bool {
        s.mu.Lock()
        defer s.mu.Unlock()

        if _, ok := s.cache[id]; !ok {
                return false
        }

        delete(s.cache, id)

        _ = os.Remove(s.pathFor(id))
        return true
}

var safeID = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func (s *Store) pathFor(id string) string {
        // The id is generated (hex) — but persisted ids are never trusted on
        // a filesystem path: anything path-like is stripped.
        safe := safeID.ReplaceAllString(id, "")
        if safe == "" {
                safe = "tool"
        }
        return filepath.Join(s.dir, safe+".json")
}

func newID() string {
        b := make([]byte, 8)
        if _, err := rand.Read(b); err != nil {
                // Degraded but unique: timestamp-based fallback.
                return fmt.Sprintf("ct-%d", time.Now().UnixNano())
        }
        return "ct-" + hex.EncodeToString(b)
}
