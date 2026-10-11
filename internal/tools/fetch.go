// Package tools contains the built-in HTTP fetch tool.
//
// fetch is intentionally lightweight compared with the browser tool:
// one bounded GET, readable text or raw bytes, strict timeouts, redirect
// protection, and SSRF protection against loopback/private/link-local and
// other non-public destinations.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FetchTool reads public HTTP(S) URLs.
type FetchTool struct {
	client *http.Client
}

// NewFetchTool builds a fetch tool with its own bounded HTTP client.
//
// v1.1.4 DNS-rebinding defense: the transport's DialContext resolves the
// hostname itself, validates every candidate address as globally routable,
// and dials a VERIFIED IP. validateFetchURL already checks DNS before the
// request, but the default dialer resolved the name a second time — a
// rebinding attacker could answer the second lookup with a private address.
// Pinning the dial to the validated IP closes that window; TLS still
// verifies the original hostname (SNI/cert), so https is not weakened.
func NewFetchTool() *FetchTool {
	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		ForceAttemptHTTP2: true,
		DialContext:       pinnedPublicDialContext(10 * time.Second),
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(
			req *http.Request,
			via []*http.Request,
		) error {
			if err := validateFetchURL(req.URL); err != nil {
				return err
			}

			if len(via) >= fetchMaxRedirects {
				return fmt.Errorf(
					"fetch: too many redirects",
				)
			}

			return nil
		},
	}

	return &FetchTool{
		client: client,
	}
}

// Name implements the agent tool interface.
func (t *FetchTool) Name() string {
	return "fetch"
}

// Description implements the agent tool interface.
func (t *FetchTool) Description() string {
	return "Fetch a public HTTP(S) URL and return readable text or raw bytes. " +
		`mode:"text" (default) | "raw". maxBytes caps the body (default 512 KB, max 4 MB). ` +
		"Private, loopback, link-local, multicast, and otherwise non-public destinations are blocked, including redirects. " +
		"Pair with webSearch (find) → fetch (read) → files (save)."
}

// Parameters implements the agent tool interface.
func (t *FetchTool) Parameters() any {
	return struct {
		URL      string `json:"url"`
		Mode     string `json:"mode,omitempty"`
		MaxBytes int    `json:"maxBytes,omitempty"`
	}{}
}

const (
	fetchDefaultCap   = 512 << 10 // 512 KB
	fetchMaxCap       = 4 << 20   // 4 MB
	fetchTimeout      = 30 * time.Second
	fetchMaxRedirects = 5
)

// Run implements the agent tool interface.
func (t *FetchTool) Run(
	ctx context.Context,
	args json.RawMessage,
) (string, error) {
	var p struct {
		URL      string `json:"url"`
		Mode     string `json:"mode"`
		MaxBytes int    `json:"maxBytes"`
	}

	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}

	p.URL = strings.TrimSpace(p.URL)

	if p.URL == "" {
		return "", fmt.Errorf("url is required")
	}

	parsedURL, err := url.ParseRequestURI(p.URL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	if err := validateFetchURL(parsedURL); err != nil {
		return "", err
	}

	bodyCap := fetchDefaultCap

	if p.MaxBytes > 0 {
		bodyCap = p.MaxBytes

		if bodyCap > fetchMaxCap {
			bodyCap = fetchMaxCap
		}
	}

	if ctx == nil {
		ctx = context.Background()
	}

	cctx, cancel := context.WithTimeout(
		ctx,
		fetchTimeout,
	)
	defer cancel()

	req, err := http.NewRequestWithContext(
		cctx,
		http.MethodGet,
		parsedURL.String(),
		nil,
	)
	if err != nil {
		return "", err
	}

	req.Header.Set(
		"User-Agent",
		"Mozilla/5.0 (compatible; SHEYTAN-Local-Agent/1.10; +local)",
	)
	req.Header.Set(
		"Accept",
		"text/html,application/xhtml+xml,application/json,text/plain,*/*",
	)

	client := t.client

	if client == nil {
		client = NewFetchTool().client
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}

	defer resp.Body.Close()

	// Redirect validation is performed by CheckRedirect. Validate the final
	// destination again as a defense-in-depth check.
	if err := validateFetchURL(resp.Request.URL); err != nil {
		return "", err
	}

	// Stream with cap: a huge response never enters memory.
	limited := io.LimitReader(
		resp.Body,
		int64(bodyCap)+1,
	)

	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf(
			"read body: %w",
			err,
		)
	}

	truncated := len(body) > bodyCap

	if truncated {
		body = body[:bodyCap]
	}

	var b strings.Builder

	fmt.Fprintf(
		&b,
		"status: %s | content-type: %s | %d bytes%s\n",
		resp.Status,
		resp.Header.Get("Content-Type"),
		len(body),
		truncMark(truncated),
	)

	ctype := strings.ToLower(
		resp.Header.Get("Content-Type"),
	)

	isHTML :=
		strings.Contains(ctype, "html") ||
			(!strings.Contains(ctype, "json") &&
				!strings.Contains(ctype, "text/plain") &&
				strings.HasPrefix(
					strings.TrimSpace(string(body)),
					"<",
				))

	if strings.EqualFold(
		strings.TrimSpace(p.Mode),
		"raw",
	) {
		b.Write(body)
		return b.String(), nil
	}

	if isHTML {
		text := htmlPageToText(
			string(body),
		)

		if len(text) > bodyCap {
			text = text[:bodyCap]
		}

		b.WriteString(text)
		return b.String(), nil
	}

	b.Write(body)

	return b.String(), nil
}

// validateFetchURL accepts only HTTP(S) URLs whose host resolves exclusively
// to public IP addresses.
//
// This blocks:
//   - localhost / localhost aliases
//   - IPv4/IPv6 loopback
//   - RFC1918/private networks
//   - link-local addresses
//   - multicast addresses
//   - unspecified addresses
//   - IPv4-mapped private/loopback IPv6 addresses
//   - unsupported URL schemes
//
// DNS resolution is performed before the request and again by the transport
// connection path, reducing the DNS-rebinding window.
func validateFetchURL(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("fetch: URL is empty")
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf(
			"fetch: only http(s) URLs are supported",
		)
	}

	if u.User != nil {
		return fmt.Errorf(
			"fetch: URL credentials are not allowed",
		)
	}

	host := strings.TrimSpace(u.Hostname())

	if host == "" {
		return fmt.Errorf(
			"fetch: URL host is required",
		)
	}

	host = strings.TrimSuffix(
		strings.ToLower(host),
		".",
	)

	if host == "localhost" ||
		strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf(
			"fetch: local hostnames are blocked",
		)
	}

	// Test-only relaxation: the release stress suite points fetch at a
	// loopback httptest server. Scheme, credential, and hostname rules
	// above still apply — only the public-IP requirement is lifted.
	if fetchPrivateAllowed() {
		return nil
	}

	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return fmt.Errorf(
				"fetch: destination %s is not a public IP address",
				host,
			)
		}

		return nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf(
			"fetch: DNS resolution failed for %q: %w",
			host,
			err,
		)
	}

	if len(ips) == 0 {
		return fmt.Errorf(
			"fetch: host %q resolved to no addresses",
			host,
		)
	}

	for _, ip := range ips {
		if !isPublicIP(ip) {
			return fmt.Errorf(
				"fetch: host %q resolves to a non-public address %s",
				host,
				ip.String(),
			)
		}
	}

	return nil
}

// isPublicIP returns true only for globally routable unicast addresses.
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	if v4 := ip.To4(); v4 != nil {
		return !isPrivateIPv4(v4)
	}

	return !isPrivateIPv6(ip)
}

func isPrivateIPv4(ip net.IP) bool {
	ip = ip.To4()

	if ip == nil {
		return true
	}

	switch {
	case ip[0] == 0:
		return true

	// RFC1918.
	case ip[0] == 10:
		return true

	case ip[0] == 172 &&
		ip[1] >= 16 &&
		ip[1] <= 31:
		return true

	case ip[0] == 192 &&
		ip[1] == 168:
		return true

	// Loopback.
	case ip[0] == 127:
		return true

	// Link-local / APIPA.
	case ip[0] == 169 &&
		ip[1] == 254:
		return true

	// Carrier-grade NAT.
	case ip[0] == 100 &&
		ip[1] >= 64 &&
		ip[1] <= 127:
		return true

	// Documentation/test networks.
	case ip[0] == 192 &&
		ip[1] == 0 &&
		ip[2] == 2:
		return true

	case ip[0] == 198 &&
		(ip[1] == 18 || ip[1] == 19):
		return true

	case ip[0] == 198 &&
		ip[1] == 51 &&
		ip[2] == 100:
		return true

	case ip[0] == 203 &&
		ip[1] == 0 &&
		ip[2] == 113:
		return true

	// Multicast.
	case ip[0] >= 224:
		return true
	}

	return false
}

func isPrivateIPv6(ip net.IP) bool {
	if len(ip) != net.IPv6len {
		ip = ip.To16()
	}

	if ip == nil {
		return true
	}

	if ip.IsUnspecified() ||
		ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsLinkLocalMulticast() {
		return true
	}

	// Unique-local fc00::/7.
	if (ip[0] & 0xfe) == 0xfc {
		return true
	}

	// Multicast ff00::/8.
	if ip[0] == 0xff {
		return true
	}

	// Documentation prefix 2001:db8::/32.
	if ip[0] == 0x20 &&
		ip[1] == 0x01 &&
		ip[2] == 0x0d &&
		ip[3] == 0xb8 {
		return true
	}

	// IPv4-mapped addresses inherit IPv4 restrictions.
	if v4 := ip.To4(); v4 != nil {
		return isPrivateIPv4(v4)
	}

	return false
}

func truncMark(t bool) string {
	if t {
		return " (truncated at cap)"
	}

	return ""
}

// htmlPageToText strips the chrome from an HTML document and returns
// readable text: script/style/head noise removed, tags dropped, entities
// decoded, whitespace squeezed while preserving paragraph structure.
func htmlPageToText(doc string) string {
	s := doc

	for _, section := range []string{
		"script",
		"style",
		"noscript",
		"svg",
		"head",
	} {
		s = removeTagBlock(s, section)
	}

	s = stripBetween(
		s,
		"<!--",
		"-->",
	)

	for _, tag := range []string{
		"</p>",
		"<br>",
		"<br/>",
		"<br />",
		"</div>",
		"</li>",
		"</tr>",
		"</h1>",
		"</h2>",
		"</h3>",
		"</h4>",
		"</h5>",
		"</h6>",
		"</pre>",
		"</table>",
	} {
		s = replaceIgnoreCase(
			s,
			tag,
			"\n",
		)
	}

	var b strings.Builder
	inTag := false

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			inTag = true

		case '>':
			if inTag {
				inTag = false
				b.WriteByte(' ')
			}

		default:
			if !inTag {
				b.WriteByte(s[i])
			}
		}
	}

	text := html.UnescapeString(b.String())

	lines := strings.Split(
		text,
		"\n",
	)

	out := make([]string, 0, len(lines))
	blank := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			blank++

			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}

		out = append(
			out,
			line,
		)
	}

	return strings.TrimSpace(
		strings.Join(out, "\n"),
	)
}

func replaceIgnoreCase(
	s,
	old,
	neu string,
) string {
	lower := strings.ToLower(s)
	oldLower := strings.ToLower(old)

	var b strings.Builder
	i := 0

	for {
		idx := strings.Index(
			lower[i:],
			oldLower,
		)

		if idx < 0 {
			b.WriteString(s[i:])
			return b.String()
		}

		idx += i

		b.WriteString(s[i:idx])
		b.WriteString(neu)

		i = idx + len(old)
	}
}

func removeTagBlock(
	s,
	tag string,
) string {
	lower := strings.ToLower(s)

	var b strings.Builder
	i := 0

	for {
		start := strings.Index(
			lower[i:],
			"<"+tag,
		)

		if start < 0 {
			b.WriteString(s[i:])
			return b.String()
		}

		start += i

		b.WriteString(s[i:start])

		end := strings.Index(
			lower[start:],
			"</"+tag+">",
		)

		if end < 0 {
			return b.String()
		}

		i = start + end + len(tag) + 3
	}
}

func stripBetween(
	s,
	open,
	close string,
) string {
	for {
		i := strings.Index(s, open)

		if i < 0 {
			return s
		}

		j := strings.Index(
			s[i:],
			close,
		)

		if j < 0 {
			return s[:i]
		}

		s = s[:i] +
			s[i+j+len(close):]
	}
}

// pinnedPublicDialContext returns a DialContext that resolves the host,
// rejects non-public addresses, and dials the verified IP (the TLS layer
// keeps using the original hostname for SNI + certificate verification).
func pinnedPublicDialContext(timeout time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if fetchPrivateAllowed() {
			return dialer.DialContext(ctx, network, addr)
		}

		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("fetch: dial %q: %w", addr, err)
		}

		if ip := net.ParseIP(host); ip != nil {
			if !isPublicIP(ip) {
				return nil, fmt.Errorf("fetch: destination %s is not a public IP address", host)
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		}

		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("fetch: DNS resolution failed for %q: %w", host, err)
		}

		for _, ip := range ips {
			if isPublicIP(ip.IP) {
				return dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			}
		}

		return nil, fmt.Errorf("fetch: host %q resolves to no public address", host)
	}
}
