// Long-term memory records provide ownership, scope, and lifecycle metadata for recall.
package model

import (
	"time"
)

// Memory stores text/metadata in PostgreSQL and its embedding in Milvus memories.
// The record ID links both stores; active-state checks determine whether a
// stored vector candidate is eligible for recall.
type Memory struct {
	ID     int64 `gorm:"primaryKey" json:"id"`
	UserID int64 `gorm:"index:idx_memories_user,priority:1" json:"user_id"`
	// AgentID nil denotes user-wide memory; a value scopes the record to an agent.
	AgentID  *int64 `gorm:"index:idx_memories_user,priority:2" json:"agent_id"`
	Content  string `gorm:"size:1024" json:"content"`
	VectorID string `gorm:"size:64" json:"vector_id"`
	// Scope stores the user/agent classification alongside the optional agent ID.
	Scope string `gorm:"size:16;default:user" json:"scope"`
	// Importance stores the extraction-assigned relevance score for the memory fact.
	Importance int16      `gorm:"default:5" json:"importance"`
	HitCount   int        `json:"hit_count"`
	LastHitAt  *time.Time `json:"last_hit_at"`
	// Status 1 is active and status 2 is archived; archival retains the text and embedding.
	Status    int16     `gorm:"index:idx_memories_user,priority:3;default:1" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
