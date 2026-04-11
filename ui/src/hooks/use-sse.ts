import { useEffect, useRef } from "preact/hooks";

/** Event shape matching the server-side SSEEvent contract. */
export interface SSEEvent {
  type: string;
  [key: string]: unknown;
}

export type SSEHandler = (event: SSEEvent) => void;

const INITIAL_RECONNECT_MS = 1_000;
const MAX_RECONNECT_MS = 10_000;

/**
 * Subscribe to the UI server's `/api/stream` Server-Sent Events feed.
 *
 * The returned hook keeps a single `EventSource` alive for the component
 * lifetime, reconnects with exponential backoff (1s → 2s → 4s → 8s, capped
 * at 10s) when the socket drops, and forwards parsed JSON payloads to the
 * caller's handler. The handler reference is captured in a ref so callers
 * can use inline arrow functions without forcing a reconnect on every
 * render.
 */
export function useSSE(handler: SSEHandler): void {
  const handlerRef = useRef<SSEHandler>(handler);
  handlerRef.current = handler;

  useEffect(() => {
    let es: EventSource | null = null;
    let reconnectTimer: number | undefined;
    let reconnectDelay = INITIAL_RECONNECT_MS;
    let cancelled = false;

    const connect = () => {
      if (cancelled) return;

      es = new EventSource("/api/stream");

      es.onopen = () => {
        reconnectDelay = INITIAL_RECONNECT_MS;
      };

      es.onmessage = (ev) => {
        if (cancelled) return;
        try {
          const data = JSON.parse(ev.data) as SSEEvent;
          handlerRef.current(data);
        } catch {
          // Malformed frame — ignore and keep the stream alive.
        }
      };

      es.onerror = () => {
        // EventSource auto-reconnects on its own, but only when it was
        // previously open. For server restarts and initial-connect failures
        // we force a fresh close-then-retry with exponential backoff so we
        // don't spin against a down server.
        es?.close();
        es = null;
        if (cancelled) return;

        reconnectTimer = window.setTimeout(() => {
          reconnectTimer = undefined;
          connect();
        }, reconnectDelay);
        reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_MS);
      };
    };

    connect();

    return () => {
      cancelled = true;
      if (reconnectTimer !== undefined) {
        window.clearTimeout(reconnectTimer);
      }
      es?.close();
      es = null;
    };
  }, []);
}
