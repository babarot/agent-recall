import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSSE } from "./use-sse";
import {
  installSyncHookScheduler,
  renderHook,
  restoreHookScheduler,
} from "./test-utils";

/**
 * Mock EventSource implementation. Tests trigger `onopen` / `onmessage` /
 * `onerror` manually and inspect the last created instance via
 * `MockEventSource.instances`.
 */
class MockEventSource {
  static instances: MockEventSource[] = [];
  url: string;
  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  closed = false;

  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }

  close(): void {
    this.closed = true;
  }

  /** Test helper: simulate a `data:` frame from the server. */
  emit(data: unknown): void {
    this.onmessage?.(new MessageEvent("message", { data: JSON.stringify(data) }));
  }

  /** Test helper: simulate an SSE connection failure. */
  fail(): void {
    this.onerror?.(new Event("error"));
  }
}

beforeEach(() => {
  installSyncHookScheduler();
  MockEventSource.instances = [];
  // @ts-expect-error — overriding the global for tests
  globalThis.EventSource = MockEventSource;
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  restoreHookScheduler();
});

describe("useSSE", () => {
  it("opens a single EventSource on mount", () => {
    const handler = vi.fn();
    const { unmount } = renderHook(() => useSSE(handler), undefined);
    expect(MockEventSource.instances.length).toBe(1);
    expect(MockEventSource.instances[0].url).toBe("/api/stream");
    unmount();
  });

  it("forwards parsed JSON frames to the handler", () => {
    const handler = vi.fn();
    const { unmount } = renderHook(() => useSSE(handler), undefined);
    const es = MockEventSource.instances[0];
    es.emit({ type: "session_updated", sessionId: "abc", status: "new" });
    expect(handler).toHaveBeenCalledWith({
      type: "session_updated",
      sessionId: "abc",
      status: "new",
    });
    unmount();
  });

  it("swallows malformed JSON frames without crashing", () => {
    const handler = vi.fn();
    const { unmount } = renderHook(() => useSSE(handler), undefined);
    const es = MockEventSource.instances[0];
    // Manually deliver a non-JSON frame.
    es.onmessage?.(
      new MessageEvent("message", { data: "not-json-{{{" }) as MessageEvent
    );
    expect(handler).not.toHaveBeenCalled();
    unmount();
  });

  it("reconnects with exponential backoff on error", async () => {
    const handler = vi.fn();
    const { unmount } = renderHook(() => useSSE(handler), undefined);
    expect(MockEventSource.instances.length).toBe(1);

    // First failure → wait 1s → reconnect
    MockEventSource.instances[0].fail();
    expect(MockEventSource.instances[0].closed).toBe(true);
    vi.advanceTimersByTime(1_000);
    expect(MockEventSource.instances.length).toBe(2);

    // Second failure → wait 2s → reconnect
    MockEventSource.instances[1].fail();
    vi.advanceTimersByTime(1_500);
    expect(MockEventSource.instances.length).toBe(2); // not yet
    vi.advanceTimersByTime(500);
    expect(MockEventSource.instances.length).toBe(3);

    unmount();
  });

  it("closes the EventSource and cancels reconnect on unmount", () => {
    const handler = vi.fn();
    const { unmount } = renderHook(() => useSSE(handler), undefined);
    const es = MockEventSource.instances[0];

    // Schedule a reconnect, then unmount before it fires.
    es.fail();
    expect(es.closed).toBe(true);

    unmount();
    vi.advanceTimersByTime(10_000);
    // No new EventSource was created after unmount.
    expect(MockEventSource.instances.length).toBe(1);
  });

  it("resets backoff on successful reopen", () => {
    const handler = vi.fn();
    const { unmount } = renderHook(() => useSSE(handler), undefined);

    // Fail twice to bump the backoff.
    MockEventSource.instances[0].fail();
    vi.advanceTimersByTime(1_000);
    MockEventSource.instances[1].fail();
    vi.advanceTimersByTime(2_000);
    expect(MockEventSource.instances.length).toBe(3);

    // Succeed: onopen fires, backoff should reset to 1s for the next failure.
    MockEventSource.instances[2].onopen?.(new Event("open"));
    MockEventSource.instances[2].fail();
    vi.advanceTimersByTime(1_000);
    expect(MockEventSource.instances.length).toBe(4);

    unmount();
  });
});
