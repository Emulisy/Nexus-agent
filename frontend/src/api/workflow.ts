/**
 * Workflow definition and execution API bindings.
 * Validation and execution are distinct requests. Runs may finish immediately or
 * return waiting_approval, in which case the run-detail/approval endpoints resume
 * or reject the paused execution.
 */
import { http } from '@/lib/request';
import type { Paged, PageQuery } from '@/types/api';
import type { Workflow, WorkflowRun } from '@/types/workflow';

/** Fetch paged workflow definitions for the current user. */
export const listWorkflows = (q?: PageQuery) =>
  http.get<Paged<Workflow>>('/workflows', q);

/** Create a definition from its name, description, and YAML DSL. */
export const createWorkflow = (req: Partial<Workflow>) =>
  http.post<Workflow>('/workflows', req);

/** Save edited definition fields; the backend validates DSL and manages version changes. */
export const updateWorkflow = (id: number, req: Partial<Workflow>) =>
  http.put<Workflow>(`/workflows/${id}`, req);

/** Delete an accessible workflow definition by ID. */
export const deleteWorkflow = (id: number) => http.delete(`/workflows/${id}`);

/**
 * Validate YAML structure and node references without saving the definition.
 * Invalid DSL is reported through ApiError rather than a false success payload.
 */
export const validateWorkflow = (dsl: string) =>
  http.post<{ valid: boolean }>('/workflows/validate', { dsl });

/**
 * Execute with structured input and return the persisted run snapshot.
 * The status may be succeeded, failed, or waiting_approval.
 */
export const runWorkflow = (workflowId: number, input: Record<string, unknown>) =>
  http.post<WorkflowRun>('/workflows/run', {
    workflow_id: workflowId,
    input,
  });

/** Fetch execution history, optionally restricted to a workflow ID. */
export const listRuns = (q?: PageQuery & { workflow_id?: number }) =>
  http.get<Paged<WorkflowRun>>('/workflows/runs', q);

/** Load a run with its persisted step snapshots for the expanded detail view. */
export const getRun = (id: number) => http.get<WorkflowRun>(`/workflows/runs/${id}`);

/**
 * Submit the decision for a paused run; approval resumes execution, rejection cancels it.
 * The optional comment is included in the payload; state handling belongs to the server.
 */
export const approveRun = (id: number, approved: boolean, comment?: string) =>
  http.post<WorkflowRun>(`/workflows/runs/${id}/approve`, {
    approved,
    comment: comment ?? '',
  });
