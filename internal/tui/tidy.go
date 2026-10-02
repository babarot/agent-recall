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
	assignment     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=`)
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

// simpleCommands splits a shell script into its simple commands, each as
// its words: split at ; & | && || newlines and parentheses, with quotes,
// $(...), ${...} and backticks kept inside one word, line continuations
// joined and comments dropped. It is no shell parser, only enough to tell
// which programs a command runs.
func simpleCommands(script string) [][]string {
	var (
		out    [][]string
		words  []string
		word   strings.Builder
		inWord bool
	)
	endWord := func() {
		if inWord {
			words = append(words, word.String())
		}
		word.Reset()
		inWord = false
	}
	endCmd := func() {
		endWord()
		if len(words) > 0 {
			out = append(out, words)
		}
		words = nil
	}
	r := []rune(script)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case c == '\\' && i+1 < len(r):
			if r[i+1] != '\n' {
				word.WriteRune(r[i+1])
				inWord = true
			}
			i++
		case c == '\'' || c == '"':
			// Quotes: up to the matching one, dropped from the word.
			j := i + 1
			for j < len(r) && r[j] != c {
				if c == '"' && r[j] == '\\' && j+1 < len(r) {
					j++
				}
				j++
			}
			word.WriteString(string(r[i+1 : min(j, len(r))]))
			inWord = true
			i = j
		case c == '`' || (c == '$' && i+1 < len(r) && (r[i+1] == '(' || r[i+1] == '{')):
			// A substitution: kept whole, nesting included.
			j := closing(r, i)
			word.WriteString(string(r[i:min(j+1, len(r))]))
			inWord = true
			i = j
		case c == '#' && !inWord:
			for i+1 < len(r) && r[i+1] != '\n' {
				i++
			}
		case c == '&' && i > 0 && (r[i-1] == '>' || r[i-1] == '<'), c == '>' && i > 0 && r[i-1] == '&':
			word.WriteRune(c) // 2>&1, &>file
			inWord = true
		case c == '(' && i+1 < len(r) && r[i+1] == ')':
			word.WriteString("()") // a function definition: name()
			inWord = true
			i++
		case c == ';' || c == '&' || c == '|' || c == '\n' || c == '(' || c == ')':
			endCmd()
		case c == ' ' || c == '\t':
			endWord()
		default:
			word.WriteRune(c)
			inWord = true
		}
	}
	endCmd()
	return out
}

// closing returns the index of what closes the substitution starting at i:
// a backtick, or the parenthesis or brace matching the one after $.
func closing(r []rune, i int) int {
	if r[i] == '`' {
		for j := i + 1; j < len(r); j++ {
			if r[j] == '`' {
				return j
			}
		}
		return len(r)
	}
	open, shut := r[i+1], ')'
	if open == '{' {
		shut = '}'
	}
	depth := 0
	for j := i + 1; j < len(r); j++ {
		switch r[j] {
		case open:
			depth++
		case shut:
			if depth--; depth == 0 {
				return j
			}
		case '\'':
			for j++; j < len(r) && r[j] != '\''; j++ {
			}
		}
	}
	return len(r)
}

var (
	// shellWords open or close a compound command; the command they
	// introduce follows them.
	shellWords = map[string]bool{
		"do": true, "then": true, "else": true, "elif": true, "if": true, "while": true, "until": true,
		"!": true, "{": true, "}": true, "done": true, "fi": true, "time": true,
	}
	// headers are compound commands whose own words are not a command:
	// `for f in a b`.
	headers = map[string]bool{"for": true, "select": true, "in": true}
	// wrappers run the command that follows them.
	wrappers = map[string]bool{"builtin": true, "command": true, "exec": true, "nohup": true, "sudo": true, "env": true, "timeout": true}
	// plumbing is shell housekeeping, not what the command is about.
	plumbing = map[string]bool{
		"cd": true, "set": true, "export": true, "unset": true, "local": true, "[": true, "[[": true,
		"test": true, "true": true, "false": true, ":": true, "trap": true, "shopt": true,
	}
)

// commandWords returns the words of the first real command in a simple
// command, without leading assignments, shell keywords and wrappers, or nil
// when it runs nothing of interest.
func commandWords(words []string) []string {
	for len(words) > 0 {
		w := words[0]
		switch {
		case assignment.MatchString(w), shellWords[w]:
			words = words[1:]
		case headers[w], plumbing[w]:
			return nil
		case strings.Contains(w, "()"):
			words = words[1:] // a function definition, name() {, then its body
		case len(words) > 1 && words[1] == "()":
			words = words[2:]
		case wrappers[w]:
			words = words[1:]
			// Their own flags, and timeout's duration.
			for len(words) > 0 && (strings.HasPrefix(words[0], "-") || w == "timeout" && words[0] != "" && words[0][0] >= '0' && words[0][0] <= '9') {
				words = words[1:]
			}
		case strings.HasPrefix(w, "$") && !strings.Contains(w, "/"), strings.HasPrefix(w, "-"), w == "":
			return nil // a command in a variable, or not a command at all
		default:
			return words
		}
	}
	return nil
}

// subcommandTools are counted by subcommand ("git commit", "go test"),
// since the program alone says little.
var subcommandTools = map[string]bool{
	"git": true, "go": true, "gh": true, "npm": true, "pnpm": true, "yarn": true, "bun": true,
	"deno": true, "cargo": true, "docker": true, "kubectl": true, "nix": true, "make": true,
	"terraform": true, "uv": true, "pip": true, "brew": true, "mise": true,
}

// programOf names what a command runs, for counting: its first real
// program, plus the subcommand for tools like git and go.
func programOf(cmd string) string {
	inCase := false
	for _, sc := range simpleCommands(cmd) {
		// A case statement's patterns read like commands; skip all of it.
		if sc[0] == "case" {
			inCase = true
		}
		if inCase {
			inCase = !slices.Contains(sc, "esac")
			continue
		}
		words := commandWords(sc)
		if len(words) == 0 {
			continue
		}
		prog := filepath.Base(words[0])
		if subcommandTools[prog] {
			// The subcommand is the first plain word: not a flag, and not a
			// flag's value such as the path in `git -C /repo status`.
			for _, a := range words[1:] {
				if subcommandWord.MatchString(a) {
					return prog + " " + a
				}
			}
		}
		return prog
	}
	return ""
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
