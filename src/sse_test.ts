import {
  assertEquals,
  assertStringIncludes,
} from "https://deno.land/std@0.224.0/assert/mod.ts";
import { SSEBroadcaster } from "./sse.ts";

/**
 * Collect every chunk enqueued into a mock controller.
 */
function mockController() {
  const chunks: Uint8Array[] = [];
  let closed = false;
  const controller = {
    enqueue(chunk: Uint8Array) {
      if (closed) throw new TypeError("already closed");
      chunks.push(chunk);
    },
    close() {
      closed = true;
    },
    error(_reason?: unknown) {
      closed = true;
    },
    get desiredSize() {
      return 1;
    },
  } as unknown as ReadableStreamDefaultController<Uint8Array>;

  return {
    controller,
    text: () => new TextDecoder().decode(concat(chunks)),
    close: () => {
      closed = true;
    },
    isClosed: () => closed,
  };
}

function concat(chunks: Uint8Array[]): Uint8Array {
  const total = chunks.reduce((n, c) => n + c.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const c of chunks) {
    out.set(c, offset);
    offset += c.length;
  }
  return out;
}

Deno.test("SSEBroadcaster starts with zero clients", () => {
  const b = new SSEBroadcaster();
  assertEquals(b.clientCount(), 0);
});

Deno.test("addClient tracks controllers", () => {
  const b = new SSEBroadcaster();
  const { controller } = mockController();
  b.addClient(controller);
  assertEquals(b.clientCount(), 1);

  b.removeClient(controller);
  assertEquals(b.clientCount(), 0);
});

Deno.test("broadcast writes one data frame per client", () => {
  const b = new SSEBroadcaster();
  const a = mockController();
  const c = mockController();
  b.addClient(a.controller);
  b.addClient(c.controller);

  b.broadcast({ type: "session_updated", sessionId: "s1" });

  assertStringIncludes(a.text(), `data: {"type":"session_updated","sessionId":"s1"}`);
  assertStringIncludes(a.text(), "\n\n");
  assertStringIncludes(c.text(), `data: {"type":"session_updated","sessionId":"s1"}`);
});

Deno.test("broadcast drops clients whose enqueue throws", () => {
  const b = new SSEBroadcaster();
  const good = mockController();
  const bad = mockController();
  // Sabotage `bad` so enqueue throws.
  bad.close();

  b.addClient(good.controller);
  b.addClient(bad.controller);

  b.broadcast({ type: "hello" });

  assertEquals(b.clientCount(), 1);
  assertStringIncludes(good.text(), `"type":"hello"`);
});

Deno.test("broadcast is a no-op when there are no clients", () => {
  const b = new SSEBroadcaster();
  // Should not throw.
  b.broadcast({ type: "noop" });
  assertEquals(b.clientCount(), 0);
});

Deno.test("ping writes a comment line", () => {
  const b = new SSEBroadcaster();
  const { controller, text } = mockController();
  b.addClient(controller);

  b.ping(controller);

  assertStringIncludes(text(), ":ping\n\n");
});

Deno.test("closeAll clears all clients", () => {
  const b = new SSEBroadcaster();
  b.addClient(mockController().controller);
  b.addClient(mockController().controller);
  b.addClient(mockController().controller);
  assertEquals(b.clientCount(), 3);

  b.closeAll();
  assertEquals(b.clientCount(), 0);
});

Deno.test("SSE frame for complex event is valid JSON", () => {
  const b = new SSEBroadcaster();
  const { controller, text } = mockController();
  b.addClient(controller);

  b.broadcast({
    type: "session_updated",
    sessionId: "abc-123",
    project: "my-proj",
    addedMessages: 2,
    totalMessages: 10,
  });

  // Extract the data payload and parse it.
  const raw = text();
  const match = raw.match(/^data: (.+)\n\n$/);
  assertEquals(match !== null, true);
  const payload = JSON.parse(match![1]);
  assertEquals(payload.type, "session_updated");
  assertEquals(payload.sessionId, "abc-123");
  assertEquals(payload.addedMessages, 2);
});
