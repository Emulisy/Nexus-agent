/**
 * Transport for POST-based chat event streams. EventSource cannot send the
 * JSON request body, so fetch exposes a ReadableStream that is decoded manually.
 * Frames follow the backend format: event: <name>, data: <JSON>, then a blank line.
 * Partial UTF-8 sequences and partial frames survive across network reads.
 * This transport reports errors through onEvent; it does not refresh tokens,
 * reconnect, or replay the chat request automatically.
 */
import type { ChatReq, SSEEventType } from '@/types/chat';

/**
 * One parsed server frame. Payload interpretation belongs to the chat reducer,
 * so this transport exposes a generic object rather than branching on event names.
 */
export interface SSEEvent {
  event: SSEEventType;
  data: Record<string, unknown>;
}

/**
 * Control and completion signals for a single in-flight fetch.
 * Aborting cancels the client request; it does not call a separate server stop API.
 */
export interface StreamHandle {
  /** Cancel the underlying fetch/reader; server-side cleanup follows connection handling. */
  abort: () => void;
  /** Resolves after completion, failure, or cancellation. */
  done: Promise<void>;
}

/**
 * Start the background reader and return its cancellation/completion handle.
 * The caller supplies the current access token; an HTTP 401 becomes an error
 * event here, rather than entering the REST client token-refresh path.
 * onEvent receives complete JSON frames. done always resolves, including failure
 * and cancellation, so the store can reconcile persisted history in one place.
 */
export async function runChatStream(
  body: ChatReq,
  onEvent: (e: SSEEvent) => void,
  token: string,
): Promise<StreamHandle> {
  const controller = new AbortController();
  let resolveDone!: () => void;
  const done = new Promise<void>((r) => {
    resolveDone = r;
  });

  /**
   * Keep the reader detached from handle creation so callers can stop a request
   * while it is still waiting for headers. finally releases the store continuation.
   */
  void (async () => {
    try {
      const res = await fetch('/api/v1/chat/stream', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify(body),
        signal: controller.signal,
      });

      /**
       * Translate an unsuccessful HTTP handshake into the same error-event path
       * used by stream failures, preserving a backend message when available.
       */
      if (res.status !== 200) {
        let msg = `Connection failed (HTTP ${res.status})`;
        try {
          const env = await res.json();
          if (env?.message) msg = env.message;
        } catch {
          // Keep the HTTP error message when the response is not JSON.
        }
        onEvent({ event: 'error', data: { code: res.status, message: msg } });
        return;
      }

      const reader = res.body!.getReader();
      const decoder = new TextDecoder();
      let buf = '';
      /** A named done/error frame distinguishes normal EOF from an interrupted stream. */
      let terminal = false;

      for (;;) {
        const { done: eof, value } = await reader.read();
        if (eof) break;
        buf += decoder.decode(value, { stream: true });

        // Keep the incomplete frame for the next read.
        const frames = buf.split('\n\n');
        buf = frames.pop() ?? '';

        for (const frame of frames) {
          /**
           * The backend emits one event line and one JSON data line per frame. This
           * parser intentionally does not implement generic multiline/event-ID SSE features.
           */
          const event = frame.match(/^event: (.+)$/m)?.[1] ?? '';
          const raw = frame.match(/^data: (.+)$/m)?.[1] ?? '{}';
          if (!event) continue;
          if (event === 'done' || event === 'error') terminal = true;
          try {
            onEvent({
              event: event as SSEEventType,
              data: JSON.parse(raw) as Record<string, unknown>,
            });
          } catch {
            // Ignore malformed frames without interrupting the stream.
          }
        }
      }

      // EOF is unexpected unless a terminal event was received.
      if (!terminal) {
        onEvent({
          event: 'error',
          data: { code: -1, message: 'Connection interrupted. The response may be incomplete; please send your message again.' },
        });
      }
    } catch (e) {
      // Cancellation ends quietly; persisted messages remain authoritative.
      if (!(e instanceof DOMException && e.name === 'AbortError')) {
        onEvent({
          event: 'error',
          data: { code: -1, message: 'Network error. Please check the backend service.' },
        });
      }
    } finally {
      resolveDone();
    }
  })();

  return { abort: () => controller.abort(), done };
}
