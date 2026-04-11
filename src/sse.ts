/**
 * Server-Sent Events broadcaster.
 *
 * Keeps a set of live `ReadableStreamDefaultController`s — one per HTTP
 * client connected to `/api/stream` — and lets callers fan out JSON events
 * to all of them in a single pass.
 *
 * The broadcaster is deliberately tiny: it knows nothing about HTTP, about
 * Deno.serve, or about the watcher. Wiring happens in `ui.ts`, which plugs
 * an instance into both the SSE request handler (adds clients) and the
 * watcher callback (calls `broadcast`).
 */

export type SSEEvent = { type: string } & Record<string, unknown>;

type Controller = ReadableStreamDefaultController<Uint8Array>;

export class SSEBroadcaster {
  private clients = new Set<Controller>();
  private encoder = new TextEncoder();

  /** Register a new SSE client (called by the stream's `start` callback). */
  addClient(controller: Controller): void {
    this.clients.add(controller);
  }

  /** Remove a client (called by the stream's `cancel` callback or on broadcast error). */
  removeClient(controller: Controller): void {
    this.clients.delete(controller);
  }

  /** Number of currently connected clients. */
  clientCount(): number {
    return this.clients.size;
  }

  /**
   * Fan `event` out to every connected client as a single `data: ...\n\n`
   * frame. Clients whose controller throws on enqueue (typically because
   * the socket was closed between ticks) are dropped silently.
   */
  broadcast(event: SSEEvent): void {
    if (this.clients.size === 0) return;
    const payload = this.encoder.encode(`data: ${JSON.stringify(event)}\n\n`);
    for (const client of this.clients) {
      try {
        client.enqueue(payload);
      } catch {
        // controller already closed — drop it
        this.clients.delete(client);
      }
    }
  }

  /** Send a raw SSE comment line (used for keep-alive pings). */
  ping(controller: Controller): void {
    try {
      controller.enqueue(this.encoder.encode(":ping\n\n"));
    } catch {
      this.clients.delete(controller);
    }
  }

  /** Close all clients. Called during server shutdown. */
  closeAll(): void {
    for (const client of this.clients) {
      try {
        client.close();
      } catch {
        // already closed
      }
    }
    this.clients.clear();
  }
}
