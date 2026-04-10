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

export type ContentBlock =
  | TextBlock
  | ToolUseBlock
  | ToolResultBlock
  | ThinkingBlock;

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
  message?: {
    role: "user" | "assistant";
    content: string | ContentBlock[];
  };
  subtype?: string;
}

/** Extracted message ready for DB insertion */
export interface ExtractedMessage {
  uuid: string;
  role: "user" | "assistant";
  content: string;
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

/** Parsed session result */
export interface ParsedSession {
  meta: SessionMeta;
  messages: ExtractedMessage[];
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
