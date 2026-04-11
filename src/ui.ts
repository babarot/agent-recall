import { VaultDB } from "./db.ts";
import { displayProject } from "./display.ts";
import { getAsset } from "./ui_assets.ts";

const DEFAULT_PORT = 6276;

interface UIOptions {
  dbPath: string;
  port: number;
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

export function runUI(options: UIOptions): void {
  const db = new VaultDB(options.dbPath);
  const ac = new AbortController();

  Deno.serve({ port: options.port, signal: ac.signal, onListen: () => {
    console.log(`agent-recall UI: http://localhost:${options.port}`);
  }}, (req) => {
    const url = new URL(req.url);

    // POST /api/shutdown
    if (req.method === "POST" && url.pathname === "/api/shutdown") {
      // Respond first, then shut down
      setTimeout(() => {
        db.close();
        ac.abort();
        Deno.exit(0);
      }, 100);
      return new Response(null, { status: 202 });
    }

    // GET /api/status
    if (url.pathname === "/api/status") {
      return jsonResponse({ status: "running", pid: Deno.pid, port: options.port });
    }

    if (url.pathname.startsWith("/api/")) {
      return handleAPI(db, url);
    }

    return serveAsset(url.pathname);
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

    return jsonResponse(
      sessions.map((s) => {
        let prompt = s.firstPrompt?.slice(0, 200) ?? "";
        if (!prompt || prompt.startsWith("<")) {
          prompt = db.getFirstUserText(s.sessionId) ?? prompt;
        }
        return {
          sessionId: s.sessionId.slice(0, 8),
          fullSessionId: s.sessionId,
          project: displayProject(s.projectPath, s.project),
          branch: s.gitBranch,
          firstPrompt: prompt,
          messages: s.messageCount,
          date: s.startedAt?.slice(0, 10),
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
