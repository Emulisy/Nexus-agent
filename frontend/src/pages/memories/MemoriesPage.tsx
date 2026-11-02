/**
 * Paged long-term memory management with archive, delete, and manual
 * extraction actions. Mutations refresh server state; deleting the only row on
 * a later page moves back one page. Extraction accepts a session ID and reloads
 * the first result page after the server completes the request.
 */
import '@/styles/management.css';
import { useCallback, useEffect, useRef, useState } from 'react';
import { clsx } from 'clsx';
import {
  Badge,
  Button,
  EmptyState,
  Input,
  PageHeader,
  Spinner,
} from '@/components/ui/primitives';
import { Modal } from '@/components/ui/Modal';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { archiveMemory, deleteMemory, extractMemory, listMemories } from '@/api/memory';
import type { Memory } from '@/types/memory';
import { fullTime, relativeTime } from '@/lib/format';
import { errMsg } from '@/lib/request';
import { toast } from '@/stores/ui';
import { staggerEnter } from '@/lib/gsap';
import { Icon } from '@/components/ui/Icon';

const PAGE_SIZE = 20;

const SCOPE_BADGE: Record<Memory['scope'], { text: string; tone: 'teal' | 'violet' }> = {
  user: { text: 'User', tone: 'teal' },
  agent: { text: 'Agent', tone: 'violet' },
};

/**
 * Map the 1-10 importance value to five bounded segments while keeping
 * the original numeric score visible beside the visual indicator.
 */
function ImportanceBar({ value }: { value: number }) {
  const filled = Math.min(5, Math.max(0, Math.ceil(value / 2)));
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="flex gap-0.5">
        {[1, 2, 3, 4, 5].map((i) => (
          <span
            key={i}
            className={clsx('h-1.5 w-2.5 rounded-sm', i <= filled ? 'bg-lime/70' : 'bg-smoke')}
          />
        ))}
      </span>
      <span className="text-xs text-ash">{value}</span>
    </span>
  );
}

export function MemoriesPage() {
  const [list, setList] = useState<Memory[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  /** Extraction dialog state is separate from paged list requests. */
  const [extractOpen, setExtractOpen] = useState(false);
  const [sessionId, setSessionId] = useState('');
  const [extracting, setExtracting] = useState(false);
  /** Only the targeted archive action needs row-specific pending feedback. */
  const [archivingId, setArchivingId] = useState<number | null>(null);
  const [delTarget, setDelTarget] = useState<Memory | null>(null);
  const tbodyRef = useRef<HTMLTableSectionElement>(null);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  /**
   * Fetch one explicit page and total count. The page state remains separate
   * so actions can refresh the current page or request the first page of results.
   */
  const load = useCallback(async (p: number) => {
    setLoading(true);
    try {
      const data = await listMemories({ page: p, page_size: PAGE_SIZE });
      setList(data.list);
      setTotal(data.total);
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(page);
  }, [load, page]);

  /**
   * Animate rows after a refreshed result set mounts; stop the previous tween
   * before a later result set replaces those elements.
   */
  useEffect(() => {
    const els = tbodyRef.current ? Array.from(tbodyRef.current.children) : [];
    if (els.length === 0) return;
    const tween = staggerEnter(els);
    return () => {
      tween.kill();
    };
  }, [list]);

  /**
   * Track the target row while the server archives it, then reload the page
   * to reflect its persisted lifecycle state.
   */
  const doArchive = async (m: Memory) => {
    setArchivingId(m.id);
    try {
      await archiveMemory(m.id);
      toast.ok('Memory archived');
      await load(page);
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setArchivingId(null);
    }
  };

  /**
   * After deletion, move back when the last row on a later page disappears.
   * Otherwise reload the current page to update its count and ordering.
   */
  const confirmDelete = async () => {
    if (!delTarget) return;
    try {
      await deleteMemory(delTarget.id);
      toast.ok('Memory deleted');
      setDelTarget(null);
      if (list.length === 1 && page > 1) {
        setPage(page - 1);
      } else {
        await load(page);
      }
    } catch (e) {
      toast.err(errMsg(e));
    }
  };

  /**
   * Require a session ID before asking the backend to extract long-term memory.
   * Close/reset the extraction dialog on success and load the first result page.
   */
  const doExtract = async () => {
    const sid = sessionId.trim();
    if (!sid) {
      toast.err('Please enter a session ID');
      return;
    }
    setExtracting(true);
    try {
      await extractMemory({ session_id: sid });
      toast.ok('Memory extraction started');
      setExtractOpen(false);
      setSessionId('');
      await load(1);
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setExtracting(false);
    }
  };

  return (
    <div className="page-container management-page">
      <PageHeader
        title="Long-term memory"
        desc="Review memories retained across conversations. Archive or delete them as needed."
        action={<Button onClick={() => setExtractOpen(true)}>Extract conversation memories</Button>}
      />

      {loading ? (
        <div className="flex justify-center py-20">
          <Spinner className="h-6 w-6 text-lime" />
        </div>
      ) : list.length === 0 ? (
        <EmptyState
          icon={<Icon name="memory" className="h-6 w-6" />}
          title="No memories yet"
          hint="Memories from agent conversations appear here. You can also extract them manually."
        />
      ) : (
        <>
          <div className="panel overflow-hidden">
            <div className="overflow-x-auto">
              <table className="management-table w-full min-w-[880px] text-left text-sm">
                <thead>
                  <tr className="border-b border-smoke bg-graphite/70 text-xs tracking-wide text-fog">
                    <th className="px-4 py-3 font-medium">Memory</th>
                    <th className="px-4 py-3 font-medium">Scope</th>
                    <th className="px-4 py-3 font-medium">Importance</th>
                    <th className="px-4 py-3 font-medium">Recalls</th>
                    <th className="px-4 py-3 font-medium">Last recalled</th>
                    <th className="px-4 py-3 font-medium">Status</th>
                    <th className="px-4 py-3 font-medium">Created</th>
                    <th className="px-4 py-3 text-right font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody ref={tbodyRef}>
                  {list.map((m) => (
                    <tr
                      key={m.id}
                      className="border-b border-smoke/60 align-top transition-colors last:border-0 hover:bg-graphite/60"
                    >
                      <td className="max-w-md whitespace-pre-wrap break-words px-4 py-3 text-mist">
                        {m.content}
                      </td>
                      <td className="px-4 py-3">
                        <Badge tone={SCOPE_BADGE[m.scope].tone}>
                          {SCOPE_BADGE[m.scope].text}
                        </Badge>
                      </td>
                      <td className="px-4 py-3">
                        <ImportanceBar value={m.importance} />
                      </td>
                      <td className="px-4 py-3 text-xs text-fog">{m.hit_count}</td>
                      <td className="px-4 py-3 text-xs text-fog">
                        {relativeTime(m.last_hit_at) || '—'}
                      </td>
                      <td className="px-4 py-3">
                        {m.status === 1 ? (
                          <Badge tone="pulse">Active</Badge>
                        ) : (
                          <Badge tone="gray">Archived</Badge>
                        )}
                      </td>
                      <td className="px-4 py-3 text-xs text-fog">{fullTime(m.created_at)}</td>
                      <td className="px-4 py-3">
                        <div className="flex justify-end gap-2">
                          {m.status === 1 && (
                            <Button
                              variant="ghost"
                              className="px-2.5 py-1 text-xs"
                              loading={archivingId === m.id}
                              onClick={() => void doArchive(m)}
                            >
                              Archive
                            </Button>
                          )}
                          <Button
                            variant="danger"
                            className="px-2.5 py-1 text-xs"
                            onClick={() => setDelTarget(m)}
                          >
                            Delete
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          <div className="mt-5 flex flex-wrap items-center justify-between gap-3 text-xs text-ash">
            <span>{total} {total === 1 ? 'memory' : 'memories'}</span>
            <div className="flex items-center gap-3">
              <Button
                variant="ghost"
                className="px-3 py-1 text-xs"
                disabled={page <= 1 || loading}
                onClick={() => setPage((p) => p - 1)}
              >
                Previous
              </Button>
              <span>
                Page {page} of {totalPages}
              </span>
              <Button
                variant="ghost"
                className="px-3 py-1 text-xs"
                disabled={page >= totalPages || loading}
                onClick={() => setPage((p) => p + 1)}
              >
                Next
              </Button>
            </div>
          </div>
        </>
      )}

      <Modal
        open={extractOpen}
        title="Extract conversation memories"
        onClose={() => setExtractOpen(false)}
        width="max-w-md"
        footer={
          <>
            <Button variant="ghost" onClick={() => setExtractOpen(false)}>
              Cancel
            </Button>
            <Button loading={extracting} onClick={() => void doExtract()}>
              Extract memories
            </Button>
          </>
        }
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void doExtract();
          }}
        >
          <label className="field-label">Conversation ID *</label>
          <Input
            value={sessionId}
            onChange={(e) => setSessionId(e.target.value)}
            placeholder="Enter the conversation ID to extract memories from"
          />
          <p className="mt-1 text-xs text-ash">
            Long-term memories will be extracted from this conversation and appear here shortly.
          </p>
        </form>
      </Modal>

      <ConfirmDialog
        open={delTarget !== null}
        title="Delete memory"
        content="Delete this memory? This cannot be undone."
        danger
        confirmText="Delete"
        onCancel={() => setDelTarget(null)}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  );
}
