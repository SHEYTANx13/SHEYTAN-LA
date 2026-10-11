// Custom tools core tests (v1.6.0, spec §9):
//
//      persistence · schema validation · execution · timeout · cancellation
//      · output limits · disabled rejection · invalid rejection
package customtools

import (
        "context"
        "encoding/json"
        "fmt"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "runtime"
        "strings"
        "testing"
        "time"
)

func validCommandDef(t *testing.T) *Definition {
        t.Helper()

        return &Definition{
                Name:        "greeter",
                ShortDesc:   "Greets a person",
                Description: "Greets a person by name using the local echo command.",
                Params: []Param{
                        {Name: "name", Type: "string", Required: true, Description: "who to greet"},
                },
                ExecType:    ExecCommand,
                Command:     &CommandExec{Executable: echoBinary(), Args: echoArgs("hello", "name")},
                Permission:  PermLocal,
                TimeoutSec:  10,
                OutputLimit: 4096,
                Enabled:     true,
        }
}

func echoBinary() string {
        if runtime.GOOS == "windows" {
                return "cmd"
        }
        return "echo"
}

func echoArgs(prefix, param string) []string {
        if runtime.GOOS == "windows" {
                return []string{"/C", "echo", prefix, "$" + param}
        }
        return []string{prefix, "$" + param}
}

// --- persistence ---------------------------------------------------------

func TestStorePersistsAndReloads(t *testing.T) {
        dir := t.TempDir()

        store, err := NewStore(dir)
        if err != nil {
                t.Fatalf("NewStore: %v", err)
        }

        def := validCommandDef(t)
        if err := store.Save(def); err != nil {
                t.Fatalf("Save: %v", err)
        }

        if def.ID == "" {
                t.Fatal("Save must assign an id")
        }

        // Storage root: <DataDir>/custom-tools (spec §8 — the existing data
        // root, no new unrelated root).
        if got := store.Dir(); got != filepath.Join(dir, "custom-tools") {
                t.Fatalf("store dir = %q, want %q", got, filepath.Join(dir, "custom-tools"))
        }

        if _, err := os.Stat(filepath.Join(store.Dir(), def.ID+".json")); err != nil {
                t.Fatalf("definition file missing: %v", err)
        }

        // A SECOND store over the same dir reloads the persisted definition.
        store2, err := NewStore(dir)
        if err != nil {
                t.Fatalf("NewStore #2: %v", err)
        }

        loaded, ok := store2.Get(def.ID)
        if !ok {
                t.Fatal("definition did not survive a reload")
        }

        if loaded.Name != def.Name || loaded.ExecType != ExecCommand {
                t.Fatalf("reloaded definition mismatch: %+v", loaded)
        }
}

func TestStoreAtomicSaveLeavesNoTempFiles(t *testing.T) {
        dir := t.TempDir()

        store, _ := NewStore(dir)

        if err := store.Save(validCommandDef(t)); err != nil {
                t.Fatalf("Save: %v", err)
        }

        entries, _ := os.ReadDir(store.Dir())
        for _, e := range entries {
                if strings.HasSuffix(e.Name(), ".tmp") {
                        t.Fatalf("atomic save leaked a temp file: %s", e.Name())
                }
        }
}

func TestStoreDeleteRemovesDefinition(t *testing.T) {
        dir := t.TempDir()

        store, _ := NewStore(dir)

        def := validCommandDef(t)
        _ = store.Save(def)

        if !store.Delete(def.ID) {
                t.Fatal("Delete must report true for an existing id")
        }

        if _, ok := store.Get(def.ID); ok {
                t.Fatal("definition still readable after delete")
        }

        if _, err := os.Stat(filepath.Join(store.Dir(), def.ID+".json")); err == nil {
                t.Fatal("definition file still on disk after delete")
        }

        if store.Delete(def.ID) {
                t.Fatal("second Delete must report false")
        }
}

// --- schema validation ---------------------------------------------------

func TestValidationRejectsInvalidDefinitions(t *testing.T) {
        cases := []struct {
                name string
                mut  func(*Definition)
        }{
                {"bad name", func(d *Definition) { d.Name = "1bad name!" }},
                {"reserved name", func(d *Definition) { d.Name = "shell" }},
                {"empty short desc", func(d *Definition) { d.ShortDesc = "" }},
                {"empty full desc", func(d *Definition) { d.Description = "  " }},
                {"unsupported param type", func(d *Definition) {
                        d.Params = []Param{{Name: "x", Type: "function"}}
                }},
                {"duplicate params", func(d *Definition) {
                        d.Params = []Param{{Name: "x", Type: "string"}, {Name: "x", Type: "string"}}
                }},
                {"missing command config", func(d *Definition) { d.Command = nil }},
                {"wrong permission", func(d *Definition) { d.Permission = PermNetwork }},
                {"zero timeout", func(d *Definition) { d.TimeoutSec = 0 }},
                {"oversized timeout", func(d *Definition) { d.TimeoutSec = MaxTimeoutSeconds + 1 }},
                {"zero output limit", func(d *Definition) { d.OutputLimit = 0 }},
                {"undersized output limit", func(d *Definition) { d.OutputLimit = 8 }},
                {"unknown exec type", func(d *Definition) { d.ExecType = "grpc" }},
                {"empty executable", func(d *Definition) { d.Command.Executable = " " }},
        }

        for _, tc := range cases {
                if tc.mut == nil {
                        continue
                }

                t.Run(tc.name, func(t *testing.T) {
                        def := validCommandDef(t)
                        tc.mut(def)

                        if vr := def.Validate(); vr.OK {
                                t.Fatalf("validation must reject: %s", tc.name)
                        }
                })
        }
}

// definition0 placeholder removed — the table now carries only real
// mutations.
func TestValidationRejectsHTTPSpecificViolations(t *testing.T) {
        base := func() *Definition {
                return &Definition{
                        Name:        "fetcher",
                        ShortDesc:   "Fetches a value",
                        Description: "Fetches a value from an endpoint.",
                        ExecType:    ExecHTTP,
                        HTTP:        &HTTPExec{Method: "GET", URL: "https://example.com/api"},
                        Permission:  PermNetwork,
                        TimeoutSec:  10,
                        OutputLimit: 4096,
                }
        }

        if vr := base().Validate(); !vr.OK {
                t.Fatalf("baseline HTTP definition must validate: %+v", vr)
        }

        http := base()
        http.HTTP.URL = "http://example.com/api"
        if vr := http.Validate(); vr.OK {
                t.Fatal("plain http:// must be rejected (HTTPS only)")
        }

        http = base()
        http.HTTP.Method = "TRACE"
        if vr := http.Validate(); vr.OK {
                t.Fatal("TRACE method must be rejected")
        }

        http = base()
        http.HTTP.Method = "POST"
        http.HTTP.BodyTemplate = `{not json`
        if vr := http.Validate(); vr.OK {
                t.Fatal("invalid JSON body template must be rejected")
        }

        http = base()
        http.Permission = PermLocal
        if vr := http.Validate(); vr.OK {
                t.Fatal("HTTP tool without network permission must be rejected")
        }

        cmd := validCommandDef(t)
        cmd.Permission = PermNetwork
        if vr := cmd.Validate(); vr.OK {
                t.Fatal("command tool without local permission must be rejected")
        }
}

// --- execution -----------------------------------------------------------

func TestCommandToolExecutesWithSubstitution(t *testing.T) {
        def := validCommandDef(t)
        tool := NewTool(def)

        out, err := tool.Run(context.Background(), json.RawMessage(`{"name":"world"}`))
        if err != nil {
                t.Fatalf("Run: %v", err)
        }

        if !strings.Contains(strings.ToLower(out), "hello") ||
                !strings.Contains(strings.ToLower(out), "world") {
                t.Fatalf("substituted output missing: %q", out)
        }
}

func TestHTTPToolExecutesAgainstTestServer(t *testing.T) {
        var gotAuth, gotPath, gotBody string

        // The executor enforces https:// — an httptest TLS server satisfies it.
        server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                gotAuth = r.Header.Get("Authorization")
                gotPath = r.URL.Path
                buf := make([]byte, 64)
                n, _ := r.Body.Read(buf)
                gotBody = string(buf[:n])

                _, _ = w.Write([]byte(`{"answer":42}`))
        }))
        t.Cleanup(server.Close)

        def := &Definition{
                Name:        "answerService",
                ShortDesc:   "Gets the answer",
                Description: "Calls the answer endpoint with a query.",
                Params: []Param{
                        {Name: "query", Type: "string", Required: true},
                },
                ExecType: ExecHTTP,
                HTTP: &HTTPExec{
                        Method:  "POST",
                        URL:     server.URL + "/answers/$query",
                        Headers: map[string]string{"Authorization": "Bearer sekrit-value-123"},
                        BodyTemplate: `{"q":"$query"}`,
                },
                Permission:  PermNetwork,
                TimeoutSec:  10,
                OutputLimit: 4096,
                Enabled:     true,
        }

        tool := NewTool(def)

        // The test TLS certificate is self-signed: the tool's transport must
        // verify by default; inject the test client for the harness.
        tool.SetClientForTest(server.Client())

        out, err := tool.Run(context.Background(), json.RawMessage(`{"query":"life"}`))
        if err != nil {
                t.Fatalf("Run: %v", err)
        }

        if !strings.Contains(out, "42") {
                t.Fatalf("answer missing from output: %q", out)
        }

        if gotAuth != "Bearer sekrit-value-123" {
                t.Fatalf("header not sent: %q", gotAuth)
        }

        if !strings.Contains(gotPath, "life") {
                t.Fatalf("URL substitution missing: %q", gotPath)
        }

        if !strings.Contains(gotBody, "life") {
                t.Fatalf("body substitution missing: %q", gotBody)
        }

        // The model-visible description NEVER contains the secret.
        if strings.Contains(tool.Description(), "sekrit-value-123") {
                t.Fatal("SECRET LEAK: header value present in the model-visible description")
        }
}

// --- disabled / invalid rejection ----------------------------------------

func TestDisabledToolIsRejected(t *testing.T) {
        def := validCommandDef(t)
        def.Enabled = false

        tool := NewTool(def)

        _, err := tool.Run(context.Background(), json.RawMessage(`{"name":"x"}`))
        if err == nil {
                t.Fatal("disabled tool must be rejected")
        }

        if !strings.Contains(err.Error(), "disabled") {
                t.Fatalf("rejection must say disabled, got: %v", err)
        }
}

func TestInvalidDefinitionIsRejectedAtRunTime(t *testing.T) {
        def := validCommandDef(t)
        def.Enabled = true

        tool := NewTool(def)

        // Simulate a hand-edited/corrupted definition AFTER construction.
        tool.def.ExecType = "grpc"

        _, err := tool.Run(context.Background(), json.RawMessage(`{}`))
        if err == nil {
                t.Fatal("invalid definition must be rejected at run time")
        }
}

func TestMissingRequiredParameterIsRejected(t *testing.T) {
        def := validCommandDef(t)

        tool := NewTool(def)

        _, err := tool.Run(context.Background(), json.RawMessage(`{}`))
        if err == nil {
                t.Fatal("missing required parameter must be rejected")
        }
}

// --- timeout / cancellation ----------------------------------------------

func TestCommandTimeoutIsEnforced(t *testing.T) {
        def := &Definition{
                Name:        "sleeper",
                ShortDesc:   "Sleeps",
                Description: "Sleeps for a while.",
                ExecType:    ExecCommand,
                Command:     sleepCommand(30),
                Permission:  PermLocal,
                TimeoutSec:  1,
                OutputLimit: 1024,
                Enabled:     true,
        }

        tool := NewTool(def)

        start := time.Now()

        _, err := tool.Run(context.Background(), json.RawMessage(`{}`))
        if err == nil {
                t.Fatal("timeout must produce an error")
        }

        if elapsed := time.Since(start); elapsed > 10*time.Second {
                t.Fatalf("timeout not enforced promptly: %v", elapsed)
        }

        if !strings.Contains(err.Error(), "timed out") {
                t.Fatalf("error must report the timeout, got: %v", err)
        }
}

func TestContextCancellationIsHonored(t *testing.T) {
        def := &Definition{
                Name:        "sleeper2",
                ShortDesc:   "Sleeps",
                Description: "Sleeps for a while.",
                ExecType:    ExecCommand,
                Command:     sleepCommand(30),
                Permission:  PermLocal,
                TimeoutSec:  60, // tool timeout long; the CALLER cancels first
                OutputLimit: 1024,
                Enabled:     true,
        }

        tool := NewTool(def)

        ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
        defer cancel()

        start := time.Now()

        _, err := tool.Run(ctx, json.RawMessage(`{}`))
        if err == nil {
                t.Fatal("cancellation must produce an error")
        }

        if elapsed := time.Since(start); elapsed > 10*time.Second {
                t.Fatalf("cancellation not honored promptly: %v", elapsed)
        }
}

func sleepCommand(seconds int) *CommandExec {
        if runtime.GOOS == "windows" {
                return &CommandExec{Executable: "cmd", Args: []string{"/C", "ping", "-n", fmt.Sprintf("%d", seconds+1), "127.0.0.1"}}
        }
        return &CommandExec{Executable: "sleep", Args: []string{fmt.Sprintf("%d", seconds)}}
}

// --- output limits --------------------------------------------------------

// fileReaderCommand returns a command that prints the contents of a file
// in the current working directory: `cmd /C type <file>` on Windows,
// `cat <file>` elsewhere. The large payload lives in the FILE — never in
// argv — so the fixture stays far below Windows command-line length
// limits while still producing real stdout through the real executor.
func fileReaderCommand(filename string) (string, []string) {
        if runtime.GOOS == "windows" {
                return "cmd", []string{"/C", "type", filename}
        }
        return "cat", []string{filename}
}

func TestOutputIsCapped(t *testing.T) {
        // Stage the large payload as a file in a private working directory.
        dir := t.TempDir()

        big := strings.Repeat("x", 8192)
        if err := os.WriteFile(filepath.Join(dir, "large-output.txt"), []byte(big+"\n"), 0o644); err != nil {
                t.Fatalf("stage large fixture file: %v", err)
        }

        bin, binArgs := fileReaderCommand("large-output.txt")

        def := &Definition{
                Name:        "chatty",
                ShortDesc:   "Prints a lot",
                Description: "Prints a lot of text.",
                ExecType:    ExecCommand,
                Command:     &CommandExec{Executable: bin, Args: binArgs, WorkingDir: dir},
                Permission:  PermLocal,
                TimeoutSec:  10,
                OutputLimit: 1024,
                Enabled:     true,
        }

        tool := NewTool(def)

        out, err := tool.Run(context.Background(), json.RawMessage(`{}`))
        if err != nil {
                t.Fatalf("Run: %v", err)
        }

        if len(out) > 1100 {
                t.Fatalf("output not capped: %d bytes", len(out))
        }

        if !strings.Contains(out, "truncated") {
                t.Fatalf("truncation marker missing: %q", tail(out, 80))
        }
}

func tail(s string, n int) string {
        if len(s) > n {
                return s[len(s)-n:]
        }
        return s
}

// --- schema export --------------------------------------------------------

func TestParametersExportsObjectRootSchema(t *testing.T) {
        def := validCommandDef(t)

        tool := NewTool(def)

        schema := tool.Parameters().(map[string]any)

        if schema["type"] != "object" {
                t.Fatalf("schema root must be an object, got %v", schema["type"])
        }

        props := schema["properties"].(map[string]any)
        nameProp := props["name"].(map[string]any)

        if nameProp["type"] != "string" {
                t.Fatalf("param type mismatch: %v", nameProp)
        }

        req, ok := schema["required"].([]string)
        if !ok || len(req) != 1 || req[0] != "name" {
                t.Fatalf("required list mismatch: %v", schema["required"])
        }
}
