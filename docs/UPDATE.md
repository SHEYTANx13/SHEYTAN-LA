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

## Honesty rules that never change

- Build success is never converted into a runtime claim.
- Hardware detection is never reported as execution evidence.
- A feature is done only when source and deterministic evidence both exist at HEAD.
- Documentation never closes a failure item by itself.

For the full architecture of the update and admission paths, see [`ARCHITECTURE.md`](ARCHITECTURE.md). For the capability roadmap, see [`ROADMAP.md`](ROADMAP.md).
