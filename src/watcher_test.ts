import {
  assertEquals,
  assertGreaterOrEqual,
} from "https://deno.land/std@0.224.0/assert/mod.ts";
import { join } from "@std/path";
import { VaultDB } from "./db.ts";
import { SSEBroadcaster, type SSEEvent } from "./sse.ts";
import { startProjectWatcher } from "./watcher.ts";

/**
 * SSEBroadcaster subclass that records every broadcast() call so tests can
 * assert on them without spinning up a real HTTP connection.
 */
class SpyBroadcaster extends SSEBroadcaster {
  events: SSEEvent[] = [];
  override broadcast(event: SSEEvent): void {
    this.events.push(event);
    super.broadcast(event);
  }
}

/** Build a user JSONL line */
function userLine(
  text: string,
  uuid: string,
  ts: string,
  sessionId: string
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

/** Poll until `predicate()` is true or `timeoutMs` elapses. */
async function waitFor(
  predicate: () => boolean,
  timeoutMs: number,
  intervalMs = 20
): Promise<boolean> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (predicate()) return true;
    await new Promise((r) => setTimeout(r, intervalMs));
  }
  return predicate();
}

Deno.test({
  name: "watcher picks up a newly created JSONL file and broadcasts",
  async fn() {
    const tempDir = Deno.makeTempDirSync();
    const db = new VaultDB(":memory:");
    const spy = new SpyBroadcaster();
    const ac = new AbortController();

    const projectDir = join(tempDir, "my-project");
    Deno.mkdirSync(projectDir);

    // Start the watcher in the background.
    const watcherPromise = startProjectWatcher(db, spy, tempDir, {
      debounceMs: 50,
      signal: ac.signal,
    });

    try {
      // Give the watcher a moment to attach before writing.
      await new Promise((r) => setTimeout(r, 100));

      const filePath = join(projectDir, "sess-w1.jsonl");
      Deno.writeTextFileSync(
        filePath,
        userLine("hello from watcher", "u1", "2026-01-01T00:00:00Z", "sess-w1") +
          "\n"
      );

      const ok = await waitFor(() => spy.events.length > 0, 3000);
      assertEquals(ok, true, "expected at least one broadcast");

      const ev = spy.events.find((e) => e.type === "session_updated");
      assertEquals(ev?.sessionId, "sess-w1");
      assertEquals(ev?.project, "my-project");
      assertEquals(ev?.status, "new");
      assertGreaterOrEqual(ev?.addedMessages as number, 1);

      // And the DB was actually written
      assertEquals(db.sessionExists("sess-w1"), 1);
    } finally {
      ac.abort();
      await watcherPromise;
      db.close();
      Deno.removeSync(tempDir, { recursive: true });
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "watcher fires an update broadcast when an already-imported session is appended",
  async fn() {
    const tempDir = Deno.makeTempDirSync();
    const db = new VaultDB(":memory:");
    const spy = new SpyBroadcaster();
    const ac = new AbortController();

    const projectDir = join(tempDir, "proj-append");
    Deno.mkdirSync(projectDir);

    const watcherPromise = startProjectWatcher(db, spy, tempDir, {
      debounceMs: 50,
      signal: ac.signal,
    });

    try {
      await new Promise((r) => setTimeout(r, 100));

      const filePath = join(projectDir, "sess-w2.jsonl");

      // Step 1: create the file → first broadcast with status "new".
      Deno.writeTextFileSync(
        filePath,
        userLine("initial", "u1", "2026-01-01T00:00:00Z", "sess-w2") + "\n"
      );

      const gotNew = await waitFor(
        () => spy.events.some((e) => e.sessionId === "sess-w2" && e.status === "new"),
        3000
      );
      assertEquals(gotNew, true, "expected 'new' broadcast for initial import");
      assertEquals(db.sessionExists("sess-w2"), 1);

      // Step 2: append a second line → expect a follow-up "updated" broadcast.
      const newCountBefore = spy.events.length;
      Deno.writeTextFileSync(
        filePath,
        userLine("appended", "u2", "2026-01-01T00:00:10Z", "sess-w2") + "\n",
        { append: true }
      );

      const gotUpdate = await waitFor(
        () =>
          spy.events
            .slice(newCountBefore)
            .some((e) => e.sessionId === "sess-w2" && e.status === "updated"),
        3000
      );
      assertEquals(gotUpdate, true, "expected 'updated' broadcast after append");
      assertGreaterOrEqual(db.sessionExists("sess-w2") ?? 0, 2);
    } finally {
      ac.abort();
      await watcherPromise;
      db.close();
      Deno.removeSync(tempDir, { recursive: true });
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "watcher ignores non-.jsonl files",
  async fn() {
    const tempDir = Deno.makeTempDirSync();
    const db = new VaultDB(":memory:");
    const spy = new SpyBroadcaster();
    const ac = new AbortController();

    const projectDir = join(tempDir, "proj-noise");
    Deno.mkdirSync(projectDir);

    const watcherPromise = startProjectWatcher(db, spy, tempDir, {
      debounceMs: 50,
      signal: ac.signal,
    });

    try {
      await new Promise((r) => setTimeout(r, 100));

      // Create some files the watcher should ignore.
      Deno.writeTextFileSync(join(projectDir, "README.md"), "noise");
      Deno.writeTextFileSync(join(projectDir, "sessions-index.json"), "{}");

      // Give the kernel enough time to deliver events and for the debounce
      // window to elapse.
      await new Promise((r) => setTimeout(r, 300));

      assertEquals(spy.events.length, 0);
    } finally {
      ac.abort();
      await watcherPromise;
      db.close();
      Deno.removeSync(tempDir, { recursive: true });
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});

Deno.test({
  name: "watcher exits quietly when projects dir does not exist",
  async fn() {
    const db = new VaultDB(":memory:");
    const spy = new SpyBroadcaster();

    // No signal needed — the function should short-circuit and resolve.
    await startProjectWatcher(db, spy, "/nonexistent/path/to/projects");

    assertEquals(spy.events.length, 0);
    db.close();
  },
});

Deno.test({
  name: "watcher stops when abort signal fires",
  async fn() {
    const tempDir = Deno.makeTempDirSync();
    const db = new VaultDB(":memory:");
    const spy = new SpyBroadcaster();
    const ac = new AbortController();

    const watcherPromise = startProjectWatcher(db, spy, tempDir, {
      debounceMs: 50,
      signal: ac.signal,
    });

    try {
      // Wait briefly to let the watcher attach.
      await new Promise((r) => setTimeout(r, 100));
      ac.abort();
      // The promise should resolve promptly.
      const raced = await Promise.race([
        watcherPromise.then(() => "resolved"),
        new Promise<string>((r) => setTimeout(() => r("timeout"), 1000)),
      ]);
      assertEquals(raced, "resolved");
    } finally {
      db.close();
      Deno.removeSync(tempDir, { recursive: true });
    }
  },
  sanitizeOps: false,
  sanitizeResources: false,
});
