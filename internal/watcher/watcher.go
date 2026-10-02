// Package watcher keeps the archive in step with ~/.claude/projects while
// the web UI or the MCP server runs (see docs/adr/002).
//
// It polls file sizes and modification times instead of using filesystem
// notifications: on macOS, fsnotify's kqueue backend needs an open file
// descriptor for every transcript, and it does not watch directories
// recursively. A scan of a thousand transcripts costs a few milliseconds.
package watcher

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/importer"
)

const (
	// DefaultDebounce is how long a file must stay unchanged before it is
	// imported, so a burst of writes leads to one import.
	DefaultDebounce = 300 * time.Millisecond
	pollInterval    = 250 * time.Millisecond
)

// Status is the watcher state reported by /api/status.
type Status struct {
	Enabled      bool   `json:"enabled"`
	Running      bool   `json:"running"`
	ProjectsDir  string `json:"projectsDir"`
	DebounceMs   int    `json:"debounceMs"`
	LastEventAt  string `json:"lastEventAt,omitempty"`
	LastImportAt string `json:"lastImportAt,omitempty"`
	LastError    string `json:"lastError,omitempty"`
	LastErrorAt  string `json:"lastErrorAt,omitempty"`
}

// Watcher imports changed transcripts.
type Watcher struct {
	DB          *db.DB
	ProjectsDir string
	Debounce    time.Duration
	// OnImport is called after a new or changed session was imported.
	OnImport func(*importer.Result)
	Log      io.Writer

	mu     sync.Mutex
	status Status
}

// Status returns a snapshot of the watcher state.
func (w *Watcher) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

func (w *Watcher) update(f func(*Status)) {
	w.mu.Lock()
	f(&w.status)
	w.mu.Unlock()
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

func (w *Watcher) fail(msg string) {
	w.update(func(s *Status) { s.LastError, s.LastErrorAt = msg, now() })
}

type fileState struct {
	size  int64
	mtime time.Time
}

func scan(dir string) map[string]fileState {
	out := map[string]fileState{}
	projects, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		sub := filepath.Join(dir, p.Name())
		files, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			info, err := f.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			out[filepath.Join(sub, f.Name())] = fileState{size: info.Size(), mtime: info.ModTime()}
		}
	}
	return out
}

// Run watches until ctx is canceled. Files present when it starts are
// taken as already imported.
func (w *Watcher) Run(ctx context.Context) {
	if w.Debounce == 0 {
		w.Debounce = DefaultDebounce
	}
	if w.Log == nil {
		w.Log = os.Stderr
	}
	w.update(func(s *Status) {
		*s = Status{Enabled: true, ProjectsDir: w.ProjectsDir, DebounceMs: int(w.Debounce / time.Millisecond)}
	})

	fi, err := os.Stat(w.ProjectsDir)
	switch {
	case err != nil:
		w.fail(w.ProjectsDir + " not found")
		fmt.Fprintf(w.Log, "[watcher] %s not found; watcher disabled.\n", w.ProjectsDir)
		return
	case !fi.IsDir():
		w.fail(w.ProjectsDir + " is not a directory")
		fmt.Fprintf(w.Log, "[watcher] %s is not a directory; watcher disabled.\n", w.ProjectsDir)
		return
	}

	w.update(func(s *Status) { s.Running = true })
	defer w.update(func(s *Status) { s.Running = false })

	seen := scan(w.ProjectsDir)
	pending := map[string]time.Time{} // path -> time of the last change
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		current := scan(w.ProjectsDir)
		changed := false
		for path, st := range current {
			if old, ok := seen[path]; !ok || old != st {
				pending[path] = time.Now()
				changed = true
			}
		}
		seen = current
		if changed {
			w.update(func(s *Status) { s.LastEventAt = now() })
		}
		for path, at := range pending {
			if time.Since(at) < w.Debounce {
				continue
			}
			delete(pending, path)
			w.importFile(path)
		}
	}
}

func (w *Watcher) importFile(path string) {
	r, err := importer.ImportFile(w.DB, path, nil)
	if err != nil {
		w.fail(err.Error())
		fmt.Fprintf(w.Log, "[watcher] import failed for %s: %v\n", path, err)
		return
	}
	if r == nil {
		return
	}
	w.update(func(s *Status) { s.LastImportAt, s.LastError, s.LastErrorAt = now(), "", "" })
	if r.Status != importer.Unchanged && w.OnImport != nil {
		w.OnImport(r)
	}
}
