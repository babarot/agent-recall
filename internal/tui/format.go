package tui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/babarot/claude-recall/internal/db"
	"github.com/babarot/claude-recall/internal/worktree"
)

// row is a session with everything the list shows, formatted once at load.
type row struct {
	s db.Session

	title    string
	folder   string // repo name, or the main checkout's name for a worktree
	worktree string // worktree name, empty outside a linked worktree
	gone     bool   // the session directory no longer exists
	mainRoot string // main checkout of a linked worktree
	// group is the folder the list narrows by: the repository, worktrees
	// included, or the directory outside git; groupName is how it shows.
	group, groupName string

	search string // lower-cased text the filter matches against
}

func newRow(s db.Session, home string, wt *worktree.Resolver) row {
	r := row{s: s, title: displayTitle(s)}
	info := wt.Resolve(s.ProjectPath)
	r.gone = !info.Exists
	r.group = realPath(s.ProjectPath)
	switch {
	case info.IsWorktree():
		r.mainRoot = info.MainRoot
		r.folder = shortPath(info.MainRoot, home)
		r.worktree = worktreeName(info.Root)
		r.group = realPath(info.MainRoot)
	case r.gone:
		// A removed worktree can no longer be resolved through git, but
		// the tools that create worktrees put them at recognizable paths.
		if repo, name, group, ok := removedWorktree(s.ProjectPath, home); ok {
			r.folder, r.worktree, r.group = repo, name, group
			break
		}
		r.folder = shortPath(s.ProjectPath, home)
	default:
		r.folder = shortPath(s.ProjectPath, home)
		if info.Root != "" {
			r.group = info.Root
		}
	}
	r.groupName = shortPath(r.group, home)
	if repo, ok := strings.CutPrefix(r.group, herdrGroup); ok {
		r.groupName = repo
	}
	r.search = strings.ToLower(strings.Join([]string{r.title, r.folder, r.worktree, s.GitBranch, s.ID}, " "))
	return r
}

// shortPath drops the ~/src/github.com/ prefix that ghq-style checkouts
// share, and otherwise shortens $HOME to ~.
func shortPath(path, home string) string {
	if path == "" {
		return "?"
	}
	if home != "" {
		if rest, ok := strings.CutPrefix(path, home+"/src/github.com/"); ok {
			return rest
		}
		if path == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(path, home+"/"); ok {
			return "~/" + rest
		}
	}
	return path
}

var (
	// herdr: ~/.herdr/worktrees/<repo>/worktree-<name>
	herdrWorktree = regexp.MustCompile(`/\.herdr/worktrees/([^/]+)/(?:worktree-)?([^/]+)$`)
	// Claude Code: <repo>/.claude/worktrees/<name>
	claudeWorktree = regexp.MustCompile(`^(.+)/\.claude/worktrees/([^/]+)$`)
)

// removedWorktree guesses the repository and worktree name of a worktree
// directory that no longer exists, from where herdr or Claude Code put it,
// and the group it belongs to. herdr's path holds only the repository's
// name, not its owner, so its group is a placeholder New resolves.
func removedWorktree(path, home string) (repo, name, group string, ok bool) {
	if m := claudeWorktree.FindStringSubmatch(path); m != nil {
		return shortPath(m[1], home), m[2], m[1], true
	}
	if m := herdrWorktree.FindStringSubmatch(path); m != nil {
		return m[1], m[2], herdrGroup + m[1], true
	}
	return "", "", "", false
}

// realPath resolves symlinks in an existing path, so one folder reached two
// ways is one group; other paths stay as they are.
func realPath(p string) string {
	if p == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return p
}

// worktreeName is the worktree directory's name without the "worktree-"
// prefix some tools add.
func worktreeName(root string) string {
	return strings.TrimPrefix(filepath.Base(root), "worktree-")
}

// tildePath shortens $HOME to ~ for display.
func tildePath(path, home string) string {
	if home != "" {
		if path == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(path, home+"/"); ok {
			return "~/" + rest
		}
	}
	return path
}

var (
	commandName = regexp.MustCompile(`<command-name>\s*([^<]*?)\s*</command-name>`)
	commandArgs = regexp.MustCompile(`<command-args>\s*([^<]*?)\s*</command-args>`)
	bashInput   = regexp.MustCompile(`<bash-input>\s*([^<]*?)\s*</bash-input>`)
	anyTag      = regexp.MustCompile(`</?[a-zA-Z][\w-]*(\s[^>]*)?>`)
	spaces      = regexp.MustCompile(`\s+`)
)

// displayTitle picks what the list shows for a session: the stored title, or
// failing that the first prompt with Claude Code's markup turned into text.
func displayTitle(s db.Session) string {
	if t := strings.TrimSpace(s.Title); t != "" {
		return t
	}
	return cleanPrompt(s.FirstPrompt)
}

func cleanPrompt(p string) string {
	if m := commandName.FindStringSubmatch(p); m != nil {
		name := m[1]
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		if a := commandArgs.FindStringSubmatch(p); a != nil && a[1] != "" {
			name += " " + a[1]
		}
		return collapse(name)
	}
	if m := bashInput.FindStringSubmatch(p); m != nil {
		return collapse("! " + m[1])
	}
	if t := collapse(anyTag.ReplaceAllString(p, " ")); t != "" {
		return t
	}
	return "(no prompt)"
}

func collapse(s string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

// formatEnded shows a time of day for today, a date and time this year, and
// a full date otherwise.
func formatEnded(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	t, now = t.Local(), now.Local()
	switch {
	case t.Year() == now.Year() && t.YearDay() == now.YearDay():
		return t.Format("15:04")
	case t.Year() == now.Year():
		return t.Format("01/02 15:04")
	default:
		return t.Format("2006/01/02")
	}
}

// relativeDate is how the list shows when a session ended, as cc360 does:
// "Today 15:04", "Yesterday", "3d ago", "Sep 28", "2025-12-31".
func relativeDate(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	t, now = t.Local(), now.Local()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	days := int(today.Sub(day).Hours() / 24)
	switch {
	case days <= 0:
		return "Today " + t.Format("15:04")
	case days == 1:
		return "Yesterday"
	case days < 7:
		return fmt.Sprintf("%dd ago", days)
	case t.Year() == now.Year():
		return t.Format("Jan _2")
	default:
		return t.Format("2006-01-02")
	}
}

func formatSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%dK", n>>10)
	default:
		return fmt.Sprintf("%dB", n)
	}
}
