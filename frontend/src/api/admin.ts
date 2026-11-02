/**
 * Admin API bindings for model configuration and external integrations.
 * All calls use the shared JSON client for credentials, response envelopes, and
 * 401 recovery. Resource discovery, secret encryption, and authorization are
 * performed by the backend; callers handle form validation and list refreshes.
 */
import { http } from '@/lib/request';
import type {
  A2AAgent,
  MCPServer,
  ModelConfig,
  ModelProvider,
} from '@/types/admin';

/** List configured providers, including masked credential summaries. */
export const listProviders = () =>
  http.get<ModelProvider[]>('/admin/models/providers');

/** Create a provider; api_key is a write-only credential supplied by the editor. */
export const createProvider = (req: Partial<ModelProvider> & { api_key?: string }) =>
  http.post<ModelProvider>('/admin/models/providers', req);

/** Update provider fields. Omitting api_key preserves the stored credential. */
export const updateProvider = (
  id: number,
  req: Partial<ModelProvider> & { api_key?: string },
) => http.put<ModelProvider>(`/admin/models/providers/${id}`, req);

/** Delete the provider by ID; dependent-resource checks belong to the backend. */
export const deleteProvider = (id: number) =>
  http.delete(`/admin/models/providers/${id}`);

/** List model configurations, optionally restricted to chat, embedding, or rerank. */
export const listModels = (type?: string) =>
  http.get<ModelConfig[]>('/admin/models', { type });

/** Create a model configuration associated with a provider. */
export const createModel = (req: Partial<ModelConfig>) =>
  http.post<ModelConfig>('/admin/models', req);

/** Apply model configuration fields without changing the provider resource itself. */
export const updateModel = (id: number, req: Partial<ModelConfig>) =>
  http.put<ModelConfig>(`/admin/models/${id}`, req);

/** Delete a model configuration selected in the admin catalog. */
export const deleteModel = (id: number) => http.delete(`/admin/models/${id}`);

/** List MCP server definitions and their most recent discovery/health metadata. */
export const listMCPServers = () => http.get<MCPServer[]>('/admin/mcp/servers');

/** Store an MCP transport definition after the page validates its JSON fields. */
export const createMCPServer = (req: Partial<MCPServer>) =>
  http.post<MCPServer>('/admin/mcp/servers', req);

/** Update an MCP server definition using parsed args, environment, and headers. */
export const updateMCPServer = (id: number, req: Partial<MCPServer>) =>
  http.put<MCPServer>(`/admin/mcp/servers/${id}`, req);

/** Delete the registered MCP server; the page reloads its discovery catalog afterward. */
export const deleteMCPServer = (id: number) =>
  http.delete(`/admin/mcp/servers/${id}`);

/**
 * Discover remote tools and persist the refreshed server cache/tool records.
 * The response reports how many tools were synchronized.
 */
export const syncMCPServer = (id: number) =>
  http.post<{ synced: number }>(`/admin/mcp/servers/${id}/sync`);

/** Run a server-side health probe and return the resulting connection status. */
export const checkMCPServer = (id: number) =>
  http.post<{ status: string }>(`/admin/mcp/servers/${id}/health`);

/**
 * Invoke a discovered tool manually with structured arguments for diagnostics.
 * The returned output is intentionally open JSON because remote tools vary.
 */
export const callMCPTool = (id: number, name: string, args: Record<string, unknown>) =>
  http.post<{ output: unknown }>(`/admin/mcp/servers/${id}/call`, { name, args });

/** List registered remote agents and their cached Agent Cards. */
export const listA2AAgents = () => http.get<A2AAgent[]>('/admin/a2a/agents');

/**
 * Register a reachable remote agent after backend Agent Card discovery.
 * An omitted name lets the backend use the name advertised by the card.
 */
export const registerA2AAgent = (req: { name?: string; base_url: string }) =>
  http.post<A2AAgent>('/admin/a2a/agents', req);

/** Remove a remote agent registration from the local catalog. */
export const deleteA2AAgent = (id: number) =>
  http.delete(`/admin/a2a/agents/${id}`);

/** Probe a remote agent and update its persisted health state. */
export const checkA2AAgent = (id: number) =>
  http.post<{ status: string }>(`/admin/a2a/agents/${id}/health`);

/** Delegate text to a registered remote agent and return its task response. */
export const sendA2ATask = (id: number, text: string) =>
  http.post<Record<string, unknown>>(`/admin/a2a/agents/${id}/tasks`, { text });
