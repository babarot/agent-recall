import { VaultDB } from "./db.ts";
import {
  discoverSessions,
  loadSessionIndex,
  parseSession,
} from "./parser.ts";
import { PROJECTS_DIR } from "./config.ts";

interface ImportOptions {
  dbPath: string;
  session?: string;
  project?: string;
  dryRun?: boolean;
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

  // Group by project for session index loading
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
      // Check if session already exists
      const existingCount = db.sessionExists(sessionId);
      if (existingCount !== null) {
        // Session exists - check if we need incremental import
        let jsonlContent: string;
        try {
          jsonlContent = Deno.readTextFileSync(filePath);
        } catch {
          skippedSessions++;
          continue;
        }

        const parsed = parseSession(
          jsonlContent,
          project,
          indexMap.get(sessionId)
        );
        if (!parsed || parsed.messages.length <= existingCount) {
          skippedSessions++;
          continue;
        }

        // Incremental: insert only new messages
        let newMessages = 0;
        for (const msg of parsed.messages) {
          db.insertMessage({
            sessionId,
            uuid: msg.uuid,
            role: msg.role,
            content: msg.content,
            timestamp: msg.timestamp,
            turnIndex: msg.turnIndex,
          });
          newMessages++;
        }
        db.updateSessionCounts(
          sessionId,
          parsed.messages.length,
          parsed.meta.endedAt
        );
        importedMessages += newMessages;
        importedSessions++;
        continue;
      }

      // New session: full import
      let jsonlContent: string;
      try {
        jsonlContent = Deno.readTextFileSync(filePath);
      } catch {
        skippedSessions++;
        continue;
      }

      const parsed = parseSession(
        jsonlContent,
        project,
        indexMap.get(sessionId)
      );
      if (!parsed) {
        skippedSessions++;
        continue;
      }

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
      });

      for (const msg of parsed.messages) {
        db.insertMessage({
          sessionId: parsed.meta.sessionId,
          uuid: msg.uuid,
          role: msg.role,
          content: msg.content,
          timestamp: msg.timestamp,
          turnIndex: msg.turnIndex,
        });
      }

      importedSessions++;
      importedMessages += parsed.messages.length;
    }
  }

  db.close();

  console.log(
    `Imported ${importedSessions} sessions (${importedMessages} messages). Skipped ${skippedSessions} already-imported sessions.`
  );
}
