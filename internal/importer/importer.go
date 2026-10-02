// Package importer copies Claude Code transcripts into the archive. Each
// import mirrors the file's current content: a changed file replaces every
// stored row of its session, so appends, /compact rewrites and interrupted
// earlier imports all resolve the same way.
package importer

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/parser"
)

// Status says what an import did.
type Status string

const (
	New       Status = "new"
	Resynced  Status = "resynced"
	Unchanged Status = "unchanged"
)

// Result is the outcome of importing one file.
type Result struct {
	Status        Status
	SessionID     string
	Project       string
	TotalMessages int
}

// ImportFile imports <projects>/<project>/<session>.jsonl. It returns nil,
// nil when the file cannot be read or holds no session, so callers can try
// again later.
func ImportFile(d *db.DB, path string, index *parser.IndexEntry) (*Result, error) {
	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	project := filepath.Base(filepath.Dir(path))
	if sessionID == "" || project == "" {
		return nil, nil
	}

	fi, err := os.Stat(path)
	if err != nil {
		return nil, nil
	}
	// JavaScript Dates hold whole milliseconds.
	mtime := float64(fi.ModTime().UnixMilli())
	size := fi.Size()

	existing, err := d.GetFileInfo(sessionID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.FileMtime != nil && *existing.FileMtime == mtime &&
		existing.FileSize != nil && *existing.FileSize == size {
		return &Result{Status: Unchanged, SessionID: sessionID, Project: project, TotalMessages: existing.MessageCount}, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	parsed := parser.Parse(decodeText(raw), project, index)
	if parsed == nil {
		return nil, nil
	}

	row := db.SessionRow{
		SessionID:     sessionID,
		Project:       parsed.Meta.Project,
		ProjectPath:   parsed.Meta.ProjectPath,
		GitBranch:     parsed.Meta.GitBranch,
		FirstPrompt:   parsed.Meta.FirstPrompt,
		Summary:       parsed.Meta.Summary,
		MessageCount:  len(parsed.Messages),
		StartedAt:     parsed.Meta.StartedAt,
		EndedAt:       parsed.Meta.EndedAt,
		ClaudeVersion: parsed.Meta.ClaudeVersion,
		FileMtime:     &mtime,
		FileSize:      &size,
	}
	msgs := make([]db.MessageRow, len(parsed.Messages))
	for i, m := range parsed.Messages {
		msgs[i] = db.MessageRow{UUID: m.UUID, Role: m.Role, BlockType: m.BlockType, BlockIndex: m.BlockIndex,
			Content: m.Content, ToolName: m.ToolName, ToolInput: m.ToolInput, Timestamp: m.Timestamp, TurnIndex: m.TurnIndex}
	}
	var imgs []db.ImageRow
	for _, img := range parsed.Images {
		data, err := atob(img.Data)
		if err != nil {
			continue // the TypeScript importer threw here and stopped the whole run
		}
		imgs = append(imgs, db.ImageRow{MessageUUID: img.MessageUUID, ImageIndex: img.ImageIndex, MediaType: img.MediaType, Data: data})
	}

	if err := d.ReplaceSession(row, msgs, imgs); err != nil {
		return nil, err
	}
	status := New
	if existing != nil {
		status = Resynced
	}
	return &Result{Status: status, SessionID: sessionID, Project: project, TotalMessages: len(msgs)}, nil
}

// decodeText reads bytes the way Deno.readTextFileSync does: a leading BOM
// is dropped and invalid UTF-8 becomes U+FFFD.
func decodeText(b []byte) string {
	b = bytes.TrimPrefix(b, []byte("\xEF\xBB\xBF"))
	if utf8.Valid(b) {
		return string(b)
	}
	return strings.ToValidUTF8(string(b), "�")
}

// atob decodes base64 like the browser's atob: ASCII whitespace is ignored
// and padding is optional.
func atob(s string) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\f', '\r':
			return -1
		}
		return r
	}, s)
	if len(s)%4 == 0 {
		s = strings.TrimSuffix(strings.TrimSuffix(s, "="), "=")
	}
	if len(s)%4 == 1 || strings.Contains(s, "=") {
		return nil, fmt.Errorf("invalid base64")
	}
	out, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []byte{}
	}
	return out, nil
}

// Options selects what Run imports.
type Options struct {
	ProjectsDir string
	Session     string // a session ID or its prefix
	Project     string // case-insensitive substring of the project dir name
	DryRun      bool
}

// Run imports every transcript that matches opts and prints the same
// summary as `agent-recall import`.
func Run(d func() (*db.DB, error), opts Options, w io.Writer) error {
	all := parser.Discover(opts.ProjectsDir)
	targets := all
	switch {
	case opts.Session != "":
		targets = nil
		for _, f := range all {
			if f.SessionID == opts.Session || strings.HasPrefix(f.SessionID, opts.Session) {
				targets = append(targets, f)
			}
		}
	case opts.Project != "":
		targets = nil
		p := strings.ToLower(opts.Project)
		for _, f := range all {
			if strings.Contains(strings.ToLower(f.Project), p) {
				targets = append(targets, f)
			}
		}
	}

	if len(targets) == 0 {
		fmt.Fprintln(w, "No sessions found to import.")
		return nil
	}
	if opts.DryRun {
		fmt.Fprintf(w, "Would import %d session files:\n", len(targets))
		for _, t := range targets[:min(20, len(targets))] {
			fmt.Fprintf(w, "  %s (%s)\n", t.SessionID, t.Project)
		}
		if len(targets) > 20 {
			fmt.Fprintf(w, "  ... and %d more\n", len(targets)-20)
		}
		return nil
	}

	fmt.Fprintf(w, "Syncing %d sessions...\n", len(targets))
	vault, err := d()
	if err != nil {
		return err
	}
	defer vault.Close()

	var order []string
	byProject := map[string][]parser.File{}
	for _, t := range targets {
		if _, ok := byProject[t.Project]; !ok {
			order = append(order, t.Project)
		}
		byProject[t.Project] = append(byProject[t.Project], t)
	}

	var imported, messages, unchanged, skipped int
	for _, project := range order {
		index := parser.LoadIndex(filepath.Join(opts.ProjectsDir, project))
		for _, f := range byProject[project] {
			r, err := ImportFile(vault, f.Path, index[f.SessionID])
			if err != nil {
				return err
			}
			switch {
			case r == nil:
				skipped++
			case r.Status == Unchanged:
				unchanged++
			default:
				imported++
				messages += r.TotalMessages
			}
		}
	}

	parts := []string{fmt.Sprintf("Imported %d sessions (%d messages).", imported, messages)}
	if unchanged > 0 {
		parts = append(parts, fmt.Sprintf("%d unchanged.", unchanged))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("Skipped %d unreadable files.", skipped))
	}
	fmt.Fprintln(w, strings.Join(parts, " "))
	return nil
}
