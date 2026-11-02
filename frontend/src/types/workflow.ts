/**
 * Persisted workflow definitions, execution records, and step snapshots.
 * Definitions retain DSL as YAML text; the server parses and validates it.
 * A run can pause for approval, and its optional steps are loaded with detailed
 * run responses rather than required on every list item.
 */
/**
 * Saved workflow definition. DSL remains YAML text in the editor; validation and
 * version assignment happen on the backend.
 */
export interface Workflow {
  id: number;
  name: string;
  description: string;
  /** YAML source retained verbatim for editing and server validation. */
  dsl: string;
  version: number;
  is_active: boolean;
  creator_id: number;
  status: number;
  created_at: string;
  updated_at: string;
}

/** Lifecycle states used by history badges and approval controls. */
export type RunStatus =
  | 'running'
  | 'waiting_approval'
  | 'succeeded'
  | 'failed'
  | 'canceled';

/**
 * Snapshot of a single node execution, including input/output, errors, and timing.
 * Missing timestamps represent steps that have not started or finished yet.
 */
export interface WorkflowStepRun {
  id: number;
  run_id: number;
  node_key: string;
  /** DSL node kind: llm, tool, kb, condition, parallel, human, or subflow. */
  node_type: string;
  status: string;
  /** Node-local input snapshot; unavailable on some partial records. */
  input?: Record<string, unknown>;
  /** Node-local output snapshot used by detail views and resumed execution. */
  output?: Record<string, unknown>;
  error: string;
  tokens: number;
  started_at?: string | null;
  finished_at?: string | null;
}

/**
 * Persisted execution snapshot. List responses may omit steps; the detail endpoint
 * adds them for the expanded timeline and approval view.
 */
export interface WorkflowRun {
  id: number;
  workflow_id: number;
  trigger_type: string;
  status: RunStatus;
  input: Record<string, unknown>;
  output?: Record<string, unknown>;
  error: string;
  /** Correlation identifier for backend logs and tracing. */
  started_at?: string | null;
  finished_at?: string | null;
  created_at: string;
  /** Detailed node snapshots loaded on demand by the execution history page. */
  steps?: WorkflowStepRun[];
}
