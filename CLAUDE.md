# CLAUDE.md

## Database

- The SQLite DB started as a derived cache of the JSONL transcripts, but it is now the only copy of sessions whose JSONL has been deleted (Claude Code removes old transcripts). Treat it as irreplaceable
- Never delete or rebuild `~/.claude/vault.db`. Schema changes must be additive (`ALTER TABLE ... ADD COLUMN`, new tables or indexes), never a drop and recreate
- All DDL in `schema.ts` uses `CREATE ... IF NOT EXISTS`, so it is safe to execute on every startup
- Develop and test against a copy (`sqlite3 ~/.claude/vault.db ".backup '/path/to/copy.db'"`), not the live file

## Go port

- The TypeScript implementation in `src/` is being ported to Go (`cmd/recall`, `internal/`). Until the switch, the Go binary is built as `recall-go` and must not write to the live database
- Output that other tools read (`--format json`, MCP tool results, HTTP API responses) must stay identical to the TypeScript version. `scripts/parity/` compares the two against a snapshot of the archive
