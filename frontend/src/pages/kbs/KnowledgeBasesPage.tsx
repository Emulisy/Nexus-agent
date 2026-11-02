/**
 * Knowledge base catalog with creation defaults and confirmed deletion.
 * The list loads up to 100 records, and card navigation opens document/retrieval
 * management. New-form state is reset on every open so canceled values do not
 * leak into a later creation request.
 */
import '@/styles/management.css';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Button,
  Card,
  EmptyState,
  Input,
  PageHeader,
  Spinner,
  StatusBadge,
  Textarea,
} from '@/components/ui/primitives';
import { Modal } from '@/components/ui/Modal';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { createKB, deleteKB, listKBs } from '@/api/kb';
import { relativeTime } from '@/lib/format';
import { gsap, staggerEnter } from '@/lib/gsap';
import { errMsg } from '@/lib/request';
import { toast } from '@/stores/ui';
import type { KnowledgeBase } from '@/types/kb';
import { Icon } from '@/components/ui/Icon';

export function KnowledgeBasesPage() {
  const navigate = useNavigate();

  const [kbs, setKbs] = useState<KnowledgeBase[]>([]);
  const [loading, setLoading] = useState(true);

  /** Creation draft values remain local until the user submits. */
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [chunkSize, setChunkSize] = useState('512');
  const [chunkOverlap, setChunkOverlap] = useState('64');
  const [creating, setCreating] = useState(false);

  /** Deletion target and pending state belong to the confirmation flow. */
  const [delKB, setDelKB] = useState<KnowledgeBase | null>(null);
  const [deleting, setDeleting] = useState(false);

  const gridRef = useRef<HTMLDivElement>(null);

  /** Reload the first 100 knowledge bases used by the card catalog. */
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listKBs({ page: 1, page_size: 100 });
      setKbs(data.list);
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  /**
   * Wait for cards to mount before selecting children. Reverting the GSAP
   * context cleans animation state when results change or the page unmounts.
   */
  useEffect(() => {
    if (loading || kbs.length === 0) return;
    const grid = gridRef.current;
    if (!grid) return;
    const ctx = gsap.context(() => staggerEnter(Array.from(grid.children)));
    return () => ctx.revert();
  }, [loading, kbs]);

  /**
   * Reset all draft fields, including chunking defaults, whenever a fresh
   * creation dialog opens.
   */
  const openCreate = () => {
    setName('');
    setDescription('');
    setChunkSize('512');
    setChunkOverlap('64');
    setCreateOpen(true);
  };

  /**
   * Require a nonempty name and convert chunking drafts to numeric values.
   * Falsy numeric conversions use the existing defaults before submission.
   */
  const submitCreate = async () => {
    const n = name.trim();
    if (!n) {
      toast.err('Please enter a knowledge base name');
      return;
    }
    setCreating(true);
    try {
      await createKB({
        name: n,
        description: description.trim(),
        chunk_size: Number(chunkSize) || 512,
        chunk_overlap: Number(chunkOverlap) || 64,
      });
      toast.ok('Knowledge base created');
      setCreateOpen(false);
      await load();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setCreating(false);
    }
  };

  /**
   * Delete from the confirmation dialog, retain pending state for repeated
   * click protection, and reload the catalog after the server accepts the action.
   */
  const confirmDelete = async () => {
    if (!delKB) return;
    setDeleting(true);
    try {
      await deleteKB(delKB.id);
      toast.ok('Knowledge base deleted');
      setDelKB(null);
      await load();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="page-container management-page">
      <PageHeader
        title="Knowledge bases"
        desc="Upload documents to build knowledge bases that agents can search and cite."
        action={<Button onClick={openCreate}>＋ New knowledge base</Button>}
      />

      {loading ? (
        <div className="flex justify-center py-20">
          <Spinner className="h-6 w-6 text-lime" />
        </div>
      ) : kbs.length === 0 ? (
        <EmptyState
          icon={<Icon name="knowledge" className="h-6 w-6" />}
          title="No knowledge bases yet"
          hint="Create a knowledge base and upload documents to use them in conversations."
          action={<Button onClick={openCreate}>New knowledge base</Button>}
        />
      ) : (
        <div ref={gridRef} className="grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
          {kbs.map((kb) => (
            <Card
              key={kb.id}
              className="management-card group relative"
              onClick={() => navigate(`/kbs/${kb.id}`)}
            >
              <div className="management-icon-well"><Icon name="knowledge" className="h-5 w-5" /></div>
              <div className="mb-2 flex items-start justify-between gap-2">
                <h3 className="min-w-0 truncate text-base font-semibold text-bone" title={kb.name}>
                  {kb.name}
                </h3>
                <div className="flex shrink-0 items-center gap-1.5">
                  <StatusBadge status={kb.status} />
                  <button
                    className="management-kb-delete text-xs text-ash transition-all hover:bg-coral/5 hover:text-coral"
                    title="Delete knowledge base"
                    onClick={(e) => {
                      e.stopPropagation();
                      setDelKB(kb);
                    }}
                  >
                    ✕
                  </button>
                </div>
              </div>

              <p className="mb-3 line-clamp-2 min-h-[2rem] text-xs leading-4 text-fog">
                {kb.description || 'No description'}
              </p>

              <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-smoke/60 pt-4 text-[11px] text-ash">
                <span className="text-lime">{kb.doc_count} {kb.doc_count === 1 ? 'document' : 'documents'}</span>
                <span>chunk_size {kb.chunk_size}</span>
                <span>overlap {kb.chunk_overlap}</span>
                <span className="ml-auto">{relativeTime(kb.updated_at)}</span>
              </div>
            </Card>
          ))}
        </div>
      )}

      <Modal
        open={createOpen}
        title="New knowledge base"
        onClose={() => setCreateOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setCreateOpen(false)}>
              Cancel
            </Button>
            <Button loading={creating} onClick={() => void submitCreate()}>
              Create
            </Button>
          </>
        }
      >
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            void submitCreate();
          }}
        >
          <div>
            <label className="field-label">Name *</label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Example: Product documentation"
              autoFocus
            />
          </div>
          <div>
            <label className="field-label">Description</label>
            <Textarea
              rows={3}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Describe the purpose of this knowledge base (optional)"
            />
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label className="field-label">Chunk size</label>
              <Input
                type="number"
                min={64}
                value={chunkSize}
                onChange={(e) => setChunkSize(e.target.value)}
              />
            </div>
            <div>
              <label className="field-label">Chunk overlap</label>
              <Input
                type="number"
                min={0}
                value={chunkOverlap}
                onChange={(e) => setChunkOverlap(e.target.value)}
              />
            </div>
          </div>
        </form>
      </Modal>

      <ConfirmDialog
        open={delKB !== null}
        title="Delete knowledge base"
        content={delKB ? `Delete “${delKB.name}” and all its documents and vectors? This cannot be undone.` : ''}
        danger
        confirmText="Delete"
        loading={deleting}
        onCancel={() => setDelKB(null)}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  );
}
