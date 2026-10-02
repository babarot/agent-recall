package db

import (
	"fmt"
	"slices"
	"time"
)

// Session is one archived session as the TUI lists it.
type Session struct {
	ID           string
	Project      string
	ProjectPath  string
	GitBranch    string
	FirstPrompt  string
	Title        string // empty until the title column exists and is filled
	MessageCount int
	FileSize     int64
	StartedAt    time.Time
	EndedAt      time.Time
}

func (d *DB) hasColumn(table, column string) (bool, error) {
	rows, err := d.sql.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Sessions returns every archived session, most recently ended first.
func (d *DB) Sessions() ([]Session, error) {
	titleExpr := "''"
	if ok, err := d.hasColumn("sessions", "title"); err != nil {
		return nil, fmt.Errorf("sessions: %w", err)
	} else if ok {
		titleExpr = "COALESCE(title, '')"
	}

	rows, err := d.sql.Query(`
      SELECT session_id, project, COALESCE(project_path, ''), COALESCE(git_branch, ''),
             COALESCE(first_prompt, ''), ` + titleExpr + `, COALESCE(message_count, 0),
             COALESCE(file_size, 0), COALESCE(started_at, ''), COALESCE(ended_at, '')
      FROM sessions
      ORDER BY ended_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var s Session
		var started, ended string
		if err := rows.Scan(&s.ID, &s.Project, &s.ProjectPath, &s.GitBranch, &s.FirstPrompt,
			&s.Title, &s.MessageCount, &s.FileSize, &started, &ended); err != nil {
			return nil, fmt.Errorf("sessions: %w", err)
		}
		s.StartedAt, s.EndedAt = parseTime(started), parseTime(ended)
		out = append(out, s)
	}
	return out, rows.Err()
}

// Message is one text block of a session.
type Message struct {
	Role      string
	Content   string
	Timestamp time.Time
}

// Preview is the start and end of a session's conversation.
type Preview struct {
	Head    []Message
	Tail    []Message
	Skipped int // messages between Head and Tail that were left out
}

// textMessages selects the text blocks a reader would recognize as the
// conversation. User blocks that are only tags (slash commands, hook output)
// are skipped.
const textMessages = `FROM messages
      WHERE session_id = ? AND block_type = 'text'
        AND NOT (role = 'user' AND content LIKE '<%')`

func (d *DB) messages(query string, args ...any) ([]Message, error) {
	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var ts string
		if err := rows.Scan(&m.Role, &m.Content, &ts); err != nil {
			return nil, err
		}
		m.Timestamp = parseTime(ts)
		out = append(out, m)
	}
	return out, rows.Err()
}

// SessionPreview returns the first head and the last tail text messages of a
// session, in conversation order.
func (d *DB) SessionPreview(sessionID string, head, tail int) (Preview, error) {
	var total int
	if err := d.sql.QueryRow(`SELECT COUNT(*) `+textMessages, sessionID).Scan(&total); err != nil {
		return Preview{}, fmt.Errorf("preview: %w", err)
	}
	const cols = `SELECT role, content, COALESCE(timestamp, '') `
	if total <= head+tail {
		all, err := d.messages(cols+textMessages+` ORDER BY turn_index, block_index`, sessionID)
		if err != nil {
			return Preview{}, fmt.Errorf("preview: %w", err)
		}
		return Preview{Head: all}, nil
	}
	h, err := d.messages(cols+textMessages+` ORDER BY turn_index, block_index LIMIT ?`, sessionID, head)
	if err != nil {
		return Preview{}, fmt.Errorf("preview: %w", err)
	}
	t, err := d.messages(cols+textMessages+` ORDER BY turn_index DESC, block_index DESC LIMIT ?`, sessionID, tail)
	if err != nil {
		return Preview{}, fmt.Errorf("preview: %w", err)
	}
	slices.Reverse(t)
	return Preview{Head: h, Tail: t, Skipped: total - len(h) - len(t)}, nil
}
