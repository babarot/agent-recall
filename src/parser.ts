import type {
  ContentBlock,
  ExtractedMessage,
  JournalLine,
  ParsedSession,
  SessionIndex,
  SessionIndexEntry,
  SessionMeta,
} from "./types.ts";

/** Types we want to extract text from */
const EXTRACTABLE_TYPES = new Set(["user", "assistant"]);

/** Types to skip entirely */
const SKIP_TYPES = new Set([
  "system",
  "file-history-snapshot",
  "progress",
  "queue-operation",
  "last-prompt",
  "pr-link",
]);

/** Extract plain text from message content (string or ContentBlock array) */
function extractText(content: string | ContentBlock[]): string {
  if (typeof content === "string") {
    return content.trim();
  }

  if (!Array.isArray(content)) {
    return "";
  }

  return content
    .filter((block): block is { type: "text"; text: string } =>
      block.type === "text" && typeof (block as { text?: string }).text === "string"
    )
    .map((block) => block.text)
    .join("\n")
    .trim();
}

/** Parse a session JSONL file into structured data */
export function parseSession(
  jsonlContent: string,
  project: string,
  indexEntry?: SessionIndexEntry
): ParsedSession | null {
  const lines = jsonlContent.split("\n").filter((line) => line.trim());
  const messages: ExtractedMessage[] = [];
  let meta: Partial<SessionMeta> = {
    project,
    projectPath: "",
    gitBranch: "",
    firstPrompt: "",
    startedAt: "",
    endedAt: "",
    claudeVersion: "",
    sessionId: "",
  };

  let turnIndex = 0;

  for (const line of lines) {
    let parsed: JournalLine;
    try {
      parsed = JSON.parse(line);
    } catch {
      continue;
    }

    // Skip non-extractable types
    if (SKIP_TYPES.has(parsed.type)) continue;
    if (!EXTRACTABLE_TYPES.has(parsed.type)) continue;

    // Skip sidechains (abandoned conversation branches)
    if (parsed.isSidechain) continue;

    // Extract metadata from first message
    if (!meta.sessionId && parsed.sessionId) {
      meta.sessionId = parsed.sessionId;
      meta.projectPath = parsed.cwd ?? "";
      meta.gitBranch = parsed.gitBranch ?? "";
      meta.claudeVersion = parsed.version ?? "";
      meta.startedAt = parsed.timestamp ?? "";
    }

    // Track end time
    if (parsed.timestamp) {
      meta.endedAt = parsed.timestamp;
    }

    // Extract text content
    if (!parsed.message?.content) continue;
    const text = extractText(parsed.message.content);
    if (!text) continue;

    // Capture first user prompt
    if (!meta.firstPrompt && parsed.type === "user") {
      meta.firstPrompt = text.slice(0, 500);
    }

    if (!parsed.uuid) continue;

    messages.push({
      uuid: parsed.uuid,
      role: parsed.message.role,
      content: text,
      timestamp: parsed.timestamp ?? "",
      turnIndex: turnIndex++,
    });
  }

  if (!meta.sessionId || messages.length === 0) {
    return null;
  }

  // Enrich from sessions-index.json if available
  if (indexEntry) {
    if (indexEntry.firstPrompt) {
      meta.firstPrompt = indexEntry.firstPrompt;
    }
    if (indexEntry.summary) {
      meta.summary = indexEntry.summary;
    }
    if (indexEntry.gitBranch && !meta.gitBranch) {
      meta.gitBranch = indexEntry.gitBranch;
    }
    if (indexEntry.projectPath && !meta.projectPath) {
      meta.projectPath = indexEntry.projectPath;
    }
  }

  return {
    meta: meta as SessionMeta,
    messages,
  };
}

/** Load and parse sessions-index.json for a project directory */
export function loadSessionIndex(
  projectDir: string
): Map<string, SessionIndexEntry> {
  const indexPath = `${projectDir}/sessions-index.json`;
  const map = new Map<string, SessionIndexEntry>();

  try {
    const content = Deno.readTextFileSync(indexPath);
    const index: SessionIndex = JSON.parse(content);
    for (const entry of index.entries) {
      map.set(entry.sessionId, entry);
    }
  } catch {
    // sessions-index.json may not exist for all projects
  }

  return map;
}

/** Discover all JSONL session files under the projects directory */
export function discoverSessions(
  projectsDir: string
): Array<{ project: string; sessionId: string; filePath: string }> {
  const results: Array<{
    project: string;
    sessionId: string;
    filePath: string;
  }> = [];

  try {
    for (const projectEntry of Deno.readDirSync(projectsDir)) {
      if (!projectEntry.isDirectory) continue;

      const projectDir = `${projectsDir}/${projectEntry.name}`;
      try {
        for (const fileEntry of Deno.readDirSync(projectDir)) {
          if (!fileEntry.isFile || !fileEntry.name.endsWith(".jsonl")) continue;
          const sessionId = fileEntry.name.replace(".jsonl", "");
          results.push({
            project: projectEntry.name,
            sessionId,
            filePath: `${projectDir}/${fileEntry.name}`,
          });
        }
      } catch {
        // Skip unreadable project directories
      }
    }
  } catch {
    // Projects directory may not exist
  }

  return results;
}
