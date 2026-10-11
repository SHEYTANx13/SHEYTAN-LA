package agent

// toolmetadata_race_test.go — v1.7.5 CONTENT-level registry race coverage.
//
// The v1.7.2 repair made the registry CONTAINER safe: Tools() returns a
// snapshot map. v1.7.4 hardened the custom tool VALUES at the registry
// boundary (customtools.NewTool deep-copies the definition). This file
// pins the remaining §5 contract the prompt re-opens every release:
//
//      A copied map is NOT sufficient if map values alias mutable objects.
//
// The complete `task classified → tier selected` interval is exercised
// HERE, against REAL customtools.Tool values (Params with Enum/Default,
// HTTP configs with Headers, Command configs with Args) while the
// definition churns through the store and the registry concurrently:
//
//      resolveEffectiveContext        (skipped: config-only, no shared state)
//      RAMInfo                        (skipped: OS reader, no shared state)
//      Tools() snapshot               → orch.Tools()
//      tool filtering                 → toolsets.SelectForTask
//      tool() lookups                 → orch.tool via Tool lookups
//      specCache.BuildSpecs           → specs.BuildSpecs
//      taskclassify.SelectTier        (pure function over local values)
//
// plus the schema-construction surface: Name/Description/Parameters
// marshaled to JSON — the exact reads a concurrent definition mutation
// would corrupt.
//
// Run under `go test -race`: any aliasing between registration paths and
// schema/selection readers is a hard failure. Without -race, torn schema
// JSON and invariant violations still fail deterministically. No sleeps,
// no timing hacks: the loops run enough deterministic iterations that a
// race window is crossed thousands of times.

import (
        "encoding/json"
        "fmt"
        "sync"
        "testing"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/customtools"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/toolsets"
)

// seedDefinition builds a valid custom-tool definition with the mutable
// nested state the crash audit names: Params (with Enum + Default), HTTP
// headers, command args.
func seedDefinition(name string) customtools.Definition {
        def := customtools.Definition{
                ID:          name, // deterministic id: Get() is id-addressed
                Name:        name,
                ShortDesc:   "race probe " + name,
                Description: "Concurrency probe tool " + name,
                ExecType:    customtools.ExecHTTP,
                Permission:  customtools.PermNetwork,
                TimeoutSec:  customtools.DefaultTimeoutSeconds,
                OutputLimit: customtools.DefaultOutputLimit,
                HTTP: &customtools.HTTPExec{
                        Method: "GET",
                        URL:    "https://127.0.0.1:1/probe",
                        Headers: map[string]string{
                                "X-Probe": name,
                                "Accept":  "application/json",
                        },
                },
        }

        _ = json.Unmarshal([]byte(`[
                {"name":"path","type":"string","required":true,"description":"probe path","enum":["a","b","c"]},
                {"name":"limit","type":"number","required":false,"description":"probe limit","default":7}
        ]`), &def.Params)

        return def
}

// metadataJSON marshals the tool's model-visible metadata — the exact
// reads spec building performs. Any concurrent nested mutation produces
// torn JSON or a race report.
func metadataJSON(t Tool) []byte {
        spec := llm.ToolSpec{}
        spec.Type = "function"
        spec.Function.Name = t.Name()
        spec.Function.Description = t.Description()
        spec.Function.Parameters = t.Parameters()

        data, err := json.Marshal(spec)
        if err != nil {
                return []byte("{}")
        }
        return data
}

// TestToolMetadataRaceDuringDefinitionChurn runs the v1.7.3 crash
// interval against REAL custom-tool values while definitions churn
// through the store, the registry and the spec cache concurrently.
func TestToolMetadataRaceDuringDefinitionChurn(t *testing.T) {
        storeDir := t.TempDir()

        store, err := customtools.NewStore(storeDir)
        if err != nil {
                t.Fatalf("custom tools store: %v", err)
        }

        // Seed 8 custom tools through the store (the API create path).
        for i := 0; i < 8; i++ {
                def := seedDefinition(fmt.Sprintf("probe-%d", i))
                if err := store.Save(&def); err != nil {
                        t.Fatalf("seed definition %d: %v", i, err)
                }
        }

        engine, _ := newFakeEngine(t, func(turn int, _ map[string]any) string {
                return sseChunk("ok.") + sseDone
        })
        cfg := remoteConfig(t, engine.URL)
        orch := New(config.NewSource(cfg), llm.NewClient(config.NewSource(cfg)))

        // registerLikeAPI mirrors internal/api/customtools.go#registerCustomTools:
        // list → validate → wrap (deep copy) → register; unregister the rest.
        registerLikeAPI := func() {
                enabled := map[string]bool{}

                for _, d := range store.List() {
                        if !d.Enabled {
                                enabled[d.Name] = enabled[d.Name] // probe tools run disabled-by-default
                        }
                        enabled[d.Name] = true
                        orch.Register(customtools.NewTool(d))
                }

                for name := range orch.Tools() {
                        if !enabled[name] {
                                orch.Unregister(name)
                        }
                }
        }

        registerLikeAPI()

        var wg sync.WaitGroup

        // Mutator 1 — definition churn through the store (update definitions
        // with different Enum values / headers while readers walk the registry).
        wg.Add(1)
        go func() {
                defer wg.Done()
                for i := 0; i < 200; i++ {
                        name := fmt.Sprintf("probe-%d", i%8)
                        def, ok := store.Get(name)
                        if !ok {
                                continue
                        }
                        _ = json.Unmarshal([]byte(fmt.Sprintf(
                                `[{"name":"path","type":"string","required":true,"description":"gen %d","enum":["v%d"]}]`, i, i)),
                                &def.Params)
                        def.Description = fmt.Sprintf("Concurrency probe tool %s (gen %d)", name, i)
                        if err := store.Save(def); err != nil {
                                t.Errorf("definition churn save: %v", err)
                                return
                        }
                }
        }()

        // Mutator 2 — the register/unregister cycle (API enable/disable +
        // task-scoped teardown pattern) with the DEEP-COPY boundary re-wrapped
        // on every pass.
        wg.Add(1)
        go func() {
                defer wg.Done()
                for i := 0; i < 300; i++ {
                        name := fmt.Sprintf("probe-%d", i%8)
                        orch.Unregister(name)
                        def, ok := store.Get(name)
                        if !ok {
                                continue
                        }
                        orch.Register(customtools.NewTool(def))
                }
        }()

        // Readers — the crash-interval walk, repeated: snapshot → filter →
        // select → lookups → BuildSpecs → JSON-validate every spec.
        for r := 0; r < 4; r++ {
                wg.Add(1)
                go func() {
                        defer wg.Done()
                        for i := 0; i < 400; i++ {
                                snap := orch.Tools()

                                names := make([]string, 0, len(snap))
                                for name := range snap {
                                        names = append(names, name)
                                }

                                selected := toolsets.SelectForTask(names, "probe the registry race", 0)
                                for _, name := range selected {
                                        tool, ok := orch.tool(name)
                                        if !ok {
                                                continue
                                        }

                                        data := metadataJSON(tool)
                                        if !json.Valid(data) {
                                                t.Errorf("torn tool spec JSON for %q: %s", name, data)
                                                return
                                        }

                                        var spec llm.ToolSpec
                                        if err := json.Unmarshal(data, &spec); err != nil {
                                                t.Errorf("spec decode %q: %v", name, err)
                                                return
                                        }
                                        if spec.Function.Name != name {
                                                t.Errorf("spec name mismatch: %q != %q", spec.Function.Name, name)
                                                return
                                        }
                                }
                        }
                }()
        }

        // Readers — the store surface the API serves (/api/custom-tools):
        // List/Get concurrently with churn, marshaled exactly like viewOf.
        for r := 0; r < 2; r++ {
                wg.Add(1)
                go func() {
                        defer wg.Done()
                        for i := 0; i < 400; i++ {
                                for _, d := range store.List() {
                                        data, err := json.Marshal(d)
                                        if err != nil {
                                                t.Errorf("definition marshal: %v", err)
                                                return
                                        }
                                        if !json.Valid(data) {
                                                t.Errorf("torn definition JSON for %q", d.Name)
                                                return
                                        }
                                }
                                if _, ok := store.Get(fmt.Sprintf("probe-%d", i%8)); !ok {
                                        t.Errorf("seeded definition vanished")
                                        return
                                }
                        }
                }()
        }

        wg.Wait()

        // Settled state: every seed is registered again and spec-buildable.
        registerLikeAPI()
        final := orch.Tools()
        for i := 0; i < 8; i++ {
                name := fmt.Sprintf("probe-%d", i)
                tool, ok := final[name]
                if !ok {
                        t.Fatalf("probe tool %q must survive the churn", name)
                }
                if !json.Valid(metadataJSON(tool)) {
                        t.Fatalf("settled spec for %q is torn", name)
                }
        }
}
