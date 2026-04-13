/**
 * Process-wide singleton for the SSE `/api/stream` connection.
 *
 * One EventSource, many subscribers. The connection opens lazily on the
 * first `subscribeSSE()` call and stays open for the life of the page,
 * reconnecting with exponential backoff on error. The subscription
 * lifetime is decoupled from component lifecycle: stores subscribe
 * once at app startup, components can subscribe inside an effect if
 * they need per-view reactions (but prefer store-level handling so
 * data stays consistent across unmounts).
 */

/** Event shape matching the server-side SSEEvent contract. */
export interface SSEEvent {
  type: string;
  [key: string]: unknown;
}

export type SSEHandler = (event: SSEEvent) => void;

const INITIAL_RECONNECT_MS = 1_000;
const MAX_RECONNECT_MS = 10_000;

const subscribers = new Set<SSEHandler>();
let es: EventSource | null = null;
let reconnectTimer: number | undefined;
let reconnectDelay = INITIAL_RECONNECT_MS;

function connect(): void {
  es = new EventSource("/api/stream");

  es.onopen = () => {
    reconnectDelay = INITIAL_RECONNECT_MS;
  };

  es.onmessage = (ev) => {
    try {
      const data = JSON.parse(ev.data) as SSEEvent;
      subscribers.forEach((h) => h(data));
    } catch {
      // Malformed frame — ignore and keep the stream alive.
    }
  };

  es.onerror = () => {
    // EventSource auto-reconnects, but only when previously open. Force
    // a fresh close-then-retry with exponential backoff so we don't spin
    // against a down server.
    es?.close();
    es = null;
    reconnectTimer = window.setTimeout(() => {
      reconnectTimer = undefined;
      connect();
    }, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_MS);
  };
}

/**
 * Subscribe to SSE events. The first subscriber opens the connection;
 * subsequent subscribers share it. Returns an unsubscribe function.
 * The connection itself is never closed by unsubscribe — it lives for
 * the tab's lifetime, which matches every real use case.
 */
export function subscribeSSE(handler: SSEHandler): () => void {
  if (!es && reconnectTimer === undefined) {
    connect();
  }
  subscribers.add(handler);
  return () => {
    subscribers.delete(handler);
  };
}

/** Test-only: reset the bus so unit tests get a clean slate. */
export function __resetSSEBusForTests(): void {
  subscribers.clear();
  if (reconnectTimer !== undefined) {
    window.clearTimeout(reconnectTimer);
    reconnectTimer = undefined;
  }
  es?.close();
  es = null;
  reconnectDelay = INITIAL_RECONNECT_MS;
}
