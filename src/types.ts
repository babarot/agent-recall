/** JSONL line types found in Claude Code session files */
export type JournalLineType =
  | "user"
  | "assistant"
  | "system"
  | "progress"
  | "file-history-snapshot"
  | "queue-operation"
  | "last-prompt"
  | "pr-link";

/** Content block types in assistant messages */
export interface TextBlock {
  type: "text";
  text: string;
}

export interface ToolUseBlock {
  type: "tool_use";
  id: string;
  name: string;
  input: Record<string, unknown>;
}

export interface ToolResultBlock {
  type: "tool_result";
  content?: string | unknown[];
}

export interface ThinkingBlock {
  type: "thinking";
  thinking: string;
  signature?: string;
}

export interface ImageBlock {
  type: "image";
  source: {
    type: "base64";
    media_type: string;
    data: string;
  };
}

export type ContentBlock =
  | TextBlock
  | ToolUseBlock
  | ToolResultBlock
  | ThinkingBlock
  | ImageBlock;

/** A single line from a session JSONL file */
export interface JournalLine {
  type: JournalLineType;
  uuid?: string;
  parentUuid?: string | null;
  sessionId?: string;
  timestamp?: string;
  cwd?: string;
  version?: string;
  gitBranch?: string;
  isSidechain?: boolean;
  /**
   * `true` when Claude Code itself wrote the message rather than the human.
   * Used for slash command / skill expansions, injected `## Context`
   * blocks, and bash caveat wrappers. We collapse these to
   * `blockType: "meta"` during parsing so the UI can render them as a
   * compact folded box instead of a giant "You" bubble.
   */
  isMeta?: boolean;
  /**
   * System-injected lines that arrive as `type: "user"` but weren't typed by
   * the human. Currently the only observed kind is `"task-notification"`
   * (background command completion/failure), which we collapse into a meta
   * bubble just like `isMeta` expansions.
   */
  origin?: { kind?: string };
  message?: {
    role: "user" | "assistant";
    content: string | ContentBlock[];
  };
  subtype?: string;
}

export type BlockType = "text" | "thinking" | "tool_use" | "tool_result" | "meta";

/** Extracted message ready for DB insertion */
export interface ExtractedMessage {
  uuid: string;
  role: "user" | "assistant";
  blockType: BlockType;
  content: string;
  toolName?: string;
  toolInput?: string;
  timestamp: string;
  turnIndex: number;
}

/** Session metadata extracted from JSONL + sessions-index.json */
export interface SessionMeta {
  sessionId: string;
  project: string;
  projectPath: string;
  gitBranch: string;
  firstPrompt: string;
  summary?: string;
  startedAt: string;
  endedAt: string;
  claudeVersion: string;
}

/** Extracted image from a message */
export interface ExtractedImage {
  messageUuid: string;
  imageIndex: number;
  mediaType: string;
  data: string; // base64
}

/** Parsed session result */
export interface ParsedSession {
  meta: SessionMeta;
  messages: ExtractedMessage[];
  images: ExtractedImage[];
}

/** Entry in sessions-index.json */
export interface SessionIndexEntry {
  sessionId: string;
  fullPath: string;
  firstPrompt?: string;
  summary?: string;
  messageCount?: number;
  created?: string;
  modified?: string;
  gitBranch?: string;
  projectPath?: string;
}

export interface SessionIndex {
  version: number;
  entries: SessionIndexEntry[];
  originalPath?: string;
}
