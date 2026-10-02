package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// layout builds a main checkout at <tmp>/src/repo and a linked worktree at
// <tmp>/wt/feature, the way `git worktree add` does.
func layout(t *testing.T) (main, wt string) {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main = filepath.Join(tmp, "src", "repo")
	wt = filepath.Join(tmp, "wt", "feature")
	mkdir(t, filepath.Join(main, ".git", "worktrees", "feature"))
	write(t, filepath.Join(main, ".git", "worktrees", "feature", "commondir"), "../..\n")
	write(t, filepath.Join(wt, ".git"), "gitdir: "+filepath.Join(main, ".git", "worktrees", "feature")+"\n")
	return main, wt
}

func TestResolveMainCheckout(t *testing.T) {
	main, _ := layout(t)
	got := NewResolver().Resolve(main)
	if !got.Exists || got.Root != main || got.IsWorktree() {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveWorktree(t *testing.T) {
	main, wt := layout(t)
	got := NewResolver().Resolve(wt)
	if got.Root != wt || got.MainRoot != main {
		t.Fatalf("got %+v, want root %s main %s", got, wt, main)
	}
}

func TestResolveSubdirectoryOfWorktree(t *testing.T) {
	main, wt := layout(t)
	sub := filepath.Join(wt, "pkg", "x")
	mkdir(t, sub)
	got := NewResolver().Resolve(sub)
	if got.Root != wt || got.MainRoot != main {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveRelativeGitdir(t *testing.T) {
	main, wt := layout(t)
	write(t, filepath.Join(wt, ".git"), "gitdir: ../../src/repo/.git/worktrees/feature\n")
	if got := NewResolver().Resolve(wt); got.MainRoot != main {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveSubmoduleIsNotWorktree(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "repo", "vendor", "lib")
	mkdir(t, filepath.Join(tmp, "repo", ".git", "modules", "lib"))
	write(t, filepath.Join(sub, ".git"), "gitdir: ../../.git/modules/lib\n")
	got := NewResolver().Resolve(sub)
	if !got.Exists || got.IsWorktree() {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveMissingDirectory(t *testing.T) {
	got := NewResolver().Resolve(filepath.Join(t.TempDir(), "gone"))
	if got.Exists {
		t.Fatalf("got %+v", got)
	}
}
