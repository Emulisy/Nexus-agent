// Operational repositories persist memories, workflow state, and integrations.
// Methods with creator/user predicates are used by ownership-sensitive services.
// Unscoped methods remain explicit internal operations; they do not infer identity
// from context. External vectors and remote clients are coordinated by services.
package repo

import (
	"context"

	"gorm.io/gorm"

	"github.com/chengpeng-cp/nexus-agent/internal/model"
)

// MemoryRepo stores long-term memory text/metadata; embeddings are maintained by the long-term manager.
type MemoryRepo struct {
	db *gorm.DB
}

// Create inserts memory metadata and fills the ID used as its vector-store primary key.
func (r *MemoryRepo) Create(ctx context.Context, m *model.Memory) error {
	return r.db.WithContext(ctx).Create(m).Error
}

// UpdateContent changes stored text only; callers must synchronize any associated embedding.
func (r *MemoryRepo) UpdateContent(ctx context.Context, id int64, content string) error {
	return r.db.WithContext(ctx).Model(&model.Memory{}).
		Where("id = ?", id).Update("content", content).Error
}

// Archive writes status 2 so active-memory queries stop returning the record.
func (r *MemoryRepo) Archive(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Model(&model.Memory{}).
		Where("id = ?", id).Update("status", 2).Error
}

// ListActive selects enabled memories for a user, newest first.
// With an agent ID it includes both user-wide and matching agent-scoped records;
// a nil agent ID leaves all active memories for the user in scope.
func (r *MemoryRepo) ListActive(ctx context.Context, userID int64, agentID *int64, limit int) ([]model.Memory, error) {
	var list []model.Memory
	q := r.db.WithContext(ctx).Where("user_id = ? AND status = 1", userID)
	if agentID != nil {
		// Include user-wide memory and memory scoped to this agent.
		q = q.Where("agent_id IS NULL OR agent_id = ?", *agentID)
	}
	err := q.Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}

// ListByUser returns all statuses for the management list, newest first with total count.
func (r *MemoryRepo) ListByUser(ctx context.Context, userID int64, offset, limit int) ([]model.Memory, int64, error) {
	var (
		list  []model.Memory
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.Memory{}).Where("user_id = ?", userID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// Get loads memory metadata so the service can validate its owner before modifying it.
func (r *MemoryRepo) Get(ctx context.Context, id int64) (*model.Memory, error) {
	var m model.Memory
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// Delete removes metadata only; the service separately attempts vector removal.
func (r *MemoryRepo) Delete(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.Memory{}, id).Error
}

// ---- Workflow ----

// WorkflowRepo stores definitions, run records, and step snapshots used for history and resume.
type WorkflowRepo struct {
	db *gorm.DB
}

// CreateWorkflow inserts a definition whose DSL and creator were supplied by the service.
func (r *WorkflowRepo) CreateWorkflow(ctx context.Context, w *model.Workflow) error {
	return r.db.WithContext(ctx).Create(w).Error
}

// UpdateWorkflow saves the full supplied definition without adding a creator predicate.
func (r *WorkflowRepo) UpdateWorkflow(ctx context.Context, w *model.Workflow) error {
	return r.db.WithContext(ctx).Save(w).Error
}

// DeleteWorkflow deletes by ID without owner filtering; request paths use the creator-scoped variant.
func (r *WorkflowRepo) DeleteWorkflow(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.Workflow{}, id).Error
}

// GetWorkflow performs an unscoped definition lookup for callers that already established access.
func (r *WorkflowRepo) GetWorkflow(ctx context.Context, id int64) (*model.Workflow, error) {
	var w model.Workflow
	if err := r.db.WithContext(ctx).First(&w, id).Error; err != nil {
		return nil, err
	}
	return &w, nil
}

// ListWorkflows lists definitions across creators; request-facing services use ListWorkflowsForCreator.
func (r *WorkflowRepo) ListWorkflows(ctx context.Context, offset, limit int) ([]model.Workflow, int64, error) {
	var (
		list  []model.Workflow
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.Workflow{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// GetWorkflowForCreator requires an exact ID/creator match and returns GORM not-found otherwise.
func (r *WorkflowRepo) GetWorkflowForCreator(ctx context.Context, id, creatorID int64) (*model.Workflow, error) {
	var w model.Workflow
	if err := r.db.WithContext(ctx).Where("id = ? AND creator_id = ?", id, creatorID).First(&w).Error; err != nil {
		return nil, err
	}
	return &w, nil
}

// ListWorkflowsForCreator scopes both the total count and newest-first page to one creator.
func (r *WorkflowRepo) ListWorkflowsForCreator(ctx context.Context, creatorID int64, offset, limit int) ([]model.Workflow, int64, error) {
	var list []model.Workflow
	var total int64
	q := r.db.WithContext(ctx).Model(&model.Workflow{}).Where("creator_id = ?", creatorID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// UpdateWorkflowForCreator writes an explicit editable-field map and reports not-found for unmatched ownership.
func (r *WorkflowRepo) UpdateWorkflowForCreator(ctx context.Context, w *model.Workflow, creatorID int64) error {
	result := r.db.WithContext(ctx).Model(&model.Workflow{}).
		Where("id = ? AND creator_id = ?", w.ID, creatorID).
		Updates(map[string]any{
			"name": w.Name, "description": w.Description, "dsl": w.DSL,
			"is_active": w.IsActive, "status": w.Status,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteWorkflowForCreator performs an owner-filtered delete and checks that a row was affected.
func (r *WorkflowRepo) DeleteWorkflowForCreator(ctx context.Context, id, creatorID int64) error {
	result := r.db.WithContext(ctx).Where("creator_id = ?", creatorID).Delete(&model.Workflow{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// CreateRun inserts the running record before the workflow engine starts execution.
func (r *WorkflowRepo) CreateRun(ctx context.Context, run *model.WorkflowRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

// UpdateRun saves lifecycle status, output/error, and timestamps after execution or approval.
func (r *WorkflowRepo) UpdateRun(ctx context.Context, run *model.WorkflowRun) error {
	return r.db.WithContext(ctx).Save(run).Error
}

// GetRun loads an unscoped run and preloads its step association.
func (r *WorkflowRepo) GetRun(ctx context.Context, id int64) (*model.WorkflowRun, error) {
	var run model.WorkflowRun
	if err := r.db.WithContext(ctx).Preload("Steps").First(&run, id).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

// ListRuns returns newest runs across creators, optionally restricted by workflow ID.
func (r *WorkflowRepo) ListRuns(ctx context.Context, workflowID int64, offset, limit int) ([]model.WorkflowRun, int64, error) {
	var (
		list  []model.WorkflowRun
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.WorkflowRun{})
	if workflowID > 0 {
		q = q.Where("workflow_id = ?", workflowID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// GetRunForCreator joins the parent definition to enforce ownership and preloads the step snapshots.
func (r *WorkflowRepo) GetRunForCreator(ctx context.Context, id, creatorID int64) (*model.WorkflowRun, error) {
	var run model.WorkflowRun
	err := r.db.WithContext(ctx).
		Joins("JOIN workflows ON workflows.id = workflow_runs.workflow_id").
		Where("workflow_runs.id = ? AND workflows.creator_id = ?", id, creatorID).
		Preload("Steps").First(&run).Error
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// ListRunsForCreator applies parent-workflow ownership to both counting and paginated history.
func (r *WorkflowRepo) ListRunsForCreator(ctx context.Context, creatorID, workflowID int64, offset, limit int) ([]model.WorkflowRun, int64, error) {
	var list []model.WorkflowRun
	var total int64
	q := r.db.WithContext(ctx).Model(&model.WorkflowRun{}).
		Joins("JOIN workflows ON workflows.id = workflow_runs.workflow_id").
		Where("workflows.creator_id = ?", creatorID)
	if workflowID > 0 {
		q = q.Where("workflow_runs.workflow_id = ?", workflowID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("workflow_runs.id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// UpsertStepRun saves a snapshot when its ID is already known and otherwise inserts it.
// The method does not resolve conflicts by run ID/node key; callers must carry
// the existing snapshot ID when updating a previously persisted row.
func (r *WorkflowRepo) UpsertStepRun(ctx context.Context, s *model.WorkflowStepRun) error {
	if s.ID > 0 {
		return r.db.WithContext(ctx).Save(s).Error
	}
	return r.db.WithContext(ctx).Create(s).Error
}

// ---- MCP ----

// MCPRepo stores remote server configuration, health metadata, and discovered-tool cache.
type MCPRepo struct {
	db *gorm.DB
}

// Create inserts a remote MCP server configuration.
func (r *MCPRepo) Create(ctx context.Context, s *model.MCPServer) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// Update saves the full server record, including status/check time and cached tool definitions.
func (r *MCPRepo) Update(ctx context.Context, s *model.MCPServer) error {
	return r.db.WithContext(ctx).Save(s).Error
}

// Delete removes the server row after the service has removed its tool catalog entries.
func (r *MCPRepo) Delete(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.MCPServer{}, id).Error
}

// Get loads current server URL/headers for management operations or a forwarding handler.
func (r *MCPRepo) Get(ctx context.Context, id int64) (*model.MCPServer, error) {
	var s model.MCPServer
	if err := r.db.WithContext(ctx).First(&s, id).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// List returns server registrations in stable ascending ID order.
func (r *MCPRepo) List(ctx context.Context) ([]model.MCPServer, error) {
	var list []model.MCPServer
	err := r.db.WithContext(ctx).Order("id ASC").Find(&list).Error
	return list, err
}

// DeleteToolsByServer removes only MCP-type catalog entries associated with the selected server.
func (r *MCPRepo) DeleteToolsByServer(ctx context.Context, serverID int64) error {
	return r.db.WithContext(ctx).
		Where("mcp_server_id = ? AND type = ?", serverID, model.ToolTypeMCP).
		Delete(&model.Tool{}).Error
}

// ---- A2A ----

// A2ARepo stores remote-agent registrations and their last fetched card/health snapshots.
type A2ARepo struct {
	db *gorm.DB
}

// Create inserts a remote registration with its fetched Agent Card.
func (r *A2ARepo) Create(ctx context.Context, a *model.A2AAgent) error {
	return r.db.WithContext(ctx).Create(a).Error
}

// Update saves the registration and its latest card/availability metadata.
func (r *A2ARepo) Update(ctx context.Context, a *model.A2AAgent) error {
	return r.db.WithContext(ctx).Save(a).Error
}

// Delete removes the local registration without making a remote protocol request.
func (r *A2ARepo) Delete(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.A2AAgent{}, id).Error
}

// Get loads the saved base URL and card metadata for one remote agent.
func (r *A2ARepo) Get(ctx context.Context, id int64) (*model.A2AAgent, error) {
	var a model.A2AAgent
	if err := r.db.WithContext(ctx).First(&a, id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// List returns remote registrations in ascending ID order.
func (r *A2ARepo) List(ctx context.Context) ([]model.A2AAgent, error) {
	var list []model.A2AAgent
	err := r.db.WithContext(ctx).Order("id ASC").Find(&list).Error
	return list, err
}
