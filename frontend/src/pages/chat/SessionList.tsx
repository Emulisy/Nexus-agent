/**
 * Server-backed conversation navigation with inline renaming and confirmed
 * deletion. Mount loads the first 100 sessions. Selecting a session delegates
 * stream cancellation and history loading to the chat store; creating a new
 * conversation clears local context before the first message creates it remotely.
 */
import { useEffect, useRef, useState } from 'react';
import { clsx } from 'clsx';
import { relativeTime } from '@/lib/format';
import { useChat } from '@/stores/chat';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { Icon } from '@/components/ui/Icon';

export function SessionList() {
  const {
    sessions,
    activeSessionId,
    selectSession,
    newSession,
    renameSession,
    removeSession,
    loadSessions,
  } = useChat();

  const [editingId, setEditingId] = useState<string | null>(null);
  const [draft, setDraft] = useState('');
  const [delId, setDelId] = useState<string | null>(null);
  const listRef = useRef<HTMLDivElement>(null);

  /**
   * Refresh session summaries on mount rather than persisting this list in
   * localStorage; the server remains authoritative across reloads.
   */
  useEffect(() => {
    void loadSessions();
  }, [loadSessions]);

  /**
   * Persist a trimmed nonempty title, then leave inline editing. Blank edits
   * exit without changing the server title.
   */
  const commitRename = async () => {
    if (editingId && draft.trim()) {
      await renameSession(editingId, draft.trim());
    }
    setEditingId(null);
  };

  return (
    <div className="session-rail flex max-h-44 w-full shrink-0 flex-col border-b border-smoke/70 md:h-full md:max-h-none md:w-48 md:border-r md:border-b-0 lg:w-56">
      <div className="px-4 pt-5 pb-4">
        <button
          className="new-session-btn btn-primary w-full justify-center py-2.5 text-sm"
          onClick={newSession}
        >
          <svg aria-hidden="true" className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
            <path d="M12 5v14M5 12h14" />
          </svg>
          New chat
        </button>
      </div>

      <div className="hidden items-center justify-between px-5 pt-2 pb-3 md:flex"><p className="section-title">Conversations</p><span className="font-mono text-[10px] text-ash">{String(sessions.length).padStart(2, '0')}</span></div>

      <div ref={listRef} className="min-h-0 flex-1 space-y-1 overflow-y-auto px-3 pb-4">
        {sessions.map((s) => {
          const active = s.id === activeSessionId;
          return (
            <div
              key={s.id}
              className={clsx(
                'session-item group cursor-pointer rounded-xl border px-3 py-3 transition-all duration-200',
                active ? 'border-lime/15 bg-lime/[0.07] text-lime' : 'border-transparent text-fog hover:bg-graphite',
              )}
              onClick={() => editingId !== s.id && void selectSession(s.id)}
            >
              {editingId === s.id ? (
                <input
                  autoFocus
                  className="field h-7 px-2 py-0 text-xs"
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                  onBlur={commitRename}
                  onKeyDown={(e) => e.key === 'Enter' && void commitRename()}
                  onClick={(e) => e.stopPropagation()}
                />
              ) : (
                <div className="flex items-center gap-2">
                  <div className="min-w-0 flex-1">
                    <p className={clsx('truncate text-sm leading-5', active && 'font-medium')}>{s.title}</p>
                    <p className="mt-1.5 font-mono text-[10px] text-ash">
                      {s.message_count} {s.message_count === 1 ? 'message' : 'messages'} · {relativeTime(s.last_msg_at ?? s.created_at)}
                    </p>
                  </div>
                  <div className="flex shrink-0 gap-0.5 transition-opacity md:opacity-0 md:group-hover:opacity-100 md:group-focus-within:opacity-100">
                    <button
                      className="rounded-md p-1 text-ash transition-colors hover:bg-lime/10 hover:text-lime"
                      title="Rename"
                      aria-label="Rename conversation"
                      onClick={(e) => {
                        e.stopPropagation();
                        setEditingId(s.id);
                        setDraft(s.title);
                      }}
                    >
                      <svg aria-hidden="true" className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
                        <path d="m15 5 4 4M4 20l5-1L20 8a2.8 2.8 0 0 0-4-4L5 15l-1 5Z" />
                      </svg>
                    </button>
                    <button
                      className="rounded-md p-1 text-ash transition-colors hover:bg-coral/10 hover:text-coral"
                      title="Delete"
                      aria-label="Delete conversation"
                      onClick={(e) => {
                        e.stopPropagation();
                        setDelId(s.id);
                      }}
                    >
                      <svg aria-hidden="true" className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M5 7h14M9 7V4h6v3M7 7l1 13h8l1-13M10 10v7M14 10v7" />
                      </svg>
                    </button>
                  </div>
                </div>
              )}
            </div>
          );
        })}
        {sessions.length === 0 && (
          <div className="session-empty px-3 py-8 text-center text-xs text-ash"><Icon name="chat" className="mx-auto mb-3 h-6 w-6 text-smoke" /><p>No conversations yet</p></div>
        )}
      </div>

      <ConfirmDialog
        open={delId !== null}
        title="Delete conversation"
        content="Delete this conversation and all its messages? This cannot be undone."
        danger
        confirmText="Delete"
        onCancel={() => setDelId(null)}
        onConfirm={async () => {
          if (delId) await removeSession(delId);
          setDelId(null);
        }}
      />
    </div>
  );
}
