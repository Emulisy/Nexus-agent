// Agent queries distinguish shared definitions from caller-owned definitions.
// Read queries include owner_id IS NULL; write queries require an exact owner.
// The tool catalog is shared and resolves the enabled bindings supplied to the
// runtime executor, independently of these agent ownership queries.
package repo

import (
	"context"

	"gorm.io/gorm"

	"github.com/chengpeng-cp/nexus-agent/internal/model"
)

// AgentRepo persists agent configuration and enforces owner predicates in scoped methods.
type AgentRepo struct {
	db *gorm.DB
}

// Create inserts the supplied agent; the service must set ownership and required model IDs first.
func (r *AgentRepo) Create(ctx context.Context, a *model.Agent) error {
	return r.db.WithContext(ctx).Create(a).Error
}

// GetByIDForUser returns only a shared agent or one owned by the supplied user.
func (r *AgentRepo) GetByIDForUser(ctx context.Context, id, userID int64) (*model.Agent, error) {
	var a model.Agent
	if err := r.db.WithContext(ctx).
		Where("id = ? AND (owner_id IS NULL OR owner_id = ?)", id, userID).
		First(&a).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// ListForUser lists shared/owned agents with optional case-insensitive name/description matching.
func (r *AgentRepo) ListForUser(ctx context.Context, userID int64, keyword string, offset, limit int) ([]model.Agent, int64, error) {
	var list []model.Agent
	var total int64
	q := r.db.WithContext(ctx).Model(&model.Agent{}).
		Where("(owner_id IS NULL OR owner_id = ?)", userID)
	if keyword != "" {
		q = q.Where("name ILIKE ? OR description ILIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id ASC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// UpdateForUser writes an explicit editable-field map for the owner/ID pair.
// Using a map retains false/zero values while excluding fields outside the edit
// contract. No matching row is returned as gorm.ErrRecordNotFound.
func (r *AgentRepo) UpdateForUser(ctx context.Context, a *model.Agent, userID int64) error {
	result := r.db.WithContext(ctx).Model(&model.Agent{}).
		Where("id = ? AND owner_id = ?", a.ID, userID).
		Updates(map[string]any{
			"name": a.Name, "description": a.Description, "type": a.Type,
			"system_prompt": a.SystemPrompt, "model_config_id": a.ModelConfigID,
			"temperature": a.Temperature, "max_tokens": a.MaxTokens,
			"max_iterations": a.MaxIterations, "memory_enabled": a.MemoryEnabled,
			"memory_long_enabled": a.MemoryLongEnabled, "kb_ids": a.KBIDs,
			"tools": a.Tools,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteForUser deletes only an owned row and reports not-found when the owner/ID pair does not match.
func (r *AgentRepo) DeleteForUser(ctx context.Context, id, userID int64) error {
	result := r.db.WithContext(ctx).Where("owner_id = ?", userID).Delete(&model.Agent{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ListExpertsForUser supplies the host with all enabled expert agents that are shared or caller-owned.
func (r *AgentRepo) ListExpertsForUser(ctx context.Context, userID int64) ([]model.Agent, error) {
	var list []model.Agent
	err := r.db.WithContext(ctx).
		Where("type = ? AND status = 1 AND (owner_id IS NULL OR owner_id = ?)", model.AgentTypeExpert, userID).
		Find(&list).Error
	return list, err
}

// ---- Tool ----

// ToolRepo stores catalog definitions; runtime handlers are held separately by the tool registry.
type ToolRepo struct {
	db *gorm.DB
}

// Create inserts a tool definition and fills its generated database ID.
func (r *ToolRepo) Create(ctx context.Context, t *model.Tool) error {
	return r.db.WithContext(ctx).Create(t).Error
}

// GetByID loads a tool catalog record by numeric ID without filtering its enabled status.
func (r *ToolRepo) GetByID(ctx context.Context, id int64) (*model.Tool, error) {
	var t model.Tool
	if err := r.db.WithContext(ctx).First(&t, id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// GetByCode resolves a unique tool code for validation and runtime metadata lookup.
func (r *ToolRepo) GetByCode(ctx context.Context, code string) (*model.Tool, error) {
	var t model.Tool
	if err := r.db.WithContext(ctx).Where("code = ?", code).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// ListByCodes returns only enabled catalog records for a set of agent tool codes.
func (r *ToolRepo) ListByCodes(ctx context.Context, codes []string) ([]model.Tool, error) {
	var list []model.Tool
	err := r.db.WithContext(ctx).Where("code IN ? AND status = 1", codes).Find(&list).Error
	return list, err
}

// ListByIDs resolves enabled numeric-ID bindings into the definitions used by the executor.
func (r *ToolRepo) ListByIDs(ctx context.Context, ids []int64) ([]model.Tool, error) {
	var list []model.Tool
	err := r.db.WithContext(ctx).Where("id IN ? AND status = 1", ids).Find(&list).Error
	return list, err
}

// List exposes catalog records with an optional type filter, stable ID ordering, and total count.
func (r *ToolRepo) List(ctx context.Context, typ string, offset, limit int) ([]model.Tool, int64, error) {
	var (
		list  []model.Tool
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.Tool{})
	if typ != "" {
		q = q.Where("type = ?", typ)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id ASC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}
