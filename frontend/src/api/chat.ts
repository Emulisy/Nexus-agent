/**
 * REST access to chat models, sessions, and persisted message history.
 * The streaming request itself lives in lib/sse.ts. These endpoints are also
 * used after a stream ends to reconcile optimistic state with stored records.
 */
import { http } from '@/lib/request';
import type { Paged, PageQuery } from '@/types/api';
import type { ChatModelItem, Message, Session } from '@/types/chat';

/** Load enabled chat-model choices for the per-request override selector. */
export const listChatModels = () => http.get<ChatModelItem[]>('/models');

/** Fetch paged session summaries, including titles, counts, and timestamps. */
export const listSessions = (q?: PageQuery) =>
  http.get<Paged<Session>>('/sessions', q);

/** Load persisted message history, including reasoning, references, and agent_trace metadata. */
export const listMessages = (sessionId: string) =>
  http.get<Message[]>(`/sessions/${sessionId}/messages`);

/** Change the session title; the store patches the visible summary after success. */
export const renameSession = (id: string, title: string) =>
  http.put(`/sessions/${id}`, { title });

/** Delete a session and its message history through the backend ownership checks. */
export const deleteSession = (id: string) =>
  http.delete(`/sessions/${id}`);
