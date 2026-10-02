# ADR-004: Port to Go and rename to claude-recall

## Status

Accepted

## Context

agent-recall was written in Deno and TypeScript because, when it started, the alternatives on the table were a shell script and Rust, and Deno offered JSONL parsing, a built-in SQLite and a single `deno compile` binary with little effort. A terminal UI was not a requirement then.

The need that came next was a TUI: a session list to find a closed Claude Code session again (when, which folder and branch, how long, its ID) and resume it or hand its ID to another agent. Bubble Tea in Go is the natural tool for that, and keeping the rest in TypeScript would have meant shipping two languages.

Two facts about the archive shaped the port:

- `vault.db` is no longer a rebuildable cache. It holds sessions whose JSONL Claude Code has deleted, so the schema can only grow by additive migrations, and the port had to read and write the existing file in place.
- Every Claude Code session starts its own `agent-recall mcp`, and each runs a watcher that imports into the same database. The TypeScript importer committed a session's delete before re-inserting its rows, so concurrent imports left duplicate image rows (854 in the author's archive).

## Decision

Rewrite everything (import, CLI, MCP server, watcher, web server) in Go as one binary, `recall`, and rename the project to claude-recall. The web UI stays as it is and is embedded into the binary.

- Compatibility first. The importer stores the same rows the TypeScript one did (JavaScript trim, UTF-16 lengths, `JSON.stringify` key order and number format, reproduced in `internal/jscompat`), and the CLI, MCP and HTTP output keeps its shape. Before the switch, scripts compared both implementations on a snapshot of a real archive (1040 sessions, about 240000 messages): every row, every CLI output, every MCP tool and HTTP endpoint matched byte for byte.
- One transaction per session import, so concurrent importers cannot leave a session half written or duplicated.
- The MCP server uses the official Go SDK instead of a hand-written JSON-RPC loop pinned to one protocol version.
- The watcher polls file sizes and modification times every 250 ms instead of using fsnotify. On macOS fsnotify's kqueue backend needs an open file descriptor for every transcript, and it does not watch directories recursively. ADR-002's decision (watch the filesystem, no hooks) stands; only the mechanism changed.
- Session titles (`/rename` and Claude Code's generated titles) are stored in a new `title` column, added by the first migration. NULL marks a row imported before titles existed, so the next import fills it in.
- The web server listens on 127.0.0.1 only, and `/api/file` serves image files only. Before, it listened on every interface and returned any file by absolute path.

## Consequences

### Benefits

- One language and one static binary, with the TUI built in.
- Imports are about twice as fast (31 s against 67 s for a full import of the archive above).
- Concurrent MCP servers no longer duplicate or tear session rows.
- Agents and the TUI can show real session titles.

### Drawbacks

- The rename changes the binary (`recall`), the plugin (`claude-recall`) and the MCP tool prefix (`mcp__plugin_claude-recall_claude-recall__`). Prompts or skills that name the old tools need updating.
- `internal/jscompat` carries JavaScript semantics forever, so rows written by either implementation stay identical. Dropping it would rewrite stored text on the next import of each session.
- Live updates arrive up to about half a second after a write instead of about 300 ms.

### Switching over

The TypeScript and Go importers must not write the archive at the same time. Install the Go release first, then end every process of the old one (restart, or `pkill -f 'agent-recall mcp'` and `agent-recall ui stop`), so no old MCP server keeps importing.
