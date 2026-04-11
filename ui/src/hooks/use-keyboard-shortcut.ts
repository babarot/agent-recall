import { useEffect } from "preact/hooks";

export function useKeyboardShortcut(
  key: string,
  handler: (e: KeyboardEvent) => void,
  deps: unknown[] = []
): void {
  useEffect(() => {
    const listener = (e: KeyboardEvent) => {
      if (e.key === key) handler(e);
    };
    window.addEventListener("keydown", listener);
    return () => window.removeEventListener("keydown", listener);
  }, [key, ...deps]);
}
