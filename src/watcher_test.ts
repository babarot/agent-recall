import {
  assertEquals,
  assertGreaterOrEqual,
} from "https://deno.land/std@0.224.0/assert/mod.ts";
import { join } from "@std/path";
import { VaultDB } from "./db.ts";
import { SSEBroadcaster, type SSEEvent } from "./sse.ts";
import { startProjectWatcher, type WatcherStatus } from "./watcher.ts";

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

      // Step 2: append a second line → expect a follow-up "resynced" broadcast.
      // The importer rebuilds the session every time, so existing sessions
      // that gained content come back as "resynced" (not "updated").
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
            .some((e) => e.sessionId === "sess-w2" && e.status === "resynced"),
        3000
      );
      assertEquals(gotUpdate, true, "expected 'resynced' broadcast after append");
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
    const status: WatcherStatus = {
      enabled: false,
      running: false,
      projectsDir: "",
      debounceMs: 0,
    };

    // No signal needed — the function should short-circuit and resolve.
    await startProjectWatcher(db, spy, "/nonexistent/path/to/projects", { status });

    assertEquals(spy.events.length, 0);
    assertEquals(status.running, false);
    assertEquals(typeof status.lastError, "string");
    db.close();
  },
});

Deno.test({
  name: "watcher broadcasts resynced when a tracked file is replaced with shorter content",
  async fn() {
    const tempDir = Deno.makeTempDirSync();
    const db = new VaultDB(":memory:");
    const spy = new SpyBroadcaster();
    const ac = new AbortController();

    const projectDir = join(tempDir, "proj-resync");
    Deno.mkdirSync(projectDir);

    const watcherPromise = startProjectWatcher(db, spy, tempDir, {
      debounceMs: 50,
      signal: ac.signal,
    });

    try {
      await new Promise((r) => setTimeout(r, 100));

      const filePath = join(projectDir, "sess-w3.jsonl");
      Deno.writeTextFileSync(
        filePath,
        [
          userLine("first", "u1", "2026-01-01T00:00:00Z", "sess-w3"),
          userLine("second", "u2", "2026-01-01T00:00:01Z", "sess-w3"),
        ].join("\n") + "\n"
      );

      const gotNew = await waitFor(
        () => spy.events.some((e) => e.sessionId === "sess-w3" && e.status === "new"),
        3000
      );
      assertEquals(gotNew, true);
      assertEquals(db.sessionExists("sess-w3"), 2);

      Deno.writeTextFileSync(
        filePath,
        userLine("replacement", "u9", "2026-01-01T00:00:05Z", "sess-w3") + "\n"
      );

      const gotResync = await waitFor(
        () => spy.events.some((e) => e.sessionId === "sess-w3" && e.status === "resynced"),
        3000
      );
      assertEquals(gotResync, true);
      assertEquals(db.sessionExists("sess-w3"), 1);
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
  name: "watcher debounces rapid writes into a single import",
  async fn() {
    const tempDir = Deno.makeTempDirSync();
    const db = new VaultDB(":memory:");
    const spy = new SpyBroadcaster();
    const ac = new AbortController();

    const projectDir = join(tempDir, "proj-debounce");
    Deno.mkdirSync(projectDir);

    // Use a relatively long debounce so the rapid writes all land inside
    // the same window even on a slow CI machine.
    const watcherPromise = startProjectWatcher(db, spy, tempDir, {
      debounceMs: 200,
      signal: ac.signal,
    });

    try {
      await new Promise((r) => setTimeout(r, 100));

      const filePath = join(projectDir, "sess-debounce.jsonl");

      // Fire several rapid writes. Each one touches the file from the
      // kernel's perspective, so a naive (non-debounced) watcher would
      // import five times.
      for (let i = 0; i < 5; i++) {
        Deno.writeTextFileSync(
          filePath,
          userLine(
            `hello ${i}`,
            `u${i}`,
            `2026-04-12T00:00:0${i}Z`,
            "sess-debounce"
          ) + "\n",
          i === 0 ? undefined : { append: true }
        );
      }

      // Wait for the debounce window plus a margin for the import to run.
      await new Promise((r) => setTimeout(r, 500));

      const updates = spy.events.filter(
        (e) => e.type === "session_updated" && e.sessionId === "sess-debounce"
      );
      // Should collapse to exactly 1 broadcast for the burst.
      assertEquals(updates.length, 1);
      // And it should have picked up all 5 messages in one go.
      assertEquals(db.sessionExists("sess-debounce"), 5);
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
