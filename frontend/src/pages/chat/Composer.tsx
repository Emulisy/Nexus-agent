/**
 * Controlled message editor with model override selection and stream controls.
 * Enter submits trimmed content, Shift+Enter keeps a newline, and IME composition
 * is left intact. The editor is disabled while a streaming placeholder exists.
 * Model selection remains in the chat store until changed; it does not modify
 * the selected Agent configuration.
 */
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { clsx } from 'clsx';
import { listChatModels } from '@/api/chat';
import { Select } from '@/components/ui/primitives';
import { useChat } from '@/stores/chat';
import type { ChatModelItem } from '@/types/chat';

export function Composer({ disabled }: { disabled?: boolean }) {
  const { send, stop, streaming, activeModelId, setActiveModel } = useChat();
  const [value, setValue] = useState('');
  const [models, setModels] = useState<ChatModelItem[]>([]);
  const taRef = useRef<HTMLTextAreaElement>(null);
  /**
   * Keep entry disabled until the store removes its streaming placeholder,
   * including the interval between a terminal event and transport cleanup.
   */
  const busy = streaming !== null || disabled;

  /**
   * Load selectable chat models once for this mount. An unavailable catalog
   * falls back to the Agent-configured option instead of blocking text entry.
   */
  useEffect(() => {
    void listChatModels()
      .then(setModels)
      .catch(() => setModels([]));
  }, []);

  /**
   * Submit one nonempty turn and clear the local draft immediately. send()
   * adds the optimistic row and owns asynchronous stream/error state afterward.
   */
  const submit = () => {
    const text = value.trim();
    if (!text || busy) return;
    setValue('');
    void send(text);
  };

  /**
   * Preserve multiline editing and IME composition; only a plain Enter invokes
   * the same submit path as the send button.
   */
  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      // Do not submit while an IME composition is active.
      e.preventDefault();
      submit();
    }
  };

  return (
    <div className="composer-area shrink-0 px-4 pt-3 pb-4 lg:px-8 lg:pb-5">
      <div
        className={clsx(
          'composer-shell mx-auto w-full max-w-3xl rounded-2xl border p-3 transition-all duration-200',
          busy ? 'border-smoke/60' : 'border-smoke focus-within:border-lime/50 focus-within:ring-4 focus-within:ring-lime/5',
        )}
      >
        <textarea
          ref={taRef}
          rows={2}
          value={value}
          disabled={busy}
          placeholder={busy ? 'Generating a response…' : 'Type a message to start a conversation…'}
          aria-label="Chat message"
          className="composer-input max-h-40 min-h-16 w-full resize-none bg-transparent px-2 py-2 text-sm leading-6 text-bone outline-none placeholder:text-ash disabled:opacity-50"
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={onKeyDown}
          onInput={(e) => {
            const el = e.currentTarget;
            el.style.height = 'auto';
            el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
          }}
        />
        <div className="mt-2 flex flex-wrap items-center justify-between gap-3 border-t border-smoke/50 pt-3">
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
            <label htmlFor="chat-model" className="shrink-0 pl-1 text-[11px] text-ash">Model</label>
            <Select
              id="chat-model"
              className="composer-model h-8 w-auto min-w-0 max-w-full flex-1 py-0 text-xs sm:max-w-64 sm:flex-none"
              value={activeModelId ?? ''}
              disabled={busy}
              onChange={(e) => {
                const v = e.target.value;
                setActiveModel(v === '' ? undefined : Number(v));
              }}
            >
              <option value="">Use agent model</option>
              {models.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.label} · {m.provider}
                  {m.is_default ? ' (default)' : ''}
                </option>
              ))}
            </Select>
          </div>
          {streaming ? (
            <button
              className="btn-danger h-9 shrink-0 px-3"
              onClick={stop}
              title="Stop generating"
            >
              <span aria-hidden="true" className="h-2.5 w-2.5 rounded-[2px] bg-current" />
              Stop
            </button>
          ) : (
            <button
              className="composer-send btn-primary h-9 shrink-0 px-4"
              disabled={!value.trim()}
              onClick={submit}
            >
              Send
              <svg aria-hidden="true" className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 19V5m-6 6 6-6 6 6" />
              </svg>
            </button>
          )}
        </div>
      </div>
      <div className="composer-help mx-auto mt-2.5 flex max-w-3xl flex-wrap items-center justify-between gap-1 px-1 text-[10px] leading-5 text-ash">
        <span>{models.length === 0 ? 'No models available. Add one in Models.' : 'Your next message will use the selected model.'}</span>
        <span><kbd>Enter</kbd> Send · <kbd>Shift + Enter</kbd> new line</span>
      </div>
    </div>
  );
}
