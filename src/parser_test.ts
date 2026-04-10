import { assertEquals, assertNotEquals } from "https://deno.land/std@0.224.0/assert/mod.ts";
import { parseSession } from "./parser.ts";

function line(obj: Record<string, unknown>): string {
  return JSON.stringify(obj);
}

const USER_MSG = (text: string, uuid: string, ts: string) =>
  line({
    type: "user",
    uuid,
    sessionId: "sess-001",
    timestamp: ts,
    cwd: "/home/user/project",
    version: "2.1.87",
    gitBranch: "main",
    isSidechain: false,
    message: { role: "user", content: text },
  });

const ASSISTANT_MSG = (blocks: unknown[], uuid: string, ts: string) =>
  line({
    type: "assistant",
    uuid,
    sessionId: "sess-001",
    timestamp: ts,
    isSidechain: false,
    message: { role: "assistant", content: blocks },
  });

const SYSTEM_MSG = line({
  type: "system",
  subtype: "turn_duration",
  durationMs: 5000,
  timestamp: "2026-01-01T00:00:05Z",
  sessionId: "sess-001",
});

const SNAPSHOT_MSG = line({
  type: "file-history-snapshot",
  messageId: "msg-1",
  snapshot: {},
});

// --- parseSession tests ---

Deno.test("parseSession extracts user and assistant text messages", () => {
  const jsonl = [
    USER_MSG("Hello world", "u1", "2026-01-01T00:00:00Z"),
    ASSISTANT_MSG(
      [{ type: "text", text: "Hi there!" }],
      "a1",
      "2026-01-01T00:00:01Z"
    ),
  ].join("\n");

  const result = parseSession(jsonl, "test-project");
  assertNotEquals(result, null);
  assertEquals(result!.messages.length, 2);
  assertEquals(result!.messages[0].role, "user");
  assertEquals(result!.messages[0].content, "Hello world");
  assertEquals(result!.messages[1].role, "assistant");
  assertEquals(result!.messages[1].content, "Hi there!");
});

Deno.test("parseSession extracts metadata from first message", () => {
  const jsonl = USER_MSG("test", "u1", "2026-01-01T00:00:00Z");
  const result = parseSession(jsonl, "my-project");

  assertNotEquals(result, null);
  assertEquals(result!.meta.sessionId, "sess-001");
  assertEquals(result!.meta.project, "my-project");
  assertEquals(result!.meta.projectPath, "/home/user/project");
  assertEquals(result!.meta.gitBranch, "main");
  assertEquals(result!.meta.claudeVersion, "2.1.87");
  assertEquals(result!.meta.firstPrompt, "test");
});

Deno.test("parseSession filters out system and snapshot messages", () => {
  const jsonl = [
    USER_MSG("hello", "u1", "2026-01-01T00:00:00Z"),
    SYSTEM_MSG,
    SNAPSHOT_MSG,
    ASSISTANT_MSG(
      [{ type: "text", text: "response" }],
      "a1",
      "2026-01-01T00:00:02Z"
    ),
  ].join("\n");

  const result = parseSession(jsonl, "test");
  assertNotEquals(result, null);
  assertEquals(result!.messages.length, 2);
  assertEquals(result!.messages[0].content, "hello");
  assertEquals(result!.messages[1].content, "response");
});

Deno.test("parseSession filters out tool_use and thinking from assistant messages", () => {
  const jsonl = [
    USER_MSG("do something", "u1", "2026-01-01T00:00:00Z"),
    ASSISTANT_MSG(
      [
        { type: "thinking", thinking: "let me think..." },
        { type: "tool_use", id: "t1", name: "Bash", input: { command: "ls" } },
        { type: "text", text: "Done!" },
      ],
      "a1",
      "2026-01-01T00:00:01Z"
    ),
  ].join("\n");

  const result = parseSession(jsonl, "test");
  assertNotEquals(result, null);
  assertEquals(result!.messages.length, 2);
  assertEquals(result!.messages[1].content, "Done!");
});

Deno.test("parseSession skips assistant messages with only tool_use (no text)", () => {
  const jsonl = [
    USER_MSG("do something", "u1", "2026-01-01T00:00:00Z"),
    ASSISTANT_MSG(
      [{ type: "tool_use", id: "t1", name: "Read", input: {} }],
      "a1",
      "2026-01-01T00:00:01Z"
    ),
    ASSISTANT_MSG(
      [{ type: "text", text: "Here is the result" }],
      "a2",
      "2026-01-01T00:00:02Z"
    ),
  ].join("\n");

  const result = parseSession(jsonl, "test");
  assertNotEquals(result, null);
  assertEquals(result!.messages.length, 2);
  assertEquals(result!.messages[0].content, "do something");
  assertEquals(result!.messages[1].content, "Here is the result");
});

Deno.test("parseSession skips sidechain messages", () => {
  const jsonl = [
    USER_MSG("hello", "u1", "2026-01-01T00:00:00Z"),
    line({
      type: "user",
      uuid: "u2",
      sessionId: "sess-001",
      timestamp: "2026-01-01T00:00:01Z",
      isSidechain: true,
      message: { role: "user", content: "sidechain message" },
    }),
    ASSISTANT_MSG(
      [{ type: "text", text: "reply" }],
      "a1",
      "2026-01-01T00:00:02Z"
    ),
  ].join("\n");

  const result = parseSession(jsonl, "test");
  assertNotEquals(result, null);
  assertEquals(result!.messages.length, 2);
  assertEquals(result!.messages[0].content, "hello");
  assertEquals(result!.messages[1].content, "reply");
});

Deno.test("parseSession assigns sequential turnIndex", () => {
  const jsonl = [
    USER_MSG("first", "u1", "2026-01-01T00:00:00Z"),
    ASSISTANT_MSG(
      [{ type: "text", text: "second" }],
      "a1",
      "2026-01-01T00:00:01Z"
    ),
    USER_MSG("third", "u2", "2026-01-01T00:00:02Z"),
  ].join("\n");

  const result = parseSession(jsonl, "test");
  assertNotEquals(result, null);
  assertEquals(result!.messages[0].turnIndex, 0);
  assertEquals(result!.messages[1].turnIndex, 1);
  assertEquals(result!.messages[2].turnIndex, 2);
});

Deno.test("parseSession tracks endedAt as last message timestamp", () => {
  const jsonl = [
    USER_MSG("first", "u1", "2026-01-01T00:00:00Z"),
    ASSISTANT_MSG(
      [{ type: "text", text: "last" }],
      "a1",
      "2026-01-01T00:05:00Z"
    ),
  ].join("\n");

  const result = parseSession(jsonl, "test");
  assertNotEquals(result, null);
  assertEquals(result!.meta.startedAt, "2026-01-01T00:00:00Z");
  assertEquals(result!.meta.endedAt, "2026-01-01T00:05:00Z");
});

Deno.test("parseSession returns null for empty input", () => {
  assertEquals(parseSession("", "test"), null);
  assertEquals(parseSession("\n\n", "test"), null);
});

Deno.test("parseSession returns null for only system messages", () => {
  const jsonl = [SYSTEM_MSG, SNAPSHOT_MSG].join("\n");
  assertEquals(parseSession(jsonl, "test"), null);
});

Deno.test("parseSession enriches from session index entry", () => {
  const jsonl = USER_MSG("hello", "u1", "2026-01-01T00:00:00Z");
  const result = parseSession(jsonl, "test", {
    sessionId: "sess-001",
    fullPath: "/path/to/sess.jsonl",
    firstPrompt: "enriched prompt",
    summary: "a summary from index",
    gitBranch: "feature-branch",
    projectPath: "/enriched/path",
  });

  assertNotEquals(result, null);
  assertEquals(result!.meta.firstPrompt, "enriched prompt");
  assertEquals(result!.meta.summary, "a summary from index");
});

Deno.test("parseSession concatenates multiple text blocks", () => {
  const jsonl = [
    USER_MSG("question", "u1", "2026-01-01T00:00:00Z"),
    ASSISTANT_MSG(
      [
        { type: "text", text: "First paragraph." },
        { type: "tool_use", id: "t1", name: "Read", input: {} },
        { type: "text", text: "Second paragraph." },
      ],
      "a1",
      "2026-01-01T00:00:01Z"
    ),
  ].join("\n");

  const result = parseSession(jsonl, "test");
  assertNotEquals(result, null);
  assertEquals(
    result!.messages[1].content,
    "First paragraph.\nSecond paragraph."
  );
});

Deno.test("parseSession truncates firstPrompt to 500 chars", () => {
  const longText = "a".repeat(600);
  const jsonl = USER_MSG(longText, "u1", "2026-01-01T00:00:00Z");
  const result = parseSession(jsonl, "test");

  assertNotEquals(result, null);
  assertEquals(result!.meta.firstPrompt.length, 500);
});

Deno.test("parseSession skips malformed JSON lines", () => {
  const jsonl = [
    "not valid json",
    USER_MSG("valid message", "u1", "2026-01-01T00:00:00Z"),
    "{ broken",
  ].join("\n");

  const result = parseSession(jsonl, "test");
  assertNotEquals(result, null);
  assertEquals(result!.messages.length, 1);
  assertEquals(result!.messages[0].content, "valid message");
});
