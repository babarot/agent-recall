package mcp

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/fixture"
	"github.com/babarot/claude-recall/internal/importer"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/mcp -update to write it)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from %s:\n%s", name, path, got)
	}
}

func openDB(t *testing.T, path string) *db.DB {
	t.Helper()
	d, err := db.Open(path, db.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// connect runs the server on d and returns a client session talking to it.
func connect(t *testing.T, d *db.DB) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := NewServer(d).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	r, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func text(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// The tool names and inputs are an interface other tools depend on.
func TestTools(t *testing.T) {
	cs := connect(t, openDB(t, fixture.Archive(t)))
	r, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(r.Tools, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "tools", string(b)+"\n")
}

// So are the results: each tool's text, as an agent reads it.
func TestResults(t *testing.T) {
	cs := connect(t, openDB(t, fixture.Archive(t)))
	for _, tc := range []struct {
		name, tool string
		args       map[string]any
	}{
		{"search", "recall_search", map[string]any{"query": "terraform"}},
		{"search-filters", "recall_search", map[string]any{"query": "the", "project": "api", "from": "2026-04-01", "to": "2026-04-30", "limit": 1}},
		{"list", "recall_list", map[string]any{}},
		{"list-project", "recall_list", map[string]any{"project": "app"}},
		{"export", "recall_export", map[string]any{"session_id": fixture.APISession[:4]}},
		{"export-missing", "recall_export", map[string]any{"session_id": "ffff"}},
		{"stats", "recall_stats", map[string]any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := call(t, cs, tc.tool, tc.args)
			if r.IsError {
				t.Fatalf("error result: %s", text(r))
			}
			golden(t, tc.name, text(r)+"\n")
		})
	}
}

// A failing call is an error result the agent can read, not a broken
// connection.
func TestErrorResult(t *testing.T) {
	path := fixture.Archive(t)
	d := openDB(t, path)
	cs := connect(t, d)
	d.Close() // every query now fails
	r := call(t, cs, "recall_stats", nil)
	if !r.IsError || !strings.HasPrefix(text(r), "Error: ") {
		t.Errorf("got %v %q", r.IsError, text(r))
	}
	if _, err := (Handlers{}).Call("recall_nope", nil); err == nil || err.Error() != "Unknown tool: recall_nope" {
		t.Errorf("unknown tool: %v", err)
	}
}

// limit defaults to 10 for search and 20 for list, and takes a number or a
// numeric string; anything else falls back to the default.
func TestLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(t.TempDir(), "vault.db")
	w, err := db.Open(path, db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 25 {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
		line, _ := json.Marshal(map[string]any{"type": "user", "uuid": fmt.Sprint("u", i), "sessionId": id,
			"timestamp": fmt.Sprintf("2026-01-%02dT00:00:00.000Z", i+1), "cwd": "/work/many",
			"message": map[string]any{"role": "user", "content": "deploy the widget"}})
		f := filepath.Join(dir, "-work-many", id+".jsonl")
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, append(line, '\n'), 0o644)
	}
	if err := importer.Run(w, importer.Options{ProjectsDir: dir}, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	w.Close()

	h := Handlers{DB: openDB(t, path)}
	count := func(tool string, a args) int {
		t.Helper()
		r, err := h.Call(tool, a)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(r)
		var v []any
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		return len(v)
	}
	for _, tc := range []struct {
		tool string
		a    args
		want int
	}{
		{"recall_search", args{"query": "deploy"}, 10},
		{"recall_search", args{"query": "deploy", "limit": 3.0}, 3},
		{"recall_search", args{"query": "deploy", "limit": "4"}, 4},
		{"recall_search", args{"query": "deploy", "limit": true}, 10},
		{"recall_list", args{}, 20},
		{"recall_list", args{"limit": 2.9}, 2},
		{"recall_list", args{"limit": "lots"}, 20},
	} {
		if got := count(tc.tool, tc.a); got != tc.want {
			t.Errorf("%s %v: got %d, want %d", tc.tool, tc.a, got, tc.want)
		}
	}
}
