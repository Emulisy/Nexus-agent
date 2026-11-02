// Package a2a implements discovery and synchronous Agent-to-Agent JSON-RPC calls.
// The current transport exchanges text parts and completed/failed task results;
// streamed tasks and non-text media are not implemented by these adapters.
package a2a

// AgentCard is the discovery document published at /.well-known/agent.json.
// Its URL is the JSON-RPC endpoint that outbound clients prefer after discovery.
type AgentCard struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	URL          string `json:"url"`
	Version      string `json:"version"`
	Protocol     string `json:"protocolVersion"`
	Capabilities struct {
		Streaming bool `json:"streaming"`
	} `json:"capabilities"`
	Skills   []AgentSkill `json:"skills"`
	Provider struct {
		Organization string `json:"organization"`
	} `json:"provider,omitempty"`
}

// AgentSkill describes a discoverable capability; it does not itself register
// a handler or select a local Agent implementation.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

const (
	MethodMessageSend  = "message/send"
	MethodMessageProbe = "message/ping"
)

const (
	RoleUser  = "user"
	RoleAgent = "agent"
)

// Part represents a message or artifact segment. The client and server process
// text parts only and ignore other type values when extracting conversational text.
type Part struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Message carries a role and ordered parts in the A2A wire format, independently
// of the internal model Message used for database history.
type Message struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

const (
	StateCompleted = "completed"
	StateFailed    = "failed"
)

// Task is the synchronous execution result. Successful answers are represented
// both as an Agent message and as a text artifact by the local server.
type Task struct {
	ID        string     `json:"id"`
	State     string     `json:"status"`
	Messages  []Message  `json:"messages,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

// Artifact groups generated parts, optionally under a descriptive name.
// ExtractText reads these parts in response order.
type Artifact struct {
	Name  string `json:"name,omitempty"`
	Parts []Part `json:"parts"`
}

// SendMessageParams is the message/send parameter envelope consumed by RPCHandler.
type SendMessageParams struct {
	Message Message `json:"message"`
}
