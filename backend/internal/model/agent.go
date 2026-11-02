// Agent and integration models describe persisted configuration, not executable handlers.
// Tool implementations live in the runtime registry; catalog codes and JSON
// bindings link saved agent settings to those implementations.
package model

import (
	"time"

	"gorm.io/datatypes"
)

// Agent stores model, prompt, tool, and memory settings for an expert or host.
// A nil owner denotes a shared definition. Host routing and expert execution
// consume this configuration through request-scoped runtime inputs.
type Agent struct {
	ID int64 `gorm:"primaryKey" json:"id"`
	// OwnerID nil makes the definition shared for reads; owner-filtered writes require a matching user ID.
	OwnerID      *int64 `json:"owner_id,omitempty"`
	Name         string `gorm:"size:128" json:"name"`
	Description  string `gorm:"type:text" json:"description"`
	Type         string `gorm:"size:20;default:expert" json:"type"` // host/expert
	SystemPrompt string `gorm:"type:text" json:"system_prompt"`
	// ModelConfigID references the persisted primary model configuration.
	ModelConfigID int64 `json:"model_config_id"`
	// FallbackModelID is optional; the explicit column tag matches the migration's fallback_model_config_id.
	FallbackModelID *int64  `gorm:"column:fallback_model_config_id" json:"fallback_model_config_id"`
	Temperature     float32 `gorm:"default:0.7" json:"temperature"`
	TopP            float32 `gorm:"default:0.9" json:"top_p"`
	MaxTokens       int     `gorm:"default:4096" json:"max_tokens"`
	// MaxIterations supplies the per-agent ReAct iteration limit in RunInput.
	MaxIterations int `gorm:"default:10" json:"max_iterations"`
	// MemoryEnabled is stored for configuration compatibility; current chat history loading does not consult it.
	MemoryEnabled bool `gorm:"default:true" json:"memory_enabled"`
	// MemoryWindow limits recent stored messages replayed into chat; nonpositive values fall back to 20.
	MemoryWindow int `gorm:"default:20" json:"memory_window"`
	// MemoryLongEnabled gates long-term recall and post-response extraction when a manager is attached.
	MemoryLongEnabled bool `gorm:"default:false" json:"memory_long_enabled"`
	// KBIDs is a JSON array of bound KB IDs; retrieval filters them by current user ownership.
	KBIDs datatypes.JSON `gorm:"type:jsonb;default:'[]'" json:"kb_ids"`
	// Tools is a JSON array of tool_id/tool_code bindings with optional per-binding configuration.
	Tools    datatypes.JSON `gorm:"type:jsonb;default:'[]'" json:"tools"`
	IsPreset bool           `json:"is_preset"`
	// Config retains additional JSON settings; host expert selection currently uses the accessible expert list.
	Config datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"config"`
	// Status defaults to enabled on insertion; expert discovery uses status 1.
	Status    int16     `gorm:"default:1" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Agent type values select host routing versus ordinary expert execution.
const (
	AgentTypeHost   = "host"
	AgentTypeExpert = "expert"
)

// Tool is a catalog definition presented to models and resolved by the executor.
// The record provides code, JSON schema, status, and execution metadata; a
// matching runtime handler must exist separately for local invocation.
type Tool struct {
	ID int64 `gorm:"primaryKey" json:"id"`
	// Code is the stable key joining catalog definitions, agent bindings, and registry handlers.
	Code        string `gorm:"size:64;uniqueIndex" json:"code"`
	Name        string `gorm:"size:128" json:"name"`
	Type        string `gorm:"size:20" json:"type"`
	Description string `gorm:"type:text" json:"description"`
	// Parameters is the model-facing JSON Schema for the tool argument object.
	Parameters    datatypes.JSON `gorm:"type:jsonb" json:"parameters"`
	Handler       string         `gorm:"size:64" json:"handler,omitempty"`
	MCPServerID   *int64         `json:"mcp_server_id,omitempty"`
	Endpoint      string         `gorm:"size:512" json:"endpoint,omitempty"`
	AuthConfig    datatypes.JSON `gorm:"type:jsonb" json:"auth_config,omitempty"`
	DefaultConfig datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"default_config"`
	// TimeoutMs supplies a per-tool execution deadline; nonpositive values use executor defaults.
	TimeoutMs int `gorm:"default:30000" json:"timeout_ms"`
	// IsDangerous is catalog metadata; it does not by itself implement an approval gate.
	IsDangerous bool      `json:"is_dangerous"`
	Status      int16     `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Tool type values distinguish built-in, MCP-backed, and custom catalog entries.
const (
	ToolTypeBuiltin = "builtin"
	ToolTypeMCP     = "mcp"
	ToolTypeCustom  = "custom"
)

// MCPServer stores remote connection settings, last availability check, and the cached discovery result.
type MCPServer struct {
	ID   int64  `gorm:"primaryKey" json:"id"`
	Name string `gorm:"size:64" json:"name"`
	// Transport is persisted metadata; this client path constructs URL-based MCP connections.
	Transport string         `gorm:"size:20" json:"transport"`
	URL       string         `gorm:"size:512" json:"url"`
	Command   string         `gorm:"size:512" json:"command"`
	Args      datatypes.JSON `gorm:"type:jsonb;default:'[]'" json:"args"`
	Env       datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"env"`
	// Headers stores the JSON object sent as remote authentication/request headers.
	Headers      datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"headers"`
	Status       string         `gorm:"size:20;default:disconnected" json:"status"`
	LastHealthAt *time.Time     `json:"last_health_at"`
	// ToolsCache retains the most recent successful discovery response for management views.
	ToolsCache datatypes.JSON `gorm:"type:jsonb;default:'[]'" json:"tools_cache"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// A2AAgent stores a remote base URL plus the most recently fetched Agent Card and availability status.
type A2AAgent struct {
	ID      int64  `gorm:"primaryKey" json:"id"`
	Name    string `gorm:"size:128" json:"name"`
	BaseURL string `gorm:"size:512" json:"base_url"`
	// Card is a JSON snapshot, refreshed during registration and explicit health checks.
	Card         datatypes.JSON `gorm:"type:jsonb" json:"card"`
	Status       string         `gorm:"size:20;default:unknown" json:"status"`
	LastHealthAt *time.Time     `json:"last_health_at"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// TableName matches a2a_agents from migrations instead of GORM's default a2_a_agents convention.
func (A2AAgent) TableName() string { return "a2a_agents" }
