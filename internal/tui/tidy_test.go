package tui

import (
	"testing"

	"github.com/babarot/claude-recall/internal/db"
)

func TestGroupFiles(t *testing.T) {
	home := "/Users/me"
	files := []db.Count{
		{Name: "/Users/me/src/github.com/me/app/main.go", N: 2},
		{Name: "/Users/me/src/github.com/me/lib/a/b/c.go", N: 1},
		{Name: "/Users/me/src/github.com/me/lib/d.go", N: 1},
		{Name: "/private/tmp/x/scratch.py", N: 1},
		{Name: "/Users/me/.herdr/worktrees/blog/worktree-calm-sea/posts/a.md", N: 3},
		{Name: "/Users/me/.config/foo/bar.toml", N: 1},
	}
	groups, temps := groupFiles(files, "/Users/me/src/github.com/me/app", home)
	if temps != 1 {
		t.Errorf("temps %d", temps)
	}
	got := map[string][]db.Count{}
	for _, g := range groups {
		got[g.name] = g.files
	}
	if groups[0].name != "" || got[""][0].Name != "main.go" {
		t.Errorf("own folder first, relative: %+v", groups)
	}
	if len(got["lib"]) != 2 || got["lib"][0].Name != "a/b/c.go" {
		t.Errorf("other repo: %+v", got["lib"])
	}
	if got["blog ⌥calm-sea"][0].Name != "posts/a.md" {
		t.Errorf("herdr worktree: %+v", got)
	}
	if got["~/.config/foo"][0].Name != "bar.toml" {
		t.Errorf("elsewhere: %+v", got)
	}
}

func TestMiddleEllipsis(t *testing.T) {
	cases := map[string]string{
		"internal/tui/view.go":                   "internal/tui/view.go",
		"internal/very/deep/path/to/the/file.go": "internal/…/the/file.go",
	}
	for in, want := range cases {
		if got := middleEllipsis(in, 24); got != want {
			t.Errorf("middleEllipsis(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCommandParts(t *testing.T) {
	cases := []struct{ in, prog, args, rest string }{
		{"cd /Users/me/src/app && go test ./... 2>&1 | tail -5", "go", "test ./... 2>&1", "| tail -5"},
		{"S=/tmp/x && cd $S && python3 run.py", "python3", "run.py", ""},
		{"git status\nsecond line", "git", "status", ""},
	}
	for _, c := range cases {
		p, a, r := commandParts(c.in)
		if p != c.prog || a != c.args || r != c.rest {
			t.Errorf("commandParts(%q) = %q %q %q", c.in, p, a, r)
		}
	}
}
