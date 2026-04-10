import { bold, dim } from "@std/fmt/colors";
import { VaultDB } from "./db.ts";
import { displayProject } from "./display.ts";

interface ListOptions {
  dbPath: string;
  project?: string;
  limit?: number;
  format?: "text" | "json";
}

export function runList(options: ListOptions): void {
  const db = new VaultDB(options.dbPath);

  const sessions = db.listSessions({
    project: options.project,
    limit: options.limit,
  });

  db.close();

  if (sessions.length === 0) {
    console.log("No sessions found.");
    return;
  }

  if (options.format === "json") {
    console.log(JSON.stringify(sessions, null, 2));
    return;
  }

  // Header
  console.log(
    `${bold("Session".padEnd(10))} ${bold("Project".padEnd(30))} ${bold("Branch".padEnd(30))} ${bold("Msgs".padStart(5))}  ${bold("Date")}`
  );
  console.log(dim("-".repeat(95)));

  for (const s of sessions) {
    const id = s.sessionId.slice(0, 8);
    const projectFull = displayProject(s.projectPath, s.project);
    const projectShort = projectFull.length > 28
      ? "..." + projectFull.slice(-25)
      : projectFull;
    const branch = (s.gitBranch ?? "").length > 28
      ? (s.gitBranch ?? "").slice(0, 28) + "..."
      : (s.gitBranch ?? "");
    const date = s.startedAt?.slice(0, 10) ?? "";
    const prompt = s.firstPrompt
      ? dim(` ${s.firstPrompt.slice(0, 60)}${s.firstPrompt.length > 60 ? "..." : ""}`)
      : "";

    console.log(
      `${id.padEnd(10)} ${projectShort.padEnd(30)} ${branch.padEnd(30)} ${String(s.messageCount).padStart(5)}  ${date}${prompt}`
    );
  }

  console.log(dim(`\n${sessions.length} sessions listed.`));
}
