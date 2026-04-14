import {
  assertEquals,
  assertNotEquals,
} from "https://deno.land/std@0.224.0/assert/mod.ts";
import { join } from "@std/path";
import { VaultDB } from "./db.ts";
import { importSingleSessionIncremental } from "./import.ts";

/** Build a user JSONL line */
function userLine(
  text: string,
  uuid: string,
  ts: string,
  sessionId = "sess-001",
): string {
  return JSON.stringify({
    type: "user",
    uuid,
    sessionId,
    timestamp: ts,
    cwd: "/home/user/project",
    version: "2.1.87",
    gitBranch: "main",
    isSidechain: false,
    message: { role: "user", content: text },
  });
}

/** Build an assistant JSONL line */
function assistantLine(
  text: string,
  uuid: string,
  ts: string,
  sessionId = "sess-001",
): string {
  return JSON.stringify({
    type: "assistant",
    uuid,
    sessionId,
    timestamp: ts,
    isSidechain: false,
    message: {
      role: "assistant",
      content: [{ type: "text", text }],
    },
  });
}

/** Create a temporary projects-layout directory with a single JSONL file */
function withTempSession(
  lines: string[],
  fn: (filePath: string, db: VaultDB) => void,
): void {
  const tempDir = Deno.makeTempDirSync();
  try {
    const projectDir = join(tempDir, "my-project");
    Deno.mkdirSync(projectDir);
    const filePath = join(projectDir, "sess-001.jsonl");
    Deno.writeTextFileSync(filePath, lines.join("\n") + "\n");

    const db = new VaultDB(":memory:");
    try {
      fn(filePath, db);
    } finally {
      db.close();
    }
  } finally {
    Deno.removeSync(tempDir, { recursive: true });
  }
}

// --- New session path ---

Deno.test("importSingleSessionIncremental imports a new session fully", () => {
  withTempSession(
    [
      userLine("hello", "u1", "2026-01-01T00:00:00Z"),
      assistantLine("hi", "a1", "2026-01-01T00:00:01Z"),
    ],
    (filePath, db) => {
      const result = importSingleSessionIncremental(db, filePath);
      assertNotEquals(result, null);
      assertEquals(result!.status, "new");
      assertEquals(result!.addedMessages, 2);
      assertEquals(result!.totalMessages, 2);
      assertEquals(db.sessionExists("sess-001"), 2);
    },
  );
});

// --- Idempotency: same content twice ---

Deno.test("importSingleSessionIncremental is idempotent on unchanged file", () => {
  withTempSession(
    [
      userLine("hello", "u1", "2026-01-01T00:00:00Z"),
      assistantLine("hi", "a1", "2026-01-01T00:00:01Z"),
    ],
    (filePath, db) => {
      const first = importSingleSessionIncremental(db, filePath);
      assertEquals(first!.status, "new");

      const second = importSingleSessionIncremental(db, filePath);
      // Second import skips because file mtime/size haven't changed.
      assertEquals(second!.status, "unchanged");
      assertEquals(db.sessionExists("sess-001"), 2);

      // turn_index sequence is preserved (same parser → same values).
      const { messages } = db.exportSession("sess-001");
      assertEquals(messages.length, 2);
      assertEquals(messages[0].turnIndex, 0);
      assertEquals(messages[1].turnIndex, 1);
    },
  );
});

// --- Append path ---

Deno.test("importSingleSessionIncremental imports appended messages", () => {
  withTempSession(
    [
      userLine("hello", "u1", "2026-01-01T00:00:00Z"),
      assistantLine("hi", "a1", "2026-01-01T00:00:01Z"),
    ],
    (filePath, db) => {
      importSingleSessionIncremental(db, filePath);
      assertEquals(db.sessionExists("sess-001"), 2);

      const appendLines = [
        userLine("how are you?", "u2", "2026-01-01T00:00:02Z"),
        assistantLine("good!", "a2", "2026-01-01T00:00:03Z"),
      ].join("\n") + "\n";
      Deno.writeTextFileSync(filePath, appendLines, { append: true });

      const second = importSingleSessionIncremental(db, filePath);
      assertEquals(second!.status, "resynced");
      assertEquals(second!.totalMessages, 4);
      assertEquals(db.sessionExists("sess-001"), 4);

      const { messages } = db.exportSession("sess-001");
      assertEquals(messages.length, 4);
      assertEquals(messages[2].content, "how are you?");
      assertEquals(messages[3].content, "good!");
    },
  );
});

// --- /compact-style in-place rewrite: file shrinks ---

Deno.test("importSingleSessionIncremental mirrors the file when it shrinks", () => {
  withTempSession(
    [
      userLine("hello", "u1", "2026-01-01T00:00:00Z"),
      assistantLine("hi", "a1", "2026-01-01T00:00:01Z"),
    ],
    (filePath, db) => {
      importSingleSessionIncremental(db, filePath);
      assertEquals(db.sessionExists("sess-001"), 2);

      // Rewrite the file from scratch — simulates /compact output.
      Deno.writeTextFileSync(
        filePath,
        userLine("rewritten", "u9", "2026-01-01T00:00:05Z") + "\n",
      );

      const second = importSingleSessionIncremental(db, filePath);
      assertEquals(second!.status, "resynced");
      assertEquals(db.sessionExists("sess-001"), 1);

      const { messages } = db.exportSession("sess-001");
      assertEquals(messages.length, 1);
      assertEquals(messages[0].content, "rewritten");
      // Old uuids must be gone — DB is a mirror, not an append log.
      assertEquals(messages[0].uuid, "u9");
    },
  );
});

// --- /compact-style in-place rewrite: file keeps some uuids, drops others ---

Deno.test("importSingleSessionIncremental drops rows whose uuid disappeared from the file", () => {
  withTempSession(
    [
      userLine("one", "u1", "2026-01-01T00:00:00Z"),
      userLine("two", "u2", "2026-01-01T00:00:01Z"),
      userLine("three", "u3", "2026-01-01T00:00:02Z"),
    ],
    (filePath, db) => {
      importSingleSessionIncremental(db, filePath);
      assertEquals(db.sessionExists("sess-001"), 3);

      // Rewrite keeping only u1 and a brand-new u4 (u2, u3 removed).
      Deno.writeTextFileSync(
        filePath,
        userLine("one", "u1", "2026-01-01T00:00:00Z") + "\n" +
          userLine("four", "u4", "2026-01-01T00:00:10Z") + "\n",
      );

      importSingleSessionIncremental(db, filePath);

      const { messages } = db.exportSession("sess-001");
      assertEquals(messages.length, 2);
      const uuids = messages.map((m) => m.uuid).sort();
      assertEquals(uuids, ["u1", "u4"]);
    },
  );
});

// --- Regression: message_count corruption recovery ---

Deno.test("importSingleSessionIncremental skips unchanged file even with corrupted message_count", () => {
  withTempSession(
    [
      userLine("one", "u1", "2026-01-01T00:00:00Z"),
      userLine("two", "u2", "2026-01-01T00:00:01Z"),
      userLine("three", "u3", "2026-01-01T00:00:02Z"),
    ],
    (filePath, db) => {
      importSingleSessionIncremental(db, filePath);
      assertEquals(db.sessionExists("sess-001"), 3);

      // Simulate the stale-message_count state.
      db.updateSessionCounts("sess-001", 0, "2026-01-01T00:00:02Z");
      assertEquals(db.sessionExists("sess-001"), 0);

      // File hasn't changed, so re-import is skipped (mtime/size match).
      const result = importSingleSessionIncremental(db, filePath);
      assertEquals(result!.status, "unchanged");

      // A file modification triggers a full resync that heals the counter.
      Deno.writeTextFileSync(filePath, Deno.readTextFileSync(filePath), {});
      const healed = importSingleSessionIncremental(db, filePath);
      assertEquals(healed!.status, "resynced");
      assertEquals(db.sessionExists("sess-001"), 3);
      const { messages } = db.exportSession("sess-001");
      assertEquals(messages.length, 3);
    },
  );
});

// --- Project / sessionId derivation from filepath ---

Deno.test("importSingleSessionIncremental derives sessionId and project from path", () => {
  const tempDir = Deno.makeTempDirSync();
  try {
    const projectDir = join(tempDir, "-Users-bob-src-myapp");
    Deno.mkdirSync(projectDir);
    const filePath = join(projectDir, "abc-123-def.jsonl");
    Deno.writeTextFileSync(
      filePath,
      userLine("test", "u1", "2026-01-01T00:00:00Z", "abc-123-def") + "\n",
    );

    const db = new VaultDB(":memory:");
    try {
      const result = importSingleSessionIncremental(db, filePath);
      assertNotEquals(result, null);
      assertEquals(result!.sessionId, "abc-123-def");
      assertEquals(result!.project, "-Users-bob-src-myapp");
    } finally {
      db.close();
    }
  } finally {
    Deno.removeSync(tempDir, { recursive: true });
  }
});

// --- Error handling ---

Deno.test("importSingleSessionIncremental returns null for missing file", () => {
  const db = new VaultDB(":memory:");
  try {
    const result = importSingleSessionIncremental(
      db,
      "/nonexistent/path/fake.jsonl",
    );
    assertEquals(result, null);
  } finally {
    db.close();
  }
});
