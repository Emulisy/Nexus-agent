/**
 * Agent configuration and tool metadata exchanged with the backend.
 * Hosts route requests to experts; experts execute a configured model/tool loop.
 * Tool bindings are stored by ID with optional per-binding configuration.
 * Flags describe server metadata and do not themselves enforce execution policy.
 */
/**
 * Persisted Agent settings, model choices, memory options, and resource bindings.
 * A zero model_config_id selects the platform default on the backend.
 */
export interface Agent {
  id: number;
  name: string;
  description: string;
  /** Host selects experts; expert executes a task directly. */
  type: 'host' | 'expert';
  system_prompt: string;
  model_config_id: number;
  /** Optional fallback configuration; null or omission leaves fallback unconfigured. */
  fallback_model_config_id?: number | null;
  temperature: number;
  top_p: number;
  max_tokens: number;
  max_iterations: number;
  memory_enabled: boolean;
  /** Number of recent messages requested for short-term context. */
  memory_window: number;
  /** Controls whether extracted long-term memory is recalled for this Agent. */
  memory_long_enabled: boolean;
  /** Knowledge bases whose accessible chunks may augment conversation context. */
  kb_ids: number[];
  /** Tool IDs and optional binding-specific configuration persisted as JSON. */
  tools: { tool_id: number; config?: Record<string, unknown> }[];
  is_preset: boolean;
  /** Additional backend-specific configuration, including host routing settings. */
  config: Record<string, unknown>;
  status: number;
  created_at: string;
  updated_at: string;
}

/**
 * Writable Agent fields with server-owned identifiers, timestamps, and preset
 * status excluded. Partial typing permits omitted values to follow backend defaults.
 */
export type AgentReq = Partial<
  Omit<Agent, 'id' | 'created_at' | 'updated_at' | 'is_preset'>
>;

/**
 * Tool catalog entry used by Agent editors and runtime definition resolution.
 * Descriptions and JSON Schema parameters are exposed to the model during execution.
 */
export interface Tool {
  id: number;
  code: string;
  name: string;
  type: 'builtin' | 'mcp' | 'custom';
  description: string;
  /** JSON Schema. */
  parameters: Record<string, unknown> | null;
  handler?: string;
  mcp_server_id?: number | null;
  endpoint?: string;
  auth_config?: Record<string, unknown>;
  default_config: Record<string, unknown>;
  /** Catalog timeout override in milliseconds. */
  timeout_ms: number;
  /** Marks a tool as dangerous; enforcement requires an execution-layer approval policy. */
  is_dangerous: boolean;
  status: number;
  created_at: string;
  updated_at: string;
}
