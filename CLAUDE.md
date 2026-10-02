# CLAUDE.md

## Database

- The SQLite DB started as a derived cache of the JSONL transcripts, but it is now the only copy of sessions whose JSONL has been deleted (Claude Code removes old transcripts). Treat it as irreplaceable
- Never delete or rebuild `~/.claude/vault.db`. Schema changes are migrations in `internal/db/db.go` that only add (`ALTER TABLE ... ADD COLUMN`, new tables or indexes), tracked by `PRAGMA user_version`
- `internal/db/schema.go` uses `CREATE ... IF NOT EXISTS`, so it is safe to execute on every startup
- Develop and test against a copy (`sqlite3 ~/.claude/vault.db ".backup '/path/to/copy.db'"`), not the live file

## Compatibility

- MCP tool names, inputs and results, and the HTTP API the web UI reads, are interfaces other tools depend on. Add fields rather than change or remove them
- `internal/jscompat` keeps stored text identical to what the earlier TypeScript importer wrote (JavaScript trim, UTF-16 lengths, JSON.stringify). Keep using it in the parser so re-imports do not rewrite existing rows
