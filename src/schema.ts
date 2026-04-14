export const SCHEMA_SQL = `
CREATE TABLE IF NOT EXISTS sessions (
    session_id     TEXT PRIMARY KEY,
    project        TEXT NOT NULL,
    project_path   TEXT,
    git_branch     TEXT,
    first_prompt   TEXT,
    summary        TEXT,
    message_count  INTEGER DEFAULT 0,
    started_at     TEXT,
    ended_at       TEXT,
    claude_version TEXT,
    file_mtime     REAL,
    file_size      INTEGER,
    imported_at    TEXT DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project);
CREATE INDEX IF NOT EXISTS idx_sessions_started_at ON sessions(started_at);
CREATE INDEX IF NOT EXISTS idx_sessions_ended_at ON sessions(ended_at);

CREATE TABLE IF NOT EXISTS messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL REFERENCES sessions(session_id),
    uuid        TEXT NOT NULL,
    role        TEXT NOT NULL,
    block_type  TEXT NOT NULL DEFAULT 'text',
    block_index INTEGER NOT NULL DEFAULT 0,
    content     TEXT NOT NULL,
    tool_name   TEXT,
    tool_input  TEXT,
    timestamp   TEXT,
    turn_index  INTEGER
);

-- Natural-key uniqueness: each (session, JSONL line uuid, block position
-- within the line) is unique. This makes re-imports idempotent — any time
-- the watcher triggers a full re-parse of a session's JSONL (e.g. after a
-- /compact rewrite), duplicate inserts are silently ignored by SQLite.
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_ident
    ON messages(session_id, uuid, block_index);

CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id);
-- Backs chat-view ORDER BY turn_index reads.
CREATE INDEX IF NOT EXISTS idx_messages_session_turn ON messages(session_id, turn_index);

CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    content,
    content='messages',
    content_rowid='id',
    tokenize='porter unicode61'
);

CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages
WHEN new.block_type = 'text' BEGIN
    INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
END;

CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages
WHEN old.block_type = 'text' BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, content) VALUES('delete', old.id, old.content);
END;

CREATE TABLE IF NOT EXISTS images (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    message_uuid TEXT,
    image_index INTEGER DEFAULT 0,
    media_type TEXT NOT NULL,
    data       BLOB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_images_session_message ON images(session_id, message_uuid);

`;
