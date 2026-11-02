/**
 * Provider and model configuration management using separate create/edit
 * drafts. API keys are write-only: an edit form starts with an empty secret and
 * only submits a replacement when entered. Provider/model catalogs load together
 * and refresh after mutations so labels, defaults, and ownership stay consistent.
 */
import '@/styles/management.css';
import { Icon } from '@/components/ui/Icon';
import { useEffect, useRef, useState } from 'react';
import {
  createModel,
  createProvider,
  deleteModel,
  deleteProvider,
  listModels,
  listProviders,
  updateModel,
  updateProvider,
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
} from '@/components/ui/primitives';
import { relativeTime } from '@/lib/format';
import { staggerEnter } from '@/lib/gsap';
import { errMsg } from '@/lib/request';
import { toast } from '@/stores/ui';
import type { ModelConfig, ModelProvider, ModelType } from '@/types/admin';

/**
 * Presentation tones and labels follow model type rather than provider,
 * so all models in the same role remain visually comparable.
 */
const TYPE_TONE: Record<ModelType, 'lime' | 'teal' | 'violet'> = {
  chat: 'lime',
  embedding: 'teal',
  rerank: 'violet',
};

const TYPE_LABEL: Record<ModelType, string> = {
  chat: 'Chat',
  embedding: 'Embedding',
  rerank: 'Reranking',
};

/**
 * Capability identifiers match backend metadata; these checkboxes store
 * labels rather than detecting support from a remote provider.
 */
const CAP_DEFS: { key: string; label: string }[] = [
  { key: 'vision', label: 'Vision' },
  { key: 'function_call', label: 'Function calling' },
  { key: 'reasoning', label: 'Reasoning' },
];

/**
 * Normalize numeric form changes to finite values before placing them in
 * the model draft, preventing NaN from entering a JSON payload.
 */
const toNum = (v: string): number => {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
};

/** Backend status 1 means enabled; other numeric values display as disabled. */
function EnabledBadge({ status }: { status: number }) {
  return status === 1 ? <Badge tone="lime">Enabled</Badge> : <Badge tone="gray">Disabled</Badge>;
}

/**
 * Editable provider fields with a boolean switch and a plaintext replacement
 * key that is never populated from the masked response.
 */
interface ProviderDraft {
  code: string;
  name: string;
  base_url: string;
  api_key: string;
  status: boolean;
}

const EMPTY_PROVIDER: ProviderDraft = {
  code: '',
  name: '',
  base_url: '',
  api_key: '',
  status: true,
};

/**
 * Model draft with string provider selection and numeric limits/prices.
 * The selection is converted to a numeric ID only when submitting.
 */
interface ModelDraft {
  provider_id: string;
  model_name: string;
  alias: string;
  type: ModelType;
  context_window: number;
  max_output: number;
  input_price: number;
  output_price: number;
  priority: number;
  capabilities: string[];
  is_default: boolean;
  status: boolean;
}

const EMPTY_MODEL: ModelDraft = {
  provider_id: '',
  model_name: '',
  alias: '',
  type: 'chat',
  context_window: 8192,
  max_output: 4096,
  input_price: 0,
  output_price: 0,
  priority: 100,
  capabilities: [],
  is_default: false,
  status: true,
};

export function ModelsAdminPage() {
  const [providers, setProviders] = useState<ModelProvider[]>([]);
  const [models, setModels] = useState<ModelConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);

  /** Provider and model editors keep independent targets and drafts. */
  const [providerOpen, setProviderOpen] = useState(false);
  const [editingProvider, setEditingProvider] = useState<ModelProvider | null>(null);
  const [providerDraft, setProviderDraft] = useState<ProviderDraft>(EMPTY_PROVIDER);

  const [modelOpen, setModelOpen] = useState(false);
  const [editingModel, setEditingModel] = useState<ModelConfig | null>(null);
  const [modelDraft, setModelDraft] = useState<ModelDraft>(EMPTY_MODEL);

  /** Resource kind lets one confirmation dialog dispatch to the correct endpoint. */
  const [delTarget, setDelTarget] = useState<{
    kind: 'provider' | 'model';
    id: number;
    label: string;
  } | null>(null);

  const rootRef = useRef<HTMLDivElement>(null);

  /**
   * Load providers and models concurrently so the relation table and provider
   * selector use one refreshed catalog after each mutation.
   */
  const load = async () => {
    setLoading(true);
    try {
      const [providerList, modelList] = await Promise.all([listProviders(), listModels()]);
      setProviders(providerList);
      setModels(modelList);
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
   * Play section entry motion only after the first completed load, avoiding
   * a repeated page entrance after every save or health-independent refresh.
   */
  const playedRef = useRef(false);
  useEffect(() => {
    if (loading || playedRef.current) return;
    playedRef.current = true;
    const sections = rootRef.current?.querySelectorAll('section');
    if (sections && sections.length > 0) staggerEnter(Array.from(sections));
  }, [loading]);

  /** Merge draft changes without modifying the provider catalog record. */
  const patchProvider = (patch: Partial<ProviderDraft>) =>
    setProviderDraft((d) => ({ ...d, ...patch }));

  /** Merge draft changes without replacing unrelated model fields. */
  const patchModel = (patch: Partial<ModelDraft>) =>
    setModelDraft((d) => ({ ...d, ...patch }));

  /** Add/remove the capability label once while preserving other selected labels. */
  const toggleCap = (key: string) => {
    setModelDraft((d) => ({
      ...d,
      capabilities: d.capabilities.includes(key)
        ? d.capabilities.filter((c) => c !== key)
        : [...d.capabilities, key],
    }));
  };

  /** Reset provider fields and secret input for a new resource. */
  const openProviderCreate = () => {
    setEditingProvider(null);
    setProviderDraft(EMPTY_PROVIDER);
    setProviderOpen(true);
  };

  /**
   * Prefill public provider fields while leaving the secret blank. A masked
   * summary is never submitted as if it were a real API key.
   */
  const openProviderEdit = (p: ModelProvider) => {
    setEditingProvider(p);
    // An empty key preserves the stored secret.
    setProviderDraft({
      code: p.code,
      name: p.name,
      base_url: p.base_url,
      api_key: '',
      status: p.status === 1,
    });
    setProviderOpen(true);
  };

  /**
   * Validate required endpoint identity fields and explicitly serialize status
   * as 1 or 0. Only a newly entered secret is included, preserving an existing
   * key on edits that change other provider settings.
   */
  const submitProvider = async () => {
    if (!providerDraft.code.trim() || !providerDraft.name.trim() || !providerDraft.base_url.trim()) {
      toast.err('Code, name, and Base URL are required');
      return;
    }
    setSaving(true);
    try {
      // Send zero explicitly to disable the provider.
      const base = {
        code: providerDraft.code.trim(),
        name: providerDraft.name.trim(),
        base_url: providerDraft.base_url.trim(),
        status: providerDraft.status ? 1 : 0,
      };
      // Only send a replacement key when one was entered.
      const payload = providerDraft.api_key.trim()
        ? { ...base, api_key: providerDraft.api_key.trim() }
        : base;
      if (editingProvider) {
        await updateProvider(editingProvider.id, payload);
        toast.ok('Provider updated');
      } else {
        await createProvider(payload);
        toast.ok('Provider created');
      }
      setProviderOpen(false);
      await load();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setSaving(false);
    }
  };

  /** Reset model-specific limits, prices, and capability flags to local defaults. */
  const openModelCreate = () => {
    setEditingModel(null);
    setModelDraft(EMPTY_MODEL);
    setModelOpen(true);
  };

  /**
   * Copy the persisted model into the draft, converting provider ID for Select
   * and normalizing an absent capabilities array.
   */
  const openModelEdit = (m: ModelConfig) => {
    setEditingModel(m);
    setModelDraft({
      provider_id: String(m.provider_id),
      model_name: m.model_name,
      alias: m.alias,
      type: m.type,
      context_window: m.context_window,
      max_output: m.max_output,
      input_price: m.input_price,
      output_price: m.output_price,
      priority: m.priority,
      capabilities: m.capabilities ?? [],
      is_default: m.is_default,
      status: m.status === 1,
    });
    setModelOpen(true);
  };

  /**
   * Require a model name and provider, serialize the full editable draft, then
   * refresh both catalogs so aliases, defaults, and provider labels stay current.
   */
  const submitModel = async () => {
    if (!modelDraft.model_name.trim()) {
      toast.err('Model name is required');
      return;
    }
    if (!modelDraft.provider_id) {
      toast.err('Please select a provider');
      return;
    }
    setSaving(true);
    try {
      const payload: Partial<ModelConfig> = {
        provider_id: Number(modelDraft.provider_id),
        model_name: modelDraft.model_name.trim(),
        alias: modelDraft.alias.trim(),
        type: modelDraft.type,
        context_window: modelDraft.context_window,
        max_output: modelDraft.max_output,
        input_price: modelDraft.input_price,
        output_price: modelDraft.output_price,
        priority: modelDraft.priority,
        capabilities: modelDraft.capabilities,
        is_default: modelDraft.is_default,
        status: modelDraft.status ? 1 : 0,
      };
      if (editingModel) {
        await updateModel(editingModel.id, payload);
        toast.ok('Model updated');
      } else {
        await createModel(payload);
        toast.ok('Model created');
      }
      setModelOpen(false);
      await load();
    } catch (e) {
      toast.err(errMsg(e));
    } finally {
      setSaving(false);
    }
  };

  /**
   * Dispatch the shared confirmation target by resource kind. Server dependency
   * checks remain authoritative, and failure leaves the target available to review.
   */
  const confirmDelete = async () => {
    if (!delTarget) return;
    setDeleting(true);
    try {
      if (delTarget.kind === 'provider') await deleteProvider(delTarget.id);
      else await deleteModel(delTarget.id);
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
        title="Model gateway"
        desc="Manage model providers and configurations. API keys are encrypted on the server and displayed as masked previews."
      />

      {loading ? (
        <div className="flex justify-center py-24">
          <Spinner className="h-6 w-6 text-lime" />
        </div>
      ) : (
        <div className="space-y-6">
          <section className="panel overflow-hidden">
            <div className="management-section-header flex flex-wrap items-center justify-between gap-3 border-b border-smoke/70 px-5 py-4">
              <h2 className="section-title">Model providers</h2>
              <Button onClick={openProviderCreate}>＋ Add provider</Button>
            </div>
            {providers.length === 0 ? (
              <EmptyState
                icon={<Icon name="models" className="h-6 w-6" />}
                title="No providers yet"
                hint="Connect OpenAI, Gemini, or DeepSeek, then add your models."
                action={<Button onClick={openProviderCreate}>Add provider</Button>}
              />
            ) : (
              <div className="overflow-x-auto">
                <table className="management-table w-full min-w-[800px] text-left text-sm">
                  <thead>
                    <tr className="border-b border-smoke bg-graphite/70 text-xs text-fog">
                      <th className="px-4 py-3.5 font-medium">Code</th>
                      <th className="px-4 py-3.5 font-medium">Name</th>
                      <th className="px-4 py-3.5 font-medium">Base URL</th>
                      <th className="px-4 py-3.5 font-medium">API Key</th>
                      <th className="px-4 py-3.5 font-medium">Status</th>
                      <th className="px-4 py-3.5 font-medium">Updated</th>
                      <th className="px-4 py-3.5 text-right font-medium">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-smoke/40">
                    {providers.map((p) => (
                      <tr key={p.id} className="transition-colors hover:bg-graphite/60">
                        <td className="px-4 py-3.5 font-mono text-lime">{p.code}</td>
                        <td className="px-4 py-3.5 text-bone">{p.name}</td>
                        <td className="px-4 py-3.5">
                          <span
                            className="block max-w-[240px] truncate font-mono text-xs text-fog"
                            title={p.base_url}
                          >
                            {p.base_url}
                          </span>
                        </td>
                        <td className="px-4 py-3.5">
                          {p.api_key_masked ? (
                            <span
                              className="font-mono text-xs text-fog"
                              title="Configured (encrypted; only a masked preview is shown)"
                            >
                              {p.api_key_masked}
                            </span>
                          ) : (
                            <Badge tone="gray">Not configured</Badge>
                          )}
                        </td>
                        <td className="px-4 py-3.5">
                          <EnabledBadge status={p.status} />
                        </td>
                        <td className="px-4 py-3.5 text-xs text-fog">
                          {relativeTime(p.updated_at) || '—'}
                        </td>
                        <td className="px-4 py-3.5">
                          <div className="flex justify-end gap-3 text-xs">
                            <button
                              className="text-ash transition-colors hover:text-lime"
                              onClick={() => openProviderEdit(p)}
                            >
                              Edit
                            </button>
                            <button
                              className="text-ash transition-colors hover:text-coral"
                              onClick={() =>
                                setDelTarget({ kind: 'provider', id: p.id, label: p.name })
                              }
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
              <h2 className="section-title">Model configurations</h2>
              <Button onClick={openModelCreate}>＋ Add model</Button>
            </div>
            {models.length === 0 ? (
              <EmptyState
                icon={<Icon name="models" className="h-6 w-6" />}
                title="No model configurations yet"
                hint={providers.length === 0 ? 'Add a provider first, then register its models.' : 'Register chat, embedding, or reranking models.'}
                action={<Button onClick={openModelCreate}>Add model</Button>}
              />
            ) : (
              <div className="overflow-x-auto">
                <table className="management-table w-full min-w-[800px] text-left text-sm">
                  <thead>
                    <tr className="border-b border-smoke bg-graphite/70 text-xs text-fog">
                      <th className="px-4 py-3.5 font-medium">Alias</th>
                      <th className="px-4 py-3.5 font-medium">Model name</th>
                      <th className="px-4 py-3.5 font-medium">Type</th>
                      <th className="px-4 py-3.5 font-medium">Provider</th>
                      <th className="px-4 py-3.5 font-medium">Context window</th>
                      <th className="px-4 py-3.5 font-medium">Input (CNY/1M)</th>
                      <th className="px-4 py-3.5 font-medium">Output (CNY/1M)</th>
                      <th className="px-4 py-3.5 font-medium">Priority</th>
                      <th className="px-4 py-3.5 font-medium">Default</th>
                      <th className="px-4 py-3.5 font-medium">Status</th>
                      <th className="px-4 py-3.5 text-right font-medium">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-smoke/40">
                    {models.map((m) => (
                      <tr key={m.id} className="transition-colors hover:bg-graphite/60">
                        <td className="px-4 py-3.5 text-bone">{m.alias || '—'}</td>
                        <td className="px-4 py-3.5 font-mono text-xs text-mist">{m.model_name}</td>
                        <td className="px-4 py-3.5">
                          <Badge tone={TYPE_TONE[m.type] ?? 'gray'}>
                            {TYPE_LABEL[m.type] ?? m.type}
                          </Badge>
                        </td>
                        <td className="px-4 py-3.5 text-fog">{m.provider?.name ?? '—'}</td>
                        <td className="px-4 py-3.5 font-mono text-xs text-fog">{m.context_window}</td>
                        <td className="px-4 py-3.5 font-mono text-xs text-fog">{m.input_price}</td>
                        <td className="px-4 py-3.5 font-mono text-xs text-fog">{m.output_price}</td>
                        <td className="px-4 py-3.5 font-mono text-xs text-fog">{m.priority}</td>
                        <td className="px-4 py-3.5">
                          {m.is_default ? <Badge tone="lime">Default</Badge> : <span className="text-xs text-ash">—</span>}
                        </td>
                        <td className="px-4 py-3.5">
                          <EnabledBadge status={m.status} />
                        </td>
                        <td className="px-4 py-3.5">
                          <div className="flex justify-end gap-3 text-xs">
                            <button
                              className="text-ash transition-colors hover:text-lime"
                              onClick={() => openModelEdit(m)}
                            >
                              Edit
                            </button>
                            <button
                              className="text-ash transition-colors hover:text-coral"
                              onClick={() =>
                                setDelTarget({ kind: 'model', id: m.id, label: m.alias || m.model_name })
                              }
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
        open={providerOpen}
        title={editingProvider ? 'Edit provider' : 'Add provider'}
        onClose={() => setProviderOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setProviderOpen(false)}>
              Cancel
            </Button>
            <Button loading={saving} onClick={() => void submitProvider()}>
              Save
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label className="field-label">Code (unique identifier)</label>
              <Input
                value={providerDraft.code}
                placeholder="e.g. deepseek"
                onChange={(e) => patchProvider({ code: e.target.value })}
              />
            </div>
            <div>
              <label className="field-label">Name</label>
              <Input
                value={providerDraft.name}
                placeholder="e.g. DeepSeek"
                onChange={(e) => patchProvider({ name: e.target.value })}
              />
            </div>
          </div>
          <div>
            <label className="field-label">Base URL</label>
            <Input
              value={providerDraft.base_url}
              placeholder="e.g. https://api.deepseek.com/v1"
              onChange={(e) => patchProvider({ base_url: e.target.value })}
            />
          </div>
          <div>
            <label className="field-label">API Key</label>
            <Input
              type="password"
              autoComplete="new-password"
              value={providerDraft.api_key}
              placeholder={editingProvider ? 'Leave blank to keep the current key' : 'sk-...'}
              onChange={(e) => patchProvider({ api_key: e.target.value })}
            />
          </div>
          <div>
            <label className="flex cursor-pointer items-center gap-2 text-sm text-mist">
              <input
                type="checkbox"
                className="accent-lime"
                checked={providerDraft.status}
                onChange={(e) => patchProvider({ status: e.target.checked })}
              />
              Enable provider (disabling it makes its models unavailable)
            </label>
          </div>
        </div>
      </Modal>

      <Modal
        open={modelOpen}
        title={editingModel ? 'Edit model' : 'Add model'}
        onClose={() => setModelOpen(false)}
        width="max-w-2xl"
        footer={
          <>
            <Button variant="ghost" onClick={() => setModelOpen(false)}>
              Cancel
            </Button>
            <Button loading={saving} onClick={() => void submitModel()}>
              Save
            </Button>
          </>
        }
      >
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="sm:col-span-2">
            <label className="field-label">Provider</label>
            <Select
              value={modelDraft.provider_id}
              onChange={(e) => patchModel({ provider_id: e.target.value })}
            >
              <option value="">Select a provider</option>
              {providers.map((p) => (
                <option key={p.id} value={String(p.id)}>
                  {p.name} ({p.code})
                </option>
              ))}
            </Select>
          </div>
          <div>
            <label className="field-label">Model name *</label>
            <Input
              value={modelDraft.model_name}
              placeholder="Provider model name, e.g. deepseek-chat"
              onChange={(e) => patchModel({ model_name: e.target.value })}
            />
          </div>
          <div>
            <label className="field-label">Display alias</label>
            <Input
              value={modelDraft.alias}
              placeholder="e.g. DeepSeek V3"
              onChange={(e) => patchModel({ alias: e.target.value })}
            />
          </div>
          <div>
            <label className="field-label">Type</label>
            <Select
              value={modelDraft.type}
              onChange={(e) => patchModel({ type: e.target.value as ModelType })}
            >
              <option value="chat">Chat</option>
              <option value="embedding">Embedding</option>
              <option value="rerank">Reranking</option>
            </Select>
          </div>
          <div>
            <label className="field-label">Priority (lower first)</label>
            <Input
              type="number"
              value={modelDraft.priority}
              onChange={(e) => patchModel({ priority: toNum(e.target.value) })}
            />
          </div>
          <div>
            <label className="field-label">Context window</label>
            <Input
              type="number"
              value={modelDraft.context_window}
              onChange={(e) => patchModel({ context_window: toNum(e.target.value) })}
            />
          </div>
          <div>
            <label className="field-label">Maximum output tokens</label>
            <Input
              type="number"
              value={modelDraft.max_output}
              onChange={(e) => patchModel({ max_output: toNum(e.target.value) })}
            />
          </div>
          <div>
            <label className="field-label">Input price (CNY/1M tokens)</label>
            <Input
              type="number"
              step="0.000001"
              value={modelDraft.input_price}
              onChange={(e) => patchModel({ input_price: toNum(e.target.value) })}
            />
          </div>
          <div>
            <label className="field-label">Output price (CNY/1M tokens)</label>
            <Input
              type="number"
              step="0.000001"
              value={modelDraft.output_price}
              onChange={(e) => patchModel({ output_price: toNum(e.target.value) })}
            />
          </div>
          <div className="sm:col-span-2">
            <label className="field-label">Capabilities</label>
            <div className="flex gap-5">
              {CAP_DEFS.map((cap) => (
                <label key={cap.key} className="flex cursor-pointer items-center gap-1.5 text-sm text-mist">
                  <input
                    type="checkbox"
                    className="accent-lime"
                    checked={modelDraft.capabilities.includes(cap.key)}
                    onChange={() => toggleCap(cap.key)}
                  />
                  {cap.label}
                </label>
              ))}
            </div>
          </div>
          <div className="sm:col-span-2">
            <label className="flex cursor-pointer items-center gap-2 text-sm text-mist">
              <input
                type="checkbox"
                className="accent-lime"
                checked={modelDraft.is_default}
                onChange={(e) => patchModel({ is_default: e.target.checked })}
              />
              Set as default model
            </label>
          </div>
          <div className="sm:col-span-2">
            <label className="flex cursor-pointer items-center gap-2 text-sm text-mist">
              <input
                type="checkbox"
                className="accent-lime"
                checked={modelDraft.status}
                onChange={(e) => patchModel({ status: e.target.checked })}
              />
              Enable model (disabled models cannot be used as defaults or fallbacks)
            </label>
          </div>
        </div>
      </Modal>

      <ConfirmDialog
        open={delTarget !== null}
        title={delTarget?.kind === 'provider' ? 'Delete provider' : 'Delete model'}
        content={
          delTarget
            ? delTarget.kind === 'provider'
              ? `Delete provider “${delTarget.label}”? Its model configurations may become unavailable. This cannot be undone.`
              : `Delete model “${delTarget.label}”? This cannot be undone.`
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
