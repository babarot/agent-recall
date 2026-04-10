import { VaultDB } from "./db.ts";

interface ExportOptions {
  dbPath: string;
  sessionId: string;
  format?: "markdown" | "json" | "text";
  output?: string;
}

export function runExport(options: ExportOptions): void {
  const db = new VaultDB(options.dbPath);
  const { session, messages } = db.exportSession(options.sessionId);
  db.close();

  if (!session) {
    console.error(`Session not found: ${options.sessionId}`);
    Deno.exit(1);
  }

  const format = options.format ?? "markdown";
  let output: string;

  switch (format) {
    case "json":
      output = JSON.stringify({ session, messages }, null, 2);
      break;
    case "text":
      output = formatText(session, messages);
      break;
    case "markdown":
    default:
      output = formatMarkdown(session, messages);
      break;
  }

  if (options.output) {
    Deno.writeTextFileSync(options.output, output);
    console.log(`Exported to ${options.output}`);
  } else {
    console.log(output);
  }
}

function formatMarkdown(
  session: {
    sessionId: string;
    project: string;
    projectPath: string;
    gitBranch: string;
    startedAt: string;
    firstPrompt: string;
    summary: string | null;
  },
  messages: Array<{
    role: string;
    content: string;
    timestamp: string;
  }>
): string {
  const lines: string[] = [];
  lines.push(`# Session: ${session.sessionId.slice(0, 8)}`);
  lines.push("");
  lines.push(`- **Project**: ${session.projectPath || session.project}`);
  lines.push(`- **Branch**: ${session.gitBranch}`);
  lines.push(`- **Date**: ${session.startedAt?.slice(0, 10) ?? ""}`);
  if (session.summary) {
    lines.push(`- **Summary**: ${session.summary}`);
  }
  lines.push("");
  lines.push("---");
  lines.push("");

  for (const msg of messages) {
    const role = msg.role === "user" ? "User" : "Assistant";
    lines.push(`**${role}**:`);
    lines.push("");
    lines.push(msg.content);
    lines.push("");
    lines.push("---");
    lines.push("");
  }

  return lines.join("\n");
}

function formatText(
  session: {
    sessionId: string;
    project: string;
    gitBranch: string;
    startedAt: string;
  },
  messages: Array<{
    role: string;
    content: string;
  }>
): string {
  const lines: string[] = [];
  lines.push(
    `Session: ${session.sessionId.slice(0, 8)} | ${session.project} (${session.gitBranch}) | ${session.startedAt?.slice(0, 10)}`
  );
  lines.push("=".repeat(80));
  lines.push("");

  for (const msg of messages) {
    const role = msg.role === "user" ? "USER" : "ASSISTANT";
    lines.push(`[${role}]`);
    lines.push(msg.content);
    lines.push("");
  }

  return lines.join("\n");
}
