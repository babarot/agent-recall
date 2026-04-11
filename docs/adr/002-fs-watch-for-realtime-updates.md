# ADR-002: FS watch instead of Claude Code hooks for real-time UI updates

## Status

Accepted

## Context

The web UI (`agent-recall ui`) initially reflected new sessions only after a manual reload, and even then only if the `SessionEnd` hook had already run `agent-recall import`. This created two user-visible problems:

1. **No real-time updates** while a Claude Code session was still in progress — the user had to end the session, wait for the hook to finish, and reload the browser.
2. **Lost sessions on Ctrl-C** — when Claude Code was killed by SIGINT before `SessionEnd` ran (or before the hook subprocess finished writing to SQLite), new sessions never made it into the DB at all.

The goal was to make the UI update live while Claude Code is running, and to eliminate the Ctrl-C data loss path.

A reference implementation exists: [claude-mem](https://github.com/thedotmack/claude-mem) achieves real-time updates by registering a `PostToolUse` hook that POSTs each tool invocation to a long-running worker service, which then broadcasts to the web viewer over SSE. This led to an obvious first instinct — mirror claude-mem's approach and register a `PostToolUse` hook in agent-recall too.

On closer inspection, the two projects have a fundamentally different relationship with their data source, and this changes which notification mechanism is appropriate.

### Data source comparison

| | agent-recall | claude-mem |
|---|---|---|
| Source of truth | `~/.claude/projects/**/*.jsonl` (raw transcript files) | Hook payloads (structured `tool_input` / `tool_response` JSON) |
| What gets stored | Raw conversation text, deduplicated by UUID | LLM-generated semantic *observations* |
| Role of the hook | Just a notification — "something changed, go read the file" | The canonical data pipeline — hook payload IS the data |

For claude-mem, the hook is load-bearing: if the hook doesn't fire, the data simply does not exist anywhere. They *must* use hooks.

For agent-recall, the hook is only a notification trigger. The actual source of truth is the JSONL files on disk, as established in [ADR-001](001-recall-not-memory-extension.md). Regardless of how we get notified, the actual data ingestion is always "read the JSONL, parse it, insert into SQLite". This means we have a free choice between two notification mechanisms:

1. **Hook-based notification**: `PostToolUse` hook POSTs `session_id` to the UI server, which then reads the JSONL file
2. **Filesystem notification**: `Deno.watchFs` on `~/.claude/projects` sees write events directly and reads the JSONL file

Both paths converge on the same work (parse JSONL, upsert into SQLite, broadcast SSE). They differ only in *who* tells the UI server that there is work to do.

### Tradeoffs considered

| Axis | Hook-based | FS watch |
|------|------------|----------|
| Installation friction | Requires editing `~/.claude/settings.json`, documenting a new hook, updating `install.sh` | Zero configuration — starts automatically with `agent-recall ui` |
| Notification precision | Fires exactly once per tool call with `session_id` in the payload | Fires per file write with the filepath, from which session_id is derivable |
| Timing vs JSONL flush | Fires after the tool call, by which time the JSONL is written | Fires on the actual write event |
| Robustness to Ctrl-C | The hook subprocess can be killed together with its parent before it finishes POSTing | The watcher runs in a fully independent process (the UI server) and is immune to Claude Code's lifecycle |
| Coupling to Claude Code | Tied to Claude Code's specific hook names and payload format | Works for any agent CLI that writes JSONL under `~/.claude/projects` (future-proof for Cursor, Gemini CLI, etc.) |
| Works when UI server is off | No — the hook would POST to a dead socket and data would be lost until the next `SessionEnd` fallback | No — same limitation |

The "works when UI server is off" limitation is identical for both mechanisms, and in both cases the existing `SessionEnd` hook remains as a fallback for bulk import on session exit.

## Decision

**Use `Deno.watchFs` on `~/.claude/projects` from within the UI server process. Do not register a new Claude Code hook.**

### Rationale

1. **Hook and FS watch would converge on the same code path anyway.** Both trigger "read the JSONL, parse it, insert into SQLite, broadcast SSE". Since there is no meaningful difference in what happens downstream, the cheapest notification mechanism wins.

2. **Zero-config matches agent-recall's philosophy.** The project's README leads with *"Zero dependencies — single binary via `deno compile`; no external services"*. Forcing users to edit `settings.json` and adding `PostToolUse` to the install script would erode this. FS watch requires no user action beyond running `agent-recall ui`.

3. **FS watch decouples agent-recall from Claude Code's hook schema.** Today Claude Code is the only writer to `~/.claude/projects`, but other agent CLIs (Gemini CLI, Cursor, future tools) also write JSONL to similar locations. A filesystem-based watcher can be extended to cover them with a directory change. A hook-based approach would need a separate hook configuration per agent.

4. **FS watch is strictly more robust to Ctrl-C.** A hook subprocess spawned with `async: true` can still be killed when Claude Code exits, before it finishes its HTTP POST. The FS watcher runs in an entirely separate long-lived process (the UI server), and the kernel's write events for already-flushed JSONL bytes are delivered regardless of whether Claude Code is still alive.

5. **Lower blast radius on failure.** If the FS watcher crashes, the UI just stops updating live — everything else (manual `import`, `SessionEnd` hook fallback, MCP read path, CLI search) keeps working. If a `PostToolUse` hook is misconfigured, every tool call in Claude Code could hit a broken command, which has a larger potential impact on the user's main workflow.

### Architecture

```
~/.claude/projects/**/*.jsonl
        │  (append by Claude Code)
        ▼
  Deno.watchFs (recursive)
        │  debounce 300ms per session
        ▼
  importSingleSessionIncremental
  (tail-read from sessions.imported_bytes)
        │
        ▼
  SQLite (INSERT OR IGNORE, count via .changes)
        │
        ▼
  SSEBroadcaster.broadcast({ type: "session_updated", ... })
        │  text/event-stream
        ▼
  GET /api/stream  (EventSource in web UI)
        │
        ▼
  React state update (SessionList, ChatView)
```

### Why not both

Supporting hook-based notification *and* FS watch as a belt-and-suspenders design was considered and rejected. The marginal gain is near-zero (FS watch already catches the same events with sub-second latency), while the cost is a second notification path to document, test, and keep in sync. For a PoC, two independent paths to the same outcome is pure overhead.

## Consequences

### Benefits

- **Real-time updates** work without any user configuration beyond starting `agent-recall ui`
- **Ctrl-C data loss is eliminated** for the common case — whatever Claude Code wrote to JSONL before dying is picked up by the watcher as long as the UI server is running
- **MCP gets live data for free** — `recall_search` / `recall_list` now see sessions the moment the watcher writes them, since SQLite WAL makes committed writes visible to other readers immediately
- **CLI `import` becomes faster** — the new `imported_bytes`-based tail read replaces the "re-parse the entire JSONL" path used by the old incremental import, reducing cost from O(session length) to O(appended bytes) per invocation
- **Not tied to Claude Code specifics** — the watcher logic can be pointed at other agents' transcript directories without touching notification plumbing
- **The existing `SessionEnd` hook remains functional** as a fallback when the UI server is not running, with no changes

### Drawbacks

- **No real-time updates when the UI server is off.** This is identical to the hook-based approach and is mitigated by the existing `SessionEnd` import. Not a regression.
- **Watches the entire `~/.claude/projects` tree.** Unrelated file changes still generate events that must be filtered (`.jsonl` extension check). The volume is low enough to be ignored in practice.
- **Coupled to the current JSONL-on-disk transport.** If Claude Code ever stops writing JSONL to `~/.claude/projects` (e.g., switches to a direct SQLite sink or an IPC channel), the watcher breaks. In that scenario the hook-based approach would also need to change, since the hook's `transcript_path` payload assumes the same on-disk layout. Both approaches share this fate.
- **The UI server process must be kept alive** to get real-time behavior. `agent-recall ui` already supports background mode, so this is not new operational burden.

### Not decided here

- Whether to add a standalone `agent-recall watch` daemon (watcher without the HTTP server). Deferred — `agent-recall ui` running in background already covers this use case.
- Whether to add a `PostToolUse` hook that POSTs to the UI server as a complementary notification path. Deferred — revisit only if FS watch proves insufficient in practice.
- Whether MCP should gain a "subscribe"-style tool. Not planned — MCP over stdio doesn't have a natural push mechanism, and polling `recall_list` already reflects live data via the watcher.
