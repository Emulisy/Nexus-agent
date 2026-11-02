// Workflow endpoints expose definitions, synchronous runs, and human-step decisions.
// The current user is supplied to ownership-aware service methods; creation
// overrides creator_id with the authenticated account rather than trusting JSON.
package api

import (
	"net/http"

	"github.com/Emulisy/SupaAgent/internal/model"
	"github.com/Emulisy/SupaAgent/internal/service"
)

// ListWorkflows returns definitions created by the authenticated user.
func (h *Handler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, total, err := h.svcs.Workflow.List(ctx, userID(r), parsePagination(r))
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"list": list, "total": total})
}

// GetWorkflow returns an owned definition using the route ID.
func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	workflow, err := h.svcs.Workflow.Get(ctx, userID(r), id)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, workflow)
}

// CreateWorkflow stamps the current user as creator before DSL validation and insertion.
func (h *Handler) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workflow := &model.Workflow{}
	if err := decodeJSON(w, r, workflow); err != nil {
		Fail(w, err)
		return
	}
	workflow.CreatorID = userID(r)
	if err := h.svcs.Workflow.Create(ctx, workflow); err != nil {
		Fail(w, err)
		return
	}
	OK(w, workflow)
}

// UpdateWorkflow converts pointer-valued JSON fields into a partial service update.
func (h *Handler) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		DSL         *string `json:"dsl"`
		IsActive    *bool   `json:"is_active"`
		Status      *int16  `json:"status"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Workflow.Update(ctx, userID(r), id, &service.WorkflowUpdateInput{
		Name: req.Name, Description: req.Description, DSL: req.DSL,
		IsActive: req.IsActive, Status: req.Status,
	}); err != nil {
		Fail(w, err)
		return
	}
	workflow, err := h.svcs.Workflow.Get(ctx, userID(r), id)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, workflow)
}

// DeleteWorkflow requests an owner-filtered definition deletion.
func (h *Handler) DeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Workflow.Delete(ctx, userID(r), id); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// ValidateWorkflow parses and statically validates the supplied DSL without saving it.
func (h *Handler) ValidateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DSL string `json:"dsl"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Workflow.Validate(req.DSL); err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"valid": true})
}

// RunWorkflowReq selects a saved workflow and supplies its initial state object.
type RunWorkflowReq struct {
	WorkflowID int64          `json:"workflow_id"`
	Input      map[string]any `json:"input"`
}

// RunWorkflow waits for execution to return and responds with the persisted run record.
func (h *Handler) RunWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req RunWorkflowReq
	if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}
	run, err := h.svcs.Workflow.Run(ctx, userID(r), &service.RunInput{
		WorkflowID: req.WorkflowID, Input: req.Input,
	})
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, run)
}

// ApproveReq carries an explicit approval decision for a waiting workflow run.
type ApproveReq struct {
	Approved bool   `json:"approved"`
	Comment  string `json:"comment"`
}

// ApproveRun resumes or rejects a waiting owned run using the submitted decision.
func (h *Handler) ApproveRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	var req ApproveReq
	if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}
	run, err := h.svcs.Workflow.Approve(ctx, userID(r), runID, req.Approved)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, run)
}

// ListRuns lists the caller's workflow runs, optionally restricted to one workflow.
func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wfID, _ := parseID(string(r.URL.Query().Get("workflow_id")))
	list, total, err := h.svcs.Workflow.ListRuns(ctx, userID(r), wfID, parsePagination(r))
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"list": list, "total": total})
}

// GetRun returns an owned run with its persisted step snapshots.
func (h *Handler) GetRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	run, err := h.svcs.Workflow.GetWorkflowRun(ctx, userID(r), runID)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, run)
}
