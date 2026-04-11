import { VaultDB } from "./db.ts";
import {
  discoverSessions,
  loadSessionIndex,
  parseJournalLines,
  parseSession,
} from "./parser.ts";
import { PROJECTS_DIR } from "./config.ts";
import { basename, dirname } from "@std/path";
import type { SessionIndexEntry } from "./types.ts";

interface ImportOptions {
  dbPath: string;
  session?: string;
  project?: string;
  dryRun?: boolean;
}

/** Result of a single incremental import attempt */
export interface IncrementalResult {
  status: "new" | "updated" | "unchanged" | "resynced";
  sessionId: string;
  project: string;
  addedMessages: number;
  totalMessages: number;
}

function importFullSession(
  db: VaultDB,
  filePath: string,
  project: string,
  _sessionId: string,
  status: "new" | "resynced",
  opts: { indexEntry?: SessionIndexEntry } = {}
): IncrementalResult | null {
  let jsonlContent: string;
  let fileSize: number;
  try {
    jsonlContent = Deno.readTextFileSync(filePath);
    fileSize = Deno.statSync(filePath).size;
  } catch {
    return null;
  }

  const parsed = parseSession(jsonlContent, project, opts.indexEntry);
  if (!parsed) return null;

  db.insertSession({
    sessionId: parsed.meta.sessionId,
    project: parsed.meta.project,
    projectPath: parsed.meta.projectPath,
    gitBranch: parsed.meta.gitBranch,
    firstPrompt: parsed.meta.firstPrompt,
    summary: parsed.meta.summary,
    messageCount: parsed.messages.length,
    startedAt: parsed.meta.startedAt,
    endedAt: parsed.meta.endedAt,
    claudeVersion: parsed.meta.claudeVersion,
    importedBytes: fileSize,
  });

  let inserted = 0;
  for (const msg of parsed.messages) {
    const { changes } = db.insertMessage({
      sessionId: parsed.meta.sessionId,
      uuid: msg.uuid,
      role: msg.role,
      blockType: msg.blockType,
      content: msg.content,
      toolName: msg.toolName,
      toolInput: msg.toolInput,
      timestamp: msg.timestamp,
      turnIndex: msg.turnIndex,
    });
    inserted += changes;
  }

  for (const img of parsed.images) {
    if (!db.hasImages(parsed.meta.sessionId, img.messageUuid)) {
      db.insertImage({
        sessionId: parsed.meta.sessionId,
        messageUuid: img.messageUuid,
        imageIndex: img.imageIndex,
        mediaType: img.mediaType,
        data: Uint8Array.from(atob(img.data), (c) => c.charCodeAt(0)),
      });
    }
  }

  return {
    status,
    sessionId: parsed.meta.sessionId,
    project: parsed.meta.project,
    addedMessages: inserted,
    totalMessages: inserted,
  };
}

/**
 * Incrementally import a single session file into the vault.
 *
 * - For a new session: reads the full file, parses header + all lines,
 *   inserts the session row and all messages/images, and records
 *   `imported_bytes = fileSize` so the next call can skip the prefix.
 * - For an existing session: seeks to `imported_bytes`, reads only the
 *   appended tail, parses it with `startTurnIndex = existingMessageCount`,
 *   inserts new messages (counting actual `INSERT OR IGNORE` changes, not
 *   parse count), advances `imported_bytes` to the last complete line, and
 *   updates `message_count` / `ended_at`.
 *
 * Returns `null` on unrecoverable errors (file unreadable, unparseable as a
 * fresh session, etc.). Callers should treat `null` as "give up on this
 * file for now and try again later".
 */
export function importSingleSessionIncremental(
  db: VaultDB,
  filePath: string,
  opts: { indexEntry?: SessionIndexEntry } = {}
): IncrementalResult | null {
  // Derive sessionId and project from the filepath.
  // Layout: <PROJECTS_DIR>/<project>/<sessionId>.jsonl
  const sessionId = basename(filePath).replace(/\.jsonl$/, "");
  const project = basename(dirname(filePath));
  if (!sessionId || !project) return null;

  let fileSize: number;
  try {
    fileSize = Deno.statSync(filePath).size;
  } catch {
    return null;
  }

  const existingBytes = db.getSessionImportedBytes(sessionId);
  const existingCount = db.sessionExists(sessionId) ?? 0;
  const hadExistingSession = existingBytes !== null;

  const importFull = (): IncrementalResult | null => {
    return importFullSession(
      db,
      filePath,
      project,
      sessionId,
      hadExistingSession ? "resynced" : "new",
      opts
    );
  };

  // ----- Existing session: tail read only -----
  if (existingBytes !== null) {
    if (fileSize <= existingBytes) {
      if (fileSize === existingBytes) {
        return {
          status: "unchanged",
          sessionId,
          project,
          addedMessages: 0,
          totalMessages: existingCount,
        };
      }

      // The file shrank or was replaced. Drop the stale DB copy and rebuild
      // from the current on-disk source of truth.
      db.resetSession(sessionId);
      return importFull();
    }

    const tailLength = fileSize - existingBytes;
    let tailBuf: Uint8Array;
    try {
      const file = Deno.openSync(filePath, { read: true });
      try {
        file.seekSync(existingBytes, Deno.SeekMode.Start);
        tailBuf = new Uint8Array(tailLength);
        let offset = 0;
        while (offset < tailLength) {
          const n = file.readSync(tailBuf.subarray(offset));
          if (n === null || n === 0) break;
          offset += n;
        }
        if (offset < tailLength) {
          tailBuf = tailBuf.subarray(0, offset);
        }
      } finally {
        file.close();
      }
    } catch {
      return null;
    }

    // Trim to the last newline so we never parse an incomplete trailing line.
    // Any bytes after the last newline are deferred until the next call.
    let processedLen = tailBuf.lastIndexOf(0x0a); // '\n'
    if (processedLen < 0) {
      // No complete line yet — don't advance imported_bytes at all.
      return {
        status: "unchanged",
        sessionId,
        project,
        addedMessages: 0,
        totalMessages: existingCount,
      };
    }
    processedLen += 1; // include the newline itself

    const tailText = new TextDecoder().decode(tailBuf.subarray(0, processedLen));
    const parsed = parseJournalLines(tailText, existingCount);

    let inserted = 0;
    for (const msg of parsed.messages) {
      const { changes } = db.insertMessage({
        sessionId,
        uuid: msg.uuid,
        role: msg.role,
        blockType: msg.blockType,
        content: msg.content,
        toolName: msg.toolName,
        toolInput: msg.toolInput,
        timestamp: msg.timestamp,
        turnIndex: msg.turnIndex,
      });
      inserted += changes;
    }

    for (const img of parsed.images) {
      if (!db.hasImages(sessionId, img.messageUuid)) {
        db.insertImage({
          sessionId,
          messageUuid: img.messageUuid,
          imageIndex: img.imageIndex,
          mediaType: img.mediaType,
          data: Uint8Array.from(atob(img.data), (c) => c.charCodeAt(0)),
        });
      }
    }

    const newTotal = existingCount + inserted;
    const newOffset = existingBytes + processedLen;
    const endedAt = parsed.lastTimestamp ?? undefined;

    if (inserted > 0 && endedAt) {
      db.updateSessionCounts(sessionId, newTotal, endedAt);
    }
    db.updateSessionImportedBytes(sessionId, newOffset, endedAt);

    return {
      status: inserted > 0 ? "updated" : "unchanged",
      sessionId,
      project,
      addedMessages: inserted,
      totalMessages: newTotal,
    };
  }

  // ----- New session: full read + insert -----
  return importFull();
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
        skippedSessions++;
        continue;
      }

      importedSessions++;
      importedMessages += result.addedMessages;
    }
  }

  db.close();

  console.log(
    `Imported ${importedSessions} sessions (${importedMessages} messages). Skipped ${skippedSessions} already-imported sessions.`
  );
}
