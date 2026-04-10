import {
  assertEquals,
  assertNotEquals,
} from "https://deno.land/std@0.224.0/assert/mod.ts";
import { VaultDB } from "./db.ts";

function withDB(fn: (db: VaultDB) => void): void {
  const db = new VaultDB(":memory:");
  try {
    fn(db);
  } finally {
    db.close();
  }
}

function seedSession(
  db: VaultDB,
  id: string,
  project = "test-project",
  branch = "main"
): void {
  db.insertSession({
    sessionId: id,
    project,
    projectPath: `/home/user/${project}`,
    gitBranch: branch,
    firstPrompt: `prompt for ${id}`,
    messageCount: 0,
    startedAt: "2026-01-01T00:00:00Z",
    endedAt: "2026-01-01T00:10:00Z",
    claudeVersion: "2.1.87",
  });
}

function seedMessage(
  db: VaultDB,
  sessionId: string,
  uuid: string,
  role: string,
  content: string,
  turnIndex: number
): void {
  db.insertMessage({
    sessionId,
    uuid,
    role,
    content,
    timestamp: "2026-01-01T00:00:00Z",
    turnIndex,
  });
}

// --- Schema / Migration ---

Deno.test("VaultDB creates tables on fresh database", () => {
  withDB((db) => {
    assertEquals(db.sessionExists("nonexistent"), null);
  });
});

// --- Session CRUD ---

Deno.test("insertSession and sessionExists", () => {
  withDB((db) => {
    assertEquals(db.sessionExists("s1"), null);
    seedSession(db, "s1");
    assertEquals(db.sessionExists("s1"), 0);
  });
});

Deno.test("updateSessionCounts updates message_count and ended_at", () => {
  withDB((db) => {
    seedSession(db, "s1");
    db.updateSessionCounts("s1", 5, "2026-01-01T01:00:00Z");
    assertEquals(db.sessionExists("s1"), 5);
  });
});

// --- Message dedup ---

Deno.test("insertMessage ignores duplicate UUIDs", () => {
  withDB((db) => {
    seedSession(db, "s1");
    seedMessage(db, "s1", "m1", "user", "hello", 0);
    seedMessage(db, "s1", "m1", "user", "hello again", 0); // duplicate uuid

    const { messages } = db.exportSession("s1");
    assertEquals(messages.length, 1);
    assertEquals(messages[0].content, "hello");
  });
});

Deno.test("insertMessage allows different UUIDs", () => {
  withDB((db) => {
    seedSession(db, "s1");
    seedMessage(db, "s1", "m1", "user", "hello", 0);
    seedMessage(db, "s1", "m2", "assistant", "hi", 1);

    const { messages } = db.exportSession("s1");
    assertEquals(messages.length, 2);
  });
});

// --- FTS5 Search ---

Deno.test("search finds matching messages", () => {
  withDB((db) => {
    seedSession(db, "s1");
    seedMessage(db, "s1", "m1", "user", "deploy terraform infrastructure", 0);
    seedMessage(db, "s1", "m2", "assistant", "deployment complete", 1);

    const results = db.search("terraform");
    assertEquals(results.length, 1);
    assertEquals(results[0].content, "deploy terraform infrastructure");
  });
});

Deno.test("search with Porter stemmer matches word forms", () => {
  withDB((db) => {
    seedSession(db, "s1");
    seedMessage(db, "s1", "m1", "user", "running the tests", 0);

    const results = db.search("run");
    assertEquals(results.length, 1);
  });
});

Deno.test("search filters by project", () => {
  withDB((db) => {
    seedSession(db, "s1", "project-alpha");
    seedSession(db, "s2", "project-beta");
    seedMessage(db, "s1", "m1", "user", "hello world", 0);
    seedMessage(db, "s2", "m2", "user", "hello world", 0);

    const results = db.search("hello", { project: "alpha" });
    assertEquals(results.length, 1);
    assertEquals(results[0].sessionId, "s1");
  });
});

Deno.test("search filters by date range", () => {
  withDB((db) => {
    seedSession(db, "s1");
    db.insertMessage({
      sessionId: "s1",
      uuid: "m1",
      role: "user",
      content: "early message",
      timestamp: "2026-01-01T00:00:00Z",
      turnIndex: 0,
    });
    db.insertMessage({
      sessionId: "s1",
      uuid: "m2",
      role: "user",
      content: "late message",
      timestamp: "2026-06-01T00:00:00Z",
      turnIndex: 1,
    });

    const results = db.search("message", { from: "2026-03-01" });
    assertEquals(results.length, 1);
    assertEquals(results[0].content, "late message");
  });
});

Deno.test("search respects limit", () => {
  withDB((db) => {
    seedSession(db, "s1");
    for (let i = 0; i < 10; i++) {
      seedMessage(db, "s1", `m${i}`, "user", `item number ${i}`, i);
    }

    const results = db.search("item", { limit: 3 });
    assertEquals(results.length, 3);
  });
});

Deno.test("search returns empty for no matches", () => {
  withDB((db) => {
    seedSession(db, "s1");
    seedMessage(db, "s1", "m1", "user", "hello world", 0);

    const results = db.search("nonexistent");
    assertEquals(results.length, 0);
  });
});

// --- List ---

Deno.test("listSessions returns sessions ordered by started_at desc", () => {
  withDB((db) => {
    db.insertSession({
      sessionId: "s1",
      project: "proj",
      projectPath: "/proj",
      gitBranch: "main",
      firstPrompt: "first",
      messageCount: 1,
      startedAt: "2026-01-01T00:00:00Z",
      endedAt: "2026-01-01T00:10:00Z",
      claudeVersion: "2.1.87",
    });
    db.insertSession({
      sessionId: "s2",
      project: "proj",
      projectPath: "/proj",
      gitBranch: "dev",
      firstPrompt: "second",
      messageCount: 2,
      startedAt: "2026-02-01T00:00:00Z",
      endedAt: "2026-02-01T00:10:00Z",
      claudeVersion: "2.1.87",
    });

    const sessions = db.listSessions();
    assertEquals(sessions.length, 2);
    assertEquals(sessions[0].sessionId, "s2"); // newer first
    assertEquals(sessions[1].sessionId, "s1");
  });
});

Deno.test("listSessions filters by project", () => {
  withDB((db) => {
    seedSession(db, "s1", "alpha-project");
    seedSession(db, "s2", "beta-project");

    const sessions = db.listSessions({ project: "alpha" });
    assertEquals(sessions.length, 1);
    assertEquals(sessions[0].sessionId, "s1");
  });
});

Deno.test("listSessions respects limit", () => {
  withDB((db) => {
    for (let i = 0; i < 5; i++) {
      seedSession(db, `s${i}`, `project-${i}`);
    }

    const sessions = db.listSessions({ limit: 2 });
    assertEquals(sessions.length, 2);
  });
});

// --- Export ---

Deno.test("exportSession returns session and messages", () => {
  withDB((db) => {
    seedSession(db, "s1");
    seedMessage(db, "s1", "m1", "user", "hello", 0);
    seedMessage(db, "s1", "m2", "assistant", "hi", 1);

    const { session, messages } = db.exportSession("s1");
    assertNotEquals(session, null);
    assertEquals(session!.sessionId, "s1");
    assertEquals(messages.length, 2);
    assertEquals(messages[0].role, "user");
    assertEquals(messages[1].role, "assistant");
  });
});

Deno.test("exportSession supports prefix matching", () => {
  withDB((db) => {
    seedSession(db, "abc12345-full-uuid");
    seedMessage(db, "abc12345-full-uuid", "m1", "user", "test", 0);

    const { session } = db.exportSession("abc12345");
    assertNotEquals(session, null);
    assertEquals(session!.sessionId, "abc12345-full-uuid");
  });
});

Deno.test("exportSession returns null for nonexistent session", () => {
  withDB((db) => {
    const { session, messages } = db.exportSession("nonexistent");
    assertEquals(session, null);
    assertEquals(messages.length, 0);
  });
});

Deno.test("exportSession returns messages ordered by turn_index", () => {
  withDB((db) => {
    seedSession(db, "s1");
    seedMessage(db, "s1", "m3", "user", "third", 2);
    seedMessage(db, "s1", "m1", "user", "first", 0);
    seedMessage(db, "s1", "m2", "assistant", "second", 1);

    const { messages } = db.exportSession("s1");
    assertEquals(messages[0].content, "first");
    assertEquals(messages[1].content, "second");
    assertEquals(messages[2].content, "third");
  });
});

// --- Stats ---

Deno.test("stats returns correct aggregates", () => {
  withDB((db) => {
    seedSession(db, "s1", "project-a");
    seedSession(db, "s2", "project-b");
    db.updateSessionCounts("s1", 10, "2026-01-01T01:00:00Z");
    db.updateSessionCounts("s2", 5, "2026-01-01T01:00:00Z");

    const s = db.stats();
    assertEquals(s.totalSessions, 2);
    assertEquals(s.totalMessages, 15);
    assertEquals(s.byProject.length, 2);
  });
});

Deno.test("stats filters by project", () => {
  withDB((db) => {
    seedSession(db, "s1", "project-a");
    seedSession(db, "s2", "project-b");
    db.updateSessionCounts("s1", 10, "2026-01-01T01:00:00Z");
    db.updateSessionCounts("s2", 5, "2026-01-01T01:00:00Z");

    const s = db.stats("project-a");
    assertEquals(s.totalSessions, 1);
    assertEquals(s.totalMessages, 10);
  });
});
