import { VaultDB } from "./db.ts";
import { displayProject } from "./display.ts";
import { getAsset } from "./ui_assets.ts";
import { SSEBroadcaster } from "./sse.ts";
import { startProjectWatcher, type WatcherStatus } from "./watcher.ts";
import { runImport } from "./import.ts";
import { PROJECTS_DIR } from "./config.ts";

const SSE_KEEPALIVE_MS = 15_000;

const DEFAULT_PORT = 6276;

interface UIOptions {
  dbPath: string;
  port: number;
  /**
   * Directory to watch for real-time JSONL updates. Defaults to the
   * production `~/.claude/projects`; tests override it with a tmpdir.
   * Pass `null` to disable the watcher entirely.
   */
  projectsDir?: string | null;
}

export async function startBackground(options: UIOptions): Promise<void> {
  // Spawn self with --foreground and detach
  // Deno.mainModule points to the original script; for compiled binaries, use Deno.execPath()
  const execPath = Deno.execPath();
  const args = ["ui", "--foreground", "--port", String(options.port), "--db", options.dbPath];

  const child = new Deno.Command(execPath, {
    args,
    detached: true,
    stdin: "null",
    stdout: "null",
    stderr: "null",
  }).spawn();
  child.unref();

  // Wait for server to be ready
  const addr = `http://localhost:${options.port}`;
  for (let i = 0; i < 20; i++) {
    await new Promise((r) => setTimeout(r, 250));
    try {
      const resp = await fetch(`${addr}/api/status`);
      if (resp.ok) {
        const data = await resp.json();
        console.log(`agent-recall UI: ${addr} (pid: ${data.pid})`);
        return;
      }
    } catch {
      // Not ready yet
    }
  }
  console.error("Failed to start UI server.");
}

/**
 * Handle returned by `runUI`, giving callers programmatic control over the
 * running server. This replaces the previous fire-and-forget `void` return,
 * making it possible to:
 *
 *   - await a clean shutdown from tests (without calling `Deno.exit`)
 *   - await `server.finished` from `main.ts` to keep the process alive
 *   - dispatch shutdown from the `/api/shutdown` HTTP handler
 */
export interface UIHandle {
  server: Deno.HttpServer;
  /** SSE broadcaster — exposed so the watcher can push events. */
  broadcaster: SSEBroadcaster;
  watcherStatus: WatcherStatus;
  /** Gracefully stop the server, the watcher, and close the DB. Safe to call multiple times. */
  shutdown: () => Promise<void>;
  /** Resolves when the server has fully stopped serving. */
  finished: Promise<void>;
}

export function runUI(options: UIOptions): UIHandle {
  const db = new VaultDB(options.dbPath);

  // Sync all sessions so the DB is up to date before serving requests.
  // Catches up on any changes that happened while the UI was not running.
  runImport({ dbPath: options.dbPath, dryRun: false });

  const ac = new AbortController();
  const broadcaster = new SSEBroadcaster();
  const watcherStatus: WatcherStatus = {
    enabled: false,
    running: false,
    projectsDir: "",
    debounceMs: 0,
  };

  // Forward-declared so the request handler and `doShutdown` can both close
  // over it. Assigned immediately below via `Deno.serve`.
  let server: Deno.HttpServer;

  // Kick off the FS watcher unless it was explicitly disabled. It runs
  // concurrently with the HTTP server and stops when `ac.signal` aborts.
  // Errors are logged but do not tear the server down.
  const projectsDir =
    options.projectsDir === undefined ? PROJECTS_DIR : options.projectsDir;
  let watcherPromise: Promise<void> = Promise.resolve();
  if (projectsDir !== null) {
    watcherStatus.enabled = true;
    watcherStatus.projectsDir = projectsDir;
    watcherPromise = startProjectWatcher(db, broadcaster, projectsDir, {
      signal: ac.signal,
      status: watcherStatus,
    }).catch((e) => {
      watcherStatus.running = false;
      watcherStatus.lastError = e instanceof Error ? e.message : String(e);
      watcherStatus.lastErrorAt = new Date().toISOString();
      console.error("[watcher] crashed:", e);
    });
  }

  let shuttingDown = false;
  const doShutdown = async (): Promise<void> => {
    if (shuttingDown) return;
    shuttingDown = true;
    broadcaster.closeAll();
    ac.abort();
    try {
      await watcherPromise;
    } catch {
      // watcher errors are already logged
    }
    try {
      await server.finished;
    } catch {
      // already stopped
    }
    try {
      db.close();
    } catch {
      // already closed
    }
  };

  server = Deno.serve({
    port: options.port,
    signal: ac.signal,
    onListen: ({ port }) => {
      console.log(`agent-recall UI: http://localhost:${port}`);
    },
  }, (req) => {
    const url = new URL(req.url);

    // POST /api/shutdown — used by `agent-recall ui stop` against a
    // background-spawned process. Shuts down gracefully then exits so the
    // detached subprocess actually terminates.
    if (req.method === "POST" && url.pathname === "/api/shutdown") {
      setTimeout(() => {
        doShutdown().finally(() => Deno.exit(0));
      }, 100);
      return new Response(null, { status: 202 });
    }

    // GET /api/status
    if (url.pathname === "/api/status") {
      return jsonResponse({
        status: "running",
        pid: Deno.pid,
        port: (server.addr as Deno.NetAddr).port,
        sseClients: broadcaster.clientCount(),
        watcher: watcherStatus,
      });
    }

    // GET /api/stream — Server-Sent Events for live UI updates
    if (url.pathname === "/api/stream") {
      return handleSSEStream(broadcaster);
    }

    if (url.pathname.startsWith("/api/")) {
      return handleAPI(db, url);
    }

    return serveAsset(url.pathname);
  });

  return {
    server,
    broadcaster,
    watcherStatus,
    shutdown: doShutdown,
    finished: server.finished,
  };
}

/**
 * Long-lived SSE response. Each client gets a fresh ReadableStream that the
 * broadcaster writes into; a 15-second keep-alive comment keeps the
 * connection alive through HTTP/proxy idle timeouts.
 */
function handleSSEStream(broadcaster: SSEBroadcaster): Response {
  let keepAliveId: number | undefined;
  let ctl: ReadableStreamDefaultController<Uint8Array> | undefined;

  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      ctl = controller;
      broadcaster.addClient(controller);

      // Initial hello so clients can confirm the stream is alive before any
      // real events arrive.
      try {
        controller.enqueue(
          new TextEncoder().encode(
            `data: ${JSON.stringify({ type: "connected", timestamp: Date.now() })}\n\n`
          )
        );
      } catch {
        // ignore
      }

      keepAliveId = setInterval(() => {
        broadcaster.ping(controller);
      }, SSE_KEEPALIVE_MS);
    },
    cancel() {
      if (keepAliveId !== undefined) {
        clearInterval(keepAliveId);
        keepAliveId = undefined;
      }
      if (ctl) broadcaster.removeClient(ctl);
    },
  });

  return new Response(body, {
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      "Connection": "keep-alive",
      "X-Accel-Buffering": "no",
    },
  });
}

export async function stopUI(port?: number): Promise<void> {
  const addr = `http://localhost:${port ?? DEFAULT_PORT}`;
  try {
    const resp = await fetch(`${addr}/api/shutdown`, { method: "POST" });
    if (resp.status === 202) {
      console.log(`Shutdown request sent to ${addr}.`);
    } else {
      console.log(`Unexpected response: ${resp.status}`);
    }
  } catch {
    console.log("UI server is not running.");
  }
}

export async function statusUI(port?: number): Promise<void> {
  const addr = `http://localhost:${port ?? DEFAULT_PORT}`;
  try {
    const resp = await fetch(`${addr}/api/status`);
    if (resp.ok) {
      const data = await resp.json();
      console.log(`UI server is running (pid: ${data.pid}, port: ${data.port}).`);
    } else {
      console.log("UI server is not running.");
    }
  } catch {
    console.log("UI server is not running.");
  }
}

function jsonResponse(data: unknown): Response {
  return new Response(JSON.stringify(data), {
    headers: { "Content-Type": "application/json" },
  });
}

function handleAPI(db: VaultDB, url: URL): Response {
  const path = url.pathname;

  // GET /api/search?q=...&project=...&limit=...&from=...&to=...
  if (path === "/api/search") {
    const query = url.searchParams.get("q") ?? "";
    if (!query) return jsonResponse([]);

    const results = db.search(query, {
      project: url.searchParams.get("project") ?? undefined,
      limit: Number(url.searchParams.get("limit") ?? 50),
      from: url.searchParams.get("from") ?? undefined,
      to: url.searchParams.get("to") ?? undefined,
    });

    return jsonResponse(
      results.map((r) => ({
        sessionId: r.sessionId,
        project: displayProject(r.projectPath, r.project),
        branch: r.gitBranch,
        date: r.startedAt?.slice(0, 10),
        role: r.role,
        content: r.content,
      }))
    );
  }

  // GET /api/sessions?project=...&limit=...
  if (path === "/api/sessions") {
    const sessions = db.listSessions({
      project: url.searchParams.get("project") ?? undefined,
      limit: Number(url.searchParams.get("limit") ?? 50),
      offset: Number(url.searchParams.get("offset") ?? 0),
    });

    const sessionIds = sessions.map((s) => s.sessionId);
    const activities = db.getSessionActivities(sessionIds);

    return jsonResponse(
      sessions.map((s) => {
        let firstPrompt = s.firstPrompt?.slice(0, 200) ?? "";
        if (!firstPrompt || firstPrompt.startsWith("<")) {
          firstPrompt = db.getFirstUserText(s.sessionId) ?? firstPrompt;
        }
        const lastPrompt = db.getLastUserText(s.sessionId) ?? "";
        return {
          sessionId: s.sessionId.slice(0, 8),
          fullSessionId: s.sessionId,
          project: displayProject(s.projectPath, s.project),
          branch: s.gitBranch,
          firstPrompt: firstPrompt,
          lastPrompt: lastPrompt,
          messages: s.messageCount,
          date: s.startedAt?.slice(0, 10),
          createdAt: s.startedAt,
          updatedAt: s.endedAt || s.startedAt,
          activity: activities.get(s.sessionId) ?? [],
        };
      })
    );
  }

  // GET /api/sessions/:id
  const sessionMatch = path.match(/^\/api\/sessions\/(.+)$/);
  if (sessionMatch) {
    const { session, messages } = db.exportSession(sessionMatch[1]);
    if (!session) {
      return jsonResponse({ session: null, messages: [] });
    }
    return jsonResponse({
      session: {
        sessionId: session.sessionId,
        project: displayProject(session.projectPath, session.project),
        branch: session.gitBranch,
        date: session.startedAt?.slice(0, 10),
        summary: session.summary,
      },
      messages: messages.map((m) => ({
        uuid: m.uuid,
        role: m.role,
        blockType: m.blockType,
        content: m.content,
        toolName: m.toolName,
        toolInput: m.toolInput,
        timestamp: m.timestamp,
      })),
    });
  }

  // GET /api/stats?project=...
  if (path === "/api/stats") {
    const s = db.stats(url.searchParams.get("project") ?? undefined);
    return jsonResponse({
      totalSessions: s.totalSessions,
      totalMessages: s.totalMessages,
      byProject: s.byProject.map((p) => ({
        project: displayProject(p.projectPath, p.project),
        projectPath: p.projectPath || p.project,
        sessions: p.sessions,
        messages: p.messages,
      })),
      byMonth: s.byMonth,
    });
  }

  // GET /api/image?session=...&message=...&index=...
  if (path === "/api/image") {
    const sessionId = url.searchParams.get("session") ?? "";
    const messageUuid = url.searchParams.get("message") ?? "";
    const index = Number(url.searchParams.get("index") ?? 0);
    const img = db.getImage(sessionId, messageUuid, index);
    if (!img) {
      return new Response("Not Found", { status: 404 });
    }
    return new Response(img.data as unknown as BodyInit, {
      headers: {
        "Content-Type": img.mediaType,
        "Cache-Control": "public, max-age=86400",
      },
    });
  }

  // GET /api/file?path=...  (serve local file for [Image: source: /path/to/file])
  if (path === "/api/file") {
    const filePath = url.searchParams.get("path") ?? "";
    if (!filePath || !filePath.startsWith("/")) {
      return new Response("Bad Request", { status: 400 });
    }
    try {
      const data = Deno.readFileSync(filePath);
      const ext = filePath.split(".").pop()?.toLowerCase() ?? "";
      const types: Record<string, string> = {
        png: "image/png",
        jpg: "image/jpeg",
        jpeg: "image/jpeg",
        gif: "image/gif",
        webp: "image/webp",
        svg: "image/svg+xml",
      };
      return new Response(data as unknown as BodyInit, {
        headers: {
          "Content-Type": types[ext] ?? "application/octet-stream",
          "Cache-Control": "public, max-age=86400",
        },
      });
    } catch {
      return new Response("Not Found", { status: 404 });
    }
  }

  return new Response("Not Found", { status: 404 });
}

function serveAsset(pathname: string): Response {
  // Try exact path
  let asset = getAsset(pathname);
  if (asset) {
    return new Response(asset.content as unknown as BodyInit, {
      headers: { "Content-Type": asset.contentType },
    });
  }

  // SPA fallback: serve index.html for non-file paths
  asset = getAsset("/index.html");
  if (asset) {
    return new Response(asset.content as unknown as BodyInit, {
      headers: { "Content-Type": asset.contentType },
    });
  }

  return new Response("Not Found", { status: 404 });
}
