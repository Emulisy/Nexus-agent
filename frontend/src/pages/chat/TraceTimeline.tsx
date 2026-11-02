/**
 * Expandable trace rows shared by live tool calls and persisted agent traces.
 * Live calls are adapted from the store map with running as the default status.
 * Details retain structured arguments and a bounded result preview, while
 * expansion hooks apply height animations only after the first render.
 */
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { clsx } from 'clsx';
import { expandToggle } from '@/lib/gsap';
import type { TraceStep } from '@/types/chat';

/**
 * Localized labels for persisted trace categories; unfamiliar types retain
 * their raw identifier in the row fallback.
 */
const LABEL: Record<string, string> = {
  plan: 'Plan',
  thought: 'Thought',
  action: 'Tool call',
  observe: 'Observation',
  delegate: 'Delegate',
  merge: 'Merge',
  answer: 'Answer',
};

const TYPE_ICON: Record<string, string> = {
  plan: '❏',
  thought: '…',
  action: '⚡',
  observe: '◉',
  delegate: '⇢',
  merge: '⊕',
  answer: '✓',
};

/**
 * Skip animation on the initial render because the caller already sets the
 * initial height. Subsequent open-state changes animate the same content body.
 */
function useRefForExpand(open: boolean) {
  const ref = useRef<HTMLDivElement>(null);
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    const el = ref.current;
    if (el) expandToggle(el, open);
  }, [open]);
  return ref;
}

/**
 * Show timing and outcome at row level while keeping verbose arguments and
 * results behind an accessible expansion button.
 */
function StepRow({ step }: { step: TraceStep }) {
  const [open, setOpen] = useState(false);
  const bodyRef = useRefForExpand(open);
  const failed = step.status === 'error' || step.status === 'timeout';
  const running = step.status === 'running';

  return (
    <li className={clsx('relative pl-7', failed && 'text-coral')}>
      <span
        className={clsx(
          'absolute left-0 top-0.5 flex h-5 w-5 items-center justify-center rounded-full border text-[10px]',
          failed
            ? 'border-coral/60 bg-coral/15 text-coral'
            : running
              ? 'border-lime/60 bg-lime/15 text-lime'
              : 'border-smoke bg-graphite text-fog',
        )}
      >
        {TYPE_ICON[step.type] ?? '·'}
      </span>

      <button
        className="flex w-full flex-wrap items-baseline gap-x-2 gap-y-1 py-0.5 text-left text-xs transition-colors hover:text-bone"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className={clsx('font-medium', failed ? 'text-coral' : 'text-mist')}>
          {LABEL[step.type] ?? step.type}
        </span>
        {(step.tool || step.agent) && (
          <span className="font-mono text-fog">{step.tool ?? step.agent}</span>
        )}
        {step.elapsed_ms > 0 && (
          <span className={clsx('tabular-nums', failed ? 'text-coral/70' : 'text-ash')}>
            {step.elapsed_ms}ms
          </span>
        )}
        {running && <span className="animate-pulse text-lime">Calling…</span>}
        {failed && <span className="text-coral">✕ {step.status === 'timeout' ? 'Timed out' : 'Failed'}</span>}
      </button>

      <div ref={bodyRef} className="overflow-hidden" style={{ height: open ? 'auto' : 0 }}>
        {step.content && (
          <p className="mt-1 whitespace-pre-wrap text-xs leading-relaxed text-fog">{step.content}</p>
        )}
        {step.args && Object.keys(step.args).length > 0 && (
          <pre className="code-block mt-1 text-[11px]">{JSON.stringify(step.args, null, 2)}</pre>
        )}
        {step.result && (
          <pre className="code-block mt-1 max-h-40 text-[11px]">{step.result.slice(0, 400)}</pre>
        )}
      </div>
    </li>
  );
}

/** Render ordered trace snapshots using the backend sequence identifier when present. */
export function TraceTimeline({ steps }: { steps: TraceStep[] }) {
  if (!steps?.length) return null;
  return (
    <ol className="trace-timeline space-y-4">
      {steps.map((s) => (
        <StepRow key={s.seq ?? `${s.time}-${s.type}`} step={s} />
      ))}
    </ol>
  );
}

/**
 * Project live tool calls into the persisted trace-view shape. Object insertion
 * order supplies display order, and a missing result status means the call is running.
 */
export function streamingToSteps(
  tools: Record<
    string,
    { name: string; args: string; result?: string; ms?: number; status?: string }
  >,
): TraceStep[] {
  return Object.entries(tools).map(([id, t], i) => ({
    seq: i,
    type: 'action',
    agent: '',
    content: '',
    tool: t.name,
    args: safeParse(t.args),
    result: t.result,
    elapsed_ms: t.ms ?? 0,
    status: t.status ?? 'running',
    time: id,
  }));
}

/**
 * Parse structured tool arguments for inspection. Preserve malformed input
 * as a raw field instead of hiding the entire timeline node.
 */
function safeParse(s?: string): Record<string, unknown> | undefined {
  if (!s) return undefined;
  try {
    return JSON.parse(s) as Record<string, unknown>;
  } catch {
    return { raw: s };
  }
}

/**
 * Reusable disclosure with aria-expanded state and a height-animated body.
 * The caller supplies its own content semantics and optional summary badge.
 */
export function Collapse({
  title,
  badge,
  children,
}: {
  title: string;
  badge?: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRefForExpand(open);
  return (
    <div className="trace-collapse rounded-xl border border-smoke/70 bg-void/60">
      <button
        className="flex w-full items-center gap-2 rounded-xl px-3 py-2.5 text-xs text-fog transition-colors hover:bg-graphite/70 hover:text-mist"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <svg aria-hidden="true" className={clsx('h-3.5 w-3.5 shrink-0 transition-transform duration-200', open && 'rotate-90')} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="m9 6 6 6-6 6" /></svg>
        <span>{title}</span>
        {badge && <span className="text-ash">{badge}</span>}
      </button>
      <div ref={ref} className="overflow-hidden" style={{ height: open ? 'auto' : 0 }}>
        <div className="px-3 pt-1 pb-3 text-xs leading-relaxed text-fog">{children}</div>
      </div>
    </div>
  );
}
