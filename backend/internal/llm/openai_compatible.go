// The OpenAI-compatible adapter translates internal messages and tools to HTTP
// payloads and converts full responses or SSE deltas back to the common types.
// All operations use the descriptor endpoint and credentials supplied by services.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Emulisy/SupaAgent/internal/pkg/errcode"
)

// openaiCompatibleProvider implements chat, embeddings, and reranking using
// OpenAI-style JSON payloads. A zero HTTP client timeout leaves deadline control
// to the request context so long-lived chat streams are not cut off globally.
type openaiCompatibleProvider struct {
	httpClient *http.Client
}

// client uses an injected HTTP client when supplied, otherwise the shared
// http.DefaultClient. Context deadlines govern requests when that client has
// no overall timeout.
func (p *openaiCompatibleProvider) client() *http.Client {
	if p.httpClient == nil {
		return http.DefaultClient
	}
	return p.httpClient
}

type openaiTool struct {
	Type     string       `json:"type"`
	Function openaiFnSpec `json:"function"`
}

type openaiFnSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// openaiRequest contains the wire names accepted by compatible chat endpoints.
// Optional sampling fields are omitted when zero, allowing provider defaults.
type openaiRequest struct {
	Model               string         `json:"model"`
	Messages            []openaiMsg    `json:"messages"`
	Tools               []openaiTool   `json:"tools,omitempty"`
	ToolChoice          string         `json:"tool_choice,omitempty"`
	Temperature         float32        `json:"temperature,omitempty"`
	TopP                float32        `json:"top_p,omitempty"`
	MaxTokens           int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens int            `json:"max_completion_tokens,omitempty"`
	Stream              bool           `json:"stream"`
	StreamOptions       *streamOptions `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// openaiMsg models role-specific provider fields. Content remains any so the
// wire representation can distinguish text from other JSON content values.
type openaiMsg struct {
	Role             string       `json:"role"`
	Content          any          `json:"content"`
	ReasoningContent string       `json:"reasoning_content,omitempty"`
	ToolCalls        []openaiCall `json:"tool_calls,omitempty"`
	ToolCallID       string       `json:"tool_call_id,omitempty"`
	Name             string       `json:"name,omitempty"`
}

// openaiCall is used for both complete calls and streamed fragments. Index is
// used to group fragments; ID, name, and arguments may arrive across many frames.
type openaiCall struct {
	Index        int             `json:"index,omitempty"`
	ID           string          `json:"id,omitempty"`
	Type         string          `json:"type,omitempty"`
	ExtraContent json.RawMessage `json:"extra_content,omitempty"`
	Function     struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type openaiResp struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content          string       `json:"content"`
			ReasoningContent string       `json:"reasoning_content"`
			ToolCalls        []openaiCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openaiUsage `json:"usage"`
	Error *openaiErr   `json:"error"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type openaiErr struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

// toOpenAIMsgs preserves role order, tool-call IDs, names, reasoning text, and
// opaque tool metadata. Provider-specific payload filtering happens later in
// newOpenAIRequest without mutating the original conversation messages.
func toOpenAIMsgs(msgs []Message) []openaiMsg {
	out := make([]openaiMsg, 0, len(msgs))
	for _, m := range msgs {
		om := openaiMsg{Role: m.Role, Name: m.Name, ReasoningContent: m.ReasoningContent}
		switch {
		case m.Role == RoleAssistant && len(m.ToolCalls) > 0:
			om.Content = m.Content
			for _, tc := range m.ToolCalls {
				om.ToolCalls = append(om.ToolCalls, openaiCall{
					ID: tc.ID, Type: "function", ExtraContent: tc.ExtraContent,
					Function: struct {
						Name      string `json:"name,omitempty"`
						Arguments string `json:"arguments,omitempty"`
					}{Name: tc.Name, Arguments: tc.ArgsJSON},
				})
			}
		case m.Role == RoleTool:
			om.Content = m.Content
			om.ToolCallID = m.ToolCallID
		default:
			om.Content = m.Content
			om.ReasoningContent = m.ReasoningContent
		}
		out = append(out, om)
	}
	return out
}

// toOpenAITools wraps each definition as a function tool without rewriting its
// parameter schema. An empty definition list omits tools from the request.
func toOpenAITools(defs []ToolDef) []openaiTool {
	if len(defs) == 0 {
		return nil
	}
	out := make([]openaiTool, 0, len(defs))
	for _, d := range defs {
		out = append(out, openaiTool{
			Type: "function",
			Function: openaiFnSpec{
				Name: d.Name, Description: d.Description, Parameters: d.Parameters,
			},
		})
	}
	return out
}

// Chat performs one HTTP request, consumes its full JSON body, and converts the
// first choice. Non-200 responses are classified before decoding successful
// response fields; retry and fallback are handled by the gateway.
func (p *openaiCompatibleProvider) Chat(ctx context.Context, desc *ModelDescriptor, req *ChatRequest) (*ChatResponse, error) {
	body := newOpenAIRequest(desc, req, false)

	httpReq, err := p.newHTTPRequest(ctx, desc, body)
	if err != nil {
		return nil, err
	}

	httpResp, err := p.client().Do(httpReq)
	if err != nil {
		return nil, classifyHTTPError(err)
	}
	defer httpResp.Body.Close()

	// Consume the complete body before returning so the HTTP connection can be
	// reused and structured API errors remain available to the caller.
	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, parseAPIError(httpResp.StatusCode, raw)
	}

	var resp openaiResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if resp.Error != nil {
		return nil, parseAPIError(http.StatusBadGateway, raw)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty choices")
	}

	ch := resp.Choices[0]
	calls := make([]ToolCall, 0, len(ch.Message.ToolCalls))
	for _, c := range ch.Message.ToolCalls {
		calls = append(calls, ToolCall{ID: c.ID, Name: c.Function.Name, ArgsJSON: c.Function.Arguments, ExtraContent: c.ExtraContent})
	}

	usage := Usage{}
	if resp.Usage != nil {
		usage = Usage{PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens}
	}
	return &ChatResponse{
		Content: ch.Message.Content, ReasoningContent: ch.Message.ReasoningContent,
		ToolCalls: calls, FinishReason: ch.FinishReason, Model: desc.ModelName, Usage: usage,
	}, nil
}

// newHTTPRequest appends /chat/completions to the configured API prefix and binds
// the HTTP lifetime to ctx. Authorization is added only when a key is configured,
// which also permits local unauthenticated compatible endpoints.
func (p *openaiCompatibleProvider) newHTTPRequest(ctx context.Context, desc *ModelDescriptor, body any) (*http.Request, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	url := strings.TrimRight(desc.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if desc.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+desc.APIKey)
	}
	return req, nil
}

// ChatStream opens an SSE response and starts a dedicated body reader after a
// 200 response. Opening errors are returned synchronously and can be retried;
// subsequent failures are emitted as StreamEvent.Error values on the channel.
func (p *openaiCompatibleProvider) ChatStream(ctx context.Context, desc *ModelDescriptor, req *ChatRequest) (<-chan StreamEvent, error) {
	body := newOpenAIRequest(desc, req, true)

	httpReq, err := p.newHTTPRequest(ctx, desc, body)
	if err != nil {
		return nil, err
	}

	httpResp, err := p.client().Do(httpReq)
	if err != nil {
		return nil, classifyHTTPError(err)
	}
	if httpResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, parseAPIError(httpResp.StatusCode, raw)
	}

	// The provider buffer absorbs small consumer delays. This layer waits for
	// capacity or cancellation, unlike the runner best-effort output channel.
	events := make(chan StreamEvent, 64)
	go p.consumeStream(ctx, httpResp.Body, events)
	return events, nil
}

// newOpenAIRequest builds a common chat payload and applies provider-specific
// compatibility rules. OpenAI uses max_completion_tokens and selected reasoning
// models omit ordinary sampling defaults; reasoning_content is retained only for
// DeepSeek. Streaming additionally requests terminal usage information.
func newOpenAIRequest(desc *ModelDescriptor, req *ChatRequest, stream bool) openaiRequest {
	body := openaiRequest{
		Model: desc.ModelName, Messages: toOpenAIMsgs(req.Messages),
		Tools: toOpenAITools(req.Tools), Temperature: desc.Temperature,
		TopP: desc.TopP, MaxTokens: desc.MaxTokens,
		Stream: stream,
	}
	// Let the model decide whether to answer directly or request one of the
	// advertised functions.
	if len(body.Tools) > 0 {
		body.ToolChoice = "auto"
	}
	if stream {
		body.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	if strings.EqualFold(desc.ProviderCode, "openai") {
		body.MaxCompletionTokens = body.MaxTokens
		body.MaxTokens = 0
		// Reasoning models use their own sampling defaults. Sending the platform's
		// ordinary temperature/top_p defaults can cause an upstream 400.
		name := strings.ToLower(desc.ModelName)
		if strings.HasPrefix(name, "gpt-5") || strings.HasPrefix(name, "gpt-6") ||
			strings.HasPrefix(name, "o1") || strings.HasPrefix(name, "o3") || strings.HasPrefix(name, "o4") {
			body.Temperature, body.TopP = 0, 0
		}
	}
	for i := range body.Messages {
		if !strings.EqualFold(desc.ProviderCode, "deepseek") {
			body.Messages[i].ReasoningContent = ""
		}
	}
	return body
}

// consumeStream forwards text/reasoning immediately and assembles tool calls by
// provider index. Successful termination emits aggregated calls, finish reason,
// and usage. Malformed JSON, explicit upstream errors, read failures, canceled
// contexts, and EOF without a completion marker produce an error event instead.
// The body and output channel are closed on every exit path.
func (p *openaiCompatibleProvider) consumeStream(ctx context.Context, body io.ReadCloser, events chan<- StreamEvent) {
	defer close(events)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	// Allow larger tool-argument frames while bounding the maximum scanned line
	// to 512 KiB.
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	type callAgg struct {
		id, name, args strings.Builder
		extraContent   json.RawMessage
	}
	// Provider indexes identify calls across chunks; the string builders retain
	// ID/name fragments and concatenate partial JSON argument strings.
	aggs := map[int]*callAgg{}
	finish := ""
	var usage *Usage
	model := ""
	completed := false

	// A canceled request must not leave the body reader blocked on a consumer.
	emit := func(ev StreamEvent) {
		select {
		case events <- ev:
		case <-ctx.Done():
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		// Ignore SSE comments, blank separators, and event-name lines; only data
		// payloads contribute to the model response.
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		// The provider sentinel ends consumption even if the TCP response stays open.
		if data == "[DONE]" {
			completed = true
			break
		}
		if data == "" {
			continue
		}

		var frame struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content          string       `json:"content"`
					ReasoningContent string       `json:"reasoning_content"`
					ToolCalls        []openaiCall `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *openaiUsage `json:"usage"`
			Error *openaiErr   `json:"error"`
		}
		// Malformed payloads terminate this round explicitly; do not silently turn a
		// partial response into a successful model completion.
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			emit(StreamEvent{Error: fmt.Errorf("decode stream: %w", err)})
			return
		}
		if frame.Error != nil {
			emit(StreamEvent{Error: parseAPIError(http.StatusBadGateway, []byte(data))})
			return
		}
		if frame.Model != "" {
			model = frame.Model
		}

		for _, ch := range frame.Choices {
			d := ch.Delta
			if d.ReasoningContent != "" {
				emit(StreamEvent{ReasoningContent: d.ReasoningContent, Model: model})
			}
			if d.Content != "" {
				emit(StreamEvent{Content: d.Content, Model: model})
			}
			// Accumulate calls until the model has finished the round so executors never
			// receive incomplete argument JSON.
			for _, tc := range d.ToolCalls {
				agg := aggs[tc.Index]
				if agg == nil {
					agg = &callAgg{}
					aggs[tc.Index] = agg
				}
				if tc.ID != "" {
					agg.id.WriteString(tc.ID)
				}
				if len(tc.ExtraContent) > 0 {
					agg.extraContent = tc.ExtraContent
				}
				if tc.Function.Name != "" {
					agg.name.WriteString(tc.Function.Name)
				}
				agg.args.WriteString(tc.Function.Arguments)
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				finish = *ch.FinishReason
			}
		}
		// Usage may arrive after a finish reason in a final frame with no choices.
		if frame.Usage != nil {
			usage = &Usage{PromptTokens: frame.Usage.PromptTokens, CompletionTokens: frame.Usage.CompletionTokens}
		}
	}
	if err := scanner.Err(); err != nil {
		emit(StreamEvent{Error: fmt.Errorf("read stream: %w", err)})
		return
	}
	if err := ctx.Err(); err != nil {
		emit(StreamEvent{Error: err})
		return
	}
	if !completed && finish == "" {
		emit(StreamEvent{Error: io.ErrUnexpectedEOF})
		return
	}

	// Sort the actual provider indexes, including sparse indexes, before rebuilding
	// calls. Opaque metadata is retained alongside the completed argument string.
	calls := make([]ToolCall, 0, len(aggs))
	indices := make([]int, 0, len(aggs))
	for index := range aggs {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		a := aggs[index]
		calls = append(calls, ToolCall{ID: a.id.String(), Name: a.name.String(), ArgsJSON: a.args.String(), ExtraContent: a.extraContent})
	}
	if len(calls) > 0 {
		finish = "tool_calls"
	}
	emit(StreamEvent{ToolCalls: calls, FinishReason: finish, Usage: usage, Model: model})
}

// Embed sends one text batch to /embeddings and places each returned vector at
// its provider index. Callers rely on the returned slice matching input order.
func (p *openaiCompatibleProvider) Embed(ctx context.Context, desc *ModelDescriptor, texts []string) ([][]float32, error) {
	payload, _ := json.Marshal(map[string]any{"model": desc.ModelName, "input": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(desc.BaseURL, "/")+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if desc.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+desc.APIKey)
	}

	resp, err := p.client().Do(req)
	if err != nil {
		return nil, classifyHTTPError(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, parseAPIError(resp.StatusCode, raw)
	}

	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode embeddings: %w", err)
	}
	vectors := make([][]float32, len(texts))
	for _, d := range out.Data {
		vectors[d.Index] = d.Embedding
	}
	return vectors, nil
}

// rerankResp retains candidate indexes so callers can reorder their own records
// without duplicating document text in the provider response.
type rerankResp struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float32 `json:"relevance_score"`
	} `json:"results"`
}

// Rerank sends the query and candidate texts to /rerank and converts the provider
// relevance_score field to the gateway result type.
func (p *openaiCompatibleProvider) Rerank(ctx context.Context, desc *ModelDescriptor, query string, docs []string, topN int) ([]RerankResult, error) {
	payload, _ := json.Marshal(map[string]any{
		"model": desc.ModelName, "query": query, "documents": docs, "top_n": topN,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(desc.BaseURL, "/")+"/rerank", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if desc.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+desc.APIKey)
	}

	resp, err := p.client().Do(req)
	if err != nil {
		return nil, classifyHTTPError(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, parseAPIError(resp.StatusCode, raw)
	}
	var out rerankResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode rerank: %w", err)
	}
	results := make([]RerankResult, 0, len(out.Results))
	for _, r := range out.Results {
		results = append(results, RerankResult{Index: r.Index, Score: r.RelevanceScore})
	}
	return results, nil
}

// retryableError marks failures eligible for gateway retry while preserving the
// underlying error for inspection.
type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// IsRetryable searches wrapped errors for the adapter transient marker.
func IsRetryable(err error) bool {
	var re *retryableError
	return asRetryable(err, &re)
}

// asRetryable follows single-error Unwrap chains without depending on concrete
// transport error types outside this adapter.
func asRetryable(err error, target **retryableError) bool {
	for err != nil {
		if re, ok := err.(*retryableError); ok {
			*target = re
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// classifyHTTPError treats transport failures as transient. RetryDo still checks
// context cancellation before issuing another attempt.
func classifyHTTPError(err error) error {
	return &retryableError{err: errcode.ErrModelUnavail.WithMsg("Could not connect to the model provider. Check its address and your network connection.").WithCause(err)}
}

// parseAPIError keeps upstream details in the diagnostic cause and exposes a
// useful, bounded message. Quota exhaustion is permanent until billing changes,
// even when the provider returns 429; only rate limits and 5xx are retried.
func parseAPIError(status int, body []byte) error {
	var eResp openaiErr
	var envelope struct {
		Error *openaiErr `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil {
		eResp = *envelope.Error
	} else {
		_ = json.Unmarshal(body, &eResp)
	}
	cause := fmt.Errorf("upstream %d: %s", status, body)
	detail := strings.ToLower(fmt.Sprint(eResp.Code) + " " + eResp.Type + " " + eResp.Message)
	if status == http.StatusPaymentRequired || strings.Contains(detail, "insufficient_quota") ||
		strings.Contains(detail, "credit_balance_exhausted") || strings.Contains(detail, "insufficient balance") ||
		strings.Contains(detail, "no credits remaining") || strings.Contains(detail, "insufficient credits") {
		return errcode.ErrModelQuota.WithCause(cause)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return errcode.ErrModelAuth.WithCause(cause)
	}
	if status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusUnprocessableEntity {
		return errcode.ErrModelRequest.WithCause(cause)
	}
	err := errcode.ErrModelUnavail.WithCause(cause)
	if status == http.StatusTooManyRequests {
		err = errcode.ErrRateLimited.WithMsg("Model provider rate limit reached. Try again later or choose another model.").WithCause(cause)
	}
	if status == http.StatusTooManyRequests || status >= 500 {
		return &retryableError{err: err}
	}
	return err
}

const defaultFirstTokenTimeout = 30 * time.Second
