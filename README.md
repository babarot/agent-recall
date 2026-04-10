# agent-recall

A CLI tool that archives Claude Code session history into SQLite for full-text search.

## Why

Claude Code stores conversations as JSONL files under `~/.claude/projects/`, but old sessions are automatically deleted and `/compact` loses detail. agent-recall automatically archives sessions on exit so you can search and reference past conversations anytime.

## Features

- **Auto-archive** -- SessionEnd hook saves sessions automatically on exit
- **Full-text search** -- Fast search powered by SQLite FTS5 with Porter stemmer
- **Noise filtering** -- Strips tool_use / tool_result / thinking, keeping only conversation text (~7% of raw data)
- **Idempotent** -- UUID-based deduplication; safe to import repeatedly
- **Zero dependencies** -- Single binary via `deno compile`; no external services

## Install

```bash
# Build from source (requires Deno 2.x)
git clone https://github.com/babarot/agent-recall.git
cd agent-recall
deno task compile
cp agent-recall ~/.claude/agent-recall
```

### Hook Setup

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

## Architecture

```
~/.claude/projects/*/*.jsonl
         |
         v
    [parser.ts]         Parse JSONL line by line
         |               Extract text blocks from user/assistant messages
         |               Filter out tool_use, tool_result, thinking, system
         v
    [SQLite + FTS5]     ~/.claude/vault.db
         |
         v
    search / list / export / stats
```

### DB Schema

```sql
sessions (session_id, project, project_path, git_branch, first_prompt,
          summary, message_count, started_at, ended_at, claude_version)

messages (id, session_id, uuid, role, content, timestamp, turn_index)

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
# Run in development
deno task dev -- search "query"

# Compile to binary
deno task compile

# Compile and install to ~/.claude/
deno task install
```

## Tech Stack

- [Deno](https://deno.com/) 2.x
- `node:sqlite` (DatabaseSync, built-in)
- SQLite FTS5
- `@std/cli`, `@std/fmt`, `@std/path`

## License

MIT
