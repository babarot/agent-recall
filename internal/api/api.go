// Package api builds the JSON values the MCP tools and the web API return.
// Field order, names and null handling are part of the interface: a date
// derived from a missing start time is left out, while a nullable column is
// kept as null.
package api

import (
	"github.com/babarot/claude-recall/internal/cli"
	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/jscompat"
)

func slice(p *string, n int) *string {
	if p == nil {
		return nil
	}
	s := jscompat.Slice(*p, n)
	return &s
}

// SearchHit is a search result. The MCP tool shortens the session ID to 8
// characters; the web API keeps it whole.
type SearchHit struct {
	SessionID string  `json:"sessionId"`
	Project   string  `json:"project"`
	Branch    *string `json:"branch"`
	Date      *string `json:"date,omitempty"`
	Role      string  `json:"role"`
	Content   string  `json:"content"`
}

// SearchHits converts search results.
func SearchHits(results []db.SearchResult, shortID bool) []SearchHit {
	out := make([]SearchHit, len(results))
	for i, r := range results {
		id := r.SessionID
		if shortID {
			id = jscompat.Slice(id, 8)
		}
		out[i] = SearchHit{SessionID: id, Project: cli.DisplayProject(r.ProjectPath, r.Project), Branch: r.GitBranch,
			Date: slice(r.StartedAt, 10), Role: r.Role, Content: r.Content}
	}
	return out
}

// ListedSession is a recall_list entry.
type ListedSession struct {
	SessionID     string  `json:"sessionId"`
	FullSessionID string  `json:"fullSessionId"`
	Title         *string `json:"title,omitempty"`
	Project       string  `json:"project"`
	Branch        *string `json:"branch"`
	FirstPrompt   *string `json:"firstPrompt,omitempty"`
	Messages      *int64  `json:"messages"`
	Date          *string `json:"date,omitempty"`
}

// List converts sessions for recall_list.
func List(sessions []db.ListedSession) []ListedSession {
	out := make([]ListedSession, len(sessions))
	for i, s := range sessions {
		out[i] = ListedSession{SessionID: jscompat.Slice(s.SessionID, 8), FullSessionID: s.SessionID, Title: s.Title,
			Project: cli.DisplayProject(s.ProjectPath, s.Project), Branch: s.GitBranch,
			FirstPrompt: slice(s.FirstPrompt, 200), Messages: s.MessageCount, Date: slice(s.StartedAt, 10)}
	}
	return out
}

// SessionHeader is the session part of recall_export and /api/sessions/:id.
type SessionHeader struct {
	SessionID string  `json:"sessionId"`
	Project   string  `json:"project"`
	Branch    *string `json:"branch"`
	Date      *string `json:"date,omitempty"`
	Summary   *string `json:"summary"`
}

func header(s *db.ExportedSession) *SessionHeader {
	return &SessionHeader{SessionID: s.SessionID, Project: cli.DisplayProject(s.ProjectPath, s.Project),
		Branch: s.GitBranch, Date: slice(s.StartedAt, 10), Summary: s.Summary}
}

// ExportMessage is a message of recall_export.
type ExportMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Export is the recall_export result.
type Export struct {
	Session  *SessionHeader  `json:"session"`
	Messages []ExportMessage `json:"messages"`
}

// ExportError is recall_export's result for an unknown session.
type ExportError struct {
	Error string `json:"error"`
}

// ExportResult converts an exported session for recall_export.
func ExportResult(id string, s *db.ExportedSession, msgs []db.ExportedMessage) any {
	if s == nil {
		return ExportError{Error: "Session not found: " + id}
	}
	out := Export{Session: header(s), Messages: make([]ExportMessage, len(msgs))}
	for i, m := range msgs {
		out.Messages[i] = ExportMessage{Role: m.Role, Content: m.Content}
	}
	return out
}

// ProjectCount is a project of recall_stats.
type ProjectCount struct {
	Project  string `json:"project"`
	Sessions int64  `json:"sessions"`
	Messages int64  `json:"messages"`
}

// StatsResult is the recall_stats result.
type StatsResult struct {
	TotalSessions int64           `json:"totalSessions"`
	TotalMessages int64           `json:"totalMessages"`
	ByProject     []ProjectCount  `json:"byProject"`
	ByMonth       []db.MonthStats `json:"byMonth"`
}

// Stats converts statistics for recall_stats.
func Stats(s *db.Stats) StatsResult {
	out := StatsResult{TotalSessions: s.TotalSessions, TotalMessages: s.TotalMessages,
		ByProject: make([]ProjectCount, len(s.ByProject)), ByMonth: s.ByMonth}
	for i, p := range s.ByProject {
		pp := p.ProjectPath
		out.ByProject[i] = ProjectCount{Project: cli.DisplayProject(&pp, p.Project), Sessions: p.Sessions, Messages: p.Messages}
	}
	return out
}

// WebProjectCount is a project of /api/stats.
type WebProjectCount struct {
	Project     string `json:"project"`
	ProjectPath string `json:"projectPath"`
	Sessions    int64  `json:"sessions"`
	Messages    int64  `json:"messages"`
}

// WebStats is the /api/stats response.
type WebStats struct {
	TotalSessions int64             `json:"totalSessions"`
	TotalMessages int64             `json:"totalMessages"`
	ByProject     []WebProjectCount `json:"byProject"`
	ByMonth       []db.MonthStats   `json:"byMonth"`
}

// WebStatsResult converts statistics for /api/stats.
func WebStatsResult(s *db.Stats) WebStats {
	out := WebStats{TotalSessions: s.TotalSessions, TotalMessages: s.TotalMessages,
		ByProject: make([]WebProjectCount, len(s.ByProject)), ByMonth: s.ByMonth}
	for i, p := range s.ByProject {
		pp := p.ProjectPath
		path := p.ProjectPath
		if path == "" {
			path = p.Project
		}
		out.ByProject[i] = WebProjectCount{Project: cli.DisplayProject(&pp, p.Project), ProjectPath: path,
			Sessions: p.Sessions, Messages: p.Messages}
	}
	return out
}

// WebSession is an entry of /api/sessions.
type WebSession struct {
	SessionID     string  `json:"sessionId"`
	FullSessionID string  `json:"fullSessionId"`
	Title         *string `json:"title,omitempty"`
	Project       string  `json:"project"`
	Branch        *string `json:"branch"`
	FirstPrompt   string  `json:"firstPrompt"`
	LastPrompt    string  `json:"lastPrompt"`
	Messages      *int64  `json:"messages"`
	Date          *string `json:"date,omitempty"`
	CreatedAt     *string `json:"createdAt"`
	UpdatedAt     *string `json:"updatedAt"`
	Activity      []int   `json:"activity"`
}

// WebSessions builds the /api/sessions response.
func WebSessions(d *db.DB, sessions []db.ListedSession) ([]WebSession, error) {
	ids := make([]string, len(sessions))
	for i, s := range sessions {
		ids[i] = s.SessionID
	}
	activities, err := d.SessionActivities(ids, 20)
	if err != nil {
		return nil, err
	}
	out := make([]WebSession, len(sessions))
	for i, s := range sessions {
		first := ""
		if s.FirstPrompt != nil {
			first = jscompat.Slice(*s.FirstPrompt, 200)
		}
		if first == "" || first[0] == '<' {
			t, err := d.FirstUserText(s.SessionID)
			if err != nil {
				return nil, err
			}
			if t != nil {
				first = *t
			}
		}
		last := ""
		if t, err := d.LastUserText(s.SessionID); err != nil {
			return nil, err
		} else if t != nil {
			last = *t
		}
		updated := s.EndedAt
		if updated == nil || *updated == "" {
			updated = s.StartedAt
		}
		activity := activities[s.SessionID]
		if activity == nil {
			activity = []int{}
		}
		out[i] = WebSession{SessionID: jscompat.Slice(s.SessionID, 8), FullSessionID: s.SessionID, Title: s.Title,
			Project: cli.DisplayProject(s.ProjectPath, s.Project), Branch: s.GitBranch, FirstPrompt: first,
			LastPrompt: last, Messages: s.MessageCount, Date: slice(s.StartedAt, 10), CreatedAt: s.StartedAt,
			UpdatedAt: updated, Activity: activity}
	}
	return out, nil
}

// WebMessage is a message of /api/sessions/:id.
type WebMessage struct {
	UUID      string  `json:"uuid"`
	Role      string  `json:"role"`
	BlockType string  `json:"blockType"`
	Content   string  `json:"content"`
	ToolName  *string `json:"toolName"`
	ToolInput *string `json:"toolInput"`
	Timestamp *string `json:"timestamp"`
}

// WebSessionDetail is the /api/sessions/:id response.
type WebSessionDetail struct {
	Session  *SessionHeader `json:"session"`
	Messages []WebMessage   `json:"messages"`
}

// WebSessionDetailResult converts an exported session for /api/sessions/:id.
func WebSessionDetailResult(s *db.ExportedSession, msgs []db.ExportedMessage) WebSessionDetail {
	if s == nil {
		return WebSessionDetail{Messages: []WebMessage{}}
	}
	out := WebSessionDetail{Session: header(s), Messages: make([]WebMessage, len(msgs))}
	for i, m := range msgs {
		out.Messages[i] = WebMessage{UUID: m.UUID, Role: m.Role, BlockType: m.BlockType, Content: m.Content,
			ToolName: m.ToolName, ToolInput: m.ToolInput, Timestamp: m.Timestamp}
	}
	return out
}
