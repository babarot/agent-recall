import { VaultDB } from "./db.ts";
import { DEFAULT_DB_PATH, PROJECTS_DIR } from "./config.ts";
import { displayProject } from "./display.ts";
import { runImport } from "./import.ts";
import { startProjectWatcher } from "./watcher.ts";

// --- MCP Tool Definitions ---

const TOOLS = [
  {
    name: "recall_search",
    description:
      "Search past coding agent session conversations by full-text query. Use this when you need to find previous discussions, decisions, or context from past sessions.",
    inputSchema: {
      type: "object" as const,
      properties: {
        query: {
          type: "string",
          description:
            'Full-text search query. Supports FTS5 syntax: "exact phrase", term1 AND term2, term1 OR term2, term1 NOT term2',
        },
        project: {
          type: "string",
          description: "Filter by project name (partial match)",
        },
        limit: {
          type: "number",
          description: "Max results (default: 10)",
        },
        from: {
          type: "string",
          description: "Start date filter (YYYY-MM-DD)",
        },
        to: {
          type: "string",
          description: "End date filter (YYYY-MM-DD)",
        },
      },
      required: ["query"],
    },
  },
  {
    name: "recall_list",
    description:
      "List archived coding agent sessions. Use this to see what sessions are available before exporting a specific one.",
    inputSchema: {
      type: "object" as const,
      properties: {
        project: {
          type: "string",
          description: "Filter by project name (partial match)",
        },
        limit: {
          type: "number",
          description: "Max sessions to return (default: 20)",
        },
      },
    },
  },
  {
    name: "recall_export",
    description:
      "Export the full conversation of a specific session. Use this to get detailed context from a past session found via recall_search or recall_list.",
    inputSchema: {
      type: "object" as const,
      properties: {
        session_id: {
          type: "string",
          description:
            "Session ID (full UUID or prefix). Get this from recall_search or recall_list results.",
        },
      },
      required: ["session_id"],
    },
  },
  {
    name: "recall_stats",
    description:
      "Show archive statistics: total sessions, messages, breakdown by project and month.",
    inputSchema: {
      type: "object" as const,
      properties: {
        project: {
          type: "string",
          description: "Filter by project name (partial match)",
        },
      },
    },
  },
];

// --- Tool Handlers ---

function handleSearch(
  db: VaultDB,
  args: Record<string, unknown>
): unknown {
  const results = db.search(args.query as string, {
    project: args.project as string | undefined,
    limit: (args.limit as number) ?? 10,
    from: args.from as string | undefined,
    to: args.to as string | undefined,
  });

  return results.map((r) => ({
    sessionId: r.sessionId.slice(0, 8),
    project: displayProject(r.projectPath, r.project),
    branch: r.gitBranch,
    date: r.startedAt?.slice(0, 10),
    role: r.role,
    content: r.content,
  }));
}

function handleList(
  db: VaultDB,
  args: Record<string, unknown>
): unknown {
  const sessions = db.listSessions({
    project: args.project as string | undefined,
    limit: (args.limit as number) ?? 20,
  });

  return sessions.map((s) => ({
    sessionId: s.sessionId.slice(0, 8),
    fullSessionId: s.sessionId,
    project: displayProject(s.projectPath, s.project),
    branch: s.gitBranch,
    firstPrompt: s.firstPrompt?.slice(0, 200),
    messages: s.messageCount,
    date: s.startedAt?.slice(0, 10),
  }));
}

function handleExport(
  db: VaultDB,
  args: Record<string, unknown>
): unknown {
  const { session, messages } = db.exportSession(args.session_id as string);
  if (!session) {
    return { error: `Session not found: ${args.session_id}` };
  }

  return {
    session: {
      sessionId: session.sessionId,
      project: displayProject(session.projectPath, session.project),
      branch: session.gitBranch,
      date: session.startedAt?.slice(0, 10),
      summary: session.summary,
    },
    messages: messages.map((m) => ({
      role: m.role,
      content: m.content,
    })),
  };
}

function handleStats(
  db: VaultDB,
  args: Record<string, unknown>
): unknown {
  const s = db.stats(args.project as string | undefined);
  return {
    totalSessions: s.totalSessions,
    totalMessages: s.totalMessages,
    byProject: s.byProject.map((p) => ({
      project: displayProject(p.projectPath, p.project),
      sessions: p.sessions,
      messages: p.messages,
    })),
    byMonth: s.byMonth,
  };
}

// --- JSON-RPC / MCP Protocol ---

interface JsonRpcRequest {
  jsonrpc: "2.0";
  id?: number | string;
  method: string;
  params?: Record<string, unknown>;
}

interface JsonRpcResponse {
  jsonrpc: "2.0";
  id: number | string | null;
  result?: unknown;
  error?: { code: number; message: string };
}

function makeResponse(id: number | string | null, result: unknown): JsonRpcResponse {
  return { jsonrpc: "2.0", id, result };
}

function makeError(
  id: number | string | null,
  code: number,
  message: string
): JsonRpcResponse {
  return { jsonrpc: "2.0", id, error: { code, message } };
}

function handleRequest(
  req: JsonRpcRequest,
  db: VaultDB
): JsonRpcResponse | null {
  // Notifications (no id) don't get a response
  if (req.id === undefined) {
    return null;
  }

  switch (req.method) {
    case "initialize":
      return makeResponse(req.id, {
        protocolVersion: "2024-11-05",
        capabilities: { tools: {} },
        serverInfo: {
          name: "agent-recall",
          version: "0.1.0",
        },
      });

    case "tools/list":
      return makeResponse(req.id, { tools: TOOLS });

    case "tools/call": {
      const name = req.params?.name as string;
      const args = (req.params?.arguments ?? {}) as Record<string, unknown>;

      let result: unknown;
      try {
        switch (name) {
          case "recall_search":
            result = handleSearch(db, args);
            break;
          case "recall_list":
            result = handleList(db, args);
            break;
          case "recall_export":
            result = handleExport(db, args);
            break;
          case "recall_stats":
            result = handleStats(db, args);
            break;
          default:
            return makeError(req.id, -32601, `Unknown tool: ${name}`);
        }
      } catch (e) {
        return makeResponse(req.id, {
          content: [
            { type: "text", text: `Error: ${(e as Error).message}` },
          ],
          isError: true,
        });
      }

      return makeResponse(req.id, {
        content: [
          { type: "text", text: JSON.stringify(result, null, 2) },
        ],
      });
    }

    default:
      return makeError(req.id, -32601, `Method not found: ${req.method}`);
  }
}

// --- stdio transport (NDJSON) ---

export async function runMcp(dbPath: string): Promise<void> {
  const db = new VaultDB(dbPath);

  // Import all sessions so the DB is up to date before serving requests.
  runImport({ dbPath, dryRun: false });

  // Start the FS watcher so new sessions are imported in real-time while
  // the MCP server is running. No broadcaster needed — there are no SSE
  // clients to notify.
  const ac = new AbortController();
  startProjectWatcher(db, null, PROJECTS_DIR, { signal: ac.signal }).catch(
    (e) => console.error("[mcp] watcher crashed:", e)
  );

  const encoder = new TextEncoder();
  const decoder = new TextDecoder();
  let buffer = "";

  const reader = Deno.stdin.readable.getReader();

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });

      // Process complete lines
      let newlineIdx: number;
      while ((newlineIdx = buffer.indexOf("\n")) !== -1) {
        const line = buffer.slice(0, newlineIdx).trim();
        buffer = buffer.slice(newlineIdx + 1);

        if (!line) continue;

        let req: JsonRpcRequest;
        try {
          req = JSON.parse(line);
        } catch {
          const errResp = makeError(null, -32700, "Parse error");
          Deno.stdout.writeSync(
            encoder.encode(JSON.stringify(errResp) + "\n")
          );
          continue;
        }

        const resp = handleRequest(req, db);
        if (resp) {
          Deno.stdout.writeSync(
            encoder.encode(JSON.stringify(resp) + "\n")
          );
        }
      }
    }
  } finally {
    ac.abort();
    reader.releaseLock();
    db.close();
  }
}

if (import.meta.main) {
  runMcp(DEFAULT_DB_PATH);
}
