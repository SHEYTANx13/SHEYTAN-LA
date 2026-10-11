package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
)

// upgrader promotes HTTP requests to WebSocket connections.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return allowedOrigin(r.Header.Get("Origin"), r.Host)
	},
}

// allowedOrigin accepts same-origin requests and explicitly permits the
// loopback origins commonly used by the local development UI.
//
// No wildcard origin is accepted.
//
// v1.9.4 (P0): the desktop shell's own page is also accepted. Packaged
// desktop builds serve the page through the webview's resource-request
// interception — origin http://wails.localhost (Windows WebView2) or
// wails://wails.localhost (Linux/WebKit custom scheme) — while the
// activity WebSocket rides the v1.9.4 loopback transport
// (http://127.0.0.1:PORT, see loopback.go). That page origin against a
// LOOPBACK request host is the local desktop application itself; it is
// never accepted against a non-loopback host, and no other origin is
// broadened.
func allowedOrigin(origin, host string) bool {
	origin = strings.TrimSpace(origin)

	// Browsers normally omit Origin for same-origin WebSocket requests.
	if origin == "" {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}

	originHost := strings.ToLower(u.Host)
	requestHost := strings.ToLower(strings.TrimSpace(host))

	// Same-origin request.
	if requestHost != "" && originHost == requestHost {
		return true
	}

	// v1.9.4: the Wails desktop shell page (the exact production origin —
	// host wails.localhost with no port, http or the custom wails scheme)
	// reaching the loopback activity transport.
	if isLoopbackHost(requestHost) && originHost == "wails.localhost" {
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "wails":
			return true
		}
	}



	// Local development frontends.
	switch originHost {
	case "localhost:3000",
		"127.0.0.1:3000",
		"localhost:4173",
		"127.0.0.1:4173",
		"localhost:5173",
		"127.0.0.1:5173":
		return isLoopbackHost(requestHost)
	default:
		return false
	}
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(host)

	return strings.HasPrefix(host, "localhost:") ||
		strings.HasPrefix(host, "127.0.0.1:") ||
		host == "localhost" ||
		host == "127.0.0.1"
}

// handleWSOrigin answers the activity-transport discovery probe (v1.9.4
// contract, v1.10.1 revision): the loopback transport was the Wails
// desktop page's channel and is retired with Wails itself. The native Qt
// host consumes the local IPC channel instead and never calls this
// endpoint; every remaining serving mode (browser/headless/dev) streams
// the activity WebSocket over its OWN origin, so the authoritative answer
// is always the empty origin.
func (s *Server) handleWSOrigin(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"origin": "",
		"token":  "",
	})
}
