// Package mcp discovers and invokes tools over an HTTP JSON-RPC endpoint.
// A client retains initialization and Mcp-Session-Id state across calls. Responses
// may be JSON or an SSE data payload; this is a request/response adapter, not a
// long-lived subscription or a process-based stdio transport.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Emulisy/SupaAgent/internal/pkg/errcode"
)

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// Client retains mutable handshake, request-ID, and session-header state.
// Use one request at a time per instance; these fields are not synchronized for
// concurrent calls. Configured headers are forwarded to the remote server.
type Client struct {
	baseURL     string
	headers     map[string]string
	httpClient  *http.Client
	nextID      int64
	sessionID   string
	initialized bool
}

// NewClient configures the endpoint, optional headers, and a 60-second HTTP timeout.
// Initialization is deferred until discovery or invocation is requested.
func NewClient(baseURL string, headers map[string]string) *Client {
	return &Client{
		baseURL:    baseURL,
		headers:    headers,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// ToolDef preserves the remote name, description, and input schema used to create
// model-visible tool definitions in the application catalog.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Initialize sends the protocol handshake once for this client instance.
// The initialized flag is set only after a successful response; subsequent operations
// reuse the handshake and any session header returned by the server.
func (c *Client) Initialize(ctx context.Context) error {
	if c.initialized {
		return nil
	}
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if _, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "nexus-agent", "version": "1.0.0"},
	}, &result); err != nil {
		return errcode.ErrMCPConnFail.WithCause(err)
	}
	c.initialized = true
	return nil
}

// ListTools initializes as needed and returns the tools/list result.
// Transport and protocol failures are wrapped as MCP connection errors.
func (c *Client) ListTools(ctx context.Context) ([]ToolDef, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	var result struct {
		Tools []ToolDef `json:"tools"`
	}
	if _, err := c.call(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, errcode.ErrMCPConnFail.WithCause(err)
	}
	return result.Tools, nil
}

// CallTool invokes one named tool after initialization. It concatenates text
// content parts in order, ignores non-text content, and turns remote isError
// results into an application tool-call failure.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	if err := c.Initialize(ctx); err != nil {
		return "", err
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if _, err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &result); err != nil {
		return "", errcode.ErrMCPToolFail.WithCause(err)
	}
	if result.IsError {
		return "", errcode.ErrMCPToolFail.WithMsg("mcp tool %s failed", name)
	}
	out := ""
	for _, part := range result.Content {
		if part.Type == "text" {
			out += part.Text
		}
	}
	return out, nil
}

// call sends one numbered JSON-RPC request with configured headers and the current
// session ID. It retains a returned session header, reads at most 4 MiB, checks
// HTTP/RPC errors, and optionally decodes the result into the supplied target.
func (c *Client) call(ctx context.Context, method string, params any, out any) (json.RawMessage, error) {
	c.nextID++
	reqBody, err := json.Marshal(jsonRPCRequest{
		JSONRPC: "2.0", ID: c.nextID, Method: method, Params: params,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp transport: %w", err)
	}
	defer resp.Body.Close()

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.sessionID = sid
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(body), 256))
	}

	payload := extractJSONPayload(body)
	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(payload, &rpcResp); err != nil {
		return nil, fmt.Errorf("decode jsonrpc: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("jsonrpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	if out != nil && len(rpcResp.Result) > 0 {
		if err := json.Unmarshal(rpcResp.Result, out); err != nil {
			return nil, fmt.Errorf("decode result: %w", err)
		}
	}
	return rpcResp.Result, nil
}

// extractJSONPayload accepts a raw JSON body or the first SSE data line.
// It does not aggregate multiple frames or implement multiline SSE data fields.
func extractJSONPayload(body []byte) []byte {
	s := string(body)

	for _, line := range splitLines(s) {
		if after, ok := cutPrefix(line, "data:"); ok {
			return []byte(after)
		}
	}
	return body
}

// splitLines separates LF-delimited frames and removes a trailing CR per line
// so the payload extractor accepts both LF and CRLF responses.
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, trimCR(s[start:i]))
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, trimCR(s[start:]))
	}
	return out
}

func trimCR(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\r' {
		return s[:len(s)-1]
	}
	return s
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		return trimSpace(s[len(prefix):]), true
	}
	return "", false
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}

// truncate bounds diagnostic response text by bytes and adds an ellipsis when cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Ping treats a successful initialized tools/list request as a health check.
// It exercises discovery rather than a dedicated transport ping method.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.ListTools(ctx)
	return err
}
