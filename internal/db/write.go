package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// FileInfo is what the importer stored about a transcript file, for change
// detection.
type FileInfo struct {
	MessageCount int
	FileMtime    *float64 // milliseconds since the epoch
	FileSize     *int64
}

// GetFileInfo returns the stored file metadata of a session, or nil when the
// session is not archived.
func (d *DB) GetFileInfo(sessionID string) (*FileInfo, error) {
	var fi FileInfo
	err := d.sql.QueryRow(`SELECT message_count, file_mtime, file_size FROM sessions WHERE session_id = ?`, sessionID).
		Scan(&fi.MessageCount, &fi.FileMtime, &fi.FileSize)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("file info: %w", err)
	}
	return &fi, nil
}

// SessionRow is a row of the sessions table.
type SessionRow struct {
	SessionID     string
	Project       string
	ProjectPath   string
	GitBranch     string
	FirstPrompt   string
	Summary       *string
	MessageCount  int
	StartedAt     string
	EndedAt       string
	ClaudeVersion string
	FileMtime     *float64
	FileSize      *int64
}

// MessageRow is a row of the messages table.
type MessageRow struct {
	UUID       string
	Role       string
	BlockType  string
	BlockIndex int
	Content    string
	ToolName   *string
	ToolInput  *string
	Timestamp  string
	TurnIndex  int
}

// ImageRow is a row of the images table.
type ImageRow struct {
	MessageUUID string
	ImageIndex  int
	MediaType   string
	Data        []byte
}

// ReplaceSession deletes everything stored for a session and inserts the
// given rows, in one transaction, so a reader or a concurrent import never
// sees the session half written.
func (d *DB) ReplaceSession(s SessionRow, msgs []MessageRow, imgs []ImageRow) (err error) {
	tx, err := d.sql.Begin()
	if err != nil {
		return fmt.Errorf("replace session: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	for _, q := range []string{
		`DELETE FROM images WHERE session_id = ?`,
		`DELETE FROM messages WHERE session_id = ?`,
		`DELETE FROM sessions WHERE session_id = ?`,
	} {
		if _, err = tx.Exec(q, s.SessionID); err != nil {
			return fmt.Errorf("replace session: %w", err)
		}
	}

	if _, err = tx.Exec(`INSERT INTO sessions (session_id, project, project_path, git_branch, first_prompt, summary, message_count, started_at, ended_at, claude_version, file_mtime, file_size)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.SessionID, s.Project, s.ProjectPath, s.GitBranch, s.FirstPrompt, s.Summary, s.MessageCount,
		s.StartedAt, s.EndedAt, s.ClaudeVersion, s.FileMtime, s.FileSize); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}

	msgStmt, err := tx.Prepare(`INSERT OR IGNORE INTO messages (session_id, uuid, role, block_type, block_index, content, tool_name, tool_input, timestamp, turn_index)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("insert messages: %w", err)
	}
	defer msgStmt.Close()
	for _, m := range msgs {
		if _, err = msgStmt.Exec(s.SessionID, m.UUID, m.Role, m.BlockType, m.BlockIndex, m.Content,
			m.ToolName, m.ToolInput, m.Timestamp, m.TurnIndex); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
	}

	imgStmt, err := tx.Prepare(`INSERT INTO images (session_id, message_uuid, image_index, media_type, data)
       VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("insert images: %w", err)
	}
	defer imgStmt.Close()
	for _, img := range imgs {
		if _, err = imgStmt.Exec(s.SessionID, img.MessageUUID, img.ImageIndex, img.MediaType, img.Data); err != nil {
			return fmt.Errorf("insert image: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("replace session: %w", err)
	}
	return nil
}
