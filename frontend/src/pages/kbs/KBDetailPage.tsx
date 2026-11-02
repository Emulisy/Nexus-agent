/**
 * Knowledge base detail, document upload/status, chunk inspection, and
 * retrieval diagnostics. Metadata and document lists load independently. Upload
 * completion queues ingestion rather than waiting for indexing; users can refresh
 * document status. Chunk data loads on demand, while null retrieval results mean
 * no query has been submitted yet.
 */
import '@/styles/management.css';
import { useCallback, useEffect, useRef, useState, type ChangeEvent } from 'react';
import { Link, useParams } from 'react-router-dom';
import {
  Button,
  EmptyState,
  Input,
  PageHeader,
  Spinner,
  StatusBadge,
} from '@/components/ui/primitives';
import { Modal } from '@/components/ui/Modal';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { deleteDoc, getKB, listChunks, listDocs, testRetrieve, uploadDoc } from '@/api/kb';
import { fileSize, relativeTime, truncate } from '@/lib/format';
import { gsap, pageEnter } from '@/lib/gsap';
import { errMsg } from '@/lib/request';
import { toast } from '@/stores/ui';
import type { Document, DocumentChunk, KnowledgeBase } from '@/types/kb';
import type { RAGRef } from '@/types/chat';
import { Icon } from '@/components/ui/Icon';

export function KBDetailPage() {
  const { id } = useParams();
  const kbId = Number(id);
  /**
   * Only reject values that cannot be parsed as a number; ownership/existence
   * and any stricter identifier checks are delegated to the backend.
   */
  const validId = !Number.isNaN(kbId);

  const [kb, setKb] = useState<KnowledgeBase | null>(null);
  const [kbLoading, setKbLoading] = useState(true);

  /** Document loading can refresh independently of the knowledge base header. */
  const [docs, setDocs] = useState<Document[]>([]);
  const [docsLoading, setDocsLoading] = useState(true);
  const [uploading, setUploading] = useState(false);

  const [delDoc, setDelDoc] = useState<Document | null>(null);
  const [deleting, setDeleting] = useState(false);

  /** A selected document opens the inspector; chunk rows load only on demand. */
  const [chunkDoc, setChunkDoc] = useState<Document | null>(null);
  const [chunks, setChunks] = useState<DocumentChunk[]>([]);
  const [chunksLoading, setChunksLoading] = useState(false);

  const [query, setQuery] = useState('');
  const [retrieving, setRetrieving] = useState(false);
  // Null distinguishes an unsubmitted search from an empty result.
  const [refs, setRefs] = useState<RAGRef[] | null>(null);

  const pageRef = useRef<HTMLDivElement>(null);

  /**
   * Refresh document status after uploads/deletions or a manual refresh.
   * The list is bounded to 100 rows; ingestion progress is read from server records.
   */
  const refreshDocs = useCallback(async () => {
    setDocsLoading(true);
    try {
      const data = await listDocs(kbId, { page: 1, page_size: 100 });
      setDocs(data.list);
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setDocsLoading(false);
    }
  }, [kbId]);

  /**
   * Start metadata and document requests independently for a numeric route ID.
   * An unparsable ID reaches the local error view without issuing these requests.
   */
  useEffect(() => {
    if (!validId) return;
    void (async () => {
      setKbLoading(true);
      try {
        setKb(await getKB(kbId));
      } catch (e) {
        toast.err(errMsg(e));
      } finally {
        setKbLoading(false);
      }
    })();
    void refreshDocs();
  }, [kbId, validId, refreshDocs]);

  /** Apply mount-only page motion and release its GSAP context during cleanup. */
  useEffect(() => {
    const el = pageRef.current;
    if (!el) return;
    const ctx = gsap.context(() => pageEnter(el));
    return () => ctx.revert();
  }, []);

  /**
   * Upload the selected file as multipart data, then reload document metadata.
   * The successful upload queues indexing; it does not wait for the worker to finish.
   */
  const onUpload = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = ''; // Allow selecting the same file again.
    if (!file) return;
    setUploading(true);
    try {
      await uploadDoc(kbId, file);
      toast.ok('Uploaded. Document processing is underway.');
      await refreshDocs();
    } catch (err) {
      toast.err(errMsg(err));
    } finally {
      setUploading(false);
    }
  };

  /**
   * Delete through the enclosing knowledge base and refresh the list after
   * the backend removes the document and indexed resources.
   */
  const confirmDeleteDoc = async () => {
    if (!delDoc) return;
    setDeleting(true);
    try {
      await deleteDoc(kbId, delDoc.id);
      toast.ok('Document deleted');
      setDelDoc(null);
      await refreshDocs();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setDeleting(false);
    }
  };

  /**
   * Open the inspector immediately, discard the previous preview, and load
   * the first 50 chunks for the selected document on demand.
   */
  const openChunks = (doc: Document) => {
    setChunkDoc(doc);
    setChunks([]); // Clear the previous document's chunks before loading another.
    setChunksLoading(true);
    listChunks(doc.id, { page: 1, page_size: 50 })
      .then((data) => setChunks(data.list))
      .catch((e: unknown) => toast.err(errMsg(e)))
      .finally(() => setChunksLoading(false));
  };

  /**
   * Treat parser metadata as untrusted JSON: expose a page number only when
   * the field is actually numeric.
   */
  const pageOf = (chunk: DocumentChunk): number | null => {
    const p = chunk.meta?.page;
    return typeof p === 'number' ? p : null;
  };

  /**
   * Run a diagnostic query without adding a chat message. A successful empty
   * array is kept distinct from the initial null state and from request failure.
   */
  const retrieve = async () => {
    const q = query.trim();
    if (!q) {
      toast.err('Please enter a search query');
      return;
    }
    setRetrieving(true);
    try {
      setRefs(await testRetrieve(kbId, q));
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setRetrieving(false);
    }
  };

  if (!validId) {
    return (
      <div className="page-container management-page">
        <EmptyState
          icon={<Icon name="knowledge" className="h-6 w-6" />}
          title="Invalid knowledge base ID"
          hint="Open this knowledge base from the list."
          action={
            <Link to="/kbs" className="btn-ghost">
              Back to list
            </Link>
          }
        />
      </div>
    );
  }

  if (kbLoading && !kb) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner className="h-6 w-6 text-lime" />
      </div>
    );
  }

  return (
    <div ref={pageRef} className="page-container management-page">
      <PageHeader
        title={kb?.name ?? 'Knowledge base details'}
        desc={kb?.description || undefined}
        action={
          <Link to="/kbs" className="btn-ghost">
            ← Back to list
          </Link>
        }
      />

      {kb && (
        <div className="mb-6 flex flex-wrap items-center gap-x-5 gap-y-2 rounded-xl border border-smoke/60 bg-graphite/70 px-4 py-3 text-xs text-ash">
          <span>
            chunk_size <span className="font-mono text-mist">{kb.chunk_size}</span>
          </span>
          <span>
            chunk_overlap <span className="font-mono text-mist">{kb.chunk_overlap}</span>
          </span>
          <span className="text-lime">{kb.doc_count} {kb.doc_count === 1 ? 'document' : 'documents'}</span>
          <span>Updated {relativeTime(kb.updated_at)}</span>
        </div>
      )}

      <section className="panel p-5 sm:p-6">
        <div className="mb-2 flex flex-wrap items-center justify-between gap-3">
          <h2 className="section-title">Documents</h2>
          <div className="flex items-center gap-2">
            <Button variant="ghost" loading={docsLoading} onClick={() => void refreshDocs()}>
              Refresh
            </Button>
            <label className="btn-primary cursor-pointer">
              {uploading && <Spinner className="h-3.5 w-3.5" />}
              {uploading ? 'Uploading…' : 'Upload documents'}
              <input
                type="file"
                accept=".txt,.md,.pdf,.docx"
                className="hidden"
                disabled={uploading}
                onChange={(e) => void onUpload(e)}
              />
            </label>
          </div>
        </div>
        <p className="mb-4 text-xs text-ash">
          Supports .txt, .md, .pdf, and .docx. Processing runs in the background; select Refresh to check progress.
        </p>

        {docsLoading && docs.length === 0 ? (
          <div className="flex justify-center py-10">
            <Spinner />
          </div>
        ) : docs.length === 0 ? (
          <EmptyState
            icon={<Icon name="knowledge" className="h-6 w-6" />}
            title="No documents yet"
            hint="Uploaded documents are automatically parsed, split into chunks, and embedded."
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="management-table w-full min-w-[720px] text-left text-sm">
              <thead>
                <tr className="border-b border-smoke bg-graphite/70 text-xs text-fog">
                  <th className="px-2 py-2 font-medium">File</th>
                  <th className="px-2 py-2 font-medium">Size</th>
                  <th className="px-2 py-2 font-medium">Status</th>
                  <th className="px-2 py-2 font-medium">Chunks</th>
                  <th className="px-2 py-2 font-medium">Tokens</th>
                  <th className="px-2 py-2 font-medium">Updated</th>
                  <th className="px-2 py-2 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {docs.map((doc) => (
                  <tr
                    key={doc.id}
                    className="border-b border-smoke/60 transition-colors last:border-0 hover:bg-graphite/60"
                  >
                    <td className="max-w-xs px-2 py-2.5">
                      <p className="truncate font-medium text-bone" title={doc.filename}>
                        {doc.filename}
                      </p>
                      <p className="text-[11px] text-ash">{doc.file_type}</p>
                      {doc.status === 'failed' && doc.error && (
                        <p className="mt-0.5 truncate text-[11px] text-coral" title={doc.error}>
                          {doc.error}
                        </p>
                      )}
                    </td>
                    <td className="px-2 py-2.5 text-xs text-fog">{fileSize(doc.file_size)}</td>
                    <td className="px-2 py-2.5">
                      <StatusBadge status={doc.status} />
                    </td>
                    <td className="px-2 py-2.5 text-xs text-fog">{doc.chunk_count}</td>
                    <td className="px-2 py-2.5 text-xs text-fog">{doc.token_count}</td>
                    <td className="px-2 py-2.5 text-xs text-fog">{relativeTime(doc.updated_at)}</td>
                    <td className="px-2 py-2.5 text-right">
                      <div className="flex justify-end gap-3 text-xs">
                        <button
                          className="text-teal transition-opacity hover:opacity-80"
                          onClick={() => openChunks(doc)}
                        >
                          View chunks
                        </button>
                        <button
                          className="text-coral transition-opacity hover:opacity-80"
                          onClick={() => setDelDoc(doc)}
                        >
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="panel mt-6 p-5 sm:p-6">
        <h2 className="section-title mb-4">Search playground</h2>
        <div className="flex flex-col gap-3 sm:flex-row">
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Enter a query to test knowledge base search"
            onKeyDown={(e) => e.key === 'Enter' && void retrieve()}
          />
          <Button loading={retrieving} onClick={() => void retrieve()}>
            Search
          </Button>
        </div>

        <div className="mt-4 space-y-2">
          {refs === null ? null : refs.length === 0 ? (
            <EmptyState
              title="No matching content found"
              hint="Try another query or check that document embedding has finished."
            />
          ) : (
            refs.map((ref, i) => (
              <div
                key={`${ref.chunk_id}-${i}`}
                className="rounded-xl border border-smoke/60 bg-graphite/50 p-4"
              >
                <div className="mb-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
                  <span className="font-mono text-lime">{i + 1}</span>
                  <span className="font-medium text-bone" title={ref.filename}>
                    {ref.filename}
                  </span>
                  {ref.page > 0 && <span className="text-ash">Page {ref.page}</span>}
                  <span className="ml-auto font-mono text-teal">{ref.score.toFixed(3)}</span>
                </div>
                <p className="text-xs leading-relaxed text-fog">{truncate(ref.content, 200)}</p>
              </div>
            ))
          )}
        </div>
      </section>

      <Modal
        open={chunkDoc !== null}
        title={chunkDoc ? `Document chunks · ${chunkDoc.filename}` : 'Document chunks'}
        onClose={() => setChunkDoc(null)}
        width="max-w-2xl"
      >
        {chunksLoading ? (
          <div className="flex justify-center py-10">
            <Spinner />
          </div>
        ) : chunks.length === 0 ? (
          <EmptyState title="No chunks available" hint="Document processing may still be in progress." />
        ) : (
          <div className="space-y-2">
            {chunks.map((c) => {
              const page = pageOf(c);
              return (
                <div key={c.id} className="rounded-xl border border-smoke/60 bg-graphite/50 p-4">
                  <div className="mb-1.5 flex items-center gap-3 text-[11px] text-ash">
                    <span className="font-mono text-lime">#{c.seq}</span>
                    {page !== null && <span>Page {page}</span>}
                    <span>{c.token_count} tokens</span>
                  </div>
                  <p className="whitespace-pre-wrap break-words text-xs leading-relaxed text-mist">
                    {c.content}
                  </p>
                </div>
              );
            })}
          </div>
        )}
      </Modal>

      <ConfirmDialog
        open={delDoc !== null}
        title="Delete document"
        content={
          delDoc
            ? `Delete “${delDoc.filename}” and all its chunks and vectors? This cannot be undone.`
            : ''
        }
        danger
        confirmText="Delete"
        loading={deleting}
        onCancel={() => setDelDoc(null)}
        onConfirm={() => void confirmDeleteDoc()}
      />
    </div>
  );
}
