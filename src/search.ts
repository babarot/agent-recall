import { bold, dim, cyan, yellow } from "@std/fmt/colors";
import { VaultDB } from "./db.ts";
import { displayProject } from "./display.ts";

interface SearchOptions {
  dbPath: string;
  query: string;
  project?: string;
  limit?: number;
  from?: string;
  to?: string;
  format?: "text" | "json";
}

export function runSearch(options: SearchOptions): void {
  const db = new VaultDB(options.dbPath);

  const results = db.search(options.query, {
    project: options.project,
    limit: options.limit,
    from: options.from,
    to: options.to,
  });

  db.close();

  if (results.length === 0) {
    console.log("No results found.");
    return;
  }

  if (options.format === "json") {
    console.log(JSON.stringify(results, null, 2));
    return;
  }

  // Group by session for display
  const bySession = new Map<string, typeof results>();
  for (const r of results) {
    const list = bySession.get(r.sessionId) ?? [];
    list.push(r);
    bySession.set(r.sessionId, list);
  }

  for (const [sessionId, messages] of bySession) {
    const first = messages[0];
    const date = first.startedAt?.slice(0, 10) ?? "unknown";
    const projectDisplay = displayProject(first.projectPath, first.project);
    const branch = first.gitBranch ?? "";

    console.log(
      `\n${dim(`[${date}]`)} ${bold(projectDisplay)} ${dim(`(${branch})`)} ${dim(`session:${sessionId.slice(0, 8)}`)}`
    );

    for (const msg of messages) {
      const roleLabel = msg.role === "user" ? cyan("user") : yellow("assistant");
      const snippet = msg.content.length > 200
        ? msg.content.slice(0, 200) + "..."
        : msg.content;
      console.log(`  ${roleLabel}: ${snippet}`);
    }
  }

  const sessionCount = bySession.size;
  console.log(
    `\n${dim(`Found ${results.length} results across ${sessionCount} sessions.`)}`
  );
}
