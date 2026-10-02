// Package parser turns a Claude Code JSONL transcript into the rows the
// archive stores. It mirrors src/parser.ts line for line, including the
// JavaScript string semantics (trim, UTF-16 length) that decide what ends up
// in the database.
package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/babarot/claude-recall/internal/jscompat"
)

// Message is one extracted content block.
type Message struct {
	UUID       string
	Role       string
	BlockType  string // text, thinking, tool_use, tool_result or meta
	BlockIndex int
	Content    string
	ToolName   *string
	ToolInput  *string
	Timestamp  string
	TurnIndex  int
}

// Image is a base64 image block from a message.
type Image struct {
	MessageUUID string
	ImageIndex  int
	MediaType   string
	Data        string // base64, decoded by the importer
}

// Meta is the session row.
type Meta struct {
	SessionID     string
	Project       string
	ProjectPath   string
	GitBranch     string
	FirstPrompt   string
	Summary       *string
	StartedAt     string
	EndedAt       string
	ClaudeVersion string
}

// Session is a parsed transcript.
type Session struct {
	Meta     Meta
	Messages []Message
	Images   []Image
}

// IndexEntry is an entry of the sessions-index.json Claude Code wrote until
// early 2026.
type IndexEntry struct {
	SessionID   string `json:"sessionId"`
	FirstPrompt string `json:"firstPrompt"`
	Summary     string `json:"summary"`
	GitBranch   string `json:"gitBranch"`
	ProjectPath string `json:"projectPath"`
}

type header struct {
	sessionID, projectPath, gitBranch, claudeVersion, startedAt string
}

type journalLine struct {
	Type        string                 `json:"type"`
	UUID        string                 `json:"uuid"`
	SessionID   string                 `json:"sessionId"`
	Timestamp   string                 `json:"timestamp"`
	Cwd         string                 `json:"cwd"`
	Version     string                 `json:"version"`
	GitBranch   string                 `json:"gitBranch"`
	IsSidechain bool                   `json:"isSidechain"`
	IsMeta      bool                   `json:"isMeta"`
	Origin      *struct{ Kind string } `json:"origin"`
	Message     *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type     string          `json:"type"`
	Text     json.RawMessage `json:"text"`
	Thinking json.RawMessage `json:"thinking"`
	Name     *string         `json:"name"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"`
	Source   *struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

// jsString returns raw as a string when it is a JSON string.
func jsString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// extractText joins the text of a message's content, like extractText in
// src/parser.ts.
func extractText(content json.RawMessage) string {
	if s, ok := jsString(content); ok {
		return jscompat.Trim(s)
	}
	var blocks []json.RawMessage
	if len(content) == 0 || content[0] != '[' || json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, raw := range blocks {
		var b block
		if json.Unmarshal(raw, &b) != nil || b.Type != "text" {
			continue
		}
		if t, ok := jsString(b.Text); ok {
			parts = append(parts, t)
		}
	}
	return jscompat.Trim(strings.Join(parts, "\n"))
}

func ptr(s string) *string { return &s }

const toolResultLimit = 10000

type parsedLines struct {
	messages      []Message
	images        []Image
	header        *header
	firstUserText string
	lastTimestamp string
}

func parseLines(content string, startTurn int) parsedLines {
	var out parsedLines
	turn := startTurn

	for _, line := range strings.Split(content, "\n") {
		if jscompat.Trim(line) == "" {
			continue
		}
		var p journalLine
		if json.Unmarshal([]byte(line), &p) != nil {
			continue
		}
		if p.Type != "user" && p.Type != "assistant" {
			continue
		}
		if p.IsSidechain {
			continue
		}
		if out.header == nil && p.SessionID != "" {
			out.header = &header{
				sessionID:     p.SessionID,
				projectPath:   p.Cwd,
				gitBranch:     p.GitBranch,
				claudeVersion: p.Version,
				startedAt:     p.Timestamp,
			}
		}
		if p.UUID == "" || p.Message == nil || !jscompat.Truthy(p.Message.Content) {
			continue
		}
		content, role, ts := p.Message.Content, p.Message.Role, p.Timestamp

		// Slash command and skill expansions, injected context and
		// background task notifications become one folded "meta" message.
		// They do not count as user activity for EndedAt.
		if p.IsMeta || (p.Origin != nil && p.Origin.Kind == "task-notification") {
			if text := extractText(content); text != "" {
				out.messages = append(out.messages, Message{UUID: p.UUID, Role: role, BlockType: "meta",
					Content: text, Timestamp: ts, TurnIndex: turn})
				turn++
			}
			continue
		}

		if p.Timestamp != "" && p.Type == "user" {
			out.lastTimestamp = p.Timestamp
		}

		if s, ok := jsString(content); ok {
			text := jscompat.Trim(s)
			if text == "" {
				continue
			}
			if out.firstUserText == "" && p.Type == "user" {
				out.firstUserText = text
			}
			out.messages = append(out.messages, Message{UUID: p.UUID, Role: role, BlockType: "text",
				Content: text, Timestamp: ts, TurnIndex: turn})
			turn++
			continue
		}

		var blocks []json.RawMessage
		if content[0] != '[' || json.Unmarshal(content, &blocks) != nil {
			continue
		}
		// BlockIndex is the block's position in the line's content array,
		// so (session, uuid, block index) stays a stable key across
		// re-imports even when some blocks are skipped.
		imgIdx := 0
		for i, raw := range blocks {
			var b block
			if json.Unmarshal(raw, &b) != nil {
				continue
			}
			msg := Message{UUID: p.UUID, Role: role, BlockIndex: i, Timestamp: ts}
			switch b.Type {
			case "text":
				s, _ := jsString(b.Text)
				text := jscompat.Trim(s)
				if text == "" {
					continue
				}
				if out.firstUserText == "" && p.Type == "user" {
					out.firstUserText = text
				}
				msg.BlockType, msg.Content = "text", text
			case "thinking":
				s, _ := jsString(b.Thinking)
				text := jscompat.Trim(s)
				if text == "" {
					continue
				}
				msg.BlockType, msg.Content = "thinking", text
			case "tool_use":
				name := "unknown"
				if b.Name != nil {
					name = *b.Name
				}
				input := ""
				if jscompat.Truthy(b.Input) {
					if s, err := jscompat.Stringify(b.Input); err == nil {
						input = s
					}
				}
				msg.BlockType, msg.Content, msg.ToolName, msg.ToolInput = "tool_use", name, ptr(name), ptr(input)
			case "tool_result":
				text := toolResultText(b.Content)
				if jscompat.Len(text) > toolResultLimit {
					text = jscompat.Slice(text, toolResultLimit) + "\n... (truncated)"
				}
				msg.BlockType, msg.Content = "tool_result", text
			case "image":
				if b.Source != nil && b.Source.Type == "base64" {
					out.images = append(out.images, Image{MessageUUID: p.UUID, ImageIndex: imgIdx,
						MediaType: b.Source.MediaType, Data: b.Source.Data})
					imgIdx++
				}
				continue
			default:
				continue
			}
			msg.TurnIndex = turn
			turn++
			out.messages = append(out.messages, msg)
		}
	}
	return out
}

// toolResultText is a tool result's text: the string itself, or its text
// blocks joined by newlines.
func toolResultText(content json.RawMessage) string {
	if s, ok := jsString(content); ok {
		return s
	}
	var blocks []json.RawMessage
	if len(content) == 0 || content[0] != '[' || json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, raw := range blocks {
		var b block
		if json.Unmarshal(raw, &b) != nil || b.Type != "text" {
			continue
		}
		// Array.join writes undefined and null as empty strings.
		s, _ := jsString(b.Text)
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n")
}

// Parse parses a whole transcript. It returns nil when the file has no
// session identity or no messages.
func Parse(content, project string, index *IndexEntry) *Session {
	r := parseLines(content, 0)
	if r.header == nil || len(r.messages) == 0 {
		return nil
	}
	meta := Meta{
		SessionID:     r.header.sessionID,
		Project:       project,
		ProjectPath:   r.header.projectPath,
		GitBranch:     r.header.gitBranch,
		FirstPrompt:   jscompat.Slice(r.firstUserText, 500),
		StartedAt:     r.header.startedAt,
		EndedAt:       r.lastTimestamp,
		ClaudeVersion: r.header.claudeVersion,
	}
	if index != nil {
		if index.FirstPrompt != "" {
			meta.FirstPrompt = index.FirstPrompt
		}
		if index.Summary != "" {
			meta.Summary = ptr(index.Summary)
		}
		if index.GitBranch != "" && meta.GitBranch == "" {
			meta.GitBranch = index.GitBranch
		}
		if index.ProjectPath != "" && meta.ProjectPath == "" {
			meta.ProjectPath = index.ProjectPath
		}
	}
	return &Session{Meta: meta, Messages: r.messages, Images: r.images}
}

// LoadIndex reads <projectDir>/sessions-index.json. A missing or broken file
// yields an empty index.
func LoadIndex(projectDir string) map[string]*IndexEntry {
	out := map[string]*IndexEntry{}
	b, err := os.ReadFile(filepath.Join(projectDir, "sessions-index.json"))
	if err != nil {
		return out
	}
	var idx struct {
		Entries []*IndexEntry `json:"entries"`
	}
	if json.Unmarshal(b, &idx) != nil {
		return out
	}
	for _, e := range idx.Entries {
		out[e.SessionID] = e
	}
	return out
}

// File is a transcript found on disk.
type File struct {
	Project   string // the encoded project directory name
	SessionID string
	Path      string
}

// Discover lists every <projectsDir>/<project>/<session>.jsonl.
func Discover(projectsDir string) []File {
	var out []File
	projects, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		dir := filepath.Join(projectsDir, p.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.Type().IsRegular() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			out = append(out, File{
				Project:   p.Name(),
				SessionID: strings.Replace(f.Name(), ".jsonl", "", 1),
				Path:      filepath.Join(dir, f.Name()),
			})
		}
	}
	return out
}
