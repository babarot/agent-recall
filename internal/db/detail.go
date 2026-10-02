package db

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
)

// Detail is what the TUI's detail pane shows about one session beyond the
// sessions row.
type Detail struct {
	// Counts of text blocks by speaker, and of tool calls and thinking.
	You, Claude, Tools, Thinking int
	// TopTools are the most used tools, most used first.
	TopTools []Count
	// Files are the files edited (Edit, Write, MultiEdit, NotebookEdit),
	// most edited first; FileCount is how many distinct files there were.
	Files     []Count
	FileCount int
	// Commands are the last Bash commands, newest first.
	Commands []string
	// Activity is the number of messages in each of 24 equal slices of the
	// session, from its first message to its last.
	Activity []int
	Images   int
	Version  string
	// First is the first real user message; Tail are the last messages of
	// the conversation, oldest first; Hidden counts the ones between.
	First  *Message
	Tail   []Message
	Hidden int
}

// Count is a name and how often it occurred.
type Count struct {
	Name string
	N    int
}

const (
	detailTools    = 6
	detailFiles    = 10
	detailCommands = 5
	detailTail     = 16
	detailBuckets  = 24
)

// SessionDetail gathers the detail pane's data for a session.
func (d *DB) SessionDetail(sessionID string) (*Detail, error) {
	var out Detail
	fail := func(err error) (*Detail, error) { return nil, fmt.Errorf("detail: %w", err) }

	rows, err := d.sql.Query(`SELECT role, block_type, COUNT(*) FROM messages WHERE session_id = ? GROUP BY role, block_type`, sessionID)
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		var role, kind string
		var n int
		if err := rows.Scan(&role, &kind, &n); err != nil {
			rows.Close()
			return fail(err)
		}
		switch {
		case role == "user" && kind == "text":
			out.You = n
		case role == "assistant" && kind == "text":
			out.Claude = n
		case kind == "tool_use":
			out.Tools += n
		case kind == "thinking":
			out.Thinking += n
		}
	}
	rows.Close()

	rows, err = d.sql.Query(`SELECT COALESCE(tool_name, ''), COUNT(*) AS n FROM messages
        WHERE session_id = ? AND block_type = 'tool_use' GROUP BY tool_name ORDER BY n DESC, tool_name LIMIT ?`, sessionID, detailTools)
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		var c Count
		if err := rows.Scan(&c.Name, &c.N); err != nil {
			rows.Close()
			return fail(err)
		}
		out.TopTools = append(out.TopTools, c)
	}
	rows.Close()

	// Edited files and Bash commands live in the tool input JSON.
	rows, err = d.sql.Query(`SELECT tool_name, COALESCE(tool_input, '') FROM messages
        WHERE session_id = ? AND block_type = 'tool_use'
          AND tool_name IN ('Edit', 'Write', 'MultiEdit', 'NotebookEdit', 'Bash')
        ORDER BY turn_index DESC`, sessionID)
	if err != nil {
		return fail(err)
	}
	files := map[string]int{}
	for rows.Next() {
		var tool, input string
		if err := rows.Scan(&tool, &input); err != nil {
			rows.Close()
			return fail(err)
		}
		var in struct {
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
			Command      string `json:"command"`
		}
		if json.Unmarshal([]byte(input), &in) != nil {
			continue
		}
		if tool == "Bash" {
			if in.Command != "" && len(out.Commands) < detailCommands {
				out.Commands = append(out.Commands, in.Command)
			}
			continue
		}
		if p := cmp.Or(in.FilePath, in.NotebookPath); p != "" {
			files[p]++
		}
	}
	rows.Close()
	for name, n := range files {
		out.Files = append(out.Files, Count{name, n})
	}
	slices.SortFunc(out.Files, func(a, b Count) int { return cmp.Or(cmp.Compare(b.N, a.N), cmp.Compare(a.Name, b.Name)) })
	out.FileCount = len(out.Files)
	out.Files = out.Files[:min(len(out.Files), detailFiles)]

	act, err := d.SessionActivities([]string{sessionID}, detailBuckets)
	if err != nil {
		return fail(err)
	}
	out.Activity = act[sessionID]

	if err := d.sql.QueryRow(`SELECT COUNT(*) FROM images WHERE session_id = ?`, sessionID).Scan(&out.Images); err != nil {
		return fail(err)
	}
	if err := d.sql.QueryRow(`SELECT COALESCE(claude_version, '') FROM sessions WHERE session_id = ?`, sessionID).Scan(&out.Version); err != nil {
		return fail(err)
	}

	p, err := d.SessionPreview(sessionID, 1, detailTail)
	if err != nil {
		return fail(err)
	}
	all := append(p.Head, p.Tail...)
	if len(all) > 0 {
		out.First, out.Tail, out.Hidden = &all[0], all[1:], p.Skipped
	}
	return &out, nil
}
