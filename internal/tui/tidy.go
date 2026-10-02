package tui

import (
	"cmp"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/babarot/claude-recall/internal/db"
)

// Edited files and commands come straight from tool inputs: absolute paths,
// scratch files, commands that start with `cd /some/where &&`. These helpers
// turn them into something to read at a glance.

var (
	tempPath       = regexp.MustCompile(`^(/private)?/tmp/|/scratchpad/`)
	herdrFile      = regexp.MustCompile(`/\.herdr/worktrees/([^/]+)/(?:worktree-)?([^/]+)/(.+)$`)
	leadingCd      = regexp.MustCompile(`^\s*cd\s+\S+\s*(&&|;)\s*`)
	leadingAssign  = regexp.MustCompile(`^\s*[A-Za-z_][A-Za-z0-9_]*=\S*\s*(&&|;)?\s*`)
	subcommandWord = regexp.MustCompile(`^[a-z][a-z0-9_:-]*$`)
)

// fileGroup is the edited files under one place: the session's own folder,
// another repository, a herdr worktree, or elsewhere.
type fileGroup struct {
	name  string // "" for the session's own folder
	files []db.Count
}

// groupFiles sorts edited files into groups, largest first, with names
// relative to their group, and counts the temporary files apart.
func groupFiles(files []db.Count, folder, home string) (groups []fileGroup, temps int) {
	byName := map[string]*fileGroup{}
	var order []string
	add := func(group, rel string, n int) {
		g, ok := byName[group]
		if !ok {
			g = &fileGroup{name: group}
			byName[group] = g
			order = append(order, group)
		}
		g.files = append(g.files, db.Count{Name: rel, N: n})
	}
	for _, f := range files {
		p := f.Name
		switch {
		case tempPath.MatchString(p):
			temps++
		case folder != "" && strings.HasPrefix(p, folder+"/"):
			add("", strings.TrimPrefix(p, folder+"/"), f.N)
		default:
			if m := herdrFile.FindStringSubmatch(p); m != nil {
				add(m[1]+" "+worktreeM+m[2], m[3], f.N)
				continue
			}
			if rest, ok := strings.CutPrefix(p, home+"/src/github.com/"); ok {
				if parts := strings.SplitN(rest, "/", 3); len(parts) == 3 {
					add(parts[1], parts[2], f.N)
					continue
				}
			}
			add(filepath.Dir(tildePath(p, home)), filepath.Base(p), f.N)
		}
	}
	for _, name := range order {
		groups = append(groups, *byName[name])
	}
	slices.SortStableFunc(groups, func(a, b fileGroup) int {
		if a.name == "" || b.name == "" { // the session's own folder first
			return cmp.Compare(boolInt(a.name != ""), boolInt(b.name != ""))
		}
		return cmp.Compare(len(b.files), len(a.files))
	})
	return groups, temps
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// middleEllipsis shortens a path to n cells by dropping middle directories,
// keeping the first one and the file name.
func middleEllipsis(p string, n int) string {
	if len([]rune(p)) <= n {
		return p
	}
	parts := strings.Split(p, "/")
	if len(parts) >= 3 {
		short := parts[0] + "/…/" + strings.Join(parts[len(parts)-2:], "/")
		if len([]rune(short)) <= n {
			return short
		}
		short = "…/" + parts[len(parts)-1]
		if len([]rune(short)) <= n {
			return short
		}
	}
	r := []rune(p)
	return "…" + string(r[len(r)-n+1:])
}

// commandParts splits a shell command for display: the program, its
// arguments, and what follows the first pipe. A leading `cd dir &&` and
// variable assignments are dropped; only the first line counts.
func commandParts(cmd string) (prog, args, rest string) {
	c, _, _ := strings.Cut(cmd, "\n")
	for range 4 {
		next := leadingAssign.ReplaceAllString(leadingCd.ReplaceAllString(c, ""), "")
		if next == c {
			break
		}
		c = next
	}
	c = collapse(c)
	main, after, piped := strings.Cut(c, " | ")
	if piped {
		rest = "| " + after
	}
	prog, args, _ = strings.Cut(main, " ")
	return prog, args, rest
}

// subcommandTools are counted by subcommand ("git commit", "go test"),
// since the program alone says little.
var subcommandTools = map[string]bool{
	"git": true, "go": true, "gh": true, "npm": true, "pnpm": true, "yarn": true, "bun": true,
	"deno": true, "cargo": true, "docker": true, "kubectl": true, "nix": true, "make": true,
	"terraform": true, "uv": true, "pip": true, "brew": true, "mise": true,
}

// programOf names what a command runs, for counting: the program, plus the
// subcommand for tools like git and go.
func programOf(cmd string) string {
	prog, args, _ := commandParts(cmd)
	if prog == "" {
		return ""
	}
	prog = filepath.Base(prog)
	if subcommandTools[prog] {
		// The subcommand is the first plain word: not a flag, and not a
		// flag's value such as the path in `git -C /repo status`.
		for _, a := range strings.Fields(args) {
			if subcommandWord.MatchString(a) {
				return prog + " " + a
			}
		}
	}
	return prog
}

// commandCounts counts commands by program, most used first.
func commandCounts(cmds []string) []db.Count {
	n := map[string]int{}
	for _, c := range cmds {
		if p := programOf(c); p != "" {
			n[p]++
		}
	}
	out := make([]db.Count, 0, len(n))
	for name, c := range n {
		out = append(out, db.Count{Name: name, N: c})
	}
	slices.SortFunc(out, func(a, b db.Count) int { return cmp.Or(cmp.Compare(b.N, a.N), cmp.Compare(a.Name, b.Name)) })
	return out
}
