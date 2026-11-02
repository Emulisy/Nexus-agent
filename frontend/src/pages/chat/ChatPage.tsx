/**
 * Conversation workspace composed from the session rail, message stream,
 * composer, and responsive Agent selector. Persisted messages and the transient
 * streaming response render separately. Plan cards display a received plan event;
 * this component does not initiate planning or execute tools.
 */
import { useEffect, useRef, useState } from 'react';
import { useChat } from '@/stores/chat';
import { listAgents } from '@/api/agent';
import type { Agent } from '@/types/agent';
import { SessionList } from './SessionList';
import { Composer } from './Composer';
import { HistoryBubble, StreamingBubble } from './MessageBubble';
import { Badge, Select } from '@/components/ui/primitives';
import { BrandMark } from '@/components/ui/Brand';
import { Icon } from '@/components/ui/Icon';
import { clsx } from 'clsx';
import '@/styles/chat.css';

/**
 * Render the latest incoming plan payload as a read-only list. No planner or
 * step execution is started here, and the card disappears with transient state.
 */
function PlanCard() {
  const plan = useChat((s) => s.streaming?.plan);
  if (!plan) return null;
  return (
    <div className="plan-card panel mx-auto w-full max-w-3xl border-lime/20 bg-lime/[0.03] p-5">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Badge tone="lime">Execution plan</Badge>
        <span className="text-sm font-medium text-bone">{plan.goal}</span>
      </div>
      <ol className="space-y-2.5">
        {plan.steps?.map((step, i) => (
          <li key={step.id} className="flex items-baseline gap-2 text-sm text-fog">
            <span className="font-mono text-xs text-lime">{i + 1}.</span>
            <span>{step.description}</span>
            {step.depends_on?.length > 0 && (
              <span className="text-[10px] text-ash">Depends on {step.depends_on.join(', ')}</span>
            )}
          </li>
        ))}
      </ol>
    </div>
  );
}

/**
 * The desktop rail shares its choices with the compact selector so agents remain
 * available below the desktop breakpoint.
 */
function AgentPanel({ agents }: { agents: Agent[] }) {
  const { activeAgentId, setActiveAgent, streaming } = useChat();

  return (
    <aside className="agent-rail hidden h-full w-48 shrink-0 flex-col gap-2 overflow-y-auto border-l border-smoke/70 px-4 py-6 xl:flex 2xl:w-56">
      <div className="mb-3 flex items-center justify-between px-1">
        <p className="section-title">Choose an agent</p>
        <Icon name="agent" className="h-4 w-4 text-ash" />
      </div>
      <button
        className={clsx(
          'agent-option rounded-xl border px-3.5 py-3 text-left text-sm transition-all',
          activeAgentId === undefined
            ? 'border-lime/15 bg-lime/[0.07] font-medium text-lime'
            : 'border-transparent text-fog hover:bg-graphite',
        )}
        onClick={() => setActiveAgent(undefined)}
        disabled={streaming !== null}
      >
        <span className="flex items-center gap-2"><span aria-hidden="true" className="agent-option-dot" />Default assistant</span>
        <span className="mt-1 block text-[11px] text-ash">No tools configured</span>
      </button>
      {agents.map((a) => (
        <button
          key={a.id}
          className={clsx(
            'agent-option rounded-xl border px-3.5 py-3 text-left transition-all',
            activeAgentId === a.id
              ? 'border-lime/15 bg-lime/[0.07] text-lime'
              : 'border-transparent text-fog hover:bg-graphite',
          )}
          onClick={() => setActiveAgent(a.id)}
          disabled={streaming !== null}
        >
          <span className="block truncate text-sm font-medium">{a.name}</span>
          <span className="mt-1 block text-[11px] text-ash">
            {a.type === 'host' ? 'Host · Routing' : 'Expert · Specialist'}
          </span>
        </button>
      ))}
    </aside>
  );
}

/**
 * Combine persisted history and one transient response while keeping the
 * composer and selector subscribed to the same chat store.
 */
export function ChatPage() {
  const { messages, streaming, activeAgentId, setActiveAgent } = useChat();
  const [agents, setAgents] = useState<Agent[]>([]);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    void listAgents({ page: 1, page_size: 100 })
      .then((p) => setAgents(p.list.filter((a) => a.status === 1)))
      .catch(() => setAgents([]));
  }, []);

  /**
   * Follow appended messages, text fragments, tool updates, and plan changes.
   * Reasoning-only updates are not a dependency of this scroll effect.
   */
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' });
  }, [messages.length, streaming?.content, streaming?.tools, streaming?.plan]);

  const hasContent = messages.length > 0 || streaming !== null;

  return (
    <div className="chat-workspace flex h-full min-h-0 flex-col md:flex-row">
      <SessionList />

      <div className="conversation-surface flex min-h-0 min-w-0 flex-1 flex-col">
        <div className="flex shrink-0 items-center gap-3 border-b border-smoke/60 px-4 py-3 xl:hidden">
          <label htmlFor="chat-agent" className="text-xs text-ash">Agent</label>
          <Select
            id="chat-agent"
            className="min-w-0 flex-1 text-xs"
            value={activeAgentId ?? ''}
            disabled={streaming !== null}
            onChange={(e) => setActiveAgent(e.target.value === '' ? undefined : Number(e.target.value))}
          >
            <option value="">Default assistant (no tools)</option>
            {agents.map((a) => <option key={a.id} value={a.id}>{a.name} · {a.tools.length} tools</option>)}
          </Select>
        </div>
        <div className="conversation-scroll min-h-0 flex-1 overflow-y-auto px-4 py-7 lg:px-8">
          {!hasContent ? (
            <div className="chat-welcome flex h-full min-h-64 flex-col items-center justify-center px-2 pb-3 text-center">
              <div className="welcome-orbit" aria-hidden="true">
                <span className="welcome-orbit-ring welcome-orbit-ring-outer" />
                <span className="welcome-orbit-ring welcome-orbit-ring-inner" />
                <span className="welcome-orbit-point" />
                <span className="welcome-orbit-cross welcome-orbit-cross-one">+</span>
                <span className="welcome-orbit-cross welcome-orbit-cross-two">+</span>
                <BrandMark className="welcome-brand" />
              </div>
              <p className="welcome-eyebrow mb-4 font-mono text-[10px] tracking-[0.22em] text-lime">SupaAgent · WORKSPACE</p>
              <h1 className="welcome-title text-2xl font-semibold tracking-tight text-bone lg:text-[32px]">Start a new conversation<span className="text-lime">.</span></h1>
              <p className="mt-4 max-w-xs text-[13px] leading-7 text-fog">
                Share an idea or ask a question below.
                <br />
                Revisit a conversation or choose an agent for your task.
              </p>
            </div>
          ) : (
            <div className="mx-auto w-full max-w-3xl space-y-6">
              {messages.map((m) => (
                <HistoryBubble key={m.id} msg={m} />
              ))}
              {streaming?.plan && <PlanCard />}
              {streaming && <StreamingBubble s={streaming} />}
              <div ref={bottomRef} />
            </div>
          )}
        </div>
        <Composer />
      </div>

      <AgentPanel agents={agents} />
    </div>
  );
}
