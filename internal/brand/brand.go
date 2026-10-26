// Package brand holds the SHEYTAN™ identity: trademark attribution,
// copyright, and license text. Every surface (GUI, CLI, docs, diagnostics)
// renders these constants so legal notices stay consistent across releases.
//
// Legal structure:
//   - Copyright holder & licensor:  Parsaetak (https://github.com/Parsaetak)
//   - Trademark:                    SHEYTAN™ — a trademark of Parsaetak,
//     used as the product name for this app.
package brand

import "time"

const (
	// Name is the short product name.
	Name = "SHEYTAN"
	// FullName is the complete product name.
	FullName = "SHEYTAN-Local-Agent"
	// Trademark is the brand with the ™ mark, for display surfaces.
	Trademark = "SHEYTAN™"
	// Licensor is the legal entity that owns the copyright and license.
	Licensor = "Parsaetak"
	// LicensorURL is the official licensor contact point.
	LicensorURL = "https://github.com/Parsaetak"
	// TrademarkNotice is the formal trademark attribution line.
	TrademarkNotice = "SHEYTAN and the SHEYTAN logo are trademarks of Parsaetak."
	// LicenseName is the human name of the license. v1.7.2: the
	// conservative mixed model — Apache-2.0 for explicitly designated
	// open components, the Parsaetak Proprietary License v1.1 for the
	// SHEYTAN-specific proprietary material — both legal texts carried
	// IN FULL inside LICENSE.md (the one, complete licensing document
	// and sole licensing artifact).
	LicenseName = "Mixed: Apache-2.0 (designated components) + Parsaetak Proprietary v1.1"

	// SignedBy is the application author/signer — v1.0.8. Every release
	// of SHEYTAN-Local-Agent is authored and signed under this name; it
	// is embedded in the exe version resource (CompanyName), printed in
	// the About dialog, and carried in the SIGNATURE block of each
	// distribution archive.
	SignedBy = "Parsa Tak"
	// SignedByRole is the role line printed under the signature.
	SignedByRole = "Author & Application Signer"
)

// LogoSVG is the full-color flame mark (gradient flame on a deep ember
// disc) used everywhere the brand appears: the app UI, the window icon,
// the splash — and, since v1.0.5, the rendered .exe icon. It lives here so
// the GUI theme and the build-time icon generator can never drift apart.
const LogoSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 24 24">
<defs>
  <linearGradient id="flame" x1="0" y1="1" x2="0" y2="0">
    <stop offset="0%" stop-color="#C9182B"/>
    <stop offset="45%" stop-color="#FF3B30"/>
    <stop offset="80%" stop-color="#FF6B1A"/>
    <stop offset="100%" stop-color="#FFC53D"/>
  </linearGradient>
  <linearGradient id="flameInner" x1="0" y1="1" x2="0" y2="0">
    <stop offset="0%" stop-color="#FFDD55"/>
    <stop offset="100%" stop-color="#FF8A50"/>
  </linearGradient>
</defs>
<circle cx="12" cy="12" r="11.4" fill="#120808"/>
<circle cx="12" cy="12" r="11.4" fill="none" stroke="#FF3B30" stroke-width=".5" opacity=".8"/>
<path d="M13.5 2.2s.74 2.65.74 4.8c0 2.06-1.35 3.73-3.41 3.73-2.07 0-3.63-1.67-3.63-3.73l.03-.36C5.2 6.6 4 9.7 4 13.1c0 4.42 3.58 8 8 8s8-3.58 8-8c0-5.4-2.59-10.2-6.5-13.33z" fill="url(#flame)"/>
<path d="M12.2 10.4c1.5 1.1 2.6 2.4 2.6 4.2 0 1.9-1.3 3.4-3 3.4-1.1 0-2-.6-2.5-1.5-.2 1.5.8 3.3 2.1 4-.5.1-1 .2-1.6.1-2.4-.3-4.1-2.5-3.8-5 .2-1.6 1.2-2.6 2.2-3.6.9-.9 1.7-1.9 2-3.1.6.7 1.3 1.2 2 1.5z" fill="url(#flameInner)" opacity=".9"/>
</svg>`

// CopyrightYears returns the copyright range "2024–<current year>".
func CopyrightYears() string {
	y := time.Now().Year()
	if y <= 2024 {
		return "2024"
	}
	return "2024–" + itoa(y)
}

// Copyright returns the canonical copyright line, e.g.
// "© 2024–2026 Parsaetak. All rights reserved."
func Copyright() string {
	return "© " + CopyrightYears() + " " + Licensor + ". All rights reserved."
}

// Notice returns the short one-line legal footer used in the UI.
func Notice() string {
	return Trademark + " · " + Copyright()
}

// SignatureLine returns the one-line authorship attribution, e.g.
// "Signed by Parsa Tak — Author & Application Signer".
func SignatureLine() string {
	return "Signed by " + SignedBy + " — " + SignedByRole
}

// SignatureBlock returns the full signature text carried in the About
// dialog and the SIGNATURE file of every distribution archive.
func SignatureBlock(version string) string {
	return "SHEYTAN-Local-Agent v" + version + "\n" +
		"Signed by: " + SignedBy + "\n" +
		"Role:      " + SignedByRole + "\n" +
		"Product:   " + FullName + " (" + Trademark + ")\n" +
		"Licensor:  " + Licensor + " <" + LicensorURL + ">\n" +
		Copyright() + "\n" +
		TrademarkNotice + "\n"
}

// LicenseFooter is the compact attribution block shown in About dialogs.
// v1.7.2: LICENSE.md is the sole licensing artifact — it carries the
// classification, the notices and BOTH full legal texts.
const LicenseFooter = "SHEYTAN™ is a trademark of Parsaetak.\nLicensed under the conservative mixed model — Apache-2.0 for designated open components, Parsaetak Proprietary for SHEYTAN-specific material. The complete licensing picture lives in LICENSE.md (the sole licensing artifact).\n" + LicensorURL

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// LicenseText is the COMPLETE consolidated license document (v1.7.2
// single-license-file architecture). It contains the whole licensing
// picture: the mixed-model summary, the component classification, the
// third-party attribution notices, the trademark notice, governance and
// contact, plus the FULL legal texts of the Apache License 2.0 and the
// Parsaetak Proprietary License v1.1.
//
// scripts/gen-license.go writes EXACTLY this text to LICENSE.md at the
// repository root — the ONE license artifact the repository is allowed to
// carry (enforced by internal/releasecontract). The file can therefore
// never drift from what the app prints through the license CLI.
const LicenseText = `# LICENSE.md — SHEYTAN-Local-Agent licensing

Copyright © 2024–2026 Parsaetak (https://github.com/Parsaetak). All rights
reserved.

This is the **sole licensing artifact** of SHEYTAN-Local-Agent ("the
Software"). It carries the complete licensing picture in one document:
the mixed-license model, the component classification, the third-party
attribution notices, the trademark notice, the governance/contact points,
and the FULL legal texts of both licenses in force — the Apache License
2.0 and the Parsaetak Proprietary License v1.1. No other license,
licence, notice, or copying file in this repository carries legal
authority; where an earlier release pointed at separate files, those
files were consolidated into this document and removed.

---

## 1. Project licensing model

SHEYTAN-Local-Agent is distributed under a deliberately **conservative
mixed licensing model**. Every file in this repository — and every
artifact built from it — carries exactly one of two licenses:

| License | Text | Scope |
|---|---|---|
| Parsaetak Proprietary License v1.1 | [§5](#5-parsaetak-proprietary-license-v11) | Everything classified **Proprietary** (the default) |
| Apache License 2.0 | [§4](#4-apache-license-20) | Components explicitly designated **Open** |

Two rules define the whole model:

1. **The default is Proprietary.** Material becomes open only by explicit
   designation in [§2](#2-component-classification) — never by omission.
2. **The classification decides.** [§2](#2-component-classification) is
   the single authority for which license governs which component. When a
   file's license is unclear, the classification decides it.

The classification attaches to **actual material** (code, documentation,
assets, implementations, and other legally relevant files). It does not
claim that abstract ideas, techniques, or functionality are inherently
protected IP; the proprietary grant covers the concrete expressions
classified below.

---

## 2. Component classification

### Open components (Apache-2.0)

| Component | Path | Notes |
|---|---|---|
| Humanize formatting utilities | internal/humanize/ | Pure, self-contained formatting helpers (bytes, durations, counts) with no product-specific logic, no state, and no coupling to any SHEYTAN subsystem. Carries "SPDX-License-Identifier: Apache-2.0" file headers. |

These components may be reused under Apache-2.0 **as extracted units**.
They remain part of the repository's tree; the classification is by
component, so every file under the listed path is covered.

### Proprietary components

Everything else in the repository is Proprietary under the Parsaetak
Proprietary License v1.1 (§5), including (non-exhaustive, grouped by
area):

| Area | Paths | Classification rationale |
|---|---|---|
| SHEYTAN product implementation | internal/ (all packages except the Open components above), cmd/ (including cmd/sheytan-local-agent/), web/ | The product-specific implementation: engine lifecycle, sampling gate, model-selection state machine, accelerator resolver, transactional updater/installer, agent orchestrator, research engine, coding lab, sessions, vision, calibration. |
| Native C++ engine | native/engine/ | SHEYTAN-specific proprietary inference engine implementation, tokenizer, scheduler, GGUF loader. |
| UI/UX | ui/fyne/ (the ONE Go + Fyne application), assets, theme tokens | Product-specific implementation and interface designs. |
| Brand, assets and designs | internal/brand/ (logo SVG and identity constants), build/ (icon, packaging art), scripts/gen-syso/logo-512.png | Trademarked assets and designs. |
| Product documentation | README.md, docs/ARCHITECTURE.md, docs/UPDATE.md, docs/agent.md, docs/ROADMAP.md, docs/REPORT.md, internal/aicontext/AI-CONTEXT.md, docs/worklog.md, native/engine/README.md | Product documentation and engineering records. |
| Build, release and CI machinery | scripts/, packaging/, .github/, e2e/ (harness), Makefiles | Product-specific delivery pipeline. |
| Governance files | LICENSE.md, docs/CONTRIBUTING.md, SECURITY.md, SIGNATURE | Legal and governance material. |
| Test suites | all *_test.go, e2e/*.spec.ts, fixtures except where a component is designated Open | Product verification records exercising proprietary components. |

### How to determine the license of a file

1. Find the component the file belongs to in the tables above; the
   classification is authoritative.
2. Files inside an Apache-2.0-designated component carry an
   "SPDX-License-Identifier: Apache-2.0" header.
3. Everything not explicitly classified as open above is Proprietary
   under the Parsaetak Proprietary License v1.1 (§5) — the conservative
   default: material is open ONLY by explicit designation, never by
   omission.

---

## 3. Third-party software and attribution notices

SHEYTAN-Local-Agent builds on, embeds, or downloads third-party software
(Go modules, frontend npm packages, provisioning artifacts and the
native-engine test tooling). Each component remains under its own
license; nothing in this repository modifies any third-party licensing
term. This section carries the attribution notices required when
redistributing builds (including the Apache-2.0 §4(d) notices).

### llama.cpp — the inference engine

The managed llama.cpp engine binaries are downloaded at runtime from
https://github.com/ggml-org/llama.cpp (MIT license) into the
application's managed bin directory and executed as a subprocess.

    Copyright (c) 2023-2026 The ggml authors
    MIT License — https://github.com/ggml-org/llama.cpp/blob/master/LICENSE

llama.cpp is NOT distributed inside this repository or its release
archives.

### Third-party material overview

The product UI is the ONE Go + Fyne application (v1.11.0 one-Go-UI
migration): no Qt runtime and no Node/React stack ships or is linked.

| Component | License | Where it lives |
|---|---|---|
| llama.cpp (downloaded engine) | MIT | fetched at runtime into the managed bin directory — never committed |
| Fyne (fyne.io/fyne/v2) | BSD-3-Clause | the ONE product UI toolkit (linked into every desktop build) |
| gorilla/websocket | BSD-style | build-time Go dependency |
| chromedp + cdproto | MIT | build-time Go dependency |
| bild, xdg, winres, go-isatty, nfnt/resize, gobwas/* | MIT / BSD-style / ISC-style (per package) | build-time Go dependencies |
| Go standard library + golang.org/x/* | BSD-style (Go license) | toolchain and build-time dependencies |

### Go dependencies (linked into the binary)

| Package | License | Copyright |
|---|---|---|
| github.com/gorilla/websocket | BSD-style | Copyright (c) 2013 The Gorilla WebSocket Authors |
| github.com/chromedp/chromedp, github.com/chromedp/cdproto | MIT | Copyright (c) 2016-2025 Kenneth Shaw |
| github.com/anthonynsimon/bild | MIT | Copyright (c) 2016-2024 Anthony Simon |
| github.com/adrg/xdg | MIT | Copyright (c) 2014 Adrian-George Bostan |
| github.com/tc-hib/winres | ISC-style | Copyright (c) 2021 Thomas Combeléran |
| github.com/mattn/go-isatty | MIT | Copyright (c) Yasuhiro Matsumoto |
| github.com/nfnt/resize | BSD-style | Copyright (c) 2012 Jan Schlicht |
| github.com/gobwas/* | MIT | Copyright (c) 2017 Sergey Kamardin et al. |
| github.com/go-json-experiment/json | BSD-style (Go) | Copyright (c) 2020 The Go Authors |
| golang.org/x/net, golang.org/x/sys, golang.org/x/image | BSD-style (Go) | Copyright The Go Authors |

### Frontend dependencies (bundled into web/static by the build)

| Package | License | Copyright |
|---|---|---|
| react, react-dom | MIT | Copyright (c) Meta Platforms, Inc. and affiliates |
| zustand | MIT | Copyright (c) 2019 Paul Henschel |
| react-markdown | MIT | Copyright (c) Espen Hovlandsdal et al. |
| remark-gfm | MIT | Copyright (c) Titus Wormer et al. |
| rehype-highlight (Highlight.js) | BSD-3-Clause | Copyright (c) Ivan Sagalaev and other contributors |

The exact license texts ship with each package in its published archive
and are available in the respective repositories/registries.

---

## 4. Apache License 2.0

The complete, legally binding Apache License 2.0 text governing the
components explicitly designated Open in §2:

<!-- apache-2.0-text-begin -->

                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

   TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

   1. Definitions.

      "License" shall mean the terms and conditions for use, reproduction,
      and distribution as defined by Sections 1 through 9 of this document.

      "Licensor" shall mean the copyright owner or entity authorized by
      the copyright owner that is granting the License.

      "Legal Entity" shall mean the union of the acting entity and all
      other entities that control, are controlled by, or are under common
      control with that entity. For the purposes of this definition,
      "control" means (i) the power, direct or indirect, to cause the
      direction or management of such entity, whether by contract or
      otherwise, or (ii) ownership of fifty percent (50%) or more of the
      outstanding shares, or (iii) beneficial ownership of such entity.

      "You" (or "Your") shall mean an individual or Legal Entity
      exercising permissions granted by this License.

      "Source" form shall mean the preferred form for making modifications,
      including but not limited to software source code, documentation
      source, and configuration files.

      "Object" form shall mean any form resulting from mechanical
      transformation or translation of a Source form, including but
      not limited to compiled object code, generated documentation,
      and conversions to other media types.

      "Work" shall mean the work of authorship, whether in Source or
      Object form, made available under the License, as indicated by a
      copyright notice that is included in or attached to the work
      (an example is provided in the Appendix below).

      "Derivative Works" shall mean any work, whether in Source or Object
      form, that is based on (or derived from) the Work and for which the
      editorial revisions, annotations, elaborations, or other modifications
      represent, as a whole, an original work of authorship. For the purposes
      of this License, Derivative Works shall not include works that remain
      separable from, or merely link (or bind by name) to the interfaces of,
      the Work and Derivative Works thereof.

      "Contribution" shall mean any work of authorship, including
      the original version of the Work and any modifications or additions
      to that Work or Derivative Works thereof, that is intentionally
      submitted to Licensor for inclusion in the Work by the copyright owner
      or by an individual or Legal Entity on behalf of the copyright owner.
      For the purposes of this definition, "submitted"
      means any form of electronic, verbal, or written communication sent
      to the Licensor or its representatives, including but not limited to
      communication on electronic mailing lists, source code control systems,
      and issue tracking systems that are managed by, or on behalf of, the
      Licensor for the purpose of discussing and improving the Work, but
      excluding communication that is conspicuously marked or otherwise
      designated in writing by the copyright owner as "Not a Contribution."

      "Contributor" shall mean Licensor and any individual or Legal Entity
      on behalf of whom a Contribution has been received by Licensor and
      subsequently incorporated within the Work.

   2. Grant of Copyright License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      copyright license to reproduce, prepare Derivative Works of,
      publicly display, publicly perform, sublicense, and distribute the
      Work and such Derivative Works in Source or Object form.

   3. Grant of Patent License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      (except as stated in this section) patent license to make, have made,
      use, offer to sell, sell, import, and otherwise transfer the Work,
      where such license applies only to those patent claims licensable
      by such Contributor that are necessarily infringed by their
      Contribution(s) alone or by combination of their Contribution(s)
      with the Work to which such Contribution(s) was submitted. If You
      institute patent litigation against any entity (including a
      cross-claim or counterclaim in a lawsuit) alleging that the Work
      or a Contribution incorporated within the Work constitutes direct
      or contributory patent infringement, then any patent licenses
      granted to You under this License for that Work shall terminate
      as of the date such litigation is filed.

   4. Redistribution. You may reproduce and distribute copies of the
      Work or Derivative Works thereof in any medium, with or without
      modifications, and in Source or Object form, provided that You
      meet the following conditions:

      (a) You must give any other recipients of the Work or
          Derivative Works a copy of this License; and

      (b) You must cause any modified files to carry prominent notices
          stating that You changed the files; and

      (c) You must retain, in the Source form of any Derivative Works
          that You distribute, all copyright, patent, trademark, and
          attribution notices from the Source form of the Work,
          excluding those notices that do not pertain to any part of
          the Derivative Works; and

      (d) If the Work includes a "NOTICE" text file as part of its
          distribution, then any Derivative Works that You distribute must
          include a readable copy of the attribution notices contained
          within such NOTICE file, excluding those notices that do not
          pertain to any part of the Derivative Works, in at least one
          of the following places: within a NOTICE text file distributed
          as part of the Derivative Works; within the Source form or
          documentation, if provided along with the Derivative Works; or,
          within a display generated by the Derivative Works, if and
          wherever such third-party notices normally appear. The contents
          of the NOTICE file are for informational purposes only and
          do not modify the License. You may add Your own attribution
          notices within Derivative Works that You distribute, alongside
          or as an addendum to the NOTICE text from the Work, provided
          that such additional attribution notices cannot be construed
          as modifying the License.

      You may add Your own copyright statement to Your modifications and
      may provide additional or different license terms and conditions
      for use, reproduction, or distribution of Your modifications, or
      for any such Derivative Works as a whole, provided Your use,
      reproduction, and distribution of the Work otherwise complies with
      the conditions stated in this License.

   5. Submission of Contributions. Unless You explicitly state otherwise,
      any Contribution intentionally submitted for inclusion in the Work
      by You to the Licensor shall be under the terms and conditions of
      this License, without any additional terms or conditions.
      Notwithstanding the above, nothing herein shall supersede or modify
      the terms of any separate license agreement you may have executed
      with Licensor regarding such Contributions.

   6. Trademarks. This License does not grant permission to use the trade
      names, trademarks, service marks, or product names of the Licensor,
      except as required for reasonable and customary use in describing the
      origin of the Work and reproducing the content of the NOTICE file.

   7. Disclaimer of Warranty. Unless required by applicable law or
      agreed to in writing, Licensor provides the Work (and each
      Contributor provides its Contributions) on an "AS IS" BASIS,
      WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
      implied, including, without limitation, any warranties or conditions
      of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS FOR A
      PARTICULAR PURPOSE. You are solely responsible for determining the
      appropriateness of using or redistributing the Work and assume any
      risks associated with Your exercise of permissions under this License.

   8. Limitation of Liability. In no event and under no legal theory,
      whether in tort (including negligence), contract, or otherwise,
      unless required by applicable law (such as deliberate and grossly
      negligent acts) or agreed to in writing, shall any Contributor be
      liable to You for damages, including any direct, indirect, special,
      incidental, or consequential damages of any character arising as a
      result of this License or out of the use or inability to use the
      Work (including but not limited to damages for loss of goodwill,
      work stoppage, computer failure or malfunction, or any and all
      other commercial damages or losses), even if such Contributor
      has been advised of the possibility of such damages.

   9. Accepting Warranty or Additional Liability. While redistributing
      the Work or Derivative Works thereof, You may choose to offer,
      and charge a fee for, acceptance of support, warranty, indemnity,
      or other liability obligations and/or rights consistent with this
      License. However, in accepting such obligations, You may act only
      on Your own behalf and on Your sole responsibility, not on behalf
      of any other Contributor, and only if You agree to indemnify,
      defend, and hold each Contributor harmless for any liability
      incurred by, or claims asserted against, such Contributor by reason
      of your accepting any such warranty or additional liability.

   END OF TERMS AND CONDITIONS

   APPENDIX: How to apply the Apache License to your work.

      To apply the Apache License to your work, attach the following
      boilerplate notice, with the fields enclosed by brackets "[]"
      replaced with your own identifying information. (Don't include
      the brackets!)  The text should be enclosed in the appropriate
      comment syntax for the file format. We also recommend that a
      file or class name and description of purpose be included on the
      same "printed page" as the copyright notice for easier
      identification within third-party archives.

   Copyright [yyyy] [name of copyright owner]

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.

<!-- apache-2.0-text-end -->

---

## 5. Parsaetak Proprietary License v1.1

The complete, legally binding Parsaetak Proprietary License text
governing every component classified Proprietary in §2:

<!-- proprietary-text-begin -->

PARSAETAK PROPRIETARY LICENSE
=============================
Version 1.1 · Last updated: 2026-09-26

Copyright © 2024–2026 Parsaetak (https://github.com/Parsaetak). All rights
reserved.

SCOPE OF THIS LICENSE
---------------------
This license governs every component of SHEYTAN-Local-Agent classified as
"Proprietary" in §2 "Component classification" of LICENSE.md — the
SHEYTAN-specific proprietary mechanisms, the product-specific
implementation, the assets and designs, and all other explicitly
classified material. Components explicitly designated as open in
§2 of LICENSE.md are governed by the Apache License 2.0 (§4) instead.
The classification in LICENSE.md §2 is authoritative.

IMPORTANT — READ CAREFULLY. By installing, copying, or otherwise using
SHEYTAN-Local-Agent ("the Software") you agree to be bound by the terms of
this agreement. If you do not agree, do not install or use the Software.

1. GRANT OF LICENSE
   Subject to the terms below, Parsaetak ("the Licensor") grants you a
   personal, non-exclusive, non-transferable, revocable license to install
   and run the Software on computers you own or control, for any lawful
   personal or commercial purpose.

2. INTELLECTUAL PROPERTY
   The Proprietary components of the Software — their source code,
   binaries, documentation, icons, and designs — are and remain the
   exclusive property of Parsaetak and its contributors. No ownership
   rights are transferred to you by this license. This license attaches
   to the concrete expressions classified in §2 of LICENSE.md (actual
   code, documentation, assets, and implementations); it does not assert
   that abstract ideas, techniques, algorithms, or functionality
   described by those expressions are themselves protected intellectual
   property.

3. TRADEMARK
   "SHEYTAN", "SHEYTAN-Local-Agent", and the SHEYTAN logo are trademarks
   of Parsaetak. You may not use the marks (or confusingly similar marks)
   to name or promote products, forks, or derivative works without prior
   written permission. Referring to the Software by its name for the
   purpose of description, review, or interoperability is permitted.

4. DISTRIBUTION
   You may NOT redistribute, sublicense, sell, rent, lease, or host the
   Proprietary components of the Software (in whole or in part, original
   or modified) without prior written permission. Sharing an unmodified
   official release archive for non-commercial personal use is permitted,
   provided all files — including this license (as consolidated in
   LICENSE.md §5) and the classification in LICENSE.md §2 — remain
   intact.

5. DERIVATIVE WORKS
   You may modify the Software for your own personal use. You may NOT
   distribute modified versions, rebranded builds, or extractions of the
   Proprietary source code without prior written permission.

6. LOCAL-FIRST PRIVACY
   The Software is designed to run inference and tool execution locally on
   your machine, with all data stored inside the application folder. When
   the optional remote provider mode is enabled, prompts you submit are
   sent to the third-party endpoint you configure; the Licensor is not
   responsible for how third parties handle that data. Local logs (tool
   calls, LLM calls, crashes) stay on your disk until you choose to export
   them.

7. ACCEPTABLE USE
   You agree to use the Software only in compliance with applicable law.
   You are solely responsible for commands the agent executes on your
   behalf, including any file, shell, browser, git, or network operations.

8. DISCLAIMER OF WARRANTY
   THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS
   OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
   MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, AND NONINFRINGEMENT.
   IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
   CLAIM, DAMAGES, OR OTHER LIABILITY ARISING FROM, OUT OF, OR IN
   CONNECTION WITH THE SOFTWARE OR ITS USE.

9. TERMINATION
   This license terminates automatically if you breach any term. Upon
   termination you must stop using the Software and delete all copies.

10. CHANGES
   The Licensor may revise this agreement for future releases. Continued
   use after an update constitutes acceptance of the revised terms.

<!-- proprietary-text-end -->

---

## 6. Trademarks

SHEYTAN™ and the SHEYTAN logo are trademarks of Parsaetak. They identify
the Software and its distribution; no license in this repository grants
any right to use the SHEYTAN name, logo or branding beyond what trademark
law allows, and nothing here implies endorsement of derived works. The
trademark grant and restrictions are stated in §5 §3 (Trademark) of the
Parsaetak Proprietary License above.

---

## 7. Governance — changing a classification

Only the repository maintainer (Parsaetak) may reclassify a component:

1. Move the component between the tables in [§2](#2-component-classification)
   with the rationale.
2. For a move to Open: add "SPDX-License-Identifier: Apache-2.0" headers
   to every file in the component and confirm the component has no
   coupling to proprietary mechanisms (or the coupling is one-directional:
   proprietary → open).
3. For a move to Proprietary: remove the Apache headers, and confirm no
   downstream Apache-licensed extraction depends on it.
4. Describe the change in the commit.

---

## 8. Contact

Licensing questions, proprietary-use inquiries and exceptions:
**https://github.com/Parsaetak** (open an issue or use the contact
channel listed there).`
