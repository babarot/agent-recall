// Package mcp is the MCP server: the recall_search, recall_list,
// recall_export and recall_stats tools over stdio, on the official Go SDK.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/babarot/claude-recall/internal/api"
	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/jscompat"
	"github.com/babarot/claude-recall/internal/version"
)

type prop struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type schema struct {
	Type       string          `json:"type"`
	Properties map[string]prop `json:"properties"`
	Required   []string        `json:"required,omitempty"`
}

var (
	projectProp = prop{"string", "Filter by project name (partial match)"}

	searchTool = &mcp.Tool{
		Name:        "recall_search",
		Description: "Search past coding agent session conversations by full-text query. Use this when you need to find previous discussions, decisions, or context from past sessions.",
		InputSchema: schema{Type: "object", Required: []string{"query"}, Properties: map[string]prop{
			"query":   {"string", `Full-text search query. Supports FTS5 syntax: "exact phrase", term1 AND term2, term1 OR term2, term1 NOT term2`},
			"project": projectProp,
			"limit":   {"number", "Max results (default: 10)"},
			"from":    {"string", "Start date filter (YYYY-MM-DD)"},
			"to":      {"string", "End date filter (YYYY-MM-DD)"},
		}},
	}
	listTool = &mcp.Tool{
		Name:        "recall_list",
		Description: "List archived coding agent sessions. Use this to see what sessions are available before exporting a specific one.",
		InputSchema: schema{Type: "object", Properties: map[string]prop{
			"project": projectProp,
			"limit":   {"number", "Max sessions to return (default: 20)"},
		}},
	}
	exportTool = &mcp.Tool{
		Name:        "recall_export",
		Description: "Export the full conversation of a specific session. Use this to get detailed context from a past session found via recall_search or recall_list.",
		InputSchema: schema{Type: "object", Required: []string{"session_id"}, Properties: map[string]prop{
			"session_id": {"string", "Session ID (full UUID or prefix). Get this from recall_search or recall_list results."},
		}},
	}
	statsTool = &mcp.Tool{
		Name:        "recall_stats",
		Description: "Show archive statistics: total sessions, messages, breakdown by project and month.",
		InputSchema: schema{Type: "object", Properties: map[string]prop{"project": projectProp}},
	}
)

// args is the loosely typed arguments object. A missing or mistyped value
// falls back to its default instead of failing the call.
type args map[string]any

func (a args) str(key string) string {
	s, _ := a[key].(string)
	return s
}

func (a args) num(key string, def int) *int {
	v, ok := a[key]
	if !ok || v == nil {
		return &def
	}
	switch t := v.(type) {
	case float64:
		n := int(t)
		return &n
	case string: // SQLite reads a numeric string in LIMIT as the number
		if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			n := int(f)
			return &n
		}
	}
	return &def
}

// Handlers holds the tool implementations, separate from the transport so
// they can be tested directly.
type Handlers struct{ DB *db.DB }

// Call runs one tool and returns its result value.
func (h Handlers) Call(name string, a args) (any, error) {
	switch name {
	case "recall_search":
		r, err := h.DB.Search(a.str("query"), db.SearchOptions{Project: a.str("project"), Limit: a.num("limit", 10),
			From: a.str("from"), To: a.str("to")})
		if err != nil {
			return nil, err
		}
		return api.SearchHits(r, true), nil
	case "recall_list":
		s, err := h.DB.ListSessions(db.ListOptions{Project: a.str("project"), Limit: a.num("limit", 20)})
		if err != nil {
			return nil, err
		}
		return api.List(s), nil
	case "recall_export":
		id := a.str("session_id")
		s, msgs, err := h.DB.ExportSession(id)
		if err != nil {
			return nil, err
		}
		return api.ExportResult(id, s, msgs), nil
	case "recall_stats":
		s, err := h.DB.Stats(a.str("project"))
		if err != nil {
			return nil, err
		}
		return api.Stats(s), nil
	}
	return nil, fmt.Errorf("Unknown tool: %s", name)
}

func (h Handlers) handler(name string) mcp.ToolHandler {
	return func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		a := args{}
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &a); err != nil {
				a = args{}
			}
		}
		result, err := h.Call(name, a)
		if err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "Error: " + err.Error()}}}, nil
		}
		text, err := jscompat.Marshal(result, "  ")
		if err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}, nil
	}
}

// NewServer returns the MCP server with the four tools.
func NewServer(d *db.DB) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "claude-recall", Version: version.Version}, nil)
	h := Handlers{DB: d}
	for _, t := range []*mcp.Tool{searchTool, listTool, exportTool, statsTool} {
		s.AddTool(t, h.handler(t.Name))
	}
	return s
}

// Run serves MCP over stdin and stdout until the client disconnects.
func Run(ctx context.Context, d *db.DB) error {
	return NewServer(d).Run(ctx, &mcp.StdioTransport{})
}
