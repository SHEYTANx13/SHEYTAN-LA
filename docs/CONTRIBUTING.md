# Contributing

Thank you for your interest in contributing to SHEYTAN-LA.

## Licensing — read this first

SHEYTAN-LA uses a **conservative mixed licensing model**. The complete picture — classification, notices, and both full legal texts — lives in the sole licensing artifact [`../LICENSE.md`](../LICENSE.md):

- `internal/humanize/` is designated **Apache-2.0** open.
- Everything else is **Proprietary** under the Proprietary License.

**What this means for contributions**

1. By submitting a contribution you grant Parsaetak the rights needed to distribute it under both licenses so the mixed model stays coherent (the proprietary grant needs it for the proprietary components; the Apache-2.0 grant applies if your contribution lands in an Apache-designated component).
2. Do not copy code from other projects into proprietary components unless its license (e.g. MIT/BSD) permits relicensing into the proprietary tree **and** you preserve the upstream attribution (add the upstream notice to `../LICENSE.md` §3).
3. Code copied into the Apache-2.0-designated components must itself be Apache-2.0-compatible and carry the upstream license header when required.
4. Never commit secrets, API keys, model files, or credentials — CI rejects these and the local-first privacy posture depends on it.

## Engineering standards

The repository enforces its contracts in CI; run them locally first:

```bash
# Go: build, tests, static analysis (headless tag covers non-GUI hosts)
go build -tags headless ./...
go test ./internal/... -tags headless -count=1
go test ./... -count=1
go vet ./...
gofmt -l .

# Desktop (requires Fyne graphics development libraries)
go test ./ui/fyne/ -count=1
```

Beyond green CI, the project’s core rules:

- **Honesty over optimism.** Never convert build success into runtime claims. A feature is done when it is *verified* — real process, real probe, real evidence — and the tests prove the failure paths too.
- **Evidence-gated claims.** The accelerator surface is the canonical example: a GPU/Vulkan claim requires runtime evidence (engine enumeration or a measured offload line), never a DLL check.
- **One authority per decision.** Sampling validation lives in `internal/config/sampling.go`; model resolution in `llm.ResolveModelPath`; engine installs in `internal/updater`. Do not add a second one.
- **Deterministic failures must fail fast.** An invalid value is refused before any engine process starts, never fed to a retry ladder.
- **Comments carry the why.** The codebase documents defects it fixed inline so regressions can be traced. Keep that discipline: a fix lands with its narrative.

## Commit and review flow

1. Fork / branch from `main`.
2. Make the change with tests that fail without it.
3. Update the affected documentation — `README.md`, `ARCHITECTURE.md`, `agent.md`, `FEATURES.md`, `ROADMAP.md` and `retired.md` are part of the deliverable, not afterthoughts.
4. Sign your commits (`git commit -s`) with a clear subject line when the change is release-relevant.
5. Open a pull request describing: defect narrative (runtime evidence if a bug), the fix, and the verification you ran. Claims without evidence will be asked for evidence.

## Reporting problems

- **Security**: see [`../SECURITY.md`](../SECURITY.md) (do not open public issues for security reports).
- **Bugs and defects**: open an issue with runtime evidence (logs from Advanced diagnostics, engine stderr, repro steps).
