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

	search string // lower-cased text the filter matches against
}

func newRow(s db.Session, home string, wt *worktree.Resolver) row {
	r := row{s: s, title: displayTitle(s)}
	info := wt.Resolve(s.ProjectPath)
	r.gone = !info.Exists
	if info.IsWorktree() {
		r.mainRoot = info.MainRoot
		r.folder = shortPath(info.MainRoot, home)
		r.worktree = worktreeName(info.Root)
	} else {
		r.folder = shortPath(s.ProjectPath, home)
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

// formatAge is a compact age for narrow terminals: 45m, 3h, 12d, 2y.
func formatAge(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", max(0, int(d.Minutes())))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
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

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	h, m := int(d.Hours()), int(d.Minutes())%60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}
