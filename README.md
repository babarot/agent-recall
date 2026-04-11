# agent-recall

[![Test](https://github.com/babarot/agent-recall/actions/workflows/test.yaml/badge.svg)](https://github.com/babarot/agent-recall/actions/workflows/test.yaml)

A CLI + MCP server that archives coding agent session history into SQLite for full-text search.

## Why

Claude Code stores conversations as JSONL files under `~/.claude/projects/`, but old sessions are automatically deleted and `/compact` loses detail. agent-recall automatically archives sessions on exit so you can search and reference past conversations anytime.

Put simply, agent-recall is a **recall tool**, not a memory system. The goal is to make `grep ~/.claude/projects/**/*.jsonl` a better experience. Nothing more, nothing less. When the agent realizes it doesn't know something, it looks it up. That's it.

## Why not just [claude-mem](https://github.com/thedotmack/claude-mem)?

Fair question. [claude-mem](https://github.com/thedotmack/claude-mem) is an excellent project solving a related problem, and if it fits your workflow, you should use it. But agent-recall is **not** trying to be claude-mem. It's deliberately solving a different problem.

**claude-mem extends the agent's memory.** It captures observations in real time on every tool use, summarizes them with an LLM into structured facts, stores them in a vector DB, and automatically injects the result into the next session's prompt. The agent picks up "where it left off" without being asked. It runs a resident worker, depends on Bun / Python / Chroma, and calls external LLM APIs during indexing. It intervenes at the memory layer.

**agent-recall is a recall tool.** It doesn't touch the memory layer at all. It just takes the JSONL files Claude Code already writes, dedupes them into SQLite, and exposes FTS5 full-text search through an MCP server. When the agent needs to know what happened last time, it calls `recall_search` the same way it'd call any other tool: a normal, explicit lookup. Nothing runs in the background. Nothing is auto-injected. Nothing is summarized by an LLM.

|  | claude-mem | agent-recall |
|---|---|---|
| **Goal** | Extend the agent's memory | Help the agent recall |
| **Layer** | Memory layer (intervenes) | Filesystem layer (doesn't touch memory) |
| **Metaphor** | RAG + auto-memory | A better `grep` for JSONLs |
| **Injection** | Push (auto-injected at `SessionStart`) | Pull (agent looks it up when needed) |
| **When it writes** | Every `PostToolUse` (real-time) | Once at `SessionEnd` |
| **What's stored** | LLM-summarized observations (title / facts / concepts) | Raw user/assistant text, noise-stripped |
| **Search** | FTS5 + Chroma vector hybrid | FTS5 only (deterministic) |
| **LLM calls during indexing** | Yes (Anthropic / Gemini / OpenRouter) | **Zero** |
| **Runtime** | Node + Bun + Python (uv) + Chroma, resident worker on `:37777` | Single `deno compile` binary, no daemon |
| **License** | AGPL-3.0 | MIT |

### Why I built agent-recall anyway

Extending the agent's memory has real costs:

- **LLM summaries drift.** By the time you want the exact wording of a past conversation, all that's left is a summary.
- **Non-determinism.** What gets stored depends on how the LLM felt that day. You can't `git log` that.
- **API cost and dependencies.** Per-tool-use LLM calls aren't free, and the runtime keeps growing.
- **Phantom memory.** Auto-injection is nice until the agent confidently references a hallucinated summary of something that never happened the way it "remembers."

agent-recall picks the opposite tradeoff:

- **Raw logs don't lie.** What's stored is literally what happened. No interpretation.
- **The agent admits when it doesn't know.** If it needs past context, it runs `recall_search`. That's a tool call, not a memory.
- **Idle means idle.** No worker, no background LLM calls, no auto-injection. The tool does nothing until you or the agent asks.
- **Zero deps.** One binary. MIT licensed. Works offline.

Different problem, different tradeoffs. If you want the agent to silently pick up where it left off, use claude-mem. If you want a deterministic, searchable archive of what actually happened (something the agent can reference like any other file system), use agent-recall.

See [`docs/comparison-claude-mem.md`](docs/comparison-claude-mem.md) for a deeper breakdown.

## Features

- **Auto-archive** -- SessionEnd hook saves sessions automatically on exit
- **Real-time Web UI** -- Sessions appear and update live as Claude Code writes to disk, without reload
- **Full-text search** -- Fast search powered by SQLite FTS5 with Porter stemmer
- **Noise filtering** -- Strips tool_use / tool_result / thinking, keeping only conversation text (~7% of raw data)
- **Incremental imports** -- Tail-read by byte offset; re-importing already-archived sessions is effectively free
- **Idempotent** -- UUID-based deduplication; safe to import repeatedly
- **MCP server** -- Agents can autonomously search past sessions via `recall_search`, `recall_list`, `recall_export`, `recall_stats` tools
- **Zero dependencies** -- Single binary via `deno compile`; no external services

## Install

Downloads precompiled binaries from GitHub Releases. No runtime dependencies needed.

```bash
# curl
curl -fsSL https://raw.githubusercontent.com/babarot/agent-recall/main/bin/install.sh | bash

# deno
deno run -A https://raw.githubusercontent.com/babarot/agent-recall/main/bin/install.ts
```

### Build from source

Requires [Deno](https://deno.com/) 2.x.

```bash
git clone https://github.com/babarot/agent-recall.git
cd agent-recall
deno task compile && cp agent-recall ~/.claude/agent-recall
```

### Hook Setup (auto-archive on session exit)

Add to `~/.claude/settings.json`:

```json
{
  "hooks": {
    "SessionEnd": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "$HOME/.claude/agent-recall import 2>/dev/null",
            "async": true
          }
        ]
      }
    ]
  }
}
```

### MCP Server Setup (agent-autonomous search)

Register the MCP server so agents can search past sessions autonomously:

```bash
claude mcp add agent-recall -s user -- ~/.claude/agent-recall mcp
```

This exposes 4 tools to the agent:

| Tool | Description |
|------|-------------|
| `recall_search` | Full-text search across past sessions |
| `recall_list` | List archived sessions |
| `recall_export` | Export a specific session's full conversation |
| `recall_stats` | Show archive statistics |

Agents will call these tools on their own when they need context from past conversations.

## Usage

```bash
# Import all sessions
agent-recall import

# Full-text search
agent-recall search "terraform module"

# Filter by project and date
agent-recall search "deploy" --project oksskolten --from 2026-03-01

# List sessions
agent-recall list
agent-recall list --project gh-infra --format json

# Export a session as Markdown
agent-recall export <session-id>
agent-recall export <session-id> --format json --output session.json

# Show statistics
agent-recall stats

# Web UI
agent-recall ui                    # Start in background (default port: 6276)
agent-recall ui --foreground       # Start in foreground
agent-recall ui --port 8080        # Custom port
agent-recall ui status             # Show server status
agent-recall ui stop               # Stop the server
```

### Import

```
agent-recall import [options]

Options:
  --session <uuid>    Import a specific session
  --project <name>    Import sessions for a specific project
  --dry-run           Show what would be imported without writing
```

### Search

```
agent-recall search <query> [options]

Options:
  --project <name>    Filter by project
  --limit <n>         Max results (default: 20)
  --from <date>       Start date (YYYY-MM-DD)
  --to <date>         End date (YYYY-MM-DD)
  --format text|json  Output format (default: text)
```

Supports FTS5 query syntax: `"exact phrase"`, `term1 AND term2`, `term1 OR term2`, `term1 NOT term2`

### List

```
agent-recall list [options]

Options:
  --project <name>    Filter by project
  --limit <n>         Max sessions (default: 50)
  --format text|json  Output format (default: text)
```

### Export

```
agent-recall export <session-id> [options]

Options:
  --format markdown|json|text  Output format (default: markdown)
  --output <file>              Write to file instead of stdout
```

Session ID supports prefix matching -- `export a1b2` works.

### Stats

```
agent-recall stats [options]

Options:
  --project <name>    Filter by project
```

### UI

```
agent-recall ui [options]

Options:
  --port <n>          Port number (default: 6276)
  --foreground        Run in foreground instead of background

Subcommands:
  agent-recall ui stop     Stop the running server
  agent-recall ui status   Show server status
```

Opens `http://localhost:6276` with session browser, chat viewer, and search.

**Live updates**: while the UI is running, a filesystem watcher observes `~/.claude/projects` and pushes new/changed sessions to the browser over Server-Sent Events. The session list reflects new activity at the top without reload, and the chat view auto-refreshes (and follows the tail if you were already scrolled to the bottom) while a session is still running in Claude Code. No Claude Code hook configuration is required; the watcher runs inside the UI process itself. See [docs/adr/001-fs-watch-for-realtime-updates.md](docs/adr/001-fs-watch-for-realtime-updates.md) for the rationale.

## Architecture

agent-recall is a single binary (`~/.claude/agent-recall`) with three interfaces:

| Interface | How it starts | Purpose |
|-----------|--------------|---------|
| **Hook** | Automatically on every Claude Code session exit (`SessionEnd` hook) | Bulk archive on exit (fallback when the UI server isn't running) |
| **MCP** | Automatically when Claude Code starts (registered via `claude mcp add`) | Lets agents search past sessions autonomously |
| **CLI** | Manually by the user (`agent-recall search ...`) | Search, list, export, stats from the terminal |
| **Web UI** | Manually by the user (`agent-recall ui`) | Browse sessions and chat history in the browser, with live updates |

When the Web UI is running, an in-process FS watcher tails `~/.claude/projects` for JSONL writes, runs an incremental byte-offset import into SQLite, and pushes `session_updated` events to browsers over Server-Sent Events. The `SessionEnd` hook still works as a fallback for when the UI isn't up.

```mermaid
flowchart TD
    JSONL["~/.claude/projects/*/*.jsonl"]
    JSONL -->|SessionEnd hook<br/>(bulk, on exit)| Import["import.ts<br/>tail-read by imported_bytes"]
    JSONL -->|Deno.watchFs<br/>(live, while UI runs)| Watcher["watcher.ts<br/>debounced per file"]
    Watcher --> Import
    Import --> DB["SQLite + FTS5<br/>~/.claude/vault.db"]
    Import --> SSE["SSEBroadcaster"]
    SSE -->|/api/stream| UI["Web UI<br/>http://localhost:6276<br/><i>live</i>"]
    DB --> CLI["CLI<br/>agent-recall search/list/export/stats"]
    DB --> MCP["MCP Server<br/>agent-recall mcp<br/><i>auto-started by Claude Code</i>"]
    DB --> UI

    style JSONL fill:#1c2128,stroke:#30363d,color:#e6edf3
    style Import fill:#1c2128,stroke:#30363d,color:#e6edf3
    style Watcher fill:#1c2128,stroke:#30363d,color:#e6edf3
    style DB fill:#1c2f50,stroke:#58a6ff,color:#e6edf3
    style SSE fill:#1c2f50,stroke:#58a6ff,color:#e6edf3
    style CLI fill:#21262d,stroke:#30363d,color:#e6edf3
    style MCP fill:#21262d,stroke:#30363d,color:#e6edf3
    style UI fill:#21262d,stroke:#30363d,color:#e6edf3
```

### DB Schema

```sql
sessions (session_id, project, project_path, git_branch, first_prompt,
          summary, message_count, started_at, ended_at, claude_version,
          imported_at, imported_bytes)
          -- imported_bytes = byte offset up to which the JSONL has been
          -- parsed; lets subsequent imports tail-read only the new suffix
          -- instead of re-parsing the entire file.

messages (id, session_id, uuid, role, block_type, content,
          tool_name, tool_input, timestamp, turn_index)
          -- UNIQUE(session_id, turn_index) is the real dedup key; together
          -- with INSERT OR IGNORE it makes concurrent tail reads safe.

messages_fts (content)  -- FTS5, porter unicode61 tokenizer
```

### Filtering

| Stored | Excluded |
|--------|----------|
| User text | tool_use |
| Assistant text | tool_result |
| | thinking |
| | system (turn_duration, etc.) |
| | file-history-snapshot |
| | isSidechain = true |

## Global Options

```
--db <path>   Database file path (default: ~/.claude/vault.db)
--help        Show help
```

## Development

```bash
# CLI
deno task dev -- search "query"

# MCP server (stdio)
deno task dev -- mcp

# Web UI (frontend dev server + API server)
deno task ui:dev               # Vite dev server (port 5173, proxies /api to 6276)
deno task dev -- ui --foreground  # API server (port 6276)

# Build UI assets
deno task ui:build             # Vite build → ui/dist/
deno task ui:embed             # Embed ui/dist/ → src/ui_assets.ts

# Compile and install
deno task compile
deno task install

# Tests
deno task test
```

## Tech Stack

- [Deno](https://deno.com/) 2.x
- `node:sqlite` (DatabaseSync, built-in)
- SQLite FTS5
- `@std/cli`, `@std/fmt`, `@std/path`
- [Preact](https://preactjs.com/) + [Vite](https://vitejs.dev/) + [Tailwind CSS](https://tailwindcss.com/) (Web UI)
- [marked](https://marked.js.org/) (Markdown rendering)

## License

MIT
