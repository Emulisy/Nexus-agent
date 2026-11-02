/**
 * Manage conversation history and the transient assistant response.
 * The server owns persisted messages; optimistic user messages and SSE state are
 * replaced by a fresh history fetch when the stream settles. Only one active
 * stream is allowed in this store, and its AbortController handle stays outside
 * reactive state. Agent and model selections persist until explicitly changed.
 */
import { create } from 'zustand';
import { runChatStream, type StreamHandle } from '@/lib/sse';
import { deleteSession, listMessages, listSessions, renameSession as renameApi } from '@/api/chat';
import { toast } from '@/stores/ui';
import { useAuth } from '@/stores/auth';
import type {
  AgentEventData,
  Message,
  PlanData,
  RAGRef,
  Session,
  ToolCallData,
  ToolResultData,
  UsageData,
} from '@/types/chat';

export interface StreamingMsg {
  sessionId: string;
  content: string;
  reasoning: string;
  plan?: PlanData;
  /** Match tool calls and results by ID. */
  tools: Record<string, ToolCallData & Partial<ToolResultData>>;
  /**
   * Delegation/progress payloads retained for state consumers; the current chat
   * view does not render a dedicated panel for these events.
   */
  agentEvents: AgentEventData[];
  refs: RAGRef[];
  usage?: UsageData;
  status: 'streaming' | 'done' | 'error';
  errorMessage?: string;
  expert?: string;
}

interface ChatState {
  sessions: Session[];
  sessionsTotal: number;
  /** Undefined starts a new session. */
  activeSessionId?: string;
  messages: Message[];
  streaming: StreamingMsg | null;
  /** Undefined uses the platform default. */
  activeAgentId?: number;
  /** Undefined uses the agent's configured model. */
  activeModelId?: number;

  loadSessions: () => Promise<void>;
  selectSession: (id: string) => Promise<void>;
  /**
   * Clear conversation context without creating a server record yet. The next
   * send omits session_id, and its meta event supplies the newly created ID.
   */
  newSession: () => void;
  setActiveAgent: (id?: number) => void;
  setActiveModel: (id?: number) => void;
  send: (content: string) => Promise<void>;
  /**
   * Cancel the active client fetch. The send continuation still runs after done
   * resolves and reconciles whatever messages the backend has persisted.
   */
  stop: () => void;
  renameSession: (id: string, title: string) => Promise<void>;
  removeSession: (id: string) => Promise<void>;
}

let handle: StreamHandle | null = null;

export const useChat = create<ChatState>((set, get) => ({
  sessions: [],
  sessionsTotal: 0,
  activeSessionId: undefined,
  messages: [],
  streaming: null,
  activeAgentId: undefined,
  activeModelId: undefined,

  /**
   * Refresh the first page of server session summaries. This store keeps the
   * matching total even though the current navigation rail renders at most 100 rows.
   */
  loadSessions: async () => {
    const page = await listSessions({ page: 1, page_size: 100 });
    set({ sessions: page.list, sessionsTotal: page.total });
  },

  /**
   * Abort a current stream, clear its transient bubble, and load the selected
   * session history and restore its saved agent. A model override from another
   * conversation must not silently replace the restored agent's model.
   */
  selectSession: async (id) => {
    if (get().streaming) get().stop();
    const session = get().sessions.find((s) => s.id === id);
    set({ activeSessionId: id, activeAgentId: session?.agent_id ?? undefined, activeModelId: undefined, messages: [], streaming: null });
    const msgs = await listMessages(id);
    set({ messages: msgs });
  },

  newSession: () => {
    if (get().streaming) get().stop();
    set({ activeSessionId: undefined, messages: [], streaming: null });
  },

  // A different agent starts a separate conversation with its configured model.
  setActiveAgent: (id) => {
    if (get().streaming || get().activeAgentId === id) return;
    set({ activeAgentId: id, activeModelId: undefined, activeSessionId: undefined, messages: [] });
  },

  setActiveModel: (id) => set({ activeModelId: id }),

  /**
   * Append an optimistic user message and start one transient assistant response.
   * Only the new content and optional selection IDs are sent; the server owns
   * history/context assembly. Once the reader settles, reload persisted messages
   * and session summaries instead of treating streamed text as a saved record.
   */
  send: async (content) => {
    if (get().streaming) return;

    /**
     * Temporary IDs are negative so the local row stays distinct from persisted
     * positive message IDs until history is fetched again.
     */
    const optimistic: Message = {
      id: -Date.now(),
      session_id: get().activeSessionId ?? '',
      role: 'user',
      content,
      prompt_tokens: 0,
      completion_tokens: 0,
      first_token_ms: 0,
      latency_ms: 0,
      status: 1,
      created_at: new Date().toISOString(),
    };
    set((s) => ({ messages: [...s.messages, optimistic], streaming: emptyStreaming() }));

    /**
     * Snapshot the token for the raw streaming fetch; it does not use the REST
     * client automatic 401 refresh and replay path.
     */
    const token = useAuth.getState().accessToken ?? '';
    const h = await runChatStream(
      { session_id: get().activeSessionId, agent_id: get().activeAgentId, content, model_config_id: get().activeModelId },
      (ev) => {
        set((s) => (s.streaming ? { streaming: reduce(s.streaming, ev.event, ev.data) } : s));
      },
      token,
    );
    handle = h;
    await h.done;

    // Reload persisted messages after the stream ends.
    const finalStatus = get().streaming?.status;
    if (finalStatus === 'error') {
      toast.err(get().streaming?.errorMessage ?? 'Chat failed');
    }
    handle = null;
    // The first response supplies the server-assigned session ID.
    const sid = get().activeSessionId ?? get().streaming?.sessionId;
    set({ streaming: null, ...(sid ? { activeSessionId: sid } : {}) });
    if (sid) {
      const [msgs, sessions] = await Promise.all([
        listMessages(sid),
        listSessions({ page: 1, page_size: 100 }),
      ]);
      set({ messages: msgs, sessions: sessions.list, sessionsTotal: sessions.total });
    } else {
      await get().loadSessions();
    }
  },

  stop: () => {
    handle?.abort();
  },

  /** Patch the visible summary only after the server accepts the new title. */
  renameSession: async (id, title) => {
    await renameApi(id, title);
    set((s) => ({
      sessions: s.sessions.map((x) => (x.id === id ? { ...x, title } : x)),
    }));
  },

  /**
   * Delete remotely before removing the summary. Deleting the active session
   * also clears its current message list and selects the new-conversation state.
   */
  removeSession: async (id) => {
    await deleteSession(id);
    set((s) => ({
      sessions: s.sessions.filter((x) => x.id !== id),
      activeSessionId: s.activeSessionId === id ? undefined : s.activeSessionId,
      messages: s.activeSessionId === id ? [] : s.messages,
    }));
  },
}));

/**
 * Create an assistant placeholder before the first network frame arrives.
 * Its empty sessionId is filled by meta for a newly created conversation.
 */
function emptyStreaming(): StreamingMsg {
  return {
    sessionId: '',
    content: '',
    reasoning: '',
    tools: {},
    agentEvents: [],
    refs: [],
    status: 'streaming',
  };
}

/**
 * Apply one SSE event without mutating previous store values. Tool calls and
 * results join by ID, text fragments append, and terminal events only change
 * status; the transport still controls when the completion promise settles.
 * Unknown events remain harmless so the protocol can grow independently.
 */
function reduce(
  cur: StreamingMsg,
  event: string,
  data: Record<string, unknown>,
): StreamingMsg {
  switch (event) {
    case 'meta':
      return {
        ...cur,
        sessionId: String(data.session_id ?? ''),
        expert: data.expert ? String(data.expert) : undefined,
      };

    case 'delta':
      return { ...cur, content: cur.content + String(data.content ?? '') };

    case 'reasoning':
      return { ...cur, reasoning: cur.reasoning + String(data.content ?? '') };

    case 'plan':
      return { ...cur, plan: data as unknown as PlanData };

    case 'tool_call': {
      const tc = data as unknown as ToolCallData;
      return { ...cur, tools: { ...cur.tools, [tc.id]: { ...tc } } };
    }

    case 'tool_result': {
      const tr = data as unknown as ToolResultData;
      const node = cur.tools[tr.id];
      /** A result without a known call cannot update an existing timeline node. */
      if (!node) return cur;
      return {
        ...cur,
        tools: { ...cur.tools, [tr.id]: { ...node, ...tr } },
      };
    }

    case 'agent_event':
      return { ...cur, agentEvents: [...cur.agentEvents, data as unknown as AgentEventData] };

    case 'refs':
      return { ...cur, refs: (data.items as RAGRef[]) ?? [] };

    case 'usage':
      return { ...cur, usage: data as unknown as UsageData };

    /**
     * A terminal event updates presentation immediately. Stream cleanup and the
     * history reload wait for the reader completion promise.
     */
    case 'done':
      return { ...cur, status: 'done' };

    case 'error':
      return {
        ...cur,
        status: 'error',
        errorMessage: String(data.message ?? 'Chat failed'),
      };

    default:
      return cur; // Ignore unknown events for protocol compatibility.
  }
}
