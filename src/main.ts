import { parseArgs } from "@std/cli/parse-args";
import { DEFAULT_DB_PATH } from "./config.ts";
import { runImport } from "./import.ts";
import { runSearch } from "./search.ts";
import { runList } from "./list.ts";
import { runExport } from "./export.ts";
import { runStats } from "./stats.ts";
import { runMcp } from "./mcp.ts";
import { runUI, startBackground, stopUI, statusUI } from "./ui.ts";

const USAGE = `agent-recall - Archive and search coding agent sessions

Usage:
  agent-recall import [options]       Import sessions into the vault
  agent-recall search <query> [opts]  Full-text search across sessions
  agent-recall list [options]         List archived sessions
  agent-recall export <id> [options]  Export a session
  agent-recall stats [options]        Show archive statistics
  agent-recall mcp                    Start MCP server (stdio transport)
  agent-recall ui [--port <n>]        Start web UI in background (default: 6276)
  agent-recall ui --foreground        Start web UI in foreground
  agent-recall ui stop                Stop running UI server
  agent-recall ui status              Show UI server status

Global Options:
  --db <path>     Database path (default: ~/.claude/vault.db)
  --help          Show this help

Import Options:
  --session <id>  Import a specific session
  --project <name> Import sessions for a project
  --dry-run       Show what would be imported

Search Options:
  --project <name> Limit to a project
  --limit <n>     Max results (default: 20)
  --from <date>   Start date (YYYY-MM-DD)
  --to <date>     End date (YYYY-MM-DD)
  --format <fmt>  Output format: text, json (default: text)

List Options:
  --project <name> Filter by project
  --limit <n>     Max sessions (default: 50)
  --format <fmt>  Output format: text, json (default: text)

Export Options:
  --format <fmt>  Output format: markdown, json, text (default: markdown)
  --output <file> Write to file instead of stdout
`;

async function main(): Promise<void> {
  const args = parseArgs(Deno.args, {
    string: ["db", "session", "project", "format", "from", "to", "output", "port"],
    boolean: ["help", "dry-run", "foreground"],
    default: { db: DEFAULT_DB_PATH },
    alias: { h: "help", n: "dry-run" },
  });

  if (args.help || args._.length === 0) {
    console.log(USAGE);
    Deno.exit(0);
  }

  const subcommand = String(args._[0]);
  const dbPath = args.db as string;

  switch (subcommand) {
    case "import":
      runImport({
        dbPath,
        session: args.session as string | undefined,
        project: args.project as string | undefined,
        dryRun: args["dry-run"] as boolean,
      });
      break;

    case "search": {
      const query = args._.slice(1).join(" ");
      if (!query) {
        console.error("Usage: agent-recall search <query>");
        Deno.exit(1);
      }
      runSearch({
        dbPath,
        query,
        project: args.project as string | undefined,
        limit: args.limit ? Number(args.limit) : undefined,
        from: args.from as string | undefined,
        to: args.to as string | undefined,
        format: (args.format as "text" | "json") ?? "text",
      });
      break;
    }

    case "list":
      runList({
        dbPath,
        project: args.project as string | undefined,
        limit: args.limit ? Number(args.limit) : undefined,
        format: (args.format as "text" | "json") ?? "text",
      });
      break;

    case "export": {
      const sessionId = args._[1] ? String(args._[1]) : undefined;
      if (!sessionId) {
        console.error("Usage: agent-recall export <session-id>");
        Deno.exit(1);
      }
      runExport({
        dbPath,
        sessionId,
        format: (args.format as "markdown" | "json" | "text") ?? "markdown",
        output: args.output as string | undefined,
      });
      break;
    }

    case "stats":
      runStats({
        dbPath,
        project: args.project as string | undefined,
      });
      break;

    case "mcp":
      await runMcp(dbPath);
      break;

    case "ui": {
      const uiAction = args._[1] ? String(args._[1]) : "start";
      const port = args.port ? Number(args.port) : 6276;
      if (uiAction === "stop") {
        await stopUI(port);
      } else if (uiAction === "status") {
        await statusUI(port);
      } else if (args.foreground) {
        const handle = runUI({ dbPath, port });
        const cleanExit = async () => {
          await handle.shutdown();
          Deno.exit(0);
        };
        Deno.addSignalListener("SIGINT", cleanExit);
        Deno.addSignalListener("SIGTERM", cleanExit);
        await handle.finished;
      } else {
        await startBackground({ dbPath, port });
      }
      break;
    }

    default:
      console.error(`Unknown command: ${subcommand}`);
      console.log(USAGE);
      Deno.exit(1);
  }
}

main();
