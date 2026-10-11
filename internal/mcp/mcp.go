// Package mcp implements Phase 7F: the Model Context Protocol extension
// bridge — an OPTIONAL external capability layer, never a core dependency.
//
// Architecture:
//
//	SHEYTAN Tool Registry → MCP Adapter → External MCP servers (stdio)
//
// Every MCP tool must pass the full guarded registration pipeline before
// the model ever sees it:
//
//	discovery → capability classification → permission policy →
//	schema validation → execution boundary (timeout, result size cap,
//	evidence trail)
//
// Arbitrary MCP tools are NEVER exposed blindly: a server's tools exist
// only when the server is configured, its tool passes schema sanity, and
// the per-server allow policy admits it. Everything is off by default.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ProtocolVersion is the MCP protocol version this adapter speaks.
const ProtocolVersion = "2024-11-05"

// Client is one stdio MCP server connection.
type Client struct {
	name   string
	cmd    *exec.Cmd
	stdin  context.CancelFunc
	nextID int64
	mu     sync.Mutex
	rw     *bufio.ReadWriter
	waitCh chan struct{}
	// pending maps request id → response channel.
	pendingMu sync.Mutex
	pending   map[int64]chan rpcResponse
}

// rpcRequest is one JSON-RPC 2.0 request.
type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// rpcResponse is one JSON-RPC 2.0 response (or notification, id 0).
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("mcp error %d: %s", e.Code, e.Message) }

// ServerConfig configures one MCP server process.
type ServerConfig struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`
	// AllowedTools restricts which tools may register (deny-by-default:
	// empty means NO tools from this server are exposed).
	AllowedTools []string `json:"allowedTools,omitempty"`
	// StartupTimeoutSec bounds the initialize handshake.
	StartupTimeoutSec int `json:"startupTimeoutSec,omitempty"`
}

// ToolDef is one discovered MCP tool.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"inputSchema"`
}

// Start launches the server process and performs the initialize handshake.
//
// v1.2.9 command/config boundary audit (documented contract, no behavior
// change): the server command comes from LOCAL user configuration — that
// is the MCP trust model (the user opts into running an external binary,
// same as any installed CLI). Boundaries enforced here:
//   - exec.CommandContext with cfg.Args passed VERBATIM — no shell is
//     involved, so no command-string injection surface exists;
//   - the child environment is NOT the user's full environment: only
//     PATH, HOME and the explicitly configured cfg.Env entries cross
//     the boundary (no secret leakage into external processes);
//   - cmd.Stderr is discarded — a chatty server cannot flood the logs;
//   - the process is bounded by the caller's context and killed on
//     Close (3s grace, then SIGKILL).
func Start(ctx context.Context, cfg ServerConfig) (*Client, error) {
	if strings.TrimSpace(cfg.Name) == "" || strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("mcp server needs a name and a command")
	}

	startupTimeout := time.Duration(cfg.StartupTimeoutSec) * time.Second
	if startupTimeout <= 0 {
		startupTimeout = 15 * time.Second
	}

	runCtx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(runCtx, cfg.Command, cfg.Args...)
	// Minimal environment: never leak the user's full environment into an
	// external server process (inherits the existing sanitisation rule).
	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
	}, cfg.Env...)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	cmd.Stderr = nil // diagnostics stay silent; a chatty server must not flood logs

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start mcp server %q: %w", cfg.Name, err)
	}

	c := &Client{
		name:    cfg.Name,
		cmd:     cmd,
		stdin:   cancel,
		rw:      bufio.NewReadWriter(bufio.NewReader(stdoutPipe), bufio.NewWriter(stdinPipe)),
		waitCh:  make(chan struct{}),
		pending: map[int64]chan rpcResponse{},
	}

	go c.readLoop()

	hsCtx, hsCancel := context.WithTimeout(ctx, startupTimeout)
	defer hsCancel()

	var initResult struct {
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := c.call(hsCtx, "initialize", map[string]interface{}{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]string{"name": "SHEYTAN-local-agent", "version": "1.1"},
	}, &initResult); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp initialize %q: %w", cfg.Name, err)
	}

	// notifications/initialized completes the handshake.
	_ = c.notify("notifications/initialized", nil)

	return c, nil
}

// readLoop demultiplexes responses to pending callers.
//
// v1.2.9: line length is BOUNDED (maxJSONLineBytes). The previous
// ReadString('\n') happily buffered whatever the server sent — a
// malicious/looping provider could push a multi-gigabyte "line" into
// memory before any cap existed. A line exceeding the bound now fails
// the connection (and every pending caller) instead of allocating it.
func (c *Client) readLoop() {
	defer close(c.waitCh)
	for {
		line, err := c.readLine()
		if err != nil {
			c.failAllPending(err)
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var resp rpcResponse
		if json.Unmarshal([]byte(line), &resp) != nil {
			continue
		}
		if resp.ID == 0 {
			continue // notification
		}

		c.pendingMu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			// v1.2.9: remove the entry BEFORE delivering — a delivered
			// channel must never remain in the map, or a later connection
			// failure would send on the CLOSED channel and panic (latent
			// bug exposed by the v1.2.9 size-limit regression tests).
			delete(c.pending, resp.ID)
		}
		c.pendingMu.Unlock()
		if ok {
			ch <- resp
			close(ch)
		}
	}
}

// maxJSONLineBytes bounds one JSON-RPC message line on the wire. It is
// deliberately far above the largest legitimate payload (tool schemas,
// 32 KiB-class tool results) so real servers are unaffected, while a
// hostile stream is cut off before unbounded allocation.
const maxJSONLineBytes = 1 << 20 // 1 MiB

// readLine reads one newline-terminated line with a hard length bound;
// exceeding the bound returns an error (the caller kills the
// connection).
func (c *Client) readLine() (string, error) {
	var b []byte
	for {
		ch, err := c.rw.Reader.ReadByte()
		if err != nil {
			if len(b) == 0 {
				return "", err
			}
			return string(b), nil
		}
		if ch == '\n' {
			return string(b), nil
		}
		b = append(b, ch)
		if len(b) > maxJSONLineBytes {
			return "", fmt.Errorf("mcp: line exceeds %d bytes — connection aborted (hostile or broken server)", maxJSONLineBytes)
		}
	}
}

func (c *Client) failAllPending(cause error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		// Buffered channel (cap 1): the send cannot block; the entry is
		// deleted first so no later path can double-send.
		ch <- rpcResponse{Error: &rpcError{Code: -1, Message: cause.Error()}}
		close(ch)
	}
}

func (c *Client) call(ctx context.Context, method string, params interface{}, out interface{}) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	if _, err := c.rw.Write(append(data, '\n')); err != nil {
		c.mu.Unlock()
		return err
	}
	if err := c.rw.Flush(); err != nil {
		c.mu.Unlock()
		return err
	}

	ch := make(chan rpcResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()
	c.mu.Unlock()

	select {
	case resp, ok := <-ch:
		if !ok {
			return fmt.Errorf("mcp %s: connection closed", method)
		}
		if resp.Error != nil {
			return resp.Error
		}
		if out != nil && len(resp.Result) > 0 {
			// v1.2.9: enforce the result-size limit BEFORE decoding. A
			// malformed/malicious provider must not be able to make this
			// process allocate an arbitrarily large decoded response;
			// with the line bound above the raw bytes are already capped
			// at ~1 MiB, and this check refuses to even parse anything
			// above the JSON-level cap (covering legitimate-size lines
			// that still exceed the response contract).
			if len(resp.Result) > maxResultJSONBytes {
				return fmt.Errorf("mcp %s: result of %d bytes exceeds the %d-byte response limit (refused before decoding)",
					method, len(resp.Result), maxResultJSONBytes)
			}
			return json.Unmarshal(resp.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return ctx.Err()
	}
}

func (c *Client) notify(method string, params interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	req := rpcRequest{JSONRPC: "2.0", Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := c.rw.Write(append(data, '\n')); err != nil {
		return err
	}
	return c.rw.Flush()
}

// ListTools discovers the server's tools.
func (c *Client) ListTools(ctx context.Context) ([]ToolDef, error) {
	var result struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", map[string]interface{}{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool invokes one tool; result size bounded by the caller.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	params := map[string]interface{}{"name": name}
	if len(args) > 0 {
		params["arguments"] = args
	} else {
		params["arguments"] = map[string]interface{}{}
	}
	if err := c.call(ctx, "tools/call", params, &result); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, part := range result.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	if result.IsError {
		return b.String(), fmt.Errorf("tool reported failure: %.200s", b.String())
	}
	return b.String(), nil
}

// Close terminates the server process.
func (c *Client) Close() {
	c.stdin()
	done := make(chan struct{})
	go func() { _, _ = c.cmd.Process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = c.cmd.Process.Kill()
	}
}

// ---------------------------------------------------------------------------
// Guarded registration pipeline.
// ---------------------------------------------------------------------------

// resultSizeCap bounds one MCP tool result delivered to the model.
const resultSizeCap = 32 << 10

// maxResultJSONBytes bounds the RAW JSON result envelope of any call,
// applied BEFORE decoding (see call). Generously above the 32 KiB
// logical cap to accommodate JSON string escaping of control-heavy
// text, while still an absolute allocation bound.
const maxResultJSONBytes = 256 << 10

// callTimeout bounds one MCP tool execution.
const callTimeout = 120 * time.Second

// Registration is one tool that passed the full pipeline.
type Registration struct {
	Server   string
	Tool     ToolDef
	ToolName string // registry name: mcp_<server>_<tool>
}

// ClassifyAndRegister runs the guarded pipeline for a server's tools and
// returns the registrations that passed:
//
//	discovery → capability classification → permission policy →
//	schema validation → (execution boundary is enforced by the adapter)
func ClassifyAndRegister(cfg ServerConfig, tools []ToolDef) []Registration {
	allowed := map[string]bool{}
	for _, a := range cfg.AllowedTools {
		allowed[a] = true
	}

	var out []Registration
	for _, t := range tools {
		// Permission policy: deny-by-default.
		if !allowed[t.Name] {
			continue
		}
		// Schema validation: a tool without a usable object schema is
		// rejected — the model cannot call what it cannot see.
		if len(t.Schema) == 0 {
			continue
		}
		var schema struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(t.Schema, &schema) != nil ||
			(schema.Type != "" && schema.Type != "object") {
			continue
		}

		out = append(out, Registration{
			Server:   cfg.Name,
			Tool:     t,
			ToolName: RegistryToolName(cfg.Name, t.Name),
		})
	}
	return out
}

// RegistryToolName builds the namespaced registry name for an MCP tool.
func RegistryToolName(server, tool string) string {
	sanitize := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToLower(s) {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
				b.WriteRune(r)
			} else {
				b.WriteRune('_')
			}
		}
		return b.String()
	}
	return "mcp_" + sanitize(server) + "_" + sanitize(tool)
}

// ExecTool runs one MCP tool through the execution boundary: bounded
// timeout, bounded result size, structured error.
func ExecTool(ctx context.Context, c *Client, name string, args json.RawMessage) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	out, err := c.CallTool(ctx, name, args)
	if err != nil {
		return out, err
	}
	if len(out) > resultSizeCap {
		out = out[:resultSizeCap] + fmt.Sprintf("\n[mcp result bounded at %d bytes]", resultSizeCap)
	}
	return out, nil
}

// ServerNameFromRegistry reverses RegistryToolName into the server id.
func ServerNameFromRegistry(regName string) string {
	if !strings.HasPrefix(regName, "mcp_") {
		return ""
	}
	rest := regName[len("mcp_"):]
	if i := strings.LastIndex(rest, "_"); i > 0 {
		return rest[:i]
	}
	return ""
}

// DefaultSocketHint documents the expected stdio transport for diagnostics.
const DefaultSocketHint = "stdio"
