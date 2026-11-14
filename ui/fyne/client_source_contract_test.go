//go:build !headless

package fyneui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// --- v1.11.1 Windows IPC repair: source contract pins ---
//
// The production CoreClient must dial through the ONE canonical
// platform-aware localipc.DialContext for BOTH transports (HTTP and
// WebSocket). The defect this pins shut: an unconditional
// net.Dialer.DialContext(ctx, "unix", endpoint) in NewCoreClient, which
// broke every tab — including Logs — on Windows, where the endpoint is a
// named pipe. These tests fail the build if anyone reintroduces a
// hard-coded Unix dial, a TCP fallback, or a second dial implementation.

const clientSource = "client.go"

func readClientSource(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(clientSource)
	if err != nil {
		t.Fatalf("read %s: %v (test must run from the package directory)", clientSource, err)
	}
	return string(data)
}

// TestCoreClientDialsThroughCanonicalLocalIPC pins the dial boundary.
func TestCoreClientDialsThroughCanonicalLocalIPC(t *testing.T) {
	src := readClientSource(t)

	if !strings.Contains(src, "localipc.DialContext(ctx, endpoint)") {
		t.Fatal("NewCoreClient must dial through localipc.DialContext(ctx, endpoint) — the ONE canonical platform-aware transport dial (v1.11.1 Windows IPC repair)")
	}

	if strings.Contains(src, "net.Dialer") {
		t.Fatal("client.go must not construct its own net.Dialer — the platform dial belongs to internal/localipc (the v1.11.1 defect was a private Unix-only dial here)")
	}

	// A hard-coded network dial call (any Dial* call with a "unix"/"tcp"
	// network literal) is the defect shape; prose in comments stays free.
	dialCall := regexp.MustCompile(`\.Dial\w*\(\s*[^,)]+,\s*"(unix|tcp)"`)
	if loc := dialCall.FindStringIndex(src); loc != nil {
		t.Fatalf("client.go hard-codes a %q network dial — the transport is platform-aware by contract: %s",
			dialCall.FindStringSubmatch(src)[1], strings.TrimSpace(src[loc[0]:loc[0]+50]))
	}

	// Both transports must share the exact same dial closure.
	if !strings.Contains(src, "DialContext: dial") {
		t.Fatal("http.Transport.DialContext must use the shared dial closure")
	}
	if !strings.Contains(src, "NetDialContext:   dial") && !strings.Contains(src, "NetDialContext: dial") {
		t.Fatal("websocket.Dialer.NetDialContext must use the SAME shared dial closure as the HTTP transport")
	}
}

// TestNoSecondDialImplementationInUI pins the one-dial rule package-wide:
// no non-test source in ui/fyne outside client.go may construct a
// transport dial of its own (the shared CoreClient dial is the only one;
// client.go's exact allowed shape is pinned by its own test above).
func TestNoSecondDialImplementationInUI(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob package sources: %v", err)
	}

	dialerPattern := regexp.MustCompile(`net\.Dialer|net\.Dial\(`)

	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == clientSource {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if loc := dialerPattern.FindStringIndex(string(data)); loc != nil {
			src := string(data)
			t.Fatalf("%s builds a transport dial of its own (%q) — UI packages must route through CoreClient's shared localipc dial",
				f, strings.TrimSpace(src[loc[0]:loc[0]+40]))
		}
	}
}

// TestSingleWebSocketDialerConstruction: exactly one websocket.Dialer is
// constructed in the package (in NewCoreClient), so HTTP and WebSocket
// traffic cannot drift onto different transports.
func TestSingleWebSocketDialerConstruction(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob package sources: %v", err)
	}

	constructions := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		constructions += strings.Count(string(data), "websocket.Dialer{")
	}
	if constructions != 1 {
		t.Fatalf("websocket.Dialer constructions in non-test UI sources = %d, want exactly 1 (NewCoreClient's shared dialer)", constructions)
	}
}
