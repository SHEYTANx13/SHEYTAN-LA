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
	// LicenseName is the human name of the license.
	LicenseName = "Mixed: Apache-2.0 (designated components) + Proprietary"

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
const LicenseFooter = "SHEYTAN™ is a trademark of Parsaetak.\nLicensed under the conservative mixed model — Apache-2.0 for designated open components, Proprietary for SHEYTAN-specific material. The complete licensing picture lives in LICENSE.md (the sole licensing artifact).\n" + LicensorURL

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

// LicenseText is the COMPLETE consolidated license document. It is the
// single source of truth written to LICENSE.md by scripts/gen-license.go
// and must stay in exact sync with the preferred short-form LICENSE.md.
const LicenseText = `# License

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

Subject to the terms of this license, **SHEYTAN** and **Parsaetak** (collectively “Licensor”) grant you a limited, non-exclusive, non-transferable, non-sublicensable, revocable license to install and use the Software on devices you own or control, solely for your personal or internal business purposes.

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
`
