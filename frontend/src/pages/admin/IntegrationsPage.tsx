/**
 * MCP and A2A integration management with discovery, health probes, and
 * manual invocation tools. Editor JSON fields are parsed before submission,
 * while tools_cache and Agent Cards are inspected defensively because remote
 * services can return incomplete or unfamiliar structures. Each modal owns a
 * separate draft/output state, and mutations reload the shared resource lists.
 */
import '@/styles/management.css';
import { Icon } from '@/components/ui/Icon';
import { useEffect, useRef, useState } from 'react';
import {
  callMCPTool,
  checkA2AAgent,
  checkMCPServer,
  createMCPServer,
  deleteA2AAgent,
  deleteMCPServer,
  listA2AAgents,
  listMCPServers,
  registerA2AAgent,
  sendA2ATask,
  syncMCPServer,
  updateMCPServer,
} from '@/api/admin';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { Modal } from '@/components/ui/Modal';
import {
  Badge,
  Button,
  EmptyState,
  Input,
  PageHeader,
  Select,
  Spinner,
  StatusBadge,
  Textarea,
} from '@/components/ui/primitives';
import { relativeTime, truncate } from '@/lib/format';
import { staggerEnter } from '@/lib/gsap';
import { errMsg } from '@/lib/request';
import { toast } from '@/stores/ui';
import type { A2AAgent, MCPServer } from '@/types/admin';

/** Badge tones for known MCP transports; unknown values use the render fallback. */
const TRANSPORT_TONE: Record<string, 'lime' | 'teal' | 'violet'> = {
  streamable_http: 'lime',
  stdio: 'teal',
  sse: 'violet',
};

/** Transport identifiers accepted by the editor and backend MCP registration. */
const TRANSPORTS = ['streamable_http', 'stdio', 'sse'] as const;

/**
 * Parse a JSON text field or use its supplied default when blank. The generic
 * type is a compile-time assertion; callers perform any additional shape checks.
 */
function parseJson<T>(text: string, fallback: T): T | null {
  const t = text.trim();
  if (!t) return fallback;
  try {
    return JSON.parse(t) as T;
  } catch {
    return null;
  }
}

/** Bound diagnostic output so a remote response cannot create an unbounded modal preview. */
const clip = (s: string) => truncate(s, 2000);

/**
 * Keep args/env/headers as editable JSON text. Parsing is deferred until
 * submission so temporarily incomplete input remains editable.
 */
interface McpDraft {
  name: string;
  transport: string;
  url: string;
  command: string;
  args: string;
  env: string;
  headers: string;
}

const EMPTY_MCP: McpDraft = {
  name: '',
  transport: 'streamable_http',
  url: '',
  command: '',
  args: '[]',
  env: '{}',
  headers: '{}',
};

export function IntegrationsPage() {
  const [servers, setServers] = useState<MCPServer[]>([]);
  const [agents, setAgents] = useState<A2AAgent[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  /** Track the row running a sync or health probe for button feedback. */
  const [busyId, setBusyId] = useState<number | null>(null);

  /** MCP editor state remains separate from discovery diagnostics. */
  const [mcpOpen, setMcpOpen] = useState(false);
  const [editingMcp, setEditingMcp] = useState<MCPServer | null>(null);
  const [mcpDraft, setMcpDraft] = useState<McpDraft>(EMPTY_MCP);

  /** Tool diagnostics retain their own input and bounded output. */
  const [debugServer, setDebugServer] = useState<MCPServer | null>(null);
  const [debugTool, setDebugTool] = useState('');
  const [debugArgs, setDebugArgs] = useState('{}');
  const [debugOutput, setDebugOutput] = useState('');
  const [debugRunning, setDebugRunning] = useState(false);

  /** A2A registration uses a fresh URL/name draft. */
  const [registerOpen, setRegisterOpen] = useState(false);
  const [regDraft, setRegDraft] = useState({ base_url: '', name: '' });

  /** Delegation results stay local to the selected remote agent dialog. */
  const [taskAgent, setTaskAgent] = useState<A2AAgent | null>(null);
  const [taskText, setTaskText] = useState('');
  const [taskResult, setTaskResult] = useState('');
  const [taskRunning, setTaskRunning] = useState(false);

  /** Kind distinguishes MCP server deletion from A2A registration deletion. */
  const [delTarget, setDelTarget] = useState<{
    kind: 'mcp' | 'a2a';
    id: number;
    label: string;
  } | null>(null);

  const rootRef = useRef<HTMLDivElement>(null);

  /**
   * Refresh MCP and A2A catalogs concurrently after writes or health/discovery
   * actions. Cached remote metadata may change along with the resource list.
   */
  const load = async () => {
    setLoading(true);
    try {
      const [serverList, agentList] = await Promise.all([listMCPServers(), listA2AAgents()]);
      setServers(serverList);
      setAgents(agentList);
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  /**
   * Run section entry motion after the first load only; subsequent list refreshes
   * should not replay the entire integration page entrance.
   */
  const playedRef = useRef(false);
  useEffect(() => {
    if (loading || playedRef.current) return;
    playedRef.current = true;
    const sections = rootRef.current?.querySelectorAll('section');
    if (sections && sections.length > 0) staggerEnter(Array.from(sections));
  }, [loading]);

  /** Merge one or more draft fields while preserving the other transport settings. */
  const patchMcp = (patch: Partial<McpDraft>) => setMcpDraft((d) => ({ ...d, ...patch }));

  /** Start from empty remote/process settings and valid empty JSON containers. */
  const openMcpCreate = () => {
    setEditingMcp(null);
    setMcpDraft(EMPTY_MCP);
    setMcpOpen(true);
  };

  /**
   * Copy an existing transport into the editor and pretty-print its JSON
   * fields. Absent values use empty containers suitable for editing.
   */
  const openMcpEdit = (s: MCPServer) => {
    setEditingMcp(s);
    setMcpDraft({
      name: s.name,
      transport: s.transport,
      url: s.url,
      command: s.command,
      args: JSON.stringify(Array.isArray(s.args) ? s.args : [], null, 2),
      env: JSON.stringify(s.env ?? {}, null, 2),
      headers: JSON.stringify(s.headers ?? {}, null, 2),
    });
    setMcpOpen(true);
  };

  /**
   * Require a name, parse JSON fields, and enforce an array for args before
   * submitting. Remaining transport-specific validation belongs to the backend.
   * Successful writes close the dialog and refresh discovery/health metadata.
   */
  const submitMcp = async () => {
    if (!mcpDraft.name.trim()) {
      toast.err('Name is required');
      return;
    }
    const args = parseJson<string[]>(mcpDraft.args, []);
    if (!args || !Array.isArray(args)) {
      toast.err('args must be a valid JSON array');
      return;
    }
    const env = parseJson<Record<string, string>>(mcpDraft.env, {});
    if (!env) {
      toast.err('env must be a valid JSON object');
      return;
    }
    const headers = parseJson<Record<string, string>>(mcpDraft.headers, {});
    if (!headers) {
      toast.err('headers must be a valid JSON object');
      return;
    }
    setSaving(true);
    try {
      const payload: Partial<MCPServer> = {
        name: mcpDraft.name.trim(),
        transport: mcpDraft.transport,
        url: mcpDraft.url.trim(),
        command: mcpDraft.command.trim(),
        args,
        env,
        headers,
      };
      if (editingMcp) {
        await updateMCPServer(editingMcp.id, payload);
        toast.ok('MCP server updated');
      } else {
        await createMCPServer(payload);
        toast.ok('MCP server created');
      }
      setMcpOpen(false);
      await load();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setSaving(false);
    }
  };

  /**
   * Disable the target row while remote tool discovery runs, report its count,
   * and reload the persisted tool-cache view after success.
   */
  const handleSync = async (s: MCPServer) => {
    setBusyId(s.id);
    try {
      const res = await syncMCPServer(s.id);
      toast.ok(`Synced ${res.synced ?? 0} tools`);
      await load();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setBusyId(null);
    }
  };

  /** Probe one MCP server and refresh its stored status/last-health timestamp. */
  const handleMcpHealth = async (s: MCPServer) => {
    setBusyId(s.id);
    try {
      const res = await checkMCPServer(s.id);
      toast.ok(`Health check complete: ${res.status}`);
      await load();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setBusyId(null);
    }
  };

  /** Bind the diagnostic dialog to one server and clear prior tool/input/output state. */
  const openDebug = (s: MCPServer) => {
    setDebugServer(s);
    setDebugTool('');
    setDebugArgs('{}');
    setDebugOutput('');
  };

  /**
   * Parse the argument text before invoking the chosen remote tool. Remote
   * output is formatted and clipped for inspection without changing the server cache.
   */
  const runDebug = async () => {
    if (!debugServer) return;
    if (!debugTool.trim()) {
      toast.err('Please select or enter a tool name');
      return;
    }
    let args: Record<string, unknown> = {};
    const text = debugArgs.trim();
    if (text) {
      try {
        args = JSON.parse(text) as Record<string, unknown>;
      } catch {
        toast.err('Arguments must be valid JSON');
        return;
      }
    }
    setDebugRunning(true);
    try {
      const res = await callMCPTool(debugServer.id, debugTool.trim(), args);
      setDebugOutput(clip(JSON.stringify(res.output ?? null, null, 2)));
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setDebugRunning(false);
    }
  };

  /**
   * Extract only valid name strings from open-shape discovery records. An empty
   * result leaves the diagnostic UI with manual tool-name entry.
   */
  const toolNames =
    debugServer && Array.isArray(debugServer.tools_cache)
      ? debugServer.tools_cache
          .map((t) => (typeof t === 'object' && t !== null ? (t as { name?: string }).name : undefined))
          .filter((n): n is string => typeof n === 'string' && n.length > 0)
      : [];

  /**
   * Require the remote Base URL and omit a blank display name so discovery can
   * supply it. Clear the successful registration draft before reloading the catalog.
   */
  const submitRegister = async () => {
    if (!regDraft.base_url.trim()) {
      toast.err('Base URL is required');
      return;
    }
    setSaving(true);
    try {
      // Omit an empty name so the backend uses the Agent Card name.
      const payload: { base_url: string; name?: string } = { base_url: regDraft.base_url.trim() };
      const name = regDraft.name.trim();
      if (name) payload.name = name;
      await registerA2AAgent(payload);
      toast.ok('Remote agent registered');
      setRegisterOpen(false);
      setRegDraft({ base_url: '', name: '' });
      await load();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setSaving(false);
    }
  };

  /** Probe one remote agent and refresh its persisted health snapshot. */
  const handleAgentHealth = async (a: A2AAgent) => {
    setBusyId(a.id);
    try {
      const res = await checkA2AAgent(a.id);
      toast.ok(`Health check complete: ${res.status}`);
      await load();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setBusyId(null);
    }
  };

  /** Start a fresh text delegation dialog for the chosen remote agent. */
  const openTask = (a: A2AAgent) => {
    setTaskAgent(a);
    setTaskText('');
    setTaskResult('');
  };

  /**
   * Send trimmed task text through the registered A2A endpoint and retain a
   * bounded JSON result in the dialog for inspection.
   */
  const submitTask = async () => {
    if (!taskAgent) return;
    if (!taskText.trim()) {
      toast.err('Please enter task content');
      return;
    }
    setTaskRunning(true);
    try {
      const res = await sendA2ATask(taskAgent.id, taskText.trim());
      setTaskResult(clip(JSON.stringify(res ?? null, null, 2)));
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setTaskRunning(false);
    }
  };

  /**
   * Use the target kind to remove an MCP server or A2A registration from the
   * same confirmation flow, then reload both integration catalogs.
   */
  const confirmDelete = async () => {
    if (!delTarget) return;
    setDeleting(true);
    try {
      if (delTarget.kind === 'mcp') await deleteMCPServer(delTarget.id);
      else await deleteA2AAgent(delTarget.id);
      toast.ok('Deleted successfully');
      setDelTarget(null);
      await load();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div ref={rootRef} className="page-container management-page">
      <PageHeader
        title="Integrations"
        desc="Connect MCP tool servers and A2A remote agents, with health checks and live debugging."
      />

      {loading ? (
        <div className="flex justify-center py-24">
          <Spinner className="h-6 w-6 text-lime" />
        </div>
      ) : (
        <div className="space-y-6">
          <section className="panel overflow-hidden">
            <div className="management-section-header flex flex-wrap items-center justify-between gap-3 border-b border-smoke/70 px-5 py-4">
              <h2 className="section-title">MCP Server</h2>
              <Button onClick={openMcpCreate}>＋ Add MCP server</Button>
            </div>
            {servers.length === 0 ? (
              <EmptyState
                icon={<Icon name="integrations" className="h-6 w-6" />}
                title="No MCP servers yet"
                hint="Connect an MCP server to make its tools available to agents."
                action={<Button onClick={openMcpCreate}>Add MCP server</Button>}
              />
            ) : (
              <div className="overflow-x-auto">
                <table className="management-table w-full min-w-[800px] text-left text-sm">
                  <thead>
                    <tr className="border-b border-smoke bg-graphite/70 text-xs text-fog">
                      <th className="px-4 py-3.5 font-medium">Name</th>
                      <th className="px-4 py-3.5 font-medium">Transport</th>
                      <th className="px-4 py-3.5 font-medium">Endpoint</th>
                      <th className="px-4 py-3.5 font-medium">Status</th>
                      <th className="px-4 py-3.5 font-medium">Tools</th>
                      <th className="px-4 py-3.5 font-medium">Last health check</th>
                      <th className="px-4 py-3.5 text-right font-medium">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-smoke/40">
                    {servers.map((s) => (
                      <tr key={s.id} className="transition-colors hover:bg-graphite/60">
                        <td className="px-4 py-3.5 text-bone">{s.name}</td>
                        <td className="px-4 py-3.5">
                          <Badge tone={TRANSPORT_TONE[s.transport] ?? 'gray'}>{s.transport}</Badge>
                        </td>
                        <td className="px-4 py-3.5">
                          <span
                            className="block max-w-[220px] truncate font-mono text-xs text-fog"
                            title={s.command || s.url}
                          >
                            {s.command || s.url || '—'}
                          </span>
                        </td>
                        <td className="px-4 py-3.5">
                          <StatusBadge status={s.status} />
                        </td>
                        <td className="px-4 py-3.5 font-mono text-xs text-fog">
                          {Array.isArray(s.tools_cache) ? s.tools_cache.length : 0}
                        </td>
                        <td className="px-4 py-3.5 text-xs text-fog">
                          {relativeTime(s.last_health_at) || '—'}
                        </td>
                        <td className="px-4 py-3.5">
                          <div className="flex justify-end gap-3 whitespace-nowrap text-xs">
                            <button
                              disabled={busyId === s.id}
                              className="text-ash transition-colors hover:text-lime disabled:opacity-40"
                              onClick={() => void handleSync(s)}
                            >
                              Sync tools
                            </button>
                            <button
                              disabled={busyId === s.id}
                              className="text-ash transition-colors hover:text-lime disabled:opacity-40"
                              onClick={() => void handleMcpHealth(s)}
                            >
                              Health check
                            </button>
                            <button
                              className="text-ash transition-colors hover:text-lime"
                              onClick={() => openDebug(s)}
                            >
                              Debug
                            </button>
                            <button
                              className="text-ash transition-colors hover:text-lime"
                              onClick={() => openMcpEdit(s)}
                            >
                              Edit
                            </button>
                            <button
                              className="text-ash transition-colors hover:text-coral"
                              onClick={() => setDelTarget({ kind: 'mcp', id: s.id, label: s.name })}
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

          <section className="panel overflow-hidden">
            <div className="management-section-header flex flex-wrap items-center justify-between gap-3 border-b border-smoke/70 px-5 py-4">
              <h2 className="section-title">A2A remote agents</h2>
              <Button onClick={() => setRegisterOpen(true)}>＋ Register remote agent</Button>
            </div>
            {agents.length === 0 ? (
              <EmptyState
                icon={<Icon name="agent" className="h-6 w-6" />}
                title="No remote agents yet"
                hint="Register an A2A remote agent to delegate tasks to it."
                action={<Button onClick={() => setRegisterOpen(true)}>Register remote agent</Button>}
              />
            ) : (
              <div className="overflow-x-auto">
                <table className="management-table w-full min-w-[800px] text-left text-sm">
                  <thead>
                    <tr className="border-b border-smoke bg-graphite/70 text-xs text-fog">
                      <th className="px-4 py-3.5 font-medium">Name</th>
                      <th className="px-4 py-3.5 font-medium">Base URL</th>
                      <th className="px-4 py-3.5 font-medium">Status</th>
                      <th className="px-4 py-3.5 font-medium">Agent card</th>
                      <th className="px-4 py-3.5 font-medium">Last health check</th>
                      <th className="px-4 py-3.5 text-right font-medium">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-smoke/40">
                    {agents.map((a) => (
                      <tr key={a.id} className="transition-colors hover:bg-graphite/60">
                        <td className="px-4 py-3.5 text-bone">{a.name}</td>
                        <td className="px-4 py-3.5">
                          <span
                            className="block max-w-[220px] truncate font-mono text-xs text-fog"
                            title={a.base_url}
                          >
                            {a.base_url}
                          </span>
                        </td>
                        <td className="px-4 py-3.5">
                          <StatusBadge status={a.status} />
                        </td>
                        <td className="px-4 py-3.5">
                          {a.card ? (
                            <div className="min-w-0">
                              <p className="max-w-[240px] truncate text-xs text-mist">
                                {(a.card as { name?: string }).name || 'Untitled'}
                              </p>
                              <p className="max-w-[240px] truncate text-xs text-ash">
                                {(a.card as { description?: string }).description ?? ''}
                              </p>
                            </div>
                          ) : (
                            <span className="text-xs text-ash">—</span>
                          )}
                        </td>
                        <td className="px-4 py-3.5 text-xs text-fog">
                          {relativeTime(a.last_health_at) || '—'}
                        </td>
                        <td className="px-4 py-3.5">
                          <div className="flex justify-end gap-3 whitespace-nowrap text-xs">
                            <button
                              disabled={busyId === a.id}
                              className="text-ash transition-colors hover:text-lime disabled:opacity-40"
                              onClick={() => void handleAgentHealth(a)}
                            >
                              Health check
                            </button>
                            <button
                              className="text-ash transition-colors hover:text-lime"
                              onClick={() => openTask(a)}
                            >
                              Delegate task
                            </button>
                            <button
                              className="text-ash transition-colors hover:text-coral"
                              onClick={() => setDelTarget({ kind: 'a2a', id: a.id, label: a.name })}
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
        </div>
      )}

      <Modal
        open={mcpOpen}
        title={editingMcp ? 'Edit MCP server' : 'Add MCP server'}
        onClose={() => setMcpOpen(false)}
        width="max-w-2xl"
        footer={
          <>
            <Button variant="ghost" onClick={() => setMcpOpen(false)}>
              Cancel
            </Button>
            <Button loading={saving} onClick={() => void submitMcp()}>
              Save
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label className="field-label">Name *</label>
              <Input
                value={mcpDraft.name}
                placeholder="e.g. filesystem"
                onChange={(e) => patchMcp({ name: e.target.value })}
              />
            </div>
            <div>
              <label className="field-label">Transport</label>
              <Select
                value={mcpDraft.transport}
                onChange={(e) => patchMcp({ transport: e.target.value })}
              >
                {TRANSPORTS.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </Select>
            </div>
          </div>
          <div>
            <label className="field-label">URL (streamable_http / sse)</label>
            <Input
              value={mcpDraft.url}
              placeholder="e.g. https://mcp.example.com/mcp"
              onChange={(e) => patchMcp({ url: e.target.value })}
            />
          </div>
          <div>
            <label className="field-label">Command (stdio)</label>
            <Input
              value={mcpDraft.command}
              placeholder="e.g. npx -y @modelcontextprotocol/server-filesystem /tmp"
              onChange={(e) => patchMcp({ command: e.target.value })}
            />
          </div>
          <div>
            <label className="field-label">Arguments (JSON array)</label>
            <Textarea
              rows={2}
              className="font-mono"
              value={mcpDraft.args}
              onChange={(e) => patchMcp({ args: e.target.value })}
            />
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label className="field-label">Environment (JSON object)</label>
              <Textarea
                rows={3}
                className="font-mono"
                value={mcpDraft.env}
                onChange={(e) => patchMcp({ env: e.target.value })}
              />
            </div>
            <div>
              <label className="field-label">Headers (JSON object)</label>
              <Textarea
                rows={3}
                className="font-mono"
                value={mcpDraft.headers}
                onChange={(e) => patchMcp({ headers: e.target.value })}
              />
            </div>
          </div>
        </div>
      </Modal>

      <Modal
        open={debugServer !== null}
        title={`Tool debugger · ${debugServer?.name ?? ''}`}
        onClose={() => setDebugServer(null)}
        width="max-w-xl"
        footer={
          <>
            <Button variant="ghost" onClick={() => setDebugServer(null)}>
              Close
            </Button>
            <Button loading={debugRunning} onClick={() => void runDebug()}>
              Run
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <div>
            <label className="field-label">Tool name</label>
            {toolNames.length > 0 ? (
              <Select value={debugTool} onChange={(e) => setDebugTool(e.target.value)}>
                <option value="">Select a tool</option>
                {toolNames.map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </Select>
            ) : (
              <Input
                value={debugTool}
                placeholder="No tool names found in the cache. Enter a tool name."
                onChange={(e) => setDebugTool(e.target.value)}
              />
            )}
          </div>
          <div>
            <label className="field-label">Parameters (JSON)</label>
            <Textarea
              rows={4}
              className="font-mono"
              value={debugArgs}
              onChange={(e) => setDebugArgs(e.target.value)}
            />
          </div>
          {debugOutput && (
            <div>
              <label className="field-label">Output</label>
              <pre className="code-block max-h-64 whitespace-pre-wrap">{debugOutput}</pre>
            </div>
          )}
        </div>
      </Modal>

      <Modal
        open={registerOpen}
        title="Register remote agent"
        onClose={() => setRegisterOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRegisterOpen(false)}>
              Cancel
            </Button>
            <Button loading={saving} onClick={() => void submitRegister()}>
              Register
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <div>
            <label className="field-label">Base URL *</label>
            <Input
              value={regDraft.base_url}
              placeholder="e.g. https://agent.example.com"
              onChange={(e) => setRegDraft((d) => ({ ...d, base_url: e.target.value }))}
            />
          </div>
          <div>
            <label className="field-label">Name (optional)</label>
            <Input
              value={regDraft.name}
              placeholder="Leave blank to use the agent card name. Registration checks /.well-known/agent.json."
              onChange={(e) => setRegDraft((d) => ({ ...d, name: e.target.value }))}
            />
          </div>
        </div>
      </Modal>

      <Modal
        open={taskAgent !== null}
        title={`Delegate task · ${taskAgent?.name ?? ''}`}
        onClose={() => setTaskAgent(null)}
        width="max-w-xl"
        footer={
          <>
            <Button variant="ghost" onClick={() => setTaskAgent(null)}>
              Close
            </Button>
            <Button loading={taskRunning} onClick={() => void submitTask()}>
              Send task
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <div>
            <label className="field-label">Task</label>
            <Textarea
              rows={4}
              value={taskText}
              placeholder="Describe the task to delegate to this agent"
              onChange={(e) => setTaskText(e.target.value)}
            />
          </div>
          {taskResult && (
            <div>
              <label className="field-label">Result</label>
              <pre className="code-block max-h-64 whitespace-pre-wrap">{taskResult}</pre>
            </div>
          )}
        </div>
      </Modal>

      <ConfirmDialog
        open={delTarget !== null}
        title={delTarget?.kind === 'mcp' ? 'Delete MCP server' : 'Delete remote agent'}
        content={
          delTarget
            ? `Delete “${delTarget.label}”? This cannot be undone.`
            : ''
        }
        danger
        confirmText="Delete"
        loading={deleting}
        onCancel={() => setDelTarget(null)}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  );
}
