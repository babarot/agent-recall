# claude-recall

[![Test](https://github.com/babarot/claude-recall/actions/workflows/test.yaml/badge.svg)](https://github.com/babarot/claude-recall/actions/workflows/test.yaml)

Recall any past Claude Code session: ask Claude to look into it, or find it yourself and go back to it.

<!-- TODO: screenshot or GIF of the TUI -->

claude-recall archives every Claude Code session into SQLite, including the ones whose JSONL Claude Code has since deleted, and gives you three ways back into them from one binary called `recall`:

- MCP server: Claude searches past sessions and reads what was done in them, from inside the session you are working in
- TUI: find a session yourself and resume it where it left off
- Web UI: read past conversations comfortably in the browser, live as they are written

## Quick start

```bash
curl -fsSL https://raw.githubusercontent.com/babarot/claude-recall/main/bin/install.sh | bash
recall
```

The installer puts `recall` in `~/.local/bin`, imports your sessions and registers the MCP server with Claude Code. Then ask Claude about a past session ("how did we set up the staging deploy last month?"), or run `recall` to browse them. See [Install](#install) for Nix, building from source and the Claude Code plugin.

## Use cases

### Pick up where an earlier task left off

At work, a project rarely fits in one session. Project A gets split into tasks 1 to 10, and each one runs in its own session. While working on task 5, you need what was decided in task 3: ask Claude "what did we decide about the retry policy in the task 3 session?" and it looks that session up through MCP and answers from it. Or, to carry on with task 3 itself, find it in the TUI and press Enter to resume it.

### Answer a question you already answered months ago

A support question comes in, you open a session, and it is settled in ten minutes. Months later a similar question arrives. By then Claude Code has deleted that transcript, but claude-recall still has it: ask Claude to find how it was handled last time, or search for it yourself and read it again.

## How to recall

| You want to | Use | What only it gives you |
|---|---|---|
| Have Claude remember what happened in a past session | [MCP server](#mcp-server) | The answer lands in the session you are working in, as context Claude can use right away |
| Find a session yourself and go back to it | [TUI](#tui) | Resume it in place (`claude -r`), or copy its ID to hand to another agent |
| Find a session yourself and read it | [Web UI](#web-ui) | A comfortable reader in the browser, for long conversations and images |

The [CLI](#cli) underneath starts each of them (`recall mcp`, `recall`, `recall ui`) and also searches, lists and exports from the terminal or a script.

### MCP server

Each Claude Code session runs its own `recall mcp`, so Claude can look into past sessions whenever it needs to. Nothing is injected into the prompt: Claude calls these tools when you ask about the past or when it realizes something was discussed before.

| Tool | Description |
|------|-------------|
| `recall_search` | Full-text search across past sessions |
| `recall_list` | List archived sessions |
| `recall_export` | Export a session's full conversation |
| `recall_stats` | Show archive statistics |

A session ID from the TUI (`y`) works too: "look up session a1b2c3 with claude-recall and continue from there".

### TUI

`recall` lists every archived session with its title, folder (a git worktree is shown under the repository it belongs to), branch, message count, size and ID. Started inside a repository, it shows only that repository's sessions; `.` switches to all of them, and `recall --all` starts with all of them.

| Key | Action |
|-----|--------|
| `Enter` | Resume the session: `claude -r <id>` from the session's folder |
| `y` / `Y` | Copy the session ID / the resume command |
| `/` | Filter by title, folder, branch, ID or what was said (see below) |
| `a` | Ask Claude to find sessions, when you remember what it was about but not what to type |
| `Space` | Read the conversation over the detail pane |
| `←` `→` | Show or hide the folder list, to narrow to one repository or folder |
| `s` | Choose the sort order |
| `?` | Show every key |
| `q` | Quit |

The filter matches words against titles, folders, branches, IDs and the conversation. Keys narrow it to one field:

| Filter | Matches |
|--------|---------|
| `text:<word>` | Only what was said in the conversation |
| `title:` `branch:` `worktree:` | Part of that field |
| `id:<prefix>` | The start of the session ID |
| `in:<folder>` | Folders whose name matches fuzzily (`in:bdot` for babarot/dotfiles) |

`a` runs Claude Code itself (`claude -p`, signed in as you, with recall's search as its only tools), so no API key is needed. It lists the sessions it found with why each matched; Enter jumps to one, `f` narrows the list to all of them.

Its settings are under `[tui]` in the [config file](#configuration). See [docs/tui.md](docs/tui.md) for every key, the detail pane and the mouse.

### Web UI

```bash
recall ui            # http://localhost:6276, in the background
recall ui stop
```

A session browser, a chat viewer and search. While it runs, new and changed sessions are imported within about half a second, the session list moves them to the top, and the chat view of a running session follows new messages. The server listens on 127.0.0.1 only.

`/recall` in Claude Code (from the [plugin](#claude-code-plugin)) opens it on the current session.

## Why

claude-recall is a recall tool, not a memory system. The goal is to make `grep ~/.claude/projects/**/*.jsonl` a better experience, and to keep those files around after Claude Code deletes them. When you or the agent realize something was discussed before, you look it up. See [ADR-001](docs/adr/001-recall-not-memory-extension.md).

### Why not just [claude-mem](https://github.com/thedotmack/claude-mem)?

[claude-mem](https://github.com/thedotmack/claude-mem) is an excellent project solving a related problem, and if it fits your workflow, you should use it. claude-recall deliberately solves a different one.

claude-mem extends the agent's memory, for Claude Code and many other agents. It captures observations on every tool use, summarizes them with an LLM into structured facts, stores them in a vector DB, and injects the result into the next session's prompt. claude-recall doesn't touch the memory layer. It stores the JSONL files Claude Code already writes and searches them with SQLite FTS5.

|  | claude-mem | claude-recall |
|---|---|---|
| Goal | Extend the agent's memory | Help you and the agent recall |
| Injection | Push (auto-injected at `SessionStart`) | Pull (looked up when needed) |
| What's stored | LLM-summarized observations | Raw conversation, noise-stripped |
| Search | FTS5 + Chroma vector hybrid | FTS5 only (deterministic) |
| LLM calls | During indexing, through a hosted observer, your own OpenRouter or Gemini key, or your Anthropic plan | Never while storing or searching; only when you ask Claude from the TUI (`a`), through your own `claude` |
| Where data lives | Local, with optional cloud sync | Local only |
| Agents | Claude Code, Codex, Gemini, OpenCode and more | Claude Code |
| Runtime | Node + Bun + Python (uv) + Chroma, resident worker | One static binary, no daemon |

The tradeoff claude-recall picks:

- Raw logs don't drift. What's stored is what happened, not a summary of it.
- The agent says when it doesn't know. Past context comes from a tool call, not from memory it may misremember.
- Idle means idle. No worker, no background LLM calls.
- One binary, works offline.

## Install

### curl

```bash
curl -fsSL https://raw.githubusercontent.com/babarot/claude-recall/main/bin/install.sh | bash
```

Installs `recall` to `~/.local/bin` (set `RECALL_INSTALL_DIR` to change it), imports your sessions and registers the MCP server.

### Nix

Each release is published to [babarot/nur-packages](https://github.com/babarot/nur-packages):

```bash
nix profile install github:babarot/nur-packages#claude-recall
```

The package also carries the [Claude Code plugin](#claude-code-plugin) under `share/claude-plugin/claude-recall`.

### Build from source

Requires Go and Node.js (for the web UI):

```bash
git clone https://github.com/babarot/claude-recall.git
cd claude-recall
make install   # builds the UI, embeds it, installs recall to ~/.local/bin
```

`go install github.com/babarot/claude-recall/cmd/recall@latest` also works; that build leaves the web UI out.

### Claude Code plugin

The plugin is the recommended way to connect claude-recall to Claude Code. [`plugin/`](plugin) bundles, with `recall` on PATH:

| Component | What it does |
|-----------|--------------|
| MCP server | Runs `recall mcp` |
| `SessionEnd` hook | Runs `recall import` when a session ends |
| `recall` skill | `/recall` opens the web UI on the current session; also `/recall list`, `/recall stats`, `/recall <session-id>` and `/recall stop` |

Each release ships it as `claude-recall-plugin.tar.gz`. A plugin directory under `~/.claude/skills/` loads as `claude-recall@skills-dir`, so with Nix:

```bash
ln -s ~/.nix-profile/share/claude-plugin/claude-recall ~/.claude/skills/claude-recall
```

Without the plugin, register just the MCP server (the curl installer does this):

```bash
claude mcp add claude-recall -s user -- recall mcp
```

For Codex and other agents, link just the skill: `plugin/skills/recall` into `~/.agents/skills/recall`.

## Your archive

The archive is `~/.claude/vault.db`. Sessions whose JSONL Claude Code has deleted stay in it, so it is the only copy of them: back it up, and never delete it to rebuild it.

```bash
sqlite3 ~/.claude/vault.db ".backup '/path/to/backup.db'"
```

Sessions are imported from `~/.claude/projects` (`$CLAUDE_CONFIG_DIR/projects` when Claude Code runs with `CLAUDE_CONFIG_DIR`) while the MCP server or the web UI runs, when a session ends (the plugin's hook), and by `recall import`. The archive stays in `~/.claude` either way; `db` in the [config file](#configuration) moves it.

| Stored | Excluded |
|--------|----------|
| User and assistant text | System events (turn_duration and others) |
| Thinking | File history snapshots |
| Tool calls and their results | Sidechains |
| Slash-command expansions, task notifications | Progress, queue operations and other bookkeeping |

## Configuration

`~/.config/claude-recall/config.toml` (or `$XDG_CONFIG_HOME/claude-recall/config.toml`). `recall` writes it the first time the TUI runs, with every setting at its default and commented out; uncomment a line to change it. An unknown key, or a key in the wrong section, is reported instead of ignored.

`db` and `port` are the defaults of `--db` and `--port`, so the MCP server and the `SessionEnd` import use the archive you set here too. A mistake under `[tui]` stops only the TUI.

```toml
# claude-recall settings. Uncomment a line to change it.

[core]
# The archive database, for every command, the MCP server and the web UI,
# unless --db says otherwise: an absolute path or one starting with ~/.
# db = "~/.claude/vault.db"

[ui]
# Where the web UI (recall ui) listens, and where recall ui stop and
# recall ui status look for it, unless --port says otherwise.
# port = 6276

[tui]
# Where the detail pane goes: "bottom" (default), "right", or "auto" to put it
# on the right when the terminal is at least detail_auto_width columns wide.
# detail_position = "bottom"
# detail_auto_width = 160
# Initial height of the detail pane below the list, in lines (at least 10).
# detail_height = 16
# Color scheme: "auto" (default) picks catppuccin-mocha on a dark terminal and
# catppuccin-latte on a light one. Also: tokyo-night, dracula, nord,
# gruvbox-dark, and ansi (the terminal's own 16 colors).
# theme = "auto"
# Which sessions to start with: "folder" (default) for the repository recall is
# started in, when it has sessions, or "all".
# scope = "folder"
# a asks Claude Code (claude -p, on your Claude plan) to find sessions.
# The model: a family and version such as "sonnet-5.5", "opus-5.5" or
# "haiku-4.5", or a full model ID. Whether to show what an answer cost (the
# price claude reports; on a Claude plan it counts toward your usage rather
# than being billed), and why Claude picked each session.
# ask_model = "sonnet-5.5"
# ask_show_cost = true
# ask_reasons = true
```

## CLI

```bash
recall                      # Browse sessions (same as recall tui)
recall import               # Import all sessions
recall search "terraform module"
recall search "deploy" --project oksskolten --from 2026-03-01
recall list --project gh-infra --format json
recall export <session-id> --format json --output session.json
recall stats
recall ui                   # Web UI in the background (http://localhost:6276)
recall mcp                  # MCP server over stdio (started by Claude Code)
recall version
```

### Import

```
recall import [options]

  --session <uuid>    Import a specific session (an ID prefix works)
  --project <name>    Import sessions whose project matches
  --dry-run           Show what would be imported without writing
```

### Search

```
recall search <query> [options]

  --project <name>    Filter by project
  --limit <n>         Max results (default: 20)
  --from <date>       Start date (YYYY-MM-DD)
  --to <date>         End date (YYYY-MM-DD)
  --format text|json  Output format (default: text)
```

Supports FTS5 query syntax: `"exact phrase"`, `term1 AND term2`, `term1 OR term2`, `term1 NOT term2`.

### List

```
recall list [options]

  --project <name>    Filter by project
  --limit <n>         Max sessions (default: 50)
  --format text|json  Output format (default: text)
```

### Export

```
recall export <session-id> [options]

  --format markdown|json|text  Output format (default: markdown)
  --output <file>              Write to file instead of stdout
```

A session ID prefix works: `recall export a1b2`.

### Stats

```
recall stats [--project <name>]
```

### Web UI

```
recall ui [--port <n>]        Start in the background (default port: port in the config file, or 6276)
recall ui --foreground        Run in the foreground
recall ui status              Show server status
recall ui stop                Stop the server
```

### Global options

```
--db <path>   Database file (default: db in the config file, or ~/.claude/vault.db)
-h, --help    Show help; recall <command> --help shows a command's flags
```

A flag a command does not take is an error, not ignored.

### Shell completion

```bash
source <(recall completion zsh)    # also bash, fish and powershell
```

## Development

```bash
make test                      # go vet, go test, UI tests
make build                     # recall with the web UI embedded
go run ./cmd/recall search "query" --db /tmp/vault-copy.db

# Web UI: Vite dev server on 5173, proxying /api to the Go server on 6276
cd ui && npm run dev
go run ./cmd/recall ui --foreground
```

Work against a copy of the archive (`sqlite3 ~/.claude/vault.db ".backup '/tmp/vault-copy.db'"`), not the live file.

[docs/architecture.md](docs/architecture.md) covers how sessions flow into the archive, the schema and the web UI's live updates. The [ADRs](docs/adr) record the larger decisions, such as why claude-recall moved from Deno to Go ([ADR-004](docs/adr/004-go-port.md)).

### Tech stack

- Go, [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) (SQLite without CGo) with FTS5
- [Bubble Tea](https://github.com/charmbracelet/bubbletea), Bubbles and Lip Gloss (TUI)
- The [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [Preact](https://preactjs.com/), [Vite](https://vitejs.dev/), [Tailwind CSS](https://tailwindcss.com/) and [marked](https://marked.js.org/) (web UI)

## License

MIT
