# SECURITY.md

## Reporting a vulnerability

**Do not open a public issue for security reports.**

Contact the maintainer directly through the repository's private security
channel (GitHub "Report a vulnerability") or open a private discussion:

- https://github.com/Parsaetak/SHEYTAN-Local-Agent/security/advisories/new

Please include reproduction steps, affected version, and any runtime
evidence (Advanced diagnostics export, engine stderr tail). You will
receive an acknowledgment; fixes ship through the normal release process
with credit unless you prefer otherwise.

## Security posture

SHEYTAN-Local-Agent is a **local-first** desktop agent: inference and tool
execution run on the user's machine, and the managed data root lives
under the application folder. The posture, and what it means for reports:

- **Local attack surface.** The HTTP/WebSocket API binds to
  `127.0.0.1` by default; it is not an internet-facing service. Treat
  reports about the local API accordingly (still real, but the threat
  model is another local process or a malicious page reaching the port).
- **Agent-executed commands.** The agent can run shell, file, browser,
  git and network operations on the user's behalf under a sandbox policy
  that fails closed (resource-governed, disabled network by default in
  the Lab). You are solely responsible for what you allow it to do —
  but a sandbox escape, path traversal, or command-injection defect in
  OUR tool implementations is a security bug we want immediately.
- **No secrets in logs.** The log catcher must never record model file
  contents, credentials, tokens, headers or secrets; the config GET
  surface redacts the remote API key. Regressions here are security
  defects.
- **Update integrity.** Engine packages are downloaded over HTTPS,
  hash-verified against the recorded identity, staged transactionally,
  and committed only after the engine actually starts and verifies; a
  failed verification rolls the previous package back byte-for-byte.
  Any weakening of that chain is a security defect.
- **Downloads.** Engine/model downloads go through the managed downloader
  with size caps and integrity checks. Unbounded or unverified download
  paths are defects.

## Scope

In scope: any component classified in `LICENSE.md` §2 (both the
proprietary product and the Apache-2.0-designated components), the build
and release pipeline, and the behavior of the managed llama.cpp
subprocess lifecycle as orchestrated by this product.

Out of scope: vulnerabilities in third-party runtimes themselves
(llama.cpp, Chromium, the Go toolchain, npm packages) — report those
upstream; we track and pick up their fixes.
