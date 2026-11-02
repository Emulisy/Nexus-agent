/**
 * Knowledge base, document, chunk, and retrieval API bindings.
 * Document upload uses multipart data; all other operations use JSON. Ingestion
 * runs asynchronously, so upload completion is distinct from retrieval readiness.
 */
import { http } from '@/lib/request';
import type { Paged, PageQuery } from '@/types/api';
import type { RAGRef } from '@/types/chat';
import type { Document, DocumentChunk, KBReq, KnowledgeBase } from '@/types/kb';

/** Fetch a page of knowledge bases accessible to the current user. */
export const listKBs = (q?: PageQuery) => http.get<Paged<KnowledgeBase>>('/kbs', q);

/** Load metadata and chunking parameters for one knowledge base. */
export const getKB = (id: number) => http.get<KnowledgeBase>(`/kbs/${id}`);

/** Create a knowledge base; omitted embedding/chunking options use backend defaults. */
export const createKB = (req: KBReq) => http.post<KnowledgeBase>('/kbs', req);

/** Delete a knowledge base and let the backend clean up its dependent resources. */
export const deleteKB = (id: number) => http.delete(`/kbs/${id}`);

/**
 * Send the selected file under the multipart field name expected by the handler.
 * The returned document represents a queued ingestion job, not completed indexing.
 */
export const uploadDoc = (kbId: number, file: File) => {
  const fd = new FormData();
  fd.append('file', file);
  return http.upload<Document>(`/kbs/${kbId}/documents`, fd);
};

/** Fetch paged document records so the UI can display asynchronous ingestion status. */
export const listDocs = (kbId: number, q?: PageQuery) =>
  http.get<Paged<Document>>(`/kbs/${kbId}/documents`, q);

/** Delete a document through its knowledge base, including indexed chunk/vector cleanup. */
export const deleteDoc = (kbId: number, docId: number) =>
  http.delete(`/kbs/${kbId}/documents/${docId}`);

/** Run retrieval against this knowledge base without starting a chat turn. */
export const testRetrieve = (kbId: number, query: string) =>
  http.post<RAGRef[]>(`/kbs/${kbId}/retrieve`, { query });

/** Inspect a page of persisted chunks for one document; content and metadata are returned. */
export const listChunks = (docId: number, q?: PageQuery) =>
  http.get<Paged<DocumentChunk>>(`/documents/${docId}/chunks`, q);
