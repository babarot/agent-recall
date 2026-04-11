import { useCallback, useEffect, useRef } from "preact/hooks";
import type { RefObject } from "preact";

/** Consider the viewport "at the bottom" if it's within this many pixels. */
const TAIL_FOLLOW_THRESHOLD_PX = 100;

/** How long after a data change we keep watching for layout shifts (e.g. images). */
const POST_RENDER_OBSERVE_MS = 500;

/**
 * Hook that makes a scroll container stick to the bottom through live
 * data updates, with protection against the three usual bugs:
 *
 *   1. Fetch race — concurrent fetches resolving out of order can stomp
 *      newer data with older data. Consumers call `markIfAtBottom()` just
 *      before firing a fetch and wrap the `setData` call with
 *      `if (isCurrentSeq(seq))`. A monotonic counter ensures only the
 *      latest in-flight fetch's result is applied.
 *
 *   2. Async layout shifts — when new content renders with images that
 *      haven't downloaded yet, a naive scrollTo happens *before* the
 *      images expand. A ResizeObserver (attached briefly after each data
 *      change) catches the subsequent size changes and re-scrolls.
 *
 *   3. User intent — if the user scrolls up between the event and the
 *      refetch completing, we flip the "follow" flag off so our scroll
 *      doesn't override them.
 *
 * The API is intentionally small: pass the scroll container ref and the
 * reactive data value. Call `markIfAtBottom()` to capture intent and a
 * sequence number; call `isCurrentSeq(seq)` before committing stale data.
 */
export function useTailFollow<T>(
  scrollRef: RefObject<HTMLElement>,
  data: T
): {
  markIfAtBottom: () => number;
  isCurrentSeq: (seq: number) => boolean;
} {
  const followRef = useRef(false);
  const seqRef = useRef(0);

  const isAtBottom = useCallback((): boolean => {
    const el = scrollRef.current;
    if (!el) return false;
    return el.scrollHeight - el.scrollTop - el.clientHeight < TAIL_FOLLOW_THRESHOLD_PX;
  }, [scrollRef]);

  /**
   * Called by consumers right before firing a refetch. Records whether the
   * user is currently at (or near) the bottom, and bumps the sequence
   * counter. Returns the new sequence number so the consumer can later
   * verify the response is still the latest in flight.
   */
  const markIfAtBottom = useCallback((): number => {
    followRef.current = isAtBottom();
    seqRef.current += 1;
    return seqRef.current;
  }, [isAtBottom]);

  const isCurrentSeq = useCallback(
    (seq: number): boolean => seq === seqRef.current,
    []
  );

  // User scroll cancels tail-follow intent. If the user drags the scrollbar
  // up between markIfAtBottom() and the post-render scroll, we should not
  // yank them back down.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const onScroll = () => {
      if (!followRef.current) return;
      if (!isAtBottom()) followRef.current = false;
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, [scrollRef, isAtBottom]);

  // After a data change, re-scroll to the bottom if the "was at bottom"
  // flag is set. The ResizeObserver keeps watching for a short window so
  // late-arriving layout (image loads, syntax highlighting, etc.) doesn't
  // leave the viewport stranded above the new tail.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (!followRef.current) return;

    const scrollToBottom = () => {
      if (!followRef.current) return;
      el.scrollTo({ top: el.scrollHeight });
    };

    // Immediate scroll based on the content that has rendered so far.
    scrollToBottom();

    // Then keep watching for a brief period in case images or other async
    // content change the layout after paint.
    const ro = new ResizeObserver(scrollToBottom);
    ro.observe(el);
    // Also observe the immediate child that wraps the message list, since
    // ResizeObserver on overflow containers can miss some growth cases.
    if (el.firstElementChild) ro.observe(el.firstElementChild);

    const timer = window.setTimeout(() => {
      ro.disconnect();
      followRef.current = false;
    }, POST_RENDER_OBSERVE_MS);

    return () => {
      window.clearTimeout(timer);
      ro.disconnect();
    };
  }, [data, scrollRef]);

  return { markIfAtBottom, isCurrentSeq };
}
