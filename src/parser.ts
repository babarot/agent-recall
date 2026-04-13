import type {
  ContentBlock,
  ExtractedImage,
  ExtractedMessage,
  ImageBlock,
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

/** Metadata captured from the first meaningful journal line */
export interface JournalHeader {
  sessionId: string;
  projectPath: string;
  gitBranch: string;
  claudeVersion: string;
  startedAt: string;
}

/** Result of parsing raw JSONL lines (stateless about session identity) */
export interface ParsedJournalLines {
  messages: ExtractedMessage[];
  images: ExtractedImage[];
  /** Header from the first journal line that carried sessionId info */
  header?: JournalHeader;
  /** First user-facing text seen (raw, before truncation) — feeds SessionMeta.firstPrompt */
  firstUserText?: string;
  /** Timestamp of the last user message — feeds SessionMeta.endedAt.
   *  Only user messages are tracked so that assistant activity (which can
   *  run in parallel across sessions) doesn't churn the sort order. */
  lastTimestamp?: string;
}

/**
 * Parse raw JSONL content into messages/images without any session-level state.
 *
 * This is the building block for both full-file parsing (`parseSession`) and
 * tail-based incremental imports driven by the FS watcher. Callers pass
 * `startTurnIndex` to continue numbering from an existing session's message
 * count when doing tail reads.
 */
export function parseJournalLines(
  jsonlContent: string,
  startTurnIndex = 0
): ParsedJournalLines {
  const lines = jsonlContent.split("\n").filter((line) => line.trim());
  const messages: ExtractedMessage[] = [];
  const images: ExtractedImage[] = [];
  let header: JournalHeader | undefined;
  let firstUserText: string | undefined;
  let lastTimestamp: string | undefined;

  let turnIndex = startTurnIndex;

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

    // Capture header from the first line that carries session identity
    if (!header && parsed.sessionId) {
      header = {
        sessionId: parsed.sessionId,
        projectPath: parsed.cwd ?? "",
        gitBranch: parsed.gitBranch ?? "",
        claudeVersion: parsed.version ?? "",
        startedAt: parsed.timestamp ?? "",
      };
    }

    if (!parsed.uuid || !parsed.message?.content) continue;

    const content = parsed.message.content;
    const role = parsed.message.role;
    const ts = parsed.timestamp ?? "";

    // Synthetic "meta" / system-injected messages are collapsed into a single
    // `blockType: "meta"` entry. Two sources qualify:
    //   - `isMeta: true` — slash command / skill expansions, injected
    //     ## Context blocks, local-command-caveat wrappers.
    //   - `origin.kind === "task-notification"` — background command
    //     completion/failure notifications that arrive as a `type: "user"`
    //     line but aren't human-typed input.
    // The UI renders these as a compact folded box rather than a giant user
    // bubble. The raw text is preserved so FTS5 search can hit it.
    //
    // NOTE: `lastTimestamp` is intentionally updated AFTER this branch, so
    // these system-injected lines don't advance `endedAt` and don't reshuffle
    // sidebar session order.
    const isSystemInjected =
      parsed.isMeta === true || parsed.origin?.kind === "task-notification";

    if (isSystemInjected) {
      const text = extractText(content);
      if (text) {
        messages.push({
          uuid: parsed.uuid,
          role,
          blockType: "meta",
          content: text,
          timestamp: ts,
          turnIndex: turnIndex++,
        });
      }
      continue;
    }

    // Track last user activity time — only real user messages (not
    // system-injected meta/notifications) count so that parallel assistant
    // runs and background notifications don't constantly reshuffle session
    // order.
    if (parsed.timestamp && parsed.type === "user") {
      lastTimestamp = parsed.timestamp;
    }

    if (typeof content === "string") {
      // Simple string content (user messages)
      const text = content.trim();
      if (!text) continue;
      if (!firstUserText && parsed.type === "user") {
        firstUserText = text;
      }
      messages.push({ uuid: parsed.uuid, role, blockType: "text", content: text, timestamp: ts, turnIndex: turnIndex++ });
      continue;
    }

    if (!Array.isArray(content)) continue;

    // Process content blocks
    let imgIdx = 0;
    for (const block of content) {
      if (block.type === "text") {
        const text = (block as { text?: string }).text?.trim();
        if (!text) continue;
        if (!firstUserText && parsed.type === "user") {
          firstUserText = text;
        }
        messages.push({ uuid: parsed.uuid, role, blockType: "text", content: text, timestamp: ts, turnIndex: turnIndex++ });
      } else if (block.type === "thinking") {
        const thinking = (block as { thinking?: string }).thinking?.trim();
        if (!thinking) continue;
        messages.push({ uuid: parsed.uuid, role, blockType: "thinking", content: thinking, timestamp: ts, turnIndex: turnIndex++ });
      } else if (block.type === "tool_use") {
        const toolBlock = block as { name?: string; input?: Record<string, unknown> };
        const name = toolBlock.name ?? "unknown";
        const input = toolBlock.input ? JSON.stringify(toolBlock.input) : "";
        messages.push({ uuid: parsed.uuid, role, blockType: "tool_use", content: name, toolName: name, toolInput: input, timestamp: ts, turnIndex: turnIndex++ });
      } else if (block.type === "tool_result") {
        const resultBlock = block as { content?: string | unknown[] };
        let text = "";
        if (typeof resultBlock.content === "string") {
          text = resultBlock.content;
        } else if (Array.isArray(resultBlock.content)) {
          text = resultBlock.content
            .filter((b): b is { type: "text"; text: string } => (b as { type?: string }).type === "text")
            .map((b) => b.text)
            .join("\n");
        }
        if (text.length > 10000) text = text.slice(0, 10000) + "\n... (truncated)";
        messages.push({ uuid: parsed.uuid, role, blockType: "tool_result", content: text, timestamp: ts, turnIndex: turnIndex++ });
      } else if (block.type === "image" && (block as ImageBlock).source?.type === "base64") {
        const imgBlock = block as ImageBlock;
        images.push({
          messageUuid: parsed.uuid,
          imageIndex: imgIdx++,
          mediaType: imgBlock.source.media_type,
          data: imgBlock.source.data,
        });
      }
    }
  }

  return { messages, images, header, firstUserText, lastTimestamp };
}

/** Parse a session JSONL file into structured data */
export function parseSession(
  jsonlContent: string,
  project: string,
  indexEntry?: SessionIndexEntry
): ParsedSession | null {
  const result = parseJournalLines(jsonlContent, 0);

  if (!result.header || result.messages.length === 0) {
    return null;
  }

  const meta: SessionMeta = {
    sessionId: result.header.sessionId,
    project,
    projectPath: result.header.projectPath,
    gitBranch: result.header.gitBranch,
    firstPrompt: result.firstUserText?.slice(0, 500) ?? "",
    startedAt: result.header.startedAt,
    endedAt: result.lastTimestamp ?? "",
    claudeVersion: result.header.claudeVersion,
  };

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
    meta,
    messages: result.messages,
    images: result.images,
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
