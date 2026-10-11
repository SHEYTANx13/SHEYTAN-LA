# CONTRIBUTING.md

Thank you for your interest in contributing to SHEYTAN-Local-Agent.

## Licensing — read this first

SHEYTAN-Local-Agent uses a **conservative mixed licensing model**
(the complete picture — classification, notices, and both full legal
texts — lives in the sole licensing artifact `LICENSE.md`):

- `internal/humanize/` is designated **Apache-2.0** open.
- Everything else is **Proprietary** under the Parsaetak Proprietary
  License v1.1.

**What this means for contributions:**

1. By submitting a contribution you grant Parsaetak the rights needed to
   distribute it under BOTH licenses so the mixed model stays coherent
   (the proprietary grant needs it for the proprietary components; the
   Apache-2.0 grant applies if your contribution lands in an
   Apache-designated component).
2. Do not copy code from other projects into proprietary components
   unless its license (e.g. MIT/BSD) permits relicensing into the
   proprietary tree AND you preserve the upstream attribution (add the
   upstream notice to `LICENSE.md` §3).
3. Code copied into the Apache-2.0-designated components must itself be
   Apache-2.0-compatible and carry the upstream license header when
   required.
4. Never commit secrets, API keys, model files, or credentials — CI
   rejects these and the local-first privacy posture depends on it.

## Engineering standards (the short list)

The repository enforces its contracts in CI; run them locally first:

```bash
# Go: build, tests, static analysis (headless tag covers non-GUI hosts)
go build -tags headless ./...
go test ./internal/... -tags headless -count=1
go test ./... -run Test -count=1
go vet ./...

# Frontend
npm ci
npm run typecheck
npm run lint
npm run test:units
npm run build
```

Beyond green CI, the project's core rules:

- **Honesty over optimism.** Never convert build success into runtime
  claims. A feature is done when it is *verified* — real process, real
  probe, real evidence — and the tests prove the failure paths too.
- **Evidence-gated claims.** The accelerator surface is the canonical
  example: a GPU/Vulkan claim requires runtime evidence
  (engine enumeration or a measured offload line), never a DLL check.
- **One authority per decision.** Sampling validation lives in
  `internal/config/sampling.go`; model resolution in `llm.ResolveModelPath`;
  engine installs in `internal/updater`. Do not add a second one.
- **Deterministic failures must fail fast.** An invalid value is refused
  before any engine process starts (`startLocked`'s sampling gate), never
  fed to a retry ladder (see ROADMAP.md for the v1.6.1 history).
- **Comments carry the why.** The codebase documents defects it fixed
  inline (`vX.Y.Z:` markers) so regressions can be traced. Keep that
  discipline: a fix lands with its narrative.

## Commit and review flow

1. Fork / branch from `main`.
2. Make the change with tests that fail without it.
3. Update the affected documentation — `README.md`, `ARCHITECTURE.md`,
   `agent.md`, and `ROADMAP.md` are part of the deliverable, not
   afterthoughts (ROADMAP.md is the forward plan the next session reads
   first).
4. Sign your commits (`git commit -s`) with a `vX.Y.Z: area — what`
   subject line when the change is release-relevant.
5. Open a pull request describing: defect narrative (runtime evidence
   if a bug), the fix, and the verification you ran. Claims without
   evidence will be asked for evidence.

## Reporting problems

- Security: see `SECURITY.md` (do not open public issues for security
  reports).
- Bugs and defects: open an issue with runtime evidence (logs from
  Advanced diagnostics, engine stderr, repro steps).
