# License

Copyright © 2024–2026 **SHEYTAN** and **Parsaetak**. All rights reserved.

This is the **sole licensing artifact** of SHEYTAN-LA (“the Software”). It contains the complete licensing picture: mixed-license model, component classification, third-party attribution, and the full terms of both licenses in force.

No other license, notice, or copying file in this repository carries legal authority.

---

## 1. Project licensing model

SHEYTAN-LA is distributed under a conservative **mixed licensing model**. Every file in this repository — past, present, and future — and every artifact built from it carries exactly one of two licenses:

| License | Scope |
|---------|-------|
| **Proprietary License** (see §5) | Everything classified Proprietary (the default) |
| **Apache License 2.0** (see §4) | Components explicitly designated Open |

**Rules**

1. The default is Proprietary. Material becomes open only by explicit designation in §2 — never by omission.
2. The classification in §2 is the single authority for which license governs which component.

This license applies to all current source, documentation, assets, tests, scripts, packaging, all previously existing / retired / legacy material (including items listed in `docs/retired.md` or retained only in git history), and all future updates and modifications.

---

## 2. Component classification

### Open components (Apache-2.0)

| Component | Path | Notes |
|-----------|------|-------|
| Humanize formatting utilities | `internal/humanize/` | Pure formatting helpers with no product-specific logic. Carries `SPDX-License-Identifier: Apache-2.0` headers. |

These components may be reused under Apache-2.0 as extracted units.

### Proprietary components

Everything else is Proprietary under the Proprietary License (§5), including:

- All product implementation (`internal/` except the Open component, `cmd/`, `ui/fyne/`, `native/`)
- Brand, assets, icons, and designs
- All product documentation (`README.md`, `docs/*`, including `docs/retired.md`)
- Build, release, CI machinery, packaging, and tests
- Governance files (this `LICENSE.md`, `SECURITY.md`, `SIGNATURE`)
- All retired, legacy, or historical code paths and mechanisms

### Determining the license of a file

1. Locate the component in the tables above.
2. Files inside an Apache-2.0 component carry the SPDX header.
3. Everything not explicitly classified as Open is Proprietary.

---

## 3. Third-party software and attribution

SHEYTAN-LA builds on, embeds, or downloads third-party software. Each remains under its own license.

| Component | License | Notes |
|-----------|---------|-------|
| llama.cpp (downloaded engine) | MIT | Fetched at runtime — never committed |
| Fyne (`fyne.io/fyne/v2`) | BSD-3-Clause | Product UI toolkit |
| Three.js 0.182.0 (vendored) | MIT | Embedded for optional 3D Studio |
| Other Go modules (gorilla/websocket, chromedp, bild, xdg, etc.) | MIT / BSD / ISC (per package) | Build-time dependencies |
| Go standard library + `golang.org/x/*` | BSD-style (Go) | Toolchain |

Exact license texts ship with each package.

---

## 4. Apache License 2.0

The components designated Open in §2 are governed by the standard Apache License, Version 2.0 (January 2004). Full text: http://www.apache.org/licenses/LICENSE-2.0

---

## 5. Proprietary License

**Proprietary License**

Copyright © 2024–2026 SHEYTAN and Parsaetak. All rights reserved.

### 1. Grant of License

Subject to the terms of this license, SHEYTAN and Parsaetak (collectively “Licensor”) grant you a limited, non-exclusive, non-transferable, non-sublicensable, revocable license to install and use the Software on devices you own or control, solely for your personal or internal business purposes.

### 2. Restrictions

You may **not**:

- Copy, modify, adapt, translate, reverse engineer, decompile, disassemble, or create derivative works of the Software, except to the extent applicable law expressly prohibits such restriction.
- Distribute, publish, sublicense, sell, rent, lease, or otherwise transfer the Software or any portion of it to any third party.
- Remove, alter, or obscure any proprietary notices, labels, or marks on the Software.
- Use the Software to develop a competing product or service.
- Use the Software in any manner that violates applicable law.

### 3. Trademark

SHEYTAN™ and the SHEYTAN logo are trademarks of the Licensor. This license does not grant any right to use the SHEYTAN name, logo, or branding beyond what trademark law allows. Nothing herein implies endorsement of derived works.

### 4. Ownership

The Software is licensed, not sold. Licensor retains all right, title, and interest in and to the Software, including all intellectual property rights. This license does not transfer any ownership.

### 5. Coverage of past and future material

This Proprietary License covers all current proprietary components, all previously existing proprietary files, code paths, mechanisms and documentation (including material listed in `docs/retired.md` or present only in git history), and all future updates, additions, modifications, and releases of the Software.

### 6. Privacy and data

The Software is local-first. Model weights, conversation data, and tool outputs remain on your machine unless you explicitly configure a remote provider. When a remote provider is enabled, prompts are sent to the endpoint you configure; the Licensor is not responsible for how third parties handle that data. Local logs stay on your disk until you choose to export them.

### 7. Acceptable use

You agree to use the Software only in compliance with applicable law. You are solely responsible for commands the agent executes on your behalf, including any file, shell, browser, git, or network operations.

### 8. Disclaimer of Warranty

THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES, OR OTHER LIABILITY ARISING FROM, OUT OF, OR IN CONNECTION WITH THE SOFTWARE OR ITS USE.

### 9. Termination

This license terminates automatically if you breach any term. Upon termination you must stop using the Software and delete all copies.

### 10. Changes

The Licensor may revise this agreement for future releases. Continued use after an update constitutes acceptance of the revised terms.

---

## 6. Governance

Only the repository maintainer may reclassify a component:

1. Update the tables in §2 with the rationale.
2. For a move to Open: add `SPDX-License-Identifier: Apache-2.0` headers and confirm no coupling to proprietary mechanisms.
3. For a move to Proprietary: remove the Apache headers.
4. Describe the change in the commit.

---

## 7. Contact

Licensing questions, proprietary-use inquiries, and exceptions:  
https://github.com/SHEYTANx13
