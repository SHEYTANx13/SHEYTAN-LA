# GPT-6 Capabilities for SHEYTAN-LA
## Engineering report: extending the Go core into a verified local data-analysis laboratory

**Repository:** https://github.com/SHEYTANx13/SHEYTAN-LA  
**Review date:** 2026-10-20  
**Target:** Go-first, Windows-first, local-first implementation  
**Purpose:** A practical, staged implementation plan—not a claim that these capabilities have already been implemented.

---

## 1. Executive recommendation

SHEYTAN-LA already describes a deterministic, pure-Go `dataAnalysis` tool that loads CSV/TSV/JSON, infers types, caches numeric columns, and supports `analyze`, `aggregate`, `join`, `quality`, and `export`. The README says its input bound is 256 MB and that an external DuckDB/SQL engine was evaluated and rejected. **Do not build a second data-analysis authority or casually reverse that architecture decision.** Extend the existing tool only after inspecting its current implementation, tests, changelog, and architecture decision record.

The useful goal is not to clone ChatGPT's internal tools. It is to bring equivalent *user-facing outcomes* into SHEYTAN-LA:

1. Reliable local dataset inspection and transformation.
2. Statistical analysis, reproducible models, and charts.
3. Spreadsheet and structured-file import/export.
4. Optional SQL-style analysis where justified.
5. Source-backed web research, with citations and freshness.
6. File-library search and safe document extraction.
7. Independent verification, stress testing, and performance evidence.
8. Honest runtime capability reporting—only advertise what is enabled and tested.

**Architectural principle:** Go remains the single authority for tool registration, policy, approvals, execution, run state, artifact ownership, and provenance. Analytical engines are libraries/adapters called by that authority, not separate competing agents or stores.

### Evidence boundary

The public repository README and `go.mod` were accessible during this review. The README states the current `dataAnalysis` design and its limits; `go.mod` declares module path `github.com/Parsaetak/SHEYTAN-local-agent` and `go 1.26`, despite the current GitHub repository name `SHEYTANx13/SHEYTAN-LA`. This report therefore uses existing architecture names where public evidence supports them and marks proposed package paths as examples. Before implementation, inspect the actual checkout and use its established module path, package layout, APIs, and tests. Do not rename the module as part of this feature unless a separate migration is approved.

## 2. Capability map: what to implement in Go

| Desired capability | Recommended implementation | Default posture |
|---|---|---|
| CSV, TSV, JSON analysis | Extend existing `dataAnalysis` authority | Local, enabled |
| Data profiling and quality | Existing analysis + explicit validation reports | Local, enabled |
| Aggregation, joins, exports | Extend current operations and contracts | Local, enabled |
| Excel `.xlsx` | Optional Go library adapter; bounded streaming where possible | Optional, feature-gated |
| SQL analysis | First offer SQL over a controlled in-memory/imported dataset only if there is a real need; evaluate SQLite/DuckDB separately | Off until justified |
| Statistics and regression | Deterministic Go implementations for core statistics; tested library or explicit adapter for advanced models | Local, scope-limited |
| Machine learning | Start with transparent regression/classification baselines; avoid implying parity with scikit-learn | Optional, experimental |
| Charts | Go produces chart specifications/data; existing React UI renders charts; export PNG/SVG only after library evaluation | Local |
| File ingestion | Sandboxed, allowlisted readers with byte/row/time limits | Local, guarded |
| Web research | Dedicated search/fetch provider adapters with provenance and citation objects | Opt-in/networked |
| Document extraction | Format-specific readers for text, Markdown, PDF, DOCX, XLSX, etc. | Optional adapters |
| Independent verification | Golden fixtures, invariants, differential tests, fuzzing, race tests, reproducible benchmark suite | Required |
| Self-reporting | Derive available capabilities from live registry, config, dependency, and test evidence | Required |

A Go implementation can cover many of these capabilities, but Go's standard library alone is not a drop-in replacement for every Python scientific library. Be explicit about unsupported algorithms and formats. Do not claim feature parity merely because a tool has a similar name.

## 3. Phase 0 — Audit the existing implementation before editing

Start from a clean checkout and record evidence:

```bash
git status --short
git branch --show-current
git rev-parse HEAD
go version
go env GOOS GOARCH CGO_ENABLED
go list ./...
go test ./...
```

On Windows, also run the repository's documented frontend, native, and packaging gates; on Linux, run its documented headless gates. Use the repository's actual scripts rather than inventing substitutes.

Inspect:

- `docs/ARCHITECTURE.md`, `docs/changelog.md`, `docs/ROADMAP.md`, current tool registry and policy code.
- The existing `dataAnalysis` registration, implementation, input schemas, output schemas, byte limits, cancellation behavior, and artifact lifecycle.
- Tests for deterministic output, join semantics, type inference, malformed input, numeric overflow, memory use, and concurrent calls.
- The exact DuckDB rejection rationale and any constraints it established.
- Existing search, file ingestion, browser, sandbox, and artifact/export paths. Reuse them where appropriate.

Produce a short audit table with: **existing / partial / absent / broken / unknown**, source path, test evidence, and proposed change. Do not duplicate existing features because this report names them.

### Gate 0

No implementation begins until the agent can name the existing authoritative data-analysis entry point, tool registration path, artifact writer, policy/approval boundary, and relevant test commands. If the repo differs from the public snapshot, the checkout wins and this report must be reconciled with it.

## 4. Phase 1 — Harden the existing data-analysis core

Keep the existing operation names and public contracts unless tests and migration notes justify a versioned change.

### 4.1 Input and schema model

Use a single internal dataset representation owned by the existing data-analysis package. A possible conceptual model:

```go
type Column struct {
    Name     string
    Type     LogicalType
    Nullable bool
}

type Dataset struct {
    Columns []Column
    Rows    int64
    // Keep values in the existing representation; do not copy the full
    // dataset into a second cache without measured justification.
}
```

This is illustrative, not a requirement to replace current types. Preserve parse-once numeric caching if benchmarks show it helps. Ensure all readers share one parser/normalization layer and one resource budget.

Requirements:
- Explicit type inference rules and stable treatment of ambiguous columns.
- Stable column ordering and stable category tie-breaking.
- Clear distinction among null, empty string, invalid numeric value, NaN, and infinity.
- Configurable limits for input bytes, rows, columns, cell/string size, nesting depth, and output size.
- Context cancellation checked during parsing, joins, grouping, statistics, and export.
- Errors identify the operation and field/row where safe, without leaking sensitive row contents.
- No silent truncation. If a result is sampled or capped, say so in structured metadata.

### 4.2 Analysis and quality report

The `analyze` operation should return a compact, typed result containing:
- Dataset dimensions and schema.
- Missingness counts and percentages.
- Numeric summaries: count, min, max, mean, median, variance/standard deviation, quantiles where defined.
- Categorical summaries with a configured top-K limit and deterministic tie breaks.
- Duplicate-row/key diagnostics where applicable.
- Correlations only for well-defined numeric pairs and with sample-size/constant-column handling.
- Outlier *flags* with the selected method and thresholds—not an assertion that a value is erroneous.
- Findings with severity, evidence fields, and caveats.
- Artifact references for larger results rather than raw-row dumps.

Define numerical semantics up front. Specify sample versus population variance; quantile interpolation; treatment of nulls; minimum sample size; rounding only at presentation time. Keep full precision internally and use stable accumulation where needed (e.g., Welford's algorithm for online variance). Avoid nondeterministic map iteration in serialized results.

### 4.3 Aggregation and joins

Aggregation:
- Multiple grouping keys and multiple aggregate functions.
- Explicit null-group behavior.
- Stable group output ordering.
- Overflow detection for integer sums/counts and defined behavior for decimal/floating totals.
- Limits on group cardinality and output rows.

Joins:
- Explicit join keys and supported join types.
- Defined null-key semantics.
- Duplicate-key multiplicity documented (SQL-like many-to-many expansion can explode row counts).
- Preflight output-size estimate or hard output bound.
- Clear behavior for mismatched key types and missing keys.
- Deterministic output column names and collision policy.
- Cancellation and memory-budget checks during hash-table construction and output emission.

Quality:
- Separate structural validation from heuristic findings.
- Every finding includes a rule ID, observed value, threshold, and remediation suggestion.
- Avoid automatic destructive cleaning. Offer a preview and export of a cleaned copy.

### 4.4 Export artifacts

Use the existing artifact authority. Each artifact should have:
- A generated, non-user-controlled storage name and safe extension.
- MIME/type, byte length, row count if applicable, SHA-256, creation time, source run ID, and operation metadata.
- Atomic write (temporary file then rename where supported).
- Root-directory containment checks; reject path traversal and symlink escapes.
- Export limits and cancellation.
- No overwrite of arbitrary user paths without explicit approval.

### Gate 1

Run existing tests unchanged first. Add golden tests for schema, null semantics, deterministic output, join multiplicity, numeric edge cases, and exports. Repeat a test with identical inputs and compare normalized output bytes. The existing deterministic contract must not regress.

## 5. Phase 2 — Statistical analysis and model evaluation

Implement transparent, testable core methods before adding a large external engine.

### 5.1 Core statistics

Suggested initial scope:
- Descriptive statistics and quantiles.
- Pearson and Spearman correlation with defined missing-data behavior.
- Ordinary least-squares regression with intercept, residual diagnostics, \(R^2\), MAE, and RMSE.
- Train/test split with explicit seed and documented split strategy.
- Bootstrap confidence intervals only when the sampling assumptions and compute limits are stated.

For linear regression, avoid computing \((X^T X)^{-1}X^T y\) naively for ill-conditioned data. Prefer QR or SVD from a vetted numerical library if available and acceptable; otherwise clearly limit supported conditions and reject singular/ill-conditioned inputs. Return rank/conditioning warnings where the implementation can measure them.

Every model result must include:
- Algorithm and implementation version.
- Dataset fingerprint and selected columns.
- Missing-value and encoding policy.
- Random seed and split details, if any.
- Training/evaluation row counts.
- Metrics and caveats.
- A warning that association and predictive performance do not establish causality.

### 5.2 Machine learning boundary

Do not promise a Go equivalent of all scikit-learn functionality. Start with a small, explicit set of supported algorithms and tests. Add classifiers, cross-validation, feature scaling, or model persistence only after specifying contracts and validating numerical correctness.

For each algorithm:
1. Define the supported data types and class labels.
2. Implement a reference fixture with known answers.
3. Compare results with an independent trusted implementation on fixed fixtures.
4. Test leakage prevention and train/test separation.
5. Add resource caps and cancellation.
6. Document unsupported cases.
7. Add benchmarks before optimizing.

Do not silently invoke Python as a hidden dependency. If a Python sidecar is ever justified, it must be optional, supervised, versioned, isolated, authenticated over local IPC, and reported as a separate execution backend—not described as pure Go.

## 6. Phase 3 — Spreadsheet and file-format adapters

### 6.1 XLSX

Add `.xlsx` through a maintained Go library only after checking its license, maintenance status, security history, streaming behavior, and Windows/Linux compatibility. Keep the adapter behind a narrow interface so it cannot become a second dataset authority.

Implementation sequence:
1. Add an adapter for workbook/sheet selection and header-row policy.
2. Enforce compressed file size, decompressed size, sheet count, row/column count, cell length, and processing-time limits. XLSX is a ZIP container; guard against ZIP bombs.
3. Treat formulas carefully: do not execute macros or imply formulas were recalculated. Define whether cached formula values are accepted and label them.
4. Normalize cells through the same schema/type inference layer as CSV/JSON.
5. Export only to the artifact authority, with deterministic sheet naming and formula-injection defenses for text beginning with `=`, `+`, `-`, or `@` when exporting to spreadsheet formats.
6. Test Unicode, dates/time zones, empty sheets, duplicate headers, formulas, large workbooks, corrupt ZIP entries, and round trips.

Do not add legacy `.xls` support unless there is a concrete requirement and a safe, maintained reader.

### 6.2 JSON and other documents

For JSON, define whether the accepted shape is arrays of objects, newline-delimited JSON, or both. Bound nesting depth and record size. Do not flatten nested objects ambiguously.

For PDF, DOCX, Markdown, HTML, and plain text, separate **document extraction** from **tabular analysis**. Each format adapter should return normalized text/tables plus source locations (page, sheet, row, or section) when available. OCR is a distinct optional capability, not implied by PDF support.

All file inputs are untrusted. Reject path traversal and archive expansion abuse, avoid shelling out with user-controlled arguments, and do not execute macros, embedded scripts, or document links.

## 7. Phase 4 — SQL-style analysis: a deliberate decision gate

The current public README says the data-analysis core has no SQL surface and that DuckDB was evaluated and rejected. Preserve that decision until a documented requirement outweighs its costs.

First ask whether the current `aggregate` and `join` operations cover the intended workloads. If yes, do not add SQL.

If SQL is needed:
1. Write a decision record comparing standard-library limitations, SQLite, DuckDB, and the existing pure-Go engine.
2. Compare binary size, cgo/native dependencies, Windows packaging, license, updates, memory behavior, cancellation, concurrency, and support burden.
3. Prefer an in-process engine and private per-run database or in-memory database. Never expose arbitrary SQL directly to persistent application state.
4. Use read-only mode where applicable, parameterized statements, explicit allowed tables/functions, query timeouts, row/output limits, and a dedicated resource budget.
5. Deny extension loading, arbitrary file/network access, `ATTACH`/`COPY` or equivalent escape paths unless a separately reviewed use case requires them.
6. Treat SQL as a *new adapter* called by the existing `dataAnalysis` tool, not a parallel tool registry or data store.
7. Add query-plan/resource tests, cancellation tests, malicious-query tests, and packaging tests for both Windows and Linux.

Do not ship a dependency solely to imitate the platform tools described in the motivating discussion.

## 8. Phase 5 — Charts and visual analytics

The simplest robust design is for Go to return a **typed chart specification** and for the existing React UI to render it. This avoids introducing a second UI stack or making the Go core depend on a browser renderer.

A chart spec should define:
- Chart kind (bar, line, scatter, histogram, box plot, heatmap where supported).
- X/Y series and logical types.
- Labels, units, aggregation, filtering, and sampling metadata.
- Missing-value handling.
- Maximum plotted points and downsampling method.
- Dataset/artifact fingerprint and analysis provenance.

Implementation steps:
1. Add a schema-validated `ChartSpec` response to the existing analysis output contract.
2. Use an allowlist of chart kinds and fields; the model may request a chart but cannot inject arbitrary JavaScript, HTML, or URLs.
3. Render in React using the project's existing dependencies where possible.
4. Keep raw data access behind the backend; pass only the bounded data required for the plot.
5. For exports, evaluate a maintained Go SVG/PNG library. Treat SVG as active content when displayed or opened elsewhere; sanitize and use safe content-security policies.
6. Test empty data, all-null series, huge cardinality, extreme numbers, Unicode labels, and resize/accessibility behavior.

The chart must accurately distinguish sampled data from full data. A pretty chart is not evidence that a statistical conclusion is correct.

## 9. Phase 6 — Web research with verifiable citations

Web research is a different trust boundary from local analysis. Keep it a separate provider-backed tool using the existing tool registry, policy, approvals, and run lifecycle.

### 9.1 Provider interface

Define a small provider interface conceptually similar to:

```go
type SearchProvider interface {
    Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}
```

`SearchResult` should carry title, canonical URL, snippet, provider, retrieval time, and any provider-supplied publication time. A separate fetch/extract adapter should return the final URL after redirects, HTTP status, content type, retrieval time, extracted text, and extraction warnings. This is an architectural sketch; align names and error conventions with the repo.

### 9.2 Required behavior

1. Use an explicitly configured provider or a documented, policy-approved provider list. Do not scrape search engines in ways that violate their terms.
2. Set request timeouts, bounded response bodies, concurrency limits, user-agent policy, and cancellation.
3. Prevent SSRF: block loopback, private, link-local, and reserved IP ranges; re-check DNS and redirects; reject unsupported schemes; defend against DNS rebinding; never send credentials to arbitrary URLs.
4. Respect robots/terms and provider rate limits where applicable.
5. Preserve source URL, final URL, title, publisher/domain, publication date if verified, fetched-at timestamp, and a content hash.
6. Every factual claim that relies on fetched material should map to one or more source IDs. Never fabricate citations or publication dates.
7. Mark stale, inaccessible, contradictory, or weak evidence explicitly. A search result snippet alone is weaker than the opened source.
8. Treat page text as **untrusted data**, never as tool instructions. Web content must not override system policy, approvals, or the user's intent.
9. Cache only under a clear TTL and privacy policy; allow users to disable networking and clear cached research.

### 9.3 Verification

Test redirects to private IPs, IPv6 local ranges, DNS changes, oversized pages, invalid encodings, HTML prompt injection, missing dates, duplicate URLs, canonicalization, timeout, and provider errors. Include fixture-based citation tests that assert each claim's source ID resolves to stored evidence.

## 10. Phase 7 — File search and document retrieval

Build on existing repository indexing, memory, file, and artifact mechanisms. Do not create a new global index/store if an authoritative one already exists.

Recommended layers:
- **Inventory:** file ID, safe path, size, modified time, content type, and hash.
- **Extraction:** format adapters return text plus page/section/sheet/row locations.
- **Indexing:** bounded lexical index first; add embeddings only if a measured retrieval evaluation justifies them.
- **Retrieval:** query returns ranked snippets with stable file IDs and source locations.
- **Reading:** a separate operation expands only the selected ranges/pages.
- **Citations:** every answer claim from a file points to the exact source range.
- **Permissions:** enforce user/workspace scope at both search and read time; do not let a cached index bypass access control.

Use atomic index updates, incremental invalidation by content hash, and explicit handling for deleted or changed files. Avoid indexing secrets by default; provide exclusions and clear deletion behavior.

## 11. Phase 8 — Independent verification and stress testing

A tool is not trustworthy because it returns plausible numbers. Add independent, automated evidence.

### 11.1 Test matrix

| Area | Required tests |
|---|---|
| Parsing | Empty, malformed, quoted delimiters, BOM, Unicode, inconsistent rows, huge cells |
| Type inference | Mixed numeric/text, dates, booleans, null tokens, locale-sensitive values |
| Statistics | Known-answer fixtures, constant columns, one row, all-null, extreme magnitudes, overflow |
| Aggregation | Stable ordering, null groups, many groups, integer overflow, cancellation |
| Joins | All join types, duplicate keys, null keys, mismatched types, output explosion |
| Export | Round trip, deterministic output, interrupted writes, path traversal, formula injection |
| XLSX | Corrupt ZIP, ZIP bomb limits, formulas, hidden sheets, dates, large sheets |
| SQL (if approved) | Injection, file/network escape attempts, timeout, read-only enforcement, output cap |
| Web | SSRF, redirect chains, private IPs, prompt injection, oversized bodies, citation integrity |
| File search | Changed/deleted files, permissions, stale indexes, source-range accuracy |
| Concurrency | `go test -race`, concurrent analyses, cancellation races, artifact collisions |
| Fuzzing | CSV/TSV/JSON parsers, type inference, URL canonicalization, export serializers |
| Performance | Representative small/medium/near-limit datasets; allocations, peak RSS, latency |
| Cross-platform | Windows and Linux tests/build/package gates |

### 11.2 Known-answer and differential testing

Create small fixtures whose expected outputs can be calculated by hand. For statistics, compare against an independent implementation (for example, Python/pandas or R in a separate test harness), but do not make Python a production dependency. Define tolerances before running comparisons; account for floating-point rounding and different quantile conventions.

For exports, parse the exported file with a second reader and compare normalized values, row counts, schema, and hashes where byte-identical output is expected.

### 11.3 Go-native quality gates

Run the project's standard checks, then add targeted checks where they fit existing CI:

```bash
gofmt -w <changed-go-files>
go vet ./...
go test ./...
go test -race ./...
go test -run='^$' -bench=. -benchmem ./...
go test -fuzz=FuzzDatasetParser -fuzztime=30s ./<actual-package>
```

Replace the placeholder package and fuzz target with real names; do not copy these commands literally before discovering the repository layout. Fuzz tests must have bounded work and stable seed corpora. Run Windows-specific and Linux-specific gates using existing workflows. Capture exact command, exit code, environment, and commit SHA.

Use `go test -coverprofile=coverage.out ./...` and `go tool cover -func=coverage.out` as supporting evidence, not as a substitute for meaningful assertions. Use `pprof`/benchmarks before optimization. Never claim a performance improvement without before/after results on the same workload and machine.

Official references:
- Go diagnostics and profiling: https://go.dev/doc/diagnostics
- Go fuzzing tutorial: https://go.dev/doc/tutorial/fuzz
- `testing` package: https://go.dev/pkg/testing/
- `net/http/pprof`: https://go.dev/pkg/net/http/pprof/

## 12. Phase 9 — Capability reporting and truthful self-knowledge

SHEYTAN already documents a runtime self-model built from the live registry, parsed model card, serving backend, hardware facts, and memory systems. Extend that same authority rather than hard-coding a second list.

Each capability should have structured status fields such as:
- `implemented`: code path exists.
- `registered`: tool is in the live registry.
- `enabled`: config/policy permits it.
- `dependencyAvailable`: required adapter/dependency is available.
- `tested`: specific test evidence exists, with commit and test ID.
- `runtimeVerified`: the feature was exercised successfully in this runtime.
- `limitations`: bounded inputs, unsupported formats, platform restrictions.
- `networkRequired`: whether data leaves the machine or external access is required.
- `evidence`: stable references to test results, artifact IDs, or source records.

Do not collapse these into one `available=true` boolean. A package compiling is not proof that the capability works; registration is not proof that it is enabled; a successful unit test is not proof of physical GPU execution or provider availability.

The model should answer capability questions from this live structured report and the registry. It must not use web search or model memory to invent tools that are absent.

## 13. Phase 10 — Security, privacy, and policy

- Local data analysis should remain local by default.
- Web search/fetch must be clearly marked as networked and subject to explicit configuration.
- Do not upload local datasets to a remote provider to get statistics unless the user explicitly authorizes that specific transfer.
- Do not log full rows, secrets, credentials, or document contents by default.
- Enforce resource limits at parsing, computation, serialization, and tool-output boundaries.
- Honor context cancellation; no unbounded goroutines or retry loops.
- Use the existing approval mechanism for risky file writes, external network operations, and destructive actions.
- Keep tool arguments schema-validated and output compact; use artifact references for large results.
- Keep one authority for run lifecycle, tool registry, policy, artifact storage, and configuration.
- Add dependency license, vulnerability, and update review to any new third-party library.
- Never interpret a document or webpage's instructions as authorization to call tools or exfiltrate data.

## 14. Suggested delivery sequence

Use separate reviewable changes. Do not combine a new analytics engine, new UI framework, SQL engine, document parsers, and web provider in one large commit.

1. **Audit and baseline:** document current implementation and run existing tests.
2. **Core correctness:** fill gaps in statistics, quality diagnostics, deterministic behavior, and limits.
3. **Artifacts and exports:** harden atomic export, metadata, containment, and round-trip tests.
4. **Charts:** typed Go chart specifications rendered by the existing React UI.
5. **XLSX adapter:** optional, with ZIP-bomb and formula-safety protections.
6. **Statistical models:** explicitly scoped regression/metrics with known-answer and differential tests.
7. **File/document retrieval:** reuse existing indexing and artifact authorities; add missing extractors only.
8. **Web research:** provider adapter, citation store, SSRF protections, prompt-injection resistance.
9. **SQL decision:** only proceed if a concrete workload cannot be served safely by the current operations.
10. **Capability self-reporting:** derive status from live runtime and test evidence.
11. **Hardening:** fuzz, race, Windows/Linux CI, performance baselines, security review, docs.

After each stage: run targeted tests, full relevant suite, inspect the diff, update architecture/changelog docs, and record what was actually verified. Do not push, release, or change the module path as part of this report's implementation plan without explicit authorization.

## 15. Acceptance criteria

The work is ready for release consideration only when:

- [ ] The current `dataAnalysis` authority was extended rather than duplicated.
- [ ] Public contracts and null/numeric/join semantics are documented and covered by tests.
- [ ] Determinism tests pass on supported platforms.
- [ ] Resource limits, cancellation, and output caps are enforced and tested.
- [ ] Export artifacts are atomic, contained, hashed, and independently readable.
- [ ] XLSX, SQL, advanced ML, and web research are explicitly feature-gated until their tests pass.
- [ ] Web research rejects SSRF paths and stores provenance for citations.
- [ ] File retrieval enforces permissions and citations point to source ranges.
- [ ] Fuzz/race tests and representative performance benchmarks have recorded results.
- [ ] Windows and Linux repository gates pass on the exact candidate commit.
- [ ] Capability reporting distinguishes implemented, enabled, tested, and runtime-verified.
- [ ] Documentation lists real limitations and never equates CI with physical-host verification.
- [ ] No duplicate tool registry, dataset store, artifact store, scheduler, or run authority was introduced.

## 16. Source references and repository evidence

### Repository evidence reviewed

- Repository and current README: https://github.com/SHEYTANx13/SHEYTAN-LA
- Go module file: https://github.com/SHEYTANx13/SHEYTAN-LA/blob/main/go.mod
- Architecture documentation (linked from README): https://github.com/SHEYTANx13/SHEYTAN-LA/blob/main/docs/ARCHITECTURE.md
- Changelog: https://github.com/SHEYTANx13/SHEYTAN-LA/blob/main/docs/changelog.md
- Roadmap: https://github.com/SHEYTANx13/SHEYTAN-LA/blob/main/docs/ROADMAP.md

The repository's public README describes the existing `dataAnalysis` tool and explicitly says it has no SQL surface and that DuckDB was evaluated and rejected. That is the main architecture constraint reflected throughout this plan. Public web rendering did not expose every source file during this review, so implementation-level details must be verified in a fresh local checkout before coding.

### Official Go engineering references

- Diagnostics, profiling, and tracing: https://go.dev/doc/diagnostics
- Fuzz testing: https://go.dev/doc/tutorial/fuzz
- Go `testing` package: https://go.dev/pkg/testing/
- HTTP profiling endpoints: https://go.dev/pkg/net/http/pprof/

### Final implementation rule

**The model proposes. The tools execute. The laboratory verifies.** Every capability must have a defined contract, bounded execution, explicit trust boundary, reproducible tests, and truthful status reporting. Prefer a smaller feature with independently verified behavior over a broad feature list whose runtime claims cannot be substantiated.
