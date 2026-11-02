// Agent management resolves defaults and applies owner-aware persistence.
// Shared agents can be read by users, while update/delete queries require an
// exact owner match. Editable fields are merged explicitly to avoid zeroing
// stored configuration that the form does not submit.
package service

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"

	"github.com/Emulisy/SupaAgent/internal/config"
	"github.com/Emulisy/SupaAgent/internal/model"
	"github.com/Emulisy/SupaAgent/internal/pkg/errcode"
	"github.com/Emulisy/SupaAgent/internal/pkg/pagination"
	"github.com/Emulisy/SupaAgent/internal/repo"
)

// AgentService validates agent definitions and resolves their default chat-model configuration.
type AgentService struct {
	repos *repo.Repos
	cfg   *config.Config
}

// NewAgentService shares repositories and default-model configuration with agent management.
func NewAgentService(repos *repo.Repos, cfg *config.Config) *AgentService {
	return &AgentService{repos: repos, cfg: cfg}
}

// List returns shared and caller-owned agents using the requested keyword and pagination.
func (s *AgentService) List(ctx context.Context, userID int64, q pagination.Query) ([]model.Agent, int64, error) {
	return s.repos.Agent.ListForUser(ctx, userID, q.Keyword, q.Offset(), q.Limit())
}

// Get maps an inaccessible or missing agent to not-found and preserves other storage failures as internal errors.
func (s *AgentService) Get(ctx context.Context, userID, id int64) (*model.Agent, error) {
	ag, err := s.repos.Agent.GetByIDForUser(ctx, id, userID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.ErrAgentNotFound
		}
		return nil, errcode.ErrInternal.WithCause(err)
	}
	return ag, nil
}

// Create validates the name, fills an absent type/model, and checks named tool bindings.
// The caller's user ID replaces any supplied owner. Missing model configuration
// is reported before insertion so the required foreign key is never left zero.
func (s *AgentService) Create(ctx context.Context, userID int64, ag *model.Agent) error {
	if ag.Name == "" {
		return errcode.ErrInvalidParam.WithMsg("name is required")
	}
	if ag.Type == "" {
		ag.Type = model.AgentTypeExpert
	}
	if err := s.resolveModel(ctx, ag); err != nil {
		return err
	}
	if err := s.validateToolBindings(ctx, ag.Tools); err != nil {
		return err
	}
	ag.OwnerID = &userID
	return s.repos.Agent.Create(ctx, ag)
}

// Update reads an accessible agent, checks tool codes, and copies editable fields.
// The owner-filtered update then writes those fields, including explicit zero
// values, while preserving fallback, top_p, window size, and other stored settings.
func (s *AgentService) Update(ctx context.Context, userID int64, ag *model.Agent) error {
	cur, err := s.Get(ctx, userID, ag.ID)
	if err != nil {
		return err
	}
	if err := s.resolveModel(ctx, ag); err != nil {
		return err
	}
	if err := s.validateToolBindings(ctx, ag.Tools); err != nil {
		return err
	}
	cur.Name = ag.Name
	cur.Type = ag.Type
	cur.Description = ag.Description
	cur.SystemPrompt = ag.SystemPrompt
	cur.ModelConfigID = ag.ModelConfigID
	cur.Temperature = ag.Temperature
	cur.MaxTokens = ag.MaxTokens
	cur.MaxIterations = ag.MaxIterations
	cur.MemoryEnabled = ag.MemoryEnabled
	cur.MemoryLongEnabled = ag.MemoryLongEnabled
	cur.KBIDs = ag.KBIDs
	cur.Tools = ag.Tools
	// Keep fields outside the edit form unchanged.
	return s.repos.Agent.UpdateForUser(ctx, cur, userID)
}

// resolveModel applies the same default and availability checks on create/edit.
// A blank model must not write a zero foreign key, or bind a disabled/non-chat model.
func (s *AgentService) resolveModel(ctx context.Context, ag *model.Agent) error {
	var m *model.ModelConfig
	var err error
	if ag.ModelConfigID <= 0 {
		m, err = s.repos.LLM.DefaultModel(ctx, s.cfg.LLM.DefaultChat, modelTypeChat)
	} else {
		m, err = s.repos.LLM.GetModel(ctx, ag.ModelConfigID)
	}
	if err != nil {
		return errcode.ErrInvalidParam.WithMsg("No available chat model was found. Choose an enabled model in Models.").WithCause(err)
	}
	if m.Type != modelTypeChat || m.Status != 1 || m.Provider == nil || m.Provider.Status != 1 ||
		(m.Provider.APIKey == "" && m.Provider.Code != "ollama") {
		return errcode.ErrInvalidParam.WithMsg("The selected model is unavailable. Choose an enabled chat model with a configured provider.")
	}
	ag.ModelConfigID = m.ID
	return nil
}

// Delete removes only an owned agent; no matching owner/ID pair becomes agent-not-found.
func (s *AgentService) Delete(ctx context.Context, userID, id int64) error {
	err := s.repos.Agent.DeleteForUser(ctx, id, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errcode.ErrAgentNotFound
	}
	if err != nil {
		return errcode.ErrInternal.WithCause(err)
	}
	return nil
}

// validateToolBindings validates both numeric-ID and stable-code bindings against
// enabled catalog records, rather than silently dropping invalid ID-only entries.
func (s *AgentService) validateToolBindings(ctx context.Context, toolsJSON []byte) error {
	if len(toolsJSON) == 0 || string(toolsJSON) == "[]" {
		return nil
	}
	bindings, err := parseToolBindings(toolsJSON)
	if err != nil {
		return errcode.ErrInvalidParam.WithMsg("Invalid tools format")
	}
	for _, b := range bindings {
		var t *model.Tool
		if b.ToolCode != "" {
			t, err = s.repos.Tool.GetByCode(ctx, b.ToolCode)
		} else if b.ToolID > 0 {
			t, err = s.repos.Tool.GetByID(ctx, b.ToolID)
		} else {
			return errcode.ErrInvalidParam.WithMsg("Each tool binding must include a tool ID or code")
		}
		if err != nil || t.Status != 1 {
			return errcode.ErrToolNotFound.WithMsg("A selected tool is missing or disabled. Refresh the tool list and select it again.")
		}
	}
	return nil
}

// parseToolBindings decodes the ID/code subset needed for agent management validation.
func parseToolBindings(raw []byte) ([]struct {
	ToolID   int64  `json:"tool_id"`
	ToolCode string `json:"tool_code"`
}, error) {
	var out []struct {
		ToolID   int64  `json:"tool_id"`
		ToolCode string `json:"tool_code"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
