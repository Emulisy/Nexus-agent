/**
 * Knowledge base, document ingestion, and chunk payloads.
 * Uploading a document queues ingestion; the returned document may still be
 * pending. Document status and error fields describe the asynchronous pipeline,
 * while chunk metadata remains parser-specific JSON.
 */
/** Document ingestion stages reported asynchronously by the worker pipeline. */
export type DocStatus =
  | 'pending'
  | 'parsing'
  | 'chunking'
  | 'embedding'
  | 'ready'
  | 'failed';

/** Knowledge base metadata, indexing settings, document count, and vector-store identity. */
export interface KnowledgeBase {
  id: number;
  name: string;
  description: string;
  user_id: number;
  /** Embedding model configuration used when indexing this knowledge base. */
  embedding_config_id: number;
  /** Target chunk length supplied to the backend splitter. */
  chunk_size: number;
  /** Overlap retained between neighboring chunks. */
  chunk_overlap: number;
  /** Parser-specific options stored as open JSON. */
  parser_config: Record<string, unknown>;
  doc_count: number;
  milvus_collection: string;
  status: string;
  created_at: string;
  updated_at: string;
}

/**
 * Fields accepted by the create dialog; server-generated ownership and indexing metadata
 * are intentionally absent from this request shape.
 */
export type KBReq = Partial<
  Pick<KnowledgeBase, 'name' | 'description' | 'chunk_size' | 'chunk_overlap' | 'embedding_config_id'>
>;

/**
 * Uploaded document metadata and ingestion progress. A failed record keeps its error
 * message so the list can explain why indexing did not complete.
 */
export interface Document {
  id: number;
  kb_id: number;
  filename: string;
  file_type: string;
  /** Object-store key for the original upload. */
  object_key: string;
  /** Original upload size in bytes. */
  file_size: number;
  source_url: string;
  status: DocStatus;
  /** Ingestion failure text; empty for a successful or still-pending document. */
  error: string;
  chunk_count: number;
  token_count: number;
  enable_graph: boolean;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

/**
 * Persisted text chunk used by the inspector and retrieval engine. Parser metadata
 * is open JSON and may contain a page number or heading.
 */
export interface DocumentChunk {
  id: number;
  kb_id: number;
  document_id: number;
  seq: number;
  content: string;
  token_count: number;
  /** Parser metadata such as page and heading; callers must check value types. */
  meta: Record<string, unknown>;
  vector_id: string;
  status: number;
  created_at: string;
}
