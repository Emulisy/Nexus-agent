// Workflow services persist definitions and execute parsed DSL through the engine.
// Owner-filtered queries scope definitions and run history to their creator.
// Runs and step snapshots are stored around synchronous engine calls; human
// approval reconstructs state from the original input and completed step outputs.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/Emulisy/SupaAgent/internal/model"
	"github.com/Emulisy/SupaAgent/internal/pkg/errcode"
	"github.com/Emulisy/SupaAgent/internal/pkg/pagination"
	"github.com/Emulisy/SupaAgent/internal/security"
	"github.com/Emulisy/SupaAgent/internal/workflow"
)

// WorkflowService shares repositories and receives an engine after the application dependencies are assembled.
type WorkflowService struct {
	svcs   *Services
	engine *workflow.Engine
}

// NewWorkflowService constructs definition management before an execution engine is attached.
func NewWorkflowService(svcs *Services) *WorkflowService {
	return &WorkflowService{svcs: svcs}
}

// SetEngine attaches execution and resume support after model/tool/RAG callbacks are available.
func (s *WorkflowService) SetEngine(e *workflow.Engine) { s.engine = e }

// ---- CRUD ----

// List returns only workflows created by the caller, with page and total count.
func (s *WorkflowService) List(ctx context.Context, userID int64, q pagination.Query) ([]model.Workflow, int64, error) {
	return s.svcs.Deps.Repos.Workflow.ListWorkflowsForCreator(ctx, userID, q.Offset(), q.Limit())
}

// Get loads an owned definition and translates inaccessible or missing records to the workflow not-found error.
func (s *WorkflowService) Get(ctx context.Context, userID, id int64) (*model.Workflow, error) {
	w, err := s.svcs.Deps.Repos.Workflow.GetWorkflowForCreator(ctx, id, userID)
	if err != nil {
		return nil, errcode.ErrRunNotFound.WithMsg("Workflow not found").WithCause(err)
	}
	return w, nil
}

// Create validates the DSL before persisting the definition.
// An empty name or description inherits parsed DSL metadata; creator assignment
// is supplied by the caller, normally the authenticated HTTP handler.
func (s *WorkflowService) Create(ctx context.Context, w *model.Workflow) error {
	d, err := workflow.ParseDSL(w.DSL)
	if err != nil {
		return err
	}
	if w.Name == "" {
		w.Name = d.Name
	}
	if w.Description == "" {
		w.Description = d.Desc
	}
	return s.svcs.Deps.Repos.Workflow.CreateWorkflow(ctx, w)
}

// WorkflowUpdateInput distinguishes omitted fields from explicit false/zero values.
// A supplied DSL is parsed before storage, and its name/description provide
// defaults only when those fields were omitted from the update.
type WorkflowUpdateInput struct {
	Name        *string
	Description *string
	DSL         *string
	IsActive    *bool
	Status      *int16
}

// Update merges supplied fields into an owned definition and saves them through an owner-filtered query.
func (s *WorkflowService) Update(ctx context.Context, userID, id int64, in *WorkflowUpdateInput) error {
	cur, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if in.DSL != nil {
		d, err := workflow.ParseDSL(*in.DSL)
		if err != nil {
			return err
		}
		cur.DSL = *in.DSL
		if in.Name == nil {
			cur.Name = d.Name
		}
		if in.Description == nil {
			cur.Description = d.Desc
		}
	}
	if in.Name != nil {
		cur.Name = *in.Name
	}
	if in.Description != nil {
		cur.Description = *in.Description
	}
	if in.IsActive != nil {
		cur.IsActive = *in.IsActive
	}
	if in.Status != nil {
		cur.Status = *in.Status
	}
	return s.svcs.Deps.Repos.Workflow.UpdateWorkflowForCreator(ctx, cur, userID)
}

// Delete applies creator ownership in the delete query and maps an unmatched row to not-found.
func (s *WorkflowService) Delete(ctx context.Context, userID, id int64) error {
	err := s.svcs.Deps.Repos.Workflow.DeleteWorkflowForCreator(ctx, id, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errcode.ErrRunNotFound.WithMsg("Workflow not found")
	}
	if err != nil {
		return errcode.ErrInternal.WithCause(err)
	}
	return nil
}

// Validate parses and statically checks DSL without saving a definition or starting execution.
func (s *WorkflowService) Validate(dslText string) error {
	_, err := workflow.ParseDSL(dslText)
	return err
}

// RunInput selects a saved workflow and provides its initial state for a manual run.
type RunInput struct {
	WorkflowID int64          `json:"workflow_id"`
	Input      map[string]any `json:"input"`
}

// Run verifies ownership, attaches the user identity to execution context, and inserts a running record.
// The engine executes synchronously. Returned steps are persisted best effort;
// the final run status/output is then saved and returned. Engine errors trigger
// a best-effort failed-run update before the error is returned to the caller.
func (s *WorkflowService) Run(ctx context.Context, userID int64, in *RunInput) (*model.WorkflowRun, error) {
	if s.engine == nil {
		return nil, errcode.ErrNotImplement.WithMsg("Workflow engine is not configured")
	}
	ctx = security.WithUserID(ctx, userID)
	w, err := s.Get(ctx, userID, in.WorkflowID)
	if err != nil {
		return nil, err
	}
	dsl, err := workflow.ParseDSL(w.DSL)
	if err != nil {
		return nil, err
	}

	run := &model.WorkflowRun{
		WorkflowID: w.ID, TriggerType: "manual",
		Status: model.RunStatusRunning, Input: datatypes.JSON(mustJSON(in.Input)),
		StartedAt: nowPtr(),
	}
	if err := s.svcs.Deps.Repos.Workflow.CreateRun(ctx, run); err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}

	res, steps, err := s.engine.Run(ctx, dsl, in.Input)
	if err != nil {
		run.Status = model.RunStatusFailed
		run.Error = err.Error()
		fin := time.Now()
		run.FinishedAt = &fin
		_ = s.svcs.Deps.Repos.Workflow.UpdateRun(ctx, run)
		return nil, errcode.From(err)
	}

	for key := range steps {
		st := steps[key]
		st.RunID = run.ID
		_ = s.svcs.Deps.Repos.Workflow.UpsertStepRun(ctx, &st)
	}

	run.Status = res.Status
	if out, err := json.Marshal(res.Output); err == nil {
		run.Output = out
	}
	if res.Status == model.RunStatusFailed {
		run.Error = res.Error
	}
	fin := time.Now()
	run.FinishedAt = &fin
	if err := s.svcs.Deps.Repos.Workflow.UpdateRun(ctx, run); err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}
	return run, nil
}

// Approve accepts a decision only for an owned run waiting for human approval.
// Rejection saves a canceled result. Approval rebuilds state from stored input
// and succeeded steps, adds approved=true, and resumes the engine with the
// current saved DSL. New step snapshots and the resulting run are then stored.
func (s *WorkflowService) Approve(ctx context.Context, userID, runID int64, approved bool) (*model.WorkflowRun, error) {
	if s.engine == nil {
		return nil, errcode.ErrNotImplement.WithMsg("Workflow engine is not configured")
	}
	ctx = security.WithUserID(ctx, userID)
	run, err := s.svcs.Deps.Repos.Workflow.GetRunForCreator(ctx, runID, userID)
	if err != nil {
		return nil, errcode.ErrRunNotFound.WithCause(err)
	}
	if run.Status != model.RunStatusWaiting {
		return nil, errcode.ErrInvalidParam.WithMsg("The run is not waiting for approval")
	}
	w, err := s.Get(ctx, userID, run.WorkflowID)
	if err != nil {
		return nil, err
	}
	dsl, err := workflow.ParseDSL(w.DSL)
	if err != nil {
		return nil, err
	}

	// Record rejection in state and terminate without resuming the engine.
	if !approved {
		run.Status = model.RunStatusCanceled
		if out, err := json.Marshal(map[string]any{"approved": false}); err == nil {
			run.Output = out
		}
		fin := time.Now()
		run.FinishedAt = &fin
		if err := s.svcs.Deps.Repos.Workflow.UpdateRun(ctx, run); err != nil {
			return nil, errcode.ErrInternal.WithCause(err)
		}
		return run, nil
	}

	// Rebuild state from the initial input and completed step outputs.
	state := map[string]any{}
	_ = json.Unmarshal(run.Input, &state)
	for _, st := range run.Steps {
		if st.Status == model.RunStatusSucceeded && len(st.Output) > 0 {
			var v any
			if json.Unmarshal(st.Output, &v) == nil {
				state[st.NodeKey] = v
			}
		}
	}
	// Expose the approval decision to subsequent nodes.
	state["approved"] = approved

	res, steps, err := s.engine.Resume(ctx, dsl, state)
	if err != nil {
		return nil, errcode.From(err)
	}
	for key := range steps {
		st := steps[key]
		st.RunID = run.ID
		_ = s.svcs.Deps.Repos.Workflow.UpsertStepRun(ctx, &st)
	}

	run.Status = res.Status
	if out, err := json.Marshal(res.Output); err == nil {
		run.Output = out
	}
	fin := time.Now()
	run.FinishedAt = &fin
	if err := s.svcs.Deps.Repos.Workflow.UpdateRun(ctx, run); err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}
	return run, nil
}

// ListRuns returns runs whose parent workflow belongs to the caller, optionally filtered by workflow ID.
func (s *WorkflowService) ListRuns(ctx context.Context, userID, workflowID int64, q pagination.Query) ([]model.WorkflowRun, int64, error) {
	return s.svcs.Deps.Repos.Workflow.ListRunsForCreator(ctx, userID, workflowID, q.Offset(), q.Limit())
}

// SubflowRunner supplies the engine callback for nested workflow execution.
// The caller identity must be present in context and own the nested definition.
// This callback returns the nested output without creating a separate persisted
// WorkflowRun record for that nested call.
func (s *WorkflowService) SubflowRunner() workflow.SubRunner {
	return func(ctx context.Context, workflowID int64, input map[string]any) (map[string]any, error) {
		userID, ok := security.UserIDFromContext(ctx)
		if !ok {
			return nil, errcode.ErrForbidden
		}
		w, err := s.svcs.Deps.Repos.Workflow.GetWorkflowForCreator(ctx, workflowID, userID)
		if err != nil {
			return nil, errcode.ErrRunNotFound.WithMsg("Subworkflow %d not found", workflowID).WithCause(err)
		}
		dsl, err := workflow.ParseDSL(w.DSL)
		if err != nil {
			return nil, err
		}
		res, _, err := s.engine.Run(ctx, dsl, input)
		if err != nil {
			return nil, err
		}
		if res.Status == model.RunStatusFailed {
			return nil, errcode.ErrWorkflowDSL.WithMsg("Subworkflow failed: %s", res.Error)
		}
		return res.Output, nil
	}
}

// GetWorkflowLookup provides an ownership-filtered KB callback using the authenticated execution context.
func (s *WorkflowService) GetWorkflowLookup() func(ctx context.Context, kbID int64) (*model.KnowledgeBase, error) {
	return func(ctx context.Context, kbID int64) (*model.KnowledgeBase, error) {
		userID, ok := security.UserIDFromContext(ctx)
		if !ok {
			return nil, errcode.ErrForbidden
		}
		return s.svcs.Deps.Repos.KB.GetKBForUser(ctx, kbID, userID)
	}
}

// GetWorkflowRun returns an owned run with preloaded step snapshots and normalizes storage errors.
func (s *WorkflowService) GetWorkflowRun(ctx context.Context, userID, runID int64) (*model.WorkflowRun, error) {
	run, err := s.svcs.Deps.Repos.Workflow.GetRunForCreator(ctx, runID, userID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.ErrRunNotFound
		}
		return nil, errcode.ErrInternal.WithCause(err)
	}
	return run, nil
}

// mustJSON serializes JSON-compatible execution state and leaves marshaling errors unchecked by the caller.
func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// nowPtr allocates a current timestamp for nullable run lifecycle fields.
func nowPtr() *time.Time {
	t := time.Now()
	return &t
}
