// Package cli renders the output of the search, list, export and stats
// commands.
package cli

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/jscompat"
)

// color wraps s like @std/fmt/colors: nested closing codes are reopened, and
// NO_COLOR (non-empty) turns colors off.
func color(s, open, close string) string {
	if os.Getenv("NO_COLOR") != "" {
		return s
	}
	return open + strings.ReplaceAll(s, close, open) + close
}

func bold(s string) string   { return color(s, "\x1b[1m", "\x1b[22m") }
func dim(s string) string    { return color(s, "\x1b[2m", "\x1b[22m") }
func cyan(s string) string   { return color(s, "\x1b[36m", "\x1b[39m") }
func yellow(s string) string { return color(s, "\x1b[33m", "\x1b[39m") }

// DisplayProject formats a project for display: the project path with $HOME
// shortened to ~, or the encoded dir name turned back into a path.
func DisplayProject(projectPath *string, project string) string {
	path := ""
	if projectPath != nil {
		path = *projectPath
	}
	if path == "" {
		// project.replace(/^-/, "/").replaceAll("-", "/")
		path = strings.ReplaceAll(project, "-", "/")
	}
	if home := os.Getenv("HOME"); home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// WriteJSON prints v like console.log(JSON.stringify(v, null, 2)).
func WriteJSON(w io.Writer, v any) error {
	b, err := jscompat.Marshal(v, "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

// Search prints search results grouped by session.
func Search(w io.Writer, results []db.SearchResult) {
	var order []string
	bySession := map[string][]db.SearchResult{}
	for _, r := range results {
		if _, ok := bySession[r.SessionID]; !ok {
			order = append(order, r.SessionID)
		}
		bySession[r.SessionID] = append(bySession[r.SessionID], r)
	}
	for _, id := range order {
		msgs := bySession[id]
		first := msgs[0]
		date := "unknown"
		if first.StartedAt != nil {
			date = jscompat.Slice(*first.StartedAt, 10)
		}
		fmt.Fprintf(w, "\n%s %s %s %s\n", dim("["+date+"]"), bold(DisplayProject(first.ProjectPath, first.Project)),
			dim("("+str(first.GitBranch)+")"), dim("session:"+jscompat.Slice(id, 8)))
		for _, m := range msgs {
			label := yellow("assistant")
			if m.Role == "user" {
				label = cyan("user")
			}
			snippet := m.Content
			if jscompat.Len(snippet) > 200 {
				snippet = jscompat.Slice(snippet, 200) + "..."
			}
			fmt.Fprintf(w, "  %s: %s\n", label, snippet)
		}
	}
	fmt.Fprintf(w, "\n%s\n", dim(fmt.Sprintf("Found %d results across %d sessions.", len(results), len(order))))
}

// List prints the session table.
func List(w io.Writer, sessions []db.ListedSession) {
	fmt.Fprintf(w, "%s %s %s %s  %s\n", bold(jscompat.PadEnd("Session", 10)), bold(jscompat.PadEnd("Project", 30)),
		bold(jscompat.PadEnd("Branch", 30)), bold(jscompat.PadStart("Msgs", 5)), bold("Date"))
	fmt.Fprintf(w, "%s\n", dim(strings.Repeat("-", 95)))
	for _, s := range sessions {
		project := DisplayProject(s.ProjectPath, s.Project)
		if jscompat.Len(project) > 28 {
			project = "..." + jscompat.SliceFrom(project, -25)
		}
		branch := str(s.GitBranch)
		if jscompat.Len(branch) > 28 {
			branch = jscompat.Slice(branch, 28) + "..."
		}
		date := ""
		if s.StartedAt != nil {
			date = jscompat.Slice(*s.StartedAt, 10)
		}
		prompt := ""
		if fp := cmp.Or(str(s.Title), str(s.FirstPrompt)); fp != "" {
			more := ""
			if jscompat.Len(fp) > 60 {
				more = "..."
			}
			prompt = dim(" " + jscompat.Slice(fp, 60) + more)
		}
		msgs := "null"
		if s.MessageCount != nil {
			msgs = fmt.Sprint(*s.MessageCount)
		}
		fmt.Fprintf(w, "%s %s %s %s  %s%s\n", jscompat.PadEnd(jscompat.Slice(s.SessionID, 8), 10), jscompat.PadEnd(project, 30),
			jscompat.PadEnd(branch, 30), jscompat.PadStart(msgs, 5), date, prompt)
	}
	fmt.Fprintf(w, "%s\n", dim(fmt.Sprintf("\n%d sessions listed.", len(sessions))))
}

func formatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return jscompat.ToFixed(float64(n)/1024, 1) + " KB"
	default:
		return jscompat.ToFixed(float64(n)/(1024*1024), 1) + " MB"
	}
}

// Stats prints archive statistics.
func Stats(w io.Writer, s *db.Stats) {
	fmt.Fprintf(w, "%s\n", bold("Archive Statistics"))
	fmt.Fprintf(w, "  Total sessions:  %d\n", s.TotalSessions)
	fmt.Fprintf(w, "  Total messages:  %d\n", s.TotalMessages)
	fmt.Fprintf(w, "  Database size:   %s\n", formatBytes(s.DBSizeBytes))
	if len(s.ByProject) > 0 {
		fmt.Fprintf(w, "\n%s\n", bold("By Project:"))
		for _, p := range s.ByProject {
			pp := p.ProjectPath
			project := DisplayProject(&pp, p.Project)
			if jscompat.Len(project) > 40 {
				project = "..." + jscompat.SliceFrom(project, -37)
			}
			fmt.Fprintf(w, "  %s %s sessions  %s messages\n", jscompat.PadEnd(project, 42),
				jscompat.PadStart(fmt.Sprint(p.Sessions), 4), jscompat.PadStart(fmt.Sprint(p.Messages), 6))
		}
	}
	if len(s.ByMonth) > 0 {
		fmt.Fprintf(w, "\n%s\n", bold("By Month:"))
		for _, m := range s.ByMonth {
			month := "null"
			if m.Month != nil {
				month = *m.Month
			}
			fmt.Fprintf(w, "  %s  %s sessions  %s messages\n", dim(month),
				jscompat.PadStart(fmt.Sprint(m.Sessions), 4), jscompat.PadStart(fmt.Sprint(m.Messages), 6))
		}
	}
}

// ExportJSON is the shape of `export --format json`.
type ExportJSON struct {
	Session  *db.ExportedSession  `json:"session"`
	Messages []db.ExportedMessage `json:"messages"`
}

// Markdown renders an exported session as Markdown.
func Markdown(s *db.ExportedSession, msgs []db.ExportedMessage) string {
	var b strings.Builder
	line := func(l string) { b.WriteString(l); b.WriteByte('\n') }
	line("# Session: " + jscompat.Slice(s.SessionID, 8))
	line("")
	project := str(s.ProjectPath)
	if project == "" {
		project = s.Project
	}
	line("- **Project**: " + project)
	line("- **Branch**: " + jsString(s.GitBranch))
	date := ""
	if s.StartedAt != nil {
		date = jscompat.Slice(*s.StartedAt, 10)
	}
	line("- **Date**: " + date)
	if str(s.Summary) != "" {
		line("- **Summary**: " + *s.Summary)
	}
	line("")
	line("---")
	line("")
	for _, m := range msgs {
		role := "Assistant"
		if m.Role == "user" {
			role = "User"
		}
		line("**" + role + "**:")
		line("")
		line(m.Content)
		line("")
		line("---")
		line("")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// Text renders an exported session as plain text.
func Text(s *db.ExportedSession, msgs []db.ExportedMessage) string {
	var b strings.Builder
	line := func(l string) { b.WriteString(l); b.WriteByte('\n') }
	date := "undefined"
	if s.StartedAt != nil {
		date = jscompat.Slice(*s.StartedAt, 10)
	}
	line(fmt.Sprintf("Session: %s | %s (%s) | %s", jscompat.Slice(s.SessionID, 8), s.Project, jsString(s.GitBranch), date))
	line(strings.Repeat("=", 80))
	line("")
	for _, m := range msgs {
		role := "ASSISTANT"
		if m.Role == "user" {
			role = "USER"
		}
		line("[" + role + "]")
		line(m.Content)
		line("")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// jsString is how a template literal prints a nullable string: null as
// "null".
func jsString(p *string) string {
	if p == nil {
		return "null"
	}
	return *p
}
