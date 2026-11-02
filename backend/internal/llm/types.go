// Package llm defines conversation, tool, and streaming types and dispatches model
// requests through provider adapters. ModelDescriptor is the boundary between
// service-side configuration/credential resolution and provider HTTP requests.
package llm

import "encoding/json"

// Message is the internal OpenAI-compatible conversation representation. Assistant
// messages can request tools; subsequent tool messages refer to those requests by
// ToolCallID. Reasoning text is carried separately from the visible answer.
type Message struct {
	// Role is one of system, user, assistant, or tool.
	Role    string `json:"role"`
	Content string `json:"content"`
	// ReasoningContent carries a provider reasoning field when one is supplied.
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID links a tool observation to its originating assistant call.
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
}

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// ToolCall is one model-requested function invocation. ArgsJSON remains encoded
// because providers stream it as string fragments and executors decode it later.
type ToolCall struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ArgsJSON string `json:"args"`
	// ExtraContent preserves opaque provider call metadata, such as Gemini thought
	// signatures, so subsequent tool observations can replay the original call.
	ExtraContent json.RawMessage `json:"extra_content,omitempty"`
}

// ToolDef describes a function advertised to the model. Parameters is its JSON
// Schema; advertising that schema does not itself validate an invocation.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ChatRequest supplies conversation messages and available tools. Sampling fields
// are part of the request type, but the OpenAI-compatible adapter currently reads
// Temperature, TopP, and MaxTokens from ModelDescriptor.
type ChatRequest struct {
	Messages []Message
	// Tools is omitted from the provider payload when no functions are available.
	Tools       []ToolDef
	Temperature float32
	TopP        float32
	MaxTokens   int
	// Stream records the caller mode; the selected Provider method controls the
	// actual HTTP stream flag.
	Stream bool
}

// Usage contains provider-reported token counts. A zero value means no usage was
// returned; it is not a local tokenizer measurement.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// ChatResponse is a complete non-streaming result from the first provider choice.
// ToolCalls and FinishReason determine whether a runner should execute tools or
// accept the content as its final answer.
type ChatResponse struct {
	Content          string
	ReasoningContent string
	ToolCalls        []ToolCall
	FinishReason     string
	Model            string
	Usage            Usage
}

// StreamEvent carries incremental text/reasoning, final tool/usage metadata, or
// an Error for an opened stream that failed. Consumers must treat Error as a
// failed model round rather than accepting already-delivered text as completion.
type StreamEvent struct {
	Content          string
	ReasoningContent string
	ToolCalls        []ToolCall
	FinishReason     string
	// Usage is nil until a provider has supplied usage information.
	Usage *Usage
	Model string
	// Error reports decoding, upstream, read, or unexpected-EOF failure after the
	// HTTP response has opened; opening errors are returned by ChatStream itself.
	Error error
}

// ModelDescriptor is a service-resolved upstream configuration. It contains the
// decrypted API key and must not be serialized into logs or client responses.
// ProviderCode partitions gateway resilience state; BaseURL selects the endpoint.
type ModelDescriptor struct {
	ProviderCode string
	// BaseURL includes the API prefix, such as /v1; adapters append operation paths.
	BaseURL string
	// APIKey is plaintext only at the execution boundary after service-side decryption.
	APIKey string
	// ModelName is the upstream identifier, not the local display alias.
	ModelName   string
	Temperature float32
	TopP        float32
	MaxTokens   int
}
