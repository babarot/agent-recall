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
