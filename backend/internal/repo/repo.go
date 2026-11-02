// Package repo provides GORM-backed persistence shared by API and worker services.
// Repository methods return storage errors, including gorm.ErrRecordNotFound,
// without translating them to HTTP/application codes. Services provide ownership
// context and coordinate operations that span repositories or external stores.
// Transactions are explicit where multiple database operations must be atomic.
package repo

import (
	"gorm.io/gorm"
)

// Repos aggregates repository handles over one shared GORM database/pool.
type Repos struct {
	DB *gorm.DB

	User     *UserRepo
	Session  *SessionRepo
	Agent    *AgentRepo
	Tool     *ToolRepo
	LLM      *LLMRepo
	KB       *KBRepo
	Memory   *MemoryRepo
	Workflow *WorkflowRepo
	MCP      *MCPRepo
	A2A      *A2ARepo
}

// New constructs repository handles without opening additional connections or applying schema migrations.
func New(db *gorm.DB) *Repos {
	return &Repos{
		DB:       db,
		User:     &UserRepo{db: db},
		Session:  &SessionRepo{db: db},
		Agent:    &AgentRepo{db: db},
		Tool:     &ToolRepo{db: db},
		LLM:      &LLMRepo{db: db},
		KB:       &KBRepo{db: db},
		Memory:   &MemoryRepo{db: db},
		Workflow: &WorkflowRepo{db: db},
		MCP:      &MCPRepo{db: db},
		A2A:      &A2ARepo{db: db},
	}
}
