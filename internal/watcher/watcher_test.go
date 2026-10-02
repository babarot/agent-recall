package watcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/importer"
)

func userLine(sessionID, uuid, text string) string {
	b, _ := json.Marshal(map[string]any{"type": "user", "uuid": uuid, "sessionId": sessionID,
		"timestamp": "2026-01-01T00:00:00Z", "message": map[string]any{"role": "user", "content": text}})
	return string(b) + "\n"
}

type recorder struct {
	mu   sync.Mutex
	seen []importer.Result
}

func (r *recorder) add(res *importer.Result) {
	r.mu.Lock()
	r.seen = append(r.seen, *res)
	r.mu.Unlock()
}

func (r *recorder) wait(t *testing.T, n int) []importer.Result {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		if len(r.seen) >= n {
			out := append([]importer.Result(nil), r.seen...)
			r.mu.Unlock()
			return out
		}
		r.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d imports", n)
	return nil
}

func TestWatcherImportsNewAndChangedTranscripts(t *testing.T) {
	projects := t.TempDir()
	d, err := db.Open(filepath.Join(t.TempDir(), "vault.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	rec := &recorder{}
	w := &Watcher{DB: d, ProjectsDir: projects, Debounce: 50 * time.Millisecond, OnImport: rec.add}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	for !w.Status().Running {
		time.Sleep(10 * time.Millisecond)
	}

	// A project directory that did not exist when the watcher started.
	dir := filepath.Join(projects, "-home-user-new")
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "s1.jsonl")
	os.WriteFile(path, []byte(userLine("s1", "u1", "hello")), 0o644)
	got := rec.wait(t, 1)
	if got[0].Status != importer.New || got[0].SessionID != "s1" || got[0].Project != "-home-user-new" {
		t.Fatalf("%+v", got[0])
	}

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(userLine("s1", "u2", "more"))
	f.Close()
	got = rec.wait(t, 2)
	if got[1].Status != importer.Resynced || got[1].TotalMessages != 2 {
		t.Fatalf("%+v", got[1])
	}

	st := w.Status()
	if st.LastEventAt == "" || st.LastImportAt == "" || st.LastError != "" || st.DebounceMs != 50 {
		t.Fatalf("status %+v", st)
	}
}

func TestWatcherMissingDirectory(t *testing.T) {
	w := &Watcher{ProjectsDir: filepath.Join(t.TempDir(), "nope"), Log: os.Stderr}
	w.Run(context.Background())
	st := w.Status()
	if st.Running || st.LastError == "" || !st.Enabled {
		t.Fatalf("status %+v", st)
	}
}
