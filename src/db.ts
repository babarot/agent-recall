import { DatabaseSync } from "node:sqlite";
import { SCHEMA_SQL } from "./schema.ts";
import { SCHEMA_VERSION } from "./config.ts";

export class VaultDB {
  private db: DatabaseSync;

  constructor(dbPath: string) {
    this.db = new DatabaseSync(dbPath);
    this.db.exec("PRAGMA journal_mode=WAL");
    this.db.exec("PRAGMA busy_timeout=5000");
    this.db.exec("PRAGMA foreign_keys=ON");
    this.migrate();
  }

  private migrate(): void {
    // Check if schema_version table exists
    const row = this.db
      .prepare(
        "SELECT name FROM sqlite_master WHERE type='table' AND name='schema_version'"
      )
      .get() as { name: string } | undefined;

    if (!row) {
      // Fresh database: apply full schema
      this.db.exec(SCHEMA_SQL);
      this.db
        .prepare("INSERT INTO schema_version (version) VALUES (?)")
        .run(SCHEMA_VERSION);
      return;
    }

    const versionRow = this.db
      .prepare("SELECT MAX(version) as version FROM schema_version")
      .get() as { version: number } | undefined;

    const currentVersion = versionRow?.version ?? 0;
    if (currentVersion < 2) {
      this.db.exec(`
        CREATE TABLE IF NOT EXISTS images (
            id         INTEGER PRIMARY KEY AUTOINCREMENT,
            session_id TEXT NOT NULL REFERENCES sessions(session_id),
            message_uuid TEXT,
            image_index INTEGER DEFAULT 0,
            media_type TEXT NOT NULL,
            data       BLOB NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_images_session_message ON images(session_id, message_uuid);
      `);
    }
    if (currentVersion < SCHEMA_VERSION) {
      this.db
        .prepare("INSERT INTO schema_version (version) VALUES (?)")
        .run(SCHEMA_VERSION);
    }
  }

  /** Check if a session already exists and return its message count */
  sessionExists(sessionId: string): number | null {
    const row = this.db
      .prepare("SELECT message_count FROM sessions WHERE session_id = ?")
      .get(sessionId) as { message_count: number } | undefined;
    return row?.message_count ?? null;
  }

  /** Insert a session record */
  insertSession(params: {
    sessionId: string;
    project: string;
    projectPath: string;
    gitBranch: string;
    firstPrompt: string;
    summary?: string;
    messageCount: number;
    startedAt: string;
    endedAt: string;
    claudeVersion: string;
  }): void {
    this.db
      .prepare(
        `INSERT INTO sessions (session_id, project, project_path, git_branch, first_prompt, summary, message_count, started_at, ended_at, claude_version)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
      )
      .run(
        params.sessionId,
        params.project,
        params.projectPath,
        params.gitBranch,
        params.firstPrompt,
        params.summary ?? null,
        params.messageCount,
        params.startedAt,
        params.endedAt,
        params.claudeVersion
      );
  }

  /** Insert a message, ignoring duplicates by uuid */
  insertMessage(params: {
    sessionId: string;
    uuid: string;
    role: string;
    blockType?: string;
    content: string;
    toolName?: string;
    toolInput?: string;
    timestamp: string;
    turnIndex: number;
  }): void {
    this.db
      .prepare(
        `INSERT OR IGNORE INTO messages (session_id, uuid, role, block_type, content, tool_name, tool_input, timestamp, turn_index)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
      )
      .run(
        params.sessionId,
        params.uuid,
        params.role,
        params.blockType ?? "text",
        params.content,
        params.toolName ?? null,
        params.toolInput ?? null,
        params.timestamp,
        params.turnIndex
      );
  }

  /** Insert an image blob */
  insertImage(params: {
    sessionId: string;
    messageUuid: string;
    imageIndex: number;
    mediaType: string;
    data: Uint8Array;
  }): void {
    this.db
      .prepare(
        `INSERT INTO images (session_id, message_uuid, image_index, media_type, data)
       VALUES (?, ?, ?, ?, ?)`
      )
      .run(
        params.sessionId,
        params.messageUuid,
        params.imageIndex,
        params.mediaType,
        params.data
      );
  }

  /** Get an image by session, message uuid and index */
  getImage(sessionId: string, messageUuid: string, imageIndex: number): {
    mediaType: string;
    data: Uint8Array;
  } | null {
    const row = this.db
      .prepare(
        `SELECT media_type as mediaType, data FROM images
         WHERE session_id = ? AND message_uuid = ? AND image_index = ?`
      )
      .get(sessionId, messageUuid, imageIndex) as {
      mediaType: string;
      data: Uint8Array;
    } | undefined;
    return row ?? null;
  }

  /** Get the first real user text for a session (skipping tag-only messages) */
  getFirstUserText(sessionId: string): string | null {
    const row = this.db
      .prepare(
        `SELECT content FROM messages
         WHERE session_id = ? AND block_type = 'text' AND role = 'user' AND content NOT LIKE '<%'
         ORDER BY turn_index LIMIT 1`
      )
      .get(sessionId) as { content: string } | undefined;
    return row?.content?.slice(0, 500) ?? null;
  }

  /** Check if images exist for a message */
  hasImages(sessionId: string, messageUuid: string): boolean {
    const row = this.db
      .prepare("SELECT 1 FROM images WHERE session_id = ? AND message_uuid = ? LIMIT 1")
      .get(sessionId, messageUuid);
    return !!row;
  }

  /** Update session message count and ended_at after incremental import */
  updateSessionCounts(
    sessionId: string,
    messageCount: number,
    endedAt: string
  ): void {
    this.db
      .prepare(
        "UPDATE sessions SET message_count = ?, ended_at = ? WHERE session_id = ?"
      )
      .run(messageCount, endedAt, sessionId);
  }

  /** FTS5 search across messages */
  search(
    query: string,
    options: {
      project?: string;
      limit?: number;
      from?: string;
      to?: string;
    } = {}
  ): Array<{
    sessionId: string;
    project: string;
    projectPath: string;
    gitBranch: string;
    startedAt: string;
    role: string;
    content: string;
    timestamp: string;
  }> {
    const limit = options.limit ?? 20;
    // Wrap query in double quotes if it contains special FTS5 characters
    // to prevent hyphens being interpreted as NOT operators etc.
    const safeQuery = /^".*"$/.test(query) || /\b(AND|OR|NOT)\b/.test(query)
      ? query
      : `"${query.replace(/"/g, '""')}"`;
    const conditions: string[] = ["messages_fts MATCH ?"];
    const params: (string | number)[] = [safeQuery];

    if (options.project) {
      conditions.push("(s.project LIKE ? OR s.project_path LIKE ?)");
      params.push(`%${options.project}%`, `%${options.project}%`);
    }
    if (options.from) {
      conditions.push("m.timestamp >= ?");
      params.push(options.from);
    }
    if (options.to) {
      conditions.push("m.timestamp <= ?");
      params.push(options.to);
    }

    params.push(limit);

    const sql = `
      SELECT s.session_id as sessionId, s.project, s.project_path as projectPath,
             s.git_branch as gitBranch,
             s.started_at as startedAt, m.role, m.content, m.timestamp
      FROM messages_fts
      JOIN messages m ON m.id = messages_fts.rowid
      JOIN sessions s ON s.session_id = m.session_id
      WHERE ${conditions.join(" AND ")}
      ORDER BY rank
      LIMIT ?`;

    return this.db.prepare(sql).all(...params) as Array<{
      sessionId: string;
      project: string;
      projectPath: string;
      gitBranch: string;
      startedAt: string;
      role: string;
      content: string;
      timestamp: string;
    }>;
  }

  /** List sessions */
  listSessions(options: {
    project?: string;
    limit?: number;
  } = {}): Array<{
    sessionId: string;
    project: string;
    projectPath: string;
    gitBranch: string;
    firstPrompt: string;
    messageCount: number;
    startedAt: string;
  }> {
    const limit = options.limit ?? 50;
    const conditions: string[] = [];
    const params: (string | number)[] = [];

    if (options.project) {
      conditions.push("(project LIKE ? OR project_path LIKE ?)");
      params.push(`%${options.project}%`, `%${options.project}%`);
    }

    params.push(limit);

    const where = conditions.length > 0 ? `WHERE ${conditions.join(" AND ")}` : "";
    const sql = `
      SELECT session_id as sessionId, project, project_path as projectPath,
             git_branch as gitBranch, first_prompt as firstPrompt,
             message_count as messageCount, started_at as startedAt
      FROM sessions ${where}
      ORDER BY started_at DESC
      LIMIT ?`;

    return this.db.prepare(sql).all(...params) as Array<{
      sessionId: string;
      project: string;
      projectPath: string;
      gitBranch: string;
      firstPrompt: string;
      messageCount: number;
      startedAt: string;
    }>;
  }

  /** Export all messages for a session */
  exportSession(sessionId: string): {
    session: {
      sessionId: string;
      project: string;
      projectPath: string;
      gitBranch: string;
      startedAt: string;
      firstPrompt: string;
      summary: string | null;
    } | null;
    messages: Array<{
      uuid: string;
      role: string;
      blockType: string;
      content: string;
      toolName: string | null;
      toolInput: string | null;
      timestamp: string;
      turnIndex: number;
    }>;
  } {
    const session = this.db
      .prepare(
        `SELECT session_id as sessionId, project, project_path as projectPath,
                git_branch as gitBranch, started_at as startedAt,
                first_prompt as firstPrompt, summary
         FROM sessions WHERE session_id = ? OR session_id LIKE ?`
      )
      .get(sessionId, `${sessionId}%`) as {
      sessionId: string;
      project: string;
      projectPath: string;
      gitBranch: string;
      startedAt: string;
      firstPrompt: string;
      summary: string | null;
    } | undefined;

    if (!session) {
      return { session: null, messages: [] };
    }

    const messages = this.db
      .prepare(
        `SELECT uuid, role, block_type as blockType, content, tool_name as toolName, tool_input as toolInput, timestamp, turn_index as turnIndex
         FROM messages WHERE session_id = ?
         ORDER BY turn_index`
      )
      .all(session.sessionId) as Array<{
      uuid: string;
      role: string;
      blockType: string;
      content: string;
      toolName: string | null;
      toolInput: string | null;
      timestamp: string;
      turnIndex: number;
    }>;

    return { session, messages };
  }

  /** Get aggregate statistics */
  stats(project?: string): {
    totalSessions: number;
    totalMessages: number;
    dbSizeBytes: number;
    byProject: Array<{
      project: string;
      projectPath: string;
      sessions: number;
      messages: number;
    }>;
    byMonth: Array<{
      month: string;
      sessions: number;
      messages: number;
    }>;
  } {
    const projectFilter = project ? "WHERE (s.project LIKE ? OR s.project_path LIKE ?)" : "";
    const projectParam = project ? [`%${project}%`, `%${project}%`] : [];

    const totals = this.db
      .prepare(
        `SELECT COUNT(DISTINCT s.session_id) as totalSessions,
                COALESCE(SUM(s.message_count), 0) as totalMessages
         FROM sessions s ${projectFilter}`
      )
      .get(...projectParam) as {
      totalSessions: number;
      totalMessages: number;
    };

    const byProject = this.db
      .prepare(
        `SELECT project, COALESCE(project_path, project) as projectPath,
                COUNT(*) as sessions,
                COALESCE(SUM(message_count), 0) as messages
         FROM sessions ${project ? "WHERE (project LIKE ? OR project_path LIKE ?)" : ""}
         GROUP BY project ORDER BY sessions DESC`
      )
      .all(...(project ? [`%${project}%`, `%${project}%`] : [])) as Array<{
      project: string;
      projectPath: string;
      sessions: number;
      messages: number;
    }>;

    const byMonth = this.db
      .prepare(
        `SELECT strftime('%Y-%m', started_at) as month,
                COUNT(*) as sessions,
                COALESCE(SUM(message_count), 0) as messages
         FROM sessions ${project ? "WHERE (project LIKE ? OR project_path LIKE ?)" : ""}
         GROUP BY month ORDER BY month DESC`
      )
      .all(...(project ? [`%${project}%`, `%${project}%`] : [])) as Array<{
      month: string;
      sessions: number;
      messages: number;
    }>;

    const sizeRow = this.db
      .prepare("SELECT page_count * page_size as size FROM pragma_page_count(), pragma_page_size()")
      .get() as { size: number } | undefined;

    return {
      ...totals,
      dbSizeBytes: sizeRow?.size ?? 0,
      byProject,
      byMonth,
    };
  }

  close(): void {
    this.db.close();
  }
}
