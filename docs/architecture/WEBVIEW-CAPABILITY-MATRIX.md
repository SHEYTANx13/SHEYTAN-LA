# WebView Capability Matrix — v1.11.1 (contract level; adapters staged)

> Per-platform capability and security posture of the optional native
> WebView layer. Every row states what was actually verified — the
> default is NOT RUN. The enforced policy is
> [`internal/webapphost`](../../internal/webapphost/webapphost.go); the
> binding comparison is [`WEBVIEW-BINDING-DECISION.md`](WEBVIEW-BINDING-DECISION.md).

## 1. Platform adapter matrix

| Platform | Engine | Adapter state in v1.11.1 | Build | Runtime | Reason / gate |
|---|---|---|---|---|---|
| Linux | WebKitGTK 4.1 | STAGED_WITH_GATE | NOT RUN | NOT RUN | WebKitGTK 4.1 development headers absent in the authoring environment (no privileged install); gate: headers + build + the §14.2 WebView acceptance list |
| Windows | WebView2 | STAGED_WITH_GATE | NOT RUN | NOT RUN | no Windows host exercised; gate includes the glaze undocumented-export risk being closed with a verified fallback (see binding decision) |
| macOS | WKWebView | STAGED_WITH_GATE | NOT RUN | NOT RUN | no macOS toolchain/runner available |
| Android | WebView (native SDK) | STAGED_WITH_GATE (contract only) | NOT RUN | NOT RUN | no Android SDK builds exist; desktop wrappers never prove mobile |
| iOS | WKWebView (native SDK) | STAGED_WITH_GATE (contract only) | NOT RUN | NOT RUN | no iOS SDK builds exist |

No adapter is claimed shipped. The contract package
(`internal/webapphost`) is FOUNDATION: typed policy, fail-closed tests
(8/8 green locally), zero placeholder engines.

## 2. Feature-support contract (measured per engine at adapter time — never promised in advance)

"Use web features" means: the capabilities exposed by the INSTALLED native
engine. The contract explicitly does NOT promise: Chrome extension APIs,
DRM/proprietary codecs, browser sync/password stores, or identical
Chrome/Edge/Safari behavior. Unsupported features get a visible reason and
an offer to open in the system browser.

At adapter time each row below must be measured on the target engine and
recorded (NOT RUN until then):

HTML5 parsing/layout · modern JavaScript/ECMAScript · modern CSS ·
canvas/2D · WebGL · WebGPU (where the OS engine implements it) ·
audio/video codecs · WebRTC · cookies/storage · downloads · clipboard ·
file selection · camera/microphone permissions · authentication redirects
· custom URL schemes · SPA/history routing.

## 3. Security posture (verified at policy level — the only level shipped)

| Control | State | Evidence |
|---|---|---|
| Privileged-scheme rejection (`javascript:`/`data:`/`file:`/scheme-less) | ENFORCED (policy) | `TestNavigationRejectsPrivilegedSchemes` |
| Uncreated `app://` origin rejection | ENFORCED (policy) | `TestNavigationAllowsTrustedAppOrigin` |
| Private-network rejection by default (loopback/RFC1918/link-local/localhost) | ENFORCED (policy) | `TestNavigationRejectsPrivateNetworkByDefault` |
| Explicit, scoped private-network grant that never re-enables privileged schemes | ENFORCED (policy) | `TestPrivateNetworkGrantIsExplicitAndScoped` |
| Remote HTTPS allowed as untrusted, bridge-less content | ENFORCED (policy) | `TestNavigationAllowsRemoteHTTPSAsUntrustedNoBridge` |
| External schemes handed to the system browser | ENFORCED (policy) | `TestNavigationExternalSchemesGoToSystemBrowser` |
| Bridge fail-closed (origin / allowlist / call id / payload bound / nil grant) | ENFORCED (policy) | `TestBridgeValidateFailClosed` |
| Download handoff to the existing safe downloader, approval required | ENFORCED (policy) | `TestDownloadPolicyFailsClosed` |
| TLS certificate validation stays on; no bypass API exists | BY CONSTRUCTION | no bypass flag exists in the contract |
| Per-engine runtime behavior of all the above | NOT RUN | no adapter built — see §1 gates |

---

*Proprietary material under the Proprietary License (see
[`../../LICENSE.md`](../../LICENSE.md)).*
