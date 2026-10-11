package mcp

// Result-size limits must be
// enforced BEFORE or DURING response decoding, and the read loop must
// bound the wire line length, so a malformed/malicious MCP provider
// cannot allocate an arbitrarily large response before the logical
// caps apply.
//
// The fake server uses the standard re-exec pattern: the test binary
// re-executes itself as a scripted stdio MCP server, which keeps the
// suite portable (Linux + Windows CI) with zero external dependencies.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const fakeServerEnv = "SHEYTAN_MCP_FAKE_SERVER"

type fakeScript struct {
	// mode selects the hostile behavior.
	mode string
	// args: [mode]
}

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeServerEnv); mode != "" {
		runFakeServer(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runFakeServer speaks just enough MCP-over-stdio for the tests: the
// initialize handshake, then scripted misbehavior for tools/call.
func runFakeServer(mode string) {
	// Read lines from stdin; answer initialize; answer tools/call per mode.
	scanner := func() func() string {
		lines := make(chan string, 16)
		go func() {
			buf := make([]byte, 1, 64<<10)
			cur := make([]byte, 0, 64<<10)
			for {
				n, err := os.Stdin.Read(buf)
				if n > 0 {
					for _, b := range buf[:n] {
						if b == '\n' {
							lines <- string(cur)
							cur = cur[:0]
							continue
						}
						cur = append(cur, b)
					}
				}
				if err != nil {
					return
				}
			}
		}()
		return func() string {
			select {
			case l := <-lines:
				return l
			case <-time.After(5 * time.Second):
				return ""
			}
		}
	}

	next := scanner()
	respond := func(id int64, result string) {
		fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":%d,"result":%s}`+"\n", id, result)
	}

	for {
		line := next()
		if line == "" {
			return
		}
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(line), &req) != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			respond(req.ID, `{"serverInfo":{"name":"fake","version":"0"}}`)
		case "tools/call":
			switch mode {
			case "huge-line":
				// One multi-megabyte "line" — must be cut off by the
				// read loop bound, never buffered.
				fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"`, req.ID)
				chunk := strings.Repeat("A", 64<<10)
				for i := 0; i < 64; i++ { // 4 MiB total, no newline
					fmt.Fprint(os.Stdout, chunk)
				}
				fmt.Fprint(os.Stdout, `"}]}}`+"\n")
			case "oversize-result":
				// A well-formed line whose result exceeds the JSON-level
				// cap — refused BEFORE decoding.
				text := strings.Repeat("B", 300<<10)
				b, _ := json.Marshal(map[string]any{
					"content": []map[string]string{{"type": "text", "text": text}},
				})
				respond(req.ID, string(b))
			case "normal":
				respond(req.ID, `{"content":[{"type":"text","text":"ok"}]}`)
			}
		}
	}
}

func startFake(t *testing.T, mode string) *Client {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Skipf("cannot re-exec test binary: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)

	c, err := Start(ctx, ServerConfig{
		Name:    "fake",
		Command: exe,
		Env:     []string{fakeServerEnv + "=" + mode},
	})
	if err != nil {
		t.Fatalf("Start fake server (%s): %v", mode, err)
	}
	t.Cleanup(c.Close)
	return c
}

// TestHugeLineAbortsConnection pins the read-loop bound: a multi-
// megabyte unterminated line must ABORT the connection (error), never
// be buffered into memory.
func TestHugeLineAbortsConnection(t *testing.T) {
	c := startFake(t, "huge-line")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := c.CallTool(ctx, "anything", nil)
	if err == nil {
		t.Fatalf("a 4 MiB line was accepted — the read loop bound did not fire")
	}
	if !strings.Contains(err.Error(), "exceeds") && !strings.Contains(err.Error(), "connection") {
		t.Fatalf("unexpected error for the oversized line: %v", err)
	}
}

// TestOversizeResultRefusedBeforeDecoding pins the pre-decode cap: a
// well-formed 300 KiB result envelope must be REFUSED with the explicit
// size error, not decoded into memory.
func TestOversizeResultRefusedBeforeDecoding(t *testing.T) {
	c := startFake(t, "oversize-result")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := c.CallTool(ctx, "anything", nil)
	if err == nil {
		t.Fatalf("a 300 KiB result was decoded — the pre-decode limit did not fire")
	}
	if !strings.Contains(err.Error(), "exceeds") || !strings.Contains(err.Error(), "refused before decoding") {
		t.Fatalf("the refusal must name the size limit and the pre-decode stage: %v", err)
	}
}

// TestNormalResultStillFlows guards the inverse: legitimate tool calls
// keep working through the bounded path.
func TestNormalResultStillFlows(t *testing.T) {
	c := startFake(t, "normal")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := c.CallTool(ctx, "anything", nil)
	if err != nil {
		t.Fatalf("normal call failed: %v", err)
	}
	if out != "ok" {
		t.Fatalf("normal result = %q, want %q", out, "ok")
	}
}
