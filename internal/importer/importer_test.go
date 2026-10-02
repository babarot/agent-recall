package importer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/babarot/claude-recall/internal/db"
)

// These cases port src/import_test.ts.

func userLine(text, uuid, ts string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "user", "uuid": uuid, "sessionId": "sess-001", "timestamp": ts, "cwd": "/home/user/project",
		"version": "2.1.87", "gitBranch": "main", "message": map[string]any{"role": "user", "content": text},
	})
	return string(b)
}

func assistantLine(text, uuid, ts string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "assistant", "uuid": uuid, "sessionId": "sess-001", "timestamp": ts,
		"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}},
	})
	return string(b)
}

type env struct {
	t    *testing.T
	path string
	db   *db.DB
}

func newEnv(t *testing.T, lines ...string) *env {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "my-project")
	os.MkdirAll(dir, 0o755)
	d, err := db.Open(filepath.Join(t.TempDir(), "vault.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	e := &env{t: t, path: filepath.Join(dir, "sess-001.jsonl"), db: d}
	e.write(lines...)
	return e
}

// write replaces the file and moves its mtime forward, so change detection
// sees the edit even within one filesystem timestamp tick.
func (e *env) write(lines ...string) {
	e.t.Helper()
	if err := os.WriteFile(e.path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		e.t.Fatal(err)
	}
	if fi, err := os.Stat(e.path); err == nil {
		next := fi.ModTime().Add(time.Second)
		os.Chtimes(e.path, next, next)
	}
}

func (e *env) importFile() *Result {
	e.t.Helper()
	r, err := ImportFile(e.db, e.path, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func (e *env) messageCount() int {
	e.t.Helper()
	fi, err := e.db.GetFileInfo("sess-001")
	if err != nil || fi == nil {
		e.t.Fatalf("file info %v %v", fi, err)
	}
	return fi.MessageCount
}

func TestImportNewSession(t *testing.T) {
	e := newEnv(t, userLine("hello", "u1", "2026-01-01T00:00:00Z"), assistantLine("hi", "a1", "2026-01-01T00:00:01Z"))
	r := e.importFile()
	if r.Status != New || r.TotalMessages != 2 || r.SessionID != "sess-001" || r.Project != "my-project" {
		t.Fatalf("%+v", r)
	}
}

func TestImportUnchangedFile(t *testing.T) {
	e := newEnv(t, userLine("hello", "u1", "2026-01-01T00:00:00Z"))
	e.importFile()
	if r := e.importFile(); r.Status != Unchanged || r.TotalMessages != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestImportAppendedMessages(t *testing.T) {
	e := newEnv(t, userLine("hello", "u1", "2026-01-01T00:00:00Z"))
	e.importFile()
	e.write(userLine("hello", "u1", "2026-01-01T00:00:00Z"), assistantLine("hi", "a1", "2026-01-01T00:00:01Z"))
	if r := e.importFile(); r.Status != Resynced || e.messageCount() != 2 {
		t.Fatalf("%+v", r)
	}
}

func TestImportMirrorsShrunkFile(t *testing.T) {
	e := newEnv(t, userLine("a", "u1", "2026-01-01T00:00:00Z"), userLine("b", "u2", "2026-01-01T00:00:01Z"))
	e.importFile()
	e.write(userLine("c", "u3", "2026-01-01T00:00:02Z"))
	e.importFile()
	if n := e.messageCount(); n != 1 {
		t.Fatalf("message count %d, want 1", n)
	}
	r, _ := e.db.Search("a", db.SearchOptions{})
	if len(r) != 0 {
		t.Fatalf("rows of the removed uuid are still searchable: %+v", r)
	}
}

func TestImportMissingFile(t *testing.T) {
	d, _ := db.Open(filepath.Join(t.TempDir(), "vault.db"), db.Options{})
	defer d.Close()
	r, err := ImportFile(d, filepath.Join(t.TempDir(), "p", "nope.jsonl"), nil)
	if r != nil || err != nil {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestRunSummary(t *testing.T) {
	e := newEnv(t, userLine("hello", "u1", "2026-01-01T00:00:00Z"))
	projects := filepath.Dir(filepath.Dir(e.path))
	os.WriteFile(filepath.Join(filepath.Dir(e.path), "broken.jsonl"), []byte("nope\n"), 0o644)
	open := func() (*db.DB, error) { return e.db, nil }

	var out bytes.Buffer
	if err := Run(open, Options{ProjectsDir: projects}, &out); err != nil {
		t.Fatal(err)
	}
	want := "Syncing 2 sessions...\nImported 1 sessions (1 messages). Skipped 1 unreadable files.\n"
	if out.String() != want {
		t.Fatalf("got %q", out.String())
	}
}

func TestAtob(t *testing.T) {
	for in, want := range map[string]string{"aGVsbG8=": "hello", "aGVsbG8": "hello", "aGVs\nbG8=": "hello", "": ""} {
		got, err := atob(in)
		if err != nil || string(got) != want {
			t.Errorf("atob(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := atob("a"); err == nil {
		t.Error("atob(\"a\") should fail")
	}
}
