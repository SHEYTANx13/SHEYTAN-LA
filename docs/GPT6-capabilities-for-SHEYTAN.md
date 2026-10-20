# GPT-6 Capabilities for SHEYTAN-LA

## Future-proof capability architecture and implementation roadmap

**Repository:** https://github.com/SHEYTANx13/SHEYTAN-LA\
**Replacement path:** `docs/GPT6-capabilities-for-SHEYTAN.md`\
**Document status:** Engineering specification and staged roadmap; not
evidence that any proposed feature is implemented\
**Review date:** 2026-10-20\
**Primary implementation language:** Go\
**Desktop direction:** Native Qt Quick/QML, subject to the repository's
current architecture document\
**Project posture:** Windows-first, local-first, evidence-driven\
**Project link:** https://linktr.ee/Parsaetak

> **The model proposes. The tools execute. The laboratory verifies.**

------------------------------------------------------------------------

## 1. Purpose and scope

This document defines how SHEYTAN-LA can progressively deliver useful
user-facing outcomes associated with modern AI assistants while
preserving its own architecture, privacy posture, local-first operation,
and verifiable engineering standards.

It is a long-lived engineering specification, not a promise to reproduce
OpenAI's private implementation, model weights, infrastructure,
proprietary interfaces, or exact performance. "GPT-6 capabilities"
refers to the motivating user-facing capability set---not an assertion
that a local model becomes GPT-6, that every feature is available
offline, or that a feature works merely because an API or UI control
exists.

The goal is to make the application increasingly capable at: -
conversation, instruction following, writing, summarization,
translation, and structured outputs; - reliable tool use, multi-step
plans, coding workflows, and bounded delegation; - web search and
multi-source research with verifiable citations; - file ingestion,
retrieval, data analysis, spreadsheets, charts, and artifact
generation; - image understanding and optional image generation; - audio
transcription, speech generation, and optional real-time voice; -
context-aware memory and long-running projects; - scheduling, resumable
workflows, and user-visible progress; - interactive, accessible UI and
truthful capability self-reporting; - privacy, permissions, safe
execution, testing, and cross-platform reliability.

Each feature must be mapped to existing code before implementation. This
plan is deliberately additive: preserve the current Go orchestration
authority, current tool registry, existing run lifecycle,
approval/policy boundary, artifact authority, memory authority,
scheduler, and native UI architecture. Do not create competing
authorities to make a feature appear easier.

## 2. What this document does and does not claim

### It does claim

-   It provides a capability taxonomy, architectural rules, staged
    implementation order, security requirements, and acceptance
    criteria.
-   It distinguishes features implemented in Go from capabilities
    supplied by model backends, optional providers, or UI clients.
-   It defines evidence required before a capability may be advertised
    as available or verified.
-   It provides a reusable checklist for future capabilities beyond
    those listed here.

### It does not claim

-   That all listed capabilities exist in the current checkout.
-   That a local model has the reasoning or multimodal quality of a
    hosted frontier model.
-   That "local-first" means every feature is offline. Web search and
    remote model APIs require network access and explicit disclosure.
-   That compiling, registering, rendering a button, passing one unit
    test, or succeeding in hosted CI proves complete runtime behavior.
-   That the suggested package names, interfaces, dependency choices, or
    example schemas exactly match current repository code. They are
    design sketches; inspect the checkout and follow established
    conventions.
-   That the project should implement every feature at once. Prioritize
    by user value, safety, feasibility, maintainability, and evidence.

## 3. Source-of-truth and repository audit

Public documentation can be stale or internally inconsistent. At the
start of every implementation, inspect the exact checkout and record the
commit SHA. The checkout, live tests, current architecture decisions,
and current CI configuration take precedence over this document.

### 3.1 Baseline commands

Run from a clean checkout using the repository's documented environment
and scripts:

``` bash
git status --short
git branch --show-current
git rev-parse HEAD
git log -5 --oneline --decorate
go version
go env GOOS GOARCH CGO_ENABLED
go list ./...
go test ./...
```

Also inspect the actual frontend/native build scripts, packaging
scripts, workflows, release-version authority, and platform-specific
acceptance tests. Do not invent replacement commands when the project
already defines a supported gate. Record command, exit code,
environment, and commit SHA.

### 3.2 Files and systems to inspect

At minimum, inspect the current versions of: - `README.md`, `go.mod`,
`docs/ARCHITECTURE.md`, `docs/ROADMAP.md`, `docs/changelog.md`, and
relevant design decision records; - the tool registry and tool schema
validation; - orchestration, run/session lifecycle, cancellation,
approvals, risk policy, and event streaming; - model/backend registry,
model metadata, context assembly, inference admission, and truthful
hardware/backend reporting; - existing `dataAnalysis` implementation,
limits, artifacts, and tests; - memory, file indexing/retrieval,
browser/search, scheduler, downloader, installer, and coding-lab
authorities; - Qt Quick/QML production UI, IPC boundary, any embedded
web/headless frontend, accessibility patterns, and UI tests; - Windows
and Linux CI workflows, packaging, release checks, dependency audits,
and smoke tests.

Produce a compact audit matrix before editing:

  -----------------------------------------------------------------------------------------------
  Capability/system   Status        Authority/package   Evidence          Gap         Next action
  ------------------- ------------- ------------------- ----------------- ----------- -----------
  Existing            implemented / exact path          test/commit/run   specific    smallest
                      partial /                                           missing     safe step
                      absent /                                            behavior    
                      broken /                                                        
                      unknown                                                         

  -----------------------------------------------------------------------------------------------

"Unknown" is a valid result. Do not convert unknown into "absent"
without inspection. Do not add a second system merely because a feature
is difficult to locate.

### 3.3 Repository identity and naming

Use the module path and public APIs already established by the checkout.
Do not rename the Go module, rename core packages, or perform a
repository-wide branding migration as part of a capability feature
unless separately authorized and planned. Prefer the existing
`SHEYTAN-LA` naming. Keep the user-provided project link,
https://linktr.ee/Parsaetak, in this document's project metadata; do not
change licensing or attribution without an explicit request.

### Gate 0 --- baseline accepted

No feature implementation starts until the agent has recorded: 1. exact
branch and commit SHA; 2. clean/dirty status and pre-existing failures;
3. the authoritative subsystem entry points; 4. existing
test/build/packaging gates; 5. relevant design constraints and open
questions; 6. what is already implemented, with evidence.

If repository reality contradicts this document, update the
implementation plan and document the discrepancy before coding.

------------------------------------------------------------------------

## 4. Capability model: separate the layers

A useful assistant capability is rarely one function. It typically
crosses several layers. Keep the responsibilities explicit.

1.  **Model capability:** what the selected model can natively
    do---text, image input, audio input/output, structured output, tool
    calling, context size, and streaming.
2.  **Orchestration capability:** Go's control over turns, plans, tools,
    approvals, budgets, cancellation, and run lifecycle.
3.  **Tool capability:** deterministic or provider-backed operations
    exposed through the existing tool registry.
4.  **Data capability:** ingestion, retrieval, analysis, artifacts,
    provenance, and permissions.
5.  **Presentation capability:** Qt Quick/QML desktop UI and any
    deliberately supported secondary UI surface.
6.  **Provider capability:** external web search, remote model API,
    transcription, speech, or image-generation services.
7.  **Verification capability:** tests and runtime evidence that prove
    the path works in a specific build and environment.

A model's native ability does not prove that the application supports
it. A Go tool does not make a model multimodal. A UI component does not
prove its backend works. A remote provider integration does not mean
offline support.

### 4.1 Capability lifecycle

Use a structured capability inventory derived from live state rather
than hard-coded marketing claims. A capability record should represent,
where applicable:

-   stable capability ID and user-facing name;
-   category and concise description;
-   implementation state and version;
-   tool registration state;
-   policy/configuration enabled state;
-   selected model/backend compatibility;
-   provider/dependency availability;
-   permission and network requirements;
-   platform support and resource constraints;
-   test evidence identifiers and commit SHA;
-   last runtime-verification time and environment;
-   known limitations and unsupported cases;
-   user-facing failure explanation and remediation.

Use separate states rather than one ambiguous `available` boolean:

  -----------------------------------------------------------------------
  State                               Meaning
  ----------------------------------- -----------------------------------
  `implemented`                       A code path exists. This alone
                                      proves little about usability.

  `registered`                        The expected tool or capability is
                                      present in the live registry.

  `enabled`                           Current configuration and policy
                                      allow it.

  `dependency_ready`                  Required model, provider, binary,
                                      or library is available.

  `compatible`                        The selected model/backend and
                                      current platform satisfy declared
                                      requirements.

  `tested`                            Named automated tests passed on a
                                      specified commit/environment.

  `runtime_verified`                  The actual feature path was
                                      exercised successfully in a
                                      recorded runtime.

  `degraded`                          A reduced mode is available, with
                                      limitations stated.

  `unavailable`                       A required condition is missing or
                                      the feature is disabled.

  `unknown`                           Evidence is insufficient; do not
                                      infer success.
  -----------------------------------------------------------------------

A feature can be implemented but disabled, enabled but incompatible,
tested but not runtime-verified, or available only through a network
provider. Preserve these distinctions in API responses, UI, logs, and
documentation.

------------------------------------------------------------------------

## 5. Capability roadmap

The following table is the complete top-level inventory. The audit
determines whether each item is already implemented, partially
implemented, missing, or broken.

  -----------------------------------------------------------------------------------------------------------
  Capability family     User-facing outcome  Main owner                   Dependencies /       Priority
                                                                          boundary             
  --------------------- -------------------- ---------------------------- -------------------- --------------
  Conversation and      Answer, explain,     existing                     model-dependent      P0
  writing               draft, revise,       conversation/orchestration                        
                        summarize, translate                                                   

  Context and           Use current turn,    context                      token/context        P0
  instruction handling  conversation         assembly/orchestrator        budgets              
                        history, task                                                          
                        constraints                                                            

  Tool calling          Invoke typed tools   existing tool registry       schemas, policy,     P0
                        safely and handle                                 cancellation         
                        results                                                                

  Planning and          Break tasks into     existing run/orchestration   checkpoints, budgets P0
  multi-step execution  steps and verify     authority                                         
                        completion                                                             

  Coding and repository Inspect, patch,      existing coding              sandbox and write    P0
  work                  build, test, review  lab/workspace                approvals            

  Search and deep       Gather sources and   existing search/browser      network, SSRF        P1
  research              produce cited        authority or adapter         controls             
                        synthesis                                                              

  Files and document    Ingest, search, read existing file/index/artifact parsers, access      P1
  retrieval             ranges, cite sources authorities                  control              

  Data analysis         Profile, transform,  existing `dataAnalysis`      bounded CPU/memory   P1
                        aggregate, join,     authority                                         
                        export                                                                 

  Charts and            Display accurate     Go typed output + Qt UI      validated schema     P1
  interactive artifacts visualizations and                                                     
                        tables                                                                 

  Spreadsheet support   Read/write supported data adapter/artifact        optional parser      P1/P2
                        workbook formats     authority                    library              

  Image understanding   Inspect              compatible vision            image-capable        P2
                        user-provided        model/provider + ingestion   backend              
                        images/screenshots                                                     

  Image                 Create or edit       optional provider/model      generation-capable   P2
  generation/editing    images               adapter                      backend, policy      

  Audio/transcription   Transcribe supported audio adapter/provider/model codecs, optional     P2
                        recordings                                        service              

  Speech                Speak responses or   audio runtime/provider       streaming, device    P2
  generation/voice      support live voice                                permissions          

  Video/frame analysis  Analyze bounded      media adapter + multimodal   extraction budget    P2
                        frame sequences with backend                                           
                        timestamps                                                             

  Memory and            Retrieve and manage  existing memory authority    consent, retention,  P1
  personalization       approved durable                                  deletion             
                        context                                                                

  Projects and          Organize artifacts   existing project/run         persistence,         P1
  resumable tasks       and continue long    authorities                  checkpoint schema    
                        workflows                                                              

  Scheduling and        Execute scheduled    existing scheduler           policy, timezone,    P2
  reminders             work transparently                                consent              

  Interactive UI        Native charts,       Qt Quick/QML                 stable IPC contracts P1
                        forms, progress,                                                       
                        accessible artifacts                                                   

  Capability            Explain what works   existing self-model/registry live evidence        P0
  self-knowledge        and why/why not                                                        

  Observability and     Explain failures     existing logging/diagnostics redaction, trace IDs P0
  diagnostics           with safe evidence                                                     

  Security and privacy  Bound execution and  shared platform policy       threat models and    P0
                        protect data                                      tests                
  -----------------------------------------------------------------------------------------------------------

**Priority key:** P0 = correctness and architecture foundations; P1 =
high-value core workflow; P2 = optional multimodal or expanded
functionality after prerequisites. Priority is not a claim of current
implementation.

------------------------------------------------------------------------

## 6. Cross-cutting architecture rules

These rules apply to every roadmap item.

### 6.1 Preserve single authorities

There must be one authoritative implementation for each of: - tool
registration and schema validation; - run/session lifecycle and
cancellation; - approval and risk policy; - model/backend selection and
inference admission; - memory and durable user context; - file indexing
and retrieval metadata; - artifact creation, ownership, and cleanup; -
scheduler and task recurrence; - download/install lifecycle; -
capability inventory and self-reporting; - UI-to-core IPC contract.

New features must call or extend the existing authority. Do not create a
second registry, scheduler, memory store, run manager, artifact store,
approval system, model manager, or independent agent runtime.

### 6.2 Go is the orchestration authority

Go owns policy, validation, tool execution, state transitions, run IDs,
cancellation, approvals, budgets, provenance, and artifact references.
Models propose actions; they do not bypass the Go authority. A UI must
not independently decide whether an operation is authorized.

Native C/C++ libraries may be used for narrowly defined inference/media
operations only when their lifecycle, ownership, compatibility, and test
evidence are explicit. Qt/QML is the production UI direction documented
by the repository; do not introduce a new UI framework because a sample
or library uses it.

### 6.3 Use explicit typed contracts

For each tool/API: - define input and output schema; - validate both
before/after execution; - version externally persisted contracts; -
distinguish user error, policy denial, dependency failure, timeout,
cancellation, and internal fault; - return stable machine-readable error
codes plus safe human-readable messages; - bound all lists, strings,
artifacts, and nested values; - avoid embedding huge payloads in model
context; use artifact IDs and paginated/ranged reads; - include
provenance and partial-result metadata when relevant.

Example only; adapt to established project conventions:

``` go
type CapabilityStatus struct {
    ID              string            `json:"id"`
    Implemented     bool              `json:"implemented"`
    Registered      bool              `json:"registered"`
    Enabled         bool              `json:"enabled"`
    DependencyReady bool              `json:"dependency_ready"`
    RuntimeVerified bool              `json:"runtime_verified"`
    Limitations     []string          `json:"limitations,omitempty"`
    Evidence        []EvidenceRef     `json:"evidence,omitempty"`
    Metadata        map[string]string `json:"metadata,omitempty"`
}
```

Do not copy this type blindly if the repository already has a compatible
status model. Avoid `map[string]any` for public contracts unless a
documented extensibility need justifies it.

### 6.4 Bounded, cancellable execution

Every operation that parses, searches, computes, downloads, calls a
provider, generates media, or streams must have: - a `context.Context`
or equivalent cancellation path; - deadlines/timeouts appropriate to the
operation; - maximum input bytes, item count, nesting, output size, and
concurrency; - explicit memory/CPU budgets where feasible; - bounded
retries with backoff only for retryable errors; - no unbounded
goroutines, polling, or sleeps as synchronization; - predictable cleanup
and terminal run states; - progress events that do not alter execution
authority.

Cancellation must propagate to child operations and subprocesses. A
timeout is not success; partial results must be labeled as partial.

### 6.5 Reproducibility and provenance

When results depend on data or computation, record enough safe metadata
to reproduce or audit them: - source artifact ID/hash; - tool and
algorithm version; - relevant options and deterministic seed; - time and
environment when time affects behavior; - provider/model identity where
disclosure is allowed; - sampling/truncation details; - source
references for external claims; - test/run IDs for verification.

Do not log secrets or full private data merely to make results
reproducible. Prefer hashes, IDs, bounded metadata, and user-controlled
diagnostic exports.

------------------------------------------------------------------------

## 7. Conversation, reasoning, and writing

This family is partly model-dependent and partly
orchestration-dependent. Do not try to implement "reasoning" as a
pretend Go algorithm. Improve the system around the model so its work is
grounded, consistent, bounded, and verifiable.

### 7.1 Context assembly

Audit the existing context builder and define: - precedence between
system/developer policy, user request, retrieved context, tool output,
and untrusted external content; - token/context budget allocation; -
truncation policy and user-visible indication when important context was
omitted; - deterministic message ordering and stable tool-result
references; - separation of trusted instructions from quoted documents
and web pages; - current-run state versus long-term memory; - handling
of conflicting instructions and stale retrieved facts.

Never allow retrieved documents, web pages, image OCR, or tool output to
override application policy or authorize a tool action.

### 7.2 Writing and structured output

Support practical writing tasks through the existing conversation/model
path. Add typed output formats only where downstream tools need them.
Validate JSON or other structured output against a schema; retry or ask
for clarification on invalid output within a bounded budget. Do not
silently convert invalid model output into a fabricated success.

Acceptance tests should cover: - instruction precedence and conflict
handling; - long context and truncation disclosures; - malformed
structured output; - multilingual Unicode; - citation preservation
during summarization; - refusal/error paths that do not execute tools; -
streaming interruption and cancellation.

### 7.3 Reasoning levels and budget

Expose reasoning effort only when the selected backend supports it.
Translate product-level settings into backend-specific parameters
through a tested adapter. Do not assume every model supports the same
levels or that a higher setting always improves quality. Record selected
settings and budgets; never expose private chain-of-thought as a
requirement. User-facing explanations should provide conclusions, key
evidence, assumptions, and verification---not hidden internal reasoning
traces.

------------------------------------------------------------------------

## 8. Tool execution, planning, and agent workflows

Use the current Go orchestration and tool registry. Avoid adding a
second agent framework.

### 8.1 Tool-call lifecycle

For every model-proposed call: 1. Parse and validate the tool name and
arguments. 2. Check whether the tool is registered, enabled, compatible,
and allowed. 3. Normalize arguments into a canonical representation. 4.
Classify risk and determine whether approval is required. 5. Bind the
call to the current run/session and an idempotency key where
appropriate. 6. Execute with context cancellation, timeouts, resource
limits, and audit metadata. 7. Validate the result schema and bound
output size. 8. Store large output as an artifact and pass a compact
reference to the model. 9. Record terminal status and safe diagnostics.
10. Re-evaluate the plan using actual tool results, not assumed success.

Approval must bind to the exact operation, canonical arguments, and risk
scope. A changed argument requires a new authorization decision. A
model's assertion that the user approved something is not approval
evidence.

### 8.2 Plans and checkpoints

Represent long tasks as explicit steps with dependencies, status,
outputs, and failure reasons. Checkpoint only stable, versioned state.
On resume, revalidate permissions, current files, provider availability,
and stale assumptions; do not blindly replay destructive operations.

Plan steps should be: - observable and user-comprehensible; - idempotent
where possible; - independently verifiable; - cancellable; - bounded by
wall-clock, tool-call, token, and resource budgets; - explicit about
irreversible side effects.

Do not use sleep-based coordination or retry storms to make workflows
appear reliable. Prefer event-driven completion, explicit deadlines, and
terminal-state checks.

### 8.3 Delegation and concurrency

Reuse the project's current delegation rules. Parallelize only
independent, safe work. Serialize conflicting writes and state
mutations. Bound fan-out and concurrency. Each child task must have a
parent run ID, limited authority, a defined output contract, and
cancellation propagation. A child agent must not become a second policy
or run authority.

### 8.4 Coding workflows

A coding task should distinguish: - repository inspection; -
diagnosis/hypothesis; - patch preparation; - tests and static checks; -
runtime/UI acceptance; - independent review of the diff; -
packaging/archive verification.

Never report a fix as verified because code was edited or compiled.
Preserve the baseline failures, compare before/after evidence, and
identify any untested platform or hardware path. Do not push, open PRs,
publish releases, or perform destructive repository actions unless the
user explicitly authorized them.

------------------------------------------------------------------------

## 9. Search, browsing, and deep research

Build on the current search/browser tool and provider architecture if
one exists. Do not create a second browser authority.

### 9.1 Research workflow

A robust research run should: 1. Clarify the question and required
freshness when materially ambiguous. 2. Create a bounded research plan.
3. Search more than one source when the question warrants it. 4. Open
and inspect primary sources where possible. 5. Extract claims with
source IDs, URLs, timestamps, and relevant ranges. 6. Compare evidence,
dates, scope, and contradictions. 7. Distinguish established facts,
inference, uncertainty, and missing evidence. 8. Produce a structured
synthesis with claim-level citations. 9. Preserve a compact source
ledger and optionally export the report as an artifact. 10. State
limitations such as inaccessible pages, search-only snippets, stale
results, or missing primary evidence.

Research quality is not measured by the number of URLs. Prefer relevant
primary sources and independent corroboration.

### 9.2 Provider abstraction

A narrow interface may look like this conceptually:

``` go
type SearchProvider interface {
    Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}
```

Align actual interfaces with repository conventions. A result should
retain provider identity, title, canonical URL, snippet, retrieval time,
verified publication time if available, and stable source ID.
Fetch/extraction should record redirect chain or final URL, HTTP status,
content type, bounded extracted text, and warnings.

Keep provider-specific fields behind adapters. Provider errors must not
be converted into fabricated sources or successful empty results.

### 9.3 Network security

-   Allow only supported URL schemes, normally HTTPS and HTTP where
    explicitly justified.
-   Block loopback, private, link-local, multicast, reserved, and
    metadata-service addresses.
-   Validate resolved addresses and re-check after redirects; defend
    against DNS rebinding.
-   Apply connect, TLS, header, body, and total request timeouts.
-   Limit redirects, response bytes, decompressed bytes, content types,
    and concurrency.
-   Never forward local credentials, cookies, or authorization headers
    to arbitrary destinations.
-   Do not expose unrestricted local filesystem or network access
    through a research tool.
-   Respect provider terms, rate limits, and configured networking
    policy.
-   Treat fetched content as untrusted data, not executable
    instructions.
-   Permit networking to be disabled and make remote data transfer
    visible to the user.

### 9.4 Citation integrity

Each factual claim derived from external material should refer to one or
more stored source IDs. Never invent a URL, quote, date, title, or
citation. Preserve the distinction between publication time and
retrieval time. If only a search snippet was available, label that
evidence as limited. Contradictory sources should remain visible rather
than being silently collapsed.

Test redirects to private IPs, IPv6 edge cases, DNS changes, oversized
pages, decompression bombs, malformed encodings, duplicate/canonical
URLs, HTML prompt injection, missing publication dates, timeouts, and
citation-to-source resolution.

------------------------------------------------------------------------

## 10. File ingestion, retrieval, and document understanding

Reuse existing file, memory, indexing, and artifact authorities. Do not
add a global index or document store without a documented gap and
decision record.

### 10.1 Pipeline

1.  **Inventory:** stable file ID, safe path or provider handle, size,
    type, modification time, and content hash.
2.  **Admission:** allowlisted type, size, archive, and resource checks.
3.  **Extraction:** format-specific parser returns text/tables plus
    source locations.
4.  **Normalization:** canonical text and metadata while retaining
    original offsets or location mapping.
5.  **Indexing:** bounded lexical retrieval first; embeddings only when
    evaluation shows a benefit.
6.  **Retrieval:** ranked snippets with stable file IDs and exact source
    ranges.
7.  **Reading:** fetch selected pages, sections, rows, or byte ranges on
    demand.
8.  **Synthesis:** answer using retrieved evidence, with file citations.
9.  **Invalidation:** detect changed/deleted files and update the index
    atomically.
10. **Deletion:** honor user deletion and retention policy across
    source, index, cache, and derived artifacts.

### 10.2 Format support

Potential adapters include plain text, Markdown, HTML, CSV/TSV,
JSON/JSONL, PDF, DOCX, XLSX, and supported image formats. Treat every
format as a separately tested capability. OCR is not implied by PDF
support. Embedded media, macros, links, and scripts must not execute
during extraction.

For archives, enforce compressed and decompressed size limits,
file-count limits, nesting limits, and path containment. Guard against
ZIP bombs and archive path traversal. Avoid launching office
applications or shell commands with user-controlled arguments.

### 10.3 Permission correctness

Enforce authorization at both search and read time. A stale cached index
must not expose a file after its permissions change. Exclude secrets and
sensitive directories by default where practical, provide configurable
exclusions, and make deletion semantics explicit. Do not place full
private document contents in logs or telemetry.

### 10.4 Citation precision

Citations should identify the exact file and source location---page,
heading, sheet/row, or section---when available. Preserve source offsets
during extraction where possible. If extraction cannot provide reliable
locations, state that limitation instead of manufacturing line numbers.

------------------------------------------------------------------------

## 11. Data analysis and statistics

The current public report describes a deterministic pure-Go
`dataAnalysis` tool supporting CSV/TSV/JSON and operations such as
analysis, aggregation, joins, quality checks, and export, with a stated
256 MB input bound. Treat those as repository claims to verify against
the checkout, not immutable facts. The current README and architecture
decision records win.

**Do not build a second data-analysis authority.** Extend the existing
tool only after reviewing its actual code, contracts, tests, and any
documented decision rejecting DuckDB or another engine.

### 11.1 Data contracts and parsing

Define explicit rules for: - supported input shapes and encodings; -
headers, duplicate column names, missing fields, and inconsistent row
widths; - type inference and ambiguous columns; - null, empty string,
invalid numeric values, NaN, and infinities; - date/time parsing and
timezone assumptions; - nested JSON and JSONL behavior; - maximum bytes,
rows, columns, cell size, nesting depth, and output size.

All readers should feed the existing normalized dataset representation.
Do not copy entire datasets into duplicate caches without measured
justification. Preserve useful parse-once numeric caches if benchmarks
demonstrate benefit.

### 11.2 Descriptive analysis

Return typed results for: - dimensions and schema; - missing-value
counts and percentages; - numeric count, min/max, mean, median,
variance/standard deviation, and supported quantiles; - categorical
frequencies with bounded top-K and stable tie-breaking; - duplicate
rows/keys where meaningful; - numeric correlations with constant-column
and sample-size handling; - outlier flags with named method and
thresholds; - quality findings with severity, rule ID, observed
evidence, and remediation suggestion.

Define sample versus population variance, quantile interpolation, null
handling, minimum sample sizes, and numeric overflow behavior. Preserve
precision internally; round only for presentation. Use stable algorithms
such as Welford's method where appropriate. Avoid nondeterministic map
iteration in serialized output.

Outliers are flags, not automatic proof of bad data. Do not silently
alter source values.

### 11.3 Aggregation, joins, and cleaning

Aggregation must define multiple grouping keys, supported aggregates,
null group behavior, output ordering, cardinality limits, and overflow
handling.

Joins must define supported join types, key normalization, null-key
semantics, duplicate-key multiplicity, collision naming,
missing/mismatched types, output-size bounds, and cancellation.
Many-to-many joins can multiply rows dramatically; preflight or cap the
result.

Data cleaning should preview changes and export a cleaned copy. Do not
overwrite original files or silently drop rows/columns.

### 11.4 Statistical models

Start with transparent, testable methods: - descriptive statistics and
quantiles; - Pearson/Spearman correlation with explicit missing-data
policy; - ordinary least-squares regression with residual diagnostics,
R², MAE, and RMSE; - deterministic train/test splits with a recorded
seed; - bootstrap intervals only when assumptions and compute budgets
are explicit.

Do not compute matrix inverses naively for ill-conditioned regression.
Prefer a maintained, license-compatible numerical library with QR/SVD
support, or explicitly reject singular/ill-conditioned inputs. Include
algorithm version, dataset fingerprint, selected columns, preprocessing,
random seed, split strategy, train/evaluation counts, metrics, and
caveats.

Do not claim parity with scikit-learn. Each additional model needs
known-answer fixtures, independent comparison, leakage tests,
cancellation, resource limits, and documented unsupported cases. Python
may be used as an independent test oracle, but must not become an
invisible production dependency. Any future sidecar must be optional,
supervised, isolated, versioned, and reported as a separate backend.

### 11.5 Spreadsheet support

Add XLSX only after reviewing candidate libraries for maintenance,
license, security history, memory/streaming behavior, and Windows/Linux
support. Enforce compressed and decompressed size, sheet, row, column,
cell, and processing-time limits. Do not execute macros or claim
formulas were recalculated. Define whether cached formula values are
accepted and label them.

Normalize spreadsheet values through the existing schema layer. Test
dates, timezone assumptions, Unicode, empty sheets, duplicate headers,
hidden sheets, formulas, corrupt archives, large files, and round trips.
Protect exported spreadsheets against formula injection for untrusted
text beginning with formula-triggering characters. Do not add legacy XLS
without a concrete requirement.

### 11.6 SQL decision gate

The public report previously stated that SQL was not part of the
existing data-analysis surface and that DuckDB had been evaluated and
rejected. Verify that decision in the current checkout before revisiting
it.

Only add SQL if a concrete workload cannot be served safely by existing
operations. Write a decision record comparing the current engine,
SQLite, DuckDB, and other viable choices across binary size, CGO/native
dependencies, licensing, packaging, concurrency, memory, cancellation,
security, and maintenance burden.

If approved, expose SQL as an adapter behind the existing tool and run
authority---not a new registry or persistent data authority. Use per-run
or in-memory datasets, read-only mode where applicable, parameterized
values, table/function allowlists, deadlines, row/output caps, and
explicit resource budgets. Deny extension loading, arbitrary
filesystem/network access, and escape paths such as unrestricted
`ATTACH` or `COPY` unless separately reviewed. Test injection, timeouts,
cancellation, file/network escape attempts, and Windows/Linux packaging.

------------------------------------------------------------------------

## 12. Charts, tables, and interactive artifacts

The backend should return a validated, typed specification; it should
not generate arbitrary executable UI code. The production desktop is
documented as Qt Quick/QML. Any React or web surface must be treated as
a separate, explicitly supported client rather than assumed to be the
desktop renderer.

### 12.1 Renderer-independent chart contract

The Go contract should describe: - chart kind from an allowlist; -
series and logical types; - labels, units, aggregation, filters, and
sorting; - missing-value behavior; - source artifact/dataset
fingerprint; - sampling/downsampling method and original point count; -
maximum plotted points; - axis ranges and safe formatting metadata; -
accessibility summary and alternative text; - provenance and analysis
operation IDs.

The model may request a chart type and data mapping, but cannot inject
JavaScript, HTML, QML, arbitrary URLs, or native code. Validate the
specification on the backend and again at the UI boundary.

### 12.2 Qt Quick/QML integration

Implement the production desktop renderer using the repository's
established Qt/QML patterns. Keep computation and authoritative data in
Go; the UI consumes a stable IPC/DTO contract. Do not move analysis
logic into QML. Preserve accessibility, keyboard navigation, resize
behavior, localization, and large-result responsiveness.

If a web/headless interface is supported, provide a separate renderer
that consumes the same versioned chart contract. Do not couple the Go
core to a particular rendering library.

### 12.3 Chart correctness and safety

Support only chart types with clear semantics and tests. Distinguish
sampled from complete data, show units and meaningful labels, avoid
misleading axes, and never imply a statistical conclusion merely because
a chart looks persuasive. Export to SVG/PNG only after evaluating
maintained libraries and safe handling. Treat SVG as potentially active
content; sanitize it and use appropriate content security policies when
displayed.

Test empty datasets, all-null series, extreme values, large cardinality,
Unicode labels, malformed specs, invalid ranges, resize/accessibility,
and sampling disclosures.

------------------------------------------------------------------------

## 13. Images, image generation, audio, voice, and video

These are optional capability families. They require a compatible
model/backend or explicit provider adapter. Do not advertise them
because a file picker or UI button exists.

### 13.1 Image understanding

Pipeline: 1. Validate file type, dimensions, encoded bytes, and decoded
pixel count. 2. Normalize orientation and supported color modes without
overwriting the original. 3. Use the selected vision-capable backend or
provider. 4. Attach image references to the correct conversation/run
with explicit privacy/network disclosure. 5. Return bounded results and
provenance where supported. 6. Preserve cancellation and cleanup of
temporary files.

Test corrupt images, oversized dimensions, decompression bombs,
unsupported formats, EXIF orientation, alpha/transparency, Unicode
filenames, and backend-unavailable behavior. Clearly distinguish OCR
output from visual interpretation.

### 13.2 Image generation and editing

Define a provider/model adapter with explicit support for prompt,
dimensions, aspect ratio, output format, seed if supported, safety
outcomes, cancellation, and artifact metadata. Validate dimensions and
output bytes. Store results through the existing artifact authority.
Editing must preserve source-image identity and produce a new artifact
unless the user explicitly requests replacement.

Never imply local generation if a remote service was used. Show when a
request requires network access, disclose provider use, and honor the
application's safety and privacy policy. Do not claim that an arbitrary
text model can generate images.

### 13.3 Audio transcription and speech

Separate: - decoding/format validation; - transcription; -
timestamps/segments/speaker labels only when supported; - text-to-speech
generation; - playback/device interaction; - optional live streaming.

Bound duration, file size, channels, sample rate, and processing time.
Avoid claiming speaker identity or emotion without supported, evaluated
behavior. Device access must be permission-gated and user-visible. Make
microphone activation explicit, provide a clear active state and stop
control, and do not record silently.

### 13.4 Video and screen content

Video support should be a bounded pipeline: inspect metadata, extract
selected frames at a configured interval/maximum count, optionally
extract supported audio, preserve timestamps, and pass only supported
inputs to a compatible multimodal backend. Do not imply continuous
real-time video understanding when the implementation processes sampled
frames.

Screen capture and computer-use features must use the existing
computer-use authority and approval policy. Distinguish observation from
action; destructive UI actions require the existing risk/approval
controls. Test capture permissions, frame caps, timestamps,
cancellation, and failure recovery.

### 13.5 Feature gates

For each media capability, report: - backend/model support; - installed
codecs/libraries; - local versus remote execution; - required
permissions and device availability; - size/duration/dimension limits; -
supported formats; - verified platforms; - known failure modes.

An unavailable optional backend must degrade clearly and safely, not
crash the app or silently route private content to another provider.

------------------------------------------------------------------------

## 14. Memory, projects, and long-running tasks

Use the existing memory authority and project/run persistence. Do not
create an additional vector database or memory store without evidence
that the current implementation cannot meet the requirement.

### 14.1 Memory lifecycle

Memory should be: - user-visible and purpose-limited; - distinguishable
from current conversation context and project artifacts; - scoped
appropriately (global preference, project fact, run state, or
source-derived fact); - attributable to source and timestamp where
useful; - editable and deletable by the user; - subject to retention and
privacy controls; - excluded from tool authorization and policy
decisions unless an explicit trusted policy allows it.

Do not store secrets or sensitive data by default. Retrieved memory is
context, not unquestionable truth; stale or conflicting entries must be
handled explicitly. Deletion must invalidate indexes and derived caches
as defined by policy.

### 14.2 Projects and resumability

Projects may group conversations, instructions, source files, run
history, and artifacts, but should reuse existing ownership and
access-control models. Persist only versioned state required to resume.
On resume, validate schema version, current file hashes, credentials,
approvals, provider availability, and stale assumptions. Never replay an
irreversible action without checking its current authorization and
idempotency.

### 14.3 Scheduling

Scheduling is a separate opt-in feature family. Reuse the current
scheduler. Every task needs a clear owner, recurrence/timezone
semantics, next-run calculation, cancellation, missed-run policy, and
visible execution history. Revalidate policy and dependencies at
execution time. A scheduled task must not gain broader permissions than
the user originally authorized. Prevent duplicate execution after
restart and test daylight-saving/timezone edge cases where relevant.

------------------------------------------------------------------------

## 15. UI architecture, streaming, and accessibility

The UI must reflect the real backend state and remain responsive during
long operations. Follow the repository's current production
architecture: Qt Quick/QML over the established Go/IPC boundary, unless
a new architecture decision explicitly changes it.

### 15.1 UI principles

-   The Go core owns run state and tool authority; the UI presents it.
-   Use typed/versioned messages for progress, results, artifacts, and
    errors.
-   Do not duplicate business logic or authorization in the UI.
-   Support cancellation, clear running/paused/failed/completed states,
    and recovery after disconnect.
-   Avoid blocking the UI thread with parsing, rendering, or large
    serialization.
-   Bound event queues and coalesce high-frequency progress events.
-   Provide keyboard navigation, screen-reader semantics, focus
    behavior, readable contrast, and reduced-motion considerations.
-   Make networked operations, permissions, and external side effects
    visible.
-   Keep loading states informative; avoid fake progress or animation
    that implies work is occurring when it is not.

### 15.2 Streaming contract

Streaming should define event ordering, run IDs, sequence numbers where
needed, partial-result semantics, terminal states, and cancellation. The
UI must tolerate duplicate or late events without resurrecting a
completed run. Test slow clients, disconnect/reconnect, stop during
generation, stop during tool execution, malformed event payloads, and
zero-session/reload behavior.

A visible token or progress indicator is not proof that the final result
was persisted. Verify the full path from backend result to stored state
and rendered UI.

### 15.3 Secondary UI surfaces

If a web UI is maintained for development, headless operation, or remote
access, document its exact role. Share typed DTOs and backend semantics,
but do not assume it is the production desktop. Test each supported
surface independently. Do not introduce React-specific chart assumptions
into the Qt production path.

------------------------------------------------------------------------

## 16. Security, privacy, and trust boundaries

Security is a shared cross-cutting requirement, not a late-stage cleanup
task.

### 16.1 Untrusted input

Treat model output, user files, archives, web pages, extracted text,
OCR, tool results, and imported configuration as potentially malicious.
Validate schemas and lengths. Do not execute instructions embedded in
external content. Separate data from control messages.

### 16.2 Filesystem and process safety

-   Normalize and validate paths; enforce root containment and defend
    against symlink escapes.
-   Use generated artifact names rather than untrusted path components.
-   Write atomically and clean temporary files on failure/cancellation.
-   Never execute imported scripts, office macros, or embedded links
    during parsing.
-   Avoid shell invocation where a direct API exists; never interpolate
    untrusted input into a command.
-   Restrict subprocess working directories, environment, permissions,
    lifetime, output, and network access.
-   Require approvals for risky writes, destructive operations,
    installation, or external side effects through the existing policy
    system.

### 16.3 Secrets and telemetry

-   Do not log API keys, cookies, authorization headers, full private
    documents, or dataset rows by default.
-   Redact secrets before logs, diagnostics, and crash reports.
-   Make remote transfer explicit; local data stays local unless the
    user authorizes a particular remote operation.
-   Keep telemetry opt-in and purpose-limited, if present.
-   Provide safe diagnostics that preserve error codes, run IDs, hashes,
    and environment facts without exposing private payloads.

### 16.4 Dependencies and supply chain

Before adding a dependency, review maintenance, license compatibility,
transitive dependencies, known vulnerabilities, binary/native impact,
update strategy, and Windows/Linux behavior. Pin or constrain versions
according to repository policy. Run existing dependency audits and test
packaging. Avoid dependencies introduced only to mimic a branded
capability.

### 16.5 Threat modeling

For each new provider, parser, native library, or tool, record: - assets
and trust boundaries; - attacker-controlled inputs; - plausible abuse
and failure paths; - permissions required; - mitigations and residual
risk; - negative tests; - safe degradation behavior.

------------------------------------------------------------------------

## 17. Reliability, diagnostics, and performance

### 17.1 Error taxonomy

Use stable error classes where appropriate: - invalid input/schema; -
unsupported capability or format; - disabled by policy/configuration; -
approval required/denied; - dependency/backend unavailable; -
provider/network error; - timeout; - cancellation; - resource limit
exceeded; - stale artifact/source; - internal failure.

Keep user-facing messages concise and actionable. Preserve technical
diagnostics in safe logs. Never translate timeout or cancellation into
success.

### 17.2 Observability

Correlate logs/events with run IDs, tool-call IDs, and artifact IDs.
Record state transitions and timing boundaries without sensitive
payloads. Diagnostics should distinguish queue/admission time, execution
time, provider time, serialization, and UI delivery when measurable.

### 17.3 Performance

Establish representative baselines before optimizing. Measure latency,
allocations, peak resident memory, output size, and concurrency. Include
small, medium, and near-limit workloads. Do not report a speedup without
before/after results on the same workload and comparable environment. Do
not trade correctness, cancellation, or bounded memory for a benchmark
win.

### 17.4 Resource admission

Use a central admission mechanism where the repository already has one.
Prevent simultaneous expensive operations from exhausting RAM, GPU/CPU
capacity, file handles, or provider quotas. Queue or reject work with a
clear reason. Do not create a separate scheduler solely for model, data,
or media workloads.

------------------------------------------------------------------------

## 18. Testing and verification strategy

A capability is complete only when its behavior is defined, its tests
are meaningful, its security boundary is reviewed, and its runtime
limitations are documented.

### 18.1 Required test layers

1.  **Unit tests:** schema validation, pure functions, edge cases,
    deterministic semantics.
2.  **Property/invariant tests:** conservation, round trips, ordering,
    bounds, idempotency.
3.  **Fuzz tests:** parsers, serializers, URL handling, archive
    extraction, schema boundaries.
4.  **Integration tests:** actual tool registry, policy, run lifecycle,
    artifact authority, provider adapters.
5.  **Cross-platform tests:** Windows and Linux paths, process
    lifecycle, packaging, permissions.
6.  **UI tests:** real state transitions, rendering, cancellation,
    accessibility, disconnect/reconnect.
7.  **Security tests:** prompt injection, SSRF, path traversal,
    malicious archives, formula injection, authorization bypass.
8.  **Performance tests:** representative datasets, large files,
    streaming load, concurrency, cancellation.
9.  **Runtime acceptance:** execute the user-visible feature in the
    actual supported environment.
10. **Independent verification:** review the diff, inspect artifacts,
    validate claims against logs and test output.

### 18.2 Minimum test matrix by family

  -----------------------------------------------------------------------
  Family                              Required negative/edge cases
  ----------------------------------- -----------------------------------
  Conversation/context                truncation, conflicting
                                      instructions, malformed structured
                                      output, Unicode

  Tool execution                      invalid schema, unknown tool,
                                      denied approval, timeout,
                                      duplicate/late result

  Plans/checkpoints                   stale state, restart, cancellation,
                                      replay prevention, partial failure

  Research                            SSRF, redirects, stale/missing
                                      citations, prompt injection,
                                      provider failure

  File retrieval                      permission changes, deletion, stale
                                      index, inaccurate source range

  Data analysis                       malformed rows, nulls, overflow,
                                      huge cardinality, join explosion,
                                      cancellation

  XLSX                                corrupt ZIP, ZIP bomb, formulas,
                                      hidden sheets, formula injection

  Charts                              invalid spec, huge points, all-null
                                      series, misleading range, UI
                                      resize/accessibility

  Image                               corrupt/oversized image,
                                      decompression limit, unavailable
                                      backend

  Audio/video                         invalid codec, duration cap,
                                      cancellation, missing device,
                                      timestamp alignment

  Memory/projects                     deletion, stale context, scope
                                      leakage, invalid checkpoint version

  Scheduler                           duplicate run, timezone change,
                                      missed run, disabled dependency

  UI streaming                        disconnect, stop, late event,
                                      malformed event, persistence/render
                                      mismatch

  Security                            path traversal, symlink escape,
                                      secrets in logs, untrusted
                                      instructions
  -----------------------------------------------------------------------

### 18.3 Go quality commands

Use actual repository scripts first. Where appropriate, supplement with:

``` bash
gofmt -w <changed-go-files>
go vet ./...
go test ./...
go test -race ./...
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go test -run='^$' -bench=. -benchmem ./...
```

Use real package names and actual fuzz targets discovered in the
checkout. Do not blindly copy placeholder commands. Keep fuzz workloads
bounded and seed corpora reproducible. Run platform-specific CI and
packaging gates from the repository's documented workflow.

Coverage percentages support review but do not replace assertions.
Differential testing may use an independent implementation in a separate
harness; it must not silently add a production dependency.

### 18.4 Evidence ledger

For each acceptance claim, record: - claim/capability ID; - commit
SHA; - exact command or test ID; - OS/architecture/runtime details; -
exit code and result summary; - artifact/log reference; - whether it is
unit, integration, hosted CI, or physical/local runtime evidence; -
known limitations.

Never claim: - "fixed" without a reproduced failure and passing
regression evidence; - "runtime-tested" when only compiled or
unit-tested; - "GPU/NPU executed" from device detection alone; - "UI
works" from an API test alone; - "CI green" from a local run; -
"cross-platform" from one OS; - "secure" from the absence of a known
exploit.

------------------------------------------------------------------------

## 19. Staged implementation plan and gates

Work in small, reviewable slices. Do not implement all roadmap items in
one giant change. The order below is a dependency-aware default; the
audit may reorder it with written justification.

### Stage 0 --- Repository audit and baseline

**Deliver:** capability matrix, commit/environment record, architecture
constraints, existing failures, dependency inventory.\
**Gate:** no duplicate authority proposed; actual test commands known.

### Stage 1 --- Contracts and capability self-knowledge

**Deliver:** typed capability inventory derived from live
registry/config/backend/provider state; status vocabulary; evidence
references; tests.\
**Gate:** "implemented," "enabled," "tested," and "runtime verified"
remain distinct.

### Stage 2 --- Tool/run correctness

**Deliver:** verified schema validation, cancellation, error taxonomy,
bounded outputs, exact approval binding, stable run lifecycle.\
**Gate:** denial, timeout, cancellation, and late-result tests pass.

### Stage 3 --- Context and research foundations

**Deliver:** instruction/data separation, provenance objects, provider
adapter review, citation validation, network safety.\
**Gate:** SSRF and prompt-injection tests pass; networking is
transparent and configurable.

### Stage 4 --- File retrieval and artifact integrity

**Deliver:** safe extraction, source locations, bounded indexing,
artifact metadata, invalidation/deletion.\
**Gate:** permission and source-range tests pass.

### Stage 5 --- Data-analysis correctness

**Deliver:** documented numerical semantics, deterministic results,
limits, cancellation, safe export, regression fixtures.\
**Gate:** known-answer and invariant tests pass; existing contracts do
not regress.

### Stage 6 --- Charts and interactive artifacts

**Deliver:** versioned chart DTO, Go validation, Qt Quick/QML renderer,
accessible states, bounded rendering.\
**Gate:** UI and backend tests pass; sampled data is disclosed.

### Stage 7 --- Optional adapters

**Deliver:** prioritize XLSX, image, audio, or other adapter only after
a specific use case, dependency review, and backend capability proof.\
**Gate:** unsupported backend and malicious input paths degrade safely.

### Stage 8 --- Memory/projects/scheduling enhancements

**Deliver:** only gaps identified in existing authorities; deletion,
scope, resume, timezone, and duplicate-run tests.\
**Gate:** no second store/scheduler/run authority introduced.

### Stage 9 --- Performance, security, and cross-platform acceptance

**Deliver:** representative benchmarks, fuzz/race/security tests,
Windows/Linux CI and packaging evidence, diff review.\
**Gate:** known failures documented; no unsupported success claims.

### Stage 10 --- Documentation and release readiness

**Deliver:** architecture/changelog/roadmap updates, feature
limitations, migration notes, acceptance evidence, dependency/license
review.\
**Gate:** all claims correspond to actual implementation and evidence;
no push/release unless separately authorized.

Each stage should be a separate reviewable change when practical. After
each: run targeted tests, relevant full suite, inspect the diff, update
docs, and record exact results. Avoid opportunistic unrelated refactors.

------------------------------------------------------------------------

## 20. Dependency decision template

Use this template before adding a major library, service, engine, or
native component.

  -----------------------------------------------------------------------
  Question                            Required answer
  ----------------------------------- -----------------------------------
  User need                           What concrete user task is blocked
                                      without this?

  Existing capability                 Why can't the current authority
                                      serve it?

  Alternatives                        Current implementation, small
                                      library, provider, native engine

  License                             License and compatibility with
                                      project distribution

  Maintenance                         Release history, ownership, support
                                      outlook

  Security                            Known advisories, attack surface,
                                      update path

  Platform                            Windows/Linux, architectures,
                                      CGO/native prerequisites

  Packaging                           Binary size, DLL/shared-library,
                                      installer, offline behavior

  Performance                         Measured workload and expected
                                      resource cost

  Cancellation                        Can work be interrupted safely?

  Isolation                           Does it require a subprocess,
                                      sandbox, or restricted permissions?

  Data transfer                       Local versus remote; what data
                                      leaves the machine?

  Exit strategy                       How can the dependency be replaced
                                      or removed?

  Decision                            accept / reject / defer with
                                      rationale and approver
  -----------------------------------------------------------------------

Do not add a dependency because it appears in a competing product or
makes a demo easier. The burden is to demonstrate net value and
sustainable support.

------------------------------------------------------------------------

## 21. Documentation and API compatibility

Every user-visible feature should update the appropriate documentation
without creating conflicting sources of truth.

Required updates, when applicable: - architecture: authority boundaries,
data flow, trust boundaries; - roadmap: feature state and next gate; -
changelog: user-visible change and compatibility notes; - tool/API
documentation: schemas, errors, limits, permissions, examples; -
security notes: threat model and mitigations; - tests/CI: exact
acceptance coverage; - help/UI text: supported formats, local/remote
behavior, limitations.

Avoid putting detailed release-version inventories in README if the
repository uses a canonical release source. Do not claim planned work as
shipped. Use relative repository links where appropriate and verify them
after editing.

For persisted contracts, define schema versions and migration behavior.
Unknown fields should be handled intentionally. Breaking changes require
a migration plan and tests for old/new persisted state. Never silently
reinterpret old data.

------------------------------------------------------------------------

## 22. Definition of done

A feature is ready for release consideration only when every applicable
item is true:

-   [ ] The current repository was audited and the exact baseline
    recorded.
-   [ ] The correct existing authority was extended; no duplicate
    registry/store/manager was created.
-   [ ] User behavior and unsupported cases are documented.
-   [ ] Inputs, outputs, errors, permissions, limits, and cancellation
    are defined.
-   [ ] Local versus remote processing and data transfer are disclosed.
-   [ ] Tool schemas and output are validated and bounded.
-   [ ] Security threats and negative tests are addressed.
-   [ ] Unit, integration, regression, and applicable fuzz/race tests
    pass.
-   [ ] Windows and Linux checks required by the repository have
    recorded results.
-   [ ] UI behavior is verified on each claimed supported surface.
-   [ ] Runtime claims have evidence from the actual execution path.
-   [ ] Artifacts are safely stored, attributable, and independently
    readable where applicable.
-   [ ] Performance claims include comparable measurements.
-   [ ] Dependencies have license/security/platform review.
-   [ ] Architecture, changelog, roadmap, and user-facing docs are
    updated.
-   [ ] The diff contains no unrelated changes or accidental
    generated/runtime artifacts.
-   [ ] Final status distinguishes completed, partial, blocked, and
    unverified items.
-   [ ] No push, PR, release, or destructive action occurred without
    authorization.

A feature may be useful before it meets release criteria, but its status
must remain accurate. "Experimental" is acceptable when clearly scoped;
false certainty is not.

------------------------------------------------------------------------

## 23. Reusable checklist for future capabilities

Use this section for any future feature not named in this document.

### A. Problem and value

-   What user task should become possible or materially better?
-   How will success be observed?
-   What is the smallest useful slice?
-   What is explicitly out of scope?

### B. Existing system fit

-   Which existing package or authority owns the closest behavior?
-   What does the current implementation already do?
-   Which architecture decisions constrain the design?
-   Can the feature be implemented as an adapter or extension?
-   What evidence justifies a new store, process, dependency, or
    subsystem?

### C. Capability and dependencies

-   Does the feature require a specific model modality or tool-call
    behavior?
-   Is a local implementation realistic on supported hardware?
-   Is a remote provider required or optional?
-   What happens when the dependency is missing, offline, incompatible,
    or rate-limited?
-   Are the platform and license implications acceptable?

### D. Contract and state

-   What are the versioned inputs/outputs?
-   Which errors are stable and actionable?
-   What is the lifecycle and cancellation model?
-   Is execution idempotent?
-   What state must persist, and how is it migrated/deleted?
-   How are artifacts and source provenance represented?

### E. Safety and privacy

-   What input is untrusted?
-   What permissions and approvals are required?
-   Can this access files, network, credentials, devices, or external
    side effects?
-   What resource limits are needed?
-   What data is logged or sent remotely?
-   How can the user inspect, cancel, revoke, or delete the result?

### F. Verification

-   What known-answer fixture proves correctness?
-   What regression test reproduces the original gap?
-   What fuzz, race, integration, UI, or security test applies?
-   Which OSes and hardware paths are actually tested?
-   What evidence is required before status changes to runtime-verified?
-   What negative result must be tested to prove safe failure?

### G. Delivery and maintenance

-   What is the implementation order and gate?
-   What docs and changelog entries are required?
-   What metrics show performance and resource cost?
-   How will the feature be diagnosed and updated?
-   What is the rollback/removal strategy?
-   What limitations must the UI disclose?

### H. Final report contract

Report only: 1. baseline commit and environment; 2. implemented changes
and exact paths; 3. tests run with commands and results; 4.
runtime/UI/platform evidence; 5. security and dependency decisions; 6.
known limitations and unverified areas; 7. artifacts produced and their
verified locations; 8. whether any external action was performed.

Do not replace evidence with confidence language.

------------------------------------------------------------------------

## 24. Official and project references

### Project sources --- verify against the current checkout

-   Repository: https://github.com/SHEYTANx13/SHEYTAN-LA
-   Architecture:
    https://github.com/SHEYTANx13/SHEYTAN-LA/blob/main/docs/ARCHITECTURE.md
-   Roadmap:
    https://github.com/SHEYTANx13/SHEYTAN-LA/blob/main/docs/ROADMAP.md
-   Changelog:
    https://github.com/SHEYTANx13/SHEYTAN-LA/blob/main/docs/changelog.md
-   README: https://github.com/SHEYTANx13/SHEYTAN-LA/blob/main/README.md
-   Go module: https://github.com/SHEYTANx13/SHEYTAN-LA/blob/main/go.mod
-   Project link: https://linktr.ee/Parsaetak

### Public capability references

These explain public user-facing capability categories; they do not
document OpenAI's private implementation and should not be interpreted
as a requirement to copy internal systems. - ChatGPT capabilities
overview:
https://help.openai.com/en/articles/9260256-chatgpt-capabilities-overview -
OpenAI API documentation: https://developers.openai.com/api/docs/ - Go
diagnostics: https://go.dev/doc/diagnostics - Go fuzzing tutorial:
https://go.dev/doc/tutorial/fuzz - Go testing package:
https://pkg.go.dev/testing - Go HTTP profiling:
https://pkg.go.dev/net/http/pprof

Recheck links and provider documentation before implementing a
provider-specific integration; external APIs and capabilities change
over time. Prefer official, current documentation over secondary
summaries.

------------------------------------------------------------------------

## 25. Final engineering rule

SHEYTAN-LA should pursue breadth through small, verified
capabilities---not through an untestable feature checklist. The best
design is one that preserves the existing authorities, makes
dependencies and trust boundaries explicit, reports what actually works,
and can be extended without rewriting the application for every new
model or provider.

**The model proposes. The tools execute. The laboratory verifies.**
