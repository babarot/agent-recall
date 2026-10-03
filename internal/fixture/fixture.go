// Package fixture writes a small set of Claude Code transcripts for tests:
// two projects with one session each, at fixed times, so what the commands
// print from them is the same on every run.
package fixture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/importer"
)

// The sessions Projects writes.
const (
	AppSession = "11111111-aaaa-4bbb-8ccc-000000000001" // /work/app on main
	APISession = "22222222-aaaa-4bbb-8ccc-000000000002" // /work/api on feature/login
)

type line map[string]any

func user(session, cwd, branch, uuid, ts, text string) line {
	return line{"type": "user", "uuid": uuid, "sessionId": session, "timestamp": ts, "cwd": cwd,
		"version": "2.1.87", "gitBranch": branch, "message": line{"role": "user", "content": text}}
}

func assistant(session, uuid, ts string, blocks ...line) line {
	content := make([]any, len(blocks))
	for i, b := range blocks {
		content[i] = b
	}
	return line{"type": "assistant", "uuid": uuid, "sessionId": session, "timestamp": ts,
		"message": line{"role": "assistant", "content": content}}
}

func text(s string) line { return line{"type": "text", "text": s} }

// Projects writes the transcripts under a new projects directory, laid out
// as ~/.claude/projects is, and returns that directory.
func Projects(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "projects")
	write(t, dir, "-work-app", AppSession,
		user(AppSession, "/work/app", "main", "a1", "2026-03-01T10:00:00.000Z", "How do I configure the terraform module for staging?"),
		assistant(AppSession, "a2", "2026-03-01T10:00:05.000Z",
			text("Put the staging values in a terraform.tfvars file and pass the module a variables block.")),
		user(AppSession, "/work/app", "main", "a3", "2026-03-01T10:01:00.000Z", "And for production?"),
		assistant(AppSession, "a4", "2026-03-01T10:01:05.000Z",
			text("Use a separate workspace so the terraform state of production stays apart.")),
	)
	write(t, dir, "-work-api", APISession,
		user(APISession, "/work/api", "feature/login", "b1", "2026-04-02T09:00:00.000Z", "Fix the login bug where the session cookie expires early"),
		assistant(APISession, "b2", "2026-04-02T09:00:10.000Z",
			line{"type": "tool_use", "id": "t1", "name": "Bash", "input": line{"command": "go test ./auth/..."}},
			text("The cookie max age was set in seconds where milliseconds were expected; fixed in auth/cookie.go.")),
	)
	return dir
}

func write(t testing.TB, dir, project, session string, lines ...line) {
	t.Helper()
	var b strings.Builder
	for _, l := range lines {
		j, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	p := filepath.Join(dir, project)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, session+".jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Archive imports the transcripts Projects writes into a new archive and
// returns its path.
func Archive(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.db")
	d, err := db.Open(path, db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := importer.Run(d, importer.Options{ProjectsDir: Projects(t)}, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	return path
}
