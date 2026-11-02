// Package model defines persisted entities and API-facing data structures.
// GORM tags encode table relationships/defaults, while JSON tags control the
// public representation. Sensitive fields are excluded explicitly; services
// still construct reduced views where the complete entity is not appropriate.
package model

import (
	"time"

	"gorm.io/datatypes"
)

// User stores account identity, role, status, and a bcrypt password hash that is excluded from JSON.
type User struct {
	ID       int64  `gorm:"primaryKey" json:"id"`
	Username string `gorm:"size:64;uniqueIndex" json:"username"`
	Email    string `gorm:"size:128;uniqueIndex" json:"email"`
	// PasswordHash contains bcrypt output and must never appear in a JSON response.
	PasswordHash string `gorm:"size:256" json:"-"`
	// Role uses the admin/user distinction checked by access-token middleware.
	Role      string `gorm:"size:20;default:user" json:"role"`
	AvatarURL string `gorm:"size:512" json:"avatar_url"`
	// Status 1 permits login; other values are treated as a locked account by AuthService.
	Status    int16     `gorm:"default:1" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Session stores conversation ownership and display statistics under a database-generated UUID.
type Session struct {
	ID     string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID int64  `gorm:"index" json:"user_id"`
	// AgentID records the creation-time agent association; chat still selects its agent from each request.
	AgentID *int64 `json:"agent_id"`
	// Title begins with the database default and can be automatically set from the first user message.
	Title string `gorm:"size:256;default:New chat" json:"title"`
	// Status controls inclusion in active-session listings; archived rows remain persisted.
	Status string `gorm:"size:20;default:active" json:"status"`
	Pinned bool   `json:"pinned"`
	// MessageCount is incremented separately after each user/assistant message write.
	MessageCount int `json:"message_count"`
	// TokenUsage accumulates counts supplied by chat persistence rather than recounting stored messages.
	TokenUsage int64          `json:"token_usage"`
	LastMsgAt  *time.Time     `json:"last_msg_at"`
	Metadata   datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// Message stores user/assistant content plus structured generation metadata.
// Tool calls, references, and the execution trace are JSONB payloads. Replaying
// conversation context is a separate service conversion, not direct entity serialization.
type Message struct {
	ID        int64  `gorm:"primaryKey" json:"id"`
	SessionID string `gorm:"type:uuid;index:idx_messages_session,priority:1" json:"session_id"`
	// Role identifies user, assistant, system, or tool content in the stored conversation protocol.
	Role    string `gorm:"size:20" json:"role"`
	Content string `gorm:"type:text" json:"content"`
	// ReasoningContent retains model reasoning events separately from the answer text.
	ReasoningContent string `gorm:"type:text" json:"reasoning_content,omitempty"`
	// ToolCalls stores structured calls collected during the run, not an independent tool-message history.
	ToolCalls  datatypes.JSON `gorm:"type:jsonb" json:"tool_calls,omitempty"`
	ToolCallID string         `gorm:"size:128" json:"tool_call_id,omitempty"`
	// Refs stores retrieval references and source metadata attached to the assistant response.
	Refs             datatypes.JSON `gorm:"type:jsonb" json:"refs,omitempty"`
	ModelName        string         `gorm:"size:64" json:"model_name,omitempty"`
	PromptTokens     int            `json:"prompt_tokens"`
	CompletionTokens int            `json:"completion_tokens"`
	FirstTokenMs     int            `json:"first_token_ms"`
	LatencyMs        int            `json:"latency_ms"`
	Status           int16          `gorm:"default:1" json:"status"`
	// Metadata carries the agent trace snapshot used by execution-history views.
	Metadata  datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	CreatedAt time.Time      `gorm:"index:idx_messages_session,priority:2" json:"created_at"`
}

// Message status values are persisted as integers; individual writers decide which terminal status to record.
const (
	MsgStatusDone    int16 = 1
	MsgStatusStopped int16 = 2
	MsgStatusFailed  int16 = 3
)
