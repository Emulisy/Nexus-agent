/**
 * Present persisted and streaming messages through the same bubble layout.
 * User content remains plain text; assistant content uses Markdown with GFM.
 * Reasoning, tool traces, references, and usage are optional attachments. Adapters
 * at the bottom keep wire records and transient store state out of the renderer.
 */
import { memo, useEffect, useRef, type ReactNode } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { clsx } from 'clsx';
import { bubbleIn } from '@/lib/gsap';
import type { Message } from '@/types/chat';
import type { TraceStep } from '@/types/chat';
import type { StreamingMsg } from '@/stores/chat';
import { Collapse, TraceTimeline, streamingToSteps } from './TraceTimeline';
import { BrandMark } from '@/components/ui/Brand';

/**
 * Show source filenames, page numbers, scores, and excerpt previews supplied
 * by retrieval metadata; absent references omit the section entirely.
 */
function RefsList({ refs }: { refs: { filename: string; page: number; content: string; score: number }[] }) {
  if (!refs?.length) return null;
  return (
    <Collapse title="Sources" badge={`${refs.length} source${refs.length === 1 ? '' : 's'}`}>
      <ul className="space-y-2">
        {refs.map((r, i) => (
          <li key={`${r.filename}-${i}`} className="rounded-lg border border-smoke/60 bg-void/70 p-3 text-xs">
            <div className="mb-1.5 flex items-center gap-2 text-fog">
              <span className="font-mono text-lime">[{i + 1}]</span>
              <span className="truncate">{r.filename}</span>
              {r.page > 0 && <span className="text-ash">p.{r.page}</span>}
              <span className="ml-auto tabular-nums text-ash">{r.score.toFixed(3)}</span>
            </div>
            <p className="line-clamp-3 leading-relaxed text-fog">{r.content}</p>
          </li>
        ))}
      </ul>
    </Collapse>
  );
}

/** Summarize trace size and failures before exposing the expandable node list. */
function TraceCollapse({ steps }: { steps: TraceStep[] }) {
  if (!steps?.length) return null;
  const errCount = steps.filter((s) => s.status === 'error' || s.status === 'timeout').length;
  return (
    <Collapse
      title="Decision trace"
      badge={`${steps.length} step${steps.length === 1 ? '' : 's'}${errCount > 0 ? ` · ${errCount} failed` : ''}`}
    >
      <TraceTimeline steps={steps} />
    </Collapse>
  );
}

/**
 * Keep code blocks inside the shared code surface and open external links in
 * a separate tab with referrer/opener isolation through rel=noreferrer.
 */
const MD_COMPONENTS = {
  pre: ({ children }: { children?: ReactNode }) => (
    <pre className="code-block my-2">{children}</pre>
  ),
  a: ({ href, children }: { href?: string; children?: ReactNode }) => (
    <a href={href} target="_blank" rel="noreferrer" className="text-lime underline-offset-2 hover:underline">
      {children}
    </a>
  ),
};

/**
 * Render assistant Markdown with GFM tables/lists. No raw-HTML plugin is enabled;
 * message text remains content supplied to the Markdown renderer.
 */
function Markdown({ content }: { content: string }) {
  return (
    <div className="min-w-0 space-y-3 text-sm leading-7 break-words text-mist [&_blockquote]:border-l-2 [&_blockquote]:border-lime/30 [&_blockquote]:pl-4 [&_blockquote]:text-fog [&_code]:rounded [&_code]:bg-graphite [&_code]:px-1 [&_code]:py-0.5 [&_code]:text-[0.9em] [&_h1]:text-xl [&_h1]:font-semibold [&_h1]:text-bone [&_h2]:text-lg [&_h2]:font-semibold [&_h2]:text-bone [&_h3]:font-semibold [&_h3]:text-bone [&_hr]:border-smoke [&_li]:ml-4 [&_ol]:list-decimal [&_p]:my-1 [&_pre]:max-w-full [&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_strong]:font-semibold [&_strong]:text-bone [&_table]:block [&_table]:max-w-full [&_table]:overflow-x-auto [&_td]:border [&_td]:border-smoke [&_td]:px-3 [&_td]:py-1.5 [&_th]:border [&_th]:border-smoke [&_th]:bg-graphite [&_th]:px-3 [&_th]:py-1.5 [&_ul]:list-disc">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={MD_COMPONENTS}>
        {content}
      </ReactMarkdown>
    </div>
  );
}

/**
 * Build a compact footer from available model, token, and latency values.
 * Zero/absent values are omitted rather than shown as empty placeholders.
 */
function UsageFoot({
  promptTokens,
  completionTokens,
  model,
  latencyMs,
}: {
  promptTokens?: number;
  completionTokens?: number;
  model?: string;
  latencyMs?: number;
}) {
  const parts = [
    model,
    promptTokens ? `Input ${promptTokens}` : '',
    completionTokens ? `Output ${completionTokens}` : '',
    latencyMs ? `${(latencyMs / 1000).toFixed(1)}s` : '',
  ].filter(Boolean);
  if (parts.length === 0) return null;
  return (
    <div className="mt-3 flex flex-wrap gap-x-3 gap-y-1 border-t border-smoke/60 pt-2.5 font-mono text-[10px] text-ash">
      {parts.map((p) => (
        <span key={p}>{p}</span>
      ))}
    </div>
  );
}

/**
 * Presentation contract shared by history and live adapters. Attachments are
 * optional so an ordinary text turn needs only role and content.
 */
interface BubbleProps {
  role: 'user' | 'assistant';
  content: string;
  reasoning?: string;
  refs?: Message['refs'];
  traceSteps?: TraceStep[];
  usage?: { promptTokens?: number; completionTokens?: number; model?: string };
  latencyMs?: number;
  streaming?: boolean;
  expert?: string;
}

/**
 * Render message content and optional attachments without reading the chat
 * store directly. Mount-only entry motion avoids replaying the bubble animation
 * for every streaming text fragment.
 */
export const MessageBubble = memo(function MessageBubble({
  role,
  content,
  reasoning,
  refs,
  traceSteps,
  usage,
  latencyMs,
  streaming,
  expert,
}: BubbleProps) {
  const isUser = role === 'user';
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = rootRef.current;
    if (el) bubbleIn(el);
  }, []);

  return (
    <div
      ref={rootRef}
      className={clsx('flex w-full items-start gap-2.5 sm:gap-3', isUser ? 'justify-end' : 'justify-start')}
    >
      {!isUser && <BrandMark className="mt-1 h-7 w-7 shrink-0" />}
      <div
        className={clsx(
          'message-bubble min-w-0 max-w-[90%] rounded-2xl border px-4 py-3.5 sm:max-w-[85%] sm:px-5',
          isUser
            ? 'message-bubble-user rounded-tr-md border-lime/15 bg-lime/[0.07]'
            : 'message-bubble-assistant rounded-tl-md border-smoke/70 bg-carbon',
        )}
      >
        {!isUser && <p className="mb-2 font-mono text-[11px] font-medium tracking-wide text-lime">SupaAgent</p>}
        {!isUser && expert && (
          <div className="mb-2 text-[10px] text-violet">⇢ Handled by {expert}</div>
        )}

        {content ? (
          isUser ? (
            <p className="whitespace-pre-wrap text-sm leading-7 break-words text-bone">{content}</p>
          ) : (
            <div className={streaming ? 'caret' : ''}>
              <Markdown content={content} />
            </div>
          )
        ) : (
          streaming && (
            <p className="text-sm text-fog">
              <span className="mr-2 inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-lime align-middle" />
              Thinking…
            </p>
          )
        )}

        {!isUser && reasoning && (
          <div className="mt-2">
            <Collapse title="Reasoning">{reasoning}</Collapse>
          </div>
        )}

        {!isUser && traceSteps && traceSteps.length > 0 && (
          <div className="mt-2">
            <TraceCollapse steps={traceSteps} />
          </div>
        )}

        {!isUser && refs && refs.length > 0 && (
          <div className="mt-2">
            <RefsList refs={refs} />
          </div>
        )}

        {!isUser && !streaming && (
          <UsageFoot
            promptTokens={usage?.promptTokens}
            completionTokens={usage?.completionTokens}
            model={usage?.model}
            latencyMs={latencyMs}
          />
        )}
      </div>
    </div>
  );
});

/**
 * Adapt a persisted message, including backend trace metadata and latency,
 * to the common presentation contract.
 */
export function HistoryBubble({ msg }: { msg: Message }) {
  const isUser = msg.role === 'user';
  return (
    <MessageBubble
      role={isUser ? 'user' : 'assistant'}
      content={msg.content}
      reasoning={msg.reasoning_content || undefined}
      refs={msg.refs ?? undefined}
      traceSteps={msg.metadata?.agent_trace}
      usage={
        isUser
          ? undefined
          : {
              promptTokens: msg.prompt_tokens,
              completionTokens: msg.completion_tokens,
              model: msg.model_name,
            }
      }
      latencyMs={msg.latency_ms}
    />
  );
}

/**
 * Adapt transient text, reasoning, citations, and tool-call state. Live tool
 * nodes are converted from the ID-keyed map; usage is shown when no longer streaming.
 */
export function StreamingBubble({ s }: { s: StreamingMsg }) {
  return (
    <MessageBubble
      role="assistant"
      content={s.content}
      reasoning={s.reasoning || undefined}
      refs={s.refs.length ? s.refs : undefined}
      traceSteps={streamingToSteps(s.tools)}
      usage={
        s.usage
          ? {
              promptTokens: s.usage.prompt_tokens,
              completionTokens: s.usage.completion_tokens,
              model: s.usage.model,
            }
          : undefined
      }
      streaming={s.status === 'streaming'}
      expert={s.expert}
    />
  );
}
