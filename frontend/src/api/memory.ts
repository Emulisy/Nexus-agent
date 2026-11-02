/**
 * Long-term memory list, archive, deletion, and extraction operations.
 * The page reloads its server-backed list after mutations instead of trying to
 * predict extraction results or memory-ranking changes locally.
 */
import { http } from '@/lib/request';
import type { Paged, PageQuery } from '@/types/api';
import type { ExtractMemoryReq, Memory } from '@/types/memory';

/** Fetch a page of user-visible long-term memories with scope and lifecycle status. */
export const listMemories = (q?: PageQuery) =>
  http.get<Paged<Memory>>('/memories', q);

/** Permanently remove a selected memory record. */
export const deleteMemory = (id: number) => http.delete(`/memories/${id}`);

/** Mark a memory archived so it is no longer treated as active retrieval context. */
export const archiveMemory = (id: number) =>
  http.post(`/memories/${id}/archive`);

/** Request long-term memory extraction for the specified accessible conversation. */
export const extractMemory = (req: ExtractMemoryReq) =>
  http.post('/memories/extract', req);
