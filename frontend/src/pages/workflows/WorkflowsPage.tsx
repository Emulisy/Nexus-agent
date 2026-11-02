/**
 * Workflow catalog, YAML editor, validation, and synchronous execution.
 * Create/edit share a draft dialog. Run input must be a JSON object; completed
 * results render inline, while paused runs link to the execution records page
 * for approval. Server responses remain the source of saved versions and status.
 */
import '@/styles/management.css';
import { Icon } from '@/components/ui/Icon';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  createWorkflow,
  deleteWorkflow,
  listWorkflows,
  runWorkflow,
  updateWorkflow,
  validateWorkflow,
} from '@/api/workflow';
import type { Workflow, WorkflowRun } from '@/types/workflow';
import { Badge, Button, EmptyState, Input, PageHeader, Spinner, StatusBadge, Textarea } from '@/components/ui/primitives';
import { Modal } from '@/components/ui/Modal';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { relativeTime, truncate } from '@/lib/format';
import { staggerEnter } from '@/lib/gsap';
import { toast } from '@/stores/ui';
import { errMsg } from '@/lib/request';

/**
 * Default draft for a retrieval-to-answer workflow. The referenced knowledge
 * base must exist and be accessible; the editor does not provision it.
 * Node kinds accepted by the backend include llm, tool, kb, condition, parallel,
 * human, and subflow. Node keys name outputs used by template references;
 * edges describe execution order, with declaration order used when omitted.
 * This YAML remains a draft until explicit validation or save reaches the server.
 */
const DSL_TEMPLATE = `name: Example workflow
desc: A simple retrieval and question-answering pipeline
input: [question]
nodes:
  - key: retrieve
    type: kb
    kb_id: 1
    query: "{{question}}"
  - key: answer
    type: llm
    prompt: |
      Answer the question using these references: {{retrieve.result}}
      Question: {{question}}
edges: [retrieve, answer]
`;

export function WorkflowsPage() {
  const [list, setList] = useState<Workflow[]>([]);
  const [loading, setLoading] = useState(true);
  const tbodyRef = useRef<HTMLTableSectionElement>(null);

  /** Definition drafts, validation, and save state are separate from run requests. */
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<Workflow | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [dsl, setDsl] = useState(DSL_TEMPLATE);
  const [saving, setSaving] = useState(false);
  const [validating, setValidating] = useState(false);

  /** One selected workflow plus its input/result drives the synchronous run dialog. */
  const [runTarget, setRunTarget] = useState<Workflow | null>(null);
  const [inputText, setInputText] = useState('{}');
  const [running, setRunning] = useState(false);
  const [runResult, setRunResult] = useState<WorkflowRun | null>(null);

  /** Deletion is confirmed independently of the editor and execution dialogs. */
  const [delTarget, setDelTarget] = useState<Workflow | null>(null);
  const [deleting, setDeleting] = useState(false);

  /**
   * Fetch the current 100-row workflow catalog and keep loading/error state
   * separate from editor and execution requests.
   */
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const p = await listWorkflows({ page: 1, page_size: 100 });
      setList(p.list);
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
   * Animate rows after the list finishes loading; this does not alter saved
   * workflow order or execution state.
   */
  useEffect(() => {
    if (!loading && tbodyRef.current && tbodyRef.current.children.length > 0) {
      staggerEnter(Array.from(tbodyRef.current.children));
    }
  }, [loading, list]);

  /**
   * Use the same modal for creation and editing. Existing records copy their
   * nonempty YAML; empty or new records receive the default draft.
   */
  const openEditor = (w?: Workflow) => {
    setEditing(w ?? null);
    setName(w?.name ?? '');
    setDescription(w?.description ?? '');
    setDsl(w?.dsl || DSL_TEMPLATE);
    setEditorOpen(true);
  };

  /**
   * Ask the backend to check DSL without saving it. Validation errors arrive
   * as request errors and are displayed without closing the editor.
   */
  const handleValidate = async () => {
    setValidating(true);
    try {
      await validateWorkflow(dsl);
      toast.ok('DSL validation passed');
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setValidating(false);
    }
  };

  /**
   * Require a name, then choose create or update from the editor target.
   * DSL validation remains a server-side check during save.
   * A successful save closes the editor and refreshes server versions in the list.
   */
  const handleSave = async () => {
    if (!name.trim()) {
      toast.err('Please enter a workflow name');
      return;
    }
    setSaving(true);
    try {
      const payload = { name: name.trim(), description: description.trim(), dsl };
      if (editing) {
        await updateWorkflow(editing.id, payload);
        toast.ok('Workflow saved');
      } else {
        await createWorkflow(payload);
        toast.ok('Workflow created');
      }
      setEditorOpen(false);
      void load();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setSaving(false);
    }
  };

  /**
   * Clear the previous input/result when choosing a workflow to execute so
   * a prior run cannot be mistaken for the newly selected target.
   */
  const openRun = (w: Workflow) => {
    setRunTarget(w);
    setInputText('{}');
    setRunResult(null);
  };

  /**
   * Accept only a JSON object as workflow input. Empty text becomes an empty
   * object; arrays, null, and primitive values stop execution with a message.
   */
  const parseInput = (): Record<string, unknown> | null => {
    const raw = inputText.trim() || '{}';
    try {
      const v: unknown = JSON.parse(raw);
      if (v === null || typeof v !== 'object' || Array.isArray(v)) {
        toast.err('Input must be a JSON object, such as {"question":"..."}');
        return null;
      }
      return v as Record<string, unknown>;
    } catch {
      toast.err('Failed to parse input JSON. Please check its syntax.');
      return null;
    }
  };

  /**
   * Execute synchronously after parsing input and keep the returned run in
   * the open dialog. Paused approval state is handled in the run history page.
   */
  const handleRun = async () => {
    if (!runTarget) return;
    const parsed = parseInput();
    if (!parsed) return;
    setRunning(true);
    try {
      const run = await runWorkflow(runTarget.id, parsed);
      setRunResult(run);
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setRunning(false);
    }
  };

  /** Remove the confirmed workflow and reload the catalog after success. */
  const handleDelete = async () => {
    if (!delTarget) return;
    setDeleting(true);
    try {
      await deleteWorkflow(delTarget.id);
      toast.ok('Workflow deleted');
      setDelTarget(null);
      void load();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="page-container management-page">
      <PageHeader
        title="Workflows"
        desc="Build workflows with YAML, using models, tools, knowledge bases, and human approval."
        action={
          <Button onClick={() => openEditor()}>＋ New workflow</Button>
        }
      />

      <div className="panel overflow-x-auto">
        <table className="management-table w-full min-w-[760px] text-sm">
          <thead>
            <tr className="border-b border-smoke bg-graphite/70 text-left text-xs text-fog">
              <th className="px-4 py-3 font-medium">Name</th>
              <th className="px-4 py-3 font-medium">Description</th>
              <th className="px-4 py-3 font-medium">Version</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium">Updated</th>
              <th className="px-4 py-3 text-right font-medium">Actions</th>
            </tr>
          </thead>
          <tbody ref={tbodyRef}>
            {loading ? (
              <tr>
                <td colSpan={6} className="px-4 py-12 text-center">
                  <Spinner className="mx-auto h-5 w-5 text-ash" />
                </td>
              </tr>
            ) : list.length === 0 ? (
              <tr>
                <td colSpan={6}>
                  <EmptyState
                    icon={<Icon name="workflow" className="h-6 w-6" />}
                    title="No workflows yet"
                    hint="Select New workflow to start from an example. Supported nodes: llm, tool, kb, condition, parallel, human, and subflow."
                  />
                </td>
              </tr>
            ) : (
              list.map((w) => (
                <tr key={w.id} className="border-b border-smoke/60 transition-colors last:border-b-0 hover:bg-graphite/60">
                  <td className="px-4 py-3 font-medium text-bone">{w.name}</td>
                  <td className="max-w-[18rem] px-4 py-3 text-xs text-fog" title={w.description}>
                    {w.description ? truncate(w.description, 40) : '—'}
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-fog">v{w.version}</td>
                  <td className="px-4 py-3">
                    <Badge tone={w.is_active ? 'pulse' : 'gray'}>{w.is_active ? 'Enabled' : 'Disabled'}</Badge>
                  </td>
                  <td className="px-4 py-3 text-xs text-ash">{relativeTime(w.updated_at)}</td>
                  <td className="px-4 py-3">
                    <div className="flex justify-end gap-3 text-xs">
                      <button className="text-ash transition-colors hover:text-lime" onClick={() => openEditor(w)}>
                        Edit
                      </button>
                      <button className="text-ash transition-colors hover:text-teal" onClick={() => openRun(w)}>
                        Run
                      </button>
                      <button className="text-ash transition-colors hover:text-coral" onClick={() => setDelTarget(w)}>
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      <Modal
        open={editorOpen}
        title={editing ? `Edit workflow · ${editing.name}` : 'New workflow'}
        onClose={() => setEditorOpen(false)}
        width="max-w-2xl"
        footer={
          <>
            <Button variant="ghost" loading={validating} onClick={() => void handleValidate()}>
              Validate DSL
            </Button>
            <Button variant="ghost" onClick={() => setEditorOpen(false)}>
              Cancel
            </Button>
            <Button loading={saving} onClick={() => void handleSave()}>
              Save
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div>
            <label className="field-label">Name *</label>
            <Input value={name} placeholder="Workflow name" onChange={(e) => setName(e.target.value)} />
          </div>
          <div>
            <label className="field-label">Description</label>
            <Input value={description} placeholder="Briefly describe its purpose (optional)" onChange={(e) => setDescription(e.target.value)} />
          </div>
          <div>
            <label className="field-label">DSL (YAML)</label>
            <Textarea
              rows={16}
              className="font-mono text-xs leading-relaxed"
              spellCheck={false}
              value={dsl}
              onChange={(e) => setDsl(e.target.value)}
            />
          </div>
        </div>
      </Modal>

      <Modal
        open={runTarget !== null}
        title={runTarget ? `Run workflow · ${runTarget.name}` : 'Run workflow'}
        onClose={() => setRunTarget(null)}
        width="max-w-xl"
        footer={
          <>
            <Button variant="ghost" onClick={() => setRunTarget(null)}>
              Close
            </Button>
            <Button loading={running} onClick={() => void handleRun()}>
              Run
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <div>
            <label className="field-label">Input (JSON object)</label>
            <Textarea
              rows={4}
              className="font-mono text-xs"
              spellCheck={false}
              placeholder='{"question": "Hello"}'
              value={inputText}
              onChange={(e) => setInputText(e.target.value)}
            />
          </div>

          {runResult && (
            <div className="space-y-2 border-t border-smoke/60 pt-3">
              <div className="flex items-center gap-2">
                <span className="section-title">Result</span>
                <StatusBadge status={runResult.status} />
              </div>
              {runResult.status === 'waiting_approval' && (
                <p className="rounded-lg border border-lime/40 bg-lime/10 px-3 py-2 text-xs text-lime">
                  Paused for approval. Review this run on the Workflow runs page.
                </p>
              )}
              {runResult.error && <p className="text-xs text-coral">✕ {runResult.error}</p>}
              {runResult.output && (
                <pre className="code-block max-h-48 whitespace-pre-wrap">
                  {truncate(JSON.stringify(runResult.output, null, 2), 500)}
                </pre>
              )}
            </div>
          )}
        </div>
      </Modal>

      <ConfirmDialog
        open={delTarget !== null}
        title="Delete workflow"
        content={`Delete workflow “${delTarget?.name ?? ''}”? Existing run records will remain, but no new runs can be started.`}
        confirmText="Delete"
        danger
        loading={deleting}
        onCancel={() => setDelTarget(null)}
        onConfirm={() => void handleDelete()}
      />
    </div>
  );
}
