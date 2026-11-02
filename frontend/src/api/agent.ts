/**
 * Agent management and the tool catalog used by the binding editor.
 * Request objects follow the backend Agent payload; server-owned identifiers,
 * timestamps, and preset status are excluded from AgentReq.
 */
import { http } from '@/lib/request';
import type { Paged, PageQuery } from '@/types/api';
import type { Agent, AgentReq, Tool } from '@/types/agent';

/** Fetch a paged catalog visible to the authenticated user, including accessible presets. */
export const listAgents = (q?: PageQuery) =>
  http.get<Paged<Agent>>('/agents', q);

/** Create an Agent from editor fields; the backend supplies resource metadata. */
export const createAgent = (req: AgentReq) =>
  http.post<Agent>('/agents', req);

/** Submit editable Agent fields for an existing resource. */
export const updateAgent = (id: number, req: AgentReq) =>
  http.put<Agent>(`/agents/${id}`, req);

/** Delete an owned Agent. Preset restrictions are enforced by the backend. */
export const deleteAgent = (id: number) => http.delete(`/agents/${id}`);

/** Fetch the tool catalog used to resolve and display Agent bindings. */
export const listTools = (q?: PageQuery) =>
  http.get<Paged<Tool>>('/tools', q);
