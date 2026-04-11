import { h, options, render } from "preact";

/**
 * Force Preact's hook effects to run synchronously with render. Preact
 * normally schedules effects via rAF/setTimeout so tests have to await a
 * tick — that's incompatible with `vi.useFakeTimers()` and makes trivial
 * assertions racy. Overriding `options.requestAnimationFrame` is the
 * supported escape hatch: it controls the "after paint" scheduler
 * preact/hooks uses for effect flushes.
 *
 * Call this once per test file in a `beforeAll`/`beforeEach`. Paired with
 * `restoreHookScheduler()` to clean up between files.
 */
let originalRaf: unknown;
export function installSyncHookScheduler(): void {
  // deno-lint-ignore no-explicit-any
  const o = options as any;
  originalRaf = o.requestAnimationFrame;
  o.requestAnimationFrame = (cb: () => void) => cb();
}
export function restoreHookScheduler(): void {
  // deno-lint-ignore no-explicit-any
  (options as any).requestAnimationFrame = originalRaf;
}

/**
 * Tiny `renderHook` helper so we can drive a Preact hook from a vitest
 * test without pulling in @testing-library/preact. The hook runs inside a
 * throwaway functional component rendered into a detached div; its return
 * value is captured in `result.current` on every render.
 *
 * Covers our needs for `useSSE` and `useTailFollow`:
 *   - mount → run effect
 *   - read returned value
 *   - rerender with an updated argument
 *   - unmount → run cleanup
 *
 * Deliberately thin; if we start needing things like async flushing or
 * context providers, swap this out for @testing-library/preact.
 */
export function renderHook<TArgs, TResult>(
  useHook: (args: TArgs) => TResult,
  initialArgs: TArgs
): {
  result: { current: TResult };
  rerender: (next: TArgs) => void;
  unmount: () => void;
} {
  const container = document.createElement("div");
  const result = { current: undefined as unknown as TResult };
  let currentArgs = initialArgs;

  function TestComponent(props: { args: TArgs }) {
    result.current = useHook(props.args);
    return null;
  }

  render(h(TestComponent, { args: currentArgs }), container);

  return {
    result,
    rerender(next) {
      currentArgs = next;
      render(h(TestComponent, { args: currentArgs }), container);
    },
    unmount() {
      render(null, container);
    },
  };
}

/**
 * Wait for a predicate to become true, polling every `intervalMs`.
 * Resolves to `true` once satisfied, or `false` after `timeoutMs`.
 */
export async function waitFor(
  predicate: () => boolean,
  timeoutMs = 1000,
  intervalMs = 10
): Promise<boolean> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (predicate()) return true;
    await new Promise((r) => setTimeout(r, intervalMs));
  }
  return predicate();
}
