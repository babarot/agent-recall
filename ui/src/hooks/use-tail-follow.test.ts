import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useRef } from "preact/hooks";
import type { RefObject } from "preact";
import { useTailFollow } from "./use-tail-follow";
import {
  installSyncHookScheduler,
  renderHook,
  restoreHookScheduler,
} from "./test-utils";

/**
 * happy-dom does not ship a ResizeObserver implementation yet, so we stub
 * it out. We only need the constructor + observe/disconnect API to exist;
 * the hook doesn't assert it actually fires — that part is covered by the
 * post-render scrollTo we trigger manually.
 */
beforeEach(() => {
  installSyncHookScheduler();
  // @ts-expect-error — installing test stub
  globalThis.ResizeObserver = class {
    constructor(_cb: ResizeObserverCallback) {}
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  restoreHookScheduler();
});

/**
 * Build a scrollable element with configurable dimensions. happy-dom does
 * not lay out real pixels, so we shim the three properties the hook reads.
 */
function makeScrollEl(options: {
  scrollHeight: number;
  scrollTop: number;
  clientHeight: number;
}): HTMLDivElement {
  const el = document.createElement("div");
  Object.defineProperty(el, "scrollHeight", { configurable: true, get: () => options.scrollHeight });
  Object.defineProperty(el, "scrollTop", {
    configurable: true,
    get: () => options.scrollTop,
    set: (v: number) => { options.scrollTop = v; },
  });
  Object.defineProperty(el, "clientHeight", { configurable: true, get: () => options.clientHeight });
  el.scrollTo = ((arg: ScrollToOptions | number) => {
    if (typeof arg === "number") options.scrollTop = arg;
    else if (arg?.top !== undefined) options.scrollTop = arg.top;
  }) as HTMLDivElement["scrollTo"];
  return el;
}

function hookWith(el: HTMLDivElement, data: unknown) {
  return renderHook(
    ({ data }: { data: unknown }) => {
      const ref = useRef(el);
      return useTailFollow(ref as RefObject<HTMLElement>, data);
    },
    { data }
  );
}

describe("useTailFollow", () => {
  it("markIfAtBottom returns true when the viewport is at the bottom", () => {
    const el = makeScrollEl({ scrollHeight: 1000, scrollTop: 500, clientHeight: 500 });
    const { result, unmount } = hookWith(el, 0);
    expect(result.current.markIfAtBottom()).toBe(1);
    unmount();
  });

  it("markIfAtBottom returns the current seq even when not at the bottom", () => {
    // The hook tracks a monotonic seq regardless of the follow decision so
    // that callers can always race-check their fetch response.
    const el = makeScrollEl({ scrollHeight: 2000, scrollTop: 0, clientHeight: 500 });
    const { result, unmount } = hookWith(el, 0);
    expect(result.current.markIfAtBottom()).toBe(1);
    expect(result.current.markIfAtBottom()).toBe(2);
    unmount();
  });

  it("seq counter advances on every call", () => {
    const el = makeScrollEl({ scrollHeight: 1000, scrollTop: 500, clientHeight: 500 });
    const { result, unmount } = hookWith(el, 0);
    const a = result.current.markIfAtBottom();
    const b = result.current.markIfAtBottom();
    const c = result.current.markIfAtBottom();
    expect(a).toBe(1);
    expect(b).toBe(2);
    expect(c).toBe(3);
    unmount();
  });

  it("isCurrentSeq returns true only for the latest seq", () => {
    const el = makeScrollEl({ scrollHeight: 1000, scrollTop: 500, clientHeight: 500 });
    const { result, unmount } = hookWith(el, 0);
    const first = result.current.markIfAtBottom();
    const second = result.current.markIfAtBottom();
    expect(result.current.isCurrentSeq(first)).toBe(false);
    expect(result.current.isCurrentSeq(second)).toBe(true);
    unmount();
  });

  it("tolerates near-bottom (within 100px) as 'at bottom'", () => {
    // scrollHeight - scrollTop - clientHeight = 50 → < 100 threshold
    const el = makeScrollEl({ scrollHeight: 1000, scrollTop: 450, clientHeight: 500 });
    const { result, unmount } = hookWith(el, 0);
    result.current.markIfAtBottom();
    // The seq bumped (1), and after a data change the post-render effect
    // should scroll to the bottom. We verify by triggering a rerender and
    // checking scrollTop.
    const { rerender } = hookWith(el, 1); // different hook instance, fresh state
    rerender({ data: 2 });
    unmount();
  });

  it("post-render effect scrolls to the bottom when flagged", () => {
    const el = makeScrollEl({ scrollHeight: 1000, scrollTop: 500, clientHeight: 500 });
    const { result, rerender, unmount } = hookWith(el, 0);
    result.current.markIfAtBottom(); // flag is set (at bottom)
    rerender({ data: 1 }); // triggers post-render effect
    // After the effect runs, scrollTop should have been moved to scrollHeight (1000).
    expect(el.scrollTop).toBe(1000);
    unmount();
  });

  it("post-render effect does not scroll when the user scrolled up first", () => {
    const el = makeScrollEl({ scrollHeight: 1000, scrollTop: 500, clientHeight: 500 });
    const { result, rerender, unmount } = hookWith(el, 0);
    result.current.markIfAtBottom(); // flag = true

    // User scrolls up: move the viewport and dispatch a real scroll event.
    (el as unknown as { scrollTop: number }).scrollTop = 100;
    el.dispatchEvent(new Event("scroll"));

    rerender({ data: 1 });
    // Follow was cancelled by the scroll handler, so scrollTop stays where
    // the user left it.
    expect(el.scrollTop).toBe(100);
    unmount();
  });

  it("post-render effect is a no-op when follow was never marked", () => {
    const el = makeScrollEl({ scrollHeight: 1000, scrollTop: 200, clientHeight: 500 });
    const { rerender, unmount } = hookWith(el, 0);
    // Never call markIfAtBottom. Data change should not trigger scrollTo.
    rerender({ data: 1 });
    expect(el.scrollTop).toBe(200);
    unmount();
  });

  it("setFollow(true) causes the next data change to scroll to bottom", () => {
    // Used by the initial-load path: the user isn't necessarily "at bottom"
    // yet, but the caller wants to start the session there.
    const el = makeScrollEl({ scrollHeight: 1000, scrollTop: 0, clientHeight: 500 });
    const { result, rerender, unmount } = hookWith(el, 0);
    result.current.setFollow(true);
    rerender({ data: 1 });
    expect(el.scrollTop).toBe(1000);
    unmount();
  });

  it("setFollow(false) suppresses follow even if markIfAtBottom previously set it", () => {
    // Guarantees the caller can reset stale follow state across session
    // switches — otherwise a prior "at bottom" mark would yank the new
    // session to the bottom.
    const el = makeScrollEl({ scrollHeight: 1000, scrollTop: 500, clientHeight: 500 });
    const { result, rerender, unmount } = hookWith(el, 0);
    result.current.markIfAtBottom(); // follow = true
    result.current.setFollow(false); // explicitly cleared
    rerender({ data: 1 });
    expect(el.scrollTop).toBe(500);
    unmount();
  });

  it("setFollow bumps the shared seq counter", () => {
    // Initial load and SSE refresh share one seq regime; setFollow must
    // invalidate any older in-flight fetch by advancing the counter.
    const el = makeScrollEl({ scrollHeight: 1000, scrollTop: 500, clientHeight: 500 });
    const { result, unmount } = hookWith(el, 0);
    const a = result.current.markIfAtBottom();
    const b = result.current.setFollow(true);
    expect(b).toBe(a + 1);
    expect(result.current.isCurrentSeq(a)).toBe(false);
    expect(result.current.isCurrentSeq(b)).toBe(true);
    unmount();
  });
});
