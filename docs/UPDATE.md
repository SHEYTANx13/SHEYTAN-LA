# Update & Maintenance

This document describes the **current** maintenance and update behavior of SHEYTAN-LA. The authoritative per-release history lives exclusively in [`changelog.md`](changelog.md).

---

## Engine and model updates

- The downloader is the single download authority.
- Engine packages are downloaded over HTTPS, hash-verified against the recorded identity, staged transactionally, and committed only after the engine actually starts and verifies.
- A failed verification rolls the previous package back byte-for-byte.
- Explicit variant requests never fall back silently to CPU.
- Models are managed in-app; GGUF files can also be imported from disk.

## Application updates

- Release assets are published only on version tags.
- The publish job produces a deterministic `SHA256SUMS.txt` and a `release-manifest.json` consumed by the in-app updater.
- The updater respects the same integrity and transactional discipline as the engine path.

## Runtime Governor and resource policy

- The Governor continuously samples live pressure and computes a resource state, an admission envelope, and a self-model.
- Model loads and heavyweight workloads are admitted against a resident budget.
- Critical pressure triggers cooperative cancellation of active runs — never process killing.
- The Governor is a policy authority; enforcement lives in the existing subsystems.

## Logging and diagnostics

- Logs never contain secrets, API keys, model contents, or capability tokens.
- Advanced diagnostics export is available for support and bug reports.
- Every CI run produces one consolidated plain-text log artifact for the entire matrix.

## CI and release hygiene

- Windows uses MSYS2 UCRT64.
- Linux desktop smoke runs under Xvfb.
- The stress suite and release-surface gates are required.
- The codename-removal gate remains enabled.
- Publish occurs only on a version tag after a green main matrix.

## Developer dependency & toolchain maintenance

The RUNTIME updater above is user-facing. This subsection is the
developer process for keeping the application's own build requirements
current — it is not runtime user instructions.

- **Inventory first**: every versioned application/build requirement is
  audited as one set — Go modules (`go list -m all`, `go mod graph`,
  `go mod why`), workflow action references, native prerequisites
  (MSYS2/UCRT64 packages, NSIS, apt set, runner labels), vendored static
  assets (Three.js under `internal/studio/assets/`) and runtime-updated
  engine resources.
- **Stable tags only**: dependencies move to the latest STABLE tagged
  release verified upstream at execution time (no alpha/beta/rc/pseudo
  versions unless a parent dependency requires one — then the reason and
  upstream origin are recorded, e.g. the Fyne-required systray and
  go-gl pseudo-versions, and the chromedp v0.16.0 + cdproto pairing
  pending its dedicated migration session).
- **Workflow action references use upstream release tags** (`@v7`, `@v6`,
  `@v2`, …), never commit hashes or moving branch refs — an explicit
  maintainability choice, mitigated by official sources,
  least-privilege permissions and the single weekly Dependabot group.
- **One combined update stream**: `.github/dependabot.yml` groups all
  supported ecosystems (github-actions + gomod) into ONE weekly PR.
  Dependabot does not manage manual versions (NSIS, MSYS2 packages,
  vendored Three.js, engine bundles) — those are pinned in the workflow
  and recorded in the run log.
- **Vendored assets move as coherent sets**: Three.js core/module/
  OrbitControls update together from one upstream tag with the
  documented local security patch re-applied (see
  [`3dstudio.md`](3dstudio.md)).
- **Proof**: `go mod tidy` is idempotent, `go mod verify` passes, the
  affected integration suites run, and the toolchain version (exact Go
  patch) is identical in `go.mod` and every workflow lane.

## Honesty rules that never change

- Build success is never converted into a runtime claim.
- Hardware detection is never reported as execution evidence.
- A feature is done only when source and deterministic evidence both exist at HEAD.
- Documentation never closes a failure item by itself.

For the full architecture of the update and admission paths, see [`ARCHITECTURE.md`](ARCHITECTURE.md). For the capability roadmap, see [`ROADMAP.md`](ROADMAP.md).
