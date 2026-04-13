/**
 * Filesystem watcher that mirrors `~/.claude/projects/**\/*.jsonl` changes
 * into SQLite and pushes `session_updated` events through an
 * `SSEBroadcaster` so connected web clients update without a page reload.
 *
 * See `docs/adr/002-fs-watch-for-realtime-updates.md` for why this is
 * FS-based instead of hook-based.
 *
 * Design notes:
 *   - `Deno.watchFs` emits raw kernel events at high frequency during writes.
 *     We de-dup per filepath with a 300 ms debounce so we do one incremental
 *     import per burst, not one per kernel event.
 *   - The watcher is best-effort: if `projectsDir` doesn't exist or vanishes,
 *     it logs and exits quietly without crashing the UI server.
 *   - All import errors are caught and logged so a single bad file cannot
 *     take the watcher down.
 */

import type { VaultDB } from "./db.ts";
import type { SSEBroadcaster } from "./sse.ts";
import { importSingleSessionIncremental } from "./import.ts";

export interface WatcherOptions {
  /** Wait this long after the last event per file before importing (default: 300 ms). */
  debounceMs?: number;
  /** Abort signal for graceful shutdown. */
  signal?: AbortSignal;
  /** Optional mutable status object for observability. */
  status?: WatcherStatus;
}

const DEFAULT_DEBOUNCE_MS = 300;

export interface WatcherStatus {
  enabled: boolean;
  running: boolean;
  projectsDir: string;
  debounceMs: number;
  lastEventAt?: string;
  lastImportAt?: string;
  lastError?: string;
  lastErrorAt?: string;
}

export async function startProjectWatcher(
  db: VaultDB,
  broadcaster: SSEBroadcaster | null,
  projectsDir: string,
  opts: WatcherOptions = {}
): Promise<void> {
  const debounceMs = opts.debounceMs ?? DEFAULT_DEBOUNCE_MS;
  const status = opts.status;
  if (status) {
    status.enabled = true;
    status.running = false;
    status.projectsDir = projectsDir;
    status.debounceMs = debounceMs;
    delete status.lastError;
    delete status.lastErrorAt;
  }

  // Bail out quietly if the projects dir isn't there yet.
  try {
    const stat = Deno.statSync(projectsDir);
    if (!stat.isDirectory) {
      if (status) {
        status.lastError = `${projectsDir} is not a directory`;
        status.lastErrorAt = new Date().toISOString();
      }
      console.error(`[watcher] ${projectsDir} is not a directory; watcher disabled.`);
      return;
    }
  } catch {
    if (status) {
      status.lastError = `${projectsDir} not found`;
      status.lastErrorAt = new Date().toISOString();
    }
    console.error(`[watcher] ${projectsDir} not found; watcher disabled.`);
    return;
  }

  let watcher: Deno.FsWatcher;
  try {
    watcher = Deno.watchFs(projectsDir, { recursive: true });
    if (status) {
      status.running = true;
    }
  } catch (e) {
    if (status) {
      status.lastError = e instanceof Error ? e.message : String(e);
      status.lastErrorAt = new Date().toISOString();
    }
    console.error(`[watcher] failed to watch ${projectsDir}:`, e);
    return;
  }

  // Stop the watcher when the signal aborts. We intentionally race close()
  // against the for-await loop; the loop will see the close and terminate.
  const onAbort = () => {
    try {
      watcher.close();
    } catch {
      // already closed
    }
  };
  opts.signal?.addEventListener("abort", onAbort, { once: true });

  const pending = new Map<string, number>();

  const scheduleImport = (filePath: string): void => {
    const existing = pending.get(filePath);
    if (existing !== undefined) clearTimeout(existing);

    const id = setTimeout(() => {
      pending.delete(filePath);
      try {
        const result = importSingleSessionIncremental(db, filePath);
        if (!result) return;
        if (status) {
          status.lastImportAt = new Date().toISOString();
          delete status.lastError;
          delete status.lastErrorAt;
        }
        // Any status that represents an actual change to DB state should
        // reach subscribed clients. Only "unchanged" is suppressed.
        if (
          broadcaster &&
          (result.status === "new" ||
           result.status === "updated" ||
           result.status === "resynced")
        ) {
          broadcaster.broadcast({
            type: "session_updated",
            sessionId: result.sessionId,
            project: result.project,
            status: result.status,
            addedMessages: result.addedMessages,
            totalMessages: result.totalMessages,
          });
        }
      } catch (e) {
        if (status) {
          status.lastError = e instanceof Error ? e.message : String(e);
          status.lastErrorAt = new Date().toISOString();
        }
        console.error(`[watcher] import failed for ${filePath}:`, e);
      }
    }, debounceMs);

    pending.set(filePath, id);
  };

  try {
    for await (const event of watcher) {
      if (status) {
        status.lastEventAt = new Date().toISOString();
      }
      // Only react to events that could mean "new content to read".
      if (event.kind !== "modify" && event.kind !== "create") continue;

      for (const path of event.paths) {
        if (!path.endsWith(".jsonl")) continue;
        scheduleImport(path);
      }
    }
  } catch (e) {
    // If we were aborted, the close() call above is the reason — swallow.
    if (!opts.signal?.aborted) {
      if (status) {
        status.lastError = e instanceof Error ? e.message : String(e);
        status.lastErrorAt = new Date().toISOString();
      }
      console.error("[watcher] loop crashed:", e);
    }
  } finally {
    if (status) {
      status.running = false;
    }
    // Cancel any debounced imports that never fired.
    for (const id of pending.values()) clearTimeout(id);
    pending.clear();
    opts.signal?.removeEventListener("abort", onAbort);
  }
}
