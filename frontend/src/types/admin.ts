/**
 * Administrative resource payloads for model providers, models, MCP servers,
 * and remote A2A agents. These endpoints require the admin role. Credential
 * responses contain masked values, while remote discovery payloads remain open
 * JSON structures so the UI can tolerate missing or additional fields.
 */
/**
 * Provider configuration with public metadata and a masked API-key summary.
 * Plaintext credentials are accepted only on write requests, not this response.
 */
export interface ModelProvider {
  id: number;
  /** Stable provider code used when the backend selects an adapter. */
  code: string;
  name: string;
  base_url: string;
  /** Empty when no key is configured; otherwise a masked value. */
  api_key_masked: string;
  status: number;
  created_at: string;
  updated_at: string;
}

/** Backend model categories used by configuration forms and filters. */
export type ModelType = 'chat' | 'embedding' | 'rerank';

/**
 * A provider-bound model configuration with limits, prices, capability metadata,
 * and an optional provider projection used by the management table.
 */
export interface ModelConfig {
  id: number;
  provider_id: number;
  /** Upstream model identifier sent in provider requests. */
  model_name: string;
  /** Optional display alias used ahead of the upstream name in selectors. */
  alias: string;
  type: ModelType;
  context_window: number;
  max_output: number;
  /** CNY per million tokens. */
  input_price: number;
  /** Output-token price in CNY per million tokens. */
  output_price: number;
  /** Lower values sort ahead of other configurations during backend selection. */
  priority: number;
  /** Provider capability labels such as vision, function_call, and reasoning. */
  capabilities: string[];
  is_default: boolean;
  status: number;
  created_at: string;
  updated_at: string;
  provider?: ModelProvider;
}

/**
 * MCP transport settings plus cached discovery and health information.
 * URL is used by remote transports; command/args/env configure stdio transports.
 */
export interface MCPServer {
  id: number;
  name: string;
  /** Transport identifier: streamable_http, stdio, or sse. */
  transport: string;
  url: string;
  command: string;
  args: string[];
  env: Record<string, string>;
  headers: Record<string, string>;
  status: string;
  last_health_at?: string | null;
  /** Discovered remote tool definitions; element shapes are checked before display. */
  tools_cache: unknown[];
  created_at: string;
  updated_at: string;
}

/** Remote agent registration whose card is the last discovered Agent Card snapshot. */
export interface A2AAgent {
  id: number;
  name: string;
  base_url: string;
  /** Cached discovery response from the remote Agent Card endpoint. */
  card: Record<string, unknown> | null;
  status: string;
  last_health_at?: string | null;
  created_at: string;
  updated_at: string;
}
