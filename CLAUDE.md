# CLAUDE.md

## Database

- The SQLite DB is a derived cache of JSONL master data, not the source of truth
- On schema changes, delete and rebuild (`rm ~/.claude/vault.db`) rather than writing incremental migrations
- All DDL in `schema.ts` uses `CREATE ... IF NOT EXISTS`, so it is safe to execute on every startup
