/**
 * Paged workflow execution history with optional workflow-ID filtering,
 * lazy-loaded step details, and approval/rejection controls. Expanding a row
 * fetches the detailed run snapshot; approval actions refresh the list to show
 * resumed or canceled state. Reopening a row fetches its latest details.
 */
import '@/styles/management.css';
import { Icon } from '@/components/ui/Icon';
import { useCallback, useEffect, useState } from 'react';
import { clsx } from 'clsx';
import { approveRun, getRun, listRuns } from '@/api/workflow';
import type { WorkflowRun, WorkflowStepRun } from '@/types/workflow';
import { Badge, Button, EmptyState, Input, PageHeader, Spinner, StatusBadge, Textarea } from '@/components/ui/primitives';
import { Modal } from '@/components/ui/Modal';
import { fullTime, tokenCount, truncate } from '@/lib/format';
import { toast } from '@/stores/ui';
import { errMsg } from '@/lib/request';

const PAGE_SIZE = 20;

/**
 * Display persisted node snapshots rather than replaying execution. Timing,
 * token counts, and errors are read from the run-detail response.
 */
function StepTimeline({ steps }: { steps: WorkflowStepRun[] }) {
  if (steps.length === 0) {
    return <p className="text-xs text-ash">No step traces available</p>;
  }
  return (
    <div className="space-y-3 border-l border-smoke/60 pl-4">
      {steps.map((s) => (
        <div key={s.id} className="relative">
          <span className="absolute -left-[21px] top-1.5 h-2 w-2 rounded-full bg-smoke" />
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <span className="font-mono font-medium text-lime">{s.node_key}</span>
            <Badge tone="violet">{s.node_type}</Badge>
            <StatusBadge status={s.status} />
            <span className="text-ash">tokens {tokenCount(s.tokens)}</span>
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-3 text-[11px] text-ash">
            <span>Started {fullTime(s.started_at)}</span>
            <span>Finished {fullTime(s.finished_at)}</span>
          </div>
          {s.error && <p className="mt-1 text-xs text-coral">✕ {s.error}</p>}
        </div>
      ))}
    </div>
  );
}

export function WorkflowRunsPage() {
  const [list, setList] = useState<WorkflowRun[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  /** Keep raw filter text separate from the validated ID currently sent to the API. */
  const [filterInput, setFilterInput] = useState('');
  const [wfFilter, setWfFilter] = useState<number | undefined>(undefined);

  /** Only one run detail panel is expanded and fetched at a time. */
  const [expandedId, setExpandedId] = useState<number | null>(null);
  const [detail, setDetail] = useState<WorkflowRun | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);

  /** Approval state is isolated from row expansion and list pagination. */
  const [approveTarget, setApproveTarget] = useState<WorkflowRun | null>(null);
  const [comment, setComment] = useState('');
  const [approving, setApproving] = useState<'pass' | 'reject' | null>(null);

  /**
   * Reload the selected page with its optional workflow filter. Detail loading
   * remains independent so the table can update without assuming every run has steps.
   */
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const p = await listRuns({ page, page_size: PAGE_SIZE, workflow_id: wfFilter });
      setList(p.list);
      setTotal(p.total);
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setLoading(false);
    }
  }, [page, wfFilter]);

  useEffect(() => {
    void load();
  }, [load]);

  /**
   * Commit a positive integer workflow ID, or clear the filter for empty input.
   * Reset pagination when the effective filter changes.
   */
  const applyFilter = () => {
    const raw = filterInput.trim();
    if (raw === '') {
      setWfFilter(undefined);
    } else {
      const n = Number(raw);
      if (!Number.isInteger(n) || n <= 0) {
        toast.err('Please enter a valid workflow ID (a positive integer)');
        return;
      }
      setWfFilter(n);
    }
    setPage(1);
  };

  /**
   * Collapse the active row, or fetch full details for a newly expanded run.
   * A failed request returns the row to its collapsed state.
   */
  const toggleExpand = async (run: WorkflowRun) => {
    if (expandedId === run.id) {
      setExpandedId(null);
      setDetail(null);
      return;
    }
    setExpandedId(run.id);
    setDetail(null);
    setDetailLoading(true);
    try {
      setDetail(await getRun(run.id));
    } catch (e) {
      toast.err(errMsg(e));
      setExpandedId(null);
    } finally {
      setDetailLoading(false);
    }
  };

  /**
   * Start a fresh decision dialog for the selected paused run and clear any
   * comment left from a previous decision.
   */
  const openApprove = (run: WorkflowRun) => {
    setApproveTarget(run);
    setComment('');
  };

  /**
   * Track pass/reject separately for button feedback, submit the decision, and
   * refresh server snapshots so resumed or canceled state appears in the table.
   */
  const doApprove = async (approved: boolean) => {
    if (!approveTarget) return;
    setApproving(approved ? 'pass' : 'reject');
    try {
      await approveRun(approveTarget.id, approved, comment.trim() || undefined);
      toast.ok(approved ? 'Approved. The workflow will continue.' : 'Rejected');
      setApproveTarget(null);
      void load();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setApproving(null);
    }
  };

  const hasFilter = wfFilter !== undefined;

  return (
    <div className="page-container management-page">
      <PageHeader
        title="Workflow runs"
        desc="View workflow traces and status. Runs awaiting approval require a human decision."
        action={
          <Button variant="ghost" onClick={() => void load()}>Refresh</Button>
        }
      />

      <div className="management-toolbar mb-5 flex flex-wrap items-center gap-3">
        <Input
          className="w-full sm:w-64"
          placeholder="Filter by workflow ID"
          value={filterInput}
          onChange={(e) => setFilterInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && applyFilter()}
        />
        <Button variant="ghost" className="px-3 py-1.5 text-xs" onClick={applyFilter}>
          Search
        </Button>
        {hasFilter && (
          <button
            className="text-xs text-ash transition-colors hover:text-lime"
            onClick={() => {
              setFilterInput('');
              setWfFilter(undefined);
              setPage(1);
            }}
          >
            ✕ Clear filter
          </button>
        )}
      </div>

      <div className="panel overflow-x-auto">
        <table className="management-table w-full min-w-[880px] text-sm">
          <thead>
            <tr className="border-b border-smoke bg-graphite/70 text-left text-xs text-fog">
              <th className="w-8 px-4 py-3 font-medium" />
              <th className="px-4 py-3 font-medium">ID</th>
              <th className="px-4 py-3 font-medium">Workflow</th>
              <th className="px-4 py-3 font-medium">Trigger</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium">Created</th>
              <th className="px-4 py-3 font-medium">Error</th>
              <th className="px-4 py-3 text-right font-medium">Actions</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={8} className="px-4 py-12 text-center">
                  <Spinner className="mx-auto h-5 w-5 text-ash" />
                </td>
              </tr>
            ) : list.length === 0 ? (
              <tr>
                <td colSpan={8}>
                  <EmptyState
                    icon={<Icon name="workflow" className="h-6 w-6" />}
                    title="No workflow runs yet"
                    hint={hasFilter ? 'No runs match this filter. Try clearing it.' : 'Run a workflow, or wait for a scheduled or API trigger.'}
                  />
                </td>
              </tr>
            ) : (
              list.map((run) => {
                const waiting = run.status === 'waiting_approval';
                const expanded = expandedId === run.id;
                return (
                  <tr
                    key={run.id}
                    className={clsx(
                      'cursor-pointer border-b border-smoke/60 align-top transition-colors last:border-b-0 hover:bg-graphite/60',
                      waiting && 'border-l-2 border-l-lime/60 bg-lime/[0.04]',
                    )}
                    onClick={() => void toggleExpand(run)}
                  >
                    <td className="px-4 py-3 text-xs text-ash">{expanded ? '▾' : '▸'}</td>
                    <td className="px-4 py-3 font-mono text-xs text-fog">#{run.id}</td>
                    <td className="px-4 py-3 font-mono text-xs text-fog">#{run.workflow_id}</td>
                    <td className="px-4 py-3 text-xs text-fog">{run.trigger_type}</td>
                    <td className="px-4 py-3"><StatusBadge status={run.status} /></td>
                    <td className="px-4 py-3 text-xs text-ash">{fullTime(run.created_at)}</td>
                    <td className="max-w-[14rem] px-4 py-3 text-xs text-coral" title={run.error}>
                      {run.error ? truncate(run.error, 30) : '—'}
                    </td>
                    <td className="px-4 py-3 text-right">
                      {waiting && (
                        <button
                          className="text-xs text-lime transition-colors hover:underline"
                          onClick={(e) => {
                            e.stopPropagation();
                            openApprove(run);
                          }}
                        >
                          Review
                        </button>
                      )}
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>

        {expandedId !== null && (
          <div className="border-t border-smoke/60 bg-void/40 px-6 py-4">
            {detailLoading ? (
              <div className="flex items-center gap-2 text-xs text-ash">
                <Spinner className="h-3.5 w-3.5" />
                Loading run trace…
              </div>
            ) : detail ? (
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <span className="section-title">Run trace · #{detail.id}</span>
                  <StatusBadge status={detail.status} />
                  {detail.status === 'waiting_approval' && (
                    <button
                      className="text-xs text-lime transition-colors hover:underline"
                      onClick={() => openApprove(detail)}
                    >
                      Review approval
                    </button>
                  )}
                </div>
                <StepTimeline steps={detail.steps ?? []} />
              </div>
            ) : null}
          </div>
        )}
      </div>

      <div className="mt-5 flex flex-wrap items-center justify-between gap-3 text-xs text-ash">
        <span>{total} {total === 1 ? 'run' : 'runs'} · Page {page}</span>
        <div className="flex gap-2">
          <Button variant="ghost" className="px-3 py-1.5 text-xs" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
            Previous
          </Button>
          <Button
            variant="ghost"
            className="px-3 py-1.5 text-xs"
            disabled={page * PAGE_SIZE >= total}
            onClick={() => setPage((p) => p + 1)}
          >
            Next
          </Button>
        </div>
      </div>

      <Modal
        open={approveTarget !== null}
        title="Human approval"
        onClose={() => setApproveTarget(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setApproveTarget(null)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              loading={approving === 'reject'}
              disabled={approving !== null}
              onClick={() => void doApprove(false)}
            >
              Reject
            </Button>
            <Button loading={approving === 'pass'} disabled={approving !== null} onClick={() => void doApprove(true)}>
              Approve
            </Button>
          </>
        }
      >
        {approveTarget && (
          <div className="space-y-3">
            <div className="space-y-1.5 text-xs text-fog">
              <p>
                Run <span className="font-mono text-bone">#{approveTarget.id}</span>
                <span className="mx-2 text-ash">·</span>
                Workflow <span className="font-mono text-bone">#{approveTarget.workflow_id}</span>
              </p>
              <p>
                Status: <StatusBadge status={approveTarget.status} />
                <span className="mx-2 text-ash">·</span>
                Trigger: {approveTarget.trigger_type}
              </p>
              <p>Created: {fullTime(approveTarget.created_at)}</p>
              {approveTarget.error && <p className="text-coral">✕ {approveTarget.error}</p>}
              {Object.keys(approveTarget.input).length > 0 && (
                <pre className="code-block max-h-32">{JSON.stringify(approveTarget.input, null, 2)}</pre>
              )}
            </div>
            <div>
              <label className="field-label">Review comment (optional)</label>
              <Textarea
                rows={3}
                placeholder="Add a comment to record with this decision"
                value={comment}
                onChange={(e) => setComment(e.target.value)}
              />
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
