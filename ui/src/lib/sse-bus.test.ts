import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { subscribeSSE, __resetSSEBusForTests } from "./sse-bus";

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
  MockEventSource.instances = [];
  // @ts-expect-error — overriding the global for tests
  globalThis.EventSource = MockEventSource;
  vi.useFakeTimers();
  __resetSSEBusForTests();
});

afterEach(() => {
  vi.useRealTimers();
  __resetSSEBusForTests();
});

describe("subscribeSSE", () => {
  it("opens a single EventSource on first subscribe", () => {
    const handler = vi.fn();
    subscribeSSE(handler);
    expect(MockEventSource.instances.length).toBe(1);
    expect(MockEventSource.instances[0].url).toBe("/api/stream");
  });

  it("shares one EventSource across multiple subscribers", () => {
    const a = vi.fn();
    const b = vi.fn();
    subscribeSSE(a);
    subscribeSSE(b);
    expect(MockEventSource.instances.length).toBe(1);

    MockEventSource.instances[0].emit({ type: "session_updated", sessionId: "s1" });
    expect(a).toHaveBeenCalledWith({ type: "session_updated", sessionId: "s1" });
    expect(b).toHaveBeenCalledWith({ type: "session_updated", sessionId: "s1" });
  });

  it("forwards parsed JSON frames to all subscribers", () => {
    const handler = vi.fn();
    subscribeSSE(handler);
    MockEventSource.instances[0].emit({ type: "session_updated", sessionId: "abc" });
    expect(handler).toHaveBeenCalledWith({
      type: "session_updated",
      sessionId: "abc",
    });
  });

  it("swallows malformed JSON frames without crashing other subscribers", () => {
    const handler = vi.fn();
    subscribeSSE(handler);
    MockEventSource.instances[0].onmessage?.(
      new MessageEvent("message", { data: "not-json-{{{" })
    );
    expect(handler).not.toHaveBeenCalled();
    // Subsequent valid frames still get delivered
    MockEventSource.instances[0].emit({ type: "ok" });
    expect(handler).toHaveBeenCalledWith({ type: "ok" });
  });

  it("reconnects with exponential backoff on error", () => {
    const handler = vi.fn();
    subscribeSSE(handler);
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
  });

  it("resets backoff on successful reopen", () => {
    const handler = vi.fn();
    subscribeSSE(handler);

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
  });

  it("unsubscribing stops delivery to that handler but keeps the connection", () => {
    const a = vi.fn();
    const b = vi.fn();
    const offA = subscribeSSE(a);
    subscribeSSE(b);

    offA();
    MockEventSource.instances[0].emit({ type: "ok" });
    expect(a).not.toHaveBeenCalled();
    expect(b).toHaveBeenCalled();
    // The EventSource stays open — the bus is process-wide.
    expect(MockEventSource.instances[0].closed).toBe(false);
  });
});
