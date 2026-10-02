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

func TestProgramOf(t *testing.T) {
	cases := []struct{ in, want string }{
		{"cd /Users/me/src/app && go test ./... 2>&1 | tail -5", "go test"},
		{"S=/tmp/x && cd $S && python3 run.py", "python3"},
		{"git status\nsecond line", "git status"},
		// Assignments whose values hold spaces and flags.
		{`c=$(readlink -f "$(command -v codex)"); echo $c`, "echo"},
		{`UA='Mozilla/5.0 (Macintosh; Intel)' curl -A "$UA" x`, "curl"},
		{"FROM=$(($(date -v-30d +%s) * 1000)); for i in a b; do pup api $i; done", "pup"},
		// Loops, comments, continuations, subshells, functions, wrappers.
		{"for f in a.tf b.tf; do\n  terraform fmt $f\ndone", "terraform fmt"},
		{"until [ -f /tmp/done ]; do sleep 5; done", "sleep"},
		{"# check the alerts first\ngh api repos/x/y", "gh api"},
		{"FILENAME=a.tf \\\n  conftest test a.tf", "conftest"},
		{"(npx wrangler dev > /tmp/log 2>&1 &)", "npx"},
		{`q() { gh api -X GET search/issues; }`, "gh api"},
		{"builtin cd ~/x\ngit fetch", "git fetch"},
		{"timeout 60 go test ./...", "go test"},
		{"q(){ pup metrics query; }; q a", "pup"},
		{"for n in 1 2; do\n  case $n in\n    1) team=a;;\n    2) team=b;;\n  esac\n  gh issue view $n\ndone", "gh issue"},
		{`"$SKILL_DIR/get-summary.ts" --json`, "get-summary.ts"},
		{`K="kubectl --context dev"; $K get pods`, ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := programOf(c.in); got != c.want {
			t.Errorf("programOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCommandCounts(t *testing.T) {
	got := commandCounts([]string{
		"cd /x && go test ./...", "go test ./internal/...", "go vet ./...",
		"git -C /repo status", "git commit -m x", "/usr/bin/python3 a.py", "python3 b.py", "",
	})
	want := []db.Count{{Name: "go test", N: 2}, {Name: "python3", N: 2}, {Name: "git commit", N: 1}, {Name: "git status", N: 1}, {Name: "go vet", N: 1}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %+v want %+v", i, got[i], want[i])
		}
	}
}
