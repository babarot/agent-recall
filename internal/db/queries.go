package db

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/babarot/claude-recall/internal/jscompat"
)

// Nullable columns are pointers so they encode as null in JSON.

// ListedSession is a row of listSessions.
type ListedSession struct {
	SessionID    string  `json:"sessionId"`
	Project      string  `json:"project"`
	ProjectPath  *string `json:"projectPath"`
	GitBranch    *string `json:"gitBranch"`
	FirstPrompt  *string `json:"firstPrompt"`
	MessageCount *int64  `json:"messageCount"`
	StartedAt    *string `json:"startedAt"`
	EndedAt      *string `json:"endedAt"`
	Title        *string `json:"title,omitempty"`
}

// ListOptions narrows ListSessions.
type ListOptions struct {
	Project string
	Limit   *int // nil means 50
	Offset  int
}

// ListSessions returns sessions, most recently active first.
func (d *DB) ListSessions(opts ListOptions) ([]ListedSession, error) {
	limit := 50
	if opts.Limit != nil {
		limit = *opts.Limit
	}
	where, args := "", []any{}
	if opts.Project != "" {
		where = "WHERE (project LIKE ? OR project_path LIKE ?)"
		args = append(args, "%"+opts.Project+"%", "%"+opts.Project+"%")
	}
	args = append(args, limit, opts.Offset)
	title := "NULLIF(title, '')"
	if ok, err := d.hasColumn("sessions", "title"); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	} else if !ok {
		title = "NULL" // an archive opened read-only before the migration
	}
	rows, err := d.sql.Query(`
      SELECT session_id, project, project_path, git_branch, first_prompt,
             message_count, started_at, ended_at, `+title+`
      FROM sessions `+where+`
      ORDER BY COALESCE(ended_at, started_at) DESC
      LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	out := []ListedSession{}
	for rows.Next() {
		var s ListedSession
		if err := rows.Scan(&s.SessionID, &s.Project, &s.ProjectPath, &s.GitBranch, &s.FirstPrompt,
			&s.MessageCount, &s.StartedAt, &s.EndedAt, &s.Title); err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ExportedSession is the session part of exportSession.
type ExportedSession struct {
	SessionID   string  `json:"sessionId"`
	Project     string  `json:"project"`
	ProjectPath *string `json:"projectPath"`
	GitBranch   *string `json:"gitBranch"`
	StartedAt   *string `json:"startedAt"`
	FirstPrompt *string `json:"firstPrompt"`
	Summary     *string `json:"summary"`
}

// ExportedMessage is a message of exportSession.
type ExportedMessage struct {
	UUID      string  `json:"uuid"`
	Role      string  `json:"role"`
	BlockType string  `json:"blockType"`
	Content   string  `json:"content"`
	ToolName  *string `json:"toolName"`
	ToolInput *string `json:"toolInput"`
	Timestamp *string `json:"timestamp"`
	TurnIndex *int64  `json:"turnIndex"`
}

// ExportSession finds a session by ID or ID prefix and returns it with all
// of its messages. The session is nil when nothing matches.
func (d *DB) ExportSession(id string) (*ExportedSession, []ExportedMessage, error) {
	var s ExportedSession
	err := d.sql.QueryRow(`
        SELECT session_id, project, project_path, git_branch, started_at, first_prompt, summary
        FROM sessions WHERE session_id = ? OR session_id LIKE ?`, id, id+"%").
		Scan(&s.SessionID, &s.Project, &s.ProjectPath, &s.GitBranch, &s.StartedAt, &s.FirstPrompt, &s.Summary)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, []ExportedMessage{}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("export: %w", err)
	}
	rows, err := d.sql.Query(`
        SELECT uuid, role, block_type, content, tool_name, tool_input, timestamp, turn_index
        FROM messages WHERE session_id = ?
        ORDER BY turn_index`, s.SessionID)
	if err != nil {
		return nil, nil, fmt.Errorf("export: %w", err)
	}
	defer rows.Close()
	msgs := []ExportedMessage{}
	for rows.Next() {
		var m ExportedMessage
		if err := rows.Scan(&m.UUID, &m.Role, &m.BlockType, &m.Content, &m.ToolName, &m.ToolInput, &m.Timestamp, &m.TurnIndex); err != nil {
			return nil, nil, fmt.Errorf("export: %w", err)
		}
		msgs = append(msgs, m)
	}
	return &s, msgs, rows.Err()
}

// ProjectStats is one project of Stats.
type ProjectStats struct {
	Project     string `json:"project"`
	ProjectPath string `json:"projectPath"`
	Sessions    int64  `json:"sessions"`
	Messages    int64  `json:"messages"`
}

// MonthStats is one month of Stats.
type MonthStats struct {
	Month    *string `json:"month"`
	Sessions int64   `json:"sessions"`
	Messages int64   `json:"messages"`
}

// Stats summarizes the archive.
type Stats struct {
	TotalSessions int64
	TotalMessages int64
	DBSizeBytes   int64
	ByProject     []ProjectStats
	ByMonth       []MonthStats
}

// Stats counts sessions and messages overall, per project and per month.
func (d *DB) Stats(project string) (*Stats, error) {
	filter, args := "", []any{}
	if project != "" {
		filter = "WHERE (project LIKE ? OR project_path LIKE ?)"
		args = []any{"%" + project + "%", "%" + project + "%"}
	}
	var s Stats
	if err := d.sql.QueryRow(`SELECT COUNT(DISTINCT session_id), COALESCE(SUM(message_count), 0)
         FROM sessions `+filter, args...).Scan(&s.TotalSessions, &s.TotalMessages); err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}

	rows, err := d.sql.Query(`SELECT project, COALESCE(project_path, project) as projectPath,
                COUNT(*) as sessions,
                COALESCE(SUM(message_count), 0) as messages
         FROM sessions `+filter+`
         GROUP BY project ORDER BY sessions DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}
	s.ByProject = []ProjectStats{}
	for rows.Next() {
		var p ProjectStats
		if err := rows.Scan(&p.Project, &p.ProjectPath, &p.Sessions, &p.Messages); err != nil {
			rows.Close()
			return nil, fmt.Errorf("stats: %w", err)
		}
		s.ByProject = append(s.ByProject, p)
	}
	rows.Close()

	rows, err = d.sql.Query(`SELECT strftime('%Y-%m', started_at) as month,
                COUNT(*) as sessions,
                COALESCE(SUM(message_count), 0) as messages
         FROM sessions `+filter+`
         GROUP BY month ORDER BY month DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}
	s.ByMonth = []MonthStats{}
	for rows.Next() {
		var m MonthStats
		if err := rows.Scan(&m.Month, &m.Sessions, &m.Messages); err != nil {
			rows.Close()
			return nil, fmt.Errorf("stats: %w", err)
		}
		s.ByMonth = append(s.ByMonth, m)
	}
	rows.Close()

	if err := d.sql.QueryRow(`SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&s.DBSizeBytes); err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}
	return &s, nil
}

func (d *DB) userText(sessionID, order string) (*string, error) {
	var content string
	err := d.sql.QueryRow(`SELECT content FROM messages
         WHERE session_id = ? AND block_type = 'text' AND role = 'user' AND content NOT LIKE '<%'
         ORDER BY turn_index `+order+` LIMIT 1`, sessionID).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := jscompat.Slice(content, 500)
	return &s, nil
}

// FirstUserText is the first real user text of a session, skipping
// tag-only messages, cut to 500 characters.
func (d *DB) FirstUserText(sessionID string) (*string, error) { return d.userText(sessionID, "") }

// LastUserText is the last real user text of a session.
func (d *DB) LastUserText(sessionID string) (*string, error) { return d.userText(sessionID, "DESC") }

// jsDateMillis is new Date(s).getTime() for the ISO timestamps Claude Code
// writes; anything else is NaN.
func jsDateMillis(s string) float64 {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return math.NaN()
	}
	return float64(t.UnixMilli())
}

// SessionActivities buckets each session's message timestamps into a
// histogram for the sidebar sparkline, like getSessionActivities.
func (d *DB) SessionActivities(ids []string, buckets int) (map[string][]int, error) {
	out := map[string][]int{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := d.sql.Query(`SELECT session_id, timestamp FROM messages
         WHERE session_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`) AND timestamp IS NOT NULL
         ORDER BY session_id, timestamp`, args...)
	if err != nil {
		return nil, fmt.Errorf("activities: %w", err)
	}
	defer rows.Close()
	var order []string
	bySession := map[string][]float64{}
	for rows.Next() {
		var id, ts string
		if err := rows.Scan(&id, &ts); err != nil {
			return nil, fmt.Errorf("activities: %w", err)
		}
		if _, ok := bySession[id]; !ok {
			order = append(order, id)
		}
		bySession[id] = append(bySession[id], jsDateMillis(ts))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range order {
		ts := bySession[id]
		counts := make([]int, buckets)
		if len(ts) < 2 {
			for i := range counts {
				counts[i] = len(ts)
			}
			out[id] = counts
			continue
		}
		lo, hi := ts[0], ts[len(ts)-1]
		span := hi - lo
		if span == 0 || math.IsNaN(span) { // `max - min || 1`
			span = 1
		}
		for _, t := range ts {
			idx := math.Min(math.Floor((t-lo)/span*float64(buckets)), float64(buckets-1))
			if math.IsNaN(idx) || idx < 0 {
				continue // counts[NaN]++ sets a non-index property in JavaScript
			}
			counts[int(idx)]++
		}
		out[id] = counts
	}
	return out, nil
}

// GetImage returns an image's media type and bytes, or ok=false.
func (d *DB) GetImage(sessionID, messageUUID string, index int) (mediaType string, data []byte, ok bool, err error) {
	err = d.sql.QueryRow(`SELECT media_type, data FROM images
         WHERE session_id = ? AND message_uuid = ? AND image_index = ?`, sessionID, messageUUID, index).Scan(&mediaType, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	return mediaType, data, true, nil
}
