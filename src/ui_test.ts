import { assertEquals } from "https://deno.land/std@0.224.0/assert/mod.ts";
import { VaultDB } from "./db.ts";
import { runUI } from "./ui.ts";

const TEST_PORT = 16276;
let db: VaultDB;
let ac: AbortController;

function setup(): void {
  db = new VaultDB(":memory:");
  // Seed test data
  db.insertSession({
    sessionId: "s1",
    project: "-Users-test-src-myproject",
    projectPath: "/Users/test/src/myproject",
    gitBranch: "main",
    firstPrompt: "hello world",
    messageCount: 2,
    startedAt: "2026-01-01T00:00:00Z",
    endedAt: "2026-01-01T00:10:00Z",
    claudeVersion: "2.1.87",
  });
  db.insertMessage({ sessionId: "s1", uuid: "m1", role: "user", content: "terraform state migration", timestamp: "2026-01-01T00:00:00Z", turnIndex: 0 });
  db.insertMessage({ sessionId: "s1", uuid: "m2", role: "assistant", content: "Here is the plan", timestamp: "2026-01-01T00:00:01Z", turnIndex: 1 });
  db.insertImage({ sessionId: "s1", messageUuid: "m1", imageIndex: 0, mediaType: "image/png", data: new TextEncoder().encode("fake-png") });
}

async function startServer(): Promise<void> {
  ac = new AbortController();
  Deno.serve({ port: TEST_PORT, signal: ac.signal, onListen: () => {} }, (req) => {
    const url = new URL(req.url);
    if (url.pathname.startsWith("/api/")) {
      return handleTestAPI(db, url, req);
    }
    return new Response("Not Found", { status: 404 });
  });
  // Wait for server
  await new Promise((r) => setTimeout(r, 100));
}

// Inline a simplified handleAPI for testing (avoids ui_assets dependency)
function handleTestAPI(db: VaultDB, url: URL, req: Request): Response {
  const path = url.pathname;
  const json = (data: unknown) => new Response(JSON.stringify(data), { headers: { "Content-Type": "application/json" } });

  if (req.method === "POST" && path === "/api/shutdown") {
    return new Response(null, { status: 202 });
  }

  if (path === "/api/status") {
    return json({ status: "running", pid: Deno.pid, port: TEST_PORT });
  }

  if (path === "/api/search") {
    const query = url.searchParams.get("q") ?? "";
    if (!query) return json([]);
    const results = db.search(query, {
      project: url.searchParams.get("project") ?? undefined,
      limit: Number(url.searchParams.get("limit") ?? 50),
    });
    return json(results);
  }

  if (path === "/api/sessions") {
    const sessions = db.listSessions({
      project: url.searchParams.get("project") ?? undefined,
      limit: Number(url.searchParams.get("limit") ?? 100),
    });
    return json(sessions);
  }

  const sessionMatch = path.match(/^\/api\/sessions\/(.+)$/);
  if (sessionMatch) {
    return json(db.exportSession(sessionMatch[1]));
  }

  if (path === "/api/stats") {
    return json(db.stats());
  }

  if (path === "/api/image") {
    const sid = url.searchParams.get("session") ?? "";
    const muuid = url.searchParams.get("message") ?? "";
    const idx = Number(url.searchParams.get("index") ?? 0);
    const img = db.getImage(sid, muuid, idx);
    if (!img) return new Response("Not Found", { status: 404 });
    return new Response(img.data as unknown as BodyInit, { headers: { "Content-Type": img.mediaType } });
  }

  return new Response("Not Found", { status: 404 });
}

function teardown(): void {
  ac.abort();
  db.close();
}

async function fetchJSON(path: string): Promise<unknown> {
  const resp = await fetch(`http://localhost:${TEST_PORT}${path}`);
  return resp.json();
}

// --- API Tests ---

Deno.test({
  name: "API: /api/status returns running",
  async fn() {
    setup();
    await startServer();
    try {
      const data = await fetchJSON("/api/status") as { status: string };
      assertEquals(data.status, "running");
    } finally {
      teardown();
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "API: /api/sessions returns session list",
  async fn() {
    setup();
    await startServer();
    try {
      const data = await fetchJSON("/api/sessions") as Array<{ sessionId: string }>;
      assertEquals(data.length, 1);
      assertEquals(data[0].sessionId, "s1");
    } finally {
      teardown();
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "API: /api/sessions/:id returns session with messages",
  async fn() {
    setup();
    await startServer();
    try {
      const data = await fetchJSON("/api/sessions/s1") as { session: { sessionId: string }; messages: Array<{ role: string }> };
      assertEquals(data.session.sessionId, "s1");
      assertEquals(data.messages.length, 2);
      assertEquals(data.messages[0].role, "user");
    } finally {
      teardown();
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "API: /api/search returns matching results",
  async fn() {
    setup();
    await startServer();
    try {
      const data = await fetchJSON("/api/search?q=terraform") as Array<{ content: string }>;
      assertEquals(data.length, 1);
    } finally {
      teardown();
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "API: /api/search with empty query returns empty array",
  async fn() {
    setup();
    await startServer();
    try {
      const data = await fetchJSON("/api/search?q=") as unknown[];
      assertEquals(data.length, 0);
    } finally {
      teardown();
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "API: /api/search handles hyphens safely",
  async fn() {
    setup();
    await startServer();
    try {
      const resp = await fetch(`http://localhost:${TEST_PORT}/api/search?q=43968160-3681`);
      assertEquals(resp.ok, true);
    } finally {
      teardown();
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "API: /api/stats returns aggregates",
  async fn() {
    setup();
    await startServer();
    try {
      const data = await fetchJSON("/api/stats") as { totalSessions: number };
      assertEquals(data.totalSessions, 1);
    } finally {
      teardown();
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "API: /api/image returns image data",
  async fn() {
    setup();
    await startServer();
    try {
      const resp = await fetch(`http://localhost:${TEST_PORT}/api/image?session=s1&message=m1&index=0`);
      assertEquals(resp.ok, true);
      assertEquals(resp.headers.get("content-type"), "image/png");
      const body = await resp.text();
      assertEquals(body, "fake-png");
    } finally {
      teardown();
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "API: /api/image returns 404 for missing image",
  async fn() {
    setup();
    await startServer();
    try {
      const resp = await fetch(`http://localhost:${TEST_PORT}/api/image?session=s1&message=m1&index=99`);
      assertEquals(resp.status, 404);
    } finally {
      teardown();
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "API: /api/sessions/:id returns null for nonexistent",
  async fn() {
    setup();
    await startServer();
    try {
      const data = await fetchJSON("/api/sessions/nonexistent") as { session: null };
      assertEquals(data.session, null);
    } finally {
      teardown();
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

// ---------------------------------------------------------------------------
// runUI integration smoke tests
// ---------------------------------------------------------------------------
//
// Unlike the tests above, these exercise the real `runUI` entry point: a
// real Deno.HttpServer, the real /api/stream handler, and the real shutdown
// path. The watcher is disabled (`projectsDir: null`) so these tests don't
// touch the host's ~/.claude/projects.

async function runUIHandleFor(): Promise<
  import("./ui.ts").UIHandle
> {
  const { runUI } = await import("./ui.ts");
  return runUI({
    dbPath: ":memory:",
    port: 0, // let the OS pick a free port
    projectsDir: null, // disable the FS watcher for integration tests
  });
}

function handleAddr(handle: import("./ui.ts").UIHandle): string {
  const addr = handle.server.addr as Deno.NetAddr;
  return `http://localhost:${addr.port}`;
}

Deno.test({
  name: "runUI: /api/status returns running with pid and port",
  async fn() {
    const handle = await runUIHandleFor();
    try {
      const resp = await fetch(`${handleAddr(handle)}/api/status`);
      assertEquals(resp.status, 200);
      const data = await resp.json() as { status: string; pid: number; port: number };
      assertEquals(data.status, "running");
      assertEquals(typeof data.pid, "number");
      assertEquals(data.port, (handle.server.addr as Deno.NetAddr).port);
    } finally {
      await handle.shutdown();
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "runUI: /api/stream opens an SSE connection with a connected frame",
  async fn() {
    const handle = await runUIHandleFor();
    try {
      const resp = await fetch(`${handleAddr(handle)}/api/stream`);
      try {
        assertEquals(resp.status, 200);
        assertEquals(resp.headers.get("content-type"), "text/event-stream");

        // Read exactly one chunk and verify it carries the initial
        // `connected` event. We don't drain the rest of the stream — the
        // cancel() below detaches us cleanly.
        const reader = resp.body!.getReader();
        const { value } = await reader.read();
        const frame = new TextDecoder().decode(value);
        assertEquals(frame.startsWith("data: "), true);
        const payload = JSON.parse(frame.slice("data: ".length).trimEnd());
        assertEquals(payload.type, "connected");

        await reader.cancel();
      } finally {
        // Ensure the body is released even if an assertion threw above.
        try {
          await resp.body?.cancel();
        } catch {
          // already cancelled
        }
      }
    } finally {
      await handle.shutdown();
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "runUI: broadcaster push reaches /api/stream subscribers",
  async fn() {
    const handle = await runUIHandleFor();
    try {
      const resp = await fetch(`${handleAddr(handle)}/api/stream`);
      const reader = resp.body!.getReader();

      // Consume the initial `connected` frame so the next read is the one
      // triggered by our broadcast.
      await reader.read();

      // Push an event through the broadcaster and verify the subscribed
      // client sees it on the wire.
      handle.broadcaster.broadcast({
        type: "session_updated",
        sessionId: "integration-test",
        project: "demo",
        status: "new",
        addedMessages: 3,
        totalMessages: 3,
      });

      const { value } = await reader.read();
      const frame = new TextDecoder().decode(value);
      assertEquals(frame.startsWith("data: "), true);
      const payload = JSON.parse(frame.slice("data: ".length).trimEnd());
      assertEquals(payload.type, "session_updated");
      assertEquals(payload.sessionId, "integration-test");
      assertEquals(payload.addedMessages, 3);

      await reader.cancel();
    } finally {
      await handle.shutdown();
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "runUI: shutdown resolves cleanly without Deno.exit",
  async fn() {
    const handle = await runUIHandleFor();

    // The server must actually be listening before we try to stop it.
    const resp = await fetch(`${handleAddr(handle)}/api/status`);
    await resp.body?.cancel();
    assertEquals(resp.status, 200);

    await handle.shutdown();
    // `finished` should now be resolved too.
    await handle.finished;

    // Second shutdown call must be a safe no-op (shuttingDown guard).
    await handle.shutdown();
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

// Full end-to-end: runUI + real filesystem watcher + incremental import +
// SSEBroadcaster + HTTP stream. This is the only test that connects every
// layer in one process, so a regression in any of them (watcher not wired,
// broadcaster passed wrong, /api/stream disconnected, debounce broken)
// turns this red.
Deno.test({
  name: "runUI: watcher → importer → broadcaster → /api/stream end-to-end",
  async fn() {
    const { runUI } = await import("./ui.ts");
    const { join } = await import("@std/path");

    const projectsDir = await Deno.makeTempDir();
    const projectDir = join(projectsDir, "demo-project");
    Deno.mkdirSync(projectDir);

    const handle = runUI({
      dbPath: ":memory:",
      port: 0,
      projectsDir,
    });

    let streamReader: ReadableStreamDefaultReader<Uint8Array> | undefined;
    try {
      const addr = `http://localhost:${(handle.server.addr as Deno.NetAddr).port}`;

      // Open an SSE subscription and skip the initial `connected` frame.
      const streamResp = await fetch(`${addr}/api/stream`);
      assertEquals(streamResp.status, 200);
      streamReader = streamResp.body!.getReader();
      await streamReader.read(); // connected

      // Write a fresh JSONL file under the watched directory. The watcher
      // debounces for 300 ms before importing.
      const filePath = join(projectDir, "sess-e2e.jsonl");
      const jsonl = JSON.stringify({
        type: "user",
        uuid: "u1",
        sessionId: "sess-e2e",
        timestamp: "2026-04-12T00:00:00Z",
        cwd: "/tmp/demo",
        version: "2.1.87",
        gitBranch: "main",
        isSidechain: false,
        message: { role: "user", content: "end-to-end hello" },
      }) + "\n";
      Deno.writeTextFileSync(filePath, jsonl);

      // Wait up to 3 s for the session_updated frame to reach the client.
      // The watcher's default 300 ms debounce plus kernel event latency
      // means this usually arrives well under 1 s.
      const deadline = Date.now() + 3_000;
      let received: { type: string; sessionId: string; status: string } | null = null;
      const decoder = new TextDecoder();
      while (Date.now() < deadline) {
        const { value, done } = await streamReader.read();
        if (done) break;
        const text = decoder.decode(value);
        // Skip keep-alive comment lines.
        const dataLine = text.split("\n").find((l) => l.startsWith("data: "));
        if (!dataLine) continue;
        const payload = JSON.parse(dataLine.slice("data: ".length));
        if (payload.type === "session_updated") {
          received = payload;
          break;
        }
      }

      if (!received) {
        throw new Error("Timed out waiting for session_updated event");
      }
      assertEquals(received.sessionId, "sess-e2e");
      assertEquals(received.status, "new");
    } finally {
      if (streamReader) {
        try {
          await streamReader.cancel();
        } catch {
          // already cancelled
        }
      }
      await handle.shutdown();
      await Deno.remove(projectsDir, { recursive: true });
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});
