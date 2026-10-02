// Package worktree finds the repository a directory belongs to and, for a
// linked git worktree, the main checkout it was created from.
package worktree

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Info describes a session directory.
type Info struct {
	Exists bool // the directory is still on disk
	// Root is the top of the checkout that contains the directory: the
	// worktree itself for a linked worktree. Empty outside git.
	Root string
	// MainRoot is the main checkout of a linked worktree. Empty when the
	// directory is not in a linked worktree or the main checkout is unknown.
	MainRoot string
}

// IsWorktree reports whether the directory is in a linked worktree whose main
// checkout is known.
func (i Info) IsWorktree() bool { return i.MainRoot != "" }

// Resolver caches lookups, since many sessions share a directory.
type Resolver struct {
	mu    sync.Mutex
	cache map[string]Info
}

// NewResolver returns an empty Resolver.
func NewResolver() *Resolver { return &Resolver{cache: map[string]Info{}} }

// Resolve inspects dir. It never fails: anything it cannot read is reported as
// unknown.
func (r *Resolver) Resolve(dir string) Info {
	r.mu.Lock()
	defer r.mu.Unlock()
	if info, ok := r.cache[dir]; ok {
		return info
	}
	info := resolve(dir)
	r.cache[dir] = info
	return info
}

func resolve(dir string) Info {
	if dir == "" {
		return Info{}
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return Info{}
	}
	info := Info{Exists: true}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		real = dir
	}
	for d := real; ; d = filepath.Dir(d) {
		git := filepath.Join(d, ".git")
		fi, err := os.Lstat(git)
		if err == nil {
			info.Root = d
			if fi.Mode().IsRegular() {
				info.MainRoot = mainRoot(git, d)
			}
			return info
		}
		if parent := filepath.Dir(d); parent == d {
			return info
		}
	}
}

// mainRoot follows a worktree's gitfile ("gitdir: <admin dir>") to the shared
// git dir named in <admin dir>/commondir. When that dir is a ".git" directory,
// its parent is the main checkout. A bare repository or a separate git dir
// leaves the main checkout unknown.
func mainRoot(gitfile, worktree string) string {
	b, err := os.ReadFile(gitfile)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(b))
	admin, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return ""
	}
	admin = strings.TrimSpace(admin)
	if !filepath.IsAbs(admin) {
		admin = filepath.Join(worktree, admin)
	}
	c, err := os.ReadFile(filepath.Join(admin, "commondir"))
	if err != nil {
		return "" // a submodule's gitfile has no commondir
	}
	common := strings.TrimSpace(string(c))
	if !filepath.IsAbs(common) {
		common = filepath.Join(admin, common)
	}
	common = filepath.Clean(common)
	if filepath.Base(common) != ".git" {
		return ""
	}
	return filepath.Dir(common)
}
