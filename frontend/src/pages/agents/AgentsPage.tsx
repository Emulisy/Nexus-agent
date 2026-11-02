/**
 * Agent catalog and create/edit dialog with model, memory, knowledge base,
 * and tool bindings. The page fetches the first 100 agents and lazily refreshes
 * the tool catalog when the editor opens. Draft values remain separate from
 * server records so canceled edits do not modify the displayed configuration.
 */
import '@/styles/management.css';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Badge,
  Button,
  Card,
  EmptyState,
  Input,
  PageHeader,
  Select,
  Spinner,
  Textarea,
} from '@/components/ui/primitives';
import { Modal } from '@/components/ui/Modal';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { createAgent, deleteAgent, listAgents, listTools, updateAgent } from '@/api/agent';
import { listChatModels } from '@/api/chat';
import type { Agent, AgentReq, Tool } from '@/types/agent';
import type { ChatModelItem } from '@/types/chat';
import { errMsg } from '@/lib/request';
import { toast } from '@/stores/ui';
import { staggerEnter } from '@/lib/gsap';
import { Icon } from '@/components/ui/Icon';
import { useChat } from '@/stores/chat';

const TYPE_BADGE: Record<Agent['type'], { text: string; tone: 'violet' | 'teal' }> = {
  host: { text: 'Host · Routing', tone: 'violet' },
  expert: { text: 'Expert · Specialist', tone: 'teal' },
};

/** Keep numeric inputs as strings until submission to allow partial edits. */
interface AgentForm {
  name: string;
  description: string;
  type: Agent['type'];
  system_prompt: string;
  model_config_id: string;
  temperature: string;
  max_tokens: string;
  max_iterations: string;
  memory_enabled: boolean;
  memory_long_enabled: boolean;
  kb_ids: string;
  tool_ids: number[];
}

const EMPTY_FORM: AgentForm = {
  name: '',
  description: '',
  type: 'expert',
  system_prompt: '',
  model_config_id: '',
  temperature: '0.7',
  max_tokens: '',
  max_iterations: '',
  memory_enabled: true,
  memory_long_enabled: false,
  kb_ids: '',
  tool_ids: [],
};

/**
 * Copy persisted values into editable strings and ID selections. Numeric
 * inputs keep intermediate text states, and canceled drafts never mutate the Agent.
 */
function toForm(a: Agent): AgentForm {
  return {
    name: a.name,
    description: a.description,
    type: a.type,
    system_prompt: a.system_prompt,
    model_config_id: a.model_config_id ? String(a.model_config_id) : '',
    temperature: a.temperature != null ? String(a.temperature) : '',
    max_tokens: a.max_tokens != null ? String(a.max_tokens) : '',
    max_iterations: a.max_iterations != null ? String(a.max_iterations) : '',
    memory_enabled: a.memory_enabled,
    memory_long_enabled: a.memory_long_enabled,
    kb_ids: (a.kb_ids ?? []).join(','),
    tool_ids: (a.tools ?? []).map((t) => t.tool_id),
  };
}

/** Accept comma- or whitespace-separated IDs and discard invalid entries. */
function parseIds(s: string): number[] {
  return s
    .split(/[,，\s]+/)
    .filter((t) => t !== '')
    .map(Number)
    .filter((n) => Number.isFinite(n));
}

/**
 * Manage list refresh, editor draft, and confirmed deletion independently.
 * Preset deletion is withheld in the UI; the server still enforces ownership.
 */
export function AgentsPage() {
  const navigate = useNavigate();
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [tools, setTools] = useState<Tool[]>([]);
  const [models, setModels] = useState<ChatModelItem[]>([]);
  const [modalOpen, setModalOpen] = useState(false);
  /** A null target means creation; the same draft/modal handles both modes. */
  const [editing, setEditing] = useState<Agent | null>(null);
  const [form, setForm] = useState<AgentForm>(EMPTY_FORM);
  const [saving, setSaving] = useState(false);
  /** Keep destructive confirmation independent from whichever editor was open. */
  const [delTarget, setDelTarget] = useState<Agent | null>(null);
  const gridRef = useRef<HTMLDivElement>(null);

  /**
   * Load the current 100-row management view and report failures through the
   * global notification queue. The busy flag is reset even when loading fails.
   */
  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listAgents({ page: 1, page_size: 100 });
      setAgents(data.list);
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
    void listChatModels().then(setModels).catch((e) => toast.err(errMsg(e)));
  }, [refresh]);

  /**
   * Animate the newly rendered card collection and kill its tween before
   * the list changes again or the page unmounts.
   */
  useEffect(() => {
    const els = gridRef.current ? Array.from(gridRef.current.children) : [];
    if (els.length === 0) return;
    const tween = staggerEnter(els);
    return () => {
      tween.kill();
    };
  }, [agents]);

  /**
   * Refresh tool choices when the editor opens so recently synchronized MCP
   * tools can be bound without reloading the whole page.
   */
  useEffect(() => {
    if (!modalOpen) return;
    void listChatModels().then(setModels).catch((e) => toast.err(errMsg(e)));
    listTools({ page: 1, page_size: 100 })
      .then((d) => setTools(d.list))
      .catch((e) => toast.err(errMsg(e)));
  }, [modalOpen]);

  /** Start an isolated draft with defaults; no API call occurs until submission. */
  const openCreate = () => {
    setEditing(null);
    setForm(EMPTY_FORM);
    setModalOpen(true);
  };

  /**
   * Copy the selected record into the shared editor instead of binding inputs
   * directly to the list item.
   */
  const openEdit = (a: Agent) => {
    setEditing(a);
    setForm(toForm(a));
    setModalOpen(true);
  };

  const openChat = (a: Agent) => {
    useChat.getState().setActiveAgent(a.id);
    useChat.getState().setActiveModel(undefined);
    useChat.getState().newSession();
    navigate('/chat');
  };

  /** Patch a single typed draft field without discarding the other editor values. */
  const setField = <K extends keyof AgentForm>(key: K, value: AgentForm[K]) => {
    setForm((f) => ({ ...f, [key]: value }));
  };

  /** Keep selected tool IDs unique while allowing checkboxes to add or remove bindings. */
  const toggleTool = (id: number) => {
    setForm((f) => ({
      ...f,
      tool_ids: f.tool_ids.includes(id)
        ? f.tool_ids.filter((t) => t !== id)
        : [...f.tool_ids, id],
    }));
  };

  /**
   * Validate the required name, convert numeric drafts, and serialize selected
   * knowledge bases/tools. The same dialog chooses create or update from editing;
   * a successful write closes the draft and reloads the catalog.
   */
  const submit = async () => {
    if (!form.name.trim()) {
      toast.err('Please enter an Agent name');
      return;
    }
    // Omit empty numeric fields so the backend can apply defaults.
    const num = (s: string): number | undefined => (s.trim() === '' ? undefined : Number(s));
    const req: AgentReq = {
      name: form.name.trim(),
      description: form.description.trim(),
      type: form.type,
      system_prompt: form.system_prompt,
      model_config_id: num(form.model_config_id),
      temperature: num(form.temperature),
      max_tokens: num(form.max_tokens),
      max_iterations: num(form.max_iterations),
      memory_enabled: form.memory_enabled,
      memory_long_enabled: form.memory_long_enabled,
      kb_ids: parseIds(form.kb_ids),
      tools: form.tool_ids.map((tool_id) => ({ tool_id })),
    };
    setSaving(true);
    try {
      if (editing) {
        await updateAgent(editing.id, req);
        toast.ok('Agent updated');
      } else {
        await createAgent(req);
        toast.ok('Agent created');
      }
      setModalOpen(false);
      await refresh();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setSaving(false);
    }
  };

  /**
   * Delete the selected resource only from the confirmation action, then
   * refresh the list so server restrictions and concurrent changes are respected.
   */
  const confirmDelete = async () => {
    if (!delTarget) return;
    try {
      await deleteAgent(delTarget.id);
      toast.ok('Agent deleted');
      setDelTarget(null);
      await refresh();
    } catch (e) {
      toast.err(errMsg(e));
    }
  };

  return (
    <div className="page-container management-page">
      <PageHeader
        title="Agents"
        desc="Hosts route tasks and combine results. Experts handle specialized tasks."
        action={<Button onClick={openCreate}>＋ New agent</Button>}
      />

      {loading ? (
        <div className="flex justify-center py-20">
          <Spinner className="h-6 w-6 text-lime" />
        </div>
      ) : agents.length === 0 ? (
        <EmptyState
          icon={<Icon name="agent" className="h-6 w-6" />}
          title="No agents yet"
          hint="Select New agent to create your first agent."
        />
      ) : (
        <div ref={gridRef} className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3">
          {agents.map((a) => (
            <Card key={a.id} className="management-card flex flex-col gap-4">
              <div className="space-y-1.5">
                <div className="management-icon-well"><Icon name="agent" className="h-5 w-5" /></div>
                <div className="flex items-center gap-2">
                  <h3 className="truncate text-base font-semibold text-bone">{a.name}</h3>
                  {a.is_preset && <Badge tone="gray">Preset</Badge>}
                </div>
                <Badge tone={TYPE_BADGE[a.type].tone}>{TYPE_BADGE[a.type].text}</Badge>
              </div>

              <p className="line-clamp-2 text-xs leading-relaxed text-fog">
                {a.description || 'No description'}
              </p>

              <p className="text-xs text-ash">
                Model: {models.find((m) => m.id === a.model_config_id)?.model_name ?? `Configuration #${a.model_config_id}`}
              </p>

              <div className="management-stat-grid grid grid-cols-3 gap-2 rounded-xl border border-smoke/60 bg-graphite/70 px-3 py-3 text-center">
                <div>
                  <p className="text-sm text-bone">{a.temperature}</p>
                  <p className="text-[10px] text-ash">Temperature</p>
                </div>
                <div>
                  <p className="text-sm text-bone">{a.max_tokens}</p>
                  <p className="text-[10px] text-ash">Max Tokens</p>
                </div>
                <div>
                  <p className="text-sm text-bone">{a.max_iterations}</p>
                  <p className="text-[10px] text-ash">Iteration limit</p>
                </div>
              </div>

              <div className="flex gap-4 text-xs text-ash">
                <span>
                  Tools <span className="text-fog">{a.tools.length}</span>
                </span>
                <span>
                  Knowledge bases <span className="text-fog">{a.kb_ids.length}</span>
                </span>
              </div>

              <div className="mt-auto flex justify-end gap-2 border-t border-smoke/40 pt-3">
                <Button variant="ghost" className="mr-auto px-3 py-1 text-xs" onClick={() => openChat(a)}>
                  Use in chat
                </Button>
                <Button variant="ghost" className="px-3 py-1 text-xs" onClick={() => openEdit(a)}>
                  Edit
                </Button>
                <Button
                  variant="danger"
                  className="px-3 py-1 text-xs"
                  disabled={a.is_preset}
                  title={a.is_preset ? 'Preset agents cannot be deleted' : undefined}
                  onClick={() => setDelTarget(a)}
                >
                  Delete
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}

      <Modal
        open={modalOpen}
        title={editing ? `Edit agent · ${editing.name}` : 'New agent'}
        onClose={() => setModalOpen(false)}
        width="max-w-2xl"
        footer={
          <>
            <Button variant="ghost" onClick={() => setModalOpen(false)}>
              Cancel
            </Button>
            <Button loading={saving} onClick={() => void submit()}>
              {editing ? 'Save changes' : 'Create'}
            </Button>
          </>
        }
      >
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label className="field-label">Name *</label>
              <Input
                value={form.name}
                onChange={(e) => setField('name', e.target.value)}
                placeholder="Example: Customer support agent"
              />
            </div>
            <div>
              <label className="field-label">Type</label>
              <Select
                value={form.type}
                onChange={(e) => setField('type', e.target.value as Agent['type'])}
              >
                <option value="expert">Expert · Specialized tasks</option>
                <option value="host">Host · Routing and aggregation</option>
              </Select>
            </div>
          </div>

          <div>
            <label className="field-label">Description</label>
            <Input
              value={form.description}
              onChange={(e) => setField('description', e.target.value)}
              placeholder="Describe the responsibilities of this agent"
            />
          </div>

          <div>
            <label className="field-label">System Prompt</label>
            <Textarea
              rows={6}
              value={form.system_prompt}
              onChange={(e) => setField('system_prompt', e.target.value)}
              placeholder="Define the role, capabilities, and response guidelines of this agent"
            />
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label htmlFor="agent-model" className="field-label">Model</label>
              <Select
                id="agent-model"
                value={form.model_config_id}
                onChange={(e) => setField('model_config_id', e.target.value)}
              >
                <option value="">Use platform default</option>
                {form.model_config_id && !models.some((m) => String(m.id) === form.model_config_id) && (
                  <option value={form.model_config_id} disabled>Unavailable model (#{form.model_config_id})</option>
                )}
                {models.map((m) => (
                  <option key={m.id} value={m.id}>{m.model_name} · {m.provider}</option>
                ))}
              </Select>
              <p className="mt-1 text-xs text-ash">Choose the model this agent uses. Chat model overrides apply only to that conversation.</p>
            </div>
            <div>
              <label className="field-label">Temperature</label>
              <Input
                type="number"
                step="0.1"
                value={form.temperature}
                onChange={(e) => setField('temperature', e.target.value)}
                placeholder="0.7"
              />
            </div>
            <div>
              <label className="field-label">Max Tokens</label>
              <Input
                type="number"
                value={form.max_tokens}
                onChange={(e) => setField('max_tokens', e.target.value)}
                placeholder="2048"
              />
            </div>
            <div>
              <label className="field-label">Maximum iterations</label>
              <Input
                type="number"
                value={form.max_iterations}
                onChange={(e) => setField('max_iterations', e.target.value)}
                placeholder="5"
              />
            </div>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <label className="flex cursor-pointer items-center gap-2 text-sm text-mist">
              <input
                type="checkbox"
                className="h-3.5 w-3.5 accent-lime"
                checked={form.memory_enabled}
                onChange={(e) => setField('memory_enabled', e.target.checked)}
              />
              Enable short-term memory
            </label>
            <label className="flex cursor-pointer items-center gap-2 text-sm text-mist">
              <input
                type="checkbox"
                className="h-3.5 w-3.5 accent-lime"
                checked={form.memory_long_enabled}
                onChange={(e) => setField('memory_long_enabled', e.target.checked)}
              />
              Enable long-term memory
            </label>
          </div>

          <div>
            <label className="field-label">Knowledge base IDs</label>
            <Input
              value={form.kb_ids}
              onChange={(e) => setField('kb_ids', e.target.value)}
              placeholder="Example: 1,2,3 (comma-separated IDs)"
            />
            <p className="mt-1 text-xs text-ash">Separate IDs with commas. Leave blank to use no knowledge bases.</p>
          </div>

          <div>
            <label className="field-label">Tools</label>
            <div className="max-h-40 space-y-0.5 overflow-y-auto rounded-lg border border-smoke/60 bg-graphite/40 p-2">
              {tools.map((t) => (
                <label
                  key={t.id}
                  className="flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 text-xs hover:bg-obsidian"
                >
                  <input
                    type="checkbox"
                    className="h-3.5 w-3.5 accent-lime"
                    disabled={t.status !== 1}
                    checked={form.tool_ids.includes(t.id)}
                    onChange={() => toggleTool(t.id)}
                  />
                  <span className="text-bone">{t.name}</span>
                  <span className="font-mono text-ash">{t.code}</span>
                  {t.status !== 1 && <Badge tone="gray">Disabled</Badge>}
                  {t.is_dangerous && <Badge tone="coral" className="ml-auto">Dangerous</Badge>}
                </label>
              ))}
              {tools.length === 0 && (
                <p className="py-2 text-center text-xs text-ash">No tools available</p>
              )}
            </div>
          </div>
        </form>
      </Modal>

      <ConfirmDialog
        open={delTarget !== null}
        title="Delete agent"
        content={`Delete agent “${delTarget?.name ?? ''}”? This cannot be undone.`}
        danger
        confirmText="Delete"
        onCancel={() => setDelTarget(null)}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  );
}
