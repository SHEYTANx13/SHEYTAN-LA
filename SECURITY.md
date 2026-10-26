# Security Policy

SHEYTAN-LA is a local-first desktop AI engineering laboratory. Inference, tool execution, memory, and knowledge remain on the user's machine. This policy defines how to report vulnerabilities and the product's security posture.

## Reporting a Vulnerability

**Do not open a public issue for security reports.**

Use the repository's private security channel:

- [Report a vulnerability](https://github.com/SHEYTANx13/SHEYTAN-LA/security/advisories/new)

Include:
- Clear reproduction steps
- Affected version (or commit)
- Any runtime evidence (Advanced diagnostics export, engine stderr tail, logs)

You will receive an acknowledgment. Fixes ship through the normal release process. Credit is given unless you prefer otherwise.

## Security Posture

SHEYTAN-LA runs entirely on the user's machine. The managed data root lives under the application folder. The following principles govern the threat model and what constitutes a reportable defect:

### Local Attack Surface
The HTTP and WebSocket API bind to `127.0.0.1` by default. They are not internet-facing services. Reports about the local API are still valid, but the threat model is another local process or a malicious page that can reach the port.

### Agent-Executed Commands
The agent can run shell, file, browser, git, and network operations on the user's behalf under a sandbox policy that fails closed (resource-governed; network disabled by default in the Lab). You remain solely responsible for what you allow the agent to do. A sandbox escape, path traversal, or command-injection defect in our tool implementations is a security bug we want reported immediately.

### No Secrets in Logs
The log catcher must never record model file contents, credentials, tokens, headers, or secrets. The configuration GET surface redacts the remote API key. Any regression is treated as a security defect.

### Update Integrity
Engine packages are downloaded over HTTPS, hash-verified against the recorded identity, staged transactionally, and committed only after the engine actually starts and verifies. A failed verification rolls the previous package back byte-for-byte. Any weakening of that chain is a security defect.

### Downloads
Engine and model downloads go through the managed downloader with size caps and integrity checks. Unbounded or unverified download paths are defects.

## Scope

**In scope**
- Any component classified in `LICENSE.md` (both the proprietary product and the Apache-2.0-designated components)
- The build and release pipeline
- The behavior of the managed llama.cpp subprocess lifecycle as orchestrated by this product
- The local IPC channel (Unix domain socket / named pipe with peer-credential or SID checks)

**Out of scope**
- Vulnerabilities in third-party runtimes themselves (llama.cpp, the Go toolchain, Fyne, Three.js, system libraries). Report those upstream; we track and pick up their fixes.

## Supported Versions

Only the latest released version on the `main` branch receives security updates. Older releases are not supported.

---

SHEYTAN™ is a trademark of Parsaetak. © 2024–2026 Parsaetak. All rights reserved.
