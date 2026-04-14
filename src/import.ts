import { VaultDB } from "./db.ts";
import { discoverSessions, loadSessionIndex, parseSession } from "./parser.ts";
import { PROJECTS_DIR } from "./config.ts";
import { basename, dirname } from "@std/path";
import type { SessionIndexEntry } from "./types.ts";

interface ImportOptions {
  dbPath: string;
  session?: string;
  project?: string;
  dryRun?: boolean;
}

/** Result of a single import attempt */
export interface IncrementalResult {
  status: "new" | "resynced" | "unchanged";
  sessionId: string;
  project: string;
  addedMessages: number;
  totalMessages: number;
}

/**
 * Import a session JSONL file into the vault, mirroring its exact current
 * content. The DB is a derived cache of the JSONL master — every import
 * wipes the session's existing rows and re-inserts everything from the
 * current file on disk.
 *
 * This keeps the code path a single branch, so all the interesting cases
 * (new session, appended lines, `/compact` in-place rewrite that removes or
 * re-orders uuids, interrupted prior imports) resolve the same way without
 * special casing. Idempotence is guaranteed by the
 * `(session_id, uuid, block_index)` UNIQUE index; duplicate inserts from
 * concurrent watchers are silently ignored.
 *
 * Returns `null` on unreadable / unparseable files so callers can treat it
 * as "give up and try again later".
 */
export function importSingleSessionIncremental(
  db: VaultDB,
  filePath: string,
  opts: { indexEntry?: SessionIndexEntry } = {},
): IncrementalResult | null {
  // Layout: <PROJECTS_DIR>/<project>/<sessionId>.jsonl
  const sessionId = basename(filePath).replace(/\.jsonl$/, "");
  const project = basename(dirname(filePath));
  if (!sessionId || !project) return null;

  // Stat the file for change detection before reading content.
  let fileStat: Deno.FileInfo;
  try {
    fileStat = Deno.statSync(filePath);
  } catch {
    return null;
  }
  const fileMtime = fileStat.mtime?.getTime() ?? null;
  const fileSize = fileStat.size;

  // Skip re-import when the file hasn't changed since the last import.
  const existing = db.getFileInfo(sessionId);
  if (
    existing &&
    existing.fileMtime !== null &&
    existing.fileMtime === fileMtime &&
    existing.fileSize === fileSize
  ) {
    return {
      status: "unchanged",
      sessionId,
      project,
      addedMessages: 0,
      totalMessages: existing.messageCount,
    };
  }

  let jsonlContent: string;
  try {
    jsonlContent = Deno.readTextFileSync(filePath);
  } catch {
    return null;
  }

  const parsed = parseSession(jsonlContent, project, opts.indexEntry);
  if (!parsed) return null;

  const hadExisting = existing !== null;

  // Mirror the file: drop any existing state for this session, then insert
  // the freshly-parsed rows. resetSession runs in its own transaction; the
  // re-insert block is small enough that we don't bother adding another.
  db.resetSession(sessionId);
  db.insertSession({
    sessionId,
    project: parsed.meta.project,
    projectPath: parsed.meta.projectPath,
    gitBranch: parsed.meta.gitBranch,
    firstPrompt: parsed.meta.firstPrompt,
    summary: parsed.meta.summary,
    messageCount: parsed.messages.length,
    startedAt: parsed.meta.startedAt,
    endedAt: parsed.meta.endedAt,
    claudeVersion: parsed.meta.claudeVersion,
    fileMtime,
    fileSize,
  });

  for (const msg of parsed.messages) {
    db.insertMessage({
      sessionId,
      uuid: msg.uuid,
      role: msg.role,
      blockType: msg.blockType,
      blockIndex: msg.blockIndex,
      content: msg.content,
      toolName: msg.toolName,
      toolInput: msg.toolInput,
      timestamp: msg.timestamp,
      turnIndex: msg.turnIndex,
    });
  }

  for (const img of parsed.images) {
    db.insertImage({
      sessionId,
      messageUuid: img.messageUuid,
      imageIndex: img.imageIndex,
      mediaType: img.mediaType,
      data: Uint8Array.from(atob(img.data), (c) => c.charCodeAt(0)),
    });
  }

  return {
    status: hadExisting ? "resynced" : "new",
    sessionId,
    project,
    addedMessages: parsed.messages.length,
    totalMessages: parsed.messages.length,
  };
}

export function runImport(options: ImportOptions): void {
  const allSessions = discoverSessions(PROJECTS_DIR);

  // Filter by session or project if specified
  let targets = allSessions;
  if (options.session) {
    targets = allSessions.filter((s) =>
      s.sessionId === options.session || s.sessionId.startsWith(options.session!)
    );
  } else if (options.project) {
    targets = allSessions.filter((s) =>
      s.project.toLowerCase().includes(options.project!.toLowerCase())
    );
  }

  if (targets.length === 0) {
    console.log("No sessions found to import.");
    return;
  }

  if (options.dryRun) {
    console.log(`Would import ${targets.length} session files:`);
    for (const t of targets.slice(0, 20)) {
      console.log(`  ${t.sessionId} (${t.project})`);
    }
    if (targets.length > 20) {
      console.log(`  ... and ${targets.length - 20} more`);
    }
    return;
  }

  console.log(`Syncing ${targets.length} sessions...`);

  const db = new VaultDB(options.dbPath);

  // Group by project so we load sessions-index.json at most once per project
  const byProject = new Map<string, typeof targets>();
  for (const t of targets) {
    const list = byProject.get(t.project) ?? [];
    list.push(t);
    byProject.set(t.project, list);
  }

  let importedSessions = 0;
  let importedMessages = 0;
  let unchangedSessions = 0;
  let skippedSessions = 0;

  for (const [project, sessions] of byProject) {
    const indexMap = loadSessionIndex(`${PROJECTS_DIR}/${project}`);

    for (const { sessionId, filePath } of sessions) {
      const result = importSingleSessionIncremental(db, filePath, {
        indexEntry: indexMap.get(sessionId),
      });

      if (!result) {
        skippedSessions++;
        continue;
      }

      if (result.status === "unchanged") {
        unchangedSessions++;
        continue;
      }

      importedSessions++;
      importedMessages += result.addedMessages;
    }
  }

  db.close();

  const parts = [`Imported ${importedSessions} sessions (${importedMessages} messages).`];
  if (unchangedSessions > 0) parts.push(`${unchangedSessions} unchanged.`);
  if (skippedSessions > 0) parts.push(`Skipped ${skippedSessions} unreadable files.`);
  console.log(parts.join(" "));
}
