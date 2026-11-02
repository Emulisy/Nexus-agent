// Workflow entities separate saved YAML definitions from execution records.
// Each run stores input/output and lifecycle status; step snapshots retain the
// node-level results used when rebuilding state for human approval.
package model

import (
	"time"

	"gorm.io/datatypes"
)

// Workflow stores a creator-owned YAML DSL definition plus display and activation metadata.
type Workflow struct {
	ID          int64  `gorm:"primaryKey" json:"id"`
	Name        string `gorm:"size:128" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	DSL         string `gorm:"column:dsl;type:text" json:"dsl"`
	Version     int    `gorm:"default:1" json:"version"`
	IsActive    bool   `json:"is_active"`
	// CreatorID is set from authenticated creation context and scopes definition/run access.
	CreatorID int64 `json:"creator_id"`
	// Status defaults to enabled when omitted on insertion; update pointers preserve explicit zero values.
	Status    int16     `gorm:"default:1" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WorkflowRun records one execution, including input/output, lifecycle timestamps, and associated step snapshots.
type WorkflowRun struct {
	ID          int64  `gorm:"primaryKey" json:"id"`
	WorkflowID  int64  `gorm:"index" json:"workflow_id"`
	TriggerType string `gorm:"size:20;default:manual" json:"trigger_type"`
	Status      string `gorm:"size:24;index;default:running" json:"status"`
	// Input is the original state object saved before execution and reused during approval reconstruction.
	Input datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"input"`
	// Output holds the engine result or the recorded rejection decision.
	Output     datatypes.JSON `gorm:"type:jsonb" json:"output,omitempty"`
	Error      string         `gorm:"size:512" json:"error"`
	StartedAt  *time.Time     `json:"started_at"`
	FinishedAt *time.Time     `json:"finished_at"`
	CreatedAt  time.Time      `json:"created_at"`

	// Steps is a GORM association loaded from workflow_step_runs, not a column on the run row.
	Steps []WorkflowStepRun `gorm:"foreignKey:RunID" json:"steps,omitempty"`
}

// Workflow run status strings are shared by the engine, persistence, and approval API.
const (
	RunStatusRunning   = "running"
	RunStatusWaiting   = "waiting_approval"
	RunStatusSucceeded = "succeeded"
	RunStatusFailed    = "failed"
	RunStatusCanceled  = "canceled"
)

// WorkflowStepRun stores a node execution snapshot whose output can rebuild state when a run resumes.
type WorkflowStepRun struct {
	ID    int64 `gorm:"primaryKey" json:"id"`
	RunID int64 `gorm:"index:idx_step_runs_run,priority:1" json:"run_id"`
	// NodeKey identifies the DSL node whose output becomes a key in reconstructed execution state.
	NodeKey string `gorm:"size:64" json:"node_key"`
	// NodeType matches one of the DSL node-type strings below.
	NodeType   string         `gorm:"size:24" json:"node_type"`
	Status     string         `gorm:"size:20;default:running" json:"status"`
	Input      datatypes.JSON `gorm:"type:jsonb" json:"input,omitempty"`
	Output     datatypes.JSON `gorm:"type:jsonb" json:"output,omitempty"`
	Error      string         `gorm:"size:512" json:"error"`
	Tokens     int            `json:"tokens"`
	StartedAt  *time.Time     `json:"started_at"`
	FinishedAt *time.Time     `json:"finished_at"`
}

// Node type values identify the DSL operations supported by the workflow engine.
const (
	NodeLLM       = "llm"
	NodeTool      = "tool"
	NodeKB        = "kb"
	NodeCondition = "condition"
	NodeParallel  = "parallel"
	NodeHuman     = "human"
	NodeSubflow   = "subflow"
)
