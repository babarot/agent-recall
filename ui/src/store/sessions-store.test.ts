import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  sessions,
  stats,
  query,
  committedQuery,
  project,
  hasMore,
  projects,
  init,
  fetchSessions,
  runSearch,
  setProject,
  setQuery,
  __resetStoreForTests,
} from "./sessions-store";
import { __resetSSEBusForTests } from "../lib/sse-bus";

/**
 * Mock EventSource so init()'s SSE subscription doesn't blow up on connect.
 * Tests that need to exercise SSE delivery grab the instance and call
 * .emit() directly.
 */
class MockEventSource {
  static instances: MockEventSource[] = [];
  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  closed = false;
  constructor(_url: string) {
    MockEventSource.instances.push(this);
  }
  close(): void {
    this.closed = true;
  }
  emit(data: unknown): void {
    this.onmessage?.(new MessageEvent("message", { data: JSON.stringify(data) }));
  }
}

function mockFetch(impl: (url: string) => Response | Promise<Response>): void {
  // @ts-expect-error — override global
  globalThis.fetch = vi.fn((input: RequestInfo) =>
    Promise.resolve(impl(String(input))),
  );
}

function jsonResponse(body: unknown, ok = true, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status: ok ? status : status === 200 ? 500 : status,
    headers: { "content-type": "application/json" },
  });
}

beforeEach(() => {
  MockEventSource.instances = [];
  // @ts-expect-error — override global
  globalThis.EventSource = MockEventSource;
  __resetStoreForTests();
  __resetSSEBusForTests();
});

afterEach(() => {
  __resetStoreForTests();
  __resetSSEBusForTests();
  vi.restoreAllMocks();
});

describe("init", () => {
  it("is idempotent: subscribing to SSE only once across calls", async () => {
    mockFetch((url) => {
      if (url.includes("/api/sessions")) return jsonResponse([]);
      if (url.includes("/api/stats")) return jsonResponse({ totalSessions: 0, totalMessages: 0 });
      return jsonResponse(null, false, 404);
    });
    init();
    init();
    init();
    expect(MockEventSource.instances.length).toBe(1);
  });

  it("retries fetch on next init() after a transient failure", async () => {
    let callCount = 0;
    mockFetch((url) => {
      if (url.includes("/api/sessions")) {
        callCount++;
        if (callCount === 1) return jsonResponse(null, false, 500);
        return jsonResponse([
          { sessionId: "s1", fullSessionId: "s1", project: "p", branch: "", firstPrompt: "hi", lastPrompt: "", messages: 1, date: "2026-01-01", activity: [] },
        ]);
      }
      if (url.includes("/api/stats")) return jsonResponse({ totalSessions: 0, totalMessages: 0 });
      return jsonResponse(null, false, 404);
    });

    init();
    await flush();
    expect(sessions.value.length).toBe(0);

    // Simulate user navigating back to list — init() runs again.
    init();
    await flush();
    expect(sessions.value.length).toBe(1);
  });
});

describe("fetchSessions", () => {
  it("appends on reset=false, replaces on reset=true", async () => {
    let call = 0;
    mockFetch(() => {
      call++;
      const data = call === 1
        ? [row("s1"), row("s2")]
        : [row("s3")];
      return jsonResponse(data);
    });
    await fetchSessions(true);
    expect(sessions.value.map((s) => s.sessionId)).toEqual(["s1", "s2"]);

    await fetchSessions(false);
    expect(sessions.value.map((s) => s.sessionId)).toEqual(["s1", "s2", "s3"]);

    await fetchSessions(true);
    // reset=true replaces with a fresh page
    expect(sessions.value.length).toBeGreaterThan(0);
  });

  it("deduplicates concurrent calls via loadingRef", async () => {
    let inFlight = 0;
    let maxInFlight = 0;
    mockFetch(() => {
      inFlight++;
      maxInFlight = Math.max(maxInFlight, inFlight);
      return new Promise<Response>((resolve) => {
        setTimeout(() => {
          inFlight--;
          resolve(jsonResponse([row("s1")]));
        }, 10);
      }) as unknown as Response;
    });

    const p1 = fetchSessions(true);
    const p2 = fetchSessions(true);
    await Promise.all([p1, p2]);
    expect(maxInFlight).toBe(1);
  });

  it("keeps the existing list intact on error", async () => {
    mockFetch(() => jsonResponse([row("s1")]));
    await fetchSessions(true);
    expect(sessions.value.length).toBe(1);

    mockFetch(() => jsonResponse(null, false, 500));
    await fetchSessions(true);
    // Previous data not wiped — user still sees something.
    expect(sessions.value.length).toBe(1);
    expect(hasMore.value).toBe(false);
  });
});

describe("runSearch", () => {
  it("empty query falls back to the live list", async () => {
    mockFetch(() => jsonResponse([row("s1")]));
    committedQuery.value = "old";
    await runSearch("   ");
    expect(committedQuery.value).toBe("");
    expect(sessions.value[0].sessionId).toBe("s1");
  });

  it("non-empty query hits /api/search and replaces sessions", async () => {
    mockFetch((url) => {
      expect(url).toContain("/api/search");
      expect(url).toContain("q=hello");
      return jsonResponse([
        { sessionId: "s1", project: "p", branch: "", content: "hello world", date: "2026-01-01" },
        { sessionId: "s1", project: "p", branch: "", content: "hello again", date: "2026-01-01" },
        { sessionId: "s2", project: "p", branch: "", content: "hello two", date: "2026-01-01" },
      ]);
    });
    await runSearch("hello");
    // Unique sessionIds only
    expect(sessions.value.length).toBe(2);
    expect(committedQuery.value).toBe("hello");
    expect(hasMore.value).toBe(false);
  });
});

describe("setProject / setQuery", () => {
  it("setProject refetches with the new project filter", async () => {
    let capturedUrl = "";
    mockFetch((url) => {
      capturedUrl = url;
      return jsonResponse([]);
    });
    setProject("my-proj");
    await flush();
    expect(project.value).toBe("my-proj");
    expect(capturedUrl).toContain("project=my-proj");
  });

  it("clearing the query while a committed search was active refetches the live list", async () => {
    let sessionsCalls = 0;
    mockFetch((url) => {
      if (url.includes("/api/sessions")) sessionsCalls++;
      return jsonResponse([]);
    });
    committedQuery.value = "something";
    setQuery("");
    await flush();
    expect(committedQuery.value).toBe("");
    expect(sessionsCalls).toBe(1);
  });
});

describe("SSE handler", () => {
  it("bumps a known session to the top with the refreshed row", async () => {
    sessions.value = [row("s1"), row("s2"), row("s3")];
    mockFetch(() => jsonResponse([{ ...row("s2"), firstPrompt: "updated" }]));

    // Install the subscriber and emit.
    init();
    await flush();
    const es = MockEventSource.instances[0];
    es.emit({ type: "session_updated", sessionId: "s2" });
    await flush();

    expect(sessions.value.map((s) => s.sessionId)).toEqual(["s2", "s1", "s3"]);
    expect(sessions.value[0].firstPrompt).toBe("updated");
  });

  it("ignores SSE events while a committed search is active", async () => {
    sessions.value = [row("s1")];
    committedQuery.value = "query";
    let called = false;
    mockFetch(() => {
      called = true;
      return jsonResponse([row("s2")]);
    });
    init();
    await flush();
    const es = MockEventSource.instances[0];
    es.emit({ type: "session_updated", sessionId: "s1" });
    await flush();
    // /api/sessions was called by init() itself but not by the SSE handler,
    // so we check the session list didn't change.
    expect(sessions.value.map((s) => s.sessionId)).toEqual(["s1"]);
    expect(called).toBe(true); // init() called it, but list is untouched
  });
});

describe("projects computed", () => {
  it("derives projects from stats.byProject", () => {
    stats.value = {
      totalSessions: 1,
      totalMessages: 1,
      byProject: [
        { project: "A", projectPath: "/a", sessions: 1, messages: 1 },
        { project: "B", projectPath: "/b", sessions: 1, messages: 1 },
      ],
    };
    expect(projects.value).toEqual([
      { display: "A", value: "/a" },
      { display: "B", value: "/b" },
    ]);
  });

  it("is an empty array when stats is null", () => {
    expect(projects.value).toEqual([]);
  });
});

// Helpers

function row(id: string): ReturnType<typeof baseRow> {
  return baseRow(id);
}

function baseRow(id: string) {
  return {
    sessionId: id,
    fullSessionId: id,
    project: "p",
    branch: "",
    firstPrompt: "hi",
    lastPrompt: "",
    messages: 1,
    date: "2026-01-01",
    activity: [],
  };
}

async function flush(): Promise<void> {
  // Let microtasks + any pending fetch promises resolve.
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 0));
}
// Unused import suppression
void query;
