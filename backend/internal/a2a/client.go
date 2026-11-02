// Outbound A2A calls discover an endpoint, send one text message, and collect a task.
// Remote failures are wrapped in application error codes for service/API callers.
package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chengpeng-cp/nexus-agent/internal/pkg/errcode"
)

// Client owns a reusable HTTP transport with a two-minute request timeout.
// Caller contexts can impose a shorter deadline on discovery and task requests.
type Client struct {
	httpClient *http.Client
}

// NewClient creates the outbound transport; it does not contact a remote Agent.
func NewClient() *Client {
	return &Client{httpClient: &http.Client{Timeout: 120 * time.Second}}
}

// FetchCard requests the standard discovery path and decodes at most 1 MiB.
// Non-200 responses and malformed documents are returned as card-fetch failures.
func (c *Client) FetchCard(ctx context.Context, baseURL string) (*AgentCard, error) {
	url := strings.TrimRight(baseURL, "/") + "/.well-known/agent.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errcode.ErrA2ACardFetch.WithCause(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errcode.ErrA2ACardFetch.WithMsg("card http %d", resp.StatusCode)
	}
	var card AgentCard
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&card); err != nil {
		return nil, errcode.ErrA2ACardFetch.WithCause(err)
	}
	return &card, nil
}

// SendMessage prefers the endpoint advertised by a successfully fetched card.
// Discovery failure falls back to the supplied base URL. The JSON-RPC response
// is bounded to 8 MiB and must contain a nonnil task without a remote RPC error.
func (c *Client) SendMessage(ctx context.Context, baseURL string, text string) (*Task, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/"
	if card, err := c.FetchCard(ctx, baseURL); err == nil && card.URL != "" {
		endpoint = card.URL
	}

	params := SendMessageParams{
		Message: Message{
			Role:  RoleUser,
			Parts: []Part{{Type: "text", Text: text}},
		},
	}
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": time.Now().UnixNano(),
		"method": MethodMessageSend, "params": params,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errcode.ErrA2ATaskFail.WithCause(err)
	}
	defer resp.Body.Close()

	var rpcResp struct {
		Result *Task `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rpcResp); err != nil {
		return nil, errcode.ErrA2ATaskFail.WithCause(err)
	}
	if rpcResp.Error != nil {
		return nil, errcode.ErrA2ATaskFail.WithMsg("remote: %s", rpcResp.Error.Message)
	}
	if rpcResp.Result == nil {
		return nil, errcode.ErrA2ATaskFail.WithMsg("empty task result")
	}
	return rpcResp.Result, nil
}

// ExtractText concatenates text artifact parts without adding separators.
// If no text exists, it returns a diagnostic containing the task ID and state.
func ExtractText(t *Task) string {
	var b strings.Builder
	for _, art := range t.Artifacts {
		for _, p := range art.Parts {
			if p.Type == "text" {
				b.WriteString(p.Text)
			}
		}
	}
	if b.Len() == 0 {
		return fmt.Sprintf("(task %s in state %s, no artifacts)", t.ID, t.State)
	}
	return b.String()
}
