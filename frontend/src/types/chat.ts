/**
 * Conversation records and the SSE payloads consumed by the chat store.
 * Persisted messages carry server IDs, usage, references, and optional trace
 * metadata. Streaming payloads are assembled independently, then replaced by
 * server history after completion, failure, or cancellation.
 */
/**
 * Session summary. IDs are server-issued UUIDs, and optional agent_id describes
 * the stored association rather than the current frontend selector value.
 */
export interface Session {
  id: string;
  user_id: number;
  agent_id?: number | null;
  title: string;
  status: 'active' | 'archived';
  pinned: boolean;
  message_count: number;
  token_usage: number;
  last_msg_at?: string | null;
  /** Additional message metadata; agent_trace supplies the persisted decision timeline. */
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

/** Roles stored in conversation history and used when constructing model context. */
export type MessageRole = 'user' | 'assistant' | 'system' | 'tool';

/**
 * Persisted messages[].metadata.agent_trace entry, matching the backend trace collector.
 * Type/status stay open strings so unfamiliar events can still be displayed.
 */
export interface TraceStep {
  seq: number;
  /** Trace category, for example thought, action, observe, delegate, merge, or answer. */
  type: string;
  agent: string;
  content: string;
  tool?: string;
  args?: Record<string, unknown>;
  result?: string;
  elapsed_ms: number;
  status?: string;
  time: string;
}

/** Retrieved chunk citation with source location, excerpt, and similarity score. */
export interface RAGRef {
  chunk_id: number;
  document_id: number;
  filename: string;
  page: number;
  content: string;
  score: number;
}

/**
 * Persisted message record returned by the history endpoint. Optional reasoning,
 * references, and trace metadata allow ordinary and tool-assisted turns to share a shape.
 */
export interface Message {
  id: number;
  session_id: string;
  role: MessageRole;
  content: string;
  reasoning_content?: string;
  tool_calls?: unknown;
  tool_call_id?: string;
  refs?: RAGRef[] | null;
  model_name?: string;
  prompt_tokens: number;
  completion_tokens: number;
  first_token_ms: number;
  latency_ms: number;
  /** 1 = complete, 2 = interrupted, 3 = failed. */
  status: 1 | 2 | 3;
  metadata?: { agent_trace?: TraceStep[] } & Record<string, unknown>;
  created_at: string;
}

/**
 * Event names understood by the chat protocol. Not every event has a visual handler:
 * approval currently falls through the reducer, and agent_event is stored without a view.
 */
export type SSEEventType =
  | 'meta'
  | 'delta'
  | 'reasoning'
  | 'plan'
  | 'tool_call'
  | 'tool_result'
  | 'agent_event'
  | 'refs'
  | 'usage'
  | 'approval'
  | 'done'
  | 'error';

/**
 * Display payload for an incoming plan event. This type does not start a planner
 * or imply that every backend runner emits plans.
 */
export interface PlanData {
  goal: string;
  steps: PlanStep[];
}

/** One displayed plan item with upstream step IDs listed as dependencies. */
export interface PlanStep {
  id: string;
  description: string;
  depends_on: string[];
}

/** Start of a tool call. Its ID joins the later result to the same live timeline node. */
export interface ToolCallData {
  id: string;
  name: string;
  args: string; // JSON-encoded arguments.
}

/** Completion of a tool call, including serialized output and elapsed milliseconds. */
export interface ToolResultData {
  id: string;
  result: string;
  /** Elapsed tool execution time in milliseconds. */
  ms: number;
  /** Tool result status, normally ok, error, or timeout. */
  status: string;
}

/** Remote-agent routing/progress payload retained by the streaming state. */
export interface AgentEventData {
  type: string;
  expert: string;
  detail: string;
}

/** Usage event reported by the backend for the completed turn. Cost is a decimal string. */
export interface UsageData {
  prompt_tokens: number;
  completion_tokens: number;
  cost: string;
  model: string;
}

/**
 * One chat turn: only the new content and selection IDs are submitted.
 * The backend loads history and constructs context from the session.
 */
export interface ChatReq {
  session_id?: string;
  agent_id?: number;
  content: string;
  /** Zero or omission uses the agent's configured model. */
  model_config_id?: number;
}

/** Enabled model option returned by /models for the conversation selector. */
export interface ChatModelItem {
  id: number;
  /** Display label resolved by the backend, preferring an alias when available. */
  label: string;
  model_name: string;
  provider: string;
  is_default: boolean;
}
