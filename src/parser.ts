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

/** Parse a session JSONL file into structured data */
export function parseSession(
  jsonlContent: string,
  project: string,
  indexEntry?: SessionIndexEntry
): ParsedSession | null {
  const lines = jsonlContent.split("\n").filter((line) => line.trim());
  const messages: ExtractedMessage[] = [];
  const images: ExtractedImage[] = [];
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

    if (!parsed.uuid || !parsed.message?.content) continue;

    const content = parsed.message.content;
    const role = parsed.message.role;
    const ts = parsed.timestamp ?? "";

    if (typeof content === "string") {
      // Simple string content (user messages)
      const text = content.trim();
      if (!text) continue;
      if (!meta.firstPrompt && parsed.type === "user") {
        meta.firstPrompt = text.slice(0, 500);
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
        if (!meta.firstPrompt && parsed.type === "user") {
          meta.firstPrompt = text.slice(0, 500);
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
    images,
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
