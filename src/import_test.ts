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
  sessionId = "sess-001"
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
  sessionId = "sess-001"
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
  fn: (filePath: string, db: VaultDB) => void
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

      // imported_bytes should now match the file size
      const size = Deno.statSync(filePath).size;
      assertEquals(db.getSessionImportedBytes("sess-001"), size);
    }
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
      assertEquals(second!.status, "unchanged");
      assertEquals(second!.addedMessages, 0);
      assertEquals(db.sessionExists("sess-001"), 2); // unchanged
    }
  );
});

// --- Append path ---

Deno.test("importSingleSessionIncremental imports appended messages via tail read", () => {
  withTempSession(
    [
      userLine("hello", "u1", "2026-01-01T00:00:00Z"),
      assistantLine("hi", "a1", "2026-01-01T00:00:01Z"),
    ],
    (filePath, db) => {
      const first = importSingleSessionIncremental(db, filePath);
      assertEquals(first!.status, "new");
      assertEquals(first!.totalMessages, 2);

      // Append two more messages
      const appendLines = [
        userLine("how are you?", "u2", "2026-01-01T00:00:02Z"),
        assistantLine("good!", "a2", "2026-01-01T00:00:03Z"),
      ].join("\n") + "\n";
      Deno.writeTextFileSync(filePath, appendLines, { append: true });

      const second = importSingleSessionIncremental(db, filePath);
      assertEquals(second!.status, "updated");
      assertEquals(second!.addedMessages, 2);
      assertEquals(second!.totalMessages, 4);
      assertEquals(db.sessionExists("sess-001"), 4);

      // Verify turn_index continued from 2
      const { messages } = db.exportSession("sess-001");
      assertEquals(messages.length, 4);
      assertEquals(messages[2].turnIndex, 2);
      assertEquals(messages[3].turnIndex, 3);
      assertEquals(messages[2].content, "how are you?");
      assertEquals(messages[3].content, "good!");
    }
  );
});

Deno.test("importSingleSessionIncremental resyncs when the file shrinks", () => {
  withTempSession(
    [
      userLine("hello", "u1", "2026-01-01T00:00:00Z"),
      assistantLine("hi", "a1", "2026-01-01T00:00:01Z"),
    ],
    (filePath, db) => {
      const first = importSingleSessionIncremental(db, filePath);
      assertEquals(first!.status, "new");
      assertEquals(db.sessionExists("sess-001"), 2);

      Deno.writeTextFileSync(
        filePath,
        userLine("rewritten", "u9", "2026-01-01T00:00:05Z") + "\n"
      );

      const second = importSingleSessionIncremental(db, filePath);
      assertEquals(second!.status, "resynced");
      assertEquals(second!.addedMessages, 1);
      assertEquals(second!.totalMessages, 1);
      assertEquals(db.sessionExists("sess-001"), 1);

      const { messages } = db.exportSession("sess-001");
      assertEquals(messages.length, 1);
      assertEquals(messages[0].content, "rewritten");
    }
  );
});

// --- Duplicate protection (critical: message_count must not inflate) ---
//
// The dedup mechanism is UNIQUE(session_id, turn_index). A race where two
// concurrent imports both read the same tail (same startTurnIndex, same
// parsed rows) must collide on that index and produce changes=0 on the
// loser, so addedMessages is counted via `.changes` rather than parse count.

Deno.test("concurrent tail reads do not double-count messages", () => {
  withTempSession(
    [
      userLine("hello", "u1", "2026-01-01T00:00:00Z"),
      assistantLine("hi", "a1", "2026-01-01T00:00:01Z"),
    ],
    (filePath, db) => {
      importSingleSessionIncremental(db, filePath);
      assertEquals(db.sessionExists("sess-001"), 2);

      // Simulate process A and process B both reading the same tail before
      // either has committed: rewind imported_bytes so the second call
      // re-parses the tail while message_count is still at its pre-commit
      // value.
      db.updateSessionImportedBytes("sess-001", 0);
      db.updateSessionCounts("sess-001", 0, "2026-01-01T00:00:01Z");

      const result = importSingleSessionIncremental(db, filePath);
      assertNotEquals(result, null);
      // Parse produces 2 messages, but (session_id, turn_index) already
      // exists, so INSERT OR IGNORE returns changes=0 on every row.
      assertEquals(result!.addedMessages, 0);
      // The physical row count hasn't changed.
      const { messages } = db.exportSession("sess-001");
      assertEquals(messages.length, 2);
    }
  );
});

// --- Incomplete trailing line ---

Deno.test("importSingleSessionIncremental defers incomplete trailing line", () => {
  const tempDir = Deno.makeTempDirSync();
  try {
    const projectDir = join(tempDir, "my-project");
    Deno.mkdirSync(projectDir);
    const filePath = join(projectDir, "sess-001.jsonl");

    // Complete first line
    Deno.writeTextFileSync(
      filePath,
      userLine("hello", "u1", "2026-01-01T00:00:00Z") + "\n"
    );

    const db = new VaultDB(":memory:");
    try {
      importSingleSessionIncremental(db, filePath);
      assertEquals(db.sessionExists("sess-001"), 1);
      const sizeAfterFirst = db.getSessionImportedBytes("sess-001");

      // Append an *incomplete* second line (no trailing newline)
      const halfLine = assistantLine("hi", "a1", "2026-01-01T00:00:01Z").slice(
        0,
        20
      );
      Deno.writeTextFileSync(filePath, halfLine, { append: true });

      const result = importSingleSessionIncremental(db, filePath);
      assertEquals(result!.status, "unchanged");
      assertEquals(result!.addedMessages, 0);
      // Offset must not advance past the last complete newline
      assertEquals(db.getSessionImportedBytes("sess-001"), sizeAfterFirst);

      // Now complete the line — next call should pick it up
      Deno.writeTextFileSync(filePath, "\n", { append: true });
      // Wait: the "half line" was truncated JSON. Replace with a real line.
      Deno.writeTextFileSync(
        filePath,
        userLine("hello", "u1", "2026-01-01T00:00:00Z") +
          "\n" +
          assistantLine("hi", "a1", "2026-01-01T00:00:01Z") +
          "\n"
      );

      const complete = importSingleSessionIncremental(db, filePath);
      assertEquals(complete!.status, "updated");
      assertEquals(complete!.addedMessages, 1);
      assertEquals(db.sessionExists("sess-001"), 2);
    } finally {
      db.close();
    }
  } finally {
    Deno.removeSync(tempDir, { recursive: true });
  }
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
      userLine("test", "u1", "2026-01-01T00:00:00Z", "abc-123-def") + "\n"
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
      "/nonexistent/path/fake.jsonl"
    );
    assertEquals(result, null);
  } finally {
    db.close();
  }
});
