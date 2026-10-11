// Package netcheck detects whether the machine has a working internet
// connection so the agent can degrade gracefully offline: web tools fail
// fast with a friendly message, remote LLM endpoints skip their retry
// ladder, and the LLM itself is told which tools are unavailable.
//
// v1.0.3 rewrite: the old single-strategy probe (a raw TCP dial to
// 1.1.1.1/8.8.8.8) reported OFFLINE forever on machines whose network only
// allows traffic through a proxy or filtered gateway — the UI pill never
// flipped back to ONLINE after reconnecting. The probe now tries THREE
// independent strategies and reports online as soon as ANY succeeds:
//
//  1. TCP dial to well-known anycast IPs (fast, no DNS)
//  2. HTTP HEAD to connectivity-check endpoints (Microsoft NCSI / Google
//     204 — flows through system/env proxies where configured)
//  3. DNS resolution of a public hostname through the OS resolver
//
// The result is cached with a short TTL so callers can ask IsOffline() on
// every tool invocation without measurable cost.
package netcheck

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ttl is how long a probe result is trusted. Kept short (15s) so a
// reconnect is picked up quickly; every consumer re-probes on its own
// cadence after the cache expires.
const ttl = 15 * time.Second

// probeTimeout bounds a single connectivity check.
const probeTimeout = 2500 * time.Millisecond

// targets are reliable anycast endpoints (Cloudflare + Google DNS). A TCP
// dial to either proves real outbound connectivity — no DNS involved, so a
// broken resolver does not produce false negatives.
var targets = []string{"1.1.1.1:443", "8.8.8.8:53", "1.0.0.1:443"}

// httpTargets are connectivity-check URLs (Windows NCSI + Google captive
// portal). They are fetched with HEAD through the default transport, which
// honors HTTP(S)_PROXY environment variables and the platform proxy where
// Go supports it — this is the strategy that fixes the stuck-OFFLINE bug on
// proxied machines.
var httpTargets = []string{
	"http://www.msftconnecttest.com/connecttest.txt",
	"https://www.gstatic.com/generate_204",
}

// dnsHosts are hostnames resolved through the OS resolver as the third
// strategy (works when raw IP dials are filtered but DNS + normal traffic
// flow).
var dnsHosts = []string{"github.com", "cloudflare.com"}

var (
	mu      sync.Mutex
	lastAt  time.Time
	online  bool
	known   bool // has any probe completed yet?
	probeFn func() bool
)

// SetProbe replaces the connectivity probe (tests only). The cached
// answer is dropped so the next Online() call uses the new probe.
func SetProbe(fn func() bool) {
	mu.Lock()
	defer mu.Unlock()
	probeFn = fn
	known = false
	lastAt = time.Time{}
}

// Force re-probes now and returns the fresh result.
func Force() bool {
	mu.Lock()
	probe := probeFn
	mu.Unlock()

	var ok bool
	if probe != nil {
		ok = probe()
	} else {
		ok = probeAll()
	}

	mu.Lock()
	online, known, lastAt = ok, true, time.Now()
	mu.Unlock()
	return ok
}

// IsOffline reports whether the machine is (very probably) offline.
// The first call probes; subsequent calls within the TTL reuse the cached
// answer.
func IsOffline() bool { return !Online() }

// Online reports whether the internet appears reachable (cached).
func Online() bool {
	mu.Lock()
	fresh := known && time.Since(lastAt) < ttl
	if fresh {
		ok := online
		mu.Unlock()
		return ok
	}
	probe := probeFn
	mu.Unlock()

	var ok bool
	if probe != nil {
		ok = probe()
	} else {
		ok = probeAll()
	}

	mu.Lock()
	online, known, lastAt = ok, true, time.Now()
	mu.Unlock()
	return ok
}

// Note returns a system-prompt environment note describing the current
// connectivity, or "" when online. The orchestrator prepends it so the LLM
// does not waste turns calling web tools that cannot work.
func Note() string {
	if Online() {
		return ""
	}
	return "ENVIRONMENT NOTE: the machine is currently OFFLINE — there is no internet connection. " +
		"The webSearch and browser tools cannot reach the web; do not call them for online information. " +
		"All local capabilities remain fully available: files, shell, codeExec, git, dataAnalysis (CSV/JSON + charts), and memory. " +
		"Answer from your own knowledge and local files, and tell the user when something would have required the web."
}

// State returns a human label for the UI: "online" | "offline".
func State() string {
	if Online() {
		return "online"
	}
	return "offline"
}

// probeAll runs every strategy in PARALLEL and reports online as soon as
// any one succeeds.
//
// v1.1.4: the sequential version took up to 7×2.5 s ≈ 17.5 s on a fully
// offline machine (every target timing out one after another) — a long
// stall on the llama-start gate for exactly the users who can least afford
// it. The parallel version resolves in ~one probeTimeout either way.
func probeAll() bool {
	type result struct{ ok bool }

	done := make(chan result, 3)

	probes := []func() bool{dialAny, httpAny, dnsAny}

	for _, probe := range probes {
		probe := probe
		go func() {
			done <- result{ok: probe()}
		}()
	}

	for range probes {
		if r := <-done; r.ok {
			return true
		}
	}

	return false
}

// dialAny tries each target once, in order, with a short timeout.
func dialAny() bool {
	for _, addr := range targets {
		conn, err := net.DialTimeout("tcp", addr, probeTimeout)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

// httpAny HEADs the connectivity-check endpoints. Any response (even a
// non-2xx) proves outbound HTTP works; only transport errors count as
// offline.
func httpAny() bool {
	client := &http.Client{
		Timeout: probeTimeout,
		// Never follow redirects into a captive portal rabbit hole — a
		// redirect is still proof of connectivity.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for _, url := range httpTargets {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
		if err != nil {
			cancel()
			continue
		}
		resp, err := client.Do(req)
		cancel()
		if err == nil {
			_ = resp.Body.Close()
			return true
		}
	}
	return false
}

// dnsAny resolves a public hostname through the OS resolver.
func dnsAny() bool {
	resolver := &net.Resolver{}
	for _, host := range dnsHosts {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		_, err := resolver.LookupHost(ctx, host)
		cancel()
		if err == nil {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// v1.1.7 — user-facing connection diagnostics.
// ---------------------------------------------------------------------------

// DiagState is the simple, user-facing connection verdict rendered by the
// Settings → Network view. States are ordered best → worst.
type DiagState string

const (
	DiagExcellent DiagState = "Excellent"
	DiagGood      DiagState = "Good"
	DiagUnstable  DiagState = "Unstable"
	DiagSlow      DiagState = "Slow"
	DiagOffline   DiagState = "Offline"
)

// DiagResult is one bounded connection diagnosis.
type DiagResult struct {
	// State is the plain-language verdict.
	State DiagState `json:"state"`

	// Reason is the first meaningful failure observed ("" when none).
	Reason string `json:"reason,omitempty"`

	// LatencyMs / Latency2Ms are the two HTTPS samples actually measured.
	LatencyMs  float64 `json:"latencyMs,omitempty"`
	Latency2Ms float64 `json:"latency2Ms,omitempty"`

	// DNSOK / HTTPSOK / EndpointOK report which checks passed.
	DNSOK      bool `json:"dnsOk"`
	HTTPSOK    bool `json:"httpsOk"`
	EndpointOK bool `json:"endpointOk"`

	// CheckedAt is when the diagnosis ran.
	CheckedAt time.Time `json:"checkedAt"`

	// TotalMs bounds the whole diagnosis for transparency.
	TotalMs int64 `json:"totalMs"`
}

// diagTimeouts bounds each individual check inside a diagnosis.
const diagTimeout = 3 * time.Second

// diagHTTPS are HTTPS endpoints used for the latency samples (real
// reachability + timing, through the system proxy when configured).
var diagHTTPS = []string{
	"https://www.gstatic.com/generate_204",
	"https://www.msftconnecttest.com/connecttest.txt",
}

// timedHTTPS performs ONE HTTPS request and reports wall latency. Any HTTP
// response proves reachability (a captive-portal redirect is still
// connectivity); only transport errors fail.
func timedHTTPS(url string) (float64, error) {
	client := &http.Client{
		Timeout: diagTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), diagTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()

	return float64(time.Since(start).Microseconds()) / 1000.0, nil
}

// classifyDiag maps the measured facts onto the plain-language verdict.
// Pure function so the thresholds are unit-testable.
func classifyDiag(dns, https bool, l1, l2 float64) (DiagState, string) {
	if !https {
		if !dns {
			return DiagOffline, "DNS resolution and HTTPS both failed — no usable internet connection."
		}
		return DiagOffline, "DNS resolves but HTTPS requests fail — a firewall, captive portal, or proxy is likely blocking traffic."
	}

	// HTTPS works: rate by latency and stability.
	lat := l1
	if l2 > 0 && (lat == 0 || l2 < lat) {
		lat = l2
	}

	// Unstable: the warm (second) HTTPS sample is dramatically slower than
	// the cold one. The first sample includes TCP+TLS handshake, so
	// "first slower than second" is NORMAL cold-start behaviour and must
	// not be flagged; degradation on the warm path is real instability.
	if l1 > 0 && l2 > 0 && l2 > 4*l1 && l2-l1 > 400 {
		return DiagUnstable, "HTTPS latency degraded between samples — the connection is intermittent."
	}

	switch {
	case !dns:
		// HTTPS reachable but the OS resolver failed: degraded but usable.
		return DiagUnstable, "HTTPS works but DNS resolution failed — some tools may be unable to connect."
	case lat > 1500:
		return DiagSlow, "HTTPS responses are slow (over 1.5 s) — research and web tools will feel sluggish."
	case lat > 600:
		return DiagGood, ""
	default:
		return DiagExcellent, ""
	}
}

// Diagnose runs ONE bounded connection check (no retries, no background
// work) and returns the plain-language verdict. Total cost is bounded by
// roughly two diagTimeout HTTPS samples plus one DNS lookup — a fully
// offline machine gets a complete, honest diagnosis in well under 10 s.
//
// SHEYTAN is local-first: this is informational only, never a gate.
func Diagnose() DiagResult {
	start := time.Now()
	res := DiagResult{CheckedAt: start.UTC()}

	// DNS through the OS resolver.
	res.DNSOK = dnsAny()
	if !res.DNSOK {
		res.Reason = "DNS resolution failed — hostnames cannot be resolved."
	}

	// Two HTTPS latency samples (stability = comparing them).
	for i := 0; i < 2; i++ {
		lat, err := timedHTTPS(diagHTTPS[0])
		if err != nil {
			if res.Reason == "" {
				res.Reason = "HTTPS request failed: " + firstErrLine(err)
			}
			continue
		}

		res.HTTPSOK = true
		if i == 0 {
			res.LatencyMs = lat
		} else {
			res.Latency2Ms = lat
		}
	}

	// Endpoint reachability: a second, independent HTTPS target proves the
	// first success was not a fluke of one provider.
	if res.HTTPSOK {
		if _, err := timedHTTPS(diagHTTPS[1]); err == nil {
			res.EndpointOK = true
		}
	}

	// Fall back to the existing multi-strategy probe so proxied machines
	// (where the raw HTTPS sample may fail but NCSI/HTTP succeeds) still
	// get an honest "connected" verdict instead of a false Offline.
	if !res.HTTPSOK && Online() {
		res.HTTPSOK = true
		res.EndpointOK = true
		if res.Reason != "" {
			res.Reason = ""
		}
	}

	res.State, res.Reason = classifyDiag(res.DNSOK, res.HTTPSOK, res.LatencyMs, res.Latency2Ms)
	res.TotalMs = time.Since(start).Milliseconds()

	return res
}

// firstErrLine keeps the reason string short — one line, no stack noise.
func firstErrLine(err error) string {
	s := err.Error()
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
