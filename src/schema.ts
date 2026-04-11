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
    imported_at    TEXT DEFAULT (datetime('now')),
    imported_bytes INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project);
CREATE INDEX IF NOT EXISTS idx_sessions_started_at ON sessions(started_at);

CREATE TABLE IF NOT EXISTS messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    uuid       TEXT,
    role       TEXT NOT NULL,
    block_type TEXT NOT NULL DEFAULT 'text',
    content    TEXT NOT NULL,
    tool_name  TEXT,
    tool_input TEXT,
    timestamp  TEXT,
    turn_index INTEGER
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_turn ON messages(session_id, turn_index);
CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id);

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

CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT DEFAULT (datetime('now'))
);
`;
